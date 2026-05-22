package app

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mealapp-backend/internal/ai"
	"mealapp-backend/internal/config"
	"mealapp-backend/internal/database"
	"mealapp-backend/internal/httpapi"
	"mealapp-backend/internal/mealplanner"
	"mealapp-backend/internal/store"
)

func Run() {
	cfg := config.Load()
	ctx := context.Background()
	log.Printf("using mongo database %q at %s", cfg.MongoDB, safeMongoHost(cfg.MongoURI))

	mongoClient, err := database.Connect(ctx, cfg.MongoURI)
	if err != nil {
		log.Fatalf("connect mongo: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = mongoClient.Disconnect(shutdownCtx)
	}()

	userStore := store.NewUserStore(mongoClient.Database(cfg.MongoDB))
	if err := userStore.EnsureIndexes(ctx); err != nil {
		log.Fatalf("ensure indexes: %v", err)
	}

	mealStore := store.NewMealStore(mongoClient.Database(cfg.MongoDB))
	if err := mealStore.EnsureIndexes(ctx); err != nil {
		log.Fatalf("ensure meal indexes: %v", err)
	}
	if err := mealStore.SeedDefaults(ctx); err != nil {
		log.Fatalf("seed meals: %v", err)
	}

	aiProvider := ai.NewOpenAIProvider(cfg.OpenAIAPIKey, cfg.OpenAIModel)
	if aiProvider == nil {
		log.Printf("ai meal generation disabled: OPENAI_API_KEY is not set")
	} else {
		log.Printf("ai meal generation enabled with model %q", cfg.OpenAIModel)
	}
	api := httpapi.NewServer(cfg, userStore, mealplanner.New(mealStore, aiProvider))
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("mealapp backend listening on %s", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}

func safeMongoHost(uri string) string {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "unknown"
	}
	return parsed.Scheme + "://" + parsed.Host
}
