package exchange

// Requirements: REQ-EXCHANGE-001, REQ-DIRECTORY-EXPANSION-005, REQ-PATTERNS-001. Features: migration.packages, integrations.protocols, templates.schemas.

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
	"unicode"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/directoryexpansion"
)

var directoryRecordTypes = []string{"directory.group", "directory.membership"}

type DirectoryProvider struct {
	target   *directoryexpansion.GroupTarget
	importer directoryexpansion.ExchangeImporter
}

type directoryGroupPayload struct {
	CreatedAt      string `json:"createdAt"`
	Description    string `json:"description,omitempty"`
	DisplayName    string `json:"displayName"`
	Metadata       string `json:"metadata"`
	Name           string `json:"name"`
	SourceRecordID string `json:"sourceRecordId"`
	SourceSystemID string `json:"sourceSystemId"`
	Status         string `json:"status"`
	UpdatedAt      string `json:"updatedAt"`
}

type directoryMembershipPayload struct {
	CreatedAt      string `json:"createdAt"`
	GroupID        string `json:"groupId"`
	GroupSourceID  string `json:"groupSourceId"`
	MemberDisplay  string `json:"memberDisplayName"`
	MemberID       string `json:"memberId"`
	MemberKind     string `json:"memberKind"`
	MemberSourceID string `json:"memberSourceId"`
	Metadata       string `json:"metadata"`
	SourceRecordID string `json:"sourceRecordId"`
	SourceSystemID string `json:"sourceSystemId"`
	Status         string `json:"status"`
	UpdatedAt      string `json:"updatedAt"`
}

func NewDirectoryProvider(target *directoryexpansion.GroupTarget, importer directoryexpansion.ExchangeImporter) (*DirectoryProvider, error) {
	if target == nil || importer == nil || !target.OwnsExchangeImporter(importer) {
		return nil, errors.New("Directory group target and its construction-time Exchange importer are required")
	}
	return &DirectoryProvider{target: target, importer: importer}, nil
}

func (*DirectoryProvider) Types() []string { return append([]string(nil), directoryRecordTypes...) }

func (p *DirectoryProvider) ListRecords(ctx context.Context) ([]Record, error) {
	snapshot, err := p.target.ExchangeSnapshot(ctx, MaximumRecords)
	if errors.Is(err, directoryexpansion.ErrTooLarge) {
		return nil, ErrTooLarge
	}
	if err != nil {
		return nil, err
	}
	result := make([]Record, 0, len(snapshot.Groups)+len(snapshot.Memberships))
	for _, item := range snapshot.Groups {
		if err := validatePortableInstants(2000, item.CreatedAt, item.UpdatedAt); err != nil {
			return nil, err
		}
		metadata, err := encodeDirectoryMetadata(item.Metadata)
		if err != nil {
			return nil, err
		}
		payload, err := marshalDirectoryPayload(directoryGroupPayload{SourceSystemID: item.SourceSystemID, SourceRecordID: item.SourceRecordID,
			Name: item.Name, DisplayName: item.DisplayName, Description: item.Description, Status: item.Status, Metadata: metadata,
			CreatedAt: directoryInstant(item.CreatedAt), UpdatedAt: directoryInstant(item.UpdatedAt)})
		if err != nil {
			return nil, err
		}
		result = append(result, Record{Type: "directory.group", ID: item.ID, Revision: int64(item.Revision), Dependencies: []Reference{},
			Provenance: directoryProvenance(item.SourceSystemID, item.SourceRecordID), Ownership: OwnershipMetadata{State: "local"}, Payload: payload})
	}
	for _, item := range snapshot.Memberships {
		if err := validatePortableInstants(2000, item.CreatedAt, item.UpdatedAt); err != nil {
			return nil, err
		}
		metadata, err := encodeDirectoryMetadata(item.Metadata)
		if err != nil {
			return nil, err
		}
		payload, err := marshalDirectoryPayload(directoryMembershipPayload{SourceSystemID: item.SourceSystemID, SourceRecordID: item.SourceRecordID,
			GroupID: item.GroupID, GroupSourceID: item.GroupSourceID, MemberID: item.MemberID, MemberSourceID: item.MemberSourceID,
			MemberKind: string(item.MemberKind), MemberDisplay: item.MemberDisplayName, Status: item.Status, Metadata: metadata,
			CreatedAt: directoryInstant(item.CreatedAt), UpdatedAt: directoryInstant(item.UpdatedAt)})
		if err != nil {
			return nil, err
		}
		result = append(result, Record{Type: "directory.membership", ID: item.ID, Revision: int64(item.Revision), Dependencies: directoryMembershipDependencies(item),
			Provenance: directoryProvenance(item.SourceSystemID, item.SourceRecordID), Ownership: OwnershipMetadata{State: "local"}, Payload: payload})
	}
	sort.Slice(result, func(i, j int) bool {
		return (Reference{Type: result[i].Type, ID: result[i].ID}).Key() < (Reference{Type: result[j].Type, ID: result[j].ID}).Key()
	})
	return result, nil
}

func (p *DirectoryProvider) Exists(ctx context.Context, reference Reference) (bool, error) {
	var err error
	switch reference.Type {
	case "directory.group":
		_, err = p.target.GetManagedGroup(ctx, reference.ID)
	case "directory.membership":
		_, err = p.target.GetManagedMembership(ctx, reference.ID)
	default:
		return false, nil
	}
	if errors.Is(err, directoryexpansion.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (p *DirectoryProvider) ImportRecordExists(ctx context.Context, record Record, _ []byte) (bool, error) {
	candidate, dependencies, err := decodeDirectoryRecord(record)
	if err != nil || !slices.Equal(dependencies, record.Dependencies) {
		return false, ErrInvalidInput
	}
	switch value := candidate.(type) {
	case directoryexpansion.ManagedGroup:
		current, err := p.target.GetManagedGroup(ctx, value.ID)
		if errors.Is(err, directoryexpansion.ErrNotFound) {
			return false, nil
		}
		return err == nil && sameDirectoryGroup(current, value), err
	case directoryexpansion.ManagedMembership:
		current, err := p.target.GetManagedMembership(ctx, value.ID)
		if errors.Is(err, directoryexpansion.ErrNotFound) {
			return false, nil
		}
		return err == nil && sameDirectoryMembership(current, value), err
	default:
		return false, ErrInvalidInput
	}
}

func (p *DirectoryProvider) ImportRecord(ctx context.Context, operation ProviderImportOperation, _ string, record Record, _ []byte) (ProviderImportResult, error) {
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
	candidate, dependencies, err := decodeDirectoryRecord(record)
	if err != nil || !slices.Equal(dependencies, record.Dependencies) {
		return ProviderImportResult{}, ErrInvalidInput
	}
	domainOperation := directoryexpansion.ExchangeImportOperation{Token: operation.Token, OccurredAt: operation.OccurredAt}
	var result directoryexpansion.ExchangeImportResult
	switch value := candidate.(type) {
	case directoryexpansion.ManagedGroup:
		result, err = p.importer.ImportManagedGroup(ctx, domainOperation, value)
	case directoryexpansion.ManagedMembership:
		result, err = p.importer.ImportManagedMembership(ctx, domainOperation, value)
	default:
		return ProviderImportResult{}, ErrInvalidInput
	}
	providerResult := ProviderImportResult{Committed: result.Committed, Created: result.Created}
	switch {
	case errors.Is(err, directoryexpansion.ErrInvalidInput):
		return providerResult, ErrInvalidInput
	case errors.Is(err, directoryexpansion.ErrConflict):
		return providerResult, ErrConflict
	case errors.Is(err, directoryexpansion.ErrNotFound), errors.Is(err, directoryexpansion.ErrReferenceMissing):
		return providerResult, ErrDependencyMissing
	default:
		return providerResult, err
	}
}

func decodeDirectoryRecord(record Record) (any, []Reference, error) {
	if record.Revision < 1 || !canonicalDirectoryManagedID(record.ID) || record.File != nil {
		return nil, nil, ErrInvalidInput
	}
	switch record.Type {
	case "directory.group":
		payload, err := decodeDirectoryPayload[directoryGroupPayload](record.Payload)
		createdAt, createdErr := parseDirectoryInstant(payload.CreatedAt)
		updatedAt, updatedErr := parseDirectoryInstant(payload.UpdatedAt)
		metadata, metadataErr := decodeDirectoryMetadata(payload.Metadata)
		value := directoryexpansion.ManagedGroup{ID: record.ID, SourceSystemID: payload.SourceSystemID, SourceRecordID: payload.SourceRecordID,
			Name: payload.Name, DisplayName: payload.DisplayName, Description: payload.Description, Status: payload.Status, Metadata: metadata,
			Revision: uint64(record.Revision), CreatedAt: createdAt, UpdatedAt: updatedAt}
		if err != nil || createdErr != nil || updatedErr != nil || metadataErr != nil || !stableIDPattern.MatchString(payload.SourceSystemID) ||
			!canonicalDirectorySource(payload.SourceRecordID, 255) || !canonicalDirectoryText(payload.Name, 512, true) ||
			!canonicalDirectoryText(payload.DisplayName, 200, true) || !canonicalDirectoryText(payload.Description, 2000, false) ||
			(payload.Status != "active" && payload.Status != "inactive") || updatedAt.Before(createdAt) {
			return nil, nil, ErrInvalidInput
		}
		return value, []Reference{}, nil
	case "directory.membership":
		payload, err := decodeDirectoryPayload[directoryMembershipPayload](record.Payload)
		createdAt, createdErr := parseDirectoryInstant(payload.CreatedAt)
		updatedAt, updatedErr := parseDirectoryInstant(payload.UpdatedAt)
		metadata, metadataErr := decodeDirectoryMetadata(payload.Metadata)
		value := directoryexpansion.ManagedMembership{ID: record.ID, SourceSystemID: payload.SourceSystemID, SourceRecordID: payload.SourceRecordID,
			GroupID: payload.GroupID, GroupSourceID: payload.GroupSourceID, MemberID: payload.MemberID, MemberSourceID: payload.MemberSourceID,
			MemberKind: directoryexpansion.MemberKind(payload.MemberKind), MemberDisplayName: payload.MemberDisplay, Status: payload.Status,
			Metadata: metadata, Revision: uint64(record.Revision), CreatedAt: createdAt, UpdatedAt: updatedAt}
		if err != nil || createdErr != nil || updatedErr != nil || metadataErr != nil || !stableIDPattern.MatchString(payload.SourceSystemID) ||
			!canonicalDirectorySource(payload.SourceRecordID, 255) || !canonicalDirectoryManagedID(payload.GroupID) || !canonicalDirectoryManagedID(payload.MemberID) ||
			!canonicalDirectorySource(payload.GroupSourceID, 255) || !canonicalDirectorySource(payload.MemberSourceID, 255) ||
			(value.MemberKind != directoryexpansion.MemberSubject && value.MemberKind != directoryexpansion.MemberGroup) ||
			!canonicalDirectoryText(payload.MemberDisplay, 200, true) || (payload.Status != "active" && payload.Status != "inactive") || updatedAt.Before(createdAt) {
			return nil, nil, ErrInvalidInput
		}
		return value, directoryMembershipDependencies(value), nil
	default:
		return nil, nil, ErrInvalidInput
	}
}

func canonicalDirectoryManagedID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func directoryMembershipDependencies(value directoryexpansion.ManagedMembership) []Reference {
	result := []Reference{{Type: "directory.group", ID: value.GroupID}}
	if value.MemberKind == directoryexpansion.MemberGroup {
		result = append(result, Reference{Type: "directory.group", ID: value.MemberID})
	}
	return normalizeReferences(result)
}

func directoryProvenance(sourceSystemID, sourceRecordID string) Provenance {
	if stableIDPattern.MatchString(sourceSystemID) && safeSourceRecordID(sourceRecordID) {
		return Provenance{SourceSystemID: sourceSystemID, SourceRecordID: sourceRecordID}
	}
	return Provenance{}
}

func decodeDirectoryPayload[T any](payload []byte) (T, error) {
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

func marshalDirectoryPayload(value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil || len(payload) == 0 || len(payload) > MaximumPayloadBytes || validateSafeJSON(payload) != nil {
		return nil, ErrInvalidInput
	}
	return payload, nil
}

func encodeDirectoryMetadata(values map[string]string) (string, error) {
	if values == nil {
		values = map[string]string{}
	}
	encoded, err := json.Marshal(values)
	if err != nil || len(encoded) > 20_000 || validateSafeJSON(encoded) != nil {
		return "", ErrInvalidInput
	}
	return string(encoded), nil
}

func decodeDirectoryMetadata(value string) (map[string]string, error) {
	if value == "" || len(value) > 20_000 || validateSafeJSON([]byte(value)) != nil {
		return nil, ErrInvalidInput
	}
	var result map[string]string
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil || result == nil || len(result) > 16 {
		return nil, ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidInput
	}
	canonical, err := encodeDirectoryMetadata(result)
	if err != nil || canonical != value {
		return nil, ErrInvalidInput
	}
	for key, item := range result {
		if key != strings.ToLower(strings.TrimSpace(key)) || !canonicalDirectorySource(key, 64) || !canonicalDirectoryText(item, 500, false) {
			return nil, ErrInvalidInput
		}
	}
	return result, nil
}

func canonicalDirectorySource(value string, maximum int) bool {
	if value == "" || value != strings.TrimSpace(value) || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func canonicalDirectoryText(value string, maximum int, required bool) bool {
	if value != strings.TrimSpace(value) || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maximum || required && value == "" {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) && character != '\t' {
			return false
		}
	}
	return true
}

func directoryInstant(value time.Time) string { return value.Format(time.RFC3339Nano) }

func parseDirectoryInstant(value string) (time.Time, error) {
	return parsePortableInstant(value, 2000)
}

func sameDirectoryGroup(left, right directoryexpansion.ManagedGroup) bool {
	return left.ID == right.ID && left.SourceSystemID == right.SourceSystemID && left.SourceRecordID == right.SourceRecordID &&
		left.Name == right.Name && left.DisplayName == right.DisplayName && left.Description == right.Description && left.Status == right.Status &&
		left.Revision == right.Revision && left.CreatedAt.Equal(right.CreatedAt) && left.UpdatedAt.Equal(right.UpdatedAt) && directoryMetadataEqual(left.Metadata, right.Metadata)
}

func sameDirectoryMembership(left, right directoryexpansion.ManagedMembership) bool {
	return left.ID == right.ID && left.SourceSystemID == right.SourceSystemID && left.SourceRecordID == right.SourceRecordID &&
		left.GroupID == right.GroupID && left.GroupSourceID == right.GroupSourceID && left.MemberID == right.MemberID &&
		left.MemberSourceID == right.MemberSourceID && left.MemberKind == right.MemberKind && left.MemberDisplayName == right.MemberDisplayName &&
		left.Status == right.Status && left.Revision == right.Revision && left.CreatedAt.Equal(right.CreatedAt) && left.UpdatedAt.Equal(right.UpdatedAt) &&
		directoryMetadataEqual(left.Metadata, right.Metadata)
}

func directoryMetadataEqual(left, right map[string]string) bool {
	leftEncoded, leftErr := encodeDirectoryMetadata(left)
	rightEncoded, rightErr := encodeDirectoryMetadata(right)
	return leftErr == nil && rightErr == nil && leftEncoded == rightEncoded
}
