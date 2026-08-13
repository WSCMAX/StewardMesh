package directoryexpansion

// Requirements: REQ-DIRECTORY-EXPANSION-004, REQ-DIRECTORY-EXPANSION-005. Features: identity.directory, integrations.protocols.

import (
	"context"
	"errors"
	"sort"
	"strings"
)

// ManagedGraphStore projects active normalized directory-provider targets into the
// existing relationship-graph contract. It is bound to one organization so a
// transport cannot accidentally widen scope through query input.
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
	groups, err := s.store.ListManagedGroups(ctx, s.organizationID)
	if err != nil {
		return Graph{}, err
	}
	memberships, err := s.store.ListManagedMemberships(ctx, s.organizationID)
	if err != nil {
		return Graph{}, err
	}
	activeGroups := make(map[string]ManagedGroup, len(groups))
	nodesByID := make(map[string]Node, len(groups)+len(memberships))
	for _, group := range groups {
		if group.Status != "active" {
			continue
		}
		activeGroups[group.ID] = group
		attributes := cloneMetadata(group.Metadata)
		if attributes == nil {
			attributes = map[string]string{}
		}
		attributes["groupName"] = group.Name
		if group.Description != "" {
			attributes["description"] = group.Description
		}
		nodesByID[group.ID] = Node{ID: group.ID, Kind: "group", Label: group.DisplayName, Attributes: attributes}
	}
	edges := make([]Edge, 0, len(memberships))
	for _, membership := range memberships {
		if membership.Status != "active" {
			continue
		}
		if _, present := activeGroups[membership.GroupID]; !present {
			continue
		}
		memberKind := "subject"
		if membership.MemberKind == MemberGroup {
			memberKind = "group"
			if _, present := activeGroups[membership.MemberID]; !present {
				continue
			}
		} else if _, present := nodesByID[membership.MemberID]; !present {
			nodesByID[membership.MemberID] = Node{ID: membership.MemberID, Kind: memberKind, Label: membership.MemberDisplayName}
		}
		edges = append(edges, Edge{ID: membership.ID, From: membership.MemberID, To: membership.GroupID,
			Kind: "member_of", Attributes: cloneMetadata(membership.Metadata)})
	}
	nodes := make([]Node, 0, len(nodesByID))
	for _, node := range nodesByID {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	return filterGraph(Graph{Nodes: nodes, Edges: edges}, query), nil
}

func filterGraph(graph Graph, query GraphQuery) Graph {
	limit := query.Limit
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	search := strings.ToLower(strings.TrimSpace(query.Search))
	kind := strings.ToLower(strings.TrimSpace(query.Kind))
	relationship := strings.ToLower(strings.TrimSpace(query.Relationship))
	result := Graph{Nodes: make([]Node, 0, min(limit, len(graph.Nodes))), Edges: []Edge{}}
	allowed := make(map[string]struct{}, len(graph.Nodes))
	for _, node := range graph.Nodes {
		if kind != "" && strings.ToLower(node.Kind) != kind {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(node.Label), search) {
			continue
		}
		if len(result.Nodes) == limit {
			break
		}
		result.Nodes = append(result.Nodes, node)
		allowed[node.ID] = struct{}{}
	}
	for _, edge := range graph.Edges {
		if relationship != "" && strings.ToLower(edge.Kind) != relationship {
			continue
		}
		_, fromAllowed := allowed[edge.From]
		_, toAllowed := allowed[edge.To]
		if fromAllowed && toAllowed {
			result.Edges = append(result.Edges, edge)
		}
	}
	return result
}
