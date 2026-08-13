// Requirements: REQ-API-001. Feature: integrations.protocols.
package grpcapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	stewardmeshv1 "github.com/maxlemke/stewardmesh/api/proto"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	grpcencoding "google.golang.org/grpc/encoding"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/tap"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestRegisterAllCoversEveryDeclaredServiceAndRPC(t *testing.T) {
	gateway, err := New(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), Options{AllowedOrigin: "http://127.0.0.1:5173", Guard: &fakeAuthenticator{}})
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	if err := gateway.RegisterAll(server); err != nil {
		t.Fatal(err)
	}
	registered := server.GetServiceInfo()
	services := stewardmeshv1.File_stewardmesh_proto.Services()
	declaredMethods := 0
	registeredMethods := 0
	if len(registered) != services.Len() {
		t.Fatalf("registered %d services, descriptor declares %d", len(registered), services.Len())
	}
	for serviceIndex := 0; serviceIndex < services.Len(); serviceIndex++ {
		service := services.Get(serviceIndex)
		information, ok := registered[string(service.FullName())]
		if !ok {
			t.Errorf("service %s is not registered", service.FullName())
			continue
		}
		declaredMethods += service.Methods().Len()
		registeredMethods += len(information.Methods)
		if len(information.Methods) != service.Methods().Len() {
			t.Errorf("service %s registered %d methods, descriptor declares %d", service.FullName(), len(information.Methods), service.Methods().Len())
		}
	}
	if declaredMethods != 154 || registeredMethods != declaredMethods {
		t.Fatalf("registered %d methods, descriptor declares %d", registeredMethods, declaredMethods)
	}
}

func TestGatewayAcceptsOnlyBearerAndUsesNonBrowserProtection(t *testing.T) {
	requests := 0
	authenticator := &fakeAuthenticator{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Origin") != "" || r.Header.Get("X-CSRF-Token") != "" || r.Header.Get("X-Client-Cookie") != "" {
			t.Errorf("client authentication metadata reached HTTP: %#v", r.Header)
		}
		switch r.URL.Path {
		case "/api/v1/guard/roles":
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), "attacker-csrf") || strings.Contains(string(body), "csrfToken") {
				t.Errorf("client CSRF reached body: %s", body)
			}
			writeTestJSON(w, http.StatusCreated, map[string]any{"id": "role-one", "name": "Operators", "permissions": []string{"assets.read"}})
		default:
			t.Fatalf("unexpected HTTP route %s", r.URL.Path)
		}
	})
	gateway, err := New(handler, Options{AllowedOrigin: "https://stewardmesh.example.test", OrganizationID: "example-org", Guard: authenticator})
	if err != nil {
		t.Fatal(err)
	}
	service := stewardmeshv1.File_stewardmesh_proto.Services().ByName("GuardService")
	method := service.Methods().ByName("CreateRole")
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"authorization", "Bearer opaque-session",
		"cookie", "stewardmesh_session=attacker-session",
		"origin", "https://attacker.example",
		"x-csrf-token", "attacker-csrf",
		"x-client-cookie", "attacker-session",
	))
	response, err := gateway.invoke(ctx, "/stewardmesh.v1.GuardService/CreateRole", method.Output(), &stewardmeshv1.CreateRoleRequest{
		Name: "Operators", Permissions: []string{"assets.read"}, CsrfToken: "attacker-csrf",
	})
	if err != nil {
		t.Fatal(err)
	}
	role := response.(*dynamicpb.Message)
	if got := role.Get(role.Descriptor().Fields().ByName("id")).String(); got != "role-one" {
		t.Fatalf("role id = %q", got)
	}
	if requests != 1 || authenticator.calls != 1 {
		t.Fatalf("HTTP requests=%d authentications=%d, want one each", requests, authenticator.calls)
	}

	for _, incoming := range []metadata.MD{
		metadata.Pairs("authorization", "Bearer one", "authorization", "Bearer two"),
		metadata.Pairs("authorization", "Basic opaque-session"),
		metadata.Pairs("authorization", " Bearer opaque-session"),
		metadata.Pairs("authorization", "Bearer  opaque-session"),
		metadata.Pairs("authorization", "Bearer opaque-session "),
		metadata.Pairs("authorization", "Bearer\topaque-session"),
		metadata.Pairs("authorization", "Bearer "+strings.Repeat("x", maximumBearerBytes+1)),
		{},
	} {
		_, err := gateway.invoke(metadata.NewIncomingContext(context.Background(), incoming), "/stewardmesh.v1.GuardService/CreateRole", method.Output(), &stewardmeshv1.CreateRoleRequest{})
		if status.Code(err) != codes.Unauthenticated {
			t.Fatalf("metadata %#v code=%s err=%v", incoming, status.Code(err), err)
		}
	}
	if requests != 1 || authenticator.calls != 1 {
		t.Fatalf("invalid authentication reached HTTP; requests=%d", requests)
	}
}

func TestTapHandleAuthenticatesProtectedRPCBeforeDecode(t *testing.T) {
	authenticator := &fakeAuthenticator{}
	gateway, err := New(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("HTTP handler must not run during transport tap authentication")
	}), Options{AllowedOrigin: "https://stewardmesh.example.test", Guard: authenticator})
	if err != nil {
		t.Fatal(err)
	}
	protected := &tap.Info{FullMethodName: "/stewardmesh.v1.ExchangeService/ImportExchangePackage"}
	for _, incoming := range []metadata.MD{
		{},
		metadata.Pairs("authorization", "Basic opaque-session"),
		metadata.Pairs("authorization", "Bearer one", "authorization", "Bearer two"),
		metadata.Pairs("authorization", "Bearer "+strings.Repeat("x", maximumBearerBytes+1)),
	} {
		_, err := gateway.TapHandle(metadata.NewIncomingContext(context.Background(), incoming), protected)
		if status.Code(err) != codes.Unauthenticated {
			t.Fatalf("metadata %#v code=%s err=%v", incoming, status.Code(err), err)
		}
	}
	if authenticator.calls != 0 {
		t.Fatalf("malformed Bearer metadata reached Guard %d times", authenticator.calls)
	}

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer opaque-session"))
	preauthenticated, err := gateway.TapHandle(ctx, protected)
	if err != nil {
		t.Fatal(err)
	}
	if authenticator.calls != 1 {
		t.Fatalf("Guard authentication calls=%d, want one in tap", authenticator.calls)
	}
	_, _, err = gateway.authenticateTransport(preauthenticated, "opaque-session")
	if err != nil {
		t.Fatal(err)
	}
	if authenticator.calls != 1 {
		t.Fatalf("preauthenticated context revalidated Guard; calls=%d", authenticator.calls)
	}

	publicContext, err := gateway.TapHandle(context.Background(), &tap.Info{FullMethodName: "/stewardmesh.v1.GuardService/GetBootstrapStatus"})
	if err != nil || publicContext == nil || authenticator.calls != 1 {
		t.Fatalf("public tap context=%v calls=%d err=%v", publicContext, authenticator.calls, err)
	}
	mismatched, err := New(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), Options{
		AllowedOrigin: "https://stewardmesh.example.test", OrganizationID: "different-org", Guard: &fakeAuthenticator{},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = mismatched.TapHandle(ctx, protected)
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("cross-organization session code=%s err=%v", status.Code(err), err)
	}
}

func TestTapHandleRejectsAuthenticationBeforeUnmarshalAndHandler(t *testing.T) {
	handlerCalls := 0
	authenticator := &fakeAuthenticator{}
	gateway, err := New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalls++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/assets" {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		writeTestJSON(w, http.StatusOK, map[string]any{"items": []any{}})
	}), Options{AllowedOrigin: "https://stewardmesh.example.test", Guard: authenticator})
	if err != nil {
		t.Fatal(err)
	}
	codec := &countingCodec{Codec: gateway.TransportCodec()}
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer(grpc.InTapHandle(gateway.TapHandle), grpc.ForceServerCodec(codec))
	if err := gateway.RegisterAll(server); err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	connection, err := grpc.NewClient("passthrough:///predecode-auth", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	client := stewardmeshv1.NewAssetServiceClient(connection)
	for _, callContext := range []context.Context{
		context.Background(),
		metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Basic opaque-session")),
		metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer one", "authorization", "Bearer two")),
	} {
		if _, err := client.ListAssets(callContext, &stewardmeshv1.ListAssetsRequest{}); status.Code(err) != codes.Unauthenticated {
			t.Fatalf("unauthenticated call code=%s err=%v", status.Code(err), err)
		}
	}
	if got := codec.unmarshals.Load(); got != 0 || handlerCalls != 0 || authenticator.calls != 0 {
		t.Fatalf("predecode rejection unmarshals=%d handlers=%d authentications=%d", got, handlerCalls, authenticator.calls)
	}
	authenticated := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer opaque-session"))
	if _, err := client.ListAssets(authenticated, &stewardmeshv1.ListAssetsRequest{}); err != nil {
		t.Fatal(err)
	}
	if got := codec.unmarshals.Load(); got != 1 || handlerCalls != 1 || authenticator.calls != 1 {
		t.Fatalf("authenticated call unmarshals=%d handlers=%d authentications=%d", got, handlerCalls, authenticator.calls)
	}
}

type countingCodec struct {
	grpcencoding.Codec
	unmarshals atomic.Int32
}

func (codec *countingCodec) Unmarshal(data []byte, value any) error {
	codec.unmarshals.Add(1)
	return codec.Codec.Unmarshal(data, value)
}

func TestTransportCodecRequestBoundaries(t *testing.T) {
	gateway, err := New(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), Options{AllowedOrigin: "https://stewardmesh.example.test", Guard: &fakeAuthenticator{}})
	if err != nil {
		t.Fatal(err)
	}
	codec := gateway.TransportCodec()

	// An unknown length-delimited field provides a valid protobuf payload at
	// the exact wire envelope without assigning attacker-controlled data to a
	// public authentication field. The transport codec discards it.
	publicAtLimit := bytesFieldWirePayload(MaximumPublicMessageBytes - 4)
	publicAtLimit[0] = 0x7a // unknown field 15, wire type 2
	if err := codec.Unmarshal(publicAtLimit, &stewardmeshv1.BootstrapAdministratorRequest{}); err != nil {
		t.Fatalf("public request at limit: %v", err)
	}
	publicOverLimit := append(append([]byte(nil), publicAtLimit...), 0)
	if err := codec.Unmarshal(publicOverLimit, &stewardmeshv1.BootstrapAdministratorRequest{}); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("public request over limit code=%s err=%v", status.Code(err), err)
	}

	for _, binary := range []struct {
		name       string
		fieldTag   byte
		newRequest func() proto.Message
		content    func(proto.Message) []byte
	}{
		{name: "Exchange archive", fieldTag: 0x0a, newRequest: func() proto.Message { return &stewardmeshv1.ImportExchangePackageRequest{} }, content: func(value proto.Message) []byte {
			return value.(*stewardmeshv1.ImportExchangePackageRequest).GetArchive()
		}},
		{name: "Vault content", fieldTag: 0x1a, newRequest: func() proto.Message { return &stewardmeshv1.CreateBlobRequest{} }, content: func(value proto.Message) []byte { return value.(*stewardmeshv1.CreateBlobRequest).GetContent() }},
	} {
		t.Run(binary.name, func(t *testing.T) {
			atLimit := bytesFieldWirePayload(MaximumMessageBytes - 5)
			atLimit[0] = binary.fieldTag
			if len(atLimit) != MaximumMessageBytes {
				t.Fatalf("test wire size=%d, want %d", len(atLimit), MaximumMessageBytes)
			}
			request := binary.newRequest()
			if err := codec.Unmarshal(atLimit, request); err != nil || len(binary.content(request)) != MaximumMessageBytes-5 {
				t.Fatalf("request at limit content=%d err=%v", len(binary.content(request)), err)
			}
			overLimit := append(append([]byte(nil), atLimit...), 0)
			if err := codec.Unmarshal(overLimit, binary.newRequest()); status.Code(err) != codes.ResourceExhausted {
				t.Fatalf("request over limit code=%s err=%v", status.Code(err), err)
			}
		})
	}
}

func TestBinaryResponseBoundaries(t *testing.T) {
	for _, binary := range []struct {
		name       string
		descriptor protoreflect.MessageDescriptor
		field      string
	}{
		{name: "Exchange archive", descriptor: (&stewardmeshv1.ExchangeExportArtifact{}).ProtoReflect().Descriptor(), field: "archive"},
		{name: "Vault content", descriptor: (&stewardmeshv1.VaultBlobContent{}).ProtoReflect().Descriptor(), field: "content"},
	} {
		t.Run(binary.name, func(t *testing.T) {
			atLimit, err := responseMessage(binary.descriptor, map[string]any{binary.field: make([]byte, MaximumMessageBytes-5)})
			if err != nil {
				t.Fatalf("response at limit: %v", err)
			}
			if got := proto.Size(atLimit); got != MaximumMessageBytes {
				t.Fatalf("response wire size=%d, want %d", got, MaximumMessageBytes)
			}
			_, err = responseMessage(binary.descriptor, map[string]any{binary.field: make([]byte, MaximumMessageBytes-4)})
			if status.Code(err) != codes.ResourceExhausted {
				t.Fatalf("response over limit code=%s err=%v", status.Code(err), err)
			}
		})
	}
}

func bytesFieldWirePayload(valueBytes int) []byte {
	result := make([]byte, 0, valueBytes+5)
	result = append(result, 0x0a)
	value := uint64(valueBytes)
	for value >= 0x80 {
		result = append(result, byte(value)|0x80)
		value >>= 7
	}
	result = append(result, byte(value))
	return append(result, make([]byte, valueBytes)...)
}

func TestPublicAuthenticationResponseOmitsBrowserCSRF(t *testing.T) {
	for _, test := range []struct {
		name       string
		fullMethod string
		request    proto.Message
		path       string
	}{
		{name: "bootstrap", fullMethod: "/stewardmesh.v1.GuardService/BootstrapAdministrator", request: &stewardmeshv1.BootstrapAdministratorRequest{Username: "admin", Password: "a sufficiently long password"}, path: "/api/v1/auth/bootstrap"},
		{name: "local login", fullMethod: "/stewardmesh.v1.GuardService/AuthenticateLocal", request: &stewardmeshv1.AuthenticateLocalRequest{Username: "admin", Password: "a sufficiently long password"}, path: "/api/v1/auth/login"},
	} {
		t.Run(test.name, func(t *testing.T) {
			gateway, err := New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != test.path {
					t.Fatalf("path=%s, want %s", r.URL.Path, test.path)
				}
				http.SetCookie(w, &http.Cookie{Name: localSessionCookie, Value: "new-session"})
				writeTestJSON(w, http.StatusOK, map[string]any{
					"sessionToken": "", "csrfToken": "browser-only-secret", "permissions": []any{"assets.read"},
				})
			}), Options{AllowedOrigin: "https://stewardmesh.example.test", Guard: &fakeAuthenticator{}})
			if err != nil {
				t.Fatal(err)
			}
			service := stewardmeshv1.File_stewardmesh_proto.Services().ByName("GuardService")
			method := service.Methods().ByName(protoreflect.Name(strings.TrimPrefix(test.fullMethod, "/stewardmesh.v1.GuardService/")))
			response, err := gateway.invoke(context.Background(), test.fullMethod, method.Output(), test.request)
			if err != nil {
				t.Fatal(err)
			}
			message := response.ProtoReflect()
			if got := message.Get(message.Descriptor().Fields().ByName("session_token")).String(); got != "new-session" {
				t.Fatalf("session token=%q", got)
			}
			if got := message.Get(message.Descriptor().Fields().ByName("csrf_token")).String(); got != "" {
				t.Fatalf("browser CSRF leaked over gRPC: %q", got)
			}
		})
	}
}

func TestBootstrapTrustRequiresExplicitLoopbackPeer(t *testing.T) {
	const allowedOrigin = "https://stewardmesh.example.test"
	requests := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if loopbackAddress(r.RemoteAddr) {
			if got := r.Header.Get("Origin"); got != allowedOrigin {
				t.Errorf("loopback bootstrap Origin=%q, want %q", got, allowedOrigin)
			}
			http.SetCookie(w, &http.Cookie{Name: localSessionCookie, Value: "new-session"})
			writeTestJSON(w, http.StatusCreated, map[string]any{"permissions": []any{"assets.read"}})
			return
		}
		if got := r.Header.Get("Origin"); got != "" {
			t.Errorf("unknown peer received trusted Origin %q", got)
		}
		if r.RemoteAddr != unknownPeerAddress {
			t.Errorf("unknown peer RemoteAddr=%q, want %q", r.RemoteAddr, unknownPeerAddress)
		}
		writeTestJSON(w, http.StatusForbidden, map[string]any{
			"error": map[string]any{"code": "bootstrap_denied", "message": "administrator setup is not authorized"},
		})
	})
	gateway, err := New(handler, Options{AllowedOrigin: allowedOrigin, Guard: &fakeAuthenticator{}})
	if err != nil {
		t.Fatal(err)
	}
	service := stewardmeshv1.File_stewardmesh_proto.Services().ByName("GuardService")
	method := service.Methods().ByName("BootstrapAdministrator")
	request := &stewardmeshv1.BootstrapAdministratorRequest{Username: "admin", Password: "a sufficiently long password"}

	if _, err := gateway.invoke(context.Background(), "/stewardmesh.v1.GuardService/BootstrapAdministrator", method.Output(), request); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("missing peer bootstrap code=%s err=%v", status.Code(err), err)
	}
	loopback := peer.NewContext(context.Background(), &peer.Peer{Addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 9443}})
	response, err := gateway.invoke(loopback, "/stewardmesh.v1.GuardService/BootstrapAdministrator", method.Output(), request)
	if err != nil {
		t.Fatalf("explicit loopback bootstrap: %v", err)
	}
	if response == nil || requests != 2 {
		t.Fatalf("response=%v requests=%d, want one denied unknown peer and one trusted loopback call", response, requests)
	}
}

func TestPatternsValidateRecordAllowsOrganizationIDAsStructKey(t *testing.T) {
	const recordOrganizationID = "user-record-organization"
	requests := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/templates/asset-record/validate" {
			t.Errorf("HTTP request = %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		values, ok := body["values"].(map[string]any)
		if !ok {
			t.Errorf("values = %#v, want object", body["values"])
		} else if got := values["organizationId"]; got != recordOrganizationID {
			t.Errorf("values.organizationId = %#v, want %q", got, recordOrganizationID)
		}
		writeTestJSON(w, http.StatusOK, map[string]any{
			"status":            "valid",
			"normalizedValues":  map[string]any{"organizationId": recordOrganizationID},
			"errors":            []any{},
			"holdingReferences": []any{},
		})
	})
	gateway, err := New(handler, Options{
		AllowedOrigin:  "https://stewardmesh.example.test",
		OrganizationID: "example-org",
		Guard:          &fakeAuthenticator{},
	})
	if err != nil {
		t.Fatal(err)
	}
	values, err := structpb.NewStruct(map[string]any{"organizationId": recordOrganizationID})
	if err != nil {
		t.Fatal(err)
	}
	service := stewardmeshv1.File_stewardmesh_proto.Services().ByName("PatternsService")
	method := service.Methods().ByName("ValidateRecord")
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer opaque-session"))
	response, err := gateway.invoke(ctx, "/stewardmesh.v1.PatternsService/ValidateRecord", method.Output(), &stewardmeshv1.ValidatePatternsRecordRequest{
		TemplateId: "asset-record",
		Values:     values,
	})
	if err != nil {
		t.Fatalf("Struct application key was rejected as transport identity: %v", err)
	}
	if response == nil || requests != 1 {
		t.Fatalf("response = %v, HTTP requests = %d; want one ordinary REST result", response, requests)
	}
}

func TestAssetLabelUsesActualPostMetadataWithoutConsumingReadRoute(t *testing.T) {
	requests := 0
	templateJSON, err := json.Marshal(map[string]any{
		"id": "label-one", "patternTemplateId": "label-one", "patternVersion": 2, "name": "Code label", "version": 2,
		"widthMm": 50, "heightMm": 25, "marginMm": 2, "quietZoneMm": 1, "symbology": "code128",
		"payloadSource": "identifier_value", "humanReadableField": "assetTag", "safeAssetFields": []any{"assetTag"},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/asset-label-batches" {
			t.Fatalf("adapter consumed a second Atlas Codes route: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Content-Disposition", `inline; filename="label.svg"`)
		w.Header().Set("X-Label-Batch-ID", "batch-one")
		w.Header().Set("X-Label-Item-Count", "1")
		w.Header().Set("X-Content-SHA256", strings.Repeat("a", 64))
		w.Header().Set("X-Label-Created-At", "2026-08-13T12:00:00Z")
		w.Header().Set("X-Idempotent-Replay", "false")
		w.Header().Set("X-StewardMesh-Label-Template", base64.RawURLEncoding.EncodeToString(templateJSON))
		_, _ = w.Write([]byte("<svg/>"))
	})
	gateway, err := New(handler, Options{AllowedOrigin: "https://stewardmesh.example.test", Guard: &fakeAuthenticator{}})
	if err != nil {
		t.Fatal(err)
	}
	service := stewardmeshv1.File_stewardmesh_proto.Services().ByName("AssetService")
	method := service.Methods().ByName("GenerateAssetLabelBatch")
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer opaque-session"))
	response, err := gateway.invoke(ctx, "/stewardmesh.v1.AssetService/GenerateAssetLabelBatch", method.Output(), &stewardmeshv1.GenerateAssetLabelBatchRequest{
		IdempotencyKey: "label-retry", TemplateId: "label-one", TemplateVersion: 2, IdentifierIds: []string{"identifier-one"}, Output: stewardmeshv1.AssetLabelOutput_ASSET_LABEL_OUTPUT_SVG,
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact := response.ProtoReflect()
	template := artifact.Get(artifact.Descriptor().Fields().ByName("template")).Message()
	if got := template.Get(template.Descriptor().Fields().ByName("id")).String(); got != "label-one" {
		t.Fatalf("template id=%q", got)
	}
	if requests != 1 {
		t.Fatalf("HTTP requests=%d, want only the label POST so the independent read rate bucket is untouched", requests)
	}
}

type fakeAuthenticator struct{ calls int }

func (f *fakeAuthenticator) AuthenticateSession(_ context.Context, token string) (guard.Authentication, error) {
	f.calls++
	if token != "opaque-session" {
		return guard.Authentication{}, guard.ErrInvalidSession
	}
	return guard.Authentication{
		Session:   guard.Session{ID: "session-one", OrganizationID: "example-org", ExpiresAt: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)},
		Principal: guard.Principal{Subject: "administrator", OrganizationID: "example-org", Username: "administrator"},
		Grants:    []guard.Grant{{Permission: guard.PermissionGuardManage, Scope: guard.Scope{Kind: guard.ScopeOrganization, OrganizationID: "example-org", ResourceID: "example-org"}}},
	}, nil
}

func writeTestJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}
