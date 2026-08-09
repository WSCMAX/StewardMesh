package directoryexpansion

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSailPointConnectorPullsReadOnlyRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte("[{\"provider\":\"sailpoint\",\"sourceId\":\"1\",\"kind\":\"person\",\"displayName\":\"Demo\"}]"))
	}))
	defer server.Close()
	records, err := NewSailPointConnector(server.URL, "token", server.Client()).Pull(context.Background())
	if err != nil || len(records) != 1 || records[0].Provider != ProviderSailPoint {
		t.Fatalf("unexpected SailPoint response: %#v %v", records, err)
	}
}

func TestSyntheticSeederRequiresExplicitEnablement(t *testing.T) {
	graph, err := (SyntheticSeeder{}).Seed(context.Background())
	if err != nil || len(graph.Nodes) != 0 {
		t.Fatalf("synthetic data enabled unexpectedly")
	}
	graph, err = (SyntheticSeeder{Enabled: true}).Seed(context.Background())
	if err != nil || len(graph.Nodes) == 0 {
		t.Fatalf("synthetic data was not seeded")
	}
}

func TestGraphFiltersNodesAndEdges(t *testing.T) {
	store := NewMemoryGraph(Graph{
		Nodes: []Node{{ID: "a", Kind: "person", Label: "Alice"}, {ID: "b", Kind: "group", Label: "Staff"}},
		Edges: []Edge{{ID: "e", From: "a", To: "b", Kind: "member_of"}},
	})
	graph, err := store.Graph(context.Background(), GraphQuery{Search: "alice"})
	if err != nil || len(graph.Nodes) != 1 || len(graph.Edges) != 0 {
		t.Fatalf("unexpected filtered graph: %#v", graph)
	}
}
