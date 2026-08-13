package main

// Local stdio MCP adapter. Requirements: REQ-API-001, SEC-MCP-001.
// Feature: integrations.protocols. GitHub: #14.

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/maxlemke/stewardmesh/internal/application"
	"github.com/maxlemke/stewardmesh/internal/config"
)

func main() {
	// Stdout is exclusively the MCP transport. Diagnostics use stderr and never
	// include raw session credentials.
	sessionToken := os.Getenv("STEWARDMESH_MCP_SESSION_TOKEN")
	scopes := os.Getenv("STEWARDMESH_MCP_SCOPES")
	_ = os.Unsetenv("STEWARDMESH_MCP_SESSION_TOKEN")
	if sessionToken == "" || scopes == "" {
		fmt.Fprintln(os.Stderr, "STEWARDMESH_MCP_SESSION_TOKEN and STEWARDMESH_MCP_SCOPES are required")
		os.Exit(1)
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "load configuration failed")
		os.Exit(1)
	}
	if cfg.RepositoryDriver != config.RepositoryDriverPostgres {
		fmt.Fprintln(os.Stderr, "local stdio requires the shared postgres repository")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	setup, cancel := context.WithTimeout(ctx, 20*time.Second)
	app, err := application.New(setup, cfg, application.Options{RunMigrations: false})
	cancel()
	cfg.DatabaseURL = ""
	cfg.BootstrapToken = ""
	cfg.OIDCClientSecret = ""
	cfg.CacheKeySecret = ""
	if err != nil {
		fmt.Fprintln(os.Stderr, "initialize application failed")
		os.Exit(1)
	}
	defer app.Close()
	access, err := app.Bridge().AuthenticateLocalSession(ctx, sessionToken, scopes)
	sessionToken = ""
	if err != nil {
		fmt.Fprintln(os.Stderr, "local MCP authentication failed")
		os.Exit(1)
	}
	if err := app.Bridge().RunStdio(ctx, access, os.Stdin, os.Stdout); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "local MCP transport stopped")
		os.Exit(1)
	}
}
