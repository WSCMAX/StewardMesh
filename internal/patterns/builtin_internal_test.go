package patterns

import "testing"

func TestBuiltInTemplateReferenceChoosesHighestActiveVersionDeterministically(t *testing.T) {
	templates := []Template{
		{ID: "later-id", RecordType: "example.row", Version: 2, Status: StatusActive},
		{ID: "retired-v3", RecordType: "example.row", Version: 3, Status: StatusRetired},
		{ID: "earlier-id", RecordType: "example.row", Version: 2, Status: StatusActive},
		{ID: "old", RecordType: "example.row", Version: 1, Status: StatusActive},
	}
	id, version, ok := builtInTemplateReference(templates, "example.row")
	if !ok || id != "earlier-id" || version != 2 {
		t.Fatalf("expected stable highest active reference, got %q v%d ok=%t", id, version, ok)
	}
}
