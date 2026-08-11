// Package application constructs StewardMesh's transport-neutral HTTP
// application and owns the lifecycle of its shared runtime dependencies.
// Requirements: REQ-FOUNDATION-001, REQ-PLATFORM-VALKEY-001, SEC-GUARD-001.
package application

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/maxlemke/stewardmesh/internal/bootstrap"
	"github.com/maxlemke/stewardmesh/internal/cache"
	"github.com/maxlemke/stewardmesh/internal/config"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/httpapi"
	"github.com/maxlemke/stewardmesh/internal/identity"
	"github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/repository"
	postgresrepository "github.com/maxlemke/stewardmesh/internal/repository/postgres"
	"github.com/maxlemke/stewardmesh/internal/storage"
)

const maximumLocalBlobBytes = 25 << 20

// Options selects construction behavior that differs by deployment transport.
// Long-running local servers currently run migrations at startup. Future
// short-lived runtimes must leave this disabled and run migrations as a
// separate deployment operation.
type Options struct {
	RunMigrations bool
}

// Application is a reusable StewardMesh HTTP application. It does not own an
// HTTP listener, process signals, or a deployment-specific transport adapter.
type Application struct {
	handler         http.Handler
	organization    bootstrap.Organization
	closeCache      func() error
	closeFoundation func() error
	closeOnce       sync.Once
	closeErr        error
}

// New validates configuration and constructs the shared repositories,
// services, cache-backed protection, and HTTP handler once for reuse by a
// long-running server or a future Lambda adapter.
func New(ctx context.Context, cfg config.Config, options Options) (*Application, error) {
	if ctx == nil {
		return nil, errors.New("construct application: context is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("construct application: %w", err)
	}
	var oidcFlow *identity.OIDCFlow
	if cfg.OIDCEnabled() {
		oidcClient, err := identity.NewOIDCClient(ctx, identity.OIDCConfig{
			IssuerURL:            cfg.OIDCIssuerURL,
			ClientID:             cfg.OIDCClientID,
			ClientSecret:         cfg.OIDCClientSecret,
			RedirectURL:          cfg.OIDCRedirectURL,
			AdministratorClaim:   cfg.OIDCAdministratorClaim,
			AdministratorValues:  cfg.OIDCAdministratorValues,
			RequireVerifiedEmail: cfg.OIDCRequireVerifiedEmail,
		})
		cfg.OIDCClientSecret = ""
		if err != nil {
			return nil, err
		}
		oidcFlow, err = identity.NewOIDCFlow(oidcClient, cfg.OIDCTransactionSecret, nil)
		cfg.OIDCTransactionSecret = ""
		if err != nil {
			return nil, fmt.Errorf("initialize OpenID Connect flow: %w", err)
		}
	}
	var samlFlow *identity.SAMLFlow
	if cfg.SAMLEnabled() {
		samlClient, err := identity.NewSAMLClient(ctx, identity.SAMLConfig{
			IDPMetadataURL:         cfg.SAMLIDPMetadataURL,
			EntityID:               cfg.EffectiveSAMLEntityID(),
			MetadataURL:            cfg.SAMLMetadataURL(),
			ACSURL:                 cfg.SAMLACSURL(),
			CertificateFile:        cfg.SAMLSPCertificateFile,
			PrivateKeyFile:         cfg.SAMLSPPrivateKeyFile,
			EmailAttribute:         cfg.SAMLEmailAttribute,
			DisplayNameAttribute:   cfg.SAMLDisplayNameAttribute,
			AdministratorAttribute: cfg.SAMLAdministratorAttribute,
			AdministratorValues:    cfg.SAMLAdministratorValues,
		})
		if err != nil {
			return nil, err
		}
		samlFlow, err = identity.NewSAMLFlow(samlClient, nil)
		if err != nil {
			return nil, fmt.Errorf("initialize SAML flow: %w", err)
		}
	}

	runtime, err := initializeFoundation(ctx, cfg, options.RunMigrations)
	if err != nil {
		return nil, fmt.Errorf("initialize foundation: %w", err)
	}
	application := &Application{
		organization:    runtime.organization,
		closeCache:      func() error { return nil },
		closeFoundation: runtime.close,
	}
	fail := func(err error) (*Application, error) {
		return nil, errors.Join(err, application.Close())
	}

	attemptLimiter, closeCache, err := initializeAttemptLimiter(ctx, cfg)
	cfg.CacheURL = ""
	cfg.CacheKeySecret = ""
	if err != nil {
		return fail(fmt.Errorf("initialize login protection: %w", err))
	}
	application.closeCache = closeCache

	assets := repository.NewMemoryAssetRepository()
	catalog := repository.NewMemoryCatalog()
	blobStore, err := storage.NewLocalBlobStore(cfg.BlobDir, maximumLocalBlobBytes)
	if err != nil {
		return fail(fmt.Errorf("initialize blob store: %w", err))
	}
	guardService, err := guard.NewService(
		runtime.guardStore,
		guard.NewArgon2idHasher(),
		runtime.auditor,
		attemptLimiter,
		guard.ServiceConfig{
			OrganizationID: cfg.OrganizationID,
			BootstrapToken: cfg.BootstrapToken,
			SessionTTL:     cfg.SessionTTL,
		},
	)
	cfg.BootstrapToken = ""
	if err != nil {
		return fail(fmt.Errorf("initialize Guard: %w", err))
	}
	peopleService, err := people.NewService(runtime.peopleStore, assets, runtime.auditor, people.ServiceConfig{
		OrganizationID: cfg.OrganizationID,
	})
	if err != nil {
		return fail(fmt.Errorf("initialize People: %w", err))
	}

	application.handler = httpapi.NewServer(httpapi.Dependencies{
		Assets:              assets,
		People:              peopleService,
		Tags:                catalog,
		Goals:               catalog,
		Blobs:               blobStore,
		Guard:               guardService,
		OIDC:                oidcFlow,
		SAML:                samlFlow,
		SessionCookieSecure: cfg.SessionCookieSecure,
	}, cfg.AllowedOrigin, runtime.organization)
	return application, nil
}

// Handler returns the transport-neutral StewardMesh HTTP handler.
func (a *Application) Handler() http.Handler {
	if a == nil {
		return nil
	}
	return a.handler
}

// Organization returns the durable organization loaded during construction.
func (a *Application) Organization() bootstrap.Organization {
	if a == nil {
		return bootstrap.Organization{}
	}
	return a.organization
}

// Close releases the cache before the authoritative repository. It is safe to
// call more than once so transports and tests can share lifecycle handling.
func (a *Application) Close() error {
	if a == nil {
		return nil
	}
	a.closeOnce.Do(func() {
		var closeCacheError, closeFoundationError error
		if a.closeCache != nil {
			closeCacheError = a.closeCache()
		}
		if a.closeFoundation != nil {
			closeFoundationError = a.closeFoundation()
		}
		a.closeErr = errors.Join(closeCacheError, closeFoundationError)
	})
	return a.closeErr
}

type foundationRuntime struct {
	organization bootstrap.Organization
	guardStore   guard.Store
	peopleStore  people.Store
	auditor      foundation.Auditor
	close        func() error
}

func initializeAttemptLimiter(ctx context.Context, cfg config.Config) (guard.AttemptLimiter, func() error, error) {
	closeNothing := func() error { return nil }
	if ctx == nil {
		return nil, closeNothing, errors.New("context is required")
	}
	var (
		store     cache.Store
		keySecret []byte
		err       error
	)
	switch cfg.CacheDriver {
	case config.CacheDriverNone:
		return nil, closeNothing, nil
	case config.CacheDriverMemory:
		store = cache.NewDefaultMemoryStore()
		keySecret = make([]byte, 32)
		if _, err := rand.Read(keySecret); err != nil {
			return nil, closeNothing, fmt.Errorf("initialize memory cache key protection: %w", err)
		}
	case config.CacheDriverValkey:
		store, err = cache.NewValkeyStore(cfg.CacheURL)
		if err != nil {
			return nil, closeNothing, fmt.Errorf("initialize configured Valkey cache: %w", err)
		}
		keySecret = []byte(cfg.CacheKeySecret)
	default:
		return nil, closeNothing, fmt.Errorf("unsupported cache driver %q", cfg.CacheDriver)
	}
	defer clear(keySecret)
	closeStore := store.Close
	if err := store.Ping(ctx); err != nil {
		_ = closeStore()
		return nil, closeNothing, fmt.Errorf("verify configured cache: %w", err)
	}
	namespace, err := cache.NewNamespace("stewardmesh", "v1", cfg.OrganizationID)
	if err != nil {
		_ = closeStore()
		return nil, closeNothing, fmt.Errorf("initialize Guard cache namespace: %w", err)
	}
	limiter, err := guard.NewDefaultCacheAttemptLimiter(store, namespace, keySecret)
	if err != nil {
		_ = closeStore()
		return nil, closeNothing, fmt.Errorf("initialize shared login protection: %w", err)
	}
	return limiter, closeStore, nil
}

func initializeFoundation(ctx context.Context, cfg config.Config, runMigrations bool) (foundationRuntime, error) {
	if ctx == nil {
		return foundationRuntime{}, errors.New("context is required")
	}
	var (
		organizations repository.OrganizationRepository
		guardStore    guard.Store
		peopleStore   people.Store
		auditor       foundation.Auditor = foundation.NopAuditor{}
		closeRuntime                     = func() error { return nil }
	)
	switch cfg.RepositoryDriver {
	case config.RepositoryDriverMemory:
		organizations = repository.NewMemoryOrganizationRepository()
		guardStore = repository.NewMemoryGuardStore()
		peopleStore = repository.NewMemoryPeopleStore()
	case config.RepositoryDriverPostgres:
		database, err := postgresrepository.Open(ctx, cfg.DatabaseURL)
		if err != nil {
			return foundationRuntime{}, err
		}
		closeRuntime = database.Close
		if runMigrations {
			if err := postgresrepository.Migrate(ctx, database); err != nil {
				_ = database.Close()
				return foundationRuntime{}, err
			}
		}
		organizations, err = postgresrepository.NewOrganizationRepository(database)
		if err != nil {
			_ = database.Close()
			return foundationRuntime{}, err
		}
		auditor, err = postgresrepository.NewAuditor(database)
		if err != nil {
			_ = database.Close()
			return foundationRuntime{}, err
		}
		guardStore, err = postgresrepository.NewGuardStore(database)
		if err != nil {
			_ = database.Close()
			return foundationRuntime{}, err
		}
		peopleStore, err = postgresrepository.NewPeopleStore(database)
		if err != nil {
			_ = database.Close()
			return foundationRuntime{}, err
		}
	default:
		return foundationRuntime{}, fmt.Errorf("unsupported repository driver %q", cfg.RepositoryDriver)
	}

	service, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		_ = closeRuntime()
		return foundationRuntime{}, err
	}
	organization, created, err := service.EnsureOrganization(ctx, cfg.OrganizationID, cfg.OrganizationName)
	if err != nil {
		_ = closeRuntime()
		return foundationRuntime{}, err
	}
	correlationID, err := foundation.NewCorrelationID()
	if err != nil {
		_ = closeRuntime()
		return foundationRuntime{}, fmt.Errorf("create bootstrap correlation id: %w", err)
	}
	eventID, err := foundation.NewCorrelationID()
	if err != nil {
		_ = closeRuntime()
		return foundationRuntime{}, fmt.Errorf("create bootstrap event id: %w", err)
	}
	action := "organization.bootstrap.verified"
	if created {
		action = "organization.bootstrap.created"
	}
	if err := auditor.Record(foundation.WithScope(ctx, foundation.Scope{
		OrganizationID: organization.ID,
		ActorID:        "system:bootstrap",
		CorrelationID:  correlationID,
	}), foundation.AuditEvent{
		ID:             eventID,
		OrganizationID: organization.ID,
		ActorID:        "system:bootstrap",
		CorrelationID:  correlationID,
		Action:         action,
		ResourceType:   "organization",
		ResourceID:     organization.ID,
		OccurredAt:     time.Now().UTC(),
		Metadata:       map[string]string{"requirementId": foundation.RequirementID},
	}); err != nil {
		_ = closeRuntime()
		return foundationRuntime{}, fmt.Errorf("audit organization bootstrap: %w", err)
	}
	return foundationRuntime{
		organization: organization,
		guardStore:   guardStore,
		peopleStore:  peopleStore,
		auditor:      auditor,
		close:        closeRuntime,
	}, nil
}
