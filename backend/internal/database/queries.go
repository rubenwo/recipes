package database

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rubenwo/recipes/internal/models"
)

type Queries struct {
	pool *pgxpool.Pool
}

func NewQueries(pool *pgxpool.Pool) *Queries {
	return &Queries{pool: pool}
}

func (q *Queries) CreateRecipe(ctx context.Context, r *models.Recipe) error {
	ingredientsJSON, err := json.Marshal(r.Ingredients)
	if err != nil {
		return fmt.Errorf("marshaling ingredients: %w", err)
	}
	instructionsJSON, err := json.Marshal(r.Instructions)
	if err != nil {
		return fmt.Errorf("marshaling instructions: %w", err)
	}

	return q.pool.QueryRow(ctx, `
		INSERT INTO recipes (title, description, cuisine_type, prep_time_minutes, cook_time_minutes,
			servings, difficulty, ingredients, instructions, dietary_restrictions, tags,
			generated_by_model, generation_prompt)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id, created_at, updated_at`,
		r.Title, r.Description, r.CuisineType, r.PrepTimeMinutes, r.CookTimeMinutes,
		r.Servings, r.Difficulty, ingredientsJSON, instructionsJSON,
		r.DietaryRestrictions, r.Tags, r.GeneratedByModel, r.GenerationPrompt,
	).Scan(&r.ID, &r.CreatedAt, &r.UpdatedAt)
}

func (q *Queries) SetRecipeImage(ctx context.Context, id int, imageURL string) error {
	_, err := q.pool.Exec(ctx, "UPDATE recipes SET image_url = $2 WHERE id = $1", id, imageURL)
	return err
}

func (q *Queries) GetRecipe(ctx context.Context, id int) (*models.Recipe, error) {
	r := &models.Recipe{}
	var ingredientsJSON, instructionsJSON []byte

	err := q.pool.QueryRow(ctx, `
		SELECT id, title, description, cuisine_type, prep_time_minutes, cook_time_minutes,
			servings, difficulty, ingredients, instructions, dietary_restrictions, tags,
			generated_by_model, generation_prompt, COALESCE(image_url, ''), created_at, updated_at
		FROM recipes WHERE id = $1`, id,
	).Scan(
		&r.ID, &r.Title, &r.Description, &r.CuisineType, &r.PrepTimeMinutes, &r.CookTimeMinutes,
		&r.Servings, &r.Difficulty, &ingredientsJSON, &instructionsJSON,
		&r.DietaryRestrictions, &r.Tags, &r.GeneratedByModel, &r.GenerationPrompt,
		&r.ImageURL, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(ingredientsJSON, &r.Ingredients); err != nil {
		return nil, fmt.Errorf("unmarshaling ingredients: %w", err)
	}
	if err := json.Unmarshal(instructionsJSON, &r.Instructions); err != nil {
		return nil, fmt.Errorf("unmarshaling instructions: %w", err)
	}

	return r, nil
}

func (q *Queries) ListRecipes(ctx context.Context, limit, offset int) ([]models.Recipe, int, error) {
	if limit <= 0 {
		limit = 20
	}

	var total int
	if err := q.pool.QueryRow(ctx, "SELECT COUNT(*) FROM recipes").Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := q.pool.Query(ctx, `
		SELECT id, title, description, cuisine_type, prep_time_minutes, cook_time_minutes,
			servings, difficulty, ingredients, instructions, dietary_restrictions, tags,
			generated_by_model, generation_prompt, COALESCE(image_url, ''), created_at, updated_at
		FROM recipes ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	return scanRecipes(rows, total)
}

func (q *Queries) DeleteRecipe(ctx context.Context, id int) error {
	tag, err := q.pool.Exec(ctx, "DELETE FROM recipes WHERE id = $1", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (q *Queries) ListRecipeTitles(ctx context.Context) ([]string, error) {
	// Limit to 40 most recent titles to keep generation prompts concise.
	rows, err := q.pool.Query(ctx, "SELECT title FROM recipes ORDER BY created_at DESC LIMIT 40")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var titles []string
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			return nil, err
		}
		titles = append(titles, title)
	}
	return titles, rows.Err()
}

type RecipeSummary struct {
	ID          int
	CuisineType string
}

func (q *Queries) ListRecipeSummaries(ctx context.Context) ([]RecipeSummary, error) {
	rows, err := q.pool.Query(ctx, "SELECT id, cuisine_type FROM recipes ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []RecipeSummary
	for rows.Next() {
		var s RecipeSummary
		if err := rows.Scan(&s.ID, &s.CuisineType); err != nil {
			return nil, err
		}
		summaries = append(summaries, s)
	}
	return summaries, rows.Err()
}

func (q *Queries) ListCuisineCounts(ctx context.Context) (map[string]int, error) {
	rows, err := q.pool.Query(ctx, "SELECT cuisine_type, COUNT(*) FROM recipes WHERE cuisine_type != '' GROUP BY cuisine_type ORDER BY COUNT(*) DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var cuisine string
		var count int
		if err := rows.Scan(&cuisine, &count); err != nil {
			return nil, err
		}
		counts[cuisine] = count
	}
	return counts, rows.Err()
}

func (q *Queries) SearchRecipes(ctx context.Context, req models.SearchRequest) ([]models.Recipe, int, error) {
	if req.Limit <= 0 {
		req.Limit = 20
	}

	where := "WHERE 1=1"
	args := []any{}
	argIdx := 1

	if req.Query != "" {
		where += fmt.Sprintf(" AND to_tsvector('english', coalesce(title, '') || ' ' || coalesce(description, '')) @@ plainto_tsquery('english', $%d)", argIdx)
		args = append(args, req.Query)
		argIdx++
	}
	if req.CuisineType != "" {
		where += fmt.Sprintf(" AND cuisine_type = $%d", argIdx)
		args = append(args, req.CuisineType)
		argIdx++
	}
	if len(req.DietaryRestrictions) > 0 {
		where += fmt.Sprintf(" AND dietary_restrictions @> $%d", argIdx)
		args = append(args, req.DietaryRestrictions)
		argIdx++
	}
	if len(req.Tags) > 0 {
		where += fmt.Sprintf(" AND tags @> $%d", argIdx)
		args = append(args, req.Tags)
		argIdx++
	}

	var total int
	countArgs := make([]any, len(args))
	copy(countArgs, args)
	if err := q.pool.QueryRow(ctx, "SELECT COUNT(*) FROM recipes "+where, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := fmt.Sprintf(`
		SELECT id, title, description, cuisine_type, prep_time_minutes, cook_time_minutes,
			servings, difficulty, ingredients, instructions, dietary_restrictions, tags,
			generated_by_model, generation_prompt, COALESCE(image_url, ''), created_at, updated_at
		FROM recipes %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, where, argIdx, argIdx+1)
	args = append(args, req.Limit, req.Offset)

	rows, err := q.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	return scanRecipes(rows, total)
}

func (q *Queries) CreateGenerationChat(ctx context.Context, prompt, model string, messagesJSON []byte) error {
	_, err := q.pool.Exec(ctx, `
		INSERT INTO generation_chats (prompt, model, messages) VALUES ($1, $2, $3)`,
		prompt, model, messagesJSON,
	)
	return err
}

func scanRecipes(rows pgx.Rows, total int) ([]models.Recipe, int, error) {
	var recipes []models.Recipe
	for rows.Next() {
		var r models.Recipe
		var ingredientsJSON, instructionsJSON []byte
		if err := rows.Scan(
			&r.ID, &r.Title, &r.Description, &r.CuisineType, &r.PrepTimeMinutes, &r.CookTimeMinutes,
			&r.Servings, &r.Difficulty, &ingredientsJSON, &instructionsJSON,
			&r.DietaryRestrictions, &r.Tags, &r.GeneratedByModel, &r.GenerationPrompt,
			&r.ImageURL, &r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}
		if err := json.Unmarshal(ingredientsJSON, &r.Ingredients); err != nil {
			return nil, 0, fmt.Errorf("unmarshaling ingredients: %w", err)
		}
		if err := json.Unmarshal(instructionsJSON, &r.Instructions); err != nil {
			return nil, 0, fmt.Errorf("unmarshaling instructions: %w", err)
		}
		recipes = append(recipes, r)
	}
	return recipes, total, rows.Err()
}
