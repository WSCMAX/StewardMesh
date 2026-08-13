package directoryexpansion

// Requirement: REQ-DIRECTORY-EXPANSION-005. Feature: integrations.protocols.

import (
	"context"
	"errors"
	"time"

	"github.com/maxlemke/stewardmesh/internal/guard"
)

const (
	groupResourceType      = "directory.group"
	membershipResourceType = "directory.membership"
)

// DirectoryTarget dispatches normalized records to the existing People
// identity target or to the durable group/membership target. Provider adapters
// remain unaware of storage details and continue to use the shared preview,
// exact-plan apply, idempotency, retry, and audit engine.
type DirectoryTarget struct {
	people *PeopleTarget
	groups *GroupTarget
}

func NewDirectoryTarget(peopleTarget *PeopleTarget, groupTarget *GroupTarget) (*DirectoryTarget, error) {
	if peopleTarget == nil || groupTarget == nil {
		return nil, errors.New("People and group directory targets are required")
	}
	return &DirectoryTarget{people: peopleTarget, groups: groupTarget}, nil
}

func (t *DirectoryTarget) Preview(ctx context.Context, organizationID string, system SourceSystem, record Record, mapping *Mapping) (TargetPlan, error) {
	switch record.Kind {
	case RecordIdentity:
		return t.people.Preview(ctx, organizationID, system, record, mapping)
	case RecordGroup, RecordMembership:
		return t.groups.Preview(ctx, organizationID, system, record, mapping)
	default:
		return TargetPlan{}, ErrInvalidInput
	}
}

func (t *DirectoryTarget) Apply(ctx context.Context, authentication guard.Authentication, system SourceSystem, item Item) (TargetResult, error) {
	switch item.Record.Kind {
	case RecordIdentity:
		return t.people.Apply(ctx, authentication, system, item)
	case RecordGroup, RecordMembership:
		return t.groups.Apply(ctx, authentication, system, item)
	default:
		return TargetResult{}, ErrInvalidInput
	}
}

func (t *DirectoryTarget) Compensate(ctx context.Context, authentication guard.Authentication, system SourceSystem, item Item, result TargetResult) error {
	switch item.Record.Kind {
	case RecordIdentity:
		return t.people.Compensate(ctx, authentication, system, item, result)
	case RecordGroup, RecordMembership:
		return t.groups.Compensate(ctx, authentication, system, item, result)
	default:
		return ErrInvalidInput
	}
}

// GroupTarget owns only normalized group and membership target records. The
// organization is always provided by the importer and every lookup is scoped.
type GroupTarget struct {
	store GroupTargetStore
	guard *guard.Service
	now   func() time.Time
}

func NewGroupTarget(store GroupTargetStore, guardService *guard.Service, now func() time.Time) (*GroupTarget, error) {
	if store == nil || guardService == nil {
		return nil, errors.New("group target store and Guard service are required")
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &GroupTarget{store: store, guard: guardService, now: now}, nil
}

func (t *GroupTarget) Preview(ctx context.Context, organizationID string, system SourceSystem, record Record, mapping *Mapping) (TargetPlan, error) {
	switch record.Kind {
	case RecordGroup:
		return t.previewGroup(ctx, organizationID, system, record, mapping)
	case RecordMembership:
		return t.previewMembership(ctx, organizationID, system, record, mapping)
	default:
		return TargetPlan{}, ErrInvalidInput
	}
}

func (t *GroupTarget) previewGroup(ctx context.Context, organizationID string, system SourceSystem, record Record, mapping *Mapping) (TargetPlan, error) {
	targetID := stableManagedID(string(RecordGroup), organizationID, system.ID, record.SourceRecordID)
	if mapping != nil {
		targetID = mapping.TargetID
	}
	desired := groupDigest(record)
	group, err := t.store.GetManagedGroup(ctx, organizationID, targetID)
	if errors.Is(err, ErrNotFound) {
		return TargetPlan{TargetID: targetID, DesiredDigest: desired}, nil
	}
	if err != nil {
		return TargetPlan{}, groupTargetStoreError("group could not be read", err)
	}
	plan := TargetPlan{TargetID: group.ID, Revision: group.Revision, CurrentDigest: existingGroupDigest(group),
		DesiredDigest: desired, Found: true,
		SourceMatched: group.SourceSystemID == system.ID && group.SourceRecordID == record.SourceRecordID}
	return t.applyOwnershipPlan(ctx, organizationID, system, record.SourceRecordID, groupResourceType, plan)
}

func (t *GroupTarget) previewMembership(ctx context.Context, organizationID string, system SourceSystem, record Record, mapping *Mapping) (TargetPlan, error) {
	targetID := stableManagedID(string(RecordMembership), organizationID, system.ID, record.SourceRecordID)
	if mapping != nil {
		targetID = mapping.TargetID
	}
	desired := membershipDigest(record)
	membership, err := t.store.GetManagedMembership(ctx, organizationID, targetID)
	if errors.Is(err, ErrNotFound) {
		return TargetPlan{TargetID: targetID, DesiredDigest: desired}, nil
	}
	if err != nil {
		return TargetPlan{}, groupTargetStoreError("membership could not be read", err)
	}
	plan := TargetPlan{TargetID: membership.ID, Revision: membership.Revision,
		CurrentDigest: existingMembershipDigest(membership), DesiredDigest: desired, Found: true,
		SourceMatched: membership.SourceSystemID == system.ID && membership.SourceRecordID == record.SourceRecordID}
	return t.applyOwnershipPlan(ctx, organizationID, system, record.SourceRecordID, membershipResourceType, plan)
}

func (t *GroupTarget) applyOwnershipPlan(ctx context.Context, organizationID string, system SourceSystem, sourceRecordID, resourceType string, plan TargetPlan) (TargetPlan, error) {
	if !plan.SourceMatched {
		plan.Conflict, plan.ConflictReason = true, "target belongs to another managed record"
		return plan, nil
	}
	ownership, err := t.guard.ImportedResourceOwnership(ctx, organizationID, resourceType, plan.TargetID)
	if err == nil {
		if ownership.SourceSystemID != system.ID || ownership.SourceRecordID != sourceRecordID {
			plan.Conflict, plan.ConflictReason = true, "target ownership belongs to another source record"
		} else if !ownership.WriteLocked && plan.CurrentDigest != plan.DesiredDigest {
			plan.Conflict, plan.ConflictReason = true, "target ownership was claimed locally"
		}
	} else if !errors.Is(err, guard.ErrNotFound) {
		return TargetPlan{}, groupTargetStoreError("target ownership could not be read", err)
	}
	return plan, nil
}

func (t *GroupTarget) Apply(ctx context.Context, authentication guard.Authentication, system SourceSystem, item Item) (TargetResult, error) {
	switch item.Record.Kind {
	case RecordGroup:
		return t.applyGroup(ctx, authentication, system, item)
	case RecordMembership:
		return t.applyMembership(ctx, authentication, system, item)
	default:
		return TargetResult{}, ErrInvalidInput
	}
}

func (t *GroupTarget) applyGroup(ctx context.Context, authentication guard.Authentication, system SourceSystem, item Item) (TargetResult, error) {
	desiredDigest := groupDigest(item.Record)
	existing, err := t.store.GetManagedGroup(ctx, item.OrganizationID, item.TargetID)
	if err == nil {
		if existing.SourceSystemID != system.ID || existing.SourceRecordID != item.Record.SourceRecordID {
			return TargetResult{}, conflictError("group ownership does not match the persisted plan")
		}
		currentDigest := existingGroupDigest(existing)
		if err := t.verifyOwnership(ctx, item, system, groupResourceType, currentDigest == desiredDigest); err != nil {
			return TargetResult{}, err
		}
		if currentDigest == desiredDigest {
			if err := t.registerOwnership(ctx, authentication, system, item, groupResourceType); err != nil {
				return TargetResult{}, err
			}
			return TargetResult{TargetID: existing.ID, Revision: existing.Revision, Digest: currentDigest}, nil
		}
		if currentDigest != item.ObservedTargetDigest || existing.Revision != item.ExpectedRevision {
			return TargetResult{}, conflictError("group changed after preview")
		}
		updated := managedGroup(item, system, existing.CreatedAt, existing.Revision+1, t.now())
		updated, err = t.store.ReconcileManagedGroup(ctx, updated, existing.Revision)
		if err != nil {
			return TargetResult{}, groupTargetStoreError("group could not be persisted", err)
		}
		if err := t.registerOwnership(ctx, authentication, system, item, groupResourceType); err != nil {
			return TargetResult{}, err
		}
		return TargetResult{TargetID: updated.ID, Revision: updated.Revision, Digest: existingGroupDigest(updated), Changed: true}, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return TargetResult{}, groupTargetStoreError("group could not be read", err)
	}
	if item.Action != ActionCreate {
		return TargetResult{}, conflictError("group no longer exists")
	}
	created, err := t.store.CreateManagedGroup(ctx, managedGroup(item, system, t.now(), 1, t.now()))
	if err != nil {
		return TargetResult{}, groupTargetStoreError("group could not be persisted", err)
	}
	if err := t.registerOwnership(ctx, authentication, system, item, groupResourceType); err != nil {
		rollbackErr := t.store.DeleteManagedGroup(ctx, item.OrganizationID, item.TargetID, created.Revision)
		return TargetResult{}, errors.Join(err, rollbackErr)
	}
	return TargetResult{TargetID: created.ID, Revision: created.Revision, Digest: existingGroupDigest(created), Changed: true}, nil
}

func (t *GroupTarget) applyMembership(ctx context.Context, authentication guard.Authentication, system SourceSystem, item Item) (TargetResult, error) {
	desiredDigest := membershipDigest(item.Record)
	existing, err := t.store.GetManagedMembership(ctx, item.OrganizationID, item.TargetID)
	if err == nil {
		if existing.SourceSystemID != system.ID || existing.SourceRecordID != item.Record.SourceRecordID {
			return TargetResult{}, conflictError("membership ownership does not match the persisted plan")
		}
		currentDigest := existingMembershipDigest(existing)
		if err := t.verifyOwnership(ctx, item, system, membershipResourceType, currentDigest == desiredDigest); err != nil {
			return TargetResult{}, err
		}
		if currentDigest == desiredDigest {
			if err := t.registerOwnership(ctx, authentication, system, item, membershipResourceType); err != nil {
				return TargetResult{}, err
			}
			return TargetResult{TargetID: existing.ID, Revision: existing.Revision, Digest: currentDigest}, nil
		}
		if currentDigest != item.ObservedTargetDigest || existing.Revision != item.ExpectedRevision {
			return TargetResult{}, conflictError("membership changed after preview")
		}
		updated := managedMembership(item, system, existing.CreatedAt, existing.Revision+1, t.now())
		updated, err = t.store.ReconcileManagedMembership(ctx, updated, existing.Revision)
		if err != nil {
			return TargetResult{}, groupTargetStoreError("membership could not be persisted", err)
		}
		if err := t.registerOwnership(ctx, authentication, system, item, membershipResourceType); err != nil {
			return TargetResult{}, err
		}
		return TargetResult{TargetID: updated.ID, Revision: updated.Revision, Digest: existingMembershipDigest(updated), Changed: true}, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return TargetResult{}, groupTargetStoreError("membership could not be read", err)
	}
	if item.Action != ActionCreate {
		return TargetResult{}, conflictError("membership no longer exists")
	}
	created, err := t.store.CreateManagedMembership(ctx, managedMembership(item, system, t.now(), 1, t.now()))
	if err != nil {
		return TargetResult{}, groupTargetStoreError("membership could not be persisted", err)
	}
	if err := t.registerOwnership(ctx, authentication, system, item, membershipResourceType); err != nil {
		rollbackErr := t.store.DeleteManagedMembership(ctx, item.OrganizationID, item.TargetID, created.Revision)
		return TargetResult{}, errors.Join(err, rollbackErr)
	}
	return TargetResult{TargetID: created.ID, Revision: created.Revision, Digest: existingMembershipDigest(created), Changed: true}, nil
}

func (t *GroupTarget) verifyOwnership(ctx context.Context, item Item, system SourceSystem, resourceType string, unchanged bool) error {
	ownership, err := t.guard.ImportedResourceOwnership(ctx, item.OrganizationID, resourceType, item.TargetID)
	if errors.Is(err, guard.ErrNotFound) {
		return nil
	}
	if err != nil {
		return groupTargetStoreError("target ownership could not be read", err)
	}
	if ownership.SourceSystemID != system.ID || ownership.SourceRecordID != item.Record.SourceRecordID || (!ownership.WriteLocked && !unchanged) {
		return conflictError("target ownership changed after preview")
	}
	return nil
}

func (t *GroupTarget) registerOwnership(ctx context.Context, authentication guard.Authentication, system SourceSystem, item Item, resourceType string) error {
	_, _, err := t.guard.RegisterImportedResourceOwnership(ctx, authentication.Principal.Subject, guard.ResourceOwnershipInput{
		ResourceType: resourceType, ResourceID: item.TargetID, SourceSystemID: system.ID, SourceRecordID: item.Record.SourceRecordID,
	})
	if err != nil {
		return groupTargetStoreError("target ownership could not be recorded", err)
	}
	return nil
}

func (t *GroupTarget) Compensate(ctx context.Context, _ guard.Authentication, system SourceSystem, item Item, result TargetResult) error {
	if item.Action != ActionCreate || !result.Changed {
		return nil
	}
	resourceType := groupResourceType
	if item.Record.Kind == RecordMembership {
		resourceType = membershipResourceType
	}
	ownership, err := t.guard.ImportedResourceOwnership(ctx, item.OrganizationID, resourceType, item.TargetID)
	if err != nil {
		return err
	}
	if ownership.SourceSystemID != system.ID || ownership.SourceRecordID != item.Record.SourceRecordID || !ownership.WriteLocked {
		return conflictError("created target ownership changed before compensation")
	}
	if item.Record.Kind == RecordGroup {
		err = t.store.DeleteManagedGroup(ctx, item.OrganizationID, item.TargetID, result.Revision)
	} else {
		err = t.store.DeleteManagedMembership(ctx, item.OrganizationID, item.TargetID, result.Revision)
	}
	if err != nil {
		return groupTargetStoreError("created target could not be compensated", err)
	}
	return t.guard.DeleteImportedResourceOwnership(ctx, ownership)
}

func managedGroup(item Item, system SourceSystem, createdAt time.Time, revision uint64, updatedAt time.Time) ManagedGroup {
	return ManagedGroup{ID: item.TargetID, OrganizationID: item.OrganizationID, SourceSystemID: system.ID,
		SourceRecordID: item.Record.SourceRecordID, Name: item.Record.GroupName, DisplayName: item.Record.DisplayName,
		Description: item.Record.Description, Status: item.Record.Status, Metadata: cloneMetadata(item.Record.NormalizedMetadata),
		Revision: revision, CreatedAt: createdAt, UpdatedAt: updatedAt}
}

func managedMembership(item Item, system SourceSystem, createdAt time.Time, revision uint64, updatedAt time.Time) ManagedMembership {
	memberIDKind := "subject"
	if item.Record.MemberKind == MemberGroup {
		memberIDKind = "group"
	}
	return ManagedMembership{ID: item.TargetID, OrganizationID: item.OrganizationID, SourceSystemID: system.ID,
		SourceRecordID: item.Record.SourceRecordID,
		GroupID:        stableManagedID("group", item.OrganizationID, system.ID, item.Record.GroupSourceID), GroupSourceID: item.Record.GroupSourceID,
		MemberID: stableManagedID(memberIDKind, item.OrganizationID, system.ID, item.Record.MemberSourceID), MemberSourceID: item.Record.MemberSourceID,
		MemberKind: item.Record.MemberKind, MemberDisplayName: item.Record.DisplayName, Status: item.Record.Status,
		Metadata: cloneMetadata(item.Record.NormalizedMetadata), Revision: revision, CreatedAt: createdAt, UpdatedAt: updatedAt}
}

func groupDigest(record Record) string {
	return digestJSON(struct {
		Name, DisplayName, Description, Status string
		Metadata                               map[string]string
	}{record.GroupName, record.DisplayName, record.Description, record.Status, record.NormalizedMetadata})
}

func existingGroupDigest(group ManagedGroup) string {
	return digestJSON(struct {
		Name, DisplayName, Description, Status string
		Metadata                               map[string]string
	}{group.Name, group.DisplayName, group.Description, group.Status, group.Metadata})
}

func membershipDigest(record Record) string {
	return digestJSON(struct {
		GroupSourceID, MemberSourceID string
		MemberKind                    MemberKind
		DisplayName, Status           string
		Metadata                      map[string]string
	}{record.GroupSourceID, record.MemberSourceID, record.MemberKind, record.DisplayName, record.Status, record.NormalizedMetadata})
}

func existingMembershipDigest(membership ManagedMembership) string {
	return digestJSON(struct {
		GroupSourceID, MemberSourceID string
		MemberKind                    MemberKind
		DisplayName, Status           string
		Metadata                      map[string]string
	}{membership.GroupSourceID, membership.MemberSourceID, membership.MemberKind, membership.MemberDisplayName, membership.Status, membership.Metadata})
}

func stableManagedID(kind, organizationID, sourceSystemID, sourceRecordID string) string {
	return digestStrings("directory."+kind, organizationID, sourceSystemID, sourceRecordID)[:32]
}

func groupTargetStoreError(message string, err error) error {
	switch {
	case errors.Is(err, ErrConflict):
		return conflictError(message)
	case errors.Is(err, ErrInvalidInput):
		return &ClassifiedError{Class: FailurePermanent, Message: message, Cause: err}
	default:
		return &ClassifiedError{Class: FailureTransient, Retryable: true, Message: message, Cause: err}
	}
}

func conflictError(message string) error {
	return &ClassifiedError{Class: FailureConflict, Message: message, Cause: ErrConflict}
}

func cloneMetadata(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	copy := make(map[string]string, len(values))
	for key, value := range values {
		copy[key] = value
	}
	return copy
}
