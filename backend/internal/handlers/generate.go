package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/rubenwo/mise/internal/database"
	"github.com/rubenwo/mise/internal/llm"
	"github.com/rubenwo/mise/internal/models"
)

type GenerateHandler struct {
	orchestrator *llm.Orchestrator
	queries      *database.Queries
}

func NewGenerateHandler(o *llm.Orchestrator, q *database.Queries) *GenerateHandler {
	return &GenerateHandler{orchestrator: o, queries: q}
}

// commonIngredients are very generic ingredients that appear in almost every
// recipe and add no identifying value to a prompt fingerprint string.
var commonIngredients = map[string]bool{
	"salt": true, "pepper": true, "oil": true, "water": true,
	"butter": true, "sugar": true, "flour": true,
	"olive oil": true, "vegetable oil": true, "black pepper": true,
	"cooking oil": true, "salt and pepper": true,
}

// fingerprintString formats one RecipeFingerprint as a compact string for the
// generation prompt: "Title (Cuisine: key1, key2, key3)".
func fingerprintString(fp database.RecipeFingerprint) string {
	var key []string
	for _, name := range fp.Ingredients {
		norm := strings.ToLower(strings.TrimSpace(name))
		if !commonIngredients[norm] {
			key = append(key, norm)
		}
		if len(key) == 3 {
			break
		}
	}
	switch {
	case fp.CuisineType != "" && len(key) > 0:
		return fmt.Sprintf("%s (%s: %s)", fp.Title, fp.CuisineType, strings.Join(key, ", "))
	case fp.CuisineType != "":
		return fmt.Sprintf("%s (%s)", fp.Title, fp.CuisineType)
	case len(key) > 0:
		return fmt.Sprintf("%s (%s)", fp.Title, strings.Join(key, ", "))
	default:
		return fp.Title
	}
}

// emitNearDuplicateWarnings checks recipe against existing fingerprints and
// writes any near_duplicate SSE events to w before the recipe event is written.
func emitNearDuplicateWarnings(w http.ResponseWriter, flusher http.Flusher, recipe models.Recipe, idx int, fingerprints []database.RecipeFingerprint) {
	for _, dup := range findNearDuplicates(recipe, fingerprints) {
		event := llm.SSEEvent{
			Type:    "near_duplicate",
			Index:   idx,
			Message: fmt.Sprintf("Similar to existing recipe: %q", dup.Title),
			Data:    map[string]any{"id": dup.ID, "title": dup.Title},
		}
		data, _ := json.Marshal(event)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
}

func (h *GenerateHandler) Single(w http.ResponseWriter, r *http.Request) {
	var req models.GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	fps, err := h.queries.ListRecipeFingerprints(r.Context())
	if err != nil {
		log.Printf("Warning: could not fetch recipe fingerprints: %v", err)
	}
	cuisineCounts, err := h.queries.ListCuisineCounts(r.Context())
	if err != nil {
		log.Printf("Warning: could not fetch cuisine counts: %v", err)
	}

	formatted := make([]string, len(fps))
	for i, fp := range fps {
		formatted[i] = fingerprintString(fp)
	}
	prompt := llm.BuildGeneratePrompt(req, formatted, cuisineCounts)
	h.streamGeneration(w, r, prompt, fps)
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

	fps, err := h.queries.ListRecipeFingerprints(r.Context())
	if err != nil {
		log.Printf("Warning: could not fetch recipe fingerprints: %v", err)
	}
	cuisineCounts, err := h.queries.ListCuisineCounts(r.Context())
	if err != nil {
		log.Printf("Warning: could not fetch cuisine counts: %v", err)
	}

	formatted := make([]string, len(fps))
	for i, fp := range fps {
		formatted[i] = fingerprintString(fp)
	}

	// Pre-compute a distinct cuisine for each recipe slot so that concurrent
	// goroutines don't all pick the same least-represented cuisine.
	tempCounts := llm.SeedCuisineCounts(cuisineCounts)
	prompts := make([]string, req.Count)
	for i := 0; i < req.Count; i++ {
		slotReq := req.GenerateRequest
		if slotReq.CuisineType == "" {
			slotReq.CuisineType = llm.PickLeastRepresentedCuisine(tempCounts)
			if slotReq.CuisineType != "" {
				tempCounts[slotReq.CuisineType]++
			}
		}
		prompts[i] = llm.BuildGeneratePrompt(slotReq, formatted, cuisineCounts)
		prompts[i] += fmt.Sprintf(" (Recipe %d of %d — make it unique from others in this batch)", i+1, req.Count)
	}

	type batchResult struct {
		prompt   string
		recipe   *models.Recipe
		messages []llm.Message
	}

	// Fan-in channel: all goroutines forward their events here.
	allEvents := make(chan llm.SSEEvent, req.Count*10)
	results := make(chan batchResult, req.Count)

	var wg sync.WaitGroup
	for i, p := range prompts {
		wg.Add(1)
		go func(idx int, prompt string) {
			defer wg.Done()
			events := make(chan llm.SSEEvent, 10)

			// Forward this goroutine's events into the shared fan-in channel,
			// tagging each with the slot index so the client can color-code them.
			var fwdWg sync.WaitGroup
			fwdWg.Add(1)
			go func() {
				defer fwdWg.Done()
				for e := range events {
					e.Index = idx
					allEvents <- e
				}
			}()

			recipe, messages, genErr := h.orchestrator.GenerateWithTag(r.Context(), prompt, events, "generation")
			close(events)
			fwdWg.Wait() // ensure all events are forwarded before signalling done

			if genErr != nil {
				allEvents <- llm.SSEEvent{Type: "error", Message: genErr.Error(), Index: idx}
			}
			results <- batchResult{prompt, recipe, messages}
		}(i, p)
	}

	// Close allEvents once every goroutine is done.
	go func() {
		wg.Wait()
		close(allEvents)
	}()

	// Stream all events to the client. Intercept recipe events to emit
	// near_duplicate warnings before forwarding the recipe itself.
	for event := range allEvents {
		if event.Type == "recipe" {
			if recipe, ok := event.Data.(models.Recipe); ok {
				emitNearDuplicateWarnings(w, flusher, recipe, event.Index, fps)
			}
		}
		data, _ := json.Marshal(event)
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return
		}
		flusher.Flush()
	}

	// All goroutines finished; drain results and persist chats.
	for i := 0; i < req.Count; i++ {
		res := <-results
		h.saveChat(res.prompt, res.messages)
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

	fps, err := h.queries.ListRecipeFingerprints(r.Context())
	if err != nil {
		log.Printf("Warning: could not fetch recipe fingerprints: %v", err)
	}
	formatted := make([]string, len(fps))
	for i, fp := range fps {
		formatted[i] = fingerprintString(fp)
	}

	prompt := llm.BuildImportPrompt(req.RawText, formatted)
	h.streamGeneration(w, r, prompt, fps)
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

func (h *GenerateHandler) streamGeneration(w http.ResponseWriter, r *http.Request, prompt string, fingerprints []database.RecipeFingerprint) {
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
		_, messages, err := h.orchestrator.GenerateWithTag(r.Context(), prompt, events, "generation")
		if err != nil {
			log.Printf("Generation error: %v", err)
			events <- llm.SSEEvent{Type: "error", Message: err.Error()}
		}
		h.saveChat(prompt, messages)
	}()

	for event := range events {
		// Emit near_duplicate warnings before the recipe event so the
		// frontend can attach them to the recipe before rendering.
		if event.Type == "recipe" {
			if recipe, ok := event.Data.(models.Recipe); ok {
				emitNearDuplicateWarnings(w, flusher, recipe, event.Index, fingerprints)
			}
		}
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
