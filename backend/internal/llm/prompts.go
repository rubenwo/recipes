package llm

import (
	"fmt"
	"strings"

	"github.com/rubenwo/recipes/internal/models"
)

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
    {"name": "ingredient name", "amount": 200, "unit": "g", "notes": "optional notes"}
  ],
  "instructions": [
    "Step 1: Do something",
    "Step 2: Do something else"
  ],
  "dietary_restrictions": ["vegetarian"],
  "tags": ["quick", "healthy"]
}

Rules:
- difficulty must be one of: easy, medium, hard
- All amounts must be numbers (not strings)
- Use metric units: grams (g), kilograms (kg), milliliters (ml), liters (l). Teaspoons (tsp) and tablespoons (tbsp) are also acceptable for small amounts. Do NOT use cups, ounces, pounds, or other imperial units.
- Include at least 3 ingredients and 3 instructions
- Be specific with measurements and cooking times
- Use your tools to search for recipe inspiration from the web
- Respond ONLY with the JSON object, no other text`

func BuildGeneratePrompt(req models.GenerateRequest, existingTitles []string, cuisineCounts map[string]int) string {
	var parts []string
	parts = append(parts, "Generate a dinner recipe")

	if req.CuisineType != "" {
		parts = append(parts, fmt.Sprintf("from %s cuisine", req.CuisineType))
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
		parts = append(parts, fmt.Sprintf("serving %d people", req.Servings))
	}
	if req.AdditionalNotes != "" {
		parts = append(parts, fmt.Sprintf("with these preferences: %s", req.AdditionalNotes))
	}

	prompt := strings.Join(parts, " ") + "."

	if req.CuisineType == "" && len(cuisineCounts) > 0 {
		var dist []string
		for cuisine, count := range cuisineCounts {
			dist = append(dist, fmt.Sprintf("%s (%d)", cuisine, count))
		}
		prompt += "\n\nThe recipe collection currently has: " + strings.Join(dist, ", ") + ". Choose a cuisine that is underrepresented to keep the collection balanced."
	}

	if len(existingTitles) > 0 {
		prompt += "\n\nDo NOT duplicate any of these existing recipes: " + strings.Join(existingTitles, ", ") + "."
	}

	return prompt
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

// BuildBackgroundGeneratePrompt creates a prompt for unattended background recipe generation.
// It emphasises diversity and avoids existing titles like BuildGeneratePrompt, but without
// any user-specific constraints.
func BuildBackgroundGeneratePrompt(existingTitles []string, cuisineCounts map[string]int, index, total, servings int) string {
	prompt := fmt.Sprintf(
		"Generate a dinner recipe for exactly %d servings. "+
			"The `servings` field in your JSON MUST be %d and all ingredient amounts MUST be scaled accordingly.",
		servings, servings,
	)

	if len(cuisineCounts) > 0 {
		var dist []string
		for cuisine, count := range cuisineCounts {
			dist = append(dist, fmt.Sprintf("%s (%d)", cuisine, count))
		}
		prompt += "\n\nThe recipe collection currently has: " + strings.Join(dist, ", ") + ". Choose an underrepresented cuisine to keep the collection balanced."
	}

	if len(existingTitles) > 0 {
		prompt += "\n\nDo NOT duplicate any of these existing recipes: " + strings.Join(existingTitles, ", ") + "."
	}

	if total > 1 {
		prompt += fmt.Sprintf("\n\n(Recipe %d of %d in this background batch — make it unique from others in this batch.)", index, total)
	}

	return prompt
}

const reviewSystemPrompt = `You are a recipe reviewer. Check the recipe and return ONLY a JSON object with corrections. Be concise.

Checks:
- Every ingredient in the list is used in instructions, and vice versa
- Quantities are realistic (not 500g salt or 1g chicken)
- All units are metric: g, kg, ml, l, tsp, tbsp (no cups, ounces, pounds)
- Instructions are in logical order
- prep_time_minutes and cook_time_minutes match what the instructions describe
- Ingredient amounts are proportional to the serving count

If the recipe is correct, respond with exactly: {}
If corrections are needed, respond with a JSON object containing ONLY the fields that need changing. For ingredients or instructions, include the full corrected array. Examples:
{"prep_time_minutes": 20}
{"ingredients": [{"name":"chicken breast","amount":500,"unit":"g"},{"name":"salt","amount":5,"unit":"g"}]}
Do NOT return the full recipe. Do NOT add commentary.`

func BuildReviewPrompt(recipeJSON string) (system string, user string) {
	return reviewSystemPrompt, fmt.Sprintf("Review this recipe:\n\n%s", recipeJSON)
}

