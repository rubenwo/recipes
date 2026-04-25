package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/rubenwo/mise/internal/config"
	"github.com/rubenwo/mise/internal/database"
	"github.com/rubenwo/mise/internal/handlers"
	"github.com/rubenwo/mise/internal/llm"
	"github.com/rubenwo/mise/internal/models"
	"github.com/rubenwo/mise/internal/server"
	"github.com/rubenwo/mise/internal/tools"
	"github.com/rubenwo/mise/internal/translation"
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

	// Apply DB-stored generation timeout if present, otherwise use config default
	genTimeout := cfg.Ollama.GenerationTimeout
	if val, err := queries.GetSetting(ctx, "generation_timeout"); err == nil {
		if secs, err := strconv.Atoi(val); err == nil && secs >= 10 {
			genTimeout = time.Duration(secs) * time.Second
			log.Printf("Using generation timeout from settings: %v", genTimeout)
		}
	}

	// Build client pool from enabled DB providers
	providers, err := queries.ListEnabledOllamaProviders(ctx)
	if err != nil {
		log.Fatalf("Failed to list providers: %v", err)
	}
	provConfigs := make([]llm.ProviderConfig, len(providers))
	for i, p := range providers {
		provConfigs[i] = llm.ProviderConfig{
			Host:         p.Host,
			Model:        p.Model,
			ProviderType: llm.ProviderType(p.ProviderType),
			Timeout:      genTimeout,
			ProviderID:   p.ID,
			Tags:         p.Tags,
		}
	}
	clientPool := llm.NewClientPool(provConfigs)
	log.Printf("Loaded %d Ollama provider(s)", len(providers))

	// Start background health checker (30 second interval).
	// An immediate check fires in a goroutine so the DB health state is populated
	// quickly without delaying startup.
	clientPool.StartHealthChecker(ctx, 30*time.Second, queries)
	log.Println("Health checker started")

	// Ensure models in the background — pulling can take minutes and must not
	// block the HTTP server from accepting requests.
	go func() {
		for _, c := range clientPool.Clients() {
			if err := c.EnsureModel(ctx); err != nil {
				log.Printf("Warning: could not ensure model on %s: %v", c.Model(), err)
			}
		}
	}()

	webSearcher := tools.NewWebSearcher(cfg.Search.Timeout)
	dbSearcher := tools.NewDBSearcher(queries)

	var edamamClient *tools.EdamamClient
	if cfg.Edamam.Enabled() {
		edamamClient = tools.NewEdamamClient(cfg.Edamam.AppID, cfg.Edamam.AppKey, cfg.Search.Timeout)
	}

	executor := tools.NewExecutor(webSearcher, dbSearcher, edamamClient)
	orchestrator := llm.NewOrchestrator(clientPool, executor, cfg.Ollama.MaxToolIterations, cfg.Edamam.Enabled())

	hub := llm.NewHub()

	translator := translation.New(clientPool, queries)

	imageSearcher := tools.NewImageSearcher(cfg.Search.Timeout, cfg.Server.ImagesDir)
	recipeHandler := handlers.NewRecipeHandler(queries, imageSearcher, clientPool)
	generateHandler := handlers.NewGenerateHandler(orchestrator, queries)
	mealPlanHandler := handlers.NewMealPlanHandler(queries, orchestrator, cfg.Search.Timeout, translator)
	settingsHandler := handlers.NewSettingsHandler(queries, clientPool, genTimeout)
	pendingHandler := handlers.NewPendingHandler(queries, imageSearcher, hub)

	bgGenerator := handlers.NewBackgroundGenerator(queries, orchestrator, hub, imageSearcher)
	bgGenerator.Start(ctx)
	log.Println("Background recipe generator started")

	bgTranslator := handlers.NewBackgroundTranslator(queries, translator)
	bgTranslator.Start(ctx)
	log.Println("Background translation job started")

	chatHandler := handlers.NewChatHandler(queries, orchestrator)

	inventoryHandler := handlers.NewInventoryHandler(queries, orchestrator)

	router := server.NewRouter(recipeHandler, generateHandler, mealPlanHandler, settingsHandler, pendingHandler, chatHandler, bgTranslator, inventoryHandler, cfg.Server.CORSOrigin, cfg.Server.ImagesDir)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Server.Port),
		Handler: router,
		// ReadHeaderTimeout / IdleTimeout defend against slowloris-style holds.
		// WriteTimeout is intentionally unset — generation SSE responses can
		// take minutes on slow local LLMs.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
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
