package handlers

import (
	"context"
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/rubenwoldhuis/recipes/internal/database"
	"github.com/rubenwoldhuis/recipes/internal/llm"
	"github.com/rubenwoldhuis/recipes/internal/models"
	"github.com/rubenwoldhuis/recipes/internal/tools"
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
const pendingExpiry = 7 * 24 * time.Hour

func (b *BackgroundGenerator) Start(ctx context.Context) {
	go func() {
		// Poll every minute to check if generation is due.
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		var lastRun time.Time

		for {
			select {
			case <-ticker.C:
				// Always run expiry cleanup on each tick.
				if n, err := b.queries.DeleteExpiredPendingRecipes(ctx, pendingExpiry); err != nil {
					log.Printf("Background: failed to clean up expired pending recipes: %v", err)
				} else if n > 0 {
					log.Printf("Background: discarded %d expired pending recipe(s)", n)
				}

				enabled, interval, count, servings, maxRetries := b.loadSettings(ctx)
				if !enabled || interval <= 0 {
					continue
				}
				if time.Since(lastRun) < interval {
					continue
				}
				lastRun = time.Now()
				nextRun := lastRun.Add(interval)
				log.Printf("Background generation: starting %d recipe(s)", count)
				b.runGeneration(ctx, count, servings, maxRetries, nextRun)

			case <-b.stop:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (b *BackgroundGenerator) Stop() {
	close(b.stop)
}

func (b *BackgroundGenerator) loadSettings(ctx context.Context) (enabled bool, interval time.Duration, count, servings, maxRetries int) {
	if val, _ := b.queries.GetSetting(ctx, "background_generation_enabled"); val != "true" {
		return false, 0, 0, 0, 0
	}

	intervalSecs := 3600 // default 1 hour
	if val, _ := b.queries.GetSetting(ctx, "background_generation_interval"); val != "" {
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			intervalSecs = n
		}
	}

	count = 1
	if val, _ := b.queries.GetSetting(ctx, "background_generation_count"); val != "" {
		if n, err := strconv.Atoi(val); err == nil && n > 0 && n <= 10 {
			count = n
		}
	}

	servings = 4
	if val, _ := b.queries.GetSetting(ctx, "default_servings"); val != "" {
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			servings = n
		}
	}

	maxRetries = 3
	if val, _ := b.queries.GetSetting(ctx, "background_generation_max_retries"); val != "" {
		if n, err := strconv.Atoi(val); err == nil && n >= 0 && n <= 10 {
			maxRetries = n
		}
	}

	return true, time.Duration(intervalSecs) * time.Second, count, servings, maxRetries
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

	for i := 0; i < count; i++ {
		prompt := llm.BuildBackgroundGeneratePrompt(titles, cuisineCounts, i+1, count, servings)
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
				imageURL, err := b.imageSearcher.SearchRecipeImage(context.Background(), r.Title)
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
		// Update the in-process title list so subsequent recipes in this batch avoid duplicates.
		titles = append(titles, recipe.Title)
	}
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
		recipe, messages, err := b.orchestrator.GenerateWithTag(ctx, prompt, events, "background")
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
