package directoryexpansion

// Occupancy edges from identity primary locations and typed location references.
// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

import (
	"context"
	"errors"
	"strings"

	"github.com/maxlemke/stewardmesh/internal/people"
)

const occupancyQueryChunk = 100

func occupancyRelationshipKind(kind string) RelationshipKind {
	switch RelationshipKind(strings.TrimSpace(kind)) {
	case RelationshipLocatedAt, RelationshipUsesOffice, RelationshipTeachesIn, RelationshipAttendsClass, RelationshipResidesIn, RelationshipUsesLab:
		return RelationshipKind(kind)
	default:
		return ""
	}
}

func (s *RelationshipGraphStore) projectOccupancy(ctx context.Context, builder *graphBuilder, visibility people.Visibility, organizationNodeID string, identities []people.Identity) error {
	if visibility.Empty() {
		return nil
	}
	identityIDs := make([]string, 0, len(identities))
	seenIdentities := make(map[string]struct{}, len(identities))
	for _, identity := range identities {
		if identity.Status != people.StatusActive || identity.ID == "" {
			continue
		}
		if _, seen := seenIdentities[identity.ID]; seen {
			continue
		}
		seenIdentities[identity.ID] = struct{}{}
		identityIDs = append(identityIDs, identity.ID)
		if err := s.ensureIdentityLocationNodes(ctx, builder, organizationNodeID, identity); err != nil {
			return err
		}
		from := typedNodeID(identityNodeKind(identity.Kind), identity.ID)
		s.addLocatedAt(builder, from, identity.BuildingID, identity.RoomID)
	}
	locationIDs := occupancyLocationIDs(builder)
	refs, err := s.listOccupancyReferences(ctx, visibility, identityIDs, locationIDs)
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		return nil
	}
	types, err := s.people.ListLocationReferenceTypes(ctx, s.organization.ID)
	if err != nil {
		return err
	}
	kindByType := make(map[string]RelationshipKind, len(types))
	for _, item := range types {
		if kind := occupancyRelationshipKind(item.RelationshipKind); kind != "" && item.Status == people.StatusActive {
			kindByType[item.ID] = kind
		}
	}
	for _, ref := range refs {
		if ref.Status != people.StatusActive {
			continue
		}
		kind, ok := kindByType[ref.TypeID]
		if !ok {
			continue
		}
		identity, err := s.people.GetIdentity(ctx, s.organization.ID, ref.IdentityID)
		if err != nil {
			if errors.Is(err, people.ErrNotFound) {
				continue
			}
			return err
		}
		if identity.Status != people.StatusActive {
			continue
		}
		s.projectIdentityRecords(builder, organizationNodeID, []people.Identity{identity})
		if err := s.ensureIdentityLocationNodes(ctx, builder, organizationNodeID, identity); err != nil {
			return err
		}
		if err := s.ensureOccupancyLocationNode(ctx, builder, organizationNodeID, ref.LocationKind, ref.LocationID); err != nil {
			return err
		}
		from := typedNodeID(identityNodeKind(identity.Kind), identity.ID)
		to := occupancyNodeID(ref.LocationKind, ref.LocationID)
		builder.addEdgeWithID(ref.ID, kind, from, to, map[string]string{
			"priority": string(ref.Priority),
			"origin":   "local",
		})
	}
	return nil
}

func (s *RelationshipGraphStore) listOccupancyReferences(ctx context.Context, visibility people.Visibility, identityIDs, locationIDs []string) ([]people.LocationReference, error) {
	seen := make(map[string]struct{})
	result := make([]people.LocationReference, 0)
	appendUnique := func(items []people.LocationReference) {
		for _, item := range items {
			if _, exists := seen[item.ID]; exists {
				continue
			}
			seen[item.ID] = struct{}{}
			result = append(result, item)
		}
	}
	for _, chunk := range chunkGraphIDs(identityIDs, occupancyQueryChunk) {
		items, err := s.people.ListLocationReferences(ctx, s.organization.ID, people.LocationReferenceQuery{
			IdentityIDs: chunk, Status: people.StatusActive, Limit: 500,
		}, visibility)
		if err != nil {
			return nil, err
		}
		appendUnique(items)
	}
	for _, chunk := range chunkGraphIDs(locationIDs, occupancyQueryChunk) {
		items, err := s.people.ListLocationReferences(ctx, s.organization.ID, people.LocationReferenceQuery{
			LocationIDs: chunk, Status: people.StatusActive, Limit: 500,
		}, visibility)
		if err != nil {
			return nil, err
		}
		appendUnique(items)
	}
	return result, nil
}

func (s *RelationshipGraphStore) ensureIdentityLocationNodes(ctx context.Context, builder *graphBuilder, organizationNodeID string, identity people.Identity) error {
	if identity.RoomID != "" {
		return s.ensureOccupancyLocationNode(ctx, builder, organizationNodeID, people.LocationKindRoom, identity.RoomID)
	}
	if identity.BuildingID != "" {
		return s.ensureOccupancyLocationNode(ctx, builder, organizationNodeID, people.LocationKindBuilding, identity.BuildingID)
	}
	return nil
}

func (s *RelationshipGraphStore) ensureOccupancyLocationNode(ctx context.Context, builder *graphBuilder, organizationNodeID string, kind people.LocationKind, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	switch kind {
	case people.LocationKindSite:
		if builder.hasNode(typedNodeID(NodeSite, id)) {
			return nil
		}
		site, err := s.people.GetSite(ctx, s.organization.ID, id)
		if err != nil {
			if errors.Is(err, people.ErrNotFound) {
				return nil
			}
			return err
		}
		s.projectLocationRecords(builder, organizationNodeID, people.GraphLocations{Sites: []people.Site{site}})
	case people.LocationKindBuilding:
		if builder.hasNode(typedNodeID(NodeBuilding, id)) {
			return nil
		}
		building, err := s.people.GetBuilding(ctx, s.organization.ID, id)
		if err != nil {
			if errors.Is(err, people.ErrNotFound) {
				return nil
			}
			return err
		}
		s.projectLocationRecords(builder, organizationNodeID, people.GraphLocations{Buildings: []people.Building{building}})
	case people.LocationKindRoom:
		if builder.hasNode(typedNodeID(NodeRoom, id)) {
			return nil
		}
		room, err := s.people.GetRoom(ctx, s.organization.ID, id)
		if err != nil {
			if errors.Is(err, people.ErrNotFound) {
				return nil
			}
			return err
		}
		s.projectLocationRecords(builder, organizationNodeID, people.GraphLocations{Rooms: []people.Room{room}})
	}
	return nil
}

func (s *RelationshipGraphStore) addLocatedAt(builder *graphBuilder, from, buildingID, roomID string) {
	if target := typedNodeID(NodeBuilding, buildingID); buildingID != "" && builder.hasNode(target) {
		builder.addEdge(RelationshipLocatedAt, from, target, nil)
	}
	if target := typedNodeID(NodeRoom, roomID); roomID != "" && builder.hasNode(target) {
		builder.addEdge(RelationshipLocatedAt, from, target, nil)
	}
}

func occupancyNodeID(kind people.LocationKind, id string) string {
	switch kind {
	case people.LocationKindSite:
		return typedNodeID(NodeSite, id)
	case people.LocationKindBuilding:
		return typedNodeID(NodeBuilding, id)
	default:
		return typedNodeID(NodeRoom, id)
	}
}

func occupancyLocationIDs(builder *graphBuilder) []string {
	ids := make([]string, 0)
	seen := make(map[string]struct{})
	for _, node := range builder.nodes {
		if node.Kind != NodeSite && node.Kind != NodeBuilding && node.Kind != NodeRoom {
			continue
		}
		_, rawID, ok := strings.Cut(node.ID, ":")
		if !ok || rawID == "" {
			continue
		}
		if _, exists := seen[rawID]; exists {
			continue
		}
		seen[rawID] = struct{}{}
		ids = append(ids, rawID)
	}
	return ids
}

func chunkGraphIDs(ids []string, size int) [][]string {
	if size < 1 || len(ids) == 0 {
		return nil
	}
	chunks := make([][]string, 0, (len(ids)+size-1)/size)
	for start := 0; start < len(ids); start += size {
		end := start + size
		if end > len(ids) {
			end = len(ids)
		}
		chunks = append(chunks, ids[start:end])
	}
	return chunks
}
