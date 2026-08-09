package directoryexpansion

import (
	"context"
	"sort"
	"strings"
)

type MemoryGraph struct{ graph Graph }

func NewMemoryGraph(graph Graph) *MemoryGraph { return &MemoryGraph{graph: graph} }
func (g *MemoryGraph) Graph(_ context.Context, q GraphQuery) (Graph, error) {
	limit := q.Limit
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	search, kind := strings.ToLower(strings.TrimSpace(q.Search)), strings.ToLower(strings.TrimSpace(q.Kind))
	nodes := make([]Node, 0, len(g.graph.Nodes))
	allowed := map[string]bool{}
	for _, n := range g.graph.Nodes {
		if kind != "" && strings.ToLower(n.Kind) != kind {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(n.Label), search) {
			continue
		}
		if len(nodes) >= limit {
			break
		}
		nodes = append(nodes, n)
		allowed[n.ID] = true
	}
	edges := make([]Edge, 0, len(g.graph.Edges))
	for _, e := range g.graph.Edges {
		if q.Relationship != "" && e.Kind != q.Relationship {
			continue
		}
		if allowed[e.From] && allowed[e.To] {
			edges = append(edges, e)
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	return Graph{Nodes: nodes, Edges: edges}, nil
}
