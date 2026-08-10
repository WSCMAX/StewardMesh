// Package cache defines StewardMesh's provider-neutral ephemeral storage seam.
// Cache state is reconstructible and must never be the authoritative copy of
// domain, session, authorization, import, or audit data.
// Requirement: REQ-PLATFORM-VALKEY-001.
package cache

import (
	"context"
	"errors"
	"time"
)

const maximumKeyBytes = 1_024

var (
	// ErrNotFound reports a cache miss.
	ErrNotFound = errors.New("cache key not found")
	// ErrClosed reports an operation attempted after the store was closed.
	ErrClosed = errors.New("cache store is closed")
	// ErrCapacity reports that a bounded store cannot accept another key.
	ErrCapacity = errors.New("cache store capacity reached")
	// ErrInvalidKey reports an empty, oversized, or unsafe cache key.
	ErrInvalidKey = errors.New("cache key is invalid")
	// ErrInvalidTTL reports an attempt to create non-expiring cache state.
	ErrInvalidTTL = errors.New("cache TTL must be positive")
	// ErrInvalidCounter reports a non-integer or overflowing counter value.
	ErrInvalidCounter = errors.New("cache value is not an incrementable integer")
)

// Store exposes only provider-neutral, context-aware cache operations. Set and
// Increment require positive TTLs so callers cannot accidentally create
// unbounded or non-expiring cache state.
type Store interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Increment(ctx context.Context, key string, ttl time.Duration) (int64, error)
	Ping(ctx context.Context) error
	Close() error
}

func validateKey(key string) error {
	if len(key) == 0 || len(key) > maximumKeyBytes {
		return ErrInvalidKey
	}
	return nil
}

func validateTTL(ttl time.Duration) error {
	if ttl <= 0 {
		return ErrInvalidTTL
	}
	return nil
}

func validateContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("cache context is required")
	}
	return ctx.Err()
}
