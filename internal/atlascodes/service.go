package atlascodes

// Requirement: REQ-ATLAS-CODES-001. Feature: inventory.identifiers.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/foundation"
)

const (
	maximumCode128Bytes = 128
	maximumQRBytes      = 512
	maximumDisplayBytes = 512
)

var stableIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type ServiceConfig struct {
	OrganizationID string
	Now            func() time.Time
}

type Service struct {
	store          Store
	assets         AssetReader
	auditor        foundation.Auditor
	organizationID string
	now            func() time.Time
}

func NewService(store Store, assets AssetReader, auditor foundation.Auditor, configuration ServiceConfig) (*Service, error) {
	if store == nil || assets == nil || auditor == nil {
		return nil, errors.New("Atlas Codes store, asset reader, and auditor are required")
	}
	configuration.OrganizationID = strings.TrimSpace(configuration.OrganizationID)
	if configuration.OrganizationID == "" {
		return nil, errors.New("Atlas Codes organization id is required")
	}
	if configuration.Now == nil {
		configuration.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		store: store, assets: assets, auditor: auditor,
		organizationID: configuration.OrganizationID, now: configuration.Now,
	}, nil
}

func (s *Service) ResolveIdentifier(ctx context.Context, symbology Symbology, value string) (Identifier, error) {
	normalizedSymbology, normalizedValue, err := normalizeCode(symbology, value)
	if err != nil {
		return Identifier{}, err
	}
	identifier, err := s.store.ResolveIdentifier(ctx, s.organizationID, normalizedSymbology, normalizedValue)
	if err != nil {
		return Identifier{}, err
	}
	if identifier.OrganizationID != s.organizationID || identifier.Status != StatusActive {
		return Identifier{}, ErrNotFound
	}
	if _, err := s.asset(ctx, identifier.AssetID); err != nil {
		return Identifier{}, err
	}
	return cloneIdentifier(identifier), nil
}

func (s *Service) ListIdentifiers(ctx context.Context, assetID string) ([]Identifier, error) {
	assetID = strings.TrimSpace(assetID)
	if !stableIDPattern.MatchString(assetID) {
		return nil, ErrInvalidInput
	}
	if _, err := s.asset(ctx, assetID); err != nil {
		return nil, err
	}
	identifiers, err := s.store.ListIdentifiers(ctx, s.organizationID, assetID)
	if err != nil {
		return nil, err
	}
	result := make([]Identifier, 0, len(identifiers))
	for _, identifier := range identifiers {
		if identifier.OrganizationID != s.organizationID || identifier.AssetID != assetID {
			return nil, fmt.Errorf("list Atlas Codes identifiers: provider returned a record outside the requested organization or asset")
		}
		result = append(result, cloneIdentifier(identifier))
	}
	return result, nil
}

// CreateIdentifier returns created=false when a stable-ID or same-intent retry
// matched the already persisted association. The deterministic audit event is
// replayed on retries so a prior audit failure can be repaired without a
// duplicate event.
func (s *Service) CreateIdentifier(ctx context.Context, input CreateIdentifierInput) (Identifier, bool, error) {
	normalized, err := normalizeCreateInput(input)
	if err != nil {
		return Identifier{}, false, err
	}
	if _, err := s.asset(ctx, normalized.AssetID); err != nil {
		return Identifier{}, false, err
	}
	id, err := s.identifierID(normalized.ID)
	if err != nil {
		return Identifier{}, false, err
	}
	actorID, correlationID, err := mutationProvenance(ctx)
	if err != nil {
		return Identifier{}, false, err
	}
	now := s.now().UTC()
	identifier := Identifier{
		ID: id, OrganizationID: s.organizationID, AssetID: normalized.AssetID,
		Symbology: normalized.Symbology, NormalizedValue: normalized.Value,
		DisplayValue: normalized.DisplayValue, Source: normalized.Source,
		Primary: normalized.Primary, Status: StatusActive, Revision: 1,
		CreatedBy: actorID, CreatedCorrelationID: correlationID,
		UpdatedBy: actorID, UpdatedCorrelationID: correlationID,
		CreatedAt: now, UpdatedAt: now,
	}
	persisted, created, err := s.store.CreateIdentifier(ctx, identifier)
	if normalized.ID == "" && errors.Is(err, ErrConflict) {
		existing, resolveErr := s.store.ResolveIdentifier(ctx, s.organizationID, identifier.Symbology, identifier.NormalizedValue)
		if resolveErr == nil && sameCreateIntent(existing, identifier) {
			persisted, created, err = existing, false, nil
		}
	}
	if err != nil {
		return Identifier{}, false, err
	}
	if err := s.audit(ctx, "atlas.identifier.created", persisted, nil); err != nil {
		return Identifier{}, false, fmt.Errorf("audit Atlas Codes identifier creation: %w", err)
	}
	return cloneIdentifier(persisted), created, nil
}

// ReplaceIdentifier atomically closes the current association and creates its
// successor. The store owns conflict detection and exact retry recognition.
func (s *Service) ReplaceIdentifier(ctx context.Context, input ReplaceIdentifierInput) (Identifier, bool, error) {
	input.AssetID = strings.TrimSpace(input.AssetID)
	input.IdentifierID = strings.TrimSpace(input.IdentifierID)
	input.ReplacementID = strings.TrimSpace(input.ReplacementID)
	if !stableIDPattern.MatchString(input.AssetID) || !stableIDPattern.MatchString(input.IdentifierID) ||
		(input.ReplacementID != "" && !stableIDPattern.MatchString(input.ReplacementID)) || input.Revision < 1 {
		return Identifier{}, false, ErrInvalidInput
	}
	if _, err := s.asset(ctx, input.AssetID); err != nil {
		return Identifier{}, false, err
	}
	existing, err := s.store.GetIdentifier(ctx, s.organizationID, input.AssetID, input.IdentifierID)
	if err != nil {
		return Identifier{}, false, err
	}
	if existing.OrganizationID != s.organizationID || existing.AssetID != input.AssetID {
		return Identifier{}, false, ErrNotFound
	}
	normalizedSymbology, normalizedValue, err := normalizeCode(input.ReplacementSymbology, input.ReplacementValue)
	if err != nil {
		return Identifier{}, false, err
	}
	displayValue, err := normalizeDisplay(input.DisplayValue, normalizedValue)
	if err != nil {
		return Identifier{}, false, err
	}
	source, err := normalizeSource(input.Source)
	if err != nil {
		return Identifier{}, false, err
	}
	if input.ReplacementID == "" && existing.Status == StatusReplaced && stableIDPattern.MatchString(existing.ReplacedByID) {
		input.ReplacementID = existing.ReplacedByID
	}
	replacementID, err := s.identifierID(input.ReplacementID)
	if err != nil {
		return Identifier{}, false, err
	}
	actorID, correlationID, err := mutationProvenance(ctx)
	if err != nil {
		return Identifier{}, false, err
	}
	now := s.now().UTC()
	replacement := Identifier{
		ID: replacementID, OrganizationID: s.organizationID, AssetID: input.AssetID,
		Symbology: normalizedSymbology, NormalizedValue: normalizedValue, DisplayValue: displayValue,
		Source: source, Primary: existing.Primary, Status: StatusActive, SupersedesID: existing.ID,
		Revision: 1, CreatedBy: actorID, CreatedCorrelationID: correlationID,
		UpdatedBy: actorID, UpdatedCorrelationID: correlationID,
		CreatedAt: now, UpdatedAt: now,
	}
	persisted, changed, err := s.store.ReplaceIdentifier(
		ctx, s.organizationID, input.AssetID, input.IdentifierID, input.Revision, replacement, now,
	)
	if err != nil {
		return Identifier{}, false, err
	}
	if err := s.audit(ctx, "atlas.identifier.replaced", persisted, map[string]string{
		"supersedesId": input.IdentifierID,
	}); err != nil {
		return Identifier{}, false, fmt.Errorf("audit Atlas Codes identifier replacement: %w", err)
	}
	return cloneIdentifier(persisted), changed, nil
}

// DeactivateIdentifier preserves the association as history. The store returns
// changed=false for an exact retry, and the deterministic audit ID makes the
// replay both repairable and duplicate-safe.
func (s *Service) DeactivateIdentifier(ctx context.Context, input DeactivateIdentifierInput) (Identifier, bool, error) {
	input.AssetID = strings.TrimSpace(input.AssetID)
	input.IdentifierID = strings.TrimSpace(input.IdentifierID)
	if !stableIDPattern.MatchString(input.AssetID) || !stableIDPattern.MatchString(input.IdentifierID) || input.Revision < 1 {
		return Identifier{}, false, ErrInvalidInput
	}
	if _, err := s.asset(ctx, input.AssetID); err != nil {
		return Identifier{}, false, err
	}
	actorID, correlationID, err := mutationProvenance(ctx)
	if err != nil {
		return Identifier{}, false, err
	}
	now := s.now().UTC()
	persisted, changed, err := s.store.DeactivateIdentifier(
		ctx, s.organizationID, input.AssetID, input.IdentifierID, input.Revision, now,
		actorID, correlationID,
	)
	if err != nil {
		return Identifier{}, false, err
	}
	if err := s.audit(ctx, "atlas.identifier.deactivated", persisted, nil); err != nil {
		return Identifier{}, false, fmt.Errorf("audit Atlas Codes identifier deactivation: %w", err)
	}
	return cloneIdentifier(persisted), changed, nil
}

func sameCreateIntent(existing, candidate Identifier) bool {
	return existing.OrganizationID == candidate.OrganizationID && existing.AssetID == candidate.AssetID &&
		existing.Symbology == candidate.Symbology && existing.NormalizedValue == candidate.NormalizedValue &&
		existing.DisplayValue == candidate.DisplayValue && existing.Source == candidate.Source &&
		existing.Primary == candidate.Primary && existing.Status == StatusActive &&
		existing.SupersedesID == "" && existing.ReplacedByID == "" && existing.Revision == 1 &&
		candidate.SupersedesID == "" && candidate.ReplacedByID == "" && candidate.Revision == 1
}

func normalizeCreateInput(input CreateIdentifierInput) (CreateIdentifierInput, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.AssetID = strings.TrimSpace(input.AssetID)
	if (input.ID != "" && !stableIDPattern.MatchString(input.ID)) || !stableIDPattern.MatchString(input.AssetID) {
		return CreateIdentifierInput{}, ErrInvalidInput
	}
	symbology, value, err := normalizeCode(input.Symbology, input.Value)
	if err != nil {
		return CreateIdentifierInput{}, err
	}
	display, err := normalizeDisplay(input.DisplayValue, value)
	if err != nil {
		return CreateIdentifierInput{}, err
	}
	source, err := normalizeSource(input.Source)
	if err != nil {
		return CreateIdentifierInput{}, err
	}
	input.Symbology, input.Value, input.DisplayValue, input.Source = symbology, value, display, source
	return input, nil
}

func normalizeCode(symbology Symbology, value string) (Symbology, string, error) {
	symbology = Symbology(strings.ToLower(strings.TrimSpace(string(symbology))))
	value = strings.TrimSpace(value)
	if !printable(value) {
		return "", "", ErrInvalidInput
	}
	switch symbology {
	case SymbologyCode128:
		if len(value) > maximumCode128Bytes {
			return "", "", ErrInvalidInput
		}
		for index := 0; index < len(value); index++ {
			if value[index] < 0x20 || value[index] > 0x7e {
				return "", "", ErrInvalidInput
			}
		}
	case SymbologyQR:
		if len(value) > maximumQRBytes {
			return "", "", ErrInvalidInput
		}
	default:
		return "", "", ErrInvalidInput
	}
	return symbology, value, nil
}

func normalizeDisplay(display, fallback string) (string, error) {
	display = strings.TrimSpace(display)
	if display == "" {
		display = fallback
	}
	if len(display) > maximumDisplayBytes || !printable(display) {
		return "", ErrInvalidInput
	}
	return display, nil
}

func normalizeSource(source Source) (Source, error) {
	source = Source(strings.ToLower(strings.TrimSpace(string(source))))
	if source == "" {
		source = SourceUserEntered
	}
	switch source {
	case SourceImported, SourceUserEntered, SourceGenerated:
		return source, nil
	default:
		return "", ErrInvalidInput
	}
}

func printable(value string) bool {
	if value == "" || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || !unicode.IsPrint(character) {
			return false
		}
	}
	return true
}

func (s *Service) identifierID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value != "" {
		if !stableIDPattern.MatchString(value) {
			return "", ErrInvalidInput
		}
		return value, nil
	}
	id, err := foundation.NewCorrelationID()
	if err != nil {
		return "", fmt.Errorf("create Atlas Codes identifier id: %w", err)
	}
	return id, nil
}

func (s *Service) asset(ctx context.Context, assetID string) (domain.Asset, error) {
	asset, err := s.assets.GetAsset(ctx, assetID)
	if err != nil {
		switch {
		case errors.Is(err, atlas.ErrNotFound):
			return domain.Asset{}, ErrReferenceMissing
		case errors.Is(err, atlas.ErrInvalidInput):
			return domain.Asset{}, ErrInvalidInput
		default:
			return domain.Asset{}, fmt.Errorf("read Atlas asset for identifier: %w", err)
		}
	}
	if asset.ID != assetID || asset.OrganizationID != s.organizationID {
		return domain.Asset{}, ErrReferenceMissing
	}
	return asset, nil
}

func actorFromContext(ctx context.Context) string {
	if scope, ok := foundation.ScopeFromContext(ctx); ok && strings.TrimSpace(scope.ActorID) != "" {
		return strings.TrimSpace(scope.ActorID)
	}
	return "system:atlas-codes"
}

func mutationProvenance(ctx context.Context) (string, string, error) {
	actorID := actorFromContext(ctx)
	if scope, ok := foundation.ScopeFromContext(ctx); ok && strings.TrimSpace(scope.CorrelationID) != "" {
		return actorID, strings.TrimSpace(scope.CorrelationID), nil
	}
	correlationID, err := foundation.NewCorrelationID()
	if err != nil {
		return "", "", fmt.Errorf("create Atlas Codes mutation correlation id: %w", err)
	}
	return actorID, correlationID, nil
}

func (s *Service) audit(ctx context.Context, action string, identifier Identifier, extra map[string]string) error {
	metadata := map[string]string{
		"requirementId": RequirementID,
		"identifierId":  identifier.ID,
		"assetId":       identifier.AssetID,
		"symbology":     string(identifier.Symbology),
		"source":        string(identifier.Source),
		"status":        string(identifier.Status),
		"primary":       strconv.FormatBool(identifier.Primary),
		"revision":      strconv.FormatInt(identifier.Revision, 10),
	}
	for key, value := range extra {
		metadata[key] = value
	}
	actorID := identifier.UpdatedBy
	correlationID := identifier.UpdatedCorrelationID
	if action == "atlas.identifier.created" || action == "atlas.identifier.replaced" {
		actorID = identifier.CreatedBy
		correlationID = identifier.CreatedCorrelationID
	}
	actorID = strings.TrimSpace(actorID)
	correlationID = strings.TrimSpace(correlationID)
	if actorID == "" || correlationID == "" {
		return errors.New("persisted Atlas Codes mutation audit provenance is incomplete")
	}
	occurredAt := auditOccurredAt(action, identifier)
	if occurredAt.IsZero() {
		return errors.New("persisted Atlas Codes mutation audit timestamp is incomplete")
	}
	return s.auditor.Record(ctx, foundation.AuditEvent{
		ID: auditEventID(action, identifier), OrganizationID: s.organizationID, ActorID: actorID,
		CorrelationID: correlationID, Action: action, ResourceType: "asset_identifier",
		ResourceID: identifier.ID, OccurredAt: occurredAt, Metadata: metadata,
	})
}

func auditEventID(action string, identifier Identifier) string {
	occurredAt := auditOccurredAt(action, identifier)
	digest := sha256.Sum256([]byte(strings.Join([]string{
		RequirementID,
		identifier.OrganizationID,
		action,
		identifier.ID,
		strconv.FormatInt(identifier.Revision, 10),
		occurredAt.Format(time.RFC3339Nano),
	}, "\x00")))
	return hex.EncodeToString(digest[:])
}

func auditOccurredAt(action string, identifier Identifier) time.Time {
	if action == "atlas.identifier.created" || action == "atlas.identifier.replaced" {
		return identifier.CreatedAt.UTC()
	}
	return identifier.UpdatedAt.UTC()
}

func cloneIdentifier(identifier Identifier) Identifier {
	if identifier.DeactivatedAt != nil {
		value := *identifier.DeactivatedAt
		identifier.DeactivatedAt = &value
	}
	return identifier
}
