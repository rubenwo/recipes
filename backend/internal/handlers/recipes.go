package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/rubenwo/mise/internal/database"
	"github.com/rubenwo/mise/internal/llm"
	"github.com/rubenwo/mise/internal/models"
	"github.com/rubenwo/mise/internal/tools"
)

type RecipeHandler struct {
	queries       *database.Queries
	imageSearcher *tools.ImageSearcher
	llmPool       *llm.ClientPool
}

func NewRecipeHandler(q *database.Queries, imageSearcher *tools.ImageSearcher, llmPool *llm.ClientPool) *RecipeHandler {
	return &RecipeHandler{queries: q, imageSearcher: imageSearcher, llmPool: llmPool}
}

func (h *RecipeHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	cuisineType := r.URL.Query().Get("cuisine_type")

	if limit <= 0 {
		limit = 20
	} else if limit > 1000 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}

	recipes, total, err := h.queries.ListRecipes(r.Context(), limit, offset, cuisineType)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list recipes")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"recipes": recipes,
		"total":   total,
	})
}

func (h *RecipeHandler) ListCuisines(w http.ResponseWriter, r *http.Request) {
	cuisines, err := h.queries.ListCuisines(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list cuisines")
		return
	}
	if cuisines == nil {
		cuisines = []database.CuisineMeta{}
	}
	writeJSON(w, http.StatusOK, cuisines)
}

func (h *RecipeHandler) LibrarySearchDirect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Keywords            string   `json:"keywords"`
		CuisineType         string   `json:"cuisine_type"`
		DietaryRestrictions []string `json:"dietary_restrictions"`
		Tags                []string `json:"tags"`
		MaxTotalMinutes     int      `json:"max_total_minutes"`
		Limit               int      `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Limit <= 0 {
		req.Limit = 50
	}

	recipes, err := h.queries.LibrarySearch(r.Context(), database.LibrarySearchRequest{
		Keywords:            req.Keywords,
		CuisineType:         req.CuisineType,
		DietaryRestrictions: req.DietaryRestrictions,
		Tags:                req.Tags,
		MaxTotalMinutes:     req.MaxTotalMinutes,
		Limit:               req.Limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to search recipes")
		return
	}
	if recipes == nil {
		recipes = []models.Recipe{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"recipes": recipes,
		"total":   len(recipes),
	})
}

func (h *RecipeHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	recipe, err := h.queries.GetRecipe(r.Context(), id)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "recipe not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get recipe")
		return
	}

	writeJSON(w, http.StatusOK, recipe)
}

func (h *RecipeHandler) Create(w http.ResponseWriter, r *http.Request) {
	var recipe models.Recipe
	if err := json.NewDecoder(r.Body).Decode(&recipe); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if recipe.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	recipe.CuisineType = llm.NormalizeCuisine(recipe.CuisineType)

	if err := h.queries.CreateRecipe(r.Context(), &recipe); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create recipe")
		return
	}

	writeJSON(w, http.StatusCreated, recipe)

	// Fetch and store an image in the background so it is ready when the user next views the recipe.
	if h.imageSearcher != nil {
		go h.fetchAndStoreImage(context.Background(), recipe.ID, recipe.Title)
	}
}

func (h *RecipeHandler) fetchAndStoreImage(ctx context.Context, id int, title string) {
	filename := fmt.Sprintf("recipe-%d", id)
	imageURL, err := h.imageSearcher.SearchAndDownloadRecipeImage(ctx, title, filename)
	if err != nil {
		log.Printf("Auto image fetch for recipe %q: %v", title, err)
		return
	}
	if err := h.queries.SetRecipeImage(ctx, id, imageURL); err != nil {
		log.Printf("Auto image fetch: failed to save image for recipe %q: %v", title, err)
	}
}

type UpdateContentRequest struct {
	Ingredients  []models.Ingredient `json:"ingredients"`
	Instructions []string            `json:"instructions"`
	CuisineType  string              `json:"cuisine_type"`
}

func (h *RecipeHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req UpdateContentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.queries.UpdateRecipeContent(r.Context(), id, req.Ingredients, req.Instructions, llm.NormalizeCuisine(req.CuisineType)); err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "recipe not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update recipe")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *RecipeHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if err := h.queries.DeleteRecipe(r.Context(), id); err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "recipe not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete recipe")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *RecipeHandler) Search(w http.ResponseWriter, r *http.Request) {
	var req models.SearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	recipes, total, err := h.queries.SearchRecipes(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to search recipes")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"recipes": recipes,
		"total":   total,
	})
}

const aiSearchSystemPrompt = `You are a recipe search assistant. Call the library_search tool to find recipes matching the user's request. Extract keywords (ingredients, dish names, cooking style), cuisine, dietary restrictions, tags, and time constraints from the query. Call the tool once.`

func (h *RecipeHandler) AISearch(w http.ResponseWriter, r *http.Request) {
	var req models.AISearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}
	if h.llmPool == nil {
		writeError(w, http.StatusServiceUnavailable, "AI search not available")
		return
	}

	client := h.llmPool.AcquireWithTag("search")
	if client == nil {
		writeError(w, http.StatusServiceUnavailable, `no provider with tag "search" configured or healthy`)
		return
	}

	messages := []llm.Message{
		{Role: "system", Content: aiSearchSystemPrompt},
		{Role: "user", Content: req.Query},
	}

	resp, err := client.Chat(r.Context(), messages, []llm.Tool{llm.LibrarySearchTool})
	if err != nil {
		log.Printf("AI search LLM error: %v", err)
		writeError(w, http.StatusInternalServerError, "AI search failed: "+err.Error())
		return
	}

	// Parse the tool call arguments from the LLM response.
	var toolArgs struct {
		Keywords            string   `json:"keywords"`
		CuisineType         string   `json:"cuisine_type"`
		DietaryRestrictions []string `json:"dietary_restrictions"`
		Tags                []string `json:"tags"`
		MaxTotalMinutes     int      `json:"max_total_minutes"`
	}

	if len(resp.Message.ToolCalls) > 0 {
		tc := resp.Message.ToolCalls[0]
		if err := json.Unmarshal(tc.Function.Arguments, &toolArgs); err != nil {
			log.Printf("AI search tool arg parse error: %v, args: %s", err, string(tc.Function.Arguments))
			writeError(w, http.StatusInternalServerError, "failed to parse tool arguments")
			return
		}
	} else {
		// Fallback: model didn't use the tool — treat entire response as keyword search.
		log.Printf("AI search: no tool call, falling back to keyword search for %q", req.Query)
		toolArgs.Keywords = req.Query
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}

	searchReq := database.LibrarySearchRequest{
		Keywords:            toolArgs.Keywords,
		CuisineType:         toolArgs.CuisineType,
		DietaryRestrictions: toolArgs.DietaryRestrictions,
		Tags:                toolArgs.Tags,
		MaxTotalMinutes:     toolArgs.MaxTotalMinutes,
		Limit:               limit,
	}

	recipes, err := h.queries.LibrarySearch(r.Context(), searchReq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to search recipes")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"recipes":     recipes,
		"total":       len(recipes),
		"interpreted": toolArgs,
	})
}

// PreviewImage returns an image URL for a recipe title without saving anything.
// Used by the frontend to show images for generated recipes before they are saved.
func (h *RecipeHandler) PreviewImage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	if h.imageSearcher == nil {
		writeError(w, http.StatusServiceUnavailable, "image search not available")
		return
	}

	imageURL, err := h.imageSearcher.SearchRecipeImage(r.Context(), req.Title)
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not find image: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"image_url": imageURL})
}

func (h *RecipeHandler) FetchImage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	recipe, err := h.queries.GetRecipe(r.Context(), id)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "recipe not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get recipe")
		return
	}

	if h.imageSearcher == nil {
		writeError(w, http.StatusServiceUnavailable, "image search not available")
		return
	}

	filename := fmt.Sprintf("recipe-%d", id)
	imageURL, err := h.imageSearcher.SearchAndDownloadRecipeImage(r.Context(), recipe.Title, filename)
	if err != nil {
		log.Printf("Image search for %q failed: %v", recipe.Title, err)
		writeError(w, http.StatusBadGateway, "could not find an image: "+err.Error())
		return
	}

	if err := h.queries.SetRecipeImage(r.Context(), id, imageURL); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save image url")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"image_url": imageURL})
}

func (h *RecipeHandler) Suggestions(w http.ResponseWriter, r *http.Request) {
	count, _ := strconv.Atoi(r.URL.Query().Get("count"))
	if count <= 0 {
		count = 3
	}
	if count > 10 {
		count = 10
	}

	meta, err := h.queries.ListRecipeMeta(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load recipes")
		return
	}

	ids := selectSuggestedIDs(meta, count, time.Now())
	recipes, err := h.queries.GetRecipesByIDs(r.Context(), ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load suggestions")
		return
	}

	// Preserve the selection order.
	order := make(map[int]int, len(ids))
	for i, id := range ids {
		order[id] = i
	}
	sort.Slice(recipes, func(i, j int) bool {
		return order[recipes[i].ID] < order[recipes[j].ID]
	})

	writeJSON(w, http.StatusOK, recipes)
}

// selectSuggestedIDs picks count recipe IDs from meta using a date-seeded algorithm
// that ensures cuisine diversity and surfaces older (less-visible) recipes.
func selectSuggestedIDs(meta []database.RecipeMeta, count int, now time.Time) []int {
	if len(meta) == 0 {
		return nil
	}
	if count > len(meta) {
		count = len(meta)
	}

	// Seed with today's date so suggestions are stable within a day.
	seed := int64(now.Year())*10000 + int64(now.Month())*100 + int64(now.Day())
	rng := rand.New(rand.NewSource(seed))

	type entry struct {
		id      int
		ageDays float64
	}

	// Group recipes by cuisine.
	pools := map[string][]entry{}
	for _, m := range meta {
		c := m.CuisineType
		if c == "" {
			c = "Other"
		}
		age := now.Sub(m.CreatedAt).Hours() / 24
		pools[c] = append(pools[c], entry{m.ID, age})
	}

	// Sort cuisines for a deterministic base, then shuffle with the seeded RNG.
	cuisines := make([]string, 0, len(pools))
	for c := range pools {
		cuisines = append(cuisines, c)
	}
	sort.Strings(cuisines)
	rng.Shuffle(len(cuisines), func(i, j int) { cuisines[i], cuisines[j] = cuisines[j], cuisines[i] })

	picked := make([]int, 0, count)
	for len(picked) < count {
		progress := false
		for _, c := range cuisines {
			if len(picked) >= count {
				break
			}
			pool := pools[c]
			if len(pool) == 0 {
				continue
			}
			// Weight each recipe by age: older recipes surface more easily.
			// w = 1 + ln(1 + ageDays/7) → ranges from 1.0 (new) to ~5 (1 year old).
			totalW := 0.0
			for _, e := range pool {
				totalW += 1.0 + math.Log1p(e.ageDays/7.0)
			}
			target := rng.Float64() * totalW
			cum, idx := 0.0, len(pool)-1
			for i, e := range pool {
				cum += 1.0 + math.Log1p(e.ageDays/7.0)
				if cum >= target {
					idx = i
					break
				}
			}
			picked = append(picked, pool[idx].id)
			pools[c] = append(pool[:idx], pool[idx+1:]...)
			progress = true
		}
		if !progress {
			break // all pools exhausted
		}
	}
	return picked
}

// FindDuplicates scans all recipes and returns groups of likely duplicates,
// identified by comparing normalised titles and descriptions without an LLM.
func (h *RecipeHandler) FindDuplicates(w http.ResponseWriter, r *http.Request) {
	stubs, err := h.queries.ListRecipesForDedup(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list recipes")
		return
	}

	n := len(stubs)
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(x, y int) { parent[find(x)] = find(y) }

	const threshold = 0.60
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if recipeSimilarity(stubs[i], stubs[j]) >= threshold {
				union(i, j)
			}
		}
	}

	// Group indices by their root.
	groups := map[int][]int{}
	for i := 0; i < n; i++ {
		root := find(i)
		groups[root] = append(groups[root], i)
	}

	// For each group with ≥2 members, collect full recipe data.
	var dupGroups [][]models.Recipe
	for _, indices := range groups {
		if len(indices) < 2 {
			continue
		}
		ids := make([]int, len(indices))
		for k, idx := range indices {
			ids[k] = stubs[idx].ID
		}
		full, err := h.queries.GetRecipesByIDs(r.Context(), ids)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to fetch duplicate recipes")
			return
		}
		dupGroups = append(dupGroups, full)
	}

	if dupGroups == nil {
		dupGroups = [][]models.Recipe{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": dupGroups})
}

// recipeSimilarity returns a score in [0,1] combining title and description
// similarity. Title is weighted more heavily (80 %).
func recipeSimilarity(a, b database.RecipeDedup) float64 {
	t1 := dedupNormalize(a.Title)
	t2 := dedupNormalize(b.Title)
	d1 := dedupNormalize(a.Description)
	d2 := dedupNormalize(b.Description)

	titleScore := math.Max(jaccardWords(t1, t2), jaccardBigrams(t1, t2))
	descScore := jaccardWords(d1, d2)
	return 0.80*titleScore + 0.20*descScore
}

// dedupNormalize lowercases s, replaces non-alphanumeric runes with spaces,
// and collapses runs of spaces.
func dedupNormalize(s string) string {
	var b strings.Builder
	for _, ch := range strings.ToLower(s) {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			b.WriteRune(ch)
		} else {
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// jaccardWords returns the Jaccard similarity of the word sets of a and b,
// ignoring tokens shorter than 3 characters (articles, prepositions, etc.).
func jaccardWords(a, b string) float64 {
	sa := wordSet(a)
	sb := wordSet(b)
	if len(sa) == 0 && len(sb) == 0 {
		return 1.0
	}
	intersection := 0
	for w := range sa {
		if _, ok := sb[w]; ok {
			intersection++
		}
	}
	union := len(sa) + len(sb) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func wordSet(s string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, w := range strings.Fields(s) {
		if len(w) >= 3 {
			set[w] = struct{}{}
		}
	}
	return set
}

// jaccardBigrams returns the Jaccard similarity of character-bigram sets,
// which handles minor spelling variations and word-order differences.
func jaccardBigrams(a, b string) float64 {
	sa := bigramSet(a)
	sb := bigramSet(b)
	if len(sa) == 0 && len(sb) == 0 {
		return 1.0
	}
	intersection := 0
	for k := range sa {
		if _, ok := sb[k]; ok {
			intersection++
		}
	}
	union := len(sa) + len(sb) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func bigramSet(s string) map[string]struct{} {
	set := map[string]struct{}{}
	runes := []rune(s)
	for i := 0; i+1 < len(runes); i++ {
		set[string(runes[i:i+2])] = struct{}{}
	}
	return set
}

// nearDuplicateThreshold is the minimum concept-similarity score at which a
// generated recipe is considered a near-duplicate of an existing one.
// Combines title Jaccard (30%), ingredient-set Jaccard (60%), and a cuisine
// bonus (10%). Empirically: same dish different name ≈ 0.66, clearly distinct
// dishes ≈ 0.20.
const nearDuplicateThreshold = 0.60

// ingredientNameSet returns the set of normalized names for a slice of raw names.
func ingredientNameSet(names []string) map[string]struct{} {
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		if norm := normalizeIngredientName(n); norm != "" {
			set[norm] = struct{}{}
		}
	}
	return set
}

// ingredientJaccard returns the Jaccard similarity of two ingredient name sets.
func ingredientJaccard(a, b []string) float64 {
	sa, sb := ingredientNameSet(a), ingredientNameSet(b)
	if len(sa) == 0 && len(sb) == 0 {
		return 0
	}
	intersection := 0
	for k := range sa {
		if _, ok := sb[k]; ok {
			intersection++
		}
	}
	union := len(sa) + len(sb) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// recipeConceptSimilarity returns a [0,1] score measuring how semantically
// similar a newly generated recipe is to an existing fingerprint.
func recipeConceptSimilarity(r models.Recipe, fp database.RecipeFingerprint) float64 {
	t1 := dedupNormalize(r.Title)
	t2 := dedupNormalize(fp.Title)
	titleScore := math.Max(jaccardWords(t1, t2), jaccardBigrams(t1, t2))

	newNames := make([]string, len(r.Ingredients))
	for i, ing := range r.Ingredients {
		newNames[i] = ing.Name
	}
	ingScore := ingredientJaccard(newNames, fp.Ingredients)

	cuisineBonus := 0.0
	if fp.CuisineType != "" && strings.EqualFold(r.CuisineType, fp.CuisineType) {
		cuisineBonus = 0.10
	}

	score := 0.30*titleScore + 0.60*ingScore + cuisineBonus
	if score > 1.0 {
		return 1.0
	}
	return score
}

// findNearDuplicates returns existing fingerprints that are conceptually similar
// to the given recipe, capped at 3 results to avoid UI noise.
func findNearDuplicates(r models.Recipe, fingerprints []database.RecipeFingerprint) []database.RecipeFingerprint {
	var matches []database.RecipeFingerprint
	for _, fp := range fingerprints {
		if recipeConceptSimilarity(r, fp) >= nearDuplicateThreshold {
			matches = append(matches, fp)
			if len(matches) == 3 {
				break
			}
		}
	}
	return matches
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
