package repository

// In-memory Ledger adapter. Requirement: REQ-LEDGER-001.

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/maxlemke/stewardmesh/internal/ledger"
)

type MemoryLedgerStore struct {
	mu             sync.RWMutex
	vendors        map[string]ledger.Vendor
	purchaseOrders map[string]ledger.PurchaseOrder
	contracts      map[string]ledger.Contract
	commitments    map[string]ledger.Commitment
	budgets        map[string]ledger.Budget
	costs          map[string]ledger.CostRecord
	costSources    map[string]string
}

func NewMemoryLedgerStore() *MemoryLedgerStore {
	return &MemoryLedgerStore{
		vendors: make(map[string]ledger.Vendor), purchaseOrders: make(map[string]ledger.PurchaseOrder),
		contracts: make(map[string]ledger.Contract), commitments: make(map[string]ledger.Commitment),
		budgets: make(map[string]ledger.Budget), costs: make(map[string]ledger.CostRecord), costSources: make(map[string]string),
	}
}

func ledgerKey(organizationID, id string) string { return organizationID + "\x00" + id }
func ledgerSourceKey(organizationID, sourceSystemID, sourceRecordID string) string {
	return organizationID + "\x00" + strings.ToLower(sourceSystemID) + "\x00" + sourceRecordID
}

func (s *MemoryLedgerStore) Snapshot(_ context.Context, organizationID string) (ledger.Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot(organizationID), nil
}

func (s *MemoryLedgerStore) snapshot(organizationID string) ledger.Snapshot {
	result := ledger.Snapshot{
		Vendors: []ledger.Vendor{}, PurchaseOrders: []ledger.PurchaseOrder{}, Contracts: []ledger.Contract{},
		Commitments: []ledger.Commitment{}, Budgets: []ledger.Budget{}, Costs: []ledger.CostRecord{},
	}
	for _, item := range s.vendors {
		if item.OrganizationID == organizationID {
			result.Vendors = append(result.Vendors, item)
		}
	}
	for _, item := range s.purchaseOrders {
		if item.OrganizationID == organizationID {
			result.PurchaseOrders = append(result.PurchaseOrders, clonePurchaseOrder(item))
		}
	}
	for _, item := range s.contracts {
		if item.OrganizationID == organizationID {
			result.Contracts = append(result.Contracts, cloneContract(item))
		}
	}
	for _, item := range s.commitments {
		if item.OrganizationID == organizationID {
			result.Commitments = append(result.Commitments, item)
		}
	}
	for _, item := range s.budgets {
		if item.OrganizationID == organizationID {
			result.Budgets = append(result.Budgets, item)
		}
	}
	for _, item := range s.costs {
		if item.OrganizationID == organizationID {
			result.Costs = append(result.Costs, item)
		}
	}
	sort.Slice(result.Vendors, func(i, j int) bool { return result.Vendors[i].Name < result.Vendors[j].Name })
	sort.Slice(result.PurchaseOrders, func(i, j int) bool { return result.PurchaseOrders[i].Number < result.PurchaseOrders[j].Number })
	sort.Slice(result.Contracts, func(i, j int) bool { return result.Contracts[i].Name < result.Contracts[j].Name })
	sort.Slice(result.Commitments, func(i, j int) bool { return result.Commitments[i].CreatedAt.Before(result.Commitments[j].CreatedAt) })
	sort.Slice(result.Budgets, func(i, j int) bool {
		if result.Budgets[i].FiscalPeriod == result.Budgets[j].FiscalPeriod {
			return result.Budgets[i].Name < result.Budgets[j].Name
		}
		return result.Budgets[i].FiscalPeriod < result.Budgets[j].FiscalPeriod
	})
	sort.Slice(result.Costs, func(i, j int) bool { return result.Costs[i].CreatedAt.Before(result.Costs[j].CreatedAt) })
	return result
}

func (s *MemoryLedgerStore) ExchangeSnapshot(ctx context.Context, organizationID string, maximum int) (ledger.Snapshot, error) {
	if organizationID == "" || maximum < 1 {
		return ledger.Snapshot{}, ledger.ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := s.snapshot(organizationID)
	count := len(result.Vendors) + len(result.PurchaseOrders) + len(result.Contracts) + len(result.Commitments) + len(result.Budgets) + len(result.Costs)
	if count > maximum {
		return ledger.Snapshot{}, ledger.ErrTooLarge
	}
	return result, nil
}

func (s *MemoryLedgerStore) GetVendor(_ context.Context, organizationID, id string) (ledger.Vendor, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.vendors[ledgerKey(organizationID, id)]
	if !ok {
		return ledger.Vendor{}, ledger.ErrNotFound
	}
	return item, nil
}

func (s *MemoryLedgerStore) CreateVendor(_ context.Context, item ledger.Vendor) (ledger.Vendor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := ledgerKey(item.OrganizationID, item.ID)
	if _, ok := s.vendors[key]; ok {
		return ledger.Vendor{}, ledger.ErrConflict
	}
	for _, existing := range s.vendors {
		if existing.OrganizationID == item.OrganizationID && strings.EqualFold(existing.Name, item.Name) {
			return ledger.Vendor{}, ledger.ErrConflict
		}
	}
	s.vendors[key] = item
	return item, nil
}

func (s *MemoryLedgerStore) GetPurchaseOrder(_ context.Context, organizationID, id string) (ledger.PurchaseOrder, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.purchaseOrders[ledgerKey(organizationID, id)]
	if !ok {
		return ledger.PurchaseOrder{}, ledger.ErrNotFound
	}
	return clonePurchaseOrder(item), nil
}

func (s *MemoryLedgerStore) CreatePurchaseOrder(_ context.Context, item ledger.PurchaseOrder) (ledger.PurchaseOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := ledgerKey(item.OrganizationID, item.ID)
	if _, ok := s.purchaseOrders[key]; ok {
		return ledger.PurchaseOrder{}, ledger.ErrConflict
	}
	for _, existing := range s.purchaseOrders {
		if existing.OrganizationID == item.OrganizationID && strings.EqualFold(existing.Number, item.Number) {
			return ledger.PurchaseOrder{}, ledger.ErrConflict
		}
	}
	s.purchaseOrders[key] = clonePurchaseOrder(item)
	return clonePurchaseOrder(item), nil
}

func (s *MemoryLedgerStore) UpdatePurchaseOrder(_ context.Context, item ledger.PurchaseOrder, expectedRevision int64) (ledger.PurchaseOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := ledgerKey(item.OrganizationID, item.ID)
	existing, ok := s.purchaseOrders[key]
	if !ok {
		return ledger.PurchaseOrder{}, ledger.ErrNotFound
	}
	if existing.Revision != expectedRevision {
		return ledger.PurchaseOrder{}, ledger.ErrConflict
	}
	s.purchaseOrders[key] = clonePurchaseOrder(item)
	return clonePurchaseOrder(item), nil
}

func (s *MemoryLedgerStore) GetContract(_ context.Context, organizationID, id string) (ledger.Contract, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.contracts[ledgerKey(organizationID, id)]
	if !ok {
		return ledger.Contract{}, ledger.ErrNotFound
	}
	return cloneContract(item), nil
}

func (s *MemoryLedgerStore) CreateContract(_ context.Context, item ledger.Contract) (ledger.Contract, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := ledgerKey(item.OrganizationID, item.ID)
	if _, ok := s.contracts[key]; ok {
		return ledger.Contract{}, ledger.ErrConflict
	}
	s.contracts[key] = cloneContract(item)
	return cloneContract(item), nil
}

func (s *MemoryLedgerStore) UpdateContract(_ context.Context, item ledger.Contract, expectedRevision int64) (ledger.Contract, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := ledgerKey(item.OrganizationID, item.ID)
	existing, ok := s.contracts[key]
	if !ok {
		return ledger.Contract{}, ledger.ErrNotFound
	}
	if existing.Revision != expectedRevision {
		return ledger.Contract{}, ledger.ErrConflict
	}
	s.contracts[key] = cloneContract(item)
	return cloneContract(item), nil
}

func (s *MemoryLedgerStore) CreateCommitment(_ context.Context, item ledger.Commitment) (ledger.Commitment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := ledgerKey(item.OrganizationID, item.ID)
	if _, ok := s.commitments[key]; ok {
		return ledger.Commitment{}, ledger.ErrConflict
	}
	s.commitments[key] = item
	return item, nil
}

func (s *MemoryLedgerStore) GetCommitment(_ context.Context, organizationID, id string) (ledger.Commitment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.commitments[ledgerKey(organizationID, id)]
	if !ok {
		return ledger.Commitment{}, ledger.ErrNotFound
	}
	return item, nil
}

func (s *MemoryLedgerStore) CreateBudget(_ context.Context, item ledger.Budget) (ledger.Budget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := ledgerKey(item.OrganizationID, item.ID)
	if _, ok := s.budgets[key]; ok {
		return ledger.Budget{}, ledger.ErrConflict
	}
	for _, existing := range s.budgets {
		if existing.OrganizationID == item.OrganizationID && existing.FiscalPeriod == item.FiscalPeriod && existing.Scenario == item.Scenario && existing.DepartmentID == item.DepartmentID && existing.SiteID == item.SiteID && strings.EqualFold(existing.Name, item.Name) {
			return ledger.Budget{}, ledger.ErrConflict
		}
	}
	s.budgets[key] = item
	return item, nil
}

func (s *MemoryLedgerStore) GetBudget(_ context.Context, organizationID, id string) (ledger.Budget, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.budgets[ledgerKey(organizationID, id)]
	if !ok {
		return ledger.Budget{}, ledger.ErrNotFound
	}
	return item, nil
}

func (s *MemoryLedgerStore) GetCost(_ context.Context, organizationID, id string) (ledger.CostRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.costs[ledgerKey(organizationID, id)]
	if !ok {
		return ledger.CostRecord{}, ledger.ErrNotFound
	}
	return item, nil
}

func (s *MemoryLedgerStore) GetCostBySource(_ context.Context, organizationID, sourceSystemID, sourceRecordID string) (ledger.CostRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.costSources[ledgerSourceKey(organizationID, sourceSystemID, sourceRecordID)]
	if !ok {
		return ledger.CostRecord{}, ledger.ErrNotFound
	}
	return s.costs[ledgerKey(organizationID, id)], nil
}

func (s *MemoryLedgerStore) CreateCost(_ context.Context, item ledger.CostRecord) (ledger.CostRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := ledgerKey(item.OrganizationID, item.ID)
	if _, ok := s.costs[key]; ok {
		return ledger.CostRecord{}, ledger.ErrConflict
	}
	if item.SourceSystemID != "" {
		sourceKey := ledgerSourceKey(item.OrganizationID, item.SourceSystemID, item.SourceRecordID)
		if _, ok := s.costSources[sourceKey]; ok {
			return ledger.CostRecord{}, ledger.ErrConflict
		}
		s.costSources[sourceKey] = item.ID
	}
	s.costs[key] = item
	return item, nil
}

func (s *MemoryLedgerStore) UpdateCost(_ context.Context, item ledger.CostRecord, expectedRevision int64) (ledger.CostRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := ledgerKey(item.OrganizationID, item.ID)
	existing, ok := s.costs[key]
	if !ok {
		return ledger.CostRecord{}, ledger.ErrNotFound
	}
	if existing.Revision != expectedRevision {
		return ledger.CostRecord{}, ledger.ErrConflict
	}
	if existing.SourceSystemID != item.SourceSystemID || existing.SourceRecordID != item.SourceRecordID {
		return ledger.CostRecord{}, ledger.ErrConflict
	}
	s.costs[key] = item
	return item, nil
}

func clonePurchaseOrder(item ledger.PurchaseOrder) ledger.PurchaseOrder {
	item.AssetIDs = append([]string(nil), item.AssetIDs...)
	item.ReceiptDocumentIDs = append([]string(nil), item.ReceiptDocumentIDs...)
	if len(item.Lines) > 0 {
		item.Lines = append([]ledger.PurchaseOrderLine(nil), item.Lines...)
	}
	if item.OrderedOn != nil {
		value := *item.OrderedOn
		item.OrderedOn = &value
	}
	return item
}

func cloneContract(item ledger.Contract) ledger.Contract {
	item.DocumentIDs = append([]string(nil), item.DocumentIDs...)
	if item.RenewsOn != nil {
		value := *item.RenewsOn
		item.RenewsOn = &value
	}
	return item
}
