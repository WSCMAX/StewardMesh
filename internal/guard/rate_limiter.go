package guard

// Requirements: REQ-PLATFORM-VALKEY-001, SEC-GUARD-001.

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

const maximumTrackedAttemptKeys = 10_000

type AttemptLimiter interface {
	Allow(ctx context.Context, key string, now time.Time) (bool, error)
	Failure(ctx context.Context, key string, now time.Time) error
	Reset(ctx context.Context, key string) error
}

type MemoryAttemptLimiter struct {
	mu          sync.Mutex
	key         []byte
	maxFailures int
	window      time.Duration
	failures    map[string][]time.Time
	saturatedTo time.Time
}

func NewMemoryAttemptLimiter(maxFailures int, window time.Duration) (*MemoryAttemptLimiter, error) {
	if maxFailures < 1 || window <= 0 {
		return nil, errors.New("positive rate limit and window are required")
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, errors.New("initialize rate limiter")
	}
	return &MemoryAttemptLimiter{
		key:         key,
		maxFailures: maxFailures,
		window:      window,
		failures:    make(map[string][]time.Time),
	}, nil
}

func (l *MemoryAttemptLimiter) Allow(ctx context.Context, key string, now time.Time) (bool, error) {
	if err := limiterContextError(ctx); err != nil {
		return false, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if now.Before(l.saturatedTo) {
		return false, nil
	}
	if !l.saturatedTo.IsZero() {
		l.pruneExpired(now)
		l.saturatedTo = time.Time{}
	}
	digest := l.digest(key)
	entries := activeFailures(l.failures[digest], now.Add(-l.window))
	if len(entries) == 0 {
		delete(l.failures, digest)
		return true, nil
	}
	l.failures[digest] = entries
	return len(entries) < l.maxFailures, nil
}

func (l *MemoryAttemptLimiter) Failure(ctx context.Context, key string, now time.Time) error {
	if err := limiterContextError(ctx); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if now.Before(l.saturatedTo) {
		return nil
	}
	if !l.saturatedTo.IsZero() {
		l.pruneExpired(now)
		l.saturatedTo = time.Time{}
	}
	digest := l.digest(key)
	entries := activeFailures(l.failures[digest], now.Add(-l.window))
	if len(entries) == 0 {
		delete(l.failures, digest)
		if len(l.failures) >= maximumTrackedAttemptKeys {
			l.pruneExpired(now)
			if len(l.failures) >= maximumTrackedAttemptKeys {
				// Fail closed for one window instead of allowing a key-flood to
				// make brute-force protection consume unbounded memory.
				l.saturatedTo = now.Add(l.window)
				return nil
			}
		}
	}
	l.failures[digest] = append(entries, now)
	return nil
}

func (l *MemoryAttemptLimiter) Reset(ctx context.Context, key string) error {
	if err := limiterContextError(ctx); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	delete(l.failures, l.digest(key))
	return nil
}

func (l *MemoryAttemptLimiter) digest(value string) string {
	mac := hmac.New(sha256.New, l.key)
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

func (l *MemoryAttemptLimiter) pruneExpired(now time.Time) {
	cutoff := now.Add(-l.window)
	for digest, entries := range l.failures {
		active := activeFailures(entries, cutoff)
		if len(active) == 0 {
			delete(l.failures, digest)
			continue
		}
		l.failures[digest] = active
	}
}

func activeFailures(entries []time.Time, cutoff time.Time) []time.Time {
	first := 0
	for first < len(entries) && entries[first].Before(cutoff) {
		first++
	}
	return append([]time.Time(nil), entries[first:]...)
}

func limiterContextError(ctx context.Context) error {
	if ctx == nil {
		return errors.New("rate limiter context is required")
	}
	return ctx.Err()
}
