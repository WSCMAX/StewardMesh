package campusseed

import (
	"context"
	"errors"
	"fmt"

	"github.com/maxlemke/stewardmesh/internal/exchange"
)

func (s *ExtendedSeeder) seedExchangePackages(ctx context.Context) error {
	now := s.now()
	holding := exchange.Package{
		OrganizationID: s.organizationID, PackageID: campusDemoExchangeHoldingID,
		Direction: exchange.DirectionImport, SchemaVersion: exchange.SchemaVersion,
		SourceSystemID: SourceSystemID,
		ArchiveSHA256:  "0000000000000000000000000000000000000000000000000000000000000001",
		SizeBytes:      1024, FileMode: exchange.FileModeMetadata,
		Status: exchange.StatusHolding, RecordCount: 1, FileCount: 0,
		HoldingCount: 1, CreatedCount: 0, UnchangedCount: 0,
		Records: []exchange.RecordOutcome{{
			Type: "stack.product", ID: "campus-demo-import-product", Revision: 1,
			Checksum: "0000000000000000000000000000000000000000000000000000000000000002",
			Status: exchange.OutcomeHolding,
			MissingDependencies: []exchange.Reference{{Type: "stack.version", ID: "campus-demo-import-version"}},
		}},
		CreatedBy: ActorID, CreatedAt: now, UpdatedAt: now,
	}
	if _, _, err := s.exchangeStore.CreatePackage(ctx, holding); err != nil && !errors.Is(err, exchange.ErrConflict) {
		return fmt.Errorf("create campus demo holding exchange package: %w", err)
	}
	claimed := exchange.Package{
		OrganizationID: s.organizationID, PackageID: campusDemoExchangeClaimedID,
		Direction: exchange.DirectionImport, SchemaVersion: exchange.SchemaVersion,
		SourceSystemID: SourceSystemID,
		ArchiveSHA256:  "0000000000000000000000000000000000000000000000000000000000000003",
		SizeBytes:      2048, FileMode: exchange.FileModeMetadata,
		Status: exchange.StatusCompleted, RecordCount: 1, FileCount: 0,
		HoldingCount: 0, CreatedCount: 1, UnchangedCount: 0,
		Records: []exchange.RecordOutcome{{
			Type: "stack.product", ID: "microsoft-365", Revision: 1,
			Checksum: "0000000000000000000000000000000000000000000000000000000000000004",
			Status: exchange.OutcomeCreated,
		}},
		CreatedBy: ActorID, CreatedAt: now, UpdatedAt: now,
	}
	if _, _, err := s.exchangeStore.CreatePackage(ctx, claimed); err != nil && !errors.Is(err, exchange.ErrConflict) {
		return fmt.Errorf("create campus demo claimed exchange package: %w", err)
	}
	return nil
}
