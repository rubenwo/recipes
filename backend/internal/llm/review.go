package llm

import (
	"log"
	"math"
	"strings"

	"github.com/rubenwo/mise/internal/models"
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

// countableSpec describes a naturally-whole ingredient: how many grams make one
// natural unit, and what that unit is called ("clove", "" for a bare count, etc.).
type countableSpec struct {
	gramsPerUnit float64
	unit         string // "" = bare count (no unit label), "clove", "stalk", etc.
}

// countableIngredients maps a key substring (lowercase) to its natural unit.
// Entries are ordered from most specific to least so that "bell pepper" is
// matched before a hypothetical shorter key.
var countableIngredients = []struct {
	key  string
	spec countableSpec
}{
	// Garlic — each clove is ~5 g
	{"garlic clove", countableSpec{5, "clove"}},
	{"clove of garlic", countableSpec{5, "clove"}},
	{"garlic", countableSpec{5, "clove"}},
	// Whole vegetables / fruit sold individually
	{"bell pepper", countableSpec{150, ""}},
	{"sweet potato", countableSpec{150, ""}},
	{"zucchini", countableSpec{200, ""}},
	{"courgette", countableSpec{200, ""}},
	{"cucumber", countableSpec{300, ""}},
	{"avocado", countableSpec{200, ""}},
	{"shallot", countableSpec{30, ""}},
	{"tomato", countableSpec{120, ""}},
	{"onion", countableSpec{150, ""}},
	{"carrot", countableSpec{60, ""}},
	{"potato", countableSpec{150, ""}},
	{"banana", countableSpec{120, ""}},
	{"apple", countableSpec{182, ""}},
	{"lemon", countableSpec{100, ""}},
	{"lime", countableSpec{67, ""}},
	{"egg", countableSpec{60, ""}},
	// Celery is measured in stalks (~40 g each)
	{"celery", countableSpec{40, "stalk"}},
}

// countableDisqualifiers are words that indicate the ingredient is a processed
// form of a whole item and should NOT be converted to a count unit.
// e.g. "garlic powder", "tomato paste", "tomato sauce" must stay in grams.
var countableDisqualifiers = map[string]bool{
	"powder": true, "paste": true, "sauce": true, "flakes": true,
	"salt": true, "extract": true, "oil": true, "puree": true,
	"juice": true, "butter": true, "pickled": true, "dried": true,
	"canned": true, "crushed": true, "stewed": true, "pureed": true,
	"puréed": true,
}

// matchesCountableKey returns true when nameLower contains all words of key
// (allowing simple plurals via HasPrefix), and contains no disqualifying word.
// Word-level matching prevents "garlic powder" from matching key "garlic".
func matchesCountableKey(nameLower, key string) bool {
	nameWords := strings.Fields(nameLower)
	for _, w := range nameWords {
		if countableDisqualifiers[w] {
			return false
		}
	}
	for _, kw := range strings.Fields(key) {
		found := false
		for _, nw := range nameWords {
			// Allow exact match or simple plural (HasPrefix handles "tomatoes"→"tomato").
			if nw == kw || strings.HasPrefix(nw, kw) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// normalizeToCountableUnit converts a weight-based ingredient amount to its
// natural count unit if the ingredient is in the countableIngredients table.
// It only acts when the unit is a weight (g, kg, mg) and the resulting count
// falls in the plausible range [0.25, 20].
func normalizeToCountableUnit(ing *models.Ingredient) {
	unitLower := strings.ToLower(strings.TrimSpace(ing.Unit))
	var grams float64
	switch unitLower {
	case "g":
		grams = ing.Amount
	case "kg":
		grams = ing.Amount * 1000
	case "mg":
		grams = ing.Amount / 1000
	default:
		return // not a weight — leave untouched
	}

	nameLower := strings.ToLower(strings.TrimSpace(ing.Name))
	for _, entry := range countableIngredients {
		if !matchesCountableKey(nameLower, entry.key) {
			continue
		}
		count := grams / entry.spec.gramsPerUnit
		if count < 0.25 || count > 20 {
			return // implausible as a whole-item count; skip
		}
		// Round to nearest half for small counts, whole number otherwise.
		var rounded float64
		if count < 3 {
			rounded = math.Round(count*2) / 2
		} else {
			rounded = math.Round(count)
		}
		if rounded < 0.5 {
			rounded = 0.5
		}
		log.Printf("Review: converted %q %.4g%s → %.4g %q",
			ing.Name, ing.Amount, ing.Unit, rounded, entry.spec.unit)
		ing.Amount = rounded
		ing.Unit = entry.spec.unit
		return
	}
}

// applyDeterministicReview validates and corrects a recipe without an LLM call.
//  1. Converts non-metric units to metric.
//  2. Converts weight-based amounts for whole/countable items to natural units.
//  3. Logs a warning when an ingredient name seems absent from the instructions.
func applyDeterministicReview(recipe *models.Recipe) {
	// 1. Convert non-metric (imperial) units to metric.
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

	// 2. Convert weight amounts for countable whole items to natural units.
	for i := range recipe.Ingredients {
		normalizeToCountableUnit(&recipe.Ingredients[i])
	}

	// 3. Warn if an ingredient's name seems absent from the instructions.
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
