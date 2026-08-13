package exchange

// Requirement: REQ-EXCHANGE-001. Feature: migration.packages. GitHub: #9.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/maxlemke/stewardmesh/internal/stack"
	"github.com/maxlemke/stewardmesh/internal/storage"
)

var stackRecordTypes = []string{
	"stack.product", "stack.version", "stack.license", "stack.installation", "stack.assignment",
}

type StackProvider struct{ service *stack.Service }

func NewStackProvider(service *stack.Service) (*StackProvider, error) {
	if service == nil {
		return nil, errors.New("Stack service is required for Exchange")
	}
	return &StackProvider{service: service}, nil
}

func (*StackProvider) Types() []string { return append([]string(nil), stackRecordTypes...) }

func (p *StackProvider) ListRecords(ctx context.Context) ([]Record, error) {
	portable, err := p.service.ExportRecords(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]Record, 0, len(portable))
	for _, item := range portable {
		payload, err := sanitizeStackPayload(item.Payload)
		if err != nil {
			return nil, err
		}
		dependencies := make([]Reference, 0, len(item.Dependencies))
		for _, dependency := range item.Dependencies {
			reference, err := parseStackReference(dependency)
			if err != nil {
				return nil, err
			}
			dependencies = append(dependencies, reference)
		}
		sort.Slice(dependencies, func(i, j int) bool { return dependencies[i].Key() < dependencies[j].Key() })
		var source struct {
			SourceSystemID string `json:"sourceSystemId"`
			SourceRecordID string `json:"sourceRecordId"`
		}
		if err := json.Unmarshal(payload, &source); err != nil {
			return nil, stack.ErrInvalidInput
		}
		result = append(result, Record{
			Type: item.Type, ID: item.ID, Revision: item.Revision, Dependencies: dependencies,
			Provenance: Provenance{SourceSystemID: source.SourceSystemID, SourceRecordID: source.SourceRecordID},
			Ownership:  OwnershipMetadata{State: "local"}, Payload: payload,
		})
	}
	return result, nil
}

func (p *StackProvider) Exists(ctx context.Context, reference Reference) (bool, error) {
	snapshot, err := p.service.Snapshot(ctx)
	if err != nil {
		return false, err
	}
	switch reference.Type {
	case "stack.product":
		return containsStackID(snapshot.Products, reference.ID, func(value stack.Product) string { return value.ID }), nil
	case "stack.version":
		return containsStackID(snapshot.Versions, reference.ID, func(value stack.Version) string { return value.ID }), nil
	case "stack.license":
		return containsStackID(snapshot.Licenses, reference.ID, func(value stack.License) string { return value.ID }), nil
	case "stack.installation":
		return containsStackID(snapshot.Installations, reference.ID, func(value stack.Installation) string { return value.ID }), nil
	case "stack.assignment":
		return containsStackID(snapshot.Assignments, reference.ID, func(value stack.Assignment) string { return value.ID }), nil
	default:
		return false, nil
	}
}

func (p *StackProvider) DependencyExists(ctx context.Context, reference Reference) (bool, bool, error) {
	return p.service.ExchangeDependencyExists(ctx, reference.Type, reference.ID)
}

func (p *StackProvider) ImportRecord(ctx context.Context, sourceSystemID string, record Record, _ []byte) (bool, error) {
	current, err := p.service.ExportRecords(ctx)
	if err != nil {
		return false, err
	}
	dependencies := make([]string, 0, len(record.Dependencies))
	for _, dependency := range record.Dependencies {
		dependencies = append(dependencies, dependency.Key())
	}
	for _, existing := range current {
		payload, sanitizeErr := sanitizeStackPayload(existing.Payload)
		if sanitizeErr != nil {
			return false, sanitizeErr
		}
		if existing.Type == record.Type && existing.ID == record.ID && existing.Revision == record.Revision &&
			slices.Equal(existing.Dependencies, dependencies) && bytes.Equal(payload, record.Payload) {
			return false, nil
		}
	}
	result, err := p.service.ImportRecords(ctx, sourceSystemID, []stack.ExchangeRecord{{
		Type: record.Type, ID: record.ID, Revision: record.Revision,
		Dependencies: dependencies, SourceSystemID: record.Provenance.SourceSystemID,
		SourceRecordID: record.Provenance.SourceRecordID, Payload: append([]byte(nil), record.Payload...),
	}})
	if errors.Is(err, stack.ErrReferenceMissing) || errors.Is(err, stack.ErrNotFound) {
		return false, ErrDependencyMissing
	}
	if errors.Is(err, stack.ErrInvalidInput) {
		return false, ErrInvalidInput
	}
	if errors.Is(err, stack.ErrConflict) {
		return false, ErrConflict
	}
	return result.Created == 1, err
}

func sanitizeStackPayload(payload []byte) ([]byte, error) {
	var value map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(&value); err != nil {
		return nil, stack.ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) || value == nil {
		return nil, stack.ErrInvalidInput
	}
	for _, field := range []string{"organizationId", "createdAt", "updatedAt"} {
		delete(value, field)
	}
	result, err := json.Marshal(value)
	if err != nil || len(result) > MaximumPayloadBytes {
		return nil, stack.ErrInvalidInput
	}
	return result, nil
}

func parseStackReference(value string) (Reference, error) {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 || !resourceTypePattern.MatchString(parts[0]) || !stableIDPattern.MatchString(parts[1]) {
		return Reference{}, ErrInvalidInput
	}
	return Reference{Type: parts[0], ID: parts[1]}, nil
}

func containsStackID[T any](values []T, id string, identifier func(T) string) bool {
	for _, value := range values {
		if identifier(value) == id {
			return true
		}
	}
	return false
}

type VaultProvider struct{ service *storage.Service }

// vaultPortablePayload deliberately excludes organization and operator
// identifiers as well as the server-owned object key. Provider is descriptive
// metadata only; it is never used to address an object during import.
type vaultPortablePayload struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	MediaType    string `json:"mediaType"`
	SizeBytes    int64  `json:"sizeBytes"`
	SHA256       string `json:"sha256"`
	Provider     string `json:"provider"`
	ResourceType string `json:"resourceType,omitempty"`
	ResourceID   string `json:"resourceId,omitempty"`
}

func NewVaultProvider(service *storage.Service) (*VaultProvider, error) {
	if service == nil {
		return nil, errors.New("Vault service is required for Exchange")
	}
	return &VaultProvider{service: service}, nil
}

func (*VaultProvider) Types() []string { return []string{"vault.blob"} }

func (p *VaultProvider) ListRecords(ctx context.Context) ([]Record, error) {
	items, err := p.service.ListBlobs(ctx)
	if err != nil {
		return nil, err
	}
	if len(items) > MaximumRecords {
		return nil, ErrTooLarge
	}
	result := make([]Record, 0, len(items))
	for _, item := range items {
		payload, err := json.Marshal(vaultPortablePayload{
			ID: item.ID, Name: item.Name, MediaType: item.MediaType, SizeBytes: item.SizeBytes,
			SHA256: item.SHA256, Provider: item.Provider, ResourceType: item.ResourceType,
			ResourceID: item.ResourceID,
		})
		if err != nil {
			return nil, err
		}
		dependencies := []Reference{}
		if item.ResourceType != "" && item.ResourceID != "" {
			dependencies = append(dependencies, canonicalResourceReference(item.ResourceType, item.ResourceID))
		}
		result = append(result, Record{
			Type: "vault.blob", ID: item.ID, Revision: 1, Dependencies: dependencies,
			Provenance: Provenance{SourceSystemID: item.SourceSystemID, SourceRecordID: item.SourceRecordID},
			Ownership:  OwnershipMetadata{State: "local"},
			File:       &FileMetadata{Mode: FileModeMetadata, Name: item.Name, MediaType: item.MediaType, SizeBytes: item.SizeBytes, SHA256: item.SHA256},
			Payload:    payload,
		})
	}
	return result, nil
}

func (p *VaultProvider) Exists(ctx context.Context, reference Reference) (bool, error) {
	if reference.Type != "vault.blob" {
		return false, nil
	}
	_, err := p.service.GetBlob(ctx, reference.ID)
	if errors.Is(err, storage.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (p *VaultProvider) ReadRecordFile(ctx context.Context, record Record) ([]byte, error) {
	if record.Type != "vault.blob" || record.File == nil {
		return nil, ErrInvalidInput
	}
	blob, content, err := p.service.OpenBlob(ctx, record.ID)
	if err != nil {
		return nil, err
	}
	defer content.Close()
	value, err := readBounded(content, MaximumFileBytes)
	if err != nil {
		return nil, err
	}
	if blob.SizeBytes != int64(len(value)) || blob.SHA256 != record.File.SHA256 {
		return nil, ErrIntegrity
	}
	return value, nil
}

func (p *VaultProvider) ImportRecord(ctx context.Context, _ string, record Record, file []byte) (bool, error) {
	if record.Type != "vault.blob" || record.Revision != 1 || record.File == nil || len(file) > int(MaximumFileBytes) {
		return false, ErrInvalidInput
	}
	var value vaultPortablePayload
	decoder := json.NewDecoder(bytes.NewReader(record.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return false, ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) || value.ID != record.ID ||
		value.Name != record.File.Name || value.MediaType != record.File.MediaType || value.SizeBytes != record.File.SizeBytes || value.SHA256 != record.File.SHA256 {
		return false, ErrInvalidInput
	}
	expectedDependencies := []Reference{}
	if value.ResourceType != "" || value.ResourceID != "" {
		if value.ResourceType == "" || value.ResourceID == "" {
			return false, ErrInvalidInput
		}
		expectedDependencies = append(expectedDependencies, canonicalResourceReference(value.ResourceType, value.ResourceID))
	}
	if !slices.Equal(record.Dependencies, expectedDependencies) {
		return false, ErrInvalidInput
	}
	_, created, err := p.service.ImportBlob(ctx, storage.ImportBlobInput{
		ID: record.ID, Name: record.File.Name, MediaType: record.File.MediaType,
		SizeBytes: record.File.SizeBytes, SHA256: record.File.SHA256,
		SourceSystemID: record.Provenance.SourceSystemID, SourceRecordID: record.Provenance.SourceRecordID,
		ResourceType: value.ResourceType, ResourceID: value.ResourceID, Content: bytes.NewReader(file),
	})
	if errors.Is(err, storage.ErrConflict) {
		return false, ErrConflict
	}
	if errors.Is(err, storage.ErrIntegrity) {
		return false, ErrIntegrity
	}
	if errors.Is(err, storage.ErrInvalidInput) {
		return false, ErrInvalidInput
	}
	if err != nil {
		return false, fmt.Errorf("import Vault file: %w", err)
	}
	return created, nil
}

func canonicalResourceReference(recordType, id string) Reference {
	original := strings.TrimSpace(recordType)
	canonical := map[string]string{
		"asset": "atlas.asset", "identity": "people.identity", "department": "people.department", "site": "people.site",
	}[original]
	if canonical == "" {
		canonical = original
	}
	return Reference{Type: canonical, ID: id}
}
