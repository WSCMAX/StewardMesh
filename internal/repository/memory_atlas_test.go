package repository_test

// Requirements: REQ-ATLAS-001, REQ-DIRECTORY-EXPANSION-008. Features: inventory.assets, threads.relationships.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/repository/contracttest"
)

// Requirements: REQ-ATLAS-001, REQ-ATLAS-MODELS-001.
func TestMemoryAtlasStoreContract(t *testing.T) {
	peopleStore := repository.NewMemoryPeopleStore()
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	if _, err := peopleStore.CreateSite(context.Background(), people.Site{ID: "visible-site", OrganizationID: "memory-atlas", Name: "Visible Site",
		NormalizedName: "visible site", Status: people.StatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := peopleStore.CreateIdentity(context.Background(), people.Identity{ID: "visible-user", OrganizationID: "memory-atlas", Kind: people.IdentityPerson,
		DisplayName: "Visible User", NormalizedName: "visible user", SiteID: "visible-site", Status: people.StatusActive,
		Revision: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	store := repository.NewMemoryAtlasStoreWithPeople(peopleStore)
	contracttest.AtlasStore(t, store, "memory-atlas", "memory")
	contracttest.AtlasGraphDirectoryStore(t, store, "memory-atlas", "memory-graph", "visible-site", "hidden-site", "visible-user")
}

func TestMemoryAtlasGraphAssetSearchIsLabelOnlyBeforeLimit(t *testing.T) {
	store := repository.NewMemoryAtlasStore()
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	for index := 0; index <= 500; index++ {
		name := fmt.Sprintf("Alpha Asset %03d", index)
		if index == 500 {
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
		LabelSearch: "needle", Visibility: atlas.GraphAssetVisibility{All: true}, Directory: atlas.GraphAssetDirectoryVisibility{All: true}, Limit: 10,
	})
	if err != nil || len(items) != 1 || items[0].Name != "Zeta Needle Asset" {
		t.Fatalf("hidden asset-tag matches crowded out graph label: %#v err=%v", items, err)
	}
}

func TestMemoryAtlasGraphAssetDirectOrganizationChildrenExcludeLinkedAssets(t *testing.T) {
	store := repository.NewMemoryAtlasStore()
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	for _, asset := range []domain.Asset{
		{ID: "root-asset", OrganizationID: "graph-org", Name: "Root Asset", Kind: "computer", Status: "active", Revision: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "linked-asset", OrganizationID: "graph-org", Name: "Linked Asset", Kind: "computer", UserID: "user-a", Status: "active", Revision: 1, CreatedAt: now, UpdatedAt: now},
	} {
		event := domain.AssetLifecycleEvent{ID: "event-" + asset.ID, OrganizationID: asset.OrganizationID, AssetID: asset.ID,
			ToStatus: asset.Status, Revision: 1, ActorID: "tester", OccurredAt: now}
		if _, err := store.CreateAsset(context.Background(), asset, event); err != nil {
			t.Fatal(err)
		}
	}
	items, err := store.ListGraphAssets(context.Background(), "graph-org", atlas.GraphAssetQuery{
		Visibility: atlas.GraphAssetVisibility{All: true}, Directory: atlas.GraphAssetDirectoryVisibility{All: true}, DirectOrganizationChildren: true, Limit: 10,
	})
	if err != nil || len(items) != 1 || items[0].ID != "root-asset" {
		t.Fatalf("direct-child graph query included a linked asset: %#v err=%v", items, err)
	}
}
