package repository_test

// Requirements: REQ-PEOPLE-001, REQ-DIRECTORY-EXPANSION-008. Features: identity.directory, threads.relationships.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/repository/contracttest"
)

func TestMemoryPeopleStoreContract(t *testing.T) {
	contracttest.PeopleStore(t, repository.NewMemoryPeopleStore(), "memory-people-organization")
}

func TestMemoryPeopleGraphIdentitySearchIsLabelOnlyBeforeLimit(t *testing.T) {
	store := repository.NewMemoryPeopleStore()
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	for index := 0; index < 500; index++ {
		name := fmt.Sprintf("Alpha Identity %03d", index)
		identity := people.Identity{ID: fmt.Sprintf("alpha-%03d", index), OrganizationID: "graph-org", Kind: people.IdentityPerson,
			DisplayName: name, NormalizedName: strings.ToLower(name), Email: fmt.Sprintf("needle-%03d@example.test", index),
			NormalizedEmail: fmt.Sprintf("needle-%03d@example.test", index), Status: people.StatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now}
		if _, err := store.CreateIdentity(context.Background(), identity); err != nil {
			t.Fatal(err)
		}
	}
	target := people.Identity{ID: "zeta-label-target", OrganizationID: "graph-org", Kind: people.IdentityPerson,
		DisplayName: "Zeta Needle", NormalizedName: "zeta needle", Status: people.StatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if _, err := store.CreateIdentity(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListGraphIdentities(context.Background(), "graph-org", people.GraphIdentityQuery{LabelSearch: "needle", Limit: 10}, people.Visibility{All: true})
	if err != nil || len(items) != 1 || items[0].ID != target.ID {
		t.Fatalf("hidden email matches crowded out graph label: %#v err=%v", items, err)
	}
}
