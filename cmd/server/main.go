package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	_ "github.com/paulozy/idp-with-ai-backend/docs"
	"github.com/paulozy/idp-with-ai-backend/internal/api"
	"github.com/paulozy/idp-with-ai-backend/internal/config"
	appcrypto "github.com/paulozy/idp-with-ai-backend/internal/crypto"
	githubclient "github.com/paulozy/idp-with-ai-backend/internal/integrations/github"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
	"github.com/paulozy/idp-with-ai-backend/internal/services"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/storage/postgres"
	redisstore "github.com/paulozy/idp-with-ai-backend/internal/storage/redis"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
	"github.com/paulozy/idp-with-ai-backend/internal/workers"
	"gorm.io/gorm/schema"
)

// @title           IDP with AI Backend API
// @version         1.0
// @description     Identity Provider platform with JWT auth and AI code analysis
// @host            localhost:3000
// @BasePath        /api/v1
// @securityDefinitions.apikey BearerAuth
// @in              header
// @name            Authorization
func main() {
	godotenv.Load()

	cfg := config.Load()

	encKeyRaw, err := base64.StdEncoding.DecodeString(cfg.Server.EncryptionKey)
	if err != nil || len(encKeyRaw) != 32 {
		fmt.Fprintln(os.Stderr, "ENCRYPTION_KEY must be a base64-encoded 32-byte value (generate with: openssl rand -base64 32)")
		os.Exit(1)
	}
	cipher, err := appcrypto.New(encKeyRaw, 0x01)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize encryption cipher: %v\n", err)
		os.Exit(1)
	}
	schema.RegisterSerializer("enc", appcrypto.EncryptedSerializer{Cipher: cipher})

	if err := utils.InitLogger(cfg.Log.Level); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer utils.CloseLogger()

	utils.Info(
		"Starting server",
		"port", cfg.Server.Port,
		"env", cfg.Server.Env,
	)

	ginMode := "debug"
	if cfg.Server.Env == "production" {
		ginMode = "release"
	}

	gin.SetMode(ginMode)

	db, err := storage.New(&cfg.Database)
	if err != nil {
		utils.Fatal("Failed to initialize database", "error", err)
	}
	defer db.Close()

	utils.Info("Database initialized successfully")

	rdbClient, err := redisstore.New(&cfg.Redis)
	if err != nil {
		utils.Warn("Redis unavailable, running without cache/queue", "error", err)
		rdbClient = redisstore.NewNoop()
	}
	defer rdbClient.Close()

	cache := redisstore.NewRedisCache(rdbClient)

	var enqueuer jobs.Enqueuer
	if rdbClient.Client() != nil {
		enqueuer = jobs.NewAsynqEnqueuer(&cfg.Redis)

		pgRepo := postgres.NewPostgresRepository(db.GetDB())

		// Recover sync rows that were left in `syncing` because a previous
		// worker process was killed mid-sync. Reset them to `idle` and
		// re-enqueue so metadata gets populated on the next worker run.
		if staleIDs, err := pgRepo.ResetStaleSyncingRepositories(context.Background()); err != nil {
			utils.Warn("startup: reset stale syncing repos failed", "error", err)
		} else if len(staleIDs) > 0 {
			utils.Info("startup: re-enqueuing stale syncing repositories", "count", len(staleIDs))
			for _, id := range staleIDs {
				payload := tasks.SyncRepoPayload{RepositoryID: id}
				if err := enqueuer.Enqueue(context.Background(), tasks.TypeSyncRepo, payload); err != nil {
					utils.Warn("startup: re-enqueue sync failed", "repo_id", id, "error", err)
				}
			}
		}
		ghClient := githubclient.NewClient(cfg.API.GithubToken)
		syncSvc := services.NewSyncService(pgRepo, ghClient, cache, cfg.API.WebhookBaseURL)

		syncWorker := workers.NewSyncWorker(syncSvc)
		webhookProcessor := workers.NewWebhookProcessor(pgRepo, syncSvc, enqueuer)
		embeddingWorker := workers.NewEmbeddingWorker(pgRepo)
		analysisWorker := workers.NewAnalysisWorker(pgRepo)
		dependencyWorker := workers.NewDependencyWorker(pgRepo, nil, ghClient)
		templateWorker := workers.NewTemplateWorker(pgRepo)
		docsWorker := workers.NewDocsWorker(pgRepo)

		worker := jobs.NewWorker(&cfg.Redis)
		worker.Register(tasks.TypeSyncRepo, syncWorker.Handle)
		worker.Register(tasks.TypeProcessWebhook, webhookProcessor.Handle)
		worker.Register(tasks.TypeGenerateEmbeddings, embeddingWorker.Handle)
		worker.Register(tasks.TypeAnalyzeRepo, analysisWorker.Handle)
		worker.Register(tasks.TypeScanDependencies, dependencyWorker.Handle)
		worker.Register(tasks.TypeGenerateTemplate, templateWorker.Handle)
		worker.Register(tasks.TypeGenerateDocs, docsWorker.Handle)

		go func() {
			if err := worker.Run(); err != nil {
				utils.Error("Job worker stopped", "error", err)
			}
		}()
		defer worker.Shutdown()
	} else {
		enqueuer = jobs.NewNoopEnqueuer()
	}

	router := gin.Default()
	api.RegisterRoutes(&api.RegisterRoutesParams{
		DB:       db.GetDB(),
		Config:   cfg,
		Router:   router,
		Cache:    cache,
		Enqueuer: enqueuer,
	})

	srv := &http.Server{
		Addr:         ":" + cfg.Server.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		utils.Info("Server is running", "addr", srv.Addr)

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			utils.Fatal("Server error", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit

	utils.Info("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		utils.Error("Server forced to shutdown", "error", err)
		os.Exit(1)
	}

	utils.Info("Server exiting")
}
