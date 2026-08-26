package campusseed

import (
	"context"
	"errors"
	"fmt"

	"github.com/maxlemke/stewardmesh/internal/labels"
	"github.com/maxlemke/stewardmesh/internal/people"
)

func (s *ExtendedSeeder) seedLabelDefinitions(ctx context.Context) error {
	definitions := []labels.CreateDefinitionInput{
		{
			ID: "campus-floor", Name: "Floor", ValueKind: labels.ValueSelect,
			ApplicableRecordTypes: []string{"people.building", "people.room", "atlas.asset"},
			Options: []string{"Basement", "1", "2", "3", "4"},
		},
		{
			ID: "campus-funding-source", Name: "Funding Source", ValueKind: labels.ValueSelect,
			ApplicableRecordTypes: []string{"atlas.asset", "ledger.purchase-order"},
			Options: []string{"Operating", "Capital", "Grant", "Student Tech Fee"},
		},
		{
			ID: "campus-demo-cohort", Name: "Demo Cohort", ValueKind: labels.ValueFlag,
			ApplicableRecordTypes: []string{"people.identity", "atlas.asset"},
		},
	}
	for _, input := range definitions {
		if _, err := s.labels.CreateDefinition(ctx, input); err != nil && !errors.Is(err, labels.ErrConflict) {
			return fmt.Errorf("create label definition %q: %w", input.Name, err)
		}
	}
	buildings, err := s.people.ListBuildings(ctx, s.siteID, people.Visibility{All: true})
	if err != nil {
		return fmt.Errorf("list buildings for label assignments: %w", err)
	}
	for _, building := range buildings {
		floor := "1"
		if _, err := s.labels.SetAssignment(ctx, labels.SetAssignmentInput{
			RecordType: "people.building", RecordID: building.ID,
			DefinitionID: "campus-floor", ValueText: floor,
		}); err != nil && !errors.Is(err, labels.ErrConflict) {
			return fmt.Errorf("assign floor label to building %q: %w", building.Name, err)
		}
	}
	if len(s.labAssetIDs["studio-arts-mac"]) > 0 {
		if _, err := s.labels.SetAssignment(ctx, labels.SetAssignmentInput{
			RecordType: "atlas.asset", RecordID: s.labAssetIDs["studio-arts-mac"][0],
			DefinitionID: "campus-funding-source", ValueText: "Capital",
		}); err != nil && !errors.Is(err, labels.ErrConflict) {
			return fmt.Errorf("assign funding source to studio arts asset: %w", err)
		}
		if _, err := s.labels.SetAssignment(ctx, labels.SetAssignmentInput{
			RecordType: "atlas.asset", RecordID: s.labAssetIDs["studio-arts-mac"][0],
			DefinitionID: "campus-demo-cohort", ValueText: "true",
		}); err != nil && !errors.Is(err, labels.ErrConflict) {
			return fmt.Errorf("assign demo cohort flag: %w", err)
		}
	}
	return nil
}
