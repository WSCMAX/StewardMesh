package exchange_test

// Requirements: REQ-EXCHANGE-001, REQ-REACH-001, REQ-PATTERNS-001.
// Features: migration.packages, messaging.delivery, templates.schemas. GitHub: #8, #9, #12.

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/exchange"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/reach"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

type reachProviderSecrets struct{}

func (reachProviderSecrets) Resolve(context.Context, string) ([]byte, error) {
	return []byte("01234567890123456789012345678901"), nil
}

type reachProviderTransport struct{}

func (reachProviderTransport) Test(context.Context, reach.Endpoint, reach.Provider, []byte) reach.DeliveryResult {
	return reach.DeliveryResult{Succeeded: true}
}

func (reachProviderTransport) Send(context.Context, reach.Endpoint, reach.Provider, reach.Message, []byte) reach.DeliveryResult {
	return reach.DeliveryResult{Succeeded: true}
}

func newReachExchangeProvider(t *testing.T, organizationID string, now time.Time) (*reach.Service, reach.ExchangeImporter, *exchange.ReachProvider) {
	t.Helper()
	endpoints, err := reach.NewEndpointCatalog([]reach.Endpoint{{ID: "hook-primary", Label: "Operations webhook", Kind: reach.ProviderWebhook, URL: "https://hooks.example.test/reach"}})
	if err != nil {
		t.Fatal(err)
	}
	transports, err := reach.NewTransportRegistry(map[reach.ProviderKind]reach.Transport{reach.ProviderWebhook: reachProviderTransport{}})
	if err != nil {
		t.Fatal(err)
	}
	service, importer, err := reach.NewServiceWithExchangeImporter(repository.NewMemoryReachStore(), endpoints, transports, reachProviderSecrets{}, nil, nil, foundation.NopAuditor{}, reach.ServiceConfig{
		OrganizationID: organizationID, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := exchange.NewReachProvider(service, importer)
	if err != nil {
		t.Fatal(err)
	}
	return service, importer, provider
}

func TestReachProviderRoundTripIsStrictTypedAndExcludesDeploymentAndDeliveryState(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.August, 13, 12, 0, 0, 123_456_789, time.UTC)
	source, _, sourceProvider := newReachExchangeProvider(t, "source-org", now)
	configured, err := source.CreateProvider(ctx, reach.CreateProviderInput{ID: "operations-hook", Name: "Operations webhook", Kind: reach.ProviderWebhook,
		EndpointID: "hook-primary", SecretRef: "external:hook-secret"})
	if err != nil || !configured.Enabled || !configured.SecretConfigured {
		t.Fatalf("seed configured source Reach provider %#v err=%v", configured, err)
	}
	template, err := source.CreateTemplate(ctx, reach.CreateTemplateInput{ID: "signal-template", Name: "Signal alert", Subject: "{{severity}}: {{title}}", Body: "{{summary}}"})
	if err != nil {
		t.Fatal(err)
	}
	group, err := source.CreateGroup(ctx, reach.CreateGroupInput{ID: "finance-owners", Name: "Finance owners", ProviderID: configured.ID, TemplateID: template.ID,
		Recipients: []reach.Recipient{{Kind: reach.RecipientEmail, Address: "owner@example.test"}}})
	if err != nil {
		t.Fatal(err)
	}
	records, err := sourceProvider.ListRecords(ctx)
	if err != nil || len(records) != 3 {
		t.Fatalf("list Reach records %#v err=%v", records, err)
	}
	byType := map[string]exchange.Record{}
	for _, record := range records {
		byType[record.Type] = record
		lowerProvider := strings.ToLower(string(record.Payload))
		if record.Type == "reach.provider" && strings.Contains(lowerProvider, "endpoint") || strings.Contains(lowerProvider, "hook-primary") ||
			strings.Contains(lowerProvider, "secret") || strings.Contains(lowerProvider, "external:hook-secret") || strings.Contains(lowerProvider, "enabled") ||
			strings.Contains(lowerProvider, "organization") || strings.Contains(lowerProvider, "createdby") || strings.Contains(lowerProvider, "updatedby") ||
			strings.Contains(lowerProvider, "provider-test") || strings.Contains(lowerProvider, "delivery") {
			t.Fatalf("Reach payload leaked deployment, credential, actor, org, test, or delivery state: %s", record.Payload)
		}
	}
	if got := byType["reach.subscriber-group"].Dependencies; !slices.Equal(got, []exchange.Reference{{Type: "reach.provider", ID: configured.ID}, {Type: "reach.template", ID: template.ID}}) {
		t.Fatalf("Reach group dependencies = %#v", got)
	}
	if len(byType["reach.provider"].Dependencies) != 0 || len(byType["reach.template"].Dependencies) != 0 {
		t.Fatalf("unexpected Reach leaf dependencies: provider=%#v template=%#v", byType["reach.provider"].Dependencies, byType["reach.template"].Dependencies)
	}

	target, _, targetProvider := newReachExchangeProvider(t, "target-org", now.Add(time.Hour))
	for index, recordType := range []string{"reach.provider", "reach.template", "reach.subscriber-group"} {
		record := byType[recordType]
		result, importErr := targetProvider.ImportRecord(ctx, exchange.ProviderImportOperation{Token: "reach-provider-import-" + recordType, OccurredAt: now.Add(time.Duration(index+1) * time.Hour), ExpectedCreated: true}, "source-system", record, nil)
		if importErr != nil || !result.Committed || !result.Created {
			t.Fatalf("import %s: result=%#v err=%v", recordType, result, importErr)
		}
	}
	imported, err := target.ExchangeProvider(ctx, configured.ID)
	if err != nil || imported.EndpointID != "" || imported.SecretRef != "" || imported.SecretConfigured || imported.Enabled || imported.Revision != configured.Revision || !imported.CreatedAt.Equal(configured.CreatedAt) {
		t.Fatalf("imported Reach provider was active or lossy: %#v err=%v", imported, err)
	}
	targetRecords, err := targetProvider.ListRecords(ctx)
	if err != nil || len(targetRecords) != len(records) {
		t.Fatalf("list target Reach records %#v err=%v", targetRecords, err)
	}
	for index := range records {
		if records[index].Type != targetRecords[index].Type || records[index].ID != targetRecords[index].ID || records[index].Revision != targetRecords[index].Revision ||
			!slices.Equal(records[index].Dependencies, targetRecords[index].Dependencies) || !bytes.Equal(records[index].Payload, targetRecords[index].Payload) {
			t.Fatalf("Reach round trip drifted: source=%#v target=%#v", records[index], targetRecords[index])
		}
	}

	noncanonical := byType["reach.provider"]
	noncanonical.Payload = append([]byte(" "), noncanonical.Payload...)
	if _, err := targetProvider.ImportRecordExists(ctx, noncanonical, nil); !errors.Is(err, exchange.ErrInvalidInput) {
		t.Fatalf("Reach provider accepted noncanonical payload: %v", err)
	}
	subMicrosecond := byType["reach.provider"]
	subMicrosecond.Payload = bytes.Replace(subMicrosecond.Payload, []byte(".123456Z"), []byte(".123456789Z"), 1)
	if _, err := targetProvider.ImportRecordExists(ctx, subMicrosecond, nil); !errors.Is(err, exchange.ErrInvalidInput) {
		t.Fatalf("Reach provider accepted a sub-microsecond timestamp: %v", err)
	}
	wrongDependencies := byType["reach.subscriber-group"]
	wrongDependencies.Dependencies = []exchange.Reference{{Type: "reach.template", ID: group.TemplateID}}
	if _, err := targetProvider.ImportRecordExists(ctx, wrongDependencies, nil); !errors.Is(err, exchange.ErrInvalidInput) {
		t.Fatalf("Reach provider accepted incomplete typed dependencies: %v", err)
	}
}

func TestReachProviderRejectsForeignImporterCapabilityAndReportsMissingDependencies(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	first, firstImporter, _ := newReachExchangeProvider(t, "first-org", now)
	second, _, secondProvider := newReachExchangeProvider(t, "second-org", now)
	if _, err := exchange.NewReachProvider(second, firstImporter); err == nil {
		t.Fatal("Reach provider accepted another service's importer capability")
	}
	_ = first
	record := exchange.Record{Type: "reach.subscriber-group", ID: "missing-group", Revision: 1,
		Dependencies: []exchange.Reference{{Type: "reach.provider", ID: "missing-provider"}, {Type: "reach.template", ID: "missing-template"}},
		Payload:      []byte(`{"createdAt":"2026-08-13T12:00:00Z","name":"Missing group","providerId":"missing-provider","recipients":"[{\"kind\":\"email\",\"address\":\"owner@example.test\"}]","templateId":"missing-template","updatedAt":"2026-08-13T12:00:00Z"}`)}
	result, err := secondProvider.ImportRecord(context.Background(), exchange.ProviderImportOperation{Token: "reach-missing-import", OccurredAt: now.Add(time.Hour), ExpectedCreated: true}, "source", record, nil)
	if !errors.Is(err, exchange.ErrDependencyMissing) || result.Committed {
		t.Fatalf("Reach provider accepted missing dependencies: result=%#v err=%v", result, err)
	}
}
