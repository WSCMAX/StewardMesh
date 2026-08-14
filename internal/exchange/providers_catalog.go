package exchange

// Requirements: REQ-EXCHANGE-001, REQ-ATLAS-CATALOG-001, REQ-PATTERNS-001. Features: migration.packages, inventory.catalog, templates.schemas. GitHub: #9.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/catalog"
)

var catalogRecordTypes = []string{"atlas.catalog-configuration", "atlas.catalog-price", "atlas.catalog-upgrade-path"}

type CatalogProvider struct {
	service  *catalog.Service
	importer catalog.ExchangeImporter
}

type catalogConfigurationPayload struct {
	ModelID        string `json:"modelId"`
	Name           string `json:"name"`
	SKU            string `json:"sku,omitempty"`
	Status         string `json:"status"`
	Specifications string `json:"specifications,omitempty"`
}

type catalogPricePayload struct {
	ModelID         string `json:"modelId"`
	ConfigurationID string `json:"configurationId,omitempty"`
	Kind            string `json:"kind"`
	Currency        string `json:"currency"`
	AmountMinor     int64  `json:"amountMinor"`
	EffectiveFrom   string `json:"effectiveFrom"`
	EffectiveTo     string `json:"effectiveTo,omitempty"`
	SourceReference string `json:"sourceReference,omitempty"`
}

type catalogUpgradePathPayload struct {
	FromModelID         string `json:"fromModelId"`
	FromConfigurationID string `json:"fromConfigurationId,omitempty"`
	ToModelID           string `json:"toModelId"`
	ToConfigurationID   string `json:"toConfigurationId,omitempty"`
	RelationshipKind    string `json:"relationshipKind"`
	EffectiveFrom       string `json:"effectiveFrom"`
}

func NewCatalogProvider(service *catalog.Service, importer catalog.ExchangeImporter) (*CatalogProvider, error) {
	if service == nil || importer == nil || !service.OwnsExchangeImporter(importer) {
		return nil, errors.New("Atlas Catalog service and its construction-time Exchange importer are required")
	}
	return &CatalogProvider{service: service, importer: importer}, nil
}

func (*CatalogProvider) Types() []string { return append([]string(nil), catalogRecordTypes...) }

func (p *CatalogProvider) ListRecords(ctx context.Context) ([]Record, error) {
	snapshot, err := p.service.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	if len(snapshot.Configurations)+len(snapshot.Prices)+len(snapshot.UpgradePaths) > MaximumRecords {
		return nil, ErrTooLarge
	}
	result := make([]Record, 0, len(snapshot.Configurations)+len(snapshot.Prices)+len(snapshot.UpgradePaths))
	for _, item := range snapshot.Configurations {
		specifications, err := canonicalCatalogSpecifications(item.Specifications)
		if err != nil {
			return nil, err
		}
		payload, err := json.Marshal(catalogConfigurationPayload{
			ModelID: item.ModelID, Name: item.Name, SKU: item.SKU, Status: string(item.Status), Specifications: specifications,
		})
		if err != nil || len(payload) > MaximumPayloadBytes {
			return nil, ErrInvalidInput
		}
		result = append(result, Record{Type: "atlas.catalog-configuration", ID: item.ID, Revision: item.Revision,
			Dependencies: catalogConfigurationDependencies(item), Ownership: OwnershipMetadata{State: "local"}, Payload: payload})
	}
	for _, item := range snapshot.Prices {
		payload, err := json.Marshal(catalogPricePayload{
			ModelID: item.ModelID, ConfigurationID: item.ConfigurationID, Kind: string(item.Kind), Currency: item.Currency,
			AmountMinor: item.AmountMinor, EffectiveFrom: catalogDate(item.EffectiveFrom), EffectiveTo: catalogOptionalDate(item.EffectiveTo),
			SourceReference: item.SourceReference,
		})
		if err != nil || len(payload) > MaximumPayloadBytes {
			return nil, ErrInvalidInput
		}
		result = append(result, Record{Type: "atlas.catalog-price", ID: item.ID, Revision: item.Revision,
			Dependencies: catalogPriceDependencies(item), Ownership: OwnershipMetadata{State: "local"}, Payload: payload})
	}
	for _, item := range snapshot.UpgradePaths {
		payload, err := json.Marshal(catalogUpgradePathPayload{
			FromModelID: item.FromModelID, FromConfigurationID: item.FromConfigurationID,
			ToModelID: item.ToModelID, ToConfigurationID: item.ToConfigurationID,
			RelationshipKind: string(item.Kind), EffectiveFrom: catalogDate(item.EffectiveFrom),
		})
		if err != nil || len(payload) > MaximumPayloadBytes {
			return nil, ErrInvalidInput
		}
		result = append(result, Record{Type: "atlas.catalog-upgrade-path", ID: item.ID, Revision: item.Revision,
			Dependencies: catalogUpgradePathDependencies(item), Ownership: OwnershipMetadata{State: "local"}, Payload: payload})
	}
	sort.Slice(result, func(i, j int) bool {
		return (Reference{Type: result[i].Type, ID: result[i].ID}).Key() < (Reference{Type: result[j].Type, ID: result[j].ID}).Key()
	})
	return result, nil
}

func (p *CatalogProvider) Exists(ctx context.Context, reference Reference) (bool, error) {
	var err error
	switch reference.Type {
	case "atlas.catalog-configuration":
		_, err = p.service.GetConfiguration(ctx, reference.ID)
	case "atlas.catalog-price":
		_, err = p.service.GetPrice(ctx, reference.ID)
	case "atlas.catalog-upgrade-path":
		_, err = p.service.GetUpgradePath(ctx, reference.ID)
	default:
		return false, nil
	}
	if errors.Is(err, catalog.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (p *CatalogProvider) DependencyExists(ctx context.Context, reference Reference) (bool, bool, error) {
	return p.service.ExchangeDependencyExists(ctx, reference.Type, reference.ID)
}

func (p *CatalogProvider) ImportRecordExists(ctx context.Context, record Record, _ []byte) (bool, error) {
	candidate, dependencies, err := decodeCatalogRecord(record)
	if err != nil || !slices.EqualFunc(dependencies, record.Dependencies, func(left, right Reference) bool { return left == right }) {
		return false, ErrInvalidInput
	}
	switch value := candidate.(type) {
	case catalog.Configuration:
		current, err := p.service.GetConfiguration(ctx, record.ID)
		if errors.Is(err, catalog.ErrNotFound) {
			return false, nil
		}
		return err == nil && sameCatalogConfiguration(current, value), err
	case catalog.Price:
		current, err := p.service.GetPrice(ctx, record.ID)
		if errors.Is(err, catalog.ErrNotFound) {
			return false, nil
		}
		return err == nil && sameCatalogPrice(current, value), err
	case catalog.UpgradePath:
		current, err := p.service.GetUpgradePath(ctx, record.ID)
		if errors.Is(err, catalog.ErrNotFound) {
			return false, nil
		}
		return err == nil && sameCatalogUpgradePath(current, value), err
	default:
		return false, ErrInvalidInput
	}
}

func (p *CatalogProvider) ImportRecord(ctx context.Context, operation ProviderImportOperation, _ string, record Record, _ []byte) (ProviderImportResult, error) {
	if !operation.ExpectedCreated {
		exact, err := p.ImportRecordExists(ctx, record, nil)
		if err != nil {
			return ProviderImportResult{}, err
		}
		if !exact {
			return ProviderImportResult{}, ErrConflict
		}
		return ProviderImportResult{Committed: true}, nil
	}
	candidate, dependencies, err := decodeCatalogRecord(record)
	if err != nil || !slices.EqualFunc(dependencies, record.Dependencies, func(left, right Reference) bool { return left == right }) {
		return ProviderImportResult{}, ErrInvalidInput
	}
	domainOperation := catalog.ExchangeImportOperation{Token: operation.Token, OccurredAt: operation.OccurredAt}
	var result catalog.ExchangeImportResult
	switch value := candidate.(type) {
	case catalog.Configuration:
		result, err = p.importer.ImportConfiguration(ctx, domainOperation, value)
	case catalog.Price:
		result, err = p.importer.ImportPrice(ctx, domainOperation, value)
	case catalog.UpgradePath:
		result, err = p.importer.ImportUpgradePath(ctx, domainOperation, value)
	default:
		return ProviderImportResult{}, ErrInvalidInput
	}
	providerResult := ProviderImportResult{Committed: result.Committed, Created: result.Created}
	switch {
	case errors.Is(err, catalog.ErrInvalidInput):
		return providerResult, ErrInvalidInput
	case errors.Is(err, catalog.ErrConflict):
		return providerResult, ErrConflict
	case errors.Is(err, catalog.ErrNotFound):
		return providerResult, ErrDependencyMissing
	default:
		return providerResult, err
	}
}

func decodeCatalogRecord(record Record) (any, []Reference, error) {
	switch record.Type {
	case "atlas.catalog-configuration":
		payload, err := decodeCatalogPayload[catalogConfigurationPayload](record.Payload)
		if err != nil {
			return nil, nil, err
		}
		var specifications map[string]string
		if err := json.Unmarshal([]byte(payload.Specifications), &specifications); err != nil || specifications == nil {
			return nil, nil, ErrInvalidInput
		}
		canonicalSpecifications, err := canonicalCatalogSpecifications(specifications)
		if err != nil || canonicalSpecifications != payload.Specifications || !canonicalCatalogConfigurationPayload(payload, specifications) {
			return nil, nil, ErrInvalidInput
		}
		value := catalog.Configuration{ID: record.ID, ModelID: payload.ModelID, Name: payload.Name, SKU: payload.SKU,
			Status: catalog.Status(payload.Status), Specifications: specifications, Revision: record.Revision}
		return value, catalogConfigurationDependencies(value), nil
	case "atlas.catalog-price":
		payload, err := decodeCatalogPayload[catalogPricePayload](record.Payload)
		if err != nil {
			return nil, nil, err
		}
		effectiveFrom, err := parseCatalogDate(payload.EffectiveFrom)
		if err != nil {
			return nil, nil, err
		}
		effectiveTo, err := parseCatalogOptionalDate(payload.EffectiveTo)
		if err != nil {
			return nil, nil, err
		}
		if !canonicalCatalogPricePayload(payload) {
			return nil, nil, ErrInvalidInput
		}
		value := catalog.Price{ID: record.ID, ModelID: payload.ModelID, ConfigurationID: payload.ConfigurationID,
			Kind: catalog.PriceKind(payload.Kind), Currency: payload.Currency, AmountMinor: payload.AmountMinor,
			EffectiveFrom: effectiveFrom, EffectiveTo: effectiveTo, SourceReference: payload.SourceReference, Revision: record.Revision}
		return value, catalogPriceDependencies(value), nil
	case "atlas.catalog-upgrade-path":
		payload, err := decodeCatalogPayload[catalogUpgradePathPayload](record.Payload)
		if err != nil {
			return nil, nil, err
		}
		effectiveFrom, err := parseCatalogDate(payload.EffectiveFrom)
		if err != nil {
			return nil, nil, err
		}
		if !canonicalCatalogUpgradePathPayload(payload) {
			return nil, nil, ErrInvalidInput
		}
		value := catalog.UpgradePath{ID: record.ID, FromModelID: payload.FromModelID, FromConfigurationID: payload.FromConfigurationID,
			ToModelID: payload.ToModelID, ToConfigurationID: payload.ToConfigurationID,
			Kind: catalog.UpgradeKind(payload.RelationshipKind), EffectiveFrom: effectiveFrom, Revision: record.Revision}
		return value, catalogUpgradePathDependencies(value), nil
	default:
		return nil, nil, ErrInvalidInput
	}
}

func canonicalCatalogConfigurationPayload(payload catalogConfigurationPayload, specifications map[string]string) bool {
	if payload.ModelID != strings.TrimSpace(payload.ModelID) || payload.Name != strings.TrimSpace(payload.Name) ||
		payload.SKU != strings.TrimSpace(payload.SKU) || payload.Status != strings.ToLower(strings.TrimSpace(payload.Status)) {
		return false
	}
	for key, value := range specifications {
		if key != strings.ToLower(strings.TrimSpace(key)) || value != strings.TrimSpace(value) {
			return false
		}
	}
	return true
}

func canonicalCatalogPricePayload(payload catalogPricePayload) bool {
	return payload.ModelID == strings.TrimSpace(payload.ModelID) &&
		payload.ConfigurationID == strings.TrimSpace(payload.ConfigurationID) &&
		payload.Kind == strings.ToLower(strings.TrimSpace(payload.Kind)) &&
		payload.Currency == strings.ToUpper(strings.TrimSpace(payload.Currency)) &&
		payload.SourceReference == strings.TrimSpace(payload.SourceReference)
}

func canonicalCatalogUpgradePathPayload(payload catalogUpgradePathPayload) bool {
	return payload.FromModelID == strings.TrimSpace(payload.FromModelID) &&
		payload.FromConfigurationID == strings.TrimSpace(payload.FromConfigurationID) &&
		payload.ToModelID == strings.TrimSpace(payload.ToModelID) &&
		payload.ToConfigurationID == strings.TrimSpace(payload.ToConfigurationID) &&
		payload.RelationshipKind == strings.ToLower(strings.TrimSpace(payload.RelationshipKind))
}

func decodeCatalogPayload[T any](payload []byte) (T, error) {
	var result T
	if len(payload) == 0 || len(payload) > MaximumPayloadBytes {
		return result, ErrInvalidInput
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return result, ErrInvalidInput
	}
	return result, nil
}

func catalogConfigurationDependencies(value catalog.Configuration) []Reference {
	return normalizeReferences([]Reference{{Type: "atlas.model", ID: value.ModelID}})
}

func catalogPriceDependencies(value catalog.Price) []Reference {
	result := []Reference{{Type: "atlas.model", ID: value.ModelID}}
	if value.ConfigurationID != "" {
		result = append(result, Reference{Type: "atlas.catalog-configuration", ID: value.ConfigurationID})
	}
	return normalizeReferences(result)
}

func catalogUpgradePathDependencies(value catalog.UpgradePath) []Reference {
	result := []Reference{{Type: "atlas.model", ID: value.FromModelID}, {Type: "atlas.model", ID: value.ToModelID}}
	if value.FromConfigurationID != "" {
		result = append(result, Reference{Type: "atlas.catalog-configuration", ID: value.FromConfigurationID})
	}
	if value.ToConfigurationID != "" {
		result = append(result, Reference{Type: "atlas.catalog-configuration", ID: value.ToConfigurationID})
	}
	return normalizeReferences(result)
}

func canonicalCatalogSpecifications(value map[string]string) (string, error) {
	if value == nil {
		value = map[string]string{}
	}
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > 40_000 {
		return "", ErrInvalidInput
	}
	return string(encoded), nil
}

func catalogDate(value time.Time) string { return value.UTC().Format("2006-01-02") }

func catalogOptionalDate(value *time.Time) string {
	if value == nil {
		return ""
	}
	return catalogDate(*value)
}

func parseCatalogDate(value string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return time.Time{}, ErrInvalidInput
	}
	return parsed, nil
}

func parseCatalogOptionalDate(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := parseCatalogDate(value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func sameCatalogConfiguration(left, right catalog.Configuration) bool {
	leftSpecifications, leftErr := canonicalCatalogSpecifications(left.Specifications)
	rightSpecifications, rightErr := canonicalCatalogSpecifications(right.Specifications)
	return leftErr == nil && rightErr == nil && left.ID == right.ID && left.ModelID == right.ModelID &&
		left.Name == right.Name && left.SKU == right.SKU && left.Status == right.Status &&
		leftSpecifications == rightSpecifications && left.Revision == right.Revision
}

func sameCatalogPrice(left, right catalog.Price) bool {
	return left.ID == right.ID && left.ModelID == right.ModelID && left.ConfigurationID == right.ConfigurationID &&
		left.Kind == right.Kind && left.Currency == right.Currency && left.AmountMinor == right.AmountMinor &&
		catalogDate(left.EffectiveFrom) == catalogDate(right.EffectiveFrom) &&
		catalogOptionalDate(left.EffectiveTo) == catalogOptionalDate(right.EffectiveTo) &&
		left.SourceReference == right.SourceReference && left.Revision == right.Revision
}

func sameCatalogUpgradePath(left, right catalog.UpgradePath) bool {
	return left.ID == right.ID && left.FromModelID == right.FromModelID && left.FromConfigurationID == right.FromConfigurationID &&
		left.ToModelID == right.ToModelID && left.ToConfigurationID == right.ToConfigurationID && left.Kind == right.Kind &&
		catalogDate(left.EffectiveFrom) == catalogDate(right.EffectiveFrom) && left.Revision == right.Revision
}
