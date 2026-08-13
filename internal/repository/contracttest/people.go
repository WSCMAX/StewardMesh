package contracttest

// Requirements: REQ-PEOPLE-001, REQ-DIRECTORY-EXPANSION-002. Features: identity.directory, integrations.protocols.

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
		Address: people.Address{
			Line1:      "100 College Avenue",
			Line2:      "Suite 200",
			City:       "Madison",
			Region:     "WI",
			PostalCode: "53703",
			Country:    "US",
		},
		Status:    people.StatusActive,
		Revision:  1,
		CreatedAt: now,
		UpdatedAt: now,
	}
	createdSite, err := store.CreateSite(ctx, site)
	if err != nil {
		t.Fatal(err)
	}
	if createdSite.ID != site.ID || createdSite.Address != site.Address {
		t.Fatalf("unexpected site %#v", createdSite)
	}

	building := people.Building{
		ID:             randomID(t),
		OrganizationID: organizationID,
		SiteID:         site.ID,
		Name:           "Science Hall " + suffix,
		NormalizedName: "science hall " + suffix,
		Status:         people.StatusActive,
		Revision:       1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	createdBuilding, err := store.CreateBuilding(ctx, building)
	if err != nil {
		t.Fatal(err)
	}
	if createdBuilding.SiteID != site.ID {
		t.Fatalf("unexpected building %#v", createdBuilding)
	}
	duplicateBuilding := building
	duplicateBuilding.ID = randomID(t)
	if _, err := store.CreateBuilding(ctx, duplicateBuilding); !errors.Is(err, people.ErrConflict) {
		t.Fatalf("expected duplicate building conflict, got %v", err)
	}
	missingSiteBuilding := building
	missingSiteBuilding.ID = randomID(t)
	missingSiteBuilding.SiteID = randomID(t)
	missingSiteBuilding.Name = "Missing site " + suffix
	missingSiteBuilding.NormalizedName = "missing site " + suffix
	if _, err := store.CreateBuilding(ctx, missingSiteBuilding); !errors.Is(err, people.ErrReferenceMissing) {
		t.Fatalf("expected missing building site reference, got %v", err)
	}
	if _, err := store.GetBuilding(ctx, organizationID+"-other", building.ID); !errors.Is(err, people.ErrNotFound) {
		t.Fatalf("expected organization-scoped building lookup, got %v", err)
	}

	room := people.Room{
		ID:               randomID(t),
		OrganizationID:   organizationID,
		SiteID:           site.ID,
		BuildingID:       building.ID,
		Number:           "101A",
		NormalizedNumber: "101a",
		Name:             "Robotics Lab",
		Status:           people.StatusActive,
		Revision:         1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	createdRoom, err := store.CreateRoom(ctx, room)
	if err != nil {
		t.Fatal(err)
	}
	if createdRoom.BuildingID != building.ID || createdRoom.SiteID != site.ID {
		t.Fatalf("unexpected room %#v", createdRoom)
	}
	duplicateRoom := room
	duplicateRoom.ID = randomID(t)
	if _, err := store.CreateRoom(ctx, duplicateRoom); !errors.Is(err, people.ErrConflict) {
		t.Fatalf("expected duplicate room conflict, got %v", err)
	}
	mismatchedRoom := room
	mismatchedRoom.ID = randomID(t)
	mismatchedRoom.SiteID = randomID(t)
	mismatchedRoom.Number = "999"
	mismatchedRoom.NormalizedNumber = "999"
	if _, err := store.CreateRoom(ctx, mismatchedRoom); !errors.Is(err, people.ErrReferenceMissing) {
		t.Fatalf("expected room/building site mismatch to fail, got %v", err)
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
	managed := contractIdentity(t, organizationID, "person", "Managed Person "+suffix, "managed."+suffix+"@example.test", "", "", now)
	managed.Provider = "directory.example"
	managed.ProviderSubject = "source-" + suffix
	if _, err := store.CreateIdentity(ctx, managed); err != nil {
		t.Fatal(err)
	}
	byProvider, err := store.GetIdentityByProvider(ctx, organizationID, managed.Provider, managed.ProviderSubject)
	if err != nil || byProvider.ID != managed.ID {
		t.Fatalf("provider identity lookup failed: %#v %v", byProvider, err)
	}
	byEmail, err := store.GetIdentityByEmail(ctx, organizationID, managed.NormalizedEmail)
	if err != nil || byEmail.ID != managed.ID {
		t.Fatalf("email identity lookup failed: %#v %v", byEmail, err)
	}
	updatedManaged := managed
	updatedManaged.DisplayName = "Reconciled Person " + suffix
	updatedManaged.NormalizedName = strings.ToLower(updatedManaged.DisplayName)
	updatedManaged.Status = people.StatusInactive
	updatedManaged.Revision = 2
	updatedManaged.UpdatedAt = now.Add(time.Minute)
	updatedManaged, err = store.ReconcileIdentity(ctx, updatedManaged, 1)
	if err != nil || updatedManaged.Revision != 2 || updatedManaged.Status != people.StatusInactive {
		t.Fatalf("reconcile identity failed: %#v %v", updatedManaged, err)
	}
	invalidRevision := updatedManaged
	invalidRevision.Revision = 4
	if _, err := store.ReconcileIdentity(ctx, invalidRevision, 2); !errors.Is(err, people.ErrConflict) {
		t.Fatalf("expected non-sequential reconciliation conflict, got %v", err)
	}
	if _, err := store.ReconcileIdentity(ctx, updatedManaged, 1); !errors.Is(err, people.ErrConflict) {
		t.Fatalf("expected stale reconciliation conflict, got %v", err)
	}
	if err := store.DeleteIdentity(ctx, organizationID, managed.ID, 1); !errors.Is(err, people.ErrConflict) {
		t.Fatalf("expected stale identity deletion conflict, got %v", err)
	}
	if err := store.DeleteIdentity(ctx, organizationID, managed.ID, updatedManaged.Revision); err != nil {
		t.Fatalf("delete compensated identity: %v", err)
	}
	if _, err := store.GetIdentity(ctx, organizationID, managed.ID); !errors.Is(err, people.ErrNotFound) {
		t.Fatalf("deleted identity remained visible: %v", err)
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
	visibleBuildings, err := store.ListBuildings(ctx, organizationID, site.ID, departmentOnly)
	if err != nil || len(visibleBuildings) != 1 || visibleBuildings[0].ID != building.ID {
		t.Fatalf("department visibility did not resolve buildings: %#v, %v", visibleBuildings, err)
	}
	visibleRooms, err := store.ListRooms(ctx, organizationID, site.ID, building.ID, departmentOnly)
	if err != nil || len(visibleRooms) != 1 || visibleRooms[0].ID != room.ID {
		t.Fatalf("department visibility did not resolve rooms: %#v, %v", visibleRooms, err)
	}
	allBuildings, err := store.ListBuildings(ctx, organizationID, "", people.Visibility{All: true})
	if err != nil || len(allBuildings) != 1 || allBuildings[0].ID != building.ID {
		t.Fatalf("unfiltered building list was incomplete: %#v, %v", allBuildings, err)
	}
	allRooms, err := store.ListRooms(ctx, organizationID, "", "", people.Visibility{All: true})
	if err != nil || len(allRooms) != 1 || allRooms[0].ID != room.ID {
		t.Fatalf("unfiltered room list was incomplete: %#v, %v", allRooms, err)
	}
	if mismatched, err := store.ListRooms(ctx, organizationID, randomID(t), building.ID, people.Visibility{All: true}); err != nil || len(mismatched) != 0 {
		t.Fatalf("mismatched room filters returned records: %#v, %v", mismatched, err)
	}
	if hidden, err := store.ListBuildings(ctx, organizationID, site.ID, people.Visibility{SiteIDs: []string{randomID(t)}}); err != nil || len(hidden) != 0 {
		t.Fatalf("unrelated site visibility exposed buildings: %#v, %v", hidden, err)
	}
	if _, err := store.ListRooms(ctx, organizationID, site.ID, building.ID, people.Visibility{}); !errors.Is(err, people.ErrScopeRequired) {
		t.Fatalf("expected explicit room visibility scope, got %v", err)
	}
	if other, err := store.ListBuildings(ctx, organizationID+"-other", site.ID, people.Visibility{All: true}); err != nil || len(other) != 0 {
		t.Fatalf("organization boundary leaked buildings: %#v, %v", other, err)
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
