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
	Addr                       string
	DataDir                    string
	BlobDir                    string
	DatabaseURL                string
	RepositoryDriver           RepositoryDriver
	CacheDriver                CacheDriver
	CacheURL                   string
	CacheKeySecret             string
	OIDCIssuerURL              string
	OIDCClientID               string
	OIDCClientSecret           string
	OIDCRedirectURL            string
	OIDCTransactionSecret      string
	OIDCAdministratorClaim     string
	OIDCAdministratorValues    []string
	OIDCRequireVerifiedEmail   bool
	SAMLIDPMetadataURL         string
	SAMLEntityID               string
	SAMLSPCertificateFile      string
	SAMLSPPrivateKeyFile       string
	SAMLEmailAttribute         string
	SAMLDisplayNameAttribute   string
	SAMLAdministratorAttribute string
	SAMLAdministratorValues    []string
	AllowedOrigin              string
	OrganizationID             string
	OrganizationName           string
	BootstrapToken             string
	SessionCookieSecure        bool
	SessionTTL                 time.Duration
	SeedSynthetic              bool
	validationError            error
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
	oidcRequireVerifiedEmail, oidcVerifiedEmailErr := envBool("STEWARDMESH_OIDC_REQUIRE_VERIFIED_EMAIL", true)
	return Config{
		Addr:                       envOr("STEWARDMESH_ADDR", "127.0.0.1:8080"),
		DataDir:                    envOr("STEWARDMESH_DATA_DIR", "./data"),
		BlobDir:                    envOr("STEWARDMESH_BLOB_DIR", "./storage"),
		DatabaseURL:                envOr("STEWARDMESH_DATABASE_URL", ""),
		RepositoryDriver:           RepositoryDriver(envOr("STEWARDMESH_REPOSITORY_DRIVER", string(RepositoryDriverPostgres))),
		CacheDriver:                CacheDriver(envOr("STEWARDMESH_CACHE_DRIVER", string(CacheDriverNone))),
		CacheURL:                   os.Getenv("STEWARDMESH_CACHE_URL"),
		CacheKeySecret:             os.Getenv("STEWARDMESH_CACHE_KEY_SECRET"),
		OIDCIssuerURL:              os.Getenv("STEWARDMESH_OIDC_ISSUER_URL"),
		OIDCClientID:               os.Getenv("STEWARDMESH_OIDC_CLIENT_ID"),
		OIDCClientSecret:           os.Getenv("STEWARDMESH_OIDC_CLIENT_SECRET"),
		OIDCRedirectURL:            os.Getenv("STEWARDMESH_OIDC_REDIRECT_URL"),
		OIDCTransactionSecret:      os.Getenv("STEWARDMESH_OIDC_TRANSACTION_SECRET"),
		OIDCAdministratorClaim:     os.Getenv("STEWARDMESH_OIDC_ADMINISTRATOR_CLAIM"),
		OIDCAdministratorValues:    envCSV("STEWARDMESH_OIDC_ADMINISTRATOR_VALUES"),
		OIDCRequireVerifiedEmail:   oidcRequireVerifiedEmail,
		SAMLIDPMetadataURL:         os.Getenv("STEWARDMESH_SAML_IDP_METADATA_URL"),
		SAMLEntityID:               os.Getenv("STEWARDMESH_SAML_ENTITY_ID"),
		SAMLSPCertificateFile:      os.Getenv("STEWARDMESH_SAML_SP_CERTIFICATE_FILE"),
		SAMLSPPrivateKeyFile:       os.Getenv("STEWARDMESH_SAML_SP_PRIVATE_KEY_FILE"),
		SAMLEmailAttribute:         os.Getenv("STEWARDMESH_SAML_EMAIL_ATTRIBUTE"),
		SAMLDisplayNameAttribute:   os.Getenv("STEWARDMESH_SAML_DISPLAY_NAME_ATTRIBUTE"),
		SAMLAdministratorAttribute: os.Getenv("STEWARDMESH_SAML_ADMINISTRATOR_ATTRIBUTE"),
		SAMLAdministratorValues:    envCSV("STEWARDMESH_SAML_ADMINISTRATOR_VALUES"),
		AllowedOrigin:              allowedOrigin,
		OrganizationID:             envOr("STEWARDMESH_ORGANIZATION_ID", "local-organization"),
		OrganizationName:           envOr("STEWARDMESH_ORGANIZATION_NAME", "StewardMesh Local Organization"),
		BootstrapToken:             os.Getenv("STEWARDMESH_BOOTSTRAP_TOKEN"),
		SessionCookieSecure:        sessionCookieSecure,
		SessionTTL:                 sessionTTL,
		SeedSynthetic:              envBoolDefault("STEWARDMESH_SEED_SYNTHETIC"),
		validationError:            errors.Join(secureErr, ttlErr, oidcVerifiedEmailErr),
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
		if c.CacheKeySecret != "" {
			return errors.New("STEWARDMESH_CACHE_KEY_SECRET must be empty unless STEWARDMESH_CACHE_DRIVER is valkey")
		}
	case CacheDriverValkey:
		if err := cache.ValidateValkeyURL(c.CacheURL); err != nil {
			return fmt.Errorf("STEWARDMESH_CACHE_URL: %w", err)
		}
		if len(c.CacheKeySecret) < 32 {
			return errors.New("STEWARDMESH_CACHE_KEY_SECRET must contain at least 32 bytes for the valkey cache driver")
		}
	default:
		return fmt.Errorf("unsupported STEWARDMESH_CACHE_DRIVER %q", c.CacheDriver)
	}
	if err := c.validateOIDC(); err != nil {
		return err
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
	if err := c.validateSAML(); err != nil {
		return err
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

func (c Config) OIDCEnabled() bool {
	return strings.TrimSpace(c.OIDCIssuerURL) != ""
}

func (c Config) SAMLEnabled() bool {
	return strings.TrimSpace(c.SAMLIDPMetadataURL) != ""
}

func (c Config) SAMLMetadataURL() string {
	return strings.TrimRight(c.AllowedOrigin, "/") + "/api/v1/auth/saml/metadata"
}

func (c Config) SAMLACSURL() string {
	return strings.TrimRight(c.AllowedOrigin, "/") + "/api/v1/auth/saml/acs"
}

func (c Config) EffectiveSAMLEntityID() string {
	if value := strings.TrimSpace(c.SAMLEntityID); value != "" {
		return value
	}
	return c.SAMLMetadataURL()
}

func (c Config) validateSAML() error {
	if !c.SAMLEnabled() {
		if c.SAMLEntityID != "" || c.SAMLSPCertificateFile != "" || c.SAMLSPPrivateKeyFile != "" ||
			c.SAMLEmailAttribute != "" || c.SAMLDisplayNameAttribute != "" || c.SAMLAdministratorAttribute != "" ||
			len(c.SAMLAdministratorValues) > 0 {
			return errors.New("STEWARDMESH_SAML_IDP_METADATA_URL is required when SAML settings are configured")
		}
		return nil
	}
	if _, err := validateOIDCURL(c.SAMLIDPMetadataURL, false); err != nil {
		return fmt.Errorf("STEWARDMESH_SAML_IDP_METADATA_URL: %w", err)
	}
	if strings.TrimSpace(c.SAMLSPCertificateFile) == "" || strings.TrimSpace(c.SAMLSPPrivateKeyFile) == "" {
		return errors.New("STEWARDMESH_SAML_SP_CERTIFICATE_FILE and STEWARDMESH_SAML_SP_PRIVATE_KEY_FILE are required")
	}
	if strings.ContainsRune(c.SAMLSPCertificateFile, '\x00') || strings.ContainsRune(c.SAMLSPPrivateKeyFile, '\x00') {
		return errors.New("SAML certificate and private key file paths must be valid")
	}
	entityID := c.EffectiveSAMLEntityID()
	parsedEntityID, err := url.Parse(entityID)
	if err != nil || !parsedEntityID.IsAbs() || parsedEntityID.User != nil || len(entityID) > 2048 {
		return errors.New("STEWARDMESH_SAML_ENTITY_ID must be an absolute URI without credentials")
	}
	for key, attribute := range map[string]string{
		"STEWARDMESH_SAML_EMAIL_ATTRIBUTE":         c.SAMLEmailAttribute,
		"STEWARDMESH_SAML_DISPLAY_NAME_ATTRIBUTE":  c.SAMLDisplayNameAttribute,
		"STEWARDMESH_SAML_ADMINISTRATOR_ATTRIBUTE": c.SAMLAdministratorAttribute,
	} {
		if len(attribute) > 512 || strings.ContainsAny(attribute, "\r\n") {
			return fmt.Errorf("%s must be a single-line value of at most 512 characters", key)
		}
	}
	if len(c.SAMLAdministratorValues) > 0 && strings.TrimSpace(c.SAMLAdministratorAttribute) == "" {
		return errors.New("STEWARDMESH_SAML_ADMINISTRATOR_ATTRIBUTE is required when administrator values are configured")
	}
	for _, value := range c.SAMLAdministratorValues {
		if value == "" || len(value) > 512 {
			return errors.New("STEWARDMESH_SAML_ADMINISTRATOR_VALUES contains an invalid value")
		}
	}
	return nil
}

func (c Config) validateOIDC() error {
	if !c.OIDCEnabled() {
		if c.OIDCClientID != "" || c.OIDCClientSecret != "" || c.OIDCRedirectURL != "" ||
			c.OIDCTransactionSecret != "" || c.OIDCAdministratorClaim != "" || len(c.OIDCAdministratorValues) > 0 {
			return errors.New("STEWARDMESH_OIDC_ISSUER_URL is required when OpenID Connect settings are configured")
		}
		return nil
	}
	if _, err := validateOIDCURL(c.OIDCIssuerURL, false); err != nil {
		return fmt.Errorf("STEWARDMESH_OIDC_ISSUER_URL: %w", err)
	}
	if strings.TrimSpace(c.OIDCClientID) == "" || c.OIDCClientSecret == "" {
		return errors.New("STEWARDMESH_OIDC_CLIENT_ID and STEWARDMESH_OIDC_CLIENT_SECRET are required")
	}
	if len(c.OIDCTransactionSecret) < 32 {
		return errors.New("STEWARDMESH_OIDC_TRANSACTION_SECRET must contain at least 32 bytes")
	}
	redirect, err := validateOIDCURL(c.OIDCRedirectURL, true)
	if err != nil {
		return fmt.Errorf("STEWARDMESH_OIDC_REDIRECT_URL: %w", err)
	}
	if redirect.Path != "/api/v1/auth/oidc/callback" {
		return errors.New("STEWARDMESH_OIDC_REDIRECT_URL must use /api/v1/auth/oidc/callback")
	}
	allowedOrigin, err := url.Parse(c.AllowedOrigin)
	if err != nil || redirect.Scheme != allowedOrigin.Scheme || redirect.Host != allowedOrigin.Host {
		return errors.New("STEWARDMESH_OIDC_REDIRECT_URL must use the configured allowed origin")
	}
	claim := strings.TrimSpace(c.OIDCAdministratorClaim)
	if claim != "" && (len(claim) > 128 || strings.ContainsAny(claim, " \t\r\n")) {
		return errors.New("STEWARDMESH_OIDC_ADMINISTRATOR_CLAIM must be a single claim name of at most 128 characters")
	}
	for _, value := range c.OIDCAdministratorValues {
		if value == "" || len(value) > 512 {
			return errors.New("STEWARDMESH_OIDC_ADMINISTRATOR_VALUES contains an invalid value")
		}
	}
	return nil
}

func validateOIDCURL(raw string, redirect bool) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("must be an absolute URL without credentials, query, or fragment")
	}
	if parsed.Scheme != "https" {
		host := parsed.Hostname()
		address := net.ParseIP(host)
		if parsed.Scheme != "http" || !(strings.EqualFold(host, "localhost") || address != nil && address.IsLoopback()) {
			return nil, errors.New("must use HTTPS except for loopback development")
		}
	}
	if redirect && parsed.Path == "" {
		return nil, errors.New("must include a callback path")
	}
	return parsed, nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envCSV(key string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil
	}
	seen := make(map[string]struct{})
	values := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		values = append(values, item)
	}
	return values
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
