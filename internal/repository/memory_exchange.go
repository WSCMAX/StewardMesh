package repository

// Requirement: REQ-EXCHANGE-001. Feature: migration.packages.

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/maxlemke/stewardmesh/internal/exchange"
)

type MemoryExchangeStore struct {
	mu       sync.RWMutex
	packages map[string]exchange.Package
}

func NewMemoryExchangeStore() *MemoryExchangeStore {
	return &MemoryExchangeStore{packages: make(map[string]exchange.Package)}
}

func exchangePackageKey(organizationID string, direction exchange.PackageDirection, packageID string) string {
	return organizationID + "\x00" + string(direction) + "\x00" + packageID
}

func (s *MemoryExchangeStore) ListPackages(_ context.Context, organizationID string, limit int) ([]exchange.Package, error) {
	if organizationID == "" || limit < 1 || limit > exchange.MaximumHistory {
		return nil, exchange.ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]exchange.Package, 0)
	for _, value := range s.packages {
		if value.OrganizationID == organizationID {
			result = append(result, cloneExchangePackage(value))
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			if result[i].Direction == result[j].Direction {
				return result[i].PackageID < result[j].PackageID
			}
			return result[i].Direction < result[j].Direction
		}
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	if len(result) > limit {
		result = result[:limit]
	}
	if result == nil {
		result = []exchange.Package{}
	}
	return result, nil
}

func (s *MemoryExchangeStore) GetPackage(_ context.Context, organizationID string, direction exchange.PackageDirection, packageID string) (exchange.Package, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.packages[exchangePackageKey(organizationID, direction, packageID)]
	if !ok {
		return exchange.Package{}, exchange.ErrNotFound
	}
	return cloneExchangePackage(value), nil
}

func (s *MemoryExchangeStore) CreatePackage(_ context.Context, value exchange.Package) (exchange.Package, bool, error) {
	if err := value.Validate(); err != nil {
		return exchange.Package{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := exchangePackageKey(value.OrganizationID, value.Direction, value.PackageID)
	if existing, ok := s.packages[key]; ok {
		if !sameExchangeArchiveIdentity(existing, value) {
			return exchange.Package{}, false, exchange.ErrConflict
		}
		return cloneExchangePackage(existing), false, nil
	}
	s.packages[key] = cloneExchangePackage(value)
	return cloneExchangePackage(value), true, nil
}

func sameExchangeArchiveIdentity(left, right exchange.Package) bool {
	return left.ArchiveSHA256 == right.ArchiveSHA256 && left.SourceSystemID == right.SourceSystemID &&
		left.SchemaVersion == right.SchemaVersion && left.SizeBytes == right.SizeBytes && left.FileMode == right.FileMode &&
		left.RecordCount == right.RecordCount && left.FileCount == right.FileCount
}

func (s *MemoryExchangeStore) UpdatePackage(_ context.Context, value exchange.Package, expectedUpdatedAt time.Time) (exchange.Package, error) {
	if err := value.Validate(); err != nil || expectedUpdatedAt.IsZero() {
		if err != nil {
			return exchange.Package{}, err
		}
		return exchange.Package{}, exchange.ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := exchangePackageKey(value.OrganizationID, value.Direction, value.PackageID)
	existing, ok := s.packages[key]
	if !ok {
		return exchange.Package{}, exchange.ErrNotFound
	}
	if !existing.UpdatedAt.Equal(expectedUpdatedAt) || !sameExchangePackageIdentity(existing, value) || value.ValidateTransitionFrom(existing) != nil {
		return exchange.Package{}, exchange.ErrConflict
	}
	s.packages[key] = cloneExchangePackage(value)
	return cloneExchangePackage(value), nil
}

func sameExchangePackageIdentity(left, right exchange.Package) bool {
	return left.OrganizationID == right.OrganizationID && left.PackageID == right.PackageID && left.Direction == right.Direction &&
		left.SchemaVersion == right.SchemaVersion && left.SourceSystemID == right.SourceSystemID && left.ArchiveSHA256 == right.ArchiveSHA256 &&
		left.SizeBytes == right.SizeBytes && left.FileMode == right.FileMode && left.RecordCount == right.RecordCount &&
		left.FileCount == right.FileCount && left.CreatedBy == right.CreatedBy && left.CreatedAt.Equal(right.CreatedAt)
}

func cloneExchangePackage(value exchange.Package) exchange.Package {
	value.Records = append([]exchange.RecordOutcome(nil), value.Records...)
	value.Progress = append([]exchange.ImportProgress(nil), value.Progress...)
	for index := range value.Records {
		value.Records[index].MissingDependencies = append([]exchange.Reference{}, value.Records[index].MissingDependencies...)
	}
	return value
}

var _ exchange.Store = (*MemoryExchangeStore)(nil)
