package postgres

import "testing"

// Requirements: REQ-FOUNDATION-001, SEC-GUARD-001.
func TestEmbeddedMigrationsAreOrderedAndChecksummed(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 4 {
		t.Fatalf("expected 4 platform migrations, got %d", len(migrations))
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
