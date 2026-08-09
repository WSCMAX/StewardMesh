package cache

// Requirement: REQ-PLATFORM-VALKEY-001.

import (
	"context"
	"errors"
	"math"
	"strconv"
	"sync"
	"time"
)

// DefaultMemoryCapacity bounds local cache growth by entry count.
const DefaultMemoryCapacity = 10_000

type memoryEntry struct {
	value     []byte
	expiresAt time.Time
}

// MemoryStore is a bounded, concurrency-safe cache for local evaluation and
// deterministic tests. It rejects new entries at capacity after reclaiming
// expired data instead of silently evicting live entries.
type MemoryStore struct {
	mu         sync.Mutex
	entries    map[string]memoryEntry
	maxEntries int
	now        func() time.Time
	closed     bool
}

var _ Store = (*MemoryStore)(nil)

// NewMemoryStore creates a memory cache with the requested entry capacity.
func NewMemoryStore(maxEntries int) (*MemoryStore, error) {
	return newMemoryStore(maxEntries, time.Now)
}

// NewDefaultMemoryStore creates a memory cache using DefaultMemoryCapacity.
func NewDefaultMemoryStore() *MemoryStore {
	return &MemoryStore{
		entries:    make(map[string]memoryEntry),
		maxEntries: DefaultMemoryCapacity,
		now:        time.Now,
	}
}

func newMemoryStore(maxEntries int, now func() time.Time) (*MemoryStore, error) {
	if maxEntries < 1 {
		return nil, errors.New("positive memory cache capacity is required")
	}
	if now == nil {
		return nil, errors.New("memory cache clock is required")
	}
	return &MemoryStore{
		entries:    make(map[string]memoryEntry),
		maxEntries: maxEntries,
		now:        now,
	}, nil
}

func (s *MemoryStore) Get(ctx context.Context, key string) ([]byte, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}
	if err := validateKey(key); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.operationError(ctx); err != nil {
		return nil, err
	}
	entry, ok := s.entries[key]
	if !ok {
		return nil, ErrNotFound
	}
	if s.expired(entry) {
		delete(s.entries, key)
		return nil, ErrNotFound
	}
	return append([]byte(nil), entry.value...), nil
}

func (s *MemoryStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if err := validateKey(key); err != nil {
		return err
	}
	if err := validateTTL(ttl); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.operationError(ctx); err != nil {
		return err
	}
	if err := s.reserve(key); err != nil {
		return err
	}
	s.entries[key] = memoryEntry{
		value:     append([]byte(nil), value...),
		expiresAt: s.now().Add(ttl),
	}
	return nil
}

func (s *MemoryStore) Delete(ctx context.Context, key string) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if err := validateKey(key); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.operationError(ctx); err != nil {
		return err
	}
	delete(s.entries, key)
	return nil
}

func (s *MemoryStore) Increment(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	if err := validateContext(ctx); err != nil {
		return 0, err
	}
	if err := validateKey(key); err != nil {
		return 0, err
	}
	if err := validateTTL(ttl); err != nil {
		return 0, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.operationError(ctx); err != nil {
		return 0, err
	}

	now := s.now()
	entry, found := s.entries[key]
	if found && !now.Before(entry.expiresAt) {
		delete(s.entries, key)
		found = false
	}
	if !found {
		if err := s.reserve(key); err != nil {
			return 0, err
		}
		entry.expiresAt = now.Add(ttl)
	} else {
		value, err := strconv.ParseInt(string(entry.value), 10, 64)
		if err != nil || value == math.MaxInt64 {
			return 0, ErrInvalidCounter
		}
		entry.value = []byte(strconv.FormatInt(value+1, 10))
		s.entries[key] = entry
		return value + 1, nil
	}

	entry.value = []byte("1")
	s.entries[key] = entry
	return 1, nil
}

func (s *MemoryStore) Ping(ctx context.Context) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.operationError(ctx)
}

func (s *MemoryStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.entries = nil
	return nil
}

func (s *MemoryStore) operationError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return ErrClosed
	}
	return nil
}

func (s *MemoryStore) reserve(key string) error {
	if _, exists := s.entries[key]; exists {
		return nil
	}
	s.removeExpired()
	if len(s.entries) >= s.maxEntries {
		return ErrCapacity
	}
	return nil
}

func (s *MemoryStore) removeExpired() {
	now := s.now()
	for key, entry := range s.entries {
		if !now.Before(entry.expiresAt) {
			delete(s.entries, key)
		}
	}
}

func (s *MemoryStore) expired(entry memoryEntry) bool {
	return !s.now().Before(entry.expiresAt)
}
