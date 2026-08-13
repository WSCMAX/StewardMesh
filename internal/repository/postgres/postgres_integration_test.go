package postgres

// Requirements: REQ-FOUNDATION-001, SEC-GUARD-001, REQ-PEOPLE-001,
// REQ-DIRECTORY-EXPANSION-001, REQ-DIRECTORY-EXPANSION-002, REQ-DIRECTORY-EXPANSION-003, REQ-DIRECTORY-EXPANSION-005, REQ-DIRECTORY-EXPANSION-006, REQ-ATLAS-001, REQ-ATLAS-MODELS-001,
// REQ-THREADS-001, REQ-STORAGE-001, REQ-LEDGER-001, REQ-HORIZON-001,
// REQ-ATLAS-CODES-001, REQ-PATTERNS-001, REQ-STACK-001, REQ-SIGNALS-001, REQ-REACH-001, REQ-EXCHANGE-001, REQ-API-001, SEC-MCP-001.
// Features: lifecycle.planning, inventory.models, inventory.identifiers, templates.schemas, alerts.rules, messaging.delivery, migration.packages, integrations.protocols.

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/bootstrap"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/repository/contracttest"
)

func TestOrganizationRepositoryIntegration(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, database); err != nil {
		t.Fatalf("expected idempotent migrations: %v", err)
	}
	repository, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	contracttest.OrganizationRepository(t, repository, fmt.Sprintf("postgres-contract-%d", time.Now().UnixNano()))
	service, err := bootstrap.NewOrganizationService(repository)
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("integration-%d", time.Now().UnixNano())
	created, wasCreated, err := service.EnsureOrganization(ctx, id, "Integration Organization")
	if err != nil {
		t.Fatal(err)
	}
	if !wasCreated || created.ID != id {
		t.Fatalf("expected organization %q to be created", id)
	}
	updated, wasCreated, err := service.EnsureOrganization(ctx, id, "Updated Integration Organization")
	if err != nil {
		t.Fatal(err)
	}
	if wasCreated || updated.Name != "Updated Integration Organization" {
		t.Fatalf("expected idempotent update, got %#v", updated)
	}
	loaded, err := repository.GetOrganization(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != id || loaded.Name != updated.Name {
		t.Fatalf("unexpected persisted organization %#v", loaded)
	}
}

func TestPatternsStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	organizations, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	organizationID := fmt.Sprintf("patterns-integration-%d", time.Now().UnixNano())
	service, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.EnsureOrganization(ctx, organizationID, "Patterns Integration"); err != nil {
		t.Fatal(err)
	}
	store, err := NewPatternsStore(database)
	if err != nil {
		t.Fatal(err)
	}
	contracttest.PatternsStore(t, store, organizationID, fmt.Sprintf("postgres-%d", time.Now().UnixNano()))
}

func TestDirectoryImportStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	organizations, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	unique := fmt.Sprintf("postgres-%d", time.Now().UnixNano())
	organizationID := "directory-import-" + unique
	service, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.EnsureOrganization(ctx, organizationID, "Directory Import Integration"); err != nil {
		t.Fatal(err)
	}
	store, err := NewDirectoryImportStore(database)
	if err != nil {
		t.Fatal(err)
	}
	contracttest.DirectoryImportStore(t, store, organizationID, unique)
	contracttest.DirectoryGroupMappingStore(t, store, organizationID, unique+"-group-mapping")
	contracttest.DirectoryGroupTargetStore(t, store, organizationID, unique+"-groups")
}

func TestExchangeStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	organizations, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	unique := fmt.Sprintf("postgres-%d", time.Now().UnixNano())
	organizationID := "exchange-" + unique
	service, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.EnsureOrganization(ctx, organizationID, "Exchange Integration"); err != nil {
		t.Fatal(err)
	}
	store, err := NewExchangeStore(database)
	if err != nil {
		t.Fatal(err)
	}
	contracttest.ExchangeStore(t, store, organizationID, unique)
}

func TestAuditorIntegrationTreatsEventIDAsAnIdempotencyKey(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	organizations, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	organizationID := fmt.Sprintf("audit-integration-%d", time.Now().UnixNano())
	service, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.EnsureOrganization(ctx, organizationID, "Audit Integration"); err != nil {
		t.Fatal(err)
	}
	auditor, err := NewAuditor(database)
	if err != nil {
		t.Fatal(err)
	}
	eventID := fmt.Sprintf("audit-event-%d", time.Now().UnixNano())
	event := foundation.AuditEvent{
		ID: eventID, OrganizationID: organizationID, ActorID: "account-one", CorrelationID: "audit-correlation-one",
		Action: "atlas.identifier.created", ResourceType: "asset_identifier", ResourceID: "identifier-one",
		OccurredAt: time.Now().UTC(), Metadata: map[string]string{"requirementId": "REQ-ATLAS-CODES-001"},
	}
	if err := auditor.Record(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err := auditor.Record(ctx, event); err != nil {
		t.Fatalf("expected exact audit replay to succeed: %v", err)
	}
	replayed := event
	replayed.Action = "must-not-overwrite-original"
	replayed.CorrelationID = "audit-correlation-retry"
	if err := auditor.Record(ctx, replayed); err == nil {
		t.Fatal("expected reused audit ID with different immutable content to fail")
	}
	var count int
	var action string
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*), MIN(action) FROM audit_events WHERE id = $1`, eventID).Scan(&count, &action); err != nil {
		t.Fatal(err)
	}
	if count != 1 || action != event.Action {
		t.Fatalf("expected one immutable audit event, count=%d action=%q", count, action)
	}
}

func TestAtlasCodesAuditProvenanceMigrationUpgradesExistingIdentifiers(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, `
		ALTER TABLE atlas_asset_identifiers
			DROP CONSTRAINT atlas_asset_identifiers_created_correlation_check,
			DROP CONSTRAINT atlas_asset_identifiers_updated_by_check,
			DROP CONSTRAINT atlas_asset_identifiers_updated_correlation_check,
			DROP COLUMN created_correlation_id,
			DROP COLUMN updated_by,
			DROP COLUMN updated_correlation_id;
		DELETE FROM stewardmesh_schema_migrations WHERE version = 22;
	`); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, database); err != nil {
		t.Fatalf("expected migration 0022 to upgrade a 0021 schema: %v", err)
	}
	var nullable string
	if err := database.QueryRowContext(ctx, `
		SELECT is_nullable
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'atlas_asset_identifiers'
		  AND column_name = 'updated_correlation_id'
	`).Scan(&nullable); err != nil {
		t.Fatal(err)
	}
	if nullable != "NO" {
		t.Fatalf("expected durable non-null Atlas Codes audit provenance, got is_nullable=%q", nullable)
	}
}

func TestGuardStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	organizations, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	organizationID := fmt.Sprintf("guard-integration-%d", time.Now().UnixNano())
	service, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.EnsureOrganization(ctx, organizationID, "Guard Integration"); err != nil {
		t.Fatal(err)
	}
	store, err := NewGuardStore(database)
	if err != nil {
		t.Fatal(err)
	}
	contracttest.GuardStore(t, store, organizationID)
}

func TestPeopleStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	organizations, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	organizationID := fmt.Sprintf("people-integration-%d", time.Now().UnixNano())
	service, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.EnsureOrganization(ctx, organizationID, "People Integration"); err != nil {
		t.Fatal(err)
	}
	store, err := NewPeopleStore(database)
	if err != nil {
		t.Fatal(err)
	}
	contracttest.PeopleStore(t, store, organizationID)
}

func TestAtlasStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	organizations, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	organizationID := fmt.Sprintf("atlas-integration-%d", time.Now().UnixNano())
	service, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.EnsureOrganization(ctx, organizationID, "Atlas Integration"); err != nil {
		t.Fatal(err)
	}
	store, err := NewAtlasStore(database)
	if err != nil {
		t.Fatal(err)
	}
	contracttest.AtlasStore(t, store, organizationID, fmt.Sprintf("postgres-%d", time.Now().UnixNano()))
}

func TestAtlasCodesStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	organizations, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	suffix := fmt.Sprintf("postgres-%d", time.Now().UnixNano())
	organizationID := "atlas-codes-" + suffix
	otherOrganizationID := organizationID + "-other"
	organizationService, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		t.Fatal(err)
	}
	for id, name := range map[string]string{
		organizationID:      "Atlas Codes Integration",
		otherOrganizationID: "Atlas Codes Other Integration",
	} {
		if _, _, err := organizationService.EnsureOrganization(ctx, id, name); err != nil {
			t.Fatal(err)
		}
	}
	assetStore, err := NewAtlasStore(database)
	if err != nil {
		t.Fatal(err)
	}
	assetID := "codes-asset-" + suffix
	otherAssetID := "codes-other-asset-" + suffix
	now := time.Date(2026, time.August, 11, 11, 0, 0, 0, time.UTC)
	for index, seed := range []struct {
		organizationID string
		assetID        string
	}{
		{organizationID: organizationID, assetID: assetID},
		{organizationID: organizationID, assetID: otherAssetID},
		{organizationID: otherOrganizationID, assetID: otherAssetID},
	} {
		asset := domain.Asset{
			ID: seed.assetID, OrganizationID: seed.organizationID, Name: "Atlas Codes Contract Asset",
			Kind: "server", Status: "active", Revision: 1, CreatedAt: now, UpdatedAt: now,
		}
		lifecycle := domain.AssetLifecycleEvent{
			ID: fmt.Sprintf("%032x", time.Now().UnixNano()+int64(index)), OrganizationID: seed.organizationID,
			AssetID: seed.assetID, ToStatus: "active", Revision: 1, ActorID: "integration", OccurredAt: now,
		}
		if _, err := assetStore.CreateAsset(ctx, asset, lifecycle); err != nil {
			t.Fatal(err)
		}
	}
	store, err := NewAtlasCodesStore(database)
	if err != nil {
		t.Fatal(err)
	}
	contracttest.AtlasCodesStore(t, store, organizationID, assetID, otherOrganizationID, otherAssetID, suffix)
}

func TestThreadsStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	organizations, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	organizationID := fmt.Sprintf("threads-integration-%d", time.Now().UnixNano())
	service, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.EnsureOrganization(ctx, organizationID, "Threads Integration"); err != nil {
		t.Fatal(err)
	}
	store, err := NewThreadsStore(database)
	if err != nil {
		t.Fatal(err)
	}
	contracttest.ThreadsStore(t, store, organizationID, fmt.Sprintf("postgres-%d", time.Now().UnixNano()))
}

func TestStorageStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	organizations, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	organizationID := fmt.Sprintf("storage-integration-%d", time.Now().UnixNano())
	service, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.EnsureOrganization(ctx, organizationID, "Storage Integration"); err != nil {
		t.Fatal(err)
	}
	store, err := NewStorageStore(database)
	if err != nil {
		t.Fatal(err)
	}
	contracttest.StorageStore(t, store, organizationID, fmt.Sprintf("postgres-%d", time.Now().UnixNano()))
}

func TestLedgerStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	organizations, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	organizationID := fmt.Sprintf("ledger-integration-%d", time.Now().UnixNano())
	service, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.EnsureOrganization(ctx, organizationID, "Ledger Integration"); err != nil {
		t.Fatal(err)
	}
	store, err := NewLedgerStore(database)
	if err != nil {
		t.Fatal(err)
	}
	contracttest.LedgerStore(t, store, organizationID, fmt.Sprintf("postgres-%d", time.Now().UnixNano()))
}

func TestHorizonStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	organizations, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	organizationID := fmt.Sprintf("horizon-integration-%d", time.Now().UnixNano())
	organizationService, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := organizationService.EnsureOrganization(ctx, organizationID, "Horizon Integration"); err != nil {
		t.Fatal(err)
	}
	assetStore, err := NewAtlasStore(database)
	if err != nil {
		t.Fatal(err)
	}
	suffix := fmt.Sprintf("postgres-%d", time.Now().UnixNano())
	assetID := "horizon-asset-" + suffix
	now := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	asset := domain.Asset{
		ID: assetID, OrganizationID: organizationID, Name: "Horizon Contract Asset", Kind: "server",
		Status: "active", Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	lifecycle := domain.AssetLifecycleEvent{
		ID: fmt.Sprintf("%032x", time.Now().UnixNano()), OrganizationID: organizationID, AssetID: assetID,
		ToStatus: "active", Revision: 1, ActorID: "integration", OccurredAt: now,
	}
	if _, err := assetStore.CreateAsset(ctx, asset, lifecycle); err != nil {
		t.Fatal(err)
	}
	store, err := NewHorizonStore(database)
	if err != nil {
		t.Fatal(err)
	}
	contracttest.HorizonStore(t, store, organizationID, assetID, suffix)
}

func TestStackStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	organizations, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	organizationID := fmt.Sprintf("stack-integration-%d", time.Now().UnixNano())
	organizationService, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := organizationService.EnsureOrganization(ctx, organizationID, "Stack Integration"); err != nil {
		t.Fatal(err)
	}
	suffix := fmt.Sprintf("postgres-%d", time.Now().UnixNano())
	assetStore, err := NewAtlasStore(database)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	assetID := "asset-" + suffix
	asset := domain.Asset{ID: assetID, OrganizationID: organizationID, Name: "Stack contract asset", Kind: "computer", Status: "active", Revision: 1, CreatedAt: now, UpdatedAt: now}
	lifecycle := domain.AssetLifecycleEvent{ID: fmt.Sprintf("%032x", time.Now().UnixNano()), OrganizationID: organizationID, AssetID: assetID, ToStatus: "active", Revision: 1, ActorID: "integration", OccurredAt: now}
	if _, err := assetStore.CreateAsset(ctx, asset, lifecycle); err != nil {
		t.Fatal(err)
	}
	store, err := NewStackStore(database)
	if err != nil {
		t.Fatal(err)
	}
	contracttest.StackStore(t, store, organizationID, suffix)
}

func TestSignalsStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	organizationID := fmt.Sprintf("signals-integration-%d", time.Now().UnixNano())
	organizations, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	organizationService, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := organizationService.EnsureOrganization(ctx, organizationID, "Signals Integration"); err != nil {
		t.Fatal(err)
	}
	store, err := NewSignalsStore(database)
	if err != nil {
		t.Fatal(err)
	}
	contracttest.SignalsStore(t, store, organizationID, fmt.Sprintf("postgres-%d", time.Now().UnixNano()))
}

func TestReachStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	organizationID := fmt.Sprintf("reach-integration-%d", time.Now().UnixNano())
	organizations, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	organizationService, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := organizationService.EnsureOrganization(ctx, organizationID, "Reach Integration"); err != nil {
		t.Fatal(err)
	}
	store, err := NewReachStore(database)
	if err != nil {
		t.Fatal(err)
	}
	contracttest.ReachStore(t, store, organizationID, fmt.Sprintf("postgres-%d", time.Now().UnixNano()))
}

func TestBridgeStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	organizationID := fmt.Sprintf("bridge-integration-%d", time.Now().UnixNano())
	organizations, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	organizationService, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := organizationService.EnsureOrganization(ctx, organizationID, "Bridge Integration"); err != nil {
		t.Fatal(err)
	}
	store, err := NewBridgeStore(database)
	if err != nil {
		t.Fatal(err)
	}
	contracttest.BridgeStore(t, store, organizationID)
}
