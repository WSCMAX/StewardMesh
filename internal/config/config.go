// Package config loads and validates provider-neutral StewardMesh settings.
// Requirement: REQ-FOUNDATION-001.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

type RepositoryDriver string

const (
	RepositoryDriverPostgres RepositoryDriver = "postgres"
	RepositoryDriverMemory   RepositoryDriver = "memory"
)

type Config struct {
	Addr             string
	DataDir          string
	BlobDir          string
	DatabaseURL      string
	RepositoryDriver RepositoryDriver
	AllowedOrigin    string
	OrganizationID   string
	OrganizationName string
}

func Load() (Config, error) {
	configuration := FromEnv()
	if err := configuration.Validate(); err != nil {
		return Config{}, err
	}
	return configuration, nil
}

func FromEnv() Config {
	return Config{
		Addr:             envOr("STEWARDMESH_ADDR", "127.0.0.1:8080"),
		DataDir:          envOr("STEWARDMESH_DATA_DIR", "./data"),
		BlobDir:          envOr("STEWARDMESH_BLOB_DIR", "./storage"),
		DatabaseURL:      envOr("STEWARDMESH_DATABASE_URL", ""),
		RepositoryDriver: RepositoryDriver(envOr("STEWARDMESH_REPOSITORY_DRIVER", string(RepositoryDriverPostgres))),
		AllowedOrigin:    envOr("STEWARDMESH_ALLOWED_ORIGIN", "http://localhost:5173"),
		OrganizationID:   envOr("STEWARDMESH_ORGANIZATION_ID", "local-organization"),
		OrganizationName: envOr("STEWARDMESH_ORGANIZATION_NAME", "StewardMesh Local Organization"),
	}
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Addr) == "" {
		return errors.New("STEWARDMESH_ADDR is required")
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
	if c.AllowedOrigin != "" {
		origin, err := url.Parse(c.AllowedOrigin)
		if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") || origin.Host == "" ||
			origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" {
			return errors.New("STEWARDMESH_ALLOWED_ORIGIN must be an HTTP or HTTPS origin without credentials, query, or path")
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
