// Requirements: REQ-API-001. Feature: integrations.protocols.
package grpcapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"

	stewardmeshv1 "github.com/maxlemke/stewardmesh/api/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
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
