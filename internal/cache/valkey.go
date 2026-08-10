package cache

// Requirement: REQ-PLATFORM-VALKEY-001.

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	valkey "github.com/valkey-io/valkey-go"
)

const incrementWithTTLScript = `local existed = redis.call('EXISTS', KEYS[1])
local value = redis.call('INCR', KEYS[1])
if existed == 0 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return value`

// ErrInvalidValkeyURL reports a missing, unsupported, or malformed Valkey URL
// without retaining or exposing its credentials.
var ErrInvalidValkeyURL = errors.New("Valkey URL must use redis:// or rediss:// and include a host")

// ValkeyStore adapts the official Valkey Go client to Store. Client-side
// caching is disabled so this shared server remains the only cache layer.
type ValkeyStore struct {
	client    valkey.Client
	closeOnce sync.Once
	closed    atomic.Bool
}

var _ Store = (*ValkeyStore)(nil)

// NewValkeyStore creates a Valkey or Redis-compatible cache from a redis:// or
// rediss:// URL. The client supports standalone, cluster, and sentinel URLs.
func NewValkeyStore(rawURL string) (*ValkeyStore, error) {
	option, err := parseValkeyURL(rawURL)
	if err != nil {
		return nil, err
	}
	client, err := valkey.NewClient(option)
	if err != nil {
		return nil, fmt.Errorf("initialize Valkey client: %w", err)
	}
	return newValkeyStore(client), nil
}

// ValidateValkeyURL checks the supported URL contract without opening a
// network connection.
func ValidateValkeyURL(rawURL string) error {
	_, err := parseValkeyURL(rawURL)
	return err
}

func newValkeyStore(client valkey.Client) *ValkeyStore {
	return &ValkeyStore{client: client}
}

func parseValkeyURL(rawURL string) (valkey.ClientOption, error) {
	if rawURL == "" || rawURL != strings.TrimSpace(rawURL) {
		return valkey.ClientOption{}, ErrInvalidValkeyURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || parsed.Fragment != "" ||
		(parsed.Scheme != "redis" && parsed.Scheme != "rediss") {
		return valkey.ClientOption{}, ErrInvalidValkeyURL
	}
	option, err := valkey.ParseURL(rawURL)
	if err != nil {
		return valkey.ClientOption{}, ErrInvalidValkeyURL
	}
	if parsed.Scheme == "rediss" && option.TLSConfig == nil {
		return valkey.ClientOption{}, ErrInvalidValkeyURL
	}
	if option.TLSConfig != nil && option.TLSConfig.InsecureSkipVerify {
		return valkey.ClientOption{}, ErrInvalidValkeyURL
	}
	option.DisableCache = true
	option.ShuffleInit = len(option.InitAddress) > 1
	if option.ClientName == "" {
		option.ClientName = "stewardmesh"
	}
	return option, nil
}

func (s *ValkeyStore) Get(ctx context.Context, key string) ([]byte, error) {
	if err := s.validateOperation(ctx, key); err != nil {
		return nil, err
	}
	value, err := s.client.Do(ctx, s.client.B().Get().Key(key).Build()).AsBytes()
	if valkey.IsValkeyNil(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, wrapValkeyError("get", err)
	}
	return append([]byte(nil), value...), nil
}

func (s *ValkeyStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := s.validateOperation(ctx, key); err != nil {
		return err
	}
	if err := validateTTL(ttl); err != nil {
		return err
	}
	command := s.client.B().Set().Key(key).Value(valkey.BinaryString(value)).Px(ttl).Build()
	return wrapValkeyError("set", s.client.Do(ctx, command).Error())
}

func (s *ValkeyStore) Delete(ctx context.Context, key string) error {
	if err := s.validateOperation(ctx, key); err != nil {
		return err
	}
	return wrapValkeyError("delete", s.client.Do(ctx, s.client.B().Del().Key(key).Build()).Error())
}

func (s *ValkeyStore) Increment(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	if err := s.validateOperation(ctx, key); err != nil {
		return 0, err
	}
	if err := validateTTL(ttl); err != nil {
		return 0, err
	}
	command := s.client.B().Eval().Script(incrementWithTTLScript).Numkeys(1).
		Key(key).Arg(strconv.FormatInt(ttl.Milliseconds(), 10)).Build()
	value, err := s.client.Do(ctx, command).AsInt64()
	if err != nil {
		return 0, wrapValkeyError("increment", err)
	}
	return value, nil
}

func (s *ValkeyStore) Ping(ctx context.Context) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if s.closed.Load() {
		return ErrClosed
	}
	return wrapValkeyError("ping", s.client.Do(ctx, s.client.B().Ping().Build()).Error())
}

func (s *ValkeyStore) Close() error {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		s.client.Close()
	})
	return nil
}

func (s *ValkeyStore) validateOperation(ctx context.Context, key string) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if err := validateKey(key); err != nil {
		return err
	}
	if s.closed.Load() {
		return ErrClosed
	}
	return nil
}

func wrapValkeyError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "not an integer or out of range") {
		return fmt.Errorf("Valkey %s: %w", operation, ErrInvalidCounter)
	}
	return fmt.Errorf("Valkey %s: %w", operation, err)
}
