package repository_test

// Requirement: REQ-SIGNALS-001. Feature: alerts.rules. GitHub: #11.

import (
	"context"
	"testing"

	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/repository/contracttest"
	"github.com/maxlemke/stewardmesh/internal/signals"
)

func TestMemorySignalsStoreContract(t *testing.T) {
	contracttest.SignalsStore(t, repository.NewMemorySignalsStore(), "memory-signals", "memory")
}

func TestMemorySignalsStorePreservesEmptyThresholdCollection(t *testing.T) {
	store := repository.NewMemorySignalsStore()
	rule, err := store.CreateRule(context.Background(), signals.Rule{
		ID: "rule-1", OrganizationID: "organization-1", Name: "Over budget",
		Condition: signals.ConditionOverBudget, Severity: signals.SeverityWarning,
	})
	if err != nil {
		t.Fatalf("create thresholdless rule: %v", err)
	}
	if rule.ThresholdDays == nil {
		t.Fatal("thresholdless rule must expose an empty collection, not null")
	}
	listed, err := store.ListRules(context.Background(), "organization-1")
	if err != nil || len(listed) != 1 || listed[0].ThresholdDays == nil {
		t.Fatalf("list thresholdless rule = %#v, err = %v", listed, err)
	}
}
