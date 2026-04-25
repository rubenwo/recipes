package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/rubenwo/mise/internal/frontend"
	"github.com/rubenwo/mise/internal/handlers"
)

func NewRouter(h *handlers.RecipeHandler, g *handlers.GenerateHandler, mp *handlers.MealPlanHandler, s *handlers.SettingsHandler, p *handlers.PendingHandler, ch *handlers.ChatHandler, bt *handlers.BackgroundTranslator, inv *handlers.InventoryHandler, corsOrigin, imagesDir string) *chi.Mux {
	r := chi.NewRouter()

	r.Use(LoggingMiddleware)
	r.Use(SecurityHeadersMiddleware)
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
		r.Post("/recipes/library-search", h.LibrarySearchDirect)
		r.Post("/recipes/preview-image", h.PreviewImage)
		r.Get("/recipes/cuisines", h.ListCuisines)
		r.Get("/recipes/suggestions", h.Suggestions)
		r.Get("/recipes/duplicates", h.FindDuplicates)
		r.Get("/recipes/{id}", h.Get)
		r.Patch("/recipes/{id}", h.Update)
		r.Delete("/recipes/{id}", h.Delete)
		r.Post("/recipes/{id}/fetch-image", h.FetchImage)
		r.Get("/recipes/{id}/chat", ch.GetHistory)
		r.Post("/recipes/{id}/chat", ch.SendMessage)

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
		r.Post("/plans/{id}/order/ah", mp.OrderAH)

		r.Get("/pending", p.List)
		r.Get("/pending/events", p.Stream)
		r.Post("/pending/{id}/approve", p.Approve)
		r.Patch("/pending/{id}", p.Update)
		r.Post("/pending/{id}/fetch-image", p.FetchImage)
		r.Delete("/pending/{id}", p.Reject)

		r.Get("/settings/features", s.GetFeatureStatus)
		r.Get("/settings/models", s.ListModels)
		r.Get("/settings/providers", s.ListProviders)
		r.Post("/settings/providers", s.CreateProvider)
		r.Patch("/settings/providers/{id}", s.UpdateProvider)
		r.Delete("/settings/providers/{id}", s.DeleteProvider)
		r.Get("/settings", s.GetSettings)
		r.Patch("/settings", s.UpdateSettings)
		r.Post("/settings/translation/run", bt.RunNow)

		r.Get("/inventory", inv.List)
		r.Post("/inventory", inv.Create)
		r.Post("/inventory/scan", inv.Scan)
		r.Get("/inventory/scans", inv.ListScans)
		r.Delete("/inventory/scans/{id}", inv.DeleteScan)
		r.Patch("/inventory/{id}", inv.Update)
		r.Delete("/inventory/{id}", inv.Delete)
	})

	if imagesDir != "" {
		fs := http.FileServer(http.Dir(imagesDir))
		// Block directory listings — http.FileServer renders an index page
		// when no index.html exists. Single-user app, but safer-by-default.
		r.Handle("/images/*", http.StripPrefix("/images/", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Path == "" || req.URL.Path[len(req.URL.Path)-1] == '/' {
				http.NotFound(w, req)
				return
			}
			fs.ServeHTTP(w, req)
		})))
	}

	r.Handle("/*", frontend.Handler())

	return r
}
