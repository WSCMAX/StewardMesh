package postgres

import (
	"strings"
	"testing"
)

// Requirements: REQ-FOUNDATION-001, SEC-GUARD-001, REQ-PEOPLE-001, REQ-DIRECTORY-EXPANSION-001.
func TestEmbeddedMigrationsAreOrderedAndChecksummed(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 9 {
		t.Fatalf("expected 9 platform migrations, got %d", len(migrations))
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
