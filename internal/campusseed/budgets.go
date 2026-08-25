package campusseed

import (
	"context"
	"errors"
	"fmt"

	"github.com/maxlemke/stewardmesh/internal/ledger"
)

func (s *Seeder) seedBudgets(ctx context.Context) error {
	fiscalPeriod := fmt.Sprintf("FY%d", s.now().Year())
	definitions := []struct {
		id, name, departmentSlug string
		siteScoped               bool
		allocatedMinor           int64
	}{
		{
			id: "budget-it-capital-campus", name: "FY2026 IT Capital Refresh",
			siteScoped: true, allocatedMinor: 1_000_000,
		},
		{
			id: "budget-studio-arts-tech", name: "FY2026 Studio Arts Instructional Technology",
			departmentSlug: "studio-arts", allocatedMinor: 25_000_000,
		},
	}
	if s.budgetIDs == nil {
		s.budgetIDs = make(map[string]string)
	}
	for _, definition := range definitions {
		input := ledger.CreateBudgetInput{
			ID: definition.id, Name: definition.name,
			FiscalPeriod: fiscalPeriod, Scenario: "baseline",
			Currency: "USD", AllocatedMinor: definition.allocatedMinor,
		}
		if definition.siteScoped {
			input.SiteID = s.siteID
		}
		if definition.departmentSlug != "" {
			input.DepartmentID = s.departmentIDs[definition.departmentSlug]
		}
		created, err := s.ledger.CreateBudget(ctx, input)
		if err != nil && !errors.Is(err, ledger.ErrConflict) {
			return fmt.Errorf("create budget %q: %w", definition.name, err)
		}
		budgetID := definition.id
		if err == nil {
			budgetID = created.ID
		}
		s.budgetIDs[definition.id] = budgetID
	}
	return nil
}
