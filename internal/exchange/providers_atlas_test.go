package exchange_test

// Requirements: REQ-ATLAS-001, REQ-ATLAS-MODELS-001, REQ-ATLAS-CODES-001, REQ-EXCHANGE-001.
// Features: inventory.assets, inventory.models, inventory.identifiers, migration.packages. GitHub: #9.

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/atlascodes"
	"github.com/maxlemke/stewardmesh/internal/exchange"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

type allowAtlasReferences struct{}

func (allowAtlasReferences) ValidateAssetReferences(context.Context, string, atlas.References) error {
	return nil
}

func TestAtlasProvidersRoundTripLosslessStateAndIdentifierHistory(t *testing.T) {
	ctx := foundation.WithScope(context.Background(), foundation.Scope{
		OrganizationID: "source-org", ActorID: "source-admin", CorrelationID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	now := time.Date(2026, time.August, 13, 14, 30, 0, 123_456_789, time.UTC)
	sourceAtlas, sourceAtlasImporter, err := atlas.NewServiceWithExchangeImporter(
		repository.NewMemoryAtlasStore(), allowAtlasReferences{}, nil, foundation.NopAuditor{},
		atlas.ServiceConfig{OrganizationID: "source-org", Now: func() time.Time { return now }},
	)
	if err != nil {
		t.Fatal(err)
	}
	model, err := sourceAtlas.CreateModel(ctx, atlas.CreateModelInput{
		ID: "model-one", Manufacturer: "Acme", Name: "Edge 9000", ModelNumber: "E-9000", Kind: "server",
		VendorIdentifier: "ACME:E9000", Specifications: map[string]string{"cpu": "16 core", "memory": "128 GB"},
		SupportURL: "https://support.example.test/models/e-9000", WarrantyMonths: 48, UsefulLifeMonths: 72,
		SourceSystemID: "legacy-cmdb", SourceRecordID: "models/e-9000",
	})
	if err != nil {
		t.Fatal(err)
	}
	purchaseDate := time.Date(2025, time.December, 1, 0, 0, 0, 0, time.UTC)
	asset, err := sourceAtlas.CreateAsset(ctx, atlas.CreateAssetInput{
		ID: "asset-one", ModelID: model.ID, Name: "Edge node one", Kind: "computer", AssetTag: "AT-100",
		SerialNumber: "SER-100", Hostname: "edge-one.example.test", DeploymentNotes: "Primary edge cluster",
		References: atlas.References{
			SiteID: "11111111111111111111111111111111", BuildingID: "22222222222222222222222222222222",
			RoomID: "33333333333333333333333333333333", DepartmentID: "44444444444444444444444444444444",
			UserID: "55555555555555555555555555555555",
		},
		Status: "draft", PurchaseDate: &purchaseDate,
	})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	asset, err = sourceAtlas.UpdateAsset(ctx, atlas.UpdateAssetInput{
		ID: asset.ID, ModelID: asset.ModelID, Name: asset.Name, Kind: asset.Kind, AssetTag: asset.AssetTag,
		SerialNumber: asset.SerialNumber, Hostname: asset.Hostname, DeploymentNotes: asset.DeploymentNotes,
		References: atlas.References{SiteID: asset.SiteID, BuildingID: asset.BuildingID, RoomID: asset.RoomID,
			DepartmentID: asset.DepartmentID, UserID: asset.UserID},
		Status: "active", PurchaseDate: asset.PurchaseDate, Revision: asset.Revision, LifecycleNote: "Deployment approved",
	})
	if err != nil {
		t.Fatal(err)
	}
	sourceCodes, sourceCodesImporter, err := atlascodes.NewServiceWithExchangeImporter(
		repository.NewMemoryAtlasCodesStore(), sourceAtlas, nil, foundation.NopAuditor{},
		atlascodes.ServiceConfig{OrganizationID: "source-org", Now: func() time.Time { return now }},
	)
	if err != nil {
		t.Fatal(err)
	}
	identifier, created, err := sourceCodes.CreateIdentifier(ctx, atlascodes.CreateIdentifierInput{
		ID: "identifier-one", AssetID: asset.ID, Symbology: atlascodes.SymbologyCode128,
		Value: "ACME-Edge-100", DisplayValue: "ACME Edge 100", Source: atlascodes.SourceImported, Primary: true,
	})
	if err != nil || !created {
		t.Fatalf("create source identifier: %#v created=%t err=%v", identifier, created, err)
	}
	now = now.Add(time.Hour)
	ctx = foundation.WithScope(ctx, foundation.Scope{OrganizationID: "source-org", ActorID: "source-admin", CorrelationID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"})
	replacement, changed, err := sourceCodes.ReplaceIdentifier(ctx, atlascodes.ReplaceIdentifierInput{
		AssetID: asset.ID, IdentifierID: identifier.ID, Revision: identifier.Revision, ReplacementID: "identifier-two",
		ReplacementSymbology: atlascodes.SymbologyQR, ReplacementValue: "asset-one:qr:v2",
		DisplayValue: "Asset one QR", Source: atlascodes.SourceGenerated,
	})
	if err != nil || !changed {
		t.Fatalf("replace source identifier: %#v changed=%t err=%v", replacement, changed, err)
	}
	now = now.Add(time.Hour)
	ctx = foundation.WithScope(ctx, foundation.Scope{OrganizationID: "source-org", ActorID: "source-admin", CorrelationID: "cccccccccccccccccccccccccccccccc"})
	if _, changed, err = sourceCodes.DeactivateIdentifier(ctx, atlascodes.DeactivateIdentifierInput{
		AssetID: asset.ID, IdentifierID: replacement.ID, Revision: replacement.Revision,
	}); err != nil || !changed {
		t.Fatalf("deactivate source identifier: changed=%t err=%v", changed, err)
	}

	sourceAtlasProvider, err := exchange.NewAtlasProvider(sourceAtlas, sourceAtlasImporter)
	if err != nil {
		t.Fatal(err)
	}
	sourceCodesProvider, err := exchange.NewAtlasCodesProvider(sourceCodes, sourceCodesImporter)
	if err != nil {
		t.Fatal(err)
	}
	atlasRecords, err := sourceAtlasProvider.ListRecords(ctx)
	if err != nil {
		t.Fatal(err)
	}
	codeRecords, err := sourceCodesProvider.ListRecords(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(atlasRecords) != 4 || len(codeRecords) != 1 {
		t.Fatalf("unexpected Atlas export counts: atlas=%d codes=%d", len(atlasRecords), len(codeRecords))
	}

	targetAtlas, targetAtlasImporter, err := atlas.NewServiceWithExchangeImporter(
		repository.NewMemoryAtlasStore(), allowAtlasReferences{}, nil, foundation.NopAuditor{},
		atlas.ServiceConfig{OrganizationID: "target-org", Now: func() time.Time { return now.Add(time.Hour) }},
	)
	if err != nil {
		t.Fatal(err)
	}
	targetCodes, targetCodesImporter, err := atlascodes.NewServiceWithExchangeImporter(
		repository.NewMemoryAtlasCodesStore(), targetAtlas, nil, foundation.NopAuditor{},
		atlascodes.ServiceConfig{OrganizationID: "target-org", Now: func() time.Time { return now.Add(time.Hour) }},
	)
	if err != nil {
		t.Fatal(err)
	}
	targetAtlasProvider, err := exchange.NewAtlasProvider(targetAtlas, targetAtlasImporter)
	if err != nil {
		t.Fatal(err)
	}
	targetCodesProvider, err := exchange.NewAtlasCodesProvider(targetCodes, targetCodesImporter)
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(atlasRecords, func(i, j int) bool {
		order := map[string]int{"atlas.model": 0, "atlas.asset": 1, "atlas.lifecycle-event": 2}
		if order[atlasRecords[i].Type] != order[atlasRecords[j].Type] {
			return order[atlasRecords[i].Type] < order[atlasRecords[j].Type]
		}
		return atlasRecords[i].Revision < atlasRecords[j].Revision
	})
	for index, record := range atlasRecords {
		result, err := targetAtlasProvider.ImportRecord(ctx, exchange.ProviderImportOperation{
			Token: "atlas-provider-import-" + record.ID, OccurredAt: now.Add(time.Duration(index) * time.Minute), ExpectedCreated: true,
		}, "source-system", record, nil)
		if err != nil || !result.Committed || !result.Created {
			t.Fatalf("import Atlas record %s:%s: %#v err=%v", record.Type, record.ID, result, err)
		}
	}
	result, err := targetCodesProvider.ImportRecord(ctx, exchange.ProviderImportOperation{
		Token: "atlas-codes-provider-import", OccurredAt: now.Add(10 * time.Minute), ExpectedCreated: true,
	}, "source-system", codeRecords[0], nil)
	if err != nil || !result.Committed || !result.Created {
		t.Fatalf("import Atlas Codes record: %#v err=%v", result, err)
	}
	if exact, err := targetCodesProvider.ImportRecordExists(ctx, codeRecords[0], nil); err != nil || !exact {
		t.Fatalf("Atlas Codes exact readback failed: exact=%t err=%v", exact, err)
	}
	if replay, err := targetCodesProvider.ImportRecord(ctx, exchange.ProviderImportOperation{ExpectedCreated: false}, "source-system", codeRecords[0], nil); err != nil || !replay.Committed || replay.Created {
		t.Fatalf("Atlas Codes replay failed: %#v err=%v", replay, err)
	}

	targetAtlasRecords, err := targetAtlasProvider.ListRecords(ctx)
	if err != nil {
		t.Fatal(err)
	}
	targetCodeRecords, err := targetCodesProvider.ListRecords(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(atlasRecords, func(i, j int) bool {
		return atlasRecords[i].Type+atlasRecords[i].ID < atlasRecords[j].Type+atlasRecords[j].ID
	})
	if !reflect.DeepEqual(atlasRecords, targetAtlasRecords) || !reflect.DeepEqual(codeRecords, targetCodeRecords) {
		t.Fatalf("lossless re-export mismatch\nsource Atlas=%#v\ntarget Atlas=%#v\nsource Codes=%#v\ntarget Codes=%#v", atlasRecords, targetAtlasRecords, codeRecords, targetCodeRecords)
	}
}

func TestAtlasProvidersRejectForeignCapabilitiesAndNonCanonicalPayloads(t *testing.T) {
	first, firstImporter, err := atlas.NewServiceWithExchangeImporter(repository.NewMemoryAtlasStore(), allowAtlasReferences{}, nil,
		foundation.NopAuditor{}, atlas.ServiceConfig{OrganizationID: "first-org"})
	if err != nil {
		t.Fatal(err)
	}
	_, secondImporter, err := atlas.NewServiceWithExchangeImporter(repository.NewMemoryAtlasStore(), allowAtlasReferences{}, nil,
		foundation.NopAuditor{}, atlas.ServiceConfig{OrganizationID: "second-org"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exchange.NewAtlasProvider(first, secondImporter); err == nil {
		t.Fatal("Atlas provider accepted an importer issued by another service")
	}
	provider, err := exchange.NewAtlasProvider(first, firstImporter)
	if err != nil {
		t.Fatal(err)
	}
	valid := exchange.Record{Type: "atlas.model", ID: "model-one", Revision: 1, Dependencies: []exchange.Reference{},
		Payload: []byte(`{"manufacturer":"Acme","name":"Model","kind":"server","specifications":"{}","warrantyMonths":0,"usefulLifeMonths":0,"status":"active","createdAt":"2026-08-13T00:00:00Z","updatedAt":"2026-08-13T00:00:00Z"}`)}
	for name, mutate := range map[string]func(exchange.Record) exchange.Record{
		"noncanonical field": func(record exchange.Record) exchange.Record {
			record.Payload = []byte(`{"manufacturer":" Acme","name":"Model","kind":"server","specifications":"{}","warrantyMonths":0,"usefulLifeMonths":0,"status":"active","createdAt":"2026-08-13T00:00:00Z","updatedAt":"2026-08-13T00:00:00Z"}`)
			return record
		},
		"noncanonical JSON bytes": func(record exchange.Record) exchange.Record {
			record.Payload = append([]byte(" "), record.Payload...)
			return record
		},
		"sub-microsecond instant": func(record exchange.Record) exchange.Record {
			record.Payload = bytes.Replace(record.Payload, []byte(`"createdAt":"2026-08-13T00:00:00Z"`), []byte(`"createdAt":"2026-08-13T00:00:00.000000001Z"`), 1)
			return record
		},
		"invalid id":    func(record exchange.Record) exchange.Record { record.ID = "invalid id"; return record },
		"zero revision": func(record exchange.Record) exchange.Record { record.Revision = 0; return record },
		"dependency drift": func(record exchange.Record) exchange.Record {
			record.Dependencies = []exchange.Reference{{Type: "atlas.model", ID: "unexpected"}}
			return record
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := provider.ImportRecordExists(context.Background(), mutate(valid), nil); !errors.Is(err, exchange.ErrInvalidInput) {
				t.Fatalf("non-canonical Atlas record was accepted: %v", err)
			}
		})
	}

	codes, codesImporter, err := atlascodes.NewServiceWithExchangeImporter(repository.NewMemoryAtlasCodesStore(), first, nil,
		foundation.NopAuditor{}, atlascodes.ServiceConfig{OrganizationID: "first-org"})
	if err != nil {
		t.Fatal(err)
	}
	codesProvider, err := exchange.NewAtlasCodesProvider(codes, codesImporter)
	if err != nil {
		t.Fatal(err)
	}
	codeRecord := exchange.Record{Type: "atlas.identifier", ID: "identifier-one", Revision: 1,
		Dependencies: []exchange.Reference{{Type: "atlas.asset", ID: "asset-one"}},
		Payload:      []byte(`{"assetId":"asset-one","history01":"[{\"id\":\"identifier-one\",\"symbology\":\"code128\",\"normalizedValue\":\"CODE-ONE\",\"displayValue\":\"Code one\",\"source\":\"imported\",\"primary\":true,\"status\":\"active\",\"revision\":1,\"createdBy\":\"source-operator\",\"createdCorrelationId\":\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"updatedBy\":\"source-operator\",\"updatedCorrelationId\":\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"createdAt\":\"2026-08-13T00:00:00Z\",\"updatedAt\":\"2026-08-13T00:00:00Z\"}]"}`)}
	if exact, err := codesProvider.ImportRecordExists(context.Background(), codeRecord, nil); err != nil || exact {
		t.Fatalf("expected valid absent Atlas Codes record, exact=%t err=%v", exact, err)
	}
	for name, mutate := range map[string]func(exchange.Record) exchange.Record{
		"noncanonical JSON bytes": func(record exchange.Record) exchange.Record {
			record.Payload = append([]byte(" "), record.Payload...)
			return record
		},
		"sub-microsecond history instant": func(record exchange.Record) exchange.Record {
			record.Payload = bytes.Replace(record.Payload, []byte(`\"createdAt\":\"2026-08-13T00:00:00Z\"`), []byte(`\"createdAt\":\"2026-08-13T00:00:00.000000001Z\"`), 1)
			return record
		},
		"invalid id":    func(record exchange.Record) exchange.Record { record.ID = "invalid id"; return record },
		"zero revision": func(record exchange.Record) exchange.Record { record.Revision = 0; return record },
	} {
		t.Run("Atlas Codes "+name, func(t *testing.T) {
			if _, err := codesProvider.ImportRecordExists(context.Background(), mutate(codeRecord), nil); !errors.Is(err, exchange.ErrInvalidInput) {
				t.Fatalf("non-canonical Atlas Codes record was accepted: %v", err)
			}
		})
	}
}
