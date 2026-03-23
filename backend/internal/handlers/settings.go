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

// GetFeatureStatus returns the availability of each AI feature tag.
// Status values: "available", "offline", "unconfigured".
func (h *SettingsHandler) GetFeatureStatus(w http.ResponseWriter, r *http.Request) {
	tags := []string{"generation", "background-generation", "chat", "search", "translation", "inventory", "deduplication"}
	status := make(map[string]string, len(tags))
	for _, tag := range tags {
		status[tag] = h.pool.FeatureStatus(tag)
	}
	writeJSON(w, http.StatusOK, status)
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
	// General settings
	"suggestion_count":    {},
	"default_servings":    {},
	"max_tool_iterations": {},
	"ui_language":         {},
	// Background generation settings
	"background_generation_enabled":     {},
	"background_generation_days":        {},
	"background_generation_time":        {},
	"background_generation_count":       {},
	"background_generation_max_retries": {},
	// Legacy key — no longer used by the scheduler but kept so existing DB rows
	// are not rejected if they are re-submitted by an old client.
	"background_generation_interval": {},
	// Background translation settings
	"background_translation_enabled": {},
	"background_translation_days":    {},
	"background_translation_time":    {},
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
	providerType := r.URL.Query().Get("type")
	if providerType == "" {
		providerType = "ollama"
	}

	httpClient := &http.Client{Timeout: 10 * time.Second}

	var names []string
	var fetchErr error

	switch llm.ProviderType(providerType) {
	case llm.ProviderTypeOpenAICompat:
		names, fetchErr = listModelsOpenAICompat(httpClient, host)
	default:
		names, fetchErr = listModelsOllama(httpClient, host)
	}

	if fetchErr != nil {
		log.Printf("ListModels: %v", fetchErr)
		writeError(w, http.StatusBadGateway, fetchErr.Error())
		return
	}

	writeJSON(w, http.StatusOK, names)
}

func listModelsOllama(client *http.Client, host string) ([]string, error) {
	resp, err := client.Get(host + "/api/tags")
	if err != nil {
		return nil, fmt.Errorf("failed to reach provider: connection error")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("provider returned unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, fmt.Errorf("failed to decode response")
	}

	names := make([]string, len(tags.Models))
	for i, m := range tags.Models {
		names[i] = m.Name
	}
	return names, nil
}

func listModelsOpenAICompat(client *http.Client, host string) ([]string, error) {
	resp, err := client.Get(host + "/v1/models")
	if err != nil {
		return nil, fmt.Errorf("failed to reach provider: connection error")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("provider returned unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response")
	}

	names := make([]string, len(result.Data))
	for i, m := range result.Data {
		names[i] = m.ID
	}
	return names, nil
}

func (h *SettingsHandler) reloadPool(r *http.Request) {
	providers, err := h.queries.ListEnabledOllamaProviders(r.Context())
	if err != nil {
		return
	}
	configs := make([]llm.ProviderConfig, len(providers))
	for i, p := range providers {
		configs[i] = llm.ProviderConfig{
			Host:         p.Host,
			Model:        p.Model,
			ProviderType: llm.ProviderType(p.ProviderType),
			Timeout:      h.timeout,
			ProviderID:   p.ID,
			Tags:         p.Tags,
		}
	}
	h.pool.Reload(configs)
}
