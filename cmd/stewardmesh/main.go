package main

// Requirements: REQ-FOUNDATION-001, REQ-PLATFORM-VALKEY-001, SEC-GUARD-001.

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/maxlemke/stewardmesh/internal/application"
	"github.com/maxlemke/stewardmesh/internal/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	setupContext, cancelSetup := context.WithTimeout(ctx, 20*time.Second)
	app, err := application.New(setupContext, cfg, application.Options{RunMigrations: true})
	cancelSetup()
	cfg.DatabaseURL = ""
	cfg.CacheURL = ""
	cfg.CacheKeySecret = ""
	cfg.BootstrapToken = ""
	if err != nil {
		logger.Error("initialize application", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := app.Close(); err != nil {
			logger.Error("close application", "error", err)
		}
	}()
	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	go func() {
		logger.Info(
			"StewardMesh server started",
			"addr", cfg.Addr,
			"organization_id", app.Organization().ID,
			"repository_driver", cfg.RepositoryDriver,
			"cache_driver", cfg.CacheDriver,
		)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server stopped", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown", "error", err)
	}
}
