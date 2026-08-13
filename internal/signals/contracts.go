// Package signals implements durable, deduplicated operational and financial alerts.
// Requirement: REQ-SIGNALS-001. Feature: alerts.rules. GitHub: #11.
package signals

import (
	"context"
	"errors"
	"time"
)

const (
	RequirementID = "REQ-SIGNALS-001"
	FeatureID     = "alerts.rules"

	MaximumRules               = 100
	MaximumAlerts              = 500
	MaximumSubscriptions       = 100
	MaximumSubscriptionTargets = 150
	MaximumDeliveryTries       = 8
)

var (
	ErrInvalidInput = errors.New("invalid Signals input")
	ErrNotFound     = errors.New("Signals record not found")
	ErrConflict     = errors.New("Signals record conflicts with existing data")
)

type Condition string

const (
	ConditionOverBudget         Condition = "over_budget"
	ConditionForecastOverBudget Condition = "forecast_over_budget"
	ConditionUnpaid             Condition = "unpaid"
	ConditionOverdue            Condition = "overdue"
	ConditionExpiration         Condition = "expiration"
	ConditionRenewal            Condition = "renewal"
	ConditionUnusedCommitment   Condition = "unused_commitment"
	ConditionReconciliation     Condition = "reconciliation"
)

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

type AlertStatus string

const (
	StatusActive       AlertStatus = "active"
	StatusAcknowledged AlertStatus = "acknowledged"
	StatusResolved     AlertStatus = "resolved"
)

type Rule struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	Name           string    `json:"name"`
	Condition      Condition `json:"condition"`
	Severity       Severity  `json:"severity"`
	Enabled        bool      `json:"enabled"`
	ThresholdDays  []int     `json:"thresholdDays"`
	FiscalPeriod   string    `json:"fiscalPeriod,omitempty"`
	Scenario       string    `json:"scenario,omitempty"`
	CreatedBy      string    `json:"createdBy"`
	Revision       int64     `json:"revision"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type CreateRuleInput struct {
	ID            string    `json:"id,omitempty"`
	Name          string    `json:"name"`
	Condition     Condition `json:"condition"`
	Severity      Severity  `json:"severity"`
	Enabled       *bool     `json:"enabled,omitempty"`
	ThresholdDays []int     `json:"thresholdDays,omitempty"`
	FiscalPeriod  string    `json:"fiscalPeriod,omitempty"`
	Scenario      string    `json:"scenario,omitempty"`
}

type UpdateRuleInput struct {
	Name          string    `json:"name"`
	Condition     Condition `json:"condition"`
	Severity      Severity  `json:"severity"`
	Enabled       bool      `json:"enabled"`
	ThresholdDays []int     `json:"thresholdDays,omitempty"`
	FiscalPeriod  string    `json:"fiscalPeriod,omitempty"`
	Scenario      string    `json:"scenario,omitempty"`
	Revision      int64     `json:"revision"`
}

// Candidate is a provider-neutral condition observation. Evaluators return
// bounded, sanitized summaries; provider payloads and credentials never enter
// Signals persistence or API responses.
type Candidate struct {
	TargetType    string
	TargetID      string
	Title         string
	Summary       string
	DueAt         *time.Time
	ThresholdDays int
}

type Alert struct {
	ID               string      `json:"id"`
	OrganizationID   string      `json:"organizationId"`
	RuleID           string      `json:"ruleId"`
	Condition        Condition   `json:"condition"`
	Severity         Severity    `json:"severity"`
	Status           AlertStatus `json:"status"`
	Title            string      `json:"title"`
	Summary          string      `json:"summary"`
	TargetType       string      `json:"targetType"`
	TargetID         string      `json:"targetId"`
	DueAt            *time.Time  `json:"dueAt,omitempty"`
	ThresholdDays    int         `json:"thresholdDays"`
	DeduplicationKey string      `json:"-"`
	AssignedKind     string      `json:"assignedKind,omitempty"`
	AssignedID       string      `json:"assignedId,omitempty"`
	AcknowledgedBy   string      `json:"acknowledgedBy,omitempty"`
	AcknowledgedAt   *time.Time  `json:"acknowledgedAt,omitempty"`
	FirstDetectedAt  time.Time   `json:"firstDetectedAt"`
	LastObservedAt   time.Time   `json:"lastObservedAt"`
	ResolvedAt       *time.Time  `json:"resolvedAt,omitempty"`
	Revision         int64       `json:"revision"`
}

type AlertQuery struct {
	RuleID    string
	Status    AlertStatus
	Severity  Severity
	Condition Condition
	Limit     int
}

type AlertHistory struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"-"`
	AlertID        string    `json:"alertId"`
	Action         string    `json:"action"`
	ActorID        string    `json:"actorId"`
	OccurredAt     time.Time `json:"occurredAt"`
	Revision       int64     `json:"revision"`
}

type AssignmentInput struct {
	Kind     string `json:"kind"`
	TargetID string `json:"targetId"`
	Revision int64  `json:"revision"`
}

type RevisionInput struct {
	Revision int64 `json:"revision"`
}

type Subscription struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	RuleID         string    `json:"ruleId,omitempty"`
	TargetKind     string    `json:"targetKind"`
	TargetID       string    `json:"targetId"`
	Enabled        bool      `json:"enabled"`
	CreatedBy      string    `json:"createdBy"`
	CreatedAt      time.Time `json:"createdAt"`
}

type CreateSubscriptionInput struct {
	ID         string `json:"id,omitempty"`
	RuleID     string `json:"ruleId,omitempty"`
	TargetKind string `json:"targetKind"`
	TargetID   string `json:"targetId"`
}

// SubscriptionTarget is safe, provider-neutral destination metadata exposed
// to Signals. It contains only an organization-scoped stable reference and a
// human label; network routes, credentials, and provider responses remain in
// Reach.
type SubscriptionTarget struct {
	TargetKind string `json:"targetKind"`
	TargetID   string `json:"targetId"`
	Label      string `json:"label"`
}

// SubscriptionTargetCatalog is the authority for targets that can currently
// receive new Signals subscriptions. Implementations must scope every lookup
// to organizationID and return only enabled, fully configured targets.
type SubscriptionTargetCatalog interface {
	ListSubscriptionTargets(context.Context, string) ([]SubscriptionTarget, error)
}

// Delivery is the durable, provider-neutral Reach handoff. Signals stores only
// configured subscriber references, never webhook URLs, OAuth tokens, or mail
// provider credentials.
type Delivery struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"-"`
	AlertID        string     `json:"alertId"`
	SubscriptionID string     `json:"subscriptionId"`
	TargetKind     string     `json:"targetKind"`
	TargetID       string     `json:"targetId"`
	Status         string     `json:"status"`
	Attempts       int        `json:"attempts"`
	NextAttemptAt  *time.Time `json:"nextAttemptAt,omitempty"`
	LastErrorCode  string     `json:"lastErrorCode,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type EvaluationResult struct {
	AsOf      time.Time `json:"asOf"`
	Rules     int       `json:"rules"`
	Created   int       `json:"created"`
	Refreshed int       `json:"refreshed"`
	Resolved  int       `json:"resolved"`
}

type Evaluator interface {
	Evaluate(context.Context, Rule, time.Time) ([]Candidate, error)
}

type Store interface {
	ListRules(context.Context, string) ([]Rule, error)
	GetRule(context.Context, string, string) (Rule, error)
	CreateRule(context.Context, Rule) (Rule, error)
	UpdateRule(context.Context, Rule, int64) (Rule, error)

	ListAlerts(context.Context, string, AlertQuery) ([]Alert, error)
	GetAlert(context.Context, string, string) (Alert, error)
	GetAlertByDeduplicationKey(context.Context, string, string) (Alert, error)
	CreateAlert(context.Context, Alert, AlertHistory) (Alert, error)
	UpdateAlert(context.Context, Alert, int64, AlertHistory) (Alert, error)
	ListAlertHistory(context.Context, string, string) ([]AlertHistory, error)

	ListSubscriptions(context.Context, string) ([]Subscription, error)
	CreateSubscription(context.Context, Subscription) (Subscription, error)
	DeleteSubscription(context.Context, string, string) (bool, error)

	CreateDelivery(context.Context, Delivery) (Delivery, bool, error)
	ListPendingDeliveries(context.Context, string, time.Time, int) ([]Delivery, error)
	UpdateDelivery(context.Context, Delivery, int) (Delivery, error)
}
