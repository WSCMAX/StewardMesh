package campusseed

import (
	"context"
	"errors"
	"fmt"

	"github.com/maxlemke/stewardmesh/internal/people"
)

func (s *Seeder) seedStudents(ctx context.Context) (int, error) {
	created := 0
	for index := 0; index < ActiveStudentCount+InactiveStudentCount; index++ {
		active := index < ActiveStudentCount
		visualArts := active && index < VisualArtsStudentCount
		displayTag := ""
		if visualArts {
			if index%2 == 0 {
				displayTag = StudioArtsStudentTag
			} else {
				displayTag = GraphicDesignStudentTag
			}
		}
		person := campusStudentName(index)
		displayName := person.Display
		if displayTag != "" {
			displayName = fmt.Sprintf("%s · %s", displayName, displayTag)
		} else if !active {
			displayName = fmt.Sprintf("%s · Alumni", displayName)
		}
		department := campusDepartments[index%len(campusDepartments)]
		if visualArts {
			if displayTag == StudioArtsStudentTag {
				department = departmentBySlug("studio-arts")
			} else {
				department = departmentBySlug("graphic-design")
			}
		}
		status := people.StatusActive
		subjectPrefix := "active-student"
		if !active {
			status = people.StatusInactive
			subjectPrefix = "inactive-student"
		}
		if visualArts {
			if displayTag == StudioArtsStudentTag {
				subjectPrefix = "studio-arts-student"
			} else {
				subjectPrefix = "graphic-design-student"
			}
		}
		emailLocal := person.emailLocal(index+1, 5)
		buildingID := s.buildingIDs[department.BuildingSlug]
		var roomID string
		if active && len(s.dormRoomIDs) > 0 && index < DormResidentCount {
			roomID = s.dormRoomIDs[index%len(s.dormRoomIDs)]
			buildingID = ""
		}
		identity, err := s.people.CreateIdentity(ctx, people.CreateIdentityInput{
			Kind: people.IdentityPerson, DisplayName: displayName,
			Email:        emailLocal + "@student.riverside-demo.invalid",
			DepartmentID: s.departmentIDs[department.Slug], SiteID: s.siteID,
			BuildingID: buildingID, RoomID: roomID,
			Status:   status,
			Provider: StudentProvider, ProviderSubject: fmt.Sprintf("%s-%05d", subjectPrefix, index+1),
		})
		if err != nil {
			if errors.Is(err, people.ErrConflict) {
				continue
			}
			return created, fmt.Errorf("create student %q: %w", displayName, err)
		}
		kind := "inactive-student"
		if active {
			kind = "active-student"
		}
		s.students = append(s.students, seededStudent{
			ID: identity.ID, Active: active, VisualArts: visualArts, DisplayTag: displayTag,
			DepartmentSlug: department.Slug,
		})
		s.peopleIndex = append(s.peopleIndex, seededPerson{
			ID: identity.ID, DepartmentSlug: department.Slug, Kind: kind, VisualArts: visualArts,
		})
		created++
	}
	return created, nil
}
