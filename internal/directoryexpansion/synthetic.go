package directoryexpansion

// Requirement: REQ-DIRECTORY-EXPANSION-007. Feature: platform.foundation.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/people"
)

const (
	SyntheticRequirementID           = "REQ-DIRECTORY-EXPANSION-007"
	SyntheticSourceSystemID          = "synthetic-demo-v1"
	SyntheticConfigRevision          = "dataset-v1"
	SyntheticProvider       Provider = "synthetic"

	syntheticActorID        = "system:synthetic-demo"
	syntheticPreviewKey     = "synthetic-demo-dataset-v1-preview"
	syntheticApplyKey       = "synthetic-demo-dataset-v1-apply"
	syntheticSiteName       = "[Synthetic Demo] Lakeside Campus"
	syntheticBuildingName   = "[Synthetic Demo] Steward Hall"
	syntheticRoomNumber     = "DEMO-101"
	syntheticRoomName       = "[Synthetic Demo] Inventory Lab"
	syntheticDepartmentName = "[Synthetic Demo] Technology Services"
)

var syntheticSiteAddress = people.Address{
	Line1: "100 Demo Way", City: "Example", Region: "IL", PostalCode: "60000", Country: "US",
}

// SyntheticConnector is a deterministic, network-free source. It is registered
// only when the deployment explicitly enables demo seeding, and its stable
// source identity keeps every mapping separate from real provider records.
type SyntheticConnector struct{}

func (SyntheticConnector) SourceSystem() SourceSystem {
	return SourceSystem{ID: SyntheticSourceSystemID, Provider: SyntheticProvider, ConfigRevision: SyntheticConfigRevision}
}

func (SyntheticConnector) PullPage(ctx context.Context, cursor string) (Page, error) {
	if ctx == nil {
		return Page{}, errors.New("synthetic demo context is required")
	}
	if err := ctx.Err(); err != nil {
		return Page{}, err
	}
	if strings.TrimSpace(cursor) != "" {
		return Page{}, fmt.Errorf("%w: synthetic demo cursor is invalid", ErrInvalidInput)
	}
	return Page{Records: syntheticRecords(), CompleteSnapshot: true}, nil
}

// SyntheticSeedResult contains only stable IDs and replay state. Demo names,
// addresses, emails, and provider payloads are intentionally excluded.
type SyntheticSeedResult struct {
	Enabled          bool
	CreatedLocations int
	SiteID           string
	BuildingID       string
	RoomID           string
	DepartmentID     string
	BatchID          string
	PreviewReplay    bool
	ApplyReplay      bool
}

// SyntheticSeeder creates the local location hierarchy and then uses the same
// durable exact-plan importer as external providers. Repeated calls use fixed
// idempotency keys and never overwrite a mismatched local record.
type SyntheticSeeder struct {
	Enabled        bool
	OrganizationID string
	People         *people.Service
	Directory      *Service
	Auditor        foundation.Auditor
}

func (s SyntheticSeeder) Seed(ctx context.Context) (SyntheticSeedResult, error) {
	result := SyntheticSeedResult{Enabled: s.Enabled}
	if !s.Enabled {
		return result, nil
	}
	organizationID := strings.ToLower(strings.TrimSpace(s.OrganizationID))
	if ctx == nil || !strings.HasPrefix(organizationID, "demo-") || s.People == nil || s.Directory == nil || s.Auditor == nil {
		return SyntheticSeedResult{}, errors.New("synthetic demo seeding requires a demo-* organization and initialized People, directory, and audit services")
	}
	correlationID, err := foundation.NewCorrelationID()
	if err != nil {
		return SyntheticSeedResult{}, fmt.Errorf("create synthetic demo correlation id: %w", err)
	}
	ctx = foundation.WithScope(ctx, foundation.Scope{
		OrganizationID: s.OrganizationID,
		ActorID:        syntheticActorID,
		CorrelationID:  correlationID,
	})

	site, created, err := s.ensureSite(ctx)
	if err != nil {
		return SyntheticSeedResult{}, err
	}
	result.SiteID = site.ID
	result.CreatedLocations += boolCount(created)
	building, created, err := s.ensureBuilding(ctx, site.ID)
	if err != nil {
		return SyntheticSeedResult{}, err
	}
	result.BuildingID = building.ID
	result.CreatedLocations += boolCount(created)
	room, created, err := s.ensureRoom(ctx, site.ID, building.ID)
	if err != nil {
		return SyntheticSeedResult{}, err
	}
	result.RoomID = room.ID
	result.CreatedLocations += boolCount(created)
	department, created, err := s.ensureDepartment(ctx, site.ID)
	if err != nil {
		return SyntheticSeedResult{}, err
	}
	result.DepartmentID = department.ID
	result.CreatedLocations += boolCount(created)

	authentication := guard.Authentication{Principal: guard.Principal{Subject: syntheticActorID}}
	preview, err := s.Directory.Preview(ctx, authentication, PreviewRequest{SourceSystemID: SyntheticSourceSystemID}, syntheticPreviewKey)
	if err != nil {
		return SyntheticSeedResult{}, fmt.Errorf("preview synthetic demo directory: %w", err)
	}
	if preview.Batch.Status != BatchPreviewed || preview.Batch.Counts.Conflicts != 0 || preview.Batch.Counts.Failed != 0 {
		return SyntheticSeedResult{}, fmt.Errorf("%w: synthetic demo preview contains conflicts or failures", ErrConflict)
	}
	result.BatchID, result.PreviewReplay = preview.Batch.ID, preview.Replay
	applied, err := s.Directory.Apply(ctx, authentication, preview.Batch.ID, syntheticApplyKey)
	if err != nil {
		return SyntheticSeedResult{}, fmt.Errorf("apply synthetic demo directory: %w", err)
	}
	if applied.Batch.Status != BatchApplied || applied.Batch.Counts.Conflicts != 0 || applied.Batch.Counts.Failed != 0 {
		return SyntheticSeedResult{}, fmt.Errorf("%w: synthetic demo apply did not complete", ErrConflict)
	}
	result.ApplyReplay = applied.Replay
	if err := s.audit(ctx, preview.Batch); err != nil {
		return SyntheticSeedResult{}, err
	}
	return result, nil
}

func (s SyntheticSeeder) audit(ctx context.Context, batch Batch) error {
	eventID := digestStrings(SyntheticRequirementID, s.OrganizationID, SyntheticSourceSystemID, SyntheticConfigRevision)[:32]
	event := foundation.AuditEvent{
		ID:             eventID,
		OrganizationID: s.OrganizationID,
		ActorID:        syntheticActorID,
		CorrelationID:  eventID,
		Action:         "synthetic_demo.seeded",
		ResourceType:   "synthetic_demo_dataset",
		ResourceID:     batch.ID,
		OccurredAt:     batch.CreatedAt,
		Metadata: map[string]string{
			"datasetVersion": SyntheticConfigRevision,
			"requirementId":  SyntheticRequirementID,
			"sourceSystemId": SyntheticSourceSystemID,
			"synthetic":      "true",
		},
	}
	if err := s.Auditor.Record(foundation.WithScope(ctx, foundation.Scope{
		OrganizationID: s.OrganizationID, ActorID: syntheticActorID, CorrelationID: eventID,
	}), event); err != nil {
		return fmt.Errorf("audit synthetic demo seed: %w", err)
	}
	return nil
}

func (s SyntheticSeeder) ensureSite(ctx context.Context) (people.Site, bool, error) {
	sites, err := s.People.ListSites(ctx, people.Visibility{All: true})
	if err != nil {
		return people.Site{}, false, fmt.Errorf("list synthetic demo sites: %w", err)
	}
	for _, site := range sites {
		if !strings.EqualFold(site.Name, syntheticSiteName) {
			continue
		}
		if site.Status != people.StatusActive || site.Address != syntheticSiteAddress {
			return people.Site{}, false, fmt.Errorf("%w: synthetic demo site label belongs to different data", people.ErrConflict)
		}
		return site, false, nil
	}
	created, err := s.People.CreateSite(ctx, people.CreateSiteInput{Name: syntheticSiteName, Address: syntheticSiteAddress, Status: people.StatusActive})
	if err != nil {
		return people.Site{}, false, fmt.Errorf("create synthetic demo site: %w", err)
	}
	return created, true, nil
}

func (s SyntheticSeeder) ensureBuilding(ctx context.Context, siteID string) (people.Building, bool, error) {
	buildings, err := s.People.ListBuildings(ctx, "", people.Visibility{All: true})
	if err != nil {
		return people.Building{}, false, fmt.Errorf("list synthetic demo buildings: %w", err)
	}
	for _, building := range buildings {
		if !strings.EqualFold(building.Name, syntheticBuildingName) {
			continue
		}
		if building.SiteID != siteID || building.Status != people.StatusActive {
			return people.Building{}, false, fmt.Errorf("%w: synthetic demo building label belongs to different data", people.ErrConflict)
		}
		return building, false, nil
	}
	created, err := s.People.CreateBuilding(ctx, people.CreateBuildingInput{SiteID: siteID, Name: syntheticBuildingName, Status: people.StatusActive})
	if err != nil {
		return people.Building{}, false, fmt.Errorf("create synthetic demo building: %w", err)
	}
	return created, true, nil
}

func (s SyntheticSeeder) ensureRoom(ctx context.Context, siteID, buildingID string) (people.Room, bool, error) {
	rooms, err := s.People.ListRooms(ctx, "", "", people.Visibility{All: true})
	if err != nil {
		return people.Room{}, false, fmt.Errorf("list synthetic demo rooms: %w", err)
	}
	for _, room := range rooms {
		if !strings.EqualFold(room.Number, syntheticRoomNumber) {
			continue
		}
		if room.SiteID != siteID || room.BuildingID != buildingID || room.Name != syntheticRoomName || room.Status != people.StatusActive {
			return people.Room{}, false, fmt.Errorf("%w: synthetic demo room label belongs to different data", people.ErrConflict)
		}
		return room, false, nil
	}
	created, err := s.People.CreateRoom(ctx, people.CreateRoomInput{
		SiteID: siteID, BuildingID: buildingID, Number: syntheticRoomNumber, Name: syntheticRoomName, Status: people.StatusActive,
	})
	if err != nil {
		return people.Room{}, false, fmt.Errorf("create synthetic demo room: %w", err)
	}
	return created, true, nil
}

func (s SyntheticSeeder) ensureDepartment(ctx context.Context, siteID string) (people.Department, bool, error) {
	departments, err := s.People.ListDepartments(ctx, people.Visibility{All: true})
	if err != nil {
		return people.Department{}, false, fmt.Errorf("list synthetic demo departments: %w", err)
	}
	for _, department := range departments {
		if !strings.EqualFold(department.Name, syntheticDepartmentName) {
			continue
		}
		if department.SiteID != siteID || department.Status != people.StatusActive {
			return people.Department{}, false, fmt.Errorf("%w: synthetic demo department label belongs to different data", people.ErrConflict)
		}
		return department, false, nil
	}
	created, err := s.People.CreateDepartment(ctx, people.CreateDepartmentInput{Name: syntheticDepartmentName, SiteID: siteID, Status: people.StatusActive})
	if err != nil {
		return people.Department{}, false, fmt.Errorf("create synthetic demo department: %w", err)
	}
	return created, true, nil
}

func syntheticRecords() []Record {
	identityMetadata := func(role string) map[string]string {
		return map[string]string{"dataset-version": SyntheticConfigRevision, "demo-role": role, "origin": "synthetic-demo"}
	}
	groupMetadata := func(kind string) map[string]string {
		return map[string]string{"dataset-version": SyntheticConfigRevision, "demo-kind": kind, "origin": "synthetic-demo"}
	}
	return []Record{
		{SourceRecordID: "synthetic-person-avery", Kind: RecordIdentity, IdentityKind: "person", DisplayName: "[Synthetic Demo] Avery Morgan", Email: "avery.morgan@example.invalid", Status: "active", Department: syntheticDepartmentName, DirectoryAttributes: identityMetadata("inventory-steward")},
		{SourceRecordID: "synthetic-person-casey", Kind: RecordIdentity, IdentityKind: "person", DisplayName: "[Synthetic Demo] Casey Rivera", Email: "casey.rivera@example.invalid", Status: "active", Department: syntheticDepartmentName, DirectoryAttributes: identityMetadata("support-lead")},
		{SourceRecordID: "synthetic-account-helpdesk", Kind: RecordIdentity, IdentityKind: "shared", DisplayName: "[Synthetic Demo] Help Desk", Email: "helpdesk@example.invalid", Status: "active", Department: syntheticDepartmentName, DirectoryAttributes: identityMetadata("shared-service")},
		{SourceRecordID: "synthetic-group-stewards", Kind: RecordGroup, DisplayName: "[Synthetic Demo] Inventory Stewards", GroupName: "demo:inventory:stewards", Description: "Synthetic demo group for inventory stewardship workflows.", Status: "active", NormalizedMetadata: groupMetadata("stewardship")},
		{SourceRecordID: "synthetic-group-technology", Kind: RecordGroup, DisplayName: "[Synthetic Demo] Technology Services", GroupName: "demo:technology:services", Description: "Synthetic demo group for nested relationship workflows.", Status: "active", NormalizedMetadata: groupMetadata("department")},
		{SourceRecordID: "synthetic-membership-avery-stewards", Kind: RecordMembership, DisplayName: "[Synthetic Demo] Avery Morgan", Status: "active", GroupSourceID: "synthetic-group-stewards", MemberSourceID: "synthetic-person-avery", MemberKind: MemberSubject, NormalizedMetadata: groupMetadata("direct")},
		{SourceRecordID: "synthetic-membership-casey-technology", Kind: RecordMembership, DisplayName: "[Synthetic Demo] Casey Rivera", Status: "active", GroupSourceID: "synthetic-group-technology", MemberSourceID: "synthetic-person-casey", MemberKind: MemberSubject, NormalizedMetadata: groupMetadata("direct")},
		{SourceRecordID: "synthetic-membership-stewards-technology", Kind: RecordMembership, DisplayName: "[Synthetic Demo] Inventory Stewards", Status: "active", GroupSourceID: "synthetic-group-technology", MemberSourceID: "synthetic-group-stewards", MemberKind: MemberGroup, NormalizedMetadata: groupMetadata("nested")},
	}
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}
