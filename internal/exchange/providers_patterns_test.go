package exchange_test

// Requirements: REQ-EXCHANGE-001, REQ-PATTERNS-001. Features: migration.packages, templates.schemas. GitHub: #9, #8.

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/exchange"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/patterns"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

func TestPatternsProviderPreservesCompleteImmutableHistory(t *testing.T) {
	source, sourceImporter := newPatternsProviderService(t, "patterns-source")
	created, err := source.CreateTemplate(context.Background(), patterns.CreateTemplateInput{
		ID: "portable-intake", RecordType: "example.record", Name: "Portable intake", Description: "Initial definition",
		Fields: []patterns.Field{{Key: "name", Label: "Name", Type: patterns.FieldText, Required: true, MaximumLength: 80}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.CreateVersion(context.Background(), created.ID, patterns.NewVersionInput{
		Description: "Add an optional owner",
		Fields: []patterns.Field{
			{Key: "name", Label: "Name", Type: patterns.FieldText, Required: true, MaximumLength: 80},
			{Key: "ownerId", Label: "Owner", Type: patterns.FieldReference, ReferenceType: "people.identity", AllowHolding: true},
		},
	}); err != nil {
		t.Fatal(err)
	}
	provider, err := exchange.NewPatternsProvider(source, sourceImporter)
	if err != nil {
		t.Fatal(err)
	}
	records, err := provider.ListRecords(context.Background())
	if err != nil || len(records) != 1 {
		t.Fatalf("list Patterns records: %#v err=%v", records, err)
	}
	record := records[0]
	if record.ID != created.ID || record.Revision != 2 || len(record.Dependencies) != 0 ||
		bytes.Contains(record.Payload, []byte("patterns-source")) || bytes.Contains(record.Payload, []byte("system:patterns")) {
		t.Fatalf("unsafe or lossy Patterns projection: %#v", record)
	}

	target, targetImporter := newPatternsProviderService(t, "patterns-target")
	targetProvider, err := exchange.NewPatternsProvider(target, targetImporter)
	if err != nil {
		t.Fatal(err)
	}
	operation := exchange.ProviderImportOperation{Token: "patterns-operation", ExpectedCreated: true,
		OccurredAt: time.Date(2026, time.August, 13, 19, 30, 0, 0, time.UTC)}
	result, err := targetProvider.ImportRecord(context.Background(), operation, "source-system", record, nil)
	if err != nil || !result.Committed || !result.Created {
		t.Fatalf("import Patterns record: %#v err=%v", result, err)
	}
	history, err := target.ExchangeTemplate(context.Background(), created.ID)
	if err != nil || len(history.Versions) != 2 || history.Versions[0].Description != "Initial definition" ||
		history.Versions[1].Fields[1].ReferenceType != "people.identity" {
		t.Fatalf("Patterns history was not lossless: %#v err=%v", history, err)
	}
	operation.ExpectedCreated = false
	replay, err := targetProvider.ImportRecord(context.Background(), operation, "source-system", record, nil)
	if err != nil || !replay.Committed || replay.Created {
		t.Fatalf("replay Patterns record: %#v err=%v", replay, err)
	}
}

func TestPatternsProviderRejectsForeignCapabilityAndNoncanonicalPayload(t *testing.T) {
	service, importer := newPatternsProviderService(t, "patterns-owner")
	foreign, foreignImporter := newPatternsProviderService(t, "patterns-foreign")
	if _, err := exchange.NewPatternsProvider(service, foreignImporter); err == nil {
		t.Fatal("accepted another service's opaque Patterns importer")
	}
	provider, err := exchange.NewPatternsProvider(service, importer)
	if err != nil {
		t.Fatal(err)
	}
	record := exchange.Record{Type: "patterns.template", ID: "template-one", Revision: 1, Dependencies: []exchange.Reference{},
		Payload: []byte(`{"name":"Template","recordType":"example.record","versions":"[]"}`)}
	if _, err := provider.ImportRecord(context.Background(), exchange.ProviderImportOperation{Token: "operation-one", ExpectedCreated: true, OccurredAt: time.Now().UTC()}, "source", record, nil); !errors.Is(err, exchange.ErrInvalidInput) {
		t.Fatalf("accepted noncanonical or empty Patterns history: %v", err)
	}
	unsafe := exchange.Record{Type: "patterns.template", ID: "template-two", Revision: 1, Dependencies: []exchange.Reference{},
		Payload: []byte(`{"recordType":"example.record","name":"Template","versions":"[{\"description\":\"https://example.test/help?access_token=private\",\"version\":1,\"status\":\"active\",\"fields\":[{\"key\":\"name\",\"label\":\"Name\",\"type\":\"text\",\"required\":true,\"accessibleLabel\":\"Name\",\"csvHeader\":\"name\"}]}]"}`)}
	if _, err := provider.ImportRecord(context.Background(), exchange.ProviderImportOperation{Token: "operation-two", ExpectedCreated: true, OccurredAt: time.Now().UTC()}, "source", unsafe, nil); !errors.Is(err, exchange.ErrInvalidInput) {
		t.Fatalf("accepted credential-bearing URL hidden in nested Patterns history: %v", err)
	}
	created, err := service.CreateTemplate(context.Background(), patterns.CreateTemplateInput{
		ID: "canonical-template", RecordType: "example.record", Name: "Canonical template", Description: "Canonical payload fixture",
		Fields: []patterns.Field{{Key: "name", Label: "Name", Type: patterns.FieldText, Required: true, MaximumLength: 80}},
	})
	if err != nil {
		t.Fatal(err)
	}
	records, err := provider.ListRecords(context.Background())
	if err != nil || len(records) != 1 || records[0].ID != created.ID {
		t.Fatalf("list canonical Patterns fixture: %#v err=%v", records, err)
	}
	valid := records[0]
	for name, mutate := range map[string]func(exchange.Record) exchange.Record{
		"noncanonical top-level JSON": func(record exchange.Record) exchange.Record {
			record.Payload = append([]byte(" "), record.Payload...)
			return record
		},
		"invalid id":    func(record exchange.Record) exchange.Record { record.ID = "invalid id"; return record },
		"zero revision": func(record exchange.Record) exchange.Record { record.Revision = 0; return record },
		"dependency drift": func(record exchange.Record) exchange.Record {
			record.Dependencies = []exchange.Reference{{Type: "patterns.template", ID: "unexpected"}}
			return record
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := provider.ImportRecordExists(context.Background(), mutate(valid), nil); !errors.Is(err, exchange.ErrInvalidInput) {
				t.Fatalf("accepted invalid Patterns envelope or JSON bytes: %v", err)
			}
		})
	}
	_ = foreign
}

func newPatternsProviderService(t *testing.T, organizationID string) (*patterns.Service, patterns.ExchangeImporter) {
	t.Helper()
	service, importer, err := patterns.NewServiceWithExchangeImporter(repository.NewMemoryPatternsStore(), nil, foundation.NopAuditor{}, patterns.ServiceConfig{
		OrganizationID: organizationID,
		Now: func() time.Time {
			return time.Date(2026, time.August, 13, 19, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, importer
}
