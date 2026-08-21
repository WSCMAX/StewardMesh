package campusseed

// Requirement: REQ-DIRECTORY-EXPANSION-007. Feature: platform.foundation.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/ledger"
	"github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/stack"
	"github.com/maxlemke/stewardmesh/internal/storage"
)

type Dependencies struct {
	OrganizationID  string
	People          *people.Service
	Atlas           *atlas.Service
	Stack           *stack.Service
	Ledger          *ledger.Service
	Vault           *storage.Service
	Guard           *guard.Service
	GuardStore      guard.Store
	Auditor         foundation.Auditor
	CredentialsPath string
}

type Result struct {
	SiteID             string
	BuildingCount      int
	DepartmentCount    int
	RoomCount          int
	LabCount           int
	EmployeeCount      int
	StudentCount       int
	ActiveStudentCount int
	ModelCount         int
	AssetCount         int
	LicenseCount       int
	AssignmentCount    int
	GuardUserCount     int
	CredentialsPath    string
	AlreadySeeded      bool
}

type Seeder struct {
	organizationID  string
	people          *people.Service
	atlas           *atlas.Service
	stack           *stack.Service
	ledger          *ledger.Service
	vault           *storage.Service
	guardService    *guard.Service
	guardStore      guard.Store
	auditor         foundation.Auditor
	credentialsPath string
	now             func() time.Time

	siteID                 string
	buildingIDs            map[string]string
	departmentIDs          map[string]string
	roomIDs                map[string]string
	locationTypeIDs        map[string]string
	classroomIDs           []string
	classroomIDsByBuilding map[string][]string
	labRoomIDsByDepartment map[string][]string
	dormRoomIDs            []string
	labIdentityIDs         []string
	modelIDs               map[string]string
	productIDs             map[string]string
	versionIDs             map[string]string
	licenseIDs             map[string]string
	employees              []seededEmployee
	students               []seededStudent
	peopleIndex            []seededPerson
	labAssetIDs            []string
	labAssetsBySlug        map[string][]string
	officeAssetIDs         []string
	officeAssetsByBuilding map[string][]string
	laptopAssetIDs         []string
	laptopAssetsByVendor   map[string][]string
	shopDesktopIDs         []string
	tabletAssetIDs         []string
	allAssetIDs            []string
	employeeAssetIDs       map[string]string
	blobIDs                map[string]string
	vendorIDs              map[string]string
}

func Seed(ctx context.Context, dependencies Dependencies) (Result, error) {
	seeder := Seeder{
		organizationID:         strings.TrimSpace(dependencies.OrganizationID),
		people:                 dependencies.People,
		atlas:                  dependencies.Atlas,
		stack:                  dependencies.Stack,
		ledger:                 dependencies.Ledger,
		vault:                  dependencies.Vault,
		guardService:           dependencies.Guard,
		guardStore:             dependencies.GuardStore,
		auditor:                dependencies.Auditor,
		credentialsPath:        strings.TrimSpace(dependencies.CredentialsPath),
		now:                    func() time.Time { return time.Now().UTC() },
		buildingIDs:            make(map[string]string),
		departmentIDs:          make(map[string]string),
		roomIDs:                make(map[string]string),
		locationTypeIDs:        make(map[string]string),
		classroomIDsByBuilding: make(map[string][]string),
		labRoomIDsByDepartment: make(map[string][]string),
		modelIDs:               make(map[string]string),
		productIDs:             make(map[string]string),
		versionIDs:             make(map[string]string),
		licenseIDs:             make(map[string]string),
		blobIDs:                make(map[string]string),
		vendorIDs:              make(map[string]string),
		labAssetsBySlug:        make(map[string][]string),
		officeAssetsByBuilding: make(map[string][]string),
		laptopAssetsByVendor:   make(map[string][]string),
		employeeAssetIDs:       make(map[string]string),
	}
	if seeder.credentialsPath == "" {
		seeder.credentialsPath = CredentialsFile
	}
	return seeder.run(ctx)
}

func (s *Seeder) run(ctx context.Context) (Result, error) {
	result := Result{CredentialsPath: s.credentialsPath}
	organizationID := strings.ToLower(s.organizationID)
	if ctx == nil || !strings.HasPrefix(organizationID, "demo-") ||
		s.people == nil || s.atlas == nil || s.stack == nil || s.ledger == nil || s.vault == nil ||
		s.guardService == nil || s.guardStore == nil || s.auditor == nil {
		return Result{}, errors.New("campus demo seeding requires a demo-* organization and initialized People, Atlas, Stack, Ledger, Vault, Guard, and audit services")
	}
	correlationID, err := foundation.NewCorrelationID()
	if err != nil {
		return Result{}, fmt.Errorf("create campus demo correlation id: %w", err)
	}
	ctx = foundation.WithScope(ctx, foundation.Scope{
		OrganizationID: s.organizationID,
		ActorID:        ActorID,
		CorrelationID:  correlationID,
	})

	if seeded, err := s.alreadySeeded(ctx); err != nil {
		return Result{}, err
	} else if seeded {
		result.AlreadySeeded = true
		if err := s.writeCredentials(); err != nil {
			return Result{}, err
		}
		return result, nil
	}

	if err := s.seedSite(ctx); err != nil {
		return Result{}, err
	}
	result.SiteID = s.siteID
	if err := s.seedBuildings(ctx); err != nil {
		return Result{}, err
	}
	result.BuildingCount = len(campusBuildings)
	if err := s.seedDepartments(ctx); err != nil {
		return Result{}, err
	}
	result.DepartmentCount = len(campusDepartments)
	if err := s.seedOfficeRooms(ctx); err != nil {
		return Result{}, err
	}
	if err := s.seedLabRooms(ctx); err != nil {
		return Result{}, err
	}
	if err := s.seedShopRooms(ctx); err != nil {
		return Result{}, err
	}
	if err := s.seedClassrooms(ctx); err != nil {
		return Result{}, err
	}
	if err := s.seedResidenceRooms(ctx); err != nil {
		return Result{}, err
	}
	result.RoomCount = len(campusOffices) + len(campusLabs) + len(campusShops) + len(campusClassrooms) + len(campusResidenceRooms())
	if err := s.seedModels(ctx); err != nil {
		return Result{}, err
	}
	result.ModelCount = len(campusModels)
	if err := s.seedLabIdentities(ctx); err != nil {
		return Result{}, err
	}
	result.LabCount = len(campusLabs)
	employeeCount, err := s.seedEmployees(ctx)
	if err != nil {
		return Result{}, err
	}
	result.EmployeeCount = employeeCount
	studentCount, err := s.seedStudents(ctx)
	if err != nil {
		return Result{}, err
	}
	result.StudentCount = studentCount
	result.ActiveStudentCount = ActiveStudentCount
	if err := s.seedOccupancy(ctx); err != nil {
		return Result{}, err
	}
	assetCount, err := s.seedAssets(ctx)
	if err != nil {
		return Result{}, err
	}
	result.AssetCount = assetCount
	if err := s.seedDocuments(ctx); err != nil {
		return Result{}, err
	}
	if err := s.seedProcurement(ctx); err != nil {
		return Result{}, err
	}
	if err := s.seedSoftwareCatalog(ctx); err != nil {
		return Result{}, err
	}
	result.LicenseCount = len(campusLicenseOfferings)
	if err := s.installLabProductivitySoftware(ctx); err != nil {
		return Result{}, err
	}
	assignmentCount, err := s.seedLicenseAssignments(ctx)
	if err != nil {
		return Result{}, err
	}
	result.AssignmentCount = assignmentCount
	guardCount, err := s.seedGuardUsers(ctx)
	if err != nil {
		return Result{}, err
	}
	result.GuardUserCount = guardCount
	if err := s.auditComplete(ctx, result); err != nil {
		return Result{}, err
	}
	if err := s.writeCredentials(); err != nil {
		return Result{}, err
	}
	return result, nil
}

func (s *Seeder) alreadySeeded(ctx context.Context) (bool, error) {
	sites, err := s.people.ListSites(ctx, people.Visibility{All: true})
	if err != nil {
		return false, fmt.Errorf("list campus demo sites: %w", err)
	}
	foundSite := false
	for _, site := range sites {
		if strings.EqualFold(site.Name, SiteName) {
			s.siteID = site.ID
			foundSite = true
			break
		}
	}
	if !foundSite {
		return false, nil
	}
	buildings, err := s.people.ListBuildings(ctx, s.siteID, people.Visibility{All: true})
	if err != nil {
		return false, fmt.Errorf("list campus demo buildings: %w", err)
	}
	hasV2Layout := false
	for _, building := range buildings {
		if strings.EqualFold(building.Name, "Services Building") {
			hasV2Layout = true
			break
		}
	}
	if !hasV2Layout {
		return false, nil
	}
	products, err := s.stack.Snapshot(ctx)
	if err != nil {
		return false, fmt.Errorf("snapshot campus demo software catalog: %w", err)
	}
	hasCatalog := false
	for _, product := range products.Products {
		if product.ID == "microsoft-365" {
			hasCatalog = true
			break
		}
	}
	if !hasCatalog || len(products.Assignments) == 0 {
		return false, nil
	}
	if _, err := os.Stat(s.credentialsPath); err != nil {
		return false, nil
	}
	if _, err := s.guardStore.FindAccountByUsername(ctx, s.organizationID, "it-director"); err != nil {
		if errors.Is(err, guard.ErrNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("lookup campus demo guard account: %w", err)
	}
	return true, nil
}

func (s *Seeder) seedSite(ctx context.Context) error {
	created, err := s.people.CreateSite(ctx, people.CreateSiteInput{
		Name: SiteName,
		Address: people.Address{
			Line1: "1200 University Parkway", City: SiteCity, Region: SiteRegion,
			PostalCode: "92507", Country: "US",
		},
		Status: people.StatusActive,
	})
	if err != nil {
		if !errors.Is(err, people.ErrConflict) {
			return fmt.Errorf("create campus site: %w", err)
		}
		sites, listErr := s.people.ListSites(ctx, people.Visibility{All: true})
		if listErr != nil {
			return listErr
		}
		for _, site := range sites {
			if strings.EqualFold(site.Name, SiteName) {
				s.siteID = site.ID
				return nil
			}
		}
		return fmt.Errorf("create campus site: %w", err)
	}
	s.siteID = created.ID
	return nil
}

func (s *Seeder) seedBuildings(ctx context.Context) error {
	for _, definition := range campusBuildings {
		created, err := s.people.CreateBuilding(ctx, people.CreateBuildingInput{
			SiteID: s.siteID, Name: definition.Name, Status: people.StatusActive,
		})
		if err != nil {
			if !errors.Is(err, people.ErrConflict) {
				return fmt.Errorf("create building %q: %w", definition.Name, err)
			}
			buildings, listErr := s.people.ListBuildings(ctx, s.siteID, people.Visibility{All: true})
			if listErr != nil {
				return listErr
			}
			for _, building := range buildings {
				if strings.EqualFold(building.Name, definition.Name) {
					s.buildingIDs[definition.Slug] = building.ID
					break
				}
			}
			continue
		}
		s.buildingIDs[definition.Slug] = created.ID
	}
	return nil
}

func (s *Seeder) seedDepartments(ctx context.Context) error {
	for _, definition := range campusDepartments {
		created, err := s.people.CreateDepartment(ctx, people.CreateDepartmentInput{
			Name: definition.Name, SiteID: s.siteID, Status: people.StatusActive,
		})
		if err != nil {
			if !errors.Is(err, people.ErrConflict) {
				return fmt.Errorf("create department %q: %w", definition.Name, err)
			}
			departments, listErr := s.people.ListDepartments(ctx, people.Visibility{All: true})
			if listErr != nil {
				return listErr
			}
			for _, department := range departments {
				if strings.EqualFold(department.Name, definition.Name) {
					s.departmentIDs[definition.Slug] = department.ID
					break
				}
			}
			continue
		}
		s.departmentIDs[definition.Slug] = created.ID
	}
	return nil
}

func (s *Seeder) seedOfficeRooms(ctx context.Context) error {
	for _, office := range campusOffices {
		buildingID := s.buildingIDs[office.BuildingSlug]
		created, err := s.people.CreateRoom(ctx, people.CreateRoomInput{
			SiteID: s.siteID, BuildingID: buildingID, Number: office.RoomNumber,
			Name: office.Name, Status: people.StatusActive,
		})
		if err != nil {
			if !errors.Is(err, people.ErrConflict) {
				return fmt.Errorf("create office room %q: %w", office.Name, err)
			}
			rooms, listErr := s.people.ListRooms(ctx, s.siteID, buildingID, people.Visibility{All: true})
			if listErr != nil {
				return listErr
			}
			for _, room := range rooms {
				if strings.EqualFold(room.Number, office.RoomNumber) {
					s.roomIDs[office.Slug] = room.ID
					break
				}
			}
			continue
		}
		s.roomIDs[office.Slug] = created.ID
	}
	return nil
}

func (s *Seeder) seedLabRooms(ctx context.Context) error {
	for _, lab := range campusLabs {
		buildingID := s.buildingIDs[lab.BuildingSlug]
		created, err := s.people.CreateRoom(ctx, people.CreateRoomInput{
			SiteID: s.siteID, BuildingID: buildingID, Number: lab.RoomNumber,
			Name: lab.Name, Status: people.StatusActive,
		})
		if err != nil {
			if !errors.Is(err, people.ErrConflict) {
				return fmt.Errorf("create lab room %q: %w", lab.Name, err)
			}
			rooms, listErr := s.people.ListRooms(ctx, s.siteID, buildingID, people.Visibility{All: true})
			if listErr != nil {
				return listErr
			}
			for _, room := range rooms {
				if strings.EqualFold(room.Number, lab.RoomNumber) {
					s.roomIDs[lab.Slug] = room.ID
					break
				}
			}
			continue
		}
		s.roomIDs[lab.Slug] = created.ID
	}
	return nil
}

func (s *Seeder) seedModels(ctx context.Context) error {
	for _, definition := range campusModels {
		created, err := s.atlas.CreateModel(ctx, atlas.CreateModelInput{
			ID: definition.Slug, Manufacturer: definition.Manufacturer, Name: definition.Name,
			ModelNumber: definition.ModelNumber, Kind: definition.Kind,
			Specifications: definition.Specifications, TemplateFields: computerTemplateFields(definition.Kind),
			UnitCostMinor: definition.UnitCostMinor, Currency: "USD",
			WarrantyMonths: 36, UsefulLifeMonths: 60, CriticalityScore: defaultModelCriticality(definition),
			SourceSystemID: SourceSystemID, SourceRecordID: definition.Slug,
		})
		if err != nil {
			if !errors.Is(err, atlas.ErrConflict) {
				return fmt.Errorf("create model %q: %w", definition.Name, err)
			}
			models, listErr := s.atlas.ListModels(ctx, atlas.ModelQuery{Search: definition.Name, Limit: 5})
			if listErr != nil || len(models) == 0 {
				return fmt.Errorf("resolve existing model %q: %w", definition.Name, err)
			}
			s.modelIDs[definition.Slug] = models[0].ID
			continue
		}
		s.modelIDs[definition.Slug] = created.ID
	}
	return nil
}

func (s *Seeder) seedLabIdentities(ctx context.Context) error {
	for _, lab := range campusLabs {
		departmentID := s.departmentIDs[lab.DepartmentSlug]
		identity, err := s.people.CreateIdentity(ctx, people.CreateIdentityInput{
			Kind: people.IdentityLab, DisplayName: lab.Name,
			Email:        fmt.Sprintf("%s@riverside-demo.invalid", lab.Slug),
			DepartmentID: departmentID, SiteID: s.siteID,
			BuildingID: s.buildingIDs[lab.BuildingSlug], RoomID: s.roomIDs[lab.Slug],
			Status:   people.StatusActive,
			Provider: SourceSystemID, ProviderSubject: lab.Slug,
		})
		if err != nil && !errors.Is(err, people.ErrConflict) {
			return fmt.Errorf("create lab identity %q: %w", lab.Name, err)
		}
		if err == nil {
			s.labIdentityIDs = append(s.labIdentityIDs, identity.ID)
		}
	}
	return nil
}

func (s *Seeder) seedEmployees(ctx context.Context) (int, error) {
	created := 0
	assignments := employeeDepartments()
	for index := 0; index < EmployeeCount; index++ {
		department := assignments[index]
		departmentID := s.departmentIDs[department.Slug]
		office := officeByDepartment(department.Slug)
		firstName := employeeFirstNames[index%len(employeeFirstNames)]
		lastName := employeeLastNames[(index*7)%len(employeeLastNames)]
		displayName := fmt.Sprintf("%s %s", firstName, lastName)
		emailLocal := fmt.Sprintf("%s.%s%04d", strings.ToLower(firstName), strings.ToLower(lastName), index+1)
		identity, err := s.people.CreateIdentity(ctx, people.CreateIdentityInput{
			Kind: people.IdentityPerson, DisplayName: displayName,
			Email:        emailLocal + "@riverside-demo.invalid",
			DepartmentID: departmentID, SiteID: s.siteID,
			BuildingID: s.buildingIDs[department.BuildingSlug], RoomID: s.roomIDs[office.Slug],
			Status:   people.StatusActive,
			Provider: SourceSystemID, ProviderSubject: fmt.Sprintf("employee-%04d", index+1),
		})
		if err != nil {
			if errors.Is(err, people.ErrConflict) {
				continue
			}
			return created, fmt.Errorf("create employee %q: %w", displayName, err)
		}
		s.employees = append(s.employees, seededEmployee{
			ID: identity.ID, DepartmentSlug: department.Slug,
			BuildingSlug: department.BuildingSlug, OfficeSlug: office.Slug,
		})
		s.peopleIndex = append(s.peopleIndex, seededPerson{
			ID: identity.ID, DepartmentSlug: department.Slug, Kind: "employee", VisualArts: false,
		})
		created++
	}
	return created, nil
}

func (s *Seeder) seedAssets(ctx context.Context) (int, error) {
	total := 0
	for _, lab := range campusLabs {
		count, err := s.seedLabAssets(ctx, lab)
		if err != nil {
			return total, err
		}
		total += count
	}
	officeCount, err := s.seedOfficeAssets(ctx)
	if err != nil {
		return total, err
	}
	total += officeCount
	custodialCount, err := s.seedCustodialAssets(ctx)
	if err != nil {
		return total, err
	}
	total += custodialCount
	workforceCount, err := s.seedWorkforceLaptops(ctx)
	if err != nil {
		return total, err
	}
	return total + workforceCount, nil
}

func (s *Seeder) seedOfficeAssets(ctx context.Context) (int, error) {
	modelID := s.modelIDs["dell-optiplex-7020"]
	created := 0
	for _, office := range campusOffices {
		buildingID := s.buildingIDs[office.BuildingSlug]
		roomID := s.roomIDs[office.Slug]
		departmentID := s.departmentIDs[office.DepartmentSlug]
		for desk := 1; desk <= office.DeskCount; desk++ {
			slug := fmt.Sprintf("office-%s-desk-%02d", office.Slug, desk)
			_, err := s.atlas.CreateAsset(ctx, atlas.CreateAssetInput{
				ID: slug, ModelID: modelID,
				Name:            fmt.Sprintf("%s Desk %02d", office.Name, desk),
				Kind:            "desktop",
				AssetTag:        fmt.Sprintf("RCC-OFF-%04d", created+1),
				Hostname:        fmt.Sprintf("office-%s-%02d.riverside-demo.invalid", office.Slug, desk),
				DeploymentNotes: "Faculty/staff office workstation; Microsoft Office 365",
				References: atlas.References{
					SiteID: s.siteID, BuildingID: buildingID, RoomID: roomID, DepartmentID: departmentID,
				},
				Status:        "active",
				Attributes:    map[string]string{"primaryUse": "office", "imageBuild": "campus-office-2026.1"},
				Components:    stationAccessories("desktop", false),
				UnitCostMinor: 98900, Currency: "USD",
			})
			if err != nil {
				if errors.Is(err, atlas.ErrConflict) {
					s.trackOfficeAsset(office.BuildingSlug, slug)
					continue
				}
				return created, fmt.Errorf("create office asset %q: %w", slug, err)
			}
			s.trackOfficeAsset(office.BuildingSlug, slug)
			created++
		}
	}
	return created, nil
}

func (s *Seeder) seedLabAssets(ctx context.Context, lab labDef) (int, error) {
	modelID := s.modelIDs[lab.ModelSlug]
	buildingID := s.buildingIDs[lab.BuildingSlug]
	roomID := s.roomIDs[lab.Slug]
	departmentID := s.departmentIDs[lab.DepartmentSlug]
	created := 0
	for start := 0; start < lab.MachineCount; start += atlas.MaximumBulkAssets {
		end := start + atlas.MaximumBulkAssets
		if end > lab.MachineCount {
			end = lab.MachineCount
		}
		items := make([]atlas.CreateAssetInput, 0, end-start)
		for station := start; station < end; station++ {
			stationNumber := station + 1
			slug := fmt.Sprintf("%s-station-%03d", lab.Slug, stationNumber)
			items = append(items, atlas.CreateAssetInput{
				ID: slug, ModelID: modelID,
				Name:            fmt.Sprintf("%s Station %03d", lab.Name, stationNumber),
				Kind:            campusModels[modelSlugIndex(lab.ModelSlug)].Kind,
				AssetTag:        labStationAssetTag(lab.Slug, stationNumber),
				Hostname:        fmt.Sprintf("lab-%s-%03d.riverside-demo.invalid", lab.Slug, stationNumber),
				DeploymentNotes: lab.SoftwareNotes,
				References: atlas.References{
					SiteID: s.siteID, BuildingID: buildingID, RoomID: roomID, DepartmentID: departmentID,
				},
				Status:        "active",
				Attributes:    map[string]string{"imageBuild": "campus-lab-2026.1", "primaryUse": "instruction"},
				Components:    stationAccessories(campusModels[modelSlugIndex(lab.ModelSlug)].Kind, false),
				UnitCostMinor: campusModels[modelSlugIndex(lab.ModelSlug)].UnitCostMinor,
				Currency:      "USD",
			})
		}
		result, err := s.atlas.CreateAssetsFromModel(ctx, atlas.BulkCreateAssetsInput{ModelID: modelID, Items: items})
		if err != nil {
			if !errors.Is(err, atlas.ErrConflict) {
				return created, fmt.Errorf("create lab assets for %q: %w", lab.Name, err)
			}
			s.rememberExistingLabAssets(ctx, lab.Slug, items)
			continue
		}
		for _, asset := range result.Items {
			s.trackLabAsset(lab.Slug, asset.ID)
		}
		created += len(result.Items)
	}
	return created, nil
}

func (s *Seeder) seedWorkforceLaptops(ctx context.Context) (int, error) {
	created := 0
	employeeIndex := 0
	custodialSerial := 0
	for _, entry := range campusWorkforceLaptops {
		modelID := s.modelIDs[entry.ModelSlug]
		modelKind := campusModels[modelSlugIndex(entry.ModelSlug)].Kind
		for index := 0; index < entry.Count; index++ {
			employee := seededEmployee{
				DepartmentSlug: campusDepartments[0].Slug,
				BuildingSlug:   campusDepartments[0].BuildingSlug,
				OfficeSlug:     campusOffices[0].Slug,
			}
			for employeeIndex < len(s.employees) {
				candidate := s.employees[employeeIndex]
				employeeIndex++
				if candidate.DepartmentSlug == "custodial" {
					custodialSerial++
				}
				if employeeReceivesLaptop(candidate, custodialSerial) {
					employee = candidate
					break
				}
			}
			buildingID := s.buildingIDs[employee.BuildingSlug]
			roomID := s.roomIDs[employee.OfficeSlug]
			departmentID := s.departmentIDs[employee.DepartmentSlug]
			slug := fmt.Sprintf("workforce-%s-%04d", entry.ModelSlug, index+1)
			_, err := s.atlas.CreateAsset(ctx, atlas.CreateAssetInput{
				ID: slug, ModelID: modelID,
				Name:            fmt.Sprintf("Faculty/Staff %s %04d", campusModels[modelSlugIndex(entry.ModelSlug)].Name, index+1),
				Kind:            modelKind,
				AssetTag:        fmt.Sprintf("RCC-LTP-%04d", created+1),
				Hostname:        fmt.Sprintf("ltp-%04d.riverside-demo.invalid", created+1),
				DeploymentNotes: "Standard faculty/staff laptop; assigned to office building",
				References: atlas.References{
					SiteID: s.siteID, BuildingID: buildingID, RoomID: roomID,
					DepartmentID: departmentID, UserID: employee.ID,
				},
				Status:        "active",
				Attributes:    map[string]string{"primaryUse": "office", "imageBuild": "campus-laptop-2026.1"},
				Components:    stationAccessories("laptop", true),
				UnitCostMinor: campusModels[modelSlugIndex(entry.ModelSlug)].UnitCostMinor, Currency: "USD",
			})
			if err != nil {
				if errors.Is(err, atlas.ErrConflict) {
					s.trackLaptopAsset(vendorForModelSlug(entry.ModelSlug), slug, employee.ID)
					continue
				}
				return created, fmt.Errorf("create workforce laptop %q: %w", slug, err)
			}
			s.trackLaptopAsset(vendorForModelSlug(entry.ModelSlug), slug, employee.ID)
			created++
		}
	}
	return created, nil
}

func (s *Seeder) seedGuardUsers(ctx context.Context) (int, error) {
	if err := s.ensureBootstrapAdmin(ctx); err != nil {
		return 0, err
	}
	provisioned := 0
	for _, user := range campusDemoUsers {
		if user.Admin {
			provisioned++
			continue
		}
		roleID := stableID("role", user.Slug)
		role := guard.Role{
			ID: roleID, OrganizationID: s.organizationID, Name: user.RoleName,
			Description: user.Description, Permissions: user.Permissions, Source: guard.LocalRoleSource,
		}
		if err := s.guardStore.CreateRole(ctx, role); err != nil && !errors.Is(err, guard.ErrConflict) {
			return provisioned, fmt.Errorf("create role %q: %w", user.RoleName, err)
		}
		accountID := stableID("account", user.Slug)
		now := s.now()
		account, _, err := s.guardStore.ProvisionExternalAccount(ctx, guard.ExternalAccountProvisioning{
			Account: guard.Account{
				ID: accountID, OrganizationID: s.organizationID,
				Username: user.Username, NormalizedUsername: strings.ToLower(user.Username),
				Email: user.Email, DisplayName: user.DisplayName, Status: "active",
				CreatedAt: now, UpdatedAt: now,
			},
			Identity: guard.ExternalIdentity{
				OrganizationID: s.organizationID, Issuer: campusDemoIssuer,
				Subject: user.Slug, AccountID: accountID, CreatedAt: now, UpdatedAt: now,
			},
			AssignmentSource: campusDemoAssignmentSource,
		})
		if err != nil {
			return provisioned, fmt.Errorf("provision account %q: %w", user.Username, err)
		}
		passwordHash, hashErr := guard.NewArgon2idHasher().Hash(user.Password)
		if hashErr != nil {
			return provisioned, hashErr
		}
		if err := s.guardStore.UpdatePasswordHash(ctx, account.ID, passwordHash, now); err != nil {
			return provisioned, fmt.Errorf("set password for %q: %w", user.Username, err)
		}
		scopeKind := user.ScopeKind
		if scopeKind == "" {
			scopeKind = guard.ScopeOrganization
		}
		scopeID := s.organizationID
		switch scopeKind {
		case guard.ScopeSite:
			scopeID = s.siteID
		case guard.ScopeDepartment:
			scopeID = s.departmentIDs[user.ScopeSlug]
		}
		assignment := guard.RoleAssignment{
			ID: stableID("assignment", user.Slug), OrganizationID: s.organizationID,
			AccountID: account.ID, RoleID: roleID,
			Scope:  guard.Scope{Kind: scopeKind, OrganizationID: s.organizationID, ResourceID: scopeID},
			Source: guard.LocalAssignmentSource, CreatedAt: now,
		}
		if err := s.guardStore.CreateRoleAssignment(ctx, assignment); err != nil && !errors.Is(err, guard.ErrConflict) {
			return provisioned, fmt.Errorf("assign role for %q: %w", user.Username, err)
		}
		provisioned++
	}
	return provisioned, nil
}

func (s *Seeder) ensureBootstrapAdmin(ctx context.Context) error {
	required, err := s.guardStore.BootstrapRequired(ctx, s.organizationID)
	if err != nil {
		return err
	}
	if !required {
		return nil
	}
	admin := campusDemoUsers[0]
	_, err = s.guardService.Bootstrap(ctx, guard.BootstrapInput{
		Username: admin.Username, Email: admin.Email, DisplayName: admin.DisplayName, Password: admin.Password,
	}, true)
	if err != nil {
		return fmt.Errorf("bootstrap campus administrator: %w", err)
	}
	return nil
}

func (s *Seeder) auditComplete(ctx context.Context, result Result) error {
	eventID := stableID("audit", "campus-seeded")
	event := foundation.AuditEvent{
		ID: eventID, OrganizationID: s.organizationID, ActorID: ActorID, CorrelationID: eventID,
		Action: "campus_demo.seeded", ResourceType: "campus_demo_dataset", ResourceID: s.siteID,
		OccurredAt: s.now(),
		Metadata: map[string]string{
			"sourceSystemId": SourceSystemID,
			"employees":      fmt.Sprintf("%d", result.EmployeeCount),
			"students":       fmt.Sprintf("%d", result.StudentCount),
			"assets":         fmt.Sprintf("%d", result.AssetCount),
			"labs":           fmt.Sprintf("%d", result.LabCount),
			"licenses":       fmt.Sprintf("%d", result.LicenseCount),
			"assignments":    fmt.Sprintf("%d", result.AssignmentCount),
		},
	}
	return s.auditor.Record(ctx, event)
}

func (s *Seeder) writeCredentials() error {
	if err := os.MkdirAll(filepath.Dir(s.credentialsPath), 0o755); err != nil {
		return fmt.Errorf("create credentials directory: %w", err)
	}
	var builder strings.Builder
	builder.WriteString("# Riverside Community College demo credentials\n")
	builder.WriteString("# Organization: demo-campus (set STEWARDMESH_ORGANIZATION_ID=demo-campus)\n")
	builder.WriteString("# API: http://localhost:8080  Web: http://localhost:5173\n\n")
	for _, user := range campusDemoUsers {
		builder.WriteString(fmt.Sprintf("[%s]\n", user.Username))
		builder.WriteString(fmt.Sprintf("username=%s\n", user.Username))
		builder.WriteString(fmt.Sprintf("password=%s\n", user.Password))
		builder.WriteString(fmt.Sprintf("email=%s\n", user.Email))
		builder.WriteString(fmt.Sprintf("display_name=%s\n", user.DisplayName))
		builder.WriteString(fmt.Sprintf("role=%s\n", user.RoleName))
		builder.WriteString(fmt.Sprintf("description=%s\n", user.Description))
		builder.WriteString("api=http://localhost:8080\n")
		builder.WriteString("web=http://localhost:5173\n\n")
	}
	return os.WriteFile(s.credentialsPath, []byte(builder.String()), 0o600)
}

func modelSlugIndex(slug string) int {
	for index, model := range campusModels {
		if model.Slug == slug {
			return index
		}
	}
	return 0
}

func labStationAssetTag(slug string, stationNumber int) string {
	return fmt.Sprintf("RCC-LAB-%s-%03d", strings.ToUpper(strings.TrimSpace(slug)), stationNumber)
}

func (s *Seeder) rememberExistingLabAssets(ctx context.Context, labSlug string, items []atlas.CreateAssetInput) {
	for _, item := range items {
		if _, err := s.atlas.GetAsset(ctx, item.ID); err != nil {
			continue
		}
		s.trackLabAsset(labSlug, item.ID)
	}
}

func (s *Seeder) trackLabAsset(labSlug, id string) {
	s.labAssetsBySlug[labSlug] = append(s.labAssetsBySlug[labSlug], id)
	s.labAssetIDs = append(s.labAssetIDs, id)
	s.allAssetIDs = append(s.allAssetIDs, id)
}

func (s *Seeder) trackOfficeAsset(buildingSlug, id string) {
	s.officeAssetsByBuilding[buildingSlug] = append(s.officeAssetsByBuilding[buildingSlug], id)
	s.officeAssetIDs = append(s.officeAssetIDs, id)
	s.allAssetIDs = append(s.allAssetIDs, id)
}

func (s *Seeder) trackLaptopAsset(vendor, id, employeeID string) {
	s.laptopAssetsByVendor[vendor] = append(s.laptopAssetsByVendor[vendor], id)
	s.laptopAssetIDs = append(s.laptopAssetIDs, id)
	s.allAssetIDs = append(s.allAssetIDs, id)
	if employeeID != "" {
		s.employeeAssetIDs[employeeID] = id
	}
}
