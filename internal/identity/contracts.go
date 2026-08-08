package identity

import "context"

type Credentials struct {
	Username string
	Password string
}

type Principal struct {
	Subject     string
	Email       string
	DisplayName string
	Roles       []string
}

// Authenticator is the application boundary for local bootstrap and future
// OIDC/OAuth and SAML providers. Credential handling belongs behind this
// interface and must never use plaintext storage.
type Authenticator interface {
	Authenticate(ctx context.Context, credentials Credentials) (Principal, error)
}
