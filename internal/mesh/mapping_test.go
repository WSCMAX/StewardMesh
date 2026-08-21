package mesh

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

import (
	"testing"

	"github.com/maxlemke/stewardmesh/internal/threads"
)

func TestRecordTypeNodeKindsMapsKnownTypesAndSkipsUnknown(t *testing.T) {
	t.Parallel()
	cases := []struct {
		recordType string
		want       []NodeKind
	}{
		{"atlas.asset", []NodeKind{NodeAsset}},
		{"atlas.model", []NodeKind{NodeModel}},
		{"people.identity", identityNodeKinds},
		{"ledger.purchase-order", []NodeKind{NodePurchaseOrder}},
		{"ledger.vendor", []NodeKind{NodeVendor}},
		{"stack.license", []NodeKind{NodeLicense}},
		{"vault.blob", []NodeKind{NodeDocument}},
		{"horizon.plan", []NodeKind{NodePlan}},
		{"people.assignment", nil},
		{"stack.installation", nil},
		{"unknown.record", nil},
	}
	for _, test := range cases {
		got := nodeKindsForRecordType(test.recordType)
		if !equalKinds(got, test.want) {
			t.Fatalf("record type %s: got %#v want %#v", test.recordType, got, test.want)
		}
	}
}

func TestGoalTargetKindsUseThreadsVocabulary(t *testing.T) {
	t.Parallel()
	if got := nodeKindsForGoalTarget(threads.TargetPurchase); !equalKinds(got, []NodeKind{NodePurchaseOrder}) {
		t.Fatalf("purchase target mapped to %#v", got)
	}
	if got := nodeKindsForGoalTarget(threads.TargetSoftware); !equalKinds(got, []NodeKind{NodeProduct}) {
		t.Fatalf("software target mapped to %#v", got)
	}
	if got := nodeKindsForGoalTarget(threads.TargetType("unknown")); got != nil {
		t.Fatalf("unknown target should drop, got %#v", got)
	}
}

func TestNodeIDForRecordRequiresAnExistingNode(t *testing.T) {
	t.Parallel()
	builder := newBuilder()
	builder.addNode(Node{ID: "person:ada", Kind: NodePerson, Label: "Ada"})
	builder.addNode(Node{ID: "asset:laptop", Kind: NodeAsset, Label: "Laptop"})
	if got := builder.nodeIDForRecord("people.identity", "ada"); got != "person:ada" {
		t.Fatalf("identity lookup got %q", got)
	}
	if got := builder.nodeIDForRecord("atlas.asset", "laptop"); got != "asset:laptop" {
		t.Fatalf("asset lookup got %q", got)
	}
	if got := builder.nodeIDForRecord("atlas.asset", "missing"); got != "" {
		t.Fatalf("missing asset should not invent a node, got %q", got)
	}
	if got := builder.nodeIDForRecord("people.assignment", "ada"); got != "" {
		t.Fatalf("unmapped record type should drop, got %q", got)
	}
}

func equalKinds(left, right []NodeKind) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
