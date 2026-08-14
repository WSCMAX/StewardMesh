package reach

// Requirements: REQ-REACH-001, REQ-EXCHANGE-001. Features: messaging.delivery, migration.packages. GitHub: #9, #12.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/portabletime"
	"github.com/maxlemke/stewardmesh/internal/signals"
)

var (
	secretReferencePattern = regexp.MustCompile(`^(?:env|external):[A-Za-z0-9][A-Za-z0-9._-]{0,95}$`)
	templateTokenPattern   = regexp.MustCompile(`\{\{([a-z][a-z0-9_.-]{0,63})\}\}`)
	safeErrorCodePattern   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	allowedTemplateTokens  = map[string]bool{
		"title": true, "summary": true, "severity": true, "record_id": true, "organization": true,
	}
)

type ServiceConfig struct {
	OrganizationID string
	Now            func() time.Time
}

type Service struct {
	store          Store
	endpoints      *EndpointCatalog
	transports     *TransportRegistry
	secrets        SecretResolver
	signals        SignalSource
	writes         WriteGate
	auditor        foundation.Auditor
	organizationID string
	now            func() time.Time
}

func NewService(store Store, endpoints *EndpointCatalog, transports *TransportRegistry, secrets SecretResolver, signalSource SignalSource, auditor foundation.Auditor, configuration ServiceConfig) (*Service, error) {
	service, _, err := NewServiceWithExchangeImporter(store, endpoints, transports, secrets, signalSource, nil, auditor, configuration)
	return service, err
}

func NewServiceWithExchangeImporter(store Store, endpoints *EndpointCatalog, transports *TransportRegistry, secrets SecretResolver, signalSource SignalSource, writes WriteGate, auditor foundation.Auditor, configuration ServiceConfig) (*Service, ExchangeImporter, error) {
	configuration.OrganizationID = strings.TrimSpace(configuration.OrganizationID)
	if store == nil || endpoints == nil || transports == nil || secrets == nil || auditor == nil || configuration.OrganizationID == "" {
		return nil, nil, errors.New("Reach store, endpoints, transports, secrets, auditor, and organization id are required")
	}
	clock := configuration.Now
	if clock == nil {
		clock = time.Now
	}
	service := &Service{store: store, endpoints: endpoints, transports: transports, secrets: secrets, signals: signalSource, writes: writes, auditor: auditor, organizationID: configuration.OrganizationID,
		now: func() time.Time { return portabletime.Normalize(clock()) }}
	return service, &exchangeImporter{service: service}, nil
}

func (s *Service) ListEndpoints() []Endpoint { return s.endpoints.List() }

func (s *Service) ListProviders(ctx context.Context) ([]Provider, error) {
	return s.store.ListProviders(ctx, s.organizationID)
}

func (s *Service) CreateProvider(ctx context.Context, input CreateProviderInput) (Provider, error) {
	input.ID, input.Name, input.EndpointID = strings.TrimSpace(input.ID), strings.TrimSpace(input.Name), strings.TrimSpace(input.EndpointID)
	input.Sender, input.SecretRef = strings.TrimSpace(input.Sender), strings.TrimSpace(input.SecretRef)
	if !optionalID(input.ID) || !validText(input.Name, 1, 160) || !validProviderKind(input.Kind) || !stableIDPattern.MatchString(input.EndpointID) ||
		!secretReferencePattern.MatchString(input.SecretRef) || !validSender(input.Kind, input.Sender) {
		return Provider{}, ErrInvalidInput
	}
	if _, err := s.endpoints.Get(input.EndpointID, input.Kind); err != nil {
		return Provider{}, err
	}
	id, err := idOrNew(input.ID)
	if err != nil {
		return Provider{}, err
	}
	if err := s.checkWrite(ctx, "reach.provider", id); err != nil {
		return Provider{}, err
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	now, actorID := s.now().UTC(), actor(ctx)
	provider := Provider{ID: id, OrganizationID: s.organizationID, Name: input.Name, Kind: input.Kind, EndpointID: input.EndpointID,
		Sender: input.Sender, SecretRef: input.SecretRef, SecretConfigured: true, Enabled: enabled, Revision: 1,
		CreatedBy: actorID, UpdatedBy: actorID, CreatedAt: now, UpdatedAt: now}
	created, err := s.store.CreateProvider(ctx, provider)
	if err != nil {
		return Provider{}, err
	}
	if err := s.audit(ctx, "reach.provider.created", "reach_provider", created.ID, map[string]string{"kind": string(created.Kind), "endpointId": created.EndpointID}); err != nil {
		return Provider{}, fmt.Errorf("audit Reach provider creation: %w", err)
	}
	return created, nil
}

func (s *Service) UpdateProvider(ctx context.Context, id string, input UpdateProviderInput) (Provider, error) {
	id, input.Name, input.Sender = strings.TrimSpace(id), strings.TrimSpace(input.Name), strings.TrimSpace(input.Sender)
	input.EndpointID = strings.TrimSpace(input.EndpointID)
	if !stableIDPattern.MatchString(id) || !validText(input.Name, 1, 160) || input.Revision < 1 {
		return Provider{}, ErrInvalidInput
	}
	if err := s.checkWrite(ctx, "reach.provider", id); err != nil {
		return Provider{}, err
	}
	existing, err := s.store.GetProvider(ctx, s.organizationID, id)
	if err != nil {
		return Provider{}, err
	}
	endpointID := existing.EndpointID
	if input.EndpointID != "" {
		endpointID = input.EndpointID
	}
	if !validSender(existing.Kind, input.Sender) || endpointID != "" && !stableIDPattern.MatchString(endpointID) ||
		input.Enabled && (endpointID == "" || existing.SecretRef == "") {
		return Provider{}, ErrInvalidInput
	}
	if endpointID != "" {
		endpoint, endpointErr := s.endpoints.Get(endpointID, existing.Kind)
		if endpointErr != nil {
			return Provider{}, endpointErr
		}
		groups, listErr := s.store.ListGroups(ctx, s.organizationID)
		if listErr != nil {
			return Provider{}, listErr
		}
		for _, group := range groups {
			if group.ProviderID == existing.ID && !compatibleRecipients(existing.Kind, endpoint, group.Recipients) {
				return Provider{}, ErrInvalidInput
			}
		}
	}
	existing.Name, existing.Sender, existing.EndpointID, existing.Enabled = input.Name, input.Sender, endpointID, input.Enabled
	existing.Revision, existing.UpdatedBy, existing.UpdatedAt = existing.Revision+1, actor(ctx), portabletime.Max(s.now(), existing.UpdatedAt)
	updated, err := s.store.UpdateProvider(ctx, existing, input.Revision)
	if err != nil {
		return Provider{}, err
	}
	if err := s.audit(ctx, "reach.provider.updated", "reach_provider", updated.ID, map[string]string{"kind": string(updated.Kind), "enabled": strconv.FormatBool(updated.Enabled)}); err != nil {
		return Provider{}, fmt.Errorf("audit Reach provider update: %w", err)
	}
	return updated, nil
}

func (s *Service) RotateProviderSecret(ctx context.Context, id string, input RotateSecretInput) (Provider, error) {
	id, input.SecretRef = strings.TrimSpace(id), strings.TrimSpace(input.SecretRef)
	if !input.Confirm || !stableIDPattern.MatchString(id) || !secretReferencePattern.MatchString(input.SecretRef) || input.Revision < 1 {
		return Provider{}, ErrInvalidInput
	}
	if err := s.checkWrite(ctx, "reach.provider", id); err != nil {
		return Provider{}, err
	}
	existing, err := s.store.GetProvider(ctx, s.organizationID, id)
	if err != nil {
		return Provider{}, err
	}
	if existing.EndpointID == "" {
		return Provider{}, ErrConflict
	}
	existing.SecretRef, existing.SecretConfigured = input.SecretRef, true
	existing.Revision, existing.UpdatedBy, existing.UpdatedAt = existing.Revision+1, actor(ctx), portabletime.Max(s.now(), existing.UpdatedAt)
	updated, err := s.store.UpdateProvider(ctx, existing, input.Revision)
	if err != nil {
		return Provider{}, err
	}
	if err := s.audit(ctx, "reach.provider.secret_rotated", "reach_provider", updated.ID, map[string]string{"kind": string(updated.Kind)}); err != nil {
		return Provider{}, fmt.Errorf("audit Reach secret rotation: %w", err)
	}
	return updated, nil
}

func (s *Service) TestProvider(ctx context.Context, id string, input TestProviderInput) (ProviderTest, error) {
	id = strings.TrimSpace(id)
	if !input.Confirm || !stableIDPattern.MatchString(id) {
		return ProviderTest{}, ErrInvalidInput
	}
	if err := s.checkWrite(ctx, "reach.provider", id); err != nil {
		return ProviderTest{}, err
	}
	provider, err := s.store.GetProvider(ctx, s.organizationID, id)
	if err != nil {
		return ProviderTest{}, err
	}
	result := s.executeProviderTest(ctx, provider)
	testID, err := foundation.NewCorrelationID()
	if err != nil {
		return ProviderTest{}, err
	}
	outcome := "succeeded"
	if !result.Succeeded {
		outcome = "failed"
	}
	record := ProviderTest{ID: testID, OrganizationID: s.organizationID, ProviderID: provider.ID, Outcome: outcome,
		ErrorCode: result.ErrorCode, TestedBy: actor(ctx), TestedAt: s.now().UTC()}
	record, err = s.store.CreateProviderTest(ctx, record)
	if err != nil {
		return ProviderTest{}, err
	}
	if err := s.audit(ctx, "reach.provider.tested", "reach_provider", provider.ID, map[string]string{"kind": string(provider.Kind), "outcome": outcome, "errorCode": result.ErrorCode}); err != nil {
		return ProviderTest{}, fmt.Errorf("audit Reach provider test: %w", err)
	}
	return record, nil
}

func (s *Service) ListProviderTests(ctx context.Context, providerID string) ([]ProviderTest, error) {
	providerID = strings.TrimSpace(providerID)
	if !stableIDPattern.MatchString(providerID) {
		return nil, ErrInvalidInput
	}
	return s.store.ListProviderTests(ctx, s.organizationID, providerID)
}

func (s *Service) executeProviderTest(ctx context.Context, provider Provider) DeliveryResult {
	if !provider.Enabled {
		return permanent("provider_disabled")
	}
	endpoint, err := s.endpoints.Get(provider.EndpointID, provider.Kind)
	if err != nil {
		return permanent("endpoint_unavailable")
	}
	transport, err := s.transports.Get(provider.Kind)
	if err != nil {
		return permanent("transport_unavailable")
	}
	secret, err := s.secrets.Resolve(ctx, provider.SecretRef)
	if err != nil {
		return retryable("secret_unavailable")
	}
	defer clear(secret)
	return normalizeResult(transport.Test(ctx, endpoint, provider, secret))
}

func (s *Service) ListTemplates(ctx context.Context) ([]Template, error) {
	return s.store.ListTemplates(ctx, s.organizationID)
}

func (s *Service) CreateTemplate(ctx context.Context, input CreateTemplateInput) (Template, error) {
	input.ID, input.Name, input.Subject, input.Body = strings.TrimSpace(input.ID), strings.TrimSpace(input.Name), strings.TrimSpace(input.Subject), strings.TrimSpace(input.Body)
	if !optionalID(input.ID) || !validText(input.Name, 1, 160) || validateTemplate(input.Subject, input.Body) != nil {
		return Template{}, ErrInvalidInput
	}
	id, err := idOrNew(input.ID)
	if err != nil {
		return Template{}, err
	}
	if err := s.checkWrite(ctx, "reach.template", id); err != nil {
		return Template{}, err
	}
	now, actorID := s.now().UTC(), actor(ctx)
	template := Template{ID: id, OrganizationID: s.organizationID, Name: input.Name, Subject: input.Subject, Body: input.Body,
		Revision: 1, CreatedBy: actorID, UpdatedBy: actorID, CreatedAt: now, UpdatedAt: now}
	created, err := s.store.CreateTemplate(ctx, template)
	if err != nil {
		return Template{}, err
	}
	if err := s.audit(ctx, "reach.template.created", "reach_template", created.ID, nil); err != nil {
		return Template{}, fmt.Errorf("audit Reach template creation: %w", err)
	}
	return created, nil
}

func (s *Service) UpdateTemplate(ctx context.Context, id string, input UpdateTemplateInput) (Template, error) {
	id, input.Name, input.Subject, input.Body = strings.TrimSpace(id), strings.TrimSpace(input.Name), strings.TrimSpace(input.Subject), strings.TrimSpace(input.Body)
	if !stableIDPattern.MatchString(id) || !validText(input.Name, 1, 160) || input.Revision < 1 || validateTemplate(input.Subject, input.Body) != nil {
		return Template{}, ErrInvalidInput
	}
	if err := s.checkWrite(ctx, "reach.template", id); err != nil {
		return Template{}, err
	}
	existing, err := s.store.GetTemplate(ctx, s.organizationID, id)
	if err != nil {
		return Template{}, err
	}
	existing.Name, existing.Subject, existing.Body = input.Name, input.Subject, input.Body
	existing.Revision, existing.UpdatedBy, existing.UpdatedAt = existing.Revision+1, actor(ctx), portabletime.Max(s.now(), existing.UpdatedAt)
	updated, err := s.store.UpdateTemplate(ctx, existing, input.Revision)
	if err != nil {
		return Template{}, err
	}
	if err := s.audit(ctx, "reach.template.updated", "reach_template", updated.ID, nil); err != nil {
		return Template{}, fmt.Errorf("audit Reach template update: %w", err)
	}
	return updated, nil
}

func (s *Service) ListGroups(ctx context.Context) ([]SubscriberGroup, error) {
	return s.store.ListGroups(ctx, s.organizationID)
}

func (s *Service) CreateGroup(ctx context.Context, input CreateGroupInput) (SubscriberGroup, error) {
	input.ID, input.Name, input.ProviderID, input.TemplateID = strings.TrimSpace(input.ID), strings.TrimSpace(input.Name), strings.TrimSpace(input.ProviderID), strings.TrimSpace(input.TemplateID)
	if !optionalID(input.ID) || !validText(input.Name, 1, 160) || !stableIDPattern.MatchString(input.ProviderID) || !stableIDPattern.MatchString(input.TemplateID) {
		return SubscriberGroup{}, ErrInvalidInput
	}
	recipients, err := normalizeRecipients(input.Recipients)
	if err != nil {
		return SubscriberGroup{}, err
	}
	provider, err := s.store.GetProvider(ctx, s.organizationID, input.ProviderID)
	if err != nil {
		return SubscriberGroup{}, err
	}
	if _, err := s.store.GetTemplate(ctx, s.organizationID, input.TemplateID); err != nil {
		return SubscriberGroup{}, err
	}
	endpoint, err := s.endpoints.Get(provider.EndpointID, provider.Kind)
	if err != nil {
		return SubscriberGroup{}, err
	}
	if !compatibleRecipients(provider.Kind, endpoint, recipients) {
		return SubscriberGroup{}, ErrInvalidInput
	}
	id, err := idOrNew(input.ID)
	if err != nil {
		return SubscriberGroup{}, err
	}
	if err := s.checkWrite(ctx, "reach.subscriber-group", id); err != nil {
		return SubscriberGroup{}, err
	}
	now, actorID := s.now().UTC(), actor(ctx)
	group := SubscriberGroup{ID: id, OrganizationID: s.organizationID, Name: input.Name, ProviderID: input.ProviderID, TemplateID: input.TemplateID,
		Recipients: recipients, Revision: 1, CreatedBy: actorID, UpdatedBy: actorID, CreatedAt: now, UpdatedAt: now}
	created, err := s.store.CreateGroup(ctx, group)
	if err != nil {
		return SubscriberGroup{}, err
	}
	if err := s.audit(ctx, "reach.group.created", "reach_subscriber_group", created.ID, map[string]string{"recipientCount": strconv.Itoa(len(created.Recipients))}); err != nil {
		return SubscriberGroup{}, fmt.Errorf("audit Reach group creation: %w", err)
	}
	return created, nil
}

func (s *Service) UpdateGroup(ctx context.Context, id string, input UpdateGroupInput) (SubscriberGroup, error) {
	id, input.Name, input.ProviderID, input.TemplateID = strings.TrimSpace(id), strings.TrimSpace(input.Name), strings.TrimSpace(input.ProviderID), strings.TrimSpace(input.TemplateID)
	if !stableIDPattern.MatchString(id) || !validText(input.Name, 1, 160) || !stableIDPattern.MatchString(input.ProviderID) || !stableIDPattern.MatchString(input.TemplateID) || input.Revision < 1 {
		return SubscriberGroup{}, ErrInvalidInput
	}
	if err := s.checkWrite(ctx, "reach.subscriber-group", id); err != nil {
		return SubscriberGroup{}, err
	}
	recipients, err := normalizeRecipients(input.Recipients)
	if err != nil {
		return SubscriberGroup{}, err
	}
	provider, err := s.store.GetProvider(ctx, s.organizationID, input.ProviderID)
	if err != nil {
		return SubscriberGroup{}, err
	}
	endpoint, endpointErr := s.endpoints.Get(provider.EndpointID, provider.Kind)
	if _, err := s.store.GetTemplate(ctx, s.organizationID, input.TemplateID); err != nil || endpointErr != nil || !compatibleRecipients(provider.Kind, endpoint, recipients) {
		if err != nil {
			return SubscriberGroup{}, err
		}
		if endpointErr != nil {
			return SubscriberGroup{}, endpointErr
		}
		return SubscriberGroup{}, ErrInvalidInput
	}
	existing, err := s.store.GetGroup(ctx, s.organizationID, id)
	if err != nil {
		return SubscriberGroup{}, err
	}
	existing.Name, existing.ProviderID, existing.TemplateID, existing.Recipients = input.Name, input.ProviderID, input.TemplateID, recipients
	existing.Revision, existing.UpdatedBy, existing.UpdatedAt = existing.Revision+1, actor(ctx), portabletime.Max(s.now(), existing.UpdatedAt)
	updated, err := s.store.UpdateGroup(ctx, existing, input.Revision)
	if err != nil {
		return SubscriberGroup{}, err
	}
	if err := s.audit(ctx, "reach.group.updated", "reach_subscriber_group", updated.ID, map[string]string{"recipientCount": strconv.Itoa(len(updated.Recipients))}); err != nil {
		return SubscriberGroup{}, fmt.Errorf("audit Reach group update: %w", err)
	}
	return updated, nil
}

func (s *Service) ListMessages(ctx context.Context, limit int) ([]Message, error) {
	if limit == 0 {
		limit = DefaultMessageLimit
	}
	if limit < 1 || limit > MaximumMessages {
		return nil, ErrInvalidInput
	}
	return s.store.ListMessages(ctx, s.organizationID, limit)
}

func (s *Service) GetMessage(ctx context.Context, id string) (Message, error) {
	id = strings.TrimSpace(id)
	if !stableIDPattern.MatchString(id) {
		return Message{}, ErrInvalidInput
	}
	return s.store.GetMessage(ctx, s.organizationID, id)
}

func (s *Service) ListAttempts(ctx context.Context, messageID string) ([]DeliveryAttempt, error) {
	messageID = strings.TrimSpace(messageID)
	if !stableIDPattern.MatchString(messageID) {
		return nil, ErrInvalidInput
	}
	return s.store.ListAttempts(ctx, s.organizationID, messageID)
}

func (s *Service) Send(ctx context.Context, input SendInput) (Message, error) {
	input.GroupID, input.IdempotencyKey = strings.TrimSpace(input.GroupID), strings.TrimSpace(input.IdempotencyKey)
	if !input.Confirm || !stableIDPattern.MatchString(input.GroupID) || !optionalID(input.IdempotencyKey) {
		return Message{}, ErrInvalidInput
	}
	group, err := s.store.GetGroup(ctx, s.organizationID, input.GroupID)
	if err != nil {
		return Message{}, err
	}
	provider, err := s.store.GetProvider(ctx, s.organizationID, group.ProviderID)
	if err != nil {
		return Message{}, err
	}
	template, err := s.store.GetTemplate(ctx, s.organizationID, group.TemplateID)
	if err != nil {
		return Message{}, err
	}
	variables, err := normalizeVariables(input.Variables)
	if err != nil {
		return Message{}, err
	}
	if _, exists := variables["organization"]; !exists {
		variables["organization"] = s.organizationID
	}
	subject, body, err := renderTemplate(template, variables)
	if err != nil {
		return Message{}, err
	}
	id := ""
	if input.IdempotencyKey != "" {
		digest := sha256.Sum256([]byte(s.organizationID + "\x00manual\x00" + input.IdempotencyKey))
		id = hex.EncodeToString(digest[:16])
	} else if id, err = foundation.NewCorrelationID(); err != nil {
		return Message{}, err
	}
	now := s.now().UTC()
	message := Message{ID: id, OrganizationID: s.organizationID, GroupID: group.ID, ProviderID: provider.ID, TemplateID: template.ID,
		SourceKind: "manual", Subject: subject, Body: body, Recipients: append([]Recipient(nil), group.Recipients...), Status: "queued",
		CreatedBy: actor(ctx), CreatedAt: now, UpdatedAt: now}
	created, wasCreated, err := s.store.CreateMessage(ctx, message)
	if err != nil {
		return Message{}, err
	}
	if !wasCreated {
		return created, nil
	}
	if err := s.audit(ctx, "reach.message.queued", "reach_message", created.ID, map[string]string{"providerKind": string(provider.Kind), "recipientCount": strconv.Itoa(len(created.Recipients))}); err != nil {
		return Message{}, fmt.Errorf("audit Reach message queue: %w", err)
	}
	return s.dispatch(ctx, created)
}

func (s *Service) Retry(ctx context.Context, id string, input RetryInput) (Message, error) {
	message, err := s.GetMessage(ctx, id)
	if err != nil {
		return Message{}, err
	}
	if !input.Confirm || message.Status == "delivered" || message.Attempts >= MaximumAttempts {
		return Message{}, ErrConflict
	}
	if message.ClaimedAt != nil && message.ClaimedAt.After(s.now().UTC().Add(-DefaultClaimTTL)) {
		return Message{}, ErrConflict
	}
	if err := s.audit(ctx, "reach.message.retry_requested", "reach_message", message.ID, map[string]string{"attempts": strconv.Itoa(message.Attempts)}); err != nil {
		return Message{}, fmt.Errorf("audit Reach retry: %w", err)
	}
	return s.dispatch(ctx, message)
}

func (s *Service) dispatch(ctx context.Context, message Message) (Message, error) {
	recoveringUnknownOutcome := message.ClaimedAt != nil && !message.ClaimedAt.After(s.now().UTC().Add(-DefaultClaimTTL))
	claimToken, err := foundation.NewCorrelationID()
	if err != nil {
		return Message{}, err
	}
	now := s.now().UTC()
	claimed, err := s.store.ClaimMessage(ctx, s.organizationID, message.ID, message.Status, message.Attempts, claimToken, now, now.Add(-DefaultClaimTTL))
	if err != nil {
		return Message{}, err
	}
	message = claimed
	provider, err := s.store.GetProvider(ctx, s.organizationID, message.ProviderID)
	if err != nil {
		return Message{}, err
	}
	result := DeliveryResult{}
	switch {
	case recoveringUnknownOutcome:
		// A prior worker may have completed the external side effect and crashed
		// before the durable result. Never resend automatically: preserve the
		// ambiguity for an operator-confirmed Retry instead of risking a duplicate.
		result = permanent("delivery_outcome_unknown")
	case !provider.Enabled:
		result = permanent("provider_disabled")
	default:
		endpoint, endpointErr := s.endpoints.Get(provider.EndpointID, provider.Kind)
		transport, transportErr := s.transports.Get(provider.Kind)
		if endpointErr != nil || transportErr != nil {
			result = permanent("endpoint_unavailable")
		} else if !compatibleRecipientsForMessage(provider.Kind, endpoint, message.Recipients) {
			result = permanent("recipient_invalid")
		} else {
			secret, secretErr := s.secrets.Resolve(ctx, provider.SecretRef)
			if secretErr != nil {
				result = retryable("secret_unavailable")
			} else {
				result = normalizeResult(transport.Send(ctx, endpoint, provider, message, secret))
				clear(secret)
			}
		}
	}
	expectedAttempts := message.Attempts
	message.Attempts++
	message.UpdatedAt, message.LastErrorCode, message.NextAttemptAt = s.now().UTC(), result.ErrorCode, nil
	status, outcome := "failed", "failed"
	if result.Succeeded {
		status, outcome, message.LastErrorCode = "delivered", "succeeded", ""
	} else if result.Retryable && message.Attempts < MaximumAttempts {
		status, outcome = "retrying", "retrying"
		next := message.UpdatedAt.Add(retryDelay(message.Attempts))
		message.NextAttemptAt = &next
	}
	message.Status = status
	attemptID := deterministicAttemptID(message.ID, message.Attempts)
	attempt := DeliveryAttempt{ID: attemptID, OrganizationID: s.organizationID, MessageID: message.ID, Attempt: message.Attempts,
		Outcome: outcome, ErrorCode: message.LastErrorCode, Retryable: outcome == "retrying", NextAttemptAt: message.NextAttemptAt, OccurredAt: message.UpdatedAt}
	updated, err := s.store.RecordAttempt(ctx, message, expectedAttempts, attempt)
	if err != nil {
		return Message{}, err
	}
	if err := s.audit(ctx, "reach.message.attempted", "reach_message", updated.ID, map[string]string{"outcome": outcome, "errorCode": updated.LastErrorCode, "attempt": strconv.Itoa(updated.Attempts)}); err != nil {
		return Message{}, fmt.Errorf("audit Reach delivery attempt: %w", err)
	}
	return updated, nil
}

func (s *Service) ProcessSignals(ctx context.Context, input ProcessSignalsInput) (ProcessResult, error) {
	if s.signals == nil || !input.Confirm {
		return ProcessResult{}, ErrInvalidInput
	}
	if input.Limit == 0 {
		input.Limit = DefaultMessageLimit
	}
	if input.Limit < 1 || input.Limit > MaximumMessages {
		return ProcessResult{}, ErrInvalidInput
	}
	pending, err := s.signals.ListPendingDeliveries(ctx, s.now().UTC(), input.Limit)
	if err != nil {
		return ProcessResult{}, err
	}
	result := ProcessResult{}
	for _, delivery := range pending {
		result.Examined++
		message, deliveryResult, err := s.processSignal(ctx, delivery)
		if err != nil {
			return result, err
		}
		_, err = s.signals.RecordDeliveryAttempt(ctx, delivery.ID, deliveryResult.Succeeded, deliveryResult.Retryable, deliveryResult.ErrorCode)
		if err != nil {
			return result, err
		}
		switch message.Status {
		case "delivered":
			result.Delivered++
		case "retrying":
			result.Retrying++
		default:
			result.Failed++
		}
	}
	if err := s.audit(ctx, "reach.signals.processed", "reach_signal_batch", s.now().UTC().Format("20060102T150405Z"), map[string]string{
		"examined": strconv.Itoa(result.Examined), "delivered": strconv.Itoa(result.Delivered), "retrying": strconv.Itoa(result.Retrying), "failed": strconv.Itoa(result.Failed),
	}); err != nil {
		return result, fmt.Errorf("audit Reach Signals processing: %w", err)
	}
	return result, nil
}

func (s *Service) processSignal(ctx context.Context, delivery signals.Delivery) (Message, DeliveryResult, error) {
	digest := sha256.Sum256([]byte(s.organizationID + "\x00signals\x00" + delivery.ID))
	messageID := hex.EncodeToString(digest[:16])
	existing, err := s.store.GetMessage(ctx, s.organizationID, messageID)
	if err == nil {
		switch existing.Status {
		case "delivered":
			return existing, DeliveryResult{Succeeded: true}, nil
		case "failed":
			return existing, permanent(nonempty(existing.LastErrorCode, "delivery_failed")), nil
		}
		// Signals and Reach persist independently. If Reach recorded a retry but
		// Signals did not receive the result, report the existing outcome before
		// its due time instead of making a duplicate external call.
		if existing.NextAttemptAt != nil && existing.NextAttemptAt.After(s.now().UTC()) {
			return existing, resultFromMessage(existing), nil
		}
		updated, dispatchErr := s.dispatch(ctx, existing)
		return updated, resultFromMessage(updated), dispatchErr
	}
	if !errors.Is(err, ErrNotFound) {
		return Message{}, DeliveryResult{}, err
	}
	alert, err := s.signals.GetAlert(ctx, delivery.AlertID)
	if err != nil {
		return Message{}, DeliveryResult{}, err
	}
	message := Message{ID: messageID, OrganizationID: s.organizationID, SourceKind: "signals", SourceID: delivery.ID,
		Subject: alert.Title, Body: alert.Summary, Recipients: []Recipient{}, Status: "queued", CreatedBy: actor(ctx), CreatedAt: s.now().UTC(), UpdatedAt: s.now().UTC()}
	if delivery.TargetKind == "group" {
		group, groupErr := s.store.GetGroup(ctx, s.organizationID, delivery.TargetID)
		if groupErr != nil {
			return s.recordUndeliverableSignal(ctx, message, "target_not_found")
		}
		provider, providerErr := s.store.GetProvider(ctx, s.organizationID, group.ProviderID)
		template, templateErr := s.store.GetTemplate(ctx, s.organizationID, group.TemplateID)
		if providerErr != nil || templateErr != nil {
			return s.recordUndeliverableSignal(ctx, message, "target_not_found")
		}
		variables := map[string]string{"title": alert.Title, "summary": alert.Summary, "severity": string(alert.Severity), "record_id": alert.ID, "organization": s.organizationID}
		subject, body, renderErr := renderTemplate(template, variables)
		if renderErr != nil {
			return s.recordUndeliverableSignal(ctx, message, "template_invalid")
		}
		message.GroupID, message.ProviderID, message.TemplateID, message.Recipients = group.ID, provider.ID, template.ID, append([]Recipient(nil), group.Recipients...)
		message.Subject, message.Body = subject, body
	} else if delivery.TargetKind == "webhook" {
		provider, providerErr := s.store.GetProvider(ctx, s.organizationID, delivery.TargetID)
		if providerErr != nil || provider.Kind != ProviderWebhook {
			return s.recordUndeliverableSignal(ctx, message, "target_not_found")
		}
		message.ProviderID = provider.ID
	} else {
		return s.recordUndeliverableSignal(ctx, message, "target_invalid")
	}
	created, _, err := s.store.CreateMessage(ctx, message)
	if err != nil {
		return Message{}, DeliveryResult{}, err
	}
	updated, err := s.dispatch(ctx, created)
	return updated, resultFromMessage(updated), err
}

func (s *Service) recordUndeliverableSignal(ctx context.Context, message Message, code string) (Message, DeliveryResult, error) {
	message.ProviderID = "unresolved"
	created, wasCreated, err := s.store.CreateMessage(ctx, message)
	if err != nil {
		return Message{}, DeliveryResult{}, err
	}
	if !wasCreated {
		return created, resultFromMessage(created), nil
	}
	claimToken, err := foundation.NewCorrelationID()
	if err != nil {
		return Message{}, DeliveryResult{}, err
	}
	created, err = s.store.ClaimMessage(ctx, s.organizationID, created.ID, created.Status, created.Attempts, claimToken,
		s.now().UTC(), s.now().UTC().Add(-DefaultClaimTTL))
	if err != nil {
		return Message{}, DeliveryResult{}, err
	}
	created.Attempts, created.Status, created.LastErrorCode, created.UpdatedAt = 1, "failed", code, s.now().UTC()
	attempt := DeliveryAttempt{ID: deterministicAttemptID(created.ID, 1), OrganizationID: s.organizationID, MessageID: created.ID,
		Attempt: 1, Outcome: "failed", ErrorCode: code, OccurredAt: created.UpdatedAt}
	updated, err := s.store.RecordAttempt(ctx, created, 0, attempt)
	return updated, permanent(code), err
}

func resultFromMessage(message Message) DeliveryResult {
	switch message.Status {
	case "delivered":
		return DeliveryResult{Succeeded: true}
	case "retrying":
		return retryable(nonempty(message.LastErrorCode, "provider_unavailable"))
	default:
		return permanent(nonempty(message.LastErrorCode, "delivery_failed"))
	}
}

func normalizeResult(result DeliveryResult) DeliveryResult {
	result.ErrorCode = strings.ToLower(strings.TrimSpace(result.ErrorCode))
	if result.Succeeded {
		return DeliveryResult{Succeeded: true}
	}
	if !safeErrorCodePattern.MatchString(result.ErrorCode) {
		return permanent("provider_failure")
	}
	return result
}

func validateTemplate(subject, body string) error {
	if !validPlainText(subject, 1, 200, false) || !validPlainText(body, 1, 4000, true) || strings.Contains(subject, "\n") {
		return ErrInvalidInput
	}
	for _, value := range []string{subject, body} {
		matches := templateTokenPattern.FindAllStringSubmatch(value, -1)
		for _, match := range matches {
			if !allowedTemplateTokens[match[1]] {
				return ErrInvalidInput
			}
		}
		without := templateTokenPattern.ReplaceAllString(value, "")
		if strings.Contains(without, "{{") || strings.Contains(without, "}}") {
			return ErrInvalidInput
		}
	}
	return nil
}

func renderTemplate(template Template, variables map[string]string) (string, string, error) {
	if err := validateTemplate(template.Subject, template.Body); err != nil {
		return "", "", err
	}
	render := func(value string) (string, error) {
		missing := false
		result := templateTokenPattern.ReplaceAllStringFunc(value, func(token string) string {
			match := templateTokenPattern.FindStringSubmatch(token)
			replacement, ok := variables[match[1]]
			if !ok {
				missing = true
				return ""
			}
			return replacement
		})
		if missing || !validPlainText(result, 1, 4000, true) {
			return "", ErrInvalidInput
		}
		return result, nil
	}
	subject, err := render(template.Subject)
	if err != nil || strings.Contains(subject, "\n") || len(subject) > 200 {
		return "", "", ErrInvalidInput
	}
	body, err := render(template.Body)
	if err != nil {
		return "", "", err
	}
	return subject, body, nil
}

func normalizeVariables(input map[string]string) (map[string]string, error) {
	if len(input) > len(allowedTemplateTokens) {
		return nil, ErrInvalidInput
	}
	result := make(map[string]string, len(input))
	for key, value := range input {
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !allowedTemplateTokens[key] || !validPlainText(value, 1, 500, true) {
			return nil, ErrInvalidInput
		}
		result[key] = value
	}
	return result, nil
}

func normalizeRecipients(input []Recipient) ([]Recipient, error) {
	if len(input) < 1 || len(input) > MaximumRecipients {
		return nil, ErrInvalidInput
	}
	result, seen := make([]Recipient, 0, len(input)), map[string]bool{}
	for _, recipient := range input {
		recipient.Address = strings.TrimSpace(recipient.Address)
		switch recipient.Kind {
		case RecipientEmail:
			parsed, err := mail.ParseAddress(recipient.Address)
			if err != nil || parsed.Address != recipient.Address || len(recipient.Address) > 320 || strings.ContainsAny(recipient.Address, "\r\n") {
				return nil, ErrInvalidInput
			}
			recipient.Address = strings.ToLower(recipient.Address)
		case RecipientChannel:
			if !stableIDPattern.MatchString(recipient.Address) {
				return nil, ErrInvalidInput
			}
		default:
			return nil, ErrInvalidInput
		}
		key := string(recipient.Kind) + "\x00" + strings.ToLower(recipient.Address)
		if seen[key] {
			return nil, ErrConflict
		}
		seen[key] = true
		result = append(result, recipient)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind == result[j].Kind {
			return result[i].Address < result[j].Address
		}
		return result[i].Kind < result[j].Kind
	})
	return result, nil
}

func compatibleRecipients(kind ProviderKind, endpoint Endpoint, recipients []Recipient) bool {
	if kind == ProviderTeams {
		return len(recipients) == 1 && recipients[0].Kind == RecipientChannel && recipients[0].Address == endpoint.DestinationKey
	}
	for _, recipient := range recipients {
		switch kind {
		case ProviderSMTP, ProviderSES, ProviderGmail, ProviderOutlook:
			if recipient.Kind != RecipientEmail {
				return false
			}
		case ProviderWebhook:
		default:
			return false
		}
	}
	return len(recipients) > 0
}

func compatibleRecipientsForMessage(kind ProviderKind, endpoint Endpoint, recipients []Recipient) bool {
	// Direct webhook deliveries from Signals intentionally have no recipient
	// envelope; the configured endpoint is the complete destination.
	if kind == ProviderWebhook && len(recipients) == 0 {
		return true
	}
	return compatibleRecipients(kind, endpoint, recipients)
}

func validSender(kind ProviderKind, sender string) bool {
	switch kind {
	case ProviderSMTP, ProviderSES, ProviderGmail, ProviderOutlook:
		parsed, err := mail.ParseAddress(sender)
		return err == nil && parsed.Address == sender && len(sender) <= 320 && !strings.ContainsAny(sender, "\r\n")
	case ProviderTeams, ProviderWebhook:
		return sender == ""
	default:
		return false
	}
}

func validPlainText(value string, minimum, maximum int, multiline bool) bool {
	if !utf8.ValidString(value) || len(value) < minimum || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if character < 0x20 && character != '\t' && (!multiline || character != '\n') {
			return false
		}
		if character == 0x7f {
			return false
		}
	}
	return true
}

func validText(value string, minimum, maximum int) bool {
	return validPlainText(strings.TrimSpace(value), minimum, maximum, false)
}

func optionalID(value string) bool { return value == "" || stableIDPattern.MatchString(value) }

func idOrNew(id string) (string, error) {
	if id != "" {
		return id, nil
	}
	return foundation.NewCorrelationID()
}

func deterministicAttemptID(messageID string, attempt int) string {
	digest := sha256.Sum256([]byte(messageID + "\x00" + strconv.Itoa(attempt)))
	return hex.EncodeToString(digest[:16])
}

func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := 5 * time.Minute * time.Duration(1<<min(attempt-1, 8))
	if delay > 24*time.Hour {
		return 24 * time.Hour
	}
	return delay
}

func nonempty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func actor(ctx context.Context) string {
	if scope, ok := foundation.ScopeFromContext(ctx); ok && strings.TrimSpace(scope.ActorID) != "" {
		return scope.ActorID
	}
	return "system:reach"
}

func (s *Service) audit(ctx context.Context, action, resourceType, resourceID string, metadata map[string]string) error {
	if metadata == nil {
		metadata = map[string]string{}
	}
	metadata["requirementId"], metadata["featureId"] = RequirementID, FeatureID
	scope, ok := foundation.ScopeFromContext(ctx)
	if !ok || scope.CorrelationID == "" {
		correlationID, err := foundation.NewCorrelationID()
		if err != nil {
			return err
		}
		scope = foundation.Scope{OrganizationID: s.organizationID, ActorID: actor(ctx), CorrelationID: correlationID}
		ctx = foundation.WithScope(ctx, scope)
	}
	eventID, err := foundation.NewCorrelationID()
	if err != nil {
		return err
	}
	return s.auditor.Record(ctx, foundation.AuditEvent{ID: eventID, OrganizationID: s.organizationID, ActorID: actor(ctx), CorrelationID: scope.CorrelationID,
		Action: action, ResourceType: resourceType, ResourceID: resourceID, OccurredAt: s.now().UTC(), Metadata: metadata})
}
