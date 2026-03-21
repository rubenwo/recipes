package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/rubenwo/recipes/internal/database"
	"github.com/rubenwo/recipes/internal/llm"
	"github.com/rubenwo/recipes/internal/models"
	"github.com/rubenwo/recipes/internal/tools"
)

type PendingHandler struct {
	queries       *database.Queries
	imageSearcher *tools.ImageSearcher
	hub           *llm.Hub
}

func NewPendingHandler(q *database.Queries, imageSearcher *tools.ImageSearcher, hub *llm.Hub) *PendingHandler {
	return &PendingHandler{queries: q, imageSearcher: imageSearcher, hub: hub}
}

func (h *PendingHandler) List(w http.ResponseWriter, r *http.Request) {
	recipes, err := h.queries.ListPendingRecipes(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pending recipes")
		return
	}
	if recipes == nil {
		recipes = []models.Recipe{}
	}
	writeJSON(w, http.StatusOK, recipes)
}

func (h *PendingHandler) Approve(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	recipe, err := h.queries.ApprovePendingRecipe(r.Context(), id)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "pending recipe not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to approve recipe")
		return
	}

	writeJSON(w, http.StatusOK, recipe)
}

func (h *PendingHandler) Reject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if err := h.queries.RejectPendingRecipe(r.Context(), id); err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "pending recipe not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to reject recipe")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *PendingHandler) FetchImage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	title, err := h.queries.GetPendingRecipeTitle(r.Context(), id)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "pending recipe not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get pending recipe")
		return
	}

	if h.imageSearcher == nil {
		writeError(w, http.StatusServiceUnavailable, "image search not available")
		return
	}

	filename := fmt.Sprintf("pending-%d", id)
	imageURL, err := h.imageSearcher.SearchAndDownloadRecipeImage(r.Context(), title, filename)
	if err != nil {
		log.Printf("Image search for pending recipe %q failed: %v", title, err)
		writeError(w, http.StatusBadGateway, "could not find an image: "+err.Error())
		return
	}

	if err := h.queries.SetPendingRecipeImage(r.Context(), id, imageURL); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save image url")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"image_url": imageURL})
}

func (h *PendingHandler) Stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := h.hub.Subscribe()
	defer h.hub.Unsubscribe(ch)

	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(ev)
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
