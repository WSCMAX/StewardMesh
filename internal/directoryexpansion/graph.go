package directoryexpansion

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

import (
	"context"
)

type MemoryGraph struct{ graph Graph }

func NewMemoryGraph(graph Graph) *MemoryGraph { return &MemoryGraph{graph: graph} }
func (g *MemoryGraph) Graph(_ context.Context, q GraphQuery) (Graph, error) {
	query, err := normalizeGraphQuery(q, true)
	if err != nil {
		return Graph{}, err
	}
	// MemoryGraph has no authoritative per-node site or department metadata.
	// It therefore supports organization-wide reads only instead of guessing at
	// scoped visibility and becoming a data-discovery oracle when injected into
	// a transport.
	if !query.Scope.Directory.All {
		return emptyGraph(), nil
	}
	graph := g.graph
	if !query.Scope.Assets.All {
		allowedAssets := make(map[string]struct{}, len(query.Scope.Assets.ResourceIDs))
		for _, resourceID := range query.Scope.Assets.ResourceIDs {
			allowedAssets[typedNodeID(NodeAsset, resourceID)] = struct{}{}
		}
		visibleNodes := make([]Node, 0, len(graph.Nodes))
		visibleIDs := make(map[string]struct{}, len(graph.Nodes))
		for _, node := range graph.Nodes {
			if node.Kind == NodeAsset {
				if _, allowed := allowedAssets[node.ID]; !allowed {
					continue
				}
			}
			visibleNodes = append(visibleNodes, node)
			visibleIDs[node.ID] = struct{}{}
		}
		visibleEdges := make([]Edge, 0, len(graph.Edges))
		for _, edge := range graph.Edges {
			_, fromVisible := visibleIDs[edge.From]
			_, toVisible := visibleIDs[edge.To]
			if fromVisible && toVisible {
				visibleEdges = append(visibleEdges, edge)
			}
		}
		graph = Graph{Nodes: visibleNodes, Edges: visibleEdges}
	}
	return filterGraph(graph, query), nil
}
