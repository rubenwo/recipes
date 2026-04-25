package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/rubenwo/mise/internal/database"
	"github.com/rubenwo/mise/internal/llm"
	"github.com/rubenwo/mise/internal/models"
)

type ChatHandler struct {
	queries      *database.Queries
	orchestrator *llm.Orchestrator
}

func NewChatHandler(q *database.Queries, o *llm.Orchestrator) *ChatHandler {
	return &ChatHandler{queries: q, orchestrator: o}
}

func (h *ChatHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	messages, err := h.queries.ListChatMessages(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get chat history")
		return
	}

	if messages == nil {
		messages = []models.ChatMessage{}
	}
	writeJSON(w, http.StatusOK, messages)
}

func (h *ChatHandler) SendMessage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Message) == "" {
		writeError(w, http.StatusBadRequest, "message is required")
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

	history, err := h.queries.ListChatMessages(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get chat history")
		return
	}

	llmHistory := make([]llm.Message, len(history))
	for i, m := range history {
		llmHistory[i] = llm.Message{Role: m.Role, Content: m.Content}
	}

	systemPrompt := buildCookingSystemPrompt(recipe)
	response, err := h.orchestrator.CookingChat(r.Context(), systemPrompt, llmHistory, req.Message)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "chat failed: "+err.Error())
		return
	}

	if err := h.queries.CreateChatMessage(r.Context(), id, "user", req.Message); err != nil {
		log.Printf("Failed to save user chat message for recipe %d: %v", id, err)
	}
	if err := h.queries.CreateChatMessage(r.Context(), id, "assistant", response); err != nil {
		log.Printf("Failed to save assistant chat message for recipe %d: %v", id, err)
	}

	writeJSON(w, http.StatusOK, map[string]string{"response": response})
}

func buildCookingSystemPrompt(recipe *models.Recipe) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "You are a cooking assistant. The user is making this recipe:\n\n")
	fmt.Fprintf(&sb, "Title: %s\n", recipe.Title)
	if recipe.CuisineType != "" {
		fmt.Fprintf(&sb, "Cuisine: %s\n", recipe.CuisineType)
	}
	fmt.Fprintf(&sb, "Serves: %d\n", recipe.Servings)
	if recipe.Description != "" {
		fmt.Fprintf(&sb, "%s\n", recipe.Description)
	}
	sb.WriteString("\nIngredients:\n")
	for _, ing := range recipe.Ingredients {
		if ing.Unit == "" {
			fmt.Fprintf(&sb, "- %g %s\n", ing.Amount, ing.Name)
		} else {
			fmt.Fprintf(&sb, "- %g %s %s\n", ing.Amount, ing.Unit, ing.Name)
		}
	}
	sb.WriteString("\nSteps:\n")
	for i, step := range recipe.Instructions {
		fmt.Fprintf(&sb, "%d. %s\n", i+1, step)
	}
	sb.WriteString("\nAnswer concisely and practically. Use web_search only when you need a fact you do not know.")
	return sb.String()
}
