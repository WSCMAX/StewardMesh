package atlascodes

// Requirement: REQ-ATLAS-CODES-001. Feature: inventory.identifiers.

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"time"
)

const defaultRequestRateLimitKeys = 4096

type RequestRateLimit struct {
	Maximum int
	Window  time.Duration
}

func (l RequestRateLimit) Validate() error {
	if l.Maximum < 1 || l.Maximum > 10000 || l.Window < time.Second || l.Window > 24*time.Hour {
		return errors.New("Atlas Codes request rate limit is invalid")
	}
	return nil
}

type RequestLimiter interface {
	Allow(context.Context, string, RequestRateLimit, time.Time) (bool, error)
}

type requestRateEntry struct {
	started time.Time
	count   int
}

// MemoryRequestLimiter is a bounded fixed-window limiter for authenticated
// Atlas Codes requests. Keys are retained only as SHA-256 digests so account,
// organization, and network identifiers are not stored in plaintext.
type MemoryRequestLimiter struct {
	mu      sync.Mutex
	maximum int
	entries map[[sha256.Size]byte]requestRateEntry
}

func NewMemoryRequestLimiter(maximumKeys int) (*MemoryRequestLimiter, error) {
	if maximumKeys < 1 || maximumKeys > 100000 {
		return nil, errors.New("Atlas Codes request rate limiter capacity is invalid")
	}
	return &MemoryRequestLimiter{maximum: maximumKeys, entries: make(map[[sha256.Size]byte]requestRateEntry)}, nil
}

func NewDefaultRequestLimiter() *MemoryRequestLimiter {
	limiter, err := NewMemoryRequestLimiter(defaultRequestRateLimitKeys)
	if err != nil {
		panic(err)
	}
	return limiter
}

func (l *MemoryRequestLimiter) Allow(ctx context.Context, key string, limit RequestRateLimit, now time.Time) (bool, error) {
	if ctx == nil || key == "" || len(key) > 1024 || limit.Validate() != nil || now.IsZero() {
		return false, errors.New("Atlas Codes request rate limit input is invalid")
	}
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}
	digest := sha256.Sum256([]byte(key))
	now = now.UTC()
	l.mu.Lock()
	defer l.mu.Unlock()
	if entry, ok := l.entries[digest]; ok {
		if now.Before(entry.started) || !now.Before(entry.started.Add(limit.Window)) {
			l.entries[digest] = requestRateEntry{started: now, count: 1}
			return true, nil
		}
		if entry.count >= limit.Maximum {
			return false, nil
		}
		entry.count++
		l.entries[digest] = entry
		return true, nil
	}
	for existingDigest, entry := range l.entries {
		if !now.Before(entry.started.Add(limit.Window)) {
			delete(l.entries, existingDigest)
		}
	}
	if len(l.entries) >= l.maximum {
		return false, errors.New("Atlas Codes request rate limiter capacity reached")
	}
	l.entries[digest] = requestRateEntry{started: now, count: 1}
	return true, nil
}
