package exchange_test

// Requirement: REQ-EXCHANGE-001. Feature: migration.packages. GitHub: #9.

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/exchange"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/stack"
	"github.com/maxlemke/stewardmesh/internal/storage"
)

func TestStackProviderRoundTripPreservesEarliestProvenance(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.August, 13, 15, 0, 0, 0, time.UTC)
	sourceStack, err := stack.NewService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "stack-source", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	product, err := sourceStack.CreateProduct(ctx, stack.CreateProductInput{
		ID: "portable-product", Name: "Portable product", Publisher: "Example",
		SourceSystemID: "original-catalog", SourceRecordID: "catalog:product:42",
	})
	if err != nil {
		t.Fatal(err)
	}
	version, err := sourceStack.CreateVersion(ctx, stack.CreateVersionInput{ID: "portable-version", ProductID: product.ID, Name: "1.0"})
	if err != nil {
		t.Fatal(err)
	}
	sourceProvider, _ := exchange.NewStackProvider(sourceStack)
	source, err := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("stack-source"), exchange.ServiceConfig{
		OrganizationID: "stack-source", SourceSystemID: "source-appliance", Now: func() time.Time { return now },
	}, sourceProvider)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := source.Export(ctx, "export-operator", exchange.ExportRequest{
		Selection: []exchange.Reference{{Type: "stack.version", ID: version.ID}}, IncludeDependencies: true, FileMode: exchange.FileModeMetadata,
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest := exchangeManifestBytes(t, artifact.Bytes)
	for _, privateField := range []string{`"organizationId"`, `"createdAt"`, `"updatedAt"`} {
		if bytes.Contains(manifest, []byte(privateField)) {
			t.Fatalf("Stack Exchange payload retained transport-local field %s: %s", privateField, manifest)
		}
	}

	targetStack, err := stack.NewService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "stack-target", Now: func() time.Time { return now.Add(time.Hour) },
	})
	if err != nil {
		t.Fatal(err)
	}
	targetProvider, _ := exchange.NewStackProvider(targetStack)
	ownership := newExchangeOwnership("stack-target")
	target, err := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
		OrganizationID: "stack-target", SourceSystemID: "target-appliance", Now: func() time.Time { return now.Add(time.Hour) },
	}, targetProvider)
	if err != nil {
		t.Fatal(err)
	}
	result, err := target.Import(ctx, "import-operator", artifact.Bytes)
	if err != nil || result.Package.Status != exchange.StatusCompleted || result.Package.CreatedCount != 2 {
		t.Fatalf("unexpected Stack provider import %#v err=%v", result, err)
	}
	snapshot, err := targetStack.Snapshot(ctx)
	if err != nil || len(snapshot.Products) != 1 || len(snapshot.Versions) != 1 {
		t.Fatalf("unexpected target Stack snapshot %#v err=%v", snapshot, err)
	}
	if snapshot.Products[0].SourceSystemID != "original-catalog" || snapshot.Products[0].SourceRecordID != "catalog:product:42" {
		t.Fatalf("earliest product provenance was lost: %#v", snapshot.Products[0])
	}
	if snapshot.Versions[0].SourceSystemID != "source-appliance" || snapshot.Versions[0].SourceRecordID != "stack.version:portable-version" {
		t.Fatalf("local source provenance was not assigned: %#v", snapshot.Versions[0])
	}
	for _, key := range []string{"stack.product:portable-product", "stack.version:portable-version"} {
		if !ownership.values[key].WriteLocked {
			t.Fatalf("imported Stack record %s was not write-locked", key)
		}
	}
	replayed, err := target.Import(ctx, "import-operator", artifact.Bytes)
	if err != nil || !replayed.Replay || replayed.Package.CreatedCount != 2 {
		t.Fatalf("Stack provider replay changed results: %#v err=%v", replayed, err)
	}
}

func TestStackProviderExposesEveryCrossDomainRelationship(t *testing.T) {
	now := time.Date(2026, time.August, 13, 15, 30, 0, 0, time.UTC)
	service, err := stack.NewService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "dependency-source", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	product, _ := service.CreateProduct(context.Background(), stack.CreateProductInput{ID: "dependency-product", Name: "Product", Publisher: "Example"})
	version, _ := service.CreateVersion(context.Background(), stack.CreateVersionInput{ID: "dependency-version", ProductID: product.ID, Name: "1.0"})
	license, err := service.CreateLicense(context.Background(), stack.CreateLicenseInput{
		ID: "dependency-license", ProductID: product.ID, VersionID: version.ID, Name: "Enterprise",
		EntitlementMetric: "enterprise", Quantity: 10, VendorID: "vendor-one", PurchaseOrderID: "po-one",
		ContractID: "contract-one", CostRecordID: "cost-one", DocumentIDs: []string{"document-one", "document-two"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assignment, err := service.CreateAssignment(context.Background(), stack.CreateAssignmentInput{
		ID: "dependency-assignment", LicenseID: license.ID, AssigneeKind: "identity", AssigneeID: "person-one", Seats: 1, AssignedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	provider, _ := exchange.NewStackProvider(service)
	records, err := provider.ListRecords(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byKey := make(map[string]exchange.Record)
	for _, record := range records {
		byKey[exchange.Reference{Type: record.Type, ID: record.ID}.Key()] = record
	}
	wantLicense := []exchange.Reference{
		{Type: "ledger.contract", ID: "contract-one"}, {Type: "ledger.cost", ID: "cost-one"},
		{Type: "ledger.purchase_order", ID: "po-one"}, {Type: "ledger.vendor", ID: "vendor-one"},
		{Type: "stack.product", ID: product.ID}, {Type: "stack.version", ID: version.ID},
		{Type: "vault.blob", ID: "document-one"}, {Type: "vault.blob", ID: "document-two"},
	}
	if !slices.Equal(byKey["stack.license:"+license.ID].Dependencies, wantLicense) {
		t.Fatalf("license relationships were incomplete: %#v", byKey["stack.license:"+license.ID].Dependencies)
	}
	wantAssignment := []exchange.Reference{{Type: "people.identity", ID: "person-one"}, {Type: "stack.license", ID: license.ID}}
	if !slices.Equal(byKey["stack.assignment:"+assignment.ID].Dependencies, wantAssignment) {
		t.Fatalf("assignment relationship was not canonical: %#v", byKey["stack.assignment:"+assignment.ID].Dependencies)
	}
}

func TestVaultProviderRoundTripIncludesVerifiedBytesAndExcludesPrivateTransportData(t *testing.T) {
	ctx := foundation.WithScope(context.Background(), foundation.Scope{
		OrganizationID: "vault-source", ActorID: "private-source-operator", CorrelationID: "exchange-vault-source",
	})
	now := time.Date(2026, time.August, 13, 16, 0, 0, 0, time.UTC)
	content := []byte("portable evidence with verified bytes")
	sourceObjects := newExchangeObjectStore("s3", 1<<20)
	sourceVault, err := storage.NewService(repository.NewMemoryStorageStore(), sourceObjects, foundation.NopAuditor{}, storage.ServiceConfig{
		OrganizationID: "vault-source", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	blob, err := sourceVault.CreateBlob(ctx, storage.CreateBlobInput{
		Name: "evidence.txt", MediaType: "text/plain", Content: bytes.NewReader(content),
		SourceSystemID: "original-vault", SourceRecordID: "evidence:42",
	})
	if err != nil {
		t.Fatal(err)
	}
	sourceProvider, _ := exchange.NewVaultProvider(sourceVault)
	source, err := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("vault-source"), exchange.ServiceConfig{
		OrganizationID: "vault-source", SourceSystemID: "source-appliance", Now: func() time.Time { return now },
	}, sourceProvider)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := source.Export(ctx, "export-operator", exchange.ExportRequest{
		Selection: []exchange.Reference{{Type: "vault.blob", ID: blob.ID}}, FileMode: exchange.FileModeInclude,
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest := exchangeManifestBytes(t, artifact.Bytes)
	for _, forbidden := range []string{"private-source-operator", "vault-source", "objectKey", "signed.example.test", "X-Amz-Signature", "credential"} {
		if bytes.Contains(manifest, []byte(forbidden)) {
			t.Fatalf("Exchange manifest leaked %q: %s", forbidden, manifest)
		}
	}
	if !bytes.Contains(manifest, []byte(`"provider":"s3"`)) || !bytes.Contains(manifest, []byte(`"entry":"files/`)) {
		t.Fatalf("S3 metadata and included file entry were not explicit: %s", manifest)
	}

	targetObjects := newExchangeObjectStore("local", 1<<20)
	targetVault, err := storage.NewService(repository.NewMemoryStorageStore(), targetObjects, foundation.NopAuditor{}, storage.ServiceConfig{
		OrganizationID: "vault-target", Now: func() time.Time { return now.Add(time.Hour) },
	})
	if err != nil {
		t.Fatal(err)
	}
	targetProvider, _ := exchange.NewVaultProvider(targetVault)
	target, err := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("vault-target"), exchange.ServiceConfig{
		OrganizationID: "vault-target", SourceSystemID: "target-appliance", Now: func() time.Time { return now.Add(time.Hour) },
	}, targetProvider)
	if err != nil {
		t.Fatal(err)
	}
	result, err := target.Import(context.Background(), "import-operator", artifact.Bytes)
	if err != nil || result.Package.Status != exchange.StatusCompleted || result.Package.CreatedCount != 1 || result.Package.FileCount != 1 {
		t.Fatalf("unexpected Vault import %#v err=%v", result, err)
	}
	imported, reader, err := targetVault.OpenBlob(context.Background(), blob.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	actual, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(actual, content) || imported.SHA256 != blob.SHA256 ||
		imported.SourceSystemID != "original-vault" || imported.SourceRecordID != "evidence:42" {
		t.Fatalf("Vault bytes or provenance did not round trip: %#v bytes=%q err=%v", imported, actual, err)
	}
}

func TestVaultMetadataOnlyImportRemainsReadableInHoldingWithoutWritingBytes(t *testing.T) {
	now := time.Date(2026, time.August, 13, 17, 0, 0, 0, time.UTC)
	sourceVault, _ := storage.NewService(repository.NewMemoryStorageStore(), newExchangeObjectStore("s3", 1<<20), foundation.NopAuditor{}, storage.ServiceConfig{
		OrganizationID: "metadata-source", Now: func() time.Time { return now },
	})
	blob, err := sourceVault.CreateBlob(context.Background(), storage.CreateBlobInput{
		Name: "metadata.txt", MediaType: "text/plain", Content: strings.NewReader("metadata only"),
	})
	if err != nil {
		t.Fatal(err)
	}
	sourceProvider, _ := exchange.NewVaultProvider(sourceVault)
	source, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("metadata-source"), exchange.ServiceConfig{
		OrganizationID: "metadata-source", SourceSystemID: "metadata-system", Now: func() time.Time { return now },
	}, sourceProvider)
	artifact, err := source.Export(context.Background(), "export-operator", exchange.ExportRequest{
		Selection: []exchange.Reference{{Type: "vault.blob", ID: blob.ID}}, FileMode: exchange.FileModeMetadata,
	})
	if err != nil {
		t.Fatal(err)
	}
	targetVault, _ := storage.NewService(repository.NewMemoryStorageStore(), newExchangeObjectStore("local", 1<<20), foundation.NopAuditor{}, storage.ServiceConfig{
		OrganizationID: "metadata-target", Now: func() time.Time { return now },
	})
	targetProvider, _ := exchange.NewVaultProvider(targetVault)
	ownership := newExchangeOwnership("metadata-target")
	target, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
		OrganizationID: "metadata-target", SourceSystemID: "target-system", Now: func() time.Time { return now },
	}, targetProvider)
	result, err := target.Import(context.Background(), "import-operator", artifact.Bytes)
	if err != nil || result.Package.Status != exchange.StatusHolding || result.Package.HoldingCount != 1 ||
		len(result.Package.Records[0].MissingDependencies) != 1 || result.Package.Records[0].MissingDependencies[0].Type != "exchange.file" {
		t.Fatalf("metadata-only Vault record was not visible in holding: %#v err=%v", result, err)
	}
	if _, err := targetVault.GetBlob(context.Background(), blob.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("metadata-only import wrote a Vault record without bytes: %v", err)
	}
	if len(ownership.values) != 0 {
		t.Fatalf("holding record acquired an ownership lock: %#v", ownership.values)
	}
}

type allowExchangeStackReferences struct{}

func (allowExchangeStackReferences) ResolveAsset(_ context.Context, id string) (stack.AssetContext, error) {
	return stack.AssetContext{ID: id}, nil
}
func (allowExchangeStackReferences) ValidateAssignee(context.Context, string, string) error {
	return nil
}
func (allowExchangeStackReferences) ValidateFinancialReferences(context.Context, string, string, string, string) error {
	return nil
}
func (allowExchangeStackReferences) ValidateDocuments(context.Context, []string) error { return nil }

type exchangeObjectStore struct {
	provider string
	maximum  int64
	objects  map[string][]byte
}

func newExchangeObjectStore(provider string, maximum int64) *exchangeObjectStore {
	return &exchangeObjectStore{provider: provider, maximum: maximum, objects: make(map[string][]byte)}
}

func (s *exchangeObjectStore) Provider() string    { return s.provider }
func (s *exchangeObjectStore) MaximumBytes() int64 { return s.maximum }
func (s *exchangeObjectStore) Put(_ context.Context, key, _ string, content io.Reader) (storage.StoredObject, error) {
	if _, exists := s.objects[key]; exists {
		return storage.StoredObject{}, storage.ErrConflict
	}
	value, err := io.ReadAll(io.LimitReader(content, s.maximum+1))
	if err != nil {
		return storage.StoredObject{}, err
	}
	if int64(len(value)) > s.maximum {
		return storage.StoredObject{}, storage.ErrTooLarge
	}
	digest := sha256.Sum256(value)
	s.objects[key] = append([]byte(nil), value...)
	return storage.StoredObject{SizeBytes: int64(len(value)), SHA256: hex.EncodeToString(digest[:])}, nil
}
func (s *exchangeObjectStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	value, exists := s.objects[key]
	if !exists {
		return nil, storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(value)), nil
}
func (s *exchangeObjectStore) Delete(_ context.Context, key string) error {
	delete(s.objects, key)
	return nil
}
func (*exchangeObjectStore) AuthorizeDownload(context.Context, string, string, time.Duration) (storage.ObjectDownloadAuthorization, error) {
	return storage.ObjectDownloadAuthorization{URL: "https://signed.example.test/file?X-Amz-Signature=must-not-export"}, nil
}
func (*exchangeObjectStore) ValidateDownload(context.Context, string, string) error { return nil }

func exchangeManifestBytes(t *testing.T, contents []byte) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(contents), int64(len(contents)))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range reader.File {
		if entry.Name != "manifest.json" {
			continue
		}
		opened, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer opened.Close()
		value, err := io.ReadAll(opened)
		if err != nil || !json.Valid(value) {
			t.Fatalf("invalid Exchange manifest: %v", err)
		}
		return value
	}
	t.Fatal("manifest.json was not found")
	return nil
}
