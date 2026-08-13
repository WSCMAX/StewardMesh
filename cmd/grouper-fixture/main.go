// StewardMesh Grouper-compatible development fixture.
// Requirement: REQ-DIRECTORY-EXPANSION-005. Feature: integrations.protocols.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/maxlemke/stewardmesh/internal/grouperfixture"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:8080/healthz", nil)
		response, err := (&http.Client{Timeout: 2 * time.Second}).Do(request)
		if err != nil || response.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		response.Body.Close()
		return
	}
	server, err := grouperfixture.New(os.Getenv("GROUPER_FIXTURE_TOKEN"))
	if err != nil {
		slog.Error("invalid fixture configuration", "error", err)
		os.Exit(1)
	}
	httpServer := &http.Server{
		Addr:              "0.0.0.0:8080",
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	slog.Info("Grouper fixture listening", "address", httpServer.Addr)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("Grouper fixture failed", "error", err)
		os.Exit(1)
	}
}
