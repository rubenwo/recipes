package handlers

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"math/rand/v2"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/rubenwo/mise/internal/database"
	"github.com/rubenwo/mise/internal/integrations/ah"
	"github.com/rubenwo/mise/internal/llm"
	"github.com/rubenwo/mise/internal/models"
	"github.com/rubenwo/mise/internal/translation"
)

type MealPlanHandler struct {
	queries      *database.Queries
	orchestrator *llm.Orchestrator
	ahClient     *ah.Client
	translator   *translation.Translator
}

func NewMealPlanHandler(q *database.Queries, o *llm.Orchestrator, searchTimeout time.Duration, t *translation.Translator) *MealPlanHandler {
	return &MealPlanHandler{queries: q, orchestrator: o, ahClient: ah.NewClient(searchTimeout), translator: t}
}

func (h *MealPlanHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	plan, err := h.queries.CreateMealPlan(r.Context(), req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create plan")
		return
	}

	writeJSON(w, http.StatusCreated, plan)
}

func (h *MealPlanHandler) List(w http.ResponseWriter, r *http.Request) {
	plans, err := h.queries.ListMealPlans(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list plans")
		return
	}

	writeJSON(w, http.StatusOK, plans)
}

func (h *MealPlanHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	plan, err := h.queries.GetMealPlan(r.Context(), id)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "plan not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get plan")
		return
	}

	writeJSON(w, http.StatusOK, plan)
}

type SuggestionsRequest struct {
	Ingredients []string `json:"ingredients"`
}

type SuggestionsResponse struct {
	DBRecipes       []models.Recipe `json:"db_recipes"`
	GeneratedRecipe *models.Recipe  `json:"generated_recipe,omitempty"`
}

func (h *MealPlanHandler) Suggestions(w http.ResponseWriter, r *http.Request) {
	var req SuggestionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Ingredients) == 0 {
		writeError(w, http.StatusBadRequest, "ingredients are required")
		return
	}

	// Search DB first
	dbRecipes, err := h.queries.SearchRecipesByIngredients(r.Context(), req.Ingredients)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to search recipes")
		return
	}

	resp := SuggestionsResponse{DBRecipes: dbRecipes}

	// If fewer than 2 DB matches, generate via LLM
	if len(dbRecipes) < 2 && h.orchestrator != nil {
		titles, _ := h.queries.ListRecipeTitles(r.Context())
		prompt := llm.BuildLeftoverPrompt(req.Ingredients)

		events := make(chan llm.SSEEvent, 10)
		go func() {
			for range events {
				// drain events, we don't need SSE here
			}
		}()

		recipe, _, genErr := h.orchestrator.GenerateWithTag(r.Context(), prompt, titles, events, "generation")
		if genErr == nil && recipe != nil {
			resp.GeneratedRecipe = recipe
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// getPlanIngredients returns the aggregated ingredient list for a plan,
// using the cache when available.
func (h *MealPlanHandler) getPlanIngredients(r *http.Request, id int) ([]models.AggregatedIngredient, error) {
	if cached, err := h.queries.GetPlanNormalizedIngredients(r.Context(), id); err == nil && len(cached) > 0 {
		var result []models.AggregatedIngredient
		if err := json.Unmarshal(cached, &result); err == nil {
			return result, nil
		}
	}

	plan, err := h.queries.GetMealPlan(r.Context(), id)
	if err != nil {
		return nil, err
	}

	type key struct{ name, unit string }
	agg := map[key]*models.AggregatedIngredient{}
	recipeNames := map[key]map[string]bool{}

	for _, mpr := range plan.Recipes {
		scale := 1.0
		if mpr.Recipe.Servings > 0 {
			scale = float64(mpr.Servings) / float64(mpr.Recipe.Servings)
		}
		for _, ing := range mpr.Recipe.Ingredients {
			normalizedName := normalizeIngredientName(ing.Name)
			k := key{normalizedName, strings.ToLower(ing.Unit)}
			if agg[k] == nil {
				agg[k] = &models.AggregatedIngredient{Name: ing.Name, Unit: ing.Unit}
				recipeNames[k] = map[string]bool{}
			}
			agg[k].Amount += ing.Amount * scale
			recipeNames[k][mpr.Recipe.Title] = true
		}
	}

	result := make([]models.AggregatedIngredient, 0, len(agg))
	for k, v := range agg {
		v.Amount = math.Round(v.Amount*100) / 100
		for title := range recipeNames[k] {
			v.Recipes = append(v.Recipes, title)
		}
		sort.Strings(v.Recipes)
		result = append(result, *v)
	}

	result = consolidateIngredients(result)

	// LLM fallback: merge any items that still share a normalized name after
	// deterministic consolidation (e.g. unknown cross-unit density).
	if h.orchestrator != nil {
		if dupes := findRemainingDuplicates(result); len(dupes) > 0 {
			if merged, err := h.orchestrator.DeduplicateIngredients(r.Context(), dupes); err == nil {
				result = applyLLMDedup(result, dupes, merged)
			} else {
				log.Printf("LLM ingredient dedup failed: %v", err)
			}
		}
	}

	if cacheJSON, err := json.Marshal(result); err == nil {
		_ = h.queries.SetPlanNormalizedIngredients(r.Context(), id, cacheJSON)
	}

	return result, nil
}

func (h *MealPlanHandler) Ingredients(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	result, err := h.getPlanIngredients(r, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "plan not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get plan")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

type AHOrderResult struct {
	Matched  []AHMatchedIngredient          `json:"matched"`
	NotFound []models.AggregatedIngredient  `json:"not_found"`
}

type AHMatchedIngredient struct {
	Ingredient models.AggregatedIngredient `json:"ingredient"`
	Product    ah.Product                  `json:"product"`
}

func (h *MealPlanHandler) OrderAH(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	ingredients, err := h.getPlanIngredients(r, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "plan not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get plan ingredients")
		return
	}
	if len(ingredients) == 0 {
		writeJSON(w, http.StatusOK, AHOrderResult{Matched: []AHMatchedIngredient{}, NotFound: []models.AggregatedIngredient{}})
		return
	}

	// Search AH for each ingredient in parallel (max 5 concurrent requests).
	type result struct {
		idx     int
		product *ah.Product
		err     error
	}

	sem := make(chan struct{}, 5)
	results := make([]result, len(ingredients))
	var wg sync.WaitGroup

	// Translation populates a DB cache, which is valuable even if the user
	// navigates away mid-request. Use a detached context with its own timeout
	// so cancelled requests still finish their cache-warming work.
	translateCtx, cancelTranslate := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelTranslate()

	for i, ing := range ingredients {
		wg.Add(1)
		go func(idx int, name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			query := name
			if h.translator != nil {
				query = h.translator.Translate(translateCtx, name, "nl")
			}
			p, err := h.ahClient.SearchProduct(query)
			results[idx] = result{idx: idx, product: p, err: err}
		}(i, ing.Name)
	}
	wg.Wait()

	var matched []AHMatchedIngredient
	var notFound []models.AggregatedIngredient

	for i, res := range results {
		if res.err != nil || res.product == nil {
			notFound = append(notFound, ingredients[i])
		} else {
			matched = append(matched, AHMatchedIngredient{
				Ingredient: ingredients[i],
				Product:    *res.product,
			})
		}
	}

	if matched == nil {
		matched = []AHMatchedIngredient{}
	}
	if notFound == nil {
		notFound = []models.AggregatedIngredient{}
	}

	writeJSON(w, http.StatusOK, AHOrderResult{Matched: matched, NotFound: notFound})
}

// sizeAdjectives are leading qualifiers that do not change what ingredient to buy.
// These are stripped from the front of an ingredient name (repeatedly) during normalization.
// Only strip from the front to avoid breaking compound ingredients like "ground beef".
var sizeAdjectives = map[string]bool{
	// Size / quantity
	"large": true, "small": true, "medium": true, "whole": true, "extra": true,
	// State
	"fresh": true, "freshly": true, "raw": true, "frozen": true, "dried": true,
	"cooked": true, "roasted": true, "toasted": true,
	// Prep method (leading only)
	"minced": true, "chopped": true, "diced": true, "sliced": true,
	"grated": true, "shredded": true, "crushed": true, "peeled": true,
	"rinsed": true, "softened": true, "melted": true, "sifted": true, "packed": true,
	// Animal cut qualifiers
	"tender": true, "boneless": true, "skinless": true, "lean": true, "trimmed": true,
	// Quality / origin
	"organic": true, "natural": true,
}

// ingredientAliases maps a fully-normalized name to its canonical equivalent.
// Applied after all other normalization so that stripping can produce matchable keys.
// Keys and values must be lowercase and already free of parentheticals/commas.
// NOTE: keys are written in their POST-normalization form (leading sizeAdjectives already
// stripped, trailing 's' already removed, parentheticals removed). For example:
//   "whole wheat flour" is never hit because "whole" is stripped → key must be "wheat flour".
//   "garlic cloves" is never hit because trailing 's' stripped → key must be "garlic clove".
var ingredientAliases = map[string]string{

	// ── Flour ──────────────────────────────────────────────────────────────────
	// "whole wheat flour" → "whole" stripped by sizeAdjectives → key is "wheat flour"
	"all purpose flour":  "flour",
	"all-purpose flour":  "flour",
	"wheat flour":        "flour",
	"plain flour":        "flour", // UK all-purpose
	"self rising flour":  "flour",
	"self-rising flour":  "flour",
	"self raising flour": "flour",
	"self-raising flour": "flour",
	"bread flour":        "flour",
	"cake flour":         "flour",
	"rice flour":         "flour",
	"00 flour":           "flour",
	"tipo 00":            "flour",
	// almond flour, spelt flour, buckwheat flour kept distinct — specialty items

	// ── Sugar ──────────────────────────────────────────────────────────────────
	"granulated sugar":     "sugar",
	"white sugar":          "sugar",
	"caster sugar":         "sugar", // UK superfine
	"castor sugar":         "sugar", // alternate spelling
	"superfine sugar":      "sugar",
	"cane sugar":           "sugar",
	"golden caster sugar":  "sugar",
	"demerara sugar":       "brown sugar",
	"muscovado":            "brown sugar",
	"muscovado sugar":      "brown sugar",
	"dark brown sugar":     "brown sugar",
	"light brown sugar":    "brown sugar",
	"powdered sugar":       "icing sugar", // US → UK canonical
	"confectioners sugar":  "icing sugar",
	"confectioners' sugar": "icing sugar",
	"treacle":              "molasses", // UK dark syrup → US equivalent
	"dark treacle":         "molasses",

	// ── Oil ────────────────────────────────────────────────────────────────────
	// "extra virgin olive oil" → "extra" stripped → key is "virgin olive oil"
	"virgin olive oil": "olive oil",
	"canola oil":       "vegetable oil", // North American name for rapeseed
	"rapeseed oil":     "vegetable oil", // UK/EU name for canola
	"corn oil":         "vegetable oil",
	"groundnut oil":    "peanut oil", // UK term

	// ── Pepper ─────────────────────────────────────────────────────────────────
	"black pepper":         "pepper",
	"ground black pepper":  "pepper",
	"cracked black pepper": "pepper",
	"black peppercorn":     "pepper",
	"ground white pepper":  "white pepper",
	"white peppercorn":     "white pepper",

	// ── Stock / Broth ───────────────────────────────────────────────────────────
	"chicken broth":   "chicken stock",
	"vegetable broth": "vegetable stock",
	"beef broth":      "beef stock",
	"fish broth":      "fish stock",
	"pork broth":      "pork stock",
	"lamb broth":      "lamb stock",

	// ── Cream ──────────────────────────────────────────────────────────────────
	"heavy cream":          "cream",
	"heavy whipping cream": "cream",
	"double cream":         "cream", // UK full-fat
	"whipping cream":       "cream",
	"single cream":         "light cream", // UK ~18% fat
	"half and half":        "light cream", // US ~10-12% fat

	// ── Milk ───────────────────────────────────────────────────────────────────
	// "whole milk" → "whole" stripped → "milk"; no alias needed
	"full fat milk":  "milk",
	"full-fat milk":  "milk",
	"skimmed milk":   "skim milk", // UK spelling
	"nonfat milk":    "skim milk", // US alternate
	"non-fat milk":   "skim milk",

	// ── Yogurt ─────────────────────────────────────────────────────────────────
	// "natural yogurt/yoghurt" → "natural" stripped → "yogurt"/"yoghurt"; only need yoghurt alias
	"yoghurt":       "yogurt",       // UK spelling
	"plain yogurt":  "yogurt",
	"plain yoghurt": "yogurt",
	"greek yoghurt": "greek yogurt", // UK spelling

	// ── Butter ─────────────────────────────────────────────────────────────────
	"unsalted butter": "butter",
	"salted butter":   "butter",

	// ── Garlic ─────────────────────────────────────────────────────────────────
	"garlic clove": "garlic",

	// ── Onion family ───────────────────────────────────────────────────────────
	"spring onion":  "green onion", // UK → US
	"scallion":      "green onion", // US regional → canonical
	"eschallot":     "shallot",     // Australian English
	"green shallot": "green onion", // Australian English

	// ── Tomato ─────────────────────────────────────────────────────────────────
	"tomato puree":       "tomato paste",
	"tomato concentrate": "tomato paste",
	"tinned tomato":      "canned tomato", // UK "tin" = US "can"

	// ── Vegetables — regional / alternate names ─────────────────────────────────
	"aubergine":     "eggplant",       // UK/FR → US
	"courgette":     "zucchini",       // UK/FR → US/IT
	"rocket":        "arugula",        // UK/AU → US/IT
	"swede":         "rutabaga",       // UK → US
	"beetroot":      "beet",           // UK → US
	"mangetout":     "snow pea",       // UK/FR → US
	"mange tout":    "snow pea",
	"capsicum":      "bell pepper",    // AU/NZ → US
	"cos lettuce":   "romaine lettuce",
	"romaine":       "romaine lettuce",
	"celery root":   "celeriac",       // US alternate
	"pak choi":      "bok choy",       // UK spelling
	"pak choy":      "bok choy",
	"baby corn":     "corn",
	"sweetcorn":     "corn",           // UK → US
	"corn on the cob": "corn",

	// ── Herbs ──────────────────────────────────────────────────────────────────
	// "coriander" alone safely maps to cilantro: compound forms like
	// "coriander seed" and "ground coriander" are never affected by this alias.
	"coriander":      "cilantro", // UK/EU leaf name → US name
	"coriander leaf": "cilantro",
	"coriander leave": "cilantro", // post-normalization form of "coriander leaves"
	// Parsley variants
	"flat leaf parsley":  "parsley",
	"flat-leaf parsley":  "parsley",
	"curly parsley":      "parsley",
	"italian parsley":    "parsley",

	// ── Cheese ─────────────────────────────────────────────────────────────────
	"parmigiano":          "parmesan",
	"parmigiano reggiano": "parmesan",
	"parmesan cheese":     "parmesan",
	"grana padano":        "parmesan", // similar hard Italian cheese; interchangeable for shopping
	"pecorino romano":     "pecorino",
	"mozzarella cheese":   "mozzarella",
	"feta cheese":         "feta",

	// ── Legumes ────────────────────────────────────────────────────────────────
	"garbanzo":            "chickpea", // US alternate → international canonical
	"garbanzo bean":       "chickpea",
	"haricot bean":        "navy bean",     // UK → US
	"cannellini bean":     "white bean",
	"great northern bean": "white bean",
	"butter bean":         "lima bean",     // UK → US
	"broad bean":          "fava bean",     // UK → US/IT
	"borlotti bean":       "cranberry bean",

	// ── Ground meat ────────────────────────────────────────────────────────────
	"beef mince":    "ground beef",    // UK → US
	"pork mince":    "ground pork",
	"turkey mince":  "ground turkey",
	"lamb mince":    "ground lamb",
	"chicken mince": "ground chicken",
	"mince beef":    "ground beef",

	// ── Seafood ────────────────────────────────────────────────────────────────
	"prawn":      "shrimp", // UK/AU → US
	"king prawn": "shrimp",

	// ── Pork cuts ──────────────────────────────────────────────────────────────
	"pork butt":   "pork shoulder",
	"boston butt": "pork shoulder",
	"pork collar": "pork shoulder",
	"pork neck":   "pork shoulder",

	// ── Pantry staples ──────────────────────────────────────────────────────────
	"cornflour":           "cornstarch",    // UK name for cornstarch
	"corn flour":          "cornstarch",
	"bicarbonate of soda": "baking soda",   // UK → US
	"bicarb":              "baking soda",
	"bicarb soda":         "baking soda",   // AU term
	"soya sauce":          "soy sauce",     // UK spelling
	"shoyu":               "soy sauce",     // Japanese name
	"vanilla essence":     "vanilla extract", // UK term
	"balsamic":            "balsamic vinegar",
	"passata":             "tomato passata", // common name shorthand

	// ── Nuts ───────────────────────────────────────────────────────────────────
	"groundnut": "peanut",   // UK term
	"filbert":   "hazelnut", // old/regional name for hazelnut
}

// ingredientDensity maps a normalized ingredient name to its density in g/mL,
// used to convert between weight and volume for the same ingredient.
var ingredientDensity = map[string]float64{
	"flour":             0.53,
	"all-purpose flour": 0.53,
	"bread flour":       0.56,
	"cake flour":        0.44,
	"whole wheat flour": 0.52,
	"wheat flour":       0.52,
	"sugar":             0.845,
	"granulated sugar":  0.845,
	"brown sugar":       0.72,
	"powdered sugar":    0.56,
	"icing sugar":       0.56,
	"salt":              1.20,
	"butter":            0.91,
	"olive oil":         0.91,
	"oil":               0.91,
	"vegetable oil":     0.91,
	"sunflower oil":     0.91,
	"water":             1.00,
	"milk":              1.03,
	"cream":             0.97,
	"honey":             1.42,
	"rice":              0.75,
	"oat":               0.35,
	"rolled oat":        0.35,
	"cornstarch":        0.61,
	"baking powder":     0.90,
	"baking soda":       1.08,
	"cocoa powder":      0.48,
	"almond flour":      0.44,
}

// normalizeIngredientName strips common leading adjectives, parentheticals,
// trailing modifiers and simple plurals so variant names key together.
// It then applies the ingredientAliases table to canonicalize known synonyms.
func normalizeIngredientName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	// Strip parenthetical notes: "butter (unsalted)" → "butter"
	if idx := strings.Index(name, "("); idx >= 0 {
		name = strings.TrimSpace(name[:idx])
	}
	// Strip after comma: "butter, softened" → "butter"
	if idx := strings.Index(name, ","); idx >= 0 {
		name = strings.TrimSpace(name[:idx])
	}
	// Strip common trailing phrases
	for _, suffix := range []string{" for serving", " for garnish", " for coating", " to taste", " to serve"} {
		name = strings.TrimSuffix(name, suffix)
	}
	// Strip leading size/quality/prep adjectives (only when words remain after stripping).
	// Repeated so "freshly ground black pepper" → "ground black pepper" → "black pepper".
	words := strings.Fields(name)
	for len(words) > 1 && sizeAdjectives[words[0]] {
		words = words[1:]
	}
	name = strings.Join(words, " ")
	// Normalize simple plurals: "eggs" → "egg", "onions" → "onion"
	// Avoid stripping from words ending in "ss" (e.g. "molasses").
	if len(name) > 3 && strings.HasSuffix(name, "s") && !strings.HasSuffix(name, "ss") {
		name = name[:len(name)-1]
	}
	// Apply synonym/alias table for known equivalents (flour variants, broth↔stock, etc.)
	if canonical, ok := ingredientAliases[name]; ok {
		name = canonical
	}
	return name
}

// isCountableUnit returns true for units that represent indivisible whole items.
func isCountableUnit(unit string) bool {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "", "piece", "pieces", "whole", "count", "pcs", "pc",
		"clove", "cloves", "slice", "slices", "head", "heads",
		"stalk", "stalks", "sprig", "sprigs", "bunch", "bunches",
		"can", "cans", "sheet", "sheets":
		return true
	}
	return false
}

// unitToBase converts an amount+unit to a canonical base unit for aggregation.
// Base units: "g" for weight, "ml" for volume. Other units are returned as-is.
func unitToBase(amount float64, unit string) (float64, string) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "kg":
		return amount * 1000, "g"
	case "oz":
		return amount * 28.35, "g"
	case "lb", "lbs":
		return amount * 453.592, "g"
	case "l", "liter", "litre", "liters", "litres":
		return amount * 1000, "ml"
	case "tsp", "teaspoon", "teaspoons":
		return amount * 4.929, "ml"
	case "tbsp", "tablespoon", "tablespoons":
		return amount * 14.787, "ml"
	case "cup", "cups":
		return amount * 240, "ml"
	default:
		return amount, strings.ToLower(strings.TrimSpace(unit))
	}
}

// baseToDisplay converts a base-unit amount to a human-friendly unit.
func baseToDisplay(amount float64, baseUnit string) (float64, string) {
	switch baseUnit {
	case "g":
		if amount >= 1000 {
			return amount / 1000, "kg"
		}
		return amount, "g"
	case "ml":
		if amount >= 1000 {
			return amount / 1000, "l"
		}
		return amount, "ml"
	default:
		return amount, baseUnit
	}
}

// roundAmount rounds to a whole number for amounts >= 10, otherwise 2 decimal places.
func roundAmount(amount float64) float64 {
	if amount >= 10 {
		return math.Round(amount)
	}
	return math.Round(amount*100) / 100
}

// compactIngredientName removes spaces and hyphens so that "chicken breast"
// and "chickenbreast" produce the same compact key for fuzzy comparison.
func compactIngredientName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r != ' ' && r != '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// levenshteinDistance computes the edit distance between two strings.
func levenshteinDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	for j := range prev {
		prev[j] = j
	}
	curr := make([]int, lb+1)
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			if del < ins {
				curr[j] = del
			} else {
				curr[j] = ins
			}
			if sub < curr[j] {
				curr[j] = sub
			}
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

// ingredientNameSimilarity returns a [0,1] similarity score between two
// already-normalized ingredient names. It strips spaces/hyphens before
// comparing so that "chicken breast" and "chickenbreast" score 1.0.
// Uses the max of edit-distance similarity and character-bigram Jaccard.
func ingredientNameSimilarity(a, b string) float64 {
	ca, cb := compactIngredientName(a), compactIngredientName(b)
	if ca == cb {
		return 1.0
	}
	maxLen := len(ca)
	if len(cb) > maxLen {
		maxLen = len(cb)
	}
	if maxLen == 0 {
		return 1.0
	}
	editSim := 1.0 - float64(levenshteinDistance(ca, cb))/float64(maxLen)
	bigramSim := jaccardBigrams(ca, cb) // reuse helper from recipes.go (same package)
	if editSim > bigramSim {
		return editSim
	}
	return bigramSim
}

// preferredIngredientName returns the "better" of two fuzzy-equivalent names.
// Prefers more words (properly spaced), then longer, then lexicographically smaller.
func preferredIngredientName(a, b string) string {
	wa, wb := len(strings.Fields(a)), len(strings.Fields(b))
	if wa != wb {
		if wa > wb {
			return a
		}
		return b
	}
	if len(a) != len(b) {
		if len(a) > len(b) {
			return a
		}
		return b
	}
	if a < b {
		return a
	}
	return b
}

// fuzzyCanonicalNames clusters the given names by similarity and returns a
// map from each name to its group's canonical representative.
// Names whose compact forms are identical, or whose similarity exceeds the
// threshold, are merged into one group. The canonical is the "best" name
// as determined by preferredIngredientName.
func fuzzyCanonicalNames(names []string) map[string]string {
	const threshold = 0.85

	// Union-Find: parent[n] starts as n itself.
	parent := make(map[string]string, len(names))
	for _, n := range names {
		parent[n] = n
	}
	var find func(string) string
	find = func(n string) string {
		if parent[n] != n {
			parent[n] = find(parent[n]) // path compression
		}
		return parent[n]
	}
	union := func(a, b string) {
		ra, rb := find(a), find(b)
		if ra == rb {
			return
		}
		// Pick the preferred name as the new root.
		if preferredIngredientName(ra, rb) == ra {
			parent[rb] = ra
		} else {
			parent[ra] = rb
		}
	}

	// O(n²) similarity check — fine for typical ingredient counts (~50-150).
	for i, a := range names {
		for _, b := range names[i+1:] {
			if ingredientNameSimilarity(a, b) >= threshold {
				union(a, b)
			}
		}
	}

	// Build final canonical map with full path compression.
	result := make(map[string]string, len(names))
	for _, n := range names {
		result[n] = find(n)
	}
	return result
}

// consolidateIngredients merges duplicate ingredients by:
//  1. normalizing names and converting to base units
//  2. fuzzy-grouping similar names (handles typos and compound-word variants)
//  3. re-aggregating by {canonicalName, baseUnit}
//  4. cross-unit merging g↔ml using known ingredient densities
//  5. rounding countable items up to the nearest whole number
func consolidateIngredients(ingredients []models.AggregatedIngredient) []models.AggregatedIngredient {
	type aggKey struct{ name, unit string }
	type entry struct {
		normName string
		amount   float64
		baseUnit string
		recipes  map[string]bool
	}

	// Pass 1: normalize + base-unit convert, aggregate by {normName, baseUnit}.
	agg := map[aggKey]*entry{}
	for _, ing := range ingredients {
		normName := normalizeIngredientName(ing.Name)
		baseAmount, baseUnit := unitToBase(ing.Amount, ing.Unit)
		k := aggKey{normName, baseUnit}
		if agg[k] == nil {
			agg[k] = &entry{normName: normName, baseUnit: baseUnit, recipes: map[string]bool{}}
		}
		agg[k].amount += baseAmount
		for _, r := range ing.Recipes {
			agg[k].recipes[r] = true
		}
	}

	// Fuzzy name grouping: collect distinct normNames, cluster by similarity,
	// then re-key the agg map so that fuzzy-equivalent names share one entry.
	{
		nameSet := make(map[string]struct{}, len(agg))
		for k := range agg {
			nameSet[k.name] = struct{}{}
		}
		names := make([]string, 0, len(nameSet))
		for n := range nameSet {
			names = append(names, n)
		}
		canon := fuzzyCanonicalNames(names)

		// Only re-aggregate when at least one name maps to a different canonical.
		needsRemap := false
		for n, c := range canon {
			if n != c {
				needsRemap = true
				break
			}
		}
		if needsRemap {
			type aggKey2 = aggKey
			agg2 := map[aggKey2]*entry{}
			for k, e := range agg {
				c := canon[k.name]
				k2 := aggKey2{c, k.unit}
				if agg2[k2] == nil {
					agg2[k2] = &entry{normName: c, baseUnit: k.unit, recipes: map[string]bool{}}
				}
				agg2[k2].amount += e.amount
				for r := range e.recipes {
					agg2[k2].recipes[r] = true
				}
			}
			agg = agg2
		}
	}

	// Pass 2: group by normName; attempt g↔ml cross-unit merge via density table.
	byName := map[string][]*entry{}
	for _, e := range agg {
		byName[e.normName] = append(byName[e.normName], e)
	}

	var merged []*entry
	for _, group := range byName {
		if len(group) == 1 {
			merged = append(merged, group[0])
			continue
		}
		// Separate into g, ml, and other-unit buckets.
		var gAmt, mlAmt float64
		var gRecipes, mlRecipes []string
		hasG, hasML := false, false
		var others []*entry
		for _, e := range group {
			switch e.baseUnit {
			case "g":
				gAmt += e.amount
				for r := range e.recipes {
					gRecipes = append(gRecipes, r)
				}
				hasG = true
			case "ml":
				mlAmt += e.amount
				for r := range e.recipes {
					mlRecipes = append(mlRecipes, r)
				}
				hasML = true
			default:
				others = append(others, e)
			}
		}
		if hasG && hasML {
			density, known := ingredientDensity[group[0].normName]
			if known {
				// Convert ml → g and merge into single g entry.
				combined := &entry{
					normName: group[0].normName,
					baseUnit: "g",
					amount:   gAmt + mlAmt*density,
					recipes:  map[string]bool{},
				}
				for _, r := range gRecipes {
					combined.recipes[r] = true
				}
				for _, r := range mlRecipes {
					combined.recipes[r] = true
				}
				merged = append(merged, combined)
				merged = append(merged, others...)
				continue
			}
		}
		// Cannot merge — leave all entries separate (LLM fallback will handle them).
		merged = append(merged, group...)
	}

	// Pass 3: build output with display units and countable ceiling.
	result := make([]models.AggregatedIngredient, 0, len(merged))
	for _, e := range merged {
		amount, unit := baseToDisplay(e.amount, e.baseUnit)
		if isCountableUnit(unit) {
			amount = math.Ceil(amount)
		} else {
			amount = roundAmount(amount)
		}
		displayName := e.normName
		if len(displayName) > 0 {
			displayName = strings.ToUpper(displayName[:1]) + displayName[1:]
		}
		recipes := make([]string, 0, len(e.recipes))
		for r := range e.recipes {
			recipes = append(recipes, r)
		}
		sort.Strings(recipes)
		result = append(result, models.AggregatedIngredient{
			Name:    displayName,
			Amount:  amount,
			Unit:    unit,
			Recipes: recipes,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	return result
}

// findRemainingDuplicates returns groups (≥2 items) that still share the same
// normalized name after deterministic consolidation.
func findRemainingDuplicates(ingredients []models.AggregatedIngredient) [][]models.AggregatedIngredient {
	groups := map[string][]int{}
	for i, ing := range ingredients {
		groups[normalizeIngredientName(ing.Name)] = append(groups[normalizeIngredientName(ing.Name)], i)
	}
	var result [][]models.AggregatedIngredient
	for _, indices := range groups {
		if len(indices) < 2 {
			continue
		}
		group := make([]models.AggregatedIngredient, len(indices))
		for j, idx := range indices {
			group[j] = ingredients[idx]
		}
		result = append(result, group)
	}
	return result
}

// applyLLMDedup replaces the duplicate groups in ingredients with the LLM-merged items.
func applyLLMDedup(ingredients []models.AggregatedIngredient, dupes [][]models.AggregatedIngredient, merged []models.AggregatedIngredient) []models.AggregatedIngredient {
	remove := map[string]bool{}
	for _, group := range dupes {
		for _, ing := range group {
			remove[normalizeIngredientName(ing.Name)] = true
		}
	}
	result := make([]models.AggregatedIngredient, 0, len(ingredients))
	for _, ing := range ingredients {
		if !remove[normalizeIngredientName(ing.Name)] {
			result = append(result, ing)
		}
	}
	result = append(result, merged...)
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	return result
}

func (h *MealPlanHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if err := h.queries.DeleteMealPlan(r.Context(), id); err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "plan not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete plan")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *MealPlanHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req models.UpdateMealPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Status != nil {
		valid := map[string]bool{"draft": true, "active": true, "completed": true}
		if !valid[*req.Status] {
			writeError(w, http.StatusBadRequest, "status must be draft, active, or completed")
			return
		}
		if err := h.queries.UpdateMealPlanStatus(r.Context(), id, *req.Status); err != nil {
			if err == pgx.ErrNoRows {
				writeError(w, http.StatusNotFound, "plan not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to update plan")
			return
		}
	}

	plan, err := h.queries.GetMealPlan(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get updated plan")
		return
	}

	writeJSON(w, http.StatusOK, plan)
}

func (h *MealPlanHandler) AddRecipe(w http.ResponseWriter, r *http.Request) {
	planID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid plan id")
		return
	}

	var req models.AddPlanRecipeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.RecipeID == 0 {
		writeError(w, http.StatusBadRequest, "recipe_id is required")
		return
	}
	if req.Servings <= 0 {
		req.Servings = 4
	}

	if err := h.queries.AddRecipeToPlan(r.Context(), planID, req.RecipeID, req.Servings); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add recipe to plan")
		return
	}
	_ = h.queries.InvalidatePlanIngredients(r.Context(), planID)

	plan, err := h.queries.GetMealPlan(r.Context(), planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get updated plan")
		return
	}

	writeJSON(w, http.StatusOK, plan)
}

func (h *MealPlanHandler) Randomize(w http.ResponseWriter, r *http.Request) {
	planID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid plan id")
		return
	}

	var req models.RandomizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Servings) == 0 {
		writeError(w, http.StatusBadRequest, "servings array is required (one entry per day)")
		return
	}
	total := len(req.Servings)

	// Load the current plan to see which recipes are already selected
	currentPlan, err := h.queries.GetMealPlan(r.Context(), planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load current plan")
		return
	}
	existingCount := len(currentPlan.Recipes)
	needed := total - existingCount
	if needed <= 0 {
		// Plan already has enough recipes; nothing to add
		writeJSON(w, http.StatusOK, currentPlan)
		return
	}

	existingIDs := make(map[int]bool, existingCount)
	for _, mpr := range currentPlan.Recipes {
		existingIDs[mpr.RecipeID] = true
	}

	// Fetch all recipes (lightweight) and eaten recipe IDs
	summaries, err := h.queries.ListRecipeSummaries(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list recipes")
		return
	}
	if len(summaries) == 0 {
		writeError(w, http.StatusBadRequest, "no recipes in the library to pick from")
		return
	}

	eaten, err := h.queries.ListEatenRecipeIDs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check eaten recipes")
		return
	}

	excludedIDs := make(map[int]bool, len(req.ExcludedIDs))
	for _, id := range req.ExcludedIDs {
		excludedIDs[id] = true
	}

	// Split into new (never eaten), eaten, and excluded pools, excluding already-selected recipes.
	// Excluded recipes are ones the user manually removed from this plan — they go into a last-resort pool.
	var newPool, eatenPool, excludedPool []database.RecipeSummary
	for _, s := range summaries {
		if existingIDs[s.ID] {
			continue
		}
		if excludedIDs[s.ID] {
			excludedPool = append(excludedPool, s)
			continue
		}
		if eaten[s.ID] {
			eatenPool = append(eatenPool, s)
		} else {
			newPool = append(newPool, s)
		}
	}

	// Determine targets: ~50/50, adjusting if a pool is too small
	newTarget := needed / 2
	eatenTarget := needed - newTarget
	if newTarget > len(newPool) {
		newTarget = len(newPool)
		eatenTarget = needed - newTarget
	}
	if eatenTarget > len(eatenPool) {
		eatenTarget = len(eatenPool)
		newTarget = needed - eatenTarget
	}
	// Final clamp in case total library is smaller than needed
	if newTarget > len(newPool) {
		newTarget = len(newPool)
	}

	selected := selectDiverse(newPool, newTarget)
	selected = append(selected, selectDiverse(eatenPool, eatenTarget)...)

	// If we still need more recipes (excluded pool was the only option), fall back to it
	if stillNeeded := needed - len(selected); stillNeeded > 0 {
		selected = append(selected, selectDiverse(excludedPool, stillNeeded)...)
	}

	// Shuffle the final selection so new and eaten are interleaved
	rand.Shuffle(len(selected), func(i, j int) {
		selected[i], selected[j] = selected[j], selected[i]
	})

	// Append new recipes to the plan (servings come from the tail of req.Servings)
	defaultServings := req.Servings[len(req.Servings)-1]
	for i, s := range selected {
		servings := defaultServings
		idx := existingCount + i
		if idx < len(req.Servings) {
			servings = req.Servings[idx]
		}
		if err := h.queries.AddRecipeToPlan(r.Context(), planID, s.ID, servings); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add recipe to plan")
			return
		}
	}
	_ = h.queries.InvalidatePlanIngredients(r.Context(), planID)

	plan, err := h.queries.GetMealPlan(r.Context(), planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get updated plan")
		return
	}

	writeJSON(w, http.StatusOK, plan)
}

// selectDiverse picks n recipes from pool, preferring distinct cuisines.
func selectDiverse(pool []database.RecipeSummary, n int) []database.RecipeSummary {
	if n <= 0 {
		return nil
	}
	if n >= len(pool) {
		result := make([]database.RecipeSummary, len(pool))
		copy(result, pool)
		rand.Shuffle(len(result), func(i, j int) {
			result[i], result[j] = result[j], result[i]
		})
		return result
	}

	// Shuffle the pool for randomness
	shuffled := make([]database.RecipeSummary, len(pool))
	copy(shuffled, pool)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	// Greedily pick recipes, preferring cuisines not yet selected
	usedCuisines := map[string]int{}
	var selected []database.RecipeSummary

	for len(selected) < n {
		bestIdx := -1
		bestCount := math.MaxInt
		for i, s := range shuffled {
			c := usedCuisines[s.CuisineType]
			if c < bestCount {
				bestCount = c
				bestIdx = i
			}
		}
		pick := shuffled[bestIdx]
		selected = append(selected, pick)
		usedCuisines[pick.CuisineType]++
		shuffled = append(shuffled[:bestIdx], shuffled[bestIdx+1:]...)
	}

	return selected
}

func (h *MealPlanHandler) RemoveRecipe(w http.ResponseWriter, r *http.Request) {
	planID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid plan id")
		return
	}
	recipeID, err := strconv.Atoi(chi.URLParam(r, "recipeId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid recipe id")
		return
	}

	if err := h.queries.RemoveRecipeFromPlan(r.Context(), planID, recipeID); err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "recipe not in plan")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to remove recipe")
		return
	}
	_ = h.queries.InvalidatePlanIngredients(r.Context(), planID)

	plan, err := h.queries.GetMealPlan(r.Context(), planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get updated plan")
		return
	}

	writeJSON(w, http.StatusOK, plan)
}

func (h *MealPlanHandler) UpdateRecipe(w http.ResponseWriter, r *http.Request) {
	planID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid plan id")
		return
	}
	recipeID, err := strconv.Atoi(chi.URLParam(r, "recipeId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid recipe id")
		return
	}

	var req models.UpdatePlanRecipeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.queries.UpdatePlanRecipe(r.Context(), planID, recipeID, req.Servings, req.Completed); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update recipe in plan")
		return
	}
	// Invalidate ingredient cache when servings change (not for completed toggle).
	if req.Servings != nil {
		_ = h.queries.InvalidatePlanIngredients(r.Context(), planID)
	}

	plan, err := h.queries.GetMealPlan(r.Context(), planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get updated plan")
		return
	}

	writeJSON(w, http.StatusOK, plan)
}
