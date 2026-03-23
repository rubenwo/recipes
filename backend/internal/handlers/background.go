package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/rubenwo/mise/internal/database"
	"github.com/rubenwo/mise/internal/llm"
	"github.com/rubenwo/mise/internal/models"
	"github.com/rubenwo/mise/internal/tools"
)

// BackgroundGenerator periodically generates recipes and saves them to the DB.
type BackgroundGenerator struct {
	queries       *database.Queries
	orchestrator  *llm.Orchestrator
	hub           *llm.Hub
	imageSearcher *tools.ImageSearcher
	stop          chan struct{}
}

func NewBackgroundGenerator(q *database.Queries, o *llm.Orchestrator, hub *llm.Hub, imageSearcher *tools.ImageSearcher) *BackgroundGenerator {
	return &BackgroundGenerator{queries: q, orchestrator: o, hub: hub, imageSearcher: imageSearcher, stop: make(chan struct{})}
}

// Start launches the background generator loop. It reads its configuration from
// app_settings on every tick so changes take effect without a restart.
// The schedule is cron-like: a set of weekdays + a time of day. The last run
// time is persisted in the database so service restarts do not trigger an
// immediate run.
const pendingExpiry = 7 * 24 * time.Hour

func (b *BackgroundGenerator) Start(ctx context.Context) {
	go func() {
		// Poll every minute to check if generation is due.
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// Always run expiry cleanup on each tick.
				if n, err := b.queries.DeleteExpiredPendingRecipes(ctx, pendingExpiry); err != nil {
					log.Printf("Background: failed to clean up expired pending recipes: %v", err)
				} else if n > 0 {
					log.Printf("Background: discarded %d expired pending recipe(s)", n)
				}

				s := b.loadSettings(ctx)
				if !s.enabled || len(s.days) == 0 || s.timeHour < 0 {
					continue
				}

				now := time.Now()
				if !s.days[now.Weekday()] {
					continue // today is not a scheduled day
				}

				todayTarget := time.Date(now.Year(), now.Month(), now.Day(), s.timeHour, s.timeMin, 0, 0, now.Location())
				if now.Before(todayTarget) {
					continue // not yet time today
				}

				// Load persisted last run time to avoid re-running after a restart.
				lastRun := b.loadLastRun(ctx)
				if !lastRun.IsZero() && lastRun.After(todayTarget) {
					continue // already ran since today's scheduled time
				}

				// Persist last run before starting so a crash/restart won't double-fire.
				if err := b.queries.SetSetting(ctx, "background_generation_last_run", now.Format(time.RFC3339)); err != nil {
					log.Printf("Background: failed to persist last run time: %v", err)
				}

				nextRun := todayTarget.Add(24 * time.Hour)
				log.Printf("Background generation: starting %d recipe(s)", s.count)
				b.runGeneration(ctx, s.count, s.servings, s.maxRetries, nextRun)

			case <-b.stop:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (b *BackgroundGenerator) loadLastRun(ctx context.Context) time.Time {
	val, _ := b.queries.GetSetting(ctx, "background_generation_last_run")
	if val == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, val)
	if err != nil {
		return time.Time{}
	}
	return t
}

func (b *BackgroundGenerator) Stop() {
	close(b.stop)
}

type bgSettings struct {
	enabled    bool
	days       map[time.Weekday]bool // which weekdays to run on
	timeHour   int                  // -1 if not configured
	timeMin    int
	count      int
	servings   int
	maxRetries int
}

func (b *BackgroundGenerator) loadSettings(ctx context.Context) bgSettings {
	s := bgSettings{timeHour: -1, days: map[time.Weekday]bool{}}

	if val, _ := b.queries.GetSetting(ctx, "background_generation_enabled"); val != "true" {
		return s
	}
	s.enabled = true

	// Days of week: comma-separated weekday numbers (0=Sunday … 6=Saturday).
	if val, _ := b.queries.GetSetting(ctx, "background_generation_days"); val != "" {
		for _, part := range strings.Split(val, ",") {
			part = strings.TrimSpace(part)
			if n, err := strconv.Atoi(part); err == nil && n >= 0 && n <= 6 {
				s.days[time.Weekday(n)] = true
			}
		}
	}

	// Required time-of-day: stored as "HH:MM", e.g. "08:30".
	if val, _ := b.queries.GetSetting(ctx, "background_generation_time"); val != "" {
		var h, m int
		if _, err := fmt.Sscanf(val, "%d:%d", &h, &m); err == nil && h >= 0 && h <= 23 && m >= 0 && m <= 59 {
			s.timeHour = h
			s.timeMin = m
		}
	}

	s.count = 1
	if val, _ := b.queries.GetSetting(ctx, "background_generation_count"); val != "" {
		if n, err := strconv.Atoi(val); err == nil && n > 0 && n <= 10 {
			s.count = n
		}
	}

	s.servings = 4
	if val, _ := b.queries.GetSetting(ctx, "default_servings"); val != "" {
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			s.servings = n
		}
	}

	s.maxRetries = 3
	if val, _ := b.queries.GetSetting(ctx, "background_generation_max_retries"); val != "" {
		if n, err := strconv.Atoi(val); err == nil && n >= 0 && n <= 10 {
			s.maxRetries = n
		}
	}

	return s
}

func (b *BackgroundGenerator) runGeneration(ctx context.Context, count, servings, maxRetries int, nextRun time.Time) {
	titles, err := b.queries.ListRecipeTitles(ctx)
	if err != nil {
		log.Printf("Background generation: failed to list titles: %v", err)
	}
	cuisineCounts, err := b.queries.ListCuisineCounts(ctx)
	if err != nil {
		log.Printf("Background generation: failed to list cuisine counts: %v", err)
	}
	cuisineCounts = llm.SeedCuisineCounts(cuisineCounts)

	for i := 0; i < count; i++ {
		targetCuisine := leastRepresentedCuisine(cuisineCounts)
		prompt := llm.BuildBackgroundGeneratePrompt(targetCuisine, titles, i+1, count, servings)
		recipe := b.generateWithRetry(ctx, prompt, i+1, count, servings, maxRetries, nextRun)
		if recipe == nil {
			continue
		}

		if err := b.queries.CreatePendingRecipe(ctx, recipe); err != nil {
			log.Printf("Background generation: failed to save pending recipe %q: %v", recipe.Title, err)
			continue
		}
		log.Printf("Background generation: queued pending recipe %q", recipe.Title)

		// Fetch an image in the background; update the pending recipe and re-broadcast with image.
		if b.imageSearcher != nil {
			go func(r *models.Recipe) {
				filename := "pending-" + strconv.Itoa(r.ID)
				imageURL, err := b.imageSearcher.SearchAndDownloadRecipeImage(context.Background(), r.Title, filename)
				if err != nil {
					log.Printf("Background generation: image fetch for %q failed: %v", r.Title, err)
					b.hub.Publish(llm.SSEEvent{Type: "pending_added", Data: *r})
					return
				}
				if err := b.queries.SetPendingRecipeImage(context.Background(), r.ID, imageURL); err != nil {
					log.Printf("Background generation: failed to save image for %q: %v", r.Title, err)
				} else {
					r.ImageURL = imageURL
				}
				b.hub.Publish(llm.SSEEvent{Type: "pending_added", Data: *r})
			}(recipe)
		} else {
			b.hub.Publish(llm.SSEEvent{Type: "pending_added", Data: *recipe})
		}
		// Update in-process tracking so subsequent recipes in this batch get diverse cuisines.
		titles = append(titles, recipe.Title)
		if recipe.CuisineType != "" {
			cuisineCounts[recipe.CuisineType]++
		}
	}
}

// leastRepresentedCuisine returns the cuisine with the fewest recipes, so background
// generation can be directed to fill gaps in the collection deterministically.
// Returns an empty string if counts is empty (model chooses freely).
func leastRepresentedCuisine(counts map[string]int) string {
	var best string
	bestCount := -1
	for cuisine, count := range counts {
		if bestCount < 0 || count < bestCount {
			bestCount = count
			best = cuisine
		}
	}
	return best
}

func (b *BackgroundGenerator) saveChat(ctx context.Context, prompt string, messages []llm.Message) {
	messagesJSON, err := json.Marshal(messages)
	if err != nil {
		log.Printf("Background generation: failed to marshal chat messages: %v", err)
		return
	}
	if err := b.queries.CreateGenerationChat(ctx, prompt, b.orchestrator.Model(), messagesJSON); err != nil {
		log.Printf("Background generation: failed to save chat: %v", err)
	}
}

// generateWithRetry attempts generation up to 1+maxRetries times, retrying on JSON parse
// errors or wrong serving count. It stops early if the next scheduled run is imminent.
func (b *BackgroundGenerator) generateWithRetry(ctx context.Context, prompt string, idx, total, wantServings, maxRetries int, nextRun time.Time) *models.Recipe {
	attempts := 1 + maxRetries
	for attempt := 1; attempt <= attempts; attempt++ {
		// Stop if the next run window is less than 30 seconds away.
		if time.Until(nextRun) < 30*time.Second {
			log.Printf("Background generation: recipe %d/%d skipping retry — next run imminent", idx, total)
			return nil
		}

		events := make(chan llm.SSEEvent, 20)
		go func() {
			for ev := range events {
				b.hub.Publish(ev)
			}
		}()

		// Prefer providers tagged "background" for scheduled tasks (slower/always-on hardware).
		recipe, messages, err := b.orchestrator.GenerateWithTag(ctx, prompt, events, "background-generation")
		if err != nil {
			if strings.Contains(err.Error(), "failed to parse recipe JSON") && attempt < attempts {
				log.Printf("Background generation: recipe %d/%d attempt %d/%d failed (invalid JSON), retrying: %v", idx, total, attempt, attempts, err)
				continue
			}
			log.Printf("Background generation: recipe %d/%d failed: %v", idx, total, err)
			return nil
		}

		if recipe.Servings != wantServings {
			if attempt < attempts {
				log.Printf("Background generation: recipe %d/%d attempt %d/%d returned %d servings (want %d), retrying", idx, total, attempt, attempts, recipe.Servings, wantServings)
				continue
			}
			log.Printf("Background generation: recipe %d/%d accepted with %d servings after %d attempt(s) (wanted %d)", idx, total, recipe.Servings, attempts, wantServings)
		} else if attempt > 1 {
			log.Printf("Background generation: recipe %d/%d succeeded on attempt %d", idx, total, attempt)
		}
		b.saveChat(ctx, prompt, messages)
		return recipe
	}
	return nil
}
