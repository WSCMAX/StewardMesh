package guard

// Requirement: SEC-GUARD-001.

import (
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
	if !limiter.Allow("account", now) {
		t.Fatal("expected first attempt to be allowed")
	}
	limiter.Failure("account", now)
	limiter.Failure("account", now.Add(time.Second))
	if limiter.Allow("account", now.Add(2*time.Second)) {
		t.Fatal("expected attempt to be rate limited")
	}
	if !limiter.Allow("account", now.Add(2*time.Minute)) {
		t.Fatal("expected expired failures to be removed")
	}
	limiter.Failure("account", now.Add(2*time.Minute))
	limiter.Reset("account")
	if !limiter.Allow("account", now.Add(2*time.Minute)) {
		t.Fatal("expected reset to remove failures")
	}
}

func TestMemoryAttemptLimiterDoesNotRetainSuccessfulKeys(t *testing.T) {
	limiter, err := NewMemoryAttemptLimiter(2, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for index := range 1_000 {
		if !limiter.Allow(fmt.Sprintf("successful-%d", index), now) {
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
	for index := range maximumTrackedAttemptKeys {
		limiter.Failure(fmt.Sprintf("failed-%d", index), now)
	}
	limiter.Failure("overflow", now)
	if limiter.Allow("new-key", now) {
		t.Fatal("expected saturated limiter to fail closed")
	}
	if !limiter.Allow("new-key", now.Add(2*time.Minute)) {
		t.Fatal("expected limiter to recover after the failure window")
	}
	if len(limiter.failures) != 0 {
		t.Fatalf("expected expired keys to be removed, got %d", len(limiter.failures))
	}
}
