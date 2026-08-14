// Package reach implements organization-scoped messaging delivery.
// Requirements: REQ-REACH-001, REQ-EXCHANGE-001. Features: messaging.delivery, migration.packages. GitHub: #9, #12.
package reach

import (
	"context"
	"errors"
	"time"

	"github.com/maxlemke/stewardmesh/internal/signals"
)

const (
	RequirementID = "REQ-REACH-001"
	FeatureID     = "messaging.delivery"

	MaximumGroups       = 100
	MaximumProviders    = 50
	MaximumTemplates    = 100
	MaximumRecipients   = 100
	MaximumMessages     = 500
	MaximumAttempts     = 8
	DefaultMessageLimit = 100
	DefaultClaimTTL     = 2 * time.Minute
)

var (
	ErrInvalidInput        = errors.New("invalid Reach input")
	ErrNotFound            = errors.New("Reach record not found")
	ErrConflict            = errors.New("Reach record conflicts with existing data")
	ErrEndpointUnavailable = errors.New("Reach endpoint is unavailable")
	ErrReferenceMissing    = errors.New("Reach referenced record is missing")
	ErrTooLarge            = errors.New("Reach Exchange snapshot is too large")
)

type ProviderKind string

const (
	ProviderSMTP    ProviderKind = "smtp"
	ProviderSES     ProviderKind = "ses"
	ProviderGmail   ProviderKind = "gmail_oauth"
	ProviderOutlook ProviderKind = "outlook_oauth"
	ProviderTeams   ProviderKind = "teams"
	ProviderWebhook ProviderKind = "webhook"
)

type RecipientKind string

const (
	RecipientEmail   RecipientKind = "email"
	RecipientChannel RecipientKind = "channel"
)

type Recipient struct {
	Kind    RecipientKind `json:"kind"`
	Address string        `json:"address"`
}

// Endpoint is deployment-owned routing configuration. Network locations are
// deliberately excluded from JSON so an API caller can select, but never
// supply or discover, an outbound destination.
type Endpoint struct {
	ID             string       `json:"id"`
	Label          string       `json:"label"`
	Kind           ProviderKind `json:"kind"`
	DestinationKey string       `json:"destinationKey,omitempty"`
	URL            string       `json:"-"`
	TestURL        string       `json:"-"`
	Address        string       `json:"-"`
	ServerName     string       `json:"-"`
	Region         string       `json:"-"`
	RequireTLS     bool         `json:"-"`
	AllowLocalHTTP bool         `json:"-"`
}

type Provider struct {
	ID               string       `json:"id"`
	OrganizationID   string       `json:"organizationId"`
	Name             string       `json:"name"`
	Kind             ProviderKind `json:"kind"`
	EndpointID       string       `json:"endpointId"`
	Sender           string       `json:"sender,omitempty"`
	SecretRef        string       `json:"-"`
	SecretConfigured bool         `json:"secretConfigured"`
	Enabled          bool         `json:"enabled"`
	Revision         int64        `json:"revision"`
	CreatedBy        string       `json:"createdBy"`
	UpdatedBy        string       `json:"updatedBy"`
	CreatedAt        time.Time    `json:"createdAt"`
	UpdatedAt        time.Time    `json:"updatedAt"`
}

type CreateProviderInput struct {
	ID         string       `json:"id,omitempty"`
	Name       string       `json:"name"`
	Kind       ProviderKind `json:"kind"`
	EndpointID string       `json:"endpointId"`
	Sender     string       `json:"sender,omitempty"`
	SecretRef  string       `json:"secretRef"`
	Enabled    *bool        `json:"enabled,omitempty"`
}

type UpdateProviderInput struct {
	Name       string `json:"name"`
	Sender     string `json:"sender,omitempty"`
	EndpointID string `json:"endpointId,omitempty"`
	Enabled    bool   `json:"enabled"`
	Revision   int64  `json:"revision"`
}

type RotateSecretInput struct {
	SecretRef string `json:"secretRef"`
	Revision  int64  `json:"revision"`
	Confirm   bool   `json:"confirm"`
}

type Template struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	Name           string    `json:"name"`
	Subject        string    `json:"subject"`
	Body           string    `json:"body"`
	Revision       int64     `json:"revision"`
	CreatedBy      string    `json:"createdBy"`
	UpdatedBy      string    `json:"updatedBy"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type CreateTemplateInput struct {
	ID      string `json:"id,omitempty"`
	Name    string `json:"name"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

type UpdateTemplateInput struct {
	Name     string `json:"name"`
	Subject  string `json:"subject"`
	Body     string `json:"body"`
	Revision int64  `json:"revision"`
}

type SubscriberGroup struct {
	ID             string      `json:"id"`
	OrganizationID string      `json:"organizationId"`
	Name           string      `json:"name"`
	ProviderID     string      `json:"providerId"`
	TemplateID     string      `json:"templateId"`
	Recipients     []Recipient `json:"recipients"`
	Revision       int64       `json:"revision"`
	CreatedBy      string      `json:"createdBy"`
	UpdatedBy      string      `json:"updatedBy"`
	CreatedAt      time.Time   `json:"createdAt"`
	UpdatedAt      time.Time   `json:"updatedAt"`
}

type CreateGroupInput struct {
	ID         string      `json:"id,omitempty"`
	Name       string      `json:"name"`
	ProviderID string      `json:"providerId"`
	TemplateID string      `json:"templateId"`
	Recipients []Recipient `json:"recipients"`
}

type UpdateGroupInput struct {
	Name       string      `json:"name"`
	ProviderID string      `json:"providerId"`
	TemplateID string      `json:"templateId"`
	Recipients []Recipient `json:"recipients"`
	Revision   int64       `json:"revision"`
}

type Message struct {
	ID             string      `json:"id"`
	OrganizationID string      `json:"organizationId"`
	GroupID        string      `json:"groupId,omitempty"`
	ProviderID     string      `json:"providerId"`
	TemplateID     string      `json:"templateId,omitempty"`
	SourceKind     string      `json:"sourceKind"`
	SourceID       string      `json:"sourceId,omitempty"`
	Subject        string      `json:"subject"`
	Body           string      `json:"body"`
	Recipients     []Recipient `json:"recipients"`
	Status         string      `json:"status"`
	Attempts       int         `json:"attempts"`
	NextAttemptAt  *time.Time  `json:"nextAttemptAt,omitempty"`
	LastErrorCode  string      `json:"lastErrorCode,omitempty"`
	ClaimToken     string      `json:"-"`
	ClaimedAt      *time.Time  `json:"-"`
	CreatedBy      string      `json:"createdBy"`
	CreatedAt      time.Time   `json:"createdAt"`
	UpdatedAt      time.Time   `json:"updatedAt"`
}

type SendInput struct {
	GroupID        string            `json:"groupId"`
	Variables      map[string]string `json:"variables,omitempty"`
	Confirm        bool              `json:"confirm"`
	IdempotencyKey string            `json:"-"`
}

type RetryInput struct {
	Confirm bool `json:"confirm"`
}

type DeliveryAttempt struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"-"`
	MessageID      string     `json:"messageId"`
	Attempt        int        `json:"attempt"`
	Outcome        string     `json:"outcome"`
	ErrorCode      string     `json:"errorCode,omitempty"`
	Retryable      bool       `json:"retryable"`
	NextAttemptAt  *time.Time `json:"nextAttemptAt,omitempty"`
	OccurredAt     time.Time  `json:"occurredAt"`
}

type ProviderTest struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"-"`
	ProviderID     string    `json:"providerId"`
	Outcome        string    `json:"outcome"`
	ErrorCode      string    `json:"errorCode,omitempty"`
	TestedBy       string    `json:"testedBy"`
	TestedAt       time.Time `json:"testedAt"`
}

type TestProviderInput struct {
	Confirm bool `json:"confirm"`
}

type ProcessSignalsInput struct {
	Confirm bool `json:"confirm"`
	Limit   int  `json:"limit,omitempty"`
}

type ProcessResult struct {
	Examined  int `json:"examined"`
	Delivered int `json:"delivered"`
	Retrying  int `json:"retrying"`
	Failed    int `json:"failed"`
}

// ExchangeSnapshot is one bounded, organization-consistent view of Reach's
// portable configuration. Messages, attempts, provider tests, endpoint
// routes, and secret references are deliberately absent.
type ExchangeSnapshot struct {
	Providers []Provider
	Templates []Template
	Groups    []SubscriberGroup
}

// ExchangeImportOperation is the durable, deterministic mutation identity
// reserved by Exchange before it invokes the private importer.
type ExchangeImportOperation struct {
	Token      string
	OccurredAt time.Time
}

type ExchangeImportResult struct {
	Committed bool
	Created   bool
}

// ExchangeImporter is an opaque construction-time capability. It alone may
// preserve source revisions and timestamps while repairing deterministic
// import audits after an ambiguous post-commit failure.
type ExchangeImporter interface {
	ImportProvider(context.Context, ExchangeImportOperation, Provider) (ExchangeImportResult, error)
	ImportTemplate(context.Context, ExchangeImportOperation, Template) (ExchangeImportResult, error)
	ImportGroup(context.Context, ExchangeImportOperation, SubscriberGroup) (ExchangeImportResult, error)
	reachExchangeImporter()
}

// WriteGate fences ordinary writes to imported Reach configuration until a
// Guard ownership claim makes the record local.
type WriteGate interface {
	CheckResourceWrite(context.Context, string, string) error
}

// DeliveryResult is deliberately small and provider-neutral. Transport
// response bodies and exception messages must never cross this boundary.
type DeliveryResult struct {
	Succeeded bool
	Retryable bool
	ErrorCode string
}

type SecretResolver interface {
	Resolve(context.Context, string) ([]byte, error)
}

type Transport interface {
	Test(context.Context, Endpoint, Provider, []byte) DeliveryResult
	Send(context.Context, Endpoint, Provider, Message, []byte) DeliveryResult
}

type SignalSource interface {
	ListPendingDeliveries(context.Context, time.Time, int) ([]signals.Delivery, error)
	GetAlert(context.Context, string) (signals.Alert, error)
	RecordDeliveryAttempt(context.Context, string, bool, bool, string) (signals.Delivery, error)
}

type Store interface {
	ExchangeSnapshot(context.Context, string, int) (ExchangeSnapshot, error)
	ListProviders(context.Context, string) ([]Provider, error)
	GetProvider(context.Context, string, string) (Provider, error)
	CreateProvider(context.Context, Provider) (Provider, error)
	UpdateProvider(context.Context, Provider, int64) (Provider, error)
	ImportProvider(context.Context, Provider) (Provider, bool, error)

	ListTemplates(context.Context, string) ([]Template, error)
	GetTemplate(context.Context, string, string) (Template, error)
	CreateTemplate(context.Context, Template) (Template, error)
	UpdateTemplate(context.Context, Template, int64) (Template, error)
	ImportTemplate(context.Context, Template) (Template, bool, error)

	ListGroups(context.Context, string) ([]SubscriberGroup, error)
	GetGroup(context.Context, string, string) (SubscriberGroup, error)
	CreateGroup(context.Context, SubscriberGroup) (SubscriberGroup, error)
	UpdateGroup(context.Context, SubscriberGroup, int64) (SubscriberGroup, error)
	ImportGroup(context.Context, SubscriberGroup) (SubscriberGroup, bool, error)

	ListMessages(context.Context, string, int) ([]Message, error)
	GetMessage(context.Context, string, string) (Message, error)
	CreateMessage(context.Context, Message) (Message, bool, error)
	ClaimMessage(context.Context, string, string, string, int, string, time.Time, time.Time) (Message, error)
	RecordAttempt(context.Context, Message, int, DeliveryAttempt) (Message, error)
	ListAttempts(context.Context, string, string) ([]DeliveryAttempt, error)

	CreateProviderTest(context.Context, ProviderTest) (ProviderTest, error)
	ListProviderTests(context.Context, string, string) ([]ProviderTest, error)
}
