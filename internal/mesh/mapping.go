package mesh

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

import (
	"strings"

	"github.com/maxlemke/stewardmesh/internal/threads"
)

// recordTypeNodeKinds maps Labels/Exchange record types onto graph node kinds.
// people.identity is resolved against person/shared/public/lab nodes already
// present in the graph rather than inventing an identity kind.
var recordTypeNodeKinds = map[string][]NodeKind{
	"people.site":           {NodeSite},
	"people.building":       {NodeBuilding},
	"people.room":           {NodeRoom},
	"people.department":     {NodeDepartment},
	"people.identity":       {NodePerson, NodeShared, NodePublic, NodeLab},
	"atlas.asset":           {NodeAsset},
	"atlas.model":           {NodeModel},
	"ledger.vendor":         {NodeVendor},
	"ledger.purchase-order": {NodePurchaseOrder},
	"ledger.contract":       {NodeContract},
	"ledger.budget":         {NodeBudget},
	"ledger.commitment":     {NodeCommitment},
	"stack.product":         {NodeProduct},
	"stack.version":         {NodeVersion},
	"stack.license":         {NodeLicense},
	"threads.goal":          {NodeGoal},
	"horizon.plan":          {NodePlan},
	"vault.blob":            {NodeDocument},
}

var identityNodeKinds = []NodeKind{NodePerson, NodeShared, NodePublic, NodeLab}

var directoryNodeKinds = []NodeKind{
	NodeOrganization, NodeSite, NodeBuilding, NodeRoom, NodeDepartment,
	NodePerson, NodeShared, NodePublic, NodeLab, NodeGroup, NodeSubject, NodeAsset,
}

func nodeKindsForRecordType(recordType string) []NodeKind {
	return recordTypeNodeKinds[strings.TrimSpace(recordType)]
}

func nodeKindsForGoalTarget(target threads.TargetType) []NodeKind {
	switch target {
	case threads.TargetAsset:
		return []NodeKind{NodeAsset}
	case threads.TargetPurchase:
		return []NodeKind{NodePurchaseOrder}
	case threads.TargetContract:
		return []NodeKind{NodeContract}
	case threads.TargetSoftware:
		return []NodeKind{NodeProduct}
	case threads.TargetBudget:
		return []NodeKind{NodeBudget}
	case threads.TargetGoal:
		return []NodeKind{NodeGoal}
	default:
		return nil
	}
}

func sourceForKind(kind NodeKind) string {
	switch kind {
	case NodeSite, NodeBuilding, NodeRoom, NodeDepartment,
		NodePerson, NodeShared, NodePublic, NodeLab, NodeGroup, NodeSubject:
		return SourcePeople
	case NodeAsset, NodeModel:
		return SourceAtlas
	case NodeVendor, NodePurchaseOrder, NodeContract, NodeBudget, NodeCommitment:
		return SourceLedger
	case NodeProduct, NodeVersion, NodeLicense:
		return SourceStack
	case NodeLabel:
		return SourceLabels
	case NodeGoal:
		return SourceGoals
	case NodeDocument:
		return SourceVault
	case NodePlan:
		return SourceHorizon
	default:
		return ""
	}
}

func validNodeKind(kind NodeKind) bool {
	switch kind {
	case "", NodeOrganization, NodeSite, NodeBuilding, NodeRoom, NodeDepartment, NodePerson,
		NodeShared, NodePublic, NodeLab, NodeGroup, NodeSubject, NodeAsset, NodeModel,
		NodeVendor, NodePurchaseOrder, NodeContract, NodeBudget, NodeCommitment,
		NodeProduct, NodeVersion, NodeLicense, NodeLabel, NodeGoal, NodeDocument, NodePlan:
		return true
	default:
		return false
	}
}

func validRelationshipKind(kind RelationshipKind) bool {
	switch kind {
	case "", RelationshipContains, RelationshipBelongsTo, RelationshipLocatedAt, RelationshipMemberOf,
		RelationshipAssignedTo, RelationshipTaggedWith, RelationshipAdvances, RelationshipPurchasedVia,
		RelationshipSuppliedBy, RelationshipCoveredBy, RelationshipDocumentedBy, RelationshipModeledAs,
		RelationshipInstalledOn, RelationshipPlannedFor,
		RelationshipUsesOffice, RelationshipTeachesIn, RelationshipAttendsClass, RelationshipResidesIn, RelationshipUsesLab:
		return true
	default:
		return false
	}
}

func isDirectoryKind(kind NodeKind) bool {
	for _, candidate := range directoryNodeKinds {
		if candidate == kind {
			return true
		}
	}
	return false
}

func wantsAny(kinds []NodeKind, candidates ...NodeKind) bool {
	if len(kinds) == 0 {
		return true
	}
	allowed := make(map[NodeKind]struct{}, len(kinds))
	for _, kind := range kinds {
		allowed[kind] = struct{}{}
	}
	for _, candidate := range candidates {
		if _, ok := allowed[candidate]; ok {
			return true
		}
	}
	return false
}

func assigneeNodeKinds(kind string) []NodeKind {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "asset":
		return []NodeKind{NodeAsset}
	case "identity":
		return identityNodeKinds
	case "department":
		return []NodeKind{NodeDepartment}
	case "site":
		return []NodeKind{NodeSite}
	case "room":
		return []NodeKind{NodeRoom}
	default:
		return nil
	}
}
