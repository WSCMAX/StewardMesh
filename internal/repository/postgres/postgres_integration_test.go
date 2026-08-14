package postgres

// Requirements: REQ-FOUNDATION-001, SEC-GUARD-001, REQ-PEOPLE-001,
// REQ-DIRECTORY-EXPANSION-001, REQ-DIRECTORY-EXPANSION-002, REQ-DIRECTORY-EXPANSION-003, REQ-DIRECTORY-EXPANSION-005, REQ-DIRECTORY-EXPANSION-006, REQ-DIRECTORY-EXPANSION-008, REQ-ATLAS-001, REQ-ATLAS-MODELS-001,
// REQ-THREADS-001, REQ-STORAGE-001, REQ-LEDGER-001, REQ-HORIZON-001,
// REQ-ATLAS-CODES-001, REQ-ATLAS-CATALOG-001, REQ-PATTERNS-001, REQ-STACK-001, REQ-SIGNALS-001, REQ-REACH-001, REQ-EXCHANGE-001, REQ-API-001, SEC-MCP-001.
// Features: lifecycle.planning, inventory.models, inventory.identifiers, templates.schemas, alerts.rules, messaging.delivery, migration.packages, integrations.protocols, threads.relationships.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/bootstrap"
	"github.com/maxlemke/stewardmesh/internal/bridge"
	"github.com/maxlemke/stewardmesh/internal/catalog"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/repository/contracttest"
)

func TestCatalogStoreIntegration(t *testing.T) {
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
	unique := fmt.Sprintf("catalog-integration-%d", time.Now().UnixNano())
	organizations, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	organizationService, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := organizationService.EnsureOrganization(ctx, unique, "Catalog Integration"); err != nil {
		t.Fatal(err)
	}
	atlasStore, err := NewAtlasStore(database)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 13, 18, 0, 0, 0, time.UTC)
	for _, id := range []string{"model-current", "model-next"} {
		if _, err := atlasStore.CreateModel(ctx, domain.AssetModel{
			ID: id, OrganizationID: unique, Manufacturer: "Example", Name: id, Kind: "server", Status: "active",
			Specifications: map[string]string{}, Revision: 1, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	store, err := NewCatalogStore(database)
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := store.CreateConfiguration(ctx, catalog.Configuration{
		ID: "configuration-one", OrganizationID: unique, ModelID: "model-current", Name: "Standard", Status: catalog.StatusActive,
		Specifications: map[string]string{"memory_gb": "64"}, Revision: 1, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	price, err := store.CreatePrice(ctx, catalog.Price{
		ID: "price-one", OrganizationID: unique, ModelID: "model-current", ConfigurationID: configuration.ID,
		Kind: catalog.PriceKindList, AmountMinor: 100, Currency: "USD", EffectiveFrom: now, Revision: 1, CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	path, err := store.CreateUpgradePath(ctx, catalog.UpgradePath{
		ID: "path-one", OrganizationID: unique, FromModelID: "model-current", FromConfigurationID: configuration.ID,
		ToModelID: "model-next", Kind: catalog.UpgradeKindSuccessor, EffectiveFrom: now, Revision: 1, CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Snapshot(ctx, unique)
	if err != nil || len(snapshot.Configurations) != 1 || len(snapshot.Prices) != 1 || len(snapshot.UpgradePaths) != 1 {
		t.Fatalf("unexpected Catalog snapshot %#v err=%v", snapshot, err)
	}
	if exact, err := store.GetConfiguration(ctx, unique, configuration.ID); err != nil || exact.ID != configuration.ID {
		t.Fatalf("get Catalog configuration %#v err=%v", exact, err)
	}
	if exact, err := store.GetPrice(ctx, unique, price.ID); err != nil || exact.ID != price.ID {
		t.Fatalf("get Catalog price %#v err=%v", exact, err)
	}
	if exact, err := store.GetUpgradePath(ctx, unique, path.ID); err != nil || exact.ID != path.ID {
		t.Fatalf("get Catalog path %#v err=%v", exact, err)
	}
}

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

func TestExchangePatternsMigrationPreservesLegacyOnePointZeroReceipt(t *testing.T) {
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
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	schema := fmt.Sprintf("exchange_upgrade_%d", time.Now().UnixNano())
	if _, err := database.ExecContext(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = database.ExecContext(context.Background(), `SET search_path TO public`)
		_, _ = database.ExecContext(context.Background(), fmt.Sprintf(`DROP SCHEMA %q CASCADE`, schema))
	}()
	if _, err := database.ExecContext(ctx, fmt.Sprintf(`SET search_path TO %q`, schema)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `CREATE TABLE stewardmesh_schema_migrations (
		version BIGINT PRIMARY KEY, name TEXT NOT NULL, checksum TEXT NOT NULL,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		t.Fatal(err)
	}
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:34] {
		if _, err := database.ExecContext(ctx, migration.contents); err != nil {
			t.Fatalf("apply legacy migration %04d: %v", migration.version, err)
		}
		if _, err := database.ExecContext(ctx, `INSERT INTO stewardmesh_schema_migrations (version,name,checksum) VALUES ($1,$2,$3)`, migration.version, migration.name, migration.checksum); err != nil {
			t.Fatal(err)
		}
	}
	unique := "legacy-exchange"
	if _, err := database.ExecContext(ctx, `INSERT INTO organizations (id,name,created_at,updated_at) VALUES ($1,$2,now(),now())`, unique, "Legacy Exchange Integration"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO exchange_packages (
			organization_id,direction,package_id,schema_version,source_system_id,archive_sha256,size_bytes,file_mode,status,
			record_count,file_count,created_count,unchanged_count,holding_count,records,error_code,created_by,created_at,updated_at
		) VALUES ($1,'export',$2,'1.0','legacy-source',repeat('a',64),128,'metadata','completed',1,0,0,1,0,
			'[{
				"type":"stack.product","id":"legacy-record","revision":1,
				"checksum":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				"status":"unchanged","missingDependencies":[],"writeLocked":false
			}]'::jsonb,NULL,'legacy-operator',now(),now())
	`, unique, "legacy-package-"+unique); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, database); err != nil {
		t.Fatalf("forward migration rejected the immutable 0032 checksum or legacy receipt: %v", err)
	}
	store, err := NewExchangeStore(database)
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.ListPackages(ctx, unique, 10)
	if err != nil || len(items) != 1 || items[0].SchemaVersion != "1.0" {
		t.Fatalf("legacy receipt was not honestly readable after migration: %#v err=%v", items, err)
	}
	var acceptedOnePointOne bool
	if err := database.QueryRowContext(ctx, `SELECT pg_get_constraintdef(constraint_row.oid) LIKE '%1.1%'
		FROM pg_constraint constraint_row
		JOIN pg_class table_row ON table_row.oid = constraint_row.conrelid
		JOIN pg_namespace schema_row ON schema_row.oid = table_row.relnamespace
		WHERE schema_row.nspname=$1 AND table_row.relname='exchange_packages'
		  AND constraint_row.conname='exchange_packages_schema_version_check'`, schema).Scan(&acceptedOnePointOne); err != nil {
		t.Fatal(err)
	}
	if !acceptedOnePointOne {
		t.Fatal("migration did not add schema 1.1 receipt storage")
	}
	var progressDefault string
	if err := database.QueryRowContext(ctx, `SELECT column_default FROM information_schema.columns WHERE table_schema=$1 AND table_name='exchange_packages' AND column_name='progress'`, schema).Scan(&progressDefault); err != nil {
		t.Fatal(err)
	}
	if progressDefault != "'[]'::jsonb" {
		t.Fatalf("migration did not add durable progress safely: %q", progressDefault)
	}
	var retained, removed int
	if err := database.QueryRowContext(ctx, `SELECT
		count(*) FILTER (WHERE constraint_row.conname IN ('exchange_packages_check3','exchange_packages_check5')),
		count(*) FILTER (WHERE constraint_row.conname IN ('exchange_packages_check4','exchange_packages_check6'))
		FROM pg_constraint constraint_row
		JOIN pg_class table_row ON table_row.oid = constraint_row.conrelid
		JOIN pg_namespace schema_row ON schema_row.oid = table_row.relnamespace
		WHERE schema_row.nspname=$1 AND table_row.relname='exchange_packages'`, schema).Scan(&retained, &removed); err != nil {
		t.Fatal(err)
	}
	if retained != 2 || removed != 0 {
		t.Fatalf("forward migration removed the wrong legacy checks: retained=%d removed=%d", retained, removed)
	}
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
	unique := fmt.Sprintf("postgres-%d", time.Now().UnixNano())
	contracttest.AtlasStore(t, store, organizationID, unique)
	peopleStore, err := NewPeopleStore(database)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	visibleSiteID, err := foundation.NewCorrelationID()
	if err != nil {
		t.Fatal(err)
	}
	hiddenSiteID, err := foundation.NewCorrelationID()
	if err != nil {
		t.Fatal(err)
	}
	visibleUserID, err := foundation.NewCorrelationID()
	if err != nil {
		t.Fatal(err)
	}
	visibleSite := people.Site{ID: visibleSiteID, OrganizationID: organizationID, Name: "Visible Graph Site " + unique,
		NormalizedName: strings.ToLower("Visible Graph Site " + unique), Status: people.StatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now}
	hiddenSite := people.Site{ID: hiddenSiteID, OrganizationID: organizationID, Name: "Hidden Graph Site " + unique,
		NormalizedName: strings.ToLower("Hidden Graph Site " + unique), Status: people.StatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if _, err := peopleStore.CreateSite(ctx, visibleSite); err != nil {
		t.Fatal(err)
	}
	if _, err := peopleStore.CreateSite(ctx, hiddenSite); err != nil {
		t.Fatal(err)
	}
	visibleUser := people.Identity{ID: visibleUserID, OrganizationID: organizationID, Kind: people.IdentityPerson,
		DisplayName: "Visible Graph User " + unique, NormalizedName: strings.ToLower("Visible Graph User " + unique),
		Email: "visible-graph-user-" + unique + "@example.test", NormalizedEmail: "visible-graph-user-" + unique + "@example.test",
		SiteID: visibleSite.ID, Status: people.StatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if _, err := peopleStore.CreateIdentity(ctx, visibleUser); err != nil {
		t.Fatal(err)
	}
	contracttest.AtlasGraphDirectoryStore(t, store, organizationID, unique, visibleSite.ID, hiddenSite.ID, visibleUser.ID)
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

func TestBridgeStoreSerializesMaximumClients(t *testing.T) {
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
	organizationID := fmt.Sprintf("bridge-capacity-%d", time.Now().UnixNano())
	organizations, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	organizationService, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := organizationService.EnsureOrganization(ctx, organizationID, "Bridge Capacity Integration"); err != nil {
		t.Fatal(err)
	}
	store, err := NewBridgeStore(database)
	if err != nil {
		t.Fatal(err)
	}

	const attempts = bridge.MaximumClients + 25
	start := make(chan struct{})
	results := make(chan error, attempts)
	var workers sync.WaitGroup
	for index := 0; index < attempts; index++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-start
			_, err := store.CreateClient(ctx, bridge.Client{
				ID: fmt.Sprintf("%032x", index+1), OrganizationID: organizationID, Name: fmt.Sprintf("Concurrent client %03d", index),
				RedirectURIs: []string{fmt.Sprintf("https://client-%03d.example.test/callback", index)}, AllowedScopes: []bridge.Scope{bridge.ScopeMCPResources},
				CreatedBy: "capacity-test", CreatedAt: time.Now().UTC(),
			})
			results <- err
		}(index)
	}
	close(start)
	workers.Wait()
	close(results)
	succeeded, rejected := 0, 0
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, bridge.ErrConflict):
			rejected++
		default:
			t.Fatalf("unexpected concurrent Bridge creation error: %v", err)
		}
	}
	if succeeded != bridge.MaximumClients || rejected != attempts-bridge.MaximumClients {
		t.Fatalf("capacity race succeeded=%d rejected=%d", succeeded, rejected)
	}
	items, err := store.ListClients(ctx, organizationID, bridge.PageRequest{Limit: bridge.MaximumAdministrationPageSize})
	if err != nil || len(items) != bridge.MaximumClients {
		t.Fatalf("persisted concurrent Bridge clients=%d err=%v", len(items), err)
	}
}
