// Requirements: REQ-API-001. Feature: integrations.protocols.
package grpcapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	stewardmeshv1 "github.com/maxlemke/stewardmesh/api/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestMessageObjectUsesRESTEnumWireValues(t *testing.T) {
	bridge, err := messageObject(&stewardmeshv1.CreateBridgeClientRequest{
		AllowedScopes: []stewardmeshv1.BridgeScope{
			stewardmeshv1.BridgeScope_BRIDGE_SCOPE_MCP_RESOURCES,
			stewardmeshv1.BridgeScope_BRIDGE_SCOPE_SIGNALS_ACKNOWLEDGE,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := bridge["allowedScopes"], []any{"mcp:resources", "signals:acknowledge"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Bridge scopes = %#v, want %#v", got, want)
	}

	label, err := messageObject(&stewardmeshv1.GenerateAssetLabelBatchRequest{
		Output: stewardmeshv1.AssetLabelOutput_ASSET_LABEL_OUTPUT_PDF,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := label["output"]; got != "pdf" {
		t.Fatalf("normal enum = %#v, want pdf", got)
	}
}

func TestMessageObjectPreservesEmptyRESTCollections(t *testing.T) {
	stackRequest, err := messageObject(&stewardmeshv1.ImportStackRecordsRequest{
		SourceSystemId: "source-one",
		Records: []*stewardmeshv1.StackExchangeRecord{{
			Type: "stack.product", Id: "product-one", Revision: 1,
			Dependencies: []string{},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	records, ok := stackRequest["records"].([]any)
	if !ok || len(records) != 1 {
		t.Fatalf("records = %#v, want one record", stackRequest["records"])
	}
	record, ok := records[0].(map[string]any)
	if !ok {
		t.Fatalf("record = %#v, want object", records[0])
	}
	dependencies, ok := record["dependencies"].([]any)
	if !ok || len(dependencies) != 0 {
		t.Fatalf("dependencies = %#v, want explicit empty array", record["dependencies"])
	}

	modelRequest, err := messageObject(&stewardmeshv1.CreateAssetModelRequest{
		Model: &stewardmeshv1.AssetModel{Id: "model-one", Specifications: map[string]string{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	model, ok := modelRequest["model"].(map[string]any)
	if !ok {
		t.Fatalf("model = %#v, want object", modelRequest["model"])
	}
	specifications, ok := model["specifications"].(map[string]any)
	if !ok || len(specifications) != 0 {
		t.Fatalf("specifications = %#v, want explicit empty object", model["specifications"])
	}
}

func TestResponseMessagePopulatesRepresentativeJSONFields(t *testing.T) {
	instant := time.Date(2026, time.August, 13, 21, 15, 14, 123456789, time.UTC)

	t.Run("integers maps lists and timestamps", func(t *testing.T) {
		got, err := responseMessage((&stewardmeshv1.HorizonForecast{}).ProtoReflect().Descriptor(), map[string]any{
			"asOf":                    "2026-08-13T16:15:14.123456789-05:00",
			"groupBy":                 "asset_class",
			"currency":                "USD",
			"scenarios":               []any{"baseline", "accelerated"},
			"plannedReplacementMinor": json.Number("9007199254740991"),
			"assetCount":              float64(4),
			"totalsByKindMinor": map[string]any{
				"laptop": json.Number("1234567890123"),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		want := &stewardmeshv1.HorizonForecast{
			AsOf:                    timestamppb.New(instant),
			GroupBy:                 "asset_class",
			Currency:                "USD",
			Scenarios:               []string{"baseline", "accelerated"},
			PlannedReplacementMinor: 9007199254740991,
			AssetCount:              4,
			TotalsByKindMinor:       map[string]int64{"laptop": 1234567890123},
		}
		if !proto.Equal(got, want) {
			t.Fatalf("response = %v, want %v", got, want)
		}
	})

	t.Run("floats bytes and nested messages", func(t *testing.T) {
		content := []byte{0, 1, 2, 0xfe, 0xff}
		got, err := responseMessage((&stewardmeshv1.AssetLabelArtifact{}).ProtoReflect().Descriptor(), map[string]any{
			"batchId":   "batch-one",
			"output":    "pdf",
			"itemCount": json.Number("2"),
			"content":   base64.StdEncoding.EncodeToString(content),
			"createdAt": instant.Format(time.RFC3339Nano),
			"template": map[string]any{
				"id":              "label-standard",
				"version":         "3",
				"widthMm":         json.Number("57.25"),
				"safeAssetFields": []any{"assetTag", "name"},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		want := &stewardmeshv1.AssetLabelArtifact{
			BatchId:   "batch-one",
			Output:    stewardmeshv1.AssetLabelOutput_ASSET_LABEL_OUTPUT_PDF,
			ItemCount: 2,
			Content:   content,
			CreatedAt: timestamppb.New(instant),
			Template: &stewardmeshv1.AssetLabelTemplate{
				Id:              "label-standard",
				Version:         3,
				WidthMm:         57.25,
				SafeAssetFields: []string{"assetTag", "name"},
			},
		}
		if !proto.Equal(got, want) {
			t.Fatalf("response = %v, want %v", got, want)
		}
	})
}

func TestInvokeRejectsNestedOrganizationIdentity(t *testing.T) {
	service := stewardmeshv1.File_stewardmesh_proto.Services().ByName("AssetService")
	method := service.Methods().ByName("CreateAsset")
	gateway := &Gateway{routes: routes()}

	_, err := gateway.invoke(context.Background(), "/stewardmesh.v1.AssetService/CreateAsset", method.Output(), &stewardmeshv1.CreateAssetRequest{
		Asset: &stewardmeshv1.Asset{OrganizationId: "caller-selected-organization"},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %s, want %s: %v", status.Code(err), codes.InvalidArgument, err)
	}
	if got := status.Convert(err).Message(); got != "organization identity is derived from authentication" {
		t.Fatalf("message = %q", got)
	}
}

func TestAtlasMutationsRejectPopulatedResponseOnlyFields(t *testing.T) {
	type mutationSpec struct {
		name       string
		fullMethod string
		prefix     string
		fields     []string
		request    func() (proto.Message, protoreflect.Message)
	}
	specs := []mutationSpec{
		{
			name: "create model", fullMethod: "/stewardmesh.v1.AssetService/CreateAssetModel", prefix: "model",
			fields: []string{"status", "instanceCount", "revision", "createdAt", "updatedAt"},
			request: func() (proto.Message, protoreflect.Message) {
				model := &stewardmeshv1.AssetModel{Id: "model-one", Manufacturer: "Example", Name: "Model one", Kind: "server"}
				return &stewardmeshv1.CreateAssetModelRequest{Model: model}, model.ProtoReflect()
			},
		},
		{
			name: "update model", fullMethod: "/stewardmesh.v1.AssetService/UpdateAssetModel", prefix: "model",
			fields: []string{"status", "instanceCount", "createdAt", "updatedAt"},
			request: func() (proto.Message, protoreflect.Message) {
				model := &stewardmeshv1.AssetModel{Id: "model-one", Manufacturer: "Example", Name: "Model one", Kind: "server", Revision: 1}
				return &stewardmeshv1.UpdateAssetModelRequest{Model: model}, model.ProtoReflect()
			},
		},
		{
			name: "create asset", fullMethod: "/stewardmesh.v1.AssetService/CreateAsset", prefix: "asset",
			fields: []string{"revision", "createdAt", "updatedAt", "modelContext"},
			request: func() (proto.Message, protoreflect.Message) {
				asset := &stewardmeshv1.Asset{Id: "asset-one", Name: "Asset one", Kind: "server", Status: "active"}
				return &stewardmeshv1.CreateAssetRequest{Asset: asset}, asset.ProtoReflect()
			},
		},
		{
			name: "update asset response", fullMethod: "/stewardmesh.v1.AssetService/UpdateAsset", prefix: "asset",
			fields: []string{"createdAt", "updatedAt", "modelContext"},
			request: func() (proto.Message, protoreflect.Message) {
				asset := &stewardmeshv1.Asset{Id: "asset-one", Name: "Asset one", Kind: "server", Status: "active", Revision: 1}
				return &stewardmeshv1.UpdateAssetRequest{Asset: asset}, asset.ProtoReflect()
			},
		},
		{
			name: "bulk create asset", fullMethod: "/stewardmesh.v1.AssetService/CreateAssetsFromModel", prefix: "items[0]",
			fields: []string{"revision", "createdAt", "updatedAt", "modelContext"},
			request: func() (proto.Message, protoreflect.Message) {
				asset := &stewardmeshv1.Asset{Id: "asset-one", Name: "Asset one", Kind: "server", Status: "active"}
				return &stewardmeshv1.CreateAssetsFromModelRequest{ModelId: "model-one", Items: []*stewardmeshv1.Asset{asset}}, asset.ProtoReflect()
			},
		},
	}
	gateway := &Gateway{}
	configuredRoutes := routes()
	instant := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	for _, spec := range specs {
		for _, fieldName := range spec.fields {
			t.Run(spec.name+" "+fieldName, func(t *testing.T) {
				request, target := spec.request()
				field := target.Descriptor().Fields().ByJSONName(fieldName)
				if field == nil {
					t.Fatalf("field %s is not declared", fieldName)
				}
				switch fieldName {
				case "createdAt", "updatedAt":
					target.Set(field, protoreflect.ValueOfMessage(timestamppb.New(instant).ProtoReflect()))
				case "modelContext":
					target.Set(field, protoreflect.ValueOfMessage((&stewardmeshv1.AssetModelContext{Name: "response snapshot"}).ProtoReflect()))
				default:
					switch field.Kind() {
					case protoreflect.StringKind:
						target.Set(field, protoreflect.ValueOfString("active"))
					case protoreflect.Int32Kind, protoreflect.Int64Kind:
						target.Set(field, protoreflect.ValueOfInt64(1))
					default:
						t.Fatalf("unsupported field kind %s", field.Kind())
					}
				}
				object, err := messageObject(request)
				if err != nil {
					t.Fatal(err)
				}
				_, err = gateway.prepareRequest(context.Background(), configuredRoutes[spec.fullMethod], object)
				if err == nil || !strings.Contains(err.Error(), spec.prefix+"."+fieldName+" is response-only") {
					t.Fatalf("error=%v", err)
				}
			})
		}
	}
}

func TestPrepareRequestExtractsPathQueryHeaderAndBody(t *testing.T) {
	gateway := &Gateway{}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-correlation-id", "correlation-one"))
	object := map[string]any{
		"model": map[string]any{
			"id":   "model /one",
			"name": "Portable Workstation",
		},
		"revision":       json.Number("7"),
		"idempotencyKey": "retry-one",
	}
	configured := route{
		method:       http.MethodPost,
		path:         "/api/v1/models/{id}",
		pathFields:   map[string]string{"id": "model.id"},
		queryFields:  map[string]string{"revision": "revision"},
		headerFields: map[string]string{"idempotencyKey": "Idempotency-Key"},
		flatten:      []string{"model"},
	}

	prepared, err := gateway.prepareRequest(ctx, configured, object)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := prepared.path, "/api/v1/models/model%20%2Fone?revision=7"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if prepared.method != http.MethodPost {
		t.Fatalf("method = %q", prepared.method)
	}
	if got := prepared.headers.Get("Idempotency-Key"); got != "retry-one" {
		t.Fatalf("Idempotency-Key = %q", got)
	}
	if got := prepared.headers.Get("X-Correlation-ID"); got != "correlation-one" {
		t.Fatalf("X-Correlation-ID = %q", got)
	}
	if got := prepared.headers.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
	var body map[string]any
	if err := json.Unmarshal(prepared.body, &body); err != nil {
		t.Fatal(err)
	}
	if want := map[string]any{"name": "Portable Workstation"}; !reflect.DeepEqual(body, want) {
		t.Fatalf("body = %#v, want %#v", body, want)
	}
}

func TestVaultMultipartRejectsUnsafeMediaTypesAndKeepsEmptyFilePart(t *testing.T) {
	for _, mediaType := range []string{
		"text/plain\r\nX-Injected: true",
		"text/plain; charset=utf-8",
		"TEXT/PLAIN",
		strings.Repeat("a", 128),
	} {
		t.Run(mediaType, func(t *testing.T) {
			_, _, err := multipartBody(map[string]any{"name": "file.txt", "mediaType": mediaType, "content": []byte{}})
			if err == nil || err.Error() != "mediaType is invalid" {
				t.Fatalf("error=%v", err)
			}
		})
	}
	body, contentType, err := multipartBody(map[string]any{"name": "empty.txt", "mediaType": "text/plain", "content": []byte{}})
	if err != nil || len(body) == 0 || !strings.HasPrefix(contentType, "multipart/form-data; boundary=") {
		t.Fatalf("empty multipart body=%d contentType=%q err=%v", len(body), contentType, err)
	}
}

func TestReachVariablesAreStringMapAtTheWireBoundary(t *testing.T) {
	request := &stewardmeshv1.SendReachMessageRequest{Variables: map[string]string{"title": "Database renewal", "severity": "warning"}}
	object, err := messageObject(request)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := object["variables"], map[string]any{"title": "Database renewal", "severity": "warning"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("variables=%#v, want %#v", got, want)
	}
	field := request.ProtoReflect().Descriptor().Fields().ByName("variables")
	if !field.IsMap() || field.MapKey().Kind() != protoreflect.StringKind || field.MapValue().Kind() != protoreflect.StringKind {
		t.Fatalf("variables descriptor=%v, want map<string,string>", field)
	}
}

func TestConfiguredSearchAndRepeatedScenarioQueriesMatchREST(t *testing.T) {
	gateway := &Gateway{}
	for _, test := range []struct {
		name       string
		fullMethod string
		object     map[string]any
		wantPath   string
	}{
		{name: "asset models", fullMethod: "/stewardmesh.v1.AssetService/ListAssetModels", object: map[string]any{"search": "portable workstation"}, wantPath: "/api/v1/asset-models?q=portable+workstation"},
		{name: "assets", fullMethod: "/stewardmesh.v1.AssetService/ListAssets", object: map[string]any{"search": "asset tag"}, wantPath: "/api/v1/assets?q=asset+tag"},
		{name: "identities", fullMethod: "/stewardmesh.v1.PeopleService/SearchIdentities", object: map[string]any{"query": "Ada Lovelace"}, wantPath: "/api/v1/identities?q=Ada+Lovelace"},
		{name: "horizon scenarios", fullMethod: "/stewardmesh.v1.HorizonService/GetForecast", object: map[string]any{"scenarios": []any{"baseline", "accelerated"}}, wantPath: "/api/v1/horizon/forecast?scenarios=baseline&scenarios=accelerated"},
	} {
		t.Run(test.name, func(t *testing.T) {
			prepared, err := gateway.prepareRequest(context.Background(), routes()[test.fullMethod], test.object)
			if err != nil {
				t.Fatal(err)
			}
			if prepared.path != test.wantPath {
				t.Fatalf("path = %q, want %q", prepared.path, test.wantPath)
			}
		})
	}
}
