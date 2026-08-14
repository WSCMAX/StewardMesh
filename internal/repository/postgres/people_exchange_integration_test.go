package postgres

// Requirements: REQ-PEOPLE-001, REQ-EXCHANGE-001. Features: identity.directory, migration.packages. GitHub: #9.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/bootstrap"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/people"
)

type peopleExchangeIntegrationAssets struct{ items map[string]domain.Asset }

func (r peopleExchangeIntegrationAssets) Get(_ context.Context, id string) (domain.Asset, error) {
	item, exists := r.items[id]
	if !exists {
		return domain.Asset{}, errors.New("asset not found")
	}
	return item, nil
}

func TestPeopleExchangeImporterPostgresIntegration(t *testing.T) {
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
	unique := fmt.Sprintf("people-exchange-%d", time.Now().UnixNano())
	organizations, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	organizationService, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := organizationService.EnsureOrganization(ctx, unique, "People Exchange Integration"); err != nil {
		t.Fatal(err)
	}
	store, err := NewPeopleStore(database)
	if err != nil {
		t.Fatal(err)
	}
	auditor, err := NewAuditor(database)
	if err != nil {
		t.Fatal(err)
	}
	assetID := "asset-" + unique
	service, importer, err := people.NewServiceWithExchangeImporter(store, peopleExchangeIntegrationAssets{items: map[string]domain.Asset{assetID: {ID: assetID, OrganizationID: unique, Status: "active"}}}, nil, auditor, people.ServiceConfig{OrganizationID: unique})
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2026, time.August, 13, 12, 34, 56, 789000000, time.UTC)
	updatedAt := createdAt.Add(24 * time.Hour)
	operation := people.ExchangeImportOperation{Token: "people-postgres-import", OccurredAt: updatedAt}
	site := people.Site{ID: fmt.Sprintf("%032x", time.Now().UnixNano()), Name: "Portable site", NormalizedName: "portable site", Address: people.Address{Line1: "100 Main", City: "Madison", Country: "US"}, Status: people.StatusInactive, Revision: 9, CreatedAt: createdAt, UpdatedAt: updatedAt}
	result, err := importer.ImportSite(ctx, operation, site)
	if err != nil || !result.Committed || !result.Created {
		t.Fatalf("import PostgreSQL People site: %#v err=%v", result, err)
	}
	stored, err := service.GetSite(ctx, site.ID)
	if err != nil || stored.OrganizationID != unique || stored.Revision != site.Revision || stored.Address != site.Address || !stored.CreatedAt.Equal(createdAt) || !stored.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("PostgreSQL People site import was not lossless: %#v err=%v", stored, err)
	}
	identityID := fmt.Sprintf("%032x", time.Now().UnixNano()+1)
	identity := people.Identity{ID: identityID, Kind: people.IdentityShared, DisplayName: "Portable identity", NormalizedName: "portable identity", Email: "portable@example.test", NormalizedEmail: "portable@example.test", SiteID: site.ID, Status: people.StatusActive, Provider: "directory.example", ProviderSubject: "subject-one", Revision: 6, CreatedAt: createdAt, UpdatedAt: updatedAt}
	result, err = importer.ImportIdentity(ctx, operation, identity)
	if err != nil || !result.Committed || !result.Created {
		t.Fatalf("import PostgreSQL People identity: %#v err=%v", result, err)
	}
	assignmentID := fmt.Sprintf("%032x", time.Now().UnixNano()+2)
	endedAt := updatedAt.Add(48 * time.Hour)
	assignment := people.AssetAssignment{ID: assignmentID, AssetID: assetID, AssigneeKind: people.AssigneeIdentity, AssigneeID: identityID, Role: people.AssignmentUser, EffectiveFrom: createdAt, EffectiveTo: &endedAt, CreatedBy: "system:exchange", CreatedAt: createdAt}
	result, err = importer.ImportAssetAssignment(ctx, operation, assignment)
	if err != nil || !result.Committed || !result.Created {
		t.Fatalf("import PostgreSQL People assignment: %#v err=%v", result, err)
	}
	storedAssignment, err := service.GetAssetAssignment(ctx, assignmentID)
	if err != nil || storedAssignment.OrganizationID != unique || storedAssignment.EffectiveTo == nil || !storedAssignment.EffectiveTo.Equal(endedAt) || storedAssignment.CreatedBy != "system:exchange" {
		t.Fatalf("PostgreSQL People assignment history was not lossless: %#v err=%v", storedAssignment, err)
	}
	snapshot, err := service.ExchangeSnapshot(ctx, 3)
	if err != nil || len(snapshot.Sites) != 1 || len(snapshot.Identities) != 1 || len(snapshot.Assignments) != 1 {
		t.Fatalf("unexpected PostgreSQL People snapshot %#v err=%v", snapshot, err)
	}
	if _, err := service.ExchangeSnapshot(ctx, 2); !errors.Is(err, people.ErrTooLarge) {
		t.Fatalf("expected bounded PostgreSQL People snapshot, got %v", err)
	}
	var auditCount int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE organization_id=$1 AND correlation_id=$2`, unique, operation.Token).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 3 {
		t.Fatalf("expected three exact People Exchange audits, got %d", auditCount)
	}
	replay, err := importer.ImportAssetAssignment(ctx, operation, assignment)
	if err != nil || !replay.Committed || replay.Created {
		t.Fatalf("replay PostgreSQL People assignment: %#v err=%v", replay, err)
	}
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE organization_id=$1 AND correlation_id=$2`, unique, operation.Token).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 3 {
		t.Fatalf("People audit replay duplicated rows: %d", auditCount)
	}
}
