package exchange_test

// Requirements: REQ-EXCHANGE-001, REQ-PATTERNS-001. Features: migration.packages, templates.schemas. GitHub: #9, #8.

import (
	"testing"

	"github.com/maxlemke/stewardmesh/internal/exchange"
	"github.com/maxlemke/stewardmesh/internal/patterns"
)

func TestPhaseOneRecordTypesHaveOneExchangeDisposition(t *testing.T) {
	if err := exchange.ValidateRecordTypeBoundary(); err != nil {
		t.Fatal(err)
	}
	if got, want := len(exchange.PortableRecordTypes()), 41; got != want {
		t.Fatalf("portable record type count = %d, want %d", got, want)
	}
	if got, want := len(exchange.ExplicitlyExcludedRecordTypes()), 12; got != want {
		t.Fatalf("excluded record type count = %d, want %d", got, want)
	}
	if got, want := len(patterns.CoreRecordTypes()), 53; got != want {
		t.Fatalf("Patterns core record type count = %d, want %d", got, want)
	}
	for _, exclusion := range exchange.ExplicitlyExcludedRecordTypes() {
		if exclusion.Reason == "" {
			t.Fatalf("excluded record type %q has no reason", exclusion.Type)
		}
	}
}
