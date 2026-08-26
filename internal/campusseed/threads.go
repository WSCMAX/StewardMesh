package campusseed

import (
	"context"
	"errors"
	"fmt"

	"github.com/maxlemke/stewardmesh/internal/threads"
)

func (s *ExtendedSeeder) seedThreads(ctx context.Context) error {
	tagDefinitions := []struct {
		id, name, parentID string
	}{
		{id: "campus-it-infrastructure", name: "IT Infrastructure"},
		{id: "campus-instructional-labs", name: "Instructional Labs", parentID: "campus-it-infrastructure"},
		{id: "campus-visual-arts", name: "Visual Arts", parentID: "campus-it-infrastructure"},
		{id: "campus-workforce-devices", name: "Workforce Devices", parentID: "campus-it-infrastructure"},
	}
	for _, definition := range tagDefinitions {
		created, err := s.threads.CreateTag(ctx, threads.CreateTagInput{
			ID: definition.id, Name: definition.name, ParentID: definition.parentID, InheritByDefault: true,
		})
		if err != nil && !errors.Is(err, threads.ErrConflict) {
			return fmt.Errorf("create tag %q: %w", definition.name, err)
		}
		tagID := definition.id
		if err == nil {
			tagID = created.ID
		}
		s.tagIDs[definition.id] = tagID
	}

	goalDefinitions := []struct {
		id, name, description string
	}{
		{id: "campus-refresh-lab-fleet", name: "Refresh aging lab fleet", description: "Replace instructional lab hardware on a predictable lifecycle cadence."},
		{id: "campus-reduce-license-waste", name: "Reduce license waste", description: "Reclaim unused software entitlements and align spend with actual usage."},
		{id: "campus-support-student-success", name: "Support student success", description: "Ensure classrooms, labs, and residence technology remain reliable for students."},
	}
	for _, definition := range goalDefinitions {
		created, err := s.threads.CreateGoal(ctx, threads.CreateGoalInput{
			ID: definition.id, Name: definition.name, Description: definition.description,
		})
		if err != nil && !errors.Is(err, threads.ErrConflict) {
			return fmt.Errorf("create goal %q: %w", definition.name, err)
		}
		goalID := definition.id
		if err == nil {
			goalID = created.ID
		}
		s.goalIDs[definition.id] = goalID
	}

	for _, lab := range campusLabs {
		if len(s.labAssetIDs[lab.Slug]) == 0 {
			continue
		}
		assetID := s.labAssetIDs[lab.Slug][0]
		if _, err := s.threads.SetTagRule(ctx, threads.SetTagRuleInput{
			TargetType: threads.TargetAsset, TargetID: assetID,
			TagID: s.tagIDs["campus-instructional-labs"], Mode: threads.RuleInclude, Revision: 0,
		}); err != nil && !errors.Is(err, threads.ErrConflict) {
			return fmt.Errorf("tag lab asset %q: %w", assetID, err)
		}
	}
	if licenseID := s.licenseIDs["adobe-creative-cloud-all-apps"]; licenseID != "" {
		if _, err := s.threads.SetTagRule(ctx, threads.SetTagRuleInput{
			TargetType: threads.TargetSoftware, TargetID: licenseID,
			TagID: s.tagIDs["campus-visual-arts"], Mode: threads.RuleInclude, Revision: 0,
		}); err != nil && !errors.Is(err, threads.ErrConflict) {
			return fmt.Errorf("tag Adobe license: %w", err)
		}
	}
	if len(s.labAssetIDs["studio-arts-mac"]) > 0 {
		if _, err := s.threads.SetTagRule(ctx, threads.SetTagRuleInput{
			TargetType: threads.TargetAsset, TargetID: s.labAssetIDs["studio-arts-mac"][0],
			TagID: s.tagIDs["campus-visual-arts"], Mode: threads.RuleInclude, Revision: 0,
		}); err != nil && !errors.Is(err, threads.ErrConflict) {
			return fmt.Errorf("tag studio arts asset: %w", err)
		}
	}
	if poID := "po-lab-studio-arts-mac"; poID != "" {
		if _, err := s.threads.LinkGoal(ctx, threads.LinkGoalInput{
			TargetType: threads.TargetPurchase, TargetID: poID, GoalID: s.goalIDs["campus-refresh-lab-fleet"],
		}); err != nil && !errors.Is(err, threads.ErrConflict) {
			return fmt.Errorf("link lab PO to refresh goal: %w", err)
		}
	}
	// Goal links currently accept only assets and purchase orders.
	if _, err := s.threads.LinkGoal(ctx, threads.LinkGoalInput{
		TargetType: threads.TargetPurchase, TargetID: "po-2026-0502", GoalID: s.goalIDs["campus-reduce-license-waste"],
	}); err != nil && !errors.Is(err, threads.ErrConflict) {
		return fmt.Errorf("link Adobe purchase order to waste goal: %w", err)
	}
	if len(s.labAssetIDs["studio-arts-mac"]) > 0 {
		if _, err := s.threads.LinkGoal(ctx, threads.LinkGoalInput{
			TargetType: threads.TargetAsset, TargetID: s.labAssetIDs["studio-arts-mac"][0], GoalID: s.goalIDs["campus-support-student-success"],
		}); err != nil && !errors.Is(err, threads.ErrConflict) {
			return fmt.Errorf("link studio arts asset to student success goal: %w", err)
		}
	}
	return nil
}
