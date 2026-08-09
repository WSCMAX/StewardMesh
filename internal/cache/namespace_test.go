package cache

// Requirement: REQ-PLATFORM-VALKEY-001.

import (
	"errors"
	"strings"
	"testing"
)

func TestNamespaceKeysAreVersionedIsolatedAndDoNotEmbedRawDimensions(t *testing.T) {
	first, err := NewNamespace("stewardmesh", "v1", "org-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewNamespace("stewardmesh", "v1", "org-2")
	if err != nil {
		t.Fatal(err)
	}
	nextVersion, err := NewNamespace("stewardmesh", "v2", "org-1")
	if err != nil {
		t.Fatal(err)
	}
	account := "person@example.com"
	firstKey, err := first.Key("guard-login", account, "192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	repeatKey, err := first.Key("guard-login", account, "192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	secondKey, err := second.Key("guard-login", account, "192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	nextVersionKey, err := nextVersion.Key("guard-login", account, "192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	if firstKey != repeatKey {
		t.Fatal("expected deterministic cache key")
	}
	if firstKey == secondKey {
		t.Fatal("expected organization-isolated cache keys")
	}
	if firstKey == nextVersionKey {
		t.Fatal("expected schema-version-isolated cache keys")
	}
	if !strings.HasPrefix(firstKey, "stewardmesh:v1:org:org-1:guard-login:") {
		t.Fatalf("unexpected namespace prefix %q", firstKey)
	}
	if strings.Contains(firstKey, account) || strings.Contains(firstKey, "192.0.2.10") {
		t.Fatalf("expected raw dimensions to be hashed, got %q", firstKey)
	}
}

func TestNamespaceRejectsUnsafeSegmentsAndEmptyDimensions(t *testing.T) {
	if _, err := NewNamespace("stewardmesh:other", "v1", "org-1"); err == nil {
		t.Fatal("expected unsafe namespace prefix to be rejected")
	}
	namespace, err := NewNamespace("stewardmesh", "v1", "org-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := namespace.Key("unsafe:resource"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected invalid resource error, got %v", err)
	}
	if _, err := namespace.Key("guard-login", ""); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected empty dimension to be rejected, got %v", err)
	}
	if _, err := (Namespace{}).Key("guard-login"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected zero namespace to be rejected, got %v", err)
	}
}
