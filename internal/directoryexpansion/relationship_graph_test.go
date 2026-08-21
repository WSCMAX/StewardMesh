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
	items           []domain.Asset
	userSites       map[string]string
	userDepartments map[string]string
}

type recordingPeopleGraphStore struct {
	people.Store
	legacyCalls int
	graphLimits []int
}

func (s *recordingPeopleGraphStore) ListSites(ctx context.Context, organizationID string, visibility people.Visibility) ([]people.Site, error) {
	s.legacyCalls++
	return s.Store.ListSites(ctx, organizationID, visibility)
}

func (s *recordingPeopleGraphStore) ListBuildings(ctx context.Context, organizationID, siteID string, visibility people.Visibility) ([]people.Building, error) {
	s.legacyCalls++
	return s.Store.ListBuildings(ctx, organizationID, siteID, visibility)
}

func (s *recordingPeopleGraphStore) ListRooms(ctx context.Context, organizationID, siteID, buildingID string, visibility people.Visibility) ([]people.Room, error) {
	s.legacyCalls++
	return s.Store.ListRooms(ctx, organizationID, siteID, buildingID, visibility)
}

func (s *recordingPeopleGraphStore) ListDepartments(ctx context.Context, organizationID string, visibility people.Visibility) ([]people.Department, error) {
	s.legacyCalls++
	return s.Store.ListDepartments(ctx, organizationID, visibility)
}

func (s *recordingPeopleGraphStore) ListGraphLocations(ctx context.Context, organizationID string, query people.GraphLocationQuery, visibility people.Visibility) (people.GraphLocations, error) {
	s.graphLimits = append(s.graphLimits, query.Limit)
	return s.Store.ListGraphLocations(ctx, organizationID, query, visibility)
}

func (s *recordingPeopleGraphStore) ListGraphIdentities(ctx context.Context, organizationID string, query people.GraphIdentityQuery, visibility people.Visibility) ([]people.Identity, error) {
	s.graphLimits = append(s.graphLimits, query.Limit)
	return s.Store.ListGraphIdentities(ctx, organizationID, query, visibility)
}

type recordingGroupGraphStore struct {
	GroupTargetStore
	legacyCalls int
	graphLimits []int
}

func (s *recordingGroupGraphStore) ListManagedGroups(ctx context.Context, organizationID string) ([]ManagedGroup, error) {
	s.legacyCalls++
	return s.GroupTargetStore.ListManagedGroups(ctx, organizationID)
}

func (s *recordingGroupGraphStore) ListManagedMemberships(ctx context.Context, organizationID string) ([]ManagedMembership, error) {
	s.legacyCalls++
	return s.GroupTargetStore.ListManagedMemberships(ctx, organizationID)
}

func (s *recordingGroupGraphStore) ListGraphManagedGroups(ctx context.Context, organizationID string, query ManagedGroupGraphQuery) ([]ManagedGroup, error) {
	s.graphLimits = append(s.graphLimits, query.Limit)
	return s.GroupTargetStore.ListGraphManagedGroups(ctx, organizationID, query)
}

func (s *recordingGroupGraphStore) ListGraphManagedMemberships(ctx context.Context, organizationID string, query ManagedMembershipGraphQuery) ([]ManagedMembership, error) {
	s.graphLimits = append(s.graphLimits, query.Limit)
	return s.GroupTargetStore.ListGraphManagedMemberships(ctx, organizationID, query)
}

type recordingAssetGraphReader struct {
	inner  graphAssetReader
	limits []int
}

func (r *recordingAssetGraphReader) ListGraphAssets(ctx context.Context, query atlas.GraphAssetQuery) ([]domain.Asset, error) {
	r.limits = append(r.limits, query.Limit)
	return r.inner.ListGraphAssets(ctx, query)
}

func (r graphAssetReader) GetAsset(_ context.Context, id string) (domain.Asset, error) {
	for _, item := range r.items {
		if item.ID == id {
			return item, nil
		}
	}
	return domain.Asset{}, atlas.ErrNotFound
}

func (r graphAssetReader) ListGraphAssets(_ context.Context, query atlas.GraphAssetQuery) ([]domain.Asset, error) {
	items := make([]domain.Asset, 0, len(r.items))
	for _, item := range r.items {
		if !query.Visibility.All && !containsTestValue(query.Visibility.ResourceIDs, item.ID) &&
			!containsTestValue(query.Visibility.SiteIDs, item.SiteID) && !containsTestValue(query.Visibility.DepartmentIDs, item.DepartmentID) {
			continue
		}
		if !query.References.Empty() && !containsTestValue(query.References.ResourceIDs, item.ID) &&
			!containsTestValue(query.References.SiteIDs, item.SiteID) && !containsTestValue(query.References.BuildingIDs, item.BuildingID) &&
			!containsTestValue(query.References.RoomIDs, item.RoomID) && !containsTestValue(query.References.DepartmentIDs, item.DepartmentID) &&
			!containsTestValue(query.References.UserIDs, item.UserID) {
			continue
		}
		if !query.Directory.All && !containsTestValue(query.Directory.SiteIDs, item.SiteID) &&
			!containsTestValue(query.Directory.DepartmentIDs, item.DepartmentID) &&
			!containsTestValue(query.Directory.UserIDs, item.UserID) &&
			!(query.Directory.MatchUserDirectory && item.UserID != "" &&
				(containsTestValue(query.Directory.SiteIDs, r.userSites[item.UserID]) ||
					containsTestValue(query.Directory.DepartmentIDs, r.userDepartments[item.UserID]))) {
			continue
		}
		if query.LabelSearch != "" && !strings.Contains(strings.ToLower(item.Name), strings.ToLower(query.LabelSearch)) {
			continue
		}
		if query.DirectOrganizationChildren && (item.SiteID != "" || item.BuildingID != "" || item.RoomID != "" || item.DepartmentID != "" || item.UserID != "") {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		left, right := strings.ToLower(items[i].Name), strings.ToLower(items[j].Name)
		if left == right {
			return items[i].ID < items[j].ID
		}
		return left < right
	})
	if query.Limit > 0 && len(items) > query.Limit {
		items = items[:query.Limit]
	}
	return items, nil
}

func containsTestValue(values []string, target string) bool {
	if target == "" {
		return false
	}
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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

func TestRelationshipGraphUsesOnlyRequestedBoundedSourceLimits(t *testing.T) {
	groups, peopleStore, assets := relationshipGraphFixture(t)
	recordingPeople := &recordingPeopleGraphStore{Store: peopleStore}
	recordingGroups := &recordingGroupGraphStore{GroupTargetStore: groups}
	recordingAssets := &recordingAssetGraphReader{inner: assets}
	graphStore, err := NewRelationshipGraphStore(recordingGroups, recordingPeople, recordingAssets, domain.Organization{ID: "example-org", Name: "Example Org"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graphStore.Graph(context.Background(), GraphQuery{Limit: 1, Scope: GraphScope{
		Directory: people.Visibility{All: true}, Assets: AssetVisibility{All: true},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := graphStore.Graph(context.Background(), GraphQuery{
		Search: "Example Org", Kind: NodeOrganization, Relationship: RelationshipContains, Limit: 1,
		Scope: GraphScope{Directory: people.Visibility{All: true}, Assets: AssetVisibility{All: true}},
	}); err != nil {
		t.Fatal(err)
	}
	if recordingPeople.legacyCalls != 0 || recordingGroups.legacyCalls != 0 {
		t.Fatalf("graph used unbounded public list methods: people=%d groups=%d", recordingPeople.legacyCalls, recordingGroups.legacyCalls)
	}
	for source, limits := range map[string][]int{
		"people": recordingPeople.graphLimits, "groups": recordingGroups.graphLimits, "assets": recordingAssets.limits,
	} {
		if len(limits) == 0 {
			t.Fatalf("%s graph source was not exercised", source)
		}
		for _, limit := range limits {
			if limit != 1 {
				t.Fatalf("limit=1 graph asked %s source for %d rows: %#v", source, limit, limits)
			}
		}
	}
}

func TestRelationshipGraphLoadsRelationshipContextBeyondFiveHundredSourceRecords(t *testing.T) {
	store, peopleStore, assets := relationshipGraphFixture(t)
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	for index := 0; index < 500; index++ {
		name := fmt.Sprintf("Alpha Context Identity %03d", index)
		if _, err := peopleStore.CreateIdentity(context.Background(), people.Identity{
			ID: fmt.Sprintf("context-person-%03d", index), OrganizationID: "example-org", Kind: people.IdentityPerson,
			DisplayName: name, NormalizedName: strings.ToLower(name), SiteID: "site-a", DepartmentID: "department-a",
			Status: people.StatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	target := people.Identity{ID: "zzzz-context-person", OrganizationID: "example-org", Kind: people.IdentityPerson,
		DisplayName: "Zeta Context Person", NormalizedName: "zeta context person", SiteID: "site-a", DepartmentID: "department-a",
		Status: people.StatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if _, err := peopleStore.CreateIdentity(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	assets.items = append(assets.items, domain.Asset{ID: "zeta-context-asset", OrganizationID: "example-org", Name: "Zeta Context Asset",
		Kind: "computer", SiteID: "site-a", DepartmentID: "department-a", UserID: target.ID, Status: "active"})
	graphStore, err := NewRelationshipGraphStore(store, peopleStore, assets, domain.Organization{ID: "example-org", Name: "Example Org"})
	if err != nil {
		t.Fatal(err)
	}
	personResult, err := graphStore.Graph(context.Background(), GraphQuery{Search: target.DisplayName, Kind: NodePerson,
		Relationship: RelationshipAssignedTo, Limit: 10, Scope: GraphScope{Directory: people.Visibility{All: true}, Assets: AssetVisibility{All: true}}})
	if err != nil || !graphHasNode(personResult, "person:"+target.ID) || !graphHasNode(personResult, "asset:zeta-context-asset") ||
		!graphHasEdge(personResult, "asset:zeta-context-asset", "person:"+target.ID, RelationshipAssignedTo) {
		t.Fatalf("person relationship context was truncated before selection: graph=%#v err=%v", personResult, err)
	}
	assetResult, err := graphStore.Graph(context.Background(), GraphQuery{Search: "Zeta Context Asset", Kind: NodeAsset,
		Relationship: RelationshipAssignedTo, Limit: 10, Scope: GraphScope{Directory: people.Visibility{All: true}, Assets: AssetVisibility{All: true}}})
	if err != nil || !graphHasNode(assetResult, "person:"+target.ID) || !graphHasNode(assetResult, "asset:zeta-context-asset") ||
		!graphHasEdge(assetResult, "asset:zeta-context-asset", "person:"+target.ID, RelationshipAssignedTo) {
		t.Fatalf("asset relationship context was truncated before selection: graph=%#v err=%v", assetResult, err)
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
	}), userSites: assets.userSites, userDepartments: assets.userDepartments}
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

func TestRelationshipGraphAuthorizesUserOnlyAssetAfterIdentityProjection(t *testing.T) {
	store, peopleStore, assets := relationshipGraphFixture(t)
	assets.items = append(assets.items, domain.Asset{
		ID: "asset-user-only", OrganizationID: "example-org", Name: "User Only Laptop", Kind: "laptop",
		UserID: "person-a", Status: "active",
	})
	graphStore, err := NewRelationshipGraphStore(store, peopleStore, assets, domain.Organization{ID: "example-org", Name: "Example Org"})
	if err != nil {
		t.Fatal(err)
	}
	visible, err := graphStore.Graph(context.Background(), GraphQuery{
		Search: "User Only Laptop", Kind: NodeAsset,
		Scope: GraphScope{Directory: people.Visibility{SiteIDs: []string{"site-a"}}, Assets: AssetVisibility{All: true}},
	})
	if err != nil || !graphHasNode(visible, "asset:asset-user-only") {
		t.Fatalf("asset whose only directory-visible link is UserID was dropped: graph=%#v err=%v", visible, err)
	}
	hidden, err := graphStore.Graph(context.Background(), GraphQuery{
		Search: "User Only Laptop", Kind: NodeAsset,
		Scope: GraphScope{Directory: people.Visibility{SiteIDs: []string{"site-b"}}, Assets: AssetVisibility{All: true}},
	})
	if err != nil || graphHasNode(hidden, "asset:asset-user-only") {
		t.Fatalf("user-only asset escaped directory visibility: graph=%#v err=%v", hidden, err)
	}
}

func TestRelationshipGraphAppliesDirectoryVisibilityBeforeAssetLimit(t *testing.T) {
	store, peopleStore, assets := relationshipGraphFixture(t)
	for index := 0; index < 20; index++ {
		assets.items = append(assets.items, domain.Asset{
			ID: fmt.Sprintf("hidden-asset-%02d", index), OrganizationID: "example-org",
			Name: fmt.Sprintf("Alpha Hidden Asset %02d", index), Kind: "computer", SiteID: "site-b", Status: "active",
		})
	}
	assets.items = append(assets.items, domain.Asset{
		ID: "visible-after-hidden-assets", OrganizationID: "example-org", Name: "Zeta Visible Asset",
		Kind: "computer", SiteID: "site-a", Status: "active",
	})
	graphStore, err := NewRelationshipGraphStore(store, peopleStore, assets, domain.Organization{ID: "example-org", Name: "Example Org"})
	if err != nil {
		t.Fatal(err)
	}
	graph, err := graphStore.Graph(context.Background(), GraphQuery{
		Kind: NodeAsset, Limit: 10,
		Scope: GraphScope{Directory: people.Visibility{SiteIDs: []string{"site-a"}}, Assets: AssetVisibility{All: true}},
	})
	if err != nil || !graphHasNode(graph, "asset:visible-after-hidden-assets") {
		t.Fatalf("out-of-directory assets crowded a valid asset out before source limit: graph=%#v err=%v", graph, err)
	}
	for index := 0; index < 20; index++ {
		if graphHasNode(graph, fmt.Sprintf("asset:hidden-asset-%02d", index)) {
			t.Fatalf("out-of-directory asset leaked through the pre-limit predicate: %#v", graph)
		}
	}
}

func TestRelationshipGraphOrganizationContainsSelectsDirectChildrenBeforeSourceLimit(t *testing.T) {
	store, peopleStore, assets := relationshipGraphFixture(t)
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	for index := 0; index < 500; index++ {
		identityName := fmt.Sprintf("Alpha Nested Identity %03d", index)
		if _, err := peopleStore.CreateIdentity(context.Background(), people.Identity{
			ID: fmt.Sprintf("nested-person-%03d", index), OrganizationID: "example-org", Kind: people.IdentityPerson,
			DisplayName: identityName, NormalizedName: strings.ToLower(identityName), SiteID: "site-a", DepartmentID: "department-a",
			Status: people.StatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		assets.items = append(assets.items, domain.Asset{
			ID: fmt.Sprintf("nested-asset-%03d", index), OrganizationID: "example-org",
			Name: fmt.Sprintf("Alpha Nested Asset %03d", index), Kind: "computer", SiteID: "site-a", Status: "active",
		})
	}
	rootIdentity := people.Identity{ID: "root-person-target", OrganizationID: "example-org", Kind: people.IdentityPerson,
		DisplayName: "Zeta Root Person", NormalizedName: "zeta root person", Status: people.StatusActive,
		Revision: 1, CreatedAt: now, UpdatedAt: now}
	if _, err := peopleStore.CreateIdentity(context.Background(), rootIdentity); err != nil {
		t.Fatal(err)
	}
	assets.items = append(assets.items, domain.Asset{ID: "root-asset-target", OrganizationID: "example-org", Name: "Zeta Root Asset", Kind: "computer", Status: "active"})
	graphStore, err := NewRelationshipGraphStore(store, peopleStore, assets, domain.Organization{ID: "example-org", Name: "Example Org"})
	if err != nil {
		t.Fatal(err)
	}
	graph, err := graphStore.Graph(context.Background(), GraphQuery{
		Search: "Example Org", Kind: NodeOrganization, Relationship: RelationshipContains, Limit: 20,
		Scope: GraphScope{Directory: people.Visibility{All: true}, Assets: AssetVisibility{All: true}},
	})
	if err != nil || !graphHasEdge(graph, "organization:example-org", "person:"+rootIdentity.ID, RelationshipContains) ||
		!graphHasEdge(graph, "organization:example-org", "asset:root-asset-target", RelationshipContains) {
		t.Fatalf("nested records crowded direct organization children out before selection: graph=%#v err=%v", graph, err)
	}
	if graphHasNode(graph, "person:nested-person-000") || graphHasNode(graph, "asset:nested-asset-000") {
		t.Fatalf("organization containment included a non-direct source record: %#v", graph)
	}
}

func TestRelationshipGraphRejectsAggregateVisibilitySelectorsOverMaximum(t *testing.T) {
	store, peopleStore, assets := relationshipGraphFixture(t)
	graphStore, err := NewRelationshipGraphStore(store, peopleStore, assets, domain.Organization{ID: "example-org", Name: "Example Org"})
	if err != nil {
		t.Fatal(err)
	}
	sites := make([]string, MaximumGraphLimit)
	for index := range sites {
		sites[index] = fmt.Sprintf("site-%03d", index)
	}
	_, err = graphStore.Graph(context.Background(), GraphQuery{Scope: GraphScope{
		Directory: people.Visibility{SiteIDs: sites}, Assets: AssetVisibility{ResourceIDs: []string{"asset-extra"}},
	}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected aggregate visibility selectors to fail as invalid input, got %v", err)
	}
	for name, invalidID := range map[string]string{
		"oversized": strings.Repeat("x", 129),
		"control":   "site\ncontrol",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := graphStore.Graph(context.Background(), GraphQuery{Scope: GraphScope{
				Directory: people.Visibility{SiteIDs: []string{invalidID}},
			}})
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("expected invalid selector ID to fail before a source query, got %v", err)
			}
		})
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

func TestRelationshipGraphProjectsOccupancyEdges(t *testing.T) {
	peopleStore := repository.NewMemoryPeopleStore()
	now := time.Date(2026, time.August, 18, 16, 0, 0, 0, time.UTC)
	ctx := context.Background()
	if _, err := peopleStore.CreateSite(ctx, people.Site{ID: "site-occ", OrganizationID: "example-org", Name: "Campus", NormalizedName: "campus", Status: people.StatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := peopleStore.CreateBuilding(ctx, people.Building{ID: "building-occ", OrganizationID: "example-org", SiteID: "site-occ", Name: "Hall", NormalizedName: "hall", Status: people.StatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := peopleStore.CreateRoom(ctx, people.Room{ID: "room-office", OrganizationID: "example-org", SiteID: "site-occ", BuildingID: "building-occ", Number: "10", NormalizedNumber: "10", Name: "Office", Status: people.StatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := peopleStore.CreateRoom(ctx, people.Room{ID: "room-class", OrganizationID: "example-org", SiteID: "site-occ", BuildingID: "building-occ", Number: "20", NormalizedNumber: "20", Name: "Lecture", Status: people.StatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := peopleStore.CreateIdentity(ctx, people.Identity{
		ID: "person-occ", OrganizationID: "example-org", Kind: people.IdentityPerson, DisplayName: "Casey Hall",
		NormalizedName: "casey hall", Email: "casey@example.invalid", NormalizedEmail: "casey@example.invalid",
		SiteID: "site-occ", BuildingID: "building-occ", RoomID: "room-office", Status: people.StatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := peopleStore.CreateLocationReferenceType(ctx, people.LocationReferenceType{
		ID: "type-teach", OrganizationID: "example-org", Name: "Instructor", NormalizedName: "instructor",
		RelationshipKind: people.RelationshipTeachesIn, LocationKind: people.LocationKindRoom, Status: people.StatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := peopleStore.CreateLocationReference(ctx, people.LocationReference{
		ID: "ref-teach", OrganizationID: "example-org", IdentityID: "person-occ", TypeID: "type-teach",
		LocationKind: people.LocationKindRoom, LocationID: "room-class", Priority: people.LocationPrioritySecondary,
		Status: people.StatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	graphStore, err := NewRelationshipGraphStore(repository.NewMemoryDirectoryImportStore(), peopleStore, graphAssetReader{}, domain.Organization{ID: "example-org", Name: "Example Org"})
	if err != nil {
		t.Fatal(err)
	}
	graph, err := graphStore.Graph(ctx, GraphQuery{Limit: 50, Scope: GraphScope{Directory: people.Visibility{All: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if !graphHasNode(graph, "person:person-occ") || !graphHasNode(graph, "room:room-office") || !graphHasNode(graph, "room:room-class") {
		t.Fatalf("occupancy graph missing person or rooms: %#v", graph.Nodes)
	}
	if !graphHasEdge(graph, "person:person-occ", "room:room-office", RelationshipLocatedAt) {
		t.Fatalf("missing primary located_at edge: %#v", graph.Edges)
	}
	if !graphHasEdge(graph, "person:person-occ", "room:room-class", RelationshipTeachesIn) {
		t.Fatalf("missing teaches_in occupancy edge: %#v", graph.Edges)
	}
	filtered, err := graphStore.Graph(ctx, GraphQuery{Search: "Lecture", Kind: NodeRoom, Relationship: RelationshipTeachesIn, Limit: 50, Scope: GraphScope{Directory: people.Visibility{All: true}}})
	if err != nil || !graphHasNode(filtered, "person:person-occ") || !graphHasEdge(filtered, "person:person-occ", "room:room-class", RelationshipTeachesIn) {
		t.Fatalf("room usage filter omitted instructor occupancy: graph=%#v err=%v", filtered, err)
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
	}, userSites: map[string]string{"person-a": "site-a", "person-b": "site-b"},
		userDepartments: map[string]string{"person-a": "department-a", "person-b": "department-b"}}
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
