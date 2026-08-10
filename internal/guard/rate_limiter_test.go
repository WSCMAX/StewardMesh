package guard

// Requirements: REQ-PLATFORM-VALKEY-001, SEC-GUARD-001.

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestMemoryAttemptLimiterExpiresFailuresAndResets(t *testing.T) {
	limiter, err := NewMemoryAttemptLimiter(2, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	ctx := context.Background()
	if allowed, err := limiter.Allow(ctx, "account", now); err != nil || !allowed {
		t.Fatal("expected first attempt to be allowed")
	}
	if err := limiter.Failure(ctx, "account", now); err != nil {
		t.Fatal(err)
	}
	if err := limiter.Failure(ctx, "account", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if allowed, err := limiter.Allow(ctx, "account", now.Add(2*time.Second)); err != nil || allowed {
		t.Fatal("expected attempt to be rate limited")
	}
	if allowed, err := limiter.Allow(ctx, "account", now.Add(2*time.Minute)); err != nil || !allowed {
		t.Fatal("expected expired failures to be removed")
	}
	if err := limiter.Failure(ctx, "account", now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := limiter.Reset(ctx, "account"); err != nil {
		t.Fatal(err)
	}
	if allowed, err := limiter.Allow(ctx, "account", now.Add(2*time.Minute)); err != nil || !allowed {
		t.Fatal("expected reset to remove failures")
	}
}

func TestMemoryAttemptLimiterDoesNotRetainSuccessfulKeys(t *testing.T) {
	limiter, err := NewMemoryAttemptLimiter(2, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	ctx := context.Background()
	for index := range 1_000 {
		allowed, err := limiter.Allow(ctx, fmt.Sprintf("successful-%d", index), now)
		if err != nil || !allowed {
			t.Fatal("expected successful key to be allowed")
		}
	}
	if len(limiter.failures) != 0 {
		t.Fatalf("expected successful keys not to be retained, got %d", len(limiter.failures))
	}
}

func TestMemoryAttemptLimiterFailsClosedAtCapacity(t *testing.T) {
	limiter, err := NewMemoryAttemptLimiter(2, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	ctx := context.Background()
	for index := range maximumTrackedAttemptKeys {
		if err := limiter.Failure(ctx, fmt.Sprintf("failed-%d", index), now); err != nil {
			t.Fatal(err)
		}
	}
	if err := limiter.Failure(ctx, "overflow", now); err != nil {
		t.Fatal(err)
	}
	if allowed, err := limiter.Allow(ctx, "new-key", now); err != nil || allowed {
		t.Fatal("expected saturated limiter to fail closed")
	}
	if allowed, err := limiter.Allow(ctx, "new-key", now.Add(2*time.Minute)); err != nil || !allowed {
		t.Fatal("expected limiter to recover after the failure window")
	}
	if len(limiter.failures) != 0 {
		t.Fatalf("expected expired keys to be removed, got %d", len(limiter.failures))
	}
}

func TestMemoryAttemptLimiterHonorsContextCancellation(t *testing.T) {
	limiter, err := NewMemoryAttemptLimiter(2, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := limiter.Allow(ctx, "account", time.Now()); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled allow, got %v", err)
	}
	if err := limiter.Failure(ctx, "account", time.Now()); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled failure, got %v", err)
	}
	if err := limiter.Reset(ctx, "account"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled reset, got %v", err)
	}
}
