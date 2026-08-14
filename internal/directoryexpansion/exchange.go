package directoryexpansion

// Requirements: REQ-DIRECTORY-EXPANSION-005, REQ-EXCHANGE-001. Features: integrations.protocols, migration.packages.

import (
	"context"
	"errors"
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/portabletime"
)

const exchangeActorID = "system:exchange"

func normalizeExchangeImportOperation(operation ExchangeImportOperation) (ExchangeImportOperation, error) {
	operation.Token = strings.TrimSpace(operation.Token)
	operation.OccurredAt = portabletime.Normalize(operation.OccurredAt)
	if !sourceSystemIDPattern.MatchString(operation.Token) || operation.OccurredAt.IsZero() || operation.OccurredAt.Year() < 2000 || operation.OccurredAt.Year() > 9999 {
		return ExchangeImportOperation{}, ErrInvalidInput
	}
	return operation, nil
}

func validExchangeTimeState(createdAt, updatedAt time.Time) bool {
	return !createdAt.IsZero() && !updatedAt.Before(createdAt) && portabletime.IsCanonical(createdAt) && portabletime.IsCanonical(updatedAt) &&
		createdAt.Year() >= 2000 && updatedAt.Year() <= 9999
}

func isCanonicalManagedID(value string) bool {
	return isRecordID(value) && value == strings.ToLower(value)
}

func validExchangeManagedGroup(candidate ManagedGroup) bool {
	if candidate.OrganizationID != "" || !isCanonicalManagedID(candidate.ID) || !sourceSystemIDPattern.MatchString(candidate.SourceSystemID) ||
		!validSourceRecordID(candidate.SourceRecordID) || candidate.Revision == 0 || !validExchangeTimeState(candidate.CreatedAt, candidate.UpdatedAt) {
		return false
	}
	normalized, err := normalizeRecord(Record{SourceRecordID: candidate.SourceRecordID, Kind: RecordGroup, DisplayName: candidate.DisplayName,
		GroupName: candidate.Name, Description: candidate.Description, Status: candidate.Status, NormalizedMetadata: candidate.Metadata})
	return err == nil && normalized.SourceRecordID == candidate.SourceRecordID && normalized.DisplayName == candidate.DisplayName &&
		normalized.GroupName == candidate.Name && normalized.Description == candidate.Description && normalized.Status == candidate.Status &&
		maps.Equal(normalized.NormalizedMetadata, candidate.Metadata)
}

func validExchangeManagedMembership(candidate ManagedMembership) bool {
	if candidate.OrganizationID != "" || !isCanonicalManagedID(candidate.ID) || !isCanonicalManagedID(candidate.GroupID) || !isCanonicalManagedID(candidate.MemberID) ||
		!sourceSystemIDPattern.MatchString(candidate.SourceSystemID) || !validSourceRecordID(candidate.SourceRecordID) ||
		candidate.Revision == 0 || !validExchangeTimeState(candidate.CreatedAt, candidate.UpdatedAt) {
		return false
	}
	normalized, err := normalizeRecord(Record{SourceRecordID: candidate.SourceRecordID, Kind: RecordMembership,
		DisplayName: candidate.MemberDisplayName, Status: candidate.Status, GroupSourceID: candidate.GroupSourceID,
		MemberSourceID: candidate.MemberSourceID, MemberKind: candidate.MemberKind, NormalizedMetadata: candidate.Metadata})
	return err == nil && normalized.SourceRecordID == candidate.SourceRecordID && normalized.DisplayName == candidate.MemberDisplayName &&
		normalized.Status == candidate.Status && normalized.GroupSourceID == candidate.GroupSourceID &&
		normalized.MemberSourceID == candidate.MemberSourceID && normalized.MemberKind == candidate.MemberKind &&
		maps.Equal(normalized.NormalizedMetadata, candidate.Metadata)
}

func (i *exchangeImporter) ImportManagedGroup(ctx context.Context, operation ExchangeImportOperation, candidate ManagedGroup) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	if err != nil || i == nil || i.target == nil || !validExchangeManagedGroup(candidate) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	candidate.OrganizationID = i.target.organizationID
	stored, created, err := i.target.exchangeStore.ImportManagedGroup(ctx, candidate)
	if err != nil {
		observed, readErr := i.target.exchangeStore.GetManagedGroup(ctx, i.target.organizationID, candidate.ID)
		if readErr == nil && sameManagedGroup(observed, candidate) {
			auditErr := i.target.auditExchange(ctx, operation, "directory.group.created", groupResourceType, candidate.ID,
				map[string]string{"sourceSystemId": candidate.SourceSystemID, "sourceRecordId": candidate.SourceRecordID, "revision": strconv.FormatUint(candidate.Revision, 10)})
			return ExchangeImportResult{Committed: true, Created: true}, errors.Join(err, auditErr)
		}
		return ExchangeImportResult{}, errors.Join(err, readErr)
	}
	if !sameManagedGroup(stored, candidate) {
		return ExchangeImportResult{}, ErrConflict
	}
	auditErr := i.target.auditExchange(ctx, operation, "directory.group.created", groupResourceType, candidate.ID,
		map[string]string{"sourceSystemId": candidate.SourceSystemID, "sourceRecordId": candidate.SourceRecordID, "revision": strconv.FormatUint(candidate.Revision, 10)})
	return ExchangeImportResult{Committed: true, Created: created}, auditErr
}

func (i *exchangeImporter) ImportManagedMembership(ctx context.Context, operation ExchangeImportOperation, candidate ManagedMembership) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	if err != nil || i == nil || i.target == nil || !validExchangeManagedMembership(candidate) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	candidate.OrganizationID = i.target.organizationID
	stored, created, err := i.target.exchangeStore.ImportManagedMembership(ctx, candidate)
	if err != nil {
		observed, readErr := i.target.exchangeStore.GetManagedMembership(ctx, i.target.organizationID, candidate.ID)
		if readErr == nil && sameManagedMembership(observed, candidate) {
			auditErr := i.target.auditExchange(ctx, operation, "directory.membership.created", membershipResourceType, candidate.ID,
				directoryMembershipAuditMetadata(candidate))
			return ExchangeImportResult{Committed: true, Created: true}, errors.Join(err, auditErr)
		}
		return ExchangeImportResult{}, errors.Join(err, readErr)
	}
	if !sameManagedMembership(stored, candidate) {
		return ExchangeImportResult{}, ErrConflict
	}
	auditErr := i.target.auditExchange(ctx, operation, "directory.membership.created", membershipResourceType, candidate.ID,
		directoryMembershipAuditMetadata(candidate))
	return ExchangeImportResult{Committed: true, Created: created}, auditErr
}

func directoryMembershipAuditMetadata(candidate ManagedMembership) map[string]string {
	return map[string]string{
		"sourceSystemId": candidate.SourceSystemID, "sourceRecordId": candidate.SourceRecordID,
		"groupId": candidate.GroupID, "memberId": candidate.MemberID, "memberKind": string(candidate.MemberKind),
		"revision": strconv.FormatUint(candidate.Revision, 10),
	}
}

func (t *GroupTarget) auditExchange(ctx context.Context, operation ExchangeImportOperation, action, resourceType, resourceID string, metadata map[string]string) error {
	eventID := digestStrings(t.organizationID, operation.Token, action, resourceType, resourceID)
	values := cloneMetadata(metadata)
	if values == nil {
		values = map[string]string{}
	}
	values["requirementId"] = GrouperRequirementID
	values["exchangeRequirementId"] = "REQ-EXCHANGE-001"
	return t.auditor.Record(foundation.WithScope(ctx, foundation.Scope{OrganizationID: t.organizationID, ActorID: exchangeActorID, CorrelationID: operation.Token}), foundation.AuditEvent{
		ID: eventID, OrganizationID: t.organizationID, ActorID: exchangeActorID, CorrelationID: operation.Token,
		Action: action, ResourceType: resourceType, ResourceID: resourceID, OccurredAt: operation.OccurredAt, Metadata: values,
	})
}

func sameManagedGroup(left, right ManagedGroup) bool {
	return left.ID == right.ID && left.OrganizationID == right.OrganizationID && left.SourceSystemID == right.SourceSystemID &&
		left.SourceRecordID == right.SourceRecordID && left.Name == right.Name && left.DisplayName == right.DisplayName &&
		left.Description == right.Description && left.Status == right.Status && maps.Equal(left.Metadata, right.Metadata) &&
		left.Revision == right.Revision && left.CreatedAt.Equal(right.CreatedAt) && left.UpdatedAt.Equal(right.UpdatedAt)
}

func sameManagedMembership(left, right ManagedMembership) bool {
	return left.ID == right.ID && left.OrganizationID == right.OrganizationID && left.SourceSystemID == right.SourceSystemID &&
		left.SourceRecordID == right.SourceRecordID && left.GroupID == right.GroupID && left.GroupSourceID == right.GroupSourceID &&
		left.MemberID == right.MemberID && left.MemberSourceID == right.MemberSourceID && left.MemberKind == right.MemberKind &&
		left.MemberDisplayName == right.MemberDisplayName && left.Status == right.Status && maps.Equal(left.Metadata, right.Metadata) &&
		left.Revision == right.Revision && left.CreatedAt.Equal(right.CreatedAt) && left.UpdatedAt.Equal(right.UpdatedAt)
}
