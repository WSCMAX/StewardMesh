package application

// Requirements: REQ-API-001, REQ-ATLAS-CODES-001, REQ-SIGNALS-001, SEC-GUARD-001.
// Features: integrations.protocols, inventory.identifiers, alerts.rules.

import (
	"bytes"
	"context"
	"net"
	"strings"
	"testing"

	stewardmeshv1 "github.com/maxlemke/stewardmesh/api/proto"
	"github.com/maxlemke/stewardmesh/internal/grpcapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

func TestGRPCRuntimeRegistersAndRoutesEveryAdvertisedRPC(t *testing.T) {
	cfg := memoryConfiguration(t)
	cfg.BootstrapToken = "grpc-bootstrap-token-0123456789abcdef"
	app, err := New(t.Context(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })

	listener := bufconn.Listen(36 << 20)
	gateway, err := grpcapi.New(app.Handler(), grpcapi.Options{
		AllowedOrigin: cfg.AllowedOrigin, SessionCookieSecure: cfg.SessionCookieSecure,
		OrganizationID: app.Organization().ID, Guard: app.Guard(), Vault: app.Vault(),
	})
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(
		grpc.MaxRecvMsgSize(grpcapi.MaximumMessageBytes), grpc.MaxSendMsgSize(grpcapi.MaximumMessageBytes),
		grpc.InTapHandle(gateway.TapHandle), grpc.ForceServerCodec(gateway.TransportCodec()),
	)
	if err := gateway.RegisterAll(server); err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	connection, err := grpc.NewClient("passthrough:///all-services", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	guardClient := stewardmeshv1.NewGuardServiceClient(connection)
	bootstrapStatus, err := guardClient.GetBootstrapStatus(t.Context(), &stewardmeshv1.GetBootstrapStatusRequest{})
	if err != nil || !bootstrapStatus.GetRequired() || !bootstrapStatus.GetTokenRequired() {
		t.Fatalf("bootstrap status %#v err=%v", bootstrapStatus, err)
	}
	session, err := guardClient.BootstrapAdministrator(t.Context(), &stewardmeshv1.BootstrapAdministratorRequest{
		Username: "grpc-runtime-admin", Email: "grpc-runtime-admin@example.test", DisplayName: "gRPC Runtime Administrator",
		Password: "correct horse battery staple", BootstrapToken: cfg.BootstrapToken,
	})
	if err != nil || session.GetSessionToken() == "" || session.GetPrincipal().GetSubject() == "" {
		t.Fatalf("gRPC bootstrap %#v err=%v", session, err)
	}
	authenticated := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("authorization", "Bearer "+session.GetSessionToken()))
	current, err := guardClient.GetSession(authenticated, &stewardmeshv1.GetSessionRequest{})
	if err != nil || current.GetSessionToken() != session.GetSessionToken() || current.GetCsrfToken() != "" || len(current.GetPermissions()) == 0 {
		t.Fatalf("non-browser session %#v err=%v", current, err)
	}

	services := stewardmeshv1.File_stewardmesh_proto.Services()
	called := 0
	for serviceIndex := 0; serviceIndex < services.Len(); serviceIndex++ {
		service := services.Get(serviceIndex)
		for methodIndex := 0; methodIndex < service.Methods().Len(); methodIndex++ {
			method := service.Methods().Get(methodIndex)
			fullMethod := "/" + string(service.FullName()) + "/" + string(method.Name())
			if fullMethod == "/stewardmesh.v1.GuardService/Logout" {
				continue
			}
			callContext := authenticated
			if fullMethod == "/stewardmesh.v1.GuardService/GetBootstrapStatus" ||
				fullMethod == "/stewardmesh.v1.GuardService/BootstrapAdministrator" ||
				fullMethod == "/stewardmesh.v1.GuardService/AuthenticateLocal" {
				callContext = t.Context()
			}
			request := dynamicpb.NewMessage(method.Input())
			populateSmokeRouteFields(request.ProtoReflect(), 0)
			response := dynamicpb.NewMessage(method.Output())
			err := connection.Invoke(callContext, fullMethod, request, response)
			code := status.Code(err)
			if code == codes.Unimplemented || code == codes.Unknown || code == codes.Internal {
				t.Errorf("%s returned code=%s err=%v", fullMethod, code, err)
			}
			if code == codes.NotFound && strings.HasPrefix(status.Convert(err).Message(), "request_failed: Not Found") {
				t.Errorf("%s did not match a registered HTTP route: %v", fullMethod, err)
			}
			called++
		}
	}
	if called != 154 {
		t.Fatalf("called %d RPCs, want 154 before logout", called)
	}
	if _, err := guardClient.Logout(authenticated, &stewardmeshv1.LogoutRequest{}); err != nil {
		t.Fatalf("gRPC logout: %v", err)
	}
	if _, err := guardClient.GetSession(authenticated, &stewardmeshv1.GetSessionRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("logged-out session code=%s err=%v", status.Code(err), err)
	}
}

func TestGRPCRuntimePreservesAtlasVaultPatternsAndExchangeParity(t *testing.T) {
	cfg := memoryConfiguration(t)
	cfg.BootstrapToken = "grpc-binary-token-0123456789abcdefg"
	app, err := New(t.Context(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	listener := bufconn.Listen(36 << 20)
	gateway, err := grpcapi.New(app.Handler(), grpcapi.Options{
		AllowedOrigin: cfg.AllowedOrigin, SessionCookieSecure: cfg.SessionCookieSecure,
		OrganizationID: app.Organization().ID, Guard: app.Guard(), Vault: app.Vault(),
	})
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(
		grpc.MaxRecvMsgSize(grpcapi.MaximumMessageBytes), grpc.MaxSendMsgSize(grpcapi.MaximumMessageBytes),
		grpc.InTapHandle(gateway.TapHandle), grpc.ForceServerCodec(gateway.TransportCodec()),
	)
	if err := gateway.RegisterAll(server); err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	connection, err := grpc.NewClient("passthrough:///domain-parity", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	session, err := stewardmeshv1.NewGuardServiceClient(connection).BootstrapAdministrator(t.Context(), &stewardmeshv1.BootstrapAdministratorRequest{
		Username: "grpc-domain-admin", Email: "grpc-domain-admin@example.test", DisplayName: "gRPC Domain Administrator",
		Password: "correct horse battery staple", BootstrapToken: cfg.BootstrapToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("authorization", "Bearer "+session.GetSessionToken()))

	assetClient := stewardmeshv1.NewAssetServiceClient(connection)
	asset, err := assetClient.CreateAsset(ctx, &stewardmeshv1.CreateAssetRequest{Asset: &stewardmeshv1.Asset{
		Name: "gRPC-labelled laptop", Kind: "laptop", Status: "active", AssetTag: "GRPC-001",
	}})
	if err != nil || asset.GetId() == "" || asset.GetOrganizationId() != cfg.OrganizationID {
		t.Fatalf("create asset %#v err=%v", asset, err)
	}
	association, err := assetClient.CreateAssetIdentifier(ctx, &stewardmeshv1.CreateAssetIdentifierRequest{
		AssetId: asset.GetId(), Symbology: stewardmeshv1.AssetIdentifierSymbology_ASSET_IDENTIFIER_SYMBOLOGY_CODE128,
		Value: "GRPC-CODE-001", DisplayValue: "GRPC-CODE-001", Source: stewardmeshv1.AssetIdentifierSource_ASSET_IDENTIFIER_SOURCE_USER_ENTERED,
		Primary: true,
	})
	if err != nil || association.GetIdentifier().GetId() == "" || !association.GetCreated() {
		t.Fatalf("create identifier %#v err=%v", association, err)
	}
	resolved, err := assetClient.ResolveAssetIdentifier(ctx, &stewardmeshv1.ResolveAssetIdentifierRequest{
		Symbology: stewardmeshv1.AssetIdentifierSymbology_ASSET_IDENTIFIER_SYMBOLOGY_CODE128, Value: "GRPC-CODE-001",
	})
	if err != nil || resolved.GetAssetId() != asset.GetId() {
		t.Fatalf("resolve identifier %#v err=%v", resolved, err)
	}
	templates, err := assetClient.ListAssetLabelTemplates(ctx, &stewardmeshv1.ListAssetLabelTemplatesRequest{})
	if err != nil || len(templates.GetItems()) == 0 {
		t.Fatalf("list label templates %#v err=%v", templates, err)
	}
	var codeTemplate *stewardmeshv1.AssetLabelTemplate
	for _, candidate := range templates.GetItems() {
		if candidate.GetSymbology() == stewardmeshv1.AssetIdentifierSymbology_ASSET_IDENTIFIER_SYMBOLOGY_CODE128 {
			codeTemplate = candidate
			break
		}
	}
	if codeTemplate == nil {
		t.Fatal("Code 128 label template was not exposed over gRPC")
	}
	label, err := assetClient.GenerateAssetLabelBatch(ctx, &stewardmeshv1.GenerateAssetLabelBatchRequest{
		IdempotencyKey: "grpc-label-one", TemplateId: codeTemplate.GetId(), TemplateVersion: codeTemplate.GetVersion(),
		IdentifierIds: []string{association.GetIdentifier().GetId()}, Output: stewardmeshv1.AssetLabelOutput_ASSET_LABEL_OUTPUT_SVG,
	})
	if err != nil || label.GetBatchId() == "" || label.GetTemplate().GetId() != codeTemplate.GetId() || label.GetMediaType() != "image/svg+xml" || !bytes.HasPrefix(label.GetContent(), []byte("<svg")) {
		t.Fatalf("generate label %#v err=%v", label, err)
	}

	patternsClient := stewardmeshv1.NewPatternsServiceClient(connection)
	patternTemplates, err := patternsClient.ListTemplates(ctx, &stewardmeshv1.ListPatternsTemplatesRequest{})
	if err != nil || len(patternTemplates.GetItems()) == 0 {
		t.Fatalf("list Patterns templates %#v err=%v", patternTemplates, err)
	}
	pattern := patternTemplates.GetItems()[0]
	csvTemplate, err := patternsClient.ExportCSVTemplate(ctx, &stewardmeshv1.ExportPatternsCSVTemplateRequest{TemplateId: pattern.GetId(), Version: pattern.GetVersion()})
	if err != nil || csvTemplate.GetTemplateId() != pattern.GetId() || csvTemplate.GetVersion() != pattern.GetVersion() || len(csvTemplate.GetContent()) == 0 || csvTemplate.GetFilename() == "" {
		t.Fatalf("Patterns CSV %#v err=%v", csvTemplate, err)
	}

	vaultClient := stewardmeshv1.NewVaultServiceClient(connection)
	content := []byte("private gRPC Vault content\n")
	blob, err := vaultClient.CreateBlob(ctx, &stewardmeshv1.CreateBlobRequest{Name: "grpc-vault.txt", MediaType: "text/plain", Content: content})
	if err != nil || blob.GetId() == "" || blob.GetSizeBytes() != int64(len(content)) {
		t.Fatalf("create Vault blob %#v err=%v", blob, err)
	}
	downloaded, err := vaultClient.DownloadBlob(ctx, &stewardmeshv1.DownloadBlobRequest{BlobId: blob.GetId()})
	if err != nil || downloaded.GetBlob().GetId() != blob.GetId() || !bytes.Equal(downloaded.GetContent(), content) {
		t.Fatalf("download Vault blob %#v err=%v", downloaded, err)
	}

	exchangeClient := stewardmeshv1.NewExchangeServiceClient(connection)
	records, err := exchangeClient.ListExchangeRecords(ctx, &stewardmeshv1.ListExchangeRecordsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var selection *stewardmeshv1.ExchangeReference
	for _, record := range records.GetItems() {
		if record.GetId() == blob.GetId() {
			selection = &stewardmeshv1.ExchangeReference{Type: record.GetType(), Id: record.GetId()}
			break
		}
	}
	if selection == nil {
		t.Fatal("Vault record was not available to Exchange")
	}
	archive, err := exchangeClient.ExportExchangePackage(ctx, &stewardmeshv1.ExportExchangePackageRequest{Selection: []*stewardmeshv1.ExchangeReference{selection}, FileMode: "include"})
	if err != nil || archive.GetPackageId() == "" || len(archive.GetArchive()) == 0 || archive.GetSha256() == "" {
		t.Fatalf("Exchange export %#v err=%v", archive, err)
	}
	// Importing an archive back into the same organization conflicts with the
	// durable export receipt for that package ID. gRPC preserves that REST
	// conflict instead of accepting bytes as a generic success.
	if _, err := exchangeClient.ImportExchangePackage(ctx, &stewardmeshv1.ImportExchangePackageRequest{Archive: archive.GetArchive()}); status.Code(err) != codes.Aborted {
		t.Fatalf("same-organization Exchange import code=%s err=%v", status.Code(err), err)
	}
}

func populateSmokeRouteFields(message protoreflect.Message, depth int) {
	if !message.IsValid() || depth > 4 || strings.HasPrefix(string(message.Descriptor().FullName()), "google.protobuf.") {
		return
	}
	fields := message.Descriptor().Fields()
	for index := 0; index < fields.Len(); index++ {
		field := fields.Get(index)
		if field.IsList() || field.IsMap() {
			continue
		}
		name := string(field.Name())
		if field.Kind() == protoreflect.StringKind && name != "organization_id" &&
			(name == "id" || strings.HasSuffix(name, "_id") || name == "resource_type" || name == "target_type") {
			value := "smoke-id"
			if name == "resource_type" || name == "target_type" {
				value = "asset"
			}
			message.Set(field, protoreflect.ValueOfString(value))
			continue
		}
		if field.Kind() == protoreflect.MessageKind {
			populateSmokeRouteFields(message.Mutable(field).Message(), depth+1)
		}
	}
}
