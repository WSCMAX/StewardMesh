// Package grpcapi exposes the checked-in protobuf contract through the same
// organization-scoped HTTP application used by REST. The adapter has an
// explicit route for every unary RPC; it does not infer URLs from caller input.
// Requirements: REQ-API-001, REQ-ATLAS-CODES-001, SEC-GUARD-001, SEC-MCP-001.
package grpcapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	stewardmeshv1 "github.com/maxlemke/stewardmesh/api/proto"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/httpapi"
	"github.com/maxlemke/stewardmesh/internal/storage"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/tap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

const (
	localSessionCookie  = "stewardmesh_session"
	secureSessionCookie = "__Host-stewardmesh_session"
	maximumBearerBytes  = 512
	maximumBodyBytes    = MaximumMessageBytes
	unknownPeerAddress  = "192.0.2.1:1"
)

// MaximumMessageBytes is the bounded protobuf receive/send envelope. It leaves
// room for protobuf framing around the 32 MiB Exchange archive contract.
const MaximumMessageBytes = 34 << 20

var (
	pathMarkerPattern    = regexp.MustCompile(`\{([^{}]+)\}`)
	correlationIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
)

type TransportGuard interface {
	httpapi.SessionAuthenticator
	CheckPermission(context.Context, guard.Authentication, guard.Permission, guard.Scope) error
}

type Options struct {
	AllowedOrigin       string
	SessionCookieSecure bool
	OrganizationID      string
	Guard               TransportGuard
	Vault               *storage.Service
}

type Gateway struct {
	handler           http.Handler
	allowedOrigin     string
	sessionCookieName string
	organizationID    string
	guard             TransportGuard
	vault             *storage.Service
	routes            map[string]route
	transportRejected sync.Map
}

type gatewayService interface{ isStewardMeshGateway() }

func (g *Gateway) isStewardMeshGateway() {}

func New(handler http.Handler, options Options) (*Gateway, error) {
	if handler == nil {
		return nil, errors.New("gRPC application handler is required")
	}
	if options.Guard == nil {
		return nil, errors.New("gRPC Guard session authenticator is required")
	}
	options.AllowedOrigin = strings.TrimRight(strings.TrimSpace(options.AllowedOrigin), "/")
	if options.AllowedOrigin == "" {
		return nil, errors.New("gRPC application origin is required")
	}
	parsedOrigin, err := url.Parse(options.AllowedOrigin)
	if err != nil || parsedOrigin.Scheme == "" || parsedOrigin.Host == "" {
		return nil, errors.New("gRPC application origin must be an absolute URL")
	}
	if options.Vault != nil && strings.TrimSpace(options.OrganizationID) != options.Vault.OrganizationID() {
		return nil, errors.New("gRPC Vault organization must match the application organization")
	}
	cookieName := localSessionCookie
	if options.SessionCookieSecure {
		cookieName = secureSessionCookie
	}
	gateway := &Gateway{
		handler: handler, allowedOrigin: options.AllowedOrigin,
		sessionCookieName: cookieName, organizationID: strings.TrimSpace(options.OrganizationID),
		guard: options.Guard, vault: options.Vault, routes: routes(),
	}
	if err := gateway.validateRoutes(); err != nil {
		return nil, err
	}
	return gateway, nil
}

// RegisterAll registers every unary service and RPC in stewardmesh.proto. The
// descriptor-driven registration is validated against an explicit route
// allowlist before the server can start.
func (g *Gateway) RegisterAll(registrar grpc.ServiceRegistrar) error {
	return g.register(registrar, func(route) bool { return true })
}

// RegisterPublic registers only the three unauthenticated Guard entry points.
// Production uses this on the small-envelope listener so public requests never
// reach the authenticated binary allocation boundary.
func (g *Gateway) RegisterPublic(registrar grpc.ServiceRegistrar) error {
	return g.register(registrar, func(configured route) bool { return configured.public })
}

// RegisterProtected registers every authenticated RPC, including Guard session
// and logout operations, on the separately bounded domain listener.
func (g *Gateway) RegisterProtected(registrar grpc.ServiceRegistrar) error {
	return g.register(registrar, func(configured route) bool { return !configured.public })
}

func (g *Gateway) register(registrar grpc.ServiceRegistrar, include func(route) bool) error {
	if g == nil || registrar == nil {
		return errors.New("gRPC gateway and registrar are required")
	}
	if include == nil {
		return errors.New("gRPC route selection is required")
	}
	if err := g.validateRoutes(); err != nil {
		return err
	}
	services := stewardmeshv1.File_stewardmesh_proto.Services()
	for serviceIndex := 0; serviceIndex < services.Len(); serviceIndex++ {
		service := services.Get(serviceIndex)
		description := grpc.ServiceDesc{
			ServiceName: string(service.FullName()),
			HandlerType: (*gatewayService)(nil),
			Metadata:    "stewardmesh.proto",
		}
		for methodIndex := 0; methodIndex < service.Methods().Len(); methodIndex++ {
			method := service.Methods().Get(methodIndex)
			fullMethod := "/" + string(service.FullName()) + "/" + string(method.Name())
			if !include(g.routes[fullMethod]) {
				continue
			}
			description.Methods = append(description.Methods, grpc.MethodDesc{
				MethodName: string(method.Name()),
				Handler:    g.methodHandler(service, method),
			})
		}
		if len(description.Methods) == 0 {
			continue
		}
		registrar.RegisterService(&description, g)
	}
	return nil
}

// TapHandle authenticates every protected RPC before gRPC allocates and
// unmarshals its protobuf request. The returned private transport context is
// reused by invoke, while the HTTP protection layer still performs its normal
// permission and resource-scope checks.
func (g *Gateway) TapHandle(ctx context.Context, information *tap.Info) (context.Context, error) {
	if g == nil || information == nil {
		return ctx, status.Error(codes.Internal, "gRPC authentication is unavailable")
	}
	configured, ok := g.routes[information.FullMethodName]
	if !ok {
		return ctx, status.Error(codes.Unimplemented, "RPC is unavailable")
	}
	if configured.public {
		return ctx, nil
	}
	token, err := bearerToken(ctx)
	if err != nil {
		return ctx, err
	}
	authenticatedContext, _, err := g.authenticateTransport(ctx, token)
	return authenticatedContext, err
}

func (g *Gateway) validateRoutes() error {
	declared := make(map[string]struct{})
	services := stewardmeshv1.File_stewardmesh_proto.Services()
	for serviceIndex := 0; serviceIndex < services.Len(); serviceIndex++ {
		service := services.Get(serviceIndex)
		for methodIndex := 0; methodIndex < service.Methods().Len(); methodIndex++ {
			method := service.Methods().Get(methodIndex)
			fullMethod := "/" + string(service.FullName()) + "/" + string(method.Name())
			declared[fullMethod] = struct{}{}
			configured, ok := g.routes[fullMethod]
			if !ok {
				return fmt.Errorf("gRPC route is missing for %s", fullMethod)
			}
			if configured.method == "" || configured.path == "" || !strings.HasPrefix(configured.path, "/") {
				return fmt.Errorf("gRPC route is invalid for %s", fullMethod)
			}
		}
	}
	for configured := range g.routes {
		if _, ok := declared[configured]; !ok {
			return fmt.Errorf("gRPC route does not name a declared RPC: %s", configured)
		}
	}
	return nil
}

func (g *Gateway) methodHandler(service protoreflect.ServiceDescriptor, method protoreflect.MethodDescriptor) grpc.MethodHandler {
	fullMethod := "/" + string(service.FullName()) + "/" + string(method.Name())
	return func(server any, ctx context.Context, decoder func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
		request := dynamicpb.NewMessage(method.Input())
		if err := decoder(request); err != nil {
			if status.Code(err) == codes.ResourceExhausted {
				return nil, err
			}
			return nil, status.Error(codes.InvalidArgument, "protobuf request is invalid")
		}
		if _, rejected := g.transportRejected.LoadAndDelete(request); rejected {
			return nil, status.Error(codes.ResourceExhausted, "protobuf request exceeds the gRPC transport limit")
		}
		handler := func(callContext context.Context, rawRequest any) (any, error) {
			message, ok := rawRequest.(proto.Message)
			if !ok {
				return nil, status.Error(codes.InvalidArgument, "protobuf request is required")
			}
			return g.invoke(callContext, fullMethod, method.Output(), message)
		}
		if interceptor == nil {
			return handler(ctx, request)
		}
		return interceptor(ctx, request, &grpc.UnaryServerInfo{Server: server, FullMethod: fullMethod}, handler)
	}
}

func (g *Gateway) invoke(ctx context.Context, fullMethod string, output protoreflect.MessageDescriptor, request proto.Message) (proto.Message, error) {
	configured, ok := g.routes[fullMethod]
	if !ok {
		return nil, status.Error(codes.Unimplemented, "RPC is unavailable")
	}
	requestObject, err := messageObject(request)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "protobuf request is invalid")
	}
	if containsDeclaredField(request.ProtoReflect(), "organizationId") {
		return nil, status.Error(codes.InvalidArgument, "organization identity is derived from authentication")
	}
	delete(requestObject, "csrfToken")

	token := ""
	var authentication guard.Authentication
	if !configured.public {
		token, err = bearerToken(ctx)
		if err != nil {
			return nil, err
		}
		ctx, authentication, err = g.authenticateTransport(ctx, token)
		if err != nil {
			return nil, err
		}
		if fullMethod == "/stewardmesh.v1.GuardService/GetSession" {
			return responseMessage(output, transportSession(authentication, token))
		}
	}
	responseContext := retainResponseContext(configured.responseKind, requestObject)

	prepared, err := g.prepareRequest(ctx, configured, requestObject)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	prepared.remoteAddr = peerAddress(ctx)
	if fullMethod == "/stewardmesh.v1.GuardService/AuthenticateLocal" {
		prepared.headers.Set("Origin", g.allowedOrigin)
	}
	if fullMethod == "/stewardmesh.v1.GuardService/BootstrapAdministrator" && loopbackAddress(prepared.remoteAddr) {
		prepared.headers.Set("Origin", g.allowedOrigin)
	}
	if configured.responseKind == responseVaultDownload {
		return g.downloadVault(ctx, output, authentication, prepared, responseContext)
	}

	result, err := g.perform(prepared)
	if err != nil {
		return nil, err
	}
	if result.status < 200 || result.status >= 300 {
		return nil, httpStatusError(result)
	}
	value, err := responseValue(configured.responseKind, result, responseContext)
	if err != nil {
		return nil, status.Error(codes.Internal, "REST response could not be translated")
	}
	if configured.public && (fullMethod == "/stewardmesh.v1.GuardService/BootstrapAdministrator" || fullMethod == "/stewardmesh.v1.GuardService/AuthenticateLocal") {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, status.Error(codes.Internal, "authentication response is invalid")
		}
		sessionToken := cookieValue(result.cookies, g.sessionCookieName)
		if sessionToken == "" {
			return nil, status.Error(codes.Internal, "authentication session was not issued")
		}
		object["sessionToken"] = sessionToken
		// gRPC authenticates with the opaque Bearer session and never accepts
		// browser CSRF. Do not disclose the browser-only token in this transport.
		delete(object, "csrfToken")
	}
	response, err := responseMessage(output, value)
	if err != nil {
		return nil, err
	}
	enrichAuthorizationScopes(response.ProtoReflect(), g.organizationID)
	return response, nil
}

func (g *Gateway) authenticateTransport(ctx context.Context, token string) (context.Context, guard.Authentication, error) {
	if err := ctx.Err(); err != nil {
		return ctx, guard.Authentication{}, contextStatus(err)
	}
	authenticatedContext, authentication, err := httpapi.AuthenticateTransportSession(ctx, g.guard, token)
	if contextErr := ctx.Err(); contextErr != nil {
		return ctx, guard.Authentication{}, contextStatus(contextErr)
	}
	if err == nil && (g.organizationID == "" ||
		(authentication.Session.OrganizationID == g.organizationID && authentication.Principal.OrganizationID == g.organizationID)) {
		return authenticatedContext, authentication, nil
	}
	if err == nil {
		return ctx, guard.Authentication{}, status.Error(codes.Unauthenticated, "authentication is required")
	}
	if errors.Is(err, guard.ErrInvalidSession) || errors.Is(err, guard.ErrNotFound) {
		return ctx, guard.Authentication{}, status.Error(codes.Unauthenticated, "authentication is required")
	}
	return ctx, guard.Authentication{}, status.Error(codes.Unavailable, "authentication is temporarily unavailable")
}

type preparedRequest struct {
	ctx        context.Context
	method     string
	path       string
	headers    http.Header
	body       []byte
	remoteAddr string
}

func (g *Gateway) prepareRequest(ctx context.Context, configured route, object map[string]any) (preparedRequest, error) {
	if err := transformRequestPresence(configured.transform, object); err != nil {
		return preparedRequest{}, err
	}
	path := configured.path
	markers := pathMarkerPattern.FindAllStringSubmatch(path, -1)
	for _, marker := range markers {
		lookup := marker[1]
		if configured.pathFields != nil && configured.pathFields[marker[1]] != "" {
			lookup = configured.pathFields[marker[1]]
		}
		value, ok := nestedValue(object, lookup)
		if !ok || strings.TrimSpace(fmt.Sprint(value)) == "" {
			return preparedRequest{}, fmt.Errorf("%s is required", lookup)
		}
		path = strings.ReplaceAll(path, marker[0], url.PathEscape(fmt.Sprint(value)))
		deleteNested(object, lookup)
	}

	headers := make(http.Header)
	for field, header := range configured.headerFields {
		if value, ok := nestedValue(object, field); ok {
			headers.Set(header, fmt.Sprint(value))
			deleteNested(object, field)
		}
	}
	query := make(url.Values)
	for field, name := range configured.queryFields {
		if value, ok := nestedValue(object, field); ok {
			if err := addQueryValue(query, name, value); err != nil {
				return preparedRequest{}, err
			}
			deleteNested(object, field)
		}
	}
	if configured.method == http.MethodGet || configured.method == http.MethodDelete {
		for name, value := range object {
			if err := addQueryValue(query, name, value); err != nil {
				return preparedRequest{}, err
			}
			delete(object, name)
		}
	}
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	for _, field := range configured.flatten {
		raw, ok := object[field]
		if !ok {
			continue
		}
		nested, ok := raw.(map[string]any)
		if !ok {
			return preparedRequest{}, fmt.Errorf("%s must be an object", field)
		}
		delete(object, field)
		for name, value := range nested {
			if _, exists := object[name]; exists {
				return preparedRequest{}, fmt.Errorf("%s conflicts with %s", field, name)
			}
			object[name] = value
		}
	}

	var body []byte
	switch configured.requestKind {
	case requestRaw:
		raw, ok := object[configured.rawBodyField].([]byte)
		if !ok || len(raw) == 0 {
			return preparedRequest{}, fmt.Errorf("%s is required", configured.rawBodyField)
		}
		body = raw
		headers.Set("Content-Type", configured.contentType)
	case requestMultipart:
		encoded, contentType, err := multipartBody(object)
		if err != nil {
			return preparedRequest{}, err
		}
		body = encoded
		headers.Set("Content-Type", contentType)
	default:
		encoded, err := json.Marshal(object)
		if err != nil {
			return preparedRequest{}, fmt.Errorf("encode request: %w", err)
		}
		body = encoded
		if configured.method != http.MethodGet && configured.method != http.MethodDelete {
			headers.Set("Content-Type", "application/json")
		}
	}
	if len(body) > maximumBodyBytes {
		return preparedRequest{}, fmt.Errorf("request exceeds the gRPC transport limit")
	}
	if correlationID := incomingCorrelationID(ctx); correlationID != "" {
		headers.Set("X-Correlation-ID", correlationID)
	}
	return preparedRequest{ctx: ctx, method: configured.method, path: path, headers: headers, body: body}, nil
}

func incomingCorrelationID(ctx context.Context) string {
	incoming, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	values := incoming.Get("x-correlation-id")
	if len(values) != 1 || !correlationIDPattern.MatchString(values[0]) {
		return ""
	}
	return values[0]
}

func transformRequestPresence(transform requestTransform, object map[string]any) error {
	switch transform {
	case transformNone:
		return nil
	case transformPatternsFieldPresence:
		items, ok := object["fields"].([]any)
		if !ok {
			return errors.New("fields must be an array")
		}
		for _, item := range items {
			field, ok := item.(map[string]any)
			if !ok {
				return errors.New("fields must contain objects")
			}
			if err := applyPresenceFlag(field, "minimum", "hasMinimum"); err != nil {
				return err
			}
			if err := applyPresenceFlag(field, "maximum", "hasMaximum"); err != nil {
				return err
			}
		}
		return nil
	case transformSignalEnabledPresence:
		return applyPresenceFlag(object, "enabled", "hasEnabled")
	case transformAtlasCreateModel:
		return rejectNestedMutationFields(object, "model", "status", "instanceCount", "revision", "createdAt", "updatedAt")
	case transformAtlasUpdateModel:
		return rejectNestedMutationFields(object, "model", "status", "instanceCount", "createdAt", "updatedAt")
	case transformAtlasCreateAsset:
		return rejectNestedMutationFields(object, "asset", "revision", "createdAt", "updatedAt", "modelContext")
	case transformAtlasUpdateAsset:
		return rejectNestedMutationFields(object, "asset", "createdAt", "updatedAt", "modelContext")
	case transformAtlasBulkCreateAssets:
		return rejectMutationListFields(object, "items", "revision", "createdAt", "updatedAt", "modelContext")
	case transformVaultCreateBlob:
		// Proto3 bytes cannot distinguish omission from an explicit empty file.
		// CreateBlob always denotes an upload, so both wire forms mean a present,
		// zero-byte file and match the REST multipart contract.
		if _, ok := object["content"]; !ok {
			object["content"] = []byte{}
		}
		return nil
	default:
		return errors.New("request presence conversion is unavailable")
	}
}

func rejectNestedMutationFields(object map[string]any, container string, fields ...string) error {
	raw, ok := object[container]
	if !ok {
		return nil
	}
	nested, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("%s must be an object", container)
	}
	return rejectMutationFields(nested, container, fields...)
}

func rejectMutationListFields(object map[string]any, container string, fields ...string) error {
	raw, ok := object[container]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return fmt.Errorf("%s must be an array", container)
	}
	for index, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must contain objects", container)
		}
		if err := rejectMutationFields(item, fmt.Sprintf("%s[%d]", container, index), fields...); err != nil {
			return err
		}
	}
	return nil
}

func rejectMutationFields(object map[string]any, prefix string, fields ...string) error {
	for _, field := range fields {
		if _, populated := object[field]; populated {
			return fmt.Errorf("%s.%s is response-only", prefix, field)
		}
	}
	return nil
}

func applyPresenceFlag(object map[string]any, valueField, presenceField string) error {
	present, _ := object[presenceField].(bool)
	_, hasValue := object[valueField]
	delete(object, presenceField)
	if !present {
		if hasValue {
			return fmt.Errorf("%s requires %s", valueField, presenceField)
		}
		delete(object, valueField)
		return nil
	}
	if !hasValue {
		object[valueField] = false
		if valueField == "minimum" || valueField == "maximum" {
			object[valueField] = float64(0)
		}
	}
	return nil
}

func bearerToken(ctx context.Context) (string, error) {
	incoming, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "authentication is required")
	}
	values := incoming.Get("authorization")
	if len(values) != 1 {
		return "", status.Error(codes.Unauthenticated, "authentication is required")
	}
	raw := values[0]
	if len(raw) < len("Bearer ")+1 || len(raw) > len("Bearer ")+maximumBearerBytes ||
		!strings.EqualFold(raw[:len("Bearer")], "Bearer") || raw[len("Bearer")] != ' ' {
		return "", status.Error(codes.Unauthenticated, "authentication is required")
	}
	token := raw[len("Bearer "):]
	if strings.ContainsAny(token, " \t\r\n") {
		return "", status.Error(codes.Unauthenticated, "authentication is required")
	}
	return token, nil
}

func peerAddress(ctx context.Context) string {
	remote, ok := peer.FromContext(ctx)
	if !ok || remote.Addr == nil || strings.TrimSpace(remote.Addr.String()) == "" {
		return unknownPeerAddress
	}
	return remote.Addr.String()
}

func loopbackAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func transportSession(authentication guard.Authentication, token string) map[string]any {
	seen := make(map[guard.Permission]struct{})
	for _, grant := range authentication.Grants {
		if grant.Scope.Kind == guard.ScopeOrganization && grant.Scope.OrganizationID == authentication.Principal.OrganizationID {
			seen[grant.Permission] = struct{}{}
		}
	}
	permissions := make([]string, 0, len(seen))
	for permission := range seen {
		permissions = append(permissions, string(permission))
	}
	sort.Strings(permissions)
	return map[string]any{
		"principal": map[string]any{
			"subject": authentication.Principal.Subject, "organizationId": authentication.Principal.OrganizationID,
			"username": authentication.Principal.Username, "email": authentication.Principal.Email,
			"displayName": authentication.Principal.DisplayName, "roles": stringValues(authentication.Principal.Roles),
		},
		"sessionToken": token,
		"expiresAt":    authentication.Session.ExpiresAt.UTC().Format(time.RFC3339Nano),
		"permissions":  stringValues(permissions),
	}
}

func stringValues(values []string) []any {
	result := make([]any, len(values))
	for index, value := range values {
		result[index] = value
	}
	return result
}

func enrichAuthorizationScopes(message protoreflect.Message, organizationID string) {
	if !message.IsValid() || organizationID == "" || message.Descriptor().FullName() == "google.protobuf.Struct" {
		return
	}
	if message.Descriptor().FullName() == "stewardmesh.v1.AuthorizationScope" {
		field := message.Descriptor().Fields().ByName("organization_id")
		if field != nil && message.Get(field).String() == "" {
			message.Set(field, protoreflect.ValueOfString(organizationID))
		}
		return
	}
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.IsMap() {
			if field.MapValue().Kind() == protoreflect.MessageKind {
				value.Map().Range(func(_ protoreflect.MapKey, item protoreflect.Value) bool {
					enrichAuthorizationScopes(item.Message(), organizationID)
					return true
				})
			}
			return true
		}
		if field.Kind() != protoreflect.MessageKind {
			return true
		}
		if field.IsList() {
			list := value.List()
			for index := 0; index < list.Len(); index++ {
				enrichAuthorizationScopes(list.Get(index).Message(), organizationID)
			}
			return true
		}
		enrichAuthorizationScopes(value.Message(), organizationID)
		return true
	})
}

type httpResult struct {
	status  int
	headers http.Header
	body    []byte
	cookies []*http.Cookie
}

func (g *Gateway) perform(prepared preparedRequest) (httpResult, error) {
	if err := prepared.ctx.Err(); err != nil {
		return httpResult{}, contextStatus(err)
	}
	target := "http://stewardmesh.internal" + prepared.path
	request := httptest.NewRequestWithContext(prepared.ctx, prepared.method, target, bytes.NewReader(prepared.body))
	request.Header = prepared.headers.Clone()
	request.RemoteAddr = prepared.remoteAddr
	if request.RemoteAddr == "" {
		request.RemoteAddr = unknownPeerAddress
	}
	recorder := httptest.NewRecorder()
	if err := serveApplicationHTTP(g.handler, recorder, request); err != nil {
		return httpResult{}, err
	}
	if err := prepared.ctx.Err(); err != nil {
		return httpResult{}, contextStatus(err)
	}
	response := recorder.Result()
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumBodyBytes+1))
	if err != nil {
		if contextErr := prepared.ctx.Err(); contextErr != nil {
			return httpResult{}, contextStatus(contextErr)
		}
		return httpResult{}, status.Error(codes.Internal, "REST response could not be read")
	}
	if err := prepared.ctx.Err(); err != nil {
		return httpResult{}, contextStatus(err)
	}
	if len(body) > maximumBodyBytes {
		return httpResult{}, status.Error(codes.ResourceExhausted, "response exceeds the gRPC transport limit")
	}
	return httpResult{status: response.StatusCode, headers: response.Header.Clone(), body: body, cookies: response.Cookies()}, nil
}

func serveApplicationHTTP(handler http.Handler, writer http.ResponseWriter, request *http.Request) (err error) {
	defer func() {
		if recover() != nil {
			err = status.Error(codes.Internal, "REST application failed")
		}
	}()
	handler.ServeHTTP(writer, request)
	return nil
}

func contextStatus(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request was canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "request deadline exceeded")
	default:
		return status.Error(codes.Internal, "request failed")
	}
}

func httpStatusError(result httpResult) error {
	code := "request_failed"
	message := http.StatusText(result.status)
	if value, err := decodeJSONObject(result.body); err == nil {
		if detail, ok := value["error"].(map[string]any); ok {
			if parsed, ok := detail["code"].(string); ok && parsed != "" {
				code = parsed
			}
			if parsed, ok := detail["message"].(string); ok && parsed != "" {
				message = parsed
			}
		}
	}
	grpcCode := codes.Internal
	switch result.status {
	case http.StatusBadRequest, http.StatusUnsupportedMediaType:
		grpcCode = codes.InvalidArgument
	case http.StatusUnauthorized:
		grpcCode = codes.Unauthenticated
	case http.StatusForbidden:
		grpcCode = codes.PermissionDenied
	case http.StatusNotFound, http.StatusGone:
		grpcCode = codes.NotFound
	case http.StatusConflict:
		grpcCode = codes.Aborted
	case http.StatusUnprocessableEntity, http.StatusLocked:
		grpcCode = codes.FailedPrecondition
	case http.StatusRequestEntityTooLarge, http.StatusTooManyRequests:
		grpcCode = codes.ResourceExhausted
	case http.StatusNotImplemented:
		grpcCode = codes.Unimplemented
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		grpcCode = codes.Unavailable
	case http.StatusGatewayTimeout:
		grpcCode = codes.DeadlineExceeded
	}
	return status.Errorf(grpcCode, "%s: %s", code, message)
}

func responseMessage(descriptor protoreflect.MessageDescriptor, value any) (proto.Message, error) {
	message := dynamicpb.NewMessage(descriptor)
	if err := populateMessage(message.ProtoReflect(), value); err != nil {
		return nil, status.Error(codes.Internal, "REST response could not be translated")
	}
	synthesizeResponsePresence(message.ProtoReflect(), value)
	if proto.Size(message) > MaximumMessageBytes {
		return nil, status.Error(codes.ResourceExhausted, "response exceeds the gRPC transport limit")
	}
	return message, nil
}

func synthesizeResponsePresence(message protoreflect.Message, value any) {
	object, ok := value.(map[string]any)
	if !ok || !message.IsValid() || message.Descriptor().FullName() == "google.protobuf.Struct" {
		return
	}
	if message.Descriptor().FullName() == "stewardmesh.v1.PatternsField" {
		for _, pair := range [][2]string{{"minimum", "has_minimum"}, {"maximum", "has_maximum"}} {
			if raw, exists := object[pair[0]]; exists && raw != nil {
				field := message.Descriptor().Fields().ByName(protoreflect.Name(pair[1]))
				if field != nil {
					message.Set(field, protoreflect.ValueOfBool(true))
				}
			}
		}
	}
	fields := message.Descriptor().Fields()
	for name, raw := range object {
		field := fieldByJSONName(fields, name)
		if field == nil || field.Kind() != protoreflect.MessageKind || !message.Has(field) {
			continue
		}
		if field.IsMap() {
			continue
		}
		if field.IsList() {
			items, ok := raw.([]any)
			if !ok {
				continue
			}
			list := message.Get(field).List()
			for index := 0; index < list.Len() && index < len(items); index++ {
				synthesizeResponsePresence(list.Get(index).Message(), items[index])
			}
			continue
		}
		synthesizeResponsePresence(message.Get(field).Message(), raw)
	}
}

func newDynamicMessage(descriptor protoreflect.MessageDescriptor) protoreflect.Message {
	return dynamicpb.NewMessage(descriptor).ProtoReflect()
}

func decodeJSONObject(body []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func responseValue(kind responseKind, result httpResult, request map[string]any) (any, error) {
	switch kind {
	case responseCSV:
		mediaType := result.headers.Get("Content-Type")
		filename := attachmentFilename(result.headers.Get("Content-Disposition"))
		value := map[string]any{"content": result.body, "mediaType": mediaType, "filename": filename}
		if templateID, ok := request["templateId"].(string); ok {
			value["templateId"] = templateID
			version, _ := int64Value(request["version"])
			if version == 0 {
				version = versionFromFilename(filename)
			}
			value["version"] = version
		} else if templateID, ok := request["sourceTemplateId"].(string); ok {
			value["templateId"] = templateID
		}
		return value, nil
	case responseExchangeExport:
		return map[string]any{
			"packageId": result.headers.Get("X-Exchange-Package-ID"),
			"sha256":    result.headers.Get("X-Content-SHA256"),
			"archive":   result.body,
		}, nil
	case responseAssetLabel:
		templateMetadata, err := base64.RawURLEncoding.DecodeString(result.headers.Get("X-StewardMesh-Label-Template"))
		if err != nil || len(templateMetadata) == 0 {
			return nil, errors.New("label template metadata is invalid")
		}
		labelTemplate, err := decodeJSONObject(templateMetadata)
		if err != nil {
			return nil, errors.New("label template metadata is invalid")
		}
		itemCount, _ := strconv.Atoi(result.headers.Get("X-Label-Item-Count"))
		replay, _ := strconv.ParseBool(result.headers.Get("X-Idempotent-Replay"))
		return map[string]any{
			"batchId": result.headers.Get("X-Label-Batch-ID"), "template": labelTemplate,
			"output": request["output"], "testPrint": request["testPrint"], "itemCount": itemCount,
			"mediaType": result.headers.Get("Content-Type"), "fileName": attachmentFilename(result.headers.Get("Content-Disposition")),
			"content": result.body, "sha256": result.headers.Get("X-Content-SHA256"), "createdAt": result.headers.Get("X-Label-Created-At"),
			"idempotentReplay": replay,
		}, nil
	case responseDeleted:
		return map[string]any{"deleted": true}, nil
	default:
		if len(bytes.TrimSpace(result.body)) == 0 {
			return map[string]any{}, nil
		}
		return decodeJSONObject(result.body)
	}
}

func attachmentFilename(value string) string {
	_, parameters, err := mime.ParseMediaType(value)
	if err != nil {
		return ""
	}
	return parameters["filename"]
}

func versionFromFilename(filename string) int64 {
	marker := strings.LastIndex(filename, "-v")
	if marker < 0 {
		return 0
	}
	value := strings.TrimSuffix(filename[marker+2:], ".csv")
	version, _ := strconv.ParseInt(value, 10, 64)
	return version
}

func cookieValue(cookies []*http.Cookie, name string) string {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie.Value
		}
	}
	return ""
}

func nestedValue(object map[string]any, path string) (any, bool) {
	parts := strings.Split(path, ".")
	var current any = object
	for _, part := range parts {
		nested, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = nested[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func retainResponseContext(kind responseKind, object map[string]any) map[string]any {
	fields := []string{}
	switch kind {
	case responseCSV:
		fields = []string{"templateId", "sourceTemplateId", "version"}
	case responseAssetLabel:
		fields = []string{"output", "testPrint"}
	case responseVaultDownload:
		fields = []string{"blobId"}
	}
	result := make(map[string]any, len(fields))
	for _, field := range fields {
		if value, ok := object[field]; ok {
			result[field] = value
		}
	}
	return result
}

func containsDeclaredField(message protoreflect.Message, jsonName string) bool {
	if !message.IsValid() || message.Descriptor().FullName() == "google.protobuf.Struct" {
		return false
	}
	found := false
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.JSONName() == jsonName {
			found = true
			return false
		}
		// Map keys and values are application data. They cannot set a typed
		// transport identity, even when a user-defined key happens to match it.
		if field.IsMap() || field.Kind() != protoreflect.MessageKind {
			return true
		}
		if field.IsList() {
			list := value.List()
			for index := 0; index < list.Len(); index++ {
				if containsDeclaredField(list.Get(index).Message(), jsonName) {
					found = true
					return false
				}
			}
			return true
		}
		found = containsDeclaredField(value.Message(), jsonName)
		return !found
	})
	return found
}

func deleteNested(object map[string]any, path string) {
	parts := strings.Split(path, ".")
	current := object
	for _, part := range parts[:len(parts)-1] {
		nested, ok := current[part].(map[string]any)
		if !ok {
			return
		}
		current = nested
	}
	delete(current, parts[len(parts)-1])
}

func addQueryValue(query url.Values, name string, value any) error {
	switch item := value.(type) {
	case []any:
		for _, entry := range item {
			if err := addQueryValue(query, name, entry); err != nil {
				return err
			}
		}
	case string:
		query.Add(name, item)
	case bool:
		query.Add(name, strconv.FormatBool(item))
	case json.Number:
		query.Add(name, item.String())
	case int, int32, int64, uint32, uint64, float64:
		query.Add(name, fmt.Sprint(item))
	default:
		return fmt.Errorf("%s cannot be encoded as a query value", name)
	}
	return nil
}

func multipartBody(object map[string]any) ([]byte, string, error) {
	content, ok := object["content"].([]byte)
	if !ok {
		return nil, "", errors.New("content is required")
	}
	name, _ := object["name"].(string)
	mediaType, _ := object["mediaType"].(string)
	if strings.TrimSpace(name) == "" {
		return nil, "", errors.New("name is required")
	}
	if strings.TrimSpace(mediaType) == "" {
		mediaType = "application/octet-stream"
	}
	parsedMediaType, parameters, err := mime.ParseMediaType(mediaType)
	if err != nil || parsedMediaType != mediaType || len(parameters) != 0 || len(mediaType) > 127 || strings.ContainsAny(mediaType, "\r\n") {
		return nil, "", errors.New("mediaType is invalid")
	}
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	header := make(map[string][]string)
	header["Content-Disposition"] = []string{mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": name})}
	header["Content-Type"] = []string{mediaType}
	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(content); err != nil {
		return nil, "", err
	}
	for _, field := range []string{"sourceSystemId", "sourceRecordId", "resourceType", "resourceId"} {
		if value, ok := object[field].(string); ok && value != "" {
			if err := writer.WriteField(field, value); err != nil {
				return nil, "", err
			}
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return buffer.Bytes(), writer.FormDataContentType(), nil
}

func (g *Gateway) downloadVault(ctx context.Context, output protoreflect.MessageDescriptor, authentication guard.Authentication, _ preparedRequest, request map[string]any) (proto.Message, error) {
	if g.vault == nil {
		return nil, status.Error(codes.Unavailable, "vault_unavailable: Vault storage is unavailable")
	}
	blobID, _ := request["blobId"].(string)
	if blobID == "" {
		return nil, status.Error(codes.InvalidArgument, "blobId is required")
	}
	if err := g.guard.CheckPermission(ctx, authentication, guard.PermissionStorageRead, guard.Scope{
		Kind: guard.ScopeOrganization, OrganizationID: g.organizationID, ResourceID: g.organizationID,
	}); err != nil {
		return nil, status.Error(codes.PermissionDenied, "permission_denied: permission is required for this operation")
	}
	correlationID := incomingCorrelationID(ctx)
	if correlationID == "" {
		generatedCorrelationID, err := foundation.NewCorrelationID()
		if err != nil {
			return nil, status.Error(codes.Internal, "request correlation could not be created")
		}
		correlationID = generatedCorrelationID
	}
	ctx = foundation.WithScope(ctx, foundation.Scope{
		OrganizationID: g.organizationID, ActorID: authentication.Principal.Subject, CorrelationID: correlationID,
	})
	blob, content, err := g.vault.AuthorizeAndOpenBlob(ctx, blobID)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, contextStatus(contextErr)
		}
		switch {
		case errors.Is(err, storage.ErrNotFound):
			return nil, status.Error(codes.NotFound, "not_found: the requested Vault file was not found")
		case errors.Is(err, storage.ErrInvalidInput):
			return nil, status.Error(codes.InvalidArgument, "validation_failed: Vault file details are invalid")
		case errors.Is(err, storage.ErrIntegrity):
			return nil, status.Error(codes.DataLoss, "integrity_failed: Vault could not verify the stored file")
		default:
			return nil, status.Error(codes.Internal, "vault_error: the Vault operation could not be completed")
		}
	}
	defer content.Close()
	contents, err := io.ReadAll(io.LimitReader(content, maximumBodyBytes+1))
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, contextStatus(contextErr)
		}
		return nil, status.Error(codes.Internal, "integrity_failed: Vault could not verify the stored file")
	}
	if err := ctx.Err(); err != nil {
		return nil, contextStatus(err)
	}
	if len(contents) > maximumBodyBytes {
		return nil, status.Error(codes.ResourceExhausted, "response exceeds the gRPC transport limit")
	}
	blobJSON, err := json.Marshal(blob)
	if err != nil {
		return nil, status.Error(codes.Internal, "Vault metadata could not be translated")
	}
	metadataValue, err := decodeJSONObject(blobJSON)
	if err != nil {
		return nil, status.Error(codes.Internal, "Vault metadata could not be translated")
	}
	return responseMessage(output, map[string]any{"blob": metadataValue, "content": contents})
}
