package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/rubenwoldhuis/recipes/internal/database"
	"github.com/rubenwoldhuis/recipes/internal/llm"
	"github.com/rubenwoldhuis/recipes/internal/models"
)

type SettingsHandler struct {
	queries *database.Queries
	pool    *llm.ClientPool
	timeout time.Duration
}

func NewSettingsHandler(q *database.Queries, pool *llm.ClientPool, timeout time.Duration) *SettingsHandler {
	return &SettingsHandler{queries: q, pool: pool, timeout: timeout}
}

func (h *SettingsHandler) ListProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := h.queries.ListOllamaProviders(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list providers")
		return
	}
	writeJSON(w, http.StatusOK, providers)
}

func (h *SettingsHandler) CreateProvider(w http.ResponseWriter, r *http.Request) {
	var p models.OllamaProvider
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if p.Name == "" || p.Host == "" || p.Model == "" {
		writeError(w, http.StatusBadRequest, "name, host, and model are required")
		return
	}

	if err := h.queries.CreateOllamaProvider(r.Context(), &p); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create provider")
		return
	}

	h.reloadPool(r)
	writeJSON(w, http.StatusCreated, p)
}

func (h *SettingsHandler) UpdateProvider(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var p models.OllamaProvider
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	p.ID = id

	if p.Name == "" || p.Host == "" || p.Model == "" {
		writeError(w, http.StatusBadRequest, "name, host, and model are required")
		return
	}

	if err := h.queries.UpdateOllamaProvider(r.Context(), &p); err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "provider not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update provider")
		return
	}

	h.reloadPool(r)
	writeJSON(w, http.StatusOK, p)
}

func (h *SettingsHandler) DeleteProvider(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if err := h.queries.DeleteOllamaProvider(r.Context(), id); err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "provider not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete provider")
		return
	}

	h.reloadPool(r)
	w.WriteHeader(http.StatusNoContent)
}

func (h *SettingsHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.queries.ListSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list settings")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (h *SettingsHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var updates map[string]string
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	for key, value := range updates {
		if err := h.queries.SetSetting(r.Context(), key, value); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update setting: "+key)
			return
		}
	}

	settings, err := h.queries.ListSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list settings")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (h *SettingsHandler) reloadPool(r *http.Request) {
	providers, err := h.queries.ListEnabledOllamaProviders(r.Context())
	if err != nil {
		return
	}
	configs := make([]llm.ProviderConfig, len(providers))
	for i, p := range providers {
		configs[i] = llm.ProviderConfig{Host: p.Host, Model: p.Model, Timeout: h.timeout}
	}
	h.pool.Reload(configs)
}
