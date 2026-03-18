package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/rubenwoldhuis/recipes/internal/models"
)

type ToolExecutor interface {
	Execute(ctx context.Context, name string, args json.RawMessage) (string, error)
}

type SSEEvent struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
	Tool    string `json:"tool,omitempty"`
	Args    any    `json:"args,omitempty"`
	Data    any    `json:"data,omitempty"`
}

type Orchestrator struct {
	pool          *ClientPool
	tools         []Tool
	toolExecutor  ToolExecutor
	maxIterations int
}

func NewOrchestrator(pool *ClientPool, toolExecutor ToolExecutor, maxIterations int, edamamEnabled bool) *Orchestrator {
	tools := []Tool{WebSearchTool, DBSearchTool}
	if edamamEnabled {
		tools = append(tools, EdamamSearchTool)
	}

	return &Orchestrator{
		pool:          pool,
		tools:         tools,
		toolExecutor:  toolExecutor,
		maxIterations: maxIterations,
	}
}

func (o *Orchestrator) Model() string {
	return o.pool.Model()
}

func (o *Orchestrator) Pool() *ClientPool {
	return o.pool
}

func (o *Orchestrator) Generate(ctx context.Context, userPrompt string, events chan<- SSEEvent) (*models.Recipe, []Message, error) {
	client := o.pool.Acquire()
	if client == nil {
		return nil, nil, fmt.Errorf("no Ollama providers available")
	}

	events <- SSEEvent{Type: "status", Message: fmt.Sprintf("Starting recipe generation (using %s)...", client.Model())}

	messages := []Message{
		{Role: "system", Content: SystemPrompt()},
		{Role: "user", Content: userPrompt},
	}

	for i := 0; i < o.maxIterations; i++ {
		events <- SSEEvent{Type: "status", Message: fmt.Sprintf("Thinking... (iteration %d/%d)", i+1, o.maxIterations)}

		resp, err := client.Chat(ctx, messages, o.tools)
		if err != nil {
			return nil, messages, fmt.Errorf("chat request failed: %w", err)
		}

		messages = append(messages, resp.Message)

		if len(resp.Message.ToolCalls) == 0 {
			recipe, err := o.parseRecipe(resp.Message.Content, client, events)
			return recipe, messages, err
		}

		for _, tc := range resp.Message.ToolCalls {
			events <- SSEEvent{
				Type: "tool_call",
				Tool: tc.Function.Name,
				Args: json.RawMessage(tc.Function.Arguments),
			}

			result, err := o.toolExecutor.Execute(ctx, tc.Function.Name, tc.Function.Arguments)
			if err != nil {
				log.Printf("Tool %s failed: %v", tc.Function.Name, err)
				result = fmt.Sprintf("Error: %v", err)
			}

			events <- SSEEvent{
				Type: "tool_result",
				Tool: tc.Function.Name,
				Data: result,
			}

			messages = append(messages, Message{
				Role:    "tool",
				Content: result,
			})
		}
	}

	events <- SSEEvent{Type: "status", Message: "Max iterations reached, generating final recipe..."}
	resp, err := client.Chat(ctx, messages, nil)
	if err != nil {
		return nil, messages, fmt.Errorf("final chat request failed: %w", err)
	}
	messages = append(messages, resp.Message)
	recipe, err := o.parseRecipe(resp.Message.Content, client, events)
	return recipe, messages, err
}

func (o *Orchestrator) parseRecipe(content string, client *Client, events chan<- SSEEvent) (*models.Recipe, error) {
	content = strings.TrimSpace(content)

	if idx := strings.Index(content, "{"); idx >= 0 {
		if endIdx := strings.LastIndex(content, "}"); endIdx >= idx {
			content = content[idx : endIdx+1]
		}
	}

	var recipe models.Recipe
	if err := json.Unmarshal([]byte(content), &recipe); err != nil {
		return nil, fmt.Errorf("failed to parse recipe JSON: %w\nraw content: %s", err, content)
	}

	recipe.GeneratedByModel = client.Model()

	events <- SSEEvent{Type: "recipe", Data: recipe}
	return &recipe, nil
}
