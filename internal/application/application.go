// Package application constructs StewardMesh's transport-neutral HTTP
// application and owns the lifecycle of its shared runtime dependencies.
// Requirements: REQ-FOUNDATION-001, REQ-ATLAS-001, REQ-ATLAS-CODES-001, REQ-DIRECTORY-EXPANSION-002, REQ-DIRECTORY-EXPANSION-003, REQ-PATTERNS-001, REQ-THREADS-001, REQ-STORAGE-001, REQ-LEDGER-001, REQ-STACK-001, REQ-HORIZON-001, REQ-SIGNALS-001, REQ-PLATFORM-VALKEY-001, SEC-GUARD-001.
package application

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/atlascodes"
	"github.com/maxlemke/stewardmesh/internal/bootstrap"
	"github.com/maxlemke/stewardmesh/internal/cache"
	"github.com/maxlemke/stewardmesh/internal/config"
	"github.com/maxlemke/stewardmesh/internal/directoryexpansion"
	"github.com/maxlemke/stewardmesh/internal/directoryexpansion/entra"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/horizon"
	"github.com/maxlemke/stewardmesh/internal/httpapi"
	"github.com/maxlemke/stewardmesh/internal/identity"
	"github.com/maxlemke/stewardmesh/internal/ledger"
	"github.com/maxlemke/stewardmesh/internal/patterns"
	"github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/repository"
	postgresrepository "github.com/maxlemke/stewardmesh/internal/repository/postgres"
	"github.com/maxlemke/stewardmesh/internal/signals"
	"github.com/maxlemke/stewardmesh/internal/stack"
	"github.com/maxlemke/stewardmesh/internal/storage"
	"github.com/maxlemke/stewardmesh/internal/threads"
)

// Options selects construction behavior that differs by deployment transport.
// Long-running local servers currently run migrations at startup. Future
// short-lived runtimes must leave this disabled and run migrations as a
// separate deployment operation.
type Options struct {
	RunMigrations       bool
	DirectoryConnectors []directoryexpansion.Connector
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

	blobStore, err := initializeBlobStore(ctx, cfg)
	cfg.S3AccessKeyID = ""
	cfg.S3SecretAccessKey = ""
	cfg.S3SessionToken = ""
	if err != nil {
		return fail(fmt.Errorf("initialize blob store: %w", err))
	}
	vaultService, err := storage.NewService(runtime.storageStore, blobStore, runtime.auditor, storage.ServiceConfig{
		OrganizationID: cfg.OrganizationID,
		DownloadTTL:    cfg.BlobDownloadTTL,
	})
	if err != nil {
		return fail(fmt.Errorf("initialize Vault: %w", err))
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
	atlasService, err := atlas.NewService(runtime.assetStore, peopleAssetReferenceValidator{store: runtime.peopleStore}, runtime.auditor, atlas.ServiceConfig{
		OrganizationID: cfg.OrganizationID,
	})
	if err != nil {
		return fail(fmt.Errorf("initialize Atlas: %w", err))
	}
	atlasCodesService, err := atlascodes.NewService(runtime.atlasCodesStore, atlasService, runtime.auditor, atlascodes.ServiceConfig{
		OrganizationID: cfg.OrganizationID,
	})
	if err != nil {
		return fail(fmt.Errorf("initialize Atlas Codes: %w", err))
	}
	threadsService, err := threads.NewService(runtime.threadsStore, threadsTargetValidator{atlas: atlasService}, runtime.auditor, threads.ServiceConfig{
		OrganizationID: cfg.OrganizationID,
	})
	if err != nil {
		return fail(fmt.Errorf("initialize Threads: %w", err))
	}
	peopleService, err := people.NewService(runtime.peopleStore, atlasService, runtime.auditor, people.ServiceConfig{
		OrganizationID: cfg.OrganizationID,
	})
	if err != nil {
		return fail(fmt.Errorf("initialize People: %w", err))
	}
	directoryConnectors := append([]directoryexpansion.Connector(nil), options.DirectoryConnectors...)
	if cfg.EntraEnabled() {
		entraConfiguration := cfg.EntraConfig()
		cfg.EntraClientSecret = ""
		entraConnector, connectorErr := entra.NewConnector(entraConfiguration, entra.Options{})
		entraConfiguration.ClientSecret = ""
		if connectorErr != nil {
			return fail(fmt.Errorf("initialize Microsoft Entra directory connector: %w", connectorErr))
		}
		directoryConnectors = append(directoryConnectors, entraConnector)
	}
	directoryRegistry, err := directoryexpansion.NewRegistry(directoryConnectors...)
	if err != nil {
		return fail(fmt.Errorf("initialize directory connector registry: %w", err))
	}
	directoryTarget, err := directoryexpansion.NewPeopleTarget(runtime.peopleStore, guardService, nil)
	if err != nil {
		return fail(fmt.Errorf("initialize directory People target: %w", err))
	}
	directoryService, err := directoryexpansion.NewService(runtime.directoryImportStore, directoryTarget, runtime.auditor, directoryRegistry, directoryexpansion.ServiceConfig{
		OrganizationID: cfg.OrganizationID,
	})
	if err != nil {
		return fail(fmt.Errorf("initialize directory imports: %w", err))
	}
	ledgerService, err := ledger.NewService(runtime.ledgerStore, ledgerReferenceValidator{
		atlas: atlasService, vault: vaultService, people: runtime.peopleStore, organizationID: cfg.OrganizationID,
	}, runtime.auditor, ledger.ServiceConfig{OrganizationID: cfg.OrganizationID})
	if err != nil {
		return fail(fmt.Errorf("initialize Ledger: %w", err))
	}
	stackService, err := stack.NewService(runtime.stackStore, stackReferenceValidator{
		atlas: atlasService, vault: vaultService, people: runtime.peopleStore, ledger: runtime.ledgerStore,
		organizationID: cfg.OrganizationID,
	}, runtime.auditor, stack.ServiceConfig{OrganizationID: cfg.OrganizationID})
	if err != nil {
		return fail(fmt.Errorf("initialize Stack: %w", err))
	}
	horizonService, err := horizon.NewService(runtime.horizonStore, atlasService, ledgerService, threadsService, runtime.auditor, horizon.ServiceConfig{OrganizationID: cfg.OrganizationID})
	if err != nil {
		return fail(fmt.Errorf("initialize Horizon: %w", err))
	}
	signalsService, err := signals.NewService(runtime.signalsStore, signalsEvaluator{ledger: ledgerService, stack: stackService, horizon: horizonService}, runtime.auditor, signals.ServiceConfig{OrganizationID: cfg.OrganizationID})
	if err != nil {
		return fail(fmt.Errorf("initialize Signals: %w", err))
	}
	patternsService, err := patterns.NewService(runtime.patternsStore, runtime.auditor, patterns.ServiceConfig{OrganizationID: cfg.OrganizationID})
	if err != nil {
		return fail(fmt.Errorf("initialize Patterns: %w", err))
	}
	atlasLabelsService, err := atlascodes.NewLabelService(atlasCodesService, atlasService, patternsService, atlascodes.DefaultLabelRenderers(), nil)
	if err != nil {
		return fail(fmt.Errorf("initialize Atlas Codes labels: %w", err))
	}

	application.handler = httpapi.NewServer(httpapi.Dependencies{
		Atlas:               atlasService,
		AtlasCodes:          atlasCodesService,
		AtlasLabels:         atlasLabelsService,
		People:              peopleService,
		DirectoryImports:    directoryService,
		Threads:             threadsService,
		Vault:               vaultService,
		Ledger:              ledgerService,
		Stack:               stackService,
		Horizon:             horizonService,
		Signals:             signalsService,
		Patterns:            patternsService,
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
	organization         bootstrap.Organization
	assetStore           atlas.Store
	atlasCodesStore      atlascodes.Store
	threadsStore         threads.Store
	storageStore         storage.MetadataStore
	ledgerStore          ledger.Store
	stackStore           stack.Store
	horizonStore         horizon.Store
	signalsStore         signals.Store
	patternsStore        patterns.Store
	guardStore           guard.Store
	peopleStore          people.Store
	directoryImportStore directoryexpansion.Store
	auditor              foundation.Auditor
	close                func() error
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
		organizations        repository.OrganizationRepository
		assetStore           atlas.Store
		atlasCodesStore      atlascodes.Store
		threadsStore         threads.Store
		storageStore         storage.MetadataStore
		ledgerStore          ledger.Store
		stackStore           stack.Store
		horizonStore         horizon.Store
		signalsStore         signals.Store
		patternsStore        patterns.Store
		guardStore           guard.Store
		peopleStore          people.Store
		directoryImportStore directoryexpansion.Store
		auditor              foundation.Auditor = foundation.NopAuditor{}
		closeRuntime                            = func() error { return nil }
	)
	switch cfg.RepositoryDriver {
	case config.RepositoryDriverMemory:
		organizations = repository.NewMemoryOrganizationRepository()
		guardStore = repository.NewMemoryGuardStore()
		peopleStore = repository.NewMemoryPeopleStore()
		directoryImportStore = repository.NewMemoryDirectoryImportStore()
		assetStore = repository.NewMemoryAtlasStore()
		atlasCodesStore = repository.NewMemoryAtlasCodesStore()
		threadsStore = repository.NewMemoryThreadsStore()
		storageStore = repository.NewMemoryStorageStore()
		ledgerStore = repository.NewMemoryLedgerStore()
		stackStore = repository.NewMemoryStackStore()
		horizonStore = repository.NewMemoryHorizonStore()
		signalsStore = repository.NewMemorySignalsStore()
		patternsStore = repository.NewMemoryPatternsStore()
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
		directoryImportStore, err = postgresrepository.NewDirectoryImportStore(database)
		if err != nil {
			_ = database.Close()
			return foundationRuntime{}, err
		}
		assetStore, err = postgresrepository.NewAtlasStore(database)
		if err != nil {
			_ = database.Close()
			return foundationRuntime{}, err
		}
		atlasCodesStore, err = postgresrepository.NewAtlasCodesStore(database)
		if err != nil {
			_ = database.Close()
			return foundationRuntime{}, err
		}
		threadsStore, err = postgresrepository.NewThreadsStore(database)
		if err != nil {
			_ = database.Close()
			return foundationRuntime{}, err
		}
		storageStore, err = postgresrepository.NewStorageStore(database)
		if err != nil {
			_ = database.Close()
			return foundationRuntime{}, err
		}
		ledgerStore, err = postgresrepository.NewLedgerStore(database)
		if err != nil {
			_ = database.Close()
			return foundationRuntime{}, err
		}
		stackStore, err = postgresrepository.NewStackStore(database)
		if err != nil {
			_ = database.Close()
			return foundationRuntime{}, err
		}
		horizonStore, err = postgresrepository.NewHorizonStore(database)
		if err != nil {
			_ = database.Close()
			return foundationRuntime{}, err
		}
		signalsStore, err = postgresrepository.NewSignalsStore(database)
		if err != nil {
			_ = database.Close()
			return foundationRuntime{}, err
		}
		patternsStore, err = postgresrepository.NewPatternsStore(database)
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
		organization:         organization,
		assetStore:           assetStore,
		atlasCodesStore:      atlasCodesStore,
		threadsStore:         threadsStore,
		storageStore:         storageStore,
		ledgerStore:          ledgerStore,
		stackStore:           stackStore,
		horizonStore:         horizonStore,
		signalsStore:         signalsStore,
		patternsStore:        patternsStore,
		guardStore:           guardStore,
		peopleStore:          peopleStore,
		directoryImportStore: directoryImportStore,
		auditor:              auditor,
		close:                closeRuntime,
	}, nil
}

func initializeBlobStore(ctx context.Context, cfg config.Config) (storage.ObjectStore, error) {
	switch cfg.StorageDriver {
	case config.StorageDriverLocal:
		return storage.NewLocalBlobStore(cfg.BlobDir, cfg.BlobMaximumBytes)
	case config.StorageDriverS3:
		return storage.NewS3BlobStore(ctx, cfg.S3Config())
	default:
		return nil, fmt.Errorf("unsupported storage driver %q", cfg.StorageDriver)
	}
}

type threadsTargetValidator struct {
	atlas *atlas.Service
}

type ledgerReferenceValidator struct {
	atlas          *atlas.Service
	vault          *storage.Service
	people         people.Store
	organizationID string
}

type stackReferenceValidator struct {
	atlas          *atlas.Service
	vault          *storage.Service
	people         people.Store
	ledger         ledger.Store
	organizationID string
}

func (v stackReferenceValidator) ResolveAsset(ctx context.Context, assetID string) (stack.AssetContext, error) {
	if v.atlas == nil {
		return stack.AssetContext{}, errors.New("Atlas service is required")
	}
	asset, err := v.atlas.GetAsset(ctx, assetID)
	if err != nil {
		return stack.AssetContext{}, mapStackReferenceError("asset", err)
	}
	return stack.AssetContext{ID: asset.ID, SiteID: asset.SiteID, DepartmentID: asset.DepartmentID, IdentityID: asset.UserID}, nil
}

func (v stackReferenceValidator) ValidateAssignee(ctx context.Context, kind, id string) error {
	if kind == "asset" {
		_, err := v.ResolveAsset(ctx, id)
		return err
	}
	if v.people == nil {
		return errors.New("People store is required")
	}
	var err error
	switch kind {
	case "identity":
		_, err = v.people.GetIdentity(ctx, v.organizationID, id)
	case "department":
		_, err = v.people.GetDepartment(ctx, v.organizationID, id)
	case "site":
		_, err = v.people.GetSite(ctx, v.organizationID, id)
	default:
		return stack.ErrInvalidInput
	}
	if err != nil {
		return mapStackReferenceError(kind, err)
	}
	return nil
}

func (v stackReferenceValidator) ValidateFinancialReferences(ctx context.Context, vendorID, purchaseOrderID, contractID, costRecordID string) error {
	if v.ledger == nil {
		return errors.New("Ledger store is required")
	}
	if vendorID != "" {
		if _, err := v.ledger.GetVendor(ctx, v.organizationID, vendorID); err != nil {
			return mapStackReferenceError("vendor", err)
		}
	}
	if purchaseOrderID != "" {
		purchaseOrder, err := v.ledger.GetPurchaseOrder(ctx, v.organizationID, purchaseOrderID)
		if err != nil {
			return mapStackReferenceError("purchase order", err)
		}
		if vendorID != "" && purchaseOrder.VendorID != vendorID {
			return stack.ErrReferenceMissing
		}
	}
	if contractID != "" {
		contract, err := v.ledger.GetContract(ctx, v.organizationID, contractID)
		if err != nil {
			return mapStackReferenceError("contract", err)
		}
		if vendorID != "" && contract.VendorID != vendorID {
			return stack.ErrReferenceMissing
		}
	}
	if costRecordID != "" {
		snapshot, err := v.ledger.Snapshot(ctx, v.organizationID)
		if err != nil {
			return fmt.Errorf("validate Stack cost record: %w", err)
		}
		found := false
		for _, cost := range snapshot.Costs {
			if cost.ID != costRecordID {
				continue
			}
			found = true
			if (purchaseOrderID != "" && cost.PurchaseOrderID != purchaseOrderID) || (contractID != "" && cost.ContractID != contractID) {
				return stack.ErrReferenceMissing
			}
			break
		}
		if !found {
			return stack.ErrReferenceMissing
		}
	}
	return nil
}

func (v stackReferenceValidator) ValidateDocuments(ctx context.Context, documentIDs []string) error {
	if v.vault == nil {
		return errors.New("Vault service is required")
	}
	for _, id := range documentIDs {
		if _, err := v.vault.GetBlob(ctx, id); err != nil {
			return mapStackReferenceError("document", err)
		}
	}
	return nil
}

func mapStackReferenceError(kind string, err error) error {
	if errors.Is(err, atlas.ErrNotFound) || errors.Is(err, people.ErrNotFound) || errors.Is(err, people.ErrReferenceMissing) ||
		errors.Is(err, ledger.ErrNotFound) || errors.Is(err, ledger.ErrReferenceMissing) || errors.Is(err, storage.ErrNotFound) {
		return stack.ErrReferenceMissing
	}
	if errors.Is(err, atlas.ErrInvalidInput) || errors.Is(err, people.ErrInvalidInput) || errors.Is(err, ledger.ErrInvalidInput) || errors.Is(err, storage.ErrInvalidInput) {
		return stack.ErrInvalidInput
	}
	return fmt.Errorf("validate Stack %s reference: %w", kind, err)
}

func (v ledgerReferenceValidator) ValidateAssets(ctx context.Context, assetIDs []string) error {
	if v.atlas == nil {
		return errors.New("Atlas service is required")
	}
	for _, id := range assetIDs {
		if _, err := v.atlas.GetAsset(ctx, id); err != nil {
			if errors.Is(err, atlas.ErrNotFound) {
				return ledger.ErrReferenceMissing
			}
			if errors.Is(err, atlas.ErrInvalidInput) {
				return ledger.ErrInvalidInput
			}
			return fmt.Errorf("validate Ledger asset reference: %w", err)
		}
	}
	return nil
}

func (v ledgerReferenceValidator) ValidateDocuments(ctx context.Context, documentIDs []string) error {
	if v.vault == nil {
		return errors.New("Vault service is required")
	}
	for _, id := range documentIDs {
		if _, err := v.vault.GetBlob(ctx, id); err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return ledger.ErrReferenceMissing
			}
			if errors.Is(err, storage.ErrInvalidInput) {
				return ledger.ErrInvalidInput
			}
			return fmt.Errorf("validate Ledger document reference: %w", err)
		}
	}
	return nil
}

func (v ledgerReferenceValidator) ValidateDirectory(ctx context.Context, siteID, departmentID string) error {
	if v.people == nil {
		return errors.New("People store is required")
	}
	if siteID != "" {
		if _, err := v.people.GetSite(ctx, v.organizationID, siteID); err != nil {
			return mapLedgerDirectoryError(err)
		}
	}
	if departmentID != "" {
		if _, err := v.people.GetDepartment(ctx, v.organizationID, departmentID); err != nil {
			return mapLedgerDirectoryError(err)
		}
	}
	return nil
}

func mapLedgerDirectoryError(err error) error {
	if errors.Is(err, people.ErrNotFound) || errors.Is(err, people.ErrReferenceMissing) {
		return ledger.ErrReferenceMissing
	}
	if errors.Is(err, people.ErrInvalidInput) {
		return ledger.ErrInvalidInput
	}
	return fmt.Errorf("validate Ledger directory reference: %w", err)
}

func (v threadsTargetValidator) ValidateThreadTarget(ctx context.Context, _ string, targetType threads.TargetType, targetID string) error {
	if targetType != threads.TargetAsset {
		// Future feature-owned record types keep stable, organization-scoped IDs.
		// Their services will extend this boundary when those domains land.
		return nil
	}
	if v.atlas == nil {
		return errors.New("Atlas service is required")
	}
	if _, err := v.atlas.GetAsset(ctx, targetID); err != nil {
		if errors.Is(err, atlas.ErrNotFound) {
			return threads.ErrNotFound
		}
		if errors.Is(err, atlas.ErrInvalidInput) {
			return threads.ErrInvalidInput
		}
		return fmt.Errorf("validate Threads asset target: %w", err)
	}
	return nil
}

type peopleAssetReferenceValidator struct {
	store people.Store
}

func (v peopleAssetReferenceValidator) ValidateAssetReferences(ctx context.Context, organizationID string, references atlas.References) error {
	if v.store == nil {
		return errors.New("people store is required")
	}
	if references.SiteID != "" {
		if _, err := v.store.GetSite(ctx, organizationID, references.SiteID); err != nil {
			return mapAtlasReferenceError("site", err)
		}
	}
	if references.BuildingID != "" {
		building, err := v.store.GetBuilding(ctx, organizationID, references.BuildingID)
		if err != nil {
			return mapAtlasReferenceError("building", err)
		}
		if building.SiteID != references.SiteID {
			return atlas.ErrReferenceMissing
		}
	}
	if references.RoomID != "" {
		room, err := v.store.GetRoom(ctx, organizationID, references.RoomID)
		if err != nil {
			return mapAtlasReferenceError("room", err)
		}
		if room.SiteID != references.SiteID || room.BuildingID != references.BuildingID {
			return atlas.ErrReferenceMissing
		}
	}
	if references.DepartmentID != "" {
		if _, err := v.store.GetDepartment(ctx, organizationID, references.DepartmentID); err != nil {
			return mapAtlasReferenceError("department", err)
		}
	}
	if references.UserID != "" {
		if _, err := v.store.GetIdentity(ctx, organizationID, references.UserID); err != nil {
			return mapAtlasReferenceError("identity", err)
		}
	}
	return nil
}

func mapAtlasReferenceError(kind string, err error) error {
	if errors.Is(err, people.ErrNotFound) || errors.Is(err, people.ErrReferenceMissing) {
		return fmt.Errorf("%s: %w", kind, atlas.ErrReferenceMissing)
	}
	return fmt.Errorf("validate Atlas %s reference: %w", kind, err)
}
