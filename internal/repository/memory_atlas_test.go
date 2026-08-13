package repository_test

// Requirements: REQ-ATLAS-001, REQ-DIRECTORY-EXPANSION-008. Features: inventory.assets, threads.relationships.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/repository/contracttest"
)

// Requirements: REQ-ATLAS-001, REQ-ATLAS-MODELS-001.
func TestMemoryAtlasStoreContract(t *testing.T) {
	contracttest.AtlasStore(t, repository.NewMemoryAtlasStore(), "memory-atlas", "memory")
}

func TestMemoryAtlasGraphAssetSearchIsLabelOnlyBeforeLimit(t *testing.T) {
	store := repository.NewMemoryAtlasStore()
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	for index := 0; index <= atlas.MaximumGraphAssetLimit; index++ {
		name := fmt.Sprintf("Alpha Asset %03d", index)
		if index == atlas.MaximumGraphAssetLimit {
			name = "Zeta Needle Asset"
		}
		asset := domain.Asset{ID: fmt.Sprintf("graph-asset-%03d", index), OrganizationID: "graph-org", Name: name,
			Kind: "computer", AssetTag: fmt.Sprintf("NEEDLE-%03d", index), Status: "active", Revision: 1, CreatedAt: now, UpdatedAt: now}
		event := domain.AssetLifecycleEvent{ID: fmt.Sprintf("graph-event-%03d", index), OrganizationID: "graph-org", AssetID: asset.ID,
			ToStatus: "active", Revision: 1, ActorID: "tester", OccurredAt: now}
		if _, err := store.CreateAsset(context.Background(), asset, event); err != nil {
			t.Fatal(err)
		}
	}
	items, err := store.ListGraphAssets(context.Background(), "graph-org", atlas.GraphAssetQuery{
		LabelSearch: "needle", Visibility: atlas.GraphAssetVisibility{All: true}, Limit: 10,
	})
	if err != nil || len(items) != 1 || items[0].Name != "Zeta Needle Asset" {
		t.Fatalf("hidden asset-tag matches crowded out graph label: %#v err=%v", items, err)
	}
}
