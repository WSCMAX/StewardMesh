package atlascodes

// Private Exchange import capability.
// Requirements: REQ-ATLAS-CODES-001, REQ-EXCHANGE-001.
// Features: inventory.identifiers, migration.packages. GitHub: #9.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/portabletime"
)

type exchangeImporter struct{ service *Service }

type exchangeImportContextKey struct{}

type exchangeImportContext struct{ operation ExchangeImportOperation }

func (s *Service) OwnsExchangeImporter(candidate ExchangeImporter) bool {
	importer, ok := candidate.(*exchangeImporter)
	return ok && importer != nil && importer.service == s
}

func (s *Service) ExchangeIdentifierChains(ctx context.Context, maximum int) ([]IdentifierChain, error) {
	if maximum < 1 {
		return nil, ErrInvalidInput
	}
	items, err := s.store.SnapshotIdentifiers(ctx, s.organizationID, maximum)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]Identifier, len(items))
	for _, item := range items {
		if _, duplicate := byID[item.ID]; duplicate {
			return nil, ErrConflict
		}
		byID[item.ID] = item
	}
	result := make([]IdentifierChain, 0)
	visited := make(map[string]struct{}, len(items))
	for _, terminal := range items {
		if terminal.ReplacedByID != "" {
			continue
		}
		reversed := []Identifier{}
		current := terminal
		lineage := make(map[string]struct{})
		for {
			if _, cycle := lineage[current.ID]; cycle {
				return nil, ErrConflict
			}
			lineage[current.ID] = struct{}{}
			reversed = append(reversed, current)
			if current.SupersedesID == "" {
				break
			}
			previous, exists := byID[current.SupersedesID]
			if !exists {
				return nil, ErrConflict
			}
			current = previous
		}
		chain := IdentifierChain{TerminalID: terminal.ID, Items: make([]Identifier, len(reversed))}
		for index := range reversed {
			chain.Items[len(reversed)-1-index] = cloneIdentifier(reversed[index])
		}
		if !s.validExchangeIdentifierChain(chain) {
			return nil, ErrInvalidInput
		}
		for _, item := range chain.Items {
			if _, duplicate := visited[item.ID]; duplicate {
				return nil, ErrConflict
			}
			visited[item.ID] = struct{}{}
		}
		result = append(result, chain)
	}
	if len(visited) != len(items) {
		return nil, ErrConflict
	}
	sort.Slice(result, func(i, j int) bool { return result[i].TerminalID < result[j].TerminalID })
	return result, nil
}

func (s *Service) ExchangeIdentifierChain(ctx context.Context, terminalID string) (IdentifierChain, error) {
	terminalID = strings.TrimSpace(terminalID)
	if !stableIDPattern.MatchString(terminalID) {
		return IdentifierChain{}, ErrInvalidInput
	}
	terminal, err := s.store.GetIdentifierByID(ctx, s.organizationID, terminalID)
	if err != nil {
		return IdentifierChain{}, err
	}
	if terminal.ReplacedByID != "" {
		return IdentifierChain{}, ErrNotFound
	}
	reversed := []Identifier{}
	seen := make(map[string]struct{})
	current := terminal
	for {
		if _, cycle := seen[current.ID]; cycle || len(reversed) >= 10_000 {
			return IdentifierChain{}, ErrConflict
		}
		seen[current.ID] = struct{}{}
		reversed = append(reversed, current)
		if current.SupersedesID == "" {
			break
		}
		current, err = s.store.GetIdentifierByID(ctx, s.organizationID, current.SupersedesID)
		if err != nil {
			return IdentifierChain{}, err
		}
	}
	chain := IdentifierChain{TerminalID: terminalID, Items: make([]Identifier, len(reversed))}
	for index := range reversed {
		chain.Items[len(reversed)-1-index] = cloneIdentifier(reversed[index])
	}
	if !s.validExchangeIdentifierChain(chain) {
		return IdentifierChain{}, ErrInvalidInput
	}
	return chain, nil
}

func (s *Service) checkWrite(ctx context.Context, recordType, id string) error {
	if _, importing := ctx.Value(exchangeImportContextKey{}).(exchangeImportContext); importing || s.writes == nil {
		return nil
	}
	return s.writes.CheckResourceWrite(ctx, recordType, id)
}

func (i *exchangeImporter) ImportIdentifierChain(ctx context.Context, operation ExchangeImportOperation, chain IdentifierChain) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	if err != nil {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	normalizedChain := IdentifierChain{TerminalID: chain.TerminalID, Items: make([]Identifier, len(chain.Items))}
	for index, item := range chain.Items {
		normalizedChain.Items[index] = cloneIdentifier(item)
	}
	chain = normalizedChain
	for index := range chain.Items {
		if chain.Items[index].OrganizationID != "" && chain.Items[index].OrganizationID != i.service.organizationID {
			return ExchangeImportResult{}, ErrInvalidInput
		}
		chain.Items[index].OrganizationID = i.service.organizationID
	}
	if !i.service.validExchangeIdentifierChain(chain) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	ctx = context.WithValue(ctx, exchangeImportContextKey{}, exchangeImportContext{operation: operation})
	if _, err := i.service.assets.GetAsset(ctx, chain.Items[0].AssetID); err != nil {
		return ExchangeImportResult{}, err
	}
	exact, partial, err := i.service.identifierChainState(ctx, chain)
	if err != nil {
		return ExchangeImportResult{}, err
	}
	if exact {
		err = i.service.auditExchangeChain(ctx, operation, chain)
		return ExchangeImportResult{Committed: true}, err
	}
	if partial {
		return ExchangeImportResult{}, ErrConflict
	}
	persisted, created, err := i.service.store.ImportIdentifierChain(ctx, i.service.organizationID, chain)
	result := ExchangeImportResult{Committed: len(persisted.Items) == len(chain.Items), Created: created}
	if err != nil {
		if exact, _, readErr := i.service.identifierChainState(ctx, chain); readErr == nil && exact {
			result.Committed, result.Created = true, true
		}
		return result, err
	}
	if !reflect.DeepEqual(persisted, chain) {
		return ExchangeImportResult{Committed: true, Created: created}, ErrConflict
	}
	err = i.service.auditExchangeChain(ctx, operation, chain)
	return ExchangeImportResult{Committed: true, Created: created}, err
}

func (*exchangeImporter) atlasCodesExchangeImporter() {}

func normalizeExchangeImportOperation(operation ExchangeImportOperation) (ExchangeImportOperation, error) {
	operation.Token = strings.TrimSpace(operation.Token)
	operation.OccurredAt = portabletime.Normalize(operation.OccurredAt)
	if !stableIDPattern.MatchString(operation.Token) || !validExchangeIdentifierTime(operation.OccurredAt) {
		return ExchangeImportOperation{}, ErrInvalidInput
	}
	return operation, nil
}

func (s *Service) identifierChainState(ctx context.Context, chain IdentifierChain) (exact, partial bool, err error) {
	matched := 0
	for _, candidate := range chain.Items {
		current, readErr := s.store.GetIdentifierByID(ctx, s.organizationID, candidate.ID)
		switch {
		case readErr == nil:
			partial = true
			if !reflect.DeepEqual(current, candidate) {
				return false, true, nil
			}
			matched++
		case errors.Is(readErr, ErrNotFound):
		default:
			return false, partial, readErr
		}
	}
	return matched == len(chain.Items), partial && matched != len(chain.Items), nil
}

func (s *Service) validExchangeIdentifierChain(chain IdentifierChain) bool {
	if !stableIDPattern.MatchString(chain.TerminalID) || len(chain.Items) < 1 || len(chain.Items) > 10_000 {
		return false
	}
	seen := make(map[string]struct{}, len(chain.Items))
	assetID := chain.Items[0].AssetID
	for index, item := range chain.Items {
		if !s.validExchangeIdentifier(item) || item.AssetID != assetID {
			return false
		}
		if _, duplicate := seen[item.ID]; duplicate {
			return false
		}
		seen[item.ID] = struct{}{}
		if index == 0 {
			if item.SupersedesID != "" {
				return false
			}
		} else {
			previous := chain.Items[index-1]
			if item.SupersedesID != previous.ID || previous.ReplacedByID != item.ID ||
				!previous.UpdatedAt.Equal(item.CreatedAt) || previous.UpdatedBy != item.CreatedBy ||
				previous.UpdatedCorrelationID != item.CreatedCorrelationID || previous.Primary != item.Primary {
				return false
			}
		}
		if index < len(chain.Items)-1 {
			if item.Status != StatusReplaced || item.ReplacedByID == "" || item.Revision != 2 ||
				item.DeactivatedAt == nil || !item.DeactivatedAt.Equal(item.UpdatedAt) {
				return false
			}
		}
	}
	terminal := chain.Items[len(chain.Items)-1]
	return terminal.ID == chain.TerminalID && terminal.ReplacedByID == "" && terminal.Status != StatusReplaced
}

func (s *Service) validExchangeIdentifier(item Identifier) bool {
	if item.OrganizationID != s.organizationID || !stableIDPattern.MatchString(item.ID) || !stableIDPattern.MatchString(item.AssetID) ||
		item.Revision < 1 || item.Revision > 2 || !validExchangeIdentifierActor(item.CreatedBy) ||
		!validExchangeIdentifierActor(item.UpdatedBy) || !stableIDPattern.MatchString(item.CreatedCorrelationID) ||
		!stableIDPattern.MatchString(item.UpdatedCorrelationID) || !validExchangeIdentifierTime(item.CreatedAt) ||
		!validExchangeIdentifierTime(item.UpdatedAt) || item.UpdatedAt.Before(item.CreatedAt) ||
		(item.SupersedesID != "" && !stableIDPattern.MatchString(item.SupersedesID)) ||
		(item.ReplacedByID != "" && !stableIDPattern.MatchString(item.ReplacedByID)) {
		return false
	}
	symbology, value, err := normalizeCode(item.Symbology, item.NormalizedValue)
	if err != nil || symbology != item.Symbology || value != item.NormalizedValue {
		return false
	}
	display, err := normalizeDisplay(item.DisplayValue, item.NormalizedValue)
	if err != nil || display != item.DisplayValue {
		return false
	}
	source, err := normalizeSource(item.Source)
	if err != nil || source != item.Source {
		return false
	}
	switch item.Status {
	case StatusActive:
		return item.Revision == 1 && item.ReplacedByID == "" && item.DeactivatedAt == nil &&
			item.CreatedBy == item.UpdatedBy && item.CreatedCorrelationID == item.UpdatedCorrelationID && item.CreatedAt.Equal(item.UpdatedAt)
	case StatusReplaced:
		return item.Revision == 2 && item.ReplacedByID != "" && item.DeactivatedAt != nil && item.DeactivatedAt.Equal(item.UpdatedAt)
	case StatusDeactivated:
		return item.Revision == 2 && item.ReplacedByID == "" && item.DeactivatedAt != nil && item.DeactivatedAt.Equal(item.UpdatedAt)
	default:
		return false
	}
}

func validExchangeIdentifierActor(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && utf8.ValidString(value) && utf8.RuneCountInString(value) <= 128
}

func validExchangeIdentifierTime(value time.Time) bool {
	return !value.IsZero() && value.Year() >= 1970 && value.Year() <= 9999 && portabletime.IsCanonical(value)
}

func (s *Service) auditExchangeChain(ctx context.Context, operation ExchangeImportOperation, chain IdentifierChain) error {
	digest := sha256.Sum256([]byte(strings.Join([]string{operation.Token, "atlas.identifier.chain_imported", chain.TerminalID}, "\x00")))
	metadata := map[string]string{
		"requirementId": RequirementID, "featureId": FeatureID, "terminalIdentifierId": chain.TerminalID,
		"assetId": chain.Items[0].AssetID, "historyCount": strconv.Itoa(len(chain.Items)),
	}
	scope := foundation.Scope{OrganizationID: s.organizationID, ActorID: "system:exchange", CorrelationID: operation.Token}
	ctx = foundation.WithScope(ctx, scope)
	return s.auditor.Record(ctx, foundation.AuditEvent{
		ID: hex.EncodeToString(digest[:]), OrganizationID: s.organizationID, ActorID: scope.ActorID,
		CorrelationID: scope.CorrelationID, Action: "atlas.identifier.chain_imported", ResourceType: "atlas.identifier",
		ResourceID: chain.TerminalID, OccurredAt: operation.OccurredAt, Metadata: metadata,
	})
}
