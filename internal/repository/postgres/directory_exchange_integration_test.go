package postgres

// Requirements: REQ-EXCHANGE-001, REQ-DIRECTORY-EXPANSION-005. Features: migration.packages, integrations.protocols.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/bootstrap"
	"github.com/maxlemke/stewardmesh/internal/directoryexpansion"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

type directoryExchangePostgresHasher struct{}

func (directoryExchangePostgresHasher) Hash(string) (string, error) {
	return "directory-postgres-hash", nil
}
func (directoryExchangePostgresHasher) Verify(string, string) (bool, bool, error) {
	return true, false, nil
}

func TestDirectoryExchangeImporterPostgresIntegration(t *testing.T) {
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
	for _, table := range []string{"directory_managed_groups", "directory_managed_memberships"} {
		var primaryKeyDefinition string
		if err := database.QueryRowContext(ctx, `SELECT pg_get_constraintdef(constraint_row.oid)
			FROM pg_constraint constraint_row
			JOIN pg_class table_row ON table_row.oid=constraint_row.conrelid
			JOIN pg_namespace schema_row ON schema_row.oid=table_row.relnamespace
			WHERE schema_row.nspname=current_schema() AND table_row.relname=$1 AND constraint_row.contype='p'`, table).Scan(&primaryKeyDefinition); err != nil {
			t.Fatal(err)
		}
		if primaryKeyDefinition != "PRIMARY KEY (organization_id, id)" {
			t.Fatalf("%s has unexpected primary key %q", table, primaryKeyDefinition)
		}
		var compositeUniqueIndexes int
		if err := database.QueryRowContext(ctx, `SELECT count(*)
			FROM pg_index index_row
			JOIN pg_class table_row ON table_row.oid=index_row.indrelid
			JOIN pg_namespace schema_row ON schema_row.oid=table_row.relnamespace
			WHERE schema_row.nspname=current_schema() AND table_row.relname=$1 AND index_row.indisunique
			  AND pg_get_indexdef(index_row.indexrelid) LIKE '% USING btree (organization_id, id)'`, table).Scan(&compositeUniqueIndexes); err != nil {
			t.Fatal(err)
		}
		if compositeUniqueIndexes != 1 {
			t.Fatalf("%s retains %d composite organization/id unique indexes; expected only its primary key", table, compositeUniqueIndexes)
		}
	}
	organizationID := fmt.Sprintf("directory-exchange-%d", time.Now().UnixNano())
	organizations, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	organizationService, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := organizationService.EnsureOrganization(ctx, organizationID, "Directory Exchange Integration"); err != nil {
		t.Fatal(err)
	}
	store, err := NewDirectoryImportStore(database)
	if err != nil {
		t.Fatal(err)
	}
	auditor, err := NewAuditor(database)
	if err != nil {
		t.Fatal(err)
	}
	guardService, err := guard.NewService(repository.NewMemoryGuardStore(), directoryExchangePostgresHasher{}, auditor, nil,
		guard.ServiceConfig{OrganizationID: organizationID})
	if err != nil {
		t.Fatal(err)
	}
	target, importer, err := directoryexpansion.NewGroupTargetWithExchangeImporter(store, guardService, auditor,
		directoryexpansion.GroupTargetExchangeConfig{OrganizationID: organizationID})
	if err != nil {
		t.Fatal(err)
	}
	operationAt := time.Date(2026, time.August, 13, 23, 0, 0, 0, time.UTC)
	createdAt := time.Date(2020, time.January, 2, 3, 4, 5, 678000000, time.UTC)
	updatedAt := time.Date(2025, time.December, 6, 7, 8, 9, 123000000, time.UTC)
	group := directoryexpansion.ManagedGroup{ID: fmt.Sprintf("%032x", time.Now().UnixNano()), SourceSystemID: "directory-source",
		SourceRecordID: "portable-group", Name: "portable", DisplayName: "Portable group", Description: "PostgreSQL round trip",
		Status: "inactive", Metadata: map[string]string{"origin": "grouper", "scope": "all"}, Revision: 37,
		CreatedAt: createdAt, UpdatedAt: updatedAt}
	operation := directoryexpansion.ExchangeImportOperation{Token: "directory-postgres-import", OccurredAt: operationAt}
	result, err := importer.ImportManagedGroup(ctx, operation, group)
	if err != nil || !result.Committed || !result.Created {
		t.Fatalf("import PostgreSQL Directory group: %#v err=%v", result, err)
	}
	stored, err := target.GetManagedGroup(ctx, group.ID)
	if err != nil || stored.OrganizationID != organizationID || stored.Revision != group.Revision || stored.Metadata["scope"] != "all" ||
		!stored.CreatedAt.Equal(createdAt) || !stored.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("PostgreSQL Directory group was not lossless: %#v err=%v", stored, err)
	}
	membership := directoryexpansion.ManagedMembership{ID: fmt.Sprintf("%032x", time.Now().UnixNano()+1), SourceSystemID: group.SourceSystemID,
		SourceRecordID: "portable-membership", GroupID: group.ID, GroupSourceID: group.SourceRecordID,
		MemberID: fmt.Sprintf("%032x", time.Now().UnixNano()+2), MemberSourceID: "embedded-subject", MemberKind: directoryexpansion.MemberSubject,
		MemberDisplayName: "Embedded subject", Status: "active", Metadata: map[string]string{"membership": "direct"}, Revision: 41,
		CreatedAt: createdAt.Add(time.Hour), UpdatedAt: updatedAt}
	result, err = importer.ImportManagedMembership(ctx, operation, membership)
	if err != nil || !result.Committed || !result.Created {
		t.Fatalf("import PostgreSQL Directory membership: %#v err=%v", result, err)
	}
	replay, err := importer.ImportManagedMembership(ctx, operation, membership)
	if err != nil || !replay.Committed || replay.Created {
		t.Fatalf("replay PostgreSQL Directory membership: %#v err=%v", replay, err)
	}
	snapshot, err := target.ExchangeSnapshot(ctx, 2)
	if err != nil || len(snapshot.Groups) != 1 || len(snapshot.Memberships) != 1 || snapshot.Memberships[0].Revision != membership.Revision {
		t.Fatalf("unexpected PostgreSQL Directory snapshot %#v err=%v", snapshot, err)
	}
	if _, err := target.ExchangeSnapshot(ctx, 1); !errors.Is(err, directoryexpansion.ErrTooLarge) {
		t.Fatalf("expected bounded PostgreSQL snapshot, got %v", err)
	}
	conflict := membership
	conflict.Status = "inactive"
	if _, err := importer.ImportManagedMembership(ctx, operation, conflict); !errors.Is(err, directoryexpansion.ErrConflict) {
		t.Fatalf("expected exact PostgreSQL replay conflict, got %v", err)
	}
	missing := membership
	missing.ID = fmt.Sprintf("%032x", time.Now().UnixNano()+3)
	missing.SourceRecordID = "missing-parent"
	missing.GroupID = fmt.Sprintf("%032x", time.Now().UnixNano()+4)
	missing.OrganizationID = organizationID
	if _, _, err := store.ImportManagedMembership(ctx, missing); !errors.Is(err, directoryexpansion.ErrReferenceMissing) {
		t.Fatalf("expected atomic missing-parent rejection, got %v", err)
	}
	mismatchedSources := membership
	mismatchedSources.ID = fmt.Sprintf("%032x", time.Now().UnixNano()+6)
	mismatchedSources.SourceRecordID = "mismatched-source-membership"
	mismatchedSources.GroupSourceID = "wrong-parent-source"
	mismatchedSources.OrganizationID = organizationID
	if _, _, err := store.ImportManagedMembership(ctx, mismatchedSources); !errors.Is(err, directoryexpansion.ErrConflict) {
		t.Fatalf("expected mismatched parent source rejection, got %v", err)
	}
	if _, err := store.GetManagedMembership(ctx, organizationID, mismatchedSources.ID); !errors.Is(err, directoryexpansion.ErrNotFound) {
		t.Fatalf("mismatched parent source import was not atomic: %v", err)
	}
	memberGroup := group
	memberGroup.ID = fmt.Sprintf("%032x", time.Now().UnixNano()+7)
	memberGroup.OrganizationID = organizationID
	memberGroup.SourceRecordID = "portable-member-group"
	if _, created, err := store.ImportManagedGroup(ctx, memberGroup); err != nil || !created {
		t.Fatalf("import nested member group created=%t err=%v", created, err)
	}
	nested := membership
	nested.ID = fmt.Sprintf("%032x", time.Now().UnixNano()+8)
	nested.OrganizationID = organizationID
	nested.SourceRecordID = "portable-nested-membership"
	nested.MemberID = memberGroup.ID
	nested.MemberKind = directoryexpansion.MemberGroup
	nested.MemberSourceID = "wrong-member-source"
	if _, _, err := store.ImportManagedMembership(ctx, nested); !errors.Is(err, directoryexpansion.ErrConflict) {
		t.Fatalf("expected mismatched nested-member source rejection, got %v", err)
	}
	if _, err := store.GetManagedMembership(ctx, organizationID, nested.ID); !errors.Is(err, directoryexpansion.ErrNotFound) {
		t.Fatalf("mismatched nested-member source import was not atomic: %v", err)
	}
	nested.MemberSourceID = memberGroup.SourceRecordID
	if _, created, err := store.ImportManagedMembership(ctx, nested); err != nil || !created {
		t.Fatalf("import source-consistent nested membership created=%t err=%v", created, err)
	}
	if err := store.DeleteManagedGroup(ctx, organizationID, memberGroup.ID, memberGroup.Revision); err != nil {
		t.Fatalf("delete nested member group: %v", err)
	}
	if _, err := store.GetManagedMembership(ctx, organizationID, nested.ID); !errors.Is(err, directoryexpansion.ErrNotFound) {
		t.Fatalf("nested member-group delete did not remove durable membership: %v", err)
	}
	if _, err := store.GetManagedGroup(ctx, organizationID, group.ID); err != nil {
		t.Fatalf("nested member-group delete removed its parent: %v", err)
	}
	var auditCount int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE organization_id=$1 AND correlation_id=$2`, organizationID, operation.Token).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 2 {
		t.Fatalf("expected two deterministic Directory audits, got %d", auditCount)
	}
	secondOrganizationID := organizationID + "-second"
	if _, _, err := organizationService.EnsureOrganization(ctx, secondOrganizationID, "Directory Exchange Integration Second"); err != nil {
		t.Fatal(err)
	}
	secondGuard, err := guard.NewService(repository.NewMemoryGuardStore(), directoryExchangePostgresHasher{}, auditor, nil,
		guard.ServiceConfig{OrganizationID: secondOrganizationID})
	if err != nil {
		t.Fatal(err)
	}
	secondTarget, secondImporter, err := directoryexpansion.NewGroupTargetWithExchangeImporter(store, secondGuard, auditor,
		directoryexpansion.GroupTargetExchangeConfig{OrganizationID: secondOrganizationID})
	if err != nil {
		t.Fatal(err)
	}
	secondGroupResult, err := secondImporter.ImportManagedGroup(ctx, operation, group)
	if err != nil || !secondGroupResult.Committed || !secondGroupResult.Created {
		t.Fatalf("same stable group ID did not import into second organization: %#v err=%v", secondGroupResult, err)
	}
	secondMembershipResult, err := secondImporter.ImportManagedMembership(ctx, operation, membership)
	if err != nil || !secondMembershipResult.Committed || !secondMembershipResult.Created {
		t.Fatalf("same stable membership ID did not import into second organization: %#v err=%v", secondMembershipResult, err)
	}
	secondStored, err := secondTarget.GetManagedMembership(ctx, membership.ID)
	if err != nil || secondStored.OrganizationID != secondOrganizationID || secondStored.ID != membership.ID || secondStored.GroupID != group.ID {
		t.Fatalf("second organization did not retain stable Directory IDs: %#v err=%v", secondStored, err)
	}
	var sharedGroupIDCount, sharedMembershipIDCount int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM directory_managed_groups WHERE id=$1`, group.ID).Scan(&sharedGroupIDCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM directory_managed_memberships WHERE id=$1`, membership.ID).Scan(&sharedMembershipIDCount); err != nil {
		t.Fatal(err)
	}
	if sharedGroupIDCount != 2 || sharedMembershipIDCount != 2 {
		t.Fatalf("stable Directory IDs remain globally constrained: groups=%d memberships=%d", sharedGroupIDCount, sharedMembershipIDCount)
	}

	concurrent := group
	concurrent.ID = fmt.Sprintf("%032x", time.Now().UnixNano()+5)
	concurrent.OrganizationID = organizationID
	concurrent.SourceRecordID = "concurrent-group"
	const workers = 12
	var wait sync.WaitGroup
	wait.Add(workers)
	createdResults := make(chan bool, workers)
	errorResults := make(chan error, workers)
	for range workers {
		go func() {
			defer wait.Done()
			observed, created, err := store.ImportManagedGroup(ctx, concurrent)
			if err == nil && (observed.ID != concurrent.ID || observed.Revision != concurrent.Revision) {
				err = errors.New("atomic PostgreSQL import returned different state")
			}
			createdResults <- created
			errorResults <- err
		}()
	}
	wait.Wait()
	close(createdResults)
	close(errorResults)
	createdCount := 0
	for created := range createdResults {
		if created {
			createdCount++
		}
	}
	for err := range errorResults {
		if err != nil {
			t.Fatal(err)
		}
	}
	if createdCount != 1 {
		t.Fatalf("expected one atomic PostgreSQL group create, got %d", createdCount)
	}
}
