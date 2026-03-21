package database

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rubenwo/recipes/internal/models"
)

func (q *Queries) ListOllamaProviders(ctx context.Context) ([]models.OllamaProvider, error) {
	rows, err := q.pool.Query(ctx, `
		SELECT id, name, host, model, enabled, created_at, health_status, last_health_check, last_error, tags
		FROM ollama_providers ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var providers []models.OllamaProvider
	for rows.Next() {
		var p models.OllamaProvider
		if err := rows.Scan(&p.ID, &p.Name, &p.Host, &p.Model, &p.Enabled, &p.CreatedAt, &p.HealthStatus, &p.LastHealthCheck, &p.LastError, &p.Tags); err != nil {
			return nil, err
		}

		providers = append(providers, p)
	}
	return providers, rows.Err()
}

func (q *Queries) ListEnabledOllamaProviders(ctx context.Context) ([]models.OllamaProvider, error) {
	rows, err := q.pool.Query(ctx, `
		SELECT id, name, host, model, enabled, created_at, health_status, last_health_check, last_error, tags
		FROM ollama_providers WHERE enabled = TRUE ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var providers []models.OllamaProvider
	for rows.Next() {
		var p models.OllamaProvider
		if err := rows.Scan(&p.ID, &p.Name, &p.Host, &p.Model, &p.Enabled, &p.CreatedAt, &p.HealthStatus, &p.LastHealthCheck, &p.LastError, &p.Tags); err != nil {
			return nil, err
		}
		providers = append(providers, p)
	}
	return providers, rows.Err()
}

func (q *Queries) CreateOllamaProvider(ctx context.Context, p *models.OllamaProvider) error {
	if p.Tags == nil {
		p.Tags = []string{}
	}
	return q.pool.QueryRow(ctx, `
		INSERT INTO ollama_providers (name, host, model, enabled, health_status, tags)
		VALUES ($1, $2, $3, $4, 'unknown', $5)
		RETURNING id, created_at`,
		p.Name, p.Host, p.Model, p.Enabled, pgtype.FlatArray[string](p.Tags),
	).Scan(&p.ID, &p.CreatedAt)
}

func (q *Queries) UpdateOllamaProvider(ctx context.Context, p *models.OllamaProvider) error {
	if p.Tags == nil {
		p.Tags = []string{}
	}
	tag, err := q.pool.Exec(ctx, `
		UPDATE ollama_providers
		SET name = $2, host = $3, model = $4, enabled = $5, health_status = $6, last_error = $7, tags = $8
		WHERE id = $1`,
		p.ID, p.Name, p.Host, p.Model, p.Enabled, p.HealthStatus, p.LastError, pgtype.FlatArray[string](p.Tags))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (q *Queries) DeleteOllamaProvider(ctx context.Context, id int) error {
	tag, err := q.pool.Exec(ctx, "DELETE FROM ollama_providers WHERE id = $1", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (q *Queries) CountOllamaProviders(ctx context.Context) (int, error) {
	var count int
	err := q.pool.QueryRow(ctx, "SELECT COUNT(*) FROM ollama_providers").Scan(&count)
	return count, err
}

func (q *Queries) UpdateProviderHealthStatus(ctx context.Context, id int, status string, lastError *string) error {
	_, err := q.pool.Exec(ctx, `
		UPDATE ollama_providers
		SET health_status = $2, last_error = $3, last_health_check = NOW()
		WHERE id = $1`,
		id, status, lastError)
	return err
}

func (q *Queries) ListSettings(ctx context.Context) ([]models.AppSetting, error) {
	rows, err := q.pool.Query(ctx, "SELECT key, value FROM app_settings ORDER BY key")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var settings []models.AppSetting
	for rows.Next() {
		var s models.AppSetting
		if err := rows.Scan(&s.Key, &s.Value); err != nil {
			return nil, err
		}
		settings = append(settings, s)
	}
	return settings, rows.Err()
}

func (q *Queries) GetSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := q.pool.QueryRow(ctx, "SELECT value FROM app_settings WHERE key = $1", key).Scan(&value)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (q *Queries) SetSetting(ctx context.Context, key, value string) error {
	_, err := q.pool.Exec(ctx, `
		INSERT INTO app_settings (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = $2`,
		key, value)
	return err
}
