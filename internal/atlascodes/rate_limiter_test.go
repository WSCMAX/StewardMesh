package atlascodes

// Requirement: REQ-ATLAS-CODES-001. Feature: inventory.identifiers.

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMemoryRequestLimiterBoundsIndependentOpaqueKeysAndExpires(t *testing.T) {
	limiter, err := NewMemoryRequestLimiter(2)
	if err != nil {
		t.Fatal(err)
	}
	limit := RequestRateLimit{Maximum: 2, Window: time.Minute}
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	for index := 0; index < 2; index++ {
		allowed, err := limiter.Allow(context.Background(), "organization:account:resolve", limit, now)
		if err != nil || !allowed {
			t.Fatalf("request %d was not allowed: allowed=%t err=%v", index+1, allowed, err)
		}
	}
	allowed, err := limiter.Allow(context.Background(), "organization:account:resolve", limit, now)
	if err != nil || allowed {
		t.Fatalf("expected bounded key to be denied: allowed=%t err=%v", allowed, err)
	}
	allowed, err = limiter.Allow(context.Background(), "organization:other-account:resolve", limit, now)
	if err != nil || !allowed {
		t.Fatalf("independent key was not allowed: allowed=%t err=%v", allowed, err)
	}
	allowed, err = limiter.Allow(context.Background(), "organization:account:resolve", limit, now.Add(time.Minute))
	if err != nil || !allowed {
		t.Fatalf("expired key was not reset: allowed=%t err=%v", allowed, err)
	}
	for digest := range limiter.entries {
		if string(digest[:]) == "organization:account:resolve" {
			t.Fatal("request limiter retained a plaintext key")
		}
	}
}

func TestMemoryRequestLimiterIsConcurrencySafeAndFailsClosedAtCapacity(t *testing.T) {
	limiter, err := NewMemoryRequestLimiter(1)
	if err != nil {
		t.Fatal(err)
	}
	limit := RequestRateLimit{Maximum: 10, Window: time.Minute}
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	var allowed atomic.Int64
	var wait sync.WaitGroup
	for index := 0; index < 40; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			ok, requestErr := limiter.Allow(context.Background(), "shared-key", limit, now)
			if requestErr != nil {
				t.Errorf("concurrent request failed: %v", requestErr)
				return
			}
			if ok {
				allowed.Add(1)
			}
		}()
	}
	wait.Wait()
	if allowed.Load() != 10 {
		t.Fatalf("expected exactly ten allowed requests, got %d", allowed.Load())
	}
	if _, err := limiter.Allow(context.Background(), "new-key", limit, now); err == nil {
		t.Fatal("expected a new key to fail closed at capacity")
	}
}

func TestMemoryRequestLimiterValidatesPolicyAndContext(t *testing.T) {
	limiter, err := NewMemoryRequestLimiter(1)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	if _, err := limiter.Allow(context.Background(), "key", RequestRateLimit{}, now); err == nil {
		t.Fatal("expected an invalid policy to fail")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := limiter.Allow(cancelled, "key", RequestRateLimit{Maximum: 1, Window: time.Minute}, now); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}
