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
	ListGraphAssets(context.Context, atlas.GraphAssetQuery) ([]domain.Asset, error)
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
	if err := s.projectAnchors(ctx, builder, "", query); err != nil {
		return Graph{}, err
	}
	anchors := graphAnchorNodes(builder.graph().Nodes, query)
	if err := s.projectRelationshipContext(ctx, builder, "", query, anchors); err != nil {
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
	organizationNodeID := typedNodeID(NodeOrganization, s.organization.ID)
	builder.addNode(Node{ID: organizationNodeID, Kind: NodeOrganization, Label: s.organization.Name})

	if locationQuery := graphAnchorLocationQuery(query); locationQuery != nil {
		if _, err := s.projectLocations(ctx, builder, query.Scope.Directory, organizationNodeID, *locationQuery); err != nil {
			return Graph{}, err
		}
	}
	if query.Scope.Directory.All {
		managed := &ManagedGraphStore{store: s.groups, organizationID: s.organization.ID}
		if err := managed.projectAnchors(ctx, builder, organizationNodeID, query); err != nil {
			return Graph{}, err
		}
	}

	var anchorIdentities []people.Identity
	if identityQuery := graphAnchorIdentityQuery(query); identityQuery != nil {
		anchorIdentities, err = s.projectIdentities(ctx, builder, query.Scope.Directory, organizationNodeID, *identityQuery)
		if err != nil {
			return Graph{}, err
		}
	}
	var anchorAssets []domain.Asset
	if !query.Scope.Assets.Empty() {
		if assetQuery := graphAnchorAssetQuery(query); assetQuery != nil {
			assetQuery.Visibility = graphAssetVisibility(query.Scope.Assets)
			anchorAssets, err = s.projectAssets(ctx, builder, query.Scope, organizationNodeID, *assetQuery)
			if err != nil {
				return Graph{}, err
			}
		}
	}
	anchors := graphAnchorNodes(builder.graph().Nodes, query)
	identityContext, assetContext, locationContext := graphRelationshipContext(query, anchors, anchorIdentities, anchorAssets)
	if locationContext != nil {
		if _, err := s.projectLocations(ctx, builder, query.Scope.Directory, organizationNodeID, *locationContext); err != nil {
			return Graph{}, err
		}
	}
	var contextIdentities []people.Identity
	if identityContext != nil {
		contextIdentities, err = s.projectIdentities(ctx, builder, query.Scope.Directory, organizationNodeID, *identityContext)
		if err != nil {
			return Graph{}, err
		}
	}
	var contextAssets []domain.Asset
	if assetContext != nil && !query.Scope.Assets.Empty() {
		assetContext.Visibility = graphAssetVisibility(query.Scope.Assets)
		contextAssets, err = s.projectAssets(ctx, builder, query.Scope, organizationNodeID, *assetContext)
		if err != nil {
			return Graph{}, err
		}
	}
	s.projectIdentityRecords(builder, organizationNodeID, append(anchorIdentities, contextIdentities...))
	if err := s.projectOccupancy(ctx, builder, query.Scope.Directory, organizationNodeID, append(anchorIdentities, contextIdentities...)); err != nil {
		return Graph{}, err
	}
	s.projectAssetRecords(builder, query.Scope, organizationNodeID, append(anchorAssets, contextAssets...))
	if query.Scope.Directory.All {
		managed := &ManagedGraphStore{store: s.groups, organizationID: s.organization.ID}
		if err := managed.projectRelationshipContext(ctx, builder, organizationNodeID, query, anchors); err != nil {
			return Graph{}, err
		}
	}
	return filterGraph(builder.graph(), query), nil
}

func graphAnchorIdentityQuery(query GraphQuery) *people.GraphIdentityQuery {
	kind, identityKind := graphIdentityKind(query.Kind)
	if query.Kind != "" && !identityKind {
		return nil
	}
	result := &people.GraphIdentityQuery{LabelSearch: query.Search, Limit: query.Limit}
	if identityKind {
		result.Kind = kind
	}
	return result
}

func graphAnchorAssetQuery(query GraphQuery) *atlas.GraphAssetQuery {
	if query.Kind != "" && query.Kind != NodeAsset {
		return nil
	}
	return &atlas.GraphAssetQuery{LabelSearch: query.Search, Limit: query.Limit}
}

func graphAnchorLocationQuery(query GraphQuery) *people.GraphLocationQuery {
	result := &people.GraphLocationQuery{LabelSearch: query.Search, Limit: query.Limit}
	switch query.Kind {
	case "":
		return result
	case NodeSite:
		result.Kind = people.GraphLocationSite
	case NodeBuilding:
		result.Kind = people.GraphLocationBuilding
	case NodeRoom:
		result.Kind = people.GraphLocationRoom
	case NodeDepartment:
		result.Kind = people.GraphLocationDepartment
	default:
		return nil
	}
	return result
}

func graphAssetVisibility(visibility AssetVisibility) atlas.GraphAssetVisibility {
	return atlas.GraphAssetVisibility{All: visibility.All, ResourceIDs: visibility.ResourceIDs, SiteIDs: visibility.SiteIDs, DepartmentIDs: visibility.DepartmentIDs}
}

func graphAnchorNodes(nodes []Node, query GraphQuery) []Node {
	ordered := append([]Node(nil), nodes...)
	sort.Slice(ordered, func(i, j int) bool { return graphNodeLess(ordered[i], ordered[j]) })
	anchors := make([]Node, 0, len(ordered))
	search := strings.ToLower(query.Search)
	for _, node := range ordered {
		if query.Kind != "" && node.Kind != query.Kind || search != "" && !strings.Contains(strings.ToLower(node.Label), search) {
			continue
		}
		anchors = append(anchors, node)
		if len(anchors) == query.Limit {
			break
		}
	}
	return anchors
}

func graphRelationshipContext(query GraphQuery, anchors []Node, anchorIdentities []people.Identity, anchorAssets []domain.Asset) (*people.GraphIdentityQuery, *atlas.GraphAssetQuery, *people.GraphLocationQuery) {
	if query.Relationship == "" {
		return nil, nil, nil
	}
	identities := &people.GraphIdentityQuery{Limit: query.Limit}
	assets := &atlas.GraphAssetQuery{Limit: query.Limit}
	locations := &people.GraphLocationQuery{Limit: query.Limit}
	identitySelectorCount := 0
	assetSelectorCount := 0
	locationSelectorCount := 0
	appendSelector := func(values *[]string, value string, count *int) {
		if value == "" || *count == query.Limit {
			return
		}
		for _, existing := range *values {
			if existing == value {
				return
			}
		}
		*values = append(*values, value)
		*count++
	}
	for _, node := range anchors {
		_, rawID, _ := strings.Cut(node.ID, ":")
		switch query.Relationship {
		case RelationshipBelongsTo:
			if node.Kind == NodeDepartment {
				appendSelector(&identities.DepartmentIDs, rawID, &identitySelectorCount)
				appendSelector(&assets.References.DepartmentIDs, rawID, &assetSelectorCount)
			}
		case RelationshipLocatedAt:
			switch node.Kind {
			case NodeSite:
				appendSelector(&identities.SiteIDs, rawID, &identitySelectorCount)
				appendSelector(&assets.References.SiteIDs, rawID, &assetSelectorCount)
			case NodeBuilding:
				appendSelector(&assets.References.BuildingIDs, rawID, &assetSelectorCount)
			case NodeRoom:
				appendSelector(&assets.References.RoomIDs, rawID, &assetSelectorCount)
			}
		case RelationshipAssignedTo:
			if isIdentityNodeKind(node.Kind) {
				appendSelector(&assets.References.UserIDs, rawID, &assetSelectorCount)
			}
		case RelationshipContains:
			switch node.Kind {
			case NodeOrganization:
				identities.DirectOrganizationChildren = true
				assets.DirectOrganizationChildren = true
				locations.DirectOrganizationChildren = true
			case NodeSite:
				appendSelector(&locations.ParentSiteIDs, rawID, &locationSelectorCount)
			case NodeBuilding:
				appendSelector(&locations.ParentBuildingIDs, rawID, &locationSelectorCount)
			}
		}
	}
	for _, identity := range anchorIdentities {
		switch query.Relationship {
		case RelationshipBelongsTo:
			appendSelector(&locations.DepartmentIDs, identity.DepartmentID, &locationSelectorCount)
		case RelationshipLocatedAt:
			appendSelector(&locations.SiteIDs, identity.SiteID, &locationSelectorCount)
			appendSelector(&locations.BuildingIDs, identity.BuildingID, &locationSelectorCount)
			appendSelector(&locations.RoomIDs, identity.RoomID, &locationSelectorCount)
		}
	}
	for _, asset := range anchorAssets {
		switch query.Relationship {
		case RelationshipBelongsTo:
			appendSelector(&locations.DepartmentIDs, asset.DepartmentID, &locationSelectorCount)
		case RelationshipLocatedAt:
			appendSelector(&locations.SiteIDs, asset.SiteID, &locationSelectorCount)
			appendSelector(&locations.BuildingIDs, asset.BuildingID, &locationSelectorCount)
			appendSelector(&locations.RoomIDs, asset.RoomID, &locationSelectorCount)
		case RelationshipAssignedTo:
			appendSelector(&identities.IdentityIDs, asset.UserID, &identitySelectorCount)
		}
	}
	if !hasGraphIdentitySelectors(*identities) && !identities.DirectOrganizationChildren {
		identities = nil
	}
	if assets.References.Empty() && !assets.DirectOrganizationChildren {
		assets = nil
	}
	if !hasGraphLocationSelectors(*locations) && !locations.DirectOrganizationChildren {
		locations = nil
	}
	return identities, assets, locations
}

func isIdentityNodeKind(kind NodeKind) bool {
	_, ok := graphIdentityKind(kind)
	return ok
}

func hasGraphIdentitySelectors(query people.GraphIdentityQuery) bool {
	return len(query.IdentityIDs)+len(query.DepartmentIDs)+len(query.SiteIDs) > 0
}

func hasGraphLocationSelectors(query people.GraphLocationQuery) bool {
	return len(query.SiteIDs)+len(query.BuildingIDs)+len(query.RoomIDs)+len(query.DepartmentIDs)+
		len(query.ParentSiteIDs)+len(query.ParentBuildingIDs) > 0
}

func graphHasKind(nodes []Node, kind NodeKind) bool {
	for _, node := range nodes {
		if node.Kind == kind {
			return true
		}
	}
	return false
}

func (s *RelationshipGraphStore) projectLocations(ctx context.Context, builder *graphBuilder, visibility people.Visibility, organizationNodeID string, graphQuery people.GraphLocationQuery) (people.GraphLocations, error) {
	locations, err := s.people.ListGraphLocations(ctx, s.organization.ID, graphQuery, visibility)
	if err != nil {
		return people.GraphLocations{}, err
	}
	s.projectLocationRecords(builder, organizationNodeID, locations)
	return locations, nil
}

func (s *RelationshipGraphStore) projectLocationRecords(builder *graphBuilder, organizationNodeID string, locations people.GraphLocations) {
	for _, site := range locations.Sites {
		if site.Status != people.StatusActive {
			continue
		}
		id := typedNodeID(NodeSite, site.ID)
		builder.addNode(Node{ID: id, Kind: NodeSite, Label: site.Name, Attributes: statusAttributes(site.Status, "local")})
		builder.addEdge(RelationshipContains, organizationNodeID, id, nil)
	}
	for _, building := range locations.Buildings {
		if building.Status != people.StatusActive {
			continue
		}
		parentID := typedNodeID(NodeSite, building.SiteID)
		id := typedNodeID(NodeBuilding, building.ID)
		builder.addNode(Node{ID: id, Kind: NodeBuilding, Label: building.Name, Attributes: statusAttributes(building.Status, "local")})
		if builder.hasNode(parentID) {
			builder.addEdge(RelationshipContains, parentID, id, nil)
		}
	}
	for _, room := range locations.Rooms {
		if room.Status != people.StatusActive {
			continue
		}
		parentID := typedNodeID(NodeBuilding, room.BuildingID)
		label := "Room " + room.Number
		if room.Name != "" {
			label += " · " + room.Name
		}
		id := typedNodeID(NodeRoom, room.ID)
		builder.addNode(Node{ID: id, Kind: NodeRoom, Label: label, Attributes: statusAttributes(room.Status, "local")})
		if builder.hasNode(parentID) {
			builder.addEdge(RelationshipContains, parentID, id, nil)
		}
	}
	for _, department := range locations.Departments {
		if department.Status != people.StatusActive {
			continue
		}
		id := typedNodeID(NodeDepartment, department.ID)
		builder.addNode(Node{ID: id, Kind: NodeDepartment, Label: department.Name, Attributes: statusAttributes(department.Status, "local")})
		if department.SiteID == "" {
			builder.addEdge(RelationshipContains, organizationNodeID, id, nil)
		} else if candidate := typedNodeID(NodeSite, department.SiteID); builder.hasNode(candidate) {
			builder.addEdge(RelationshipContains, candidate, id, nil)
		}
	}
}

func (s *RelationshipGraphStore) projectIdentities(ctx context.Context, builder *graphBuilder, visibility people.Visibility, organizationNodeID string, graphQuery people.GraphIdentityQuery) ([]people.Identity, error) {
	identities, err := s.people.ListGraphIdentities(ctx, s.organization.ID, graphQuery, visibility)
	if err != nil {
		return nil, err
	}
	s.projectIdentityRecords(builder, organizationNodeID, identities)
	return identities, nil
}

func (s *RelationshipGraphStore) projectIdentityRecords(builder *graphBuilder, organizationNodeID string, identities []people.Identity) {
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
		related := identity.DepartmentID != "" || identity.SiteID != "" || identity.BuildingID != "" || identity.RoomID != ""
		if target := typedNodeID(NodeDepartment, identity.DepartmentID); identity.DepartmentID != "" && builder.hasNode(target) {
			builder.addEdge(RelationshipBelongsTo, id, target, nil)
		}
		if target := typedNodeID(NodeSite, identity.SiteID); identity.SiteID != "" && builder.hasNode(target) {
			builder.addEdge(RelationshipLocatedAt, id, target, nil)
		}
		if target := typedNodeID(NodeBuilding, identity.BuildingID); identity.BuildingID != "" && builder.hasNode(target) {
			builder.addEdge(RelationshipLocatedAt, id, target, nil)
		}
		if target := typedNodeID(NodeRoom, identity.RoomID); identity.RoomID != "" && builder.hasNode(target) {
			builder.addEdge(RelationshipLocatedAt, id, target, nil)
		}
		if !related {
			builder.addEdge(RelationshipContains, organizationNodeID, id, nil)
		}
	}
}

func (s *ManagedGraphStore) projectAnchors(ctx context.Context, builder *graphBuilder, organizationNodeID string, query GraphQuery) error {
	groups := make([]ManagedGroup, 0)
	memberships := make([]ManagedMembership, 0)
	var err error
	if query.Kind == "" || query.Kind == NodeGroup {
		groups, err = s.store.ListGraphManagedGroups(ctx, s.organizationID, ManagedGroupGraphQuery{LabelSearch: query.Search, Limit: query.Limit})
		if err != nil {
			return err
		}
	}
	if query.Kind == "" || query.Kind == NodeSubject {
		memberships, err = s.store.ListGraphManagedMemberships(ctx, s.organizationID, ManagedMembershipGraphQuery{LabelSearch: query.Search, Limit: query.Limit})
		if err != nil {
			return err
		}
		membershipGroups, err := s.listMembershipGroups(ctx, memberships, query.Limit)
		if err != nil {
			return err
		}
		groups = mergeManagedGroups(groups, membershipGroups)
	}
	s.projectManagedRecords(builder, organizationNodeID, groups, memberships)
	return nil
}

func (s *ManagedGraphStore) projectRelationshipContext(ctx context.Context, builder *graphBuilder, organizationNodeID string, query GraphQuery, anchors []Node) error {
	if query.Relationship == RelationshipContains && graphHasKind(anchors, NodeOrganization) {
		groups, err := s.store.ListGraphManagedGroups(ctx, s.organizationID, ManagedGroupGraphQuery{Limit: query.Limit})
		if err != nil {
			return err
		}
		s.projectManagedRecords(builder, organizationNodeID, groups, nil)
		return nil
	}
	if query.Relationship != RelationshipMemberOf {
		return nil
	}
	groupIDs := make([]string, 0)
	memberIDs := make([]string, 0)
	for _, node := range anchors {
		_, rawID, _ := strings.Cut(node.ID, ":")
		switch node.Kind {
		case NodeGroup:
			appendUniqueGraphID(&groupIDs, rawID, query.Limit)
			appendUniqueGraphID(&memberIDs, rawID, query.Limit)
		case NodeSubject:
			appendUniqueGraphID(&memberIDs, rawID, query.Limit)
		}
	}
	membershipSets := make([][]ManagedMembership, 0, 2)
	if len(groupIDs) > 0 {
		items, err := s.store.ListGraphManagedMemberships(ctx, s.organizationID, ManagedMembershipGraphQuery{GroupIDs: groupIDs, Limit: query.Limit})
		if err != nil {
			return err
		}
		membershipSets = append(membershipSets, items)
	}
	if len(memberIDs) > 0 {
		items, err := s.store.ListGraphManagedMemberships(ctx, s.organizationID, ManagedMembershipGraphQuery{MemberIDs: memberIDs, Limit: query.Limit})
		if err != nil {
			return err
		}
		membershipSets = append(membershipSets, items)
	}
	memberships := mergeManagedMemberships(membershipSets...)
	if len(memberships) > query.Limit {
		memberships = memberships[:query.Limit]
	}
	groups, err := s.listMembershipGroups(ctx, memberships, query.Limit)
	if err != nil {
		return err
	}
	s.projectManagedRecords(builder, organizationNodeID, groups, memberships)
	return nil
}

func (s *ManagedGraphStore) listMembershipGroups(ctx context.Context, memberships []ManagedMembership, limit int) ([]ManagedGroup, error) {
	ids := make([]string, 0, boundedMembershipIDCapacity(len(memberships), limit))
	for _, membership := range memberships {
		appendUniqueGraphID(&ids, membership.GroupID, limit)
		if membership.MemberKind == MemberGroup {
			appendUniqueGraphID(&ids, membership.MemberID, limit)
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return []ManagedGroup{}, nil
	}
	groups, err := s.store.ListGraphManagedGroups(ctx, s.organizationID, ManagedGroupGraphQuery{GroupIDs: ids, Limit: limit})
	if err != nil {
		return nil, err
	}
	return mergeManagedGroups(groups), nil
}

func (s *ManagedGraphStore) projectManagedRecords(builder *graphBuilder, organizationNodeID string, groups []ManagedGroup, memberships []ManagedMembership) {
	for _, group := range groups {
		if group.Status != "active" {
			continue
		}
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
		to := typedNodeID(NodeGroup, membership.GroupID)
		if !builder.hasNode(to) {
			continue
		}
		from := ""
		if membership.MemberKind == MemberGroup {
			from = typedNodeID(NodeGroup, membership.MemberID)
			if !builder.hasNode(from) {
				continue
			}
		} else {
			from = typedNodeID(NodeSubject, membership.MemberID)
			builder.addNode(Node{ID: from, Kind: NodeSubject, Label: membership.MemberDisplayName, Attributes: statusAttributes(membership.Status, "imported")})
		}
		builder.addEdgeWithID(membership.ID, RelationshipMemberOf, from, to, map[string]string{"origin": "imported"})
	}
}

func boundedGraphCapacity(limit int) int {
	if limit < 1 {
		return 1
	}
	if limit > MaximumGraphLimit {
		return MaximumGraphLimit
	}
	return limit
}

func boundedMembershipIDCapacity(membershipCount, limit int) int {
	if membershipCount > MaximumGraphLimit {
		membershipCount = MaximumGraphLimit
	}
	return min(membershipCount*2, boundedGraphCapacity(limit))
}

func appendUniqueGraphID(values *[]string, value string, limit int) {
	if value == "" || len(*values) == limit {
		return
	}
	for _, existing := range *values {
		if existing == value {
			return
		}
	}
	*values = append(*values, value)
}

func mergeManagedGroups(sets ...[]ManagedGroup) []ManagedGroup {
	byID := make(map[string]ManagedGroup)
	for _, set := range sets {
		for _, group := range set {
			byID[group.ID] = group
		}
	}
	result := make([]ManagedGroup, 0, len(byID))
	for _, group := range byID {
		result = append(result, group)
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := strings.ToLower(result[i].DisplayName), strings.ToLower(result[j].DisplayName)
		if left == right {
			return result[i].ID < result[j].ID
		}
		return left < right
	})
	return result
}

func mergeManagedMemberships(sets ...[]ManagedMembership) []ManagedMembership {
	byID := make(map[string]ManagedMembership)
	for _, set := range sets {
		for _, membership := range set {
			byID[membership.ID] = membership
		}
	}
	result := make([]ManagedMembership, 0, len(byID))
	for _, membership := range byID {
		result = append(result, membership)
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := strings.ToLower(result[i].MemberDisplayName), strings.ToLower(result[j].MemberDisplayName)
		if left == right {
			return result[i].ID < result[j].ID
		}
		return left < right
	})
	return result
}

func (s *RelationshipGraphStore) projectAssets(ctx context.Context, builder *graphBuilder, scope GraphScope, organizationNodeID string, graphQuery atlas.GraphAssetQuery) ([]domain.Asset, error) {
	if scope.Directory.All {
		graphQuery.Directory = atlas.GraphAssetDirectoryVisibility{All: true}
	} else {
		graphQuery.Directory = atlas.GraphAssetDirectoryVisibility{
			SiteIDs: scope.Directory.SiteIDs, DepartmentIDs: scope.Directory.DepartmentIDs, MatchUserDirectory: true,
		}
	}
	assets, err := s.assets.ListGraphAssets(ctx, graphQuery)
	if err != nil {
		return nil, err
	}
	userIDs := make([]string, 0, len(assets))
	for _, asset := range assets {
		if asset.OrganizationID == s.organization.ID {
			appendUniqueGraphID(&userIDs, asset.UserID, graphQuery.Limit)
		}
	}
	sort.Strings(userIDs)
	if len(userIDs) > 0 {
		if _, err := s.projectIdentities(ctx, builder, scope.Directory, organizationNodeID, people.GraphIdentityQuery{
			IdentityIDs: userIDs,
			Limit:       graphQuery.Limit,
		}); err != nil {
			return nil, err
		}
	}
	s.projectAssetRecords(builder, scope, organizationNodeID, assets)
	return assets, nil
}

func (s *RelationshipGraphStore) projectAssetRecords(builder *graphBuilder, scope GraphScope, organizationNodeID string, assets []domain.Asset) {
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
		related := asset.SiteID != "" || asset.BuildingID != "" || asset.RoomID != "" || asset.DepartmentID != "" || asset.UserID != ""
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
			}
		}
		if target := typedNodeID(NodeDepartment, asset.DepartmentID); asset.DepartmentID != "" && builder.hasNode(target) {
			builder.addEdge(RelationshipBelongsTo, id, target, nil)
		}
		if asset.UserID != "" {
			for _, kind := range []NodeKind{NodePerson, NodeShared, NodePublic, NodeLab} {
				target := typedNodeID(kind, asset.UserID)
				if builder.hasNode(target) {
					builder.addEdge(RelationshipAssignedTo, id, target, nil)
					break
				}
			}
		}
		if !related {
			builder.addEdge(RelationshipContains, organizationNodeID, id, nil)
		}
	}
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
	if !validGraphSelectorValues(query.Scope.Directory.DepartmentIDs) || !validGraphSelectorValues(query.Scope.Directory.SiteIDs) ||
		!validGraphSelectorValues(query.Scope.Assets.ResourceIDs) || !validGraphSelectorValues(query.Scope.Assets.SiteIDs) ||
		!validGraphSelectorValues(query.Scope.Assets.DepartmentIDs) {
		return GraphQuery{}, ErrInvalidInput
	}
	query.Scope.Directory.DepartmentIDs = uniqueGraphValues(query.Scope.Directory.DepartmentIDs)
	query.Scope.Directory.SiteIDs = uniqueGraphValues(query.Scope.Directory.SiteIDs)
	query.Scope.Assets.ResourceIDs = uniqueGraphValues(query.Scope.Assets.ResourceIDs)
	query.Scope.Assets.SiteIDs = uniqueGraphValues(query.Scope.Assets.SiteIDs)
	query.Scope.Assets.DepartmentIDs = uniqueGraphValues(query.Scope.Assets.DepartmentIDs)
	if len(query.Scope.Directory.DepartmentIDs)+len(query.Scope.Directory.SiteIDs)+
		len(query.Scope.Assets.ResourceIDs)+len(query.Scope.Assets.SiteIDs)+len(query.Scope.Assets.DepartmentIDs) > MaximumGraphLimit {
		return GraphQuery{}, ErrInvalidInput
	}
	if requireScope && query.Scope.Directory.Empty() {
		return GraphQuery{}, ErrGraphScope
	}
	return query, nil
}

func validGraphSelectorValues(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 128 {
			return false
		}
		for _, character := range value {
			if unicode.IsControl(character) {
				return false
			}
		}
	}
	return true
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
	case "", RelationshipContains, RelationshipBelongsTo, RelationshipLocatedAt, RelationshipMemberOf, RelationshipAssignedTo,
		RelationshipUsesOffice, RelationshipTeachesIn, RelationshipAttendsClass, RelationshipResidesIn, RelationshipUsesLab:
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
	allowed := make(map[string]struct{}, boundedGraphCapacity(min(query.Limit, len(anchors))))
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
	selected := make(map[string]struct{}, boundedGraphCapacity(min(query.Limit, len(available))))
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
