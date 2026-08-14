package people_test

// Requirements: REQ-PEOPLE-001, REQ-EXCHANGE-001. Features: identity.directory, migration.packages. GitHub: #9.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

type peopleExchangeAssets struct{ item domain.Asset }

func (r peopleExchangeAssets) Get(_ context.Context, id string) (domain.Asset, error) {
	if r.item.ID != id {
		return domain.Asset{}, errors.New("not found")
	}
	return r.item, nil
}

type peopleExchangeWriteGate struct {
	locked map[string]bool
	calls  []string
	err    error
}

func (g *peopleExchangeWriteGate) CheckResourceWrite(_ context.Context, recordType, id string) error {
	key := recordType + "/" + id
	g.calls = append(g.calls, key)
	if g.err != nil {
		return g.err
	}
	if g.locked[key] {
		return guard.ErrResourceWriteLocked
	}
	return nil
}

func TestPeopleExchangeImporterPreservesStateAndUsesOpaqueCapability(t *testing.T) {
	now := time.Date(2026, time.August, 13, 17, 0, 0, 0, time.UTC)
	gate := &peopleExchangeWriteGate{locked: map[string]bool{}}
	service, importer, err := people.NewServiceWithExchangeImporter(repository.NewMemoryPeopleStore(), peopleExchangeAssets{item: domain.Asset{ID: "asset-one"}}, gate, foundation.NopAuditor{}, people.ServiceConfig{OrganizationID: "example-org", Now: func() time.Time { return now.Add(time.Hour) }})
	if err != nil {
		t.Fatal(err)
	}
	if !service.OwnsExchangeImporter(importer) {
		t.Fatal("People service did not recognize its importer capability")
	}
	candidate := people.Site{ID: "0123456789abcdef0123456789abcdef", Name: "Main Campus", NormalizedName: "main campus", Address: people.Address{Line1: "100 Main", City: "Madison", Country: "US"}, Status: people.StatusInactive, Revision: 7, CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now}
	result, err := importer.ImportSite(context.Background(), people.ExchangeImportOperation{Token: "people-import-site", OccurredAt: now}, candidate)
	if err != nil || !result.Committed || !result.Created {
		t.Fatalf("import People site: %#v err=%v", result, err)
	}
	stored, err := service.GetSite(context.Background(), candidate.ID)
	if err != nil || stored.OrganizationID != "example-org" || stored.Revision != candidate.Revision || stored.Address != candidate.Address || !stored.CreatedAt.Equal(candidate.CreatedAt) || !stored.UpdatedAt.Equal(candidate.UpdatedAt) {
		t.Fatalf("People importer did not preserve state: %#v err=%v", stored, err)
	}
	replay, err := importer.ImportSite(context.Background(), people.ExchangeImportOperation{Token: "people-import-site", OccurredAt: now}, candidate)
	if err != nil || !replay.Committed || replay.Created {
		t.Fatalf("replay People site: %#v err=%v", replay, err)
	}
	changed := candidate
	changed.Name = "Changed"
	changed.NormalizedName = "changed"
	if _, err := importer.ImportSite(context.Background(), people.ExchangeImportOperation{Token: "people-import-site", OccurredAt: now}, changed); !errors.Is(err, people.ErrConflict) {
		t.Fatalf("expected changed replay conflict, got %v", err)
	}
	if len(gate.calls) != 0 {
		t.Fatalf("Exchange importer unexpectedly used ordinary write gate: %#v", gate.calls)
	}
}

func TestPeopleExchangeImporterRejectsRevisionOneTimestampDrift(t *testing.T) {
	createdAt := time.Date(2026, time.August, 13, 17, 0, 0, 0, time.UTC)
	service, importer, err := people.NewServiceWithExchangeImporter(
		repository.NewMemoryPeopleStore(), peopleExchangeAssets{}, nil, foundation.NopAuditor{},
		people.ServiceConfig{OrganizationID: "example-org"},
	)
	if err != nil {
		t.Fatal(err)
	}
	candidate := people.Site{ID: "0123456789abcdef0123456789abcdef", Name: "Main Campus", NormalizedName: "main campus",
		Status: people.StatusActive, Revision: 1, CreatedAt: createdAt, UpdatedAt: createdAt.Add(time.Minute)}
	if result, err := importer.ImportSite(context.Background(), people.ExchangeImportOperation{
		Token: "people-revision-one-drift", OccurredAt: createdAt.Add(time.Hour),
	}, candidate); !errors.Is(err, people.ErrInvalidInput) || result.Committed {
		t.Fatalf("accepted revision-one People timestamp drift: %#v err=%v", result, err)
	}
	if _, err := service.GetSite(context.Background(), candidate.ID); !errors.Is(err, people.ErrNotFound) {
		t.Fatalf("invalid People record was persisted: %v", err)
	}
}

func TestPeopleOrdinaryServiceWritesUseGuardFence(t *testing.T) {
	gate := &peopleExchangeWriteGate{locked: map[string]bool{}}
	service, _, err := people.NewServiceWithExchangeImporter(repository.NewMemoryPeopleStore(), peopleExchangeAssets{}, gate, foundation.NopAuditor{}, people.ServiceConfig{OrganizationID: "example-org"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateSite(context.Background(), people.CreateSiteInput{Name: "Local"}); err != nil {
		t.Fatal(err)
	}
	if len(gate.calls) != 1 || !strings.HasPrefix(gate.calls[0], "people.site/") {
		t.Fatalf("ordinary People write did not use its typed Guard fence: %#v", gate.calls)
	}
	gate.err = guard.ErrResourceWriteLocked
	if _, err := service.CreateSite(context.Background(), people.CreateSiteInput{Name: "Locked"}); !errors.Is(err, guard.ErrResourceWriteLocked) {
		t.Fatalf("ordinary People write did not preserve Guard lock error: %v", err)
	}
	sites, err := service.ListSites(context.Background(), people.Visibility{All: true})
	if err != nil || len(sites) != 1 {
		t.Fatalf("Guard-locked People write changed persisted state: %#v err=%v", sites, err)
	}
}
