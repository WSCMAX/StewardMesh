package contracttest

// Provider-neutral Atlas adapter contract. Requirements: REQ-ATLAS-001, REQ-ATLAS-MODELS-001, REQ-DIRECTORY-EXPANSION-008. Features: inventory.assets, threads.relationships.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/domain"
)

func AtlasStore(t testing.TB, subject atlas.Store, organizationID, suffix string) {
	t.Helper()
	ctx := context.Background()
	assetID := "asset-" + suffix
	if _, err := subject.GetAsset(ctx, organizationID, assetID); !errors.Is(err, atlas.ErrNotFound) {
		t.Fatalf("expected missing Atlas asset, got %v", err)
	}
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	model := domain.AssetModel{
		ID: "model-" + suffix, OrganizationID: organizationID, Manufacturer: "Contract", Name: "Server",
		ModelNumber: "M-" + suffix, Kind: "server", Specifications: map[string]string{"CPU": "test"},
		WarrantyMonths: 36, UsefulLifeMonths: 48, Status: "active", Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	createdModel, err := subject.CreateModel(ctx, model)
	if err != nil {
		t.Fatal(err)
	}
	if createdModel.Revision != 1 || createdModel.InstanceCount != 0 {
		t.Fatalf("unexpected created Atlas model %#v", createdModel)
	}
	emptyModel := model
	emptyModel.ID = "model-empty-" + suffix
	emptyModel.ModelNumber = "EMPTY-" + suffix
	emptyModel.Specifications = nil
	createdEmptyModel, err := subject.CreateModel(ctx, emptyModel)
	if err != nil || len(createdEmptyModel.Specifications) != 0 {
		t.Fatalf("expected empty specifications to persist as an object, model=%#v err=%v", createdEmptyModel, err)
	}
	if _, err := subject.CreateModel(ctx, model); !errors.Is(err, atlas.ErrConflict) {
		t.Fatalf("expected duplicate Atlas model conflict, got %v", err)
	}
	resolvedModel, err := subject.ResolveModel(ctx, organizationID, atlas.ModelIdentity{
		Manufacturer: "contract", Name: "server", ModelNumber: strings.ToLower(model.ModelNumber),
	})
	if err != nil || resolvedModel.ID != model.ID {
		t.Fatalf("unexpected resolved Atlas model %#v err=%v", resolvedModel, err)
	}
	asset := domain.Asset{
		ID: assetID, OrganizationID: organizationID, ModelID: model.ID, ModelContext: contractModelContext(model, "server", now),
		Name: "Contract Server", Kind: "server",
		AssetTag: "TAG-" + suffix, SerialNumber: "SERIAL-" + suffix, Hostname: "contract.example.test",
		Status: "draft", Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	initial := domain.AssetLifecycleEvent{
		ID: lifecycleID(suffix, '1'), OrganizationID: organizationID, AssetID: assetID,
		ToStatus: "draft", Note: "Asset registered", Revision: 1, ActorID: "contract-user", OccurredAt: now,
	}
	created, err := subject.CreateAsset(ctx, asset, initial)
	if err != nil {
		t.Fatal(err)
	}
	if created.OrganizationID != organizationID || created.Revision != 1 {
		t.Fatalf("unexpected created Atlas asset %#v", created)
	}
	loadedModel, err := subject.GetModel(ctx, organizationID, model.ID)
	if err != nil || loadedModel.InstanceCount != 1 {
		t.Fatalf("unexpected Atlas model count %#v err=%v", loadedModel, err)
	}
	bulkAssets := []domain.Asset{
		{ID: "bulk-a-" + suffix, OrganizationID: organizationID, ModelID: model.ID, ModelContext: contractModelContext(model, "server", now), Name: "Bulk node A", Kind: "server", AssetTag: "BULK-A-" + suffix, DeploymentNotes: "Rack staging", Status: "draft", Revision: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "bulk-b-" + suffix, OrganizationID: organizationID, ModelID: model.ID, ModelContext: contractModelContext(model, "server", now), Name: "Bulk node B", Kind: "server", AssetTag: "BULK-B-" + suffix, Status: "draft", Revision: 1, CreatedAt: now, UpdatedAt: now},
	}
	bulkEvents := []domain.AssetLifecycleEvent{
		{ID: lifecycleID(suffix, '3'), OrganizationID: organizationID, AssetID: bulkAssets[0].ID, ToStatus: "draft", Revision: 1, ActorID: "contract-user", OccurredAt: now},
		{ID: lifecycleID(suffix, '4'), OrganizationID: organizationID, AssetID: bulkAssets[1].ID, ToStatus: "draft", Revision: 1, ActorID: "contract-user", OccurredAt: now},
	}
	createdBulk, err := subject.CreateAssets(ctx, bulkAssets, bulkEvents)
	if err != nil || len(createdBulk) != 2 || createdBulk[0].DeploymentNotes != "Rack staging" {
		t.Fatalf("unexpected bulk Atlas create %#v err=%v", createdBulk, err)
	}
	failedBatch := []domain.Asset{
		{ID: "bulk-c-" + suffix, OrganizationID: organizationID, ModelID: model.ID, ModelContext: contractModelContext(model, "server", now), Name: "Bulk node C", Kind: "server", AssetTag: "BULK-C-" + suffix, Status: "draft", Revision: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "bulk-d-" + suffix, OrganizationID: organizationID, ModelID: model.ID, ModelContext: contractModelContext(model, "server", now), Name: "Bulk duplicate", Kind: "server", AssetTag: asset.AssetTag, Status: "draft", Revision: 1, CreatedAt: now, UpdatedAt: now},
	}
	failedEvents := []domain.AssetLifecycleEvent{
		{ID: lifecycleID(suffix, '5'), OrganizationID: organizationID, AssetID: failedBatch[0].ID, ToStatus: "draft", Revision: 1, ActorID: "contract-user", OccurredAt: now},
		{ID: lifecycleID(suffix, '6'), OrganizationID: organizationID, AssetID: failedBatch[1].ID, ToStatus: "draft", Revision: 1, ActorID: "contract-user", OccurredAt: now},
	}
	if _, err := subject.CreateAssets(ctx, failedBatch, failedEvents); !errors.Is(err, atlas.ErrConflict) {
		t.Fatalf("expected atomic bulk conflict, got %v", err)
	}
	if _, err := subject.GetAsset(ctx, organizationID, failedBatch[0].ID); !errors.Is(err, atlas.ErrNotFound) {
		t.Fatalf("expected failed bulk create to roll back, got %v", err)
	}
	model.Name = "Updated Server"
	model.Revision = 2
	model.UpdatedAt = now.Add(30 * time.Minute)
	updatedModel, err := subject.UpdateModel(ctx, model, 1)
	if err != nil || updatedModel.Revision != 2 {
		t.Fatalf("unexpected updated Atlas model %#v err=%v", updatedModel, err)
	}
	models, err := subject.ListModels(ctx, organizationID, atlas.ModelQuery{Search: "updated", Status: "active", Limit: 10})
	if err != nil || len(models) != 1 || models[0].InstanceCount != 3 {
		t.Fatalf("unexpected Atlas model search %#v err=%v", models, err)
	}
	preserved, err := subject.GetAsset(ctx, organizationID, asset.ID)
	if err != nil || preserved.ModelContext == nil || preserved.ModelContext.ModelRevision != 1 || preserved.ModelContext.Name != "Server" {
		t.Fatalf("model update rewrote persisted asset context %#v err=%v", preserved.ModelContext, err)
	}
	if _, err := subject.CreateAsset(ctx, asset, initial); !errors.Is(err, atlas.ErrConflict) {
		t.Fatalf("expected duplicate Atlas conflict, got %v", err)
	}
	items, err := subject.ListAssets(ctx, organizationID, atlas.Query{Search: "CONTRACT", Kind: "server", Status: "draft", ModelID: model.ID, Limit: 10})
	if err != nil || len(items) != 1 || items[0].ID != assetID {
		t.Fatalf("unexpected Atlas search result %#v err=%v", items, err)
	}
	graphItems, err := subject.ListGraphAssets(ctx, organizationID, atlas.GraphAssetQuery{
		LabelSearch: "contract server", Visibility: atlas.GraphAssetVisibility{All: true},
		Directory: atlas.GraphAssetDirectoryVisibility{All: true}, References: atlas.GraphAssetReferences{ResourceIDs: []string{asset.ID}}, Limit: 10,
	})
	if err != nil || len(graphItems) != 1 || graphItems[0].ID != asset.ID {
		t.Fatalf("graph asset label/reference query failed: %#v err=%v", graphItems, err)
	}
	directGraphItems, err := subject.ListGraphAssets(ctx, organizationID, atlas.GraphAssetQuery{
		Visibility: atlas.GraphAssetVisibility{All: true}, Directory: atlas.GraphAssetDirectoryVisibility{All: true}, DirectOrganizationChildren: true, Limit: 10,
	})
	if err != nil || !contractAssetPresent(directGraphItems, asset.ID) {
		t.Fatalf("graph direct-child asset query failed: %#v err=%v", directGraphItems, err)
	}
	hiddenGraphItems, err := subject.ListGraphAssets(ctx, organizationID, atlas.GraphAssetQuery{
		Visibility: atlas.GraphAssetVisibility{ResourceIDs: []string{bulkAssets[0].ID}},
		Directory:  atlas.GraphAssetDirectoryVisibility{All: true}, References: atlas.GraphAssetReferences{ResourceIDs: []string{asset.ID}}, Limit: 10,
	})
	if err != nil || len(hiddenGraphItems) != 0 {
		t.Fatalf("graph asset reference widened authenticated visibility: %#v err=%v", hiddenGraphItems, err)
	}
	preLimitDirectoryItems, err := subject.ListGraphAssets(ctx, organizationID, atlas.GraphAssetQuery{
		Visibility: atlas.GraphAssetVisibility{All: true},
		Directory:  atlas.GraphAssetDirectoryVisibility{SiteIDs: []string{"graph-site-" + suffix}},
		Limit:      10,
	})
	if err != nil || len(preLimitDirectoryItems) != 0 {
		t.Fatalf("graph directory predicate failed closed incorrectly: %#v err=%v", preLimitDirectoryItems, err)
	}
	if _, err := subject.ListGraphAssets(ctx, organizationID, atlas.GraphAssetQuery{
		Visibility: atlas.GraphAssetVisibility{All: true},
		Directory:  atlas.GraphAssetDirectoryVisibility{UserIDs: []string{"user-" + suffix}, MatchUserDirectory: true},
		Limit:      10,
	}); !errors.Is(err, atlas.ErrInvalidInput) {
		t.Fatalf("expected user-only MatchUserDirectory query to fail before SQL, got %v", err)
	}
	deploymentItems, err := subject.ListAssets(ctx, organizationID, atlas.Query{ModelID: model.ID, DeploymentContext: "RACK", Limit: 10})
	if err != nil || len(deploymentItems) != 1 || deploymentItems[0].ID != bulkAssets[0].ID {
		t.Fatalf("unexpected Atlas deployment search result %#v err=%v", deploymentItems, err)
	}
	inventory, err := subject.GetModelInventory(ctx, organizationID, model.ID, atlas.ModelInventoryQuery{
		Status: "draft", DeploymentContext: "rack", GroupBy: atlas.ModelInventoryGroupDeployment, Limit: 10,
	})
	if err != nil || inventory.TotalCount != 3 || inventory.FilteredCount != 1 || len(inventory.Items) != 1 ||
		len(inventory.Groups) != 1 || inventory.Groups[0].Key != "Rack staging" || inventory.Groups[0].Count != 1 {
		t.Fatalf("unexpected Atlas model inventory %#v err=%v", inventory, err)
	}
	if _, err := subject.GetModelInventory(ctx, organizationID, "missing-model", atlas.ModelInventoryQuery{Limit: 10}); !errors.Is(err, atlas.ErrNotFound) {
		t.Fatalf("expected missing Atlas model inventory, got %v", err)
	}
	assertAuthorizedAtlasKeyset(t, subject, organizationID, suffix, now)
	asset.Name = "Updated Contract Server"
	asset.Status = "active"
	asset.Revision = 2
	asset.UpdatedAt = now.Add(time.Hour)
	changed := domain.AssetLifecycleEvent{
		ID: lifecycleID(suffix, '2'), OrganizationID: organizationID, AssetID: assetID,
		FromStatus: "draft", ToStatus: "active", Note: "Deployed", Revision: 2,
		ActorID: "contract-user", OccurredAt: asset.UpdatedAt,
	}
	updated, err := subject.UpdateAsset(ctx, asset, 1, &changed)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 2 || updated.Status != "active" {
		t.Fatalf("unexpected updated Atlas asset %#v", updated)
	}
	if _, err := subject.UpdateAsset(ctx, asset, 1, nil); !errors.Is(err, atlas.ErrConflict) {
		t.Fatalf("expected stale Atlas conflict, got %v", err)
	}
	history, err := subject.ListAssetLifecycle(ctx, organizationID, assetID)
	if err != nil || len(history) != 2 || history[0].Revision != 1 || history[1].Revision != 2 {
		t.Fatalf("unexpected Atlas lifecycle %#v err=%v", history, err)
	}
	retiredModel, err := subject.RetireModel(ctx, organizationID, model.ID, updatedModel.Revision, now.Add(2*time.Hour))
	if err != nil || retiredModel.Status != "retired" || retiredModel.InstanceCount != 3 {
		t.Fatalf("unexpected retired Atlas model %#v err=%v", retiredModel, err)
	}
	asset.Name = "Maintained after model retirement"
	asset.Revision = 3
	asset.UpdatedAt = now.Add(3 * time.Hour)
	maintained, err := subject.UpdateAsset(ctx, asset, 2, nil)
	if err != nil || maintained.Revision != 3 || maintained.ModelContext == nil || maintained.ModelContext.ModelRevision != 1 {
		t.Fatalf("existing retired-model link prevented Atlas asset maintenance %#v err=%v", maintained, err)
	}
}

func assertAuthorizedAtlasKeyset(t testing.TB, subject atlas.Store, organizationID, suffix string, now time.Time) {
	t.Helper()
	assets := make([]domain.Asset, 0, 206)
	events := make([]domain.AssetLifecycleEvent, 0, 206)
	authorizedIDs := make([]string, 0, 101)
	for index := 0; index < 206; index++ {
		authorized := index >= 105
		prefix := "a"
		if authorized {
			prefix = "z"
		}
		id := fmt.Sprintf("mcp-%s-%03d-%s", prefix, index, suffix)
		asset := domain.Asset{ID: id, OrganizationID: organizationID, Name: fmt.Sprintf("MCP authorized contract %03d", index),
			Kind: "computer", Status: "active", Revision: 1, CreatedAt: now, UpdatedAt: now}
		assets = append(assets, asset)
		events = append(events, domain.AssetLifecycleEvent{ID: fmt.Sprintf("%032x", 1000+index), OrganizationID: organizationID,
			AssetID: id, ToStatus: asset.Status, Revision: 1, ActorID: "contract-user", OccurredAt: now})
		if authorized {
			authorizedIDs = append(authorizedIDs, id)
		}
	}
	if _, err := subject.CreateAssets(context.Background(), assets, events); err != nil {
		t.Fatalf("create authorized Atlas keyset fixtures: %v", err)
	}
	visibility := atlas.GraphAssetVisibility{ResourceIDs: authorizedIDs}
	first, err := subject.ListAuthorizedAssets(context.Background(), organizationID, atlas.AuthorizedAssetQuery{
		Search: "authorized contract", Limit: 26, Visibility: visibility,
	})
	if err != nil || len(first) != 26 || first[0].ID != authorizedIDs[0] {
		t.Fatalf("unauthorized assets crowded the authorized page: %#v err=%v", first, err)
	}
	seen := make(map[string]struct{}, len(authorizedIDs))
	cursor := ""
	for {
		items, listErr := subject.ListAuthorizedAssets(context.Background(), organizationID, atlas.AuthorizedAssetQuery{
			Search: "authorized contract", Cursor: cursor, Limit: 25, Visibility: visibility,
		})
		if listErr != nil {
			t.Fatalf("page authorized Atlas keyset after %q: %v", cursor, listErr)
		}
		if len(items) == 0 {
			break
		}
		for _, item := range items {
			if _, duplicate := seen[item.ID]; duplicate || item.ID <= cursor {
				t.Fatalf("unstable authorized Atlas keyset item %#v after %q", item, cursor)
			}
			seen[item.ID] = struct{}{}
		}
		cursor = items[len(items)-1].ID
		if len(items) < 25 {
			break
		}
	}
	if len(seen) != len(authorizedIDs) {
		t.Fatalf("authorized Atlas keyset lost records: got %d want %d", len(seen), len(authorizedIDs))
	}
}

func AtlasGraphDirectoryStore(t testing.TB, subject atlas.Store, organizationID, suffix, visibleSite, hiddenSite, visibleUser string) {
	t.Helper()
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	for index := 0; index < 20; index++ {
		marker := byte(index)
		asset := domain.Asset{ID: "hidden-graph-asset-" + suffix + string(rune('a'+index)), OrganizationID: organizationID,
			Name: "Alpha Hidden Graph Asset " + string(rune('a'+index)), Kind: "computer", SiteID: hiddenSite,
			Status: "active", Revision: 1, CreatedAt: now, UpdatedAt: now}
		event := domain.AssetLifecycleEvent{ID: graphLifecycleID(suffix, marker), OrganizationID: organizationID,
			AssetID: asset.ID, ToStatus: asset.Status, Revision: 1, ActorID: "contract-user", OccurredAt: now}
		if _, err := subject.CreateAsset(context.Background(), asset, event); err != nil {
			t.Fatalf("create hidden graph asset: %v", err)
		}
	}
	visible := domain.Asset{ID: "visible-graph-asset-" + suffix, OrganizationID: organizationID, Name: "Zeta Visible Graph Asset",
		Kind: "computer", SiteID: visibleSite, Status: "active", Revision: 1, CreatedAt: now, UpdatedAt: now}
	event := domain.AssetLifecycleEvent{ID: graphLifecycleID(suffix, 20), OrganizationID: organizationID, AssetID: visible.ID,
		ToStatus: visible.Status, Revision: 1, ActorID: "contract-user", OccurredAt: now}
	if _, err := subject.CreateAsset(context.Background(), visible, event); err != nil {
		t.Fatalf("create visible graph asset: %v", err)
	}
	userAsset := domain.Asset{ID: "visible-user-graph-asset-" + suffix, OrganizationID: organizationID, Name: "Zeta User Graph Asset",
		Kind: "computer", UserID: visibleUser, Status: "active", Revision: 1, CreatedAt: now, UpdatedAt: now}
	userEvent := domain.AssetLifecycleEvent{ID: graphLifecycleID(suffix, 21), OrganizationID: organizationID, AssetID: userAsset.ID,
		ToStatus: userAsset.Status, Revision: 1, ActorID: "contract-user", OccurredAt: now}
	if _, err := subject.CreateAsset(context.Background(), userAsset, userEvent); err != nil {
		t.Fatalf("create user-linked graph asset: %v", err)
	}
	items, err := subject.ListGraphAssets(context.Background(), organizationID, atlas.GraphAssetQuery{
		Visibility: atlas.GraphAssetVisibility{All: true}, Directory: atlas.GraphAssetDirectoryVisibility{SiteIDs: []string{visibleSite}, MatchUserDirectory: true}, Limit: 10,
	})
	if err != nil || len(items) != 2 || !contractAssetPresent(items, visible.ID) || !contractAssetPresent(items, userAsset.ID) {
		t.Fatalf("out-of-directory assets crowded valid asset before source limit: %#v err=%v", items, err)
	}
}

func graphLifecycleID(suffix string, marker byte) string {
	const hexadecimal = "0123456789abcdef"
	value := make([]byte, 32)
	for index := range value {
		value[index] = hexadecimal[(index+len(suffix))%len(hexadecimal)]
	}
	value[len(value)-2] = hexadecimal[marker>>4]
	value[len(value)-1] = hexadecimal[marker&0x0f]
	return string(value)
}

func contractAssetPresent(assets []domain.Asset, id string) bool {
	for _, asset := range assets {
		if asset.ID == id {
			return true
		}
	}
	return false
}

func contractModelContext(model domain.AssetModel, kind string, appliedAt time.Time) *domain.AssetModelContext {
	overrides := []string{}
	if kind != model.Kind {
		overrides = []string{"kind"}
	}
	return &domain.AssetModelContext{
		Manufacturer: model.Manufacturer, Name: model.Name, ModelNumber: model.ModelNumber, Kind: model.Kind,
		Specifications: map[string]string{"CPU": "test"}, WarrantyMonths: model.WarrantyMonths,
		UsefulLifeMonths: model.UsefulLifeMonths, ModelRevision: model.Revision,
		DefaultsEffectiveAt: model.UpdatedAt, AppliedAt: appliedAt, Overrides: overrides,
	}
}

func lifecycleID(suffix string, marker byte) string {
	value := make([]byte, 32)
	for index := range value {
		value[index] = 'a'
	}
	value[len(value)-1] = marker
	if len(suffix) > 0 {
		value[len(value)-2] = "0123456789abcdef"[len(suffix)%16]
	}
	return string(value)
}
