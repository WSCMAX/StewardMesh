//go:build campusdemo

package application

// Requirement: REQ-DIRECTORY-EXPANSION-007. Feature: platform.foundation.

import (
	"context"
	"fmt"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/atlascodes"
	"github.com/maxlemke/stewardmesh/internal/campusseed"
	"github.com/maxlemke/stewardmesh/internal/config"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/horizon"
	"github.com/maxlemke/stewardmesh/internal/labels"
	"github.com/maxlemke/stewardmesh/internal/ledger"
	"github.com/maxlemke/stewardmesh/internal/patterns"
	"github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/reach"
	"github.com/maxlemke/stewardmesh/internal/signals"
	"github.com/maxlemke/stewardmesh/internal/stack"
	"github.com/maxlemke/stewardmesh/internal/storage"
	"github.com/maxlemke/stewardmesh/internal/threads"
)

const CampusDemoIncluded = true

func seedCampusCore(ctx context.Context, cfg config.Config, options Options, runtime foundationRuntime, vaultService *storage.Service, guardService *guard.Service, atlasService *atlas.Service, stackService *stack.Service) error {
	if !options.RunCampusSeed && !cfg.SeedCampus {
		return nil
	}
	campusPeopleService, err := people.NewService(runtime.peopleStore, atlasService, runtime.auditor, people.ServiceConfig{
		OrganizationID: cfg.OrganizationID,
	})
	if err != nil {
		return fmt.Errorf("initialize campus demo People service: %w", err)
	}
	campusAtlasService, err := atlas.NewService(runtime.assetStore, peopleAssetReferenceValidator{store: runtime.peopleStore}, runtime.auditor, atlas.ServiceConfig{
		OrganizationID: cfg.OrganizationID,
	})
	if err != nil {
		return fmt.Errorf("initialize campus demo Atlas service: %w", err)
	}
	campusLedgerService, err := ledger.NewService(runtime.ledgerStore, ledgerReferenceValidator{
		atlas: campusAtlasService, vault: vaultService, people: runtime.peopleStore, organizationID: cfg.OrganizationID,
	}, runtime.auditor, ledger.ServiceConfig{OrganizationID: cfg.OrganizationID})
	if err != nil {
		return fmt.Errorf("initialize campus demo Ledger service: %w", err)
	}
	campusAtlasCodesService, err := atlascodes.NewService(runtime.atlasCodesStore, campusAtlasService, runtime.auditor, atlascodes.ServiceConfig{
		OrganizationID: cfg.OrganizationID,
	})
	if err != nil {
		return fmt.Errorf("initialize campus demo Atlas Codes service: %w", err)
	}
	if _, err := campusseed.Seed(ctx, campusseed.Dependencies{
		OrganizationID: cfg.OrganizationID,
		People:         campusPeopleService,
		Atlas:          campusAtlasService,
		AtlasCodes:     campusAtlasCodesService,
		Stack:          stackService,
		Ledger:         campusLedgerService,
		Vault:          vaultService,
		Guard:          guardService,
		GuardStore:     runtime.guardStore,
		Auditor:        runtime.auditor,
	}); err != nil {
		return fmt.Errorf("seed campus demo data: %w", err)
	}
	return nil
}

func seedCampusLifecycle(ctx context.Context, cfg config.Config, options Options, runtime foundationRuntime, vaultService *storage.Service, horizonService *horizon.Service) error {
	if !options.RunCampusSeed && !cfg.SeedCampus {
		return nil
	}
	campusAtlasService, err := atlas.NewService(runtime.assetStore, peopleAssetReferenceValidator{store: runtime.peopleStore}, runtime.auditor, atlas.ServiceConfig{
		OrganizationID: cfg.OrganizationID,
	})
	if err != nil {
		return fmt.Errorf("initialize campus lifecycle Atlas service: %w", err)
	}
	campusLedgerService, err := ledger.NewService(runtime.ledgerStore, ledgerReferenceValidator{
		atlas: campusAtlasService, vault: vaultService, people: runtime.peopleStore, organizationID: cfg.OrganizationID,
	}, runtime.auditor, ledger.ServiceConfig{OrganizationID: cfg.OrganizationID})
	if err != nil {
		return fmt.Errorf("initialize campus lifecycle Ledger service: %w", err)
	}
	if err := campusseed.SeedLifecycle(ctx, campusseed.LifecycleDependencies{
		OrganizationID: cfg.OrganizationID,
		Atlas:          campusAtlasService,
		Horizon:        horizonService,
		Ledger:         campusLedgerService,
	}); err != nil {
		return fmt.Errorf("seed campus lifecycle data: %w", err)
	}
	return nil
}

func seedCampusExtended(ctx context.Context, cfg config.Config, options Options, runtime foundationRuntime, vaultService *storage.Service, atlasService *atlas.Service, stackService *stack.Service, horizonService *horizon.Service, patternsService *patterns.Service, reachEndpointCatalog *reach.EndpointCatalog, reachTransports *reach.TransportRegistry, reachSecrets reach.SecretResolver, reachEndpoints []reach.Endpoint) error {
	if !options.RunCampusSeed && !cfg.SeedCampus {
		return nil
	}
	campusPeopleService, err := people.NewService(runtime.peopleStore, atlasService, runtime.auditor, people.ServiceConfig{
		OrganizationID: cfg.OrganizationID,
	})
	if err != nil {
		return fmt.Errorf("initialize campus extended People service: %w", err)
	}
	campusAtlasService, err := atlas.NewService(runtime.assetStore, peopleAssetReferenceValidator{store: runtime.peopleStore}, runtime.auditor, atlas.ServiceConfig{
		OrganizationID: cfg.OrganizationID,
	})
	if err != nil {
		return fmt.Errorf("initialize campus extended Atlas service: %w", err)
	}
	campusLedgerService, err := ledger.NewService(runtime.ledgerStore, ledgerReferenceValidator{
		atlas: campusAtlasService, vault: vaultService, people: runtime.peopleStore, organizationID: cfg.OrganizationID,
	}, runtime.auditor, ledger.ServiceConfig{OrganizationID: cfg.OrganizationID})
	if err != nil {
		return fmt.Errorf("initialize campus extended Ledger service: %w", err)
	}
	campusThreadsService, err := threads.NewService(runtime.threadsStore, threadsTargetValidator{atlas: campusAtlasService}, runtime.auditor, threads.ServiceConfig{
		OrganizationID: cfg.OrganizationID,
	})
	if err != nil {
		return fmt.Errorf("initialize campus extended Threads service: %w", err)
	}
	campusLabelsService, err := labels.NewService(runtime.labelsStore, labelsRecordValidator{
		people: runtime.peopleStore, atlas: campusAtlasService, stack: runtime.stackStore, ledger: runtime.ledgerStore,
		vault: vaultService, horizon: runtime.horizonStore, organizationID: cfg.OrganizationID,
	}, runtime.auditor, labels.ServiceConfig{OrganizationID: cfg.OrganizationID})
	if err != nil {
		return fmt.Errorf("initialize campus extended Labels service: %w", err)
	}
	signalTargets, err := reach.NewSubscriptionTargetCatalog(runtime.reachStore, reachEndpointCatalog)
	if err != nil {
		return fmt.Errorf("initialize campus extended subscription targets: %w", err)
	}
	campusSignalsService, err := signals.NewService(runtime.signalsStore, signalsEvaluator{ledger: campusLedgerService, stack: stackService, horizon: horizonService}, runtime.auditor, signals.ServiceConfig{
		OrganizationID:               cfg.OrganizationID,
		SubscriptionTargets:          signalTargets,
		SubscriptionTargetReferences: signalTargets,
	})
	if err != nil {
		return fmt.Errorf("initialize campus extended Signals service: %w", err)
	}
	campusReachService, err := reach.NewService(runtime.reachStore, reachEndpointCatalog, reachTransports, reachSecrets, campusSignalsService, runtime.auditor, reach.ServiceConfig{
		OrganizationID: cfg.OrganizationID,
	})
	if err != nil {
		return fmt.Errorf("initialize campus extended Reach service: %w", err)
	}
	campusAtlasCodesService, err := atlascodes.NewService(runtime.atlasCodesStore, campusAtlasService, runtime.auditor, atlascodes.ServiceConfig{
		OrganizationID: cfg.OrganizationID,
	})
	if err != nil {
		return fmt.Errorf("initialize campus extended Atlas Codes service: %w", err)
	}
	campusAtlasLabelsService, err := atlascodes.NewLabelService(campusAtlasCodesService, campusAtlasService, patternsService, atlascodes.DefaultLabelRenderers(), nil)
	if err != nil {
		return fmt.Errorf("initialize campus extended Atlas label service: %w", err)
	}
	reachWebhookEndpointID := "operations-hook"
	for _, endpoint := range reachEndpoints {
		if endpoint.Kind == reach.ProviderWebhook {
			reachWebhookEndpointID = endpoint.ID
			break
		}
	}
	if _, err := campusseed.SeedExtended(ctx, campusseed.ExtendedDependencies{
		OrganizationID:         cfg.OrganizationID,
		People:                 campusPeopleService,
		Atlas:                  campusAtlasService,
		Stack:                  stackService,
		Ledger:                 campusLedgerService,
		Threads:                campusThreadsService,
		Labels:                 campusLabelsService,
		Signals:                campusSignalsService,
		Reach:                  campusReachService,
		BridgeStore:            runtime.bridgeStore,
		ExchangeStore:          runtime.exchangeStore,
		DirectoryGroups:        runtime.directoryImportStore,
		AtlasLabels:            campusAtlasLabelsService,
		AtlasCodes:             campusAtlasCodesService,
		Patterns:               patternsService,
		ReachWebhookEndpointID: reachWebhookEndpointID,
		Auditor:                runtime.auditor,
	}); err != nil {
		return fmt.Errorf("seed campus extended demo data: %w", err)
	}
	return nil
}
