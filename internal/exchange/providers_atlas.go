package exchange

// Lossless Atlas and Atlas Codes providers.
// Requirements: REQ-ATLAS-001, REQ-ATLAS-MODELS-001, REQ-ATLAS-CODES-001, REQ-PATTERNS-001, REQ-EXCHANGE-001.
// Features: inventory.assets, inventory.models, inventory.identifiers, templates.schemas, migration.packages. GitHub: #9.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/atlascodes"
	"github.com/maxlemke/stewardmesh/internal/domain"
)

var atlasRecordTypes = []string{"atlas.asset", "atlas.lifecycle-event", "atlas.model"}

type AtlasProvider struct {
	service  *atlas.Service
	importer atlas.ExchangeImporter
}

type atlasModelPayload struct {
	Manufacturer     string `json:"manufacturer"`
	Name             string `json:"name"`
	ModelNumber      string `json:"modelNumber,omitempty"`
	Kind             string `json:"kind"`
	VendorIdentifier string `json:"vendorIdentifier,omitempty"`
	Specifications   string `json:"specifications"`
	SupportURL       string `json:"supportUrl,omitempty"`
	WarrantyMonths   int    `json:"warrantyMonths"`
	UsefulLifeMonths int    `json:"usefulLifeMonths"`
	Status           string `json:"status"`
	SourceSystemID   string `json:"sourceSystemId,omitempty"`
	SourceRecordID   string `json:"sourceRecordId,omitempty"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
}

type atlasAssetPayload struct {
	ModelID         string `json:"modelId,omitempty"`
	ModelContext    string `json:"modelContext,omitempty"`
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	AssetTag        string `json:"assetTag,omitempty"`
	SerialNumber    string `json:"serialNumber,omitempty"`
	Hostname        string `json:"hostname,omitempty"`
	DeploymentNotes string `json:"deploymentNotes,omitempty"`
	SiteID          string `json:"siteId,omitempty"`
	BuildingID      string `json:"buildingId,omitempty"`
	RoomID          string `json:"roomId,omitempty"`
	DepartmentID    string `json:"departmentId,omitempty"`
	UserID          string `json:"userId,omitempty"`
	Status          string `json:"status"`
	PurchaseDate    string `json:"purchaseDate,omitempty"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

type atlasLifecyclePayload struct {
	AssetID    string `json:"assetId"`
	FromStatus string `json:"fromStatus,omitempty"`
	ToStatus   string `json:"toStatus"`
	Note       string `json:"note,omitempty"`
	ActorID    string `json:"actorId"`
	OccurredAt string `json:"occurredAt"`
}

func NewAtlasProvider(service *atlas.Service, importer atlas.ExchangeImporter) (*AtlasProvider, error) {
	if service == nil || importer == nil || !service.OwnsExchangeImporter(importer) {
		return nil, errors.New("Atlas service and its construction-time Exchange importer are required")
	}
	return &AtlasProvider{service: service, importer: importer}, nil
}

func (*AtlasProvider) Types() []string { return append([]string(nil), atlasRecordTypes...) }

func (p *AtlasProvider) ListRecords(ctx context.Context) ([]Record, error) {
	snapshot, err := p.service.ExchangeSnapshot(ctx, MaximumRecords)
	if err != nil {
		if errors.Is(err, atlas.ErrTooLarge) {
			return nil, ErrTooLarge
		}
		return nil, err
	}
	result := make([]Record, 0, len(snapshot.Models)+len(snapshot.Assets)+len(snapshot.LifecycleEvents))
	for _, model := range snapshot.Models {
		if err := validatePortableInstants(1970, model.CreatedAt, model.UpdatedAt); err != nil {
			return nil, err
		}
		specifications, err := canonicalAtlasSpecifications(model.Specifications)
		if err != nil {
			return nil, err
		}
		payload, err := marshalAtlasPayload(atlasModelPayload{
			Manufacturer: model.Manufacturer, Name: model.Name, ModelNumber: model.ModelNumber, Kind: model.Kind,
			VendorIdentifier: model.VendorIdentifier, Specifications: specifications, SupportURL: model.SupportURL,
			WarrantyMonths: model.WarrantyMonths, UsefulLifeMonths: model.UsefulLifeMonths, Status: model.Status,
			SourceSystemID: model.SourceSystemID, SourceRecordID: model.SourceRecordID,
			CreatedAt: atlasInstant(model.CreatedAt), UpdatedAt: atlasInstant(model.UpdatedAt),
		})
		if err != nil {
			return nil, err
		}
		result = append(result, Record{Type: "atlas.model", ID: model.ID, Revision: model.Revision,
			Dependencies: []Reference{}, Provenance: atlasModelProvenance(model), Ownership: OwnershipMetadata{State: "local"}, Payload: payload})
	}
	for _, asset := range snapshot.Assets {
		if err := validatePortableInstants(1970, asset.CreatedAt, asset.UpdatedAt); err != nil {
			return nil, err
		}
		modelContext, err := canonicalAtlasModelContext(asset.ModelContext)
		if err != nil {
			return nil, err
		}
		payload, err := marshalAtlasPayload(atlasAssetPayload{
			ModelID: asset.ModelID, ModelContext: modelContext, Name: asset.Name, Kind: asset.Kind,
			AssetTag: asset.AssetTag, SerialNumber: asset.SerialNumber, Hostname: asset.Hostname,
			DeploymentNotes: asset.DeploymentNotes, SiteID: asset.SiteID, BuildingID: asset.BuildingID,
			RoomID: asset.RoomID, DepartmentID: asset.DepartmentID, UserID: asset.UserID, Status: asset.Status,
			PurchaseDate: atlasOptionalDate(asset.PurchaseDate), CreatedAt: atlasInstant(asset.CreatedAt), UpdatedAt: atlasInstant(asset.UpdatedAt),
		})
		if err != nil {
			return nil, err
		}
		result = append(result, Record{Type: "atlas.asset", ID: asset.ID, Revision: asset.Revision,
			Dependencies: atlasAssetDependencies(asset), Ownership: OwnershipMetadata{State: "local"}, Payload: payload})
	}
	for _, event := range snapshot.LifecycleEvents {
		if err := validatePortableInstants(1970, event.OccurredAt); err != nil {
			return nil, err
		}
		payload, err := marshalAtlasPayload(atlasLifecyclePayload{
			AssetID: event.AssetID, FromStatus: event.FromStatus, ToStatus: event.ToStatus,
			Note: event.Note, ActorID: event.ActorID, OccurredAt: atlasInstant(event.OccurredAt),
		})
		if err != nil {
			return nil, err
		}
		result = append(result, Record{Type: "atlas.lifecycle-event", ID: event.ID, Revision: event.Revision,
			Dependencies: []Reference{{Type: "atlas.asset", ID: event.AssetID}}, Ownership: OwnershipMetadata{State: "local"}, Payload: payload})
	}
	sort.Slice(result, func(i, j int) bool {
		return (Reference{Type: result[i].Type, ID: result[i].ID}).Key() < (Reference{Type: result[j].Type, ID: result[j].ID}).Key()
	})
	return result, nil
}

func (p *AtlasProvider) Exists(ctx context.Context, reference Reference) (bool, error) {
	var err error
	switch reference.Type {
	case "atlas.model":
		_, err = p.service.GetModel(ctx, reference.ID)
	case "atlas.asset":
		_, err = p.service.GetAsset(ctx, reference.ID)
	case "atlas.lifecycle-event":
		_, err = p.service.ExchangeLifecycleEvent(ctx, reference.ID)
	default:
		return false, nil
	}
	if errors.Is(err, atlas.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (p *AtlasProvider) ImportRecordExists(ctx context.Context, record Record, _ []byte) (bool, error) {
	candidate, dependencies, err := decodeAtlasRecord(record)
	if err != nil || !reflect.DeepEqual(dependencies, record.Dependencies) {
		return false, ErrInvalidInput
	}
	switch value := candidate.(type) {
	case domain.AssetModel:
		current, err := p.service.GetModel(ctx, record.ID)
		if errors.Is(err, atlas.ErrNotFound) {
			return false, nil
		}
		current.OrganizationID = ""
		current.InstanceCount = 0
		return err == nil && reflect.DeepEqual(current, value), err
	case domain.Asset:
		current, err := p.service.GetAsset(ctx, record.ID)
		if errors.Is(err, atlas.ErrNotFound) {
			return false, nil
		}
		current.OrganizationID = ""
		return err == nil && reflect.DeepEqual(current, value), err
	case domain.AssetLifecycleEvent:
		current, err := p.service.ExchangeLifecycleEvent(ctx, record.ID)
		if errors.Is(err, atlas.ErrNotFound) {
			return false, nil
		}
		current.OrganizationID = ""
		return err == nil && current == value, err
	default:
		return false, ErrInvalidInput
	}
}

func (p *AtlasProvider) ImportRecord(ctx context.Context, operation ProviderImportOperation, _ string, record Record, _ []byte) (ProviderImportResult, error) {
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
	candidate, dependencies, err := decodeAtlasRecord(record)
	if err != nil || !reflect.DeepEqual(dependencies, record.Dependencies) {
		return ProviderImportResult{}, ErrInvalidInput
	}
	domainOperation := atlas.ExchangeImportOperation{Token: operation.Token, OccurredAt: operation.OccurredAt}
	var result atlas.ExchangeImportResult
	switch value := candidate.(type) {
	case domain.AssetModel:
		result, err = p.importer.ImportModel(ctx, domainOperation, value)
	case domain.Asset:
		result, err = p.importer.ImportAsset(ctx, domainOperation, value)
	case domain.AssetLifecycleEvent:
		result, err = p.importer.ImportLifecycleEvent(ctx, domainOperation, value)
	default:
		return ProviderImportResult{}, ErrInvalidInput
	}
	return translateAtlasImportResult(result, err)
}

func decodeAtlasRecord(record Record) (any, []Reference, error) {
	if record.Revision < 1 || !stableIDPattern.MatchString(record.ID) {
		return nil, nil, ErrInvalidInput
	}
	switch record.Type {
	case "atlas.model":
		payload, err := decodeAtlasPayload[atlasModelPayload](record.Payload)
		if err != nil || !canonicalAtlasModelPayload(payload) {
			return nil, nil, ErrInvalidInput
		}
		specifications, err := parseAtlasSpecifications(payload.Specifications)
		if err != nil {
			return nil, nil, err
		}
		createdAt, err := parseAtlasInstant(payload.CreatedAt)
		if err != nil {
			return nil, nil, err
		}
		updatedAt, err := parseAtlasInstant(payload.UpdatedAt)
		if err != nil {
			return nil, nil, err
		}
		return domain.AssetModel{ID: record.ID, Manufacturer: payload.Manufacturer, Name: payload.Name,
			ModelNumber: payload.ModelNumber, Kind: payload.Kind, VendorIdentifier: payload.VendorIdentifier,
			Specifications: specifications, SupportURL: payload.SupportURL, WarrantyMonths: payload.WarrantyMonths,
			UsefulLifeMonths: payload.UsefulLifeMonths, Status: payload.Status, SourceSystemID: payload.SourceSystemID,
			SourceRecordID: payload.SourceRecordID, Revision: record.Revision, CreatedAt: createdAt, UpdatedAt: updatedAt}, []Reference{}, nil
	case "atlas.asset":
		payload, err := decodeAtlasPayload[atlasAssetPayload](record.Payload)
		if err != nil || !canonicalAtlasAssetPayload(payload) {
			return nil, nil, ErrInvalidInput
		}
		modelContext, err := parseAtlasModelContext(payload.ModelContext)
		if err != nil {
			return nil, nil, err
		}
		purchaseDate, err := parseAtlasOptionalDate(payload.PurchaseDate)
		if err != nil {
			return nil, nil, err
		}
		createdAt, err := parseAtlasInstant(payload.CreatedAt)
		if err != nil {
			return nil, nil, err
		}
		updatedAt, err := parseAtlasInstant(payload.UpdatedAt)
		if err != nil {
			return nil, nil, err
		}
		value := domain.Asset{ID: record.ID, ModelID: payload.ModelID, ModelContext: modelContext, Name: payload.Name,
			Kind: payload.Kind, AssetTag: payload.AssetTag, SerialNumber: payload.SerialNumber, Hostname: payload.Hostname,
			DeploymentNotes: payload.DeploymentNotes, SiteID: payload.SiteID, BuildingID: payload.BuildingID,
			RoomID: payload.RoomID, DepartmentID: payload.DepartmentID, UserID: payload.UserID, Status: payload.Status,
			PurchaseDate: purchaseDate, Revision: record.Revision, CreatedAt: createdAt, UpdatedAt: updatedAt}
		return value, atlasAssetDependencies(value), nil
	case "atlas.lifecycle-event":
		payload, err := decodeAtlasPayload[atlasLifecyclePayload](record.Payload)
		if err != nil || !canonicalAtlasLifecyclePayload(payload) {
			return nil, nil, ErrInvalidInput
		}
		occurredAt, err := parseAtlasInstant(payload.OccurredAt)
		if err != nil {
			return nil, nil, err
		}
		value := domain.AssetLifecycleEvent{ID: record.ID, AssetID: payload.AssetID, FromStatus: payload.FromStatus,
			ToStatus: payload.ToStatus, Note: payload.Note, Revision: record.Revision, ActorID: payload.ActorID, OccurredAt: occurredAt}
		return value, []Reference{{Type: "atlas.asset", ID: value.AssetID}}, nil
	default:
		return nil, nil, ErrInvalidInput
	}
}

func translateAtlasImportResult(result atlas.ExchangeImportResult, err error) (ProviderImportResult, error) {
	providerResult := ProviderImportResult{Committed: result.Committed, Created: result.Created}
	switch {
	case errors.Is(err, atlas.ErrInvalidInput):
		return providerResult, ErrInvalidInput
	case errors.Is(err, atlas.ErrConflict):
		return providerResult, ErrConflict
	case errors.Is(err, atlas.ErrReferenceMissing), errors.Is(err, atlas.ErrNotFound):
		return providerResult, ErrDependencyMissing
	case errors.Is(err, atlas.ErrTooLarge):
		return providerResult, ErrTooLarge
	default:
		return providerResult, err
	}
}

func atlasAssetDependencies(asset domain.Asset) []Reference {
	result := []Reference{}
	if asset.ModelID != "" {
		result = append(result, Reference{Type: "atlas.model", ID: asset.ModelID})
	}
	for _, candidate := range []Reference{
		{Type: "people.site", ID: asset.SiteID}, {Type: "people.building", ID: asset.BuildingID},
		{Type: "people.room", ID: asset.RoomID}, {Type: "people.department", ID: asset.DepartmentID},
		{Type: "people.identity", ID: asset.UserID},
	} {
		if candidate.ID != "" {
			result = append(result, candidate)
		}
	}
	return normalizeReferences(result)
}

func atlasModelProvenance(model domain.AssetModel) Provenance {
	if stableIDPattern.MatchString(model.SourceSystemID) && safeSourceRecordID(model.SourceRecordID) {
		return Provenance{SourceSystemID: model.SourceSystemID, SourceRecordID: model.SourceRecordID}
	}
	return Provenance{}
}

func marshalAtlasPayload(value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil || len(payload) == 0 || len(payload) > MaximumPayloadBytes {
		return nil, ErrTooLarge
	}
	return payload, nil
}

func decodeAtlasPayload[T any](payload []byte) (T, error) {
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
	if !canonicalJSONEqual(payload, result) {
		return result, ErrInvalidInput
	}
	return result, nil
}

func canonicalAtlasModelPayload(value atlasModelPayload) bool {
	return value.Manufacturer == strings.TrimSpace(value.Manufacturer) && value.Name == strings.TrimSpace(value.Name) &&
		value.ModelNumber == strings.TrimSpace(value.ModelNumber) && value.Kind == strings.ToLower(strings.TrimSpace(value.Kind)) &&
		value.VendorIdentifier == strings.TrimSpace(value.VendorIdentifier) && value.SupportURL == strings.TrimSpace(value.SupportURL) &&
		value.Status == strings.ToLower(strings.TrimSpace(value.Status)) && value.SourceSystemID == strings.TrimSpace(value.SourceSystemID) &&
		value.SourceRecordID == strings.TrimSpace(value.SourceRecordID)
}

func canonicalAtlasAssetPayload(value atlasAssetPayload) bool {
	return value.ModelID == strings.TrimSpace(value.ModelID) && value.Name == strings.TrimSpace(value.Name) &&
		value.Kind == strings.ToLower(strings.TrimSpace(value.Kind)) && value.AssetTag == strings.TrimSpace(value.AssetTag) &&
		value.SerialNumber == strings.TrimSpace(value.SerialNumber) && value.Hostname == strings.ToLower(strings.TrimSpace(value.Hostname)) &&
		value.DeploymentNotes == strings.TrimSpace(value.DeploymentNotes) && value.SiteID == strings.TrimSpace(value.SiteID) &&
		value.BuildingID == strings.TrimSpace(value.BuildingID) && value.RoomID == strings.TrimSpace(value.RoomID) &&
		value.DepartmentID == strings.TrimSpace(value.DepartmentID) && value.UserID == strings.TrimSpace(value.UserID) &&
		value.Status == strings.ToLower(strings.TrimSpace(value.Status))
}

func canonicalAtlasLifecyclePayload(value atlasLifecyclePayload) bool {
	return value.AssetID == strings.TrimSpace(value.AssetID) && value.FromStatus == strings.ToLower(strings.TrimSpace(value.FromStatus)) &&
		value.ToStatus == strings.ToLower(strings.TrimSpace(value.ToStatus)) && value.Note == strings.TrimSpace(value.Note) &&
		value.ActorID == strings.TrimSpace(value.ActorID)
}

func canonicalAtlasSpecifications(value map[string]string) (string, error) {
	if value == nil {
		value = map[string]string{}
	}
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > 15_000 {
		return "", ErrInvalidInput
	}
	return string(encoded), nil
}

func parseAtlasSpecifications(value string) (map[string]string, error) {
	var result map[string]string
	if err := json.Unmarshal([]byte(value), &result); err != nil || result == nil {
		return nil, ErrInvalidInput
	}
	canonical, err := canonicalAtlasSpecifications(result)
	if err != nil || canonical != value {
		return nil, ErrInvalidInput
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

func canonicalAtlasModelContext(value *domain.AssetModelContext) (string, error) {
	if value == nil {
		return "", nil
	}
	if err := validatePortableInstants(1970, value.DefaultsEffectiveAt, value.AppliedAt); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > 20_000 {
		return "", ErrInvalidInput
	}
	return string(encoded), nil
}

func parseAtlasModelContext(value string) (*domain.AssetModelContext, error) {
	if value == "" {
		return nil, nil
	}
	context, err := decodeAtlasPayload[domain.AssetModelContext]([]byte(value))
	if err != nil {
		return nil, err
	}
	canonical, err := canonicalAtlasModelContext(&context)
	if err != nil || canonical != value {
		return nil, ErrInvalidInput
	}
	return &context, nil
}

func atlasInstant(value time.Time) string { return value.Format(time.RFC3339Nano) }

func parseAtlasInstant(value string) (time.Time, error) {
	return parsePortableInstant(value, 1970)
}

func atlasOptionalDate(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format("2006-01-02")
}

func parseAtlasOptionalDate(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return nil, ErrInvalidInput
	}
	return &parsed, nil
}

// Atlas Codes history is chunked into canonical JSON arrays so every Patterns
// text field remains under its 100,000-character bound without splitting a
// Unicode value or relying on whitespace-sensitive concatenation.
const (
	atlasIdentifierHistoryChunks    = 10
	atlasIdentifierHistoryChunkSize = 90_000
)

type AtlasCodesProvider struct {
	service  *atlascodes.Service
	importer atlascodes.ExchangeImporter
}

type atlasIdentifierPayload struct {
	AssetID   string `json:"assetId"`
	History01 string `json:"history01"`
	History02 string `json:"history02,omitempty"`
	History03 string `json:"history03,omitempty"`
	History04 string `json:"history04,omitempty"`
	History05 string `json:"history05,omitempty"`
	History06 string `json:"history06,omitempty"`
	History07 string `json:"history07,omitempty"`
	History08 string `json:"history08,omitempty"`
	History09 string `json:"history09,omitempty"`
	History10 string `json:"history10,omitempty"`
}

func (p atlasIdentifierPayload) chunks() []string {
	return []string{p.History01, p.History02, p.History03, p.History04, p.History05, p.History06, p.History07, p.History08, p.History09, p.History10}
}

type atlasIdentifierHistoryRow struct {
	ID                   string `json:"id"`
	Symbology            string `json:"symbology"`
	NormalizedValue      string `json:"normalizedValue"`
	DisplayValue         string `json:"displayValue"`
	Source               string `json:"source"`
	Primary              bool   `json:"primary"`
	Status               string `json:"status"`
	SupersedesID         string `json:"supersedesId,omitempty"`
	ReplacedByID         string `json:"replacedById,omitempty"`
	Revision             int64  `json:"revision"`
	CreatedBy            string `json:"createdBy"`
	CreatedCorrelationID string `json:"createdCorrelationId"`
	UpdatedBy            string `json:"updatedBy"`
	UpdatedCorrelationID string `json:"updatedCorrelationId"`
	CreatedAt            string `json:"createdAt"`
	UpdatedAt            string `json:"updatedAt"`
	DeactivatedAt        string `json:"deactivatedAt,omitempty"`
}

func NewAtlasCodesProvider(service *atlascodes.Service, importer atlascodes.ExchangeImporter) (*AtlasCodesProvider, error) {
	if service == nil || importer == nil || !service.OwnsExchangeImporter(importer) {
		return nil, errors.New("Atlas Codes service and its construction-time Exchange importer are required")
	}
	return &AtlasCodesProvider{service: service, importer: importer}, nil
}

func (*AtlasCodesProvider) Types() []string { return []string{"atlas.identifier"} }

func (p *AtlasCodesProvider) ListRecords(ctx context.Context) ([]Record, error) {
	chains, err := p.service.ExchangeIdentifierChains(ctx, MaximumRecords)
	if err != nil {
		if errors.Is(err, atlascodes.ErrTooLarge) {
			return nil, ErrTooLarge
		}
		return nil, err
	}
	result := make([]Record, 0, len(chains))
	for _, chain := range chains {
		payload, err := encodeAtlasIdentifierPayload(chain)
		if err != nil {
			return nil, err
		}
		terminal := chain.Items[len(chain.Items)-1]
		result = append(result, Record{Type: "atlas.identifier", ID: chain.TerminalID, Revision: terminal.Revision,
			Dependencies: []Reference{{Type: "atlas.asset", ID: terminal.AssetID}}, Ownership: OwnershipMetadata{State: "local"}, Payload: payload})
	}
	return result, nil
}

func (p *AtlasCodesProvider) Exists(ctx context.Context, reference Reference) (bool, error) {
	if reference.Type != "atlas.identifier" {
		return false, nil
	}
	_, err := p.service.ExchangeIdentifierChain(ctx, reference.ID)
	if errors.Is(err, atlascodes.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (p *AtlasCodesProvider) ImportRecordExists(ctx context.Context, record Record, _ []byte) (bool, error) {
	chain, dependencies, err := decodeAtlasIdentifierRecord(record)
	if err != nil || !reflect.DeepEqual(dependencies, record.Dependencies) {
		return false, ErrInvalidInput
	}
	current, err := p.service.ExchangeIdentifierChain(ctx, record.ID)
	if errors.Is(err, atlascodes.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for index := range current.Items {
		current.Items[index].OrganizationID = ""
	}
	return reflect.DeepEqual(current, chain), nil
}

func (p *AtlasCodesProvider) ImportRecord(ctx context.Context, operation ProviderImportOperation, _ string, record Record, _ []byte) (ProviderImportResult, error) {
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
	chain, dependencies, err := decodeAtlasIdentifierRecord(record)
	if err != nil || !reflect.DeepEqual(dependencies, record.Dependencies) {
		return ProviderImportResult{}, ErrInvalidInput
	}
	result, err := p.importer.ImportIdentifierChain(ctx, atlascodes.ExchangeImportOperation{Token: operation.Token, OccurredAt: operation.OccurredAt}, chain)
	providerResult := ProviderImportResult{Committed: result.Committed, Created: result.Created}
	switch {
	case errors.Is(err, atlascodes.ErrInvalidInput):
		return providerResult, ErrInvalidInput
	case errors.Is(err, atlascodes.ErrConflict):
		return providerResult, ErrConflict
	case errors.Is(err, atlascodes.ErrReferenceMissing), errors.Is(err, atlascodes.ErrNotFound), errors.Is(err, atlas.ErrNotFound):
		return providerResult, ErrDependencyMissing
	case errors.Is(err, atlascodes.ErrTooLarge):
		return providerResult, ErrTooLarge
	default:
		return providerResult, err
	}
}

func encodeAtlasIdentifierPayload(chain atlascodes.IdentifierChain) ([]byte, error) {
	if len(chain.Items) == 0 {
		return nil, ErrInvalidInput
	}
	rows := make([]atlasIdentifierHistoryRow, len(chain.Items))
	for index, item := range chain.Items {
		if err := validatePortableInstants(1970, item.CreatedAt, item.UpdatedAt); err != nil {
			return nil, err
		}
		if err := validateOptionalPortableInstant(1970, item.DeactivatedAt); err != nil {
			return nil, err
		}
		rows[index] = atlasIdentifierRow(item)
	}
	chunks := make([]string, 0, atlasIdentifierHistoryChunks)
	for len(rows) > 0 {
		count := 1
		var encoded []byte
		for count <= len(rows) {
			candidate, err := json.Marshal(rows[:count])
			if err != nil {
				return nil, ErrInvalidInput
			}
			if len(candidate) > atlasIdentifierHistoryChunkSize {
				break
			}
			encoded = candidate
			count++
		}
		if len(encoded) == 0 || len(chunks) == atlasIdentifierHistoryChunks {
			return nil, ErrTooLarge
		}
		used := count - 1
		chunks = append(chunks, string(encoded))
		rows = rows[used:]
	}
	for len(chunks) < atlasIdentifierHistoryChunks {
		chunks = append(chunks, "")
	}
	payload := atlasIdentifierPayload{AssetID: chain.Items[0].AssetID, History01: chunks[0], History02: chunks[1],
		History03: chunks[2], History04: chunks[3], History05: chunks[4], History06: chunks[5], History07: chunks[6],
		History08: chunks[7], History09: chunks[8], History10: chunks[9]}
	return marshalAtlasPayload(payload)
}

func decodeAtlasIdentifierRecord(record Record) (atlascodes.IdentifierChain, []Reference, error) {
	if record.Type != "atlas.identifier" || record.Revision < 1 || !stableIDPattern.MatchString(record.ID) {
		return atlascodes.IdentifierChain{}, nil, ErrInvalidInput
	}
	payload, err := decodeAtlasPayload[atlasIdentifierPayload](record.Payload)
	if err != nil || payload.AssetID != strings.TrimSpace(payload.AssetID) {
		return atlascodes.IdentifierChain{}, nil, ErrInvalidInput
	}
	rows := []atlasIdentifierHistoryRow{}
	gap := false
	for _, chunk := range payload.chunks() {
		if chunk == "" {
			gap = true
			continue
		}
		if gap || len(chunk) > atlasIdentifierHistoryChunkSize {
			return atlascodes.IdentifierChain{}, nil, ErrInvalidInput
		}
		var values []atlasIdentifierHistoryRow
		decoder := json.NewDecoder(strings.NewReader(chunk))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&values); err != nil || len(values) == 0 {
			return atlascodes.IdentifierChain{}, nil, ErrInvalidInput
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return atlascodes.IdentifierChain{}, nil, ErrInvalidInput
		}
		canonical, err := json.Marshal(values)
		if err != nil || string(canonical) != chunk {
			return atlascodes.IdentifierChain{}, nil, ErrInvalidInput
		}
		rows = append(rows, values...)
	}
	if len(rows) == 0 || rows[len(rows)-1].ID != record.ID || rows[len(rows)-1].Revision != record.Revision {
		return atlascodes.IdentifierChain{}, nil, ErrInvalidInput
	}
	chain := atlascodes.IdentifierChain{TerminalID: record.ID, Items: make([]atlascodes.Identifier, len(rows))}
	for index, row := range rows {
		item, err := parseAtlasIdentifierRow(payload.AssetID, row)
		if err != nil {
			return atlascodes.IdentifierChain{}, nil, err
		}
		chain.Items[index] = item
	}
	return chain, []Reference{{Type: "atlas.asset", ID: payload.AssetID}}, nil
}

func atlasIdentifierRow(item atlascodes.Identifier) atlasIdentifierHistoryRow {
	return atlasIdentifierHistoryRow{ID: item.ID, Symbology: string(item.Symbology), NormalizedValue: item.NormalizedValue,
		DisplayValue: item.DisplayValue, Source: string(item.Source), Primary: item.Primary, Status: string(item.Status),
		SupersedesID: item.SupersedesID, ReplacedByID: item.ReplacedByID, Revision: item.Revision,
		CreatedBy: item.CreatedBy, CreatedCorrelationID: item.CreatedCorrelationID, UpdatedBy: item.UpdatedBy,
		UpdatedCorrelationID: item.UpdatedCorrelationID, CreatedAt: atlasInstant(item.CreatedAt), UpdatedAt: atlasInstant(item.UpdatedAt),
		DeactivatedAt: atlasOptionalInstant(item.DeactivatedAt)}
}

func parseAtlasIdentifierRow(assetID string, row atlasIdentifierHistoryRow) (atlascodes.Identifier, error) {
	createdAt, err := parseAtlasInstant(row.CreatedAt)
	if err != nil {
		return atlascodes.Identifier{}, err
	}
	updatedAt, err := parseAtlasInstant(row.UpdatedAt)
	if err != nil {
		return atlascodes.Identifier{}, err
	}
	deactivatedAt, err := parseAtlasOptionalInstant(row.DeactivatedAt)
	if err != nil {
		return atlascodes.Identifier{}, err
	}
	return atlascodes.Identifier{ID: row.ID, AssetID: assetID, Symbology: atlascodes.Symbology(row.Symbology),
		NormalizedValue: row.NormalizedValue, DisplayValue: row.DisplayValue, Source: atlascodes.Source(row.Source),
		Primary: row.Primary, Status: atlascodes.Status(row.Status), SupersedesID: row.SupersedesID,
		ReplacedByID: row.ReplacedByID, Revision: row.Revision, CreatedBy: row.CreatedBy,
		CreatedCorrelationID: row.CreatedCorrelationID, UpdatedBy: row.UpdatedBy,
		UpdatedCorrelationID: row.UpdatedCorrelationID, CreatedAt: createdAt, UpdatedAt: updatedAt,
		DeactivatedAt: deactivatedAt}, nil
}

func atlasOptionalInstant(value *time.Time) string {
	if value == nil {
		return ""
	}
	return atlasInstant(*value)
}

func parseAtlasOptionalInstant(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := parseAtlasInstant(value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
