package application

import (
	"strings"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/horizon"
	"github.com/maxlemke/stewardmesh/internal/ledger"
	"github.com/maxlemke/stewardmesh/internal/signals"
	"github.com/maxlemke/stewardmesh/internal/stack"
)

// Requirement: REQ-SIGNALS-001. Feature: alerts.rules. GitHub: #11.

func TestSignalsEvaluatorsCoverFinancialConditions(t *testing.T) {
	asOf := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	ordered := asOf.AddDate(0, 0, -31)
	finance := ledger.Snapshot{
		Budgets: []ledger.Budget{{ID: "budget-one", FiscalPeriod: "FY2027", Scenario: "baseline", Currency: "USD", AllocatedMinor: 100}},
		Costs: []ledger.CostRecord{
			{ID: "cost-one", Kind: "actual", FiscalPeriod: "FY2027", Scenario: "baseline", Currency: "USD", AmountMinor: 125},
			{ID: "cost-unreconciled", Kind: "billed", FiscalPeriod: "FY2027", Scenario: "baseline", Currency: "USD", AmountMinor: 10},
		},
		PurchaseOrders: []ledger.PurchaseOrder{
			{ID: "po-unpaid", Number: "PO-UNPAID", Status: "received", OrderedOn: &ordered},
			{ID: "po-overdue", Number: "PO-OVERDUE", Status: "partially_received", OrderedOn: &ordered},
		},
	}

	tests := []struct {
		name       string
		candidates []signals.Candidate
		targetType string
		targetID   string
	}{
		{"over budget", evaluateOverBudget(signals.Rule{FiscalPeriod: "FY2027", Scenario: "baseline"}, finance), "budget", "budget-fy2027-baseline-usd"},
		{"unpaid", evaluateUnpaid(signals.Rule{ThresholdDays: []int{30}}, asOf, finance), "purchase_order", "po-unpaid"},
		{"overdue", evaluateOverdue(signals.Rule{ThresholdDays: []int{30}}, asOf, finance), "purchase_order", "po-overdue"},
		{"reconciliation", evaluateReconciliation(signals.Rule{FiscalPeriod: "FY2027", Scenario: "baseline"}, finance), "cost", "cost-unreconciled"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			found := false
			for _, candidate := range test.candidates {
				if candidate.TargetType == test.targetType && candidate.TargetID == test.targetID {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %s:%s in %#v", test.targetType, test.targetID, test.candidates)
			}
		})
	}
}

func TestSignalsEvaluatorsCoverDatesAndUsageAtUTCBoundaries(t *testing.T) {
	asOf := time.Date(2026, 8, 13, 23, 59, 0, 0, time.FixedZone("offset", -5*60*60))
	end := time.Date(2026, 9, 12, 0, 1, 0, 0, time.FixedZone("offset", 2*60*60))
	renew := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	finance := ledger.Snapshot{
		Contracts:   []ledger.Contract{{ID: "contract-one", Name: "Support", OperationalStatus: "active", EndsOn: end, RenewsOn: &renew}},
		Commitments: []ledger.Commitment{{ID: "commitment-one", ContractID: "unused-contract", Description: "Unused reservation", EndsOn: end, FiscalPeriod: "FY2027", Scenario: "baseline"}},
	}
	licenseEnd := end
	software := stack.Snapshot{Licenses: []stack.License{{ID: "license-one", Name: "Writer", Status: "active", ExpiresOn: &licenseEnd}}}

	expiration := evaluateExpiration(signals.Rule{ThresholdDays: []int{180, 90, 60, 30}}, asOf, finance, software)
	if len(expiration) != 2 || expiration[0].ThresholdDays != 30 || expiration[1].ThresholdDays != 30 {
		t.Fatalf("unexpected expiration candidates %#v", expiration)
	}
	renewal := evaluateRenewal(signals.Rule{ThresholdDays: []int{180, 90, 60, 30}}, asOf, finance)
	if len(renewal) != 1 || renewal[0].TargetID != "contract-one" || renewal[0].ThresholdDays != 90 {
		t.Fatalf("unexpected renewal candidates %#v", renewal)
	}
	unused := evaluateUnusedCommitments(signals.Rule{ThresholdDays: []int{30}, FiscalPeriod: "FY2027", Scenario: "baseline"}, asOf, finance)
	if len(unused) != 1 || unused[0].TargetID != "commitment-one" {
		t.Fatalf("unexpected unused commitment candidates %#v", unused)
	}
	filtered := evaluateUnusedCommitments(signals.Rule{ThresholdDays: []int{30}, FiscalPeriod: "FY2028"}, asOf, finance)
	if len(filtered) != 0 {
		t.Fatalf("period filter leaked candidates %#v", filtered)
	}
}

func TestForecastOverBudgetCandidateUsesExplicitScenarioPeriodAndCurrency(t *testing.T) {
	finance := ledger.Snapshot{Budgets: []ledger.Budget{
		{FiscalPeriod: "FY2027", Scenario: "baseline", Currency: "USD", AllocatedMinor: 100},
		{FiscalPeriod: "FY2027", Scenario: "optimistic", Currency: "USD", AllocatedMinor: 1000},
		{FiscalPeriod: "FY2028", Scenario: "baseline", Currency: "USD", AllocatedMinor: 1000},
	}}
	forecast := horizon.Forecast{Currency: "USD", PlannedReplacementMinor: 125}
	candidates := forecastOverBudgetCandidates(signals.Rule{FiscalPeriod: "FY2027", Scenario: "baseline"}, "baseline", 2027, 2027, finance, forecast)
	if len(candidates) != 1 || candidates[0].TargetID != "forecast-baseline-fy2027" || !strings.Contains(candidates[0].Summary, "125 minor units against 100") {
		t.Fatalf("unexpected forecast candidate %#v", candidates)
	}
	forecast.PlannedReplacementMinor = 100
	if candidates := forecastOverBudgetCandidates(signals.Rule{FiscalPeriod: "FY2027", Scenario: "baseline"}, "baseline", 2027, 2027, finance, forecast); len(candidates) != 0 {
		t.Fatalf("equal allocation should not alert %#v", candidates)
	}
}

func TestSignalsThresholdSelectionIsDeterministic(t *testing.T) {
	if threshold, ok := upcomingThreshold(91, []int{180, 90, 60, 30}); !ok || threshold != 180 {
		t.Fatalf("unexpected upcoming threshold %d %v", threshold, ok)
	}
	if threshold, ok := upcomingThreshold(90, []int{180, 90, 60, 30}); !ok || threshold != 90 {
		t.Fatalf("unexpected boundary threshold %d %v", threshold, ok)
	}
	if threshold, ok := reachedThreshold(181, []int{30, 90, 180}); !ok || threshold != 180 {
		t.Fatalf("unexpected reached threshold %d %v", threshold, ok)
	}
}
