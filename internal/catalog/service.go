package catalog

// Requirement: REQ-ATLAS-CATALOG-001. Feature: inventory.catalog.

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/foundation"
)

var (
	stableIDPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	specificationPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
	currencyPattern      = regexp.MustCompile(`^[A-Z]{3}$`)
)

type ServiceConfig struct {
	OrganizationID string
	Now            func() time.Time
}

type Service struct {
	store          Store
	models         ModelReader
	writes         WriteGate
	auditor        foundation.Auditor
	organizationID string
	now            func() time.Time
}

type exchangeImporter struct{ service *Service }

type exchangeImportContextKey struct{}

type exchangeImportContext struct {
	operation ExchangeImportOperation
	revision  int64
}

func NewService(store Store, models ModelReader, auditor foundation.Auditor, configuration ServiceConfig) (*Service, error) {
	service, _, err := NewServiceWithExchangeImporter(store, models, nil, auditor, configuration)
	return service, err
}

func NewServiceWithExchangeImporter(store Store, models ModelReader, writes WriteGate, auditor foundation.Auditor, configuration ServiceConfig) (*Service, ExchangeImporter, error) {
	if store == nil || models == nil || auditor == nil {
		return nil, nil, errors.New("Atlas Catalog store, Atlas Models reader, and auditor are required")
	}
	configuration.OrganizationID = strings.TrimSpace(configuration.OrganizationID)
	if configuration.OrganizationID == "" {
		return nil, nil, errors.New("Atlas Catalog organization id is required")
	}
	if configuration.Now == nil {
		configuration.Now = func() time.Time { return time.Now().UTC() }
	}
	service := &Service{store: store, models: models, writes: writes, auditor: auditor, organizationID: configuration.OrganizationID, now: configuration.Now}
	return service, &exchangeImporter{service: service}, nil
}

// Snapshot returns every durable Catalog record without weakening normal
// model-scoped list validation. Exchange applies its independent 10,000-record
// package/catalog bound to this transport-neutral snapshot.
func (s *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	return s.store.Snapshot(ctx, s.organizationID)
}

func (s *Service) GetConfiguration(ctx context.Context, id string) (Configuration, error) {
	id = strings.TrimSpace(id)
	if !stableIDPattern.MatchString(id) {
		return Configuration{}, ErrInvalidInput
	}
	return s.store.GetConfiguration(ctx, s.organizationID, id)
}

func (s *Service) GetPrice(ctx context.Context, id string) (Price, error) {
	id = strings.TrimSpace(id)
	if !stableIDPattern.MatchString(id) {
		return Price{}, ErrInvalidInput
	}
	return s.store.GetPrice(ctx, s.organizationID, id)
}

func (s *Service) GetUpgradePath(ctx context.Context, id string) (UpgradePath, error) {
	id = strings.TrimSpace(id)
	if !stableIDPattern.MatchString(id) {
		return UpgradePath{}, ErrInvalidInput
	}
	return s.store.GetUpgradePath(ctx, s.organizationID, id)
}

// ExchangeDependencyExists resolves Catalog's Atlas Model references without
// exposing Atlas repositories to the Exchange package.
func (s *Service) ExchangeDependencyExists(ctx context.Context, recordType, id string) (bool, bool, error) {
	if recordType != "atlas.model" {
		return false, false, nil
	}
	id = strings.TrimSpace(id)
	if !stableIDPattern.MatchString(id) {
		return true, false, ErrInvalidInput
	}
	err := s.validateModel(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return true, false, nil
	}
	return true, err == nil, err
}

func (s *Service) OwnsExchangeImporter(candidate ExchangeImporter) bool {
	importer, ok := candidate.(*exchangeImporter)
	return ok && importer != nil && importer.service == s
}

func (i *exchangeImporter) ImportConfiguration(ctx context.Context, operation ExchangeImportOperation, candidate Configuration) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	if err != nil || candidate.Revision < 1 {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	ctx = context.WithValue(ctx, exchangeImportContextKey{}, exchangeImportContext{operation: operation, revision: candidate.Revision})
	existing, err := i.service.GetConfiguration(ctx, candidate.ID)
	if err == nil {
		if !sameExchangeConfiguration(existing, candidate) {
			return ExchangeImportResult{}, ErrConflict
		}
		err = i.service.audit(ctx, "atlas.catalog.configuration.created", "catalog_configuration", existing.ID, map[string]string{
			"modelId": existing.ModelID, "status": string(existing.Status), "revision": strconv.FormatInt(existing.Revision, 10),
		})
		return ExchangeImportResult{Committed: true}, err
	}
	if !errors.Is(err, ErrNotFound) {
		return ExchangeImportResult{}, err
	}
	_, err = i.service.CreateConfiguration(ctx, CreateConfigurationInput{
		ID: candidate.ID, ModelID: candidate.ModelID, Name: candidate.Name, SKU: candidate.SKU,
		Status: candidate.Status, Specifications: candidate.Specifications,
	})
	if err == nil {
		return ExchangeImportResult{Committed: true, Created: true}, nil
	}
	if observed, readErr := i.service.GetConfiguration(ctx, candidate.ID); readErr == nil && sameExchangeConfiguration(observed, candidate) {
		return ExchangeImportResult{Committed: true, Created: true}, err
	}
	return ExchangeImportResult{}, err
}

func (i *exchangeImporter) ImportPrice(ctx context.Context, operation ExchangeImportOperation, candidate Price) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	if err != nil || candidate.Revision != 1 {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	ctx = context.WithValue(ctx, exchangeImportContextKey{}, exchangeImportContext{operation: operation, revision: candidate.Revision})
	existing, err := i.service.GetPrice(ctx, candidate.ID)
	if err == nil {
		if !sameExchangePrice(existing, candidate) {
			return ExchangeImportResult{}, ErrConflict
		}
		err = i.service.audit(ctx, "atlas.catalog.price.recorded", "catalog_price", existing.ID, map[string]string{
			"modelId": existing.ModelID, "configurationId": existing.ConfigurationID, "kind": string(existing.Kind),
			"currency": existing.Currency, "revision": strconv.FormatInt(existing.Revision, 10),
		})
		return ExchangeImportResult{Committed: true}, err
	}
	if !errors.Is(err, ErrNotFound) {
		return ExchangeImportResult{}, err
	}
	_, err = i.service.RecordPrice(ctx, RecordPriceInput{
		ID: candidate.ID, ModelID: candidate.ModelID, ConfigurationID: candidate.ConfigurationID,
		Kind: candidate.Kind, AmountMinor: candidate.AmountMinor, Currency: candidate.Currency,
		EffectiveFrom: candidate.EffectiveFrom, EffectiveTo: candidate.EffectiveTo, SourceReference: candidate.SourceReference,
	})
	if err == nil {
		return ExchangeImportResult{Committed: true, Created: true}, nil
	}
	if observed, readErr := i.service.GetPrice(ctx, candidate.ID); readErr == nil && sameExchangePrice(observed, candidate) {
		return ExchangeImportResult{Committed: true, Created: true}, err
	}
	return ExchangeImportResult{}, err
}

func (i *exchangeImporter) ImportUpgradePath(ctx context.Context, operation ExchangeImportOperation, candidate UpgradePath) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	if err != nil || candidate.Revision != 1 {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	ctx = context.WithValue(ctx, exchangeImportContextKey{}, exchangeImportContext{operation: operation, revision: candidate.Revision})
	existing, err := i.service.GetUpgradePath(ctx, candidate.ID)
	if err == nil {
		if !sameExchangeUpgradePath(existing, candidate) {
			return ExchangeImportResult{}, ErrConflict
		}
		err = i.service.audit(ctx, "atlas.catalog.upgrade_path.created", "catalog_upgrade_path", existing.ID, map[string]string{
			"fromModelId": existing.FromModelID, "fromConfigurationId": existing.FromConfigurationID,
			"toModelId": existing.ToModelID, "toConfigurationId": existing.ToConfigurationID,
			"kind": string(existing.Kind), "revision": strconv.FormatInt(existing.Revision, 10),
		})
		return ExchangeImportResult{Committed: true}, err
	}
	if !errors.Is(err, ErrNotFound) {
		return ExchangeImportResult{}, err
	}
	_, err = i.service.CreateUpgradePath(ctx, CreateUpgradePathInput{
		ID: candidate.ID, FromModelID: candidate.FromModelID, FromConfigurationID: candidate.FromConfigurationID,
		ToModelID: candidate.ToModelID, ToConfigurationID: candidate.ToConfigurationID,
		Kind: candidate.Kind, EffectiveFrom: candidate.EffectiveFrom,
	})
	if err == nil {
		return ExchangeImportResult{Committed: true, Created: true}, nil
	}
	if observed, readErr := i.service.GetUpgradePath(ctx, candidate.ID); readErr == nil && sameExchangeUpgradePath(observed, candidate) {
		return ExchangeImportResult{Committed: true, Created: true}, err
	}
	return ExchangeImportResult{}, err
}

func normalizeExchangeImportOperation(operation ExchangeImportOperation) (ExchangeImportOperation, error) {
	operation.Token = strings.TrimSpace(operation.Token)
	operation.OccurredAt = operation.OccurredAt.UTC()
	if !stableIDPattern.MatchString(operation.Token) || operation.OccurredAt.IsZero() || operation.OccurredAt.Year() < 2000 || operation.OccurredAt.Year() > 9999 {
		return ExchangeImportOperation{}, ErrInvalidInput
	}
	return operation, nil
}

func (s *Service) creationState(ctx context.Context) (time.Time, int64) {
	if state, ok := ctx.Value(exchangeImportContextKey{}).(exchangeImportContext); ok && state.revision > 0 && !state.operation.OccurredAt.IsZero() {
		return state.operation.OccurredAt.UTC(), state.revision
	}
	return s.now().UTC(), 1
}

func (s *Service) checkWrite(ctx context.Context, recordType, id string) error {
	if _, importing := ctx.Value(exchangeImportContextKey{}).(exchangeImportContext); importing || s.writes == nil {
		return nil
	}
	return s.writes.CheckResourceWrite(ctx, recordType, id)
}

func sameExchangeConfiguration(left, right Configuration) bool {
	return left.ID == right.ID && left.ModelID == right.ModelID && left.Name == right.Name && left.SKU == right.SKU &&
		left.Status == right.Status && left.Revision == right.Revision && maps.Equal(left.Specifications, right.Specifications)
}

func sameExchangePrice(left, right Price) bool {
	return left.ID == right.ID && left.ModelID == right.ModelID && left.ConfigurationID == right.ConfigurationID &&
		left.Kind == right.Kind && left.AmountMinor == right.AmountMinor && left.Currency == right.Currency &&
		left.EffectiveFrom.Equal(right.EffectiveFrom) && equalOptionalTime(left.EffectiveTo, right.EffectiveTo) &&
		left.SourceReference == right.SourceReference && left.Revision == right.Revision
}

func sameExchangeUpgradePath(left, right UpgradePath) bool {
	return left.ID == right.ID && left.FromModelID == right.FromModelID && left.FromConfigurationID == right.FromConfigurationID &&
		left.ToModelID == right.ToModelID && left.ToConfigurationID == right.ToConfigurationID && left.Kind == right.Kind &&
		left.EffectiveFrom.Equal(right.EffectiveFrom) && left.Revision == right.Revision
}

func equalOptionalTime(left, right *time.Time) bool {
	return left == nil && right == nil || left != nil && right != nil && left.Equal(*right)
}

func (s *Service) ListConfigurations(ctx context.Context, modelID string) ([]Configuration, error) {
	modelID = strings.TrimSpace(modelID)
	if !stableIDPattern.MatchString(modelID) {
		return nil, ErrInvalidInput
	}
	if err := s.validateModel(ctx, modelID); err != nil {
		return nil, err
	}
	return s.store.ListConfigurations(ctx, s.organizationID, modelID)
}

func (s *Service) CreateConfiguration(ctx context.Context, input CreateConfigurationInput) (Configuration, error) {
	input.ModelID = strings.TrimSpace(input.ModelID)
	input.Name = strings.TrimSpace(input.Name)
	input.SKU = strings.TrimSpace(input.SKU)
	input.Status = normalizeStatus(input.Status)
	specifications, err := normalizeSpecifications(input.Specifications)
	if err != nil || !stableIDPattern.MatchString(input.ModelID) || !validPrintableText(input.Name, 1, 200) ||
		!validOptionalPrintableText(input.SKU, 128) || !validStatus(input.Status) {
		return Configuration{}, ErrInvalidInput
	}
	if err := s.validateModel(ctx, input.ModelID); err != nil {
		return Configuration{}, err
	}
	id, err := catalogID(input.ID)
	if err != nil {
		return Configuration{}, err
	}
	if err := s.checkWrite(ctx, "atlas.catalog-configuration", id); err != nil {
		return Configuration{}, err
	}
	now, revision := s.creationState(ctx)
	configuration, err := s.store.CreateConfiguration(ctx, Configuration{
		ID: id, OrganizationID: s.organizationID, ModelID: input.ModelID, Name: input.Name,
		SKU: input.SKU, Status: input.Status, Specifications: specifications, Revision: revision, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return Configuration{}, err
	}
	if err := s.audit(ctx, "atlas.catalog.configuration.created", "catalog_configuration", configuration.ID, map[string]string{
		"modelId": configuration.ModelID, "status": string(configuration.Status), "revision": strconv.FormatInt(configuration.Revision, 10),
	}); err != nil {
		return Configuration{}, fmt.Errorf("audit Atlas Catalog configuration creation: %w", err)
	}
	return configuration, nil
}

func (s *Service) RecordPrice(ctx context.Context, input RecordPriceInput) (Price, error) {
	input.ModelID = strings.TrimSpace(input.ModelID)
	input.ConfigurationID = strings.TrimSpace(input.ConfigurationID)
	input.Kind = PriceKind(strings.ToLower(strings.TrimSpace(string(input.Kind))))
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
	input.SourceReference = strings.TrimSpace(input.SourceReference)
	input.EffectiveFrom = calendarDate(input.EffectiveFrom)
	input.EffectiveTo = cloneCalendarDate(input.EffectiveTo)
	if !stableIDPattern.MatchString(input.ModelID) || input.ConfigurationID != "" && !stableIDPattern.MatchString(input.ConfigurationID) ||
		!validPriceKind(input.Kind) || input.AmountMinor < 0 || input.AmountMinor > MaximumExactMinorUnits ||
		!currencyPattern.MatchString(input.Currency) || input.EffectiveFrom.IsZero() ||
		input.EffectiveTo != nil && input.EffectiveTo.Before(input.EffectiveFrom) ||
		!validOptionalPrintableText(input.SourceReference, 200) {
		return Price{}, ErrInvalidInput
	}
	if err := s.validateModelConfiguration(ctx, input.ModelID, input.ConfigurationID); err != nil {
		return Price{}, err
	}
	id, err := catalogID(input.ID)
	if err != nil {
		return Price{}, err
	}
	if err := s.checkWrite(ctx, "atlas.catalog-price", id); err != nil {
		return Price{}, err
	}
	now, revision := s.creationState(ctx)
	price, err := s.store.CreatePrice(ctx, Price{
		ID: id, OrganizationID: s.organizationID, ModelID: input.ModelID, ConfigurationID: input.ConfigurationID,
		Kind: input.Kind, AmountMinor: input.AmountMinor, Currency: input.Currency, EffectiveFrom: input.EffectiveFrom,
		EffectiveTo: input.EffectiveTo, SourceReference: input.SourceReference, Revision: revision, CreatedAt: now,
	})
	if err != nil {
		return Price{}, err
	}
	if err := s.audit(ctx, "atlas.catalog.price.recorded", "catalog_price", price.ID, map[string]string{
		"modelId": price.ModelID, "configurationId": price.ConfigurationID, "kind": string(price.Kind),
		"currency": price.Currency, "revision": strconv.FormatInt(price.Revision, 10),
	}); err != nil {
		return Price{}, fmt.Errorf("audit Atlas Catalog price recording: %w", err)
	}
	return price, nil
}

func (s *Service) ResolvePrice(ctx context.Context, input ResolvePriceInput) (Price, error) {
	input.ModelID = strings.TrimSpace(input.ModelID)
	input.ConfigurationID = strings.TrimSpace(input.ConfigurationID)
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
	input.Kind = PriceKind(strings.ToLower(strings.TrimSpace(string(input.Kind))))
	input.AsOf = calendarDate(input.AsOf)
	if !stableIDPattern.MatchString(input.ModelID) || input.ConfigurationID != "" && !stableIDPattern.MatchString(input.ConfigurationID) ||
		input.AsOf.IsZero() || input.Currency != "" && !currencyPattern.MatchString(input.Currency) ||
		input.Kind != "" && !validPriceKind(input.Kind) {
		return Price{}, ErrInvalidInput
	}
	if err := s.validateModelConfiguration(ctx, input.ModelID, input.ConfigurationID); err != nil {
		return Price{}, err
	}
	prices, err := s.store.ListPrices(ctx, s.organizationID, input.ModelID, "")
	if err != nil {
		return Price{}, err
	}
	candidates := make([]Price, 0, len(prices))
	for _, price := range prices {
		if price.EffectiveFrom.After(input.AsOf) || price.EffectiveTo != nil && price.EffectiveTo.Before(input.AsOf) ||
			input.Currency != "" && price.Currency != input.Currency || input.Kind != "" && price.Kind != input.Kind {
			continue
		}
		candidates = append(candidates, price)
	}
	if input.ConfigurationID != "" {
		configurationPrices := candidates[:0]
		for _, price := range candidates {
			if price.ConfigurationID == input.ConfigurationID {
				configurationPrices = append(configurationPrices, price)
			}
		}
		if len(configurationPrices) > 0 {
			candidates = configurationPrices
		} else {
			basePrices := candidates[:0]
			for _, price := range candidates {
				if price.ConfigurationID == "" {
					basePrices = append(basePrices, price)
				}
			}
			candidates = basePrices
		}
	} else {
		basePrices := candidates[:0]
		for _, price := range candidates {
			if price.ConfigurationID == "" {
				basePrices = append(basePrices, price)
			}
		}
		candidates = basePrices
	}
	if len(candidates) == 0 {
		return Price{}, ErrNotFound
	}
	currencies := make(map[string]struct{})
	for _, price := range candidates {
		currencies[price.Currency] = struct{}{}
	}
	if len(currencies) > 1 {
		return Price{}, ErrMixedCurrency
	}
	sort.Slice(candidates, func(i, j int) bool {
		leftRank, rightRank := priceKindRank(candidates[i].Kind), priceKindRank(candidates[j].Kind)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if !candidates[i].EffectiveFrom.Equal(candidates[j].EffectiveFrom) {
			return candidates[i].EffectiveFrom.After(candidates[j].EffectiveFrom)
		}
		return candidates[i].ID < candidates[j].ID
	})
	return candidates[0], nil
}

func (s *Service) ListUpgradePaths(ctx context.Context, fromModelID, fromConfigurationID string) ([]UpgradePath, error) {
	fromModelID = strings.TrimSpace(fromModelID)
	fromConfigurationID = strings.TrimSpace(fromConfigurationID)
	if !stableIDPattern.MatchString(fromModelID) || fromConfigurationID != "" && !stableIDPattern.MatchString(fromConfigurationID) {
		return nil, ErrInvalidInput
	}
	if err := s.validateModelConfiguration(ctx, fromModelID, fromConfigurationID); err != nil {
		return nil, err
	}
	return s.store.ListUpgradePaths(ctx, s.organizationID, fromModelID, fromConfigurationID)
}

func (s *Service) CreateUpgradePath(ctx context.Context, input CreateUpgradePathInput) (UpgradePath, error) {
	input.FromModelID = strings.TrimSpace(input.FromModelID)
	input.FromConfigurationID = strings.TrimSpace(input.FromConfigurationID)
	input.ToModelID = strings.TrimSpace(input.ToModelID)
	input.ToConfigurationID = strings.TrimSpace(input.ToConfigurationID)
	input.Kind = UpgradeKind(strings.ToLower(strings.TrimSpace(string(input.Kind))))
	input.EffectiveFrom = calendarDate(input.EffectiveFrom)
	if !stableIDPattern.MatchString(input.FromModelID) || !stableIDPattern.MatchString(input.ToModelID) ||
		input.FromConfigurationID != "" && !stableIDPattern.MatchString(input.FromConfigurationID) ||
		input.ToConfigurationID != "" && !stableIDPattern.MatchString(input.ToConfigurationID) ||
		!validUpgradeKind(input.Kind) || input.EffectiveFrom.IsZero() ||
		input.FromModelID == input.ToModelID && input.FromConfigurationID == input.ToConfigurationID {
		return UpgradePath{}, ErrInvalidInput
	}
	if err := s.validateModelConfiguration(ctx, input.FromModelID, input.FromConfigurationID); err != nil {
		return UpgradePath{}, err
	}
	if err := s.validateModelConfiguration(ctx, input.ToModelID, input.ToConfigurationID); err != nil {
		return UpgradePath{}, err
	}
	id, err := catalogID(input.ID)
	if err != nil {
		return UpgradePath{}, err
	}
	if err := s.checkWrite(ctx, "atlas.catalog-upgrade-path", id); err != nil {
		return UpgradePath{}, err
	}
	now, revision := s.creationState(ctx)
	path, err := s.store.CreateUpgradePath(ctx, UpgradePath{
		ID: id, OrganizationID: s.organizationID, FromModelID: input.FromModelID,
		FromConfigurationID: input.FromConfigurationID, ToModelID: input.ToModelID,
		ToConfigurationID: input.ToConfigurationID, Kind: input.Kind, EffectiveFrom: input.EffectiveFrom,
		Revision: revision, CreatedAt: now,
	})
	if err != nil {
		return UpgradePath{}, err
	}
	if err := s.audit(ctx, "atlas.catalog.upgrade_path.created", "catalog_upgrade_path", path.ID, map[string]string{
		"fromModelId": path.FromModelID, "fromConfigurationId": path.FromConfigurationID,
		"toModelId": path.ToModelID, "toConfigurationId": path.ToConfigurationID,
		"kind": string(path.Kind), "revision": strconv.FormatInt(path.Revision, 10),
	}); err != nil {
		return UpgradePath{}, fmt.Errorf("audit Atlas Catalog upgrade path creation: %w", err)
	}
	return path, nil
}

func (s *Service) validateModelConfiguration(ctx context.Context, modelID, configurationID string) error {
	if err := s.validateModel(ctx, modelID); err != nil {
		return err
	}
	if configurationID == "" {
		return nil
	}
	configuration, err := s.store.GetConfiguration(ctx, s.organizationID, configurationID)
	if err != nil {
		return err
	}
	if configuration.ModelID != modelID {
		return ErrInvalidInput
	}
	return nil
}

func (s *Service) validateModel(ctx context.Context, modelID string) error {
	model, err := s.models.GetModel(ctx, modelID)
	if err != nil {
		switch {
		case errors.Is(err, atlas.ErrNotFound):
			return ErrNotFound
		case errors.Is(err, atlas.ErrInvalidInput):
			return ErrInvalidInput
		default:
			return fmt.Errorf("read Atlas model for Catalog: %w", err)
		}
	}
	if model.ID != modelID || model.OrganizationID != s.organizationID {
		return ErrNotFound
	}
	return nil
}

func normalizeSpecifications(input map[string]string) (map[string]string, error) {
	if len(input) > 64 {
		return nil, ErrInvalidInput
	}
	result := make(map[string]string, len(input))
	for rawKey, rawValue := range input {
		key := strings.ToLower(strings.TrimSpace(rawKey))
		value := strings.TrimSpace(rawValue)
		if !specificationPattern.MatchString(key) || !validPrintableText(value, 1, 500) {
			return nil, ErrInvalidInput
		}
		if _, duplicate := result[key]; duplicate {
			return nil, ErrInvalidInput
		}
		result[key] = value
	}
	return result, nil
}

func normalizeStatus(status Status) Status {
	status = Status(strings.ToLower(strings.TrimSpace(string(status))))
	if status == "" {
		return StatusActive
	}
	return status
}

func validStatus(status Status) bool { return status == StatusActive || status == StatusRetired }

func validPriceKind(kind PriceKind) bool {
	return kind == PriceKindList || kind == PriceKindQuote || kind == PriceKindContract || kind == PriceKindEstimate
}

func validUpgradeKind(kind UpgradeKind) bool {
	return kind == UpgradeKindSuccessor || kind == UpgradeKindReplacement || kind == UpgradeKindUpgrade
}

func priceKindRank(kind PriceKind) int {
	switch kind {
	case PriceKindContract:
		return 0
	case PriceKindQuote:
		return 1
	case PriceKindEstimate:
		return 2
	default:
		return 3
	}
}

func validPrintableText(value string, minimum, maximum int) bool {
	if len(value) < minimum || len(value) > maximum || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || !unicode.IsPrint(character) {
			return false
		}
	}
	return true
}

func validOptionalPrintableText(value string, maximum int) bool {
	return value == "" || validPrintableText(value, 1, maximum)
}

func calendarDate(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func cloneCalendarDate(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	result := calendarDate(*value)
	return &result
}

func catalogID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value != "" {
		if !stableIDPattern.MatchString(value) {
			return "", ErrInvalidInput
		}
		return value, nil
	}
	id, err := foundation.NewCorrelationID()
	if err != nil {
		return "", fmt.Errorf("create Atlas Catalog id: %w", err)
	}
	return id, nil
}

func (s *Service) audit(ctx context.Context, action, resourceType, resourceID string, metadata map[string]string) error {
	actorID := "system:atlas-catalog"
	correlationID := ""
	occurredAt := s.now().UTC()
	eventID := ""
	if state, ok := ctx.Value(exchangeImportContextKey{}).(exchangeImportContext); ok {
		actorID = "system:exchange"
		correlationID = state.operation.Token
		occurredAt = state.operation.OccurredAt.UTC()
		digest := sha256.Sum256([]byte(s.organizationID + "\x00" + state.operation.Token + "\x00" + action + "\x00" + resourceType + "\x00" + resourceID))
		eventID = fmt.Sprintf("%x", digest[:])
	}
	if scope, ok := foundation.ScopeFromContext(ctx); ok {
		if eventID == "" && strings.TrimSpace(scope.ActorID) != "" {
			actorID = strings.TrimSpace(scope.ActorID)
		}
		if correlationID == "" {
			correlationID = strings.TrimSpace(scope.CorrelationID)
		}
	}
	if correlationID == "" {
		var err error
		correlationID, err = foundation.NewCorrelationID()
		if err != nil {
			return fmt.Errorf("create Atlas Catalog audit correlation id: %w", err)
		}
	}
	if eventID == "" {
		var err error
		eventID, err = foundation.NewCorrelationID()
		if err != nil {
			return fmt.Errorf("create Atlas Catalog audit event id: %w", err)
		}
	}
	metadata["requirementId"] = RequirementID
	metadata["featureId"] = FeatureID
	metadata["organizationScoped"] = strconv.FormatBool(true)
	return s.auditor.Record(ctx, foundation.AuditEvent{
		ID: eventID, OrganizationID: s.organizationID, ActorID: actorID, CorrelationID: correlationID,
		Action: action, ResourceType: resourceType, ResourceID: resourceID, OccurredAt: occurredAt, Metadata: metadata,
	})
}
