package contracttest

// Provider-neutral Horizon adapter contract.
// Requirement: REQ-HORIZON-001. Feature: lifecycle.planning.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/horizon"
)

// HorizonStore verifies persistence behavior using an Atlas asset that already
// exists in the subject provider. PostgreSQL callers must create assetID first
// because Horizon intentionally preserves its organization-scoped Atlas FK.
func HorizonStore(t testing.TB, subject horizon.Store, organizationID, assetID, suffix string) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	planID := "horizon-plan-" + suffix
	if _, err := subject.GetPlan(ctx, organizationID, planID); !errors.Is(err, horizon.ErrNotFound) {
		t.Fatalf("expected missing Horizon plan, got %v", err)
	}
	if items, err := subject.ListPlans(ctx, organizationID, horizon.ListPlansQuery{}); err != nil || len(items) != 0 {
		t.Fatalf("expected an empty Horizon store, items=%#v err=%v", items, err)
	}

	replacement := time.Date(2027, time.July, 1, 0, 0, 0, 0, time.UTC)
	expectedReplacement := replacement
	plan := horizon.Plan{
		ID: planID, OrganizationID: organizationID, AssetID: assetID, Scenario: "baseline",
		ExpectedUsefulLifeMonths: 48, ReplacementDate: &replacement, LifecycleStage: "planned",
		ReplacementCostMinor: 125_000, Currency: "USD", EffectiveFrom: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	version := horizonVersion(plan, "account-contract", now)
	created, err := subject.CreatePlan(ctx, plan, version)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != planID || created.Revision != 1 {
		t.Fatalf("unexpected created Horizon plan %#v", created)
	}
	// Mutating caller-owned date pointers must not mutate persisted state.
	*plan.ReplacementDate = plan.ReplacementDate.AddDate(10, 0, 0)
	*version.ReplacementDate = version.ReplacementDate.AddDate(10, 0, 0)
	loaded, err := subject.GetPlan(ctx, organizationID, planID)
	if err != nil || loaded.ReplacementDate == nil || !loaded.ReplacementDate.Equal(expectedReplacement) {
		t.Fatalf("Horizon plan was not defensively persisted: %#v err=%v", loaded, err)
	}
	if _, err := subject.CreatePlan(ctx, created, horizonVersion(created, "duplicate", now)); !errors.Is(err, horizon.ErrConflict) {
		t.Fatalf("expected duplicate Horizon ID conflict, got %v", err)
	}
	conflicting := created
	conflicting.ID = "horizon-conflict-" + suffix
	if _, err := subject.CreatePlan(ctx, conflicting, horizonVersion(conflicting, "duplicate", now)); !errors.Is(err, horizon.ErrConflict) {
		t.Fatalf("expected asset-and-scenario conflict, got %v", err)
	}

	alternative := created
	alternative.ID = "horizon-alternative-" + suffix
	alternative.Scenario = "optimistic"
	alternative.ReplacementCostMinor = 100_000
	if _, err := subject.CreatePlan(ctx, alternative, horizonVersion(alternative, "account-contract", now)); err != nil {
		t.Fatalf("create alternate scenario: %v", err)
	}
	filtered, err := subject.ListPlans(ctx, organizationID, horizon.ListPlansQuery{AssetID: assetID, Scenario: "optimistic"})
	if err != nil || len(filtered) != 1 || filtered[0].ID != alternative.ID {
		t.Fatalf("unexpected Horizon filtering %#v err=%v", filtered, err)
	}
	if isolated, err := subject.ListPlans(ctx, organizationID+"-other", horizon.ListPlansQuery{}); err != nil || len(isolated) != 0 {
		t.Fatalf("expected Horizon organization isolation, items=%#v err=%v", isolated, err)
	}
	if _, err := subject.GetPlan(ctx, organizationID+"-other", planID); !errors.Is(err, horizon.ErrNotFound) {
		t.Fatalf("expected organization-isolated lookup, got %v", err)
	}
	if _, err := subject.ListPlanVersions(ctx, organizationID+"-other", planID); !errors.Is(err, horizon.ErrNotFound) {
		t.Fatalf("expected organization-isolated history, got %v", err)
	}

	updated := loaded
	updated.ExpectedUsefulLifeMonths = 60
	updated.ReplacementCostMinor = 150_000
	updated.LifecycleStage = "approved"
	updated.EffectiveFrom = time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	updated.Revision = 2
	updated.UpdatedAt = now.Add(time.Minute)
	updatedVersion := horizonVersion(updated, "account-updater", now.Add(time.Minute))
	stored, err := subject.UpdatePlan(ctx, updated, 1, updatedVersion)
	if err != nil || stored.Revision != 2 || stored.ReplacementCostMinor != 150_000 {
		t.Fatalf("unexpected Horizon update %#v err=%v", stored, err)
	}
	if _, err := subject.UpdatePlan(ctx, updated, 1, updatedVersion); !errors.Is(err, horizon.ErrConflict) {
		t.Fatalf("expected stale Horizon update conflict, got %v", err)
	}
	history, err := subject.ListPlanVersions(ctx, organizationID, planID)
	if err != nil || len(history) != 2 || history[0].Revision != 2 || history[1].Revision != 1 ||
		history[0].ActorID != "account-updater" || history[1].ReplacementCostMinor != 125_000 {
		t.Fatalf("unexpected immutable Horizon history %#v err=%v", history, err)
	}

	concurrentPlan := created
	concurrentPlan.ID = "horizon-concurrent-" + suffix
	concurrentPlan.Scenario = "concurrent"
	concurrentPlan.ReplacementCostMinor = 1
	if _, err := subject.CreatePlan(ctx, concurrentPlan, horizonVersion(concurrentPlan, "account-contract", now)); err != nil {
		t.Fatalf("create concurrent Horizon plan: %v", err)
	}
	const workers = 12
	results := make(chan error, workers)
	var wait sync.WaitGroup
	for index := range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			candidate := concurrentPlan
			candidate.Revision = 2
			candidate.ReplacementCostMinor = int64(100 + index)
			candidate.UpdatedAt = now.Add(time.Duration(index+1) * time.Minute)
			_, err := subject.UpdatePlan(ctx, candidate, 1, horizonVersion(candidate, fmt.Sprintf("worker-%d", index), candidate.UpdatedAt))
			results <- err
		}()
	}
	wait.Wait()
	close(results)
	succeeded, conflicted := 0, 0
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, horizon.ErrConflict):
			conflicted++
		default:
			t.Fatalf("unexpected concurrent Horizon update error: %v", err)
		}
	}
	if succeeded != 1 || conflicted != workers-1 {
		t.Fatalf("expected one optimistic update winner, succeeded=%d conflicted=%d", succeeded, conflicted)
	}
	concurrentHistory, err := subject.ListPlanVersions(ctx, organizationID, concurrentPlan.ID)
	if err != nil || len(concurrentHistory) != 2 || concurrentHistory[0].Revision != 2 || concurrentHistory[1].Revision != 1 {
		t.Fatalf("unexpected concurrent Horizon history %#v err=%v", concurrentHistory, err)
	}
}

func horizonVersion(plan horizon.Plan, actorID string, recordedAt time.Time) horizon.PlanVersion {
	var replacement *time.Time
	if plan.ReplacementDate != nil {
		value := *plan.ReplacementDate
		replacement = &value
	}
	return horizon.PlanVersion{
		PlanID: plan.ID, OrganizationID: plan.OrganizationID, AssetID: plan.AssetID, Scenario: plan.Scenario,
		ExpectedUsefulLifeMonths: plan.ExpectedUsefulLifeMonths, ReplacementDate: replacement,
		LifecycleStage: plan.LifecycleStage, ReplacementCostMinor: plan.ReplacementCostMinor, Currency: plan.Currency,
		EffectiveFrom: plan.EffectiveFrom, Revision: plan.Revision, ActorID: actorID, RecordedAt: recordedAt,
	}
}
