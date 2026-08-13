// Package bridgegrpc provides the authenticated gRPC transport adapter for
// Bridge administration. OAuth redirects/token exchange and MCP retain their
// native HTTP/stdio wire protocols.
// Requirements: REQ-API-001, SEC-MCP-001. Feature: integrations.protocols. GitHub: #14.
package bridgegrpc

import (
	"context"
	"errors"
	"strings"
	"time"

	stewardmeshv1 "github.com/maxlemke/stewardmesh/api/proto"
	"github.com/maxlemke/stewardmesh/internal/bridge"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type authenticationKey struct{}

// Server translates generated protobuf messages into the transport-neutral
// Bridge service. It intentionally owns no authorization truth.
type Server struct {
	stewardmeshv1.UnimplementedBridgeServiceServer
	bridge *bridge.Service
}

func NewServer(service *bridge.Service) (*Server, error) {
	if service == nil {
		return nil, errors.New("Bridge service is required")
	}
	return &Server{bridge: service}, nil
}

// UnaryAuthenticationInterceptor revalidates the opaque Guard session bearer
// from incoming metadata on every RPC. Browser cookies and CSRF values are not
// accepted by this non-browser transport.
func UnaryAuthenticationInterceptor(service *bridge.Service) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if service == nil || info == nil || !strings.HasPrefix(info.FullMethod, "/stewardmesh.v1.BridgeService/") {
			return nil, status.Error(codes.Unimplemented, "RPC is unavailable")
		}
		incoming, ok := metadata.FromIncomingContext(ctx)
		values := incoming.Get("authorization")
		if !ok || len(values) != 1 {
			return nil, status.Error(codes.Unauthenticated, "authentication is required")
		}
		fields := strings.Fields(values[0])
		if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") || len(fields[1]) > 512 {
			return nil, status.Error(codes.Unauthenticated, "authentication is required")
		}
		authentication, err := service.AuthenticateAdministrationSession(ctx, fields[1])
		if err != nil {
			return nil, bridgeStatus(err)
		}
		return handler(context.WithValue(ctx, authenticationKey{}, authentication), request)
	}
}

func (s *Server) ListClients(ctx context.Context, request *stewardmeshv1.ListBridgeClientsRequest) (*stewardmeshv1.ListBridgeClientsResponse, error) {
	authentication, err := authenticated(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	page, err := s.bridge.ListClients(ctx, authentication, bridge.PageRequest{Cursor: request.GetCursor(), Limit: int(request.GetLimit())})
	if err != nil {
		return nil, bridgeStatus(err)
	}
	response := &stewardmeshv1.ListBridgeClientsResponse{Items: make([]*stewardmeshv1.BridgeClient, len(page.Items)), NextCursor: page.NextCursor}
	for index, item := range page.Items {
		response.Items[index] = bridgeClient(item)
	}
	return response, nil
}

func (s *Server) CreateClient(ctx context.Context, request *stewardmeshv1.CreateBridgeClientRequest) (*stewardmeshv1.BridgeClient, error) {
	authentication, err := authenticated(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	scopes := make([]bridge.Scope, len(request.GetAllowedScopes()))
	for index, scope := range request.GetAllowedScopes() {
		scopes[index] = bridgeScope(scope)
	}
	created, err := s.bridge.CreateClient(ctx, authentication, bridge.CreateClientInput{
		Name: request.GetName(), RedirectURIs: append([]string(nil), request.GetRedirectUris()...), AllowedScopes: scopes,
	})
	if err != nil {
		return nil, bridgeStatus(err)
	}
	return bridgeClient(created), nil
}

func (s *Server) RevokeClient(ctx context.Context, request *stewardmeshv1.RevokeBridgeClientRequest) (*stewardmeshv1.BridgeClient, error) {
	authentication, err := authenticated(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	revoked, err := s.bridge.RevokeClient(ctx, authentication, request.GetClientId())
	if err != nil {
		return nil, bridgeStatus(err)
	}
	return bridgeClient(revoked), nil
}

func (s *Server) ListGrants(ctx context.Context, request *stewardmeshv1.ListBridgeGrantsRequest) (*stewardmeshv1.ListBridgeGrantsResponse, error) {
	authentication, err := authenticated(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	page, err := s.bridge.ListGrants(ctx, authentication, bridge.PageRequest{Cursor: request.GetCursor(), Limit: int(request.GetLimit())})
	if err != nil {
		return nil, bridgeStatus(err)
	}
	response := &stewardmeshv1.ListBridgeGrantsResponse{Items: make([]*stewardmeshv1.BridgeGrant, len(page.Items)), NextCursor: page.NextCursor}
	for index, item := range page.Items {
		response.Items[index] = bridgeGrant(item)
	}
	return response, nil
}

func (s *Server) RevokeGrant(ctx context.Context, request *stewardmeshv1.RevokeBridgeGrantRequest) (*stewardmeshv1.BridgeGrant, error) {
	authentication, err := authenticated(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	revoked, err := s.bridge.RevokeGrant(ctx, authentication, request.GetGrantId())
	if err != nil {
		return nil, bridgeStatus(err)
	}
	return bridgeGrant(revoked), nil
}

func authenticated(ctx context.Context) (guard.Authentication, error) {
	authentication, ok := ctx.Value(authenticationKey{}).(guard.Authentication)
	if !ok || authentication.Principal.Subject == "" {
		return guard.Authentication{}, status.Error(codes.Unauthenticated, "authentication is required")
	}
	return authentication, nil
}

func bridgeClient(client bridge.Client) *stewardmeshv1.BridgeClient {
	result := &stewardmeshv1.BridgeClient{
		Id: client.ID, OrganizationId: client.OrganizationID, Name: client.Name,
		RedirectUris: append([]string(nil), client.RedirectURIs...), AllowedScopes: bridgeScopes(client.AllowedScopes),
		CreatedBy: client.CreatedBy, CreatedAt: timestamp(client.CreatedAt),
	}
	if client.RevokedAt != nil {
		result.RevokedAt = timestamp(*client.RevokedAt)
	}
	return result
}

func bridgeGrant(grant bridge.Grant) *stewardmeshv1.BridgeGrant {
	result := &stewardmeshv1.BridgeGrant{
		Id: grant.ID, OrganizationId: grant.OrganizationID, ClientId: grant.ClientID, ClientName: grant.ClientName,
		ActorId: grant.ActorID, ResourceUri: grant.ResourceURI, Scopes: bridgeScopes(grant.Scopes),
		AccessExpiresAt: timestamp(grant.AccessExpiresAt), RefreshExpiresAt: timestamp(grant.RefreshExpiresAt), CreatedAt: timestamp(grant.CreatedAt),
	}
	if grant.LastUsedAt != nil {
		result.LastUsedAt = timestamp(*grant.LastUsedAt)
	}
	if grant.RevokedAt != nil {
		result.RevokedAt = timestamp(*grant.RevokedAt)
	}
	return result
}

func bridgeScopes(scopes []bridge.Scope) []stewardmeshv1.BridgeScope {
	result := make([]stewardmeshv1.BridgeScope, len(scopes))
	for index, scope := range scopes {
		switch scope {
		case bridge.ScopeMCPResources:
			result[index] = stewardmeshv1.BridgeScope_BRIDGE_SCOPE_MCP_RESOURCES
		case bridge.ScopeAssetsRead:
			result[index] = stewardmeshv1.BridgeScope_BRIDGE_SCOPE_ASSETS_READ
		case bridge.ScopeDirectoryRead:
			result[index] = stewardmeshv1.BridgeScope_BRIDGE_SCOPE_DIRECTORY_READ
		case bridge.ScopeSignalsRead:
			result[index] = stewardmeshv1.BridgeScope_BRIDGE_SCOPE_SIGNALS_READ
		case bridge.ScopeSignalsAcknowledge:
			result[index] = stewardmeshv1.BridgeScope_BRIDGE_SCOPE_SIGNALS_ACKNOWLEDGE
		}
	}
	return result
}

func bridgeScope(scope stewardmeshv1.BridgeScope) bridge.Scope {
	switch scope {
	case stewardmeshv1.BridgeScope_BRIDGE_SCOPE_MCP_RESOURCES:
		return bridge.ScopeMCPResources
	case stewardmeshv1.BridgeScope_BRIDGE_SCOPE_ASSETS_READ:
		return bridge.ScopeAssetsRead
	case stewardmeshv1.BridgeScope_BRIDGE_SCOPE_DIRECTORY_READ:
		return bridge.ScopeDirectoryRead
	case stewardmeshv1.BridgeScope_BRIDGE_SCOPE_SIGNALS_READ:
		return bridge.ScopeSignalsRead
	case stewardmeshv1.BridgeScope_BRIDGE_SCOPE_SIGNALS_ACKNOWLEDGE:
		return bridge.ScopeSignalsAcknowledge
	default:
		return ""
	}
}

func timestamp(value time.Time) *timestamppb.Timestamp {
	return timestamppb.New(value.UTC())
}

func bridgeStatus(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request was canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "request deadline exceeded")
	case errors.Is(err, bridge.ErrInvalidInput):
		return status.Error(codes.InvalidArgument, "Bridge input is invalid")
	case errors.Is(err, bridge.ErrUnauthorized):
		return status.Error(codes.Unauthenticated, "Bridge authentication failed")
	case errors.Is(err, bridge.ErrPermissionDenied), errors.Is(err, guard.ErrPermissionDenied):
		return status.Error(codes.PermissionDenied, "Bridge permission is required")
	case errors.Is(err, bridge.ErrNotFound):
		return status.Error(codes.NotFound, "Bridge record was not found")
	case errors.Is(err, bridge.ErrConflict), errors.Is(err, bridge.ErrReplay):
		return status.Error(codes.AlreadyExists, "Bridge operation conflicts with current state")
	case errors.Is(err, bridge.ErrExpired):
		return status.Error(codes.FailedPrecondition, "Bridge credential expired")
	case errors.Is(err, bridge.ErrRateLimited):
		return status.Error(codes.ResourceExhausted, "Bridge rate limit reached")
	default:
		return status.Error(codes.Internal, "Bridge operation could not be completed")
	}
}

var _ stewardmeshv1.BridgeServiceServer = (*Server)(nil)
