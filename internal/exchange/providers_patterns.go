package exchange

// Requirements: REQ-EXCHANGE-001, REQ-PATTERNS-001. Features: migration.packages, templates.schemas. GitHub: #9, #8.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"

	"github.com/maxlemke/stewardmesh/internal/patterns"
)

const patternsTemplateRecordType = "patterns.template"

type PatternsProvider struct {
	service  *patterns.Service
	importer patterns.ExchangeImporter
}

type patternsTemplatePayload struct {
	RecordType string `json:"recordType"`
	Name       string `json:"name"`
	Versions   string `json:"versions"`
}

func NewPatternsProvider(service *patterns.Service, importer patterns.ExchangeImporter) (*PatternsProvider, error) {
	if service == nil || importer == nil || !service.OwnsExchangeImporter(importer) {
		return nil, errors.New("Patterns service and its construction-time Exchange importer are required")
	}
	return &PatternsProvider{service: service, importer: importer}, nil
}

func (*PatternsProvider) Types() []string { return []string{patternsTemplateRecordType} }

func (p *PatternsProvider) ListRecords(ctx context.Context) ([]Record, error) {
	items, err := p.service.ExchangeTemplates(ctx)
	if err != nil {
		return nil, err
	}
	if len(items) > MaximumRecords {
		return nil, ErrTooLarge
	}
	result := make([]Record, 0, len(items))
	for _, item := range items {
		payload, err := encodePatternsTemplatePayload(item)
		if err != nil {
			return nil, err
		}
		result = append(result, Record{Type: patternsTemplateRecordType, ID: item.ID,
			Revision: item.Versions[len(item.Versions)-1].Version, Dependencies: []Reference{},
			Ownership: OwnershipMetadata{State: "local"}, Payload: payload})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (p *PatternsProvider) Exists(ctx context.Context, reference Reference) (bool, error) {
	if reference.Type != patternsTemplateRecordType {
		return false, nil
	}
	_, err := p.service.ExchangeTemplate(ctx, reference.ID)
	if errors.Is(err, patterns.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (p *PatternsProvider) ImportRecordExists(ctx context.Context, record Record, _ []byte) (bool, error) {
	candidate, err := decodePatternsTemplateRecord(record)
	if err != nil {
		return false, err
	}
	existing, err := p.service.ExchangeTemplate(ctx, record.ID)
	if errors.Is(err, patterns.ErrNotFound) {
		return false, nil
	}
	return err == nil && samePatternsTemplate(existing, candidate), err
}

func (p *PatternsProvider) ImportRecord(ctx context.Context, operation ProviderImportOperation, _ string, record Record, _ []byte) (ProviderImportResult, error) {
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
	candidate, err := decodePatternsTemplateRecord(record)
	if err != nil {
		return ProviderImportResult{}, err
	}
	result, err := p.importer.ImportTemplate(ctx, patterns.ExchangeImportOperation{Token: operation.Token, OccurredAt: operation.OccurredAt}, candidate)
	providerResult := ProviderImportResult{Committed: result.Committed, Created: result.Created}
	switch {
	case errors.Is(err, patterns.ErrInvalidInput):
		return providerResult, ErrInvalidInput
	case errors.Is(err, patterns.ErrConflict):
		return providerResult, ErrConflict
	default:
		return providerResult, err
	}
}

func encodePatternsTemplatePayload(item patterns.ExchangeTemplate) ([]byte, error) {
	versions, err := json.Marshal(item.Versions)
	if err != nil || len(versions) > patterns.MaximumExchangeHistoryBytes || !safePatternsVersions(versions) {
		return nil, ErrInvalidInput
	}
	payload, err := json.Marshal(patternsTemplatePayload{RecordType: item.RecordType, Name: item.Name, Versions: string(versions)})
	if err != nil || len(payload) > MaximumPayloadBytes {
		return nil, ErrInvalidInput
	}
	return payload, nil
}

func decodePatternsTemplateRecord(record Record) (patterns.ExchangeTemplate, error) {
	if record.Type != patternsTemplateRecordType || !stableIDPattern.MatchString(record.ID) || len(record.Dependencies) != 0 || record.Revision < 1 {
		return patterns.ExchangeTemplate{}, ErrInvalidInput
	}
	var payload patternsTemplatePayload
	decoder := json.NewDecoder(bytes.NewReader(record.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return patterns.ExchangeTemplate{}, ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return patterns.ExchangeTemplate{}, ErrInvalidInput
	}
	if !canonicalJSONEqual(record.Payload, payload) {
		return patterns.ExchangeTemplate{}, ErrInvalidInput
	}
	var versions []patterns.ExchangeTemplateVersion
	versionDecoder := json.NewDecoder(strings.NewReader(payload.Versions))
	versionDecoder.DisallowUnknownFields()
	if err := versionDecoder.Decode(&versions); err != nil {
		return patterns.ExchangeTemplate{}, ErrInvalidInput
	}
	if err := versionDecoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) || len(payload.Versions) > patterns.MaximumExchangeHistoryBytes ||
		!safePatternsVersions([]byte(payload.Versions)) || len(versions) == 0 || int64(len(versions)) != record.Revision {
		return patterns.ExchangeTemplate{}, ErrInvalidInput
	}
	candidate := patterns.ExchangeTemplate{ID: record.ID, RecordType: payload.RecordType, Name: payload.Name, Versions: versions}
	if !canonicalJSONEqual([]byte(payload.Versions), candidate.Versions) || patterns.ValidateExchangeTemplate(candidate) != nil {
		return patterns.ExchangeTemplate{}, ErrInvalidInput
	}
	return candidate, nil
}

func safePatternsVersions(encoded []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil || unsafeJSONValue(value) {
		return false
	}
	return errors.Is(decoder.Decode(&struct{}{}), io.EOF)
}

func samePatternsTemplate(left, right patterns.ExchangeTemplate) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}
