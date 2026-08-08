// Package foundation defines shared operation, ownership, correlation, and
// audit boundaries used by every StewardMesh feature.
// Requirement: REQ-FOUNDATION-001.
package foundation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

const (
	RequirementID = "REQ-FOUNDATION-001"
	FeatureID     = "platform.foundation"
)

// Scope carries the minimum ownership and request identity needed by domain
// operations. ActorID is intentionally optional for trusted startup work.
type Scope struct {
	OrganizationID string
	ActorID        string
	CorrelationID  string
}

func (s Scope) Validate() error {
	if strings.TrimSpace(s.OrganizationID) == "" {
		return errors.New("organization id is required")
	}
	if strings.TrimSpace(s.CorrelationID) == "" {
		return errors.New("correlation id is required")
	}
	return nil
}

type scopeKey struct{}

func WithScope(ctx context.Context, scope Scope) context.Context {
	return context.WithValue(ctx, scopeKey{}, scope)
}

func ScopeFromContext(ctx context.Context) (Scope, bool) {
	scope, ok := ctx.Value(scopeKey{}).(Scope)
	return scope, ok
}

func NewCorrelationID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

// AuditEvent is provider-neutral so PostgreSQL, DynamoDB, and external audit
// adapters can share one contract.
type AuditEvent struct {
	ID             string
	OrganizationID string
	ActorID        string
	CorrelationID  string
	Action         string
	ResourceType   string
	ResourceID     string
	OccurredAt     time.Time
	Metadata       map[string]string
}

type Auditor interface {
	Record(ctx context.Context, event AuditEvent) error
}

type NopAuditor struct{}

func (NopAuditor) Record(context.Context, AuditEvent) error {
	return nil
}
