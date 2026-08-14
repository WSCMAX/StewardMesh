package repository_test

// Requirements: REQ-DIRECTORY-EXPANSION-002, REQ-DIRECTORY-EXPANSION-003, REQ-DIRECTORY-EXPANSION-005, REQ-DIRECTORY-EXPANSION-006.
// Features: integrations.protocols, identity.directory.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/directoryexpansion"
	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/repository/contracttest"
)

func TestMemoryDirectoryImportStoreContract(t *testing.T) {
	store := repository.NewMemoryDirectoryImportStore()
	contracttest.DirectoryImportStore(t, store, "memory-directory-import", "memory")
	contracttest.DirectoryGroupMappingStore(t, store, "memory-directory-import", "memory-group-mapping")
	contracttest.DirectoryGroupTargetStore(t, store, "memory-directory-groups", "memory-groups")
}

func TestMemoryDirectoryExchangeIDsAreOrganizationScoped(t *testing.T) {
	store := repository.NewMemoryDirectoryImportStore()
	now := time.Date(2026, time.August, 13, 23, 30, 0, 0, time.UTC)
	for _, organizationID := range []string{"organization-one", "organization-two"} {
		group := directoryexpansion.ManagedGroup{ID: "11111111111111111111111111111111", OrganizationID: organizationID,
			SourceSystemID: "directory-source", SourceRecordID: "shared-source-record", Name: "shared", DisplayName: "Shared group",
			Status: "active", Revision: 7, CreatedAt: now, UpdatedAt: now}
		if stored, created, err := store.ImportManagedGroup(context.Background(), group); err != nil || !created || stored.OrganizationID != organizationID {
			t.Fatalf("import stable group ID into %s: %#v created=%t err=%v", organizationID, stored, created, err)
		}
		membership := directoryexpansion.ManagedMembership{ID: "22222222222222222222222222222222", OrganizationID: organizationID,
			SourceSystemID: "directory-source", SourceRecordID: "shared-membership", GroupID: group.ID, GroupSourceID: group.SourceRecordID,
			MemberID: "33333333333333333333333333333333", MemberSourceID: "subject", MemberKind: directoryexpansion.MemberSubject,
			MemberDisplayName: "Subject", Status: "active", Revision: 11, CreatedAt: now, UpdatedAt: now}
		if stored, created, err := store.ImportManagedMembership(context.Background(), membership); err != nil || !created || stored.OrganizationID != organizationID {
			t.Fatalf("import stable membership ID into %s: %#v created=%t err=%v", organizationID, stored, created, err)
		}
	}
	for _, organizationID := range []string{"organization-one", "organization-two"} {
		group, err := store.GetManagedGroup(context.Background(), organizationID, "11111111111111111111111111111111")
		if err != nil || group.OrganizationID != organizationID {
			t.Fatalf("organization-scoped group lookup for %s: %#v err=%v", organizationID, group, err)
		}
		membership, err := store.GetManagedMembership(context.Background(), organizationID, "22222222222222222222222222222222")
		if err != nil || membership.OrganizationID != organizationID {
			t.Fatalf("organization-scoped membership lookup for %s: %#v err=%v", organizationID, membership, err)
		}
	}
}

func TestMemoryDirectoryExchangeMembershipImportRequiresExactGroupSources(t *testing.T) {
	store := repository.NewMemoryDirectoryImportStore()
	ctx := context.Background()
	now := time.Date(2026, time.August, 13, 23, 45, 0, 0, time.UTC)
	parent := directoryexpansion.ManagedGroup{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", OrganizationID: "example-org",
		SourceSystemID: "directory-source", SourceRecordID: "parent", Name: "parent", DisplayName: "Parent",
		Status: "active", Revision: 4, CreatedAt: now, UpdatedAt: now}
	member := directoryexpansion.ManagedGroup{ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", OrganizationID: parent.OrganizationID,
		SourceSystemID: parent.SourceSystemID, SourceRecordID: "nested", Name: "nested", DisplayName: "Nested",
		Status: "active", Revision: 6, CreatedAt: now, UpdatedAt: now}
	for _, group := range []directoryexpansion.ManagedGroup{parent, member} {
		if _, created, err := store.ImportManagedGroup(ctx, group); err != nil || !created {
			t.Fatalf("import dependency group %#v created=%t err=%v", group, created, err)
		}
	}
	membership := directoryexpansion.ManagedMembership{ID: "cccccccccccccccccccccccccccccccc", OrganizationID: parent.OrganizationID,
		SourceSystemID: parent.SourceSystemID, SourceRecordID: "nested-membership", GroupID: parent.ID, GroupSourceID: "wrong-parent",
		MemberID: member.ID, MemberSourceID: member.SourceRecordID, MemberKind: directoryexpansion.MemberGroup,
		MemberDisplayName: member.DisplayName, Status: "active", Revision: 9, CreatedAt: now, UpdatedAt: now}
	if _, _, err := store.ImportManagedMembership(ctx, membership); !errors.Is(err, directoryexpansion.ErrConflict) {
		t.Fatalf("expected mismatched parent source rejection, got %v", err)
	}
	if _, err := store.GetManagedMembership(ctx, parent.OrganizationID, membership.ID); !errors.Is(err, directoryexpansion.ErrNotFound) {
		t.Fatalf("mismatched parent import was not atomic: %v", err)
	}
	membership.GroupSourceID = parent.SourceRecordID
	membership.MemberSourceID = "wrong-member"
	if _, _, err := store.ImportManagedMembership(ctx, membership); !errors.Is(err, directoryexpansion.ErrConflict) {
		t.Fatalf("expected mismatched nested-member source rejection, got %v", err)
	}
	if _, err := store.GetManagedMembership(ctx, parent.OrganizationID, membership.ID); !errors.Is(err, directoryexpansion.ErrNotFound) {
		t.Fatalf("mismatched nested-member import was not atomic: %v", err)
	}
	membership.MemberSourceID = member.SourceRecordID
	if _, created, err := store.ImportManagedMembership(ctx, membership); err != nil || !created {
		t.Fatalf("import source-consistent nested membership created=%t err=%v", created, err)
	}
}
