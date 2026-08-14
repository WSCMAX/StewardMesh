package repository

// Requirements: REQ-REACH-001, REQ-EXCHANGE-001. Features: messaging.delivery, migration.packages. GitHub: #9, #12.

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/maxlemke/stewardmesh/internal/reach"
)

type MemoryReachStore struct {
	mu            sync.RWMutex
	providers     map[string]reach.Provider
	templates     map[string]reach.Template
	groups        map[string]reach.SubscriberGroup
	messages      map[string]reach.Message
	attempts      map[string][]reach.DeliveryAttempt
	providerTests map[string][]reach.ProviderTest
}

func NewMemoryReachStore() *MemoryReachStore {
	return &MemoryReachStore{
		providers: map[string]reach.Provider{}, templates: map[string]reach.Template{}, groups: map[string]reach.SubscriberGroup{},
		messages: map[string]reach.Message{}, attempts: map[string][]reach.DeliveryAttempt{}, providerTests: map[string][]reach.ProviderTest{},
	}
}

func reachKey(organizationID, id string) string { return organizationID + "\x00" + id }

func (s *MemoryReachStore) ExchangeSnapshot(_ context.Context, organizationID string, maximum int) (reach.ExchangeSnapshot, error) {
	if maximum < 1 {
		return reach.ExchangeSnapshot{}, reach.ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := reach.ExchangeSnapshot{Providers: []reach.Provider{}, Templates: []reach.Template{}, Groups: []reach.SubscriberGroup{}}
	for _, item := range s.providers {
		if item.OrganizationID == organizationID {
			result.Providers = append(result.Providers, item)
		}
	}
	for _, item := range s.templates {
		if item.OrganizationID == organizationID {
			result.Templates = append(result.Templates, item)
		}
	}
	for _, item := range s.groups {
		if item.OrganizationID == organizationID {
			result.Groups = append(result.Groups, cloneReachGroup(item))
		}
	}
	if len(result.Providers)+len(result.Templates)+len(result.Groups) > maximum {
		return reach.ExchangeSnapshot{}, reach.ErrTooLarge
	}
	sort.Slice(result.Providers, func(i, j int) bool { return result.Providers[i].ID < result.Providers[j].ID })
	sort.Slice(result.Templates, func(i, j int) bool { return result.Templates[i].ID < result.Templates[j].ID })
	sort.Slice(result.Groups, func(i, j int) bool { return result.Groups[i].ID < result.Groups[j].ID })
	return result, nil
}

func (s *MemoryReachStore) ListProviders(_ context.Context, organizationID string) ([]reach.Provider, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := []reach.Provider{}
	for _, item := range s.providers {
		if item.OrganizationID == organizationID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

func (s *MemoryReachStore) GetProvider(_ context.Context, organizationID, id string) (reach.Provider, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.providers[reachKey(organizationID, id)]
	if !ok {
		return reach.Provider{}, reach.ErrNotFound
	}
	return item, nil
}

func (s *MemoryReachStore) CreateProvider(_ context.Context, item reach.Provider) (reach.Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := reachKey(item.OrganizationID, item.ID)
	if _, exists := s.providers[key]; exists || memoryReachNameConflict(s.providers, item.OrganizationID, item.Name, "") || countReachOrganization(s.providers, item.OrganizationID) >= reach.MaximumProviders {
		return reach.Provider{}, reach.ErrConflict
	}
	s.providers[key] = item
	return item, nil
}

func (s *MemoryReachStore) UpdateProvider(_ context.Context, item reach.Provider, expectedRevision int64) (reach.Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := reachKey(item.OrganizationID, item.ID)
	existing, ok := s.providers[key]
	if !ok {
		return reach.Provider{}, reach.ErrNotFound
	}
	if existing.Revision != expectedRevision || memoryReachNameConflict(s.providers, item.OrganizationID, item.Name, item.ID) {
		return reach.Provider{}, reach.ErrConflict
	}
	s.providers[key] = item
	return item, nil
}

func (s *MemoryReachStore) ImportProvider(_ context.Context, item reach.Provider) (reach.Provider, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := reachKey(item.OrganizationID, item.ID)
	if existing, exists := s.providers[key]; exists {
		return existing, false, nil
	}
	if memoryReachNameConflict(s.providers, item.OrganizationID, item.Name, "") || countReachOrganization(s.providers, item.OrganizationID) >= reach.MaximumProviders {
		return reach.Provider{}, false, reach.ErrConflict
	}
	s.providers[key] = item
	return item, true, nil
}

func (s *MemoryReachStore) ListTemplates(_ context.Context, organizationID string) ([]reach.Template, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := []reach.Template{}
	for _, item := range s.templates {
		if item.OrganizationID == organizationID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

func (s *MemoryReachStore) GetTemplate(_ context.Context, organizationID, id string) (reach.Template, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.templates[reachKey(organizationID, id)]
	if !ok {
		return reach.Template{}, reach.ErrNotFound
	}
	return item, nil
}

func (s *MemoryReachStore) CreateTemplate(_ context.Context, item reach.Template) (reach.Template, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := reachKey(item.OrganizationID, item.ID)
	if _, exists := s.templates[key]; exists || memoryReachNameConflict(s.templates, item.OrganizationID, item.Name, "") || countReachOrganization(s.templates, item.OrganizationID) >= reach.MaximumTemplates {
		return reach.Template{}, reach.ErrConflict
	}
	s.templates[key] = item
	return item, nil
}

func (s *MemoryReachStore) UpdateTemplate(_ context.Context, item reach.Template, expectedRevision int64) (reach.Template, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := reachKey(item.OrganizationID, item.ID)
	existing, ok := s.templates[key]
	if !ok {
		return reach.Template{}, reach.ErrNotFound
	}
	if existing.Revision != expectedRevision || memoryReachNameConflict(s.templates, item.OrganizationID, item.Name, item.ID) {
		return reach.Template{}, reach.ErrConflict
	}
	s.templates[key] = item
	return item, nil
}

func (s *MemoryReachStore) ImportTemplate(_ context.Context, item reach.Template) (reach.Template, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := reachKey(item.OrganizationID, item.ID)
	if existing, exists := s.templates[key]; exists {
		return existing, false, nil
	}
	if memoryReachNameConflict(s.templates, item.OrganizationID, item.Name, "") || countReachOrganization(s.templates, item.OrganizationID) >= reach.MaximumTemplates {
		return reach.Template{}, false, reach.ErrConflict
	}
	s.templates[key] = item
	return item, true, nil
}

func (s *MemoryReachStore) ListGroups(_ context.Context, organizationID string) ([]reach.SubscriberGroup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := []reach.SubscriberGroup{}
	for _, item := range s.groups {
		if item.OrganizationID == organizationID {
			items = append(items, cloneReachGroup(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

func (s *MemoryReachStore) GetGroup(_ context.Context, organizationID, id string) (reach.SubscriberGroup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.groups[reachKey(organizationID, id)]
	if !ok {
		return reach.SubscriberGroup{}, reach.ErrNotFound
	}
	return cloneReachGroup(item), nil
}

func (s *MemoryReachStore) CreateGroup(_ context.Context, item reach.SubscriberGroup) (reach.SubscriberGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := reachKey(item.OrganizationID, item.ID)
	if _, exists := s.groups[key]; exists || memoryReachNameConflict(s.groups, item.OrganizationID, item.Name, "") || countReachOrganization(s.groups, item.OrganizationID) >= reach.MaximumGroups {
		return reach.SubscriberGroup{}, reach.ErrConflict
	}
	s.groups[key] = cloneReachGroup(item)
	return cloneReachGroup(item), nil
}

func (s *MemoryReachStore) UpdateGroup(_ context.Context, item reach.SubscriberGroup, expectedRevision int64) (reach.SubscriberGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := reachKey(item.OrganizationID, item.ID)
	existing, ok := s.groups[key]
	if !ok {
		return reach.SubscriberGroup{}, reach.ErrNotFound
	}
	if existing.Revision != expectedRevision || memoryReachNameConflict(s.groups, item.OrganizationID, item.Name, item.ID) {
		return reach.SubscriberGroup{}, reach.ErrConflict
	}
	s.groups[key] = cloneReachGroup(item)
	return cloneReachGroup(item), nil
}

func (s *MemoryReachStore) ImportGroup(_ context.Context, item reach.SubscriberGroup) (reach.SubscriberGroup, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := reachKey(item.OrganizationID, item.ID)
	if existing, exists := s.groups[key]; exists {
		return cloneReachGroup(existing), false, nil
	}
	if _, ok := s.providers[reachKey(item.OrganizationID, item.ProviderID)]; !ok {
		return reach.SubscriberGroup{}, false, reach.ErrNotFound
	}
	if _, ok := s.templates[reachKey(item.OrganizationID, item.TemplateID)]; !ok {
		return reach.SubscriberGroup{}, false, reach.ErrNotFound
	}
	if memoryReachNameConflict(s.groups, item.OrganizationID, item.Name, "") || countReachOrganization(s.groups, item.OrganizationID) >= reach.MaximumGroups {
		return reach.SubscriberGroup{}, false, reach.ErrConflict
	}
	s.groups[key] = cloneReachGroup(item)
	return cloneReachGroup(item), true, nil
}

func (s *MemoryReachStore) ListMessages(_ context.Context, organizationID string, limit int) ([]reach.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := []reach.Message{}
	for _, item := range s.messages {
		if item.OrganizationID == organizationID {
			items = append(items, cloneReachMessage(item))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *MemoryReachStore) GetMessage(_ context.Context, organizationID, id string) (reach.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.messages[reachKey(organizationID, id)]
	if !ok {
		return reach.Message{}, reach.ErrNotFound
	}
	return cloneReachMessage(item), nil
}

func (s *MemoryReachStore) CreateMessage(_ context.Context, item reach.Message) (reach.Message, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := reachKey(item.OrganizationID, item.ID)
	if existing, exists := s.messages[key]; exists {
		if existing.SourceKind == item.SourceKind && existing.SourceID == item.SourceID && existing.GroupID == item.GroupID && existing.ProviderID == item.ProviderID {
			return cloneReachMessage(existing), false, nil
		}
		return reach.Message{}, false, reach.ErrConflict
	}
	if countReachOrganization(s.messages, item.OrganizationID) >= reach.MaximumMessages {
		return reach.Message{}, false, reach.ErrConflict
	}
	s.messages[key] = cloneReachMessage(item)
	return cloneReachMessage(item), true, nil
}

func (s *MemoryReachStore) ClaimMessage(_ context.Context, organizationID, id, expectedStatus string, expectedAttempts int, claimToken string, claimedAt, staleBefore time.Time) (reach.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := reachKey(organizationID, id)
	item, ok := s.messages[key]
	if !ok {
		return reach.Message{}, reach.ErrNotFound
	}
	if item.Status != expectedStatus || item.Attempts != expectedAttempts ||
		(item.ClaimedAt != nil && item.ClaimedAt.After(staleBefore)) {
		return reach.Message{}, reach.ErrConflict
	}
	item.ClaimToken, item.ClaimedAt, item.UpdatedAt = claimToken, &claimedAt, claimedAt
	s.messages[key] = cloneReachMessage(item)
	return cloneReachMessage(item), nil
}

func (s *MemoryReachStore) RecordAttempt(_ context.Context, item reach.Message, expectedAttempts int, attempt reach.DeliveryAttempt) (reach.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := reachKey(item.OrganizationID, item.ID)
	existing, ok := s.messages[key]
	if !ok {
		return reach.Message{}, reach.ErrNotFound
	}
	if existing.Attempts != expectedAttempts || item.Attempts != expectedAttempts+1 || existing.ClaimToken == "" || item.ClaimToken != existing.ClaimToken {
		return reach.Message{}, reach.ErrConflict
	}
	for _, current := range s.attempts[key] {
		if current.ID == attempt.ID || current.Attempt == attempt.Attempt {
			return reach.Message{}, reach.ErrConflict
		}
	}
	item.ClaimToken, item.ClaimedAt = "", nil
	s.messages[key] = cloneReachMessage(item)
	s.attempts[key] = append(s.attempts[key], attempt)
	return cloneReachMessage(item), nil
}

func (s *MemoryReachStore) ListAttempts(_ context.Context, organizationID, messageID string) ([]reach.DeliveryAttempt, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := reachKey(organizationID, messageID)
	if _, exists := s.messages[key]; !exists {
		return nil, reach.ErrNotFound
	}
	items := append([]reach.DeliveryAttempt{}, s.attempts[key]...)
	sort.Slice(items, func(i, j int) bool { return items[i].Attempt < items[j].Attempt })
	return items, nil
}

func (s *MemoryReachStore) CreateProviderTest(_ context.Context, item reach.ProviderTest) (reach.ProviderTest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := reachKey(item.OrganizationID, item.ProviderID)
	for _, current := range s.providerTests[key] {
		if current.ID == item.ID {
			return reach.ProviderTest{}, reach.ErrConflict
		}
	}
	s.providerTests[key] = append(s.providerTests[key], item)
	return item, nil
}

func (s *MemoryReachStore) ListProviderTests(_ context.Context, organizationID, providerID string) ([]reach.ProviderTest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, exists := s.providers[reachKey(organizationID, providerID)]; !exists {
		return nil, reach.ErrNotFound
	}
	items := append([]reach.ProviderTest{}, s.providerTests[reachKey(organizationID, providerID)]...)
	sort.Slice(items, func(i, j int) bool { return items[i].TestedAt.After(items[j].TestedAt) })
	return items, nil
}

type namedReachRecord interface {
	reach.Provider | reach.Template | reach.SubscriberGroup
}

func memoryReachNameConflict[T namedReachRecord](items map[string]T, organizationID, name, excludedID string) bool {
	for _, value := range items {
		var candidateOrganization, candidateID, candidateName string
		switch item := any(value).(type) {
		case reach.Provider:
			candidateOrganization, candidateID, candidateName = item.OrganizationID, item.ID, item.Name
		case reach.Template:
			candidateOrganization, candidateID, candidateName = item.OrganizationID, item.ID, item.Name
		case reach.SubscriberGroup:
			candidateOrganization, candidateID, candidateName = item.OrganizationID, item.ID, item.Name
		}
		if candidateOrganization == organizationID && candidateID != excludedID && strings.EqualFold(strings.TrimSpace(candidateName), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

type organizationReachRecord interface {
	reach.Provider | reach.Template | reach.SubscriberGroup | reach.Message
}

func countReachOrganization[T organizationReachRecord](items map[string]T, organizationID string) int {
	count := 0
	for _, value := range items {
		var candidate string
		switch item := any(value).(type) {
		case reach.Provider:
			candidate = item.OrganizationID
		case reach.Template:
			candidate = item.OrganizationID
		case reach.SubscriberGroup:
			candidate = item.OrganizationID
		case reach.Message:
			candidate = item.OrganizationID
		}
		if candidate == organizationID {
			count++
		}
	}
	return count
}

func cloneReachGroup(item reach.SubscriberGroup) reach.SubscriberGroup {
	item.Recipients = append([]reach.Recipient(nil), item.Recipients...)
	return item
}

func cloneReachMessage(item reach.Message) reach.Message {
	item.Recipients = append([]reach.Recipient(nil), item.Recipients...)
	if item.NextAttemptAt != nil {
		next := *item.NextAttemptAt
		item.NextAttemptAt = &next
	}
	if item.ClaimedAt != nil {
		claimed := *item.ClaimedAt
		item.ClaimedAt = &claimed
	}
	return item
}
