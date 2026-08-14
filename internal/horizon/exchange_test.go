package horizon_test

// Requirements: REQ-HORIZON-001, REQ-EXCHANGE-001. Features: lifecycle.planning, migration.packages. GitHub: #9.

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/horizon"
	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/threads"
)

type horizonExchangeAuditor struct {
	events   map[string]foundation.AuditEvent
	failNext error
}

func (a *horizonExchangeAuditor) Record(_ context.Context, event foundation.AuditEvent) error {
	if a.failNext != nil {
		err := a.failNext
		a.failNext = nil
		return err
	}
	if a.events == nil {
		a.events = make(map[string]foundation.AuditEvent)
	}
	if existing, ok := a.events[event.ID]; ok {
		if !reflect.DeepEqual(existing, event) {
			return errors.New("audit event id conflicts with different immutable content")
		}
		return nil
	}
	a.events[event.ID] = event
	return nil
}

type horizonWriteGate struct {
	err      error
	requests [][2]string
}

func (g *horizonWriteGate) CheckResourceWrite(_ context.Context, recordType, recordID string) error {
	g.requests = append(g.requests, [2]string{recordType, recordID})
	return g.err
}

func newHorizonExchangeService(t *testing.T, organizationID string, auditor foundation.Auditor, gate horizon.WriteGate) (*horizon.Service, horizon.ExchangeImporter, *repository.MemoryHorizonStore) {
	t.Helper()
	store := repository.NewMemoryHorizonStore()
	assets := &horizonAssets{items: map[string]domain.Asset{
		"asset-portable": horizonAsset("asset-portable", nil),
	}}
	assets.items["asset-portable"] = domain.Asset{
		ID: "asset-portable", OrganizationID: organizationID, Name: "Portable asset", Kind: "server",
		Status: "active", Revision: 1, CreatedAt: horizonNow, UpdatedAt: horizonNow,
	}
	service, importer, err := horizon.NewServiceWithExchangeImporter(
		store, assets, &horizonFinance{}, &horizonRelationships{tags: map[string][]threads.EffectiveTag{}, links: map[string][]threads.GoalLink{}},
		gate, auditor, horizon.ServiceConfig{OrganizationID: organizationID, Now: func() time.Time { return horizonNow }},
	)
	if err != nil {
		t.Fatal(err)
	}
	return service, importer, store
}

func portableHorizonPlan() horizon.Plan {
	replacement := time.Date(2030, time.June, 30, 0, 0, 0, 0, time.UTC)
	return horizon.Plan{
		ID: "plan-portable", AssetID: "asset-portable", Scenario: "baseline", ExpectedUsefulLifeMonths: 84,
		ReplacementDate: &replacement, LifecycleStage: "approved", ReplacementCostMinor: 250_000, Currency: "USD",
		EffectiveFrom: time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC), Revision: 7,
	}
}

func TestExchangeImporterPreservesExactPlanRevisionAndReplaysAudit(t *testing.T) {
	auditor := &horizonExchangeAuditor{}
	service, importer, store := newHorizonExchangeService(t, "target-org", auditor, nil)
	operation := horizon.ExchangeImportOperation{
		Token: "horizon-exchange-import", OccurredAt: time.Date(2026, time.August, 13, 20, 0, 0, 0, time.UTC),
	}
	candidate := portableHorizonPlan()

	result, err := importer.ImportPlan(context.Background(), operation, candidate)
	if err != nil || !result.Committed || !result.Created {
		t.Fatalf("import exact Horizon plan: %#v err=%v", result, err)
	}
	stored, err := store.GetPlan(context.Background(), "target-org", candidate.ID)
	if err != nil || stored.OrganizationID != "target-org" || stored.Revision != candidate.Revision ||
		stored.CreatedAt != operation.OccurredAt || stored.UpdatedAt != operation.OccurredAt || !samePortableHorizonPlan(stored, candidate) {
		t.Fatalf("Horizon import was not lossless: %#v err=%v", stored, err)
	}
	history, err := service.ListPlanHistory(context.Background(), candidate.ID)
	if err != nil || len(history) != 1 || history[0].Revision != candidate.Revision ||
		history[0].ActorID != "system:exchange" || history[0].RecordedAt != operation.OccurredAt {
		t.Fatalf("unexpected imported Horizon history %#v err=%v", history, err)
	}
	if len(auditor.events) != 1 {
		t.Fatalf("expected one idempotent import audit, got %#v", auditor.events)
	}
	for _, event := range auditor.events {
		if event.ActorID != "system:exchange" || event.CorrelationID != operation.Token || event.OccurredAt != operation.OccurredAt ||
			event.Action != "horizon.plan.created" || event.ResourceID != candidate.ID || event.Metadata["revision"] != "7" {
			t.Fatalf("unexpected deterministic Horizon import audit %#v", event)
		}
		if _, exposed := event.Metadata["replacementCostMinor"]; exposed {
			t.Fatalf("Horizon import audit exposed monetary data: %#v", event.Metadata)
		}
	}

	replay, err := importer.ImportPlan(context.Background(), operation, candidate)
	if err != nil || !replay.Committed || replay.Created || len(auditor.events) != 1 {
		t.Fatalf("exact Horizon replay was not idempotent: %#v audits=%d err=%v", replay, len(auditor.events), err)
	}
	changed := candidate
	changed.ReplacementCostMinor++
	if _, err := importer.ImportPlan(context.Background(), operation, changed); !errors.Is(err, horizon.ErrConflict) {
		t.Fatalf("expected changed Horizon replay conflict, got %v", err)
	}
}

func TestExchangeImporterReportsAmbiguousAuditCommitAndRepairsIt(t *testing.T) {
	auditFailure := errors.New("audit temporarily unavailable")
	auditor := &horizonExchangeAuditor{failNext: auditFailure}
	_, importer, store := newHorizonExchangeService(t, "target-org", auditor, nil)
	operation := horizon.ExchangeImportOperation{
		Token: "horizon-audit-repair", OccurredAt: time.Date(2026, time.August, 13, 20, 30, 0, 0, time.UTC),
	}
	candidate := portableHorizonPlan()

	result, err := importer.ImportPlan(context.Background(), operation, candidate)
	if !errors.Is(err, auditFailure) || !result.Committed || !result.Created {
		t.Fatalf("expected truthful post-commit audit failure, result=%#v err=%v", result, err)
	}
	if _, err := store.GetPlan(context.Background(), "target-org", candidate.ID); err != nil {
		t.Fatalf("provider did not retain committed plan: %v", err)
	}
	repaired, err := importer.ImportPlan(context.Background(), operation, candidate)
	if err != nil || !repaired.Committed || repaired.Created || len(auditor.events) != 1 {
		t.Fatalf("repair did not converge: %#v audits=%d err=%v", repaired, len(auditor.events), err)
	}
}

func TestUpdatePlanChecksImportedOwnershipFenceBeforeMutation(t *testing.T) {
	locked := errors.New("resource is externally write-locked")
	gate := &horizonWriteGate{err: locked}
	auditor := &horizonExchangeAuditor{}
	service, _, store := newHorizonExchangeService(t, "target-org", auditor, gate)
	created, err := service.CreatePlan(context.Background(), horizon.CreatePlanInput{
		ID: "plan-locked", AssetID: "asset-portable", Scenario: "baseline", ExpectedUsefulLifeMonths: 36,
		LifecycleStage: "planned", ReplacementCostMinor: 100, Currency: "USD",
		EffectiveFrom: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	beforeAudits := len(auditor.events)
	_, err = service.UpdatePlan(context.Background(), horizon.UpdatePlanInput{
		ID: created.ID, AssetID: created.AssetID, Scenario: created.Scenario,
		ExpectedUsefulLifeMonths: 48, LifecycleStage: "approved", ReplacementCostMinor: 200, Currency: "USD",
		EffectiveFrom: created.EffectiveFrom, Revision: created.Revision,
	})
	if !errors.Is(err, locked) {
		t.Fatalf("expected Horizon ownership fence, got %v", err)
	}
	if len(gate.requests) != 1 || gate.requests[0] != [2]string{"horizon.plan", created.ID} {
		t.Fatalf("unexpected Horizon write gate calls %#v", gate.requests)
	}
	stored, readErr := store.GetPlan(context.Background(), "target-org", created.ID)
	if readErr != nil || stored.Revision != 1 || stored.ReplacementCostMinor != 100 || len(auditor.events) != beforeAudits {
		t.Fatalf("denied Horizon update mutated state: %#v audits=%d err=%v", stored, len(auditor.events), readErr)
	}
}

func TestExchangeImporterRejectsDeploymentStateAndForeignCapability(t *testing.T) {
	_, sourceImporter, _ := newHorizonExchangeService(t, "source-org", &horizonExchangeAuditor{}, nil)
	targetService, _, _ := newHorizonExchangeService(t, "target-org", &horizonExchangeAuditor{}, nil)
	if targetService.OwnsExchangeImporter(sourceImporter) {
		t.Fatal("Horizon service accepted another service's importer capability")
	}
	candidate := portableHorizonPlan()
	candidate.OrganizationID = "source-org"
	_, err := sourceImporter.ImportPlan(context.Background(), horizon.ExchangeImportOperation{
		Token: "deployment-state", OccurredAt: time.Date(2026, time.August, 13, 21, 0, 0, 0, time.UTC),
	}, candidate)
	if !errors.Is(err, horizon.ErrInvalidInput) {
		t.Fatalf("expected deployment-state rejection, got %v", err)
	}
}

func samePortableHorizonPlan(left, right horizon.Plan) bool {
	return left.ID == right.ID && left.AssetID == right.AssetID && left.Scenario == right.Scenario &&
		left.ExpectedUsefulLifeMonths == right.ExpectedUsefulLifeMonths && left.ReplacementDate != nil && right.ReplacementDate != nil &&
		left.ReplacementDate.Equal(*right.ReplacementDate) && left.LifecycleStage == right.LifecycleStage &&
		left.ReplacementCostMinor == right.ReplacementCostMinor && left.Currency == right.Currency &&
		left.EffectiveFrom.Equal(right.EffectiveFrom) && left.Revision == right.Revision
}
