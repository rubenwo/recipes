package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/rubenwo/mise/internal/database"
	"github.com/rubenwo/mise/internal/llm"
	"github.com/rubenwo/mise/internal/models"
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
		log.Printf("error listing providers: %v", err)
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

// allowedSettingKeys is the exhaustive set of keys that may be written to app_settings.
var allowedSettingKeys = map[string]struct{}{
	"generation_timeout": {},
	"default_model":      {},
	"search_enabled":     {},
	"edamam_app_id":      {},
	"edamam_app_key":     {},
	"ollama_host":        {},
	"ui_language":        {},
}

func (h *SettingsHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var updates map[string]string
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	for key, value := range updates {
		if _, ok := allowedSettingKeys[key]; !ok {
			writeError(w, http.StatusBadRequest, "unknown setting key: "+key)
			return
		}
		if err := h.queries.SetSetting(r.Context(), key, value); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update setting: "+key)
			return
		}
	}

	if val, ok := updates["generation_timeout"]; ok {
		if secs, err := strconv.Atoi(val); err == nil && secs >= 10 {
			h.timeout = time.Duration(secs) * time.Second
			h.reloadPool(r)
		}
	}

	settings, err := h.queries.ListSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list settings")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (h *SettingsHandler) ListModels(w http.ResponseWriter, r *http.Request) {
	host := r.URL.Query().Get("host")
	if host == "" {
		writeError(w, http.StatusBadRequest, "host query parameter is required")
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(host + "/api/tags")
	if err != nil {
		log.Printf("ListModels: failed to reach Ollama at %s: %v", host, err)
		writeError(w, http.StatusBadGateway, "failed to reach Ollama: connection error")
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("ListModels: Ollama returned %d: %s", resp.StatusCode, string(body))
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Ollama returned unexpected status %d", resp.StatusCode))
		return
	}

	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decode Ollama response")
		return
	}

	names := make([]string, len(tags.Models))
	for i, m := range tags.Models {
		names[i] = m.Name
	}

	writeJSON(w, http.StatusOK, names)
}

func (h *SettingsHandler) reloadPool(r *http.Request) {
	providers, err := h.queries.ListEnabledOllamaProviders(r.Context())
	if err != nil {
		return
	}
	configs := make([]llm.ProviderConfig, len(providers))
	for i, p := range providers {
		configs[i] = llm.ProviderConfig{Host: p.Host, Model: p.Model, Timeout: h.timeout, ProviderID: p.ID, Tags: p.Tags}
	}
	h.pool.Reload(configs)
}
