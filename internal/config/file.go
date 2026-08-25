package config

// Requirement: REQ-DIRECTORY-EXPANSION-007. Feature: platform.foundation.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	configPathEnv      = "STEWARDMESH_CONFIG"
	maximumConfigBytes = 1 << 20
)

type fileConfig struct {
	Listen       *fileListen       `yaml:"listen"`
	Organization *fileOrganization `yaml:"organization"`
	Repository   *fileRepository   `yaml:"repository"`
	Storage      *fileStorage      `yaml:"storage"`
	Cache        *fileCache        `yaml:"cache"`
	Web          *fileWeb          `yaml:"web"`
	Session      *fileSession      `yaml:"session"`
	Seed         *fileSeed         `yaml:"seed"`
	Reach        *fileReach        `yaml:"reach"`
}

type fileListen struct {
	Addr          *string `yaml:"addr"`
	AllowedOrigin *string `yaml:"allowed_origin"`
	InsecureBind  *bool   `yaml:"insecure_bind"`
}

type fileOrganization struct {
	ID                     *string `yaml:"id"`
	Name                   *string `yaml:"name"`
	ExchangeSourceSystemID *string `yaml:"exchange_source_system_id"`
}

type fileRepository struct {
	Driver *string `yaml:"driver"`
	URL    *string `yaml:"url"`
}

type fileStorage struct {
	Driver           *string `yaml:"driver"`
	DataDir          *string `yaml:"data_dir"`
	BlobDir          *string `yaml:"blob_dir"`
	BlobMaximumBytes *int64  `yaml:"blob_maximum_bytes"`
	BlobDownloadTTL  *string `yaml:"blob_download_ttl"`
}

type fileCache struct {
	Driver    *string `yaml:"driver"`
	URL       *string `yaml:"url"`
	KeySecret *string `yaml:"key_secret"`
}

type fileWeb struct {
	Dir *string `yaml:"dir"`
}

type fileSession struct {
	CookieSecure   *bool   `yaml:"cookie_secure"`
	TTL            *string `yaml:"ttl"`
	BootstrapToken *string `yaml:"bootstrap_token"`
}

type fileSeed struct {
	Synthetic *bool `yaml:"synthetic"`
	Campus    *bool `yaml:"campus"`
}

type fileReach struct {
	EndpointsFile *string `yaml:"endpoints_file"`
	SecretPrefix  *string `yaml:"secret_prefix"`
}

func loadOptionalFile() (fileConfig, error) {
	path := strings.TrimSpace(os.Getenv(configPathEnv))
	if path == "" {
		return fileConfig{}, nil
	}
	return loadFile(path)
}

func loadFile(path string) (fileConfig, error) {
	if path != filepath.Clean(path) || strings.ContainsRune(path, '\x00') || len(path) > 1024 {
		return fileConfig{}, fmt.Errorf("%s is invalid", configPathEnv)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fileConfig{}, fmt.Errorf("read %s: %w", configPathEnv, err)
	}
	if !info.Mode().IsRegular() {
		return fileConfig{}, fmt.Errorf("%s must be a regular file", configPathEnv)
	}
	if info.Size() > maximumConfigBytes {
		return fileConfig{}, fmt.Errorf("%s exceeds %d bytes", configPathEnv, maximumConfigBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fileConfig{}, fmt.Errorf("read %s: %w", configPathEnv, err)
	}
	var file fileConfig
	if err := yaml.Unmarshal(data, &file); err != nil {
		return fileConfig{}, fmt.Errorf("parse %s: %w", configPathEnv, err)
	}
	return file, nil
}

func overlayFile(cfg Config, file fileConfig) Config {
	if listen := file.Listen; listen != nil {
		overlayString(&cfg.Addr, "STEWARDMESH_ADDR", listen.Addr)
		overlayString(&cfg.AllowedOrigin, "STEWARDMESH_ALLOWED_ORIGIN", listen.AllowedOrigin)
		overlayBool(&cfg.InsecureBind, "STEWARDMESH_INSECURE_BIND", listen.InsecureBind)
	}
	if organization := file.Organization; organization != nil {
		overlayString(&cfg.OrganizationID, "STEWARDMESH_ORGANIZATION_ID", organization.ID)
		overlayString(&cfg.OrganizationName, "STEWARDMESH_ORGANIZATION_NAME", organization.Name)
		overlayString(&cfg.ExchangeSourceSystemID, "STEWARDMESH_EXCHANGE_SOURCE_SYSTEM_ID", organization.ExchangeSourceSystemID)
	}
	if repository := file.Repository; repository != nil {
		if os.Getenv("STEWARDMESH_REPOSITORY_DRIVER") == "" && repository.Driver != nil {
			cfg.RepositoryDriver = RepositoryDriver(strings.TrimSpace(*repository.Driver))
		}
		overlayString(&cfg.DatabaseURL, "STEWARDMESH_DATABASE_URL", repository.URL)
	}
	if storageCfg := file.Storage; storageCfg != nil {
		if os.Getenv("STEWARDMESH_STORAGE_DRIVER") == "" && storageCfg.Driver != nil {
			cfg.StorageDriver = StorageDriver(strings.TrimSpace(*storageCfg.Driver))
		}
		overlayString(&cfg.DataDir, "STEWARDMESH_DATA_DIR", storageCfg.DataDir)
		overlayString(&cfg.BlobDir, "STEWARDMESH_BLOB_DIR", storageCfg.BlobDir)
		if os.Getenv("STEWARDMESH_BLOB_MAXIMUM_BYTES") == "" && storageCfg.BlobMaximumBytes != nil {
			cfg.BlobMaximumBytes = *storageCfg.BlobMaximumBytes
		}
		if os.Getenv("STEWARDMESH_BLOB_DOWNLOAD_TTL") == "" && storageCfg.BlobDownloadTTL != nil {
			if parsed, err := time.ParseDuration(strings.TrimSpace(*storageCfg.BlobDownloadTTL)); err == nil {
				cfg.BlobDownloadTTL = parsed
			} else {
				cfg.validationError = joinConfigError(cfg.validationError, fmt.Errorf("storage.blob_download_ttl must be a valid duration"))
			}
		}
	}
	if cacheCfg := file.Cache; cacheCfg != nil {
		if os.Getenv("STEWARDMESH_CACHE_DRIVER") == "" && cacheCfg.Driver != nil {
			cfg.CacheDriver = CacheDriver(strings.TrimSpace(*cacheCfg.Driver))
		}
		overlayString(&cfg.CacheURL, "STEWARDMESH_CACHE_URL", cacheCfg.URL)
		overlayString(&cfg.CacheKeySecret, "STEWARDMESH_CACHE_KEY_SECRET", cacheCfg.KeySecret)
	}
	if web := file.Web; web != nil {
		overlayString(&cfg.WebDir, "STEWARDMESH_WEB_DIR", web.Dir)
	}
	if session := file.Session; session != nil {
		overlayBool(&cfg.SessionCookieSecure, "STEWARDMESH_SESSION_COOKIE_SECURE", session.CookieSecure)
		overlayString(&cfg.BootstrapToken, "STEWARDMESH_BOOTSTRAP_TOKEN", session.BootstrapToken)
		if os.Getenv("STEWARDMESH_SESSION_TTL") == "" && session.TTL != nil {
			if parsed, err := time.ParseDuration(strings.TrimSpace(*session.TTL)); err == nil {
				cfg.SessionTTL = parsed
			} else {
				cfg.validationError = joinConfigError(cfg.validationError, fmt.Errorf("session.ttl must be a valid duration"))
			}
		}
	}
	if seed := file.Seed; seed != nil {
		overlayBool(&cfg.SeedSynthetic, "STEWARDMESH_SEED_SYNTHETIC", seed.Synthetic)
		overlayBool(&cfg.SeedCampus, "STEWARDMESH_SEED_CAMPUS", seed.Campus)
	}
	if reach := file.Reach; reach != nil {
		overlayString(&cfg.ReachEndpointsFile, "STEWARDMESH_REACH_ENDPOINTS_FILE", reach.EndpointsFile)
		overlayString(&cfg.ReachSecretPrefix, "STEWARDMESH_REACH_SECRET_PREFIX", reach.SecretPrefix)
	}
	if os.Getenv("STEWARDMESH_EXCHANGE_SOURCE_SYSTEM_ID") == "" &&
		(file.Organization == nil || file.Organization.ExchangeSourceSystemID == nil || strings.TrimSpace(*file.Organization.ExchangeSourceSystemID) == "") {
		cfg.ExchangeSourceSystemID = cfg.OrganizationID
	}
	return cfg
}

func overlayString(dest *string, envKey string, fileValue *string) {
	if os.Getenv(envKey) != "" || fileValue == nil {
		return
	}
	*dest = *fileValue
}

func overlayBool(dest *bool, envKey string, fileValue *bool) {
	if os.Getenv(envKey) != "" || fileValue == nil {
		return
	}
	*dest = *fileValue
}

func joinConfigError(current, next error) error {
	if current == nil {
		return next
	}
	if next == nil {
		return current
	}
	return fmt.Errorf("%v; %v", current, next)
}
