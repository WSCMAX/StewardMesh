package reach_test

// Requirements: REQ-REACH-001, REQ-EXCHANGE-001.
// Features: messaging.delivery, migration.packages. GitHub: #9, #12.

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/reach"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

type reachExchangeAuditor struct {
	events   map[string]foundation.AuditEvent
	attempts []foundation.AuditEvent
}

func (a *reachExchangeAuditor) Record(_ context.Context, event foundation.AuditEvent) error {
	if a.events == nil {
		a.events = map[string]foundation.AuditEvent{}
	}
	a.attempts = append(a.attempts, event)
	if existing, ok := a.events[event.ID]; ok {
		if !reflect.DeepEqual(existing, event) {
			return errors.New("conflicting audit replay")
		}
		return nil
	}
	a.events[event.ID] = event
	return nil
}

type reachExchangeWriteGate struct {
	err   error
	calls []string
}

func (g *reachExchangeWriteGate) CheckResourceWrite(_ context.Context, recordType, id string) error {
	g.calls = append(g.calls, recordType+":"+id)
	return g.err
}

func TestReachExchangeImporterPreservesPortableStateKeepsProvidersInertAndFencesWrites(t *testing.T) {
	createdAt := time.Date(2023, time.April, 5, 6, 7, 8, 900_000_000, time.UTC)
	updatedAt := createdAt.Add(48 * time.Hour)
	operation := reach.ExchangeImportOperation{Token: "reach-import-one", OccurredAt: updatedAt.Add(time.Hour)}
	locked := errors.New("imported Reach record is locked")
	gate := &reachExchangeWriteGate{err: locked}
	auditor := &reachExchangeAuditor{}
	endpoints, err := reach.NewEndpointCatalog([]reach.Endpoint{{ID: "hook-primary", Label: "Operations webhook", Kind: reach.ProviderWebhook, URL: "https://hooks.example.test/reach"}})
	if err != nil {
		t.Fatal(err)
	}
	transports, err := reach.NewTransportRegistry(map[reach.ProviderKind]reach.Transport{reach.ProviderWebhook: &testTransport{}})
	if err != nil {
		t.Fatal(err)
	}
	service, importer, err := reach.NewServiceWithExchangeImporter(
		repository.NewMemoryReachStore(), endpoints, transports,
		testSecrets{values: map[string][]byte{"external:imported-hook": []byte("01234567890123456789012345678901")}}, nil,
		gate, auditor, reach.ServiceConfig{OrganizationID: "target-org", Now: func() time.Time { return updatedAt.Add(2 * time.Hour) }},
	)
	if err != nil {
		t.Fatal(err)
	}
	provider := reach.Provider{ID: "portable-hook", Name: "Portable webhook", Kind: reach.ProviderWebhook, Revision: 7, CreatedAt: createdAt, UpdatedAt: updatedAt}
	result, err := importer.ImportProvider(context.Background(), operation, provider)
	if err != nil || !result.Committed || !result.Created {
		t.Fatalf("import Reach provider: result=%#v err=%v", result, err)
	}
	template := reach.Template{ID: "portable-template", Name: "Portable template", Subject: "{{title}}", Body: "{{summary}}", Revision: 4, CreatedAt: createdAt, UpdatedAt: updatedAt}
	result, err = importer.ImportTemplate(context.Background(), operation, template)
	if err != nil || !result.Committed || !result.Created {
		t.Fatalf("import Reach template: result=%#v err=%v", result, err)
	}
	group := reach.SubscriberGroup{ID: "portable-group", Name: "Portable group", ProviderID: provider.ID, TemplateID: template.ID,
		Recipients: []reach.Recipient{{Kind: reach.RecipientEmail, Address: "owner@example.test"}}, Revision: 3, CreatedAt: createdAt, UpdatedAt: updatedAt}
	result, err = importer.ImportGroup(context.Background(), operation, group)
	if err != nil || !result.Committed || !result.Created {
		t.Fatalf("import Reach group: result=%#v err=%v", result, err)
	}
	snapshot, err := service.ExchangeSnapshot(context.Background(), 3)
	if err != nil || len(snapshot.Providers) != 1 || len(snapshot.Templates) != 1 || len(snapshot.Groups) != 1 || snapshot.Providers[0].Revision != 7 ||
		snapshot.Templates[0].Revision != 4 || snapshot.Groups[0].Revision != 3 || snapshot.Providers[0].EndpointID != "" || snapshot.Providers[0].SecretRef != "" ||
		snapshot.Providers[0].SecretConfigured || snapshot.Providers[0].Enabled || !snapshot.Providers[0].CreatedAt.Equal(createdAt) || !snapshot.Groups[0].UpdatedAt.Equal(updatedAt) {
		t.Fatalf("lossy or active Reach import snapshot %#v err=%v", snapshot, err)
	}
	if _, err := service.ExchangeSnapshot(context.Background(), 2); !errors.Is(err, reach.ErrTooLarge) {
		t.Fatalf("bounded Reach snapshot accepted too many records: %v", err)
	}
	replay, err := importer.ImportProvider(context.Background(), operation, provider)
	if err != nil || !replay.Committed || replay.Created || len(auditor.events) != 3 || len(auditor.attempts) != 4 || auditor.attempts[0].ID != auditor.attempts[3].ID {
		t.Fatalf("deterministic Reach replay failed: result=%#v events=%#v attempts=%#v err=%v", replay, auditor.events, auditor.attempts, err)
	}
	for _, event := range auditor.events {
		if event.OrganizationID != "target-org" || event.ActorID != "system:exchange" || event.CorrelationID != operation.Token || !event.OccurredAt.Equal(operation.OccurredAt) ||
			strings.Contains(strings.ToLower(event.ResourceID), "secret") || strings.Contains(strings.ToLower(event.Metadata["deploymentState"]), "external:") {
			t.Fatalf("unsafe Reach Exchange audit %#v", event)
		}
	}
	if _, err := service.UpdateProvider(context.Background(), provider.ID, reach.UpdateProviderInput{Name: provider.Name, Revision: provider.Revision}); !errors.Is(err, locked) {
		t.Fatalf("ordinary imported Reach update bypassed ownership fence: %v", err)
	}

	gate.err = nil // Simulates the explicit Guard local-ownership claim.
	configured, err := service.UpdateProvider(context.Background(), provider.ID, reach.UpdateProviderInput{Name: provider.Name, EndpointID: "hook-primary", Revision: provider.Revision})
	if err != nil || configured.Enabled || configured.SecretConfigured || configured.EndpointID != "hook-primary" || configured.Revision != 8 {
		t.Fatalf("post-claim endpoint selection was unsafe: %#v err=%v", configured, err)
	}
	if _, err := service.UpdateProvider(context.Background(), provider.ID, reach.UpdateProviderInput{Name: provider.Name, EndpointID: "hook-primary", Enabled: true, Revision: configured.Revision}); !errors.Is(err, reach.ErrInvalidInput) {
		t.Fatalf("provider enabled before secret configuration: %v", err)
	}
	configured, err = service.RotateProviderSecret(context.Background(), provider.ID, reach.RotateSecretInput{SecretRef: "external:imported-hook", Revision: configured.Revision, Confirm: true})
	if err != nil || configured.Enabled || !configured.SecretConfigured || configured.Revision != 9 {
		t.Fatalf("confirmed post-claim secret rotation failed: %#v err=%v", configured, err)
	}
	configured, err = service.UpdateProvider(context.Background(), provider.ID, reach.UpdateProviderInput{Name: provider.Name, EndpointID: "hook-primary", Enabled: true, Revision: configured.Revision})
	if err != nil || !configured.Enabled || configured.Revision != 10 {
		t.Fatalf("explicit post-claim enable failed: %#v err=%v", configured, err)
	}
}

func TestReachExchangeImporterRejectsDeploymentStateForeignCapabilitiesAndMissingDependencies(t *testing.T) {
	createdAt := time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC)
	operation := reach.ExchangeImportOperation{Token: "reach-import-reject", OccurredAt: createdAt.Add(time.Hour)}
	endpoints, _ := reach.NewEndpointCatalog([]reach.Endpoint{{ID: "hook-primary", Label: "Hook", Kind: reach.ProviderWebhook, URL: "https://hooks.example.test/reach"}})
	transports, _ := reach.NewTransportRegistry(map[reach.ProviderKind]reach.Transport{reach.ProviderWebhook: &testTransport{}})
	first, firstImporter, err := reach.NewServiceWithExchangeImporter(repository.NewMemoryReachStore(), endpoints, transports, testSecrets{values: map[string][]byte{}}, nil, nil, foundation.NopAuditor{}, reach.ServiceConfig{OrganizationID: "first-org"})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := reach.NewServiceWithExchangeImporter(repository.NewMemoryReachStore(), endpoints, transports, testSecrets{values: map[string][]byte{}}, nil, nil, foundation.NopAuditor{}, reach.ServiceConfig{OrganizationID: "second-org"})
	if err != nil {
		t.Fatal(err)
	}
	if !first.OwnsExchangeImporter(firstImporter) || second.OwnsExchangeImporter(firstImporter) {
		t.Fatal("Reach Exchange importer capability was transferable")
	}
	unsafe := reach.Provider{ID: "unsafe-provider", Name: "Unsafe", Kind: reach.ProviderWebhook, EndpointID: "hook-primary", SecretRef: "external:secret", SecretConfigured: true,
		Enabled: true, Revision: 1, CreatedAt: createdAt, UpdatedAt: createdAt}
	if _, err := firstImporter.ImportProvider(context.Background(), operation, unsafe); !errors.Is(err, reach.ErrInvalidInput) {
		t.Fatalf("Reach importer accepted deployment or secret state: %v", err)
	}
	missing := reach.SubscriberGroup{ID: "missing-group", Name: "Missing", ProviderID: "missing-provider", TemplateID: "missing-template",
		Recipients: []reach.Recipient{{Kind: reach.RecipientEmail, Address: "owner@example.test"}}, Revision: 1, CreatedAt: createdAt, UpdatedAt: createdAt}
	if _, err := firstImporter.ImportGroup(context.Background(), operation, missing); !errors.Is(err, reach.ErrReferenceMissing) {
		t.Fatalf("Reach importer accepted missing provider/template dependencies: %v", err)
	}
}
