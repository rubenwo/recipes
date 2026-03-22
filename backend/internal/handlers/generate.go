package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/rubenwo/recipes/internal/database"
	"github.com/rubenwo/recipes/internal/llm"
	"github.com/rubenwo/recipes/internal/models"
)

type GenerateHandler struct {
	orchestrator *llm.Orchestrator
	queries      *database.Queries
}

func NewGenerateHandler(o *llm.Orchestrator, q *database.Queries) *GenerateHandler {
	return &GenerateHandler{orchestrator: o, queries: q}
}

func (h *GenerateHandler) Single(w http.ResponseWriter, r *http.Request) {
	var req models.GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	titles, err := h.queries.ListRecipeTitles(r.Context())
	if err != nil {
		log.Printf("Warning: could not fetch existing titles: %v", err)
	}
	cuisineCounts, err := h.queries.ListCuisineCounts(r.Context())
	if err != nil {
		log.Printf("Warning: could not fetch cuisine counts: %v", err)
	}

	prompt := llm.BuildGeneratePrompt(req, titles, cuisineCounts)
	h.streamGeneration(w, r, prompt)
}

func (h *GenerateHandler) Batch(w http.ResponseWriter, r *http.Request) {
	var req models.BatchGenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Count <= 0 {
		req.Count = 3
	}
	if req.Count > 10 {
		req.Count = 10
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	titles, err := h.queries.ListRecipeTitles(r.Context())
	if err != nil {
		log.Printf("Warning: could not fetch existing titles: %v", err)
	}
	cuisineCounts, err := h.queries.ListCuisineCounts(r.Context())
	if err != nil {
		log.Printf("Warning: could not fetch cuisine counts: %v", err)
	}

	for i := 0; i < req.Count; i++ {
		events := make(chan llm.SSEEvent, 10)

		go func() {
			defer close(events)
			prompt := llm.BuildGeneratePrompt(req.GenerateRequest, titles, cuisineCounts)
			prompt += fmt.Sprintf(" (Recipe %d of %d — make it unique from others in this batch)", i+1, req.Count)
			_, messages, err := h.orchestrator.Generate(r.Context(), prompt, events)
			if err != nil {
				events <- llm.SSEEvent{Type: "error", Message: err.Error()}
			}
			h.saveChat(prompt, messages)
		}()

		for event := range events {
			data, _ := json.Marshal(event)
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (h *GenerateHandler) Import(w http.ResponseWriter, r *http.Request) {
	var req models.ImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.RawText == "" {
		writeError(w, http.StatusBadRequest, "raw_text is required")
		return
	}

	titles, err := h.queries.ListRecipeTitles(r.Context())
	if err != nil {
		log.Printf("Warning: could not fetch existing titles: %v", err)
	}

	prompt := llm.BuildImportPrompt(req.RawText, titles)
	h.streamGeneration(w, r, prompt)
}

func (h *GenerateHandler) Refine(w http.ResponseWriter, r *http.Request) {
	var req models.RefineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Feedback == "" {
		writeError(w, http.StatusBadRequest, "feedback is required")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	prompt := llm.BuildRefinePrompt(req.Recipe, req.Feedback)
	events := make(chan llm.SSEEvent, 10)

	go func() {
		defer close(events)
		_, messages, err := h.orchestrator.GenerateRefine(r.Context(), prompt, events)
		if err != nil {
			log.Printf("Refine error: %v", err)
			events <- llm.SSEEvent{Type: "error", Message: err.Error()}
		}
		h.saveChat(prompt, messages)
	}()

	for event := range events {
		data, _ := json.Marshal(event)
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return
		}
		flusher.Flush()
	}
}

func (h *GenerateHandler) streamGeneration(w http.ResponseWriter, r *http.Request, prompt string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	events := make(chan llm.SSEEvent, 10)

	go func() {
		defer close(events)
		_, messages, err := h.orchestrator.Generate(r.Context(), prompt, events)
		if err != nil {
			log.Printf("Generation error: %v", err)
			events <- llm.SSEEvent{Type: "error", Message: err.Error()}
		}
		h.saveChat(prompt, messages)
	}()

	for event := range events {
		data, _ := json.Marshal(event)
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return
		}
		flusher.Flush()
	}
}

func (h *GenerateHandler) saveChat(prompt string, messages []llm.Message) {
	messagesJSON, err := json.Marshal(messages)
	if err != nil {
		log.Printf("Failed to marshal chat messages: %v", err)
		return
	}
	if err := h.queries.CreateGenerationChat(context.Background(), prompt, h.orchestrator.Model(), messagesJSON); err != nil {
		log.Printf("Failed to save generation chat: %v", err)
	}
}
