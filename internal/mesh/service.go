// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.
package mesh

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/directoryexpansion"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/horizon"
	"github.com/maxlemke/stewardmesh/internal/labels"
	"github.com/maxlemke/stewardmesh/internal/people"
)

type Service struct {
	directory      DirectoryGraph
	atlas          AtlasReader
	ledger         LedgerReader
	stack          StackReader
	labels         LabelsReader
	goals          GoalsReader
	vault          VaultReader
	horizon        HorizonReader
	organizationID string
	organization   string
}

func NewService(deps Dependencies) (*Service, error) {
	deps.OrganizationID = strings.TrimSpace(deps.OrganizationID)
	deps.Organization = strings.TrimSpace(deps.Organization)
	if deps.OrganizationID == "" || deps.Organization == "" {
		return nil, errors.New("mesh graph organization id and name are required")
	}
	return &Service{
		directory: deps.Directory, atlas: deps.Atlas, ledger: deps.Ledger, stack: deps.Stack,
		labels: deps.Labels, goals: deps.Goals, vault: deps.Vault, horizon: deps.Horizon,
		organizationID: deps.OrganizationID, organization: deps.Organization,
	}, nil
}

func (s *Service) Graph(ctx context.Context, query Query) (Graph, error) {
	query, err := normalizeQuery(query)
	if err != nil {
		return Graph{}, err
	}
	builder := newBuilder()
	if wantsAny(query.Kinds, NodeOrganization) {
		builder.addNode(Node{
			ID: typedNodeID(NodeOrganization, s.organizationID), Kind: NodeOrganization, Label: s.organization,
			Attributes: attributes(SourcePeople, "", ""),
		})
	}

	if s.directory != nil && !query.Scope.Directory.Empty() && wantsAny(query.Kinds, directoryNodeKinds...) {
		directoryQuery := directoryexpansion.GraphQuery{
			Search: query.Search, Limit: query.Limit,
			Scope: directoryexpansion.GraphScope{Directory: query.Scope.Directory, Assets: query.Scope.Assets},
		}
		if len(query.Kinds) == 1 && isDirectoryKind(query.Kinds[0]) {
			directoryQuery.Kind = query.Kinds[0]
		}
		graph, err := s.directory.Graph(ctx, directoryQuery)
		if err != nil {
			return Graph{}, err
		}
		builder.addDirectory(graph)
	}

	if s.atlas != nil && !query.Scope.Assets.Empty() {
		if err := s.projectAtlas(ctx, builder, query); err != nil {
			return Graph{}, err
		}
	}
	if s.ledger != nil && query.Scope.Finance {
		if err := s.projectLedger(ctx, builder, query); err != nil {
			return Graph{}, err
		}
	}
	if s.stack != nil && query.Scope.Software {
		if err := s.projectStack(ctx, builder, query); err != nil {
			return Graph{}, err
		}
	}
	if s.labels != nil && query.Scope.Labels {
		if err := s.projectLabels(ctx, builder, query); err != nil {
			return Graph{}, err
		}
	}
	if s.goals != nil && query.Scope.Goals {
		if err := s.projectGoals(ctx, builder, query); err != nil {
			return Graph{}, err
		}
	}
	if s.vault != nil && query.Scope.Storage {
		if err := s.projectVault(ctx, builder, query); err != nil {
			return Graph{}, err
		}
	}
	if s.horizon != nil && query.Scope.Planning {
		if err := s.projectHorizon(ctx, builder, query); err != nil {
			return Graph{}, err
		}
	}
	return filterGraph(builder.graph(), query), nil
}

func (s *Service) projectAtlas(ctx context.Context, builder *graphBuilder, query Query) error {
	orgID := typedNodeID(NodeOrganization, s.organizationID)
	modelsByNumber := map[string]string{}
	if wantsAny(query.Kinds, NodeModel) {
		models, err := s.atlas.ListModels(ctx, atlas.ModelQuery{Limit: boundedAtlasListLimit(MaximumLimit)})
		if err != nil {
			return err
		}
		for _, model := range models {
			label := strings.TrimSpace(model.Manufacturer + " " + model.Name)
			if number := strings.TrimSpace(model.ModelNumber); number != "" {
				if label == "" {
					label = number
				} else {
					label += " · " + number
				}
			}
			id := typedNodeID(NodeModel, model.ID)
			builder.addNode(Node{
				ID: id, Kind: NodeModel, Label: firstNonEmpty(label, model.Name, model.ID),
				Attributes: attributes(SourceAtlas, model.Status, firstNonEmpty(model.ModelNumber, model.Kind)),
			})
			builder.addEdge(RelationshipContains, orgID, id, nil)
			if number := normalizeModelNumber(model.ModelNumber); number != "" {
				modelsByNumber[number] = id
			}
		}
	}
	if !wantsAny(query.Kinds, NodeAsset) && !wantsAny(query.Kinds, NodeModel) {
		return nil
	}
	assets, err := s.atlas.ListGraphAssets(ctx, atlas.GraphAssetQuery{
		LabelSearch: query.Search, Limit: query.Limit,
		Visibility: graphAssetVisibility(query.Scope.Assets),
		Directory:  atlasDirectoryVisibility(query.Scope.Directory),
	})
	if err != nil {
		return err
	}
	for _, asset := range assets {
		if asset.OrganizationID != s.organizationID {
			continue
		}
		if wantsAny(query.Kinds, NodeAsset) {
			builder.addNode(Node{
				ID: typedNodeID(NodeAsset, asset.ID), Kind: NodeAsset, Label: firstNonEmpty(asset.Name, asset.ID),
				Attributes: attributes(SourceAtlas, asset.Status, asset.Kind),
			})
		}
		s.linkAsset(builder, asset, modelsByNumber)
	}
	return nil
}

func (s *Service) linkAsset(builder *graphBuilder, asset domain.Asset, modelsByNumber map[string]string) {
	assetID := typedNodeID(NodeAsset, asset.ID)
	if !builder.hasNode(assetID) {
		return
	}
	orgID := typedNodeID(NodeOrganization, s.organizationID)
	builder.addEdge(RelationshipContains, orgID, assetID, nil)
	if asset.DepartmentID != "" {
		departmentID := typedNodeID(NodeDepartment, asset.DepartmentID)
		builder.addEdge(RelationshipBelongsTo, assetID, departmentID, nil)
		builder.addEdge(RelationshipContains, orgID, departmentID, nil)
	}
	for _, reference := range []struct {
		kind NodeKind
		id   string
	}{
		{NodeSite, asset.SiteID},
		{NodeBuilding, asset.BuildingID},
		{NodeRoom, asset.RoomID},
	} {
		if reference.id == "" {
			continue
		}
		builder.addEdge(RelationshipLocatedAt, assetID, typedNodeID(reference.kind, reference.id), nil)
	}
	if asset.ModelID != "" {
		builder.addEdge(RelationshipModeledAs, assetID, typedNodeID(NodeModel, asset.ModelID), nil)
		return
	}
	if modelID := modelsByNumber[normalizeModelNumber(assetModelNumber(asset))]; modelID != "" {
		builder.addEdge(RelationshipModeledAs, assetID, modelID, nil)
	}
}

func (s *Service) projectLedger(ctx context.Context, builder *graphBuilder, query Query) error {
	snapshot, err := s.ledger.Snapshot(ctx)
	if err != nil {
		return err
	}
	orgID := typedNodeID(NodeOrganization, s.organizationID)
	if wantsAny(query.Kinds, NodeVendor) {
		for _, vendor := range snapshot.Vendors {
			id := typedNodeID(NodeVendor, vendor.ID)
			builder.addNode(Node{ID: id, Kind: NodeVendor, Label: firstNonEmpty(vendor.Name, vendor.ID), Attributes: attributes(SourceLedger, vendor.Status, "")})
			builder.addEdge(RelationshipContains, orgID, id, nil)
		}
	}
	if wantsAny(query.Kinds, NodePurchaseOrder) {
		for _, purchaseOrder := range snapshot.PurchaseOrders {
			id := typedNodeID(NodePurchaseOrder, purchaseOrder.ID)
			builder.addNode(Node{
				ID: id, Kind: NodePurchaseOrder, Label: firstNonEmpty(purchaseOrder.Number, purchaseOrder.ID),
				Attributes: attributes(SourceLedger, purchaseOrder.Status, ""),
			})
			builder.addEdge(RelationshipSuppliedBy, id, typedNodeID(NodeVendor, purchaseOrder.VendorID), nil)
			for _, assetID := range purchaseOrder.AssetIDs {
				builder.addEdge(RelationshipPurchasedVia, typedNodeID(NodeAsset, assetID), id, nil)
			}
			for _, documentID := range purchaseOrder.ReceiptDocumentIDs {
				builder.addEdge(RelationshipDocumentedBy, id, typedNodeID(NodeDocument, documentID), nil)
			}
			for _, line := range purchaseOrder.Lines {
				if line.AssetID != "" {
					builder.addEdge(RelationshipPurchasedVia, typedNodeID(NodeAsset, line.AssetID), id, nil)
				}
				if line.ModelID != "" {
					builder.addEdge(RelationshipPurchasedVia, typedNodeID(NodeModel, line.ModelID), id, nil)
				}
				if line.LicenseID != "" {
					builder.addEdge(RelationshipPurchasedVia, typedNodeID(NodeLicense, line.LicenseID), id, nil)
				}
			}
		}
	}
	if wantsAny(query.Kinds, NodeContract) {
		for _, contract := range snapshot.Contracts {
			id := typedNodeID(NodeContract, contract.ID)
			builder.addNode(Node{ID: id, Kind: NodeContract, Label: firstNonEmpty(contract.Name, contract.ID), Attributes: attributes(SourceLedger, contract.OperationalStatus, "")})
			builder.addEdge(RelationshipSuppliedBy, id, typedNodeID(NodeVendor, contract.VendorID), nil)
			for _, documentID := range contract.DocumentIDs {
				builder.addEdge(RelationshipDocumentedBy, id, typedNodeID(NodeDocument, documentID), nil)
			}
		}
	}
	if wantsAny(query.Kinds, NodeBudget) {
		for _, budget := range snapshot.Budgets {
			id := typedNodeID(NodeBudget, budget.ID)
			builder.addNode(Node{ID: id, Kind: NodeBudget, Label: firstNonEmpty(budget.Name, budget.ID), Attributes: attributes(SourceLedger, "", budget.FiscalPeriod)})
			if budget.DepartmentID != "" {
				builder.addEdge(RelationshipBelongsTo, id, typedNodeID(NodeDepartment, budget.DepartmentID), nil)
			}
			if budget.SiteID != "" {
				builder.addEdge(RelationshipLocatedAt, id, typedNodeID(NodeSite, budget.SiteID), nil)
			}
			if budget.DepartmentID == "" && budget.SiteID == "" {
				builder.addEdge(RelationshipContains, orgID, id, nil)
			}
		}
	}
	if wantsAny(query.Kinds, NodeCommitment) {
		for _, commitment := range snapshot.Commitments {
			id := typedNodeID(NodeCommitment, commitment.ID)
			builder.addNode(Node{ID: id, Kind: NodeCommitment, Label: firstNonEmpty(commitment.Description, commitment.ID), Attributes: attributes(SourceLedger, "", commitment.Kind)})
			builder.addEdge(RelationshipCoveredBy, id, typedNodeID(NodeContract, commitment.ContractID), nil)
		}
	}
	return nil
}

func (s *Service) projectStack(ctx context.Context, builder *graphBuilder, query Query) error {
	snapshot, err := s.stack.Snapshot(ctx)
	if err != nil {
		return err
	}
	orgID := typedNodeID(NodeOrganization, s.organizationID)
	if wantsAny(query.Kinds, NodeProduct) {
		for _, product := range snapshot.Products {
			id := typedNodeID(NodeProduct, product.ID)
			builder.addNode(Node{ID: id, Kind: NodeProduct, Label: firstNonEmpty(product.Name, product.ID), Attributes: attributes(SourceStack, product.Status, product.Publisher)})
			builder.addEdge(RelationshipContains, orgID, id, nil)
		}
	}
	if wantsAny(query.Kinds, NodeVersion) {
		for _, version := range snapshot.Versions {
			id := typedNodeID(NodeVersion, version.ID)
			builder.addNode(Node{ID: id, Kind: NodeVersion, Label: firstNonEmpty(version.Name, version.ID), Attributes: attributes(SourceStack, version.Status, "")})
			builder.addEdge(RelationshipContains, typedNodeID(NodeProduct, version.ProductID), id, nil)
		}
	}
	if wantsAny(query.Kinds, NodeLicense) {
		for _, license := range snapshot.Licenses {
			id := typedNodeID(NodeLicense, license.ID)
			builder.addNode(Node{ID: id, Kind: NodeLicense, Label: firstNonEmpty(license.Name, license.ID), Attributes: attributes(SourceStack, license.Status, "")})
			builder.addEdge(RelationshipBelongsTo, id, typedNodeID(NodeProduct, license.ProductID), nil)
			if license.VersionID != "" {
				builder.addEdge(RelationshipBelongsTo, id, typedNodeID(NodeVersion, license.VersionID), nil)
			}
			if license.VendorID != "" {
				builder.addEdge(RelationshipSuppliedBy, id, typedNodeID(NodeVendor, license.VendorID), nil)
			}
			if license.PurchaseOrderID != "" {
				builder.addEdge(RelationshipPurchasedVia, id, typedNodeID(NodePurchaseOrder, license.PurchaseOrderID), nil)
			}
			if license.ContractID != "" {
				builder.addEdge(RelationshipCoveredBy, id, typedNodeID(NodeContract, license.ContractID), nil)
			}
			for _, documentID := range license.DocumentIDs {
				builder.addEdge(RelationshipDocumentedBy, id, typedNodeID(NodeDocument, documentID), nil)
			}
		}
	}
	for _, installation := range snapshot.Installations {
		if installation.RemovedAt != nil || strings.EqualFold(installation.Status, "removed") {
			continue
		}
		builder.addEdge(RelationshipInstalledOn, typedNodeID(NodeVersion, installation.VersionID), typedNodeID(NodeAsset, installation.AssetID), nil)
	}
	for _, assignment := range snapshot.Assignments {
		if assignment.EndedAt != nil {
			continue
		}
		from := typedNodeID(NodeLicense, assignment.LicenseID)
		for _, kind := range assigneeNodeKinds(assignment.AssigneeKind) {
			if builder.addEdge(RelationshipAssignedTo, from, typedNodeID(kind, assignment.AssigneeID), nil) {
				break
			}
		}
	}
	return nil
}

func (s *Service) projectLabels(ctx context.Context, builder *graphBuilder, query Query) error {
	snapshot, err := s.labels.Snapshot(ctx)
	if err != nil {
		return err
	}
	orgID := typedNodeID(NodeOrganization, s.organizationID)
	if wantsAny(query.Kinds, NodeLabel) {
		for _, definition := range snapshot.Definitions {
			if definition.Status != labels.StatusActive {
				continue
			}
			id := typedNodeID(NodeLabel, definition.ID)
			builder.addNode(Node{
				ID: id, Kind: NodeLabel, Label: firstNonEmpty(definition.Name, definition.ID),
				Attributes: attributes(SourceLabels, string(definition.Status), string(definition.ValueKind)),
			})
			if definition.ParentID != "" {
				builder.addEdge(RelationshipContains, typedNodeID(NodeLabel, definition.ParentID), id, nil)
			} else {
				builder.addEdge(RelationshipContains, orgID, id, nil)
			}
			if definition.GoalID != "" {
				builder.addEdge(RelationshipAdvances, id, typedNodeID(NodeGoal, definition.GoalID), nil)
			}
		}
	}
	for _, assignment := range snapshot.Assignments {
		from := builder.nodeIDForRecord(assignment.RecordType, assignment.RecordID)
		if from == "" {
			continue
		}
		builder.addEdge(RelationshipTaggedWith, from, typedNodeID(NodeLabel, assignment.DefinitionID), nil)
	}
	return nil
}

func (s *Service) projectGoals(ctx context.Context, builder *graphBuilder, query Query) error {
	snapshot, err := s.goals.Snapshot(ctx)
	if err != nil {
		return err
	}
	orgID := typedNodeID(NodeOrganization, s.organizationID)
	if wantsAny(query.Kinds, NodeGoal) {
		for _, goal := range snapshot.Goals {
			id := typedNodeID(NodeGoal, goal.ID)
			builder.addNode(Node{ID: id, Kind: NodeGoal, Label: firstNonEmpty(goal.Name, goal.ID), Attributes: attributes(SourceGoals, "", "")})
			if goal.ParentID != "" {
				builder.addEdge(RelationshipContains, typedNodeID(NodeGoal, goal.ParentID), id, nil)
			} else {
				builder.addEdge(RelationshipContains, orgID, id, nil)
			}
		}
	}
	for _, link := range snapshot.GoalLinks {
		from := builder.nodeIDForKinds(nodeKindsForGoalTarget(link.TargetType), link.TargetID)
		if from == "" {
			continue
		}
		builder.addEdge(RelationshipAdvances, from, typedNodeID(NodeGoal, link.GoalID), nil)
	}
	return nil
}

func (s *Service) projectVault(ctx context.Context, builder *graphBuilder, query Query) error {
	if !wantsAny(query.Kinds, NodeDocument) {
		return nil
	}
	blobs, err := s.vault.ListBlobs(ctx)
	if err != nil {
		return err
	}
	orgID := typedNodeID(NodeOrganization, s.organizationID)
	for _, blob := range blobs {
		id := typedNodeID(NodeDocument, blob.ID)
		builder.addNode(Node{ID: id, Kind: NodeDocument, Label: firstNonEmpty(blob.Name, blob.ID), Attributes: attributes(SourceVault, blob.MediaType, "")})
		if blob.ResourceType != "" && blob.ResourceID != "" {
			if owner := builder.nodeIDForRecord(blob.ResourceType, blob.ResourceID); owner != "" {
				builder.addEdge(RelationshipDocumentedBy, owner, id, nil)
				continue
			}
		}
		builder.addEdge(RelationshipContains, orgID, id, nil)
	}
	return nil
}

func (s *Service) projectHorizon(ctx context.Context, builder *graphBuilder, query Query) error {
	if !wantsAny(query.Kinds, NodePlan) {
		return nil
	}
	plans, err := s.horizon.ListPlans(ctx, horizon.ListPlansQuery{})
	if err != nil {
		return err
	}
	for _, plan := range plans {
		id := typedNodeID(NodePlan, plan.ID)
		label := strings.TrimSpace(plan.Scenario)
		if label != "" && plan.AssetID != "" {
			label += " · " + plan.AssetID
		}
		builder.addNode(Node{ID: id, Kind: NodePlan, Label: firstNonEmpty(label, plan.ID), Attributes: attributes(SourceHorizon, plan.LifecycleStage, plan.Scenario)})
		builder.addEdge(RelationshipPlannedFor, id, typedNodeID(NodeAsset, plan.AssetID), nil)
	}
	return nil
}

func graphAssetVisibility(visibility directoryexpansion.AssetVisibility) atlas.GraphAssetVisibility {
	return atlas.GraphAssetVisibility{
		All: visibility.All, ResourceIDs: visibility.ResourceIDs, SiteIDs: visibility.SiteIDs, DepartmentIDs: visibility.DepartmentIDs,
	}
}

func atlasDirectoryVisibility(visibility people.Visibility) atlas.GraphAssetDirectoryVisibility {
	if visibility.All {
		return atlas.GraphAssetDirectoryVisibility{All: true}
	}
	if visibility.Empty() {
		return atlas.GraphAssetDirectoryVisibility{All: true}
	}
	return atlas.GraphAssetDirectoryVisibility{SiteIDs: visibility.SiteIDs, DepartmentIDs: visibility.DepartmentIDs, MatchUserDirectory: true}
}

func boundedAtlasListLimit(limit int) int {
	if limit < 1 {
		return 1
	}
	if limit > 100 {
		return 100
	}
	return limit
}

type graphBuilder struct {
	nodes map[string]Node
	edges map[string]Edge
}

func newBuilder() *graphBuilder {
	return &graphBuilder{nodes: make(map[string]Node), edges: make(map[string]Edge)}
}

func (b *graphBuilder) addDirectory(graph directoryexpansion.Graph) {
	for _, node := range graph.Nodes {
		if node.Attributes == nil {
			node.Attributes = map[string]string{}
		}
		if _, ok := node.Attributes["source"]; !ok {
			if copied := cloneAttributes(node.Attributes); copied != nil {
				copied["source"] = sourceForKind(node.Kind)
				node.Attributes = copied
			} else {
				node.Attributes = attributes(sourceForKind(node.Kind), "", "")
			}
		}
		b.addNode(node)
	}
	for _, edge := range graph.Edges {
		b.addEdgeWithID(edge.ID, edge.Kind, edge.From, edge.To, edge.Attributes)
	}
}

func (b *graphBuilder) addNode(node Node) {
	node.Label = boundedLabel(node.Label)
	if node.ID == "" || node.Kind == "" || node.Label == "" {
		return
	}
	if existing, ok := b.nodes[node.ID]; ok {
		if nodeLess(node, existing) {
			b.nodes[node.ID] = node
		}
		return
	}
	b.nodes[node.ID] = node
}

func (b *graphBuilder) hasNode(id string) bool {
	_, ok := b.nodes[id]
	return ok
}

func (b *graphBuilder) nodeIDForRecord(recordType, recordID string) string {
	return b.nodeIDForKinds(nodeKindsForRecordType(recordType), recordID)
}

func (b *graphBuilder) nodeIDForKinds(kinds []NodeKind, recordID string) string {
	recordID = strings.TrimSpace(recordID)
	if recordID == "" {
		return ""
	}
	for _, kind := range kinds {
		id := typedNodeID(kind, recordID)
		if b.hasNode(id) {
			return id
		}
	}
	return ""
}

func (b *graphBuilder) addEdge(kind RelationshipKind, from, to string, attributes map[string]string) bool {
	return b.addEdgeWithID("", kind, from, to, attributes)
}

func (b *graphBuilder) addEdgeWithID(id string, kind RelationshipKind, from, to string, attrs map[string]string) bool {
	if kind == "" || !b.hasNode(from) || !b.hasNode(to) {
		return false
	}
	key := from + "\x00" + string(kind) + "\x00" + to
	if id == "" {
		id = digestStrings(RequirementID, from, string(kind), to)[:32]
	}
	edge := Edge{ID: id, From: from, To: to, Kind: kind, Attributes: cloneAttributes(attrs)}
	if existing, ok := b.edges[key]; !ok || edge.ID < existing.ID {
		b.edges[key] = edge
	}
	return true
}

func (b *graphBuilder) graph() Graph {
	nodes := make([]Node, 0, len(b.nodes))
	for _, node := range b.nodes {
		nodes = append(nodes, node)
	}
	edges := make([]Edge, 0, len(b.edges))
	for _, edge := range b.edges {
		edges = append(edges, edge)
	}
	return Graph{Nodes: nodes, Edges: edges}
}

func normalizeQuery(query Query) (Query, error) {
	query.Search = strings.TrimSpace(query.Search)
	if !validGraphText(query.Search, 200) {
		return Query{}, ErrInvalidInput
	}
	kinds := uniqueNodeKinds(query.Kinds)
	for _, kind := range kinds {
		if !validNodeKind(kind) || kind == "" {
			return Query{}, ErrInvalidInput
		}
	}
	query.Kinds = kinds
	relationships := uniqueRelationshipKinds(query.Relationships)
	for _, kind := range relationships {
		if !validRelationshipKind(kind) || kind == "" {
			return Query{}, ErrInvalidInput
		}
	}
	query.Relationships = relationships
	if query.Limit == 0 {
		query.Limit = DefaultLimit
	}
	if query.Limit < 1 || query.Limit > MaximumLimit {
		return Query{}, ErrInvalidInput
	}
	if query.Scope.Empty() {
		return Query{}, ErrGraphScope
	}
	return query, nil
}

func uniqueNodeKinds(values []NodeKind) []NodeKind {
	seen := make(map[NodeKind]struct{}, len(values))
	result := make([]NodeKind, 0, len(values))
	for _, value := range values {
		value = NodeKind(strings.ToLower(strings.TrimSpace(string(value))))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func uniqueRelationshipKinds(values []RelationshipKind) []RelationshipKind {
	seen := make(map[RelationshipKind]struct{}, len(values))
	result := make([]RelationshipKind, 0, len(values))
	for _, value := range values {
		value = RelationshipKind(strings.ToLower(strings.TrimSpace(string(value))))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func filterGraph(graph Graph, query Query) Graph {
	nodes := append([]Node(nil), graph.Nodes...)
	sort.Slice(nodes, func(i, j int) bool { return nodeLess(nodes[i], nodes[j]) })
	available := make(map[string]Node, len(nodes))
	kindAllowed := make(map[NodeKind]struct{}, len(query.Kinds))
	for _, kind := range query.Kinds {
		kindAllowed[kind] = struct{}{}
	}
	search := strings.ToLower(query.Search)
	anchors := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		available[node.ID] = node
		if len(kindAllowed) > 0 {
			if _, ok := kindAllowed[node.Kind]; !ok {
				continue
			}
		}
		if search != "" && !strings.Contains(strings.ToLower(node.Label), search) {
			continue
		}
		anchors = append(anchors, node)
	}
	edges := append([]Edge(nil), graph.Edges...)
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Kind != edges[j].Kind {
			return edges[i].Kind < edges[j].Kind
		}
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		return edges[i].ID < edges[j].ID
	})
	relationshipAllowed := make(map[RelationshipKind]struct{}, len(query.Relationships))
	for _, kind := range query.Relationships {
		relationshipAllowed[kind] = struct{}{}
	}

	selected := make(map[string]struct{})
	addNode := func(node Node) bool {
		if _, present := selected[node.ID]; present {
			return true
		}
		if len(kindAllowed) > 0 {
			if _, ok := kindAllowed[node.Kind]; !ok {
				return false
			}
		}
		if len(selected) >= query.Limit {
			return false
		}
		selected[node.ID] = struct{}{}
		return true
	}
	adjacency := make(map[string][]string, len(available))
	for _, edge := range edges {
		if len(relationshipAllowed) > 0 {
			if _, ok := relationshipAllowed[edge.Kind]; !ok {
				continue
			}
		}
		adjacency[edge.From] = append(adjacency[edge.From], edge.To)
		adjacency[edge.To] = append(adjacency[edge.To], edge.From)
	}
	addWithNeighbors := func(node Node) {
		if !addNode(node) {
			return
		}
		for _, otherID := range adjacency[node.ID] {
			other, ok := available[otherID]
			if !ok {
				continue
			}
			addNode(other)
		}
	}
	for _, node := range anchors {
		addWithNeighbors(node)
	}

	resultEdges := make([]Edge, 0, len(edges))
	seenEdges := make(map[string]struct{}, len(edges))
	for _, edge := range edges {
		if len(relationshipAllowed) > 0 {
			if _, ok := relationshipAllowed[edge.Kind]; !ok {
				continue
			}
		}
		from, fromOK := available[edge.From]
		to, toOK := available[edge.To]
		if !fromOK || !toOK {
			continue
		}
		_, fromSelected := selected[from.ID]
		_, toSelected := selected[to.ID]
		if !fromSelected && !toSelected {
			if len(query.Relationships) == 0 && search == "" {
				continue
			}
			if !addNode(from) || !addNode(to) {
				continue
			}
		} else {
			if !fromSelected && !addNode(from) {
				continue
			}
			if !toSelected && !addNode(to) {
				continue
			}
		}
		if _, fromSelected = selected[from.ID]; !fromSelected {
			continue
		}
		if _, toSelected = selected[to.ID]; !toSelected {
			continue
		}
		key := edge.From + "\x00" + string(edge.Kind) + "\x00" + edge.To
		if _, duplicate := seenEdges[key]; duplicate {
			continue
		}
		seenEdges[key] = struct{}{}
		resultEdges = append(resultEdges, edge)
		if len(resultEdges) == MaximumEdges {
			break
		}
	}

	resultNodes := make([]Node, 0, len(selected))
	for _, node := range available {
		if _, ok := selected[node.ID]; ok {
			resultNodes = append(resultNodes, node)
		}
	}
	sort.Slice(resultNodes, func(i, j int) bool { return nodeLess(resultNodes[i], resultNodes[j]) })
	sources := make(map[string]struct{})
	for _, node := range resultNodes {
		if source := sourceForKind(node.Kind); source != "" {
			sources[source] = struct{}{}
		}
	}
	sourceList := make([]string, 0, len(sources))
	for source := range sources {
		sourceList = append(sourceList, source)
	}
	sort.Strings(sourceList)
	return Graph{Nodes: resultNodes, Edges: resultEdges, Sources: sourceList}
}

func nodeLess(left, right Node) bool {
	if left.Kind != right.Kind {
		return left.Kind < right.Kind
	}
	leftLabel, rightLabel := strings.ToLower(left.Label), strings.ToLower(right.Label)
	if leftLabel != rightLabel {
		return leftLabel < rightLabel
	}
	return left.ID < right.ID
}

func typedNodeID(kind NodeKind, recordID string) string {
	return string(kind) + ":" + recordID
}

func attributes(source, status, extra string) map[string]string {
	result := map[string]string{}
	if source != "" {
		result["source"] = boundedAttr(source)
	}
	if status != "" {
		result["status"] = boundedAttr(status)
	}
	if extra != "" {
		result["detail"] = boundedAttr(extra)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func cloneAttributes(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	count := 0
	for key, value := range values {
		if count == 8 {
			break
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" || !validAttrKey(key) {
			continue
		}
		cloned[key] = boundedAttr(value)
		count++
	}
	if len(cloned) == 0 {
		return nil
	}
	return cloned
}

func validAttrKey(value string) bool {
	if utf8.RuneCountInString(value) > 64 {
		return false
	}
	for index, character := range value {
		if index == 0 && (character < 'a' || character > 'z') {
			return false
		}
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func boundedAttr(value string) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > 200 {
		return string(runes[:200])
	}
	return value
}

func boundedLabel(value string) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > 320 {
		return string(runes[:319]) + "…"
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeModelNumber(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func assetModelNumber(asset domain.Asset) string {
	if asset.ModelContext == nil {
		return ""
	}
	return asset.ModelContext.ModelNumber
}

func validGraphText(value string, maximum int) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func digestStrings(values ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(sum[:])
}
