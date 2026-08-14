package application

// Requirement: REQ-SIGNALS-001. Feature: alerts.rules. GitHub: #11.

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/horizon"
	"github.com/maxlemke/stewardmesh/internal/ledger"
	"github.com/maxlemke/stewardmesh/internal/signals"
	"github.com/maxlemke/stewardmesh/internal/stack"
)

type signalsEvaluator struct {
	ledger  *ledger.Service
	stack   *stack.Service
	horizon *horizon.Service
}

func (e signalsEvaluator) Evaluate(ctx context.Context, rule signals.Rule, asOf time.Time) ([]signals.Candidate, error) {
	finance, err := e.ledger.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	switch rule.Condition {
	case signals.ConditionOverBudget:
		return evaluateOverBudget(rule, finance), nil
	case signals.ConditionForecastOverBudget:
		return e.evaluateForecastOverBudget(ctx, rule, asOf, finance)
	case signals.ConditionUnpaid:
		return evaluateUnpaid(rule, asOf, finance), nil
	case signals.ConditionOverdue:
		return evaluateOverdue(rule, asOf, finance), nil
	case signals.ConditionExpiration:
		software, err := e.stack.Snapshot(ctx)
		if err != nil {
			return nil, err
		}
		return evaluateExpiration(rule, asOf, finance, software), nil
	case signals.ConditionRenewal:
		return evaluateRenewal(rule, asOf, finance), nil
	case signals.ConditionUnusedCommitment:
		return evaluateUnusedCommitments(rule, asOf, finance), nil
	case signals.ConditionReconciliation:
		return evaluateReconciliation(rule, finance), nil
	default:
		return nil, signals.ErrInvalidInput
	}
}

func evaluateOverBudget(rule signals.Rule, finance ledger.Snapshot) []signals.Candidate {
	type totals struct{ allocated, recognized int64 }
	groups := map[string]totals{}
	for _, budget := range finance.Budgets {
		if !ruleMatchesPeriod(rule, budget.FiscalPeriod, budget.Scenario) {
			continue
		}
		key := budget.FiscalPeriod + "\x00" + budget.Scenario + "\x00" + budget.Currency
		value := groups[key]
		value.allocated += budget.AllocatedMinor
		groups[key] = value
	}
	for _, cost := range finance.Costs {
		if !ruleMatchesPeriod(rule, cost.FiscalPeriod, cost.Scenario) || !recognizedCost(cost.Kind) {
			continue
		}
		key := cost.FiscalPeriod + "\x00" + cost.Scenario + "\x00" + cost.Currency
		value := groups[key]
		value.recognized += cost.AmountMinor
		groups[key] = value
	}
	candidates := make([]signals.Candidate, 0)
	for key, value := range groups {
		if value.recognized <= value.allocated {
			continue
		}
		parts := strings.Split(key, "\x00")
		targetID := stableSignalTarget("budget", parts[0], parts[1], parts[2])
		candidates = append(candidates, signals.Candidate{TargetType: "budget", TargetID: targetID, Title: "Budget is over its allocation",
			Summary: fmt.Sprintf("%s %s recognized %d minor units against %d allocated minor units.", parts[0], parts[1], value.recognized, value.allocated)})
	}
	sortCandidates(candidates)
	return candidates
}

func (e signalsEvaluator) evaluateForecastOverBudget(ctx context.Context, rule signals.Rule, asOf time.Time, finance ledger.Snapshot) ([]signals.Candidate, error) {
	scenario := rule.Scenario
	if scenario == "" {
		scenario = "baseline"
	}
	fromYear, toYear := asOf.Year(), asOf.Year()+10
	if strings.HasPrefix(rule.FiscalPeriod, "FY") {
		var year int
		if _, err := fmt.Sscanf(rule.FiscalPeriod, "FY%d", &year); err == nil && year >= 1970 && year <= 9999 {
			fromYear, toYear = year, year
		}
	}
	forecast, err := e.horizon.Forecast(ctx, horizon.ForecastQuery{Scenarios: []string{scenario}, AsOf: asOf, FromYear: fromYear, ToYear: toYear, FiscalYearStartMonth: 1, GroupBy: "fiscal_year"})
	if err != nil {
		return nil, err
	}
	return forecastOverBudgetCandidates(rule, scenario, fromYear, toYear, finance, forecast), nil
}

func forecastOverBudgetCandidates(rule signals.Rule, scenario string, fromYear, toYear int, finance ledger.Snapshot, forecast horizon.Forecast) []signals.Candidate {
	allocated := int64(0)
	for _, budget := range finance.Budgets {
		if budget.Scenario == scenario && (rule.FiscalPeriod == "" || budget.FiscalPeriod == rule.FiscalPeriod) && (forecast.Currency == "" || budget.Currency == forecast.Currency) {
			allocated += budget.AllocatedMinor
		}
	}
	if forecast.PlannedReplacementMinor <= allocated {
		return []signals.Candidate{}
	}
	period := rule.FiscalPeriod
	if period == "" {
		period = fmt.Sprintf("FY%d-FY%d", fromYear, toYear)
	}
	return []signals.Candidate{{TargetType: "forecast", TargetID: stableSignalTarget("forecast", scenario, period), Title: "Forecast exceeds available budget",
		Summary: fmt.Sprintf("%s plans require %d minor units against %d allocated minor units for %s.", scenario, forecast.PlannedReplacementMinor, allocated, period)}}
}

func evaluateUnpaid(rule signals.Rule, asOf time.Time, finance ledger.Snapshot) []signals.Candidate {
	paid := map[string]bool{}
	for _, cost := range finance.Costs {
		if cost.PurchaseOrderID != "" && cost.Kind == "paid" {
			paid[cost.PurchaseOrderID] = true
		}
	}
	candidates := []signals.Candidate{}
	for _, order := range finance.PurchaseOrders {
		if !ruleMatchesPeriod(rule, "", "") || order.Status != "received" || order.OrderedOn == nil || paid[order.ID] {
			continue
		}
		age := calendarDays(*order.OrderedOn, asOf)
		threshold, ok := reachedThreshold(age, rule.ThresholdDays)
		if !ok {
			continue
		}
		due := order.OrderedOn.AddDate(0, 0, threshold)
		candidates = append(candidates, signals.Candidate{TargetType: "purchase_order", TargetID: order.ID, Title: "Received purchase order remains unpaid",
			Summary: fmt.Sprintf("Purchase order %s has no paid cost record %d days after ordering.", order.Number, age), DueAt: &due, ThresholdDays: threshold})
	}
	sortCandidates(candidates)
	return candidates
}

func evaluateOverdue(rule signals.Rule, asOf time.Time, finance ledger.Snapshot) []signals.Candidate {
	candidates := []signals.Candidate{}
	for _, order := range finance.PurchaseOrders {
		if order.OrderedOn == nil || order.Status != "ordered" && order.Status != "partially_received" {
			continue
		}
		age := calendarDays(*order.OrderedOn, asOf)
		threshold, ok := reachedThreshold(age, rule.ThresholdDays)
		if !ok {
			continue
		}
		due := order.OrderedOn.AddDate(0, 0, threshold)
		candidates = append(candidates, signals.Candidate{TargetType: "purchase_order", TargetID: order.ID, Title: "Purchase order is overdue",
			Summary: fmt.Sprintf("Purchase order %s remains %s %d days after ordering.", order.Number, strings.ReplaceAll(order.Status, "_", " "), age), DueAt: &due, ThresholdDays: threshold})
	}
	sortCandidates(candidates)
	return candidates
}

func evaluateExpiration(rule signals.Rule, asOf time.Time, finance ledger.Snapshot, software stack.Snapshot) []signals.Candidate {
	candidates := []signals.Candidate{}
	for _, contract := range finance.Contracts {
		if contract.OperationalStatus == "terminated" || contract.OperationalStatus == "cancelled" {
			continue
		}
		if threshold, ok := upcomingThreshold(calendarDays(asOf, contract.EndsOn), rule.ThresholdDays); ok {
			due := contract.EndsOn
			candidates = append(candidates, signals.Candidate{TargetType: "contract", TargetID: contract.ID, Title: "Contract expiration requires attention",
				Summary: fmt.Sprintf("Contract %s reaches its end date in %d days.", contract.Name, calendarDays(asOf, contract.EndsOn)), DueAt: &due, ThresholdDays: threshold})
		}
	}
	for _, license := range software.Licenses {
		if license.Status == "retired" || license.ExpiresOn == nil {
			continue
		}
		if threshold, ok := upcomingThreshold(calendarDays(asOf, *license.ExpiresOn), rule.ThresholdDays); ok {
			due := *license.ExpiresOn
			candidates = append(candidates, signals.Candidate{TargetType: "software_license", TargetID: license.ID, Title: "Software license expiration requires attention",
				Summary: fmt.Sprintf("License %s expires in %d days.", license.Name, calendarDays(asOf, *license.ExpiresOn)), DueAt: &due, ThresholdDays: threshold})
		}
	}
	sortCandidates(candidates)
	return candidates
}

func evaluateRenewal(rule signals.Rule, asOf time.Time, finance ledger.Snapshot) []signals.Candidate {
	candidates := []signals.Candidate{}
	for _, contract := range finance.Contracts {
		if contract.RenewsOn == nil || contract.OperationalStatus == "terminated" || contract.OperationalStatus == "cancelled" {
			continue
		}
		days := calendarDays(asOf, *contract.RenewsOn)
		if threshold, ok := upcomingThreshold(days, rule.ThresholdDays); ok {
			due := *contract.RenewsOn
			candidates = append(candidates, signals.Candidate{TargetType: "contract", TargetID: contract.ID, Title: "Contract renewal decision is approaching",
				Summary: fmt.Sprintf("Contract %s renews in %d days.", contract.Name, days), DueAt: &due, ThresholdDays: threshold})
		}
	}
	sortCandidates(candidates)
	return candidates
}

func evaluateUnusedCommitments(rule signals.Rule, asOf time.Time, finance ledger.Snapshot) []signals.Candidate {
	used := map[string]int64{}
	for _, cost := range finance.Costs {
		if cost.ContractID != "" && recognizedCost(cost.Kind) {
			used[cost.ContractID] += cost.AmountMinor
		}
	}
	candidates := []signals.Candidate{}
	for _, commitment := range finance.Commitments {
		if !ruleMatchesPeriod(rule, commitment.FiscalPeriod, commitment.Scenario) || used[commitment.ContractID] > 0 {
			continue
		}
		days := calendarDays(asOf, commitment.EndsOn)
		threshold, ok := upcomingThreshold(days, rule.ThresholdDays)
		if !ok {
			continue
		}
		due := commitment.EndsOn
		candidates = append(candidates, signals.Candidate{TargetType: "commitment", TargetID: commitment.ID, Title: "Commitment has no recognized usage",
			Summary: fmt.Sprintf("%s has no actual, billed, paid, or committed cost and ends in %d days.", commitment.Description, days), DueAt: &due, ThresholdDays: threshold})
	}
	sortCandidates(candidates)
	return candidates
}

func evaluateReconciliation(rule signals.Rule, finance ledger.Snapshot) []signals.Candidate {
	candidates := []signals.Candidate{}
	for _, cost := range finance.Costs {
		if rule.FiscalPeriod != "" && cost.FiscalPeriod != rule.FiscalPeriod || rule.Scenario != "" && cost.Scenario != rule.Scenario {
			continue
		}
		if (cost.Kind == "actual" || cost.Kind == "billed") && (cost.SourceSystemID == "" || cost.SourceRecordID == "") {
			candidates = append(candidates, signals.Candidate{TargetType: "cost", TargetID: cost.ID, Title: "Cost requires source reconciliation",
				Summary: fmt.Sprintf("%s %s cost has no complete source-system identity.", cost.FiscalPeriod, cost.Kind)})
		}
	}
	sortCandidates(candidates)
	return candidates
}

func upcomingThreshold(days int, thresholds []int) (int, bool) {
	if days < 0 {
		return 0, true
	}
	selected, found := 0, false
	for _, threshold := range thresholds {
		if days <= threshold && (!found || threshold < selected) {
			selected, found = threshold, true
		}
	}
	return selected, found
}

func reachedThreshold(age int, thresholds []int) (int, bool) {
	selected, found := 0, false
	for _, threshold := range thresholds {
		if age >= threshold && (!found || threshold > selected) {
			selected, found = threshold, true
		}
	}
	return selected, found
}

func calendarDays(from, to time.Time) int {
	left := time.Date(from.UTC().Year(), from.UTC().Month(), from.UTC().Day(), 0, 0, 0, 0, time.UTC)
	right := time.Date(to.UTC().Year(), to.UTC().Month(), to.UTC().Day(), 0, 0, 0, 0, time.UTC)
	return int(right.Sub(left) / (24 * time.Hour))
}

func recognizedCost(kind string) bool {
	return kind == "actual" || kind == "billed" || kind == "paid" || kind == "committed"
}

func ruleMatchesPeriod(rule signals.Rule, fiscalPeriod, scenario string) bool {
	return (rule.FiscalPeriod == "" || rule.FiscalPeriod == fiscalPeriod) && (rule.Scenario == "" || rule.Scenario == scenario)
}

func stableSignalTarget(parts ...string) string {
	result := strings.ToLower(strings.Join(parts, "-"))
	result = strings.Map(func(character rune) rune {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || strings.ContainsRune("._:-", character) {
			return character
		}
		return '-'
	}, result)
	if len(result) > 128 {
		result = result[:128]
	}
	return strings.Trim(result, "-")
}

func sortCandidates(candidates []signals.Candidate) {
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].TargetType+"\x00"+candidates[i].TargetID < candidates[j].TargetType+"\x00"+candidates[j].TargetID
	})
}
