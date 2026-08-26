package campusseed

import (
	"context"
	"errors"
	"fmt"

	"github.com/maxlemke/stewardmesh/internal/directoryexpansion"
)

func (s *ExtendedSeeder) seedDirectoryGroups(ctx context.Context) (int, error) {
	now := s.now()
	systemID := SourceSystemID
	groups := []struct {
		id, sourceRecordID, name, displayName, description string
	}{
		{
			id: stableID("directory-group", "it-operations"), sourceRecordID: "campus-group-it-operations",
			name: "demo:it:operations", displayName: "[Campus Demo] IT Operations",
			description: "IT Services operators responsible for campus infrastructure and support.",
		},
		{
			id: stableID("directory-group", "visual-arts-faculty"), sourceRecordID: "campus-group-visual-arts-faculty",
			name: "demo:visual-arts:faculty", displayName: "[Campus Demo] Visual Arts Faculty",
			description: "Studio and design faculty grouped for instructional technology workflows.",
		},
	}
	created := 0
	for _, definition := range groups {
		_, err := s.directoryGroups.CreateManagedGroup(ctx, directoryexpansion.ManagedGroup{
			ID: definition.id, OrganizationID: s.organizationID,
			SourceSystemID: systemID, SourceRecordID: definition.sourceRecordID,
			Name: definition.name, DisplayName: definition.displayName,
			Description: definition.description, Status: "active",
			Metadata: map[string]string{"origin": "campus-demo", "dataset-version": SourceSystemID},
			Revision: 1, CreatedAt: now, UpdatedAt: now,
		})
		if err != nil && !errors.Is(err, directoryexpansion.ErrConflict) {
			return created, fmt.Errorf("create directory group %q: %w", definition.displayName, err)
		}
		if err == nil {
			created++
		}
	}
	if len(s.departmentIDs) == 0 {
		return created, nil
	}
	itGroupID := stableID("directory-group", "it-operations")
	artsGroupID := stableID("directory-group", "visual-arts-faculty")
	memberships := []directoryexpansion.ManagedMembership{
		{
			ID: stableID("directory-membership", "it-director-it-ops"), OrganizationID: s.organizationID,
			SourceSystemID: systemID, SourceRecordID: "campus-membership-it-director-it-ops",
			GroupID: itGroupID, GroupSourceID: "campus-group-it-operations",
			MemberID: stableID("account", "it-director"), MemberSourceID: "it-director",
			MemberKind: directoryexpansion.MemberSubject, MemberDisplayName: "Jordan Blake", Status: "active",
			Metadata: map[string]string{"origin": "campus-demo"}, Revision: 1, CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: stableID("directory-membership", "lab-manager-visual-arts"), OrganizationID: s.organizationID,
			SourceSystemID: systemID, SourceRecordID: "campus-membership-lab-manager-visual-arts",
			GroupID: artsGroupID, GroupSourceID: "campus-group-visual-arts-faculty",
			MemberID: stableID("account", "lab-manager-arts"), MemberSourceID: "lab-manager-arts",
			MemberKind: directoryexpansion.MemberSubject, MemberDisplayName: "Morgan Chen", Status: "active",
			Metadata: map[string]string{"origin": "campus-demo"}, Revision: 1, CreatedAt: now, UpdatedAt: now,
		},
	}
	for _, membership := range memberships {
		if _, err := s.directoryGroups.CreateManagedMembership(ctx, membership); err != nil && !errors.Is(err, directoryexpansion.ErrConflict) {
			return created, fmt.Errorf("create directory membership: %w", err)
		}
	}
	return created, nil
}
