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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/rubenwo/recipes/internal/database"
	"github.com/rubenwo/recipes/internal/llm"
	"github.com/rubenwo/recipes/internal/models"
	"github.com/rubenwo/recipes/internal/tools"
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

	recipes, total, err := h.queries.ListRecipes(r.Context(), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list recipes")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"recipes": recipes,
		"total":   total,
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
	Ingredients []models.Ingredient `json:"ingredients"`
	Instructions []string            `json:"instructions"`
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

	if err := h.queries.UpdateRecipeContent(r.Context(), id, req.Ingredients, req.Instructions); err != nil {
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

const aiSearchSystemPrompt = `You are a recipe search assistant. Convert the user's natural language query into structured search parameters. Return valid JSON only with these fields:
- "query": string - key terms for full-text search (dish name, main ingredient, cooking style). Keep short and relevant.
- "cuisine_type": string - exact cuisine if clearly mentioned (e.g. "Italian", "Mexican", "Asian") or empty string
- "dietary_restrictions": array of strings - dietary labels if mentioned (e.g. "vegetarian", "vegan", "gluten-free", "dairy-free", "low-carb", "keto", "high-protein") or empty array
- "tags": array of strings - any other relevant category tags or empty array
- "max_total_minutes": number - max total cook+prep time in minutes if a time limit is mentioned (e.g. 30 for "under 30 minutes"), or 0 for no limit

Return only the JSON object, no explanation.`

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

	client := h.llmPool.Acquire()
	if client == nil {
		writeError(w, http.StatusServiceUnavailable, "no AI provider available")
		return
	}

	messages := []llm.Message{
		{Role: "system", Content: aiSearchSystemPrompt},
		{Role: "user", Content: req.Query},
	}

	resp, err := client.ChatJSON(r.Context(), messages)
	if err != nil {
		log.Printf("AI search LLM error: %v", err)
		writeError(w, http.StatusInternalServerError, "AI search failed: "+err.Error())
		return
	}

	var parsed struct {
		Query               string   `json:"query"`
		CuisineType         string   `json:"cuisine_type"`
		DietaryRestrictions []string `json:"dietary_restrictions"`
		Tags                []string `json:"tags"`
		MaxTotalMinutes     int      `json:"max_total_minutes"`
	}
	if err := json.Unmarshal([]byte(resp.Message.Content), &parsed); err != nil {
		log.Printf("AI search parse error: %v, content: %s", err, resp.Message.Content)
		writeError(w, http.StatusInternalServerError, "failed to parse AI response")
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}

	searchReq := models.SearchRequest{
		Query:               parsed.Query,
		CuisineType:         parsed.CuisineType,
		DietaryRestrictions: parsed.DietaryRestrictions,
		Tags:                parsed.Tags,
		MaxTotalMinutes:     parsed.MaxTotalMinutes,
		Limit:               limit,
	}

	recipes, total, err := h.queries.SearchRecipes(r.Context(), searchReq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to search recipes")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"recipes":     recipes,
		"total":       total,
		"interpreted": parsed,
	})
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

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
