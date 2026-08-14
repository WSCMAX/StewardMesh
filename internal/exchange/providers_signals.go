package exchange

// Requirements: REQ-EXCHANGE-001, REQ-SIGNALS-001, REQ-PATTERNS-001.
// Features: migration.packages, alerts.rules, templates.schemas. GitHub: #9, #11.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/signals"
)

var signalsRecordTypes = []string{"signals.rule", "signals.subscription"}

var (
	signalsFiscalPeriodPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,31}$`)
	signalsScenarioPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
)

type SignalsProvider struct {
	service  *signals.Service
	importer signals.ExchangeImporter
}

type signalsRulePayload struct {
	Name          string            `json:"name"`
	Condition     signals.Condition `json:"condition"`
	Severity      signals.Severity  `json:"severity"`
	Enabled       string            `json:"enabled"`
	ThresholdDays string            `json:"thresholdDays"`
	FiscalPeriod  string            `json:"fiscalPeriod,omitempty"`
	Scenario      string            `json:"scenario,omitempty"`
	CreatedAt     string            `json:"createdAt"`
	UpdatedAt     string            `json:"updatedAt"`
}

type signalsSubscriptionPayload struct {
	RuleID     string `json:"ruleId,omitempty"`
	TargetKind string `json:"targetKind"`
	TargetID   string `json:"targetId"`
	Enabled    string `json:"enabled"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

func NewSignalsProvider(service *signals.Service, importer signals.ExchangeImporter) (*SignalsProvider, error) {
	if service == nil || importer == nil || !service.OwnsExchangeImporter(importer) {
		return nil, errors.New("Signals service and its construction-time Exchange importer are required")
	}
	return &SignalsProvider{service: service, importer: importer}, nil
}

func (*SignalsProvider) Types() []string { return append([]string(nil), signalsRecordTypes...) }

func (p *SignalsProvider) ListRecords(ctx context.Context) ([]Record, error) {
	snapshot, err := p.service.ExchangeSnapshot(ctx, MaximumRecords)
	if errors.Is(err, signals.ErrTooLarge) {
		return nil, ErrTooLarge
	}
	if err != nil {
		return nil, err
	}
	result := make([]Record, 0, len(snapshot.Rules)+len(snapshot.Subscriptions))
	for _, rule := range snapshot.Rules {
		if err := validatePortableInstants(1970, rule.CreatedAt, rule.UpdatedAt); err != nil {
			return nil, err
		}
		thresholdDays, err := encodeSignalsThresholds(rule.ThresholdDays)
		if err != nil {
			return nil, err
		}
		payload, err := marshalSignalsPayload(signalsRulePayload{
			Name: rule.Name, Condition: rule.Condition, Severity: rule.Severity, Enabled: strconv.FormatBool(rule.Enabled),
			ThresholdDays: thresholdDays, FiscalPeriod: rule.FiscalPeriod, Scenario: rule.Scenario,
			CreatedAt: signalsInstant(rule.CreatedAt), UpdatedAt: signalsInstant(rule.UpdatedAt),
		})
		if err != nil {
			return nil, err
		}
		result = append(result, Record{Type: "signals.rule", ID: rule.ID, Revision: rule.Revision,
			Dependencies: []Reference{}, Ownership: OwnershipMetadata{State: "local"}, Payload: payload})
	}
	for _, subscription := range snapshot.Subscriptions {
		if err := validatePortableInstants(1970, subscription.CreatedAt, subscription.UpdatedAt); err != nil {
			return nil, err
		}
		payload, err := marshalSignalsPayload(signalsSubscriptionPayload{
			RuleID: subscription.RuleID, TargetKind: subscription.TargetKind, TargetID: subscription.TargetID, Enabled: strconv.FormatBool(subscription.Enabled),
			CreatedAt: signalsInstant(subscription.CreatedAt), UpdatedAt: signalsInstant(subscription.UpdatedAt),
		})
		if err != nil {
			return nil, err
		}
		result = append(result, Record{Type: "signals.subscription", ID: subscription.ID, Revision: subscription.Revision,
			Dependencies: signalsSubscriptionDependencies(subscription), Ownership: OwnershipMetadata{State: "local"}, Payload: payload})
	}
	sort.Slice(result, func(i, j int) bool {
		return (Reference{Type: result[i].Type, ID: result[i].ID}).Key() < (Reference{Type: result[j].Type, ID: result[j].ID}).Key()
	})
	return result, nil
}

func (p *SignalsProvider) Exists(ctx context.Context, reference Reference) (bool, error) {
	var err error
	switch reference.Type {
	case "signals.rule":
		_, err = p.service.ExchangeRule(ctx, reference.ID)
	case "signals.subscription":
		_, err = p.service.ExchangeSubscription(ctx, reference.ID)
	default:
		return false, nil
	}
	if errors.Is(err, signals.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (p *SignalsProvider) ImportRecordExists(ctx context.Context, record Record, _ []byte) (bool, error) {
	candidate, dependencies, err := decodeSignalsRecord(record)
	if err != nil || !slices.Equal(dependencies, record.Dependencies) {
		return false, ErrInvalidInput
	}
	switch item := candidate.(type) {
	case signals.Rule:
		current, err := p.service.ExchangeRule(ctx, item.ID)
		if errors.Is(err, signals.ErrNotFound) {
			return false, nil
		}
		return err == nil && sameSignalsRule(current, item), err
	case signals.Subscription:
		current, err := p.service.ExchangeSubscription(ctx, item.ID)
		if errors.Is(err, signals.ErrNotFound) {
			return false, nil
		}
		return err == nil && sameSignalsSubscription(current, item), err
	default:
		return false, ErrInvalidInput
	}
}

func (p *SignalsProvider) ImportRecord(ctx context.Context, operation ProviderImportOperation, _ string, record Record, _ []byte) (ProviderImportResult, error) {
	if !operation.ExpectedCreated {
		exact, err := p.ImportRecordExists(ctx, record, nil)
		if err != nil {
			return ProviderImportResult{}, err
		}
		if !exact {
			return ProviderImportResult{}, ErrConflict
		}
		return ProviderImportResult{Committed: true}, nil
	}
	candidate, dependencies, err := decodeSignalsRecord(record)
	if err != nil || !slices.Equal(dependencies, record.Dependencies) {
		return ProviderImportResult{}, ErrInvalidInput
	}
	domainOperation := signals.ExchangeImportOperation{Token: operation.Token, OccurredAt: operation.OccurredAt}
	var result signals.ExchangeImportResult
	switch item := candidate.(type) {
	case signals.Rule:
		result, err = p.importer.ImportRule(ctx, domainOperation, item)
	case signals.Subscription:
		result, err = p.importer.ImportSubscription(ctx, domainOperation, item)
	default:
		return ProviderImportResult{}, ErrInvalidInput
	}
	providerResult := ProviderImportResult{Committed: result.Committed, Created: result.Created}
	switch {
	case errors.Is(err, signals.ErrInvalidInput):
		return providerResult, ErrInvalidInput
	case errors.Is(err, signals.ErrConflict):
		return providerResult, ErrConflict
	case errors.Is(err, signals.ErrReferenceMissing), errors.Is(err, signals.ErrNotFound):
		return providerResult, ErrDependencyMissing
	default:
		return providerResult, err
	}
}

func decodeSignalsRecord(record Record) (any, []Reference, error) {
	if record.Revision < 1 || !stableIDPattern.MatchString(record.ID) {
		return nil, nil, ErrInvalidInput
	}
	switch record.Type {
	case "signals.rule":
		payload, err := decodeSignalsPayload[signalsRulePayload](record.Payload)
		if err != nil || !signalsTextRange(payload.Name, 1, 160) || payload.Name != strings.TrimSpace(payload.Name) ||
			string(payload.Condition) != strings.ToLower(strings.TrimSpace(string(payload.Condition))) ||
			string(payload.Severity) != strings.ToLower(strings.TrimSpace(string(payload.Severity))) ||
			payload.FiscalPeriod != strings.TrimSpace(payload.FiscalPeriod) || payload.Scenario != strings.ToLower(strings.TrimSpace(payload.Scenario)) {
			return nil, nil, ErrInvalidInput
		}
		createdAt, updatedAt, err := parseSignalsStateTimes(payload.CreatedAt, payload.UpdatedAt)
		if err != nil {
			return nil, nil, err
		}
		thresholdDays, err := decodeSignalsThresholds(payload.ThresholdDays)
		if err != nil || !validSignalsRulePayload(payload, thresholdDays, record.Revision, createdAt, updatedAt) {
			return nil, nil, ErrInvalidInput
		}
		enabled, _ := strconv.ParseBool(payload.Enabled)
		candidate := signals.Rule{
			ID: record.ID, Name: payload.Name, Condition: payload.Condition, Severity: payload.Severity, Enabled: enabled,
			ThresholdDays: thresholdDays, FiscalPeriod: payload.FiscalPeriod, Scenario: payload.Scenario,
			Revision: record.Revision, CreatedAt: createdAt, UpdatedAt: updatedAt,
		}
		return candidate, []Reference{}, nil
	case "signals.subscription":
		payload, err := decodeSignalsPayload[signalsSubscriptionPayload](record.Payload)
		if err != nil || payload.RuleID != strings.TrimSpace(payload.RuleID) || payload.TargetKind != strings.ToLower(strings.TrimSpace(payload.TargetKind)) ||
			payload.TargetID != strings.TrimSpace(payload.TargetID) {
			return nil, nil, ErrInvalidInput
		}
		createdAt, updatedAt, err := parseSignalsStateTimes(payload.CreatedAt, payload.UpdatedAt)
		if err != nil {
			return nil, nil, err
		}
		if record.Revision == 1 && !createdAt.Equal(updatedAt) {
			return nil, nil, ErrInvalidInput
		}
		enabled, err := strconv.ParseBool(payload.Enabled)
		if err != nil || strconv.FormatBool(enabled) != payload.Enabled {
			return nil, nil, ErrInvalidInput
		}
		candidate := signals.Subscription{
			ID: record.ID, RuleID: payload.RuleID, TargetKind: payload.TargetKind, TargetID: payload.TargetID, Enabled: enabled,
			Revision: record.Revision, CreatedAt: createdAt, UpdatedAt: updatedAt,
		}
		dependencies, err := signalsSubscriptionDependenciesChecked(candidate)
		return candidate, dependencies, err
	default:
		return nil, nil, ErrInvalidInput
	}
}

func decodeSignalsPayload[T any](payload []byte) (T, error) {
	var result T
	if len(payload) == 0 || len(payload) > MaximumPayloadBytes || !utf8.Valid(payload) {
		return result, ErrInvalidInput
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return result, ErrInvalidInput
	}
	if !canonicalJSONEqual(payload, result) {
		return result, ErrInvalidInput
	}
	return result, nil
}

func marshalSignalsPayload(value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil || len(payload) == 0 || len(payload) > MaximumPayloadBytes {
		return nil, ErrInvalidInput
	}
	return payload, nil
}

func signalsSubscriptionDependencies(value signals.Subscription) []Reference {
	result, _ := signalsSubscriptionDependenciesChecked(value)
	return result
}

func signalsSubscriptionDependenciesChecked(value signals.Subscription) ([]Reference, error) {
	dependencies := []Reference{}
	if value.RuleID != "" {
		if !stableIDPattern.MatchString(value.RuleID) {
			return nil, ErrInvalidInput
		}
		dependencies = append(dependencies, Reference{Type: "signals.rule", ID: value.RuleID})
	}
	var targetType string
	switch value.TargetKind {
	case "group":
		targetType = "reach.subscriber-group"
	case "webhook":
		targetType = "reach.provider"
	default:
		return nil, ErrInvalidInput
	}
	if !stableIDPattern.MatchString(value.TargetID) {
		return nil, ErrInvalidInput
	}
	dependencies = append(dependencies, Reference{Type: targetType, ID: value.TargetID})
	return normalizeReferences(dependencies), nil
}

func signalsInstant(value time.Time) string { return value.Format(time.RFC3339Nano) }

func parseSignalsInstant(value string) (time.Time, error) {
	return parsePortableInstant(value, 1970)
}

func parseSignalsStateTimes(created, updated string) (time.Time, time.Time, error) {
	createdAt, err := parseSignalsInstant(created)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	updatedAt, err := parseSignalsInstant(updated)
	if err != nil || updatedAt.Before(createdAt) {
		return time.Time{}, time.Time{}, ErrInvalidInput
	}
	return createdAt, updatedAt, nil
}

func signalsTextRange(value string, minimum, maximum int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) >= minimum && utf8.RuneCountInString(value) <= maximum && !strings.ContainsRune(value, '\x00')
}

func validSignalsRulePayload(payload signalsRulePayload, thresholdDays []int, revision int64, createdAt, updatedAt time.Time) bool {
	if revision == 1 && !createdAt.Equal(updatedAt) {
		return false
	}
	switch payload.Condition {
	case signals.ConditionOverBudget, signals.ConditionForecastOverBudget, signals.ConditionUnpaid, signals.ConditionOverdue,
		signals.ConditionExpiration, signals.ConditionRenewal, signals.ConditionUnusedCommitment, signals.ConditionReconciliation:
	default:
		return false
	}
	if payload.Severity != signals.SeverityInfo && payload.Severity != signals.SeverityWarning && payload.Severity != signals.SeverityCritical {
		return false
	}
	enabled, err := strconv.ParseBool(payload.Enabled)
	if err != nil || strconv.FormatBool(enabled) != payload.Enabled {
		return false
	}
	if payload.FiscalPeriod != "" && !signalsFiscalPeriodPattern.MatchString(payload.FiscalPeriod) ||
		payload.Scenario != "" && !signalsScenarioPattern.MatchString(payload.Scenario) {
		return false
	}
	periodFilter := payload.FiscalPeriod != "" || payload.Scenario != ""
	if periodFilter && payload.Condition != signals.ConditionOverBudget && payload.Condition != signals.ConditionForecastOverBudget &&
		payload.Condition != signals.ConditionUnusedCommitment && payload.Condition != signals.ConditionReconciliation {
		return false
	}
	needsThreshold := payload.Condition == signals.ConditionRenewal || payload.Condition == signals.ConditionExpiration ||
		payload.Condition == signals.ConditionOverdue || payload.Condition == signals.ConditionUnpaid || payload.Condition == signals.ConditionUnusedCommitment
	if len(thresholdDays) > 8 || needsThreshold != (len(thresholdDays) > 0) {
		return false
	}
	previous := 3661
	for _, value := range thresholdDays {
		if value < 0 || value > 3660 || value >= previous {
			return false
		}
		previous = value
	}
	return true
}

func encodeSignalsThresholds(values []int) (string, error) {
	encoded, err := json.Marshal(append([]int{}, values...))
	if err != nil || len(encoded) > 50 {
		return "", ErrInvalidInput
	}
	return string(encoded), nil
}

func decodeSignalsThresholds(value string) ([]int, error) {
	if value != strings.TrimSpace(value) || len(value) < 2 || len(value) > 50 {
		return nil, ErrInvalidInput
	}
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	var result []int
	if err := decoder.Decode(&result); err != nil || result == nil {
		return nil, ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidInput
	}
	canonical, err := encodeSignalsThresholds(result)
	if err != nil || canonical != value {
		return nil, ErrInvalidInput
	}
	return result, nil
}

func sameSignalsRule(left, right signals.Rule) bool {
	return left.ID == right.ID && left.Name == right.Name && left.Condition == right.Condition && left.Severity == right.Severity &&
		left.Enabled == right.Enabled && slices.Equal(left.ThresholdDays, right.ThresholdDays) && left.FiscalPeriod == right.FiscalPeriod &&
		left.Scenario == right.Scenario && left.Revision == right.Revision && left.CreatedAt.Equal(right.CreatedAt) && left.UpdatedAt.Equal(right.UpdatedAt)
}

func sameSignalsSubscription(left, right signals.Subscription) bool {
	return left.ID == right.ID && left.RuleID == right.RuleID && left.TargetKind == right.TargetKind && left.TargetID == right.TargetID &&
		left.Enabled == right.Enabled && left.Revision == right.Revision && left.CreatedAt.Equal(right.CreatedAt) && left.UpdatedAt.Equal(right.UpdatedAt)
}
