package patterns_test

// Requirement: REQ-PATTERNS-001. Feature: templates.schemas. GitHub: #8.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/patterns"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

func TestBuiltInTemplatesCoverCoreRecordsAndEveryFieldType(t *testing.T) {
	service := newPatternsService(t)
	items, err := service.ListTemplates(context.Background(), patterns.ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	wantRecords := []string{
		"foundation.organization",
		"atlas.asset", "atlas.model", "atlas.identifier", "atlas.lifecycle-event",
		"atlas.catalog-configuration", "atlas.catalog-price", "atlas.catalog-upgrade-path",
		"people.site", "people.building", "people.room", "people.department", "people.identity", "people.assignment",
		"threads.tag", "threads.goal", "threads.tag-rule", "threads.goal-link", "vault.blob", "ledger.vendor", "ledger.purchase-order", "ledger.contract",
		"ledger.commitment", "ledger.budget", "ledger.cost", "horizon.plan", "guard.role", "guard.role-assignment",
		"guard.account", "guard.policy-bundle", "guard.resource-ownership", "patterns.template",
	}
	records := map[string]bool{}
	types := map[patterns.FieldType]bool{}
	for _, item := range items {
		if !item.BuiltIn || item.Version != 1 || item.Status != patterns.StatusActive {
			t.Fatalf("unexpected built-in metadata: %#v", item)
		}
		records[item.RecordType] = true
		for _, field := range item.Fields {
			types[field.Type] = true
			if field.AccessibleLabel == "" || field.CSVHeader == "" {
				t.Fatalf("field metadata is incomplete: %#v", field)
			}
		}
	}
	for _, record := range wantRecords {
		if !records[record] {
			t.Errorf("missing core template for %s", record)
		}
	}
	for _, fieldType := range []patterns.FieldType{patterns.FieldText, patterns.FieldNumber, patterns.FieldDate, patterns.FieldMoney, patterns.FieldEnum, patterns.FieldAttachment, patterns.FieldReference} {
		if !types[fieldType] {
			t.Errorf("missing field type %s", fieldType)
		}
	}
}

func TestCustomTemplatesAreCopyableAndAppendOnlyVersioned(t *testing.T) {
	service := newPatternsService(t)
	created, err := service.CreateTemplate(context.Background(), patterns.CreateTemplateInput{
		ID: "custom-intake", RecordType: "atlas.asset", Name: "Custom intake",
		Fields: []patterns.Field{{Key: "name", Label: "Name", Help: "Shown on inventory pages.", Type: patterns.FieldText, Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Version != 1 || created.BuiltIn || created.Fields[0].AccessibleLabel != "Name" || created.Fields[0].CSVHeader != "name" {
		t.Fatalf("unexpected custom template: %#v", created)
	}
	copy, err := service.CopyTemplate(context.Background(), "builtin-atlas-asset", 1, patterns.CopyTemplateInput{ID: "asset-copy", Name: "Asset copy"})
	if err != nil {
		t.Fatal(err)
	}
	if copy.BuiltIn || copy.RecordType != "atlas.asset" || len(copy.Fields) < 2 {
		t.Fatalf("unexpected built-in copy: %#v", copy)
	}
	versionTwo, err := service.CreateVersion(context.Background(), created.ID, patterns.NewVersionInput{
		Description: "Second immutable version.",
		Fields: []patterns.Field{
			{Key: "name", Label: "Name", Type: patterns.FieldText, Required: true},
			{Key: "commissionedOn", Label: "Commissioned on", Type: patterns.FieldDate},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	versionOne, err := service.GetTemplate(context.Background(), created.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	latest, err := service.GetTemplate(context.Background(), created.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if versionTwo.Version != 2 || latest.Version != 2 || len(versionOne.Fields) != 1 || len(latest.Fields) != 2 {
		t.Fatalf("versions were not preserved: v1=%#v v2=%#v latest=%#v", versionOne, versionTwo, latest)
	}
}

func TestValidationSupportsTypedValuesAndVisibleHoldingRecords(t *testing.T) {
	service := newPatternsService(t)
	minimum, maximum := 1.0, 10.0
	template, err := service.CreateTemplate(context.Background(), patterns.CreateTemplateInput{
		ID: "typed-intake", RecordType: "exchange.row", Name: "Typed intake",
		Fields: []patterns.Field{
			{Key: "title", Label: "Title", Help: "A useful name.", Type: patterns.FieldText, Required: true, MaximumLength: 20},
			{Key: "quantity", Label: "Quantity", Type: patterns.FieldNumber, Minimum: &minimum, Maximum: &maximum},
			{Key: "dueOn", Label: "Due on", Type: patterns.FieldDate, Required: true},
			{Key: "currency", Label: "Currency", Type: patterns.FieldText, Required: true, MaximumLength: 3},
			{Key: "budgetMinor", Label: "Budget", Type: patterns.FieldMoney, Required: true, CurrencyField: "currency"},
			{Key: "state", Label: "State", Type: patterns.FieldEnum, Required: true, Options: []string{"new", "ready"}},
			{Key: "evidence", Label: "Evidence", Type: patterns.FieldAttachment, AllowHolding: true, ReferenceType: "vault.blob"},
			{Key: "owner", Label: "Owner", Type: patterns.FieldReference, Required: true, AllowHolding: true, ReferenceType: "people.identity"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]any{"title": "  Row one  ", "quantity": 3.5, "dueOn": "2026-08-12", "currency": "USD", "budgetMinor": float64(1250), "state": "ready", "evidence": "blob-1", "owner": "person-1"}
	valid, err := service.Validate(context.Background(), template.ID, 1, patterns.ValidationInput{Values: values})
	if err != nil {
		t.Fatal(err)
	}
	if valid.Status != patterns.ValidationValid || len(valid.Errors) != 0 || valid.NormalizedValues["title"] != "Row one" || valid.NormalizedValues["budgetMinor"] != int64(1250) {
		t.Fatalf("unexpected valid result: %#v", valid)
	}
	holding, err := service.Validate(context.Background(), template.ID, 1, patterns.ValidationInput{Values: values, MissingReferences: []string{"owner"}, AllowHoldingRecord: true})
	if err != nil {
		t.Fatal(err)
	}
	if holding.Status != patterns.ValidationHolding || len(holding.HoldingReferences) != 1 || holding.HoldingReferences[0].Field != "owner" {
		t.Fatalf("missing reference was not held visibly: %#v", holding)
	}
	attachmentHolding, err := service.Validate(context.Background(), template.ID, 1, patterns.ValidationInput{Values: values, MissingReferences: []string{"evidence"}, AllowHoldingRecord: true})
	if err != nil {
		t.Fatal(err)
	}
	if attachmentHolding.Status != patterns.ValidationHolding || len(attachmentHolding.HoldingReferences) != 1 || attachmentHolding.HoldingReferences[0].Field != "evidence" {
		t.Fatalf("missing attachment was not held visibly: %#v", attachmentHolding)
	}
	invalidValues := map[string]any{"title": "A title", "quantity": 11.0, "dueOn": "12/08/2026", "currency": "usd", "budgetMinor": 1.5, "state": "unknown", "evidence": "not/a/stable/id", "owner": "person-1", "surprise": true}
	invalid, err := service.Validate(context.Background(), template.ID, 1, patterns.ValidationInput{Values: invalidValues, MissingReferences: []string{"owner"}})
	if err != nil {
		t.Fatal(err)
	}
	if invalid.Status != patterns.ValidationInvalid || len(invalid.Errors) < 7 || len(invalid.HoldingReferences) != 0 {
		t.Fatalf("typed errors were not surfaced: %#v", invalid)
	}
}

func TestTemplateMetadataRejectsSpreadsheetFormulaHeadersAndUnknownMissingKeys(t *testing.T) {
	service := newPatternsService(t)
	if _, err := service.CreateTemplate(context.Background(), patterns.CreateTemplateInput{
		RecordType: "exchange.row", Name: "Unsafe CSV",
		Fields: []patterns.Field{{Key: "name", Label: "Name", Type: patterns.FieldText, CSVHeader: "=HYPERLINK(example)"}},
	}); !errors.Is(err, patterns.ErrInvalidInput) {
		t.Fatalf("expected formula-like CSV header rejection, got %v", err)
	}
	if _, err := service.Validate(context.Background(), "builtin-atlas-asset", 1, patterns.ValidationInput{
		Values: map[string]any{"name": "Asset", "kind": "server"}, MissingReferences: []string{"unknown"},
	}); !errors.Is(err, patterns.ErrInvalidInput) {
		t.Fatalf("expected unknown missing-reference key rejection, got %v", err)
	}
}

func TestCSVTemplateUsesVersionedHeaders(t *testing.T) {
	service := newPatternsService(t)
	contents, err := service.CSVTemplate(context.Background(), "builtin-atlas-asset", 1)
	if err != nil {
		t.Fatal(err)
	}
	line := string(contents)
	if !strings.HasPrefix(line, "name,kind,assetTag") || !strings.HasSuffix(line, "\n") {
		t.Fatalf("unexpected CSV template %q", line)
	}
}

func newPatternsService(t *testing.T) *patterns.Service {
	t.Helper()
	service, err := patterns.NewService(repository.NewMemoryPatternsStore(), foundation.NopAuditor{}, patterns.ServiceConfig{
		OrganizationID: "example-org",
		Now:            func() time.Time { return time.Date(2026, time.August, 12, 9, 30, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}
