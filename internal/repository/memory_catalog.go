package repository

// In-memory Atlas Catalog adapter.
// Requirement: REQ-ATLAS-CATALOG-001. Feature: inventory.catalog.

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/maxlemke/stewardmesh/internal/catalog"
)

type MemoryCatalogStore struct {
	mu             sync.RWMutex
	configurations map[string]catalog.Configuration
	prices         map[string]catalog.Price
	upgradePaths   map[string]catalog.UpgradePath
}

var _ catalog.Store = (*MemoryCatalogStore)(nil)

func NewMemoryCatalogStore() *MemoryCatalogStore {
	return &MemoryCatalogStore{
		configurations: make(map[string]catalog.Configuration),
		prices:         make(map[string]catalog.Price),
		upgradePaths:   make(map[string]catalog.UpgradePath),
	}
}

func (s *MemoryCatalogStore) Snapshot(_ context.Context, organizationID string) (catalog.Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := catalog.Snapshot{Configurations: []catalog.Configuration{}, Prices: []catalog.Price{}, UpgradePaths: []catalog.UpgradePath{}}
	for _, item := range s.configurations {
		if item.OrganizationID == organizationID {
			result.Configurations = append(result.Configurations, cloneCatalogConfiguration(item))
		}
	}
	for _, item := range s.prices {
		if item.OrganizationID == organizationID {
			result.Prices = append(result.Prices, cloneCatalogPrice(item))
		}
	}
	for _, item := range s.upgradePaths {
		if item.OrganizationID == organizationID {
			result.UpgradePaths = append(result.UpgradePaths, item)
		}
	}
	sort.Slice(result.Configurations, func(i, j int) bool { return result.Configurations[i].ID < result.Configurations[j].ID })
	sort.Slice(result.Prices, func(i, j int) bool { return result.Prices[i].ID < result.Prices[j].ID })
	sort.Slice(result.UpgradePaths, func(i, j int) bool { return result.UpgradePaths[i].ID < result.UpgradePaths[j].ID })
	return result, nil
}

func catalogMemoryKey(organizationID, id string) string { return organizationID + "\x00" + id }

func (s *MemoryCatalogStore) ListConfigurations(_ context.Context, organizationID, modelID string) ([]catalog.Configuration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]catalog.Configuration, 0)
	for _, configuration := range s.configurations {
		if configuration.OrganizationID == organizationID && configuration.ModelID == modelID {
			items = append(items, cloneCatalogConfiguration(configuration))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		left, right := strings.ToLower(items[i].Name), strings.ToLower(items[j].Name)
		if left == right {
			return items[i].ID < items[j].ID
		}
		return left < right
	})
	return items, nil
}

func (s *MemoryCatalogStore) GetConfiguration(_ context.Context, organizationID, configurationID string) (catalog.Configuration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	configuration, exists := s.configurations[catalogMemoryKey(organizationID, configurationID)]
	if !exists {
		return catalog.Configuration{}, catalog.ErrNotFound
	}
	return cloneCatalogConfiguration(configuration), nil
}

func (s *MemoryCatalogStore) CreateConfiguration(_ context.Context, configuration catalog.Configuration) (catalog.Configuration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := catalogMemoryKey(configuration.OrganizationID, configuration.ID)
	if _, exists := s.configurations[key]; exists {
		return catalog.Configuration{}, catalog.ErrConflict
	}
	for _, existing := range s.configurations {
		if existing.OrganizationID != configuration.OrganizationID {
			continue
		}
		if existing.ModelID == configuration.ModelID && strings.EqualFold(existing.Name, configuration.Name) ||
			configuration.SKU != "" && existing.SKU != "" && strings.EqualFold(existing.SKU, configuration.SKU) {
			return catalog.Configuration{}, catalog.ErrConflict
		}
	}
	s.configurations[key] = cloneCatalogConfiguration(configuration)
	return cloneCatalogConfiguration(configuration), nil
}

func (s *MemoryCatalogStore) ListPrices(_ context.Context, organizationID, modelID, configurationID string) ([]catalog.Price, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]catalog.Price, 0)
	for _, price := range s.prices {
		if price.OrganizationID == organizationID && price.ModelID == modelID &&
			(configurationID == "" || price.ConfigurationID == configurationID) {
			items = append(items, cloneCatalogPrice(price))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].EffectiveFrom.Equal(items[j].EffectiveFrom) {
			return items[i].ID < items[j].ID
		}
		return items[i].EffectiveFrom.After(items[j].EffectiveFrom)
	})
	return items, nil
}

func (s *MemoryCatalogStore) CreatePrice(_ context.Context, price catalog.Price) (catalog.Price, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if price.Revision != 1 {
		return catalog.Price{}, catalog.ErrInvalidInput
	}
	if price.ConfigurationID != "" {
		configuration, exists := s.configurations[catalogMemoryKey(price.OrganizationID, price.ConfigurationID)]
		if !exists || configuration.ModelID != price.ModelID {
			return catalog.Price{}, catalog.ErrNotFound
		}
	}
	key := catalogMemoryKey(price.OrganizationID, price.ID)
	if _, exists := s.prices[key]; exists {
		return catalog.Price{}, catalog.ErrConflict
	}
	s.prices[key] = cloneCatalogPrice(price)
	return cloneCatalogPrice(price), nil
}

func (s *MemoryCatalogStore) GetPrice(_ context.Context, organizationID, priceID string) (catalog.Price, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	price, exists := s.prices[catalogMemoryKey(organizationID, priceID)]
	if !exists {
		return catalog.Price{}, catalog.ErrNotFound
	}
	return cloneCatalogPrice(price), nil
}

func (s *MemoryCatalogStore) ListUpgradePaths(_ context.Context, organizationID, fromModelID, fromConfigurationID string) ([]catalog.UpgradePath, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]catalog.UpgradePath, 0)
	for _, path := range s.upgradePaths {
		if path.OrganizationID == organizationID && path.FromModelID == fromModelID &&
			path.FromConfigurationID == fromConfigurationID {
			items = append(items, path)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].EffectiveFrom.Equal(items[j].EffectiveFrom) {
			return items[i].ID < items[j].ID
		}
		return items[i].EffectiveFrom.After(items[j].EffectiveFrom)
	})
	return items, nil
}

func (s *MemoryCatalogStore) CreateUpgradePath(_ context.Context, path catalog.UpgradePath) (catalog.UpgradePath, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if path.Revision != 1 {
		return catalog.UpgradePath{}, catalog.ErrInvalidInput
	}
	if !s.validCatalogEndpoint(path.OrganizationID, path.FromModelID, path.FromConfigurationID) ||
		!s.validCatalogEndpoint(path.OrganizationID, path.ToModelID, path.ToConfigurationID) {
		return catalog.UpgradePath{}, catalog.ErrNotFound
	}
	key := catalogMemoryKey(path.OrganizationID, path.ID)
	if _, exists := s.upgradePaths[key]; exists {
		return catalog.UpgradePath{}, catalog.ErrConflict
	}
	for _, existing := range s.upgradePaths {
		if existing.OrganizationID == path.OrganizationID && existing.FromModelID == path.FromModelID &&
			existing.FromConfigurationID == path.FromConfigurationID && existing.ToModelID == path.ToModelID &&
			existing.ToConfigurationID == path.ToConfigurationID && existing.Kind == path.Kind &&
			existing.EffectiveFrom.Equal(path.EffectiveFrom) {
			return catalog.UpgradePath{}, catalog.ErrConflict
		}
	}
	s.upgradePaths[key] = path
	return path, nil
}

func (s *MemoryCatalogStore) GetUpgradePath(_ context.Context, organizationID, pathID string) (catalog.UpgradePath, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	path, exists := s.upgradePaths[catalogMemoryKey(organizationID, pathID)]
	if !exists {
		return catalog.UpgradePath{}, catalog.ErrNotFound
	}
	return path, nil
}

func (s *MemoryCatalogStore) validCatalogEndpoint(organizationID, modelID, configurationID string) bool {
	if configurationID == "" {
		return true
	}
	configuration, exists := s.configurations[catalogMemoryKey(organizationID, configurationID)]
	return exists && configuration.ModelID == modelID
}

func cloneCatalogConfiguration(configuration catalog.Configuration) catalog.Configuration {
	configuration.Specifications = cloneCatalogSpecifications(configuration.Specifications)
	return configuration
}

func cloneCatalogPrice(price catalog.Price) catalog.Price {
	if price.EffectiveTo != nil {
		value := *price.EffectiveTo
		price.EffectiveTo = &value
	}
	return price
}

func cloneCatalogSpecifications(specifications map[string]string) map[string]string {
	result := make(map[string]string, len(specifications))
	for key, value := range specifications {
		result[key] = value
	}
	return result
}
