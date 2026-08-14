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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	stewardmeshv1 "github.com/maxlemke/stewardmesh/api/proto"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/storage"
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

	publicServer := grpc.NewServer()
	if err := gateway.RegisterPublic(publicServer); err != nil {
		t.Fatal(err)
	}
	public := publicServer.GetServiceInfo()
	if len(public) != 1 || len(public["stewardmesh.v1.GuardService"].Methods) != 3 {
		t.Fatalf("public registration=%#v", public)
	}
	protectedServer := grpc.NewServer()
	if err := gateway.RegisterProtected(protectedServer); err != nil {
		t.Fatal(err)
	}
	protectedMethods := 0
	for _, information := range protectedServer.GetServiceInfo() {
		protectedMethods += len(information.Methods)
	}
	if protectedMethods != 151 {
		t.Fatalf("protected methods=%d, want 151", protectedMethods)
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

func TestGatewayContainsApplicationPanicAndServesNextCall(t *testing.T) {
	requests := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			panic("private handler failure")
		}
		writeTestJSON(w, http.StatusCreated, map[string]any{"id": "role-one", "name": "Operators", "permissions": []any{"assets.read"}})
	})
	gateway, err := New(handler, Options{AllowedOrigin: "https://stewardmesh.example.test", Guard: &fakeAuthenticator{}})
	if err != nil {
		t.Fatal(err)
	}
	service := stewardmeshv1.File_stewardmesh_proto.Services().ByName("GuardService")
	method := service.Methods().ByName("CreateRole")
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer opaque-session"))
	request := &stewardmeshv1.CreateRoleRequest{Name: "Operators", Permissions: []string{"assets.read"}}
	if _, err := gateway.invoke(ctx, "/stewardmesh.v1.GuardService/CreateRole", method.Output(), request); status.Code(err) != codes.Internal || strings.Contains(status.Convert(err).Message(), "private handler failure") {
		t.Fatalf("panic code=%s message=%q", status.Code(err), status.Convert(err).Message())
	}
	response, err := gateway.invoke(ctx, "/stewardmesh.v1.GuardService/CreateRole", method.Output(), request)
	if err != nil || response == nil || requests != 2 {
		t.Fatalf("subsequent call response=%v requests=%d err=%v", response, requests, err)
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

func TestTransportCancellationTakesPrecedence(t *testing.T) {
	for _, test := range []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
		code codes.Code
	}{
		{name: "canceled", ctx: func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx, func() {}
		}, code: codes.Canceled},
		{name: "deadline", ctx: func() (context.Context, context.CancelFunc) {
			return context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		}, code: codes.DeadlineExceeded},
	} {
		t.Run("Guard "+test.name, func(t *testing.T) {
			ctx, cancel := test.ctx()
			defer cancel()
			gateway := &Gateway{guard: &fakeAuthenticator{}}
			_, _, err := gateway.authenticateTransport(ctx, "opaque-session")
			if status.Code(err) != test.code {
				t.Fatalf("code=%s err=%v", status.Code(err), err)
			}
		})
	}

	t.Run("handler canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			cancel()
			writeTestJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]any{"code": "handler_failed"}})
		})
		gateway, err := New(handler, Options{AllowedOrigin: "https://stewardmesh.example.test", Guard: &fakeAuthenticator{}})
		if err != nil {
			t.Fatal(err)
		}
		ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer opaque-session"))
		method := stewardmeshv1.File_stewardmesh_proto.Services().ByName("GuardService").Methods().ByName("CreateRole")
		_, err = gateway.invoke(ctx, "/stewardmesh.v1.GuardService/CreateRole", method.Output(), &stewardmeshv1.CreateRoleRequest{Name: "Operators"})
		if status.Code(err) != codes.Canceled {
			t.Fatalf("code=%s err=%v", status.Code(err), err)
		}
	})

	t.Run("handler deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
			writeTestJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]any{"code": "handler_failed"}})
		})
		gateway, err := New(handler, Options{AllowedOrigin: "https://stewardmesh.example.test", Guard: &fakeAuthenticator{}})
		if err != nil {
			t.Fatal(err)
		}
		ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer opaque-session"))
		method := stewardmeshv1.File_stewardmesh_proto.Services().ByName("GuardService").Methods().ByName("CreateRole")
		_, err = gateway.invoke(ctx, "/stewardmesh.v1.GuardService/CreateRole", method.Output(), &stewardmeshv1.CreateRoleRequest{Name: "Operators"})
		if status.Code(err) != codes.DeadlineExceeded {
			t.Fatalf("code=%s err=%v", status.Code(err), err)
		}
	})

	t.Run("Vault read canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		blobID := "0123456789abcdef0123456789abcdef"
		blob := storage.Blob{ID: blobID, OrganizationID: "example-org", Name: "file.txt", MediaType: "text/plain", SizeBytes: 1, SHA256: strings.Repeat("0", 64), Provider: "canceling", CreatedBy: "administrator", CreatedAt: time.Now().UTC()}
		blob.SetObjectKey("private/object")
		vault, err := storage.NewService(&grpcTestMetadataStore{blob: blob}, &grpcCancelingObjectStore{ctx: ctx, cancel: cancel}, foundation.NopAuditor{}, storage.ServiceConfig{OrganizationID: "example-org"})
		if err != nil {
			t.Fatal(err)
		}
		gateway, err := New(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) }), Options{
			AllowedOrigin: "https://stewardmesh.example.test", OrganizationID: "example-org", Guard: &fakeAuthenticator{}, Vault: vault,
		})
		if err != nil {
			t.Fatal(err)
		}
		output := stewardmeshv1.File_stewardmesh_proto.Services().ByName("VaultService").Methods().ByName("DownloadBlob").Output()
		_, err = gateway.downloadVault(ctx, output, guard.Authentication{Principal: guard.Principal{Subject: "administrator"}}, preparedRequest{}, map[string]any{"blobId": blobID})
		if status.Code(err) != codes.Canceled {
			t.Fatalf("code=%s err=%v", status.Code(err), err)
		}
	})
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
	publicRequest := &stewardmeshv1.BootstrapAdministratorRequest{}
	if err := codec.Unmarshal(publicOverLimit, publicRequest); err != nil {
		t.Fatalf("public request marker: %v", err)
	}
	if _, rejected := gateway.transportRejected.LoadAndDelete(publicRequest); !rejected {
		t.Fatal("public request over limit was not rejected before protobuf decoding")
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
			overRequest := binary.newRequest()
			if err := codec.Unmarshal(overLimit, overRequest); err != nil {
				t.Fatalf("request over limit marker: %v", err)
			}
			if _, rejected := gateway.transportRejected.LoadAndDelete(overRequest); !rejected {
				t.Fatal("request over limit was not rejected before protobuf decoding")
			}
		})
	}
}

func TestRealTransportPreservesPublicResourceExhausted(t *testing.T) {
	gateway, err := New(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("oversized public request reached the application handler")
	}), Options{AllowedOrigin: "https://stewardmesh.example.test", Guard: &fakeAuthenticator{}})
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer(grpc.MaxRecvMsgSize(MaximumMessageBytes), grpc.ForceServerCodec(gateway.TransportCodec()))
	if err := gateway.RegisterAll(server); err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	connection, err := grpc.NewClient("passthrough:///public-limit", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	request := &stewardmeshv1.BootstrapAdministratorRequest{}
	unknown := bytesFieldWirePayload(MaximumPublicMessageBytes - 3)
	unknown[0] = 0x7a
	request.ProtoReflect().SetUnknown(unknown)
	_, err = stewardmeshv1.NewGuardServiceClient(connection).BootstrapAdministrator(t.Context(), request)
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("code=%s err=%v", status.Code(err), err)
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

func TestBinaryConvertersAvoidRedundantTransportCopies(t *testing.T) {
	archive := make([]byte, 1024)
	archive[0] = 0x42
	request := &stewardmeshv1.ImportExchangePackageRequest{Archive: archive}
	object, err := messageObject(request)
	if err != nil {
		t.Fatal(err)
	}
	converted, ok := object["archive"].([]byte)
	if !ok || len(converted) != len(archive) || &converted[0] != &archive[0] {
		t.Fatal("protobuf archive was copied before synchronous REST adaptation")
	}
	configured := routes()["/stewardmesh.v1.ExchangeService/ImportExchangePackage"]
	responseContext := retainResponseContext(configured.responseKind, object)
	if len(responseContext) != 0 {
		t.Fatalf("binary request was retained for an unrelated JSON response: %#v", responseContext)
	}
	prepared, err := (&Gateway{}).prepareRequest(context.Background(), configured, object)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.body) != len(archive) || &prepared.body[0] != &archive[0] {
		t.Fatal("raw Exchange adaptation copied the archive")
	}

	content := make([]byte, 1024)
	content[0] = 0x24
	response, err := responseMessage((&stewardmeshv1.VaultBlobContent{}).ProtoReflect().Descriptor(), map[string]any{"content": content})
	if err != nil {
		t.Fatal(err)
	}
	returned := response.(*dynamicpb.Message).Get(response.ProtoReflect().Descriptor().Fields().ByName("content")).Bytes()
	if len(returned) != len(content) || &returned[0] != &content[0] {
		t.Fatal("binary REST response was copied before synchronous protobuf framing")
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
	const recordScopeKind = "user-authored-scope"
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
		} else if scope, ok := values["scope"].(map[string]any); !ok || scope["kind"] != recordScopeKind {
			t.Errorf("values.scope = %#v, want user-authored Struct data", values["scope"])
		}
		writeTestJSON(w, http.StatusOK, map[string]any{
			"status":            "valid",
			"normalizedValues":  map[string]any{"organizationId": recordOrganizationID, "scope": map[string]any{"kind": recordScopeKind}},
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
	values, err := structpb.NewStruct(map[string]any{
		"organizationId": recordOrganizationID,
		"scope":          map[string]any{"kind": recordScopeKind},
	})
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
	normalizedField := response.ProtoReflect().Descriptor().Fields().ByName("normalized_values")
	normalized, ok := response.ProtoReflect().Get(normalizedField).Message().Interface().(*structpb.Struct)
	if !ok {
		t.Fatalf("normalized values type = %T, want google.protobuf.Struct", response.ProtoReflect().Get(normalizedField).Message().Interface())
	}
	scope, ok := normalized.AsMap()["scope"].(map[string]any)
	if !ok || scope["kind"] != recordScopeKind {
		t.Fatalf("normalized scope = %#v, want preserved user data", normalized.AsMap()["scope"])
	}
	if _, injected := scope["organizationId"]; injected {
		t.Fatalf("transport organization was injected into user-owned Struct scope: %#v", scope)
	}
}

func TestTypedAuthorizationScopeDerivesOrganization(t *testing.T) {
	response, err := responseMessage((&stewardmeshv1.ListGuardAccessResponse{}).ProtoReflect().Descriptor(), map[string]any{
		"accounts":             []any{},
		"roles":                []any{},
		"policyBundles":        []any{},
		"availablePermissions": []any{},
		"assignments": []any{map[string]any{
			"id": "assignment-one", "accountId": "account-one", "roleId": "role-one",
			"scope": map[string]any{"kind": "site", "resourceId": "site-one"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	enrichAuthorizationScopes(response.ProtoReflect(), "example-org")
	assignments := response.ProtoReflect().Get(response.ProtoReflect().Descriptor().Fields().ByName("assignments")).List()
	if assignments.Len() != 1 {
		t.Fatalf("assignments = %d, want one", assignments.Len())
	}
	assignment := assignments.Get(0).Message()
	scope := assignment.Get(assignment.Descriptor().Fields().ByName("scope")).Message()
	if got := scope.Get(scope.Descriptor().Fields().ByName("organization_id")).String(); got != "example-org" {
		t.Fatalf("typed scope organization = %q, want derived organization", got)
	}
}

func TestPresenceHelpersTranslateToStrictRESTContracts(t *testing.T) {
	requests := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode %s: %v", r.URL.Path, err)
		}
		switch r.URL.Path {
		case "/api/v1/templates":
			fields, ok := body["fields"].([]any)
			if !ok || len(fields) != 1 {
				t.Fatalf("Patterns fields = %#v", body["fields"])
			}
			field := fields[0].(map[string]any)
			if field["minimum"] != float64(0) || field["maximum"] != float64(0) || field["hasMinimum"] != nil || field["hasMaximum"] != nil {
				t.Fatalf("Patterns REST presence body = %#v", field)
			}
			writeTestJSON(w, http.StatusCreated, map[string]any{
				"id": "bounded-zero", "recordType": "custom.zero", "name": "Bounded zero", "version": 1,
				"builtIn": false, "status": "active", "createdBy": "administrator", "createdAt": "2026-08-13T12:00:00Z",
				"fields": []any{map[string]any{
					"key": "count", "label": "Count", "type": "number", "required": true,
					"minimum": 0, "maximum": 0, "accessibleLabel": "Count", "csvHeader": "count",
				}},
			})
		case "/api/v1/signals/rules":
			if enabled, exists := body["enabled"]; !exists || enabled != false || body["hasEnabled"] != nil {
				t.Fatalf("Signals REST presence body = %#v", body)
			}
			writeTestJSON(w, http.StatusCreated, map[string]any{
				"id": "disabled-rule", "name": "Disabled rule", "condition": "renewal", "severity": "warning",
				"enabled": false, "thresholdDays": []any{}, "createdBy": "administrator", "revision": 1,
				"createdAt": "2026-08-13T12:00:00Z", "updatedAt": "2026-08-13T12:00:00Z",
			})
		case "/api/v1/reach/providers":
			enabled, exists := body["enabled"]
			if !exists {
				enabled = true
			}
			writeTestJSON(w, http.StatusCreated, map[string]any{
				"id": "reach-provider", "organizationId": "example-org", "name": "Reach provider", "kind": "webhook",
				"endpointId": "webhook-primary", "secretConfigured": true, "enabled": enabled, "revision": 1,
				"createdBy": "administrator", "updatedBy": "administrator",
				"createdAt": "2026-08-13T12:00:00Z", "updatedAt": "2026-08-13T12:00:00Z",
			})
		case "/api/v1/reach/providers/reach-provider":
			if body["endpointId"] != "webhook-primary" || body["name"] != "Reach provider" || body["enabled"] != false || body["revision"] != float64(1) {
				t.Fatalf("Reach provider update body = %#v", body)
			}
			if _, leaked := body["secretRef"]; leaked {
				t.Fatalf("generic Reach provider update accepted a secret field: %#v", body)
			}
			writeTestJSON(w, http.StatusOK, map[string]any{
				"id": "reach-provider", "organizationId": "example-org", "name": "Reach provider", "kind": "webhook",
				"endpointId": "webhook-primary", "secretConfigured": false, "enabled": false, "revision": 2,
				"createdBy": "administrator", "updatedBy": "administrator",
				"createdAt": "2026-08-13T12:00:00Z", "updatedAt": "2026-08-13T12:01:00Z",
			})
		default:
			t.Fatalf("unexpected route %s", r.URL.Path)
		}
	})
	gateway, err := New(handler, Options{AllowedOrigin: "https://stewardmesh.example.test", Guard: &fakeAuthenticator{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer opaque-session"))
	patternsService := stewardmeshv1.File_stewardmesh_proto.Services().ByName("PatternsService")
	patternsMethod := patternsService.Methods().ByName("CreateTemplate")
	patternsResponse, err := gateway.invoke(ctx, "/stewardmesh.v1.PatternsService/CreateTemplate", patternsMethod.Output(), &stewardmeshv1.CreatePatternsTemplateRequest{
		Id: "bounded-zero", RecordType: "custom.zero", Name: "Bounded zero",
		Fields: []*stewardmeshv1.PatternsField{{
			Key: "count", Label: "Count", Type: stewardmeshv1.PatternsFieldType_PATTERNS_FIELD_TYPE_NUMBER,
			Required: true, Minimum: 0, HasMinimum: true, Maximum: 0, HasMaximum: true,
			AccessibleLabel: "Count", CsvHeader: "count",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	patterns := patternsResponse.ProtoReflect()
	fields := patterns.Get(patterns.Descriptor().Fields().ByName("fields")).List()
	if fields.Len() != 1 {
		t.Fatalf("Patterns response fields = %d", fields.Len())
	}
	field := fields.Get(0).Message()
	if !field.Get(field.Descriptor().Fields().ByName("has_minimum")).Bool() || !field.Get(field.Descriptor().Fields().ByName("has_maximum")).Bool() {
		t.Fatalf("Patterns response lost explicit zero presence: %v", field.Interface())
	}

	signalsService := stewardmeshv1.File_stewardmesh_proto.Services().ByName("SignalsService")
	signalsMethod := signalsService.Methods().ByName("CreateRule")
	signalsResponse, err := gateway.invoke(ctx, "/stewardmesh.v1.SignalsService/CreateRule", signalsMethod.Output(), &stewardmeshv1.CreateSignalRuleRequest{
		Id: "disabled-rule", Name: "Disabled rule", Condition: "renewal", Severity: "warning", Enabled: false, HasEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if enabled := signalsResponse.ProtoReflect().Get(signalsResponse.ProtoReflect().Descriptor().Fields().ByName("enabled")).Bool(); enabled {
		t.Fatal("explicit disabled Signals rule became enabled")
	}

	reachService := stewardmeshv1.File_stewardmesh_proto.Services().ByName("ReachService")
	reachMethod := reachService.Methods().ByName("CreateProvider")
	for _, test := range []struct {
		name    string
		enabled *bool
		want    bool
	}{
		{name: "explicit disabled", enabled: proto.Bool(false), want: false},
		{name: "omitted defaults enabled", want: true},
	} {
		t.Run("Reach "+test.name, func(t *testing.T) {
			response, invokeErr := gateway.invoke(ctx, "/stewardmesh.v1.ReachService/CreateProvider", reachMethod.Output(), &stewardmeshv1.CreateReachProviderRequest{
				Id: "reach-provider", Name: "Reach provider", Kind: "webhook", EndpointId: "webhook-primary", SecretRef: "env:REACH_WEBHOOK", Enabled: test.enabled,
			})
			if invokeErr != nil {
				t.Fatal(invokeErr)
			}
			if enabled := response.ProtoReflect().Get(response.ProtoReflect().Descriptor().Fields().ByName("enabled")).Bool(); enabled != test.want {
				t.Fatalf("enabled=%t, want %t", enabled, test.want)
			}
		})
	}
	updateMethod := reachService.Methods().ByName("UpdateProvider")
	updated, err := gateway.invoke(ctx, "/stewardmesh.v1.ReachService/UpdateProvider", updateMethod.Output(), &stewardmeshv1.UpdateReachProviderRequest{
		ProviderId: "reach-provider", Name: "Reach provider", EndpointId: "webhook-primary", Enabled: false, Revision: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if endpoint := updated.ProtoReflect().Get(updated.ProtoReflect().Descriptor().Fields().ByName("endpoint_id")).String(); endpoint != "webhook-primary" {
		t.Fatalf("updated Reach endpoint = %q", endpoint)
	}
	if requests != 5 {
		t.Fatalf("HTTP requests = %d, want five", requests)
	}
}

func TestVaultGRPCAcceptsAndDownloadsZeroByteFile(t *testing.T) {
	metadataStore := &grpcMemoryMetadataStore{blobs: make(map[string]storage.Blob)}
	objects, err := storage.NewLocalBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	vault, err := storage.NewService(metadataStore, objects, foundation.NopAuditor{}, storage.ServiceConfig{OrganizationID: "example-org"})
	if err != nil {
		t.Fatal(err)
	}
	handlerCalls := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalls++
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/blobs":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatalf("parse multipart: %v", err)
			}
			file, header, err := r.FormFile("file")
			if err != nil {
				t.Fatalf("read file part: %v", err)
			}
			defer file.Close()
			created, err := vault.CreateBlob(r.Context(), storage.CreateBlobInput{Name: header.Filename, MediaType: header.Header.Get("Content-Type"), Content: file})
			if err != nil {
				t.Fatalf("create blob: %v", err)
			}
			writeTestJSON(w, http.StatusCreated, created)
		default:
			t.Fatalf("unexpected Vault route %s %s", r.Method, r.URL.Path)
		}
	})
	gateway, err := New(handler, Options{AllowedOrigin: "https://stewardmesh.example.test", OrganizationID: "example-org", Guard: &fakeAuthenticator{}, Vault: vault})
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer(grpc.InTapHandle(gateway.TapHandle), grpc.ForceServerCodec(gateway.TransportCodec()))
	if err := gateway.RegisterAll(server); err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	connection, err := grpc.NewClient("passthrough:///zero-byte-vault", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	client := stewardmeshv1.NewVaultServiceClient(connection)
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer opaque-session"))
	created, err := client.CreateBlob(ctx, &stewardmeshv1.CreateBlobRequest{Name: "empty.txt", MediaType: "text/plain", Content: []byte{}})
	if err != nil {
		t.Fatal(err)
	}
	if created.GetSizeBytes() != 0 || created.GetSha256() != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("zero-byte metadata size=%d sha256=%q", created.GetSizeBytes(), created.GetSha256())
	}
	downloaded, err := client.DownloadBlob(ctx, &stewardmeshv1.DownloadBlobRequest{BlobId: created.GetId()})
	if err != nil {
		t.Fatal(err)
	}
	if downloaded.GetBlob().GetId() != created.GetId() || len(downloaded.GetContent()) != 0 {
		t.Fatalf("downloaded=%#v", downloaded)
	}
	if handlerCalls != 1 {
		t.Fatalf("HTTP handler calls=%d, want only the CreateBlob request; direct download authorization and bytes must stay bound to one Vault", handlerCalls)
	}
}

func TestGatewayRejectsMismatchedVaultOrganization(t *testing.T) {
	objects, err := storage.NewLocalBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	vault, err := storage.NewService(&grpcMemoryMetadataStore{blobs: make(map[string]storage.Blob)}, objects, foundation.NopAuditor{}, storage.ServiceConfig{OrganizationID: "other-org"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), Options{
		AllowedOrigin: "https://stewardmesh.example.test", OrganizationID: "example-org", Guard: &fakeAuthenticator{}, Vault: vault,
	})
	if err == nil || err.Error() != "gRPC Vault organization must match the application organization" {
		t.Fatalf("error=%v", err)
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

type fakeAuthenticator struct {
	calls         int
	permissionErr error
}

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

func (f *fakeAuthenticator) CheckPermission(context.Context, guard.Authentication, guard.Permission, guard.Scope) error {
	return f.permissionErr
}

type grpcTestMetadataStore struct{ blob storage.Blob }

func (s *grpcTestMetadataStore) ListBlobs(context.Context, string) ([]storage.Blob, error) {
	return []storage.Blob{s.blob}, nil
}

func (s *grpcTestMetadataStore) GetBlob(_ context.Context, organizationID, id string) (storage.Blob, error) {
	if s.blob.OrganizationID == organizationID && s.blob.ID == id {
		return s.blob, nil
	}
	return storage.Blob{}, storage.ErrNotFound
}

func (*grpcTestMetadataStore) CreateBlob(context.Context, storage.Blob) (storage.Blob, error) {
	return storage.Blob{}, storage.ErrConflict
}

type grpcCancelingObjectStore struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func (*grpcCancelingObjectStore) Provider() string    { return "canceling" }
func (*grpcCancelingObjectStore) MaximumBytes() int64 { return 1024 }
func (*grpcCancelingObjectStore) Put(context.Context, string, string, io.Reader) (storage.StoredObject, error) {
	return storage.StoredObject{}, storage.ErrInvalidInput
}
func (s *grpcCancelingObjectStore) Open(context.Context, string) (io.ReadCloser, error) {
	return &grpcCancelingReader{ctx: s.ctx, cancel: s.cancel}, nil
}
func (*grpcCancelingObjectStore) Delete(context.Context, string) error {
	return storage.ErrInvalidInput
}
func (*grpcCancelingObjectStore) AuthorizeDownload(context.Context, string, string, time.Duration) (storage.ObjectDownloadAuthorization, error) {
	return storage.ObjectDownloadAuthorization{}, storage.ErrInvalidInput
}
func (*grpcCancelingObjectStore) ValidateDownload(context.Context, string, string) error {
	return storage.ErrInvalidInput
}

type grpcCancelingReader struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func (r *grpcCancelingReader) Read([]byte) (int, error) {
	r.cancel()
	return 0, r.ctx.Err()
}

func (*grpcCancelingReader) Close() error { return nil }

type grpcMemoryMetadataStore struct {
	mu    sync.Mutex
	blobs map[string]storage.Blob
}

func (s *grpcMemoryMetadataStore) ListBlobs(_ context.Context, organizationID string) ([]storage.Blob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]storage.Blob, 0, len(s.blobs))
	for _, blob := range s.blobs {
		if blob.OrganizationID == organizationID {
			items = append(items, blob)
		}
	}
	return items, nil
}

func (s *grpcMemoryMetadataStore) GetBlob(_ context.Context, organizationID, id string) (storage.Blob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	blob, ok := s.blobs[organizationID+"\x00"+id]
	if !ok {
		return storage.Blob{}, storage.ErrNotFound
	}
	return blob, nil
}

func (s *grpcMemoryMetadataStore) CreateBlob(_ context.Context, blob storage.Blob) (storage.Blob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := blob.OrganizationID + "\x00" + blob.ID
	if _, exists := s.blobs[key]; exists {
		return storage.Blob{}, storage.ErrConflict
	}
	s.blobs[key] = blob
	return blob, nil
}

func writeTestJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}
