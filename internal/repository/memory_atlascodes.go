package repository

// In-memory Atlas Codes adapter.
// Requirement: REQ-ATLAS-CODES-001. Feature: inventory.identifiers.

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/atlascodes"
)

var atlasCodesStableIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type MemoryAtlasCodesStore struct {
	mu          sync.RWMutex
	identifiers map[string]atlascodes.Identifier
}

var _ atlascodes.Store = (*MemoryAtlasCodesStore)(nil)

func NewMemoryAtlasCodesStore() *MemoryAtlasCodesStore {
	return &MemoryAtlasCodesStore{identifiers: make(map[string]atlascodes.Identifier)}
}

func atlasCodesMemoryKey(organizationID, identifierID string) string {
	return organizationID + "\x00" + identifierID
}

func (s *MemoryAtlasCodesStore) ListIdentifiers(_ context.Context, organizationID, assetID string) ([]atlascodes.Identifier, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]atlascodes.Identifier, 0)
	for _, item := range s.identifiers {
		if item.OrganizationID == organizationID && item.AssetID == assetID {
			items = append(items, cloneAtlasCodesIdentifier(item))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items, nil
}

func (s *MemoryAtlasCodesStore) GetIdentifier(_ context.Context, organizationID, assetID, identifierID string) (atlascodes.Identifier, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, exists := s.identifiers[atlasCodesMemoryKey(organizationID, identifierID)]
	if !exists || item.AssetID != assetID {
		return atlascodes.Identifier{}, atlascodes.ErrNotFound
	}
	return cloneAtlasCodesIdentifier(item), nil
}

func (s *MemoryAtlasCodesStore) GetIdentifierByID(_ context.Context, organizationID, identifierID string) (atlascodes.Identifier, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, exists := s.identifiers[atlasCodesMemoryKey(organizationID, identifierID)]
	if !exists {
		return atlascodes.Identifier{}, atlascodes.ErrNotFound
	}
	return cloneAtlasCodesIdentifier(item), nil
}

func (s *MemoryAtlasCodesStore) ResolveIdentifier(_ context.Context, organizationID string, symbology atlascodes.Symbology, normalizedValue string) (atlascodes.Identifier, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.identifiers {
		if item.OrganizationID == organizationID && item.Symbology == symbology &&
			item.NormalizedValue == normalizedValue && item.Status == atlascodes.StatusActive {
			return cloneAtlasCodesIdentifier(item), nil
		}
	}
	return atlascodes.Identifier{}, atlascodes.ErrNotFound
}

func (s *MemoryAtlasCodesStore) CreateIdentifier(_ context.Context, item atlascodes.Identifier) (atlascodes.Identifier, bool, error) {
	if !validNewAtlasCodesIdentifier(item, false) {
		return atlascodes.Identifier{}, false, atlascodes.ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := atlasCodesMemoryKey(item.OrganizationID, item.ID)
	if existing, exists := s.identifiers[key]; exists {
		if sameAtlasCodesIntent(existing, item) {
			return cloneAtlasCodesIdentifier(existing), false, nil
		}
		return atlascodes.Identifier{}, false, atlascodes.ErrConflict
	}
	if s.activeAtlasCodesConflict(item, "") {
		return atlascodes.Identifier{}, false, atlascodes.ErrConflict
	}
	s.identifiers[key] = cloneAtlasCodesIdentifier(item)
	return cloneAtlasCodesIdentifier(item), true, nil
}

func (s *MemoryAtlasCodesStore) ReplaceIdentifier(
	_ context.Context,
	organizationID, assetID, identifierID string,
	expectedRevision int64,
	replacement atlascodes.Identifier,
	changedAt time.Time,
) (atlascodes.Identifier, bool, error) {
	if strings.TrimSpace(organizationID) == "" || strings.TrimSpace(assetID) == "" || strings.TrimSpace(identifierID) == "" ||
		expectedRevision < 1 || changedAt.IsZero() || replacement.OrganizationID != organizationID ||
		replacement.AssetID != assetID || replacement.SupersedesID != identifierID || !validNewAtlasCodesIdentifier(replacement, true) {
		return atlascodes.Identifier{}, false, atlascodes.ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previousKey := atlasCodesMemoryKey(organizationID, identifierID)
	previous, exists := s.identifiers[previousKey]
	if !exists || previous.AssetID != assetID {
		return atlascodes.Identifier{}, false, atlascodes.ErrNotFound
	}
	if previous.Status == atlascodes.StatusReplaced {
		if previous.Revision <= 1 || expectedRevision != previous.Revision-1 || previous.ReplacedByID != replacement.ID {
			return atlascodes.Identifier{}, false, atlascodes.ErrConflict
		}
		existing, exists := s.identifiers[atlasCodesMemoryKey(organizationID, replacement.ID)]
		if !exists || existing.AssetID != assetID || !sameAtlasCodesIntent(existing, replacement) {
			return atlascodes.Identifier{}, false, atlascodes.ErrConflict
		}
		return cloneAtlasCodesIdentifier(existing), false, nil
	}
	if previous.Status != atlascodes.StatusActive || previous.Revision != expectedRevision {
		return atlascodes.Identifier{}, false, atlascodes.ErrConflict
	}
	if changedAt.Before(previous.UpdatedAt) {
		return atlascodes.Identifier{}, false, atlascodes.ErrInvalidInput
	}
	replacementKey := atlasCodesMemoryKey(organizationID, replacement.ID)
	if _, exists := s.identifiers[replacementKey]; exists || s.activeAtlasCodesConflict(replacement, previousKey) {
		return atlascodes.Identifier{}, false, atlascodes.ErrConflict
	}
	transitioned := cloneAtlasCodesIdentifier(previous)
	transitioned.Status = atlascodes.StatusReplaced
	transitioned.ReplacedByID = replacement.ID
	transitioned.Revision = previous.Revision + 1
	transitioned.UpdatedAt = changedAt
	transitioned.UpdatedBy = replacement.CreatedBy
	transitioned.UpdatedCorrelationID = replacement.CreatedCorrelationID
	transitioned.DeactivatedAt = cloneAtlasCodesTime(&changedAt)
	s.identifiers[previousKey] = transitioned
	s.identifiers[replacementKey] = cloneAtlasCodesIdentifier(replacement)
	return cloneAtlasCodesIdentifier(replacement), true, nil
}

func (s *MemoryAtlasCodesStore) DeactivateIdentifier(
	_ context.Context,
	organizationID, assetID, identifierID string,
	expectedRevision int64,
	deactivatedAt time.Time,
	actorID, correlationID string,
) (atlascodes.Identifier, bool, error) {
	if strings.TrimSpace(organizationID) == "" || strings.TrimSpace(assetID) == "" || strings.TrimSpace(identifierID) == "" ||
		expectedRevision < 1 || deactivatedAt.IsZero() || !validAtlasCodesProvenance(actorID, correlationID) {
		return atlascodes.Identifier{}, false, atlascodes.ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := atlasCodesMemoryKey(organizationID, identifierID)
	item, exists := s.identifiers[key]
	if !exists || item.AssetID != assetID {
		return atlascodes.Identifier{}, false, atlascodes.ErrNotFound
	}
	if item.Status == atlascodes.StatusDeactivated {
		if item.Revision <= 1 || expectedRevision != item.Revision-1 {
			return atlascodes.Identifier{}, false, atlascodes.ErrConflict
		}
		return cloneAtlasCodesIdentifier(item), false, nil
	}
	if item.Status != atlascodes.StatusActive || item.Revision != expectedRevision {
		return atlascodes.Identifier{}, false, atlascodes.ErrConflict
	}
	if deactivatedAt.Before(item.UpdatedAt) {
		return atlascodes.Identifier{}, false, atlascodes.ErrInvalidInput
	}
	item.Status = atlascodes.StatusDeactivated
	item.Revision++
	item.UpdatedAt = deactivatedAt
	item.UpdatedBy = strings.TrimSpace(actorID)
	item.UpdatedCorrelationID = strings.TrimSpace(correlationID)
	item.DeactivatedAt = cloneAtlasCodesTime(&deactivatedAt)
	s.identifiers[key] = cloneAtlasCodesIdentifier(item)
	return cloneAtlasCodesIdentifier(item), true, nil
}

func (s *MemoryAtlasCodesStore) activeAtlasCodesConflict(candidate atlascodes.Identifier, excludingKey string) bool {
	for key, existing := range s.identifiers {
		if key == excludingKey || existing.OrganizationID != candidate.OrganizationID || existing.Status != atlascodes.StatusActive {
			continue
		}
		if existing.Symbology == candidate.Symbology && existing.NormalizedValue == candidate.NormalizedValue {
			return true
		}
		if candidate.Primary && existing.Primary && existing.AssetID == candidate.AssetID {
			return true
		}
	}
	return false
}

func validNewAtlasCodesIdentifier(item atlascodes.Identifier, replacement bool) bool {
	if strings.TrimSpace(item.OrganizationID) == "" || !atlasCodesStableIDPattern.MatchString(item.ID) ||
		!atlasCodesStableIDPattern.MatchString(item.AssetID) || !validAtlasCodesEncodedValue(item.Symbology, item.NormalizedValue) ||
		!validAtlasCodesDisplayValue(item.DisplayValue) || strings.TrimSpace(item.CreatedBy) == "" ||
		utf8.RuneCountInString(item.CreatedBy) > 128 ||
		!validAtlasCodesProvenance(item.UpdatedBy, item.UpdatedCorrelationID) ||
		!validAtlasCodesCorrelationID(item.CreatedCorrelationID) ||
		item.CreatedBy != item.UpdatedBy || item.CreatedCorrelationID != item.UpdatedCorrelationID || item.Revision != 1 ||
		item.Status != atlascodes.StatusActive || item.CreatedAt.IsZero() || item.UpdatedAt.IsZero() ||
		item.UpdatedAt.Before(item.CreatedAt) || item.DeactivatedAt != nil || item.ReplacedByID != "" {
		return false
	}
	if item.Symbology != atlascodes.SymbologyCode128 && item.Symbology != atlascodes.SymbologyQR {
		return false
	}
	if item.Source != atlascodes.SourceImported && item.Source != atlascodes.SourceUserEntered && item.Source != atlascodes.SourceGenerated {
		return false
	}
	if replacement {
		return strings.TrimSpace(item.SupersedesID) != "" && item.SupersedesID != item.ID
	}
	return item.SupersedesID == ""
}

func validAtlasCodesProvenance(actorID, correlationID string) bool {
	return actorID != "" && actorID == strings.TrimSpace(actorID) &&
		utf8.RuneCountInString(actorID) <= 128 && validAtlasCodesCorrelationID(correlationID)
}

func validAtlasCodesCorrelationID(value string) bool {
	return value == strings.TrimSpace(value) && atlasCodesStableIDPattern.MatchString(value)
}

func validAtlasCodesEncodedValue(symbology atlascodes.Symbology, value string) bool {
	if value == "" || strings.TrimSpace(value) != value || !printableAtlasCodesText(value) {
		return false
	}
	switch symbology {
	case atlascodes.SymbologyCode128:
		if len(value) > 128 {
			return false
		}
		for index := range len(value) {
			if value[index] < 0x20 || value[index] > 0x7e {
				return false
			}
		}
		return true
	case atlascodes.SymbologyQR:
		return len(value) <= 512
	default:
		return false
	}
}

func validAtlasCodesDisplayValue(value string) bool {
	return value != "" && len(value) <= 512 && strings.TrimSpace(value) == value && printableAtlasCodesText(value)
}

func printableAtlasCodesText(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || !unicode.IsPrint(character) {
			return false
		}
	}
	return true
}

func sameAtlasCodesIntent(left, right atlascodes.Identifier) bool {
	return left.ID == right.ID && left.OrganizationID == right.OrganizationID && left.AssetID == right.AssetID &&
		left.Symbology == right.Symbology && left.NormalizedValue == right.NormalizedValue &&
		left.DisplayValue == right.DisplayValue && left.Source == right.Source && left.Primary == right.Primary &&
		left.Status == right.Status && left.SupersedesID == right.SupersedesID
}

func cloneAtlasCodesIdentifier(item atlascodes.Identifier) atlascodes.Identifier {
	item.DeactivatedAt = cloneAtlasCodesTime(item.DeactivatedAt)
	return item
}

func cloneAtlasCodesTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
