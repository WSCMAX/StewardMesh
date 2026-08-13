package repository

// In-memory Atlas Catalog adapter.
// Requirement: REQ-ATLAS-CATALOG-001. Feature: inventory.products.

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/maxlemke/stewardmesh/internal/catalog"
)

type MemoryCatalogStore struct {
	mu             sync.RWMutex
	products       map[string]catalog.Product
	configurations map[string]catalog.Configuration
	prices         map[string]catalog.Price
	upgradePaths   map[string]catalog.UpgradePath
}

var _ catalog.Store = (*MemoryCatalogStore)(nil)

func NewMemoryCatalogStore() *MemoryCatalogStore {
	return &MemoryCatalogStore{
		products:       make(map[string]catalog.Product),
		configurations: make(map[string]catalog.Configuration),
		prices:         make(map[string]catalog.Price),
		upgradePaths:   make(map[string]catalog.UpgradePath),
	}
}

func catalogMemoryKey(organizationID, id string) string { return organizationID + "\x00" + id }

func (s *MemoryCatalogStore) ListProducts(_ context.Context, organizationID string, query catalog.ProductQuery) ([]catalog.Product, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	search := strings.ToLower(query.Search)
	items := make([]catalog.Product, 0)
	for _, product := range s.products {
		if product.OrganizationID != organizationID || query.AssetKind != "" && product.AssetKind != query.AssetKind ||
			query.Status != "" && product.Status != query.Status {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(product.Manufacturer+" "+product.Model), search) {
			continue
		}
		items = append(items, cloneCatalogProduct(product))
	}
	sort.Slice(items, func(i, j int) bool {
		left := strings.ToLower(items[i].Manufacturer + "\x00" + items[i].Model)
		right := strings.ToLower(items[j].Manufacturer + "\x00" + items[j].Model)
		if left == right {
			return items[i].ID < items[j].ID
		}
		return left < right
	})
	if len(items) > query.Limit && query.Limit > 0 {
		items = items[:query.Limit]
	}
	return items, nil
}

func (s *MemoryCatalogStore) GetProduct(_ context.Context, organizationID, productID string) (catalog.Product, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	product, exists := s.products[catalogMemoryKey(organizationID, productID)]
	if !exists {
		return catalog.Product{}, catalog.ErrNotFound
	}
	return cloneCatalogProduct(product), nil
}

func (s *MemoryCatalogStore) CreateProduct(_ context.Context, product catalog.Product) (catalog.Product, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := catalogMemoryKey(product.OrganizationID, product.ID)
	if _, exists := s.products[key]; exists {
		return catalog.Product{}, catalog.ErrConflict
	}
	for _, existing := range s.products {
		if existing.OrganizationID == product.OrganizationID && strings.EqualFold(existing.Manufacturer, product.Manufacturer) &&
			strings.EqualFold(existing.Model, product.Model) {
			return catalog.Product{}, catalog.ErrConflict
		}
	}
	s.products[key] = cloneCatalogProduct(product)
	return cloneCatalogProduct(product), nil
}

func (s *MemoryCatalogStore) ListConfigurations(_ context.Context, organizationID, productID string) ([]catalog.Configuration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, exists := s.products[catalogMemoryKey(organizationID, productID)]; !exists {
		return nil, catalog.ErrNotFound
	}
	items := make([]catalog.Configuration, 0)
	for _, configuration := range s.configurations {
		if configuration.OrganizationID == organizationID && configuration.ProductID == productID {
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
	if _, exists := s.products[catalogMemoryKey(configuration.OrganizationID, configuration.ProductID)]; !exists {
		return catalog.Configuration{}, catalog.ErrNotFound
	}
	key := catalogMemoryKey(configuration.OrganizationID, configuration.ID)
	if _, exists := s.configurations[key]; exists {
		return catalog.Configuration{}, catalog.ErrConflict
	}
	for _, existing := range s.configurations {
		if existing.OrganizationID != configuration.OrganizationID {
			continue
		}
		if existing.ProductID == configuration.ProductID && strings.EqualFold(existing.Name, configuration.Name) ||
			configuration.SKU != "" && existing.SKU != "" && strings.EqualFold(existing.SKU, configuration.SKU) {
			return catalog.Configuration{}, catalog.ErrConflict
		}
	}
	s.configurations[key] = cloneCatalogConfiguration(configuration)
	return cloneCatalogConfiguration(configuration), nil
}

func (s *MemoryCatalogStore) ListPrices(_ context.Context, organizationID, productID, configurationID string) ([]catalog.Price, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, exists := s.products[catalogMemoryKey(organizationID, productID)]; !exists {
		return nil, catalog.ErrNotFound
	}
	items := make([]catalog.Price, 0)
	for _, price := range s.prices {
		if price.OrganizationID == organizationID && price.ProductID == productID &&
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
	if _, exists := s.products[catalogMemoryKey(price.OrganizationID, price.ProductID)]; !exists {
		return catalog.Price{}, catalog.ErrNotFound
	}
	if price.ConfigurationID != "" {
		configuration, exists := s.configurations[catalogMemoryKey(price.OrganizationID, price.ConfigurationID)]
		if !exists || configuration.ProductID != price.ProductID {
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

func (s *MemoryCatalogStore) ListUpgradePaths(_ context.Context, organizationID, fromProductID, fromConfigurationID string) ([]catalog.UpgradePath, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, exists := s.products[catalogMemoryKey(organizationID, fromProductID)]; !exists {
		return nil, catalog.ErrNotFound
	}
	items := make([]catalog.UpgradePath, 0)
	for _, path := range s.upgradePaths {
		if path.OrganizationID == organizationID && path.FromProductID == fromProductID &&
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
	if !s.validCatalogEndpoint(path.OrganizationID, path.FromProductID, path.FromConfigurationID) ||
		!s.validCatalogEndpoint(path.OrganizationID, path.ToProductID, path.ToConfigurationID) {
		return catalog.UpgradePath{}, catalog.ErrNotFound
	}
	key := catalogMemoryKey(path.OrganizationID, path.ID)
	if _, exists := s.upgradePaths[key]; exists {
		return catalog.UpgradePath{}, catalog.ErrConflict
	}
	for _, existing := range s.upgradePaths {
		if existing.OrganizationID == path.OrganizationID && existing.FromProductID == path.FromProductID &&
			existing.FromConfigurationID == path.FromConfigurationID && existing.ToProductID == path.ToProductID &&
			existing.ToConfigurationID == path.ToConfigurationID && existing.Kind == path.Kind &&
			existing.EffectiveFrom.Equal(path.EffectiveFrom) {
			return catalog.UpgradePath{}, catalog.ErrConflict
		}
	}
	s.upgradePaths[key] = path
	return path, nil
}

func (s *MemoryCatalogStore) validCatalogEndpoint(organizationID, productID, configurationID string) bool {
	if _, exists := s.products[catalogMemoryKey(organizationID, productID)]; !exists {
		return false
	}
	if configurationID == "" {
		return true
	}
	configuration, exists := s.configurations[catalogMemoryKey(organizationID, configurationID)]
	return exists && configuration.ProductID == productID
}

func cloneCatalogProduct(product catalog.Product) catalog.Product {
	product.Specifications = cloneCatalogSpecifications(product.Specifications)
	return product
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
