package stack

// Requirements: REQ-STACK-001, REQ-EXCHANGE-001. Features: software.licenses, migration.packages.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/foundation"
)

var stableIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
var sourceRecordIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,254}$`)

var (
	productStatuses      = stringSet("active", "retired")
	versionStatuses      = stringSet("active", "unsupported", "retired")
	installationStatuses = stringSet("installed", "removed")
	licenseStatuses      = stringSet("active", "expired", "retired")
	usageStates          = stringSet("unknown", "used", "unused")
	entitlementMetrics   = stringSet("device", "user", "concurrent", "site", "enterprise")
	assigneeKinds        = stringSet("asset", "identity", "department", "site")
)

type ServiceConfig struct {
	OrganizationID string
	Now            func() time.Time
}

type Service struct {
	store          Store
	references     ReferenceValidator
	auditor        foundation.Auditor
	organizationID string
	now            func() time.Time
}

func NewService(store Store, references ReferenceValidator, auditor foundation.Auditor, configuration ServiceConfig) (*Service, error) {
	if store == nil || references == nil || auditor == nil {
		return nil, errors.New("Stack store, reference validator, and auditor are required")
	}
	configuration.OrganizationID = strings.TrimSpace(configuration.OrganizationID)
	if !stableIDPattern.MatchString(configuration.OrganizationID) {
		return nil, errors.New("Stack organization id is required")
	}
	if configuration.Now == nil {
		configuration.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{store: store, references: references, auditor: auditor, organizationID: configuration.OrganizationID, now: configuration.Now}, nil
}

func (s *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	return s.store.Snapshot(ctx, s.organizationID)
}

func (s *Service) CreateProduct(ctx context.Context, input CreateProductInput) (Product, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.Name = strings.TrimSpace(input.Name)
	input.Publisher = strings.TrimSpace(input.Publisher)
	input.Category = strings.TrimSpace(input.Category)
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	input.SourceSystemID = strings.ToLower(strings.TrimSpace(input.SourceSystemID))
	input.SourceRecordID = strings.TrimSpace(input.SourceRecordID)
	if input.Status == "" {
		input.Status = "active"
	}
	if !optionalID(input.ID) || !validTextRange(input.Name, 1, 200) || !validTextRange(input.Publisher, 1, 200) ||
		!validText(input.Category, 100) || !hasString(productStatuses, input.Status) || !validSource(input.SourceSystemID, input.SourceRecordID) {
		return Product{}, ErrInvalidInput
	}
	id, err := s.newID(input.ID)
	if err != nil {
		return Product{}, err
	}
	now := s.now().UTC()
	product, created, err := s.store.CreateProduct(ctx, Product{
		ID: id, OrganizationID: s.organizationID, Name: input.Name, Publisher: input.Publisher,
		Category: input.Category, Status: input.Status, SourceSystemID: input.SourceSystemID,
		SourceRecordID: input.SourceRecordID, Revision: 1, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return Product{}, err
	}
	if created {
		if err := s.audit(ctx, "stack.product.created", "software_product", product.ID, map[string]string{"status": product.Status}); err != nil {
			return Product{}, fmt.Errorf("audit Stack product creation: %w", err)
		}
	}
	return product, nil
}

func (s *Service) CreateVersion(ctx context.Context, input CreateVersionInput) (Version, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.ProductID = strings.TrimSpace(input.ProductID)
	input.Name = strings.TrimSpace(input.Name)
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	input.SourceSystemID = strings.ToLower(strings.TrimSpace(input.SourceSystemID))
	input.SourceRecordID = strings.TrimSpace(input.SourceRecordID)
	if input.Status == "" {
		input.Status = "active"
	}
	releasedOn, ok := optionalDate(input.ReleasedOn)
	if !optionalID(input.ID) || !stableIDPattern.MatchString(input.ProductID) || !validTextRange(input.Name, 1, 100) ||
		!hasString(versionStatuses, input.Status) || !ok || !validSource(input.SourceSystemID, input.SourceRecordID) {
		return Version{}, ErrInvalidInput
	}
	if _, err := s.store.GetProduct(ctx, s.organizationID, input.ProductID); err != nil {
		return Version{}, referenceError(err)
	}
	id, err := s.newID(input.ID)
	if err != nil {
		return Version{}, err
	}
	now := s.now().UTC()
	version, created, err := s.store.CreateVersion(ctx, Version{
		ID: id, OrganizationID: s.organizationID, ProductID: input.ProductID, Name: input.Name,
		ReleasedOn: releasedOn, Status: input.Status, SourceSystemID: input.SourceSystemID,
		SourceRecordID: input.SourceRecordID, Revision: 1, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return Version{}, err
	}
	if created {
		if err := s.audit(ctx, "stack.version.created", "software_version", version.ID, map[string]string{"productId": version.ProductID, "status": version.Status}); err != nil {
			return Version{}, fmt.Errorf("audit Stack version creation: %w", err)
		}
	}
	return version, nil
}

func (s *Service) RecordInstallation(ctx context.Context, input RecordInstallationInput) (Installation, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.VersionID = strings.TrimSpace(input.VersionID)
	input.AssetID = strings.TrimSpace(input.AssetID)
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	input.UsageState = strings.ToLower(strings.TrimSpace(input.UsageState))
	input.SourceSystemID = strings.ToLower(strings.TrimSpace(input.SourceSystemID))
	input.SourceRecordID = strings.TrimSpace(input.SourceRecordID)
	if input.Status == "" {
		input.Status = "installed"
	}
	if input.UsageState == "" {
		input.UsageState = "unknown"
	}
	installedAt, installedOK := requiredInstant(input.InstalledAt)
	lastUsedAt, lastUsedOK := optionalInstant(input.LastUsedAt)
	removedAt, removedOK := optionalInstant(input.RemovedAt)
	if !optionalID(input.ID) || !stableIDPattern.MatchString(input.VersionID) || !stableIDPattern.MatchString(input.AssetID) ||
		!hasString(installationStatuses, input.Status) || !hasString(usageStates, input.UsageState) || !installedOK || !lastUsedOK || !removedOK ||
		(input.Status == "installed" && removedAt != nil) || (input.Status == "removed" && removedAt == nil) ||
		(lastUsedAt != nil && lastUsedAt.Before(installedAt)) || (removedAt != nil && removedAt.Before(installedAt)) ||
		(lastUsedAt != nil && removedAt != nil && lastUsedAt.After(*removedAt)) ||
		!validSource(input.SourceSystemID, input.SourceRecordID) {
		return Installation{}, ErrInvalidInput
	}
	if _, err := s.store.GetVersion(ctx, s.organizationID, input.VersionID); err != nil {
		return Installation{}, referenceError(err)
	}
	asset, err := s.references.ResolveAsset(ctx, input.AssetID)
	if err != nil {
		return Installation{}, err
	}
	if asset.ID != input.AssetID {
		return Installation{}, ErrReferenceMissing
	}
	id, err := s.newID(input.ID)
	if err != nil {
		return Installation{}, err
	}
	now := s.now().UTC()
	installation, created, err := s.store.CreateInstallation(ctx, Installation{
		ID: id, OrganizationID: s.organizationID, VersionID: input.VersionID, AssetID: input.AssetID,
		Status: input.Status, UsageState: input.UsageState, InstalledAt: installedAt, LastUsedAt: lastUsedAt,
		RemovedAt: removedAt, SourceSystemID: input.SourceSystemID, SourceRecordID: input.SourceRecordID,
		Revision: 1, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return Installation{}, err
	}
	if created {
		if err := s.audit(ctx, "stack.installation.recorded", "software_installation", installation.ID, map[string]string{
			"versionId": installation.VersionID, "assetId": installation.AssetID, "status": installation.Status,
		}); err != nil {
			return Installation{}, fmt.Errorf("audit Stack installation: %w", err)
		}
	}
	return installation, nil
}

func (s *Service) CreateLicense(ctx context.Context, input CreateLicenseInput) (License, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.ProductID = strings.TrimSpace(input.ProductID)
	input.VersionID = strings.TrimSpace(input.VersionID)
	input.Name = strings.TrimSpace(input.Name)
	input.EntitlementMetric = strings.ToLower(strings.TrimSpace(input.EntitlementMetric))
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	input.VendorID = strings.TrimSpace(input.VendorID)
	input.PurchaseOrderID = strings.TrimSpace(input.PurchaseOrderID)
	input.ContractID = strings.TrimSpace(input.ContractID)
	input.CostRecordID = strings.TrimSpace(input.CostRecordID)
	input.DocumentIDs = normalizeIDs(input.DocumentIDs)
	input.SourceSystemID = strings.ToLower(strings.TrimSpace(input.SourceSystemID))
	input.SourceRecordID = strings.TrimSpace(input.SourceRecordID)
	if input.Status == "" {
		input.Status = "active"
	}
	startsOn, startsOK := optionalDate(input.StartsOn)
	expiresOn, expiresOK := optionalDate(input.ExpiresOn)
	if !optionalID(input.ID) || !stableIDPattern.MatchString(input.ProductID) || !optionalStableID(input.VersionID) ||
		!validTextRange(input.Name, 1, 200) || !hasString(entitlementMetrics, input.EntitlementMetric) ||
		input.Quantity < 1 || input.Quantity > 1_000_000_000 || !hasString(licenseStatuses, input.Status) ||
		!startsOK || !expiresOK || (startsOn != nil && expiresOn != nil && expiresOn.Before(*startsOn)) ||
		!allOptionalIDs(input.VendorID, input.PurchaseOrderID, input.ContractID, input.CostRecordID) ||
		len(input.DocumentIDs) > 100 || !allStableIDs(input.DocumentIDs) || hasDuplicates(input.DocumentIDs) ||
		!validSource(input.SourceSystemID, input.SourceRecordID) {
		return License{}, ErrInvalidInput
	}
	if _, err := s.store.GetProduct(ctx, s.organizationID, input.ProductID); err != nil {
		return License{}, referenceError(err)
	}
	if input.VersionID != "" {
		version, err := s.store.GetVersion(ctx, s.organizationID, input.VersionID)
		if err != nil || version.ProductID != input.ProductID {
			if err != nil {
				return License{}, referenceError(err)
			}
			return License{}, ErrReferenceMissing
		}
	}
	if err := s.references.ValidateFinancialReferences(ctx, input.VendorID, input.PurchaseOrderID, input.ContractID, input.CostRecordID); err != nil {
		return License{}, err
	}
	if err := s.references.ValidateDocuments(ctx, input.DocumentIDs); err != nil {
		return License{}, err
	}
	id, err := s.newID(input.ID)
	if err != nil {
		return License{}, err
	}
	now := s.now().UTC()
	license, created, err := s.store.CreateLicense(ctx, License{
		ID: id, OrganizationID: s.organizationID, ProductID: input.ProductID, VersionID: input.VersionID,
		Name: input.Name, EntitlementMetric: input.EntitlementMetric, Quantity: input.Quantity,
		Status: input.Status, StartsOn: startsOn, ExpiresOn: expiresOn, VendorID: input.VendorID,
		PurchaseOrderID: input.PurchaseOrderID, ContractID: input.ContractID, CostRecordID: input.CostRecordID,
		DocumentIDs: input.DocumentIDs, SourceSystemID: input.SourceSystemID, SourceRecordID: input.SourceRecordID,
		Revision: 1, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return License{}, err
	}
	if created {
		if err := s.audit(ctx, "stack.license.created", "software_license", license.ID, map[string]string{
			"productId": license.ProductID, "metric": license.EntitlementMetric, "quantity": strconv.FormatInt(license.Quantity, 10), "status": license.Status,
		}); err != nil {
			return License{}, fmt.Errorf("audit Stack license creation: %w", err)
		}
	}
	return license, nil
}

func (s *Service) CreateAssignment(ctx context.Context, input CreateAssignmentInput) (Assignment, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.LicenseID = strings.TrimSpace(input.LicenseID)
	input.AssigneeKind = strings.ToLower(strings.TrimSpace(input.AssigneeKind))
	input.AssigneeID = strings.TrimSpace(input.AssigneeID)
	input.UsageState = strings.ToLower(strings.TrimSpace(input.UsageState))
	input.SourceSystemID = strings.ToLower(strings.TrimSpace(input.SourceSystemID))
	input.SourceRecordID = strings.TrimSpace(input.SourceRecordID)
	if input.UsageState == "" {
		input.UsageState = "unknown"
	}
	assignedAt, assignedOK := requiredInstant(input.AssignedAt)
	lastUsedAt, lastUsedOK := optionalInstant(input.LastUsedAt)
	endedAt, endedOK := optionalInstant(input.EndedAt)
	if !optionalID(input.ID) || !stableIDPattern.MatchString(input.LicenseID) || !hasString(assigneeKinds, input.AssigneeKind) ||
		!stableIDPattern.MatchString(input.AssigneeID) || input.Seats < 1 || input.Seats > 1_000_000_000 ||
		!hasString(usageStates, input.UsageState) || !assignedOK || !lastUsedOK || !endedOK ||
		(lastUsedAt != nil && lastUsedAt.Before(assignedAt)) || (endedAt != nil && endedAt.Before(assignedAt)) ||
		(lastUsedAt != nil && endedAt != nil && lastUsedAt.After(*endedAt)) || !validSource(input.SourceSystemID, input.SourceRecordID) {
		return Assignment{}, ErrInvalidInput
	}
	now := s.now().UTC()
	license, err := s.store.GetLicense(ctx, s.organizationID, input.LicenseID)
	if err != nil {
		return Assignment{}, referenceError(err)
	}
	licenseCheckAt := now
	if assignedAt.After(now) {
		licenseCheckAt = assignedAt
	}
	if (endedAt == nil || endedAt.After(now)) && !licenseActiveAt(license, licenseCheckAt) {
		return Assignment{}, ErrConflict
	}
	if !assignmentKindMatchesMetric(license.EntitlementMetric, input.AssigneeKind) {
		return Assignment{}, ErrInvalidInput
	}
	if err := s.references.ValidateAssignee(ctx, input.AssigneeKind, input.AssigneeID); err != nil {
		return Assignment{}, err
	}
	id, err := s.newID(input.ID)
	if err != nil {
		return Assignment{}, err
	}
	assignment, created, err := s.store.CreateAssignment(ctx, Assignment{
		ID: id, OrganizationID: s.organizationID, LicenseID: input.LicenseID,
		AssigneeKind: input.AssigneeKind, AssigneeID: input.AssigneeID, Seats: input.Seats,
		UsageState: input.UsageState, AssignedAt: assignedAt, LastUsedAt: lastUsedAt, EndedAt: endedAt,
		SourceSystemID: input.SourceSystemID, SourceRecordID: input.SourceRecordID,
		Revision: 1, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return Assignment{}, err
	}
	if created {
		if err := s.audit(ctx, "stack.assignment.created", "software_license_assignment", assignment.ID, map[string]string{
			"licenseId": assignment.LicenseID, "assigneeKind": assignment.AssigneeKind, "seats": strconv.FormatInt(assignment.Seats, 10),
		}); err != nil {
			return Assignment{}, fmt.Errorf("audit Stack assignment creation: %w", err)
		}
	}
	return assignment, nil
}

func (s *Service) UpdateAssignmentUsage(ctx context.Context, input UpdateAssignmentUsageInput) (Assignment, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.UsageState = strings.ToLower(strings.TrimSpace(input.UsageState))
	lastUsedAt, ok := optionalInstant(input.LastUsedAt)
	if !stableIDPattern.MatchString(input.ID) || !hasString(usageStates, input.UsageState) || input.Revision < 1 || !ok {
		return Assignment{}, ErrInvalidInput
	}
	existing, err := s.store.GetAssignment(ctx, s.organizationID, input.ID)
	if err != nil {
		return Assignment{}, err
	}
	if existing.Revision != input.Revision || existing.EndedAt != nil || (lastUsedAt != nil && lastUsedAt.Before(existing.AssignedAt)) {
		return Assignment{}, ErrConflict
	}
	updated := existing
	updated.UsageState = input.UsageState
	updated.LastUsedAt = lastUsedAt
	updated.Revision++
	updated.UpdatedAt = s.now().UTC()
	updated, err = s.store.UpdateAssignment(ctx, updated, existing.Revision)
	if err != nil {
		return Assignment{}, err
	}
	if err := s.audit(ctx, "stack.assignment.usage_updated", "software_license_assignment", updated.ID, map[string]string{
		"usageState": updated.UsageState, "revision": strconv.FormatInt(updated.Revision, 10),
	}); err != nil {
		return Assignment{}, fmt.Errorf("audit Stack assignment usage: %w", err)
	}
	return updated, nil
}

func (s *Service) UpdateProductStatus(ctx context.Context, input UpdateProductStatusInput) (Product, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	if !stableIDPattern.MatchString(input.ID) || !hasString(productStatuses, input.Status) || input.Revision < 1 {
		return Product{}, ErrInvalidInput
	}
	existing, err := s.store.GetProduct(ctx, s.organizationID, input.ID)
	if err != nil {
		return Product{}, err
	}
	if existing.Revision != input.Revision || existing.Status == "retired" {
		return Product{}, ErrConflict
	}
	updated := existing
	updated.Status = input.Status
	updated.Revision++
	updated.UpdatedAt = s.now().UTC()
	updated, err = s.store.UpdateProduct(ctx, updated, existing.Revision)
	if err != nil {
		return Product{}, err
	}
	if err := s.audit(ctx, "stack.product.status_updated", "software_product", updated.ID, map[string]string{"status": updated.Status, "revision": strconv.FormatInt(updated.Revision, 10)}); err != nil {
		return Product{}, fmt.Errorf("audit Stack product status: %w", err)
	}
	return updated, nil
}

func (s *Service) UpdateVersionStatus(ctx context.Context, input UpdateVersionStatusInput) (Version, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	if !stableIDPattern.MatchString(input.ID) || !hasString(versionStatuses, input.Status) || input.Revision < 1 {
		return Version{}, ErrInvalidInput
	}
	existing, err := s.store.GetVersion(ctx, s.organizationID, input.ID)
	if err != nil {
		return Version{}, err
	}
	if existing.Revision != input.Revision || existing.Status == "retired" {
		return Version{}, ErrConflict
	}
	updated := existing
	updated.Status = input.Status
	updated.Revision++
	updated.UpdatedAt = s.now().UTC()
	updated, err = s.store.UpdateVersion(ctx, updated, existing.Revision)
	if err != nil {
		return Version{}, err
	}
	if err := s.audit(ctx, "stack.version.status_updated", "software_version", updated.ID, map[string]string{"status": updated.Status, "revision": strconv.FormatInt(updated.Revision, 10)}); err != nil {
		return Version{}, fmt.Errorf("audit Stack version status: %w", err)
	}
	return updated, nil
}

func (s *Service) UpdateInstallationState(ctx context.Context, input UpdateInstallationStateInput) (Installation, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	input.UsageState = strings.ToLower(strings.TrimSpace(input.UsageState))
	lastUsedAt, lastUsedOK := optionalInstant(input.LastUsedAt)
	removedAt, removedOK := optionalInstant(input.RemovedAt)
	if !stableIDPattern.MatchString(input.ID) || !hasString(installationStatuses, input.Status) || !hasString(usageStates, input.UsageState) ||
		input.Revision < 1 || !lastUsedOK || !removedOK || (input.Status == "installed" && removedAt != nil) || (input.Status == "removed" && removedAt == nil) ||
		(lastUsedAt != nil && removedAt != nil && lastUsedAt.After(*removedAt)) {
		return Installation{}, ErrInvalidInput
	}
	existing, err := s.store.GetInstallation(ctx, s.organizationID, input.ID)
	if err != nil {
		return Installation{}, err
	}
	if existing.Revision != input.Revision || existing.Status == "removed" || (lastUsedAt != nil && lastUsedAt.Before(existing.InstalledAt)) || (removedAt != nil && removedAt.Before(existing.InstalledAt)) {
		return Installation{}, ErrConflict
	}
	updated := existing
	updated.Status, updated.UsageState, updated.LastUsedAt, updated.RemovedAt = input.Status, input.UsageState, lastUsedAt, removedAt
	updated.Revision++
	updated.UpdatedAt = s.now().UTC()
	updated, err = s.store.UpdateInstallation(ctx, updated, existing.Revision)
	if err != nil {
		return Installation{}, err
	}
	if err := s.audit(ctx, "stack.installation.state_updated", "software_installation", updated.ID, map[string]string{"status": updated.Status, "usageState": updated.UsageState, "revision": strconv.FormatInt(updated.Revision, 10)}); err != nil {
		return Installation{}, fmt.Errorf("audit Stack installation state: %w", err)
	}
	return updated, nil
}

func (s *Service) UpdateLicenseEntitlement(ctx context.Context, input UpdateLicenseEntitlementInput) (License, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	startsOn, startsOK := optionalDate(input.StartsOn)
	expiresOn, expiresOK := optionalDate(input.ExpiresOn)
	if !stableIDPattern.MatchString(input.ID) || input.Quantity < 1 || input.Quantity > 1_000_000_000 || !hasString(licenseStatuses, input.Status) ||
		input.Revision < 1 || !startsOK || !expiresOK || (startsOn != nil && expiresOn != nil && expiresOn.Before(*startsOn)) {
		return License{}, ErrInvalidInput
	}
	existing, err := s.store.GetLicense(ctx, s.organizationID, input.ID)
	if err != nil {
		return License{}, err
	}
	if existing.Revision != input.Revision || existing.Status == "retired" {
		return License{}, ErrConflict
	}
	updated := existing
	updated.Quantity, updated.Status, updated.StartsOn, updated.ExpiresOn = input.Quantity, input.Status, startsOn, expiresOn
	updated.Revision++
	updated.UpdatedAt = s.now().UTC()
	updated, err = s.store.UpdateLicense(ctx, updated, existing.Revision)
	if err != nil {
		return License{}, err
	}
	if err := s.audit(ctx, "stack.license.entitlement_updated", "software_license", updated.ID, map[string]string{"status": updated.Status, "quantity": strconv.FormatInt(updated.Quantity, 10), "revision": strconv.FormatInt(updated.Revision, 10)}); err != nil {
		return License{}, fmt.Errorf("audit Stack license entitlement: %w", err)
	}
	return updated, nil
}

func (s *Service) EndAssignment(ctx context.Context, input EndAssignmentInput) (Assignment, error) {
	input.ID = strings.TrimSpace(input.ID)
	endedAt, ok := requiredInstant(input.EndedAt)
	if !stableIDPattern.MatchString(input.ID) || input.Revision < 1 || !ok {
		return Assignment{}, ErrInvalidInput
	}
	existing, err := s.store.GetAssignment(ctx, s.organizationID, input.ID)
	if err != nil {
		return Assignment{}, err
	}
	if existing.Revision != input.Revision || existing.EndedAt != nil || endedAt.Before(existing.AssignedAt) ||
		(existing.LastUsedAt != nil && existing.LastUsedAt.After(endedAt)) {
		return Assignment{}, ErrConflict
	}
	updated := existing
	updated.EndedAt = &endedAt
	updated.Revision++
	updated.UpdatedAt = s.now().UTC()
	updated, err = s.store.UpdateAssignment(ctx, updated, existing.Revision)
	if err != nil {
		return Assignment{}, err
	}
	if err := s.audit(ctx, "stack.assignment.ended", "software_license_assignment", updated.ID, map[string]string{"revision": strconv.FormatInt(updated.Revision, 10)}); err != nil {
		return Assignment{}, fmt.Errorf("audit Stack assignment end: %w", err)
	}
	return updated, nil
}

func (s *Service) Analytics(ctx context.Context, asOf time.Time, expiringWithinDays int64) (Analytics, error) {
	if asOf.IsZero() {
		asOf = s.now()
	}
	asOf = asOf.UTC()
	if asOf.Year() < 1970 || asOf.Year() > 9999 || expiringWithinDays < 0 || expiringWithinDays > 3660 {
		return Analytics{}, ErrInvalidInput
	}
	if expiringWithinDays == 0 {
		expiringWithinDays = 90
	}
	snapshot, err := s.store.Snapshot(ctx, s.organizationID)
	if err != nil {
		return Analytics{}, err
	}
	assetContexts := make(map[string]AssetContext)
	for _, installation := range snapshot.Installations {
		if !installationActiveAt(installation, asOf) {
			continue
		}
		assetContext, resolveErr := s.references.ResolveAsset(ctx, installation.AssetID)
		if resolveErr != nil {
			return Analytics{}, resolveErr
		}
		if assetContext.ID != installation.AssetID {
			return Analytics{}, ErrReferenceMissing
		}
		assetContexts[installation.AssetID] = assetContext
	}
	report := buildAnalytics(snapshot, assetContexts, asOf, expiringWithinDays)
	return report, nil
}

func buildAnalytics(snapshot Snapshot, assetContexts map[string]AssetContext, asOf time.Time, expiringWithinDays int64) Analytics {
	report := Analytics{
		AsOf:                 asOf,
		ExpiringWithinDays:   expiringWithinDays,
		Products:             len(snapshot.Products),
		ComplianceConditions: []Condition{},
	}
	versions := make(map[string]Version, len(snapshot.Versions))
	for _, version := range snapshot.Versions {
		versions[version.ID] = version
	}
	licensesByProduct := make(map[string][]License)
	activeLicenses := make(map[string]License)
	for _, license := range snapshot.Licenses {
		licensesByProduct[license.ProductID] = append(licensesByProduct[license.ProductID], license)
		if licenseActiveAt(license, asOf) {
			activeLicenses[license.ID] = license
			report.ActiveLicenses++
			report.EntitledQuantity += license.Quantity
		}
		if license.Status == "retired" {
			continue
		}
		if license.ExpiresOn != nil {
			days := wholeDays(asOf, *license.ExpiresOn)
			if days < 0 || license.Status == "expired" {
				report.ComplianceConditions = append(report.ComplianceConditions, Condition{
					Code: "expired", Severity: "critical", ProductID: license.ProductID, VersionID: license.VersionID,
					LicenseID: license.ID, DaysUntilExpiry: days, HumanReadableState: "License expired; assignments no longer establish coverage.",
				})
			} else if license.Status == "active" && days <= expiringWithinDays {
				report.ComplianceConditions = append(report.ComplianceConditions, Condition{
					Code: "expiring", Severity: "warning", ProductID: license.ProductID, VersionID: license.VersionID,
					LicenseID: license.ID, DaysUntilExpiry: days, HumanReadableState: fmt.Sprintf("License expires in %d days.", days),
				})
			}
		} else if license.Status == "expired" {
			report.ComplianceConditions = append(report.ComplianceConditions, Condition{
				Code: "expired", Severity: "critical", ProductID: license.ProductID, VersionID: license.VersionID,
				LicenseID: license.ID, HumanReadableState: "License is marked expired; assignments no longer establish coverage.",
			})
		}
	}
	assignedByLicense := make(map[string]int64)
	underusedByLicense := make(map[string]int64)
	assignmentsByLicense := make(map[string][]Assignment)
	for _, assignment := range snapshot.Assignments {
		if !assignmentActiveAt(assignment, asOf) {
			continue
		}
		license, active := activeLicenses[assignment.LicenseID]
		if !active {
			continue
		}
		assignedByLicense[assignment.LicenseID] += assignment.Seats
		report.AssignedQuantity += assignment.Seats
		if assignment.UsageState == "unused" {
			underusedByLicense[assignment.LicenseID] += assignment.Seats
			report.UnderusedAssignments++
		}
		assignmentsByLicense[license.ID] = append(assignmentsByLicense[license.ID], assignment)
	}
	for licenseID, assigned := range assignedByLicense {
		license := activeLicenses[licenseID]
		if assigned > license.Quantity {
			report.ComplianceConditions = append(report.ComplianceConditions, Condition{
				Code: "over_assigned", Severity: "critical", ProductID: license.ProductID, VersionID: license.VersionID,
				LicenseID: license.ID, EntitledQuantity: license.Quantity, AssignedQuantity: assigned,
				HumanReadableState: fmt.Sprintf("%d seats are assigned against %d entitled seats.", assigned, license.Quantity),
			})
		}
		if underused := underusedByLicense[licenseID]; underused > 0 {
			report.ComplianceConditions = append(report.ComplianceConditions, Condition{
				Code: "under_used", Severity: "info", ProductID: license.ProductID, VersionID: license.VersionID,
				LicenseID: license.ID, EntitledQuantity: license.Quantity, AssignedQuantity: assigned, UnderusedQuantity: underused,
				HumanReadableState: fmt.Sprintf("%d assigned seats are marked unused.", underused),
			})
		}
	}
	for _, installation := range snapshot.Installations {
		if !installationActiveAt(installation, asOf) {
			continue
		}
		report.ActiveInstallations++
		version, ok := versions[installation.VersionID]
		if !ok {
			continue
		}
		covered := false
		assetContext := assetContexts[installation.AssetID]
		for _, license := range licensesByProduct[version.ProductID] {
			if !licenseActiveAt(license, asOf) || (license.VersionID != "" && license.VersionID != version.ID) {
				continue
			}
			if license.EntitlementMetric == "enterprise" || assignmentCoversAsset(assignmentsByLicense[license.ID], installation.AssetID, assetContext) {
				covered = true
				break
			}
		}
		if !covered {
			report.ComplianceConditions = append(report.ComplianceConditions, Condition{
				Code: "missing_license", Severity: "critical", ProductID: version.ProductID, VersionID: version.ID,
				AssetID: installation.AssetID, HumanReadableState: "Installed software has no active matching asset, identity, department, site, or enterprise entitlement.",
			})
		}
	}
	sort.Slice(report.ComplianceConditions, func(i, j int) bool {
		left, right := report.ComplianceConditions[i], report.ComplianceConditions[j]
		return strings.Join([]string{left.Code, left.ProductID, left.LicenseID, left.AssetID}, "\x00") <
			strings.Join([]string{right.Code, right.ProductID, right.LicenseID, right.AssetID}, "\x00")
	})
	return report
}

func assignmentCoversAsset(assignments []Assignment, assetID string, asset AssetContext) bool {
	for _, assignment := range assignments {
		switch assignment.AssigneeKind {
		case "asset":
			if assignment.AssigneeID == assetID {
				return true
			}
		case "identity":
			if asset.IdentityID != "" && assignment.AssigneeID == asset.IdentityID {
				return true
			}
		case "department":
			if asset.DepartmentID != "" && assignment.AssigneeID == asset.DepartmentID {
				return true
			}
		case "site":
			if asset.SiteID != "" && assignment.AssigneeID == asset.SiteID {
				return true
			}
		}
	}
	return false
}

func installationActiveAt(installation Installation, asOf time.Time) bool {
	return !installation.InstalledAt.After(asOf) && (installation.RemovedAt == nil || installation.RemovedAt.After(asOf))
}

func assignmentActiveAt(assignment Assignment, asOf time.Time) bool {
	return !assignment.AssignedAt.After(asOf) && (assignment.EndedAt == nil || assignment.EndedAt.After(asOf))
}

func assignmentKindMatchesMetric(metric, kind string) bool {
	switch metric {
	case "device":
		return kind == "asset"
	case "user":
		return kind == "identity"
	case "site":
		return kind == "site"
	case "concurrent", "enterprise":
		return hasString(assigneeKinds, kind)
	default:
		return false
	}
}

func (s *Service) ExportRecords(ctx context.Context) ([]ExchangeRecord, error) {
	snapshot, err := s.store.Snapshot(ctx, s.organizationID)
	if err != nil {
		return nil, err
	}
	if len(snapshot.Products)+len(snapshot.Versions)+len(snapshot.Installations)+len(snapshot.Licenses)+len(snapshot.Assignments) > 100_000 {
		return nil, ErrInvalidInput
	}
	records := make([]ExchangeRecord, 0, len(snapshot.Products)+len(snapshot.Versions)+len(snapshot.Installations)+len(snapshot.Licenses)+len(snapshot.Assignments))
	appendRecord := func(recordType, id string, revision int64, dependencies []string, value any) error {
		payload, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			return marshalErr
		}
		if len(payload) > 1<<20 {
			return ErrInvalidInput
		}
		if dependencies == nil {
			dependencies = []string{}
		}
		sourceSystemID, sourceRecordID, ok := portableProvenance(value)
		if !ok {
			return ErrInvalidInput
		}
		records = append(records, ExchangeRecord{
			Type: recordType, ID: id, Revision: revision, Dependencies: dependencies,
			SourceSystemID: sourceSystemID, SourceRecordID: sourceRecordID, Payload: payload,
		})
		return nil
	}
	for _, product := range snapshot.Products {
		if err := appendRecord("stack.product", product.ID, product.Revision, nil, product); err != nil {
			return nil, err
		}
	}
	for _, version := range snapshot.Versions {
		if err := appendRecord("stack.version", version.ID, version.Revision, []string{"stack.product:" + version.ProductID}, version); err != nil {
			return nil, err
		}
	}
	for _, license := range snapshot.Licenses {
		dependencies := portableLicenseDependencies(license)
		if err := appendRecord("stack.license", license.ID, license.Revision, dependencies, license); err != nil {
			return nil, err
		}
	}
	for _, installation := range snapshot.Installations {
		if err := appendRecord("stack.installation", installation.ID, installation.Revision, []string{"stack.version:" + installation.VersionID, "atlas.asset:" + installation.AssetID}, installation); err != nil {
			return nil, err
		}
	}
	for _, assignment := range snapshot.Assignments {
		if err := appendRecord("stack.assignment", assignment.ID, assignment.Revision, portableAssignmentDependencies(assignment), assignment); err != nil {
			return nil, err
		}
	}
	return records, nil
}

func (s *Service) ImportRecords(ctx context.Context, sourceSystemID string, records []ExchangeRecord) (ImportResult, error) {
	sourceSystemID = strings.ToLower(strings.TrimSpace(sourceSystemID))
	if !stableIDPattern.MatchString(sourceSystemID) || len(records) == 0 || len(records) > 10_000 {
		return ImportResult{}, ErrInvalidInput
	}
	order := map[string]int{"stack.product": 0, "stack.version": 1, "stack.license": 2, "stack.installation": 3, "stack.assignment": 4}
	prepared := make([]portableRecord, 0, len(records))
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		if _, ok := order[record.Type]; !ok || !stableIDPattern.MatchString(record.ID) || record.Revision < 1 || record.Dependencies == nil ||
			len(record.Payload) == 0 || len(record.Payload) > 1<<20 || !validSource(record.SourceSystemID, record.SourceRecordID) {
			return ImportResult{}, ErrInvalidInput
		}
		key := record.Type + "\x00" + record.ID
		if _, duplicate := seen[key]; duplicate {
			return ImportResult{}, ErrInvalidInput
		}
		seen[key] = struct{}{}
		value, err := preparePortableRecord(record, sourceSystemID)
		if err != nil {
			return ImportResult{}, err
		}
		prepared = append(prepared, value)
	}
	sort.SliceStable(prepared, func(i, j int) bool { return order[prepared[i].record.Type] < order[prepared[j].record.Type] })
	result := ImportResult{}
	for _, preparedRecord := range prepared {
		record := preparedRecord.record
		existed, err := s.recordExists(ctx, record.Type, record.ID)
		if err != nil {
			return result, err
		}
		switch record.Type {
		case "stack.product":
			value := preparedRecord.value.(Product)
			_, err = s.CreateProduct(ctx, CreateProductInput{ID: value.ID, Name: value.Name, Publisher: value.Publisher, Category: value.Category, Status: value.Status, SourceSystemID: preparedRecord.sourceSystemID, SourceRecordID: preparedRecord.sourceRecordID})
		case "stack.version":
			value := preparedRecord.value.(Version)
			_, err = s.CreateVersion(ctx, CreateVersionInput{ID: value.ID, ProductID: value.ProductID, Name: value.Name, ReleasedOn: value.ReleasedOn, Status: value.Status, SourceSystemID: preparedRecord.sourceSystemID, SourceRecordID: preparedRecord.sourceRecordID})
		case "stack.license":
			value := preparedRecord.value.(License)
			_, err = s.CreateLicense(ctx, CreateLicenseInput{ID: value.ID, ProductID: value.ProductID, VersionID: value.VersionID, Name: value.Name, EntitlementMetric: value.EntitlementMetric, Quantity: value.Quantity, Status: value.Status, StartsOn: value.StartsOn, ExpiresOn: value.ExpiresOn, VendorID: value.VendorID, PurchaseOrderID: value.PurchaseOrderID, ContractID: value.ContractID, CostRecordID: value.CostRecordID, DocumentIDs: value.DocumentIDs, SourceSystemID: preparedRecord.sourceSystemID, SourceRecordID: preparedRecord.sourceRecordID})
		case "stack.installation":
			value := preparedRecord.value.(Installation)
			_, err = s.RecordInstallation(ctx, RecordInstallationInput{ID: value.ID, VersionID: value.VersionID, AssetID: value.AssetID, Status: value.Status, UsageState: value.UsageState, InstalledAt: value.InstalledAt, LastUsedAt: value.LastUsedAt, RemovedAt: value.RemovedAt, SourceSystemID: preparedRecord.sourceSystemID, SourceRecordID: preparedRecord.sourceRecordID})
		case "stack.assignment":
			value := preparedRecord.value.(Assignment)
			_, err = s.CreateAssignment(ctx, CreateAssignmentInput{ID: value.ID, LicenseID: value.LicenseID, AssigneeKind: value.AssigneeKind, AssigneeID: value.AssigneeID, Seats: value.Seats, UsageState: value.UsageState, AssignedAt: value.AssignedAt, LastUsedAt: value.LastUsedAt, EndedAt: value.EndedAt, SourceSystemID: preparedRecord.sourceSystemID, SourceRecordID: preparedRecord.sourceRecordID})
		}
		if err != nil {
			return result, err
		}
		if existed {
			result.Unchanged++
		} else {
			result.Created++
		}
	}
	return result, nil
}

type portableRecord struct {
	record         ExchangeRecord
	value          any
	sourceSystemID string
	sourceRecordID string
}

func preparePortableRecord(record ExchangeRecord, fallbackSourceSystemID string) (portableRecord, error) {
	var value any
	var dependencies []string
	switch record.Type {
	case "stack.product":
		decoded, err := decodePortablePayload[Product](record.Payload)
		if err != nil {
			return portableRecord{}, err
		}
		value = decoded
	case "stack.version":
		decoded, err := decodePortablePayload[Version](record.Payload)
		if err != nil {
			return portableRecord{}, err
		}
		value, dependencies = decoded, []string{"stack.product:" + decoded.ProductID}
	case "stack.license":
		decoded, err := decodePortablePayload[License](record.Payload)
		if err != nil {
			return portableRecord{}, err
		}
		dependencies = portableLicenseDependencies(decoded)
		value = decoded
	case "stack.installation":
		decoded, err := decodePortablePayload[Installation](record.Payload)
		if err != nil {
			return portableRecord{}, err
		}
		value, dependencies = decoded, []string{"stack.version:" + decoded.VersionID, "atlas.asset:" + decoded.AssetID}
	case "stack.assignment":
		decoded, err := decodePortablePayload[Assignment](record.Payload)
		if err != nil {
			return portableRecord{}, err
		}
		value, dependencies = decoded, portableAssignmentDependencies(decoded)
	default:
		return portableRecord{}, ErrInvalidInput
	}

	id, revision := portableIdentity(value)
	actualDependencies := append([]string(nil), record.Dependencies...)
	sort.Strings(actualDependencies)
	sort.Strings(dependencies)
	if id != record.ID || revision != record.Revision || hasDuplicates(actualDependencies) || !slices.Equal(actualDependencies, dependencies) {
		return portableRecord{}, ErrInvalidInput
	}
	payloadSourceSystemID, payloadSourceRecordID, ok := portableProvenance(value)
	if !ok {
		return portableRecord{}, ErrInvalidInput
	}
	if record.SourceSystemID != "" && payloadSourceSystemID != "" &&
		(record.SourceSystemID != payloadSourceSystemID || record.SourceRecordID != payloadSourceRecordID) {
		return portableRecord{}, ErrInvalidInput
	}
	effectiveSourceSystemID, effectiveSourceRecordID := record.SourceSystemID, record.SourceRecordID
	if effectiveSourceSystemID == "" {
		effectiveSourceSystemID, effectiveSourceRecordID = payloadSourceSystemID, payloadSourceRecordID
	}
	if effectiveSourceSystemID == "" {
		effectiveSourceSystemID, effectiveSourceRecordID = fallbackSourceSystemID, record.Type+":"+record.ID
	}
	if !validSource(effectiveSourceSystemID, effectiveSourceRecordID) {
		return portableRecord{}, ErrInvalidInput
	}
	return portableRecord{
		record: record, value: value, sourceSystemID: effectiveSourceSystemID, sourceRecordID: effectiveSourceRecordID,
	}, nil
}

func portableLicenseDependencies(value License) []string {
	dependencies := []string{"stack.product:" + value.ProductID}
	for _, optional := range []struct{ recordType, id string }{
		{"stack.version", value.VersionID},
		{"ledger.vendor", value.VendorID},
		{"ledger.purchase_order", value.PurchaseOrderID},
		{"ledger.contract", value.ContractID},
		{"ledger.cost", value.CostRecordID},
	} {
		if optional.id != "" {
			dependencies = append(dependencies, optional.recordType+":"+optional.id)
		}
	}
	for _, documentID := range value.DocumentIDs {
		dependencies = append(dependencies, "vault.blob:"+documentID)
	}
	sort.Strings(dependencies)
	return dependencies
}

func portableAssignmentDependencies(value Assignment) []string {
	recordType := map[string]string{
		"asset": "atlas.asset", "identity": "people.identity", "department": "people.department", "site": "people.site",
	}[value.AssigneeKind]
	dependencies := []string{"stack.license:" + value.LicenseID, recordType + ":" + value.AssigneeID}
	sort.Strings(dependencies)
	return dependencies
}

// ExchangeDependencyExists verifies a canonical cross-domain relationship
// through Stack's existing read-only validators. It never creates or mutates
// the dependency and reports unhandled types explicitly.
func (s *Service) ExchangeDependencyExists(ctx context.Context, recordType, id string) (handled bool, exists bool, err error) {
	if !stableIDPattern.MatchString(id) {
		return true, false, ErrInvalidInput
	}
	switch recordType {
	case "atlas.asset":
		asset, err := s.references.ResolveAsset(ctx, id)
		if err == nil {
			return true, asset.ID == id, nil
		}
		return exchangeDependencyResult(err)
	case "people.identity":
		err = s.references.ValidateAssignee(ctx, "identity", id)
	case "people.department":
		err = s.references.ValidateAssignee(ctx, "department", id)
	case "people.site":
		err = s.references.ValidateAssignee(ctx, "site", id)
	case "ledger.vendor":
		err = s.references.ValidateFinancialReferences(ctx, id, "", "", "")
	case "ledger.purchase_order":
		err = s.references.ValidateFinancialReferences(ctx, "", id, "", "")
	case "ledger.contract":
		err = s.references.ValidateFinancialReferences(ctx, "", "", id, "")
	case "ledger.cost":
		err = s.references.ValidateFinancialReferences(ctx, "", "", "", id)
	case "vault.blob":
		err = s.references.ValidateDocuments(ctx, []string{id})
	default:
		return false, false, nil
	}
	return exchangeDependencyResult(err)
}

func exchangeDependencyResult(err error) (bool, bool, error) {
	switch {
	case err == nil:
		return true, true, nil
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrReferenceMissing):
		return true, false, nil
	default:
		return true, false, err
	}
}

func portableProvenance(value any) (string, string, bool) {
	var sourceSystemID, sourceRecordID string
	switch typed := value.(type) {
	case Product:
		sourceSystemID, sourceRecordID = typed.SourceSystemID, typed.SourceRecordID
	case Version:
		sourceSystemID, sourceRecordID = typed.SourceSystemID, typed.SourceRecordID
	case License:
		sourceSystemID, sourceRecordID = typed.SourceSystemID, typed.SourceRecordID
	case Installation:
		sourceSystemID, sourceRecordID = typed.SourceSystemID, typed.SourceRecordID
	case Assignment:
		sourceSystemID, sourceRecordID = typed.SourceSystemID, typed.SourceRecordID
	default:
		return "", "", false
	}
	return sourceSystemID, sourceRecordID, validSource(sourceSystemID, sourceRecordID)
}

func decodePortablePayload[T any](payload json.RawMessage) (T, error) {
	var value T
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return value, ErrInvalidInput
	}
	return value, nil
}

func portableIdentity(value any) (string, int64) {
	switch typed := value.(type) {
	case Product:
		return typed.ID, typed.Revision
	case Version:
		return typed.ID, typed.Revision
	case License:
		return typed.ID, typed.Revision
	case Installation:
		return typed.ID, typed.Revision
	case Assignment:
		return typed.ID, typed.Revision
	default:
		return "", 0
	}
}

func (s *Service) recordExists(ctx context.Context, recordType, id string) (bool, error) {
	var err error
	switch recordType {
	case "stack.product":
		_, err = s.store.GetProduct(ctx, s.organizationID, id)
	case "stack.version":
		_, err = s.store.GetVersion(ctx, s.organizationID, id)
	case "stack.license":
		_, err = s.store.GetLicense(ctx, s.organizationID, id)
	case "stack.installation":
		_, err = s.store.GetInstallation(ctx, s.organizationID, id)
	case "stack.assignment":
		_, err = s.store.GetAssignment(ctx, s.organizationID, id)
	default:
		return false, ErrInvalidInput
	}
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func licenseActiveAt(license License, asOf time.Time) bool {
	if license.Status != "active" || (license.StartsOn != nil && license.StartsOn.After(asOf)) ||
		(license.ExpiresOn != nil && wholeDays(asOf, *license.ExpiresOn) < 0) {
		return false
	}
	return true
}

func wholeDays(from, to time.Time) int64 {
	from = time.Date(from.UTC().Year(), from.UTC().Month(), from.UTC().Day(), 0, 0, 0, 0, time.UTC)
	to = time.Date(to.UTC().Year(), to.UTC().Month(), to.UTC().Day(), 0, 0, 0, 0, time.UTC)
	return int64(to.Sub(from) / (24 * time.Hour))
}

func (s *Service) newID(value string) (string, error) {
	if value != "" {
		return value, nil
	}
	id, err := foundation.NewCorrelationID()
	if err != nil {
		return "", fmt.Errorf("create Stack id: %w", err)
	}
	return id, nil
}

func optionalID(value string) bool       { return value == "" || stableIDPattern.MatchString(value) }
func optionalStableID(value string) bool { return value == "" || stableIDPattern.MatchString(value) }
func allOptionalIDs(values ...string) bool {
	for _, value := range values {
		if !optionalStableID(value) {
			return false
		}
	}
	return true
}
func allStableIDs(values []string) bool {
	for _, value := range values {
		if !stableIDPattern.MatchString(value) {
			return false
		}
	}
	return true
}
func validSource(systemID, recordID string) bool {
	if systemID == "" && recordID == "" {
		return true
	}
	return stableIDPattern.MatchString(systemID) && sourceRecordIDPattern.MatchString(recordID)
}
func validText(value string, maximum int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= maximum
}
func validTextRange(value string, minimum, maximum int) bool {
	length := utf8.RuneCountInString(value)
	return utf8.ValidString(value) && length >= minimum && length <= maximum
}
func stringSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
func hasString(values map[string]struct{}, value string) bool { _, ok := values[value]; return ok }
func normalizeIDs(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
func hasDuplicates(values []string) bool {
	for i := 1; i < len(values); i++ {
		if values[i] == values[i-1] {
			return true
		}
	}
	return false
}
func optionalDate(value *time.Time) (*time.Time, bool) {
	if value == nil {
		return nil, true
	}
	date := value.UTC()
	if date.IsZero() || date.Year() < 1970 || date.Year() > 9999 {
		return nil, false
	}
	normalized := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	return &normalized, true
}
func requiredInstant(value time.Time) (time.Time, bool) {
	value = value.UTC()
	return value, !value.IsZero() && value.Year() >= 1970 && value.Year() <= 9999
}
func optionalInstant(value *time.Time) (*time.Time, bool) {
	if value == nil {
		return nil, true
	}
	normalized, ok := requiredInstant(*value)
	if !ok {
		return nil, false
	}
	return &normalized, true
}
func referenceError(err error) error {
	if errors.Is(err, ErrNotFound) {
		return ErrReferenceMissing
	}
	return err
}

func actorFromContext(ctx context.Context) string {
	if scope, ok := foundation.ScopeFromContext(ctx); ok && strings.TrimSpace(scope.ActorID) != "" {
		return scope.ActorID
	}
	return "system:stack"
}

func (s *Service) audit(ctx context.Context, action, resourceType, resourceID string, metadata map[string]string) error {
	scope, ok := foundation.ScopeFromContext(ctx)
	if !ok || scope.CorrelationID == "" {
		correlationID, err := foundation.NewCorrelationID()
		if err != nil {
			return err
		}
		scope = foundation.Scope{OrganizationID: s.organizationID, ActorID: actorFromContext(ctx), CorrelationID: correlationID}
		ctx = foundation.WithScope(ctx, scope)
	}
	if metadata == nil {
		metadata = make(map[string]string)
	}
	metadata["requirementId"] = RequirementID
	eventID, err := foundation.NewCorrelationID()
	if err != nil {
		return err
	}
	return s.auditor.Record(ctx, foundation.AuditEvent{ID: eventID, OrganizationID: s.organizationID,
		ActorID: actorFromContext(ctx), CorrelationID: scope.CorrelationID, Action: action,
		ResourceType: resourceType, ResourceID: resourceID, OccurredAt: s.now().UTC(), Metadata: metadata})
}
