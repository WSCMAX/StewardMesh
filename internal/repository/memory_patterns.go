package repository

// In-memory Patterns adapter. Requirement: REQ-PATTERNS-001. Feature: templates.schemas.

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/maxlemke/stewardmesh/internal/patterns"
)

type MemoryPatternsStore struct {
	mu       sync.RWMutex
	versions map[string][]patterns.Template
}

var _ patterns.Store = (*MemoryPatternsStore)(nil)

func NewMemoryPatternsStore() *MemoryPatternsStore {
	return &MemoryPatternsStore{versions: make(map[string][]patterns.Template)}
}

func patternsMemoryKey(organizationID, id string) string { return organizationID + "\x00" + id }

func (s *MemoryPatternsStore) ListTemplates(_ context.Context, organizationID string, query patterns.ListQuery) ([]patterns.Template, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]patterns.Template, 0)
	for _, versions := range s.versions {
		if len(versions) == 0 {
			continue
		}
		latest := versions[len(versions)-1]
		if latest.OrganizationID != organizationID || query.RecordType != "" && latest.RecordType != query.RecordType || !query.IncludeRetired && latest.Status == patterns.StatusRetired {
			continue
		}
		items = append(items, cloneMemoryTemplate(latest))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].RecordType != items[j].RecordType {
			return items[i].RecordType < items[j].RecordType
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	return items, nil
}

func (s *MemoryPatternsStore) GetTemplate(_ context.Context, organizationID, id string, version int64) (patterns.Template, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	versions := s.versions[patternsMemoryKey(organizationID, id)]
	if len(versions) == 0 {
		return patterns.Template{}, patterns.ErrNotFound
	}
	if version == 0 {
		return cloneMemoryTemplate(versions[len(versions)-1]), nil
	}
	for _, candidate := range versions {
		if candidate.Version == version {
			return cloneMemoryTemplate(candidate), nil
		}
	}
	return patterns.Template{}, patterns.ErrNotFound
}

func (s *MemoryPatternsStore) CreateTemplate(_ context.Context, template patterns.Template) (patterns.Template, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := patternsMemoryKey(template.OrganizationID, template.ID)
	if len(s.versions[key]) > 0 {
		return patterns.Template{}, patterns.ErrConflict
	}
	for _, versions := range s.versions {
		latest := versions[len(versions)-1]
		if latest.OrganizationID == template.OrganizationID && strings.EqualFold(latest.Name, template.Name) {
			return patterns.Template{}, patterns.ErrConflict
		}
	}
	s.versions[key] = []patterns.Template{cloneMemoryTemplate(template)}
	return cloneMemoryTemplate(template), nil
}

func (s *MemoryPatternsStore) CreateVersion(_ context.Context, template patterns.Template) (patterns.Template, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := patternsMemoryKey(template.OrganizationID, template.ID)
	versions := s.versions[key]
	if len(versions) == 0 {
		return patterns.Template{}, patterns.ErrNotFound
	}
	if versions[len(versions)-1].Version+1 != template.Version || versions[len(versions)-1].RecordType != template.RecordType || versions[len(versions)-1].Name != template.Name {
		return patterns.Template{}, patterns.ErrConflict
	}
	s.versions[key] = append(versions, cloneMemoryTemplate(template))
	return cloneMemoryTemplate(template), nil
}

func cloneMemoryTemplate(template patterns.Template) patterns.Template {
	template.Fields = append([]patterns.Field(nil), template.Fields...)
	for index := range template.Fields {
		template.Fields[index].Options = append([]string(nil), template.Fields[index].Options...)
		if template.Fields[index].Minimum != nil {
			value := *template.Fields[index].Minimum
			template.Fields[index].Minimum = &value
		}
		if template.Fields[index].Maximum != nil {
			value := *template.Fields[index].Maximum
			template.Fields[index].Maximum = &value
		}
	}
	return template
}
