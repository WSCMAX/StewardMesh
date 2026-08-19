package exchange

// Requirements: REQ-EXCHANGE-001. Feature: migration.packages.

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/stack"
)

func TestPortableTimestampClassificationCoversRegistry(t *testing.T) {
	t.Parallel()

	classifications := map[string][]string{
		"atlas.asset":                 {"createdAt", "updatedAt", "modelContext.defaultsEffectiveAt", "modelContext.appliedAt"},
		"atlas.model":                 {"createdAt", "updatedAt"},
		"atlas.identifier":            {"history[].createdAt", "history[].updatedAt", "history[].deactivatedAt"},
		"atlas.lifecycle-event":       {"occurredAt"},
		"atlas.catalog-configuration": {},
		"atlas.catalog-price":         {},
		"atlas.catalog-upgrade-path":  {},
		"people.site":                 {"createdAt", "updatedAt"},
		"people.building":             {"createdAt", "updatedAt"},
		"people.room":                 {"createdAt", "updatedAt"},
		"people.department":           {"createdAt", "updatedAt"},
		"people.identity":             {"createdAt", "updatedAt"},
		"people.assignment":           {"effectiveFrom", "effectiveTo", "createdAt"},
		"threads.tag":                 {},
		"threads.goal":                {},
		"threads.tag-rule":            {},
		"threads.goal-link":           {},
		"labels.definition":           {},
		"labels.assignment":           {},
		"vault.blob":                  {},
		"ledger.vendor":               {"createdAt", "updatedAt"},
		"ledger.purchase-order":       {"createdAt", "updatedAt"},
		"ledger.contract":             {"createdAt", "updatedAt"},
		"ledger.commitment":           {"createdAt", "updatedAt"},
		"ledger.budget":               {"createdAt", "updatedAt"},
		"ledger.cost":                 {"createdAt", "updatedAt"},
		"horizon.plan":                {},
		"patterns.template":           {},
		"stack.product":               {},
		"stack.version":               {},
		"stack.installation":          {"installedAt", "lastUsedAt", "removedAt"},
		"stack.license":               {},
		"stack.assignment":            {"assignedAt", "lastUsedAt", "endedAt"},
		"signals.rule":                {"createdAt", "updatedAt"},
		"signals.subscription":        {"createdAt", "updatedAt"},
		"reach.provider":              {"createdAt", "updatedAt"},
		"reach.template":              {"createdAt", "updatedAt"},
		"reach.subscriber-group":      {"createdAt", "updatedAt"},
		"directory.group":             {"createdAt", "updatedAt"},
		"directory.membership":        {"createdAt", "updatedAt"},
		"bridge.oauth-client":         {"revokedAt"},
	}

	want := PortableRecordTypes()
	got := make([]string, 0, len(classifications))
	for recordType, fields := range classifications {
		got = append(got, recordType)
		seen := make(map[string]struct{}, len(fields))
		for _, field := range fields {
			if field == "" {
				t.Fatalf("%s has an empty timestamp field classification", recordType)
			}
			if _, duplicate := seen[field]; duplicate {
				t.Fatalf("%s classifies %s more than once", recordType, field)
			}
			seen[field] = struct{}{}
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("timestamp classifications do not cover the portable registry\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestParsePortableInstantRejectsLossyExchangeValues(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"2026-08-13T12:00:00.123456789Z",
		"2026-08-13T07:00:00.123456-05:00",
		"2026-08-13T12:00:00.123456000Z",
	} {
		if _, err := parsePortableInstant(value, 1970); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("parsePortableInstant(%q) error = %v; want ErrInvalidInput", value, err)
		}
	}
}

func TestPortableExporterValidationRejectsLegacyLossyValues(t *testing.T) {
	t.Parallel()

	canonical := time.Date(2026, time.August, 13, 12, 0, 0, 123456000, time.UTC)
	for _, value := range []time.Time{
		canonical.Add(time.Nanosecond),
		canonical.In(time.FixedZone("source", -5*60*60)),
	} {
		if err := validatePortableInstants(1970, value); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("validatePortableInstants(%v, %v) error = %v; want ErrInvalidInput", value, value.Location(), err)
		}
	}
}

func TestAtlasModelContextRejectsLossyNestedInstants(t *testing.T) {
	t.Parallel()

	canonical := time.Date(2026, time.August, 13, 12, 0, 0, 123456000, time.UTC)
	value := domain.AssetModelContext{
		DefaultsEffectiveAt: canonical,
		AppliedAt:           canonical.Add(time.Nanosecond),
	}
	if _, err := canonicalAtlasModelContext(&value); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("canonicalAtlasModelContext() error = %v; want ErrInvalidInput", err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseAtlasModelContext(string(encoded)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("parseAtlasModelContext() error = %v; want ErrInvalidInput", err)
	}
}

func TestStackProviderRejectsEveryLossyPortableInstantField(t *testing.T) {
	t.Parallel()

	fieldsByType := map[string][]string{
		"stack.installation": {"installedAt", "lastUsedAt", "removedAt"},
		"stack.assignment":   {"assignedAt", "lastUsedAt", "endedAt"},
	}
	for recordType, fields := range fieldsByType {
		for _, field := range fields {
			field := field
			t.Run(recordType+"/"+field, func(t *testing.T) {
				t.Parallel()
				payload, err := json.Marshal(map[string]string{field: "2026-08-13T12:00:00.123456789Z"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := projectStackPayload(recordType, payload); !errors.Is(err, stack.ErrInvalidInput) {
					t.Fatalf("projectStackPayload() error = %v; want stack.ErrInvalidInput", err)
				}
				if _, err := restoreStackPayload(Record{Type: recordType, ID: "record-one", Revision: 1, Payload: payload}); !errors.Is(err, stack.ErrInvalidInput) {
					t.Fatalf("restoreStackPayload() error = %v; want stack.ErrInvalidInput", err)
				}
			})
		}
	}
}
