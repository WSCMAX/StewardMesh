package people_test

// Requirement: REQ-PEOPLE-001. Feature: identity.directory.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	. "github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

type testAssetReader struct {
	assets map[string]domain.Asset
}

func (r testAssetReader) Get(_ context.Context, id string) (domain.Asset, error) {
	asset, exists := r.assets[id]
	if !exists {
		return domain.Asset{}, errors.New("asset not found")
	}
	return asset, nil
}

type captureAuditor struct {
	events []foundation.AuditEvent
}

func (a *captureAuditor) Record(_ context.Context, event foundation.AuditEvent) error {
	a.events = append(a.events, event)
	return nil
}

func TestPeopleServiceBuildsTypedDirectoryAndAssignmentHistory(t *testing.T) {
	now := time.Date(2026, time.August, 9, 8, 0, 0, 0, time.UTC)
	auditor := &captureAuditor{}
	assets := testAssetReader{assets: map[string]domain.Asset{
		"asset-1": {ID: "asset-1", Name: "Lab workstation", Kind: "computer"},
	}}
	service, err := NewService(repository.NewMemoryPeopleStore(), assets, auditor, ServiceConfig{
		OrganizationID: "example-org",
		Now:            func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := foundation.WithScope(context.Background(), foundation.Scope{
		OrganizationID: "example-org",
		ActorID:        "account-1",
		CorrelationID:  "people-test-correlation",
	})
	site, err := service.CreateSite(ctx, CreateSiteInput{Name: " Main Campus "})
	if err != nil {
		t.Fatal(err)
	}
	department, err := service.CreateDepartment(ctx, CreateDepartmentInput{Name: "Technology", SiteID: site.ID})
	if err != nil {
		t.Fatal(err)
	}
	person, err := service.CreateIdentity(ctx, CreateIdentityInput{
		Kind:         IdentityPerson,
		DisplayName:  "Alex Rivera",
		Email:        "Alex.Rivera@Example.Test",
		DepartmentID: department.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if person.SiteID != site.ID || person.Email != "alex.rivera@example.test" {
		t.Fatalf("expected department site inference and normalized email, got %#v", person)
	}
	shared, err := service.CreateIdentity(ctx, CreateIdentityInput{
		Kind:         IdentityShared,
		DisplayName:  "Public workstation users",
		DepartmentID: department.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	results, err := service.SearchIdentities(ctx, IdentityQuery{Search: "public", Kind: IdentityShared}, Visibility{DepartmentIDs: []string{department.ID}})
	if err != nil || len(results) != 1 || results[0].ID != shared.ID {
		t.Fatalf("unexpected scoped results %#v, %v", results, err)
	}

	primary, err := service.CreateAssetAssignment(ctx, CreateAssetAssignmentInput{
		AssetID:      "asset-1",
		AssigneeKind: AssigneeIdentity,
		AssigneeID:   person.ID,
		Role:         AssignmentPrimary,
	})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	replacement, err := service.CreateAssetAssignment(ctx, CreateAssetAssignmentInput{
		AssetID:      "asset-1",
		AssigneeKind: AssigneeIdentity,
		AssigneeID:   shared.ID,
		Role:         AssignmentPrimary,
	})
	if err != nil {
		t.Fatal(err)
	}
	history, err := service.ListAssetAssignments(ctx, "asset-1", Visibility{All: true})
	if err != nil || len(history) != 2 {
		t.Fatalf("unexpected assignment history %#v, %v", history, err)
	}
	for _, assignment := range history {
		if assignment.ID == primary.ID && (assignment.EffectiveTo == nil || !assignment.EffectiveTo.Equal(replacement.EffectiveFrom)) {
			t.Fatalf("previous primary was not effective-dated: %#v", assignment)
		}
	}
	now = now.Add(time.Hour)
	ended, err := service.EndAssetAssignment(ctx, EndAssetAssignmentInput{AssetID: "asset-1", AssignmentID: replacement.ID})
	if err != nil || ended.EffectiveTo == nil {
		t.Fatalf("expected active assignment to end: %#v, %v", ended, err)
	}

	if len(auditor.events) != 7 {
		t.Fatalf("expected seven write audit events, got %#v", auditor.events)
	}
	for _, event := range auditor.events {
		if event.OrganizationID != "example-org" || event.ActorID != "account-1" || event.CorrelationID != "people-test-correlation" || event.Metadata["requirementId"] != RequirementID {
			t.Fatalf("incomplete people audit event %#v", event)
		}
		encodedMetadata := ""
		for key, value := range event.Metadata {
			encodedMetadata += key + value
		}
		if strings.Contains(encodedMetadata, person.Email) || strings.Contains(encodedMetadata, person.DisplayName) {
			t.Fatalf("audit metadata included directory PII: %#v", event.Metadata)
		}
	}
}

func TestPeopleServiceCreatesVisibleLocationHierarchy(t *testing.T) {
	now := time.Date(2026, time.August, 9, 9, 0, 0, 0, time.UTC)
	auditor := &captureAuditor{}
	service, err := NewService(
		repository.NewMemoryPeopleStore(),
		testAssetReader{assets: map[string]domain.Asset{}},
		auditor,
		ServiceConfig{OrganizationID: "example-org", Now: func() time.Time { return now }},
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	site, err := service.CreateSite(ctx, CreateSiteInput{
		Name: " North Campus ",
		Address: Address{
			Line1:      " 100 College Avenue ",
			Line2:      " Suite 200 ",
			City:       " Madison ",
			Region:     " WI ",
			PostalCode: " 53703 ",
			Country:    " us ",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if site.Address.Line1 != "100 College Avenue" || site.Address.Country != "US" {
		t.Fatalf("expected canonical structured address, got %#v", site.Address)
	}
	building, err := service.CreateBuilding(ctx, CreateBuildingInput{SiteID: site.ID, Name: " Science Hall "})
	if err != nil {
		t.Fatal(err)
	}
	room, err := service.CreateRoom(ctx, CreateRoomInput{
		SiteID: site.ID, BuildingID: building.ID, Number: " 101A ", Name: " Robotics Lab ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if building.Name != "Science Hall" || room.Number != "101A" || room.SiteID != site.ID || room.BuildingID != building.ID {
		t.Fatalf("unexpected location hierarchy %#v, %#v", building, room)
	}
	department, err := service.CreateDepartment(ctx, CreateDepartmentInput{Name: "Engineering", SiteID: site.ID})
	if err != nil {
		t.Fatal(err)
	}
	otherSite, err := service.CreateSite(ctx, CreateSiteInput{Name: "South Campus"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateBuilding(ctx, CreateBuildingInput{SiteID: otherSite.ID, Name: "Library"}); err != nil {
		t.Fatal(err)
	}

	buildings, err := service.ListBuildings(ctx, "", Visibility{DepartmentIDs: []string{department.ID}})
	if err != nil || len(buildings) != 1 || buildings[0].ID != building.ID {
		t.Fatalf("department visibility did not restrict buildings: %#v, %v", buildings, err)
	}
	rooms, err := service.ListRooms(ctx, "", "", Visibility{SiteIDs: []string{site.ID}})
	if err != nil || len(rooms) != 1 || rooms[0].ID != room.ID {
		t.Fatalf("site visibility did not restrict rooms: %#v, %v", rooms, err)
	}
	mismatched, err := service.ListRooms(ctx, otherSite.ID, building.ID, Visibility{All: true})
	if err != nil || len(mismatched) != 0 {
		t.Fatalf("mismatched location filters should be empty: %#v, %v", mismatched, err)
	}
	if _, err := service.ListBuildings(ctx, "", Visibility{}); !errors.Is(err, ErrScopeRequired) {
		t.Fatalf("expected building reads to require visibility, got %v", err)
	}

	var addressedSiteAuditFound bool
	var legacySiteAuditFound bool
	for _, event := range auditor.events {
		if event.ResourceType == "site" && event.ResourceID == site.ID {
			addressedSiteAuditFound = event.Metadata["requirementId"] == DirectoryExpansionRequirementID
		}
		if event.ResourceType == "site" && event.ResourceID == otherSite.ID {
			legacySiteAuditFound = event.Metadata["requirementId"] == RequirementID
		}
		if (event.ResourceType == "building" || event.ResourceType == "room") &&
			event.Metadata["requirementId"] != DirectoryExpansionRequirementID {
			t.Fatalf("location audit used the wrong requirement: %#v", event)
		}
	}
	if !addressedSiteAuditFound || !legacySiteAuditFound {
		t.Fatalf("site audits did not distinguish addressed and legacy sites: %#v", auditor.events)
	}
}

func TestPeopleServiceRejectsInvalidLocations(t *testing.T) {
	now := time.Date(2026, time.August, 9, 9, 0, 0, 0, time.UTC)
	service, err := NewService(
		repository.NewMemoryPeopleStore(),
		testAssetReader{assets: map[string]domain.Asset{}},
		foundation.NopAuditor{},
		ServiceConfig{OrganizationID: "example-org", Now: func() time.Time { return now }},
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := service.CreateSite(ctx, CreateSiteInput{
		Name: "Incomplete address", Address: Address{Line1: "100 College Avenue"},
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected incomplete address to fail, got %v", err)
	}
	if _, err := service.CreateSite(ctx, CreateSiteInput{
		Name: "Invalid country", Address: Address{Line1: "100 College Avenue", City: "Madison", Country: "USA"},
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid country to fail, got %v", err)
	}
	if _, err := service.CreateBuilding(ctx, CreateBuildingInput{SiteID: strings.Repeat("a", 32), Name: "Missing"}); !errors.Is(err, ErrReferenceMissing) {
		t.Fatalf("expected missing building site to fail, got %v", err)
	}
	first, err := service.CreateSite(ctx, CreateSiteInput{Name: "First"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateSite(ctx, CreateSiteInput{Name: "Second"})
	if err != nil {
		t.Fatal(err)
	}
	building, err := service.CreateBuilding(ctx, CreateBuildingInput{SiteID: first.ID, Name: "Main"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateRoom(ctx, CreateRoomInput{SiteID: second.ID, BuildingID: building.ID, Number: "101"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected mismatched room site to fail, got %v", err)
	}
	if _, err := service.CreateRoom(ctx, CreateRoomInput{SiteID: first.ID, BuildingID: building.ID}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected room number validation, got %v", err)
	}
	if _, err := service.ListRooms(ctx, "bad", "", Visibility{All: true}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid location filter to fail, got %v", err)
	}
}

func TestPeopleServiceRejectsInvalidReferencesAndUnscopedReads(t *testing.T) {
	now := time.Date(2026, time.August, 9, 8, 0, 0, 0, time.UTC)
	service, err := NewService(
		repository.NewMemoryPeopleStore(),
		testAssetReader{assets: map[string]domain.Asset{}},
		foundation.NopAuditor{},
		ServiceConfig{OrganizationID: "example-org", Now: func() time.Time { return now }},
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := service.CreateIdentity(ctx, CreateIdentityInput{Kind: IdentityPerson, DisplayName: "Missing Email"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected person email validation, got %v", err)
	}
	if _, err := service.CreateDepartment(ctx, CreateDepartmentInput{Name: "Unknown Site", SiteID: strings.Repeat("b", 32)}); !errors.Is(err, ErrReferenceMissing) {
		t.Fatalf("expected missing site reference, got %v", err)
	}
	if _, err := service.SearchIdentities(ctx, IdentityQuery{}, Visibility{}); !errors.Is(err, ErrScopeRequired) {
		t.Fatalf("expected explicit visibility, got %v", err)
	}
	if _, err := service.SearchIdentities(ctx, IdentityQuery{Limit: 101}, Visibility{All: true}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected bounded search limit, got %v", err)
	}
	if _, err := service.CreateAssetAssignment(ctx, CreateAssetAssignmentInput{
		AssetID:      "missing",
		AssigneeKind: AssigneeIdentity,
		AssigneeID:   strings.Repeat("a", 32),
		Role:         AssignmentUser,
	}); !errors.Is(err, ErrReferenceMissing) {
		t.Fatalf("expected missing asset reference, got %v", err)
	}
}

func TestPeopleServiceEnforcesDepartmentSiteConsistency(t *testing.T) {
	now := time.Date(2026, time.August, 9, 8, 0, 0, 0, time.UTC)
	service, err := NewService(
		repository.NewMemoryPeopleStore(),
		testAssetReader{assets: map[string]domain.Asset{"asset-1": {ID: "asset-1"}}},
		foundation.NopAuditor{},
		ServiceConfig{OrganizationID: "example-org", Now: func() time.Time { return now }},
	)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := service.CreateSite(context.Background(), CreateSiteInput{Name: "First"})
	second, _ := service.CreateSite(context.Background(), CreateSiteInput{Name: "Second"})
	department, _ := service.CreateDepartment(context.Background(), CreateDepartmentInput{Name: "Finance", SiteID: first.ID})
	_, err = service.CreateIdentity(context.Background(), CreateIdentityInput{
		Kind:         IdentityPerson,
		DisplayName:  "Taylor Morgan",
		Email:        "taylor@example.test",
		DepartmentID: department.ID,
		SiteID:       second.ID,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected mismatched department and site to fail, got %v", err)
	}
}

func TestPeopleServiceUpdatesDirectoryRecords(t *testing.T) {
	now := time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)
	service, err := NewService(
		repository.NewMemoryPeopleStore(),
		testAssetReader{assets: map[string]domain.Asset{}},
		foundation.NopAuditor{},
		ServiceConfig{OrganizationID: "example-org", Now: func() time.Time { return now }},
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := foundation.WithScope(context.Background(), foundation.Scope{
		OrganizationID: "example-org", ActorID: "account-1", CorrelationID: "people-update-test",
	})
	site, err := service.CreateSite(ctx, CreateSiteInput{Name: "Main Campus"})
	if err != nil {
		t.Fatal(err)
	}
	building, err := service.CreateBuilding(ctx, CreateBuildingInput{SiteID: site.ID, Name: "Hall"})
	if err != nil {
		t.Fatal(err)
	}
	room, err := service.CreateRoom(ctx, CreateRoomInput{SiteID: site.ID, BuildingID: building.ID, Number: "10", Name: "Studio"})
	if err != nil {
		t.Fatal(err)
	}
	department, err := service.CreateDepartment(ctx, CreateDepartmentInput{Name: "Arts", SiteID: site.ID})
	if err != nil {
		t.Fatal(err)
	}
	person, err := service.CreateIdentity(ctx, CreateIdentityInput{
		Kind: IdentityPerson, DisplayName: "Jordan Lee", Email: "jordan@example.test", DepartmentID: department.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	updatedSite, err := service.UpdateSite(ctx, UpdateSiteInput{ID: site.ID, Name: "East Campus", Status: StatusActive, Revision: site.Revision})
	if err != nil || updatedSite.Name != "East Campus" || updatedSite.Revision != site.Revision+1 {
		t.Fatalf("unexpected site update %#v, %v", updatedSite, err)
	}
	updatedRoom, err := service.UpdateRoom(ctx, UpdateRoomInput{
		ID: room.ID, SiteID: site.ID, BuildingID: building.ID, Number: "11", Name: "Gallery", Status: StatusActive, Revision: room.Revision,
	})
	if err != nil || updatedRoom.Number != "11" || updatedRoom.Name != "Gallery" {
		t.Fatalf("unexpected room update %#v, %v", updatedRoom, err)
	}
	updatedPerson, err := service.UpdateIdentity(ctx, UpdateIdentityInput{
		ID: person.ID, Kind: IdentityPerson, DisplayName: "Jordan Lee-Updated", Email: "jordan@example.test",
		DepartmentID: department.ID, SiteID: site.ID, Status: StatusActive, Revision: person.Revision,
	})
	if err != nil || updatedPerson.DisplayName != "Jordan Lee-Updated" {
		t.Fatalf("unexpected identity update %#v, %v", updatedPerson, err)
	}
	if _, err := service.UpdateIdentity(ctx, UpdateIdentityInput{
		ID: person.ID, Kind: IdentityPerson, DisplayName: "Stale", Email: "jordan@example.test", Revision: person.Revision,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected stale identity update conflict, got %v", err)
	}
}

func TestPeopleServiceRecordsTypedLocationReferences(t *testing.T) {
	now := time.Date(2026, time.August, 18, 15, 0, 0, 0, time.UTC)
	service, err := NewService(
		repository.NewMemoryPeopleStore(),
		testAssetReader{assets: map[string]domain.Asset{}},
		foundation.NopAuditor{},
		ServiceConfig{OrganizationID: "example-org", Now: func() time.Time { return now }},
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := foundation.WithScope(context.Background(), foundation.Scope{
		OrganizationID: "example-org", ActorID: "account-1", CorrelationID: "people-occupancy-test",
	})
	site, err := service.CreateSite(ctx, CreateSiteInput{Name: "Main Campus"})
	if err != nil {
		t.Fatal(err)
	}
	building, err := service.CreateBuilding(ctx, CreateBuildingInput{SiteID: site.ID, Name: "Hall"})
	if err != nil {
		t.Fatal(err)
	}
	office, err := service.CreateRoom(ctx, CreateRoomInput{SiteID: site.ID, BuildingID: building.ID, Number: "10", Name: "Office"})
	if err != nil {
		t.Fatal(err)
	}
	classroom, err := service.CreateRoom(ctx, CreateRoomInput{SiteID: site.ID, BuildingID: building.ID, Number: "20", Name: "Lecture"})
	if err != nil {
		t.Fatal(err)
	}
	person, err := service.CreateIdentity(ctx, CreateIdentityInput{
		Kind: IdentityPerson, DisplayName: "Jordan Lee", Email: "jordan@example.test",
		BuildingID: building.ID, RoomID: office.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if person.SiteID != site.ID || person.BuildingID != building.ID || person.RoomID != office.ID {
		t.Fatalf("expected primary location inheritance, got %#v", person)
	}
	officeType, err := service.CreateLocationReferenceType(ctx, CreateLocationReferenceTypeInput{
		Name: "Office", RelationshipKind: RelationshipUsesOffice, LocationKind: LocationKindRoom,
	})
	if err != nil {
		t.Fatal(err)
	}
	instructorType, err := service.CreateLocationReferenceType(ctx, CreateLocationReferenceTypeInput{
		Name: "Instructor", RelationshipKind: RelationshipTeachesIn, LocationKind: LocationKindRoom,
	})
	if err != nil {
		t.Fatal(err)
	}
	primary, err := service.CreateLocationReference(ctx, CreateLocationReferenceInput{
		IdentityID: person.ID, TypeID: officeType.ID, LocationKind: LocationKindRoom, LocationID: office.ID, Priority: LocationPriorityPrimary,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateLocationReference(ctx, CreateLocationReferenceInput{
		IdentityID: person.ID, TypeID: officeType.ID, LocationKind: LocationKindRoom, LocationID: classroom.ID, Priority: LocationPriorityPrimary,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected one active primary office reference, got %v", err)
	}
	secondary, err := service.CreateLocationReference(ctx, CreateLocationReferenceInput{
		IdentityID: person.ID, TypeID: instructorType.ID, LocationKind: LocationKindRoom, LocationID: classroom.ID, Priority: LocationPrioritySecondary,
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err := service.ListLocationReferences(ctx, LocationReferenceQuery{LocationIDs: []string{classroom.ID}, Limit: 20}, Visibility{All: true})
	if err != nil || len(items) != 1 || items[0].ID != secondary.ID {
		t.Fatalf("expected classroom usage, got %#v, %v", items, err)
	}
	if primary.Priority != LocationPriorityPrimary || secondary.TypeID != instructorType.ID {
		t.Fatalf("unexpected occupancy records %#v %#v", primary, secondary)
	}
	if _, err := service.CreateLocationReference(ctx, CreateLocationReferenceInput{
		IdentityID: person.ID, TypeID: officeType.ID, LocationKind: LocationKindSite, LocationID: site.ID,
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected location kind mismatch, got %v", err)
	}
}
