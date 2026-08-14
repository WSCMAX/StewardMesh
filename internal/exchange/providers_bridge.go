package exchange

// Requirements: REQ-EXCHANGE-001, REQ-API-001, SEC-MCP-001, REQ-PATTERNS-001, REQ-ATLAS-MODELS-001.
// Features: migration.packages, integrations.protocols, templates.schemas, inventory.models. GitHub: #9, #14, #68.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/bridge"
)

const bridgeOAuthClientRecordType = "bridge.oauth-client"

type BridgeProvider struct {
	service  *bridge.Service
	importer bridge.ExchangeImporter
}

// bridgeOAuthClientPayload is intentionally limited to public PKCE client
// configuration. Secrets, token hashes, grants, consent transactions,
// authorization codes, creator identity, and organization state have no field.
type bridgeOAuthClientPayload struct {
	AllowedScopes string `json:"allowedScopes"`
	Name          string `json:"name"`
	RedirectURIs  string `json:"redirectUris"`
	RevokedAt     string `json:"revokedAt,omitempty"`
}

func NewBridgeProvider(service *bridge.Service, importer bridge.ExchangeImporter) (*BridgeProvider, error) {
	if service == nil || importer == nil || !service.OwnsExchangeImporter(importer) {
		return nil, errors.New("Bridge service and its construction-time Exchange importer are required")
	}
	return &BridgeProvider{service: service, importer: importer}, nil
}

func (*BridgeProvider) Types() []string { return []string{bridgeOAuthClientRecordType} }

func (p *BridgeProvider) ListRecords(ctx context.Context) ([]Record, error) {
	clients, err := p.service.ExchangeClients(ctx, MaximumRecords)
	if errors.Is(err, bridge.ErrTooLarge) {
		return nil, ErrTooLarge
	}
	if err != nil {
		return nil, err
	}
	result := make([]Record, 0, len(clients))
	for _, client := range clients {
		payload, err := encodeBridgeOAuthClientPayload(client)
		if err != nil {
			return nil, err
		}
		result = append(result, Record{
			Type: bridgeOAuthClientRecordType, ID: client.ID, Revision: bridgeClientRevision(client),
			Dependencies: []Reference{}, Ownership: OwnershipMetadata{State: "local"}, Payload: payload,
		})
	}
	return result, nil
}

func (p *BridgeProvider) Exists(ctx context.Context, reference Reference) (bool, error) {
	if reference.Type != bridgeOAuthClientRecordType {
		return false, nil
	}
	_, err := p.service.ExchangeClient(ctx, reference.ID)
	if errors.Is(err, bridge.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (p *BridgeProvider) ImportRecordExists(ctx context.Context, record Record, _ []byte) (bool, error) {
	candidate, err := decodeBridgeOAuthClientRecord(record)
	if err != nil {
		return false, err
	}
	existing, err := p.service.ExchangeClient(ctx, record.ID)
	if errors.Is(err, bridge.ErrNotFound) {
		return false, nil
	}
	return err == nil && sameBridgeOAuthClient(existing, candidate) && bridgeClientRevision(existing) == record.Revision, err
}

func (p *BridgeProvider) ImportRecord(ctx context.Context, operation ProviderImportOperation, _ string, record Record, file []byte) (ProviderImportResult, error) {
	if len(file) != 0 {
		return ProviderImportResult{}, ErrInvalidInput
	}
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
	candidate, err := decodeBridgeOAuthClientRecord(record)
	if err != nil {
		return ProviderImportResult{}, err
	}
	result, err := p.importer.ImportClient(ctx, bridge.ExchangeImportOperation{Token: operation.Token, OccurredAt: operation.OccurredAt}, record.Revision, candidate)
	providerResult := ProviderImportResult{Committed: result.Committed, Created: result.Created}
	switch {
	case errors.Is(err, bridge.ErrInvalidInput):
		return providerResult, ErrInvalidInput
	case errors.Is(err, bridge.ErrConflict):
		return providerResult, ErrConflict
	case errors.Is(err, bridge.ErrTooLarge):
		return providerResult, ErrTooLarge
	default:
		return providerResult, err
	}
}

func encodeBridgeOAuthClientPayload(client bridge.Client) ([]byte, error) {
	if client.OrganizationID == "" || client.CreatedBy == "" || client.CreatedAt.IsZero() {
		return nil, ErrInvalidInput
	}
	candidate := client
	candidate.OrganizationID, candidate.CreatedBy, candidate.CreatedAt = "", "", time.Time{}
	revision := bridgeClientRevision(candidate)
	if err := bridge.ValidateExchangeClient(candidate, revision); err != nil {
		return nil, ErrInvalidInput
	}
	payload := bridgeOAuthClientPayload{
		Name: candidate.Name, RedirectURIs: strings.Join(candidate.RedirectURIs, "\n"),
		AllowedScopes: bridgeScopeList(candidate.AllowedScopes),
	}
	if candidate.RevokedAt != nil {
		if err := validateOptionalPortableInstant(2000, candidate.RevokedAt); err != nil {
			return nil, err
		}
		payload.RevokedAt = candidate.RevokedAt.Format(time.RFC3339Nano)
	}
	encoded, err := json.Marshal(payload)
	if err != nil || len(encoded) == 0 || len(encoded) > MaximumPayloadBytes {
		return nil, ErrInvalidInput
	}
	return encoded, nil
}

func decodeBridgeOAuthClientRecord(record Record) (bridge.Client, error) {
	if record.Type != bridgeOAuthClientRecordType || record.File != nil || len(record.Dependencies) != 0 ||
		(record.Revision != 1 && record.Revision != 2) || len(record.Payload) == 0 || len(record.Payload) > MaximumPayloadBytes {
		return bridge.Client{}, ErrInvalidInput
	}
	var payload bridgeOAuthClientPayload
	decoder := json.NewDecoder(bytes.NewReader(record.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return bridge.Client{}, ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return bridge.Client{}, ErrInvalidInput
	}
	redirects := strings.Split(payload.RedirectURIs, "\n")
	rawScopes := strings.Split(payload.AllowedScopes, ",")
	scopes := make([]bridge.Scope, len(rawScopes))
	for index, scope := range rawScopes {
		scopes[index] = bridge.Scope(scope)
	}
	candidate := bridge.Client{ID: record.ID, Name: payload.Name, RedirectURIs: redirects, AllowedScopes: scopes}
	if payload.RevokedAt != "" {
		revokedAt, err := parsePortableInstant(payload.RevokedAt, 2000)
		if err != nil {
			return bridge.Client{}, ErrInvalidInput
		}
		candidate.RevokedAt = &revokedAt
	}
	if err := bridge.ValidateExchangeClient(candidate, record.Revision); err != nil {
		return bridge.Client{}, ErrInvalidInput
	}
	if !canonicalJSONEqual(record.Payload, payload) {
		return bridge.Client{}, ErrInvalidInput
	}
	return candidate, nil
}

func bridgeClientRevision(client bridge.Client) int64 {
	if client.RevokedAt != nil {
		return 2
	}
	return 1
}

func bridgeScopeList(scopes []bridge.Scope) string {
	values := make([]string, len(scopes))
	for index, scope := range scopes {
		values[index] = string(scope)
	}
	return strings.Join(values, ",")
}

func sameBridgeOAuthClient(left, right bridge.Client) bool {
	return left.ID == right.ID && left.Name == right.Name && slices.Equal(left.RedirectURIs, right.RedirectURIs) &&
		slices.Equal(left.AllowedScopes, right.AllowedScopes) &&
		(left.RevokedAt == nil && right.RevokedAt == nil || left.RevokedAt != nil && right.RevokedAt != nil && left.RevokedAt.Equal(*right.RevokedAt))
}
