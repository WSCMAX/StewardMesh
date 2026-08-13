package reach_test

// Requirement: REQ-REACH-001. Feature: messaging.delivery. GitHub: #12.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/reach"
	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/signals"
)

type testSecrets struct{ values map[string][]byte }

func (s testSecrets) Resolve(_ context.Context, reference string) ([]byte, error) {
	value, ok := s.values[reference]
	if !ok {
		return nil, errors.New("missing")
	}
	return append([]byte(nil), value...), nil
}

type testTransport struct {
	mu      sync.Mutex
	results []reach.DeliveryResult
	sent    []reach.Message
	entered chan struct{}
	release chan struct{}
}

func (t *testTransport) Test(context.Context, reach.Endpoint, reach.Provider, []byte) reach.DeliveryResult {
	return reach.DeliveryResult{Succeeded: true}
}

func (t *testTransport) Send(_ context.Context, _ reach.Endpoint, _ reach.Provider, message reach.Message, secret []byte) reach.DeliveryResult {
	t.mu.Lock()
	t.sent = append(t.sent, message)
	entered, release := t.entered, t.release
	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
	}
	if string(secret) == "" {
		t.mu.Unlock()
		return reach.DeliveryResult{ErrorCode: "credential_invalid"}
	}
	if len(t.results) == 0 {
		t.mu.Unlock()
		if release != nil {
			<-release
		}
		return reach.DeliveryResult{Succeeded: true}
	}
	result := t.results[0]
	t.results = t.results[1:]
	t.mu.Unlock()
	if release != nil {
		<-release
	}
	return result
}

type signalStub struct {
	pending []signals.Delivery
	alert   signals.Alert
	results []reach.DeliveryResult
}

func (s *signalStub) ListPendingDeliveries(context.Context, time.Time, int) ([]signals.Delivery, error) {
	return append([]signals.Delivery(nil), s.pending...), nil
}
func (s *signalStub) GetAlert(context.Context, string) (signals.Alert, error) { return s.alert, nil }
func (s *signalStub) RecordDeliveryAttempt(_ context.Context, _ string, succeeded, retryable bool, code string) (signals.Delivery, error) {
	s.results = append(s.results, reach.DeliveryResult{Succeeded: succeeded, Retryable: retryable, ErrorCode: code})
	return signals.Delivery{}, nil
}

func TestServiceConfiguresTestsSendsRetriesAndProcessesSignalsWithoutExposingSecrets(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	endpointCatalog, err := reach.NewEndpointCatalog([]reach.Endpoint{{ID: "hook-primary", Label: "Operations webhook", Kind: reach.ProviderWebhook, URL: "https://hooks.example.test/reach", TestURL: "https://hooks.example.test/health"}})
	if err != nil {
		t.Fatal(err)
	}
	transport := &testTransport{}
	registry, err := reach.NewTransportRegistry(map[reach.ProviderKind]reach.Transport{reach.ProviderWebhook: transport})
	if err != nil {
		t.Fatal(err)
	}
	signalSource := &signalStub{pending: []signals.Delivery{{ID: "0123456789abcdef0123456789abcdef", AlertID: "alert-one", TargetKind: "group", TargetID: "finance-owners"}},
		alert: signals.Alert{ID: "alert-one", Title: "Budget exceeded", Summary: "Actual costs exceeded plan.", Severity: signals.SeverityCritical}}
	service, err := reach.NewService(repository.NewMemoryReachStore(), endpointCatalog, registry,
		testSecrets{values: map[string][]byte{"external:hook-secret-v1": []byte("01234567890123456789012345678901"), "external:hook-secret-v2": []byte("abcdefghijklmnopqrstuvwxyz123456")}},
		signalSource, foundation.NopAuditor{}, reach.ServiceConfig{OrganizationID: "organization-one", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}

	provider, err := service.CreateProvider(context.Background(), reach.CreateProviderInput{ID: "operations-hook", Name: "Operations webhook", Kind: reach.ProviderWebhook, EndpointID: "hook-primary", SecretRef: "external:hook-secret-v1"})
	if err != nil {
		t.Fatal(err)
	}
	serialized, err := json.Marshal(provider)
	if err != nil || string(serialized) == "" || containsAny(string(serialized), "hook-secret-v1", "0123456789") {
		t.Fatalf("provider response exposed secret material: %s %v", serialized, err)
	}
	provider, err = service.RotateProviderSecret(context.Background(), provider.ID, reach.RotateSecretInput{SecretRef: "external:hook-secret-v2", Revision: provider.Revision, Confirm: true})
	if err != nil || provider.Revision != 2 || provider.SecretRef != "external:hook-secret-v2" {
		t.Fatalf("rotate external secret reference %#v: %v", provider, err)
	}
	if _, err := service.RotateProviderSecret(context.Background(), provider.ID, reach.RotateSecretInput{SecretRef: "external:hook-secret-v1", Revision: provider.Revision}); !errors.Is(err, reach.ErrInvalidInput) {
		t.Fatalf("expected explicit confirmation requirement, got %v", err)
	}
	testResult, err := service.TestProvider(context.Background(), provider.ID, reach.TestProviderInput{Confirm: true})
	if err != nil || testResult.Outcome != "succeeded" {
		t.Fatalf("test provider %#v: %v", testResult, err)
	}

	template, err := service.CreateTemplate(context.Background(), reach.CreateTemplateInput{ID: "signal-template", Name: "Signal alert", Subject: "{{severity}}: {{title}}", Body: "{{summary}}\nAlert {{record_id}}"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateTemplate(context.Background(), reach.CreateTemplateInput{Name: "Unsafe", Subject: "{{unknown}}", Body: "test"}); !errors.Is(err, reach.ErrInvalidInput) {
		t.Fatalf("expected unknown token rejection, got %v", err)
	}
	group, err := service.CreateGroup(context.Background(), reach.CreateGroupInput{ID: "finance-owners", Name: "Finance owners", ProviderID: provider.ID, TemplateID: template.ID,
		Recipients: []reach.Recipient{{Kind: reach.RecipientEmail, Address: "owner@example.test"}}})
	if err != nil {
		t.Fatal(err)
	}

	message, err := service.Send(context.Background(), reach.SendInput{GroupID: group.ID, Confirm: true, IdempotencyKey: "manual-one",
		Variables: map[string]string{"severity": "warning", "title": "Renewal", "summary": "Renewal is due.", "record_id": "contract-one"}})
	if err != nil || message.Status != "delivered" || message.Attempts != 1 {
		t.Fatalf("send message %#v: %v", message, err)
	}
	replayed, err := service.Send(context.Background(), reach.SendInput{GroupID: group.ID, Confirm: true, IdempotencyKey: "manual-one",
		Variables: map[string]string{"severity": "warning", "title": "Renewal", "summary": "Renewal is due.", "record_id": "contract-one"}})
	if err != nil || replayed.ID != message.ID || len(transport.sent) != 1 {
		t.Fatalf("idempotent send replayed an external call: %#v sends=%d err=%v", replayed, len(transport.sent), err)
	}
	if attempts, err := service.ListAttempts(context.Background(), message.ID); err != nil || len(attempts) != 1 || attempts[0].Outcome != "succeeded" {
		t.Fatalf("delivery history %#v: %v", attempts, err)
	}

	transport.results = []reach.DeliveryResult{{Retryable: true, ErrorCode: "provider_unavailable"}, {Succeeded: true}}
	retrying, err := service.Send(context.Background(), reach.SendInput{GroupID: group.ID, Confirm: true, IdempotencyKey: "manual-two",
		Variables: map[string]string{"severity": "critical", "title": "Budget", "summary": "Budget exceeded.", "record_id": "budget-one"}})
	if err != nil || retrying.Status != "retrying" || retrying.NextAttemptAt == nil {
		t.Fatalf("bounded retry scheduling %#v: %v", retrying, err)
	}
	now = now.Add(10 * time.Minute)
	delivered, err := service.Retry(context.Background(), retrying.ID, reach.RetryInput{Confirm: true})
	if err != nil || delivered.Status != "delivered" || delivered.Attempts != 2 {
		t.Fatalf("retry delivery %#v: %v", delivered, err)
	}

	processed, err := service.ProcessSignals(context.Background(), reach.ProcessSignalsInput{Confirm: true, Limit: 10})
	if err != nil || processed.Examined != 1 || processed.Delivered != 1 || len(signalSource.results) != 1 || !signalSource.results[0].Succeeded {
		t.Fatalf("Signals handoff %#v results=%#v: %v", processed, signalSource.results, err)
	}
	if len(transport.sent) != 4 || transport.sent[3].SourceKind != "signals" || transport.sent[3].Subject != "critical: Budget exceeded" {
		t.Fatalf("unexpected Signals delivery %#v", transport.sent)
	}
}

func TestEndpointCatalogRejectsArbitraryOrInsecureDestinations(t *testing.T) {
	for _, endpoint := range []reach.Endpoint{
		{ID: "hook", Label: "Hook", Kind: reach.ProviderWebhook, URL: "http://example.test/hook"},
		{ID: "hook", Label: "Hook", Kind: reach.ProviderWebhook, URL: "https://user:pass@example.test/hook"},
		{ID: "hook", Label: "Hook", Kind: reach.ProviderWebhook, URL: "https://example.test/hook?token=secret"},
		{ID: "smtp", Label: "SMTP", Kind: reach.ProviderSMTP, Address: "smtp.example.test:25", ServerName: "smtp.example.test"},
	} {
		if _, err := reach.NewEndpointCatalog([]reach.Endpoint{endpoint}); err == nil {
			t.Fatalf("expected insecure endpoint to fail: %#v", endpoint)
		}
	}
	if _, err := reach.NewEndpointCatalog([]reach.Endpoint{{ID: "fixture", Label: "Local fixture", Kind: reach.ProviderWebhook, URL: "http://127.0.0.1:9090/hook", AllowLocalHTTP: true}}); err != nil {
		t.Fatalf("explicit loopback fixture should be supported: %v", err)
	}
}

func TestServiceConfiguresAndTestsEveryProviderKind(t *testing.T) {
	endpoints := []reach.Endpoint{
		{ID: "smtp", Label: "SMTP", Kind: reach.ProviderSMTP, Address: "smtp.example.test:587", ServerName: "smtp.example.test", RequireTLS: true},
		{ID: "ses", Label: "SES", Kind: reach.ProviderSES, URL: "https://email.us-east-1.amazonaws.com/v2/email/outbound-emails", Region: "us-east-1"},
		{ID: "gmail", Label: "Gmail", Kind: reach.ProviderGmail, URL: "https://gmail.googleapis.com/gmail/v1/users/me/messages/send", TestURL: "https://gmail.googleapis.com/gmail/v1/users/me/profile"},
		{ID: "outlook", Label: "Outlook", Kind: reach.ProviderOutlook, URL: "https://graph.microsoft.com/v1.0/me/sendMail", TestURL: "https://graph.microsoft.com/v1.0/me"},
		{ID: "teams", Label: "Teams", Kind: reach.ProviderTeams, URL: "https://graph.microsoft.com/v1.0/teams/team/channels/channel/messages"},
		{ID: "webhook", Label: "Webhook", Kind: reach.ProviderWebhook, URL: "https://hooks.example.test/reach"},
	}
	catalog, err := reach.NewEndpointCatalog(endpoints)
	if err != nil {
		t.Fatal(err)
	}
	transport := &testTransport{}
	registry, err := reach.NewTransportRegistry(map[reach.ProviderKind]reach.Transport{
		reach.ProviderSMTP: transport, reach.ProviderSES: transport, reach.ProviderGmail: transport,
		reach.ProviderOutlook: transport, reach.ProviderTeams: transport, reach.ProviderWebhook: transport,
	})
	if err != nil {
		t.Fatal(err)
	}
	secrets := testSecrets{values: map[string][]byte{}}
	for _, endpoint := range endpoints {
		secrets.values["external:"+endpoint.ID] = []byte("01234567890123456789012345678901")
	}
	service, err := reach.NewService(repository.NewMemoryReachStore(), catalog, registry, secrets, nil, foundation.NopAuditor{}, reach.ServiceConfig{OrganizationID: "organization-one"})
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range endpoints {
		t.Run(string(endpoint.Kind), func(t *testing.T) {
			sender := ""
			if endpoint.Kind == reach.ProviderSMTP || endpoint.Kind == reach.ProviderSES || endpoint.Kind == reach.ProviderGmail || endpoint.Kind == reach.ProviderOutlook {
				sender = "sender@example.test"
			}
			provider, err := service.CreateProvider(context.Background(), reach.CreateProviderInput{ID: endpoint.ID, Name: endpoint.Label, Kind: endpoint.Kind, EndpointID: endpoint.ID, Sender: sender, SecretRef: "external:" + endpoint.ID})
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.TestProvider(context.Background(), provider.ID, reach.TestProviderInput{Confirm: true})
			if err != nil || result.Outcome != "succeeded" {
				t.Fatalf("test provider %#v: %v", result, err)
			}
		})
	}
}

func TestSignalsRetryResultReplayDoesNotDuplicateExternalCallBeforeDueTime(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	catalog, _ := reach.NewEndpointCatalog([]reach.Endpoint{{ID: "hook", Label: "Hook", Kind: reach.ProviderWebhook, URL: "https://hooks.example.test/reach"}})
	transport := &testTransport{results: []reach.DeliveryResult{{Retryable: true, ErrorCode: "provider_unavailable"}}}
	registry, _ := reach.NewTransportRegistry(map[reach.ProviderKind]reach.Transport{reach.ProviderWebhook: transport})
	source := &signalStub{pending: []signals.Delivery{{ID: "0123456789abcdef0123456789abcdef", AlertID: "alert", TargetKind: "webhook", TargetID: "hook"}}, alert: signals.Alert{ID: "alert", Title: "Alert", Summary: "Summary", Severity: signals.SeverityWarning}}
	service, err := reach.NewService(repository.NewMemoryReachStore(), catalog, registry, testSecrets{values: map[string][]byte{"external:hook": []byte("01234567890123456789012345678901")}}, source, foundation.NopAuditor{}, reach.ServiceConfig{OrganizationID: "organization-one", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateProvider(context.Background(), reach.CreateProviderInput{ID: "hook", Name: "Hook", Kind: reach.ProviderWebhook, EndpointID: "hook", SecretRef: "external:hook"}); err != nil {
		t.Fatal(err)
	}
	for count := 0; count < 2; count++ {
		result, err := service.ProcessSignals(context.Background(), reach.ProcessSignalsInput{Confirm: true})
		if err != nil || result.Retrying != 1 {
			t.Fatalf("process retry result %#v: %v", result, err)
		}
	}
	if len(transport.sent) != 1 {
		t.Fatalf("retry result replay made %d external calls", len(transport.sent))
	}
}

func TestConcurrentRetriesClaimBeforeExternalDelivery(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	catalog, _ := reach.NewEndpointCatalog([]reach.Endpoint{{ID: "hook", Label: "Hook", Kind: reach.ProviderWebhook, URL: "https://hooks.example.test/reach"}})
	transport := &testTransport{results: []reach.DeliveryResult{{Retryable: true, ErrorCode: "provider_unavailable"}}}
	registry, _ := reach.NewTransportRegistry(map[reach.ProviderKind]reach.Transport{reach.ProviderWebhook: transport})
	service, err := reach.NewService(repository.NewMemoryReachStore(), catalog, registry,
		testSecrets{values: map[string][]byte{"external:hook": []byte("01234567890123456789012345678901")}}, nil,
		foundation.NopAuditor{}, reach.ServiceConfig{OrganizationID: "concurrent-reach", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := service.CreateProvider(context.Background(), reach.CreateProviderInput{ID: "hook", Name: "Hook", Kind: reach.ProviderWebhook, EndpointID: "hook", SecretRef: "external:hook"})
	if err != nil {
		t.Fatal(err)
	}
	template, err := service.CreateTemplate(context.Background(), reach.CreateTemplateInput{ID: "template", Name: "Template", Subject: "Alert", Body: "Body"})
	if err != nil {
		t.Fatal(err)
	}
	group, err := service.CreateGroup(context.Background(), reach.CreateGroupInput{ID: "group", Name: "Group", ProviderID: provider.ID, TemplateID: template.ID,
		Recipients: []reach.Recipient{{Kind: reach.RecipientEmail, Address: "owner@example.test"}}})
	if err != nil {
		t.Fatal(err)
	}
	retrying, err := service.Send(context.Background(), reach.SendInput{GroupID: group.ID, Confirm: true, IdempotencyKey: "concurrent"})
	if err != nil || retrying.Status != "retrying" {
		t.Fatalf("prepare retrying message %#v: %v", retrying, err)
	}
	now = now.Add(10 * time.Minute)
	transport.entered = make(chan struct{}, 1)
	transport.release = make(chan struct{})
	transport.results = []reach.DeliveryResult{{Succeeded: true}}

	type outcome struct {
		message reach.Message
		err     error
	}
	results := make(chan outcome, 2)
	go func() {
		message, retryErr := service.Retry(context.Background(), retrying.ID, reach.RetryInput{Confirm: true})
		results <- outcome{message: message, err: retryErr}
	}()
	select {
	case <-transport.entered:
	case <-time.After(time.Second):
		t.Fatal("first retry did not reach transport")
	}
	go func() {
		message, retryErr := service.Retry(context.Background(), retrying.ID, reach.RetryInput{Confirm: true})
		results <- outcome{message: message, err: retryErr}
	}()
	second := <-results
	if !errors.Is(second.err, reach.ErrConflict) {
		t.Fatalf("concurrent retry was not rejected: %#v err=%v", second.message, second.err)
	}
	close(transport.release)
	first := <-results
	if first.err != nil || first.message.Status != "delivered" {
		t.Fatalf("claimed retry failed: %#v err=%v", first.message, first.err)
	}
	transport.mu.Lock()
	sends := len(transport.sent)
	transport.mu.Unlock()
	if sends != 2 { // initial retrying send plus exactly one concurrent retry.
		t.Fatalf("concurrent retries made %d external calls; want 2 total", sends)
	}
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if candidate != "" && strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}
