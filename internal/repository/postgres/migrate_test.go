package postgres

import (
	"strings"
	"testing"
)

// Requirements: REQ-FOUNDATION-001, SEC-GUARD-001, REQ-PEOPLE-001,
// REQ-DIRECTORY-EXPANSION-001, REQ-DIRECTORY-EXPANSION-002, REQ-DIRECTORY-EXPANSION-005, REQ-ATLAS-001, REQ-THREADS-001, REQ-STORAGE-001, REQ-LEDGER-001,
// REQ-HORIZON-001, REQ-ATLAS-CODES-001, REQ-ATLAS-MODELS-001, REQ-ATLAS-CATALOG-001, REQ-PATTERNS-001, REQ-STACK-001, REQ-SIGNALS-001, REQ-REACH-001, REQ-EXCHANGE-001.
// Features: lifecycle.planning, inventory.identifiers, inventory.models, inventory.catalog, templates.schemas, software.licenses, alerts.rules, messaging.delivery, migration.packages.
func TestEmbeddedMigrationsAreOrderedAndChecksummed(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 49 {
		t.Fatalf("expected 49 platform migrations, got %d", len(migrations))
	}
	for index, migration := range migrations {
		expectedVersion := int64(index + 1)
		if migration.version != expectedVersion {
			t.Fatalf("expected migration %d, got %d", expectedVersion, migration.version)
		}
		if len(migration.checksum) != 64 {
			t.Fatalf("expected SHA-256 checksum for migration %d", migration.version)
		}
	}
}

func TestDirectoryExchangeMigrationScopesStableIDsByOrganization(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	contents := migrations[38].contents
	for _, expected := range []string{
		"REQ-DIRECTORY-EXPANSION-005", "REQ-EXCHANGE-001", "directory_managed_groups_pkey",
		"directory_managed_memberships_pkey", "PRIMARY KEY (organization_id, id)",
		"DROP CONSTRAINT directory_managed_groups_organization_id_id_key",
		"DROP CONSTRAINT directory_managed_memberships_organization_id_id_key",
		"ADD CONSTRAINT directory_managed_memberships_organization_id_group_id_fkey",
	} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("Directory Exchange organization-key migration is missing %q", expected)
		}
	}
}

func TestExchangePatternsMigrationPreservesLegacyReceiptsWhileAddingOnePointOne(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	contents := migrations[34].contents
	for _, expected := range []string{
		"REQ-PATTERNS-001", "REQ-EXCHANGE-001", "ADD COLUMN progress", "jsonb_array_length(progress) <= 10000",
		"DROP CONSTRAINT exchange_packages_check4", "DROP CONSTRAINT exchange_packages_check6",
		"exchange_packages_records_counts_check", "exchange_packages_terminal_progress_check",
		"exchange_packages_nonterminal_holding_check", "exchange_packages_schema_version_check",
		"schema_version IN ('1.0', '1.1')", "Existing 1.0 rows",
	} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("Exchange Patterns migration is missing %q", expected)
		}
	}
}

func TestReachDeliveryClaimMigrationPrecedesExternalSideEffects(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	var contents string
	for _, migration := range migrations {
		if migration.version == 36 {
			contents = migration.contents
		}
	}
	for _, expected := range []string{"REQ-REACH-001", "claim_token", "claimed_at", "reach_messages_claim_pair", "reach_messages_claim_recovery_idx"} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("Reach claim migration is missing %q", expected)
		}
	}
}

func TestSignalsSubscriptionPortabilityMigrationPreservesExistingRows(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	contents := migrations[36].contents
	for _, expected := range []string{"REQ-SIGNALS-001", "REQ-EXCHANGE-001", "ADD COLUMN revision", "UPDATE signal_subscriptions SET updated_at = created_at", "updated_at >= created_at"} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("Signals subscription portability migration is missing %q", expected)
		}
	}
}

func TestReachExchangeMigrationMakesImportedProvidersInert(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	contents := migrations[37].contents
	for _, expected := range []string{
		"REQ-REACH-001", "REQ-EXCHANGE-001", "ALTER COLUMN endpoint_id DROP NOT NULL", "ALTER COLUMN secret_ref DROP NOT NULL",
		"reach_providers_endpoint_id_optional_check", "reach_providers_secret_ref_optional_check", "reach_providers_enabled_configuration_check",
	} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("Reach Exchange inert-provider migration is missing %q", expected)
		}
	}
}

func TestSignalsMigrationAddsDurableRulesAlertsHistoryAndDeliveryQueue(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	contents := migrations[30].contents
	for _, expected := range []string{
		"REQ-SIGNALS-001", "alerts.rules", "GitHub: #11", "CREATE TABLE signal_rules",
		"forecast_over_budget", "CREATE TABLE signal_alerts", "deduplication_key",
		"CREATE TABLE signal_alert_history", "CREATE TABLE signal_subscriptions",
		"CREATE TABLE signal_deliveries", "signals.read", "signals.write",
	} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("Signals migration is missing %q", expected)
		}
	}
}

func TestGrouperMigrationAddsDurableNormalizedGroupGraph(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	contents := migrations[29].contents
	for _, expected := range []string{
		"REQ-DIRECTORY-EXPANSION-005", "integrations.protocols", "CREATE TABLE directory_managed_groups",
		"CREATE TABLE directory_managed_memberships", "source_system_id", "source_record_id",
		"kind IN ('identity', 'group', 'membership')", "member_kind IN ('subject', 'group')",
		"REFERENCES directory_managed_groups", "ON DELETE CASCADE",
	} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("Grouper directory graph migration is missing %q", expected)
		}
	}
}

func TestExchangeMigrationAddsBoundedPackageReceiptsWithoutExpandingRoles(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	contents := migrations[31].contents
	if migrations[31].checksum != "628bf882c9632690c5d365cc8c9272011fc114b2b22469947d414f7b670bd0a2" {
		t.Fatalf("applied Exchange migration 0032 changed: %s", migrations[31].checksum)
	}
	for _, expected := range []string{
		"REQ-EXCHANGE-001", "migration.packages", "GitHub: #9", "CREATE TABLE exchange_packages",
		"archive_sha256", "size_bytes BETWEEN 1 AND 33554432", "jsonb_array_length(records) <= 10000",
		"status IN ('processing', 'completed', 'holding', 'failed')",
		"status <> 'processing' OR (created_count = 0", "status <> 'failed' OR (created_count = 0",
		"exchange_packages_history_idx",
	} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("Exchange migration is missing %q", expected)
		}
	}
	for _, forbidden := range []string{"guard_policy_bundle_permissions", "migration.read", "migration.write"} {
		if strings.Contains(contents, forbidden) {
			t.Fatalf("Exchange migration must not silently expand deployed role permissions with %q", forbidden)
		}
	}
	if strings.Contains(contents, "progress JSONB") {
		t.Fatal("immutable migration 0032 must not contain recovery state added by a later release")
	}
}

func TestReachMigrationAddsProviderSafeMessagingAndAdministratorPermissions(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	contents := migrations[32].contents
	for _, expected := range []string{
		"REQ-REACH-001", "messaging.delivery", "GitHub: #12", "CREATE TABLE reach_providers", "secret_ref",
		"CREATE TABLE reach_templates", "CREATE TABLE reach_subscriber_groups", "CREATE TABLE reach_messages",
		"CREATE TABLE reach_delivery_attempts", "CREATE TABLE reach_provider_tests", "messaging.read", "messaging.write",
	} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("Reach migration is missing %q", expected)
		}
	}
}

func TestBridgeMigrationStoresOnlyCredentialHashesAndBoundedOAuthState(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	var contents string
	for _, migration := range migrations {
		if migration.version == 34 {
			contents = migration.contents
		}
	}
	for _, expected := range []string{"SEC-MCP-001", "integrations.protocols", "GitHub: #14", "CREATE TABLE bridge_oauth_clients", "access_token_hash", "refresh_token_hash", "CREATE TABLE bridge_mcp_confirmations", "token_hash", "CREATE TABLE bridge_rate_windows"} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("Bridge migration is missing %q", expected)
		}
	}
	for _, forbidden := range []string{"access_token TEXT", "refresh_token TEXT", "client_secret"} {
		if strings.Contains(contents, forbidden) {
			t.Fatalf("Bridge migration must not persist %q", forbidden)
		}
	}
}

func TestDirectoryImportMigrationAddsDurablePlansAttemptsMappingsAndLeases(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	contents := migrations[27].contents
	for _, expected := range []string{
		"REQ-DIRECTORY-EXPANSION-002", "integrations.protocols", "GitHub: #25",
		"CREATE TABLE directory_import_items", "CREATE TABLE directory_import_attempts",
		"CREATE TABLE directory_import_mappings", "idempotency_hash", "lease_token",
		"integrations.read", "integrations.write", "lower(btrim(r.name)) = 'administrator'",
	} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("directory import migration is missing %q", expected)
		}
	}
}

func TestStackMigrationAddsSoftwareEntitlementsAndAdministratorPermissions(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	var contents string
	for _, migration := range migrations {
		if migration.version == 29 {
			contents = migration.contents
			break
		}
	}
	if contents == "" {
		t.Fatal("Stack migration 0029 is missing")
	}
	for _, expected := range []string{
		"REQ-STACK-001", "software.licenses", "GitHub: #7", "CREATE TABLE stack_products", "CREATE TABLE stack_versions",
		"CREATE TABLE stack_installations", "stack_installations_active_unique", "CREATE TABLE stack_licenses", "document_ids JSONB",
		"CREATE TABLE stack_assignments", "stack_assignments_active_unique", "'software.read'", "'software.write'", "r.source = 'builtin'",
	} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("Stack migration is missing %q", expected)
		}
	}
}

func TestStackAssignmentRoomMigrationAllowsLabAssignees(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) < 44 {
		t.Fatal("Stack assignment room migration 0044 is missing")
	}
	contents := migrations[43].contents
	for _, expected := range []string{
		"REQ-STACK-001", "software.licenses", "stack_assignments_assignee_kind_check", "'room'",
	} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("Stack room assignment migration is missing %q", expected)
		}
	}
}

func TestPatternsMigrationAddsImmutableOrganizationScopedTemplateVersions(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	contents := migrations[26].contents
	for _, expected := range []string{
		"REQ-PATTERNS-001", "templates.schemas", "GitHub: #8", "CREATE TABLE pattern_template_versions",
		"PRIMARY KEY (organization_id, id, version)", "fields JSONB", "pattern_template_name_unique",
	} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("Patterns migration is missing %q", expected)
		}
	}
}

func TestAtlasModelDefaultProvenanceMigrationSnapshotsExistingLinks(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	contents := migrations[25].contents
	for _, expected := range []string{
		"REQ-ATLAS-MODELS-001", "inventory.models", "GitHub: #74",
		"ADD COLUMN model_context JSONB", "modelRevision", "defaultsEffectiveAt",
		"sourceSystemId", "sourceRecordId", "CURRENT_TIMESTAMP", "asset.kind = model.kind",
		"model_context_consistency_check",
	} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("Atlas model provenance migration is missing %q", expected)
		}
	}
}

func TestAtlasModelBulkIntakeMigrationPreservesDeploymentNotes(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	contents := migrations[24].contents
	for _, expected := range []string{
		"REQ-ATLAS-MODELS-001", "inventory.models", "GitHub: #73",
		"ADD COLUMN deployment_notes TEXT", "char_length(deployment_notes) <= 2000",
	} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("Atlas Models bulk intake migration is missing %q", expected)
		}
	}
}

func TestAtlasModelsMigrationAddsCatalogDefaultsAndAssetLinks(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	contents := migrations[22].contents
	for _, expected := range []string{
		"REQ-ATLAS-MODELS-001", "inventory.models", "CREATE TABLE atlas_models",
		"UNIQUE (organization_id, normalized_manufacturer, normalized_name, normalized_model_number)",
		"specifications JSONB", "warranty_months", "useful_life_months",
		"ADD COLUMN model_id TEXT", "atlas_assets_model_fk", "CREATE INDEX atlas_assets_model_idx",
	} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("Atlas Models migration is missing %q", expected)
		}
	}
}

func TestAtlasCatalogMigrationExtendsModelsWithConfigurationsPricesAndUpgradePaths(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	contents := migrations[23].contents
	for _, expected := range []string{
		"REQ-ATLAS-CATALOG-001", "inventory.catalog", "CREATE TABLE atlas_catalog_configurations",
		"REFERENCES atlas_models (organization_id, id)", "CREATE TABLE atlas_catalog_prices",
		"amount_minor BETWEEN 0 AND 9007199254740991", "effective_to >= effective_from",
		"CREATE TABLE atlas_catalog_upgrade_paths", "relationship_kind IN ('successor', 'replacement', 'upgrade')",
		"from_configuration_id IS DISTINCT FROM to_configuration_id", "atlas_catalog_upgrade_paths_identity_idx",
	} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("Atlas Catalog migration is missing %q", expected)
		}
	}
}

func TestAtlasCodesAuditProvenanceMigrationPreservesOriginalMutationIdentity(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	contents := migrations[21].contents
	for _, expected := range []string{
		"REQ-ATLAS-CODES-001", "inventory.identifiers", "created_correlation_id", "updated_by",
		"updated_correlation_id", "atlas.identifier.deactivated", "atlas.identifier.replaced",
		"ALTER COLUMN created_correlation_id SET NOT NULL", "migration:atlas-codes-provenance",
	} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("Atlas Codes audit provenance migration is missing %q", expected)
		}
	}
}

func TestAtlasCodesMigrationAddsScopedIdentifierHistoryAndConflictBoundaries(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	contents := migrations[20].contents
	for _, expected := range []string{
		"REQ-ATLAS-CODES-001", "inventory.identifiers", "CREATE TABLE atlas_asset_identifiers",
		"REFERENCES atlas_assets (organization_id, id)", "DEFERRABLE INITIALLY DEFERRED",
		"REFERENCES atlas_asset_identifiers (organization_id, asset_id, id)",
		"status IN ('active', 'replaced', 'deactivated')", "octet_length(normalized_value) BETWEEN 1 AND 128",
		"octet_length(normalized_value) BETWEEN 1 AND 512", "normalized_value ~ '^[ -~]+$'",
		"CREATE UNIQUE INDEX atlas_asset_identifiers_active_value_idx",
		"CREATE UNIQUE INDEX atlas_asset_identifiers_active_primary_idx",
		"WHERE status = 'active'", "CREATE INDEX atlas_asset_identifiers_asset_history_idx",
	} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("Atlas Codes migration is missing %q", expected)
		}
	}
}

func TestHorizonMigrationsAddVersionedPlansAndAdministratorPermissions(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"REQ-HORIZON-001", "CREATE TABLE horizon_plans", "expected_useful_life_months",
		"UNIQUE (organization_id, asset_id, scenario)", "CREATE TABLE horizon_plan_versions",
		"effective_from DESC", "lifecycle.planning",
	} {
		if !strings.Contains(migrations[17].contents, expected) {
			t.Fatalf("Horizon planning migration is missing %q", expected)
		}
	}
	for _, expected := range []string{
		"REQ-HORIZON-001", "'planning.read'", "'planning.write'", "r.source = 'builtin'", "ON CONFLICT",
	} {
		if !strings.Contains(migrations[18].contents, expected) {
			t.Fatalf("Horizon permission migration is missing %q", expected)
		}
	}
	for _, expected := range []string{
		"REQ-HORIZON-001", "horizon_plans_exact_money_check", "horizon_plan_versions_exact_money_check", "9007199254740991",
	} {
		if !strings.Contains(migrations[19].contents, expected) {
			t.Fatalf("Horizon exact-money migration is missing %q", expected)
		}
	}
}

func TestLedgerMigrationsAddFinancialRecordsAndAdministratorPermissions(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"REQ-LEDGER-001", "CREATE TABLE ledger_vendors", "CREATE TABLE ledger_purchase_orders",
		"asset_ids TEXT[]", "receipt_document_ids TEXT[]", "CREATE TABLE ledger_contracts",
		"operational_status", "financial_status", "CREATE TABLE ledger_commitments",
		"CREATE TABLE ledger_budgets", "CREATE TABLE ledger_costs", "ledger_costs_source_idx",
	} {
		if !strings.Contains(migrations[15].contents, expected) {
			t.Fatalf("Ledger finance migration is missing %q", expected)
		}
	}
	for _, expected := range []string{"REQ-LEDGER-001", "'finance.read'", "'finance.write'", "r.source = 'builtin'", "ON CONFLICT"} {
		if !strings.Contains(migrations[16].contents, expected) {
			t.Fatalf("Ledger permission migration is missing %q", expected)
		}
	}
}

func TestVaultMigrationsAddPrivateMetadataAndAdministratorPermissions(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"REQ-STORAGE-001", "CREATE TABLE vault_blobs", "UNIQUE (organization_id, object_key)",
		"sha256", "source_system_id", "resource_type",
	} {
		if !strings.Contains(migrations[13].contents, expected) {
			t.Fatalf("Vault metadata migration is missing %q", expected)
		}
	}
	for _, expected := range []string{
		"REQ-STORAGE-001", "'storage.read'", "'storage.write'", "r.source = 'builtin'", "ON CONFLICT",
	} {
		if !strings.Contains(migrations[14].contents, expected) {
			t.Fatalf("Vault administrator permission migration is missing %q", expected)
		}
	}
}

func TestThreadsAdministratorPermissionMigrationUpgradesOnlyBuiltInRoleBundles(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) < 13 {
		t.Fatal("Threads administrator permission migration is missing")
	}
	contents := migrations[12].contents
	for _, expected := range []string{
		"REQ-THREADS-001", "SEC-GUARD-001", "guard_policy_bundle_permissions",
		"'goals.write'", "r.source = 'builtin'", "lower(btrim(r.name)) = 'administrator'", "ON CONFLICT",
	} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("Threads administrator permission migration is missing %q", expected)
		}
	}
}

func TestThreadsMigrationAddsScopedHierarchiesRulesAndLinks(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) < 12 {
		t.Fatal("Threads migration is missing")
	}
	contents := migrations[11].contents
	for _, expected := range []string{
		"REQ-THREADS-001", "CREATE TABLE threads_tags", "REFERENCES threads_tags",
		"CREATE TABLE threads_goals", "REFERENCES threads_goals", "CREATE TABLE threads_tag_rules",
		"mode IN ('include', 'suppress')", "CREATE TABLE threads_goal_links",
		"target_type IN ('asset', 'purchase')", "PRIMARY KEY (organization_id, target_type, target_id, goal_id)",
	} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("Threads migration is missing %q", expected)
		}
	}
}

func TestAtlasMigrationAddsDurableScopedAssetsAndLifecycle(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) < 11 {
		t.Fatal("Atlas migration is missing")
	}
	contents := migrations[10].contents
	for _, expected := range []string{
		"REQ-ATLAS-001", "CREATE TABLE atlas_assets", "PRIMARY KEY (organization_id, id)",
		"CREATE UNIQUE INDEX atlas_assets_asset_tag_idx", "CREATE TABLE atlas_asset_lifecycle_events",
		"UNIQUE (organization_id, asset_id, revision)", "REFERENCES people_rooms",
	} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("Atlas migration is missing %q", expected)
		}
	}
}

func TestGuardSAMLMigrationTracksOneTimeRequestsWithoutAssertions(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) < 10 {
		t.Fatal("Guard SAML migration is missing")
	}
	contents := migrations[9].contents
	for _, expected := range []string{
		"SEC-GUARD-001",
		"^(oidc|saml):[a-f0-9]{32}$",
		"CREATE TABLE guard_saml_requests",
		"state_hash BYTEA NOT NULL",
		"request_id TEXT NOT NULL",
		"CHECK (expires_at > created_at)",
	} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("Guard SAML migration is missing %q", expected)
		}
	}
	for _, forbidden := range []string{"assertion", "name_id", "attribute"} {
		if strings.Contains(strings.ToLower(contents), forbidden) {
			t.Fatalf("Guard SAML request tracking must not persist %q", forbidden)
		}
	}
}

func TestGuardCustomRoleMigration(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) < 9 {
		t.Fatal("Guard custom role migration is missing")
	}
	contents := migrations[8].contents
	for _, expected := range []string{"SEC-GUARD-001", "ADD COLUMN source", "'builtin'", "'local'", "lower(btrim(name))"} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("Guard custom role migration is missing %q", expected)
		}
	}
}

func TestGuardResourceOwnershipMigrationEnforcesWriteLockState(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) < 8 {
		t.Fatal("Guard resource ownership migration is missing")
	}
	contents := migrations[7].contents
	for _, expected := range []string{
		"CREATE TABLE guard_resource_ownership",
		"write_locked BOOLEAN NOT NULL DEFAULT TRUE",
		"guard_resource_ownership_claim_check",
		"NOT write_locked AND claimed_by IS NOT NULL",
		"CREATE UNIQUE INDEX guard_resource_ownership_source_idx",
	} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("Guard resource ownership migration is missing %q", expected)
		}
	}
}

func TestGuardOIDCMigrationSeparatesExternalIdentityFromLocalCredentials(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) < 7 {
		t.Fatal("Guard OpenID Connect migration is missing")
	}
	contents := migrations[6].contents
	for _, expected := range []string{
		"ALTER COLUMN password_hash DROP NOT NULL",
		"password_hash IS NULL",
		"ADD COLUMN source TEXT NOT NULL DEFAULT 'local'",
		"CREATE TABLE guard_external_identities",
		"PRIMARY KEY (organization_id, issuer, subject)",
		"REFERENCES guard_accounts (organization_id, id)",
	} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("Guard OpenID Connect migration is missing %q", expected)
		}
	}
}

func TestDirectoryExpansionMigrationEnforcesLocationHierarchy(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) < 6 {
		t.Fatalf("directory expansion migration is missing")
	}
	contents := migrations[5].contents
	for _, expected := range []string{
		"people_sites_address_complete",
		"CREATE TABLE people_buildings",
		"UNIQUE (organization_id, site_id, id)",
		"CREATE TABLE people_rooms",
		"FOREIGN KEY (organization_id, site_id, building_id)",
		"REFERENCES people_buildings (organization_id, site_id, id)",
		"UNIQUE (organization_id, building_id, normalized_number)",
	} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("directory expansion migration is missing %q", expected)
		}
	}
}
