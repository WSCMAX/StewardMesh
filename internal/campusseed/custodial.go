package campusseed

import (
	"context"
	"errors"
	"fmt"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/people"
)

func (s *Seeder) seedShopRooms(ctx context.Context) error {
	for _, shop := range campusShops {
		buildingID := s.buildingIDs[shop.BuildingSlug]
		created, err := s.people.CreateRoom(ctx, people.CreateRoomInput{
			SiteID: s.siteID, BuildingID: buildingID, Number: shop.RoomNumber,
			Name: shop.Name, Status: people.StatusActive,
		})
		if err != nil {
			if !errors.Is(err, people.ErrConflict) {
				return fmt.Errorf("create shop room %q: %w", shop.Name, err)
			}
			rooms, listErr := s.people.ListRooms(ctx, s.siteID, buildingID, people.Visibility{All: true})
			if listErr != nil {
				return listErr
			}
			for _, room := range rooms {
				if room.Number == shop.RoomNumber {
					s.roomIDs[shop.Slug] = room.ID
					break
				}
			}
			continue
		}
		s.roomIDs[shop.Slug] = created.ID
	}
	return nil
}

func (s *Seeder) seedCustodialAssets(ctx context.Context) (int, error) {
	created := 0
	desktopModel := s.modelIDs["dell-optiplex-7020"]
	tabletModel := s.modelIDs["apple-ipad-air"]
	buildingID := s.buildingIDs["services"]
	departmentID := s.departmentIDs["custodial"]

	for _, shop := range campusShops {
		roomID := s.roomIDs[shop.Slug]
		for desk := 1; desk <= shop.SharedDesktopCount; desk++ {
			slug := fmt.Sprintf("shop-%s-desk-%02d", shop.Slug, desk)
			_, err := s.atlas.CreateAsset(ctx, atlas.CreateAssetInput{
				ID: slug, ModelID: desktopModel,
				Name:            fmt.Sprintf("%s Shared Desktop %02d", shop.Name, desk),
				Kind:            "desktop",
				AssetTag:        fmt.Sprintf("RCC-SHOP-%04d", created+1),
				Hostname:        fmt.Sprintf("%s-desk-%02d.riverside-demo.invalid", shop.Slug, desk),
				DeploymentNotes: "Shared custodial workstation; no primary user assignment",
				References: atlas.References{
					SiteID: s.siteID, BuildingID: buildingID, RoomID: roomID, DepartmentID: departmentID,
				},
				Status:        "active",
				Attributes:    map[string]string{"primaryUse": "admin", "imageBuild": "campus-shop-2026.1"},
				Components:    stationAccessories("desktop", false),
				UnitCostMinor: 98900, Currency: "USD",
			})
			if err != nil {
				if errors.Is(err, atlas.ErrConflict) {
					s.trackShopDesktop(slug)
					continue
				}
				return created, fmt.Errorf("create shop desktop %q: %w", slug, err)
			}
			s.trackShopDesktop(slug)
			created++
		}
	}

	custodialIndex := 0
	for _, employee := range s.employees {
		if employee.DepartmentSlug != "custodial" {
			continue
		}
		custodialIndex++
		slug := fmt.Sprintf("custodial-ipad-%03d", custodialIndex)
		roomID := s.roomIDs["svc-custodial"]
		_, err := s.atlas.CreateAsset(ctx, atlas.CreateAssetInput{
			ID: slug, ModelID: tabletModel,
			Name:            fmt.Sprintf("Custodial iPad %03d", custodialIndex),
			Kind:            "tablet",
			AssetTag:        fmt.Sprintf("RCC-IPD-%04d", created+1),
			Hostname:        fmt.Sprintf("custodial-ipad-%03d.riverside-demo.invalid", custodialIndex),
			DeploymentNotes: "Field iPad for work orders, inspections, and inventory scans",
			References: atlas.References{
				SiteID: s.siteID, BuildingID: buildingID, RoomID: roomID,
				DepartmentID: departmentID, UserID: employee.ID,
			},
			Status:        "active",
			Attributes:    map[string]string{"cellular": "wifi", "primaryUse": "admin"},
			UnitCostMinor: 79900, Currency: "USD",
		})
		if err != nil {
			if errors.Is(err, atlas.ErrConflict) {
				s.trackTablet(slug)
				continue
			}
			return created, fmt.Errorf("create custodial ipad %q: %w", slug, err)
		}
		s.trackTablet(slug)
		created++
	}

	fieldDepartments := []string{"it-services", "facilities", "campus-security"}
	fieldCounts := map[string]int{"it-services": 12, "facilities": 6, "campus-security": 10}
	for _, departmentSlug := range fieldDepartments {
		count := 0
		for _, employee := range s.employees {
			if employee.DepartmentSlug != departmentSlug || count >= fieldCounts[departmentSlug] {
				continue
			}
			count++
			slug := fmt.Sprintf("field-ipad-%s-%03d", departmentSlug, count)
			office := officeByDepartment(departmentSlug)
			_, err := s.atlas.CreateAsset(ctx, atlas.CreateAssetInput{
				ID: slug, ModelID: tabletModel,
				Name:            fmt.Sprintf("%s Field iPad %03d", departmentBySlug(departmentSlug).Name, count),
				Kind:            "tablet",
				AssetTag:        fmt.Sprintf("RCC-IPD-%04d", created+1),
				DeploymentNotes: "Secondary field device paired with primary laptop",
				References: atlas.References{
					SiteID: s.siteID, BuildingID: s.buildingIDs[departmentBySlug(departmentSlug).BuildingSlug],
					RoomID: s.roomIDs[office.Slug], DepartmentID: s.departmentIDs[departmentSlug], UserID: employee.ID,
				},
				Status:        "active",
				Attributes:    map[string]string{"cellular": "wifi", "primaryUse": "admin"},
				UnitCostMinor: 79900, Currency: "USD",
			})
			if err != nil {
				if errors.Is(err, atlas.ErrConflict) {
					s.trackTablet(slug)
					continue
				}
				return created, fmt.Errorf("create field ipad %q: %w", slug, err)
			}
			s.trackTablet(slug)
			created++
		}
	}
	return created, nil
}

func (s *Seeder) trackShopDesktop(slug string) {
	s.shopDesktopIDs = append(s.shopDesktopIDs, slug)
	s.allAssetIDs = append(s.allAssetIDs, slug)
}

func (s *Seeder) trackTablet(slug string) {
	s.tabletAssetIDs = append(s.tabletAssetIDs, slug)
	s.allAssetIDs = append(s.allAssetIDs, slug)
}

func employeeReceivesLaptop(employee seededEmployee, custodialSupervisorSerial int) bool {
	switch employee.DepartmentSlug {
	case "custodial":
		return custodialSupervisorSerial <= 8
	default:
		return true
	}
}
