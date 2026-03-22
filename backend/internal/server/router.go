package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/rubenwo/recipes/internal/frontend"
	"github.com/rubenwo/recipes/internal/handlers"
)

func NewRouter(h *handlers.RecipeHandler, g *handlers.GenerateHandler, mp *handlers.MealPlanHandler, s *handlers.SettingsHandler, p *handlers.PendingHandler, corsOrigin, imagesDir string) *chi.Mux {
	r := chi.NewRouter()

	r.Use(LoggingMiddleware)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{corsOrigin},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Content-Type"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Route("/api", func(r chi.Router) {
		r.Get("/recipes", h.List)
		r.Post("/recipes", h.Create)
		r.Post("/recipes/search", h.Search)
		r.Post("/recipes/ai-search", h.AISearch)
		r.Get("/recipes/suggestions", h.Suggestions)
		r.Get("/recipes/{id}", h.Get)
		r.Patch("/recipes/{id}", h.Update)
		r.Delete("/recipes/{id}", h.Delete)
		r.Post("/recipes/{id}/fetch-image", h.FetchImage)

		r.Post("/generate/single", g.Single)
		r.Post("/generate/batch", g.Batch)
		r.Post("/generate/refine", g.Refine)
		r.Post("/generate/import", g.Import)

		r.Post("/plans", mp.Create)
		r.Get("/plans", mp.List)
		r.Get("/plans/{id}", mp.Get)
		r.Patch("/plans/{id}", mp.Update)
		r.Delete("/plans/{id}", mp.Delete)
		r.Get("/plans/{id}/ingredients", mp.Ingredients)
		r.Post("/plans/{id}/suggestions", mp.Suggestions)
		r.Post("/plans/{id}/randomize", mp.Randomize)
		r.Post("/plans/{id}/recipes", mp.AddRecipe)
		r.Delete("/plans/{id}/recipes/{recipeId}", mp.RemoveRecipe)
		r.Patch("/plans/{id}/recipes/{recipeId}", mp.UpdateRecipe)

		r.Get("/pending", p.List)
		r.Get("/pending/events", p.Stream)
		r.Post("/pending/{id}/approve", p.Approve)
		r.Patch("/pending/{id}", p.Update)
		r.Post("/pending/{id}/fetch-image", p.FetchImage)
		r.Delete("/pending/{id}", p.Reject)

		r.Get("/settings/models", s.ListModels)
		r.Get("/settings/providers", s.ListProviders)
		r.Post("/settings/providers", s.CreateProvider)
		r.Patch("/settings/providers/{id}", s.UpdateProvider)
		r.Delete("/settings/providers/{id}", s.DeleteProvider)
		r.Get("/settings", s.GetSettings)
		r.Patch("/settings", s.UpdateSettings)
	})

	if imagesDir != "" {
		r.Handle("/images/*", http.StripPrefix("/images/", http.FileServer(http.Dir(imagesDir))))
	}

	r.Handle("/*", frontend.Handler())

	return r
}
