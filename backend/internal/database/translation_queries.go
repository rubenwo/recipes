package database

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// GetTranslation returns a cached translation, or ("", nil) if not cached.
func (q *Queries) GetTranslation(ctx context.Context, sourceText, targetLang string) (string, error) {
	var translated string
	err := q.pool.QueryRow(ctx,
		"SELECT translated_text FROM translation_cache WHERE source_text = $1 AND target_lang = $2",
		sourceText, targetLang,
	).Scan(&translated)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	return translated, err
}

// SetTranslation stores or updates a translation in the cache.
func (q *Queries) SetTranslation(ctx context.Context, sourceText, targetLang, translatedText string) error {
	_, err := q.pool.Exec(ctx, `
		INSERT INTO translation_cache (source_text, target_lang, translated_text)
		VALUES ($1, $2, $3)
		ON CONFLICT (source_text, target_lang) DO UPDATE SET translated_text = $3`,
		sourceText, targetLang, translatedText)
	return err
}
