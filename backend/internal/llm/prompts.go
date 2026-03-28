package llm

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rubenwo/mise/internal/models"
)

// KnownCuisines is the canonical list of cuisines the app supports.
// It is used to seed cuisine counts so that cuisines with zero recipes
// are treated as the most underrepresented when balancing the library.
var KnownCuisines = []string{
	"American", "Argentine", "Brazilian", "British", "Caribbean",
	"Chinese", "Dutch", "Eastern European", "Ethiopian", "Filipino",
	"French", "German", "Indian", "Italian", "Japanese", "Korean",
	"Mediterranean", "Mexican", "Middle Eastern", "Moroccan", "Peruvian",
	"Scandinavian", "Spanish", "Thai", "Turkish", "Vietnamese",
}

// SeedCuisineCounts returns a copy of counts that includes every KnownCuisine
// at zero if it was not already present, so balancing logic sees the full list.
func SeedCuisineCounts(counts map[string]int) map[string]int {
	seeded := make(map[string]int, len(KnownCuisines))
	for _, c := range KnownCuisines {
		seeded[c] = 0
	}
	for c, n := range counts {
		seeded[c] = n
	}
	return seeded
}

const systemPrompt = `You are a creative chef and recipe developer. Generate detailed, practical dinner recipes.

IMPORTANT: You MUST respond with valid JSON matching this exact structure:
{
  "title": "Recipe Title",
  "description": "A brief description of the dish",
  "cuisine_type": "Italian",
  "prep_time_minutes": 15,
  "cook_time_minutes": 30,
  "servings": 4,
  "difficulty": "easy",
  "ingredients": [
    {"name": "onion", "amount": 1, "unit": ""},
    {"name": "garlic", "amount": 3, "unit": "clove"},
    {"name": "bell pepper", "amount": 2, "unit": ""},
    {"name": "chicken breast", "amount": 400, "unit": "g"},
    {"name": "olive oil", "amount": 30, "unit": "ml"},
    {"name": "salt", "amount": 5, "unit": "g"},
    {"name": "cumin", "amount": 2, "unit": "tsp"},
    {"name": "ingredient name", "amount": 200, "unit": "g", "notes": "optional notes"}
  ],
  "instructions": [
    "Step 1: Do something",
    "Step 2: Do something else"
  ],
  "dietary_restrictions": ["vegetarian"],
  "tags": ["quick", "high-protein"]
}

Rules:
- difficulty must be one of: easy, medium, hard
- All amounts must be numbers (not strings)
- Whole items use count units: garlic uses "clove" (e.g. 3 clove), eggs/onions/bell peppers/tomatoes/lemons/limes/carrots/potatoes/avocados use "" empty unit (e.g. 2 onions → amount 2, unit ""). Do NOT use grams for these.
- All other ingredients use metric units: grams (g), kilograms (kg), milliliters (ml), liters (l). Teaspoons (tsp) and tablespoons (tbsp) are acceptable for spices and small liquid amounts. Do NOT use cups, ounces, pounds, or other imperial units.
- Include at least 3 ingredients and 3 instructions
- Be specific with measurements and cooking times
- For tags, use relevant labels from this list when appropriate: high-protein, low-carb, omega-3, low-calorie, high-fiber, meal-prep, quick, budget-friendly, one-pot, freezer-friendly. You may also add other descriptive tags.
- Respond ONLY with the JSON object, no other text`

func BuildGeneratePrompt(req models.GenerateRequest, existingTitles []string, cuisineCounts map[string]int) string {
	var parts []string
	parts = append(parts, "Generate a dinner recipe")

	cuisineType := req.CuisineType
	if cuisineType == "" {
		cuisineType = PickLeastRepresentedCuisine(SeedCuisineCounts(cuisineCounts))
	}
	if cuisineType != "" {
		parts = append(parts, fmt.Sprintf("from %s cuisine", cuisineType))
	}
	if len(req.DietaryRestrictions) > 0 {
		parts = append(parts, fmt.Sprintf("that is %s", strings.Join(req.DietaryRestrictions, " and ")))
	}
	if req.MaxPrepTime > 0 {
		parts = append(parts, fmt.Sprintf("with prep time under %d minutes", req.MaxPrepTime))
	}
	if req.Difficulty != "" {
		parts = append(parts, fmt.Sprintf("at %s difficulty level", req.Difficulty))
	}
	if req.Servings > 0 {
		parts = append(parts, fmt.Sprintf("for exactly %d servings", req.Servings))
	}
	if req.AdditionalNotes != "" {
		parts = append(parts, fmt.Sprintf("with these preferences: %s", req.AdditionalNotes))
	}

	prompt := strings.Join(parts, " ") + "."

	if req.Servings > 0 {
		prompt += fmt.Sprintf(" The `servings` field in your JSON MUST be %d and all ingredient amounts MUST be scaled accordingly.", req.Servings)
	}

	if len(existingTitles) > 0 {
		prompt += "\n\nDo NOT generate a recipe with a title that already exists in the collection: " + strings.Join(existingTitles, ", ") + "."
	}

	return prompt
}

// PickLeastRepresentedCuisine returns the cuisine with the fewest recipes.
// Alphabetical order is used as a tiebreaker for stable, deterministic picks.
// Returns an empty string if counts is empty.
func PickLeastRepresentedCuisine(counts map[string]int) string {
	type entry struct {
		cuisine string
		count   int
	}
	entries := make([]entry, 0, len(counts))
	for c, cnt := range counts {
		entries = append(entries, entry{c, cnt})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count != entries[j].count {
			return entries[i].count < entries[j].count
		}
		return entries[i].cuisine < entries[j].cuisine
	})
	if len(entries) == 0 {
		return ""
	}
	return entries[0].cuisine
}

func BuildRefinePrompt(recipe models.Recipe, feedback string) string {
	var ingredientLines []string
	for _, ing := range recipe.Ingredients {
		line := fmt.Sprintf("- %s: %.4g %s", ing.Name, ing.Amount, ing.Unit)
		if ing.Notes != "" {
			line += fmt.Sprintf(" (%s)", ing.Notes)
		}
		ingredientLines = append(ingredientLines, line)
	}

	var instructionLines []string
	for i, step := range recipe.Instructions {
		instructionLines = append(instructionLines, fmt.Sprintf("%d. %s", i+1, step))
	}

	var dietaryLines []string
	if len(recipe.DietaryRestrictions) > 0 {
		dietaryLines = recipe.DietaryRestrictions
	}

	return fmt.Sprintf(`Here is a recipe that needs refinement:

Title: %s
Description: %s
Cuisine: %s
Prep time: %d minutes
Cook time: %d minutes
Servings: %d
Difficulty: %s
Dietary restrictions: %s
Tags: %s

Ingredients:
%s

Instructions:
%s

The user wants the following changes: %s

Generate an improved version of this recipe incorporating the feedback. Respond with the complete updated recipe JSON.`,
		recipe.Title,
		recipe.Description,
		recipe.CuisineType,
		recipe.PrepTimeMinutes,
		recipe.CookTimeMinutes,
		recipe.Servings,
		recipe.Difficulty,
		strings.Join(dietaryLines, ", "),
		strings.Join(recipe.Tags, ", "),
		strings.Join(ingredientLines, "\n"),
		strings.Join(instructionLines, "\n"),
		feedback,
	)
}

func BuildLeftoverPrompt(ingredients []string, existingTitles []string) string {
	prompt := fmt.Sprintf(`The user has leftover ingredients from meal planning and wants a recipe that uses them up.

Leftover ingredients: %s

Generate a recipe that uses as many of these ingredients as possible. Prioritize using the fresh/perishable items.
Use web_search to find inspiration for recipes that combine these ingredients well.
Use db_search to check if a similar recipe already exists.`, strings.Join(ingredients, ", "))

	if len(existingTitles) > 0 {
		prompt += "\n\nDo NOT duplicate any of these existing recipes: " + strings.Join(existingTitles, ", ") + "."
	}

	return prompt
}

func BuildImportPrompt(rawText string, existingTitles []string) string {
	prompt := fmt.Sprintf(`The user wants to import an existing recipe from free-form text. Parse the following text and produce a complete, structured recipe.

The text may only contain a recipe name, or it may include partial ingredients and instructions. Use your tools:
- Use web_search to find the full recipe details (ingredients, instructions, cooking times) if they are missing or incomplete
- Use db_search to check if a similar recipe already exists in the database

Here is the user's input:
---
%s
---

Generate the complete recipe JSON based on this input, filling in any missing details from your search results.`, rawText)

	if len(existingTitles) > 0 {
		prompt += "\n\nThese recipes already exist in the database: " + strings.Join(existingTitles, ", ") + ". Flag if this is a duplicate but still generate the recipe."
	}

	return prompt
}

func SystemPrompt() string {
	return systemPrompt
}

func BuildScanIngredientPrompt() string {
	return `Look at this image and identify the food product(s) shown.

Focus on what each PRODUCT is (e.g. "soy sauce", "whole milk", "olive oil", "canned tomatoes") — not the sub-ingredients listed on any nutrition or ingredient label. If a label is visible, use it only to confirm the product name and net weight/volume, not to enumerate what the product is made of.

Respond with a JSON array — one object per distinct product:
[
  {"name": "product name in English, lowercase", "amount": 500, "unit": "g", "confident": true}
]

Rules:
- One entry per distinct product/package visible
- name: most specific product name you can determine (e.g. "whole milk", "extra virgin olive oil", "dark soy sauce")
- amount: net weight or volume from the packaging if visible, otherwise 0
- unit: the unit for the amount (g, kg, ml, l, etc.), empty string if unknown
- confident: true if you are reasonably sure about BOTH the name AND the amount (if amount > 0); false otherwise
- Respond ONLY with the JSON array, no other text`
}

// BuildDeduplicateIngredientsPrompt builds a prompt asking the LLM to merge groups of
// ingredients that share the same name but couldn't be merged deterministically.
func BuildDeduplicateIngredientsPrompt(groupsJSON string) string {
	return fmt.Sprintf(`You are consolidating a grocery shopping list. The following groups each contain ingredients that appear to be the same item but are listed with different names or units and could not be merged automatically.

For each group, return a single merged ingredient with the correct total amount in the most practical unit for shopping.

Input groups (JSON array of arrays):
%s

Return a JSON array with exactly one object per input group. Always use a JSON array even if there is only one group:
[{"name": "...", "amount": 0, "unit": "..."}]

Rules:
- Use the most common/generic ingredient name in English, lowercase
- Round up countable items (eggs, onions, garlic cloves, etc.) to the nearest whole number
- Use weight (g or kg) for dry goods, volume (ml or l) for liquids, count (empty unit) for whole items
- Sum amounts across all entries in each group, converting units as needed
- Respond ONLY with the JSON array (even for a single item), no other text`, groupsJSON)
}

// BuildBackgroundGeneratePrompt creates a prompt for unattended background recipe generation.
// targetCuisine is pre-selected by the caller based on the current cuisine distribution;
// pass an empty string to let the model choose freely.
func BuildBackgroundGeneratePrompt(targetCuisine string, existingTitles []string, index, total, servings int) string {
	cuisineDesc := ""
	if targetCuisine != "" {
		cuisineDesc = targetCuisine + " "
	}
	prompt := fmt.Sprintf(
		"Generate a %sdinner recipe for exactly %d servings. "+
			"The `servings` field in your JSON MUST be %d and all ingredient amounts MUST be scaled accordingly.",
		cuisineDesc, servings, servings,
	)

	if total > 1 {
		prompt += fmt.Sprintf("\n\n(Recipe %d of %d in this background batch — make it unique from others in this batch.)", index, total)
	}

	if len(existingTitles) > 0 {
		prompt += "\n\nDo NOT generate a recipe with a title that already exists in the collection: " + strings.Join(existingTitles, ", ") + "."
	}

	return prompt
}

