package main

// Requirements: REQ-FOUNDATION-001, REQ-PLATFORM-VALKEY-001, SEC-GUARD-001.

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/config"
)

func TestInitializeFoundationWithMemoryAdapter(t *testing.T) {
	configuration := config.FromEnv()
	configuration.RepositoryDriver = config.RepositoryDriverMemory
	configuration.OrganizationID = "clean-install"
	configuration.OrganizationName = "Clean Install"
	runtime, err := initializeFoundation(context.Background(), configuration)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.close()
	if runtime.organization.ID != "clean-install" || runtime.organization.Name != "Clean Install" {
		t.Fatalf("unexpected organization %#v", runtime.organization)
	}
}

func TestInitializeAttemptLimiterPreservesDisabledModeAndSupportsMemory(t *testing.T) {
	disabled := config.FromEnv()
	disabled.CacheDriver = config.CacheDriverNone
	disabled.CacheURL = ""
	disabled.CacheKeySecret = ""
	limiter, closeCache, err := initializeAttemptLimiter(context.Background(), disabled)
	if err != nil {
		t.Fatal(err)
	}
	if limiter != nil {
		t.Fatal("expected disabled cache mode to preserve Guard's local limiter")
	}
	if err := closeCache(); err != nil {
		t.Fatal(err)
	}

	memory := disabled
	memory.CacheDriver = config.CacheDriverMemory
	limiter, closeCache, err = initializeAttemptLimiter(context.Background(), memory)
	if err != nil {
		t.Fatal(err)
	}
	defer closeCache()
	if limiter == nil {
		t.Fatal("expected memory cache mode to create a cache-backed limiter")
	}
	allowed, err := limiter.Allow(context.Background(), "account|administrator", time.Now())
	if err != nil || !allowed {
		t.Fatalf("expected initialized memory limiter to allow, allowed=%t err=%v", allowed, err)
	}
}

func TestInitializeAttemptLimiterFailsClosedWhenConfiguredValkeyIsUnavailable(t *testing.T) {
	configuration := config.FromEnv()
	configuration.CacheDriver = config.CacheDriverValkey
	configuration.CacheURL = "redis://127.0.0.1:6379/0"
	configuration.CacheKeySecret = "0123456789abcdef0123456789abcdef"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	limiter, closeCache, err := initializeAttemptLimiter(ctx, configuration)
	if err == nil || limiter != nil {
		t.Fatalf("expected configured Valkey startup to fail closed, limiter=%T err=%v", limiter, err)
	}
	if err := closeCache(); err != nil {
		t.Fatal(err)
	}
}

func TestInitializeFoundationWithPostgres(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	configuration := config.FromEnv()
	configuration.RepositoryDriver = config.RepositoryDriverPostgres
	configuration.DatabaseURL = databaseURL
	configuration.OrganizationID = fmt.Sprintf("clean-install-%d", time.Now().UnixNano())
	configuration.OrganizationName = "PostgreSQL Clean Install"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	runtime, err := initializeFoundation(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.close()
	if runtime.organization.ID != configuration.OrganizationID {
		t.Fatalf("unexpected organization %#v", runtime.organization)
	}
}
