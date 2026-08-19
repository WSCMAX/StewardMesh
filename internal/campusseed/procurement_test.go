package campusseed

import "testing"

func TestVendorFollowsTheLabManufacturer(t *testing.T) {
	if got := vendorForModelSlug("imac-m3"); got != "apple" {
		t.Fatalf("mac lab should purchase from Apple, got %q", got)
	}
	if got := vendorForModelSlug("dell-precision-7865"); got != "dell" {
		t.Fatalf("windows lab should purchase from Dell, got %q", got)
	}
	if got := vendorForModelSlug("lenovo-thinkpad-t14"); got != "cdw" {
		t.Fatalf("non-Apple/Dell fleet should purchase through CDW, got %q", got)
	}
}

func TestHardwareOrdersAssignEachAssetOnce(t *testing.T) {
	seeder := &Seeder{
		labAssetsBySlug: map[string][]string{
			"studio-arts-mac": {"lab-a", "lab-b"},
			"cs-programming":  {"lab-c"},
		},
		officeAssetsByBuilding: map[string][]string{
			"education": {"office-a"},
			"library":   {"office-b"},
		},
		laptopAssetsByVendor: map[string][]string{
			"dell": {"laptop-dell"},
			"cdw":  {"laptop-cdw"},
		},
		shopDesktopIDs: []string{"shop-a"},
		tabletAssetIDs: []string{"ipad-a"},
	}
	orders := seeder.hardwareOrders()
	if len(orders) < 8 {
		t.Fatalf("expected per-lab and per-vendor orders, got %d", len(orders))
	}
	groups := make([][]string, 0, len(orders))
	var labOrder hardwareOrder
	for _, order := range orders {
		groups = append(groups, order.AssetIDs)
		if order.ID == "po-lab-studio-arts-mac" {
			labOrder = order
		}
	}
	if labOrder.Vendor != "apple" || labOrder.Number != "PO-2026-L01" {
		t.Fatalf("studio arts lab should be a single Apple PO, got %+v", labOrder)
	}
	if dup := firstDuplicateAsset(groups); dup != "" {
		t.Fatalf("asset %q belongs to more than one purchase order", dup)
	}
}

func TestDuplicateAssetHelper(t *testing.T) {
	if firstDuplicateAsset([][]string{{"a", "b"}, {"c"}}) != "" {
		t.Fatal("expected unique groups")
	}
	if firstDuplicateAsset([][]string{{"a"}, {"a"}}) != "a" {
		t.Fatal("expected duplicate a")
	}
}
