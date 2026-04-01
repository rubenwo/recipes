package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/rubenwo/mise/internal/models"
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
	Index   int    `json:"index,omitempty"` // batch slot (0 = first / single)
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

func (o *Orchestrator) GenerateWithTag(ctx context.Context, userPrompt string, events chan<- SSEEvent, tag string) (*models.Recipe, []Message, error) {
	client := o.pool.AcquireWithTag(tag)
	if client == nil {
		return nil, nil, fmt.Errorf("no provider with tag %q configured or healthy", tag)
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
			recipe, err := o.parseRecipe(resp.Message.Content, client)
			if err != nil {
				// Model returned non-JSON (e.g. markdown). Ask it to reformat.
				events <- SSEEvent{Type: "status", Message: "Reformatting response as JSON..."}
				fixMessages := append(messages, Message{
					Role:    "user",
					Content: "Your response was not valid JSON. Please output only the recipe JSON object with no other text.",
				})
				fixResp, fixErr := client.ChatJSON(ctx, fixMessages)
				if fixErr != nil {
					return nil, messages, err // return original parse error
				}
				messages = append(messages, fixResp.Message)
				recipe, err = o.parseRecipe(fixResp.Message.Content, client)
				if err != nil {
					return nil, messages, err
				}
			}
			events <- SSEEvent{Type: "status", Message: "Reviewing recipe..."}
			applyDeterministicReview(recipe)
			events <- SSEEvent{Type: "recipe", Data: *recipe}
			return recipe, messages, nil
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
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    result,
			})
		}
	}

	events <- SSEEvent{Type: "status", Message: "Max iterations reached, generating final recipe..."}
	resp, err := client.ChatJSON(ctx, messages)
	if err != nil {
		return nil, messages, fmt.Errorf("final chat request failed: %w", err)
	}
	messages = append(messages, resp.Message)
	recipe, err := o.parseRecipe(resp.Message.Content, client)
	if err != nil {
		return nil, messages, err
	}
	events <- SSEEvent{Type: "status", Message: "Reviewing recipe..."}
	applyDeterministicReview(recipe)
	events <- SSEEvent{Type: "recipe", Data: *recipe}
	return recipe, messages, nil
}

// GenerateRefine refines an existing recipe based on user feedback.
// It makes a single LLM call with no tools — the model only needs to apply
// the requested changes to the recipe it already has in context.
func (o *Orchestrator) GenerateRefine(ctx context.Context, userPrompt string, events chan<- SSEEvent) (*models.Recipe, []Message, error) {
	client := o.pool.AcquireWithTag("generation")
	if client == nil {
		return nil, nil, fmt.Errorf("no provider with tag %q configured or healthy", "generation")
	}

	events <- SSEEvent{Type: "status", Message: fmt.Sprintf("Refining recipe (using %s)...", client.Model())}

	messages := []Message{
		{Role: "system", Content: SystemPrompt()},
		{Role: "user", Content: userPrompt},
	}

	resp, err := client.ChatJSON(ctx, messages)
	if err != nil {
		return nil, messages, fmt.Errorf("chat request failed: %w", err)
	}
	messages = append(messages, resp.Message)

	recipe, err := o.parseRecipe(resp.Message.Content, client)
	if err != nil {
		return nil, messages, err
	}

	events <- SSEEvent{Type: "status", Message: "Reviewing recipe..."}
	applyDeterministicReview(recipe)
	events <- SSEEvent{Type: "recipe", Data: *recipe}
	return recipe, messages, nil
}


// CookingChat handles a single conversational turn for the cooking assistant.
// It uses the full recipe as system context and has access to web search tools.
func (o *Orchestrator) CookingChat(ctx context.Context, systemContext string, history []Message, userMessage string) (string, error) {
	client := o.pool.AcquireWithTag("chat")
	if client == nil {
		return "", fmt.Errorf("no provider with tag %q configured or healthy", "chat")
	}

	messages := make([]Message, 0, 1+len(history)+1)
	messages = append(messages, Message{Role: "system", Content: systemContext})
	messages = append(messages, history...)
	messages = append(messages, Message{Role: "user", Content: userMessage})

	for i := 0; i < o.maxIterations; i++ {
		resp, err := client.Chat(ctx, messages, o.tools)
		if err != nil {
			return "", fmt.Errorf("chat request failed: %w", err)
		}
		messages = append(messages, resp.Message)

		if len(resp.Message.ToolCalls) == 0 {
			return resp.Message.Content, nil
		}

		for _, tc := range resp.Message.ToolCalls {
			result, err := o.toolExecutor.Execute(ctx, tc.Function.Name, tc.Function.Arguments)
			if err != nil {
				log.Printf("Tool %s failed: %v", tc.Function.Name, err)
				result = fmt.Sprintf("Error: %v", err)
			}
			messages = append(messages, Message{Role: "tool", ToolCallID: tc.ID, Content: result})
		}
	}

	// Max iterations reached — final call without tools.
	resp, err := client.Chat(ctx, messages, nil)
	if err != nil {
		return "", fmt.Errorf("final chat request failed: %w", err)
	}
	return resp.Message.Content, nil
}

// ScanIngredient uses a vision-capable model (tagged "inventory") to identify
// all ingredients visible in an image. imageB64 is the raw base64-encoded
// image data (no data-URI prefix).
func (o *Orchestrator) ScanIngredient(ctx context.Context, imageB64 string) ([]models.IngredientScan, error) {
	client := o.pool.AcquireWithTag("inventory")
	if client == nil {
		return nil, fmt.Errorf("no provider with tag %q configured or healthy", "inventory")
	}

	messages := []Message{
		{
			Role:    "user",
			Content: BuildScanIngredientPrompt(),
			Images:  []string{imageB64},
		},
	}

	resp, err := client.ChatJSON(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("vision chat failed: %w", err)
	}

	content := strings.TrimSpace(resp.Message.Content)
	if idx := strings.Index(content, "["); idx >= 0 {
		if endIdx := strings.LastIndex(content, "]"); endIdx >= idx {
			content = content[idx : endIdx+1]
		}
	}

	var scans []models.IngredientScan
	if err := json.Unmarshal([]byte(content), &scans); err != nil {
		return nil, fmt.Errorf("failed to parse scan JSON: %w\nraw: %s", err, content)
	}
	return scans, nil
}

// DeduplicateIngredients asks the LLM to merge groups of ingredients that share a name
// but could not be consolidated deterministically (e.g. unknown cross-unit density).
// Each group becomes exactly one output entry.
func (o *Orchestrator) DeduplicateIngredients(ctx context.Context, groups [][]models.AggregatedIngredient) ([]models.AggregatedIngredient, error) {
	client := o.pool.AcquireWithTag("deduplication")
	if client == nil {
		return nil, fmt.Errorf("no provider with tag %q configured or healthy", "deduplication")
	}

	groupsJSON, err := json.Marshal(groups)
	if err != nil {
		return nil, err
	}

	messages := []Message{
		{Role: "user", Content: BuildDeduplicateIngredientsPrompt(string(groupsJSON))},
	}

	resp, err := client.ChatJSON(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("LLM dedup chat failed: %w", err)
	}

	content := strings.TrimSpace(resp.Message.Content)
	if idx := strings.Index(content, "["); idx >= 0 {
		if endIdx := strings.LastIndex(content, "]"); endIdx >= idx {
			content = content[idx : endIdx+1]
		}
	}
	// LLMs sometimes return a bare object instead of a single-element array.
	if strings.HasPrefix(content, "{") {
		content = "[" + content + "]"
	}

	type llmItem struct {
		Name   string  `json:"name"`
		Amount float64 `json:"amount"`
		Unit   string  `json:"unit"`
	}
	var items []llmItem
	if err := json.Unmarshal([]byte(content), &items); err != nil {
		return nil, fmt.Errorf("failed to parse dedup response: %w\nraw: %s", err, content)
	}

	out := make([]models.AggregatedIngredient, 0, len(items))
	for i, item := range items {
		if i >= len(groups) {
			break
		}
		recipeSet := map[string]bool{}
		for _, ing := range groups[i] {
			for _, r := range ing.Recipes {
				recipeSet[r] = true
			}
		}
		recipes := make([]string, 0, len(recipeSet))
		for r := range recipeSet {
			recipes = append(recipes, r)
		}
		sort.Strings(recipes)

		name := strings.TrimSpace(item.Name)
		if len(name) > 0 {
			name = strings.ToUpper(name[:1]) + name[1:]
		}
		out = append(out, models.AggregatedIngredient{
			Name:    name,
			Amount:  item.Amount,
			Unit:    item.Unit,
			Recipes: recipes,
		})
	}
	return out, nil
}

func (o *Orchestrator) parseRecipe(content string, client *Client) (*models.Recipe, error) {
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
	return &recipe, nil
}
