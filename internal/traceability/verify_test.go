package traceability

// Requirements: REQ-FOUNDATION-001, REQ-ATLAS-CATALOG-001.
// Features: platform.foundation, inventory.products.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const foundationMarker = "REQ-FOUNDATION-001 platform.foundation"

func TestVerifyAcceptsCompleteRequirementTrace(t *testing.T) {
	root := t.TempDir()
	paths := []string{
		"docs/requirements/initial.md",
		"docs/features/dictionary.md",
		"docs/feature.md",
		"api/openapi.yaml",
		"internal/code.go",
		"migrations/0001.sql",
		"tests/code_test.go",
		"web/App.tsx",
	}
	for _, path := range paths {
		writeFixture(t, root, path, foundationMarker)
	}
	manifest := `{
		"entries": [{
			"requirementId": "REQ-FOUNDATION-001",
			"featureId": "platform.foundation",
			"artifacts": {
				"documentation": ["docs/feature.md"],
				"api": ["api/openapi.yaml"],
				"code": ["internal/code.go"],
				"schema": ["migrations/0001.sql"],
				"tests": ["tests/code_test.go"],
				"ui": ["web/App.tsx"]
			}
		}]
	}`
	writeFixture(t, root, "docs/requirements/traceability.json", manifest)
	if err := Verify(root, "docs/requirements/traceability.json"); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyReportsMissingArtifacts(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "docs/requirements/initial.md", foundationMarker)
	writeFixture(t, root, "docs/features/dictionary.md", foundationMarker)
	writeFixture(t, root, "docs/requirements/traceability.json", `{
		"entries": [{
			"requirementId": "REQ-FOUNDATION-001",
			"featureId": "platform.foundation",
			"artifacts": {}
		}]
	}`)
	err := Verify(root, "docs/requirements/traceability.json")
	if err == nil || !strings.Contains(err.Error(), "has no api artifacts") {
		t.Fatalf("expected missing artifact error, got %v", err)
	}
}

func TestVerifySupportsHonestFoundationTraceWithoutTransportOrUI(t *testing.T) {
	root := t.TempDir()
	paths := []string{
		"docs/requirements/initial.md",
		"docs/features/dictionary.md",
		"docs/feature.md",
		"internal/code.go",
		"migrations/0001.sql",
		"tests/code_test.go",
	}
	for _, path := range paths {
		writeFixture(t, root, path, foundationMarker)
	}
	writeFixture(t, root, "docs/requirements/traceability.json", `{
		"entries": [{
			"requirementId": "REQ-FOUNDATION-001",
			"featureId": "platform.foundation",
			"deliveryStatus": "foundation",
			"artifacts": {
				"documentation": ["docs/feature.md"],
				"code": ["internal/code.go"],
				"schema": ["migrations/0001.sql"],
				"tests": ["tests/code_test.go"]
			}
		}]
	}`)
	if err := Verify(root, "docs/requirements/traceability.json"); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejectsUnknownDeliveryStatus(t *testing.T) {
	if _, err := artifactKindsForStatus("half-built"); err == nil {
		t.Fatal("expected unknown delivery status rejection")
	}
}

func TestRequirementIDPatternSupportsCatalogFormats(t *testing.T) {
	for _, id := range []string{"REQ-FOUNDATION-001", "REQ-DIRECTORY-EXPANSION-001", "SEC-HTTP-001", "A11Y-001", "DOC-001"} {
		if !requirementIDPattern.MatchString(id) {
			t.Fatalf("expected %q to be supported", id)
		}
	}
}

func writeFixture(t *testing.T, root, path, contents string) {
	t.Helper()
	absolutePath := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolutePath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
