package people_test

// Requirement: REQ-PEOPLE-001. Feature: identity.directory.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	. "github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

func TestPeopleCheckoutResolvesConflictsAndRanksTaggedPools(t *testing.T) {
	now := time.Date(2026, time.August, 21, 10, 0, 0, 0, time.UTC)
	assets := testAssetReader{assets: map[string]domain.Asset{
		"laptop-a": {ID: "laptop-a", Name: "Dell 14", Kind: "computer", ModelID: "model-dell"},
		"laptop-b": {ID: "laptop-b", Name: "Lenovo 14", Kind: "computer", ModelID: "model-lenovo"},
		"laptop-c": {ID: "laptop-c", Name: "Dell 16", Kind: "computer", ModelID: "model-dell"},
		"laptop-d": {ID: "laptop-d", Name: "HP 14", Kind: "computer", ModelID: "model-hp"},
	}}
	service, err := NewService(repository.NewMemoryPeopleStore(), assets, foundation.NopAuditor{}, ServiceConfig{
		OrganizationID: "example-org",
		Now:            func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := foundation.WithScope(context.Background(), foundation.Scope{
		OrganizationID: "example-org", ActorID: "account-1", CorrelationID: "checkout-test",
	})
	department, err := service.CreateDepartment(ctx, CreateDepartmentInput{Name: "IT"})
	if err != nil {
		t.Fatal(err)
	}
	alex, err := service.CreateIdentity(ctx, CreateIdentityInput{
		Kind: IdentityPerson, DisplayName: "Alex Rivera", Email: "alex@example.test", DepartmentID: department.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	jordan, err := service.CreateIdentity(ctx, CreateIdentityInput{
		Kind: IdentityPerson, DisplayName: "Jordan Lee", Email: "jordan@example.test", DepartmentID: department.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.CreateAssetAssignment(ctx, CreateAssetAssignmentInput{
		AssetID: "laptop-a", AssigneeKind: AssigneeIdentity, AssigneeID: alex.ID, Role: AssignmentUser,
	}); err != nil {
		t.Fatal(err)
	}
	_, err = service.CreateAssetAssignment(ctx, CreateAssetAssignmentInput{
		AssetID: "laptop-a", AssigneeKind: AssigneeIdentity, AssigneeID: jordan.ID, Role: AssignmentUser,
	})
	var overlap *OverlapError
	if !errors.As(err, &overlap) || overlap.ConflictKind != string(PurposeCheckout) || len(overlap.Conflicts) != 1 {
		t.Fatalf("expected checkout overlap, got %#v, %v", overlap, err)
	}
	if overlap.Conflicts[0].AssigneeLabel != "Alex Rivera" {
		t.Fatalf("expected current assignee in overlap, got %#v", overlap.Conflicts)
	}

	replaced, err := service.CreateAssetAssignment(ctx, CreateAssetAssignmentInput{
		AssetID: "laptop-a", AssigneeKind: AssigneeIdentity, AssigneeID: jordan.ID, Role: AssignmentUser,
		ConflictPolicy: ConflictReplace,
	})
	if err != nil || replaced.AssigneeID != jordan.ID {
		t.Fatalf("expected replace checkout, got %#v, %v", replaced, err)
	}
	history, err := service.ListAssetAssignments(ctx, "laptop-a", Visibility{All: true})
	if err != nil {
		t.Fatal(err)
	}
	var alexReturned bool
	for _, item := range history {
		if item.AssigneeID == alex.ID && item.EffectiveTo != nil {
			alexReturned = true
		}
	}
	if !alexReturned {
		t.Fatalf("replace did not end the previous checkout: %#v", history)
	}

	if _, err := service.CreateAssetAssignment(ctx, CreateAssetAssignmentInput{
		AssetID: "laptop-b", AssigneeKind: AssigneeIdentity, AssigneeID: alex.ID, Role: AssignmentUser,
	}); err != nil {
		t.Fatal(err)
	}
	grouped, err := service.CreateAssetAssignment(ctx, CreateAssetAssignmentInput{
		AssetID: "laptop-b", AssigneeKind: AssigneeIdentity, AssigneeID: jordan.ID, Role: AssignmentUser,
		EventSummary:   "Studio cart",
		ConflictPolicy: ConflictGroup,
	})
	if err != nil || grouped.AssigneeKind != AssigneeGroup {
		t.Fatalf("expected group checkout, got %#v, %v", grouped, err)
	}
	group, err := service.GetCheckoutGroup(ctx, grouped.AssigneeID)
	if err != nil || len(group.MemberIDs) != 2 {
		t.Fatalf("expected both users in checkout group, got %#v, %v", group, err)
	}

	conferenceStart := now.Add(30 * 24 * time.Hour)
	conferenceEnd := conferenceStart.Add(3 * 24 * time.Hour)
	reservation, err := service.CreateAssetAssignment(ctx, CreateAssetAssignmentInput{
		AssetID: "laptop-c", AssigneeKind: AssigneeIdentity, AssigneeID: alex.ID, Role: AssignmentUser,
		Purpose: PurposeReservation, EventSummary: "Fall faculty conference",
		EffectiveFrom: conferenceStart, DueAt: &conferenceEnd,
	})
	if err != nil || reservation.Purpose != PurposeReservation {
		t.Fatalf("expected reservation, got %#v, %v", reservation, err)
	}
	_, err = service.CreateAssetAssignment(ctx, CreateAssetAssignmentInput{
		AssetID: "laptop-c", AssigneeKind: AssigneeIdentity, AssigneeID: jordan.ID, Role: AssignmentUser,
		EffectiveFrom: now, DueAt: &conferenceEnd,
	})
	if !errors.As(err, &overlap) || overlap.ConflictKind != string(PurposeReservation) {
		t.Fatalf("expected reservation overlap warning, got %#v, %v", overlap, err)
	}
	if overlap.Conflicts[0].EventSummary != "Fall faculty conference" {
		t.Fatalf("expected event description on overlap, got %#v", overlap.Conflicts)
	}
	loan, err := service.CreateAssetAssignment(ctx, CreateAssetAssignmentInput{
		AssetID: "laptop-c", AssigneeKind: AssigneeIdentity, AssigneeID: jordan.ID, Role: AssignmentUser,
		EffectiveFrom: now, DueAt: &conferenceEnd, ConflictPolicy: ConflictProceed,
	})
	if err != nil || loan.AssigneeID != jordan.ID {
		t.Fatalf("expected acknowledged overlapping checkout, got %#v, %v", loan, err)
	}

	candidates, err := service.RankCheckoutCandidates(ctx, RankCheckoutInput{
		AssetIDs:          []string{"laptop-a", "laptop-b", "laptop-c"},
		From:              now,
		To:                now.Add(2 * 24 * time.Hour),
		PreferredModelIDs: []string{"model-dell"},
		Visibility:        Visibility{All: true},
	})
	if err != nil || len(candidates) != 3 {
		t.Fatalf("unexpected candidates %#v, %v", candidates, err)
	}
	if candidates[0].AssetID != "laptop-a" || candidates[1].AssetID != "laptop-c" || candidates[2].AssetID != "laptop-b" {
		t.Fatalf("expected preferred models and current-checkout penalty, got %#v", candidates)
	}

	bulk, created, err := service.CreateBulkCheckout(ctx, CreateBulkCheckoutInput{
		AssigneeKind:   AssigneeIdentity,
		AssigneeID:     alex.ID,
		Purpose:        PurposeReservation,
		EffectiveFrom:  conferenceStart.Add(10 * 24 * time.Hour),
		DueAt:          ptrTime(conferenceStart.Add(12 * 24 * time.Hour)),
		EventSummary:   "Adjunct onboarding",
		AssetIDs:       []string{"laptop-a", "laptop-b"},
		ConflictPolicy: ConflictProceed,
	})
	if err != nil || bulk.RequestedCount != 2 || len(created) != 2 {
		t.Fatalf("expected bulk reservation, got %#v %#v, %v", bulk, created, err)
	}
	if created[0].BulkCheckoutID != bulk.ID || created[1].BulkCheckoutID != bulk.ID {
		t.Fatalf("bulk assignments missing parent id: %#v", created)
	}

	overdueEnd := now.Add(-24 * time.Hour)
	overdue, err := service.CreateAssetAssignment(ctx, CreateAssetAssignmentInput{
		AssetID: "laptop-d", AssigneeKind: AssigneeIdentity, AssigneeID: alex.ID, Role: AssignmentUser,
		EffectiveFrom: now.Add(-48 * time.Hour), DueAt: &overdueEnd,
	})
	if err != nil {
		t.Fatalf("overdue checkout: %v", err)
	}
	futureStart := now.Add(24 * time.Hour)
	futureEnd := futureStart.Add(24 * time.Hour)
	_, err = service.CreateAssetAssignment(ctx, CreateAssetAssignmentInput{
		AssetID: "laptop-d", AssigneeKind: AssigneeIdentity, AssigneeID: jordan.ID, Role: AssignmentUser,
		EffectiveFrom: futureStart, DueAt: &futureEnd,
	})
	if !errors.As(err, &overlap) || overlap.ConflictKind != string(PurposeCheckout) {
		t.Fatalf("expected overdue checkout to remain active, got %#v, %v", overlap, err)
	}
	if overdue.EffectiveTo != nil {
		t.Fatalf("overdue checkout was closed by due date: %#v", overdue)
	}
}

func ptrTime(value time.Time) *time.Time {
	return &value
}
