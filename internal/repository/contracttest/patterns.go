package contracttest

// Provider-neutral Patterns adapter contract.
// Requirement: REQ-PATTERNS-001. Feature: templates.schemas. GitHub: #8.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/patterns"
)

func PatternsStore(t testing.TB, subject patterns.Store, organizationID, suffix string) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	minimum := 1.0
	versionOne := patterns.Template{
		ID: "patterns-" + suffix, OrganizationID: organizationID, RecordType: "exchange.row", Name: "Intake " + suffix,
		Description: "First version", Version: 1, Status: patterns.StatusActive, CreatedBy: "account-one", CreatedAt: now,
		Fields: []patterns.Field{{Key: "state", Label: "State", Type: patterns.FieldEnum, Required: true, Options: []string{"new", "ready"}, AccessibleLabel: "State", CSVHeader: "state"}, {Key: "quantity", Label: "Quantity", Type: patterns.FieldNumber, Minimum: &minimum, AccessibleLabel: "Quantity", CSVHeader: "quantity"}},
	}
	if _, err := subject.GetTemplate(ctx, organizationID, versionOne.ID, 0); !errors.Is(err, patterns.ErrNotFound) {
		t.Fatalf("expected missing Patterns template, got %v", err)
	}
	created, err := subject.CreateTemplate(ctx, versionOne)
	if err != nil {
		t.Fatal(err)
	}
	if created.Version != 1 || created.ID != versionOne.ID {
		t.Fatalf("unexpected created Patterns template %#v", created)
	}
	versionOne.Fields[0].Options[0] = "mutated"
	*versionOne.Fields[1].Minimum = 99
	loaded, err := subject.GetTemplate(ctx, organizationID, created.ID, 1)
	if err != nil || loaded.Fields[0].Options[0] != "new" || loaded.Fields[1].Minimum == nil || *loaded.Fields[1].Minimum != 1 {
		t.Fatalf("Patterns template was not defensively persisted: %#v err=%v", loaded, err)
	}
	if _, err := subject.CreateTemplate(ctx, loaded); !errors.Is(err, patterns.ErrConflict) {
		t.Fatalf("expected duplicate Patterns ID conflict, got %v", err)
	}
	duplicateName := loaded
	duplicateName.ID += "-duplicate"
	if _, err := subject.CreateTemplate(ctx, duplicateName); !errors.Is(err, patterns.ErrConflict) {
		t.Fatalf("expected duplicate Patterns name conflict, got %v", err)
	}
	items, err := subject.ListTemplates(ctx, organizationID, patterns.ListQuery{RecordType: "exchange.row"})
	if err != nil || len(items) != 1 || items[0].ID != loaded.ID {
		t.Fatalf("unexpected Patterns list %#v err=%v", items, err)
	}
	if isolated, err := subject.ListTemplates(ctx, organizationID+"-other", patterns.ListQuery{}); err != nil || len(isolated) != 0 {
		t.Fatalf("expected Patterns organization isolation, items=%#v err=%v", isolated, err)
	}
	if _, err := subject.GetTemplate(ctx, organizationID+"-other", loaded.ID, 0); !errors.Is(err, patterns.ErrNotFound) {
		t.Fatalf("expected organization-isolated Patterns lookup, got %v", err)
	}

	versionTwo := loaded
	versionTwo.Version = 2
	versionTwo.Description = "Second version"
	versionTwo.Fields = append(versionTwo.Fields, patterns.Field{Key: "dueOn", Label: "Due on", Type: patterns.FieldDate, AccessibleLabel: "Due on", CSVHeader: "dueOn"})
	versionTwo.CreatedAt = now.Add(time.Minute)
	stored, err := subject.CreateVersion(ctx, versionTwo)
	if err != nil || stored.Version != 2 {
		t.Fatalf("create Patterns version two: %#v err=%v", stored, err)
	}
	latest, err := subject.GetTemplate(ctx, organizationID, loaded.ID, 0)
	if err != nil || latest.Version != 2 || len(latest.Fields) != 3 {
		t.Fatalf("unexpected latest Patterns version %#v err=%v", latest, err)
	}
	preserved, err := subject.GetTemplate(ctx, organizationID, loaded.ID, 1)
	if err != nil || preserved.Version != 1 || len(preserved.Fields) != 2 {
		t.Fatalf("version one was not preserved %#v err=%v", preserved, err)
	}
	if _, err := subject.CreateVersion(ctx, versionTwo); !errors.Is(err, patterns.ErrConflict) {
		t.Fatalf("expected stale Patterns version conflict, got %v", err)
	}
}
