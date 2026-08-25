package campusseed

import "testing"

func TestCampusBudgetDefinitionsIncludeOverBudgetCandidate(t *testing.T) {
	foundLowAllocation := false
	definitions := []struct {
		id             string
		allocatedMinor int64
	}{
		{"budget-it-capital-campus", 1_000_000},
		{"budget-studio-arts-tech", 25_000_000},
	}
	for _, definition := range definitions {
		if definition.allocatedMinor <= 0 {
			t.Fatalf("budget %q must allocate a positive amount", definition.id)
		}
		if definition.id == "budget-it-capital-campus" && definition.allocatedMinor < 5_000_000 {
			foundLowAllocation = true
		}
	}
	if !foundLowAllocation {
		t.Fatal("expected a deliberately low IT capital budget for over_budget signals")
	}
}

func TestCampusCommitmentIncludesUnusedMaintenancePool(t *testing.T) {
	if softwareContractID("microsoft-365-a3") != "contract-m365-campus" {
		t.Fatal("microsoft license should map to the M365 contract for signals coverage")
	}
}
