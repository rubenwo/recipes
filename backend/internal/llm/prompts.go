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
	"Malaysian", "Mediterranean", "Mexican", "Middle Eastern", "Moroccan", "Peruvian",
	"Scandinavian", "Spanish", "Thai", "Turkish", "Vietnamese",
}

// cuisineAliases maps non-canonical cuisine names to their canonical form.
// All Malay, Nyonya, and Peranakan variants are unified under "Malaysian".
var cuisineAliases = map[string]string{
	"malay":            "Malaysian",
	"malaysian":        "Malaysian",
	"nyonya":           "Malaysian",
	"nonya":            "Malaysian",
	"peranakan":        "Malaysian",
	"nyonya peranakan": "Malaysian",
	"nonya peranakan":  "Malaysian",
	"malay/nyonya":     "Malaysian",
}

// NormalizeCuisine maps cuisine aliases to their canonical name.
// Returns the input unchanged if no alias is found.
func NormalizeCuisine(cuisine string) string {
	if canonical, ok := cuisineAliases[strings.ToLower(strings.TrimSpace(cuisine))]; ok {
		return canonical
	}
	return cuisine
}

// AllCuisines returns a sorted, deduplicated list of every cuisine the app
// recognises: the hardcoded KnownCuisines plus any cuisine already present in
// the database (via counts). This is used to constrain the LLM's cuisine_type
// output so it never invents a new label that diverges from the existing set.
func AllCuisines(counts map[string]int) []string {
	seen := make(map[string]bool, len(KnownCuisines)+len(counts))
	all := make([]string, 0, len(KnownCuisines)+len(counts))
	for _, c := range KnownCuisines {
		if !seen[c] {
			seen[c] = true
			all = append(all, c)
		}
	}
	for c := range counts {
		if c != "" && !seen[c] {
			seen[c] = true
			all = append(all, c)
		}
	}
	sort.Strings(all)
	return all
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

const systemPrompt = `You are a chef. Generate one dinner recipe as a JSON object with this shape:
{
  "title": "<recipe name>",
  "description": "<one short sentence>",
  "cuisine_type": "<cuisine>",
  "prep_time_minutes": 15,
  "cook_time_minutes": 30,
  "servings": 4,
  "difficulty": "easy|medium|hard",
  "ingredients": [
    {"name": "<ingredient>", "amount": 200, "unit": "g", "notes": "optional"}
  ],
  "instructions": ["<step>", "<step>", "<step>"],
  "dietary_restrictions": ["<tag>"],
  "tags": ["<tag>"]
}

Rules:
- Output JSON only, no surrounding text or markdown.
- Use metric units (g, kg, ml, l). tsp/tbsp allowed for spices.
- Whole items (onion, egg, tomato, bell pepper, lemon) use unit "" with a count; garlic uses "clove".
- At least 3 ingredients and 3 instructions.`

// BuildGeneratePrompt builds the user prompt for a single generation.
// Avoid-title hints are appended to the system message by the orchestrator,
// not included here.
func BuildGeneratePrompt(req models.GenerateRequest, cuisineCounts map[string]int) string {
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
		prompt += fmt.Sprintf(" The `servings` field MUST be %d; scale ingredient amounts accordingly.", req.Servings)
	}

	if cuisines := AllCuisines(cuisineCounts); len(cuisines) > 0 {
		prompt += "\n\nThe `cuisine_type` MUST be one of: " + strings.Join(cuisines, ", ") + "."
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

// BuildLeftoverPrompt builds the user prompt for a leftover-ingredient recipe.
// Avoid-title hints are added to the system message by the orchestrator.
func BuildLeftoverPrompt(ingredients []string) string {
	return fmt.Sprintf(`The user has leftover ingredients and wants a recipe that uses them up.

Leftover ingredients: %s

Generate a recipe that uses as many of these as possible, prioritizing perishable items.
Use web_search only if you need cooking technique details. Use db_search to check for similar existing recipes.`, strings.Join(ingredients, ", "))
}

// BuildImportPrompt builds the user prompt for importing a free-form recipe text.
// Avoid-title hints are added to the system message by the orchestrator.
func BuildImportPrompt(rawText string) string {
	return fmt.Sprintf(`Parse the following text into a complete, structured recipe.

The text may be just a name or a partial recipe. Use web_search only if details are missing or incomplete; use db_search to check for an existing similar recipe.

User input:
---
%s
---

Generate the complete recipe JSON, filling in any missing details from search results.`, rawText)
}

func SystemPrompt() string {
	return systemPrompt
}

// SystemPromptWithAvoid returns the base system prompt with an avoid-titles
// addendum appended. Empty avoidTitles returns the unchanged base prompt.
// Titles are capped at 30 to keep the prompt token-efficient for small models.
func SystemPromptWithAvoid(avoidTitles []string) string {
	if len(avoidTitles) == 0 {
		return systemPrompt
	}
	if len(avoidTitles) > 30 {
		avoidTitles = avoidTitles[:30]
	}
	return systemPrompt + "\n\nAvoid these existing titles: " + strings.Join(avoidTitles, ", ") + "."
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

// BuildDeduplicateIngredientsPrompt asks the LLM to merge groups of ingredients
// that share a name but couldn't be merged deterministically. Returns
// {"items": [...]} so JSON-mode (which requires a top-level object) is satisfied.
func BuildDeduplicateIngredientsPrompt(groupsJSON string) string {
	return fmt.Sprintf(`Consolidate a grocery shopping list. Each input group contains entries of the same item with different names/units that couldn't be merged automatically.

Input groups (JSON array of arrays):
%s

Return one merged ingredient per input group, in this exact shape:
{"items": [{"name": "<name>", "amount": 0, "unit": "<unit>"}]}

Rules:
- Generic English name, lowercase.
- Round countable items (eggs, onions, cloves) up to the nearest whole number.
- Weight (g, kg) for dry goods, volume (ml, l) for liquids, "" for whole items.
- Sum amounts across entries, converting units as needed.
- Output the JSON object only.`, groupsJSON)
}

// BuildBackgroundGeneratePrompt creates a prompt for unattended background recipe generation.
// targetCuisine is pre-selected by the caller; pass an empty string to let the model choose.
// allCuisines constrains the model's cuisine_type output to known + DB cuisines; pass nil to skip.
// Avoid-title hints go in the system message via the orchestrator.
func BuildBackgroundGeneratePrompt(targetCuisine string, allCuisines []string, index, total, servings int) string {
	cuisineDesc := ""
	if targetCuisine != "" {
		cuisineDesc = targetCuisine + " "
	}
	prompt := fmt.Sprintf(
		"Generate a %sdinner recipe for exactly %d servings. The `servings` field MUST be %d; scale ingredient amounts accordingly.",
		cuisineDesc, servings, servings,
	)

	if len(allCuisines) > 0 {
		prompt += "\n\nThe `cuisine_type` MUST be one of: " + strings.Join(allCuisines, ", ") + "."
	}

	if total > 1 {
		prompt += fmt.Sprintf(" (Recipe %d of %d in this batch — make it distinct from the others.)", index, total)
	}

	return prompt
}

