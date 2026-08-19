package main

// Command campus-seed initializes the Riverside Community College demo dataset.

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/application"
	"github.com/maxlemke/stewardmesh/internal/config"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if strings.TrimSpace(os.Getenv("STEWARDMESH_SEED_CAMPUS")) == "" {
		_ = os.Setenv("STEWARDMESH_SEED_CAMPUS", "true")
	}
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(cfg.OrganizationID)), "demo-") {
		logger.Error("campus seed requires a demo-* organization", "organization_id", cfg.OrganizationID, "hint", "export STEWARDMESH_ORGANIZATION_ID=demo-campus from .env")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	app, err := application.New(ctx, cfg, application.Options{RunMigrations: true, RunCampusSeed: true})
	if err != nil {
		logger.Error("initialize application", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := app.Close(); err != nil {
			logger.Error("close application", "error", err)
		}
	}()
	logger.Info("Campus demo dataset initialized",
		"organization_id", app.Organization().ID,
		"credentials", "tmp/campus-test-users.env",
	)
}
