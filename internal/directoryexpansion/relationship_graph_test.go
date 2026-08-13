package directoryexpansion_test

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	. "github.com/maxlemke/stewardmesh/internal/directoryexpansion"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

type graphAssetReader struct {
	items []domain.Asset
}

func (r graphAssetReader) GetAsset(_ context.Context, id string) (domain.Asset, error) {
	for _, item := range r.items {
		if item.ID == id {
			return item, nil
		}
	}
	return domain.Asset{}, atlas.ErrNotFound
}

func (r graphAssetReader) ListGraphAssets(_ context.Context, query atlas.Query) ([]domain.Asset, error) {
	items := make([]domain.Asset, 0, len(r.items))
	for _, item := range r.items {
		if query.SiteID != "" && item.SiteID != query.SiteID || query.DepartmentID != "" && item.DepartmentID != query.DepartmentID {
			continue
		}
		if query.Search != "" && !strings.Contains(strings.ToLower(item.Name), strings.ToLower(query.Search)) {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	if query.Limit > 0 && len(items) > query.Limit {
		items = items[:query.Limit]
	}
	return items, nil
}

func TestRelationshipGraphProjectsTypedRecordsCyclesAndSemanticDeduplication(t *testing.T) {
	store, peopleStore, assets := relationshipGraphFixture(t)
	graphStore, err := NewRelationshipGraphStore(store, peopleStore, assets, domain.Organization{ID: "example-org", Name: "Example Org"})
	if err != nil {
		t.Fatal(err)
	}
	graph, err := graphStore.Graph(context.Background(), GraphQuery{Limit: MaximumGraphLimit, Scope: GraphScope{
		Directory: people.Visibility{All: true}, Assets: AssetVisibility{All: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"organization:example-org", "site:site-a", "building:building-a", "room:room-a", "department:department-a",
		"person:person-a", "group:group-a", "group:group-b", "group:group-c", "subject:subject-a", "asset:asset-a",
	} {
		if !graphHasNode(graph, expected) {
			t.Errorf("missing typed graph node %q in %#v", expected, graph.Nodes)
		}
	}
	for _, expected := range []struct {
		from string
		to   string
		kind RelationshipKind
	}{
		{"asset:asset-a", "site:site-a", RelationshipLocatedAt},
		{"asset:asset-a", "department:department-a", RelationshipBelongsTo},
		{"asset:asset-a", "person:person-a", RelationshipAssignedTo},
	} {
		if !graphHasEdge(graph, expected.from, expected.to, expected.kind) {
			t.Errorf("missing projected asset relationship %s -%s-> %s", expected.from, expected.kind, expected.to)
		}
	}
	if !graphHasEdge(graph, "group:group-a", "group:group-b", RelationshipMemberOf) ||
		!graphHasEdge(graph, "group:group-b", "group:group-a", RelationshipMemberOf) {
		t.Fatalf("nested group cycle was not represented safely: %#v", graph.Edges)
	}
	seen := make(map[string]struct{}, len(graph.Edges))
	for _, edge := range graph.Edges {
		key := edge.From + "\x00" + string(edge.Kind) + "\x00" + edge.To
		if _, duplicate := seen[key]; duplicate {
			t.Fatalf("semantic duplicate edge returned: %#v", edge)
		}
		seen[key] = struct{}{}
		if !graphHasNode(graph, edge.From) || !graphHasNode(graph, edge.To) {
			t.Fatalf("edge exposes a missing endpoint: %#v", edge)
		}
	}
	encoded := strings.ToLower(graphText(graph))
	for _, privateValue := range []string{"alice@example.invalid", "secret-source-subject"} {
		if strings.Contains(encoded, privateValue) {
			t.Fatalf("graph exposed private source data %q: %s", privateValue, encoded)
		}
	}

	groups, err := graphStore.Graph(context.Background(), GraphQuery{Kind: NodeGroup, Relationship: RelationshipMemberOf, Limit: 10, Scope: GraphScope{
		Directory: people.Visibility{All: true},
	}})
	if err != nil || len(groups.Nodes) != 4 || len(groups.Edges) != 3 {
		t.Fatalf("group/cycle filter did not retain unique relationships, their subject endpoint, and the disconnected group: graph=%#v err=%v", groups, err)
	}
	if !graphHasNode(groups, "subject:subject-a") || !graphHasEdge(groups, "subject:subject-a", "group:group-a", RelationshipMemberOf) {
		t.Fatalf("relationship filtering omitted the connected cross-type endpoint: %#v", groups)
	}
	search, err := graphStore.Graph(context.Background(), GraphQuery{Search: "alice", Scope: GraphScope{Directory: people.Visibility{All: true}}})
	if err != nil || len(search.Nodes) != 1 || search.Nodes[0].ID != "person:person-a" || len(search.Edges) != 0 {
		t.Fatalf("search did not return a deterministic disconnected result: graph=%#v err=%v", search, err)
	}
	for _, filtered := range []GraphQuery{
		{Search: "Alice", Kind: NodePerson, Relationship: RelationshipAssignedTo},
		{Search: "North Laptop", Kind: NodeAsset, Relationship: RelationshipAssignedTo},
		{Search: "North Campus", Kind: NodeSite, Relationship: RelationshipLocatedAt},
		{Search: "North Laptop", Kind: NodeAsset, Relationship: RelationshipLocatedAt},
	} {
		filtered.Scope = GraphScope{Directory: people.Visibility{All: true}, Assets: AssetVisibility{All: true}}
		filteredGraph, filterErr := graphStore.Graph(context.Background(), filtered)
		if filterErr != nil || !graphHasNode(filteredGraph, "person:person-a") && filtered.Relationship == RelationshipAssignedTo ||
			!graphHasNode(filteredGraph, "asset:asset-a") || len(filteredGraph.Edges) == 0 {
			t.Fatalf("cross-type relationship filter lost a contextual endpoint: query=%#v graph=%#v err=%v", filtered, filteredGraph, filterErr)
		}
	}
}

func TestRelationshipGraphDelegatesSearchAndHonorsLimitsBeyondSourceDefaults(t *testing.T) {
	store, peopleStore, assets := relationshipGraphFixture(t)
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	for index := 0; index < 125; index++ {
		name := fmt.Sprintf("Scale Identity %03d", index)
		if index == 124 {
			name = "Zeta Exact Identity"
		}
		if _, err := peopleStore.CreateIdentity(context.Background(), people.Identity{
			ID: fmt.Sprintf("scale-person-%03d", index), OrganizationID: "example-org", Kind: people.IdentityPerson,
			DisplayName: name, NormalizedName: strings.ToLower(name), SiteID: "site-a", DepartmentID: "department-a",
			Status: people.StatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		assetName := fmt.Sprintf("Scale Asset %03d", index)
		assetID := fmt.Sprintf("scale-asset-%03d", index)
		if index == 124 {
			assetName = "Zeta Exact Asset"
			assetID = "zzzz-asset-target"
		}
		assets.items = append(assets.items, domain.Asset{
			ID: assetID, OrganizationID: "example-org", Name: assetName, Kind: "computer",
			SiteID: "site-a", DepartmentID: "department-a", Status: "active",
		})
	}
	graphStore, err := NewRelationshipGraphStore(store, peopleStore, assets, domain.Organization{ID: "example-org", Name: "Example Org"})
	if err != nil {
		t.Fatal(err)
	}
	identityResult, err := graphStore.Graph(context.Background(), GraphQuery{
		Search: "Zeta Exact Identity", Scope: GraphScope{Directory: people.Visibility{All: true}},
	})
	if err != nil || len(identityResult.Nodes) != 1 || identityResult.Nodes[0].ID != "person:scale-person-124" {
		t.Fatalf("identity search was truncated before filtering: graph=%#v err=%v", identityResult, err)
	}
	assetResult, err := graphStore.Graph(context.Background(), GraphQuery{
		Search: "Zeta Exact Asset", Kind: NodeAsset, Scope: GraphScope{Directory: people.Visibility{All: true}, Assets: AssetVisibility{All: true}},
	})
	if err != nil || len(assetResult.Nodes) != 1 || assetResult.Nodes[0].ID != "asset:zzzz-asset-target" {
		t.Fatalf("asset search was truncated before filtering: graph=%#v err=%v", assetResult, err)
	}
	allAssets, err := graphStore.Graph(context.Background(), GraphQuery{
		Kind: NodeAsset, Limit: MaximumGraphLimit, Scope: GraphScope{Directory: people.Visibility{All: true}, Assets: AssetVisibility{All: true}},
	})
	if err != nil || len(allAssets.Nodes) != len(assets.items) {
		t.Fatalf("500-node graph contract was silently capped by Atlas's public default: nodes=%d want=%d err=%v", len(allAssets.Nodes), len(assets.items), err)
	}
}

func TestRelationshipGraphIntersectsDirectoryAndAssetVisibility(t *testing.T) {
	store, peopleStore, assets := relationshipGraphFixture(t)
	graphStore, err := NewRelationshipGraphStore(store, peopleStore, assets, domain.Organization{ID: "example-org", Name: "Example Org"})
	if err != nil {
		t.Fatal(err)
	}
	graph, err := graphStore.Graph(context.Background(), GraphQuery{Limit: MaximumGraphLimit, Scope: GraphScope{
		Directory: people.Visibility{SiteIDs: []string{"site-a"}},
		Assets:    AssetVisibility{All: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, visible := range []string{"organization:example-org", "site:site-a", "person:person-a", "asset:asset-a"} {
		if !graphHasNode(graph, visible) {
			t.Errorf("authorized node %q was omitted: %#v", visible, graph.Nodes)
		}
	}
	for _, hidden := range []string{"site:site-b", "person:person-b", "asset:asset-b", "group:group-a", "subject:subject-a"} {
		if graphHasNode(graph, hidden) {
			t.Errorf("out-of-scope node %q leaked: %#v", hidden, graph.Nodes)
		}
	}

	crossScope, err := graphStore.Graph(context.Background(), GraphQuery{Scope: GraphScope{
		Directory: people.Visibility{SiteIDs: []string{"site-a"}},
		Assets:    AssetVisibility{ResourceIDs: []string{"asset-b"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if graphHasNode(crossScope, "asset:asset-b") {
		t.Fatalf("resource grant escaped the caller's directory visibility: %#v", crossScope.Nodes)
	}
	sameSiteOtherDepartment := graphAssetReader{items: append(append([]domain.Asset(nil), assets.items...), domain.Asset{
		ID: "asset-same-site-hidden-department", OrganizationID: "example-org", Name: "Same Site Hidden Department Asset",
		Kind: "laptop", SiteID: "site-a", DepartmentID: "department-b", Status: "active",
	})}
	departmentGraphStore, err := NewRelationshipGraphStore(store, peopleStore, sameSiteOtherDepartment, domain.Organization{ID: "example-org", Name: "Example Org"})
	if err != nil {
		t.Fatal(err)
	}
	departmentGraph, err := departmentGraphStore.Graph(context.Background(), GraphQuery{Limit: MaximumGraphLimit, Scope: GraphScope{
		Directory: people.Visibility{DepartmentIDs: []string{"department-a"}},
		Assets:    AssetVisibility{All: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if graphHasNode(departmentGraph, "asset:asset-same-site-hidden-department") {
		t.Fatalf("department scope inherited its ancestor site as an asset grant: %#v", departmentGraph.Nodes)
	}
	if !graphHasNode(departmentGraph, "asset:asset-a") {
		t.Fatalf("directly authorized department asset was omitted: %#v", departmentGraph.Nodes)
	}
	if _, err := graphStore.Graph(context.Background(), GraphQuery{}); !errors.Is(err, ErrGraphScope) {
		t.Fatalf("expected mandatory server-owned graph scope, got %v", err)
	}
	if _, err := graphStore.Graph(context.Background(), GraphQuery{Limit: MaximumGraphLimit + 1, Scope: GraphScope{Directory: people.Visibility{All: true}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected bounded graph limit, got %v", err)
	}
	if _, err := graphStore.Graph(context.Background(), GraphQuery{Kind: "unregistered", Scope: GraphScope{Directory: people.Visibility{All: true}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected an unregistered node kind to fail closed, got %v", err)
	}
}

func TestMemoryGraphFailsClosedWithoutOrganizationWideVisibility(t *testing.T) {
	store := NewMemoryGraph(Graph{Nodes: []Node{
		{ID: "person:one", Kind: NodePerson, Label: "Person One"},
		{ID: "asset:one", Kind: NodeAsset, Label: "Asset One"},
	}, Edges: []Edge{{ID: "assigned", From: "asset:one", To: "person:one", Kind: RelationshipAssignedTo}}})
	if _, err := store.Graph(context.Background(), GraphQuery{}); !errors.Is(err, ErrGraphScope) {
		t.Fatalf("expected a mandatory visibility scope, got %v", err)
	}
	graph, err := store.Graph(context.Background(), GraphQuery{Scope: GraphScope{Directory: people.Visibility{SiteIDs: []string{"site-a"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != 0 || len(graph.Edges) != 0 {
		t.Fatalf("unscoped memory graph leaked records to a scoped caller: %#v", graph)
	}
	directoryOnly, err := store.Graph(context.Background(), GraphQuery{Scope: GraphScope{Directory: people.Visibility{All: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if graphHasNode(directoryOnly, "asset:one") || len(directoryOnly.Edges) != 0 || !graphHasNode(directoryOnly, "person:one") {
		t.Fatalf("memory graph leaked an asset without an asset grant: %#v", directoryOnly)
	}
	resourceScoped, err := store.Graph(context.Background(), GraphQuery{Scope: GraphScope{
		Directory: people.Visibility{All: true}, Assets: AssetVisibility{ResourceIDs: []string{"one"}},
	}})
	if err != nil || !graphHasNode(resourceScoped, "asset:one") || len(resourceScoped.Edges) != 1 {
		t.Fatalf("memory graph did not honor an explicit asset resource grant: graph=%#v err=%v", resourceScoped, err)
	}
}

func TestRelationshipGraphEmptyStateUsesStableArrays(t *testing.T) {
	store := repository.NewMemoryDirectoryImportStore()
	peopleStore := repository.NewMemoryPeopleStore()
	graphStore, err := NewRelationshipGraphStore(store, peopleStore, graphAssetReader{}, domain.Organization{ID: "example-org", Name: "Example Org"})
	if err != nil {
		t.Fatal(err)
	}
	graph, err := graphStore.Graph(context.Background(), GraphQuery{Search: "not present", Scope: GraphScope{Directory: people.Visibility{All: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if graph.Nodes == nil || graph.Edges == nil || len(graph.Nodes) != 0 || len(graph.Edges) != 0 {
		t.Fatalf("empty state must use non-null stable arrays: %#v", graph)
	}
}

func TestRelationshipGraphRejectsCrossOrganizationAssetsFromInjectedReader(t *testing.T) {
	store, peopleStore, assets := relationshipGraphFixture(t)
	assets.items = append(assets.items, domain.Asset{
		ID: "foreign-asset", OrganizationID: "other-org", Name: "Foreign Asset", Kind: "computer", Status: "active",
	})
	graphStore, err := NewRelationshipGraphStore(store, peopleStore, assets, domain.Organization{ID: "example-org", Name: "Example Org"})
	if err != nil {
		t.Fatal(err)
	}
	graph, err := graphStore.Graph(context.Background(), GraphQuery{Limit: MaximumGraphLimit, Scope: GraphScope{
		Directory: people.Visibility{All: true}, Assets: AssetVisibility{All: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if graphHasNode(graph, "asset:foreign-asset") {
		t.Fatalf("injected asset reader escaped the organization boundary: %#v", graph.Nodes)
	}
}

func relationshipGraphFixture(t *testing.T) (*repository.MemoryDirectoryImportStore, *repository.MemoryPeopleStore, graphAssetReader) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	peopleStore := repository.NewMemoryPeopleStore()
	for _, site := range []people.Site{
		{ID: "site-a", OrganizationID: "example-org", Name: "North Campus", NormalizedName: "north campus", Status: people.StatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "site-b", OrganizationID: "example-org", Name: "South Campus", NormalizedName: "south campus", Status: people.StatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now},
	} {
		if _, err := peopleStore.CreateSite(ctx, site); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := peopleStore.CreateBuilding(ctx, people.Building{ID: "building-a", OrganizationID: "example-org", SiteID: "site-a", Name: "Main", NormalizedName: "main", Status: people.StatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := peopleStore.CreateRoom(ctx, people.Room{ID: "room-a", OrganizationID: "example-org", SiteID: "site-a", BuildingID: "building-a", Number: "101", NormalizedNumber: "101", Name: "Lab", Status: people.StatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	for _, department := range []people.Department{
		{ID: "department-a", OrganizationID: "example-org", SiteID: "site-a", Name: "Engineering", NormalizedName: "engineering", Status: people.StatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "department-b", OrganizationID: "example-org", SiteID: "site-b", Name: "Finance", NormalizedName: "finance", Status: people.StatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now},
	} {
		if _, err := peopleStore.CreateDepartment(ctx, department); err != nil {
			t.Fatal(err)
		}
	}
	for _, identity := range []people.Identity{
		{ID: "person-a", OrganizationID: "example-org", Kind: people.IdentityPerson, DisplayName: "Alice North", NormalizedName: "alice north", Email: "alice@example.invalid", NormalizedEmail: "alice@example.invalid", DepartmentID: "department-a", SiteID: "site-a", Status: people.StatusActive, Provider: "people-soft", ProviderSubject: "secret-source-subject", Revision: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "person-b", OrganizationID: "example-org", Kind: people.IdentityPerson, DisplayName: "Bob South", NormalizedName: "bob south", DepartmentID: "department-b", SiteID: "site-b", Status: people.StatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now},
	} {
		if _, err := peopleStore.CreateIdentity(ctx, identity); err != nil {
			t.Fatal(err)
		}
	}

	directoryStore := repository.NewMemoryDirectoryImportStore()
	for _, group := range []ManagedGroup{
		{ID: "group-a", OrganizationID: "example-org", SourceSystemID: "source", SourceRecordID: "group-source-a", DisplayName: "Group A", Status: "active", Revision: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "group-b", OrganizationID: "example-org", SourceSystemID: "source", SourceRecordID: "group-source-b", DisplayName: "Group B", Status: "active", Revision: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "group-c", OrganizationID: "example-org", SourceSystemID: "source", SourceRecordID: "group-source-c", DisplayName: "Disconnected Group", Status: "active", Revision: 1, CreatedAt: now, UpdatedAt: now},
	} {
		if _, err := directoryStore.CreateManagedGroup(ctx, group); err != nil {
			t.Fatal(err)
		}
	}
	for _, membership := range []ManagedMembership{
		{ID: "membership-a-b", OrganizationID: "example-org", SourceSystemID: "source", SourceRecordID: "member-source-a-b", GroupID: "group-b", MemberID: "group-a", MemberKind: MemberGroup, Status: "active", Revision: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "membership-b-a", OrganizationID: "example-org", SourceSystemID: "source", SourceRecordID: "member-source-b-a", GroupID: "group-a", MemberID: "group-b", MemberKind: MemberGroup, Status: "active", Revision: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "membership-a-b-duplicate", OrganizationID: "example-org", SourceSystemID: "source", SourceRecordID: "member-source-a-b-duplicate", GroupID: "group-b", MemberID: "group-a", MemberKind: MemberGroup, Status: "active", Revision: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "membership-subject-a", OrganizationID: "example-org", SourceSystemID: "source", SourceRecordID: "member-source-subject", GroupID: "group-a", MemberID: "subject-a", MemberKind: MemberSubject, MemberDisplayName: "Imported Subject", Status: "active", Revision: 1, CreatedAt: now, UpdatedAt: now},
	} {
		if _, err := directoryStore.CreateManagedMembership(ctx, membership); err != nil {
			t.Fatal(err)
		}
	}
	assets := graphAssetReader{items: []domain.Asset{
		{ID: "asset-a", OrganizationID: "example-org", Name: "North Laptop", Kind: "laptop", SiteID: "site-a", DepartmentID: "department-a", UserID: "person-a", Status: "active"},
		{ID: "asset-b", OrganizationID: "example-org", Name: "South Laptop", Kind: "laptop", SiteID: "site-b", DepartmentID: "department-b", UserID: "person-b", Status: "active"},
	}}
	return directoryStore, peopleStore, assets
}

func graphHasNode(graph Graph, id string) bool {
	for _, node := range graph.Nodes {
		if node.ID == id {
			return true
		}
	}
	return false
}

func graphHasEdge(graph Graph, from, to string, kind RelationshipKind) bool {
	for _, edge := range graph.Edges {
		if edge.From == from && edge.To == to && edge.Kind == kind {
			return true
		}
	}
	return false
}

func graphText(graph Graph) string {
	var text strings.Builder
	for _, node := range graph.Nodes {
		text.WriteString(node.ID)
		text.WriteString(node.Label)
		for key, value := range node.Attributes {
			text.WriteString(key)
			text.WriteString(value)
		}
	}
	return text.String()
}
