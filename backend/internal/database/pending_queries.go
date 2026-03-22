package database

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rubenwo/recipes/internal/models"
)

func (q *Queries) CreatePendingRecipe(ctx context.Context, r *models.Recipe) error {
	ingredientsJSON, err := json.Marshal(r.Ingredients)
	if err != nil {
		return fmt.Errorf("marshaling ingredients: %w", err)
	}
	instructionsJSON, err := json.Marshal(r.Instructions)
	if err != nil {
		return fmt.Errorf("marshaling instructions: %w", err)
	}

	return q.pool.QueryRow(ctx, `
		INSERT INTO pending_recipes (title, description, cuisine_type, prep_time_minutes, cook_time_minutes,
			servings, difficulty, ingredients, instructions, dietary_restrictions, tags,
			generated_by_model, image_url)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id, created_at`,
		r.Title, r.Description, r.CuisineType, r.PrepTimeMinutes, r.CookTimeMinutes,
		r.Servings, r.Difficulty, ingredientsJSON, instructionsJSON,
		r.DietaryRestrictions, r.Tags, r.GeneratedByModel, nullableString(r.ImageURL),
	).Scan(&r.ID, &r.CreatedAt)
}

func (q *Queries) ListPendingRecipes(ctx context.Context) ([]models.Recipe, error) {
	rows, err := q.pool.Query(ctx, `
		SELECT id, title, description, cuisine_type, prep_time_minutes, cook_time_minutes,
			servings, difficulty, ingredients, instructions, dietary_restrictions, tags,
			generated_by_model, COALESCE(image_url, ''), created_at
		FROM pending_recipes ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var recipes []models.Recipe
	for rows.Next() {
		var r models.Recipe
		var ingredientsJSON, instructionsJSON []byte
		if err := rows.Scan(
			&r.ID, &r.Title, &r.Description, &r.CuisineType, &r.PrepTimeMinutes, &r.CookTimeMinutes,
			&r.Servings, &r.Difficulty, &ingredientsJSON, &instructionsJSON,
			&r.DietaryRestrictions, &r.Tags, &r.GeneratedByModel, &r.ImageURL, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(ingredientsJSON, &r.Ingredients); err != nil {
			return nil, fmt.Errorf("unmarshaling ingredients: %w", err)
		}
		if err := json.Unmarshal(instructionsJSON, &r.Instructions); err != nil {
			return nil, fmt.Errorf("unmarshaling instructions: %w", err)
		}
		recipes = append(recipes, r)
	}
	return recipes, rows.Err()
}

func (q *Queries) CountPendingRecipes(ctx context.Context) (int, error) {
	var count int
	err := q.pool.QueryRow(ctx, "SELECT COUNT(*) FROM pending_recipes").Scan(&count)
	return count, err
}

// ApprovePendingRecipe moves a pending recipe into the recipes table and removes it from pending.
func (q *Queries) ApprovePendingRecipe(ctx context.Context, pendingID int) (*models.Recipe, error) {
	tx, err := q.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var r models.Recipe
	var ingredientsJSON, instructionsJSON []byte
	err = tx.QueryRow(ctx, `
		SELECT id, title, description, cuisine_type, prep_time_minutes, cook_time_minutes,
			servings, difficulty, ingredients, instructions, dietary_restrictions, tags,
			generated_by_model, COALESCE(image_url, '')
		FROM pending_recipes WHERE id = $1`, pendingID,
	).Scan(
		&r.ID, &r.Title, &r.Description, &r.CuisineType, &r.PrepTimeMinutes, &r.CookTimeMinutes,
		&r.Servings, &r.Difficulty, &ingredientsJSON, &instructionsJSON,
		&r.DietaryRestrictions, &r.Tags, &r.GeneratedByModel, &r.ImageURL,
	)
	if err == pgx.ErrNoRows {
		return nil, pgx.ErrNoRows
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(ingredientsJSON, &r.Ingredients); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(instructionsJSON, &r.Instructions); err != nil {
		return nil, err
	}

	// Insert into recipes table.
	err = tx.QueryRow(ctx, `
		INSERT INTO recipes (title, description, cuisine_type, prep_time_minutes, cook_time_minutes,
			servings, difficulty, ingredients, instructions, dietary_restrictions, tags,
			generated_by_model, image_url)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id, created_at, updated_at`,
		r.Title, r.Description, r.CuisineType, r.PrepTimeMinutes, r.CookTimeMinutes,
		r.Servings, r.Difficulty, ingredientsJSON, instructionsJSON,
		r.DietaryRestrictions, r.Tags, r.GeneratedByModel, nullableString(r.ImageURL),
	).Scan(&r.ID, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx, "DELETE FROM pending_recipes WHERE id = $1", pendingID); err != nil {
		return nil, err
	}

	return &r, tx.Commit(ctx)
}

func (q *Queries) RejectPendingRecipe(ctx context.Context, id int) error {
	tag, err := q.pool.Exec(ctx, "DELETE FROM pending_recipes WHERE id = $1", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (q *Queries) GetPendingRecipeTitle(ctx context.Context, id int) (string, error) {
	var title string
	err := q.pool.QueryRow(ctx, "SELECT title FROM pending_recipes WHERE id = $1", id).Scan(&title)
	return title, err
}

func (q *Queries) SetPendingRecipeImage(ctx context.Context, id int, imageURL string) error {
	_, err := q.pool.Exec(ctx, "UPDATE pending_recipes SET image_url = $2 WHERE id = $1", id, imageURL)
	return err
}

func (q *Queries) UpdatePendingRecipeContent(ctx context.Context, id int, ingredients []models.Ingredient, instructions []string) error {
	ingredientsJSON, err := json.Marshal(ingredients)
	if err != nil {
		return fmt.Errorf("marshaling ingredients: %w", err)
	}
	instructionsJSON, err := json.Marshal(instructions)
	if err != nil {
		return fmt.Errorf("marshaling instructions: %w", err)
	}
	tag, err := q.pool.Exec(ctx,
		"UPDATE pending_recipes SET ingredients = $2, instructions = $3 WHERE id = $1",
		id, ingredientsJSON, instructionsJSON,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// DeleteExpiredPendingRecipes removes pending recipes older than the given age.
func (q *Queries) DeleteExpiredPendingRecipes(ctx context.Context, maxAge time.Duration) (int64, error) {
	tag, err := q.pool.Exec(ctx,
		"DELETE FROM pending_recipes WHERE created_at < $1",
		time.Now().Add(-maxAge))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// nullableString returns nil for empty strings so the DB stores NULL instead of "".
func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
