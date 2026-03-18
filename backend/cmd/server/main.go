package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rubenwoldhuis/recipes/internal/config"
	"github.com/rubenwoldhuis/recipes/internal/database"
	"github.com/rubenwoldhuis/recipes/internal/handlers"
	"github.com/rubenwoldhuis/recipes/internal/llm"
	"github.com/rubenwoldhuis/recipes/internal/models"
	"github.com/rubenwoldhuis/recipes/internal/server"
	"github.com/rubenwoldhuis/recipes/internal/tools"
)

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dbPool, err := database.NewPool(ctx, cfg.Database.ConnString())
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer dbPool.Close()

	if err := database.RunMigrations(ctx, dbPool); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	queries := database.NewQueries(dbPool)

	// Seed Ollama provider from config.yaml if none exist in DB
	count, err := queries.CountOllamaProviders(ctx)
	if err != nil {
		log.Fatalf("Failed to count providers: %v", err)
	}
	if count == 0 {
		seed := &models.OllamaProvider{
			Name:    "Default",
			Host:    cfg.Ollama.Host,
			Model:   cfg.Ollama.Model,
			Enabled: true,
		}
		if err := queries.CreateOllamaProvider(ctx, seed); err != nil {
			log.Fatalf("Failed to seed provider: %v", err)
		}
		log.Printf("Seeded default Ollama provider: %s (%s)", seed.Host, seed.Model)
	}

	// Build client pool from enabled DB providers
	providers, err := queries.ListEnabledOllamaProviders(ctx)
	if err != nil {
		log.Fatalf("Failed to list providers: %v", err)
	}
	provConfigs := make([]llm.ProviderConfig, len(providers))
	for i, p := range providers {
		provConfigs[i] = llm.ProviderConfig{
			Host:    p.Host,
			Model:   p.Model,
			Timeout: cfg.Ollama.GenerationTimeout,
		}
	}
	clientPool := llm.NewClientPool(provConfigs)
	log.Printf("Loaded %d Ollama provider(s)", len(providers))

	// Ensure models on all providers
	for _, c := range clientPool.Clients() {
		if err := c.EnsureModel(ctx); err != nil {
			log.Printf("Warning: could not ensure model on %s: %v", c.Model(), err)
		}
	}

	webSearcher := tools.NewWebSearcher(cfg.Search.Timeout)
	dbSearcher := tools.NewDBSearcher(queries)

	var edamamClient *tools.EdamamClient
	if cfg.Edamam.Enabled() {
		edamamClient = tools.NewEdamamClient(cfg.Edamam.AppID, cfg.Edamam.AppKey, cfg.Search.Timeout)
	}

	executor := tools.NewExecutor(webSearcher, dbSearcher, edamamClient)
	orchestrator := llm.NewOrchestrator(clientPool, executor, cfg.Ollama.MaxToolIterations, cfg.Edamam.Enabled())

	recipeHandler := handlers.NewRecipeHandler(queries)
	generateHandler := handlers.NewGenerateHandler(orchestrator, queries)
	mealPlanHandler := handlers.NewMealPlanHandler(queries, orchestrator)
	settingsHandler := handlers.NewSettingsHandler(queries, clientPool, cfg.Ollama.GenerationTimeout)

	router := server.NewRouter(recipeHandler, generateHandler, mealPlanHandler, settingsHandler, cfg.Server.CORSOrigin)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Server.Port),
		Handler: router,
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh

		log.Println("Shutting down...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("Shutdown error: %v", err)
		}
	}()

	log.Printf("Server starting on :%d (%d provider(s))", cfg.Server.Port, len(providers))
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}
