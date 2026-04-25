package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/seu-org/idp-with-ai-backend/internal/api"
	"github.com/seu-org/idp-with-ai-backend/internal/config"
	"github.com/seu-org/idp-with-ai-backend/internal/utils"
)

func main() {
	cfg := config.Load()

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

	router := gin.Default()
	api.RegisterRoutes(router)

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
