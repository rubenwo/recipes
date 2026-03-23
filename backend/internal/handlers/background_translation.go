package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rubenwo/mise/internal/database"
	"github.com/rubenwo/mise/internal/translation"
)

// translationBatchSize caps how many ingredients are translated per run to keep
// each job short and LLM cost predictable.
const translationBatchSize = 50

// BackgroundTranslator pre-populates the translation cache on a cron-like
// schedule so ingredients are already translated when users open AH ordering.
type BackgroundTranslator struct {
	queries    *database.Queries
	translator *translation.Translator
	stop       chan struct{}
}

func NewBackgroundTranslator(q *database.Queries, t *translation.Translator) *BackgroundTranslator {
	return &BackgroundTranslator{queries: q, translator: t, stop: make(chan struct{})}
}

// Start launches the background translation loop. Schedule is read from
// app_settings on every tick so changes take effect without a restart.
func (b *BackgroundTranslator) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s := b.loadSettings(ctx)
				if !s.enabled || len(s.days) == 0 || s.timeHour < 0 {
					continue
				}

				now := time.Now()
				if !s.days[now.Weekday()] {
					continue
				}

				todayTarget := time.Date(now.Year(), now.Month(), now.Day(), s.timeHour, s.timeMin, 0, 0, now.Location())
				if now.Before(todayTarget) {
					continue
				}

				lastRun := b.loadLastRun(ctx)
				if !lastRun.IsZero() && lastRun.After(todayTarget) {
					continue
				}

				if err := b.queries.SetSetting(ctx, "background_translation_last_run", now.Format(time.RFC3339)); err != nil {
					log.Printf("BackgroundTranslator: failed to persist last run time: %v", err)
				}

				// Dutch is always required for AH ordering regardless of ui_language.
				// Also translate to ui_language if it differs from "nl".
				uiLang, _ := b.queries.GetSetting(ctx, "ui_language")
				langs := translationTargets(uiLang)
				log.Printf("BackgroundTranslator: starting (langs=%v)", langs)
				for _, lang := range langs {
					b.runTranslation(ctx, lang)
				}

			case <-b.stop:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (b *BackgroundTranslator) Stop() {
	close(b.stop)
}

// translationTargets returns the set of languages to pre-translate.
// Dutch ("nl") is always included because AH ordering requires it.
// ui_language is added when it is a non-English language other than "nl".
func translationTargets(uiLang string) []string {
	langs := []string{"nl"}
	if uiLang != "" && uiLang != "en" && uiLang != "nl" {
		langs = append(langs, uiLang)
	}
	return langs
}

type bgTranslationSettings struct {
	enabled  bool
	days     map[time.Weekday]bool
	timeHour int // -1 if not configured
	timeMin  int
}

func (b *BackgroundTranslator) loadSettings(ctx context.Context) bgTranslationSettings {
	s := bgTranslationSettings{timeHour: -1, days: map[time.Weekday]bool{}}

	if val, _ := b.queries.GetSetting(ctx, "background_translation_enabled"); val != "true" {
		return s
	}
	s.enabled = true

	if val, _ := b.queries.GetSetting(ctx, "background_translation_days"); val != "" {
		for _, part := range strings.Split(val, ",") {
			part = strings.TrimSpace(part)
			if n, err := strconv.Atoi(part); err == nil && n >= 0 && n <= 6 {
				s.days[time.Weekday(n)] = true
			}
		}
	}

	if val, _ := b.queries.GetSetting(ctx, "background_translation_time"); val != "" {
		var h, m int
		if _, err := fmt.Sscanf(val, "%d:%d", &h, &m); err == nil && h >= 0 && h <= 23 && m >= 0 && m <= 59 {
			s.timeHour = h
			s.timeMin = m
		}
	}

	return s
}

func (b *BackgroundTranslator) loadLastRun(ctx context.Context) time.Time {
	val, _ := b.queries.GetSetting(ctx, "background_translation_last_run")
	if val == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, val)
	if err != nil {
		return time.Time{}
	}
	return t
}

func (b *BackgroundTranslator) runTranslation(ctx context.Context, targetLang string) int {
	names, err := b.queries.GetUntranslatedIngredientNames(ctx, targetLang, translationBatchSize)
	if err != nil {
		log.Printf("BackgroundTranslator: failed to query untranslated ingredients: %v", err)
		return 0
	}
	if len(names) == 0 {
		log.Printf("BackgroundTranslator: all ingredients already translated to %s", targetLang)
		return 0
	}
	log.Printf("BackgroundTranslator: translating %d ingredient(s) to %s", len(names), targetLang)
	b.translator.TranslateMany(ctx, names, targetLang)
	log.Printf("BackgroundTranslator: finished translating to %s", targetLang)
	return len(names)
}

// RunNow is an HTTP handler that triggers a translation run immediately,
// bypassing the schedule. Returns the number of ingredients translated.
func (b *BackgroundTranslator) RunNow(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	uiLang, _ := b.queries.GetSetting(ctx, "ui_language")
	langs := translationTargets(uiLang)

	total := 0
	for _, lang := range langs {
		total += b.runTranslation(ctx, lang)
	}
	writeJSON(w, http.StatusOK, map[string]any{"translated": total})
}
