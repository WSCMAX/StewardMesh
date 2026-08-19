package campusseed

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/ledger"
)

type hardwareOrder struct {
	ID          string
	Number      string
	Vendor      string
	Description string
	AssetIDs    []string
	UnitCost    int64
	Documents   []string
	Docks       bool
}

func (s *Seeder) seedProcurement(ctx context.Context) error {
	if err := s.seedVendors(ctx); err != nil {
		return err
	}
	orderedOn := time.Date(s.now().Year(), 7, 15, 0, 0, 0, 0, time.UTC)
	hardware := s.hardwareOrders()
	for index, order := range hardware {
		if err := s.createHardwarePO(ctx, order, orderedOn.Add(time.Duration(index)*24*time.Hour)); err != nil {
			return err
		}
	}
	softwareStart := orderedOn.Add(time.Duration(len(hardware)) * 24 * time.Hour)
	if err := s.createSoftwarePOs(ctx, softwareStart); err != nil {
		return err
	}
	return s.createWarrantyPO(ctx, softwareStart.Add(24*time.Hour))
}

func (s *Seeder) hardwareOrders() []hardwareOrder {
	orders := make([]hardwareOrder, 0, len(campusLabs)+len(campusBuildings)+6)
	for index, lab := range campusLabs {
		model := campusModels[modelSlugIndex(lab.ModelSlug)]
		orders = append(orders, hardwareOrder{
			ID:          "po-lab-" + lab.Slug,
			Number:      fmt.Sprintf("PO-2026-L%02d", index+1),
			Vendor:      vendorForModelSlug(lab.ModelSlug),
			Description: fmt.Sprintf("%s computers (%s)", lab.Name, model.Name),
			AssetIDs:    s.labAssetsBySlug[lab.Slug],
			UnitCost:    model.UnitCostMinor,
			Documents:   []string{"po-hardware-pdf", "packing-slip-png", "asset-photo-png"},
			Docks:       model.Kind == "laptop",
		})
	}
	for index, building := range campusBuildings {
		ids := s.officeAssetsByBuilding[building.Slug]
		if len(ids) == 0 {
			continue
		}
		orders = append(orders, hardwareOrder{
			ID:          "po-office-" + building.Slug,
			Number:      fmt.Sprintf("PO-2026-O%02d", index+1),
			Vendor:      "dell",
			Description: fmt.Sprintf("%s office desktops", building.Name),
			AssetIDs:    ids,
			UnitCost:    98900,
			Documents:   []string{"po-hardware-pdf"},
		})
	}
	for _, vendor := range []string{"dell", "cdw"} {
		ids := s.laptopAssetsByVendor[vendor]
		if len(ids) == 0 {
			continue
		}
		label := "Dell"
		if vendor == "cdw" {
			label = "Lenovo and HP"
		}
		orders = append(orders, hardwareOrder{
			ID:          "po-laptops-" + vendor,
			Number:      "PO-2026-LTP-" + strings.ToUpper(vendor),
			Vendor:      vendor,
			Description: label + " faculty and staff laptops",
			AssetIDs:    ids,
			UnitCost:    124900,
			Documents:   []string{"po-laptops-pdf"},
			Docks:       true,
		})
	}
	if len(s.shopDesktopIDs) > 0 {
		orders = append(orders, hardwareOrder{
			ID: "po-shop-desktops", Number: "PO-2026-SHOP", Vendor: "dell",
			Description: "Custodial shop shared desktops", AssetIDs: s.shopDesktopIDs,
			UnitCost: 98900, Documents: []string{"po-hardware-pdf"},
		})
	}
	if len(s.tabletAssetIDs) > 0 {
		orders = append(orders, hardwareOrder{
			ID: "po-field-tablets", Number: "PO-2026-IPAD", Vendor: "apple",
			Description: "Field and custodial iPads", AssetIDs: s.tabletAssetIDs,
			UnitCost: 79900, Documents: []string{"po-hardware-pdf", "asset-photo-png"},
		})
	}
	return orders
}

func vendorForModelSlug(modelSlug string) string {
	switch strings.ToLower(campusModels[modelSlugIndex(modelSlug)].Manufacturer) {
	case "apple":
		return "apple"
	case "dell":
		return "dell"
	default:
		return "cdw"
	}
}

func firstDuplicateAsset(groups [][]string) string {
	seen := map[string]struct{}{}
	for _, group := range groups {
		for _, id := range group {
			if id == "" {
				continue
			}
			if _, exists := seen[id]; exists {
				return id
			}
			seen[id] = struct{}{}
		}
	}
	return ""
}

func (s *Seeder) seedVendors(ctx context.Context) error {
	vendors := []struct{ slug, name string }{
		{"cdw", "CDW Government"},
		{"dell", "Dell Technologies"},
		{"apple", "Apple Education"},
		{"microsoft", "Microsoft"},
		{"adobe", "Adobe"},
		{"autodesk", "Autodesk"},
		{"logitech", "Logitech"},
	}
	for _, vendor := range vendors {
		created, err := s.ledger.CreateVendor(ctx, ledger.CreateVendorInput{
			ID: vendor.slug, Name: vendor.name, ExternalID: "campus-" + vendor.slug, Status: "active",
		})
		if err != nil && !errors.Is(err, ledger.ErrConflict) {
			return fmt.Errorf("create vendor %q: %w", vendor.name, err)
		}
		vendorID := vendor.slug
		if err == nil {
			vendorID = created.ID
		}
		s.vendorIDs[vendor.slug] = vendorID
	}
	return nil
}

func (s *Seeder) createHardwarePO(ctx context.Context, order hardwareOrder, orderedOn time.Time) error {
	if len(order.AssetIDs) == 0 {
		return nil
	}
	count := int64(len(order.AssetIDs))
	unit := order.UnitCost
	if unit <= 0 {
		unit = 125000
	}
	lines := []ledger.PurchaseOrderLine{{
		ID: order.ID + "-hardware", Description: order.Description, Kind: "asset",
		Quantity: count, UnitCostMinor: unit, AmountMinor: count * unit,
	}, {
		ID: order.ID + "-monitor", Description: "Dell P2422H monitors", Kind: "accessory",
		Quantity: count, UnitCostMinor: 21900, AmountMinor: count * 21900,
	}, {
		ID: order.ID + "-combo", Description: "Logitech MK270 mouse and keyboard combos", Kind: "accessory",
		Quantity: count, UnitCostMinor: 2999, AmountMinor: count * 2999,
	}}
	if order.Docks {
		lines = append(lines, ledger.PurchaseOrderLine{
			ID: order.ID + "-dock", Description: "Dell WD19 docking stations", Kind: "accessory",
			Quantity: count, UnitCostMinor: 18900, AmountMinor: count * 18900,
		})
	}
	return s.createPO(ctx, order.ID, order.Number, order.Vendor, order.AssetIDs, order.Documents, lines, orderedOn)
}

func (s *Seeder) createWarrantyPO(ctx context.Context, orderedOn time.Time) error {
	lines := []ledger.PurchaseOrderLine{{
		ID: "po-warranty-hardware", Description: "Three-year campus hardware warranty addendum", Kind: "other",
		Quantity: 1, UnitCostMinor: 18500000, AmountMinor: 18500000,
	}}
	return s.createPO(ctx, "po-warranty-campus", "PO-2026-WTY", "cdw", nil, []string{"warranty-pdf"}, lines, orderedOn)
}

func (s *Seeder) createSoftwarePOs(ctx context.Context, orderedOn time.Time) error {
	orders := []struct {
		id, number, vendor, description, document, offering string
		quantity, unit                                      int64
	}{
		{"po-2026-0501", "PO-2026-0501", "microsoft", "Microsoft 365 A3 volume license", "m365-contract-docx", "microsoft-365-a3", 22000, 14400},
		{"po-2026-0502", "PO-2026-0502", "adobe", "Adobe Creative Cloud All Apps", "adobe-contract-html", "adobe-creative-cloud-all-apps", 1800, 35900},
		{"po-2026-0503", "PO-2026-0503", "autodesk", "Autodesk Education Lab Pack", "autodesk-quote-pdf", "autodesk-education-lab", 500, 85000},
	}
	for index, order := range orders {
		lines := []ledger.PurchaseOrderLine{{
			ID: order.id + "-license", Description: order.description, Kind: "software", LicenseID: order.offering,
			Quantity: order.quantity, UnitCostMinor: order.unit, AmountMinor: order.quantity * order.unit,
		}}
		if err := s.createPO(ctx, order.id, order.number, order.vendor, nil, []string{order.document}, lines, orderedOn.Add(time.Duration(index)*24*time.Hour)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Seeder) createPO(ctx context.Context, id, number, vendorSlug string, assetIDs, documentSlugs []string, lines []ledger.PurchaseOrderLine, orderedOn time.Time) error {
	documents := make([]string, 0, len(documentSlugs))
	for _, slug := range documentSlugs {
		if blobID := s.blobIDs[slug]; blobID != "" {
			documents = append(documents, blobID)
		}
	}
	created, err := s.ledger.CreatePurchaseOrder(ctx, ledger.CreatePurchaseOrderInput{
		ID: id, Number: number, VendorID: s.vendorIDs[vendorSlug], Status: "received",
		Currency: "USD", OrderedOn: &orderedOn, AssetIDs: assetIDs, ReceiptDocumentIDs: documents, Lines: lines,
	})
	if err != nil && !errors.Is(err, ledger.ErrConflict) {
		return fmt.Errorf("create purchase order %q: %w", number, err)
	}
	poID := id
	if err == nil {
		poID = created.ID
	}
	_, costErr := s.ledger.ReconcileCost(ctx, ledger.ReconcileCostInput{
		ID: poID + "-cost", Description: number + " received hardware and software",
		Kind: "actual", Currency: "USD", AmountMinor: createdOrLineTotal(created, lines),
		FiscalPeriod: fmt.Sprintf("FY%d", s.now().Year()), Scenario: "baseline",
		PurchaseOrderID: poID, DocumentID: firstString(documents),
		SourceSystemID: SourceSystemID, SourceRecordID: poID + "-cost",
	})
	if costErr != nil {
		return fmt.Errorf("reconcile cost for %q: %w", number, costErr)
	}
	return nil
}

func createdOrLineTotal(created ledger.PurchaseOrder, lines []ledger.PurchaseOrderLine) int64 {
	if created.TotalMinor > 0 {
		return created.TotalMinor
	}
	var total int64
	for _, line := range lines {
		total += line.AmountMinor
	}
	return total
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func softwareVendorID(productSlug string, vendorIDs map[string]string) string {
	switch productSlug {
	case "microsoft-365":
		return vendorIDs["microsoft"]
	case "adobe-creative-cloud":
		return vendorIDs["adobe"]
	case "autodesk-education":
		return vendorIDs["autodesk"]
	default:
		return ""
	}
}

func softwarePurchaseOrderID(offeringSlug string) string {
	switch offeringSlug {
	case "microsoft-365-a3":
		return "po-2026-0501"
	case "adobe-creative-cloud-all-apps":
		return "po-2026-0502"
	case "autodesk-education-lab":
		return "po-2026-0503"
	default:
		return ""
	}
}

func softwareDocumentIDs(productSlug string, blobIDs map[string]string) []string {
	slug := ""
	switch productSlug {
	case "microsoft-365":
		slug = "m365-contract-docx"
	case "adobe-creative-cloud":
		slug = "adobe-contract-html"
	case "autodesk-education":
		slug = "autodesk-quote-pdf"
	}
	if id := blobIDs[slug]; id != "" {
		return []string{id}
	}
	return nil
}
