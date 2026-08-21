package horizon

// Requirement: REQ-HORIZON-001. Feature: lifecycle.planning.

import (
	"time"

	"github.com/maxlemke/stewardmesh/internal/domain"
)

// LifecycleAnchor returns the calendar date Horizon uses to measure useful life.
func LifecycleAnchor(asset domain.Asset) *time.Time {
	if asset.LifecycleStartDate != nil {
		return cloneDate(asset.LifecycleStartDate)
	}
	if asset.InstalledDate != nil {
		return cloneDate(asset.InstalledDate)
	}
	return cloneDate(asset.PurchaseDate)
}

// UsefulLifeMonths prefers the immutable model snapshot, then the live model record.
func UsefulLifeMonths(asset domain.Asset, model *domain.AssetModel) int {
	if asset.ModelContext != nil && asset.ModelContext.UsefulLifeMonths > 0 {
		return asset.ModelContext.UsefulLifeMonths
	}
	if model != nil && model.UsefulLifeMonths > 0 {
		return model.UsefulLifeMonths
	}
	return 0
}

// LifecyclePercent reports elapsed useful life through the anchor date, capped at 100.
func LifecyclePercent(anchor *time.Time, usefulLifeMonths int, asOf time.Time) *int {
	if anchor == nil || usefulLifeMonths <= 0 {
		return nil
	}
	start := normalizeDate(*anchor)
	end := addCalendarMonths(start, usefulLifeMonths)
	today := normalizeDate(asOf)
	if today.Before(start) {
		zero := 0
		return &zero
	}
	if !today.Before(end) {
		hundred := 100
		return &hundred
	}
	total := end.Sub(start)
	if total <= 0 {
		return nil
	}
	elapsed := today.Sub(start)
	percent := int(elapsed * 100 / total)
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	return &percent
}

// ModelPastLifecycle is true when a model's last effective date is on or before the as-of date.
func ModelPastLifecycle(model domain.AssetModel, asOf time.Time) bool {
	if model.LastEffectiveDate == nil {
		return false
	}
	return !normalizeDate(*model.LastEffectiveDate).After(normalizeDate(asOf))
}

// LifecycleStage derives the baseline Horizon stage from Atlas asset state, model lineage, and useful life.
func LifecycleStage(asset domain.Asset, model *domain.AssetModel, asOf time.Time) string {
	if asset.Status == "retired" || asset.Status == "disposed" {
		return "retired"
	}
	if model != nil && ModelPastLifecycle(*model, asOf) {
		return "refresh_due"
	}
	anchor := LifecycleAnchor(asset)
	usefulLife := UsefulLifeMonths(asset, model)
	if anchor == nil || usefulLife <= 0 {
		return "planned"
	}
	percent := LifecyclePercent(anchor, usefulLife, asOf)
	if percent == nil {
		return "planned"
	}
	if *percent >= 75 {
		return "refresh_due"
	}
	return "in_service"
}

// ReplacementModelID resolves the planned replacement model from asset override, linked model lineage, or kind default.
func ReplacementModelID(asset domain.Asset, model *domain.AssetModel, kindDefaultReplacementID string) string {
	if asset.ReplacementModelID != "" {
		return asset.ReplacementModelID
	}
	if model != nil && model.ReplacementModelID != "" {
		return model.ReplacementModelID
	}
	return kindDefaultReplacementID
}
