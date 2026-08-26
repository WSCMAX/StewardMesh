package repository

// Requirement: REQ-PEOPLE-001. Feature: identity.directory.

import (
	"context"
	"sort"
	"time"

	"github.com/maxlemke/stewardmesh/internal/people"
)

func (s *MemoryPeopleStore) CreateCheckoutGroup(_ context.Context, group people.CheckoutGroup) (people.CheckoutGroup, error) {
	if group.ID == "" || group.OrganizationID == "" || group.Name == "" || len(group.MemberIDs) == 0 {
		return people.CheckoutGroup{}, people.ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.checkoutGroups[group.ID]; exists {
		return people.CheckoutGroup{}, people.ErrConflict
	}
	for _, memberID := range group.MemberIDs {
		identity, exists := s.identities[memberID]
		if !exists || identity.OrganizationID != group.OrganizationID {
			return people.CheckoutGroup{}, people.ErrReferenceMissing
		}
	}
	cloned := cloneCheckoutGroup(group)
	s.checkoutGroups[group.ID] = cloned
	return cloneCheckoutGroup(cloned), nil
}

func (s *MemoryPeopleStore) GetCheckoutGroup(_ context.Context, organizationID, id string) (people.CheckoutGroup, error) {
	if organizationID == "" || id == "" {
		return people.CheckoutGroup{}, people.ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	group, exists := s.checkoutGroups[id]
	if !exists || group.OrganizationID != organizationID {
		return people.CheckoutGroup{}, people.ErrNotFound
	}
	return cloneCheckoutGroup(group), nil
}

func (s *MemoryPeopleStore) ListCheckoutGroups(_ context.Context, organizationID string) ([]people.CheckoutGroup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]people.CheckoutGroup, 0)
	for _, group := range s.checkoutGroups {
		if group.OrganizationID == organizationID {
			result = append(result, cloneCheckoutGroup(group))
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name == result[j].Name {
			return result[i].ID < result[j].ID
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func (s *MemoryPeopleStore) AddCheckoutGroupMember(_ context.Context, organizationID, groupID, identityID string) (people.CheckoutGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	group, exists := s.checkoutGroups[groupID]
	if !exists || group.OrganizationID != organizationID {
		return people.CheckoutGroup{}, people.ErrNotFound
	}
	identity, exists := s.identities[identityID]
	if !exists || identity.OrganizationID != organizationID {
		return people.CheckoutGroup{}, people.ErrReferenceMissing
	}
	for _, memberID := range group.MemberIDs {
		if memberID == identityID {
			return cloneCheckoutGroup(group), nil
		}
	}
	group.MemberIDs = append(append([]string{}, group.MemberIDs...), identityID)
	group.UpdatedAt = time.Now().UTC()
	group.Revision++
	s.checkoutGroups[groupID] = group
	return cloneCheckoutGroup(group), nil
}

func (s *MemoryPeopleStore) CreateBulkCheckout(_ context.Context, item people.BulkCheckout) (people.BulkCheckout, error) {
	if item.ID == "" || item.OrganizationID == "" || item.AssigneeID == "" {
		return people.BulkCheckout{}, people.ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.bulkCheckouts[item.ID]; exists {
		return people.BulkCheckout{}, people.ErrConflict
	}
	cloned := cloneBulkCheckout(item)
	s.bulkCheckouts[item.ID] = cloned
	return cloneBulkCheckout(cloned), nil
}

func (s *MemoryPeopleStore) GetBulkCheckout(_ context.Context, organizationID, id string) (people.BulkCheckout, error) {
	if organizationID == "" || id == "" {
		return people.BulkCheckout{}, people.ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, exists := s.bulkCheckouts[id]
	if !exists || item.OrganizationID != organizationID {
		return people.BulkCheckout{}, people.ErrNotFound
	}
	return cloneBulkCheckout(item), nil
}

func (s *MemoryPeopleStore) ListBulkCheckouts(_ context.Context, organizationID string) ([]people.BulkCheckout, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]people.BulkCheckout, 0)
	for _, item := range s.bulkCheckouts {
		if item.OrganizationID == organizationID {
			result = append(result, cloneBulkCheckout(item))
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID > result[j].ID
		}
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}

func (s *MemoryPeopleStore) DeleteBulkCheckout(_ context.Context, organizationID, id string) error {
	if organizationID == "" || id == "" {
		return people.ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, exists := s.bulkCheckouts[id]
	if !exists || item.OrganizationID != organizationID {
		return people.ErrNotFound
	}
	for assignmentID, assignment := range s.assignments {
		if assignment.OrganizationID == organizationID && assignment.BulkCheckoutID == id {
			delete(s.assignments, assignmentID)
		}
	}
	delete(s.bulkCheckouts, id)
	return nil
}

func cloneCheckoutGroup(group people.CheckoutGroup) people.CheckoutGroup {
	group.MemberIDs = append([]string{}, group.MemberIDs...)
	sort.Strings(group.MemberIDs)
	return group
}

func cloneBulkCheckout(item people.BulkCheckout) people.BulkCheckout {
	if item.DueAt != nil {
		dueAt := *item.DueAt
		item.DueAt = &dueAt
	}
	return item
}
