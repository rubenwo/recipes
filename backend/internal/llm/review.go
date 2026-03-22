package llm

import (
	"log"
	"math"
	"strings"

	"github.com/rubenwo/recipes/internal/models"
)

type unitConversion struct {
	factor     float64
	targetUnit string
}

// imperialToMetric maps common non-metric unit strings to their metric equivalent.
var imperialToMetric = map[string]unitConversion{
	"cup":      {240, "ml"},
	"cups":     {240, "ml"},
	"oz":       {28.35, "g"},
	"ounce":    {28.35, "g"},
	"ounces":   {28.35, "g"},
	"lb":       {453.6, "g"},
	"lbs":      {453.6, "g"},
	"pound":    {453.6, "g"},
	"pounds":   {453.6, "g"},
	"fl oz":    {29.57, "ml"},
	"fl. oz.":  {29.57, "ml"},
	"pint":     {473.2, "ml"},
	"pints":    {473.2, "ml"},
	"quart":    {946.4, "ml"},
	"quarts":   {946.4, "ml"},
	"gallon":   {3785.4, "ml"},
	"gallons":  {3785.4, "ml"},
	"stick":    {113.4, "g"}, // stick of butter
	"sticks":   {113.4, "g"},
}

// applyDeterministicReview validates and corrects a recipe without an LLM call.
// It converts any non-metric units to their metric equivalents and logs a warning
// when an ingredient name does not appear to be referenced in the instructions.
func applyDeterministicReview(recipe *models.Recipe) {
	// 1. Convert non-metric units to metric.
	for i, ing := range recipe.Ingredients {
		normalized := strings.ToLower(strings.TrimSpace(ing.Unit))
		if conv, ok := imperialToMetric[normalized]; ok {
			converted := math.Round(ing.Amount*conv.factor*10) / 10
			log.Printf("Review: converted %s %.4g %s → %.4g %s",
				ing.Name, ing.Amount, ing.Unit, converted, conv.targetUnit)
			recipe.Ingredients[i].Amount = converted
			recipe.Ingredients[i].Unit = conv.targetUnit
		}
	}

	// 2. Warn if an ingredient's name seems absent from the instructions.
	// Uses the longest word (>3 chars) in the ingredient name as a proxy.
	instructions := strings.ToLower(strings.Join(recipe.Instructions, " "))
	for _, ing := range recipe.Ingredients {
		words := strings.Fields(strings.ToLower(ing.Name))
		found := false
		for _, w := range words {
			if len(w) > 3 && strings.Contains(instructions, w) {
				found = true
				break
			}
		}
		if !found {
			log.Printf("Review: ingredient %q may not be referenced in instructions", ing.Name)
		}
	}
}
