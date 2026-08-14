package postgres

// Requirements: REQ-REACH-001, REQ-EXCHANGE-001.
// Features: messaging.delivery, migration.packages. GitHub: #9, #12.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/bootstrap"
	"github.com/maxlemke/stewardmesh/internal/reach"
)

type reachExchangeSecrets struct{}

func (reachExchangeSecrets) Resolve(context.Context, string) ([]byte, error) {
	return nil, errors.New("secret is not configured")
}

func TestReachExchangeImporterPostgresIntegration(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	organizationID := fmt.Sprintf("reach-exchange-%d", time.Now().UnixNano())
	organizations, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	organizationService, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := organizationService.EnsureOrganization(ctx, organizationID, "Reach Exchange Integration"); err != nil {
		t.Fatal(err)
	}
	store, err := NewReachStore(database)
	if err != nil {
		t.Fatal(err)
	}
	auditor, err := NewAuditor(database)
	if err != nil {
		t.Fatal(err)
	}
	endpoints, err := reach.NewEndpointCatalog(nil)
	if err != nil {
		t.Fatal(err)
	}
	transports, err := reach.NewTransportRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	service, importer, err := reach.NewServiceWithExchangeImporter(
		store, endpoints, transports, reachExchangeSecrets{}, nil, nil, auditor,
		reach.ServiceConfig{OrganizationID: organizationID},
	)
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2023, time.March, 4, 5, 6, 7, 800_000_000, time.UTC)
	updatedAt := createdAt.Add(48 * time.Hour)
	operation := reach.ExchangeImportOperation{Token: "reach-postgres-import", OccurredAt: updatedAt.Add(time.Hour)}
	provider := reach.Provider{ID: "portable-hook", Name: "Portable webhook", Kind: reach.ProviderWebhook,
		Revision: 11, CreatedAt: createdAt, UpdatedAt: updatedAt}
	if result, err := importer.ImportProvider(ctx, operation, provider); err != nil || !result.Committed || !result.Created {
		t.Fatalf("import PostgreSQL Reach provider: %#v err=%v", result, err)
	}
	template := reach.Template{ID: "portable-template", Name: "Portable template", Subject: "{{title}}", Body: "{{summary}}",
		Revision: 7, CreatedAt: createdAt.Add(time.Hour), UpdatedAt: updatedAt}
	if result, err := importer.ImportTemplate(ctx, operation, template); err != nil || !result.Committed || !result.Created {
		t.Fatalf("import PostgreSQL Reach template: %#v err=%v", result, err)
	}
	group := reach.SubscriberGroup{ID: "portable-group", Name: "Portable group", ProviderID: provider.ID, TemplateID: template.ID,
		Recipients: []reach.Recipient{{Kind: reach.RecipientEmail, Address: "owner@example.test"}}, Revision: 5,
		CreatedAt: createdAt.Add(2 * time.Hour), UpdatedAt: updatedAt}
	if result, err := importer.ImportGroup(ctx, operation, group); err != nil || !result.Committed || !result.Created {
		t.Fatalf("import PostgreSQL Reach group: %#v err=%v", result, err)
	}

	snapshot, err := service.ExchangeSnapshot(ctx, 3)
	if err != nil || len(snapshot.Providers) != 1 || len(snapshot.Templates) != 1 || len(snapshot.Groups) != 1 ||
		snapshot.Providers[0].Revision != provider.Revision || snapshot.Templates[0].Revision != template.Revision || snapshot.Groups[0].Revision != group.Revision ||
		!snapshot.Providers[0].CreatedAt.Equal(createdAt) || !snapshot.Groups[0].UpdatedAt.Equal(updatedAt) || snapshot.Providers[0].EndpointID != "" ||
		snapshot.Providers[0].SecretRef != "" || snapshot.Providers[0].SecretConfigured || snapshot.Providers[0].Enabled {
		t.Fatalf("PostgreSQL Reach snapshot was lossy or active: %#v err=%v", snapshot, err)
	}
	if _, err := service.ExchangeSnapshot(ctx, 2); !errors.Is(err, reach.ErrTooLarge) {
		t.Fatalf("expected bounded PostgreSQL Reach snapshot rejection, got %v", err)
	}
	var endpointMissing, secretMissing bool
	if err := database.QueryRowContext(ctx, `SELECT endpoint_id IS NULL,secret_ref IS NULL FROM reach_providers WHERE organization_id=$1 AND id=$2`, organizationID, provider.ID).Scan(&endpointMissing, &secretMissing); err != nil {
		t.Fatal(err)
	}
	if !endpointMissing || !secretMissing {
		t.Fatal("PostgreSQL persisted deployment-owned Reach provider configuration")
	}

	replay, err := importer.ImportGroup(ctx, operation, group)
	if err != nil || !replay.Committed || replay.Created {
		t.Fatalf("replay PostgreSQL Reach group: %#v err=%v", replay, err)
	}
	var auditCount int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE organization_id=$1 AND correlation_id=$2`, organizationID, operation.Token).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 3 {
		t.Fatalf("Reach exact replay duplicated or omitted audits: %d", auditCount)
	}
}
