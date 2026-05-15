package models

import "time"

type MealPlan struct {
	ID        int              `json:"id"`
	Name      string           `json:"name"`
	Status    string           `json:"status"`
	Recipes   []MealPlanRecipe `json:"recipes,omitempty"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}

type MealPlanRecipe struct {
	ID          int        `json:"id"`
	RecipeID    int        `json:"recipe_id"`
	Servings    int        `json:"servings"`
	SortOrder   int        `json:"sort_order"`
	Completed   bool       `json:"completed"`
	Skipped     bool       `json:"skipped"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Rating      *int       `json:"rating,omitempty"`
	Recipe      Recipe     `json:"recipe"`
}

type AddPlanRecipeRequest struct {
	RecipeID int `json:"recipe_id"`
	Servings int `json:"servings"`
}

// UpdatePlanRecipeRequest carries optional updates for one plan-recipe row.
// Rating: omit = leave alone; 0 = clear; 1-10 = set.
// Completed=false clears completed_at and rating regardless of Rating.
// Skipped=true also clears completed/completed_at/rating (can't be both).
type UpdatePlanRecipeRequest struct {
	Servings  *int  `json:"servings,omitempty"`
	Completed *bool `json:"completed,omitempty"`
	Skipped   *bool `json:"skipped,omitempty"`
	Rating    *int  `json:"rating,omitempty"`
}

// RecipeHistoryEntry represents one occurrence of a recipe on a meal plan,
// returned by GET /api/recipes/{id}/history.
type RecipeHistoryEntry struct {
	PlanID      int        `json:"plan_id"`
	PlanName    string     `json:"plan_name"`
	PlanStatus  string     `json:"plan_status"`
	Completed   bool       `json:"completed"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Rating      *int       `json:"rating,omitempty"`
}

// RecipeEatStats aggregates completion data across all plans for one recipe,
// returned (per recipe ID) by GET /api/recipes/eat-counts.
type RecipeEatStats struct {
	RecipeID     int        `json:"recipe_id"`
	Count        int        `json:"count"`
	LastCookedAt *time.Time `json:"last_cooked_at,omitempty"`
	AvgRating    *float64   `json:"avg_rating,omitempty"`
	RatedCount   int        `json:"rated_count"`
}

type UpdateMealPlanRequest struct {
	Status *string `json:"status,omitempty"`
}

type RandomizeRequest struct {
	Servings    []int `json:"servings"`
	ExcludedIDs []int `json:"excluded_ids,omitempty"`
}

type AggregatedIngredient struct {
	Name    string   `json:"name"`
	Amount  float64  `json:"amount"`
	Unit    string   `json:"unit"`
	Recipes []string `json:"recipes"`
}
