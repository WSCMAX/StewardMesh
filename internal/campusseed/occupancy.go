package campusseed

// Occupancy types, classrooms, residence halls, and person location references.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/maxlemke/stewardmesh/internal/people"
)

type occupancyTypeDef struct {
	Slug             string
	Name             string
	Description      string
	RelationshipKind string
}

var campusOccupancyTypes = []occupancyTypeDef{
	{Slug: "office", Name: "Office", Description: "Primary or secondary workspace a person occupies.", RelationshipKind: people.RelationshipUsesOffice},
	{Slug: "instructor", Name: "Instructor", Description: "Classroom or lab where the person teaches.", RelationshipKind: people.RelationshipTeachesIn},
	{Slug: "class", Name: "Class enrollment", Description: "Classroom where a student attends class.", RelationshipKind: people.RelationshipAttendsClass},
	{Slug: "dormitory", Name: "Dormitory", Description: "Residence hall room assignment.", RelationshipKind: people.RelationshipResidesIn},
	{Slug: "lab", Name: "Lab assignment", Description: "Computer or instructional lab the person uses.", RelationshipKind: people.RelationshipUsesLab},
}

func (s *Seeder) seedClassrooms(ctx context.Context) error {
	for _, classroom := range campusClassrooms {
		buildingID := s.buildingIDs[classroom.BuildingSlug]
		created, err := s.people.CreateRoom(ctx, people.CreateRoomInput{
			SiteID: s.siteID, BuildingID: buildingID, Number: classroom.RoomNumber,
			Name: classroom.Name, Status: people.StatusActive,
		})
		if err != nil {
			if !errors.Is(err, people.ErrConflict) {
				return fmt.Errorf("create classroom %q: %w", classroom.Name, err)
			}
			rooms, listErr := s.people.ListRooms(ctx, s.siteID, buildingID, people.Visibility{All: true})
			if listErr != nil {
				return listErr
			}
			for _, room := range rooms {
				if !strings.EqualFold(room.Number, classroom.RoomNumber) {
					continue
				}
				s.roomIDs[classroom.Slug] = room.ID
				s.classroomIDs = append(s.classroomIDs, room.ID)
				s.classroomIDsByBuilding[classroom.BuildingSlug] = append(s.classroomIDsByBuilding[classroom.BuildingSlug], room.ID)
				break
			}
			continue
		}
		s.roomIDs[classroom.Slug] = created.ID
		s.classroomIDs = append(s.classroomIDs, created.ID)
		s.classroomIDsByBuilding[classroom.BuildingSlug] = append(s.classroomIDsByBuilding[classroom.BuildingSlug], created.ID)
	}
	return nil
}

func (s *Seeder) seedResidenceRooms(ctx context.Context) error {
	buildingID := s.buildingIDs[ResidenceBuildingSlug]
	for _, room := range campusResidenceRooms() {
		created, err := s.people.CreateRoom(ctx, people.CreateRoomInput{
			SiteID: s.siteID, BuildingID: buildingID, Number: room.RoomNumber,
			Name: room.Name, Status: people.StatusActive,
		})
		if err != nil {
			if !errors.Is(err, people.ErrConflict) {
				return fmt.Errorf("create residence room %q: %w", room.Name, err)
			}
			rooms, listErr := s.people.ListRooms(ctx, s.siteID, buildingID, people.Visibility{All: true})
			if listErr != nil {
				return listErr
			}
			for _, existing := range rooms {
				if strings.EqualFold(existing.Number, room.RoomNumber) {
					s.roomIDs[room.Slug] = existing.ID
					s.dormRoomIDs = append(s.dormRoomIDs, existing.ID)
					break
				}
			}
			continue
		}
		s.roomIDs[room.Slug] = created.ID
		s.dormRoomIDs = append(s.dormRoomIDs, created.ID)
	}
	return nil
}

func (s *Seeder) seedOccupancy(ctx context.Context) error {
	if err := s.seedOccupancyTypes(ctx); err != nil {
		return err
	}
	s.indexLabRooms()
	if err := s.seedEmployeeOccupancy(ctx); err != nil {
		return err
	}
	if err := s.seedStudentOccupancy(ctx); err != nil {
		return err
	}
	return s.seedLabIdentityOccupancy(ctx)
}

func (s *Seeder) seedOccupancyTypes(ctx context.Context) error {
	for _, definition := range campusOccupancyTypes {
		created, err := s.people.CreateLocationReferenceType(ctx, people.CreateLocationReferenceTypeInput{
			Name: definition.Name, Description: definition.Description,
			RelationshipKind: definition.RelationshipKind, LocationKind: people.LocationKindRoom,
			Status: people.StatusActive,
		})
		if err != nil {
			if !errors.Is(err, people.ErrConflict) {
				return fmt.Errorf("create location reference type %q: %w", definition.Name, err)
			}
			types, listErr := s.people.ListLocationReferenceTypes(ctx)
			if listErr != nil {
				return listErr
			}
			for _, item := range types {
				if strings.EqualFold(item.Name, definition.Name) {
					s.locationTypeIDs[definition.Slug] = item.ID
					break
				}
			}
			continue
		}
		s.locationTypeIDs[definition.Slug] = created.ID
	}
	return nil
}

func (s *Seeder) indexLabRooms() {
	for _, lab := range campusLabs {
		roomID := s.roomIDs[lab.Slug]
		if roomID == "" {
			continue
		}
		s.labRoomIDsByDepartment[lab.DepartmentSlug] = append(s.labRoomIDsByDepartment[lab.DepartmentSlug], roomID)
	}
}

func (s *Seeder) seedEmployeeOccupancy(ctx context.Context) error {
	officeType := s.locationTypeIDs["office"]
	instructorType := s.locationTypeIDs["instructor"]
	labType := s.locationTypeIDs["lab"]
	for index, employee := range s.employees {
		officeID := s.roomIDs[employee.OfficeSlug]
		if err := s.createOccupancy(ctx, employee.ID, officeType, officeID, people.LocationPriorityPrimary); err != nil {
			return err
		}
		department := departmentBySlug(employee.DepartmentSlug)
		if !isTeachingDepartment(department) {
			continue
		}
		classroomID := s.classroomForBuilding(department.BuildingSlug, index)
		if err := s.createOccupancy(ctx, employee.ID, instructorType, classroomID, people.LocationPriorityPrimary); err != nil {
			return err
		}
		labs := s.labRoomIDsByDepartment[department.Slug]
		if index%5 == 0 && len(labs) > 0 {
			if err := s.createOccupancy(ctx, employee.ID, labType, labs[index%len(labs)], people.LocationPrioritySecondary); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Seeder) seedStudentOccupancy(ctx context.Context) error {
	classType := s.locationTypeIDs["class"]
	dormType := s.locationTypeIDs["dormitory"]
	labType := s.locationTypeIDs["lab"]
	activeIndex := 0
	for _, student := range s.students {
		if !student.Active {
			continue
		}
		department := departmentBySlug(student.DepartmentSlug)
		classroomID := s.classroomForBuilding(department.BuildingSlug, activeIndex)
		if err := s.createOccupancy(ctx, student.ID, classType, classroomID, people.LocationPriorityPrimary); err != nil {
			return err
		}
		if activeIndex < DormResidentCount && len(s.dormRoomIDs) > 0 {
			if err := s.createOccupancy(ctx, student.ID, dormType, s.dormRoomIDs[activeIndex%len(s.dormRoomIDs)], people.LocationPriorityPrimary); err != nil {
				return err
			}
		}
		if student.VisualArts {
			labs := s.labRoomIDsByDepartment[department.Slug]
			if len(labs) > 0 {
				if err := s.createOccupancy(ctx, student.ID, labType, labs[activeIndex%len(labs)], people.LocationPrioritySecondary); err != nil {
					return err
				}
			}
		}
		activeIndex++
	}
	return nil
}

func (s *Seeder) seedLabIdentityOccupancy(ctx context.Context) error {
	labType := s.locationTypeIDs["lab"]
	for index, identityID := range s.labIdentityIDs {
		if index >= len(campusLabs) {
			break
		}
		roomID := s.roomIDs[campusLabs[index].Slug]
		if err := s.createOccupancy(ctx, identityID, labType, roomID, people.LocationPriorityPrimary); err != nil {
			return err
		}
	}
	return nil
}

func (s *Seeder) classroomForBuilding(buildingSlug string, index int) string {
	rooms := s.classroomIDsByBuilding[buildingSlug]
	if len(rooms) == 0 {
		rooms = s.classroomIDs
	}
	if len(rooms) == 0 {
		return ""
	}
	return rooms[index%len(rooms)]
}

func (s *Seeder) createOccupancy(ctx context.Context, identityID, typeID, roomID string, priority people.LocationPriority) error {
	if identityID == "" || typeID == "" || roomID == "" {
		return nil
	}
	_, err := s.people.CreateLocationReference(ctx, people.CreateLocationReferenceInput{
		IdentityID: identityID, TypeID: typeID, LocationKind: people.LocationKindRoom,
		LocationID: roomID, Priority: priority, Status: people.StatusActive,
	})
	if err != nil && !errors.Is(err, people.ErrConflict) {
		return fmt.Errorf("create location reference: %w", err)
	}
	return nil
}
