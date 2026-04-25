package database

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rubenwo/mise/internal/models"
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

func (q *Queries) UpdateRecipeContent(ctx context.Context, id int, ingredients []models.Ingredient, instructions []string, cuisineType string) error {
	ingredientsJSON, err := json.Marshal(ingredients)
	if err != nil {
		return fmt.Errorf("marshaling ingredients: %w", err)
	}
	instructionsJSON, err := json.Marshal(instructions)
	if err != nil {
		return fmt.Errorf("marshaling instructions: %w", err)
	}
	tag, err := q.pool.Exec(ctx,
		"UPDATE recipes SET ingredients = $2, instructions = $3, cuisine_type = $4, updated_at = NOW() WHERE id = $1",
		id, ingredientsJSON, instructionsJSON, cuisineType,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (q *Queries) CreateChatMessage(ctx context.Context, recipeID int, role, content string) error {
	_, err := q.pool.Exec(ctx,
		"INSERT INTO recipe_chats (recipe_id, role, content) VALUES ($1, $2, $3)",
		recipeID, role, content,
	)
	return err
}

func (q *Queries) ListChatMessages(ctx context.Context, recipeID int) ([]models.ChatMessage, error) {
	rows, err := q.pool.Query(ctx,
		"SELECT id, recipe_id, role, content, created_at FROM recipe_chats WHERE recipe_id = $1 ORDER BY created_at ASC",
		recipeID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []models.ChatMessage
	for rows.Next() {
		var m models.ChatMessage
		if err := rows.Scan(&m.ID, &m.RecipeID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
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

// CuisineMeta holds lightweight metadata for one cuisine group.
type CuisineMeta struct {
	CuisineType   string   `json:"cuisine_type"`
	Count         int      `json:"count"`
	PreviewImages []string `json:"preview_images"`
}

// ListCuisines returns per-cuisine counts and up to 4 preview image URLs.
// Single CTE with ROW_NUMBER() + array_agg replaces the previous N+1 fan-out.
func (q *Queries) ListCuisines(ctx context.Context) ([]CuisineMeta, error) {
	rows, err := q.pool.Query(ctx, `
		WITH counts AS (
			SELECT
				CASE WHEN cuisine_type = '' OR cuisine_type IS NULL THEN 'Other' ELSE cuisine_type END AS ct,
				COUNT(*) AS cnt
			FROM recipes
			GROUP BY ct
		),
		ranked AS (
			SELECT
				CASE WHEN cuisine_type = '' OR cuisine_type IS NULL THEN 'Other' ELSE cuisine_type END AS ct,
				image_url,
				ROW_NUMBER() OVER (
					PARTITION BY CASE WHEN cuisine_type = '' OR cuisine_type IS NULL THEN 'Other' ELSE cuisine_type END
					ORDER BY created_at DESC
				) AS rn
			FROM recipes
			WHERE image_url IS NOT NULL AND image_url <> ''
		)
		SELECT
			c.ct,
			c.cnt,
			COALESCE(
				ARRAY_AGG(r.image_url ORDER BY r.rn) FILTER (WHERE r.rn IS NOT NULL),
				ARRAY[]::TEXT[]
			) AS preview_images
		FROM counts c
		LEFT JOIN ranked r ON r.ct = c.ct AND r.rn <= 4
		GROUP BY c.ct, c.cnt
		ORDER BY c.ct`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CuisineMeta
	for rows.Next() {
		var m CuisineMeta
		if err := rows.Scan(&m.CuisineType, &m.Count, &m.PreviewImages); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (q *Queries) ListRecipes(ctx context.Context, limit, offset int, cuisineType string) ([]models.Recipe, int, error) {
	if limit <= 0 {
		limit = 20
	}

	var total int
	var err error
	if cuisineType != "" {
		err = q.pool.QueryRow(ctx, "SELECT COUNT(*) FROM recipes WHERE cuisine_type = $1", cuisineType).Scan(&total)
	} else {
		err = q.pool.QueryRow(ctx, "SELECT COUNT(*) FROM recipes").Scan(&total)
	}
	if err != nil {
		return nil, 0, err
	}

	var rows pgx.Rows
	if cuisineType != "" {
		rows, err = q.pool.Query(ctx, `
			SELECT id, title, description, cuisine_type, prep_time_minutes, cook_time_minutes,
				servings, difficulty, ingredients, instructions, dietary_restrictions, tags,
				generated_by_model, generation_prompt, COALESCE(image_url, ''), created_at, updated_at
			FROM recipes WHERE cuisine_type = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
			cuisineType, limit, offset)
	} else {
		rows, err = q.pool.Query(ctx, `
			SELECT id, title, description, cuisine_type, prep_time_minutes, cook_time_minutes,
				servings, difficulty, ingredients, instructions, dietary_restrictions, tags,
				generated_by_model, generation_prompt, COALESCE(image_url, ''), created_at, updated_at
			FROM recipes ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	}
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

// RecipeFingerprint holds the minimal data needed for semantic duplicate detection
// and for building informative "avoid" hints in generation prompts.
type RecipeFingerprint struct {
	ID          int
	Title       string
	CuisineType string
	Ingredients []string // raw ingredient names
}

// ListRecipeFingerprints returns the 100 most-recent recipes with their title,
// cuisine, and ingredient names. Used for post-generation near-duplicate checking
// and for building richer prompt avoid-lists.
func (q *Queries) ListRecipeFingerprints(ctx context.Context) ([]RecipeFingerprint, error) {
	rows, err := q.pool.Query(ctx,
		"SELECT id, title, cuisine_type, ingredients FROM recipes ORDER BY created_at DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RecipeFingerprint
	for rows.Next() {
		var fp RecipeFingerprint
		var ingredientsJSON []byte
		if err := rows.Scan(&fp.ID, &fp.Title, &fp.CuisineType, &ingredientsJSON); err != nil {
			return nil, err
		}
		var ings []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(ingredientsJSON, &ings); err == nil {
			for _, ing := range ings {
				if ing.Name != "" {
					fp.Ingredients = append(fp.Ingredients, ing.Name)
				}
			}
		}
		out = append(out, fp)
	}
	return out, rows.Err()
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
	if req.MaxTotalMinutes > 0 {
		where += fmt.Sprintf(" AND (prep_time_minutes + cook_time_minutes) <= $%d", argIdx)
		args = append(args, req.MaxTotalMinutes)
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

type RecipeMeta struct {
	ID          int
	CuisineType string
	CreatedAt   time.Time
}

func (q *Queries) ListRecipeMeta(ctx context.Context) ([]RecipeMeta, error) {
	rows, err := q.pool.Query(ctx, "SELECT id, cuisine_type, created_at FROM recipes ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RecipeMeta
	for rows.Next() {
		var m RecipeMeta
		if err := rows.Scan(&m.ID, &m.CuisineType, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (q *Queries) GetRecipesByIDs(ctx context.Context, ids []int) ([]models.Recipe, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	rows, err := q.pool.Query(ctx, fmt.Sprintf(`
		SELECT id, title, description, cuisine_type, prep_time_minutes, cook_time_minutes,
			servings, difficulty, ingredients, instructions, dietary_restrictions, tags,
			generated_by_model, generation_prompt, COALESCE(image_url, ''), created_at, updated_at
		FROM recipes WHERE id IN (%s)`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	recipes, _, err := scanRecipes(rows, len(ids))
	return recipes, err
}

// LibrarySearchRequest holds parameters for the comprehensive library search.
type LibrarySearchRequest struct {
	Keywords            string   // space-separated words; each word must match somewhere
	CuisineType         string
	DietaryRestrictions []string
	Tags                []string
	MaxTotalMinutes     int
	Limit               int
}

// LibrarySearch searches recipes using wildcard matching across title, description,
// cuisine_type, and ingredient names. Each keyword must match in at least one of those
// fields. Structured filters (cuisine, dietary restrictions, tags, time) are applied
// with AND logic.
func (q *Queries) LibrarySearch(ctx context.Context, req LibrarySearchRequest) ([]models.Recipe, error) {
	if req.Limit <= 0 {
		req.Limit = 50
	}

	conditions := []string{"1=1"}
	args := []any{}
	argIdx := 1

	// ILIKE (case-insensitive substring) so the title/description/cuisine
	// trigram indexes (migration 015) are used. The JSONB ingredient lookup
	// stays unindexed; ingredients are filtered on the candidate set produced
	// by the indexed columns, which keeps the work bounded.
	for _, word := range strings.Fields(req.Keywords) {
		pattern := "%" + word + "%"
		conditions = append(conditions, fmt.Sprintf(
			"(title ILIKE $%d OR description ILIKE $%d OR cuisine_type ILIKE $%d"+
				" OR EXISTS (SELECT 1 FROM jsonb_array_elements(COALESCE(ingredients, '[]'::jsonb)) AS ing WHERE ing->>'name' ILIKE $%d))",
			argIdx, argIdx, argIdx, argIdx,
		))
		args = append(args, pattern)
		argIdx++
	}

	if req.CuisineType != "" {
		conditions = append(conditions, fmt.Sprintf("cuisine_type ILIKE $%d", argIdx))
		args = append(args, req.CuisineType)
		argIdx++
	}
	if len(req.DietaryRestrictions) > 0 {
		conditions = append(conditions, fmt.Sprintf("dietary_restrictions @> $%d", argIdx))
		args = append(args, req.DietaryRestrictions)
		argIdx++
	}
	if len(req.Tags) > 0 {
		conditions = append(conditions, fmt.Sprintf("tags @> $%d", argIdx))
		args = append(args, req.Tags)
		argIdx++
	}
	if req.MaxTotalMinutes > 0 {
		conditions = append(conditions, fmt.Sprintf("(prep_time_minutes + cook_time_minutes) <= $%d", argIdx))
		args = append(args, req.MaxTotalMinutes)
		argIdx++
	}

	where := "WHERE " + strings.Join(conditions, " AND ")
	sqlQuery := fmt.Sprintf(`
		SELECT id, title, description, cuisine_type, prep_time_minutes, cook_time_minutes,
			servings, difficulty, ingredients, instructions, dietary_restrictions, tags,
			generated_by_model, generation_prompt, COALESCE(image_url, ''), created_at, updated_at
		FROM recipes %s ORDER BY created_at DESC LIMIT $%d`, where, argIdx)
	args = append(args, req.Limit)

	rows, err := q.pool.Query(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	recipes, _, err := scanRecipes(rows, 0)
	return recipes, err
}

// RecipeDedup holds the minimal data needed for duplicate detection.
type RecipeDedup struct {
	ID          int
	Title       string
	Description string
}

func (q *Queries) ListRecipesForDedup(ctx context.Context) ([]RecipeDedup, error) {
	rows, err := q.pool.Query(ctx, "SELECT id, title, description FROM recipes ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RecipeDedup
	for rows.Next() {
		var r RecipeDedup
		if err := rows.Scan(&r.ID, &r.Title, &r.Description); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (q *Queries) CreateGenerationChat(ctx context.Context, prompt, model string, messagesJSON []byte) error {
	_, err := q.pool.Exec(ctx, `
		INSERT INTO generation_chats (prompt, model, messages) VALUES ($1, $2, $3)`,
		prompt, model, messagesJSON,
	)
	return err
}

func (q *Queries) ListInventory(ctx context.Context) ([]models.InventoryItem, error) {
	rows, err := q.pool.Query(ctx, `
        SELECT id, name, amount, unit, notes, updated_at
        FROM inventory ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []models.InventoryItem
	for rows.Next() {
		var item models.InventoryItem
		if err := rows.Scan(&item.ID, &item.Name, &item.Amount, &item.Unit, &item.Notes, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (q *Queries) CreateInventoryItem(ctx context.Context, item *models.InventoryItem) error {
	return q.pool.QueryRow(ctx, `
        INSERT INTO inventory (name, amount, unit, notes)
        VALUES ($1, $2, $3, $4)
        RETURNING id, updated_at`,
		item.Name, item.Amount, item.Unit, item.Notes,
	).Scan(&item.ID, &item.UpdatedAt)
}

func (q *Queries) UpdateInventoryItem(ctx context.Context, item *models.InventoryItem) error {
	tag, err := q.pool.Exec(ctx, `
        UPDATE inventory SET name=$2, amount=$3, unit=$4, notes=$5, updated_at=NOW()
        WHERE id=$1`,
		item.ID, item.Name, item.Amount, item.Unit, item.Notes,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (q *Queries) DeleteInventoryItem(ctx context.Context, id int) error {
	tag, err := q.pool.Exec(ctx, "DELETE FROM inventory WHERE id=$1", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (q *Queries) ListPendingIngredientScans(ctx context.Context) ([]models.PendingIngredientScan, error) {
	rows, err := q.pool.Query(ctx, `
		SELECT id, name, amount, unit, confident, created_at
		FROM pending_ingredient_scans ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []models.PendingIngredientScan
	for rows.Next() {
		var s models.PendingIngredientScan
		if err := rows.Scan(&s.ID, &s.Name, &s.Amount, &s.Unit, &s.Confident, &s.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, s)
	}
	return items, rows.Err()
}

func (q *Queries) CreatePendingIngredientScan(ctx context.Context, s *models.PendingIngredientScan) error {
	return q.pool.QueryRow(ctx, `
		INSERT INTO pending_ingredient_scans (name, amount, unit, confident)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at`,
		s.Name, s.Amount, s.Unit, s.Confident,
	).Scan(&s.ID, &s.CreatedAt)
}

func (q *Queries) DeletePendingIngredientScan(ctx context.Context, id int) error {
	tag, err := q.pool.Exec(ctx, "DELETE FROM pending_ingredient_scans WHERE id=$1", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
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
