package directoryexpansion

// Requirement: REQ-DIRECTORY-EXPANSION-002. Feature: integrations.protocols.

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/people"
)

const peopleIdentityResourceType = "people.identity"

type PeopleTarget struct {
	store people.Store
	guard *guard.Service
	now   func() time.Time
}

func NewPeopleTarget(store people.Store, guardService *guard.Service, now func() time.Time) (*PeopleTarget, error) {
	if store == nil || guardService == nil {
		return nil, errors.New("people store and Guard service are required")
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &PeopleTarget{store: store, guard: guardService, now: now}, nil
}

func (t *PeopleTarget) Preview(ctx context.Context, organizationID string, system SourceSystem, record Record, mapping *Mapping) (TargetPlan, error) {
	desired := identityDigest(record)
	provider := peopleProvider(system)
	var identity people.Identity
	var err error
	sourceMatched := false
	if mapping != nil {
		identity, err = t.store.GetIdentity(ctx, organizationID, mapping.TargetID)
		sourceMatched = err == nil && identity.Provider == provider && identity.ProviderSubject == record.SourceRecordID
	} else {
		identity, err = t.store.GetIdentityByProvider(ctx, organizationID, provider, record.SourceRecordID)
		sourceMatched = err == nil
		if errors.Is(err, people.ErrNotFound) && record.Email != "" {
			identity, err = t.store.GetIdentityByEmail(ctx, organizationID, record.Email)
			sourceMatched = err == nil && identity.Provider == provider && identity.ProviderSubject == record.SourceRecordID
		}
	}
	if errors.Is(err, people.ErrNotFound) {
		return TargetPlan{TargetID: stableTargetID(organizationID, system.ID, record.SourceRecordID), DesiredDigest: desired}, nil
	}
	if err != nil {
		return TargetPlan{}, &ClassifiedError{Class: FailureTransient, Retryable: true, Message: "target identity could not be read", Cause: err}
	}
	plan := TargetPlan{TargetID: identity.ID, Revision: identity.Revision, CurrentDigest: existingIdentityDigest(identity),
		DesiredDigest: desired, Found: true, SourceMatched: sourceMatched}
	if !sourceMatched {
		plan.Conflict, plan.ConflictReason = true, "email belongs to another managed or local identity"
		return plan, nil
	}
	ownership, ownershipErr := t.guard.ImportedResourceOwnership(ctx, organizationID, peopleIdentityResourceType, identity.ID)
	if ownershipErr == nil {
		if ownership.SourceSystemID != system.ID || ownership.SourceRecordID != record.SourceRecordID {
			plan.Conflict, plan.ConflictReason = true, "target ownership belongs to another source record"
		} else if !ownership.WriteLocked && plan.CurrentDigest != plan.DesiredDigest {
			plan.Conflict, plan.ConflictReason = true, "target ownership was claimed locally"
		}
	} else if !errors.Is(ownershipErr, guard.ErrNotFound) {
		return TargetPlan{}, &ClassifiedError{Class: FailureTransient, Retryable: true, Message: "target ownership could not be read", Cause: ownershipErr}
	}
	return plan, nil
}

func (t *PeopleTarget) Apply(ctx context.Context, authentication guard.Authentication, system SourceSystem, item Item) (TargetResult, error) {
	provider := peopleProvider(system)
	desiredDigest := identityDigest(item.Record)
	existing, err := t.store.GetIdentity(ctx, item.OrganizationID, item.TargetID)
	if err == nil {
		currentDigest := existingIdentityDigest(existing)
		if existing.Provider != provider || existing.ProviderSubject != item.Record.SourceRecordID {
			return TargetResult{}, &ClassifiedError{Class: FailureConflict, Message: "target ownership does not match the persisted plan", Cause: ErrConflict}
		}
		ownership, ownershipErr := t.guard.ImportedResourceOwnership(ctx, item.OrganizationID, peopleIdentityResourceType, item.TargetID)
		if ownershipErr == nil {
			if ownership.SourceSystemID != system.ID || ownership.SourceRecordID != item.Record.SourceRecordID || (!ownership.WriteLocked && item.Action != ActionUnchanged) {
				return TargetResult{}, &ClassifiedError{Class: FailureConflict, Message: "target ownership changed after preview", Cause: ErrConflict}
			}
		} else if !errors.Is(ownershipErr, guard.ErrNotFound) {
			return TargetResult{}, &ClassifiedError{Class: FailureTransient, Retryable: true, Message: "target ownership could not be read", Cause: ownershipErr}
		}
		if currentDigest == desiredDigest {
			if ownershipErr == nil && !ownership.WriteLocked {
				return TargetResult{TargetID: existing.ID, Revision: existing.Revision, Digest: currentDigest}, nil
			}
			if err := t.registerOwnership(ctx, authentication, system, item); err != nil {
				return TargetResult{}, err
			}
			return TargetResult{TargetID: existing.ID, Revision: existing.Revision, Digest: currentDigest}, nil
		}
		if currentDigest != item.ObservedTargetDigest || existing.Revision != item.ExpectedRevision {
			return TargetResult{}, &ClassifiedError{Class: FailureConflict, Message: "target changed after preview", Cause: ErrConflict}
		}
		updated := importedIdentity(item, system, existing.CreatedAt, existing.Revision+1, t.now())
		updated, err = t.store.ReconcileIdentity(ctx, updated, existing.Revision)
		if err != nil {
			return TargetResult{}, targetStoreError(err)
		}
		if err := t.registerOwnership(ctx, authentication, system, item); err != nil {
			return TargetResult{}, err
		}
		return TargetResult{TargetID: updated.ID, Revision: updated.Revision, Digest: existingIdentityDigest(updated), Changed: true}, nil
	}
	if !errors.Is(err, people.ErrNotFound) {
		return TargetResult{}, targetStoreError(err)
	}
	if item.Action != ActionCreate {
		return TargetResult{}, &ClassifiedError{Class: FailureConflict, Message: "target no longer exists", Cause: ErrConflict}
	}
	created := importedIdentity(item, system, t.now(), 1, t.now())
	created, err = t.store.CreateIdentity(ctx, created)
	if err != nil {
		// A previous attempt may have committed the deterministic identity before
		// losing its batch lease. The next retry will verify it exactly.
		return TargetResult{}, targetStoreError(err)
	}
	if err := t.registerOwnership(ctx, authentication, system, item); err != nil {
		rollbackErr := t.store.DeleteIdentity(ctx, item.OrganizationID, item.TargetID, created.Revision)
		return TargetResult{}, errors.Join(err, rollbackErr)
	}
	return TargetResult{TargetID: created.ID, Revision: created.Revision, Digest: existingIdentityDigest(created), Changed: true}, nil
}

func (t *PeopleTarget) Compensate(ctx context.Context, _ guard.Authentication, system SourceSystem, item Item, result TargetResult) error {
	if item.Action != ActionCreate || !result.Changed {
		return nil
	}
	ownership, err := t.guard.ImportedResourceOwnership(ctx, item.OrganizationID, peopleIdentityResourceType, item.TargetID)
	if err != nil {
		return err
	}
	if ownership.SourceSystemID != system.ID || ownership.SourceRecordID != item.Record.SourceRecordID || !ownership.WriteLocked {
		return &ClassifiedError{Class: FailureConflict, Message: "created target ownership changed before compensation", Cause: ErrConflict}
	}
	if err := t.store.DeleteIdentity(ctx, item.OrganizationID, item.TargetID, result.Revision); err != nil {
		return targetStoreError(err)
	}
	// Delete the ownership only after the target is gone. If this cleanup fails,
	// the orphaned write lock is fail-safe and a retry can repair the same
	// deterministic target; the inverse ordering could leave an unlocked record.
	return t.guard.DeleteImportedResourceOwnership(ctx, ownership)
}

func (t *PeopleTarget) registerOwnership(ctx context.Context, authentication guard.Authentication, system SourceSystem, item Item) error {
	_, _, err := t.guard.RegisterImportedResourceOwnership(ctx, authentication.Principal.Subject, guard.ResourceOwnershipInput{
		ResourceType: peopleIdentityResourceType, ResourceID: item.TargetID, SourceSystemID: system.ID, SourceRecordID: item.Record.SourceRecordID,
	})
	if err != nil {
		return &ClassifiedError{Class: FailureTransient, Retryable: true, Message: "target ownership could not be recorded", Cause: err}
	}
	return nil
}

func importedIdentity(item Item, system SourceSystem, createdAt time.Time, revision uint64, updatedAt time.Time) people.Identity {
	return people.Identity{ID: item.TargetID, OrganizationID: item.OrganizationID, Kind: people.IdentityKind(item.Record.IdentityKind),
		DisplayName: item.Record.DisplayName, NormalizedName: strings.ToLower(item.Record.DisplayName), Email: item.Record.Email,
		NormalizedEmail: item.Record.Email, Status: people.RecordStatus(item.Record.Status), Provider: peopleProvider(system),
		ProviderSubject: item.Record.SourceRecordID, Revision: revision, CreatedAt: createdAt, UpdatedAt: updatedAt}
}

func peopleProvider(system SourceSystem) string { return "directory." + digestStrings(system.ID)[:16] }
func stableTargetID(organizationID, sourceSystemID, sourceRecordID string) string {
	return digestStrings("people.identity", organizationID, sourceSystemID, sourceRecordID)[:32]
}
func identityDigest(record Record) string {
	return digestJSON(struct{ Kind, DisplayName, Email, Status string }{record.IdentityKind, record.DisplayName, record.Email, record.Status})
}
func existingIdentityDigest(identity people.Identity) string {
	return digestJSON(struct{ Kind, DisplayName, Email, Status string }{string(identity.Kind), identity.DisplayName, identity.Email, string(identity.Status)})
}

func targetStoreError(err error) error {
	switch {
	case errors.Is(err, people.ErrConflict):
		return &ClassifiedError{Class: FailureConflict, Message: "target conflicts with another identity", Cause: err}
	case errors.Is(err, people.ErrInvalidInput), errors.Is(err, people.ErrReferenceMissing):
		return &ClassifiedError{Class: FailurePermanent, Message: "target rejected the normalized identity", Cause: err}
	default:
		return &ClassifiedError{Class: FailureTransient, Retryable: true, Message: "target identity could not be persisted", Cause: err}
	}
}
