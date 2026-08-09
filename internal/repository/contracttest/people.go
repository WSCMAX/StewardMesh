package contracttest

// Requirement: REQ-PEOPLE-001. Feature: identity.directory.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/people"
)

func PeopleStore(t *testing.T, store people.Store, organizationID string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	suffix := randomID(t)[:8]

	site := people.Site{
		ID:             randomID(t),
		OrganizationID: organizationID,
		Name:           "North Campus " + suffix,
		NormalizedName: "north campus " + suffix,
		Status:         people.StatusActive,
		Revision:       1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	createdSite, err := store.CreateSite(ctx, site)
	if err != nil {
		t.Fatal(err)
	}
	if createdSite.ID != site.ID {
		t.Fatalf("unexpected site %#v", createdSite)
	}

	department := people.Department{
		ID:             randomID(t),
		OrganizationID: organizationID,
		Name:           "Infrastructure " + suffix,
		NormalizedName: "infrastructure " + suffix,
		SiteID:         site.ID,
		Status:         people.StatusActive,
		Revision:       1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	createdDepartment, err := store.CreateDepartment(ctx, department)
	if err != nil {
		t.Fatal(err)
	}
	if createdDepartment.SiteID != site.ID {
		t.Fatalf("unexpected department %#v", createdDepartment)
	}

	person := contractIdentity(t, organizationID, "person", "Alex Rivera "+suffix, "alex."+suffix+"@example.test", department.ID, site.ID, now)
	shared := contractIdentity(t, organizationID, "shared", "Operations Desk "+suffix, "", department.ID, site.ID, now)
	public := contractIdentity(t, organizationID, "public", "Public Lab "+suffix, "", "", "", now)
	for _, identity := range []people.Identity{person, shared, public} {
		if _, err := store.CreateIdentity(ctx, identity); err != nil {
			t.Fatal(err)
		}
	}
	duplicate := contractIdentity(t, organizationID, "person", "Duplicate email "+suffix, person.Email, department.ID, site.ID, now)
	if _, err := store.CreateIdentity(ctx, duplicate); !errors.Is(err, people.ErrConflict) {
		t.Fatalf("expected duplicate email conflict, got %v", err)
	}

	departmentOnly := people.Visibility{DepartmentIDs: []string{department.ID}}
	visibleSites, err := store.ListSites(ctx, organizationID, departmentOnly)
	if err != nil || len(visibleSites) != 1 || visibleSites[0].ID != site.ID {
		t.Fatalf("department visibility did not resolve its site: %#v, %v", visibleSites, err)
	}
	visibleDepartments, err := store.ListDepartments(ctx, organizationID, people.Visibility{SiteIDs: []string{site.ID}})
	if err != nil || len(visibleDepartments) != 1 || visibleDepartments[0].ID != department.ID {
		t.Fatalf("site visibility did not restrict departments: %#v, %v", visibleDepartments, err)
	}
	visibleIdentities, err := store.SearchIdentities(ctx, organizationID, people.IdentityQuery{Limit: 100}, departmentOnly)
	if err != nil || len(visibleIdentities) != 2 {
		t.Fatalf("department search returned unexpected identities: %#v, %v", visibleIdentities, err)
	}
	for _, identity := range visibleIdentities {
		if identity.DepartmentID != department.ID {
			t.Fatalf("scoped search leaked identity %#v", identity)
		}
	}
	searchResults, err := store.SearchIdentities(ctx, organizationID, people.IdentityQuery{Search: "operations", Kind: people.IdentityShared, Status: people.StatusActive, Limit: 10}, people.Visibility{All: true})
	if err != nil || len(searchResults) != 1 || searchResults[0].ID != shared.ID {
		t.Fatalf("unexpected filtered search %#v, %v", searchResults, err)
	}
	otherOrganization, err := store.SearchIdentities(ctx, organizationID+"-other", people.IdentityQuery{Limit: 100}, people.Visibility{All: true})
	if err != nil || len(otherOrganization) != 0 {
		t.Fatalf("organization boundary leaked records: %#v, %v", otherOrganization, err)
	}

	assetID := "asset-" + suffix
	primary := contractAssignment(t, organizationID, assetID, person.ID, people.AssigneeIdentity, people.AssignmentPrimary, now)
	if _, err := store.CreateAssetAssignment(ctx, primary, true); err != nil {
		t.Fatal(err)
	}
	replacement := contractAssignment(t, organizationID, assetID, shared.ID, people.AssigneeIdentity, people.AssignmentPrimary, now.Add(time.Hour))
	if _, err := store.CreateAssetAssignment(ctx, replacement, true); err != nil {
		t.Fatal(err)
	}
	userOne := contractAssignment(t, organizationID, assetID, person.ID, people.AssigneeIdentity, people.AssignmentUser, now.Add(2*time.Hour))
	userTwo := contractAssignment(t, organizationID, assetID, shared.ID, people.AssigneeIdentity, people.AssignmentUser, now.Add(2*time.Hour))
	if _, err := store.CreateAssetAssignment(ctx, userOne, false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateAssetAssignment(ctx, userTwo, false); err != nil {
		t.Fatal(err)
	}
	duplicateUser := contractAssignment(t, organizationID, assetID, person.ID, people.AssigneeIdentity, people.AssignmentUser, now.Add(3*time.Hour))
	if _, err := store.CreateAssetAssignment(ctx, duplicateUser, false); !errors.Is(err, people.ErrConflict) {
		t.Fatalf("expected duplicate active user conflict, got %v", err)
	}
	ended, err := store.EndAssetAssignment(ctx, organizationID, assetID, userOne.ID, now.Add(4*time.Hour))
	if err != nil || ended.EffectiveTo == nil {
		t.Fatalf("expected effective-dated end, got %#v, %v", ended, err)
	}
	history, err := store.ListAssetAssignments(ctx, organizationID, assetID)
	if err != nil || len(history) != 4 {
		t.Fatalf("unexpected assignment history %#v, %v", history, err)
	}
	var previousPrimaryFound bool
	var activeUsers int
	for _, assignment := range history {
		if assignment.ID == primary.ID && assignment.EffectiveTo != nil && assignment.EffectiveTo.Equal(replacement.EffectiveFrom) {
			previousPrimaryFound = true
		}
		if assignment.Role == people.AssignmentUser && assignment.EffectiveTo == nil {
			activeUsers++
		}
	}
	if !previousPrimaryFound || activeUsers != 1 {
		t.Fatalf("assignment history lost replacement or active-user state: %#v", history)
	}
}

func contractIdentity(t *testing.T, organizationID string, kind people.IdentityKind, name, email, departmentID, siteID string, now time.Time) people.Identity {
	t.Helper()
	return people.Identity{
		ID:              randomID(t),
		OrganizationID:  organizationID,
		Kind:            kind,
		DisplayName:     name,
		NormalizedName:  strings.ToLower(name),
		Email:           email,
		NormalizedEmail: strings.ToLower(email),
		DepartmentID:    departmentID,
		SiteID:          siteID,
		Status:          people.StatusActive,
		Revision:        1,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func contractAssignment(t *testing.T, organizationID, assetID, assigneeID string, assigneeKind people.AssigneeKind, role people.AssignmentRole, effectiveFrom time.Time) people.AssetAssignment {
	t.Helper()
	return people.AssetAssignment{
		ID:             randomID(t),
		OrganizationID: organizationID,
		AssetID:        assetID,
		AssigneeKind:   assigneeKind,
		AssigneeID:     assigneeID,
		Role:           role,
		EffectiveFrom:  effectiveFrom,
		CreatedBy:      "contract-test",
		CreatedAt:      effectiveFrom,
	}
}
