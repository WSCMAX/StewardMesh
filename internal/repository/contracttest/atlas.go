package contracttest

// Provider-neutral Atlas adapter contract. Requirement: REQ-ATLAS-001.

import (
	"context"
	"errors"
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
	asset := domain.Asset{
		ID: assetID, OrganizationID: organizationID, Name: "Contract Server", Kind: "server",
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
	if _, err := subject.CreateAsset(ctx, asset, initial); !errors.Is(err, atlas.ErrConflict) {
		t.Fatalf("expected duplicate Atlas conflict, got %v", err)
	}
	items, err := subject.ListAssets(ctx, organizationID, atlas.Query{Search: "CONTRACT", Kind: "server", Status: "draft", Limit: 10})
	if err != nil || len(items) != 1 || items[0].ID != assetID {
		t.Fatalf("unexpected Atlas search result %#v err=%v", items, err)
	}
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
