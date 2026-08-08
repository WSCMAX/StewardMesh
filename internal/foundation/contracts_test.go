package foundation

import (
	"context"
	"testing"
)

func TestScopeRoundTrip(t *testing.T) {
	scope := Scope{OrganizationID: "local-org", ActorID: "user-1", CorrelationID: "request-1"}
	if err := scope.Validate(); err != nil {
		t.Fatal(err)
	}
	actual, ok := ScopeFromContext(WithScope(context.Background(), scope))
	if !ok || actual != scope {
		t.Fatalf("expected scope %#v, got %#v", scope, actual)
	}
}

func TestCorrelationIDsAreRandomAndFixedLength(t *testing.T) {
	first, err := NewCorrelationID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewCorrelationID()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 32 || len(second) != 32 {
		t.Fatalf("expected 32-character identifiers, got %d and %d", len(first), len(second))
	}
	if first == second {
		t.Fatal("expected unique correlation identifiers")
	}
}
