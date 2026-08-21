package campusseed

// Requirement: REQ-HORIZON-001. Feature: lifecycle.planning.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/horizon"
)

type LifecycleDependencies struct {
	OrganizationID string
	Atlas          *atlas.Service
	Horizon        *horizon.Service
}

type campusAssetRef struct {
	ID        string
	Kind      string
	ModelSlug string
	AgeIndex  int
	Profile   string
}

func defaultModelCriticality(definition modelDef) int {
	if definition.CriticalityScore > 0 {
		return definition.CriticalityScore
	}
	switch definition.Kind {
	case "laptop", "tablet":
		return 4
	case "desktop":
		return 3
	default:
		return 2
	}
}

func SeedLifecycle(ctx context.Context, dependencies LifecycleDependencies) error {
	organizationID := strings.ToLower(strings.TrimSpace(dependencies.OrganizationID))
	if ctx == nil || !strings.HasPrefix(organizationID, "demo-") || dependencies.Atlas == nil || dependencies.Horizon == nil {
		return errors.New("campus lifecycle seeding requires a demo-* organization and initialized Atlas and Horizon services")
	}
	correlationID, err := foundation.NewCorrelationID()
	if err != nil {
		return fmt.Errorf("create campus lifecycle correlation id: %w", err)
	}
	ctx = foundation.WithScope(ctx, foundation.Scope{
		OrganizationID: dependencies.OrganizationID,
		ActorID:        ActorID,
		CorrelationID:  correlationID,
	})

	modelIDs, err := resolveCampusModelIDs(ctx, dependencies.Atlas)
	if err != nil {
		return err
	}
	if err := seedCampusModelLifecycle(ctx, dependencies.Atlas, modelIDs); err != nil {
		return err
	}
	if err := seedCampusKindDefaults(ctx, dependencies.Horizon, modelIDs); err != nil {
		return err
	}
	refs := campusAssetRefs()
	existingPlans, err := dependencies.Horizon.ListPlans(ctx, horizon.ListPlansQuery{Scenario: "baseline"})
	if err != nil {
		return fmt.Errorf("list campus Horizon plans: %w", err)
	}
	plannedAssets := make(map[string]struct{}, len(existingPlans))
	for _, plan := range existingPlans {
		plannedAssets[plan.AssetID] = struct{}{}
	}

	now := time.Now().UTC()
	effectiveFrom := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	for _, ref := range refs {
		purchase, installed, lifecycleStart := lifecycleDatesForRef(ref, now)
		replacementModelID := replacementModelID(ref.ModelSlug, ref.Kind, modelIDs)
		if err := patchCampusAssetLifecycle(ctx, dependencies.Atlas, ref, purchase, installed, lifecycleStart, replacementModelID); err != nil {
			if errors.Is(err, atlas.ErrNotFound) {
				continue
			}
			return err
		}
		if _, exists := plannedAssets[ref.ID]; exists {
			continue
		}
		asset, err := dependencies.Atlas.GetAsset(ctx, ref.ID)
		if err != nil {
			if errors.Is(err, atlas.ErrNotFound) {
				continue
			}
			return fmt.Errorf("load campus asset %q: %w", ref.ID, err)
		}
		var linkedModel *domain.AssetModel
		if asset.ModelID != "" {
			model, modelErr := dependencies.Atlas.GetModel(ctx, asset.ModelID)
			if modelErr == nil {
				linkedModel = &model
			}
		}
		stage := horizonLifecycleStage(asset, linkedModel, now)
		costMinor := asset.UnitCostMinor
		if costMinor <= 0 {
			costMinor = campusModels[modelSlugIndex(ref.ModelSlug)].UnitCostMinor
		}
		if _, err := dependencies.Horizon.CreatePlan(ctx, horizon.CreatePlanInput{
			ID:                       ref.ID + "-baseline",
			AssetID:                  ref.ID,
			Scenario:                 "baseline",
			ExpectedUsefulLifeMonths: 0,
			LifecycleStage:           stage,
			ReplacementCostMinor:     costMinor,
			Currency:                 "USD",
			EffectiveFrom:            effectiveFrom,
		}); err != nil && !errors.Is(err, horizon.ErrConflict) {
			return fmt.Errorf("create Horizon plan for %q: %w", ref.ID, err)
		}
	}
	return nil
}

func resolveCampusModelIDs(ctx context.Context, atlasService *atlas.Service) (map[string]string, error) {
	modelIDs := make(map[string]string, len(campusModels))
	for _, definition := range campusModels {
		models, err := atlasService.ListModels(ctx, atlas.ModelQuery{Search: definition.Name, Limit: 5})
		if err != nil {
			return nil, fmt.Errorf("list campus model %q: %w", definition.Name, err)
		}
		for _, model := range models {
			if strings.EqualFold(model.Name, definition.Name) {
				modelIDs[definition.Slug] = model.ID
				break
			}
		}
		if modelIDs[definition.Slug] == "" {
			modelIDs[definition.Slug] = definition.Slug
		}
	}
	return modelIDs, nil
}

func seedCampusModelLifecycle(ctx context.Context, atlasService *atlas.Service, modelIDs map[string]string) error {
	updates := map[string]struct {
		UsefulLifeMonths     int
		LastEffectiveDate    *time.Time
		ReplacementModelSlug string
		CriticalityScore     int
		RetireAfterUpdate    bool
	}{
		"dell-latitude-5540":       {UsefulLifeMonths: 48, LastEffectiveDate: dateUTC(2026, time.June, 1), ReplacementModelSlug: "dell-latitude-7440", CriticalityScore: 4},
		"hp-elitebook-840":          {UsefulLifeMonths: 48, LastEffectiveDate: dateUTC(2025, time.December, 1), ReplacementModelSlug: "dell-latitude-7440", CriticalityScore: 4},
		"lenovo-thinkpad-t14-gen3":  {UsefulLifeMonths: 48, LastEffectiveDate: dateUTC(2024, time.January, 1), ReplacementModelSlug: "dell-latitude-7440", CriticalityScore: 4, RetireAfterUpdate: true},
		"lenovo-thinkpad-t14":       {UsefulLifeMonths: 48, LastEffectiveDate: dateUTC(2025, time.September, 1), ReplacementModelSlug: "dell-latitude-7440", CriticalityScore: 4, RetireAfterUpdate: true},
		"dell-optiplex-7020":        {UsefulLifeMonths: 60, CriticalityScore: 3},
		"dell-latitude-7440":        {UsefulLifeMonths: 48, CriticalityScore: 4},
		"imac-m3":                   {UsefulLifeMonths: 60, CriticalityScore: 5},
		"mac-studio-m2":             {UsefulLifeMonths: 72, CriticalityScore: 5},
		"macbook-pro-m3":            {UsefulLifeMonths: 48, CriticalityScore: 5},
		"dell-precision-7865":       {UsefulLifeMonths: 72, CriticalityScore: 5},
		"apple-ipad-air":            {UsefulLifeMonths: 36, CriticalityScore: 3},
	}
	for slug, patch := range updates {
		modelID := modelIDs[slug]
		if modelID == "" {
			continue
		}
		model, err := atlasService.GetModel(ctx, modelID)
		if err != nil {
			if errors.Is(err, atlas.ErrNotFound) {
				continue
			}
			return fmt.Errorf("load campus model %q: %w", slug, err)
		}
		usefulLife := patch.UsefulLifeMonths
		if usefulLife <= 0 {
			usefulLife = model.UsefulLifeMonths
		}
		criticality := patch.CriticalityScore
		if criticality <= 0 {
			criticality = model.CriticalityScore
		}
		replacementModelID := ""
		if patch.ReplacementModelSlug != "" {
			replacementModelID = modelIDs[patch.ReplacementModelSlug]
		}
		if model.Status == "retired" {
			continue
		}
		updated, err := atlasService.UpdateModel(ctx, atlas.UpdateModelInput{
			ID: model.ID, Manufacturer: model.Manufacturer, Name: model.Name, ModelNumber: model.ModelNumber,
			Kind: model.Kind, VendorIdentifier: model.VendorIdentifier, Specifications: model.Specifications,
			TemplateFields: model.TemplateFields, SupportURL: model.SupportURL, WarrantyMonths: model.WarrantyMonths,
			UsefulLifeMonths: usefulLife, LastEffectiveDate: patch.LastEffectiveDate, ReplacementModelID: replacementModelID,
			CriticalityScore: criticality, UnitCostMinor: model.UnitCostMinor, Currency: model.Currency,
			SourceSystemID: model.SourceSystemID, SourceRecordID: model.SourceRecordID, Revision: model.Revision,
		})
		if err != nil && !errors.Is(err, atlas.ErrConflict) {
			return fmt.Errorf("update campus model %q: %w", slug, err)
		}
		if patch.RetireAfterUpdate && updated.Status == "active" {
			if _, err := atlasService.RetireModel(ctx, updated.ID, updated.Revision); err != nil && !errors.Is(err, atlas.ErrConflict) {
				return fmt.Errorf("retire campus model %q: %w", slug, err)
			}
		}
	}
	return nil
}

func seedCampusKindDefaults(ctx context.Context, horizonService *horizon.Service, modelIDs map[string]string) error {
	defaults := []horizon.UpsertKindDefaultInput{
		{AssetKind: "desktop", Scenario: "baseline", ExpectedUsefulLifeMonths: 60, ReplacementModelID: modelIDs["dell-optiplex-7020"]},
		{AssetKind: "laptop", Scenario: "baseline", ExpectedUsefulLifeMonths: 48, ReplacementModelID: modelIDs["dell-latitude-7440"]},
		{AssetKind: "tablet", Scenario: "baseline", ExpectedUsefulLifeMonths: 36, ReplacementModelID: modelIDs["apple-ipad-air"]},
		{AssetKind: "peripheral", Scenario: "baseline", ExpectedUsefulLifeMonths: 84},
	}
	for _, item := range defaults {
		if _, err := horizonService.UpsertKindDefault(ctx, item); err != nil {
			return fmt.Errorf("upsert Horizon kind default for %q: %w", item.AssetKind, err)
		}
	}
	return nil
}

func patchCampusAssetLifecycle(ctx context.Context, atlasService *atlas.Service, ref campusAssetRef, purchase, installed, lifecycleStart time.Time, replacementModelID string) error {
	asset, err := atlasService.GetAsset(ctx, ref.ID)
	if err != nil {
		return err
	}
	targetStatus := assetStatusForProfile(ref.Profile)
	targetCriticality := asset.CriticalityScore
	if targetCriticality <= 0 && ref.ModelSlug != "" {
		targetCriticality = campusModels[modelSlugIndex(ref.ModelSlug)].CriticalityScore
		if targetCriticality <= 0 {
			targetCriticality = defaultModelCriticality(campusModels[modelSlugIndex(ref.ModelSlug)])
		}
	}
	if asset.PurchaseDate != nil && asset.LifecycleStartDate != nil && asset.InstalledDate != nil &&
		asset.ReplacementModelID != "" && asset.Status == targetStatus && asset.CriticalityScore == targetCriticality {
		return nil
	}
	purchaseDate := purchase
	installedDate := installed
	lifecycleStartDate := lifecycleStart
	note := lifecycleNoteForProfile(ref.Profile)
	_, err = atlasService.UpdateAsset(ctx, atlas.UpdateAssetInput{
		ID: asset.ID, ModelID: asset.ModelID, Name: asset.Name, Kind: asset.Kind,
		AssetTag: asset.AssetTag, SerialNumber: asset.SerialNumber, Hostname: asset.Hostname,
		DeploymentNotes: asset.DeploymentNotes, References: atlas.References{
			SiteID: asset.SiteID, BuildingID: asset.BuildingID, RoomID: asset.RoomID,
			DepartmentID: asset.DepartmentID, UserID: asset.UserID,
		},
		AdditionalUserIDs: asset.AdditionalUserIDs,
		Status: targetStatus, PurchaseDate: &purchaseDate, InstalledDate: &installedDate,
		LifecycleStartDate: &lifecycleStartDate, ReplacementModelID: replacementModelID,
		CriticalityScore: targetCriticality,
		Attributes: asset.Attributes, Components: asset.Components,
		UnitCostMinor: asset.UnitCostMinor, Currency: asset.Currency, Revision: asset.Revision,
		LifecycleNote: note,
	})
	return err
}

func campusAssetRefs() []campusAssetRef {
	refs := make([]campusAssetRef, 0, 4096)
	ageIndex := 0
	for _, lab := range campusLabs {
		kind := campusModels[modelSlugIndex(lab.ModelSlug)].Kind
		for station := 1; station <= lab.MachineCount; station++ {
			refs = append(refs, campusAssetRef{
				ID: fmt.Sprintf("%s-station-%03d", lab.Slug, station), Kind: kind, ModelSlug: lab.ModelSlug, AgeIndex: ageIndex,
			})
			ageIndex++
		}
	}
	for _, office := range campusOffices {
		for desk := 1; desk <= office.DeskCount; desk++ {
			refs = append(refs, campusAssetRef{
				ID: fmt.Sprintf("office-%s-desk-%02d", office.Slug, desk), Kind: "desktop", ModelSlug: "dell-optiplex-7020", AgeIndex: ageIndex,
			})
			ageIndex++
		}
	}
	for _, shop := range campusShops {
		for desk := 1; desk <= shop.SharedDesktopCount; desk++ {
			refs = append(refs, campusAssetRef{
				ID: fmt.Sprintf("shop-%s-desk-%02d", shop.Slug, desk), Kind: "desktop", ModelSlug: "dell-optiplex-7020", AgeIndex: ageIndex,
			})
			ageIndex++
		}
	}
	for _, entry := range campusWorkforceLaptops {
		kind := campusModels[modelSlugIndex(entry.ModelSlug)].Kind
		for index := 1; index <= entry.Count; index++ {
			refs = append(refs, campusAssetRef{
				ID: fmt.Sprintf("workforce-%s-%04d", entry.ModelSlug, index), Kind: kind, ModelSlug: entry.ModelSlug,
				AgeIndex: ageIndex, Profile: workforceAssetProfile(entry.ModelSlug, index),
			})
			ageIndex++
		}
	}
	for index := 1; index <= 32; index++ {
		refs = append(refs, campusAssetRef{
			ID: fmt.Sprintf("custodial-ipad-%03d", index), Kind: "tablet", ModelSlug: "apple-ipad-air", AgeIndex: ageIndex,
		})
		ageIndex++
	}
	fieldCounts := map[string]int{"it-services": 12, "facilities": 6, "campus-security": 10}
	for departmentSlug, count := range fieldCounts {
		for index := 1; index <= count; index++ {
			refs = append(refs, campusAssetRef{
				ID: fmt.Sprintf("field-ipad-%s-%03d", departmentSlug, index), Kind: "tablet", ModelSlug: "apple-ipad-air", AgeIndex: ageIndex,
			})
			ageIndex++
		}
	}
	return refs
}

func workforceAssetProfile(modelSlug string, index int) string {
	switch modelSlug {
	case "dell-latitude-5540":
		if index <= 80 {
			return "eol-active"
		}
	case "lenovo-thinkpad-t14":
		switch {
		case index <= 45:
			return "young-retired-model"
		case index <= 65:
			return "asset-retired"
		default:
			return "asset-disposed"
		}
	case "lenovo-thinkpad-t14-gen3":
		return "legacy-active"
	}
	return ""
}

func assetStatusForProfile(profile string) string {
	switch profile {
	case "asset-retired":
		return "retired"
	case "asset-disposed":
		return "disposed"
	default:
		return "active"
	}
}

func lifecycleNoteForProfile(profile string) string {
	switch profile {
	case "asset-retired":
		return "Campus demo: retired from service pending replacement"
	case "asset-disposed":
		return "Campus demo: recycled through certified e-waste vendor"
	default:
		return ""
	}
}

func lifecycleDatesForRef(ref campusAssetRef, now time.Time) (purchase, installed, lifecycleStart time.Time) {
	switch ref.Profile {
	case "eol-active":
		purchase = addCalendarMonths(normalizeDate(now), -72)
	case "young-retired-model":
		purchase = addCalendarMonths(normalizeDate(now), -36)
	case "legacy-active":
		purchase = addCalendarMonths(normalizeDate(now), -54)
	case "asset-retired", "asset-disposed":
		purchase = addCalendarMonths(normalizeDate(now), -48)
	default:
		return lifecycleDates(ref.AgeIndex, ref.Kind, now)
	}
	installed = addCalendarMonths(purchase, 0)
	installed = time.Date(installed.Year(), installed.Month(), installed.Day()+14, 0, 0, 0, 0, time.UTC)
	lifecycleStart = installed
	return purchase, installed, lifecycleStart
}

func lifecycleDates(ageIndex int, kind string, now time.Time) (purchase, installed, lifecycleStart time.Time) {
	spreadMonths := map[string]int{"desktop": 54, "laptop": 42, "tablet": 30, "peripheral": 72}
	monthsBack := spreadMonths[kind]
	if monthsBack <= 0 {
		monthsBack = 48
	}
	offset := ageIndex % monthsBack
	if ageIndex%17 == 0 {
		offset = monthsBack - 2
	}
	purchase = addCalendarMonths(normalizeDate(now), -(offset + 6))
	installed = addCalendarMonths(purchase, 0)
	installed = time.Date(installed.Year(), installed.Month(), installed.Day()+14, 0, 0, 0, 0, time.UTC)
	lifecycleStart = installed
	return purchase, installed, lifecycleStart
}

func replacementModelID(modelSlug, kind string, modelIDs map[string]string) string {
	switch {
	case strings.HasPrefix(modelSlug, "imac"), strings.HasPrefix(modelSlug, "mac-"):
		return modelIDs["imac-m3"]
	case modelSlug == "dell-precision-7865":
		return modelIDs["dell-precision-7865"]
	case kind == "laptop":
		return modelIDs["dell-latitude-7440"]
	case kind == "tablet":
		return modelIDs["apple-ipad-air"]
	case kind == "desktop":
		return modelIDs["dell-optiplex-7020"]
	default:
		return ""
	}
}

func horizonLifecycleStage(asset domain.Asset, model *domain.AssetModel, now time.Time) string {
	if asset.Status == "retired" || asset.Status == "disposed" {
		return "retired"
	}
	if model != nil && model.LastEffectiveDate != nil && !normalizeDate(*model.LastEffectiveDate).After(normalizeDate(now)) {
		return "refresh_due"
	}
	anchor := normalizeDate(now)
	if asset.LifecycleStartDate != nil {
		anchor = normalizeDate(*asset.LifecycleStartDate)
	} else if asset.InstalledDate != nil {
		anchor = normalizeDate(*asset.InstalledDate)
	} else if asset.PurchaseDate != nil {
		anchor = normalizeDate(*asset.PurchaseDate)
	}
	usefulLife := 60
	if asset.ModelContext != nil && asset.ModelContext.UsefulLifeMonths > 0 {
		usefulLife = asset.ModelContext.UsefulLifeMonths
	} else if model != nil && model.UsefulLifeMonths > 0 {
		usefulLife = model.UsefulLifeMonths
	}
	end := addCalendarMonths(normalizeDate(anchor), usefulLife)
	today := normalizeDate(now)
	if !today.Before(end) {
		return "refresh_due"
	}
	if today.Before(addCalendarMonths(normalizeDate(anchor), usefulLife*3/4)) {
		return "in_service"
	}
	return "refresh_due"
}

func normalizeDate(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func addCalendarMonths(value time.Time, months int) time.Time {
	return normalizeDate(normalizeDate(value).AddDate(0, months, 0))
}

func dateUTC(year int, month time.Month, day int) *time.Time {
	value := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	return &value
}
