package repository

// Requirement: REQ-THREADS-001. Feature: goals.tags.

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/maxlemke/stewardmesh/internal/threads"
)

type MemoryThreadsStore struct {
	mu        sync.RWMutex
	tags      map[string]threads.Tag
	goals     map[string]threads.Goal
	tagRules  map[string]threads.TagRule
	goalLinks map[string]threads.GoalLink
}

var _ threads.Store = (*MemoryThreadsStore)(nil)

func NewMemoryThreadsStore() *MemoryThreadsStore {
	return &MemoryThreadsStore{
		tags: make(map[string]threads.Tag), goals: make(map[string]threads.Goal),
		tagRules: make(map[string]threads.TagRule), goalLinks: make(map[string]threads.GoalLink),
	}
}

func threadsRecordKey(organizationID, id string) string {
	return organizationID + "\x00" + id
}

func threadsTargetKey(organizationID string, targetType threads.TargetType, targetID, relationID string) string {
	return organizationID + "\x00" + string(targetType) + "\x00" + targetID + "\x00" + relationID
}

func (s *MemoryThreadsStore) ListTags(_ context.Context, organizationID string) ([]threads.Tag, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]threads.Tag, 0)
	for _, tag := range s.tags {
		if tag.OrganizationID == organizationID {
			items = append(items, tag)
		}
	}
	sortTags(items)
	return items, nil
}

func (s *MemoryThreadsStore) GetTag(_ context.Context, organizationID, id string) (threads.Tag, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tag, ok := s.tags[threadsRecordKey(organizationID, id)]
	if !ok {
		return threads.Tag{}, threads.ErrNotFound
	}
	return tag, nil
}

func (s *MemoryThreadsStore) CreateTag(_ context.Context, tag threads.Tag) (threads.Tag, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := threadsRecordKey(tag.OrganizationID, tag.ID)
	if _, exists := s.tags[key]; exists || s.tagNameConflict(tag, "") {
		return threads.Tag{}, threads.ErrConflict
	}
	s.tags[key] = tag
	return tag, nil
}

func (s *MemoryThreadsStore) UpdateTag(_ context.Context, tag threads.Tag, expectedRevision int64) (threads.Tag, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := threadsRecordKey(tag.OrganizationID, tag.ID)
	existing, ok := s.tags[key]
	if !ok {
		return threads.Tag{}, threads.ErrNotFound
	}
	if existing.Revision != expectedRevision || tag.Revision != expectedRevision+1 || s.tagNameConflict(tag, tag.ID) {
		return threads.Tag{}, threads.ErrConflict
	}
	s.tags[key] = tag
	return tag, nil
}

func (s *MemoryThreadsStore) ListGoals(_ context.Context, organizationID string) ([]threads.Goal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]threads.Goal, 0)
	for _, goal := range s.goals {
		if goal.OrganizationID == organizationID {
			items = append(items, goal)
		}
	}
	sortGoals(items)
	return items, nil
}

func (s *MemoryThreadsStore) GetGoal(_ context.Context, organizationID, id string) (threads.Goal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	goal, ok := s.goals[threadsRecordKey(organizationID, id)]
	if !ok {
		return threads.Goal{}, threads.ErrNotFound
	}
	return goal, nil
}

func (s *MemoryThreadsStore) CreateGoal(_ context.Context, goal threads.Goal) (threads.Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := threadsRecordKey(goal.OrganizationID, goal.ID)
	if _, exists := s.goals[key]; exists || s.goalNameConflict(goal, "") {
		return threads.Goal{}, threads.ErrConflict
	}
	s.goals[key] = goal
	return goal, nil
}

func (s *MemoryThreadsStore) UpdateGoal(_ context.Context, goal threads.Goal, expectedRevision int64) (threads.Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := threadsRecordKey(goal.OrganizationID, goal.ID)
	existing, ok := s.goals[key]
	if !ok {
		return threads.Goal{}, threads.ErrNotFound
	}
	if existing.Revision != expectedRevision || goal.Revision != expectedRevision+1 || s.goalNameConflict(goal, goal.ID) {
		return threads.Goal{}, threads.ErrConflict
	}
	s.goals[key] = goal
	return goal, nil
}

func (s *MemoryThreadsStore) ListTagRules(_ context.Context, organizationID string, targetType threads.TargetType, targetID string) ([]threads.TagRule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]threads.TagRule, 0)
	for _, rule := range s.tagRules {
		if rule.OrganizationID == organizationID && rule.TargetType == targetType && rule.TargetID == targetID {
			items = append(items, rule)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].TagID < items[j].TagID })
	return items, nil
}

func (s *MemoryThreadsStore) PutTagRule(_ context.Context, rule threads.TagRule, expectedRevision int64) (threads.TagRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := threadsTargetKey(rule.OrganizationID, rule.TargetType, rule.TargetID, rule.TagID)
	existing, exists := s.tagRules[key]
	if expectedRevision == 0 {
		if exists || rule.Revision != 1 {
			return threads.TagRule{}, threads.ErrConflict
		}
	} else {
		if !exists || existing.Revision != expectedRevision || rule.Revision != expectedRevision+1 {
			return threads.TagRule{}, threads.ErrConflict
		}
		rule.CreatedAt = existing.CreatedAt
	}
	s.tagRules[key] = rule
	return rule, nil
}

func (s *MemoryThreadsStore) DeleteTagRule(_ context.Context, organizationID string, targetType threads.TargetType, targetID, tagID string, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := threadsTargetKey(organizationID, targetType, targetID, tagID)
	existing, exists := s.tagRules[key]
	if !exists {
		return threads.ErrNotFound
	}
	if existing.Revision != expectedRevision {
		return threads.ErrConflict
	}
	delete(s.tagRules, key)
	return nil
}

func (s *MemoryThreadsStore) ListGoalLinks(_ context.Context, organizationID string, targetType threads.TargetType, targetID string) ([]threads.GoalLink, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]threads.GoalLink, 0)
	for _, link := range s.goalLinks {
		if link.OrganizationID == organizationID && link.TargetType == targetType && link.TargetID == targetID {
			items = append(items, link)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].GoalID < items[j].GoalID })
	return items, nil
}

func (s *MemoryThreadsStore) CreateGoalLink(_ context.Context, link threads.GoalLink) (threads.GoalLink, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := threadsTargetKey(link.OrganizationID, link.TargetType, link.TargetID, link.GoalID)
	if existing, exists := s.goalLinks[key]; exists {
		return existing, false, nil
	}
	s.goalLinks[key] = link
	return link, true, nil
}

func (s *MemoryThreadsStore) DeleteGoalLink(_ context.Context, organizationID string, targetType threads.TargetType, targetID, goalID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := threadsTargetKey(organizationID, targetType, targetID, goalID)
	if _, exists := s.goalLinks[key]; !exists {
		return false, nil
	}
	delete(s.goalLinks, key)
	return true, nil
}

func (s *MemoryThreadsStore) tagNameConflict(candidate threads.Tag, excludingID string) bool {
	for _, existing := range s.tags {
		if existing.OrganizationID == candidate.OrganizationID && existing.ID != excludingID && strings.EqualFold(existing.Name, candidate.Name) {
			return true
		}
	}
	return false
}

func (s *MemoryThreadsStore) goalNameConflict(candidate threads.Goal, excludingID string) bool {
	for _, existing := range s.goals {
		if existing.OrganizationID == candidate.OrganizationID && existing.ID != excludingID && strings.EqualFold(existing.Name, candidate.Name) {
			return true
		}
	}
	return false
}

func sortTags(items []threads.Tag) {
	sort.Slice(items, func(i, j int) bool {
		left, right := strings.ToLower(items[i].Name), strings.ToLower(items[j].Name)
		if left == right {
			return items[i].ID < items[j].ID
		}
		return left < right
	})
}

func sortGoals(items []threads.Goal) {
	sort.Slice(items, func(i, j int) bool {
		left, right := strings.ToLower(items[i].Name), strings.ToLower(items[j].Name)
		if left == right {
			return items[i].ID < items[j].ID
		}
		return left < right
	})
}
