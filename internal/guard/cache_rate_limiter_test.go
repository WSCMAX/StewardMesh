package guard

// Requirements: REQ-PLATFORM-VALKEY-001, SEC-GUARD-001.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/cache"
)

func TestCacheAttemptLimiterSharesFailuresAcrossReplicasAndResets(t *testing.T) {
	store := cache.NewDefaultMemoryStore()
	namespace, err := cache.NewNamespace("stewardmesh", "v1", "shared-organization")
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte(strings.Repeat("s", minimumRateKeySecretBytes))
	first, err := NewCacheAttemptLimiter(store, namespace, secret, 2, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewCacheAttemptLimiter(store, namespace, secret, 2, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now()
	if err := first.Failure(ctx, "account|administrator", now); err != nil {
		t.Fatal(err)
	}
	if err := second.Failure(ctx, "account|administrator", now); err != nil {
		t.Fatal(err)
	}
	if allowed, err := first.Allow(ctx, "account|administrator", now); err != nil || allowed {
		t.Fatalf("expected shared failure limit, allowed=%t err=%v", allowed, err)
	}
	if err := second.Reset(ctx, "account|administrator"); err != nil {
		t.Fatal(err)
	}
	if allowed, err := first.Allow(ctx, "account|administrator", now); err != nil || !allowed {
		t.Fatalf("expected shared reset, allowed=%t err=%v", allowed, err)
	}
}

func TestCacheAttemptLimiterIsolatesOrganizations(t *testing.T) {
	store := cache.NewDefaultMemoryStore()
	secret := []byte(strings.Repeat("s", minimumRateKeySecretBytes))
	firstNamespace, err := cache.NewNamespace("stewardmesh", "v1", "organization-one")
	if err != nil {
		t.Fatal(err)
	}
	secondNamespace, err := cache.NewNamespace("stewardmesh", "v1", "organization-two")
	if err != nil {
		t.Fatal(err)
	}
	first, err := NewCacheAttemptLimiter(store, firstNamespace, secret, 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewCacheAttemptLimiter(store, secondNamespace, secret, 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := first.Failure(ctx, "client|127.0.0.1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if allowed, err := first.Allow(ctx, "client|127.0.0.1", time.Now()); err != nil || allowed {
		t.Fatalf("expected first organization to be limited, allowed=%t err=%v", allowed, err)
	}
	if allowed, err := second.Allow(ctx, "client|127.0.0.1", time.Now()); err != nil || !allowed {
		t.Fatalf("expected second organization to remain isolated, allowed=%t err=%v", allowed, err)
	}
}

func TestCacheAttemptLimiterUsesOpaqueKeysAndFixedTTL(t *testing.T) {
	store := &observedCacheStore{getErr: cache.ErrNotFound, incrementValue: 1}
	namespace, err := cache.NewNamespace("stewardmesh", "v1", "opaque-organization")
	if err != nil {
		t.Fatal(err)
	}
	limiter, err := NewCacheAttemptLimiter(
		store,
		namespace,
		[]byte(strings.Repeat("s", minimumRateKeySecretBytes)),
		5,
		7*time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	rawKey := "account|administrator@example.test"
	if allowed, err := limiter.Allow(ctx, rawKey, time.Now()); err != nil || !allowed {
		t.Fatalf("expected cache miss to allow login, allowed=%t err=%v", allowed, err)
	}
	if err := limiter.Failure(ctx, rawKey, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := limiter.Reset(ctx, rawKey); err != nil {
		t.Fatal(err)
	}
	if store.incrementTTL != 7*time.Minute {
		t.Fatalf("expected fixed failure window, got %s", store.incrementTTL)
	}
	for _, key := range store.keys {
		if strings.Contains(key, "administrator") || strings.Contains(key, "example.test") {
			t.Fatalf("expected opaque cache key, got %q", key)
		}
		if !strings.HasPrefix(key, "stewardmesh:v1:org:opaque-organization:guard-login-failures:") {
			t.Fatalf("expected organization-scoped cache key, got %q", key)
		}
	}
}

func TestCacheAttemptLimiterFailsClosedOnCacheErrorsAndInvalidCounters(t *testing.T) {
	unavailable := errors.New("cache unavailable")
	store := &observedCacheStore{
		getErr:       unavailable,
		incrementErr: unavailable,
		deleteErr:    unavailable,
	}
	namespace, err := cache.NewNamespace("stewardmesh", "v1", "outage-organization")
	if err != nil {
		t.Fatal(err)
	}
	limiter, err := NewDefaultCacheAttemptLimiter(
		store,
		namespace,
		[]byte(strings.Repeat("s", minimumRateKeySecretBytes)),
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if allowed, err := limiter.Allow(ctx, "account|administrator", time.Now()); err == nil || allowed {
		t.Fatalf("expected cache read to fail closed, allowed=%t err=%v", allowed, err)
	}
	if err := limiter.Failure(ctx, "account|administrator", time.Now()); err == nil {
		t.Fatal("expected cache increment error")
	}
	if err := limiter.Reset(ctx, "account|administrator"); err == nil {
		t.Fatal("expected cache reset error")
	}
	store.getErr = nil
	store.getValue = []byte("not-a-counter")
	if allowed, err := limiter.Allow(ctx, "account|administrator", time.Now()); err == nil || allowed {
		t.Fatalf("expected invalid counter to fail closed, allowed=%t err=%v", allowed, err)
	}
}

func TestCacheAttemptLimiterValidatesDependenciesAndContext(t *testing.T) {
	namespace, err := cache.NewNamespace("stewardmesh", "v1", "validation-organization")
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte(strings.Repeat("s", minimumRateKeySecretBytes))
	if _, err := NewCacheAttemptLimiter(nil, namespace, secret, 1, time.Minute); err == nil {
		t.Fatal("expected nil store to be rejected")
	}
	if _, err := NewCacheAttemptLimiter(cache.NewNoopStore(), namespace, []byte("short"), 1, time.Minute); err == nil {
		t.Fatal("expected short key secret to be rejected")
	}
	if _, err := NewCacheAttemptLimiter(cache.NewNoopStore(), namespace, secret, 0, time.Minute); err == nil {
		t.Fatal("expected invalid failure threshold to be rejected")
	}
	if _, err := NewCacheAttemptLimiter(cache.NewNoopStore(), cache.Namespace{}, secret, 1, time.Minute); err == nil {
		t.Fatal("expected invalid namespace to be rejected")
	}
	limiter, err := NewCacheAttemptLimiter(cache.NewNoopStore(), namespace, secret, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := limiter.Allow(canceled, "account|administrator", time.Now()); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context, got %v", err)
	}
}

type observedCacheStore struct {
	getValue       []byte
	getErr         error
	incrementValue int64
	incrementErr   error
	deleteErr      error
	incrementTTL   time.Duration
	keys           []string
}

func (s *observedCacheStore) Get(_ context.Context, key string) ([]byte, error) {
	s.keys = append(s.keys, key)
	return append([]byte(nil), s.getValue...), s.getErr
}

func (s *observedCacheStore) Set(context.Context, string, []byte, time.Duration) error {
	return nil
}

func (s *observedCacheStore) Delete(_ context.Context, key string) error {
	s.keys = append(s.keys, key)
	return s.deleteErr
}

func (s *observedCacheStore) Increment(_ context.Context, key string, ttl time.Duration) (int64, error) {
	s.keys = append(s.keys, key)
	s.incrementTTL = ttl
	return s.incrementValue, s.incrementErr
}

func (s *observedCacheStore) Ping(context.Context) error { return nil }
func (s *observedCacheStore) Close() error               { return nil }
