// Package translation provides LLM-backed text translation with DB caching.
// Translations are cached so repeated calls for the same text are instant.
//
// Provider selection: if a provider is tagged "translation" it is preferred;
// otherwise any healthy pool client is used. To use a lighter/faster model for
// translation, add a provider with the "translation" tag in Settings.
package translation

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/rubenwo/mise/internal/database"
	"github.com/rubenwo/mise/internal/llm"
)

// Translator translates text using Ollama, with results cached in PostgreSQL.
type Translator struct {
	pool    *llm.ClientPool
	queries *database.Queries
}

// New creates a Translator backed by the given client pool and DB queries.
func New(pool *llm.ClientPool, queries *database.Queries) *Translator {
	return &Translator{pool: pool, queries: queries}
}

// Translate translates text from English to targetLang (e.g. "nl").
// Returns the original text unchanged if translation is unavailable or fails.
func (t *Translator) Translate(ctx context.Context, text, targetLang string) string {
	if text == "" || targetLang == "" || targetLang == "en" {
		return text
	}

	// Check DB cache first — avoids an LLM call for repeated ingredients.
	if cached, err := t.queries.GetTranslation(ctx, text, targetLang); err == nil && cached != "" {
		return cached
	}

	// Prefer a provider tagged "translation"; fall back to any healthy client.
	client := t.pool.AcquireWithTag("translation")
	if client == nil {
		client = t.pool.Acquire()
	}
	if client == nil {
		log.Printf("translation: no Ollama provider available, using original text %q", text)
		return text
	}

	prompt := fmt.Sprintf(
		"Translate the following food ingredient name from English to %s.\n"+
			"Reply with ONLY the translation. No explanation, no extra words, no punctuation at the end.\n\n"+
			"Ingredient: %s\nTranslation:",
		langName(targetLang), text,
	)

	messages := []llm.Message{
		{Role: "user", Content: prompt},
	}

	resp, err := client.Chat(ctx, messages, nil)
	if err != nil {
		log.Printf("translation: LLM call failed for %q: %v", text, err)
		return text
	}

	translated := strings.TrimSpace(resp.Message.Content)
	// Strip surrounding quotes some models add.
	translated = strings.Trim(translated, `"'`)
	translated = strings.TrimSpace(translated)
	if translated == "" {
		return text
	}

	// Cache for future calls.
	if err := t.queries.SetTranslation(ctx, text, targetLang, translated); err != nil {
		log.Printf("translation: failed to cache %q -> %q: %v", text, translated, err)
	}

	return translated
}

// TranslateMany translates a slice of texts, returning results in the same order.
func (t *Translator) TranslateMany(ctx context.Context, texts []string, targetLang string) []string {
	out := make([]string, len(texts))
	for i, text := range texts {
		out[i] = t.Translate(ctx, text, targetLang)
	}
	return out
}

// langName converts a BCP-47 language code to a human-readable name for prompts.
func langName(code string) string {
	switch code {
	case "nl":
		return "Dutch"
	case "fr":
		return "French"
	case "de":
		return "German"
	case "es":
		return "Spanish"
	case "it":
		return "Italian"
	default:
		return code
	}
}
