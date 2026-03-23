package models

import (
	"time"
)

type Ingredient struct {
	Name     string  `json:"name"`
	Amount   float64 `json:"amount"`
	Unit     string  `json:"unit"`
	Notes    string  `json:"notes,omitempty"`
}

type Recipe struct {
	ID                  int          `json:"id"`
	Title               string       `json:"title"`
	Description         string       `json:"description"`
	CuisineType         string       `json:"cuisine_type"`
	PrepTimeMinutes     int          `json:"prep_time_minutes"`
	CookTimeMinutes     int          `json:"cook_time_minutes"`
	Servings            int          `json:"servings"`
	Difficulty          string       `json:"difficulty"`
	Ingredients         []Ingredient `json:"ingredients"`
	Instructions        []string     `json:"instructions"`
	DietaryRestrictions []string     `json:"dietary_restrictions"`
	Tags                []string     `json:"tags"`
	GeneratedByModel    string       `json:"generated_by_model"`
	GenerationPrompt    string       `json:"generation_prompt"`
	ImageURL            string       `json:"image_url,omitempty"`
	CreatedAt           time.Time    `json:"created_at"`
	UpdatedAt           time.Time    `json:"updated_at"`
}

type GenerateRequest struct {
	CuisineType         string   `json:"cuisine_type"`
	DietaryRestrictions []string `json:"dietary_restrictions"`
	MaxPrepTime         int      `json:"max_prep_time"`
	Difficulty          string   `json:"difficulty"`
	Servings            int      `json:"servings"`
	AdditionalNotes     string   `json:"additional_notes"`
}

type BatchGenerateRequest struct {
	GenerateRequest
	Count int `json:"count"`
}

type RefineRequest struct {
	Recipe  Recipe `json:"recipe"`
	Feedback string `json:"feedback"`
}

type ImportRequest struct {
	RawText string `json:"raw_text"`
}

type SearchRequest struct {
	Query               string   `json:"query"`
	CuisineType         string   `json:"cuisine_type"`
	DietaryRestrictions []string `json:"dietary_restrictions"`
	Tags                []string `json:"tags"`
	MaxTotalMinutes     int      `json:"max_total_minutes"`
	Limit               int      `json:"limit"`
	Offset              int      `json:"offset"`
}

type AISearchRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type ChatMessage struct {
	ID        int       `json:"id"`
	RecipeID  int       `json:"recipe_id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type InventoryItem struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Amount    float64   `json:"amount"`
	Unit      string    `json:"unit"`
	Notes     string    `json:"notes"`
	UpdatedAt time.Time `json:"updated_at"`
}

type IngredientScan struct {
	Name      string  `json:"name"`
	Amount    float64 `json:"amount"`
	Unit      string  `json:"unit"`
	Confident bool    `json:"confident"`
}
