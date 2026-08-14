package patterns_test

// Requirement: REQ-PATTERNS-001. Feature: templates.schemas. GitHub: #8, #9.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/patterns"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

var errPatternsAuditUnavailable = errors.New("Patterns audit unavailable")
var errPatternsWriteLocked = errors.New("Patterns resource write locked")

type patternsExchangeAuditor struct {
	mu       sync.Mutex
	failNext bool
	attempts []foundation.AuditEvent
	events   map[string]foundation.AuditEvent
}

func (a *patternsExchangeAuditor) Record(_ context.Context, event foundation.AuditEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.attempts = append(a.attempts, event)
	if a.failNext {
		a.failNext = false
		return errPatternsAuditUnavailable
	}
	if a.events == nil {
		a.events = make(map[string]foundation.AuditEvent)
	}
	if existing, ok := a.events[event.ID]; ok && existing.OrganizationID != event.OrganizationID {
		return errors.New("audit event id collision")
	}
	a.events[event.ID] = event
	return nil
}

type patternsDenyWriteGate struct{}

func (patternsDenyWriteGate) CheckResourceWrite(context.Context, string, string) error {
	return errPatternsWriteLocked
}

func TestPatternsExchangeImporterRepairsAuditAndBypassesOnlyReservedWrite(t *testing.T) {
	auditor := &patternsExchangeAuditor{failNext: true}
	service, importer, err := patterns.NewServiceWithExchangeImporter(
		repository.NewMemoryPatternsStore(), patternsDenyWriteGate{}, auditor,
		patterns.ServiceConfig{OrganizationID: "patterns-audit-org", Now: func() time.Time {
			return time.Date(2026, time.August, 13, 20, 0, 0, 0, time.UTC)
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	operation := patterns.ExchangeImportOperation{Token: "patterns-audit-operation", OccurredAt: time.Date(2026, time.August, 13, 19, 45, 0, 0, time.UTC)}
	candidate := patterns.ExchangeTemplate{ID: "portable-schema", RecordType: "example.record", Name: "Portable schema", Versions: []patterns.ExchangeTemplateVersion{
		{Version: 1, Status: patterns.StatusActive, Description: "First", Fields: []patterns.Field{{Key: "name", Label: "Name", AccessibleLabel: "Name", CSVHeader: "name", Type: patterns.FieldText, Required: true}}},
		{Version: 2, Status: patterns.StatusActive, Description: "Second", Fields: []patterns.Field{{Key: "name", Label: "Name", AccessibleLabel: "Name", CSVHeader: "name", Type: patterns.FieldText, Required: true}, {Key: "state", Label: "State", AccessibleLabel: "State", CSVHeader: "state", Type: patterns.FieldEnum, Options: []string{"new", "ready"}}}},
	}}
	result, err := importer.ImportTemplate(context.Background(), operation, candidate)
	if !errors.Is(err, errPatternsAuditUnavailable) || !result.Committed || !result.Created {
		t.Fatalf("expected truthful post-commit audit failure, result=%#v err=%v", result, err)
	}
	loaded, err := service.ExchangeTemplate(context.Background(), candidate.ID)
	if err != nil || len(loaded.Versions) != 2 {
		t.Fatalf("committed history was not readable after audit failure: %#v err=%v", loaded, err)
	}
	repaired, err := importer.ImportTemplate(context.Background(), operation, candidate)
	if err != nil || !repaired.Committed || repaired.Created {
		t.Fatalf("repair replay did not converge: %#v err=%v", repaired, err)
	}
	if len(auditor.attempts) != 2 || auditor.attempts[0].ID != auditor.attempts[1].ID ||
		auditor.attempts[1].OrganizationID != "patterns-audit-org" || auditor.attempts[1].OccurredAt != operation.OccurredAt ||
		auditor.attempts[1].ActorID != "system:exchange" || auditor.attempts[1].CorrelationID != operation.Token {
		t.Fatalf("import audit identity was not deterministic and scoped: %#v", auditor.attempts)
	}
	if _, err := service.CreateVersion(context.Background(), candidate.ID, patterns.NewVersionInput{
		Description: "Local overwrite", Fields: candidate.Versions[1].Fields,
	}); !errors.Is(err, errPatternsWriteLocked) {
		t.Fatalf("ordinary Patterns write bypassed ownership fence: %v", err)
	}
}

func TestPatternsExchangeAuditIdentityIncludesOrganization(t *testing.T) {
	operation := patterns.ExchangeImportOperation{Token: "same-operation", OccurredAt: time.Date(2026, time.August, 13, 19, 50, 0, 0, time.UTC)}
	candidate := patterns.ExchangeTemplate{ID: "same-template", RecordType: "example.record", Name: "Same template", Versions: []patterns.ExchangeTemplateVersion{{
		Version: 1, Status: patterns.StatusActive, Fields: []patterns.Field{{Key: "name", Label: "Name", AccessibleLabel: "Name", CSVHeader: "name", Type: patterns.FieldText, Required: true}},
	}}}
	ids := make([]string, 0, 2)
	for _, organizationID := range []string{"patterns-org-one", "patterns-org-two"} {
		auditor := &patternsExchangeAuditor{}
		_, importer, err := patterns.NewServiceWithExchangeImporter(repository.NewMemoryPatternsStore(), nil, auditor, patterns.ServiceConfig{OrganizationID: organizationID})
		if err != nil {
			t.Fatal(err)
		}
		if result, err := importer.ImportTemplate(context.Background(), operation, candidate); err != nil || !result.Committed {
			t.Fatalf("import in %s: %#v err=%v", organizationID, result, err)
		}
		ids = append(ids, auditor.attempts[0].ID)
	}
	if ids[0] == ids[1] {
		t.Fatalf("organization-scoped imports reused audit id %q", ids[0])
	}
}
