package campusseed

// Requirement: REQ-HORIZON-001. Feature: lifecycle.planning.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/horizon"
	"github.com/maxlemke/stewardmesh/internal/ledger"
)

const (
	forecastCostSourcePrefix = "horizon-forecast/"
	forecastCostAssetLimit   = 240
)

func seedCampusForecastCosts(ctx context.Context, horizonService *horizon.Service, ledgerService *ledger.Service, now time.Time) error {
	if horizonService == nil || ledgerService == nil {
		return errors.New("campus forecast cost seeding requires Horizon and Ledger")
	}
	snapshot, err := ledgerService.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("snapshot Ledger for forecast costs: %w", err)
	}
	for _, cost := range snapshot.Costs {
		if strings.HasPrefix(cost.SourceRecordID, forecastCostSourcePrefix) {
			return nil
		}
	}
	plans, err := horizonService.ListPlans(ctx, horizon.ListPlansQuery{Scenario: "baseline"})
	if err != nil {
		return fmt.Errorf("list campus Horizon plans for forecast costs: %w", err)
	}
	type candidate struct {
		plan       horizon.Plan
		fiscalYear int
		yearsUntil int
	}
	currentYear := now.UTC().Year()
	candidates := make([]candidate, 0, len(plans))
	for _, plan := range plans {
		if plan.LifecycleStage == "retired" {
			continue
		}
		replacement := plan.ReplacementDate
		if replacement == nil {
			replacement = plan.DerivedReplacementDate
		}
		if replacement == nil || plan.ReplacementCostMinor <= 0 {
			continue
		}
		fiscalYear := replacement.UTC().Year()
		candidates = append(candidates, candidate{plan: plan, fiscalYear: fiscalYear, yearsUntil: fiscalYear - currentYear})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].yearsUntil == candidates[j].yearsUntil {
			return candidates[i].plan.AssetID < candidates[j].plan.AssetID
		}
		return candidates[i].yearsUntil < candidates[j].yearsUntil
	})
	if len(candidates) > forecastCostAssetLimit {
		candidates = candidates[:forecastCostAssetLimit]
	}
	for _, item := range candidates {
		amounts := forecastCostAmounts(item.plan.ReplacementCostMinor, item.yearsUntil)
		for _, kind := range []string{"actual", "estimated", "committed", "normalized_real", "tco"} {
			amount := amounts[kind]
			if amount <= 0 {
				continue
			}
			if _, err := ledgerService.ReconcileCost(ctx, ledger.ReconcileCostInput{
				Description:    forecastCostDescription(kind, item.fiscalYear),
				Kind:           kind,
				Currency:       firstNonEmpty(item.plan.Currency, "USD"),
				AmountMinor:    amount,
				FiscalPeriod:   fmt.Sprintf("FY%d", item.fiscalYear),
				Scenario:       item.plan.Scenario,
				AssetID:        item.plan.AssetID,
				SourceSystemID: SourceSystemID,
				SourceRecordID: forecastCostSourcePrefix + item.plan.AssetID + "/" + kind,
			}); err != nil {
				return fmt.Errorf("reconcile %s forecast cost for %q: %w", kind, item.plan.AssetID, err)
			}
		}
	}
	return nil
}

func forecastCostAmounts(replacementMinor int64, yearsUntil int) map[string]int64 {
	if replacementMinor <= 0 {
		return map[string]int64{}
	}
	estimated := replacementMinor
	amounts := map[string]int64{
		"estimated":       estimated,
		"normalized_real": estimated * 103 / 100,
		"tco":             estimated * 128 / 100,
	}
	switch {
	case yearsUntil <= 0:
		amounts["committed"] = estimated * 92 / 100
		amounts["actual"] = estimated * 58 / 100
	case yearsUntil == 1:
		amounts["committed"] = estimated * 80 / 100
		amounts["actual"] = estimated * 22 / 100
	case yearsUntil == 2:
		amounts["committed"] = estimated * 28 / 100
	}
	return amounts
}

func forecastCostDescription(kind string, fiscalYear int) string {
	switch kind {
	case "actual":
		return fmt.Sprintf("Recognized hardware refresh spend booked against FY%d", fiscalYear)
	case "estimated":
		return fmt.Sprintf("Vendor replacement quote for the FY%d refresh cycle", fiscalYear)
	case "committed":
		return fmt.Sprintf("Issued purchase commitment for the FY%d refresh cycle", fiscalYear)
	case "normalized_real":
		return fmt.Sprintf("Inflation-normalized replacement comparison for FY%d", fiscalYear)
	case "tco":
		return fmt.Sprintf("Hardware, warranty, imaging, and support TCO for FY%d", fiscalYear)
	default:
		return fmt.Sprintf("Campus demo forecast cost for FY%d", fiscalYear)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
