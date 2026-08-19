package mesh

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/directoryexpansion"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/horizon"
	"github.com/maxlemke/stewardmesh/internal/labels"
	"github.com/maxlemke/stewardmesh/internal/ledger"
	"github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/stack"
	"github.com/maxlemke/stewardmesh/internal/storage"
	"github.com/maxlemke/stewardmesh/internal/threads"
)

type fakeDirectory struct {
	graph directoryexpansion.Graph
	err   error
	last  directoryexpansion.GraphQuery
}

func (f *fakeDirectory) Graph(_ context.Context, query directoryexpansion.GraphQuery) (directoryexpansion.Graph, error) {
	f.last = query
	if f.err != nil {
		return directoryexpansion.Graph{}, f.err
	}
	return f.graph, nil
}

type fakeAtlas struct {
	models []domain.AssetModel
	assets []domain.Asset
}

func (f fakeAtlas) ListModels(context.Context, atlas.ModelQuery) ([]domain.AssetModel, error) {
	return f.models, nil
}
func (f fakeAtlas) ListGraphAssets(context.Context, atlas.GraphAssetQuery) ([]domain.Asset, error) {
	return f.assets, nil
}

type fakeLedger struct{ snapshot ledger.Snapshot }

func (f fakeLedger) Snapshot(context.Context) (ledger.Snapshot, error) { return f.snapshot, nil }

type fakeStack struct{ snapshot stack.Snapshot }

func (f fakeStack) Snapshot(context.Context) (stack.Snapshot, error) { return f.snapshot, nil }

type fakeLabels struct{ snapshot labels.Snapshot }

func (f fakeLabels) Snapshot(context.Context) (labels.Snapshot, error) { return f.snapshot, nil }

type fakeGoals struct{ snapshot threads.Snapshot }

func (f fakeGoals) Snapshot(context.Context) (threads.Snapshot, error) { return f.snapshot, nil }

type fakeVault struct{ blobs []storage.Blob }

func (f fakeVault) ListBlobs(context.Context) ([]storage.Blob, error) { return f.blobs, nil }

type fakeHorizon struct{ plans []horizon.Plan }

func (f fakeHorizon) ListPlans(context.Context, horizon.ListPlansQuery) ([]horizon.Plan, error) {
	return f.plans, nil
}

func testService(t *testing.T, deps Dependencies) *Service {
	t.Helper()
	deps.OrganizationID = "example-org"
	deps.Organization = "Example Organization"
	service, err := NewService(deps)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestGraphRequiresScopeAndRejectsUnknownFilters(t *testing.T) {
	t.Parallel()
	service := testService(t, Dependencies{})
	if _, err := service.Graph(context.Background(), Query{}); !errors.Is(err, ErrGraphScope) {
		t.Fatalf("expected scope error, got %v", err)
	}
	if _, err := service.Graph(context.Background(), Query{Kinds: []NodeKind{"unregistered"}, Scope: Scope{Finance: true}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid kind, got %v", err)
	}
	if _, err := service.Graph(context.Background(), Query{Limit: MaximumLimit + 1, Scope: Scope{Finance: true}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid limit, got %v", err)
	}
	if _, err := service.Graph(context.Background(), Query{Limit: 1000, Scope: Scope{Finance: true}}); err != nil {
		t.Fatalf("limit 1000 should be accepted, got %v", err)
	}
}

func TestGraphProjectsCrossProductLinksWithoutInventingUnauthorizedNodes(t *testing.T) {
	t.Parallel()
	directory := &fakeDirectory{graph: directoryexpansion.Graph{
		Nodes: []directoryexpansion.Node{
			{ID: "organization:example-org", Kind: NodeOrganization, Label: "Example Organization"},
			{ID: "person:ada", Kind: NodePerson, Label: "Ada Example"},
			{ID: "asset:laptop", Kind: NodeAsset, Label: "Research Laptop"},
		},
		Edges: []directoryexpansion.Edge{
			{ID: "assigned", From: "asset:laptop", To: "person:ada", Kind: RelationshipAssignedTo},
		},
	}}
	service := testService(t, Dependencies{
		Directory: directory,
		Atlas: fakeAtlas{
			models: []domain.AssetModel{{ID: "model-1", OrganizationID: "example-org", Manufacturer: "Lenovo", Name: "ThinkPad", Kind: "laptop", Status: "active"}},
			assets: []domain.Asset{{ID: "laptop", OrganizationID: "example-org", Name: "Research Laptop", ModelID: "model-1", Status: "active", Kind: "laptop"}},
		},
		Ledger: fakeLedger{snapshot: ledger.Snapshot{
			Vendors: []ledger.Vendor{{ID: "vendor-1", Name: "Campus Store", Status: "active"}},
			PurchaseOrders: []ledger.PurchaseOrder{{
				ID: "po-1", Number: "PO-2026-001", VendorID: "vendor-1", Status: "ordered",
				AssetIDs: []string{"laptop", "secret-asset"}, Lines: []ledger.PurchaseOrderLine{{LicenseID: "license-1"}},
			}},
			Contracts: []ledger.Contract{{ID: "contract-1", Name: "Hardware agreement", VendorID: "vendor-1", OperationalStatus: "active"}},
		}},
		Stack: fakeStack{snapshot: stack.Snapshot{
			Products:      []stack.Product{{ID: "office", Name: "Office Suite", Status: "active", Publisher: "ExampleSoft"}},
			Versions:      []stack.Version{{ID: "office-2026", ProductID: "office", Name: "2026", Status: "active"}},
			Licenses:      []stack.License{{ID: "license-1", ProductID: "office", Name: "Office seats", Status: "active", PurchaseOrderID: "po-1", VendorID: "vendor-1"}},
			Installations: []stack.Installation{{ID: "install-1", VersionID: "office-2026", AssetID: "laptop", Status: "installed"}},
			Assignments:   []stack.Assignment{{ID: "assign-1", LicenseID: "license-1", AssigneeKind: "identity", AssigneeID: "ada", Seats: 1}},
		}},
		Labels: fakeLabels{snapshot: labels.Snapshot{
			Definitions: []labels.Definition{{ID: "research", Name: "Research program", ValueKind: labels.ValueFlag, Status: labels.StatusActive, GoalID: "goal-1"}},
			Assignments: []labels.Assignment{
				{DefinitionID: "research", RecordType: "atlas.asset", RecordID: "laptop"},
				{DefinitionID: "research", RecordType: "atlas.asset", RecordID: "secret-asset"},
				{DefinitionID: "research", RecordType: "people.assignment", RecordID: "ada"},
			},
		}},
		Goals: fakeGoals{snapshot: threads.Snapshot{
			Goals:     []threads.Goal{{ID: "goal-1", Name: "Expand research computing"}},
			GoalLinks: []threads.GoalLink{{GoalID: "goal-1", TargetType: threads.TargetAsset, TargetID: "laptop"}},
		}},
		Vault:   fakeVault{blobs: []storage.Blob{{ID: "receipt-1", Name: "PO receipt.pdf", MediaType: "application/pdf"}}},
		Horizon: fakeHorizon{plans: []horizon.Plan{{ID: "plan-1", AssetID: "laptop", Scenario: "baseline", LifecycleStage: "in_use"}}},
	})

	graph, err := service.Graph(context.Background(), Query{Limit: 500, Scope: Scope{
		Directory: people.Visibility{All: true}, Assets: directoryexpansion.AssetVisibility{All: true},
		Finance: true, Software: true, Labels: true, Goals: true, Storage: true, Planning: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	nodes := nodeIDs(graph)
	if !nodes["person:ada"] || !nodes["asset:laptop"] || !nodes["purchase_order:po-1"] || !nodes["vendor:vendor-1"] ||
		!nodes["label:research"] || !nodes["goal:goal-1"] || !nodes["license:license-1"] || !nodes["model:model-1"] ||
		!nodes["plan:plan-1"] || !nodes["document:receipt-1"] {
		t.Fatalf("missing expected nodes: %#v", nodes)
	}
	if nodes["asset:secret-asset"] {
		t.Fatalf("unauthorized asset became a graph node: %#v", nodes)
	}
	edges := edgeKinds(graph)
	if !edges["asset:laptop|modeled_as|model:model-1"] || !edges["asset:laptop|purchased_via|purchase_order:po-1"] ||
		!edges["purchase_order:po-1|supplied_by|vendor:vendor-1"] || !edges["asset:laptop|tagged_with|label:research"] ||
		!edges["asset:laptop|advances|goal:goal-1"] || !edges["license:license-1|assigned_to|person:ada"] ||
		!edges["version:office-2026|installed_on|asset:laptop"] || !edges["plan:plan-1|planned_for|asset:laptop"] ||
		!edges["license:license-1|purchased_via|purchase_order:po-1"] {
		t.Fatalf("missing expected edges: %#v", edges)
	}
	if edges["asset:secret-asset|tagged_with|label:research"] || edges["asset:secret-asset|purchased_via|purchase_order:po-1"] {
		t.Fatalf("dropped record types or missing nodes still produced edges: %#v", edges)
	}
	if !containsAll(graph.Sources, SourcePeople, SourceAtlas, SourceLedger, SourceStack, SourceLabels, SourceGoals, SourceVault, SourceHorizon) {
		t.Fatalf("unexpected sources %#v", graph.Sources)
	}
}

func TestGraphKeepsSharedModelDepartmentAndOrganizationLinksWithinLimit(t *testing.T) {
	t.Parallel()
	assets := make([]domain.Asset, 0, 40)
	for index := 0; index < 40; index++ {
		id := "asset-" + strconv.Itoa(index)
		assets = append(assets, domain.Asset{
			ID: id, OrganizationID: "example-org", Name: "Laptop " + id,
			ModelID: "model-shared", DepartmentID: "dept-1", SiteID: "site-1", Status: "active", Kind: "laptop",
		})
	}
	assets[1].ModelID = ""
	assets[1].ModelContext = &domain.AssetModelContext{ModelNumber: "T14-G5"}
	service := testService(t, Dependencies{
		Directory: &fakeDirectory{graph: directoryexpansion.Graph{
			Nodes: []directoryexpansion.Node{
				{ID: "organization:example-org", Kind: NodeOrganization, Label: "Example Organization"},
				{ID: "department:dept-1", Kind: NodeDepartment, Label: "Technology"},
				{ID: "site:site-1", Kind: NodeSite, Label: "Main Campus"},
			},
		}},
		Atlas: fakeAtlas{
			models: []domain.AssetModel{{
				ID: "model-shared", OrganizationID: "example-org", Manufacturer: "Lenovo",
				Name: "ThinkPad T14", ModelNumber: "T14-G5", Kind: "laptop", Status: "active",
			}},
			assets: assets,
		},
	})
	graph, err := service.Graph(context.Background(), Query{Limit: 20, Scope: Scope{
		Directory: people.Visibility{All: true}, Assets: directoryexpansion.AssetVisibility{All: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	ids := nodeIDs(graph)
	if !ids["organization:example-org"] || !ids["department:dept-1"] || !ids["model:model-shared"] {
		t.Fatalf("expected shared hubs in the limited graph: %#v", ids)
	}
	edges := edgeKinds(graph)
	if !edges["asset:asset-0|modeled_as|model:model-shared"] || !edges["asset:asset-1|modeled_as|model:model-shared"] {
		t.Fatalf("expected shared model-number links: %#v", edges)
	}
	if !edges["asset:asset-0|belongs_to|department:dept-1"] || !edges["organization:example-org|contains|asset:asset-0"] {
		t.Fatalf("expected department and organization links: %#v", edges)
	}
	if !edges["asset:asset-0|located_at|site:site-1"] {
		t.Fatalf("expected site location link: %#v", edges)
	}
}

func TestGraphOmitsUnauthorizedProductsAndHonorsKindSelection(t *testing.T) {
	t.Parallel()
	service := testService(t, Dependencies{
		Directory: &fakeDirectory{graph: directoryexpansion.Graph{
			Nodes: []directoryexpansion.Node{{ID: "person:ada", Kind: NodePerson, Label: "Ada Example"}},
		}},
		Ledger: fakeLedger{snapshot: ledger.Snapshot{
			Vendors:        []ledger.Vendor{{ID: "vendor-1", Name: "Campus Store", Status: "active"}},
			PurchaseOrders: []ledger.PurchaseOrder{{ID: "po-1", Number: "PO-1", VendorID: "vendor-1", Status: "draft"}},
		}},
	})
	financeOnly, err := service.Graph(context.Background(), Query{Limit: 50, Scope: Scope{Finance: true}})
	if err != nil {
		t.Fatal(err)
	}
	ids := nodeIDs(financeOnly)
	if ids["person:ada"] || !ids["purchase_order:po-1"] || !ids["vendor:vendor-1"] {
		t.Fatalf("finance-only graph leaked directory or dropped POs: %#v", ids)
	}

	selected, err := service.Graph(context.Background(), Query{
		Kinds: []NodeKind{NodePurchaseOrder, NodeVendor}, Limit: 50,
		Scope: Scope{Directory: people.Visibility{All: true}, Finance: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	selectedIDs := nodeIDs(selected)
	if selectedIDs["person:ada"] || !selectedIDs["purchase_order:po-1"] || !selectedIDs["vendor:vendor-1"] {
		t.Fatalf("kind selection did not restrict the graph: %#v", selectedIDs)
	}
	if !edgeKinds(selected)["purchase_order:po-1|supplied_by|vendor:vendor-1"] {
		t.Fatalf("kind selection dropped the remaining relationship: %#v", edgeKinds(selected))
	}
}

func TestGraphSearchKeepsDirectConnections(t *testing.T) {
	t.Parallel()
	service := testService(t, Dependencies{
		Ledger: fakeLedger{snapshot: ledger.Snapshot{
			Vendors:        []ledger.Vendor{{ID: "vendor-1", Name: "Campus Store", Status: "active"}},
			PurchaseOrders: []ledger.PurchaseOrder{{ID: "po-1", Number: "PO-2026-001", VendorID: "vendor-1", Status: "ordered"}},
		}},
	})
	graph, err := service.Graph(context.Background(), Query{Search: "PO-2026", Limit: 50, Scope: Scope{Finance: true}})
	if err != nil {
		t.Fatal(err)
	}
	ids := nodeIDs(graph)
	if !ids["purchase_order:po-1"] || !ids["vendor:vendor-1"] {
		t.Fatalf("search should retain the matching PO and its vendor: %#v", ids)
	}
}

func TestNewServiceRequiresOrganization(t *testing.T) {
	t.Parallel()
	if _, err := NewService(Dependencies{}); err == nil {
		t.Fatal("expected organization validation")
	}
}

func nodeIDs(graph Graph) map[string]bool {
	result := map[string]bool{}
	for _, node := range graph.Nodes {
		result[node.ID] = true
	}
	return result
}

func edgeKinds(graph Graph) map[string]bool {
	result := map[string]bool{}
	for _, edge := range graph.Edges {
		result[edge.From+"|"+string(edge.Kind)+"|"+edge.To] = true
	}
	return result
}

func containsAll(values []string, required ...string) bool {
	present := map[string]struct{}{}
	for _, value := range values {
		present[value] = struct{}{}
	}
	for _, value := range required {
		if _, ok := present[value]; !ok {
			return false
		}
	}
	return true
}

func TestFilterDropsUnknownRelationshipKinds(t *testing.T) {
	t.Parallel()
	service := testService(t, Dependencies{Ledger: fakeLedger{snapshot: ledger.Snapshot{
		Vendors: []ledger.Vendor{{ID: "vendor-1", Name: "Campus Store", Status: "active"}},
	}}})
	if _, err := service.Graph(context.Background(), Query{Relationships: []RelationshipKind{"invented"}, Scope: Scope{Finance: true}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid relationship, got %v", err)
	}
}

func TestVaultDocumentsLinkOnlyWhenOwnerIsPresent(t *testing.T) {
	t.Parallel()
	blob := storage.Blob{ID: "blob-1", Name: "Receipt", MediaType: "application/pdf", ResourceType: "ledger.purchase-order", ResourceID: "po-1"}
	service := testService(t, Dependencies{
		Ledger: fakeLedger{snapshot: ledger.Snapshot{
			PurchaseOrders: []ledger.PurchaseOrder{{ID: "po-1", Number: "PO-1", VendorID: "missing", Status: "draft"}},
		}},
		Vault: fakeVault{blobs: []storage.Blob{blob}},
	})
	graph, err := service.Graph(context.Background(), Query{Limit: 50, Scope: Scope{Finance: true, Storage: true}})
	if err != nil {
		t.Fatal(err)
	}
	if !edgeKinds(graph)["purchase_order:po-1|documented_by|document:blob-1"] {
		t.Fatalf("expected document edge, got %#v", edgeKinds(graph))
	}
}

func TestSearchRejectsControlCharacters(t *testing.T) {
	t.Parallel()
	service := testService(t, Dependencies{})
	if _, err := service.Graph(context.Background(), Query{Search: "bad\nquery", Scope: Scope{Finance: true}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid search, got %v", err)
	}
}

func TestEndedAssignmentsAndRemovedInstallationsAreOmitted(t *testing.T) {
	t.Parallel()
	ended := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	removed := ended
	service := testService(t, Dependencies{
		Directory: &fakeDirectory{graph: directoryexpansion.Graph{
			Nodes: []directoryexpansion.Node{{ID: "asset:laptop", Kind: NodeAsset, Label: "Laptop"}},
		}},
		Stack: fakeStack{snapshot: stack.Snapshot{
			Products:      []stack.Product{{ID: "office", Name: "Office", Status: "active"}},
			Versions:      []stack.Version{{ID: "v1", ProductID: "office", Name: "1", Status: "active"}},
			Licenses:      []stack.License{{ID: "lic", ProductID: "office", Name: "Seats", Status: "active"}},
			Installations: []stack.Installation{{ID: "old", VersionID: "v1", AssetID: "laptop", Status: "removed", RemovedAt: &removed}},
			Assignments:   []stack.Assignment{{ID: "old-assign", LicenseID: "lic", AssigneeKind: "asset", AssigneeID: "laptop", EndedAt: &ended}},
		}},
	})
	graph, err := service.Graph(context.Background(), Query{Limit: 50, Scope: Scope{Directory: people.Visibility{All: true}, Software: true}})
	if err != nil {
		t.Fatal(err)
	}
	for key := range edgeKinds(graph) {
		if strings.Contains(key, "installed_on") || strings.HasPrefix(key, "license:lic|assigned_to") {
			t.Fatalf("inactive stack relationship leaked: %s", key)
		}
	}
}
