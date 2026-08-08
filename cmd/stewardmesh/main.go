package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/maxlemke/stewardmesh/internal/bootstrap"
	"github.com/maxlemke/stewardmesh/internal/config"
	"github.com/maxlemke/stewardmesh/internal/httpapi"
	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/storage"
)

func main() {
	cfg := config.FromEnv()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	organization, err := bootstrap.NewOrganization(cfg.OrganizationID, "StewardMesh Local Organization")
	if err != nil {
		logger.Error("initialize organization", "error", err)
		os.Exit(1)
	}

	assets := repository.NewMemoryAssetRepository()
	catalog := repository.NewMemoryCatalog()
	blobStore, err := storage.NewLocalBlobStore(cfg.BlobDir, 25<<20)
	if err != nil {
		logger.Error("initialize blob store", "error", err)
		os.Exit(1)
	}

	handler := httpapi.NewServer(httpapi.Dependencies{
		Assets:      assets,
		Departments: catalog,
		Users:       catalog,
		Tags:        catalog,
		Goals:       catalog,
		Blobs:       blobStore,
	}, cfg.AllowedOrigin, organization)
	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("StewardMesh server started", "addr", cfg.Addr)
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
