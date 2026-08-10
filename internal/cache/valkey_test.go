package cache

// Requirement: REQ-PLATFORM-VALKEY-001.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	valkeymock "github.com/valkey-io/valkey-go/mock"
	"go.uber.org/mock/gomock"
)

func TestParseValkeyURLAcceptsRedisAndTLSWithoutClientCache(t *testing.T) {
	tests := []struct {
		name       string
		rawURL     string
		expectsTLS bool
	}{
		{name: "redis", rawURL: "redis://localhost:6379/0"},
		{name: "rediss", rawURL: "rediss://cache.example.test:6379/0", expectsTLS: true},
		{name: "cluster", rawURL: "redis://cache-1:6379?addr=cache-2:6379"},
		{name: "sentinel", rawURL: "redis://sentinel:26379/0?master_set=stewardmesh"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			option, err := parseValkeyURL(test.rawURL)
			if err != nil {
				t.Fatal(err)
			}
			if option.DisableCache != true {
				t.Fatal("expected client-side caching to be disabled")
			}
			if (option.TLSConfig != nil) != test.expectsTLS {
				t.Fatalf("unexpected TLS configuration for %q", test.rawURL)
			}
			if option.ClientName != "stewardmesh" {
				t.Fatalf("expected StewardMesh client name, got %q", option.ClientName)
			}
			if option.ShuffleInit != (len(option.InitAddress) > 1) {
				t.Fatal("expected multi-address clients to shuffle initial nodes")
			}
		})
	}
}

func TestValidateValkeyURLRejectsUnsupportedOrMalformedValuesWithoutLeakingSecrets(t *testing.T) {
	invalid := []string{
		"",
		" redis://localhost:6379",
		"http://localhost:6379",
		"unix:///run/valkey.sock",
		"redis:///0",
		"redis://localhost:6379/0#fragment",
		"rediss://cache.example.test:6379?skip_verify=true",
		"redis://user:super-secret@",
	}
	for _, rawURL := range invalid {
		err := ValidateValkeyURL(rawURL)
		if !errors.Is(err, ErrInvalidValkeyURL) {
			t.Fatalf("expected invalid Valkey URL for %q, got %v", rawURL, err)
		}
		if strings.Contains(err.Error(), "super-secret") {
			t.Fatal("expected URL validation error to redact credentials")
		}
	}
}

func TestParseValkeyURLPreservesExplicitClientNameAndSentinelMaster(t *testing.T) {
	option, err := parseValkeyURL("redis://sentinel:26379/0?master_set=stewardmesh&client_name=worker")
	if err != nil {
		t.Fatal(err)
	}
	if option.ClientName != "worker" {
		t.Fatalf("expected explicit client name, got %q", option.ClientName)
	}
	if option.Sentinel.MasterSet != "stewardmesh" {
		t.Fatalf("expected sentinel master set, got %q", option.Sentinel.MasterSet)
	}
}

func TestParseValkeyURLDecodesTLSCredentialsForManagedCaches(t *testing.T) {
	option, err := parseValkeyURL("rediss://stewardmesh:p%40ss%3Aword@cache.example.test:6379/0")
	if err != nil {
		t.Fatal(err)
	}
	if option.Username != "stewardmesh" || option.Password != "p@ss:word" {
		t.Fatal("expected percent-encoded managed-cache credentials to be decoded")
	}
	if option.TLSConfig == nil || option.TLSConfig.InsecureSkipVerify {
		t.Fatal("expected managed-cache credentials to preserve verified TLS")
	}
	if option.SelectDB != 0 {
		t.Fatalf("expected managed cache database 0, got %d", option.SelectDB)
	}
}

func TestValkeyStoreImplementsCacheCommands(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := valkeymock.NewClient(ctrl)
	store := newValkeyStore(client)
	ctx := context.Background()

	client.EXPECT().Do(ctx, valkeymock.Match("GET", "key")).
		Return(valkeymock.Result(valkeymock.ValkeyBlobString("value")))
	value, err := store.Get(ctx, "key")
	if err != nil || string(value) != "value" {
		t.Fatalf("unexpected get value=%q err=%v", value, err)
	}

	client.EXPECT().Do(ctx, valkeymock.Match("SET", "key", "value", "PX", "60000")).
		Return(valkeymock.Result(valkeymock.ValkeyString("OK")))
	if err := store.Set(ctx, "key", []byte("value"), time.Minute); err != nil {
		t.Fatal(err)
	}

	client.EXPECT().Do(ctx, valkeymock.Match("DEL", "key")).
		Return(valkeymock.Result(valkeymock.ValkeyInt64(1)))
	if err := store.Delete(ctx, "key"); err != nil {
		t.Fatal(err)
	}

	client.EXPECT().Do(ctx, valkeymock.Match(
		"EVAL", incrementWithTTLScript, "1", "counter", "60000",
	)).
		Return(valkeymock.Result(valkeymock.ValkeyInt64(2)))
	count, err := store.Increment(ctx, "counter", time.Minute)
	if err != nil || count != 2 {
		t.Fatalf("unexpected increment count=%d err=%v", count, err)
	}

	client.EXPECT().Do(ctx, valkeymock.Match("PING")).
		Return(valkeymock.Result(valkeymock.ValkeyString("PONG")))
	if err := store.Ping(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestValkeyStoreMapsMissesCounterErrorsAndClose(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := valkeymock.NewClient(ctrl)
	store := newValkeyStore(client)
	ctx := context.Background()

	client.EXPECT().Do(ctx, valkeymock.Match("GET", "missing")).
		Return(valkeymock.Result(valkeymock.ValkeyNil()))
	if _, err := store.Get(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected cache miss, got %v", err)
	}

	client.EXPECT().Do(ctx, gomock.Any()).
		Return(valkeymock.Result(valkeymock.ValkeyError("ERR value is not an integer or out of range")))
	if _, err := store.Increment(ctx, "counter", time.Minute); !errors.Is(err, ErrInvalidCounter) {
		t.Fatalf("expected invalid counter, got %v", err)
	}

	client.EXPECT().Do(ctx, valkeymock.Match("GET", "key")).
		Return(valkeymock.ErrorResult(context.DeadlineExceeded))
	if _, err := store.Get(ctx, "key"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected wrapped context deadline, got %v", err)
	}

	client.EXPECT().Close()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Ping(ctx); !errors.Is(err, ErrClosed) {
		t.Fatalf("expected closed store, got %v", err)
	}
}

func TestValkeyStoreRejectsInvalidInputsBeforeCallingClient(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := newValkeyStore(valkeymock.NewClient(ctrl))
	if err := store.Set(context.Background(), "", nil, time.Minute); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected invalid key, got %v", err)
	}
	if err := store.Set(context.Background(), "key", nil, time.Nanosecond); !errors.Is(err, ErrInvalidTTL) {
		t.Fatalf("expected invalid TTL, got %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Get(canceled, "key"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context, got %v", err)
	}
}
