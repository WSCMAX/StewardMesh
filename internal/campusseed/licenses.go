package campusseed

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/maxlemke/stewardmesh/internal/ledger"
	"github.com/maxlemke/stewardmesh/internal/stack"
)

type softwareProductDef struct {
	Slug      string
	Name      string
	Publisher string
	Category  string
	Version   string
}

type licenseOfferingDef struct {
	Slug              string
	ProductSlug       string
	Name              string
	EntitlementMetric string
	Quantity          int64
	PerSeatMinor      int64
	Currency          string
	Description       string
}

var campusSoftwareProducts = []softwareProductDef{
	{Slug: "microsoft-365", Name: "Microsoft 365", Publisher: "Microsoft", Category: "productivity", Version: "A3 for faculty, staff, and students"},
	{Slug: "adobe-creative-cloud", Name: "Adobe Creative Cloud", Publisher: "Adobe", Category: "creative", Version: "All Apps"},
	{Slug: "autodesk-education", Name: "Autodesk Education Suite", Publisher: "Autodesk", Category: "engineering", Version: "2026"},
}

var campusLicenseOfferings = []licenseOfferingDef{
	{
		Slug: "microsoft-365-a3", ProductSlug: "microsoft-365", Name: "Microsoft 365 A3 (volume)",
		EntitlementMetric: "user", Quantity: 22000, PerSeatMinor: 14400, Currency: "USD",
		Description: "Annual Microsoft 365 A3 per-seat subscription for employees and students",
	},
	{
		Slug: "adobe-creative-cloud-all-apps", ProductSlug: "adobe-creative-cloud", Name: "Adobe Creative Cloud All Apps",
		EntitlementMetric: "user", Quantity: 1800, PerSeatMinor: 35900, Currency: "USD",
		Description: "Annual Adobe Creative Cloud All Apps for staff and visual arts students",
	},
	{
		Slug: "autodesk-education-lab", ProductSlug: "autodesk-education", Name: "Autodesk Education Lab Pack",
		EntitlementMetric: "device", Quantity: 500, PerSeatMinor: 85000, Currency: "USD",
		Description: "Annual Autodesk lab device entitlement for AutoCAD, Revit, and Maya seats",
	},
}

func (s *Seeder) seedSoftwareCatalog(ctx context.Context) error {
	now := s.now()
	startsOn := time.Date(now.Year(), 7, 1, 0, 0, 0, 0, time.UTC)
	expiresOn := time.Date(now.Year()+1, 6, 30, 0, 0, 0, 0, time.UTC)

	for _, product := range campusSoftwareProducts {
		created, err := s.stack.CreateProduct(ctx, stack.CreateProductInput{
			ID: product.Slug, Name: product.Name, Publisher: product.Publisher, Category: product.Category,
			Status: "active", SourceSystemID: SourceSystemID, SourceRecordID: product.Slug,
		})
		if err != nil && !errors.Is(err, stack.ErrConflict) {
			return fmt.Errorf("create software product %q: %w", product.Name, err)
		}
		productID := product.Slug
		if err == nil {
			productID = created.ID
		}
		s.productIDs[product.Slug] = productID

		versionID := product.Slug + "-version"
		version, err := s.stack.CreateVersion(ctx, stack.CreateVersionInput{
			ID: versionID, ProductID: productID, Name: product.Version, Status: "active",
			ReleasedOn: &startsOn, SourceSystemID: SourceSystemID, SourceRecordID: versionID,
		})
		if err != nil && !errors.Is(err, stack.ErrConflict) {
			return fmt.Errorf("create software version for %q: %w", product.Name, err)
		}
		if err == nil {
			versionID = version.ID
		}
		s.versionIDs[product.Slug] = versionID
	}

	for _, offering := range campusLicenseOfferings {
		totalMinor := offering.PerSeatMinor * offering.Quantity
		cost, err := s.ledger.ReconcileCost(ctx, ledger.ReconcileCostInput{
			ID: offering.Slug + "-annual-cost", Description: offering.Description,
			Kind: "actual", Currency: offering.Currency, AmountMinor: totalMinor,
			FiscalPeriod: fmt.Sprintf("FY%d", now.Year()), Scenario: "baseline",
			PurchaseOrderID: softwarePurchaseOrderID(offering.Slug),
			SourceSystemID:  SourceSystemID, SourceRecordID: offering.Slug + "-annual-cost",
		})
		if err != nil {
			return fmt.Errorf("reconcile license cost for %q: %w", offering.Name, err)
		}
		license, err := s.stack.CreateLicense(ctx, stack.CreateLicenseInput{
			ID: offering.Slug, ProductID: s.productIDs[offering.ProductSlug],
			VersionID: s.versionIDs[offering.ProductSlug], Name: offering.Name,
			EntitlementMetric: offering.EntitlementMetric, Quantity: offering.Quantity,
			Status: "active", StartsOn: &startsOn, ExpiresOn: &expiresOn,
			VendorID:        softwareVendorID(offering.ProductSlug, s.vendorIDs),
			PurchaseOrderID: softwarePurchaseOrderID(offering.Slug),
			CostRecordID:    cost.Record.ID,
			DocumentIDs:     softwareDocumentIDs(offering.ProductSlug, s.blobIDs),
			SourceSystemID:  SourceSystemID, SourceRecordID: offering.Slug,
		})
		if err != nil && !errors.Is(err, stack.ErrConflict) {
			return fmt.Errorf("create license %q: %w", offering.Name, err)
		}
		licenseID := offering.Slug
		if err == nil {
			licenseID = license.ID
		}
		s.licenseIDs[offering.Slug] = licenseID
	}
	return nil
}

func (s *Seeder) seedLicenseAssignments(ctx context.Context) (int, error) {
	created := 0
	now := s.now()
	assignedAt := now.Add(-180 * 24 * time.Hour)
	lastUsed := now.Add(-24 * time.Hour)
	alumniEnded := now.Add(-90 * 24 * time.Hour)
	alumniAssigned := alumniEnded.Add(-365 * 24 * time.Hour)

	o365License := s.licenseIDs["microsoft-365-a3"]
	adobeLicense := s.licenseIDs["adobe-creative-cloud-all-apps"]
	autodeskLicense := s.licenseIDs["autodesk-education-lab"]

	employeeLimit := 48
	if len(s.employees) < employeeLimit {
		employeeLimit = len(s.employees)
	}
	for _, employee := range s.employees[:employeeLimit] {
		count, err := s.createIdentityLicense(ctx, o365License, employee.ID, "o365-employee", assignedAt, lastUsed, nil)
		if err != nil {
			return created, err
		}
		created += count
		if employee.DepartmentSlug == "graphic-design" || employee.DepartmentSlug == "studio-arts" {
			count, err = s.createIdentityLicense(ctx, adobeLicense, employee.ID, "adobe-employee", assignedAt, lastUsed, nil)
			if err != nil {
				return created, err
			}
			created += count
		}
	}

	activeAssigned, visualAssigned, alumniAssignedCount := 0, 0, 0
	for _, student := range s.students {
		if student.Active {
			if activeAssigned < 24 {
				count, err := s.createIdentityLicense(ctx, o365License, student.ID, "o365-student-active", assignedAt, lastUsed, nil)
				if err != nil {
					return created, err
				}
				created += count
				activeAssigned++
			}
			if student.VisualArts && visualAssigned < 12 {
				count, err := s.createIdentityLicense(ctx, adobeLicense, student.ID, "adobe-student", assignedAt, lastUsed, nil)
				if err != nil {
					return created, err
				}
				created += count
				visualAssigned++
			}
			continue
		}
		if alumniAssignedCount < 8 {
			count, err := s.createIdentityLicense(ctx, o365License, student.ID, "o365-student-alumni", alumniAssigned, lastUsed, &alumniEnded)
			if err != nil {
				return created, err
			}
			created += count
			alumniAssignedCount++
		}
	}

	for _, lab := range campusLabs {
		roomID := s.roomIDs[lab.Slug]
		if roomID == "" {
			continue
		}
		_, err := s.stack.CreateAssignment(ctx, stack.CreateAssignmentInput{
			ID: stableID("stack-assignment", "autodesk-lab-room-"+lab.Slug), LicenseID: autodeskLicense,
			AssigneeKind: "room", AssigneeID: roomID, Seats: int64(lab.MachineCount), UsageState: "used",
			AssignedAt: assignedAt, LastUsedAt: &lastUsed,
			SourceSystemID: SourceSystemID, SourceRecordID: "autodesk-lab-room-" + lab.Slug,
		})
		if err != nil && !errors.Is(err, stack.ErrConflict) {
			return created, fmt.Errorf("assign Autodesk lab pack to room %q: %w", lab.Name, err)
		}
		if err == nil {
			created++
		}
	}

	for _, assetID := range s.labAssetIDs {
		if versionID, ok := s.versionIDs["autodesk-education"]; ok {
			if err := s.recordSoftwareInstallation(ctx, versionID, assetID, "autodesk"); err != nil {
				return created, err
			}
		}
	}
	return created, nil
}

func (s *Seeder) createIdentityLicense(ctx context.Context, licenseID, identityID, slug string, assignedAt, lastUsed time.Time, endedAt *time.Time) (int, error) {
	usage := "used"
	lastUsedAt := lastUsed
	if endedAt != nil {
		usage = "unused"
		lastUsedAt = endedAt.Add(-30 * 24 * time.Hour)
	}
	_, err := s.stack.CreateAssignment(ctx, stack.CreateAssignmentInput{
		ID: stableID("stack-assignment", slug+"-"+identityID), LicenseID: licenseID,
		AssigneeKind: "identity", AssigneeID: identityID, Seats: 1, UsageState: usage,
		AssignedAt: assignedAt, LastUsedAt: &lastUsedAt, EndedAt: endedAt,
		SourceSystemID: SourceSystemID, SourceRecordID: slug + "-" + identityID,
	})
	if err != nil {
		if errors.Is(err, stack.ErrConflict) {
			return 0, nil
		}
		return 0, fmt.Errorf("assign license %q to identity %q: %w", licenseID, identityID, err)
	}
	return 1, nil
}

func (s *Seeder) recordSoftwareInstallation(ctx context.Context, versionID, assetID, productSlug string) error {
	installedAt := s.now().Add(-120 * 24 * time.Hour)
	lastUsed := s.now().Add(-12 * time.Hour)
	_, err := s.stack.RecordInstallation(ctx, stack.RecordInstallationInput{
		ID: stableID("stack-install", productSlug+"-"+assetID), VersionID: versionID, AssetID: assetID,
		Status: "installed", UsageState: "used", InstalledAt: installedAt, LastUsedAt: &lastUsed,
		SourceSystemID: SourceSystemID, SourceRecordID: productSlug + "-" + assetID,
	})
	if err != nil && !errors.Is(err, stack.ErrConflict) {
		return fmt.Errorf("record installation on asset %q: %w", assetID, err)
	}
	return nil
}

func (s *Seeder) installLabProductivitySoftware(ctx context.Context) error {
	o365Version := s.versionIDs["microsoft-365"]
	if o365Version == "" {
		return nil
	}
	employeeLimit := 48
	if len(s.employees) < employeeLimit {
		employeeLimit = len(s.employees)
	}
	for _, employee := range s.employees[:employeeLimit] {
		assetID := s.employeeAssetIDs[employee.ID]
		if assetID == "" {
			continue
		}
		if err := s.recordSoftwareInstallation(ctx, o365Version, assetID, "office365"); err != nil {
			return err
		}
	}
	return nil
}
