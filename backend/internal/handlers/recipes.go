package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/rubenwo/recipes/internal/database"
	"github.com/rubenwo/recipes/internal/models"
	"github.com/rubenwo/recipes/internal/tools"
)

type RecipeHandler struct {
	queries       *database.Queries
	imageSearcher *tools.ImageSearcher
}

func NewRecipeHandler(q *database.Queries, imageSearcher *tools.ImageSearcher) *RecipeHandler {
	return &RecipeHandler{queries: q, imageSearcher: imageSearcher}
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

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
