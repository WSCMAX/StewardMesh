package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/maxlemke/stewardmesh/internal/bootstrap"
	"github.com/maxlemke/stewardmesh/internal/config"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/httpapi"
	"github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/repository"
	postgresrepository "github.com/maxlemke/stewardmesh/internal/repository/postgres"
	"github.com/maxlemke/stewardmesh/internal/storage"
)

type foundationRuntime struct {
	organization domain.Organization
	guardStore   guard.Store
	peopleStore  people.Store
	auditor      foundation.Auditor
	close        func() error
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	setupContext, cancelSetup := context.WithTimeout(ctx, 20*time.Second)
	runtime, err := initializeFoundation(setupContext, cfg)
	cancelSetup()
	if err != nil {
		logger.Error("initialize foundation", "error", err)
		os.Exit(1)
	}
	defer runtime.close()

	assets := repository.NewMemoryAssetRepository()
	catalog := repository.NewMemoryCatalog()
	blobStore, err := storage.NewLocalBlobStore(cfg.BlobDir, 25<<20)
	if err != nil {
		logger.Error("initialize blob store", "error", err)
		os.Exit(1)
	}
	guardService, err := guard.NewService(
		runtime.guardStore,
		guard.NewArgon2idHasher(),
		runtime.auditor,
		nil,
		guard.ServiceConfig{
			OrganizationID: cfg.OrganizationID,
			BootstrapToken: cfg.BootstrapToken,
			SessionTTL:     cfg.SessionTTL,
		},
	)
	cfg.BootstrapToken = ""
	if err != nil {
		logger.Error("initialize Guard", "error", err)
		os.Exit(1)
	}
	peopleService, err := people.NewService(runtime.peopleStore, assets, runtime.auditor, people.ServiceConfig{
		OrganizationID: cfg.OrganizationID,
	})
	if err != nil {
		logger.Error("initialize People", "error", err)
		os.Exit(1)
	}

	handler := httpapi.NewServer(httpapi.Dependencies{
		Assets:              assets,
		People:              peopleService,
		Tags:                catalog,
		Goals:               catalog,
		Blobs:               blobStore,
		Guard:               guardService,
		SessionCookieSecure: cfg.SessionCookieSecure,
	}, cfg.AllowedOrigin, runtime.organization)
	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	go func() {
		logger.Info(
			"StewardMesh server started",
			"addr", cfg.Addr,
			"organization_id", runtime.organization.ID,
			"repository_driver", cfg.RepositoryDriver,
		)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server stopped", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown", "error", err)
	}
}

func initializeFoundation(ctx context.Context, cfg config.Config) (foundationRuntime, error) {
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
		if err := postgresrepository.Migrate(ctx, database); err != nil {
			database.Close()
			return foundationRuntime{}, err
		}
		organizations, err = postgresrepository.NewOrganizationRepository(database)
		if err != nil {
			database.Close()
			return foundationRuntime{}, err
		}
		auditor, err = postgresrepository.NewAuditor(database)
		if err != nil {
			database.Close()
			return foundationRuntime{}, err
		}
		guardStore, err = postgresrepository.NewGuardStore(database)
		if err != nil {
			database.Close()
			return foundationRuntime{}, err
		}
		peopleStore, err = postgresrepository.NewPeopleStore(database)
		if err != nil {
			database.Close()
			return foundationRuntime{}, err
		}
	default:
		return foundationRuntime{}, fmt.Errorf("unsupported repository driver %q", cfg.RepositoryDriver)
	}

	service, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		closeRuntime()
		return foundationRuntime{}, err
	}
	organization, created, err := service.EnsureOrganization(ctx, cfg.OrganizationID, cfg.OrganizationName)
	if err != nil {
		closeRuntime()
		return foundationRuntime{}, err
	}
	correlationID, err := foundation.NewCorrelationID()
	if err != nil {
		closeRuntime()
		return foundationRuntime{}, fmt.Errorf("create bootstrap correlation id: %w", err)
	}
	eventID, err := foundation.NewCorrelationID()
	if err != nil {
		closeRuntime()
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
		closeRuntime()
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
