package exchange_test

// Requirements: REQ-EXCHANGE-001, REQ-PEOPLE-001, REQ-PATTERNS-001. Features: migration.packages, identity.directory, templates.schemas. GitHub: #9.

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/exchange"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

type peopleProviderAssets struct{ items map[string]domain.Asset }

func (r peopleProviderAssets) Get(_ context.Context, id string) (domain.Asset, error) {
	item, exists := r.items[id]
	if !exists {
		return domain.Asset{}, errors.New("asset not found")
	}
	return item, nil
}

func newPeopleProviderService(t *testing.T, organizationID string, assets peopleProviderAssets, now time.Time) (*people.Service, people.ExchangeImporter, *repository.MemoryPeopleStore) {
	t.Helper()
	store := repository.NewMemoryPeopleStore()
	service, importer, err := people.NewServiceWithExchangeImporter(store, assets, nil, foundation.NopAuditor{}, people.ServiceConfig{OrganizationID: organizationID, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return service, importer, store
}

func TestPeopleProviderRoundTripPreservesFieldsHistoryAndDependencies(t *testing.T) {
	ctx := foundation.WithScope(context.Background(), foundation.Scope{OrganizationID: "source-org", ActorID: "source-operator", CorrelationID: "source-correlation"})
	now := time.Date(2026, time.August, 13, 19, 20, 30, 123456789, time.UTC)
	assets := peopleProviderAssets{items: map[string]domain.Asset{"asset-one": {ID: "asset-one", OrganizationID: "source-org", Status: "active"}}}
	sourceService, sourceImporter, _ := newPeopleProviderService(t, "source-org", assets, now)
	if _, err := exchange.NewPeopleProvider(sourceService, nil); err == nil {
		t.Fatal("expected People provider to require its importer capability")
	}
	sourceProvider, err := exchange.NewPeopleProvider(sourceService, sourceImporter)
	if err != nil {
		t.Fatal(err)
	}
	if got := sourceProvider.Types(); !slices.Equal(got, []string{"people.site", "people.building", "people.room", "people.department", "people.identity", "people.assignment"}) {
		t.Fatalf("unexpected People provider types %#v", got)
	}
	site, err := sourceService.CreateSite(ctx, people.CreateSiteInput{Name: "Main Campus", Address: people.Address{Line1: "100 College Avenue", Line2: "Suite 2", City: "Madison", Region: "WI", PostalCode: "53703", Country: "US"}, Status: people.StatusInactive})
	if err != nil {
		t.Fatal(err)
	}
	building, err := sourceService.CreateBuilding(ctx, people.CreateBuildingInput{SiteID: site.ID, Name: "Science Hall", Status: people.StatusInactive})
	if err != nil {
		t.Fatal(err)
	}
	_, err = sourceService.CreateRoom(ctx, people.CreateRoomInput{SiteID: site.ID, BuildingID: building.ID, Number: "101A", Name: "Robotics", Status: people.StatusInactive})
	if err != nil {
		t.Fatal(err)
	}
	department, err := sourceService.CreateDepartment(ctx, people.CreateDepartmentInput{Name: "Engineering", SiteID: site.ID, Status: people.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	identity, err := sourceService.CreateIdentity(ctx, people.CreateIdentityInput{Kind: people.IdentityShared, DisplayName: "Lab operators", Email: "lab@example.test", DepartmentID: department.ID, SiteID: site.ID, Status: people.StatusActive, Provider: "directory.example", ProviderSubject: "subject-one"})
	if err != nil {
		t.Fatal(err)
	}
	assignment, err := sourceService.CreateAssetAssignment(ctx, people.CreateAssetAssignmentInput{AssetID: "asset-one", AssigneeKind: people.AssigneeIdentity, AssigneeID: identity.ID, Role: people.AssignmentUser, EffectiveFrom: now.Add(-24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	ended, err := sourceService.EndAssetAssignment(ctx, people.EndAssetAssignmentInput{AssetID: "asset-one", AssignmentID: assignment.ID, EffectiveTo: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	records, err := sourceProvider.ListRecords(ctx)
	if err != nil || len(records) != 6 {
		t.Fatalf("list People Exchange records %#v err=%v", records, err)
	}
	byType := make(map[string]exchange.Record, len(records))
	for _, record := range records {
		byType[record.Type] = record
		for _, forbidden := range [][]byte{[]byte("organizationId"), []byte("source-operator"), []byte(`"createdBy"`)} {
			if bytes.Contains(record.Payload, forbidden) {
				t.Fatalf("People payload %s leaked deployment/operator state: %s", record.Type, record.Payload)
			}
		}
	}
	if got := byType["people.room"].Dependencies; !slices.Equal(got, []exchange.Reference{{Type: "people.building", ID: building.ID}, {Type: "people.site", ID: site.ID}}) {
		t.Fatalf("unexpected room dependencies %#v", got)
	}
	if got := byType["people.identity"].Dependencies; !slices.Equal(got, []exchange.Reference{{Type: "people.department", ID: department.ID}, {Type: "people.site", ID: site.ID}}) {
		t.Fatalf("unexpected identity dependencies %#v", got)
	}
	if got := byType["people.assignment"].Dependencies; !slices.Equal(got, []exchange.Reference{{Type: "atlas.asset", ID: "asset-one"}, {Type: "people.identity", ID: identity.ID}}) {
		t.Fatalf("unexpected assignment dependencies %#v", got)
	}

	targetAssets := peopleProviderAssets{items: map[string]domain.Asset{"asset-one": {ID: "asset-one", OrganizationID: "target-org", Status: "active"}}}
	targetService, targetImporter, _ := newPeopleProviderService(t, "target-org", targetAssets, now.Add(48*time.Hour))
	targetProvider, err := exchange.NewPeopleProvider(targetService, targetImporter)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exchange.NewPeopleProvider(sourceService, targetImporter); err == nil {
		t.Fatal("expected People provider to reject another service's importer")
	}
	for index, recordType := range []string{"people.site", "people.building", "people.room", "people.department", "people.identity", "people.assignment"} {
		record := byType[recordType]
		result, err := targetProvider.ImportRecord(ctx, exchange.ProviderImportOperation{Token: "people-import-" + string(rune('a'+index)), OccurredAt: now, ExpectedCreated: true}, "source", record, nil)
		if err != nil || !result.Committed || !result.Created {
			t.Fatalf("import %s result=%#v err=%v", recordType, result, err)
		}
		exact, err := targetProvider.ImportRecordExists(ctx, record, nil)
		if err != nil || !exact {
			t.Fatalf("exact compare %s exact=%t err=%v", recordType, exact, err)
		}
	}
	importedSite, _ := targetService.GetSite(ctx, site.ID)
	if importedSite.Address != site.Address || importedSite.Revision != site.Revision || !importedSite.CreatedAt.Equal(site.CreatedAt) || !importedSite.UpdatedAt.Equal(site.UpdatedAt) {
		t.Fatalf("site did not round trip losslessly: %#v", importedSite)
	}
	importedIdentity, _ := targetService.GetIdentity(ctx, identity.ID)
	if importedIdentity.Provider != identity.Provider || importedIdentity.ProviderSubject != identity.ProviderSubject || importedIdentity.Status != identity.Status {
		t.Fatalf("identity did not round trip losslessly: %#v", importedIdentity)
	}
	importedAssignment, _ := targetService.GetAssetAssignment(ctx, assignment.ID)
	if importedAssignment.CreatedBy != "system:exchange" || importedAssignment.EffectiveTo == nil || ended.EffectiveTo == nil || !importedAssignment.EffectiveTo.Equal(*ended.EffectiveTo) || !importedAssignment.CreatedAt.Equal(assignment.CreatedAt) {
		t.Fatalf("assignment history did not round trip losslessly: %#v", importedAssignment)
	}
	replay, err := targetProvider.ImportRecord(ctx, exchange.ProviderImportOperation{Token: "people-import-replay", OccurredAt: now, ExpectedCreated: false}, "source", byType["people.assignment"], nil)
	if err != nil || !replay.Committed || replay.Created {
		t.Fatalf("replay People assignment: %#v err=%v", replay, err)
	}
}

func TestPeopleProviderRejectsNoncanonicalAndMismatchedRecords(t *testing.T) {
	now := time.Date(2026, time.August, 13, 20, 0, 0, 0, time.UTC)
	service, importer, _ := newPeopleProviderService(t, "example-org", peopleProviderAssets{items: map[string]domain.Asset{}}, now)
	provider, err := exchange.NewPeopleProvider(service, importer)
	if err != nil {
		t.Fatal(err)
	}
	record := exchange.Record{Type: "people.site", ID: "0123456789abcdef0123456789abcdef", Revision: 1, Dependencies: []exchange.Reference{}, Payload: []byte(`{"name":" Site ","status":"active","createdAt":"2026-08-13T20:00:00Z","updatedAt":"2026-08-13T20:00:00Z"}`)}
	if _, err := provider.ImportRecord(context.Background(), exchange.ProviderImportOperation{Token: "people-invalid", OccurredAt: now, ExpectedCreated: true}, "source", record, nil); !errors.Is(err, exchange.ErrInvalidInput) {
		t.Fatalf("expected noncanonical People payload rejection, got %v", err)
	}
	record.Payload = []byte(`{"name":"Site","status":"active","createdAt":"2026-08-13T20:00:00Z","updatedAt":"2026-08-13T20:00:00Z"}`)
	noncanonicalJSON := record
	noncanonicalJSON.Payload = append([]byte(" "), noncanonicalJSON.Payload...)
	if _, err := provider.ImportRecordExists(context.Background(), noncanonicalJSON, nil); !errors.Is(err, exchange.ErrInvalidInput) {
		t.Fatalf("expected noncanonical top-level People JSON rejection, got %v", err)
	}
	subMicrosecond := record
	subMicrosecond.Payload = []byte(`{"name":"Site","status":"active","createdAt":"2026-08-13T20:00:00.000000001Z","updatedAt":"2026-08-13T20:00:00.000000001Z"}`)
	if _, err := provider.ImportRecordExists(context.Background(), subMicrosecond, nil); !errors.Is(err, exchange.ErrInvalidInput) {
		t.Fatalf("expected sub-microsecond People timestamp rejection, got %v", err)
	}
	for name, mutate := range map[string]func(exchange.Record) exchange.Record{
		"invalid id":    func(value exchange.Record) exchange.Record { value.ID = "invalid-id"; return value },
		"zero revision": func(value exchange.Record) exchange.Record { value.Revision = 0; return value },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := provider.ImportRecordExists(context.Background(), mutate(record), nil); !errors.Is(err, exchange.ErrInvalidInput) {
				t.Fatalf("expected invalid People identity/revision rejection, got %v", err)
			}
		})
	}
	revisionDrift := record
	revisionDrift.Payload = []byte(`{"name":"Site","status":"active","createdAt":"2026-08-13T20:00:00Z","updatedAt":"2026-08-13T20:01:00Z"}`)
	if _, err := provider.ImportRecord(context.Background(), exchange.ProviderImportOperation{Token: "people-revision-one-drift", OccurredAt: now, ExpectedCreated: true}, "source", revisionDrift, nil); !errors.Is(err, exchange.ErrInvalidInput) {
		t.Fatalf("expected revision-one People timestamp rejection, got %v", err)
	}
	record.Dependencies = []exchange.Reference{{Type: "people.site", ID: record.ID}}
	if _, err := provider.ImportRecord(context.Background(), exchange.ProviderImportOperation{Token: "people-dependency", OccurredAt: now, ExpectedCreated: true}, "source", record, nil); !errors.Is(err, exchange.ErrInvalidInput) {
		t.Fatalf("expected mismatched People dependencies rejection, got %v", err)
	}
}
