package contracttest

// Requirement: REQ-REACH-001. Feature: messaging.delivery. GitHub: #12.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/reach"
)

func ReachStore(t *testing.T, store reach.Store, organizationID, suffix string) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	provider := reach.Provider{ID: "reach-provider-" + suffix, OrganizationID: organizationID, Name: "Operations " + suffix, Kind: reach.ProviderWebhook,
		EndpointID: "hook-primary", SecretRef: "external:hook-secret", SecretConfigured: true, Enabled: true, Revision: 1,
		CreatedBy: "account-one", UpdatedBy: "account-one", CreatedAt: now, UpdatedAt: now}
	createdProvider, err := store.CreateProvider(ctx, provider)
	if err != nil || createdProvider.SecretRef != provider.SecretRef || !createdProvider.SecretConfigured {
		t.Fatalf("create Reach provider %#v: %v", createdProvider, err)
	}
	if _, err := store.CreateProvider(ctx, provider); !errors.Is(err, reach.ErrConflict) {
		t.Fatalf("expected duplicate provider conflict, got %v", err)
	}
	providers, err := store.ListProviders(ctx, organizationID)
	if err != nil || len(providers) != 1 {
		t.Fatalf("list Reach providers %#v: %v", providers, err)
	}
	provider.Name, provider.Revision, provider.UpdatedAt = "Operations updated "+suffix, 2, now.Add(time.Minute)
	if updated, err := store.UpdateProvider(ctx, provider, 1); err != nil || updated.Revision != 2 {
		t.Fatalf("update Reach provider %#v: %v", updated, err)
	}
	if _, err := store.UpdateProvider(ctx, provider, 1); !errors.Is(err, reach.ErrConflict) {
		t.Fatalf("expected stale provider conflict, got %v", err)
	}

	template := reach.Template{ID: "reach-template-" + suffix, OrganizationID: organizationID, Name: "Alert " + suffix,
		Subject: "{{title}}", Body: "{{summary}}", Revision: 1, CreatedBy: "account-one", UpdatedBy: "account-one", CreatedAt: now, UpdatedAt: now}
	if _, err := store.CreateTemplate(ctx, template); err != nil {
		t.Fatal(err)
	}
	if templates, err := store.ListTemplates(ctx, organizationID); err != nil || len(templates) != 1 {
		t.Fatalf("list Reach templates %#v: %v", templates, err)
	}
	template.Name, template.Revision, template.UpdatedAt = "Alert updated "+suffix, 2, now.Add(time.Minute)
	if _, err := store.UpdateTemplate(ctx, template, 1); err != nil {
		t.Fatal(err)
	}

	group := reach.SubscriberGroup{ID: "reach-group-" + suffix, OrganizationID: organizationID, Name: "Owners " + suffix,
		ProviderID: provider.ID, TemplateID: template.ID, Recipients: []reach.Recipient{{Kind: reach.RecipientEmail, Address: "owner@example.test"}},
		Revision: 1, CreatedBy: "account-one", UpdatedBy: "account-one", CreatedAt: now, UpdatedAt: now}
	if _, err := store.CreateGroup(ctx, group); err != nil {
		t.Fatal(err)
	}
	groups, err := store.ListGroups(ctx, organizationID)
	if err != nil || len(groups) != 1 || groups[0].Recipients[0].Address != "owner@example.test" {
		t.Fatalf("list Reach groups %#v: %v", groups, err)
	}
	group.Name, group.Revision, group.UpdatedAt = "Owners updated "+suffix, 2, now.Add(time.Minute)
	if _, err := store.UpdateGroup(ctx, group, 1); err != nil {
		t.Fatal(err)
	}

	message := reach.Message{ID: "reach-message-" + suffix, OrganizationID: organizationID, GroupID: group.ID, ProviderID: provider.ID, TemplateID: template.ID,
		SourceKind: "manual", Subject: "Budget alert", Body: "A budget is over plan.", Recipients: group.Recipients,
		Status: "queued", CreatedBy: "account-one", CreatedAt: now, UpdatedAt: now}
	createdMessage, wasCreated, err := store.CreateMessage(ctx, message)
	if err != nil || !wasCreated || createdMessage.ID != message.ID {
		t.Fatalf("create Reach message %#v created=%v: %v", createdMessage, wasCreated, err)
	}
	if replay, created, err := store.CreateMessage(ctx, message); err != nil || created || replay.ID != message.ID {
		t.Fatalf("idempotent Reach message %#v created=%v: %v", replay, created, err)
	}
	message.Attempts, message.Status, message.UpdatedAt = 1, "delivered", now.Add(2*time.Minute)
	attempt := reach.DeliveryAttempt{ID: "0123456789abcdef0123456789abcdef", OrganizationID: organizationID, MessageID: message.ID,
		Attempt: 1, Outcome: "succeeded", OccurredAt: message.UpdatedAt}
	if updated, err := store.RecordAttempt(ctx, message, 0, attempt); err != nil || updated.Status != "delivered" {
		t.Fatalf("record Reach attempt %#v: %v", updated, err)
	}
	if _, err := store.RecordAttempt(ctx, message, 0, attempt); !errors.Is(err, reach.ErrConflict) {
		t.Fatalf("expected stale attempt conflict, got %v", err)
	}
	if attempts, err := store.ListAttempts(ctx, organizationID, message.ID); err != nil || len(attempts) != 1 || attempts[0].Outcome != "succeeded" {
		t.Fatalf("list Reach attempts %#v: %v", attempts, err)
	}
	if messages, err := store.ListMessages(ctx, organizationID, 10); err != nil || len(messages) != 1 || messages[0].Status != "delivered" {
		t.Fatalf("list Reach messages %#v: %v", messages, err)
	}

	test := reach.ProviderTest{ID: "reach-test-" + suffix, OrganizationID: organizationID, ProviderID: provider.ID, Outcome: "succeeded", TestedBy: "account-one", TestedAt: now}
	if _, err := store.CreateProviderTest(ctx, test); err != nil {
		t.Fatal(err)
	}
	if tests, err := store.ListProviderTests(ctx, organizationID, provider.ID); err != nil || len(tests) != 1 {
		t.Fatalf("list Reach provider tests %#v: %v", tests, err)
	}

	other := organizationID + "-other"
	if items, err := store.ListProviders(ctx, other); err != nil || len(items) != 0 {
		t.Fatalf("provider organization isolation failed %#v: %v", items, err)
	}
	if _, err := store.GetMessage(ctx, other, message.ID); !errors.Is(err, reach.ErrNotFound) {
		t.Fatalf("message organization isolation failed: %v", err)
	}
}
