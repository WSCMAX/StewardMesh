package campusseed

import "github.com/maxlemke/stewardmesh/internal/domain"

func computerTemplateFields(kind string) []domain.AssetTemplateField {
	switch kind {
	case "peripheral":
		return []domain.AssetTemplateField{
			{Key: "modelNumber", Label: "Model number", Kind: "text", Required: true, Help: "Manufacturer model or SKU."},
			{Key: "condition", Label: "Condition", Kind: "select", Options: []string{"new", "good", "fair", "spare"}, Default: "new"},
		}
	case "tablet":
		return []domain.AssetTemplateField{
			{Key: "cellular", Label: "Cellular plan", Kind: "select", Options: []string{"wifi", "cellular"}, Default: "wifi"},
			{Key: "caseColor", Label: "Case color", Kind: "text"},
		}
	default:
		return []domain.AssetTemplateField{
			{Key: "hostnamePattern", Label: "Hostname pattern", Kind: "text", Help: "Expected DNS name for this station."},
			{Key: "imageBuild", Label: "Image build", Kind: "text", Help: "Campus image identifier."},
			{Key: "primaryUse", Label: "Primary use", Kind: "select", Options: []string{"instruction", "office", "checkout", "research", "admin"}, Default: "instruction"},
		}
	}
}

func stationAccessories(kind string, includeDock bool) []domain.AssetComponent {
	components := []domain.AssetComponent{
		{ID: "monitor", Kind: "monitor", Name: "Dell P2422H", ModelNumber: "P2422H", ModelID: "dell-p2422h", Quantity: 1, UnitCostMinor: 21900, Currency: "USD"},
		{ID: "combo", Kind: "combo", Name: "Logitech MK270 mouse and keyboard", ModelNumber: "MK270", ModelID: "logitech-mk270", Quantity: 1, UnitCostMinor: 2999, Currency: "USD"},
	}
	if kind == "laptop" || includeDock {
		components = append(components, domain.AssetComponent{
			ID: "dock", Kind: "dock", Name: "Dell WD19 docking station", ModelNumber: "WD19", ModelID: "dell-wd19",
			Quantity: 1, UnitCostMinor: 18900, Currency: "USD",
		})
	}
	return components
}
