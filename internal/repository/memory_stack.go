package repository

// Requirement: REQ-STACK-001. Feature: software.licenses.

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/maxlemke/stewardmesh/internal/stack"
)

type MemoryStackStore struct {
	mu            sync.RWMutex
	products      map[string]stack.Product
	versions      map[string]stack.Version
	installations map[string]stack.Installation
	licenses      map[string]stack.License
	assignments   map[string]stack.Assignment
	sources       map[string]string
}

func NewMemoryStackStore() *MemoryStackStore {
	return &MemoryStackStore{
		products: make(map[string]stack.Product), versions: make(map[string]stack.Version),
		installations: make(map[string]stack.Installation), licenses: make(map[string]stack.License),
		assignments: make(map[string]stack.Assignment), sources: make(map[string]string),
	}
}

func stackKey(organizationID, id string) string { return organizationID + "\x00" + id }
func stackSourceKey(kind, organizationID, systemID, recordID string) string {
	if systemID == "" {
		return ""
	}
	return kind + "\x00" + organizationID + "\x00" + strings.ToLower(systemID) + "\x00" + recordID
}

func (s *MemoryStackStore) Snapshot(_ context.Context, organizationID string) (stack.Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := stack.Snapshot{
		Products: []stack.Product{}, Versions: []stack.Version{}, Installations: []stack.Installation{},
		Licenses: []stack.License{}, Assignments: []stack.Assignment{},
	}
	for _, value := range s.products {
		if value.OrganizationID == organizationID {
			result.Products = append(result.Products, value)
		}
	}
	for _, value := range s.versions {
		if value.OrganizationID == organizationID {
			result.Versions = append(result.Versions, cloneStackVersion(value))
		}
	}
	for _, value := range s.installations {
		if value.OrganizationID == organizationID {
			result.Installations = append(result.Installations, cloneStackInstallation(value))
		}
	}
	for _, value := range s.licenses {
		if value.OrganizationID == organizationID {
			result.Licenses = append(result.Licenses, cloneStackLicense(value))
		}
	}
	for _, value := range s.assignments {
		if value.OrganizationID == organizationID {
			result.Assignments = append(result.Assignments, cloneStackAssignment(value))
		}
	}
	sort.Slice(result.Products, func(i, j int) bool { return result.Products[i].ID < result.Products[j].ID })
	sort.Slice(result.Versions, func(i, j int) bool { return result.Versions[i].ID < result.Versions[j].ID })
	sort.Slice(result.Installations, func(i, j int) bool { return result.Installations[i].ID < result.Installations[j].ID })
	sort.Slice(result.Licenses, func(i, j int) bool { return result.Licenses[i].ID < result.Licenses[j].ID })
	sort.Slice(result.Assignments, func(i, j int) bool { return result.Assignments[i].ID < result.Assignments[j].ID })
	return result, nil
}

func (s *MemoryStackStore) GetProduct(_ context.Context, organizationID, id string) (stack.Product, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.products[stackKey(organizationID, id)]
	if !ok {
		return stack.Product{}, stack.ErrNotFound
	}
	return value, nil
}
func (s *MemoryStackStore) GetVersion(_ context.Context, organizationID, id string) (stack.Version, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.versions[stackKey(organizationID, id)]
	if !ok {
		return stack.Version{}, stack.ErrNotFound
	}
	return cloneStackVersion(value), nil
}
func (s *MemoryStackStore) GetInstallation(_ context.Context, organizationID, id string) (stack.Installation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.installations[stackKey(organizationID, id)]
	if !ok {
		return stack.Installation{}, stack.ErrNotFound
	}
	return cloneStackInstallation(value), nil
}
func (s *MemoryStackStore) GetLicense(_ context.Context, organizationID, id string) (stack.License, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.licenses[stackKey(organizationID, id)]
	if !ok {
		return stack.License{}, stack.ErrNotFound
	}
	return cloneStackLicense(value), nil
}
func (s *MemoryStackStore) GetAssignment(_ context.Context, organizationID, id string) (stack.Assignment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.assignments[stackKey(organizationID, id)]
	if !ok {
		return stack.Assignment{}, stack.ErrNotFound
	}
	return cloneStackAssignment(value), nil
}

func (s *MemoryStackStore) CreateProduct(_ context.Context, value stack.Product) (stack.Product, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.products {
		if existing.OrganizationID == value.OrganizationID && strings.EqualFold(existing.Publisher, value.Publisher) && strings.EqualFold(existing.Name, value.Name) && existing.ID != value.ID {
			return stack.Product{}, false, stack.ErrConflict
		}
	}
	return createMemoryStack(s.products, s.sources, "product", value.OrganizationID, value.ID, value.SourceSystemID, value.SourceRecordID, value, equalStackProduct)
}
func (s *MemoryStackStore) CreateVersion(_ context.Context, value stack.Version) (stack.Version, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.versions {
		if existing.OrganizationID == value.OrganizationID && existing.ProductID == value.ProductID && strings.EqualFold(existing.Name, value.Name) && existing.ID != value.ID {
			return stack.Version{}, false, stack.ErrConflict
		}
	}
	return createMemoryStack(s.versions, s.sources, "version", value.OrganizationID, value.ID, value.SourceSystemID, value.SourceRecordID, cloneStackVersion(value), equalStackVersion)
}
func (s *MemoryStackStore) CreateInstallation(_ context.Context, value stack.Installation) (stack.Installation, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.installations {
		if existing.OrganizationID == value.OrganizationID && existing.VersionID == value.VersionID && existing.AssetID == value.AssetID && existing.Status == "installed" && value.Status == "installed" && existing.ID != value.ID {
			return stack.Installation{}, false, stack.ErrConflict
		}
	}
	created, ok, err := createMemoryStack(s.installations, s.sources, "installation", value.OrganizationID, value.ID, value.SourceSystemID, value.SourceRecordID, cloneStackInstallation(value), equalStackInstallation)
	return cloneStackInstallation(created), ok, err
}
func (s *MemoryStackStore) CreateLicense(_ context.Context, value stack.License) (stack.License, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	created, ok, err := createMemoryStack(s.licenses, s.sources, "license", value.OrganizationID, value.ID, value.SourceSystemID, value.SourceRecordID, cloneStackLicense(value), equalStackLicense)
	return cloneStackLicense(created), ok, err
}
func (s *MemoryStackStore) CreateAssignment(_ context.Context, value stack.Assignment) (stack.Assignment, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.assignments {
		if existing.OrganizationID == value.OrganizationID && existing.LicenseID == value.LicenseID && existing.AssigneeKind == value.AssigneeKind && existing.AssigneeID == value.AssigneeID && existing.EndedAt == nil && value.EndedAt == nil && existing.ID != value.ID {
			return stack.Assignment{}, false, stack.ErrConflict
		}
	}
	created, ok, err := createMemoryStack(s.assignments, s.sources, "assignment", value.OrganizationID, value.ID, value.SourceSystemID, value.SourceRecordID, cloneStackAssignment(value), equalStackAssignment)
	return cloneStackAssignment(created), ok, err
}

func (s *MemoryStackStore) UpdateAssignment(_ context.Context, value stack.Assignment, expectedRevision int64) (stack.Assignment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := stackKey(value.OrganizationID, value.ID)
	existing, ok := s.assignments[key]
	if !ok {
		return stack.Assignment{}, stack.ErrNotFound
	}
	if existing.Revision != expectedRevision || value.Revision != expectedRevision+1 || existing.OrganizationID != value.OrganizationID || existing.ID != value.ID || existing.LicenseID != value.LicenseID || existing.AssigneeKind != value.AssigneeKind || existing.AssigneeID != value.AssigneeID || existing.Seats != value.Seats || !existing.AssignedAt.Equal(value.AssignedAt) || existing.SourceSystemID != value.SourceSystemID || existing.SourceRecordID != value.SourceRecordID || !existing.CreatedAt.Equal(value.CreatedAt) {
		return stack.Assignment{}, stack.ErrConflict
	}
	s.assignments[key] = cloneStackAssignment(value)
	return cloneStackAssignment(value), nil
}

func (s *MemoryStackStore) UpdateProduct(_ context.Context, value stack.Product, expectedRevision int64) (stack.Product, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := stackKey(value.OrganizationID, value.ID)
	existing, ok := s.products[key]
	if !ok {
		return stack.Product{}, stack.ErrNotFound
	}
	if existing.Revision != expectedRevision || value.Revision != expectedRevision+1 || existing.OrganizationID != value.OrganizationID || existing.ID != value.ID || existing.Name != value.Name || existing.Publisher != value.Publisher || existing.Category != value.Category || existing.SourceSystemID != value.SourceSystemID || existing.SourceRecordID != value.SourceRecordID || !existing.CreatedAt.Equal(value.CreatedAt) {
		return stack.Product{}, stack.ErrConflict
	}
	s.products[key] = value
	return value, nil
}

func (s *MemoryStackStore) UpdateVersion(_ context.Context, value stack.Version, expectedRevision int64) (stack.Version, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := stackKey(value.OrganizationID, value.ID)
	existing, ok := s.versions[key]
	if !ok {
		return stack.Version{}, stack.ErrNotFound
	}
	if existing.Revision != expectedRevision || value.Revision != expectedRevision+1 || existing.OrganizationID != value.OrganizationID || existing.ID != value.ID || existing.ProductID != value.ProductID || existing.Name != value.Name || !equalStackTime(existing.ReleasedOn, value.ReleasedOn) || existing.SourceSystemID != value.SourceSystemID || existing.SourceRecordID != value.SourceRecordID || !existing.CreatedAt.Equal(value.CreatedAt) {
		return stack.Version{}, stack.ErrConflict
	}
	s.versions[key] = cloneStackVersion(value)
	return cloneStackVersion(value), nil
}

func (s *MemoryStackStore) UpdateInstallation(_ context.Context, value stack.Installation, expectedRevision int64) (stack.Installation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := stackKey(value.OrganizationID, value.ID)
	existing, ok := s.installations[key]
	if !ok {
		return stack.Installation{}, stack.ErrNotFound
	}
	if existing.Revision != expectedRevision || value.Revision != expectedRevision+1 || existing.OrganizationID != value.OrganizationID || existing.ID != value.ID || existing.VersionID != value.VersionID || existing.AssetID != value.AssetID || !existing.InstalledAt.Equal(value.InstalledAt) || existing.SourceSystemID != value.SourceSystemID || existing.SourceRecordID != value.SourceRecordID || !existing.CreatedAt.Equal(value.CreatedAt) {
		return stack.Installation{}, stack.ErrConflict
	}
	s.installations[key] = cloneStackInstallation(value)
	return cloneStackInstallation(value), nil
}

func (s *MemoryStackStore) UpdateLicense(_ context.Context, value stack.License, expectedRevision int64) (stack.License, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := stackKey(value.OrganizationID, value.ID)
	existing, ok := s.licenses[key]
	if !ok {
		return stack.License{}, stack.ErrNotFound
	}
	if existing.Revision != expectedRevision || value.Revision != expectedRevision+1 || existing.OrganizationID != value.OrganizationID || existing.ID != value.ID || existing.ProductID != value.ProductID || existing.VersionID != value.VersionID || existing.Name != value.Name || existing.EntitlementMetric != value.EntitlementMetric || existing.VendorID != value.VendorID || existing.PurchaseOrderID != value.PurchaseOrderID || existing.ContractID != value.ContractID || existing.CostRecordID != value.CostRecordID || !reflect.DeepEqual(existing.DocumentIDs, value.DocumentIDs) || existing.SourceSystemID != value.SourceSystemID || existing.SourceRecordID != value.SourceRecordID || !existing.CreatedAt.Equal(value.CreatedAt) {
		return stack.License{}, stack.ErrConflict
	}
	s.licenses[key] = cloneStackLicense(value)
	return cloneStackLicense(value), nil
}

type stackStored interface {
	stack.Product | stack.Version | stack.Installation | stack.License | stack.Assignment
}

func createMemoryStack[T stackStored](values map[string]T, sources map[string]string, kind, organizationID, id, systemID, recordID string, value T, equal func(T, T) bool) (T, bool, error) {
	key := stackKey(organizationID, id)
	source := stackSourceKey(kind, organizationID, systemID, recordID)
	if source != "" {
		if existingKey, ok := sources[source]; ok {
			existing := values[existingKey]
			if equal(existing, value) {
				return existing, false, nil
			}
			var zero T
			return zero, false, stack.ErrConflict
		}
	}
	if existing, ok := values[key]; ok {
		if equal(existing, value) {
			return existing, false, nil
		}
		var zero T
		return zero, false, stack.ErrConflict
	}
	values[key] = value
	if source != "" {
		sources[source] = key
	}
	return value, true, nil
}

func equalStackProduct(left, right stack.Product) bool {
	return left.ID == right.ID && left.OrganizationID == right.OrganizationID && left.Name == right.Name && left.Publisher == right.Publisher && left.Category == right.Category && left.Status == right.Status && left.SourceSystemID == right.SourceSystemID && left.SourceRecordID == right.SourceRecordID
}
func equalStackVersion(left, right stack.Version) bool {
	return left.ID == right.ID && left.OrganizationID == right.OrganizationID && left.ProductID == right.ProductID && left.Name == right.Name && equalStackTime(left.ReleasedOn, right.ReleasedOn) && left.Status == right.Status && left.SourceSystemID == right.SourceSystemID && left.SourceRecordID == right.SourceRecordID
}
func equalStackInstallation(left, right stack.Installation) bool {
	return left.ID == right.ID && left.OrganizationID == right.OrganizationID && left.VersionID == right.VersionID && left.AssetID == right.AssetID && left.Status == right.Status && left.UsageState == right.UsageState && left.InstalledAt.Equal(right.InstalledAt) && equalStackTime(left.LastUsedAt, right.LastUsedAt) && equalStackTime(left.RemovedAt, right.RemovedAt) && left.SourceSystemID == right.SourceSystemID && left.SourceRecordID == right.SourceRecordID
}
func equalStackLicense(left, right stack.License) bool {
	return left.ID == right.ID && left.OrganizationID == right.OrganizationID && left.ProductID == right.ProductID && left.VersionID == right.VersionID && left.Name == right.Name && left.EntitlementMetric == right.EntitlementMetric && left.Quantity == right.Quantity && left.Status == right.Status && equalStackTime(left.StartsOn, right.StartsOn) && equalStackTime(left.ExpiresOn, right.ExpiresOn) && left.VendorID == right.VendorID && left.PurchaseOrderID == right.PurchaseOrderID && left.ContractID == right.ContractID && left.CostRecordID == right.CostRecordID && reflect.DeepEqual(left.DocumentIDs, right.DocumentIDs) && left.SourceSystemID == right.SourceSystemID && left.SourceRecordID == right.SourceRecordID
}
func equalStackAssignment(left, right stack.Assignment) bool {
	return left.ID == right.ID && left.OrganizationID == right.OrganizationID && left.LicenseID == right.LicenseID && left.AssigneeKind == right.AssigneeKind && left.AssigneeID == right.AssigneeID && left.Seats == right.Seats && left.UsageState == right.UsageState && left.AssignedAt.Equal(right.AssignedAt) && equalStackTime(left.LastUsedAt, right.LastUsedAt) && equalStackTime(left.EndedAt, right.EndedAt) && left.SourceSystemID == right.SourceSystemID && left.SourceRecordID == right.SourceRecordID
}
func equalStackTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}
func cloneStackVersion(value stack.Version) stack.Version {
	if value.ReleasedOn != nil {
		cloned := *value.ReleasedOn
		value.ReleasedOn = &cloned
	}
	return value
}
func cloneStackInstallation(value stack.Installation) stack.Installation {
	if value.LastUsedAt != nil {
		cloned := *value.LastUsedAt
		value.LastUsedAt = &cloned
	}
	if value.RemovedAt != nil {
		cloned := *value.RemovedAt
		value.RemovedAt = &cloned
	}
	return value
}
func cloneStackLicense(value stack.License) stack.License {
	if value.StartsOn != nil {
		cloned := *value.StartsOn
		value.StartsOn = &cloned
	}
	if value.ExpiresOn != nil {
		cloned := *value.ExpiresOn
		value.ExpiresOn = &cloned
	}
	value.DocumentIDs = append([]string(nil), value.DocumentIDs...)
	return value
}
func cloneStackAssignment(value stack.Assignment) stack.Assignment {
	if value.LastUsedAt != nil {
		cloned := *value.LastUsedAt
		value.LastUsedAt = &cloned
	}
	if value.EndedAt != nil {
		cloned := *value.EndedAt
		value.EndedAt = &cloned
	}
	return value
}
