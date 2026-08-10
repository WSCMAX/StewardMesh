package guard

// Requirements: REQ-PLATFORM-VALKEY-001, SEC-GUARD-001.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/maxlemke/stewardmesh/internal/cache"
)

const (
	defaultMaximumLoginFailures = 5
	defaultLoginFailureWindow   = 15 * time.Minute
	minimumRateKeySecretBytes   = 32
)

// CacheAttemptLimiter stores only ephemeral failure counters in a
// provider-neutral cache. A stable HMAC secret prevents low-entropy account and
// client identifiers from being recoverable from cache keys.
type CacheAttemptLimiter struct {
	store       cache.Store
	namespace   cache.Namespace
	keySecret   []byte
	maxFailures int64
	window      time.Duration
}

var _ AttemptLimiter = (*CacheAttemptLimiter)(nil)

// NewDefaultCacheAttemptLimiter creates Guard's five-failure, fifteen-minute
// shared login limiter.
func NewDefaultCacheAttemptLimiter(store cache.Store, namespace cache.Namespace, keySecret []byte) (*CacheAttemptLimiter, error) {
	return NewCacheAttemptLimiter(
		store,
		namespace,
		keySecret,
		defaultMaximumLoginFailures,
		defaultLoginFailureWindow,
	)
}

// NewCacheAttemptLimiter creates a shared limiter with an explicit failure
// threshold and fixed first-failure window.
func NewCacheAttemptLimiter(
	store cache.Store,
	namespace cache.Namespace,
	keySecret []byte,
	maxFailures int,
	window time.Duration,
) (*CacheAttemptLimiter, error) {
	if store == nil {
		return nil, errors.New("cache store is required for shared login rate limiting")
	}
	if len(keySecret) < minimumRateKeySecretBytes {
		return nil, errors.New("shared login rate-limit key secret must contain at least 32 bytes")
	}
	if maxFailures < 1 || window < time.Millisecond {
		return nil, errors.New("positive shared rate limit and window are required")
	}
	if _, err := namespace.Key("guard-login-failures", "validation"); err != nil {
		return nil, fmt.Errorf("valid shared login rate-limit namespace is required: %w", err)
	}
	return &CacheAttemptLimiter{
		store:       store,
		namespace:   namespace,
		keySecret:   append([]byte(nil), keySecret...),
		maxFailures: int64(maxFailures),
		window:      window,
	}, nil
}

func (l *CacheAttemptLimiter) Allow(ctx context.Context, key string, _ time.Time) (bool, error) {
	cacheKey, err := l.cacheKey(ctx, key)
	if err != nil {
		return false, err
	}
	value, err := l.store.Get(ctx, cacheKey)
	if errors.Is(err, cache.ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("read shared login failure count: %w", err)
	}
	count, err := strconv.ParseInt(string(value), 10, 64)
	if err != nil || count < 1 {
		return false, errors.New("shared login failure count is invalid")
	}
	return count < l.maxFailures, nil
}

func (l *CacheAttemptLimiter) Failure(ctx context.Context, key string, _ time.Time) error {
	cacheKey, err := l.cacheKey(ctx, key)
	if err != nil {
		return err
	}
	count, err := l.store.Increment(ctx, cacheKey, l.window)
	if err != nil {
		return fmt.Errorf("increment shared login failure count: %w", err)
	}
	if count < 1 {
		return errors.New("shared login failure count is invalid")
	}
	return nil
}

func (l *CacheAttemptLimiter) Reset(ctx context.Context, key string) error {
	cacheKey, err := l.cacheKey(ctx, key)
	if err != nil {
		return err
	}
	if err := l.store.Delete(ctx, cacheKey); err != nil {
		return fmt.Errorf("reset shared login failure count: %w", err)
	}
	return nil
}

func (l *CacheAttemptLimiter) cacheKey(ctx context.Context, key string) (string, error) {
	if err := limiterContextError(ctx); err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, l.keySecret)
	_, _ = mac.Write([]byte(key))
	dimension := hex.EncodeToString(mac.Sum(nil))
	cacheKey, err := l.namespace.Key("guard-login-failures", dimension)
	if err != nil {
		return "", fmt.Errorf("build shared login failure key: %w", err)
	}
	return cacheKey, nil
}
