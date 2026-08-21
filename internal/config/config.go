// Package config loads and validates provider-neutral StewardMesh settings.
// Requirements: REQ-FOUNDATION-001, REQ-DIRECTORY-EXPANSION-003, REQ-DIRECTORY-EXPANSION-004, REQ-DIRECTORY-EXPANSION-005, REQ-DIRECTORY-EXPANSION-006, REQ-DIRECTORY-EXPANSION-007, REQ-STORAGE-001, REQ-REACH-001, REQ-PLATFORM-VALKEY-001, SEC-GUARD-001, SEC-HTTP-001.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/cache"
	"github.com/maxlemke/stewardmesh/internal/directoryexpansion"
	"github.com/maxlemke/stewardmesh/internal/directoryexpansion/entra"
	"github.com/maxlemke/stewardmesh/internal/directoryexpansion/sailpoint"
	"github.com/maxlemke/stewardmesh/internal/storage"
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

type StorageDriver string

const (
	StorageDriverLocal StorageDriver = "local"
	StorageDriverS3    StorageDriver = "s3"
)

type Config struct {
	Addr                        string
	DataDir                     string
	BlobDir                     string
	StorageDriver               StorageDriver
	BlobMaximumBytes            int64
	BlobDownloadTTL             time.Duration
	S3Bucket                    string
	S3Region                    string
	S3EndpointURL               string
	S3ForcePathStyle            bool
	S3AccessKeyID               string
	S3SecretAccessKey           string
	S3SessionToken              string
	S3RoleARN                   string
	S3Encryption                string
	S3KMSKeyID                  string
	DatabaseURL                 string
	RepositoryDriver            RepositoryDriver
	CacheDriver                 CacheDriver
	CacheURL                    string
	CacheKeySecret              string
	OIDCIssuerURL               string
	OIDCClientID                string
	OIDCClientSecret            string
	OIDCRedirectURL             string
	OIDCTransactionSecret       string
	OIDCAdministratorClaim      string
	OIDCAdministratorValues     []string
	OIDCRequireVerifiedEmail    bool
	SAMLIDPMetadataURL          string
	SAMLEntityID                string
	SAMLSPCertificateFile       string
	SAMLSPPrivateKeyFile        string
	SAMLEmailAttribute          string
	SAMLDisplayNameAttribute    string
	SAMLAdministratorAttribute  string
	SAMLAdministratorValues     []string
	EntraSourceSystemID         string
	EntraTenantID               string
	EntraClientID               string
	EntraClientSecret           string
	SailPointSourceSystemID     string
	SailPointBaseURL            string
	SailPointClientID           string
	SailPointClientSecret       string
	ReachEndpointsFile          string
	ReachSecretPrefix           string
	AllowedOrigin               string
	OrganizationID              string
	OrganizationName            string
	ExchangeSourceSystemID      string
	BootstrapToken              string
	SessionCookieSecure         bool
	SessionTTL                  time.Duration
	SeedSynthetic               bool
	SeedCampus                  bool
	GrouperURL                  string
	GrouperSourceSystemID       string
	GrouperUsername             string
	GrouperPassword             string
	GrouperBearerToken          string
	GrouperConfigRevision       string
	GrouperPageSize             int
	GrouperMaximumResponseBytes int64
	GrouperTimeout              time.Duration
	GrouperAllowPrivateNetwork  bool
	PeopleSoftSourceSystemID    string
	PeopleSoftBaseURL           string
	PeopleSoftUsername          string
	PeopleSoftPassword          string
	PeopleSoftBearerToken       string
	PeopleSoftQueryOwner        string
	PeopleSoftOrganizationQuery string
	PeopleSoftLocationQuery     string
	PeopleSoftBuildingQuery     string
	PeopleSoftDepartmentQuery   string
	PeopleSoftFieldMappingsJSON string
	PeopleSoftMaximumRows       int
	PeopleSoftResponseBytes     int64
	PeopleSoftTimeout           time.Duration
	PeopleSoftAllowPrivate      bool
	validationError             error
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
	organizationID := envOr("STEWARDMESH_ORGANIZATION_ID", "local-organization")
	storageDriver := StorageDriver(envOr("STEWARDMESH_STORAGE_DRIVER", string(StorageDriverLocal)))
	sessionCookieSecure, secureErr := envBool(
		"STEWARDMESH_SESSION_COOKIE_SECURE",
		strings.HasPrefix(allowedOrigin, "https://"),
	)
	sessionTTL, ttlErr := envDuration("STEWARDMESH_SESSION_TTL", 12*time.Hour)
	blobDownloadTTL, blobTTLErr := envDuration("STEWARDMESH_BLOB_DOWNLOAD_TTL", 5*time.Minute)
	blobMaximumBytes, blobSizeErr := envInt64("STEWARDMESH_BLOB_MAXIMUM_BYTES", 25<<20)
	s3ForcePathStyle, s3PathStyleErr := envBool("STEWARDMESH_S3_FORCE_PATH_STYLE", false)
	oidcRequireVerifiedEmail, oidcVerifiedEmailErr := envBool("STEWARDMESH_OIDC_REQUIRE_VERIFIED_EMAIL", true)
	seedSynthetic, seedSyntheticErr := envBool("STEWARDMESH_SEED_SYNTHETIC", false)
	seedCampus, seedCampusErr := envBool("STEWARDMESH_SEED_CAMPUS", false)
	grouperAllowPrivate, grouperPrivateErr := envBool("STEWARDMESH_GROUPER_ALLOW_PRIVATE_NETWORK", false)
	grouperTimeout, grouperTimeoutErr := envDuration("STEWARDMESH_GROUPER_TIMEOUT", directoryexpansion.DefaultGrouperTimeout)
	grouperPageSize, grouperPageErr := envInt("STEWARDMESH_GROUPER_PAGE_SIZE", directoryexpansion.DefaultGrouperPageSize)
	grouperResponseBytes, grouperResponseErr := envInt64("STEWARDMESH_GROUPER_MAXIMUM_RESPONSE_BYTES", directoryexpansion.DefaultGrouperResponseBytes)
	peopleSoftAllowPrivate, peopleSoftPrivateErr := envBool("STEWARDMESH_PEOPLESOFT_ALLOW_PRIVATE_NETWORK", false)
	peopleSoftTimeout, peopleSoftTimeoutErr := envDuration("STEWARDMESH_PEOPLESOFT_TIMEOUT", directoryexpansion.DefaultPeopleSoftTimeout)
	peopleSoftMaximumRows, peopleSoftRowsErr := envInt("STEWARDMESH_PEOPLESOFT_MAXIMUM_ROWS", directoryexpansion.DefaultPeopleSoftMaximumRows)
	peopleSoftResponseBytes, peopleSoftResponseErr := envInt64("STEWARDMESH_PEOPLESOFT_MAXIMUM_RESPONSE_BYTES", directoryexpansion.DefaultPeopleSoftResponseBytes)
	grouperURL := strings.TrimSpace(os.Getenv("STEWARDMESH_GROUPER_URL"))
	grouperSourceSystemID := strings.TrimSpace(os.Getenv("STEWARDMESH_GROUPER_SOURCE_SYSTEM_ID"))
	grouperRevision := strings.TrimSpace(os.Getenv("STEWARDMESH_GROUPER_CONFIG_REVISION"))
	if grouperURL != "" {
		if grouperSourceSystemID == "" {
			grouperSourceSystemID = "grouper-primary"
		}
		if grouperRevision == "" {
			grouperRevision = "v1"
		}
	}
	s3Encryption := os.Getenv("STEWARDMESH_S3_ENCRYPTION")
	if storageDriver == StorageDriverS3 && s3Encryption == "" {
		s3Encryption = "AES256"
	}
	return Config{
		Addr:                        envOr("STEWARDMESH_ADDR", "127.0.0.1:8080"),
		DataDir:                     envOr("STEWARDMESH_DATA_DIR", "./data"),
		BlobDir:                     envOr("STEWARDMESH_BLOB_DIR", "./storage"),
		StorageDriver:               storageDriver,
		BlobMaximumBytes:            blobMaximumBytes,
		BlobDownloadTTL:             blobDownloadTTL,
		S3Bucket:                    os.Getenv("STEWARDMESH_S3_BUCKET"),
		S3Region:                    os.Getenv("STEWARDMESH_S3_REGION"),
		S3EndpointURL:               os.Getenv("STEWARDMESH_S3_ENDPOINT_URL"),
		S3ForcePathStyle:            s3ForcePathStyle,
		S3AccessKeyID:               os.Getenv("STEWARDMESH_S3_ACCESS_KEY_ID"),
		S3SecretAccessKey:           os.Getenv("STEWARDMESH_S3_SECRET_ACCESS_KEY"),
		S3SessionToken:              os.Getenv("STEWARDMESH_S3_SESSION_TOKEN"),
		S3RoleARN:                   os.Getenv("STEWARDMESH_S3_ROLE_ARN"),
		S3Encryption:                s3Encryption,
		S3KMSKeyID:                  os.Getenv("STEWARDMESH_S3_KMS_KEY_ID"),
		DatabaseURL:                 envOr("STEWARDMESH_DATABASE_URL", ""),
		RepositoryDriver:            RepositoryDriver(envOr("STEWARDMESH_REPOSITORY_DRIVER", string(RepositoryDriverPostgres))),
		CacheDriver:                 CacheDriver(envOr("STEWARDMESH_CACHE_DRIVER", string(CacheDriverNone))),
		CacheURL:                    os.Getenv("STEWARDMESH_CACHE_URL"),
		CacheKeySecret:              os.Getenv("STEWARDMESH_CACHE_KEY_SECRET"),
		OIDCIssuerURL:               os.Getenv("STEWARDMESH_OIDC_ISSUER_URL"),
		OIDCClientID:                os.Getenv("STEWARDMESH_OIDC_CLIENT_ID"),
		OIDCClientSecret:            os.Getenv("STEWARDMESH_OIDC_CLIENT_SECRET"),
		OIDCRedirectURL:             os.Getenv("STEWARDMESH_OIDC_REDIRECT_URL"),
		OIDCTransactionSecret:       os.Getenv("STEWARDMESH_OIDC_TRANSACTION_SECRET"),
		OIDCAdministratorClaim:      os.Getenv("STEWARDMESH_OIDC_ADMINISTRATOR_CLAIM"),
		OIDCAdministratorValues:     envCSV("STEWARDMESH_OIDC_ADMINISTRATOR_VALUES"),
		OIDCRequireVerifiedEmail:    oidcRequireVerifiedEmail,
		SAMLIDPMetadataURL:          os.Getenv("STEWARDMESH_SAML_IDP_METADATA_URL"),
		SAMLEntityID:                os.Getenv("STEWARDMESH_SAML_ENTITY_ID"),
		SAMLSPCertificateFile:       os.Getenv("STEWARDMESH_SAML_SP_CERTIFICATE_FILE"),
		SAMLSPPrivateKeyFile:        os.Getenv("STEWARDMESH_SAML_SP_PRIVATE_KEY_FILE"),
		SAMLEmailAttribute:          os.Getenv("STEWARDMESH_SAML_EMAIL_ATTRIBUTE"),
		SAMLDisplayNameAttribute:    os.Getenv("STEWARDMESH_SAML_DISPLAY_NAME_ATTRIBUTE"),
		SAMLAdministratorAttribute:  os.Getenv("STEWARDMESH_SAML_ADMINISTRATOR_ATTRIBUTE"),
		SAMLAdministratorValues:     envCSV("STEWARDMESH_SAML_ADMINISTRATOR_VALUES"),
		EntraSourceSystemID:         envOr("STEWARDMESH_ENTRA_SOURCE_SYSTEM_ID", "entra"),
		EntraTenantID:               os.Getenv("STEWARDMESH_ENTRA_TENANT_ID"),
		EntraClientID:               os.Getenv("STEWARDMESH_ENTRA_CLIENT_ID"),
		EntraClientSecret:           os.Getenv("STEWARDMESH_ENTRA_CLIENT_SECRET"),
		SailPointSourceSystemID:     envOr("STEWARDMESH_SAILPOINT_SOURCE_SYSTEM_ID", "sailpoint"),
		SailPointBaseURL:            os.Getenv("STEWARDMESH_SAILPOINT_BASE_URL"),
		SailPointClientID:           os.Getenv("STEWARDMESH_SAILPOINT_CLIENT_ID"),
		SailPointClientSecret:       os.Getenv("STEWARDMESH_SAILPOINT_CLIENT_SECRET"),
		ReachEndpointsFile:          os.Getenv("STEWARDMESH_REACH_ENDPOINTS_FILE"),
		ReachSecretPrefix:           envOr("STEWARDMESH_REACH_SECRET_PREFIX", "STEWARDMESH_REACH_SECRET_"),
		AllowedOrigin:               allowedOrigin,
		OrganizationID:              organizationID,
		OrganizationName:            envOr("STEWARDMESH_ORGANIZATION_NAME", "StewardMesh Local Organization"),
		ExchangeSourceSystemID:      envOr("STEWARDMESH_EXCHANGE_SOURCE_SYSTEM_ID", organizationID),
		BootstrapToken:              os.Getenv("STEWARDMESH_BOOTSTRAP_TOKEN"),
		SessionCookieSecure:         sessionCookieSecure,
		SessionTTL:                  sessionTTL,
		SeedSynthetic:               seedSynthetic,
		SeedCampus:                  seedCampus,
		GrouperURL:                  grouperURL,
		GrouperSourceSystemID:       grouperSourceSystemID,
		GrouperUsername:             os.Getenv("STEWARDMESH_GROUPER_USERNAME"),
		GrouperPassword:             os.Getenv("STEWARDMESH_GROUPER_PASSWORD"),
		GrouperBearerToken:          os.Getenv("STEWARDMESH_GROUPER_BEARER_TOKEN"),
		GrouperConfigRevision:       grouperRevision,
		GrouperPageSize:             grouperPageSize,
		GrouperMaximumResponseBytes: grouperResponseBytes,
		GrouperTimeout:              grouperTimeout,
		GrouperAllowPrivateNetwork:  grouperAllowPrivate,
		PeopleSoftSourceSystemID:    envOr("STEWARDMESH_PEOPLESOFT_SOURCE_SYSTEM_ID", "peoplesoft"),
		PeopleSoftBaseURL:           os.Getenv("STEWARDMESH_PEOPLESOFT_BASE_URL"),
		PeopleSoftUsername:          os.Getenv("STEWARDMESH_PEOPLESOFT_USERNAME"),
		PeopleSoftPassword:          os.Getenv("STEWARDMESH_PEOPLESOFT_PASSWORD"),
		PeopleSoftBearerToken:       os.Getenv("STEWARDMESH_PEOPLESOFT_BEARER_TOKEN"),
		PeopleSoftQueryOwner:        envOr("STEWARDMESH_PEOPLESOFT_QUERY_OWNER", "public"),
		PeopleSoftOrganizationQuery: os.Getenv("STEWARDMESH_PEOPLESOFT_ORGANIZATION_QUERY"),
		PeopleSoftLocationQuery:     os.Getenv("STEWARDMESH_PEOPLESOFT_LOCATION_QUERY"),
		PeopleSoftBuildingQuery:     os.Getenv("STEWARDMESH_PEOPLESOFT_BUILDING_QUERY"),
		PeopleSoftDepartmentQuery:   os.Getenv("STEWARDMESH_PEOPLESOFT_DEPARTMENT_QUERY"),
		PeopleSoftFieldMappingsJSON: os.Getenv("STEWARDMESH_PEOPLESOFT_FIELD_MAPPINGS_JSON"),
		PeopleSoftMaximumRows:       peopleSoftMaximumRows,
		PeopleSoftResponseBytes:     peopleSoftResponseBytes,
		PeopleSoftTimeout:           peopleSoftTimeout,
		PeopleSoftAllowPrivate:      peopleSoftAllowPrivate,
		validationError: errors.Join(secureErr, ttlErr, blobTTLErr, blobSizeErr, s3PathStyleErr, oidcVerifiedEmailErr,
			seedSyntheticErr, seedCampusErr, grouperPrivateErr, grouperTimeoutErr, grouperPageErr, grouperResponseErr,
			peopleSoftPrivateErr, peopleSoftTimeoutErr, peopleSoftRowsErr, peopleSoftResponseErr),
	}
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
	if c.SeedSynthetic && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(c.OrganizationID)), "demo-") {
		return errors.New("STEWARDMESH_SEED_SYNTHETIC requires a demo-* STEWARDMESH_ORGANIZATION_ID")
	}
	if c.SeedCampus && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(c.OrganizationID)), "demo-") {
		return errors.New("STEWARDMESH_SEED_CAMPUS requires a demo-* STEWARDMESH_ORGANIZATION_ID")
	}
	if !stableConfigurationID(c.ExchangeSourceSystemID) {
		return errors.New("STEWARDMESH_EXCHANGE_SOURCE_SYSTEM_ID must be a stable identifier")
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
	if c.BlobMaximumBytes <= 0 || c.BlobMaximumBytes > 5<<30 {
		return errors.New("STEWARDMESH_BLOB_MAXIMUM_BYTES must be between 1 byte and 5 GiB")
	}
	if c.BlobDownloadTTL < time.Minute || c.BlobDownloadTTL > 15*time.Minute {
		return errors.New("STEWARDMESH_BLOB_DOWNLOAD_TTL must be between 1m and 15m")
	}
	switch c.StorageDriver {
	case StorageDriverLocal:
		if strings.TrimSpace(c.BlobDir) == "" {
			return errors.New("STEWARDMESH_BLOB_DIR is required for local storage")
		}
		if c.S3Bucket != "" || c.S3Region != "" || c.S3EndpointURL != "" || c.S3ForcePathStyle ||
			c.S3AccessKeyID != "" || c.S3SecretAccessKey != "" || c.S3SessionToken != "" ||
			c.S3RoleARN != "" || c.S3Encryption != "" || c.S3KMSKeyID != "" {
			return errors.New("S3 settings must be empty unless STEWARDMESH_STORAGE_DRIVER is s3")
		}
	case StorageDriverS3:
		if err := storage.ValidateS3Config(c.S3Config()); err != nil {
			return fmt.Errorf("invalid S3 storage configuration: %w", err)
		}
	default:
		return fmt.Errorf("unsupported STEWARDMESH_STORAGE_DRIVER %q", c.StorageDriver)
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
		if origin.Scheme == "http" && !c.SessionCookieSecure && !isLoopbackHost(origin.Hostname()) {
			return errors.New("STEWARDMESH_SESSION_COOKIE_SECURE must be true for a non-loopback HTTP origin")
		}
	}
	if err := c.validateSAML(); err != nil {
		return err
	}
	if err := c.validateEntra(); err != nil {
		return err
	}
	if err := c.validateSailPoint(); err != nil {
		return err
	}
	if err := c.validateGrouper(); err != nil {
		return err
	}
	if err := c.validatePeopleSoft(); err != nil {
		return err
	}
	if len(c.ReachEndpointsFile) > 1024 || strings.ContainsRune(c.ReachEndpointsFile, '\x00') {
		return errors.New("STEWARDMESH_REACH_ENDPOINTS_FILE is invalid")
	}
	if !regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,63}$`).MatchString(c.ReachSecretPrefix) {
		return errors.New("STEWARDMESH_REACH_SECRET_PREFIX must be an uppercase environment-variable prefix")
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

func (c Config) GrouperEnabled() bool { return strings.TrimSpace(c.GrouperURL) != "" }

func (c Config) GrouperConnectorConfig() directoryexpansion.GrouperConnectorConfig {
	return directoryexpansion.GrouperConnectorConfig{SourceSystemID: c.GrouperSourceSystemID, BaseURL: c.GrouperURL,
		Username: c.GrouperUsername, Password: c.GrouperPassword, BearerToken: c.GrouperBearerToken,
		ConfigRevision: c.GrouperConfigRevision, PageSize: c.GrouperPageSize,
		MaximumResponseBytes: c.GrouperMaximumResponseBytes, Timeout: c.GrouperTimeout,
		AllowPrivateNetwork: c.GrouperAllowPrivateNetwork}
}

func (c Config) validateGrouper() error {
	if !c.GrouperEnabled() {
		if c.GrouperSourceSystemID != "" || c.GrouperUsername != "" || c.GrouperPassword != "" || c.GrouperBearerToken != "" ||
			c.GrouperConfigRevision != "" || c.GrouperAllowPrivateNetwork {
			return errors.New("STEWARDMESH_GROUPER_URL is required when Grouper settings are configured")
		}
		return nil
	}
	if _, err := directoryexpansion.NewGrouperConnector(c.GrouperConnectorConfig()); err != nil {
		return errors.New("Grouper connector settings are invalid")
	}
	return nil
}

func stableConfigurationID(value string) bool {
	if strings.TrimSpace(value) != value || len(value) == 0 || len(value) > 128 {
		return false
	}
	for index, character := range value {
		if (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') || (index > 0 && strings.ContainsRune("._:-", character)) {
			continue
		}
		return false
	}
	return true
}

func (c Config) PeopleSoftEnabled() bool { return strings.TrimSpace(c.PeopleSoftBaseURL) != "" }

func (c Config) PeopleSoftConnectorConfig() directoryexpansion.PeopleSoftConnectorConfig {
	return directoryexpansion.PeopleSoftConnectorConfig{
		SourceSystemID: c.PeopleSoftSourceSystemID, BaseURL: c.PeopleSoftBaseURL,
		Username: c.PeopleSoftUsername, Password: c.PeopleSoftPassword, BearerToken: c.PeopleSoftBearerToken,
		QueryOwner: c.PeopleSoftQueryOwner, OrganizationQuery: c.PeopleSoftOrganizationQuery,
		LocationQuery: c.PeopleSoftLocationQuery, BuildingQuery: c.PeopleSoftBuildingQuery,
		DepartmentQuery: c.PeopleSoftDepartmentQuery, FieldMappingsJSON: c.PeopleSoftFieldMappingsJSON,
		MaximumRows: c.PeopleSoftMaximumRows, MaximumResponseBytes: c.PeopleSoftResponseBytes,
		Timeout: c.PeopleSoftTimeout, AllowPrivateNetwork: c.PeopleSoftAllowPrivate,
	}
}

func (c Config) validatePeopleSoft() error {
	if !c.PeopleSoftEnabled() {
		if c.PeopleSoftUsername != "" || c.PeopleSoftPassword != "" || c.PeopleSoftBearerToken != "" ||
			c.PeopleSoftOrganizationQuery != "" || c.PeopleSoftLocationQuery != "" ||
			c.PeopleSoftBuildingQuery != "" || c.PeopleSoftDepartmentQuery != "" || c.PeopleSoftFieldMappingsJSON != "" ||
			c.PeopleSoftAllowPrivate {
			return errors.New("STEWARDMESH_PEOPLESOFT_BASE_URL is required when PeopleSoft settings are configured")
		}
		return nil
	}
	if _, err := directoryexpansion.NewPeopleSoftConnector(c.PeopleSoftConnectorConfig()); err != nil {
		return errors.New("PeopleSoft Query Access Service configuration is invalid")
	}
	return nil
}

func (c Config) OIDCEnabled() bool {
	return strings.TrimSpace(c.OIDCIssuerURL) != ""
}

func (c Config) S3Config() storage.S3Config {
	return storage.S3Config{
		Bucket: c.S3Bucket, Region: c.S3Region, EndpointURL: c.S3EndpointURL,
		ForcePathStyle: c.S3ForcePathStyle, AccessKeyID: c.S3AccessKeyID,
		SecretAccessKey: c.S3SecretAccessKey, SessionToken: c.S3SessionToken,
		RoleARN: c.S3RoleARN, Encryption: c.S3Encryption, KMSKeyID: c.S3KMSKeyID,
		MaximumBytes: c.BlobMaximumBytes,
	}
}

func (c Config) SAMLEnabled() bool {
	return strings.TrimSpace(c.SAMLIDPMetadataURL) != ""
}

func (c Config) EntraEnabled() bool {
	return strings.TrimSpace(c.EntraTenantID) != ""
}

func (c Config) EntraConfig() entra.Config {
	return entra.Config{
		SourceSystemID: c.EntraSourceSystemID,
		TenantID:       c.EntraTenantID,
		ClientID:       c.EntraClientID,
		ClientSecret:   c.EntraClientSecret,
	}
}

func (c Config) validateEntra() error {
	if !c.EntraEnabled() {
		if strings.TrimSpace(c.EntraClientID) != "" || c.EntraClientSecret != "" {
			return errors.New("STEWARDMESH_ENTRA_TENANT_ID is required when Microsoft Entra credentials are configured")
		}
		return nil
	}
	if err := entra.ValidateConfig(c.EntraConfig()); err != nil {
		return errors.New("Microsoft Entra tenant and application credential configuration is invalid")
	}
	return nil
}

func (c Config) SailPointEnabled() bool {
	return strings.TrimSpace(c.SailPointBaseURL) != ""
}

func (c Config) SailPointConfig() sailpoint.Config {
	return sailpoint.Config{
		SourceSystemID: c.SailPointSourceSystemID,
		BaseURL:        c.SailPointBaseURL,
		ClientID:       c.SailPointClientID,
		ClientSecret:   c.SailPointClientSecret,
	}
}

func (c Config) validateSailPoint() error {
	if !c.SailPointEnabled() {
		if strings.TrimSpace(c.SailPointClientID) != "" || c.SailPointClientSecret != "" {
			return errors.New("STEWARDMESH_SAILPOINT_BASE_URL is required when SailPoint credentials are configured")
		}
		return nil
	}
	if err := sailpoint.ValidateConfig(c.SailPointConfig()); err != nil {
		return errors.New("SailPoint endpoint and client credential configuration is invalid")
	}
	return nil
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

func envInt64(key string, fallback int64) (int64, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return parsed, nil
}

func envInt(key string, fallback int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, strconv.IntSize)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return int(parsed), nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
