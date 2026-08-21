package directoryexpansion

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

import (
	"fmt"
	"testing"
)

func TestGraphRelationshipContextDeterministicallyCapsLocationAnchors(t *testing.T) {
	const crowd = 500
	nodes := make([]Node, 0, crowd+100)
	for index := crowd + 99; index >= 0; index-- {
		id := fmt.Sprintf("site-%03d", index)
		nodes = append(nodes, Node{ID: typedNodeID(NodeSite, id), Kind: NodeSite, Label: id})
	}
	query := GraphQuery{Kind: NodeSite, Relationship: RelationshipContains, Limit: crowd}
	anchors := graphAnchorNodes(nodes, query)
	if len(anchors) != crowd || anchors[0].ID != "site:site-000" || anchors[len(anchors)-1].ID != "site:site-499" {
		t.Fatalf("location anchors were not sorted before the cap: first=%q last=%q count=%d", anchors[0].ID, anchors[len(anchors)-1].ID, len(anchors))
	}
	_, _, locations := graphRelationshipContext(query, anchors, nil, nil)
	if locations == nil || len(locations.ParentSiteIDs) != crowd || locations.ParentSiteIDs[0] != "site-000" ||
		locations.ParentSiteIDs[len(locations.ParentSiteIDs)-1] != "site-499" {
		t.Fatalf("location selectors were nondeterministic or exceeded the cap: %#v", locations)
	}
}
