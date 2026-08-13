package directoryexpansion

// Requirements: REQ-DIRECTORY-EXPANSION-004, REQ-DIRECTORY-EXPANSION-005, REQ-DIRECTORY-EXPANSION-006, REQ-DIRECTORY-EXPANSION-008.
// Features: identity.directory, integrations.protocols, threads.relationships.

import (
	"context"
	"errors"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/people"
)

// AssetGraphReader is the bounded Atlas seam used by the relationship graph.
// Authorization remains caller-owned and is intersected with directory scope
// before any asset is added to a response.
type AssetGraphReader interface {
	ListGraphAssets(context.Context, atlas.Query) ([]domain.Asset, error)
	GetAsset(context.Context, string) (domain.Asset, error)
}

// ManagedGraphStore preserves the provider-only projection used by connector
// contract tests. Scoped callers receive no group data because managed groups
// do not currently carry a site or department visibility boundary.
type ManagedGraphStore struct {
	store          GroupTargetStore
	organizationID string
}

func NewManagedGraphStore(store GroupTargetStore, organizationID string) (*ManagedGraphStore, error) {
	organizationID = strings.TrimSpace(organizationID)
	if store == nil || organizationID == "" {
		return nil, errors.New("group target store and organization id are required")
	}
	return &ManagedGraphStore{store: store, organizationID: organizationID}, nil
}

func (s *ManagedGraphStore) Graph(ctx context.Context, query GraphQuery) (Graph, error) {
	query, err := normalizeGraphQuery(query, true)
	if err != nil {
		return Graph{}, err
	}
	if !query.Scope.Directory.All {
		return emptyGraph(), nil
	}
	builder := newGraphBuilder()
	if err := s.project(ctx, builder, ""); err != nil {
		return Graph{}, err
	}
	return filterGraph(builder.graph(), query), nil
}

// RelationshipGraphStore projects organization, People, imported directory,
// and authorized Atlas records into one typed read model. The source stores
// remain authoritative; graph reads never copy or mutate domain state.
type RelationshipGraphStore struct {
	groups       GroupTargetStore
	people       people.Store
	assets       AssetGraphReader
	organization domain.Organization
}

func NewRelationshipGraphStore(groups GroupTargetStore, peopleStore people.Store, assets AssetGraphReader, organization domain.Organization) (*RelationshipGraphStore, error) {
	organization.ID = strings.TrimSpace(organization.ID)
	organization.Name = strings.TrimSpace(organization.Name)
	if groups == nil || peopleStore == nil || assets == nil || organization.ID == "" || organization.Name == "" {
		return nil, errors.New("group, People, Atlas, and organization graph dependencies are required")
	}
	return &RelationshipGraphStore{groups: groups, people: peopleStore, assets: assets, organization: organization}, nil
}

func (s *RelationshipGraphStore) Graph(ctx context.Context, query GraphQuery) (Graph, error) {
	query, err := normalizeGraphQuery(query, true)
	if err != nil {
		return Graph{}, err
	}
	builder := newGraphBuilder()
	sourceQuery := query
	if query.Relationship != "" {
		// Relationship queries need both endpoints. Filtering source reads by an
		// anchor label/type would remove the contextual endpoint before the edge
		// can be constructed, so those queries filter after the bounded projection.
		sourceQuery.Search = ""
		sourceQuery.Kind = ""
	}
	organizationNodeID := typedNodeID(NodeOrganization, s.organization.ID)
	builder.addNode(Node{ID: organizationNodeID, Kind: NodeOrganization, Label: s.organization.Name})

	if err := s.projectPeople(ctx, builder, query.Scope.Directory, organizationNodeID, sourceQuery); err != nil {
		return Graph{}, err
	}
	if query.Scope.Directory.All {
		managed := &ManagedGraphStore{store: s.groups, organizationID: s.organization.ID}
		if err := managed.project(ctx, builder, organizationNodeID); err != nil {
			return Graph{}, err
		}
	}
	if !query.Scope.Assets.Empty() && (sourceQuery.Kind == "" || sourceQuery.Kind == NodeAsset) {
		if err := s.projectAssets(ctx, builder, query.Scope, organizationNodeID, sourceQuery); err != nil {
			return Graph{}, err
		}
	}
	return filterGraph(builder.graph(), query), nil
}

func (s *RelationshipGraphStore) projectPeople(ctx context.Context, builder *graphBuilder, visibility people.Visibility, organizationNodeID string, graphQuery GraphQuery) error {
	sites, err := s.people.ListSites(ctx, s.organization.ID, visibility)
	if err != nil {
		return err
	}
	buildings, err := s.people.ListBuildings(ctx, s.organization.ID, "", visibility)
	if err != nil {
		return err
	}
	rooms, err := s.people.ListRooms(ctx, s.organization.ID, "", "", visibility)
	if err != nil {
		return err
	}
	departments, err := s.people.ListDepartments(ctx, s.organization.ID, visibility)
	if err != nil {
		return err
	}
	identityQuery := people.IdentityQuery{Search: graphQuery.Search, Status: people.StatusActive, Limit: MaximumGraphLimit}
	if kind, ok := graphIdentityKind(graphQuery.Kind); ok {
		identityQuery.Kind = kind
	}
	identities, err := s.people.SearchIdentities(ctx, s.organization.ID, identityQuery, visibility)
	if err != nil {
		return err
	}

	for _, site := range sites {
		if site.Status != people.StatusActive {
			continue
		}
		id := typedNodeID(NodeSite, site.ID)
		builder.addNode(Node{ID: id, Kind: NodeSite, Label: site.Name, Attributes: statusAttributes(site.Status, "local")})
		builder.addEdge(RelationshipContains, organizationNodeID, id, nil)
	}
	for _, building := range buildings {
		if building.Status != people.StatusActive {
			continue
		}
		parentID := typedNodeID(NodeSite, building.SiteID)
		if !builder.hasNode(parentID) {
			continue
		}
		id := typedNodeID(NodeBuilding, building.ID)
		builder.addNode(Node{ID: id, Kind: NodeBuilding, Label: building.Name, Attributes: statusAttributes(building.Status, "local")})
		builder.addEdge(RelationshipContains, parentID, id, nil)
	}
	for _, room := range rooms {
		if room.Status != people.StatusActive {
			continue
		}
		parentID := typedNodeID(NodeBuilding, room.BuildingID)
		if !builder.hasNode(parentID) {
			continue
		}
		label := "Room " + room.Number
		if room.Name != "" {
			label += " · " + room.Name
		}
		id := typedNodeID(NodeRoom, room.ID)
		builder.addNode(Node{ID: id, Kind: NodeRoom, Label: label, Attributes: statusAttributes(room.Status, "local")})
		builder.addEdge(RelationshipContains, parentID, id, nil)
	}
	for _, department := range departments {
		if department.Status != people.StatusActive {
			continue
		}
		id := typedNodeID(NodeDepartment, department.ID)
		builder.addNode(Node{ID: id, Kind: NodeDepartment, Label: department.Name, Attributes: statusAttributes(department.Status, "local")})
		parentID := organizationNodeID
		if candidate := typedNodeID(NodeSite, department.SiteID); department.SiteID != "" && builder.hasNode(candidate) {
			parentID = candidate
		}
		builder.addEdge(RelationshipContains, parentID, id, nil)
	}
	for _, identity := range identities {
		if identity.Status != people.StatusActive {
			continue
		}
		kind := identityNodeKind(identity.Kind)
		id := typedNodeID(kind, identity.ID)
		origin := "local"
		if identity.Provider != "" {
			origin = "imported"
		}
		builder.addNode(Node{ID: id, Kind: kind, Label: identity.DisplayName, Attributes: statusAttributes(identity.Status, origin)})
		related := false
		if target := typedNodeID(NodeDepartment, identity.DepartmentID); identity.DepartmentID != "" && builder.hasNode(target) {
			builder.addEdge(RelationshipBelongsTo, id, target, nil)
			related = true
		}
		if target := typedNodeID(NodeSite, identity.SiteID); identity.SiteID != "" && builder.hasNode(target) {
			builder.addEdge(RelationshipLocatedAt, id, target, nil)
			related = true
		}
		if !related {
			builder.addEdge(RelationshipContains, organizationNodeID, id, nil)
		}
	}
	return nil
}

func (s *ManagedGraphStore) project(ctx context.Context, builder *graphBuilder, organizationNodeID string) error {
	groups, err := s.store.ListManagedGroups(ctx, s.organizationID)
	if err != nil {
		return err
	}
	memberships, err := s.store.ListManagedMemberships(ctx, s.organizationID)
	if err != nil {
		return err
	}
	activeGroups := make(map[string]ManagedGroup, len(groups))
	for _, group := range groups {
		if group.Status != "active" {
			continue
		}
		activeGroups[group.ID] = group
		id := typedNodeID(NodeGroup, group.ID)
		builder.addNode(Node{ID: id, Kind: NodeGroup, Label: group.DisplayName, Attributes: statusAttributes(group.Status, "imported")})
		if organizationNodeID != "" {
			builder.addEdge(RelationshipContains, organizationNodeID, id, nil)
		}
	}
	for _, membership := range memberships {
		if membership.Status != "active" {
			continue
		}
		if _, present := activeGroups[membership.GroupID]; !present {
			continue
		}
		to := typedNodeID(NodeGroup, membership.GroupID)
		from := ""
		if membership.MemberKind == MemberGroup {
			if _, present := activeGroups[membership.MemberID]; !present {
				continue
			}
			from = typedNodeID(NodeGroup, membership.MemberID)
		} else {
			from = typedNodeID(NodeSubject, membership.MemberID)
			builder.addNode(Node{ID: from, Kind: NodeSubject, Label: membership.MemberDisplayName, Attributes: statusAttributes(membership.Status, "imported")})
		}
		builder.addEdgeWithID(membership.ID, RelationshipMemberOf, from, to, map[string]string{"origin": "imported"})
	}
	return nil
}

func (s *RelationshipGraphStore) projectAssets(ctx context.Context, builder *graphBuilder, scope GraphScope, organizationNodeID string, graphQuery GraphQuery) error {
	assets, err := s.loadScopedAssets(ctx, scope.Assets, graphQuery.Search)
	if err != nil {
		return err
	}
	for _, asset := range assets {
		if asset.OrganizationID != s.organization.ID {
			continue
		}
		if !assetVisible(asset, scope, builder) {
			continue
		}
		id := typedNodeID(NodeAsset, asset.ID)
		builder.addNode(Node{ID: id, Kind: NodeAsset, Label: asset.Name, Attributes: map[string]string{
			"asset_kind": asset.Kind,
			"status":     asset.Status,
		}})
		related := false
		for _, reference := range []struct {
			kind NodeKind
			id   string
		}{
			{NodeSite, asset.SiteID},
			{NodeBuilding, asset.BuildingID},
			{NodeRoom, asset.RoomID},
		} {
			target := typedNodeID(reference.kind, reference.id)
			if reference.id != "" && builder.hasNode(target) {
				builder.addEdge(RelationshipLocatedAt, id, target, nil)
				related = true
			}
		}
		if target := typedNodeID(NodeDepartment, asset.DepartmentID); asset.DepartmentID != "" && builder.hasNode(target) {
			builder.addEdge(RelationshipBelongsTo, id, target, nil)
			related = true
		}
		if asset.UserID != "" {
			for _, kind := range []NodeKind{NodePerson, NodeShared, NodePublic, NodeLab} {
				target := typedNodeID(kind, asset.UserID)
				if builder.hasNode(target) {
					builder.addEdge(RelationshipAssignedTo, id, target, nil)
					related = true
					break
				}
			}
		}
		if !related {
			builder.addEdge(RelationshipContains, organizationNodeID, id, nil)
		}
	}
	return nil
}

func (s *RelationshipGraphStore) loadScopedAssets(ctx context.Context, visibility AssetVisibility, search string) ([]domain.Asset, error) {
	assetsByID := make(map[string]domain.Asset)
	add := func(items []domain.Asset) {
		for _, asset := range items {
			if len(assetsByID) == MaximumGraphLimit {
				return
			}
			assetsByID[asset.ID] = asset
		}
	}
	list := func(query atlas.Query) error {
		query.Search = search
		query.Limit = MaximumGraphLimit
		items, err := s.assets.ListGraphAssets(ctx, query)
		if err != nil {
			return err
		}
		add(items)
		return nil
	}
	if visibility.All {
		if err := list(atlas.Query{}); err != nil {
			return nil, err
		}
	} else {
		for _, id := range visibility.ResourceIDs {
			asset, err := s.assets.GetAsset(ctx, id)
			if errors.Is(err, atlas.ErrNotFound) || errors.Is(err, atlas.ErrInvalidInput) {
				continue
			}
			if err != nil {
				return nil, err
			}
			add([]domain.Asset{asset})
		}
		for _, siteID := range visibility.SiteIDs {
			if err := list(atlas.Query{SiteID: siteID}); errors.Is(err, atlas.ErrInvalidInput) {
				continue
			} else if err != nil {
				return nil, err
			}
		}
		for _, departmentID := range visibility.DepartmentIDs {
			if err := list(atlas.Query{DepartmentID: departmentID}); errors.Is(err, atlas.ErrInvalidInput) {
				continue
			} else if err != nil {
				return nil, err
			}
		}
	}
	assets := make([]domain.Asset, 0, len(assetsByID))
	for _, asset := range assetsByID {
		assets = append(assets, asset)
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].ID < assets[j].ID })
	return assets, nil
}

func assetVisible(asset domain.Asset, scope GraphScope, builder *graphBuilder) bool {
	assetAllowed := scope.Assets.All || containsString(scope.Assets.ResourceIDs, asset.ID) ||
		(asset.SiteID != "" && containsString(scope.Assets.SiteIDs, asset.SiteID)) ||
		(asset.DepartmentID != "" && containsString(scope.Assets.DepartmentIDs, asset.DepartmentID))
	if !assetAllowed {
		return false
	}
	if scope.Directory.All {
		return true
	}
	// Only direct site and department grants authorize location-bound assets.
	// Department visibility deliberately exposes its ancestor site for People
	// navigation; treating that derived node as a site grant would reveal assets
	// owned by other departments at the same site.
	if asset.SiteID != "" && containsString(scope.Directory.SiteIDs, asset.SiteID) {
		return true
	}
	if asset.DepartmentID != "" && containsString(scope.Directory.DepartmentIDs, asset.DepartmentID) {
		return true
	}
	if asset.UserID != "" {
		for _, kind := range []NodeKind{NodePerson, NodeShared, NodePublic, NodeLab} {
			if builder.hasNode(typedNodeID(kind, asset.UserID)) {
				return true
			}
		}
	}
	return false
}

func identityNodeKind(kind people.IdentityKind) NodeKind {
	switch kind {
	case people.IdentityPerson:
		return NodePerson
	case people.IdentityShared:
		return NodeShared
	case people.IdentityPublic:
		return NodePublic
	case people.IdentityLab:
		return NodeLab
	default:
		return NodeSubject
	}
}

func graphIdentityKind(kind NodeKind) (people.IdentityKind, bool) {
	switch kind {
	case NodePerson:
		return people.IdentityPerson, true
	case NodeShared:
		return people.IdentityShared, true
	case NodePublic:
		return people.IdentityPublic, true
	case NodeLab:
		return people.IdentityLab, true
	default:
		return "", false
	}
}

func statusAttributes(status any, origin string) map[string]string {
	return map[string]string{"status": strings.TrimSpace(stringValue(status)), "origin": origin}
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case people.RecordStatus:
		return string(typed)
	default:
		return ""
	}
}

func typedNodeID(kind NodeKind, recordID string) string {
	return string(kind) + ":" + recordID
}

type graphBuilder struct {
	nodes map[string]Node
	edges map[string]Edge
}

func newGraphBuilder() *graphBuilder {
	return &graphBuilder{nodes: make(map[string]Node), edges: make(map[string]Edge)}
}

func (b *graphBuilder) addNode(node Node) {
	if node.ID == "" || node.Kind == "" || strings.TrimSpace(node.Label) == "" {
		return
	}
	if existing, ok := b.nodes[node.ID]; ok {
		if graphNodeLess(node, existing) {
			b.nodes[node.ID] = node
		}
		return
	}
	b.nodes[node.ID] = node
}

func (b *graphBuilder) hasNode(id string) bool {
	_, ok := b.nodes[id]
	return ok
}

func (b *graphBuilder) addEdge(kind RelationshipKind, from, to string, attributes map[string]string) {
	b.addEdgeWithID("", kind, from, to, attributes)
}

func (b *graphBuilder) addEdgeWithID(id string, kind RelationshipKind, from, to string, attributes map[string]string) {
	if kind == "" || !b.hasNode(from) || !b.hasNode(to) {
		return
	}
	key := from + "\x00" + string(kind) + "\x00" + to
	if id == "" {
		id = digestStrings(GraphRequirementID, from, string(kind), to)[:32]
	}
	edge := Edge{ID: id, From: from, To: to, Kind: kind, Attributes: cloneMetadata(attributes)}
	if existing, ok := b.edges[key]; !ok || edge.ID < existing.ID {
		b.edges[key] = edge
	}
}

func (b *graphBuilder) graph() Graph {
	nodes := make([]Node, 0, len(b.nodes))
	for _, node := range b.nodes {
		nodes = append(nodes, node)
	}
	edges := make([]Edge, 0, len(b.edges))
	for _, edge := range b.edges {
		edges = append(edges, edge)
	}
	return Graph{Nodes: nodes, Edges: edges}
}

func normalizeGraphQuery(query GraphQuery, requireScope bool) (GraphQuery, error) {
	query.Search = strings.TrimSpace(query.Search)
	query.Kind = NodeKind(strings.ToLower(strings.TrimSpace(string(query.Kind))))
	query.Relationship = RelationshipKind(strings.ToLower(strings.TrimSpace(string(query.Relationship))))
	if !validGraphText(query.Search, 200) || !validNodeKind(query.Kind) || !validRelationshipKind(query.Relationship) {
		return GraphQuery{}, ErrInvalidInput
	}
	if query.Limit == 0 {
		query.Limit = DefaultGraphLimit
	}
	if query.Limit < 1 || query.Limit > MaximumGraphLimit {
		return GraphQuery{}, ErrInvalidInput
	}
	query.Scope.Directory.DepartmentIDs = uniqueGraphValues(query.Scope.Directory.DepartmentIDs)
	query.Scope.Directory.SiteIDs = uniqueGraphValues(query.Scope.Directory.SiteIDs)
	query.Scope.Assets.ResourceIDs = uniqueGraphValues(query.Scope.Assets.ResourceIDs)
	query.Scope.Assets.SiteIDs = uniqueGraphValues(query.Scope.Assets.SiteIDs)
	query.Scope.Assets.DepartmentIDs = uniqueGraphValues(query.Scope.Assets.DepartmentIDs)
	if requireScope && query.Scope.Directory.Empty() {
		return GraphQuery{}, ErrGraphScope
	}
	return query, nil
}

func validNodeKind(kind NodeKind) bool {
	switch kind {
	case "", NodeOrganization, NodeSite, NodeBuilding, NodeRoom, NodeDepartment, NodePerson,
		NodeShared, NodePublic, NodeLab, NodeGroup, NodeSubject, NodeAsset:
		return true
	default:
		return false
	}
}

func validRelationshipKind(kind RelationshipKind) bool {
	switch kind {
	case "", RelationshipContains, RelationshipBelongsTo, RelationshipLocatedAt, RelationshipMemberOf, RelationshipAssignedTo:
		return true
	default:
		return false
	}
}

func validGraphText(value string, maximum int) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func uniqueGraphValues(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func containsString(values []string, target string) bool {
	index := sort.SearchStrings(values, target)
	return index < len(values) && values[index] == target
}

func filterGraph(graph Graph, query GraphQuery) Graph {
	nodes := append([]Node(nil), graph.Nodes...)
	sort.Slice(nodes, func(i, j int) bool { return graphNodeLess(nodes[i], nodes[j]) })
	available := make(map[string]Node, len(nodes))
	anchors := make([]Node, 0, len(nodes))
	seenNodes := make(map[string]struct{}, len(nodes))
	search := strings.ToLower(query.Search)
	for _, node := range nodes {
		if _, duplicate := seenNodes[node.ID]; duplicate {
			continue
		}
		seenNodes[node.ID] = struct{}{}
		available[node.ID] = node
		if query.Kind != "" && node.Kind != query.Kind {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(node.Label), search) {
			continue
		}
		anchors = append(anchors, node)
	}
	edges := append([]Edge(nil), graph.Edges...)
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Kind != edges[j].Kind {
			return edges[i].Kind < edges[j].Kind
		}
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		return edges[i].ID < edges[j].ID
	})
	if query.Relationship != "" {
		return filterRelationshipGraph(available, anchors, edges, query)
	}

	result := emptyGraph()
	allowed := make(map[string]struct{}, min(query.Limit, len(anchors)))
	for _, node := range anchors {
		if len(result.Nodes) == query.Limit {
			break
		}
		result.Nodes = append(result.Nodes, node)
		allowed[node.ID] = struct{}{}
	}
	seenEdges := make(map[string]struct{}, len(edges))
	for _, edge := range edges {
		if _, ok := allowed[edge.From]; !ok {
			continue
		}
		if _, ok := allowed[edge.To]; !ok {
			continue
		}
		key := edge.From + "\x00" + string(edge.Kind) + "\x00" + edge.To
		if _, duplicate := seenEdges[key]; duplicate {
			continue
		}
		seenEdges[key] = struct{}{}
		result.Edges = append(result.Edges, edge)
		if len(result.Edges) == MaximumGraphEdges {
			break
		}
	}
	return result
}

// filterRelationshipGraph treats search and node-kind matches as anchors and
// returns both endpoints of matching relationships. Without the contextual
// endpoint, a cross-type filter such as asset + located_at could never expose
// the relationship it asks for. The record limit still bounds the complete
// response, and matching anchors without an included edge remain available as
// disconnected rows when capacity permits.
func filterRelationshipGraph(available map[string]Node, anchors []Node, edges []Edge, query GraphQuery) Graph {
	result := emptyGraph()
	anchorIDs := make(map[string]struct{}, len(anchors))
	for _, node := range anchors {
		anchorIDs[node.ID] = struct{}{}
	}
	selected := make(map[string]struct{}, min(query.Limit, len(available)))
	seenEdges := make(map[string]struct{}, len(edges))
	for _, edge := range edges {
		if edge.Kind != query.Relationship {
			continue
		}
		if _, matchesFrom := anchorIDs[edge.From]; !matchesFrom {
			if _, matchesTo := anchorIDs[edge.To]; !matchesTo {
				continue
			}
		}
		from, fromPresent := available[edge.From]
		to, toPresent := available[edge.To]
		if !fromPresent || !toPresent {
			continue
		}
		needed := 0
		if _, present := selected[from.ID]; !present {
			needed++
		}
		if _, present := selected[to.ID]; !present {
			needed++
		}
		if len(selected)+needed > query.Limit {
			continue
		}
		selected[from.ID] = struct{}{}
		selected[to.ID] = struct{}{}
		key := edge.From + "\x00" + string(edge.Kind) + "\x00" + edge.To
		if _, duplicate := seenEdges[key]; duplicate {
			continue
		}
		seenEdges[key] = struct{}{}
		result.Edges = append(result.Edges, edge)
		if len(result.Edges) == MaximumGraphEdges {
			break
		}
	}
	for _, node := range anchors {
		if len(selected) == query.Limit {
			break
		}
		selected[node.ID] = struct{}{}
	}
	for _, node := range available {
		if _, present := selected[node.ID]; present {
			result.Nodes = append(result.Nodes, node)
		}
	}
	sort.Slice(result.Nodes, func(i, j int) bool { return graphNodeLess(result.Nodes[i], result.Nodes[j]) })
	return result
}

func graphNodeLess(left, right Node) bool {
	if left.Kind != right.Kind {
		return left.Kind < right.Kind
	}
	leftLabel, rightLabel := strings.ToLower(left.Label), strings.ToLower(right.Label)
	if leftLabel != rightLabel {
		return leftLabel < rightLabel
	}
	return left.ID < right.ID
}

func emptyGraph() Graph {
	return Graph{Nodes: []Node{}, Edges: []Edge{}}
}
