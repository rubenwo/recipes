package handlers

import (
	"encoding/json"
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
		prompt := llm.BuildLeftoverPrompt(req.Ingredients, titles)

		events := make(chan llm.SSEEvent, 10)
		go func() {
			for range events {
				// drain events, we don't need SSE here
			}
		}()

		recipe, _, genErr := h.orchestrator.Generate(r.Context(), prompt, events)
		if genErr == nil && recipe != nil {
			resp.GeneratedRecipe = recipe
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

type AggregatedIngredient struct {
	Name    string   `json:"name"`
	Amount  float64  `json:"amount"`
	Unit    string   `json:"unit"`
	Recipes []string `json:"recipes"`
}

// getPlanIngredients returns the aggregated ingredient list for a plan,
// using the cache when available.
func (h *MealPlanHandler) getPlanIngredients(r *http.Request, id int) ([]AggregatedIngredient, error) {
	if cached, err := h.queries.GetPlanNormalizedIngredients(r.Context(), id); err == nil && len(cached) > 0 {
		var result []AggregatedIngredient
		if err := json.Unmarshal(cached, &result); err == nil {
			return result, nil
		}
	}

	plan, err := h.queries.GetMealPlan(r.Context(), id)
	if err != nil {
		return nil, err
	}

	type key struct{ name, unit string }
	agg := map[key]*AggregatedIngredient{}
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
				agg[k] = &AggregatedIngredient{Name: ing.Name, Unit: ing.Unit}
				recipeNames[k] = map[string]bool{}
			}
			agg[k].Amount += ing.Amount * scale
			recipeNames[k][mpr.Recipe.Title] = true
		}
	}

	result := make([]AggregatedIngredient, 0, len(agg))
	for k, v := range agg {
		v.Amount = math.Round(v.Amount*100) / 100
		for title := range recipeNames[k] {
			v.Recipes = append(v.Recipes, title)
		}
		sort.Strings(v.Recipes)
		result = append(result, *v)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})

	if len(plan.Recipes) > 1 {
		result = consolidateIngredients(result)
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
	Matched  []AHMatchedIngredient `json:"matched"`
	NotFound []AggregatedIngredient `json:"not_found"`
}

type AHMatchedIngredient struct {
	Ingredient AggregatedIngredient `json:"ingredient"`
	Product    ah.Product           `json:"product"`
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
		writeJSON(w, http.StatusOK, AHOrderResult{Matched: []AHMatchedIngredient{}, NotFound: []AggregatedIngredient{}})
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

	for i, ing := range ingredients {
		wg.Add(1)
		go func(idx int, name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			query := name
			if h.translator != nil {
				query = h.translator.Translate(r.Context(), name, "nl")
			}
			p, err := h.ahClient.SearchProduct(query)
			results[idx] = result{idx: idx, product: p, err: err}
		}(i, ing.Name)
	}
	wg.Wait()

	var matched []AHMatchedIngredient
	var notFound []AggregatedIngredient

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
		notFound = []AggregatedIngredient{}
	}

	writeJSON(w, http.StatusOK, AHOrderResult{Matched: matched, NotFound: notFound})
}

// normalizeIngredientName strips common trailing modifiers so ingredients like
// "butter" and "butter, softened" key together for deduplication.
func normalizeIngredientName(name string) string {
	name = strings.ToLower(name)
	if idx := strings.Index(name, ","); idx >= 0 {
		name = strings.TrimSpace(name[:idx])
	}
	for _, suffix := range []string{" for serving", " for garnish", " for coating", " to taste", " to serve"} {
		name = strings.TrimSuffix(name, suffix)
	}
	return strings.TrimSpace(name)
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

// consolidateIngredients merges duplicate ingredients by normalizing names and
// converting to base units before summing. Intended for plans with >1 recipe.
func consolidateIngredients(ingredients []AggregatedIngredient) []AggregatedIngredient {
	type key struct{ name, unit string }
	type entry struct {
		displayName string
		amount      float64
		baseUnit    string
		recipes     map[string]bool
	}

	agg := map[key]*entry{}
	for _, ing := range ingredients {
		normName := normalizeIngredientName(ing.Name)
		baseAmount, baseUnit := unitToBase(ing.Amount, ing.Unit)
		k := key{normName, baseUnit}
		if agg[k] == nil {
			agg[k] = &entry{
				displayName: normName,
				baseUnit:    baseUnit,
				recipes:     map[string]bool{},
			}
		}
		agg[k].amount += baseAmount
		for _, r := range ing.Recipes {
			agg[k].recipes[r] = true
		}
	}

	result := make([]AggregatedIngredient, 0, len(agg))
	for _, e := range agg {
		displayAmount, displayUnit := baseToDisplay(e.amount, e.baseUnit)
		displayAmount = roundAmount(displayAmount)
		// Capitalize first letter for display.
		displayName := e.displayName
		if len(displayName) > 0 {
			displayName = strings.ToUpper(displayName[:1]) + displayName[1:]
		}
		recipes := make([]string, 0, len(e.recipes))
		for r := range e.recipes {
			recipes = append(recipes, r)
		}
		sort.Strings(recipes)
		result = append(result, AggregatedIngredient{
			Name:    displayName,
			Amount:  displayAmount,
			Unit:    displayUnit,
			Recipes: recipes,
		})
	}
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
	count := len(req.Servings)

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

	// Split into new (never eaten) and eaten pools
	var newPool, eatenPool []database.RecipeSummary
	for _, s := range summaries {
		if eaten[s.ID] {
			eatenPool = append(eatenPool, s)
		} else {
			newPool = append(newPool, s)
		}
	}

	// Determine targets: ~50/50, adjusting if a pool is too small
	newTarget := count / 2
	eatenTarget := count - newTarget
	if newTarget > len(newPool) {
		newTarget = len(newPool)
		eatenTarget = count - newTarget
	}
	if eatenTarget > len(eatenPool) {
		eatenTarget = len(eatenPool)
		newTarget = count - eatenTarget
	}
	// Final clamp in case total library is smaller than count
	if newTarget > len(newPool) {
		newTarget = len(newPool)
	}

	selected := selectDiverse(newPool, newTarget)
	selected = append(selected, selectDiverse(eatenPool, eatenTarget)...)

	// Shuffle the final selection so new and eaten are interleaved
	rand.Shuffle(len(selected), func(i, j int) {
		selected[i], selected[j] = selected[j], selected[i]
	})

	recipeIDs := make([]int, len(selected))
	for i, s := range selected {
		recipeIDs[i] = s.ID
	}

	if err := h.queries.ReplacePlanRecipes(r.Context(), planID, recipeIDs, req.Servings); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to set plan recipes")
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
