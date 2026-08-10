package cache

// Requirement: REQ-PLATFORM-VALKEY-001.

import (
	"context"
	"time"
)

// NoopStore implements disabled cache mode. Writes intentionally disappear,
// reads always miss, and increments behave like a new one-shot counter.
type NoopStore struct{}

var _ Store = (*NoopStore)(nil)

// NewNoopStore creates a disabled cache adapter.
func NewNoopStore() *NoopStore {
	return &NoopStore{}
}

func (*NoopStore) Get(ctx context.Context, key string) ([]byte, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}
	if err := validateKey(key); err != nil {
		return nil, err
	}
	return nil, ErrNotFound
}

func (*NoopStore) Set(ctx context.Context, key string, _ []byte, ttl time.Duration) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if err := validateKey(key); err != nil {
		return err
	}
	return validateTTL(ttl)
}

func (*NoopStore) Delete(ctx context.Context, key string) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	return validateKey(key)
}

func (*NoopStore) Increment(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	if err := validateContext(ctx); err != nil {
		return 0, err
	}
	if err := validateKey(key); err != nil {
		return 0, err
	}
	if err := validateTTL(ttl); err != nil {
		return 0, err
	}
	return 1, nil
}

func (*NoopStore) Ping(ctx context.Context) error {
	return validateContext(ctx)
}

func (*NoopStore) Close() error {
	return nil
}
