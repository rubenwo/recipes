package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/rubenwo/mise/internal/database"
	"github.com/rubenwo/mise/internal/llm"
	"github.com/rubenwo/mise/internal/models"
)

type InventoryHandler struct {
	queries      *database.Queries
	orchestrator *llm.Orchestrator
}

func NewInventoryHandler(q *database.Queries, o *llm.Orchestrator) *InventoryHandler {
	return &InventoryHandler{queries: q, orchestrator: o}
}

func (h *InventoryHandler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.queries.ListInventory(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list inventory")
		return
	}
	if items == nil {
		items = []models.InventoryItem{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *InventoryHandler) Create(w http.ResponseWriter, r *http.Request) {
	var item models.InventoryItem
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if item.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := h.queries.CreateInventoryItem(r.Context(), &item); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create inventory item")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *InventoryHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var item models.InventoryItem
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	item.ID = id
	if item.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := h.queries.UpdateInventoryItem(r.Context(), &item); err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update item")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *InventoryHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.queries.DeleteInventoryItem(r.Context(), id); err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete item")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// maxScanImageBytes caps the decoded image fed to the vision model.
// Vision-capable LLMs that fit in 32 GB VRAM choke on multi-MB inputs;
// the upstream Ollama call also gets slower super-linearly with image size.
const maxScanImageBytes = 4 * 1024 * 1024 // 4 MB decoded

func (h *InventoryHandler) Scan(w http.ResponseWriter, r *http.Request) {
	// Accept either multipart form (image file) or JSON with base64
	contentType := r.Header.Get("Content-Type")

	var imageB64 string

	if len(contentType) >= 19 && contentType[:19] == "multipart/form-data" {
		if err := r.ParseMultipartForm(10 << 20); err != nil { // 10 MB limit
			writeError(w, http.StatusBadRequest, "failed to parse form")
			return
		}
		file, _, err := r.FormFile("image")
		if err != nil {
			writeError(w, http.StatusBadRequest, "image file required")
			return
		}
		defer file.Close()

		data, err := io.ReadAll(file)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read image")
			return
		}
		if len(data) > maxScanImageBytes {
			writeError(w, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("image too large (%d bytes); max %d", len(data), maxScanImageBytes))
			return
		}
		imageB64 = base64.StdEncoding.EncodeToString(data)
	} else {
		var body struct {
			Image string `json:"image"` // base64
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Image == "" {
			writeError(w, http.StatusBadRequest, "image (base64) required")
			return
		}
		// Validate decoded length so a JSON client can't bypass the limit.
		decoded, err := base64.StdEncoding.DecodeString(body.Image)
		if err != nil {
			writeError(w, http.StatusBadRequest, "image is not valid base64")
			return
		}
		if len(decoded) > maxScanImageBytes {
			writeError(w, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("image too large (%d bytes); max %d", len(decoded), maxScanImageBytes))
			return
		}
		imageB64 = body.Image
	}

	scans, err := h.orchestrator.ScanIngredient(r.Context(), imageB64)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scan failed: "+err.Error())
		return
	}

	var saved []models.PendingIngredientScan
	for _, s := range scans {
		p := models.PendingIngredientScan{
			Name:      s.Name,
			Amount:    s.Amount,
			Unit:      s.Unit,
			Confident: s.Confident,
		}
		if err := h.queries.CreatePendingIngredientScan(r.Context(), &p); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save scan result")
			return
		}
		saved = append(saved, p)
	}
	if saved == nil {
		saved = []models.PendingIngredientScan{}
	}
	writeJSON(w, http.StatusOK, saved)
}

func (h *InventoryHandler) ListScans(w http.ResponseWriter, r *http.Request) {
	items, err := h.queries.ListPendingIngredientScans(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pending scans")
		return
	}
	if items == nil {
		items = []models.PendingIngredientScan{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *InventoryHandler) DeleteScan(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.queries.DeletePendingIngredientScan(r.Context(), id); err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "scan not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete scan")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
