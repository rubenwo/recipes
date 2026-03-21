package tools

import (
	"context"

	"github.com/rubenwo/recipes/internal/database"
	"github.com/rubenwo/recipes/internal/models"
)

type DBSearcher struct {
	queries *database.Queries
}

func NewDBSearcher(queries *database.Queries) *DBSearcher {
	return &DBSearcher{queries: queries}
}

type DBSearchResult struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	CuisineType string `json:"cuisine_type"`
}

func (d *DBSearcher) Search(ctx context.Context, query string) ([]DBSearchResult, error) {
	recipes, _, err := d.queries.SearchRecipes(ctx, models.SearchRequest{
		Query: query,
		Limit: 5,
	})
	if err != nil {
		return nil, err
	}

	results := make([]DBSearchResult, len(recipes))
	for i, r := range recipes {
		results[i] = DBSearchResult{
			ID:          r.ID,
			Title:       r.Title,
			Description: r.Description,
			CuisineType: r.CuisineType,
		}
	}

	return results, nil
}
