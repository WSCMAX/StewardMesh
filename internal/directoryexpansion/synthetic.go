package directoryexpansion

import "context"

// SyntheticSeeder is deliberately opt-in. Applications must require the
// caller to explicitly enable it before invoking Seed.
type SyntheticSeeder struct{ Enabled bool }

func (s SyntheticSeeder) Seed(_ context.Context) (Graph, error) {
	if !s.Enabled {
		return Graph{}, nil
	}
	return Graph{Nodes: []Node{
		{ID: "synthetic-site", Kind: "site", Label: "Synthetic Demo Campus"},
		{ID: "synthetic-building", Kind: "building", Label: "Synthetic Science Hall"},
		{ID: "synthetic-room", Kind: "room", Label: "101"},
		{ID: "synthetic-person", Kind: "person", Label: "Demo Person", Attributes: map[string]string{"email": "demo@example.invalid"}},
		{ID: "synthetic-group", Kind: "group", Label: "Synthetic Researchers"},
	}, Edges: []Edge{
		{ID: "synthetic-site-building", From: "synthetic-site", To: "synthetic-building", Kind: "contains"},
		{ID: "synthetic-building-room", From: "synthetic-building", To: "synthetic-room", Kind: "contains"},
		{ID: "synthetic-person-group", From: "synthetic-person", To: "synthetic-group", Kind: "member_of"},
	}}, nil
}
