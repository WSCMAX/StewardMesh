package httpapi

// Requirements: REQ-API-001, REQ-ATLAS-CODES-001, SEC-GUARD-001.
// Features: integrations.protocols, inventory.identifiers.

import (
	"context"
	"errors"

	"github.com/maxlemke/stewardmesh/internal/guard"
)

type transportAuthenticationKey struct{}

type transportSessionAuthentication struct {
	authentication guard.Authentication
	token          string
}

type SessionAuthenticator interface {
	AuthenticateSession(context.Context, string) (guard.Authentication, error)
}

// AuthenticateTransportSession revalidates a non-browser session bearer and
// places the resulting current Guard grants in a private HTTP context. The
// caller cannot supply a principal, organization, grant, cookie, origin, or
// CSRF value directly. This is intentionally restricted to packages under the
// repository's internal boundary.
func AuthenticateTransportSession(ctx context.Context, service SessionAuthenticator, rawToken string) (context.Context, guard.Authentication, error) {
	if ctx == nil || service == nil {
		return ctx, guard.Authentication{}, errors.New("Guard transport authentication is unavailable")
	}
	if current, ok := ctx.Value(transportAuthenticationKey{}).(transportSessionAuthentication); ok && current.token == rawToken {
		return ctx, current.authentication, nil
	}
	authentication, err := service.AuthenticateSession(ctx, rawToken)
	if err != nil {
		return ctx, guard.Authentication{}, err
	}
	transport := transportSessionAuthentication{authentication: authentication, token: rawToken}
	return context.WithValue(ctx, transportAuthenticationKey{}, transport), authentication, nil
}

func transportAuthentication(ctx context.Context) (guard.Authentication, bool) {
	transport, ok := ctx.Value(transportAuthenticationKey{}).(transportSessionAuthentication)
	authentication := transport.authentication
	return authentication, ok && authentication.Session.ID != "" && authentication.Principal.Subject != ""
}
