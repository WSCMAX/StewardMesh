package directoryexpansion_test

// Requirement: REQ-DIRECTORY-EXPANSION-007. Feature: platform.foundation.

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	. "github.com/maxlemke/stewardmesh/internal/directoryexpansion"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

type syntheticAssetReader struct{}

func (syntheticAssetReader) Get(context.Context, string) (domain.Asset, error) {
	return domain.Asset{}, errors.New("synthetic demo does not seed assets")
}

type syntheticAuditRecorder struct {
	events map[string]foundation.AuditEvent
	calls  map[string]int
}

func newSyntheticAuditRecorder() *syntheticAuditRecorder {
	return &syntheticAuditRecorder{events: make(map[string]foundation.AuditEvent), calls: make(map[string]int)}
}

func (a *syntheticAuditRecorder) Record(_ context.Context, event foundation.AuditEvent) error {
	if existing, ok := a.events[event.ID]; ok && !reflect.DeepEqual(existing, event) {
		return errors.New("audit event replay changed")
	}
	a.events[event.ID] = event
	a.calls[event.ID]++
	return nil
}

func TestSyntheticSeederUsesDurableMappingsAndExactReplayWithoutChangingProductionData(t *testing.T) {
	ctx := context.Background()
	seeder, peopleService, peopleStore, directoryStore, auditor := newSyntheticTestSeeder(t, "demo-example")
	production, err := peopleService.CreateSite(ctx, people.CreateSiteInput{Name: "Production Campus", Status: people.StatusActive})
	if err != nil {
		t.Fatal(err)
	}

	first, err := seeder.Seed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Enabled || first.CreatedLocations != 4 || first.PreviewReplay || first.ApplyReplay || first.BatchID == "" {
		t.Fatalf("unexpected first synthetic result %#v", first)
	}
	sites, err := peopleService.ListSites(ctx, people.Visibility{All: true})
	if err != nil || len(sites) != 2 {
		t.Fatalf("expected production and synthetic sites, sites=%#v err=%v", sites, err)
	}
	retained, err := peopleStore.GetSite(ctx, "demo-example", production.ID)
	if err != nil || retained.Name != production.Name || !retained.Address.Empty() {
		t.Fatalf("production site changed: %#v err=%v", retained, err)
	}
	buildings, err := peopleService.ListBuildings(ctx, "", people.Visibility{All: true})
	if err != nil || len(buildings) != 1 || buildings[0].SiteID != first.SiteID {
		t.Fatalf("unexpected synthetic buildings %#v err=%v", buildings, err)
	}
	rooms, err := peopleService.ListRooms(ctx, "", "", people.Visibility{All: true})
	if err != nil || len(rooms) != 1 || rooms[0].BuildingID != first.BuildingID {
		t.Fatalf("unexpected synthetic rooms %#v err=%v", rooms, err)
	}
	departments, err := peopleService.ListDepartments(ctx, people.Visibility{All: true})
	if err != nil || len(departments) != 1 || departments[0].SiteID != first.SiteID {
		t.Fatalf("unexpected synthetic departments %#v err=%v", departments, err)
	}
	identities, err := peopleService.SearchIdentities(ctx, people.IdentityQuery{Limit: 100}, people.Visibility{All: true})
	if err != nil || len(identities) != 3 {
		t.Fatalf("unexpected synthetic identities %#v err=%v", identities, err)
	}
	for _, identity := range identities {
		if !strings.HasPrefix(identity.DisplayName, "[Synthetic Demo]") || !strings.HasPrefix(identity.Provider, "directory.") || identity.ProviderSubject == "" {
			t.Fatalf("identity is not clearly isolated synthetic data: %#v", identity)
		}
	}
	mappings, err := directoryStore.ListMappings(ctx, "demo-example", SyntheticSourceSystemID)
	if err != nil || len(mappings) != 8 {
		t.Fatalf("unexpected synthetic mappings count=%d err=%v", len(mappings), err)
	}
	for _, mapping := range mappings {
		if mapping.Provider != SyntheticProvider || !mapping.Active || mapping.LastAppliedBatchID != first.BatchID {
			t.Fatalf("mapping is not isolated or applied: %#v", mapping)
		}
	}
	graphStore, err := NewManagedGraphStore(directoryStore, "demo-example")
	if err != nil {
		t.Fatal(err)
	}
	graph, err := graphStore.Graph(ctx, GraphQuery{})
	if err != nil || len(graph.Nodes) != 4 || len(graph.Edges) != 3 {
		t.Fatalf("unexpected synthetic relationship graph %#v err=%v", graph, err)
	}

	second, err := seeder.Seed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second.CreatedLocations != 0 || !second.PreviewReplay || !second.ApplyReplay || second.BatchID != first.BatchID ||
		second.SiteID != first.SiteID || second.BuildingID != first.BuildingID || second.RoomID != first.RoomID || second.DepartmentID != first.DepartmentID {
		t.Fatalf("synthetic replay was not exact: first=%#v second=%#v", first, second)
	}
	secondSites, _ := peopleService.ListSites(ctx, people.Visibility{All: true})
	secondIdentities, _ := peopleService.SearchIdentities(ctx, people.IdentityQuery{Limit: 100}, people.Visibility{All: true})
	if len(secondSites) != 2 || len(secondIdentities) != 3 {
		t.Fatalf("synthetic replay duplicated data: sites=%d identities=%d", len(secondSites), len(secondIdentities))
	}
	syntheticAuditEvents := 0
	for id, event := range auditor.events {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		serialized := strings.ToLower(string(encoded))
		for _, privateValue := range []string{"avery", "casey", "helpdesk", "100 demo way", "example.invalid"} {
			if strings.Contains(serialized, privateValue) {
				t.Fatalf("audit event %s retained synthetic personal or address data: %s", id, serialized)
			}
		}
		if event.Action == "synthetic_demo.seeded" {
			syntheticAuditEvents++
			if event.Metadata["requirementId"] != SyntheticRequirementID || event.Metadata["sourceSystemId"] != SyntheticSourceSystemID || auditor.calls[id] != 2 {
				t.Fatalf("synthetic audit was not sanitized and exactly replayable: event=%#v calls=%d", event, auditor.calls[id])
			}
		}
	}
	if syntheticAuditEvents != 1 {
		t.Fatalf("expected one durable synthetic seed audit event, got %d", syntheticAuditEvents)
	}
}

func TestSyntheticSeederFailsClosedWhenDemoLabelCollidesWithDifferentLocalData(t *testing.T) {
	ctx := context.Background()
	seeder, peopleService, _, directoryStore, _ := newSyntheticTestSeeder(t, "demo-collision")
	created, err := peopleService.CreateSite(ctx, people.CreateSiteInput{
		Name:    "[Synthetic Demo] Lakeside Campus",
		Address: people.Address{Line1: "1 Real Street", City: "Real City", Region: "IL", PostalCode: "60001", Country: "US"},
		Status:  people.StatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seeder.Seed(ctx); !errors.Is(err, people.ErrConflict) {
		t.Fatalf("expected synthetic label collision to fail closed, got %v", err)
	}
	retained, err := peopleService.ListSites(ctx, people.Visibility{All: true})
	if err != nil || len(retained) != 1 || retained[0].ID != created.ID || retained[0].Address.Line1 != "1 Real Street" {
		t.Fatalf("colliding local record changed: %#v err=%v", retained, err)
	}
	mappings, err := directoryStore.ListMappings(ctx, "demo-collision", SyntheticSourceSystemID)
	if err != nil || len(mappings) != 0 {
		t.Fatalf("directory data was written after location conflict: %#v err=%v", mappings, err)
	}
}

func TestSyntheticConnectorIsSinglePageContextAwareAndReturnsFreshRecords(t *testing.T) {
	connector := SyntheticConnector{}
	if system := connector.SourceSystem(); system.ID != SyntheticSourceSystemID || system.Provider != SyntheticProvider || system.ConfigRevision != SyntheticConfigRevision {
		t.Fatalf("unexpected synthetic source system %#v", system)
	}
	page, err := connector.PullPage(context.Background(), "")
	if err != nil || !page.CompleteSnapshot || page.NextCursor != "" || len(page.Records) != 8 {
		t.Fatalf("unexpected synthetic page %#v err=%v", page, err)
	}
	page.Records[0].DirectoryAttributes["origin"] = "mutated"
	fresh, err := connector.PullPage(context.Background(), "")
	if err != nil || fresh.Records[0].DirectoryAttributes["origin"] != "synthetic-demo" {
		t.Fatalf("connector leaked mutable fixture state %#v err=%v", fresh, err)
	}
	if _, err := connector.PullPage(context.Background(), "unexpected"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid cursor rejection, got %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := connector.PullPage(canceled, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled pull, got %v", err)
	}
}

func newSyntheticTestSeeder(t *testing.T, organizationID string) (SyntheticSeeder, *people.Service, *repository.MemoryPeopleStore, *repository.MemoryDirectoryImportStore, *syntheticAuditRecorder) {
	t.Helper()
	auditor := newSyntheticAuditRecorder()
	peopleStore := repository.NewMemoryPeopleStore()
	directoryStore := repository.NewMemoryDirectoryImportStore()
	guardService, err := guard.NewService(repository.NewMemoryGuardStore(), contractPasswordHasher{}, auditor, nil, guard.ServiceConfig{OrganizationID: organizationID})
	if err != nil {
		t.Fatal(err)
	}
	peopleService, err := people.NewService(peopleStore, syntheticAssetReader{}, auditor, people.ServiceConfig{OrganizationID: organizationID})
	if err != nil {
		t.Fatal(err)
	}
	peopleTarget, err := NewPeopleTarget(peopleStore, guardService, nil)
	if err != nil {
		t.Fatal(err)
	}
	groupTarget, err := NewGroupTarget(directoryStore, guardService, nil)
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewDirectoryTarget(peopleTarget, groupTarget)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(SyntheticConnector{})
	if err != nil {
		t.Fatal(err)
	}
	directoryService, err := NewService(directoryStore, target, auditor, registry, ServiceConfig{OrganizationID: organizationID})
	if err != nil {
		t.Fatal(err)
	}
	return SyntheticSeeder{Enabled: true, OrganizationID: organizationID, People: peopleService, Directory: directoryService, Auditor: auditor}, peopleService, peopleStore, directoryStore, auditor
}
