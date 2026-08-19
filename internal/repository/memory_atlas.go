package repository

// Requirements: REQ-ATLAS-001, REQ-ATLAS-MODELS-001, REQ-DIRECTORY-EXPANSION-008. Features: inventory.assets, inventory.models, threads.relationships.

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/people"
)

type memoryGraphIdentityVisibility interface {
	GraphIdentityVisible(string, string, people.Visibility) bool
}

type MemoryAtlasStore struct {
	mu         sync.RWMutex
	models     map[string]domain.AssetModel
	assets     map[string]domain.Asset
	lifecycle  map[string][]domain.AssetLifecycleEvent
	identities memoryGraphIdentityVisibility
}

func NewMemoryAtlasStoreWithPeople(identities *MemoryPeopleStore) *MemoryAtlasStore {
	store := NewMemoryAtlasStore()
	store.identities = identities
	return store
}

var _ atlas.Store = (*MemoryAtlasStore)(nil)

func NewMemoryAtlasStore() *MemoryAtlasStore {
	return &MemoryAtlasStore{
		models: make(map[string]domain.AssetModel), assets: make(map[string]domain.Asset),
		lifecycle: make(map[string][]domain.AssetLifecycleEvent),
	}
}

func atlasMemoryKey(organizationID, id string) string {
	return organizationID + "\x00" + id
}

func (s *MemoryAtlasStore) ExchangeSnapshot(_ context.Context, organizationID string, maximum int) (atlas.ExchangeSnapshot, error) {
	if strings.TrimSpace(organizationID) == "" || maximum < 1 {
		return atlas.ExchangeSnapshot{}, atlas.ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := atlas.ExchangeSnapshot{Models: []domain.AssetModel{}, Assets: []domain.Asset{}, LifecycleEvents: []domain.AssetLifecycleEvent{}}
	for _, model := range s.models {
		if model.OrganizationID == organizationID {
			model.InstanceCount = 0
			result.Models = append(result.Models, cloneAssetModel(model))
		}
	}
	for key, asset := range s.assets {
		if asset.OrganizationID != organizationID {
			continue
		}
		result.Assets = append(result.Assets, cloneAsset(asset))
		result.LifecycleEvents = append(result.LifecycleEvents, s.lifecycle[key]...)
	}
	if len(result.Models)+len(result.Assets)+len(result.LifecycleEvents) > maximum {
		return atlas.ExchangeSnapshot{}, atlas.ErrTooLarge
	}
	sortAssetModels(result.Models)
	sort.Slice(result.Assets, func(i, j int) bool { return result.Assets[i].ID < result.Assets[j].ID })
	sort.Slice(result.LifecycleEvents, func(i, j int) bool { return result.LifecycleEvents[i].ID < result.LifecycleEvents[j].ID })
	return result, nil
}

func (s *MemoryAtlasStore) ListModels(_ context.Context, organizationID string, query atlas.ModelQuery) ([]domain.AssetModel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	search := strings.ToLower(query.Search)
	items := make([]domain.AssetModel, 0)
	for _, model := range s.models {
		if model.OrganizationID != organizationID || (query.Kind != "" && model.Kind != query.Kind) ||
			(query.Status != "" && model.Status != query.Status) {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(strings.Join([]string{
			model.Manufacturer, model.Name, model.ModelNumber, model.VendorIdentifier,
		}, "\n")), search) {
			continue
		}
		model.InstanceCount = s.modelInstanceCountLocked(model.OrganizationID, model.ID)
		items = append(items, cloneAssetModel(model))
	}
	sortAssetModels(items)
	if len(items) > query.Limit {
		items = items[:query.Limit]
	}
	return items, nil
}

func (s *MemoryAtlasStore) GetModel(_ context.Context, organizationID, id string) (domain.AssetModel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	model, ok := s.models[atlasMemoryKey(organizationID, id)]
	if !ok {
		return domain.AssetModel{}, atlas.ErrNotFound
	}
	model.InstanceCount = s.modelInstanceCountLocked(organizationID, id)
	return cloneAssetModel(model), nil
}

func (s *MemoryAtlasStore) ResolveModel(_ context.Context, organizationID string, identity atlas.ModelIdentity) (domain.AssetModel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, model := range s.models {
		if model.OrganizationID == organizationID &&
			strings.EqualFold(model.Manufacturer, identity.Manufacturer) &&
			strings.EqualFold(model.Name, identity.Name) &&
			strings.EqualFold(model.ModelNumber, identity.ModelNumber) {
			model.InstanceCount = s.modelInstanceCountLocked(organizationID, model.ID)
			return cloneAssetModel(model), nil
		}
	}
	return domain.AssetModel{}, atlas.ErrNotFound
}

func (s *MemoryAtlasStore) CreateModel(_ context.Context, model domain.AssetModel) (domain.AssetModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := atlasMemoryKey(model.OrganizationID, model.ID)
	if _, exists := s.models[key]; exists || s.modelIdentityConflict(model, "") {
		return domain.AssetModel{}, atlas.ErrConflict
	}
	s.models[key] = cloneAssetModel(model)
	return cloneAssetModel(model), nil
}

func (s *MemoryAtlasStore) UpdateModel(_ context.Context, model domain.AssetModel, expectedRevision int64) (domain.AssetModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := atlasMemoryKey(model.OrganizationID, model.ID)
	existing, ok := s.models[key]
	if !ok {
		return domain.AssetModel{}, atlas.ErrNotFound
	}
	if existing.Revision != expectedRevision || model.Revision != expectedRevision+1 || s.modelIdentityConflict(model, model.ID) {
		return domain.AssetModel{}, atlas.ErrConflict
	}
	s.models[key] = cloneAssetModel(model)
	model.InstanceCount = s.modelInstanceCountLocked(model.OrganizationID, model.ID)
	return cloneAssetModel(model), nil
}

func (s *MemoryAtlasStore) RetireModel(_ context.Context, organizationID, id string, expectedRevision int64, retiredAt time.Time) (domain.AssetModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := atlasMemoryKey(organizationID, id)
	model, ok := s.models[key]
	if !ok {
		return domain.AssetModel{}, atlas.ErrNotFound
	}
	if model.Revision != expectedRevision || model.Status == "retired" {
		return domain.AssetModel{}, atlas.ErrConflict
	}
	model.Status = "retired"
	model.Revision++
	model.UpdatedAt = retiredAt
	s.models[key] = cloneAssetModel(model)
	model.InstanceCount = s.modelInstanceCountLocked(model.OrganizationID, model.ID)
	return cloneAssetModel(model), nil
}

func (s *MemoryAtlasStore) ReactivateModel(_ context.Context, organizationID, id string, expectedRevision int64, reactivatedAt time.Time) (domain.AssetModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := atlasMemoryKey(organizationID, id)
	model, ok := s.models[key]
	if !ok {
		return domain.AssetModel{}, atlas.ErrNotFound
	}
	if model.Revision != expectedRevision || model.Status != "retired" {
		return domain.AssetModel{}, atlas.ErrConflict
	}
	model.Status = "active"
	model.Revision++
	model.UpdatedAt = reactivatedAt
	s.models[key] = cloneAssetModel(model)
	model.InstanceCount = s.modelInstanceCountLocked(model.OrganizationID, model.ID)
	return cloneAssetModel(model), nil
}

func (s *MemoryAtlasStore) GetModelInventory(_ context.Context, organizationID, modelID string, query atlas.ModelInventoryQuery) (atlas.ModelInventory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, exists := s.models[atlasMemoryKey(organizationID, modelID)]; !exists {
		return atlas.ModelInventory{}, atlas.ErrNotFound
	}
	assetQuery := atlas.Query{
		Status: query.Status, ModelID: modelID, SiteID: query.SiteID, DepartmentID: query.DepartmentID,
		UserID: query.UserID, DeploymentContext: query.DeploymentContext, Limit: query.Limit,
	}
	result := atlas.ModelInventory{
		ModelID: modelID, GroupBy: query.GroupBy, Groups: []atlas.ModelInventoryGroup{}, Items: []domain.Asset{},
	}
	groupCounts := make(map[string]int)
	for _, asset := range s.assets {
		if asset.OrganizationID != organizationID || asset.ModelID != modelID {
			continue
		}
		result.TotalCount++
		if !assetMatchesQuery(asset, assetQuery) {
			continue
		}
		result.FilteredCount++
		result.Items = append(result.Items, cloneAsset(asset))
		if query.GroupBy != "" {
			groupCounts[modelInventoryGroupKey(asset, query.GroupBy)]++
		}
	}
	sortAssets(result.Items)
	if len(result.Items) > query.Limit {
		result.Items = result.Items[:query.Limit]
	}
	for key, count := range groupCounts {
		result.Groups = append(result.Groups, atlas.ModelInventoryGroup{Key: key, Count: count})
	}
	sort.Slice(result.Groups, func(left, right int) bool {
		if result.Groups[left].Count != result.Groups[right].Count {
			return result.Groups[left].Count > result.Groups[right].Count
		}
		leftKey, rightKey := strings.ToLower(result.Groups[left].Key), strings.ToLower(result.Groups[right].Key)
		if leftKey != rightKey {
			return leftKey < rightKey
		}
		return result.Groups[left].Key < result.Groups[right].Key
	})
	return result, nil
}

func (s *MemoryAtlasStore) ListAssets(_ context.Context, organizationID string, query atlas.Query) ([]domain.Asset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]domain.Asset, 0)
	for _, asset := range s.assets {
		if asset.OrganizationID != organizationID || !assetMatchesQuery(asset, query) {
			continue
		}
		items = append(items, cloneAsset(asset))
	}
	sortAssets(items)
	if query.Cursor != "" {
		start := 0
		for start < len(items) && items[start].ID != query.Cursor {
			start++
		}
		if start >= len(items) {
			return []domain.Asset{}, nil
		}
		items = items[start+1:]
	}
	if len(items) > query.Limit {
		items = items[:query.Limit]
	}
	return items, nil
}

func (s *MemoryAtlasStore) ListAuthorizedAssets(_ context.Context, organizationID string, query atlas.AuthorizedAssetQuery) ([]domain.Asset, error) {
	if organizationID == "" || query.Limit < 1 || query.Limit > 100 || !query.Visibility.Valid() {
		return nil, atlas.ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]domain.Asset, 0, query.Limit)
	for _, asset := range s.assets {
		if asset.OrganizationID != organizationID || asset.ID <= query.Cursor ||
			!memoryAuthorizedAssetVisible(asset, query.Visibility) || !assetMatchesQuery(asset, atlas.Query{Search: query.Search}) {
			continue
		}
		items = append(items, cloneAsset(asset))
	}
	sort.Slice(items, func(left, right int) bool { return items[left].ID < items[right].ID })
	if len(items) > query.Limit {
		items = items[:query.Limit]
	}
	return items, nil
}

func memoryAuthorizedAssetVisible(asset domain.Asset, visibility atlas.GraphAssetVisibility) bool {
	return visibility.All || sliceContains(visibility.ResourceIDs, asset.ID) ||
		(asset.SiteID != "" && sliceContains(visibility.SiteIDs, asset.SiteID)) ||
		(asset.DepartmentID != "" && sliceContains(visibility.DepartmentIDs, asset.DepartmentID))
}

func (s *MemoryAtlasStore) ListGraphAssets(_ context.Context, organizationID string, query atlas.GraphAssetQuery) ([]domain.Asset, error) {
	if organizationID == "" || !query.Valid() {
		return nil, atlas.ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]domain.Asset, 0)
	for _, asset := range s.assets {
		if asset.OrganizationID != organizationID || !memoryGraphAssetVisible(asset, query.Visibility) ||
			!s.memoryGraphAssetDirectoryVisible(asset, query.Directory) ||
			!memoryGraphAssetMatchesReferences(asset, query.References) ||
			query.DirectOrganizationChildren && (asset.SiteID != "" || asset.BuildingID != "" || asset.RoomID != "" ||
				asset.DepartmentID != "" || asset.UserID != "") ||
			query.LabelSearch != "" && !strings.Contains(strings.ToLower(asset.Name), strings.ToLower(query.LabelSearch)) {
			continue
		}
		items = append(items, cloneAsset(asset))
	}
	sortAssets(items)
	if len(items) > query.Limit {
		items = items[:query.Limit]
	}
	return items, nil
}

func (s *MemoryAtlasStore) memoryGraphAssetDirectoryVisible(asset domain.Asset, visibility atlas.GraphAssetDirectoryVisibility) bool {
	return visibility.All ||
		(asset.SiteID != "" && sliceContains(visibility.SiteIDs, asset.SiteID)) ||
		(asset.DepartmentID != "" && sliceContains(visibility.DepartmentIDs, asset.DepartmentID)) ||
		(asset.UserID != "" && sliceContains(visibility.UserIDs, asset.UserID)) ||
		(visibility.MatchUserDirectory && asset.UserID != "" && s.identities != nil &&
			s.identities.GraphIdentityVisible(asset.OrganizationID, asset.UserID, people.Visibility{
				SiteIDs: visibility.SiteIDs, DepartmentIDs: visibility.DepartmentIDs,
			}))
}

func memoryGraphAssetVisible(asset domain.Asset, visibility atlas.GraphAssetVisibility) bool {
	return visibility.All || sliceContains(visibility.ResourceIDs, asset.ID) ||
		(asset.SiteID != "" && sliceContains(visibility.SiteIDs, asset.SiteID)) ||
		(asset.DepartmentID != "" && sliceContains(visibility.DepartmentIDs, asset.DepartmentID))
}

func memoryGraphAssetMatchesReferences(asset domain.Asset, references atlas.GraphAssetReferences) bool {
	return references.Empty() || sliceContains(references.ResourceIDs, asset.ID) ||
		(asset.SiteID != "" && sliceContains(references.SiteIDs, asset.SiteID)) ||
		(asset.BuildingID != "" && sliceContains(references.BuildingIDs, asset.BuildingID)) ||
		(asset.RoomID != "" && sliceContains(references.RoomIDs, asset.RoomID)) ||
		(asset.DepartmentID != "" && sliceContains(references.DepartmentIDs, asset.DepartmentID)) ||
		(asset.UserID != "" && sliceContains(references.UserIDs, asset.UserID))
}

func sliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func assetMatchesQuery(asset domain.Asset, query atlas.Query) bool {
	if (query.Kind != "" && asset.Kind != query.Kind) || (query.Status != "" && asset.Status != query.Status) ||
		(query.ModelID != "" && asset.ModelID != query.ModelID) || (query.SiteID != "" && asset.SiteID != query.SiteID) ||
		(query.DepartmentID != "" && asset.DepartmentID != query.DepartmentID) || (query.UserID != "" && asset.UserID != query.UserID) {
		return false
	}
	if query.Search != "" && !strings.Contains(strings.ToLower(strings.Join([]string{
		asset.Name, asset.AssetTag, asset.SerialNumber, asset.Hostname,
	}, "\n")), strings.ToLower(query.Search)) {
		return false
	}
	return query.DeploymentContext == "" || strings.Contains(strings.ToLower(strings.Join([]string{
		asset.Hostname, asset.DeploymentNotes,
	}, "\n")), strings.ToLower(query.DeploymentContext))
}

func modelInventoryGroupKey(asset domain.Asset, groupBy string) string {
	switch groupBy {
	case atlas.ModelInventoryGroupStatus:
		return asset.Status
	case atlas.ModelInventoryGroupSite:
		return asset.SiteID
	case atlas.ModelInventoryGroupDepartment:
		return asset.DepartmentID
	case atlas.ModelInventoryGroupUser:
		return asset.UserID
	case atlas.ModelInventoryGroupDeployment:
		if strings.TrimSpace(asset.DeploymentNotes) != "" {
			return asset.DeploymentNotes
		}
		return asset.Hostname
	default:
		return ""
	}
}

func (s *MemoryAtlasStore) GetAsset(_ context.Context, organizationID, id string) (domain.Asset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	asset, ok := s.assets[atlasMemoryKey(organizationID, id)]
	if !ok {
		return domain.Asset{}, atlas.ErrNotFound
	}
	return cloneAsset(asset), nil
}

func (s *MemoryAtlasStore) CreateAsset(ctx context.Context, asset domain.Asset, initialEvent domain.AssetLifecycleEvent) (domain.Asset, error) {
	created, err := s.CreateAssets(ctx, []domain.Asset{asset}, []domain.AssetLifecycleEvent{initialEvent})
	if err != nil {
		return domain.Asset{}, err
	}
	return created[0], nil
}

func (s *MemoryAtlasStore) CreateAssets(_ context.Context, assets []domain.Asset, initialEvents []domain.AssetLifecycleEvent) ([]domain.Asset, error) {
	if len(assets) == 0 || len(assets) != len(initialEvents) {
		return nil, atlas.ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for index, asset := range assets {
		key := atlasMemoryKey(asset.OrganizationID, asset.ID)
		if asset.ModelID != "" {
			model, exists := s.models[atlasMemoryKey(asset.OrganizationID, asset.ModelID)]
			if !exists || model.Status != "active" {
				return nil, atlas.ErrReferenceMissing
			}
		}
		if initialEvents[index].OrganizationID != asset.OrganizationID || initialEvents[index].AssetID != asset.ID {
			return nil, atlas.ErrInvalidInput
		}
		if _, exists := s.assets[key]; exists || s.assetIdentityConflict(asset, "") || batchAssetIdentityConflict(assets[:index], asset) {
			return nil, atlas.ErrConflict
		}
	}
	created := make([]domain.Asset, len(assets))
	for index, asset := range assets {
		key := atlasMemoryKey(asset.OrganizationID, asset.ID)
		s.assets[key] = cloneAsset(asset)
		s.lifecycle[key] = []domain.AssetLifecycleEvent{initialEvents[index]}
		created[index] = cloneAsset(asset)
	}
	return created, nil
}

func (s *MemoryAtlasStore) UpdateAsset(_ context.Context, asset domain.Asset, expectedRevision int64, lifecycleEvent *domain.AssetLifecycleEvent) (domain.Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := atlasMemoryKey(asset.OrganizationID, asset.ID)
	existing, ok := s.assets[key]
	if !ok {
		return domain.Asset{}, atlas.ErrNotFound
	}
	if existing.Revision != expectedRevision || asset.Revision != expectedRevision+1 || s.assetIdentityConflict(asset, asset.ID) {
		return domain.Asset{}, atlas.ErrConflict
	}
	if asset.ModelID != "" {
		model, exists := s.models[atlasMemoryKey(asset.OrganizationID, asset.ModelID)]
		if !exists || (model.Status != "active" && asset.ModelID != existing.ModelID) {
			return domain.Asset{}, atlas.ErrReferenceMissing
		}
	}
	s.assets[key] = cloneAsset(asset)
	if lifecycleEvent != nil {
		s.lifecycle[key] = append(s.lifecycle[key], *lifecycleEvent)
	}
	return cloneAsset(asset), nil
}

func (s *MemoryAtlasStore) ListAssetLifecycle(_ context.Context, organizationID, assetID string) ([]domain.AssetLifecycleEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := atlasMemoryKey(organizationID, assetID)
	if _, ok := s.assets[key]; !ok {
		return nil, atlas.ErrNotFound
	}
	items := append([]domain.AssetLifecycleEvent(nil), s.lifecycle[key]...)
	sortLifecycle(items)
	return items, nil
}

func (s *MemoryAtlasStore) GetAssetLifecycleEvent(_ context.Context, organizationID, eventID string) (domain.AssetLifecycleEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for key, events := range s.lifecycle {
		asset, exists := s.assets[key]
		if !exists || asset.OrganizationID != organizationID {
			continue
		}
		for _, event := range events {
			if event.ID == eventID {
				return event, nil
			}
		}
	}
	return domain.AssetLifecycleEvent{}, atlas.ErrNotFound
}

func (s *MemoryAtlasStore) ImportModel(_ context.Context, model domain.AssetModel) (domain.AssetModel, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := atlasMemoryKey(model.OrganizationID, model.ID)
	if existing, exists := s.models[key]; exists {
		existing.InstanceCount, model.InstanceCount = 0, 0
		if reflect.DeepEqual(existing, model) {
			return cloneAssetModel(existing), false, nil
		}
		return domain.AssetModel{}, false, atlas.ErrConflict
	}
	if s.modelIdentityConflict(model, "") {
		return domain.AssetModel{}, false, atlas.ErrConflict
	}
	model.InstanceCount = 0
	s.models[key] = cloneAssetModel(model)
	return cloneAssetModel(model), true, nil
}

func (s *MemoryAtlasStore) ImportAsset(_ context.Context, asset domain.Asset) (domain.Asset, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := atlasMemoryKey(asset.OrganizationID, asset.ID)
	if existing, exists := s.assets[key]; exists {
		if reflect.DeepEqual(existing, asset) {
			return cloneAsset(existing), false, nil
		}
		return domain.Asset{}, false, atlas.ErrConflict
	}
	if asset.ModelID != "" {
		if _, exists := s.models[atlasMemoryKey(asset.OrganizationID, asset.ModelID)]; !exists {
			return domain.Asset{}, false, atlas.ErrReferenceMissing
		}
	}
	if s.assetIdentityConflict(asset, "") {
		return domain.Asset{}, false, atlas.ErrConflict
	}
	s.assets[key] = cloneAsset(asset)
	s.lifecycle[key] = []domain.AssetLifecycleEvent{}
	return cloneAsset(asset), true, nil
}

func (s *MemoryAtlasStore) ImportAssetLifecycleEvent(_ context.Context, event domain.AssetLifecycleEvent) (domain.AssetLifecycleEvent, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := atlasMemoryKey(event.OrganizationID, event.AssetID)
	if _, exists := s.assets[key]; !exists {
		return domain.AssetLifecycleEvent{}, false, atlas.ErrReferenceMissing
	}
	for _, existing := range s.lifecycle[key] {
		if existing.ID == event.ID {
			if reflect.DeepEqual(existing, event) {
				return existing, false, nil
			}
			return domain.AssetLifecycleEvent{}, false, atlas.ErrConflict
		}
		if existing.Revision == event.Revision {
			return domain.AssetLifecycleEvent{}, false, atlas.ErrConflict
		}
	}
	s.lifecycle[key] = append(s.lifecycle[key], event)
	return event, true, nil
}

func (s *MemoryAtlasStore) modelIdentityConflict(candidate domain.AssetModel, excludingID string) bool {
	for _, existing := range s.models {
		if existing.OrganizationID != candidate.OrganizationID || existing.ID == excludingID {
			continue
		}
		if strings.EqualFold(existing.Manufacturer, candidate.Manufacturer) &&
			strings.EqualFold(existing.Name, candidate.Name) &&
			strings.EqualFold(existing.ModelNumber, candidate.ModelNumber) {
			return true
		}
	}
	return false
}

func (s *MemoryAtlasStore) modelInstanceCountLocked(organizationID, modelID string) int {
	count := 0
	for _, asset := range s.assets {
		if asset.OrganizationID == organizationID && asset.ModelID == modelID {
			count++
		}
	}
	return count
}

func (s *MemoryAtlasStore) assetIdentityConflict(candidate domain.Asset, excludingID string) bool {
	for _, existing := range s.assets {
		if existing.OrganizationID != candidate.OrganizationID || existing.ID == excludingID {
			continue
		}
		if candidate.AssetTag != "" && strings.EqualFold(existing.AssetTag, candidate.AssetTag) {
			return true
		}
		if candidate.SerialNumber != "" && strings.EqualFold(existing.SerialNumber, candidate.SerialNumber) {
			return true
		}
	}
	return false
}

func batchAssetIdentityConflict(existing []domain.Asset, candidate domain.Asset) bool {
	for _, asset := range existing {
		if asset.OrganizationID != candidate.OrganizationID {
			continue
		}
		if asset.ID == candidate.ID || (candidate.AssetTag != "" && strings.EqualFold(asset.AssetTag, candidate.AssetTag)) ||
			(candidate.SerialNumber != "" && strings.EqualFold(asset.SerialNumber, candidate.SerialNumber)) {
			return true
		}
	}
	return false
}

func cloneAsset(asset domain.Asset) domain.Asset {
	if asset.PurchaseDate != nil {
		value := *asset.PurchaseDate
		asset.PurchaseDate = &value
	}
	if asset.LifecycleStartDate != nil {
		value := *asset.LifecycleStartDate
		asset.LifecycleStartDate = &value
	}
	if asset.InstalledDate != nil {
		value := *asset.InstalledDate
		asset.InstalledDate = &value
	}
	if asset.ModelContext != nil {
		context := *asset.ModelContext
		if len(context.Specifications) > 0 {
			context.Specifications = make(map[string]string, len(asset.ModelContext.Specifications))
			for key, value := range asset.ModelContext.Specifications {
				context.Specifications[key] = value
			}
		}
		context.Overrides = append([]string{}, asset.ModelContext.Overrides...)
		asset.ModelContext = &context
	}
	if len(asset.Attributes) > 0 {
		attributes := make(map[string]string, len(asset.Attributes))
		for key, value := range asset.Attributes {
			attributes[key] = value
		}
		asset.Attributes = attributes
	}
	asset.Components = append([]domain.AssetComponent(nil), asset.Components...)
	return asset
}

func cloneAssetModel(model domain.AssetModel) domain.AssetModel {
	if len(model.Specifications) > 0 {
		specifications := make(map[string]string, len(model.Specifications))
		for key, value := range model.Specifications {
			specifications[key] = value
		}
		model.Specifications = specifications
	}
	if len(model.TemplateFields) > 0 {
		fields := make([]domain.AssetTemplateField, len(model.TemplateFields))
		for index, field := range model.TemplateFields {
			field.Options = append([]string(nil), field.Options...)
			fields[index] = field
		}
		model.TemplateFields = fields
	}
	return model
}
