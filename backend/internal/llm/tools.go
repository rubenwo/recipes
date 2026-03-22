package llm

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  ToolParameters `json:"parameters"`
}

type ToolParameters struct {
	Type       string                 `json:"type"`
	Required   []string               `json:"required"`
	Properties map[string]ToolProperty `json:"properties"`
}

type ToolProperty struct {
	Type        string     `json:"type"`
	Description string     `json:"description"`
	Items       *ToolItems `json:"items,omitempty"`
}

type ToolItems struct {
	Type string `json:"type"`
}

var WebSearchTool = Tool{
	Type: "function",
	Function: ToolFunction{
		Name:        "web_search",
		Description: "Search the web for recipe ideas, cooking techniques, ingredient combinations, or cuisine information. Use this to find inspiration and trending recipes.",
		Parameters: ToolParameters{
			Type:     "object",
			Required: []string{"query"},
			Properties: map[string]ToolProperty{
				"query": {
					Type:        "string",
					Description: "The search query for finding recipe information",
				},
			},
		},
	},
}

var DBSearchTool = Tool{
	Type: "function",
	Function: ToolFunction{
		Name:        "db_search",
		Description: "Search the user's saved recipe database for existing recipes. Use this to avoid generating duplicates and to build on the user's preferences.",
		Parameters: ToolParameters{
			Type:     "object",
			Required: []string{"query"},
			Properties: map[string]ToolProperty{
				"query": {
					Type:        "string",
					Description: "Search query to find existing recipes in the database",
				},
			},
		},
	},
}

var LibrarySearchTool = Tool{
	Type: "function",
	Function: ToolFunction{
		Name:        "library_search",
		Description: "Search your saved recipe library. Searches across recipe title, description, ingredients, and cuisine using keyword wildcards. Use this to find existing recipes.",
		Parameters: ToolParameters{
			Type:     "object",
			Required: []string{},
			Properties: map[string]ToolProperty{
				"keywords": {
					Type:        "string",
					Description: "Space-separated words to find in recipe title, description, ingredient names, or cuisine. Example: \"garlic chicken lemon\"",
				},
				"cuisine_type": {
					Type:        "string",
					Description: "Filter by cuisine type. Example: \"Italian\", \"Mexican\", \"Asian\"",
				},
				"dietary_restrictions": {
					Type:        "array",
					Description: "Dietary filters. Options: vegetarian, vegan, gluten-free, dairy-free, low-carb, keto, high-protein",
					Items:       &ToolItems{Type: "string"},
				},
				"tags": {
					Type:        "array",
					Description: "Recipe tags. Options: high-protein, low-carb, omega-3, low-calorie, high-fiber, meal-prep, quick, budget-friendly, one-pot, freezer-friendly",
					Items:       &ToolItems{Type: "string"},
				},
				"max_total_minutes": {
					Type:        "integer",
					Description: "Maximum total cook+prep time in minutes. Omit or use 0 for no limit.",
				},
			},
		},
	},
}

var EdamamSearchTool = Tool{
	Type: "function",
	Function: ToolFunction{
		Name:        "edamam_search",
		Description: "Search the Edamam recipe database for detailed recipe information including ingredients, nutrition, and source URLs. Use this for accurate ingredient lists and nutritional data.",
		Parameters: ToolParameters{
			Type:     "object",
			Required: []string{"query"},
			Properties: map[string]ToolProperty{
				"query": {
					Type:        "string",
					Description: "Recipe search query for the Edamam API",
				},
			},
		},
	},
}
