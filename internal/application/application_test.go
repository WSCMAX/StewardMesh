package application

// Requirements: REQ-FOUNDATION-001, REQ-STORAGE-001, REQ-HORIZON-001, REQ-PLATFORM-VALKEY-001, SEC-GUARD-001.
// Feature: lifecycle.planning.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/config"
)

func TestNewBuildsReusableMemoryApplication(t *testing.T) {
	cfg := memoryConfiguration(t)
	app, err := New(context.Background(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Fatal(err)
		}
	})
	if app.Organization().ID != cfg.OrganizationID || app.Organization().Name != cfg.OrganizationName {
		t.Fatalf("unexpected organization %#v", app.Organization())
	}

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected reusable handler health response, got %d: %s", response.Code, response.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["organizationId"] != cfg.OrganizationID {
		t.Fatalf("unexpected health response %#v", body)
	}
}

func TestNewSupportsMemoryCacheAndFailsClosedForUnavailableValkey(t *testing.T) {
	memory := memoryConfiguration(t)
	memory.CacheDriver = config.CacheDriverMemory
	app, err := New(context.Background(), memory, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}

	valkey := memoryConfiguration(t)
	valkey.CacheDriver = config.CacheDriverValkey
	valkey.CacheURL = "redis://127.0.0.1:6379/0"
	valkey.CacheKeySecret = "0123456789abcdef0123456789abcdef"
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if app, err := New(canceled, valkey, Options{}); err == nil || app != nil {
		t.Fatalf("expected configured Valkey startup to fail closed, app=%T err=%v", app, err)
	}
}

func TestInitializeAttemptLimiterPreservesDisabledModeAndSupportsMemory(t *testing.T) {
	disabled := memoryConfiguration(t)
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
	t.Cleanup(func() {
		if err := closeCache(); err != nil {
			t.Fatal(err)
		}
	})
	if limiter == nil {
		t.Fatal("expected memory cache mode to create a cache-backed limiter")
	}
	allowed, err := limiter.Allow(context.Background(), "account|administrator", time.Now())
	if err != nil || !allowed {
		t.Fatalf("expected initialized memory limiter to allow, allowed=%t err=%v", allowed, err)
	}
}

func TestCloseIsIdempotentAndReleasesResourcesInReverseOrder(t *testing.T) {
	var order []string
	cacheError := errors.New("cache close")
	foundationError := errors.New("foundation close")
	app := &Application{
		closeCache: func() error {
			order = append(order, "cache")
			return cacheError
		},
		closeFoundation: func() error {
			order = append(order, "foundation")
			return foundationError
		},
	}
	for range 2 {
		err := app.Close()
		if !errors.Is(err, cacheError) || !errors.Is(err, foundationError) {
			t.Fatalf("expected joined close errors, got %v", err)
		}
	}
	if !reflect.DeepEqual(order, []string{"cache", "foundation"}) {
		t.Fatalf("unexpected close order %v", order)
	}
}

func TestNewRejectsMissingContextAndInvalidConfiguration(t *testing.T) {
	cfg := memoryConfiguration(t)
	if app, err := New(nil, cfg, Options{}); err == nil || app != nil {
		t.Fatalf("expected nil context to fail, app=%T err=%v", app, err)
	}
	cfg.BlobDir = ""
	if app, err := New(context.Background(), cfg, Options{}); err == nil || app != nil {
		t.Fatalf("expected invalid blob configuration to fail, app=%T err=%v", app, err)
	}
}

func TestNewBuildsPostgresApplicationWithExplicitMigrations(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	cfg := memoryConfiguration(t)
	cfg.RepositoryDriver = config.RepositoryDriverPostgres
	cfg.DatabaseURL = databaseURL
	cfg.OrganizationID = fmt.Sprintf("application-construction-%d", time.Now().UnixNano())
	cfg.OrganizationName = "Application Construction"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	app, err := New(ctx, cfg, Options{RunMigrations: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Fatal(err)
		}
	})
	if app.Organization().ID != cfg.OrganizationID {
		t.Fatalf("unexpected organization %#v", app.Organization())
	}
}

func memoryConfiguration(t *testing.T) config.Config {
	t.Helper()
	cfg := config.FromEnv()
	cfg.Addr = "127.0.0.1:8080"
	cfg.RepositoryDriver = config.RepositoryDriverMemory
	cfg.DatabaseURL = ""
	cfg.CacheDriver = config.CacheDriverNone
	cfg.CacheURL = ""
	cfg.CacheKeySecret = ""
	cfg.OIDCIssuerURL = ""
	cfg.OIDCClientID = ""
	cfg.OIDCClientSecret = ""
	cfg.OIDCRedirectURL = ""
	cfg.OIDCTransactionSecret = ""
	cfg.OIDCAdministratorClaim = ""
	cfg.OIDCAdministratorValues = nil
	cfg.BlobDir = t.TempDir()
	cfg.AllowedOrigin = "http://localhost:5173"
	cfg.SessionCookieSecure = false
	cfg.BootstrapToken = ""
	cfg.SessionTTL = time.Hour
	cfg.OrganizationID = "application-test"
	cfg.OrganizationName = "Application Test"
	return cfg
}
