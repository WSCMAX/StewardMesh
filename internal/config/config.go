// Package config loads and validates provider-neutral StewardMesh settings.
// Requirements: REQ-FOUNDATION-001, REQ-PLATFORM-VALKEY-001, SEC-GUARD-001, SEC-HTTP-001.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/cache"
)

type RepositoryDriver string

const (
	RepositoryDriverPostgres RepositoryDriver = "postgres"
	RepositoryDriverMemory   RepositoryDriver = "memory"
)

type CacheDriver string

const (
	CacheDriverNone   CacheDriver = "none"
	CacheDriverMemory CacheDriver = "memory"
	CacheDriverValkey CacheDriver = "valkey"
)

type Config struct {
	Addr                string
	DataDir             string
	BlobDir             string
	DatabaseURL         string
	RepositoryDriver    RepositoryDriver
	CacheDriver         CacheDriver
	CacheURL            string
	AllowedOrigin       string
	OrganizationID      string
	OrganizationName    string
	BootstrapToken      string
	SessionCookieSecure bool
	SessionTTL          time.Duration
	SeedSynthetic       bool
	validationError     error
}

func Load() (Config, error) {
	configuration := FromEnv()
	if err := configuration.Validate(); err != nil {
		return Config{}, err
	}
	return configuration, nil
}

func FromEnv() Config {
	allowedOrigin := envOr("STEWARDMESH_ALLOWED_ORIGIN", "http://localhost:5173")
	sessionCookieSecure, secureErr := envBool(
		"STEWARDMESH_SESSION_COOKIE_SECURE",
		strings.HasPrefix(allowedOrigin, "https://"),
	)
	sessionTTL, ttlErr := envDuration("STEWARDMESH_SESSION_TTL", 12*time.Hour)
	return Config{
		Addr:                envOr("STEWARDMESH_ADDR", "127.0.0.1:8080"),
		DataDir:             envOr("STEWARDMESH_DATA_DIR", "./data"),
		BlobDir:             envOr("STEWARDMESH_BLOB_DIR", "./storage"),
		DatabaseURL:         envOr("STEWARDMESH_DATABASE_URL", ""),
		RepositoryDriver:    RepositoryDriver(envOr("STEWARDMESH_REPOSITORY_DRIVER", string(RepositoryDriverPostgres))),
		CacheDriver:         CacheDriver(envOr("STEWARDMESH_CACHE_DRIVER", string(CacheDriverNone))),
		CacheURL:            os.Getenv("STEWARDMESH_CACHE_URL"),
		AllowedOrigin:       allowedOrigin,
		OrganizationID:      envOr("STEWARDMESH_ORGANIZATION_ID", "local-organization"),
		OrganizationName:    envOr("STEWARDMESH_ORGANIZATION_NAME", "StewardMesh Local Organization"),
		BootstrapToken:      os.Getenv("STEWARDMESH_BOOTSTRAP_TOKEN"),
		SessionCookieSecure: sessionCookieSecure,
		SessionTTL:          sessionTTL,
		SeedSynthetic:       envBoolDefault("STEWARDMESH_SEED_SYNTHETIC"),
		validationError:     errors.Join(secureErr, ttlErr),
	}
}

func envBoolDefault(key string) bool {
	return strings.EqualFold(os.Getenv(key), "true")
}

func (c Config) Validate() error {
	if c.validationError != nil {
		return c.validationError
	}
	if strings.TrimSpace(c.Addr) == "" {
		return errors.New("STEWARDMESH_ADDR is required")
	}
	host, port, err := net.SplitHostPort(c.Addr)
	if err != nil || port == "" {
		return errors.New("STEWARDMESH_ADDR must contain a valid host and port")
	}
	if strings.TrimSpace(c.OrganizationID) == "" {
		return errors.New("STEWARDMESH_ORGANIZATION_ID is required")
	}
	if strings.TrimSpace(c.OrganizationName) == "" {
		return errors.New("STEWARDMESH_ORGANIZATION_NAME is required")
	}
	switch c.RepositoryDriver {
	case RepositoryDriverPostgres:
		if strings.TrimSpace(c.DatabaseURL) == "" {
			return errors.New("STEWARDMESH_DATABASE_URL is required for the postgres repository driver")
		}
	case RepositoryDriverMemory:
	default:
		return fmt.Errorf("unsupported STEWARDMESH_REPOSITORY_DRIVER %q", c.RepositoryDriver)
	}
	switch c.CacheDriver {
	case CacheDriverNone, CacheDriverMemory:
		if c.CacheURL != "" {
			return errors.New("STEWARDMESH_CACHE_URL must be empty unless STEWARDMESH_CACHE_DRIVER is valkey")
		}
	case CacheDriverValkey:
		if err := cache.ValidateValkeyURL(c.CacheURL); err != nil {
			return fmt.Errorf("STEWARDMESH_CACHE_URL: %w", err)
		}
	default:
		return fmt.Errorf("unsupported STEWARDMESH_CACHE_DRIVER %q", c.CacheDriver)
	}
	var origin *url.URL
	if c.AllowedOrigin != "" {
		origin, err = url.Parse(c.AllowedOrigin)
		if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") || origin.Host == "" ||
			origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" {
			return errors.New("STEWARDMESH_ALLOWED_ORIGIN must be an HTTP or HTTPS origin without credentials, query, or path")
		}
		if origin.Scheme == "https" && !c.SessionCookieSecure {
			return errors.New("STEWARDMESH_SESSION_COOKIE_SECURE must be true for an HTTPS origin")
		}
		if origin.Scheme == "http" && c.SessionCookieSecure {
			return errors.New("STEWARDMESH_SESSION_COOKIE_SECURE must be false for an HTTP development origin")
		}
	}
	if c.SessionTTL < 15*time.Minute || c.SessionTTL > 24*time.Hour {
		return errors.New("STEWARDMESH_SESSION_TTL must be between 15m and 24h")
	}
	if c.BootstrapToken != "" && len(c.BootstrapToken) < 32 {
		return errors.New("STEWARDMESH_BOOTSTRAP_TOKEN must contain at least 32 bytes")
	}
	if !isLoopbackHost(host) {
		if c.AllowedOrigin == "" || origin == nil || origin.Scheme != "https" {
			return errors.New("a shared listener requires an HTTPS STEWARDMESH_ALLOWED_ORIGIN")
		}
		if !c.SessionCookieSecure {
			return errors.New("a shared listener requires secure session cookies")
		}
		if len(c.BootstrapToken) < 32 {
			return errors.New("a shared listener requires a 32-byte STEWARDMESH_BOOTSTRAP_TOKEN")
		}
	}
	return nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) (bool, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", key)
	}
	return parsed, nil
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid duration", key)
	}
	return parsed, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
