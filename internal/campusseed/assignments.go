package campusseed

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/maxlemke/stewardmesh/internal/people"
)

func (s *Seeder) seedPeopleAssetAssignments(ctx context.Context) error {
	if len(s.employeeAssetIDs) == 0 {
		return nil
	}
	effectiveFrom := s.now().Add(-180 * 24 * time.Hour)
	created := 0
	for employeeID, assetID := range s.employeeAssetIDs {
		if created >= 48 {
			break
		}
		_, err := s.people.CreateAssetAssignment(ctx, people.CreateAssetAssignmentInput{
			AssetID: assetID, AssigneeKind: people.AssigneeIdentity,
			AssigneeID: employeeID, Role: people.AssignmentPrimary,
			EffectiveFrom: effectiveFrom, DueAt: dueAtForSeed(s.now(), created),
		})
		if err != nil && !errors.Is(err, people.ErrConflict) {
			return fmt.Errorf("create primary asset assignment for %q: %w", assetID, err)
		}
		created++
	}
	if departmentID := s.departmentIDs["it-services"]; departmentID != "" && len(s.laptopAssetIDs) > 0 {
		_, err := s.people.CreateAssetAssignment(ctx, people.CreateAssetAssignmentInput{
			AssetID: s.laptopAssetIDs[0], AssigneeKind: people.AssigneeDepartment,
			AssigneeID: departmentID, Role: people.AssignmentDepartment,
			EffectiveFrom: effectiveFrom,
		})
		if err != nil && !errors.Is(err, people.ErrConflict) {
			return fmt.Errorf("create department asset assignment: %w", err)
		}
	}
	return nil
}

func dueAtForSeed(now time.Time, index int) *time.Time {
	if index%5 != 0 {
		return nil
	}
	due := now.Add(30 * 24 * time.Hour)
	return &due
}
