package campusseed

import "testing"

func TestCampusContractDefinitionsReferenceKnownVendorsAndDocuments(t *testing.T) {
	vendors := map[string]struct{}{"microsoft": {}, "adobe": {}, "autodesk": {}, "cdw": {}}
	documents := map[string]struct{}{
		"m365-contract-docx": {}, "adobe-contract-html": {}, "autodesk-quote-pdf": {}, "warranty-pdf": {},
	}
	definitions := []struct {
		id, vendor string
		docs       []string
	}{
		{"contract-m365-campus", "microsoft", []string{"m365-contract-docx"}},
		{"contract-adobe-campus", "adobe", []string{"adobe-contract-html"}},
		{"contract-autodesk-campus", "autodesk", []string{"autodesk-quote-pdf"}},
		{"contract-warranty-campus", "cdw", []string{"warranty-pdf"}},
	}
	for _, definition := range definitions {
		if _, ok := vendors[definition.vendor]; !ok {
			t.Fatalf("unknown vendor %q for contract %q", definition.vendor, definition.id)
		}
		for _, slug := range definition.docs {
			if _, ok := documents[slug]; !ok {
				t.Fatalf("unknown document slug %q for contract %q", slug, definition.id)
			}
		}
	}
}

func TestSoftwareContractIDsMapOfferings(t *testing.T) {
	for _, offering := range campusLicenseOfferings {
		contractID := softwareContractID(offering.Slug)
		if contractID == "" {
			t.Fatalf("expected contract id for offering %q", offering.Slug)
		}
	}
}
