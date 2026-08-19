package campusseed

import "testing"

func TestCampusDatasetShape(t *testing.T) {
	if len(campusBuildings) != 8 {
		t.Fatalf("expected 8 campus buildings including the residence hall, got %d", len(campusBuildings))
	}
	if len(campusDepartments) < 26 {
		t.Fatalf("expected many departments including custodial, got %d", len(campusDepartments))
	}
	if len(campusOffices) < 25 {
		t.Fatalf("expected office suites across buildings, got %d", len(campusOffices))
	}
	if len(campusShops) < 2 {
		t.Fatalf("expected custodial shop rooms, got %d", len(campusShops))
	}
	if len(campusLabs) != 15 {
		t.Fatalf("expected 15 computer labs, got %d", len(campusLabs))
	}
	if len(campusModels) < 9 {
		t.Fatalf("expected diverse asset models including iPads, got %d", len(campusModels))
	}
	if len(campusSoftwareProducts) != 3 {
		t.Fatalf("expected three campus software products, got %d", len(campusSoftwareProducts))
	}
	if len(campusLicenseOfferings) != 3 {
		t.Fatalf("expected three license offerings, got %d", len(campusLicenseOfferings))
	}
	if EmployeeCount < 1000 {
		t.Fatalf("expected at least 1000 employees, got %d", EmployeeCount)
	}
	if ActiveStudentCount != 5000 {
		t.Fatalf("expected 5000 active students, got %d", ActiveStudentCount)
	}
	if InactiveStudentCount != 15000 {
		t.Fatalf("expected 15000 inactive students, got %d", InactiveStudentCount)
	}

	buildingSlugs := map[string]struct{}{}
	for _, building := range campusBuildings {
		buildingSlugs[building.Slug] = struct{}{}
	}

	labMachines := 0
	for _, lab := range campusLabs {
		if lab.MachineCount < 1 {
			t.Fatalf("lab %q must have machines", lab.Name)
		}
		if _, ok := buildingSlugs[lab.BuildingSlug]; !ok {
			t.Fatalf("lab %q references unknown building %q", lab.Name, lab.BuildingSlug)
		}
		labMachines += lab.MachineCount
	}
	if labMachines < 400 {
		t.Fatalf("expected a substantial lab fleet, got %d machines", labMachines)
	}
	if len(campusClassrooms) != 18 {
		t.Fatalf("expected 18 classrooms, got %d", len(campusClassrooms))
	}
	if rooms := campusResidenceRooms(); len(rooms) != 40 {
		t.Fatalf("expected 40 residence rooms, got %d", len(rooms))
	}
	for _, classroom := range campusClassrooms {
		if _, ok := buildingSlugs[classroom.BuildingSlug]; !ok {
			t.Fatalf("classroom %q references unknown building %q", classroom.Name, classroom.BuildingSlug)
		}
	}
	if _, ok := buildingSlugs[ResidenceBuildingSlug]; !ok {
		t.Fatal("residence hall building is missing")
	}

	officeDesks := 0
	for _, office := range campusOffices {
		if _, ok := buildingSlugs[office.BuildingSlug]; !ok {
			t.Fatalf("office %q references unknown building %q", office.Name, office.BuildingSlug)
		}
		officeDesks += office.DeskCount
	}
	if officeDesks < 200 {
		t.Fatalf("expected a substantial office desk fleet, got %d desks", officeDesks)
	}

	for _, department := range campusDepartments {
		if _, ok := buildingSlugs[department.BuildingSlug]; !ok {
			t.Fatalf("department %q references unknown building %q", department.Name, department.BuildingSlug)
		}
		office := officeByDepartment(department.Slug)
		if office.DepartmentSlug != department.Slug {
			t.Fatalf("department %q has no dedicated office suite", department.Slug)
		}
	}

	assignments := employeeDepartments()
	if len(assignments) != EmployeeCount {
		t.Fatalf("expected %d employee assignments, got %d", EmployeeCount, len(assignments))
	}

	if len(campusDemoUsers) < 8 {
		t.Fatalf("expected multiple demo guard users, got %d", len(campusDemoUsers))
	}
	for _, user := range campusDemoUsers {
		if len(user.Password) < 15 {
			t.Fatalf("password for %q must satisfy Guard minimum length", user.Username)
		}
	}
}

func TestStableIDDeterministic(t *testing.T) {
	first := stableID("role", "inventory-steward")
	second := stableID("role", "inventory-steward")
	if first != second || len(first) != 32 {
		t.Fatalf("unexpected stable id: %q", first)
	}
}

func TestEmployeeReceivesLaptop(t *testing.T) {
	if employeeReceivesLaptop(seededEmployee{DepartmentSlug: "custodial"}, 9) {
		t.Fatal("regular custodial staff should not receive laptops")
	}
	if !employeeReceivesLaptop(seededEmployee{DepartmentSlug: "custodial"}, 4) {
		t.Fatal("custodial supervisors should receive laptops")
	}
	if !employeeReceivesLaptop(seededEmployee{DepartmentSlug: "computer-science"}, 99) {
		t.Fatal("non-custodial staff should receive laptops")
	}
}

func TestCampusLabAssetTagsAreUnique(t *testing.T) {
	tags := map[string]string{}
	slugs := map[string]struct{}{}
	for _, lab := range campusLabs {
		if _, exists := slugs[lab.Slug]; exists {
			t.Fatalf("duplicate lab slug %q", lab.Slug)
		}
		slugs[lab.Slug] = struct{}{}
		for station := 1; station <= lab.MachineCount; station++ {
			tag := labStationAssetTag(lab.Slug, station)
			if other, exists := tags[tag]; exists {
				t.Fatalf("lab asset tag %q collides between %q and %q", tag, other, lab.Name)
			}
			tags[tag] = lab.Name
		}
	}
}
