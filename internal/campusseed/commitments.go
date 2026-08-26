package campusseed

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/maxlemke/stewardmesh/internal/ledger"
)

func (s *Seeder) seedCommitments(ctx context.Context) error {
	now := s.now()
	fiscalPeriod := fmt.Sprintf("FY%d", now.Year())
	startsOn := time.Date(now.Year(), 7, 1, 0, 0, 0, 0, time.UTC)
	endsOn := time.Date(now.Year()+1, 6, 30, 0, 0, 0, 0, time.UTC)
	unusedEndsOn := now.Add(75 * 24 * time.Hour)

	definitions := []struct {
		id, contractSlug, kind, description string
		amountMinor                           int64
		endsOn                                time.Time
	}{
		{
			id: "commitment-m365-subscription", contractSlug: "contract-m365-campus",
			kind: "subscription", description: "Microsoft 365 A3 annual subscription commitment",
			amountMinor: 316_800_000, endsOn: endsOn,
		},
		{
			id: "commitment-adobe-subscription", contractSlug: "contract-adobe-campus",
			kind: "subscription", description: "Adobe Creative Cloud annual subscription commitment",
			amountMinor: 64_620_000, endsOn: endsOn,
		},
		{
			id: "commitment-maintenance-unused", contractSlug: "contract-warranty-campus",
			kind: "maintenance", description: "Unused campus print-services maintenance pool",
			amountMinor: 450_000, endsOn: unusedEndsOn,
		},
	}
	if s.commitmentIDs == nil {
		s.commitmentIDs = make(map[string]string)
	}
	for _, definition := range definitions {
		created, err := s.ledger.CreateCommitment(ctx, ledger.CreateCommitmentInput{
			ID: definition.id, ContractID: s.contractID(definition.contractSlug),
			Kind: definition.kind, Description: definition.description,
			Currency: "USD", AmountMinor: definition.amountMinor,
			StartsOn: startsOn, EndsOn: definition.endsOn,
			FiscalPeriod: fiscalPeriod, Scenario: "baseline",
		})
		if err != nil && !errors.Is(err, ledger.ErrConflict) {
			return fmt.Errorf("create commitment %q: %w", definition.description, err)
		}
		commitmentID := definition.id
		if err == nil {
			commitmentID = created.ID
		}
		s.commitmentIDs[definition.id] = commitmentID
	}
	return nil
}

func (s *Seeder) seedReconciliationDemoCost(ctx context.Context) error {
	fiscalPeriod := fmt.Sprintf("FY%d", s.now().Year())
	_, err := s.ledger.ReconcileCost(ctx, ledger.ReconcileCostInput{
		ID: "campus-demo-unreconciled-cost", Description: "Legacy vendor invoice awaiting source reconciliation",
		Kind: "actual", Currency: "USD", AmountMinor: 125_000,
		FiscalPeriod: fiscalPeriod, Scenario: "baseline",
	})
	if err != nil && !errors.Is(err, ledger.ErrConflict) {
		return fmt.Errorf("create reconciliation demo cost: %w", err)
	}
	return nil
}
