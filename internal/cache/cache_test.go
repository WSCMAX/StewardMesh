package cache

// Requirement: REQ-PLATFORM-VALKEY-001.

import (
	"context"
	"errors"
	"math"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestNoopStoreDoesNotRetainValues(t *testing.T) {
	store := NewNoopStore()
	ctx := context.Background()
	if err := store.Set(ctx, "key", []byte("value"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, "key"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected cache miss, got %v", err)
	}
	count, err := store.Increment(ctx, "counter", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one-shot counter value 1, got %d", count)
	}
	if err := store.Delete(ctx, "key"); err != nil {
		t.Fatal(err)
	}
	if err := store.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryStoreCopiesValuesAndDeletes(t *testing.T) {
	store, err := NewMemoryStore(2)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	input := []byte("value")
	if err := store.Set(ctx, "key", input, time.Minute); err != nil {
		t.Fatal(err)
	}
	input[0] = 'X'
	value, err := store.Get(ctx, "key")
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != "value" {
		t.Fatalf("expected copied value, got %q", value)
	}
	value[0] = 'Y'
	value, err = store.Get(ctx, "key")
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != "value" {
		t.Fatalf("expected copied result, got %q", value)
	}
	if err := store.Delete(ctx, "key"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, "key"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected deleted key to miss, got %v", err)
	}
}

func TestMemoryStoreExpiresAndReclaimsEntries(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	store, err := newMemoryStore(1, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.Set(ctx, "first", []byte("value"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(ctx, "second", []byte("value"), time.Minute); !errors.Is(err, ErrCapacity) {
		t.Fatalf("expected capacity error, got %v", err)
	}
	now = now.Add(time.Minute)
	if _, err := store.Get(ctx, "first"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected expired key to miss, got %v", err)
	}
	if err := store.Set(ctx, "second", []byte("value"), time.Minute); err != nil {
		t.Fatalf("expected expired capacity to be reclaimed: %v", err)
	}
}

func TestMemoryStoreIncrementIsAtomicAndPreservesTTL(t *testing.T) {
	store, err := NewMemoryStore(1)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const workers = 100
	var wg sync.WaitGroup
	errorsFound := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Increment(ctx, "counter", time.Minute)
			errorsFound <- err
		}()
	}
	wg.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
	value, err := store.Get(ctx, "counter")
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != strconv.Itoa(workers) {
		t.Fatalf("expected %d increments, got %q", workers, value)
	}

	clock := time.Unix(1_700_000_000, 0)
	ttlStore, err := newMemoryStore(1, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ttlStore.Increment(ctx, "counter", time.Minute); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(30 * time.Second)
	if count, err := ttlStore.Increment(ctx, "counter", time.Hour); err != nil || count != 2 {
		t.Fatalf("expected second increment, got count=%d err=%v", count, err)
	}
	clock = clock.Add(30 * time.Second)
	if count, err := ttlStore.Increment(ctx, "counter", time.Minute); err != nil || count != 1 {
		t.Fatalf("expected original TTL to expire counter, got count=%d err=%v", count, err)
	}
}

func TestMemoryStoreRejectsInvalidCounter(t *testing.T) {
	store, err := NewMemoryStore(1)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.Set(ctx, "counter", []byte("not-a-number"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Increment(ctx, "counter", time.Minute); !errors.Is(err, ErrInvalidCounter) {
		t.Fatalf("expected invalid counter error, got %v", err)
	}
	if err := store.Set(ctx, "counter", []byte(strconv.FormatInt(math.MaxInt64, 10)), time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Increment(ctx, "counter", time.Minute); !errors.Is(err, ErrInvalidCounter) {
		t.Fatalf("expected overflow counter error, got %v", err)
	}
}

func TestStoresValidateInputsAndContext(t *testing.T) {
	if _, err := NewMemoryStore(0); err == nil {
		t.Fatal("expected non-positive memory capacity to be rejected")
	}
	stores := []Store{NewNoopStore(), NewDefaultMemoryStore()}
	for _, store := range stores {
		if err := store.Set(context.Background(), "", nil, time.Minute); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("expected invalid key error from %T, got %v", store, err)
		}
		if err := store.Set(context.Background(), "key", nil, 0); !errors.Is(err, ErrInvalidTTL) {
			t.Fatalf("expected invalid TTL error from %T, got %v", store, err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := store.Ping(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("expected canceled context from %T, got %v", store, err)
		}
	}
}

func TestMemoryStoreCloseIsIdempotent(t *testing.T) {
	store := NewDefaultMemoryStore()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Ping(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("expected closed error, got %v", err)
	}
}
