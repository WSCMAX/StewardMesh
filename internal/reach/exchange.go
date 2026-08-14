package reach

// Private Exchange import capability.
// Requirements: REQ-REACH-001, REQ-EXCHANGE-001.
// Features: messaging.delivery, migration.packages. GitHub: #9, #12.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/portabletime"
)

type exchangeImporter struct{ service *Service }

func (*exchangeImporter) reachExchangeImporter() {}

func (s *Service) OwnsExchangeImporter(candidate ExchangeImporter) bool {
	importer, ok := candidate.(*exchangeImporter)
	return ok && importer != nil && importer.service == s
}

func (s *Service) ExchangeSnapshot(ctx context.Context, maximum int) (ExchangeSnapshot, error) {
	if maximum < 1 {
		return ExchangeSnapshot{}, ErrInvalidInput
	}
	return s.store.ExchangeSnapshot(ctx, s.organizationID, maximum)
}

func (s *Service) ExchangeProvider(ctx context.Context, id string) (Provider, error) {
	id = strings.TrimSpace(id)
	if !stableIDPattern.MatchString(id) {
		return Provider{}, ErrInvalidInput
	}
	return s.store.GetProvider(ctx, s.organizationID, id)
}

func (s *Service) ExchangeTemplate(ctx context.Context, id string) (Template, error) {
	id = strings.TrimSpace(id)
	if !stableIDPattern.MatchString(id) {
		return Template{}, ErrInvalidInput
	}
	return s.store.GetTemplate(ctx, s.organizationID, id)
}

func (s *Service) ExchangeGroup(ctx context.Context, id string) (SubscriberGroup, error) {
	id = strings.TrimSpace(id)
	if !stableIDPattern.MatchString(id) {
		return SubscriberGroup{}, ErrInvalidInput
	}
	return s.store.GetGroup(ctx, s.organizationID, id)
}

func (s *Service) checkWrite(ctx context.Context, recordType, id string) error {
	if s.writes == nil {
		return nil
	}
	return s.writes.CheckResourceWrite(ctx, recordType, id)
}

func (i *exchangeImporter) ImportProvider(ctx context.Context, operation ExchangeImportOperation, candidate Provider) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeOperation(operation)
	if err != nil || candidate.OrganizationID != "" && candidate.OrganizationID != i.service.organizationID || candidate.CreatedBy != "" || candidate.UpdatedBy != "" ||
		candidate.EndpointID != "" || candidate.SecretRef != "" || candidate.SecretConfigured || candidate.Enabled {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	candidate.OrganizationID, candidate.CreatedBy, candidate.UpdatedBy = i.service.organizationID, "system:exchange", "system:exchange"
	if !validExchangeProvider(candidate) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	if existing, readErr := i.service.store.GetProvider(ctx, i.service.organizationID, candidate.ID); readErr == nil {
		if !sameExchangeProvider(existing, candidate) {
			return ExchangeImportResult{}, ErrConflict
		}
		err = i.service.auditExchange(ctx, operation, "reach.provider.imported", "reach_provider", existing.ID, map[string]string{
			"kind": string(existing.Kind), "revision": strconv.FormatInt(existing.Revision, 10), "deploymentState": "unconfigured",
		})
		return ExchangeImportResult{Committed: true}, err
	} else if !errors.Is(readErr, ErrNotFound) {
		return ExchangeImportResult{}, readErr
	}
	persisted, created, writeErr := i.service.store.ImportProvider(ctx, candidate)
	result := ExchangeImportResult{Committed: persisted.ID != "", Created: created}
	if writeErr != nil {
		if observed, readErr := i.service.store.GetProvider(ctx, i.service.organizationID, candidate.ID); readErr == nil && sameExchangeProvider(observed, candidate) {
			result.Committed = true
			auditErr := i.service.auditExchange(ctx, operation, "reach.provider.imported", "reach_provider", observed.ID, map[string]string{
				"kind": string(observed.Kind), "revision": strconv.FormatInt(observed.Revision, 10), "deploymentState": "unconfigured",
			})
			return result, errors.Join(writeErr, auditErr)
		}
		return result, writeErr
	}
	if !sameExchangeProvider(persisted, candidate) {
		return ExchangeImportResult{}, ErrConflict
	}
	err = i.service.auditExchange(ctx, operation, "reach.provider.imported", "reach_provider", persisted.ID, map[string]string{
		"kind": string(persisted.Kind), "revision": strconv.FormatInt(persisted.Revision, 10), "deploymentState": "unconfigured",
	})
	return ExchangeImportResult{Committed: true, Created: created}, err
}

func (i *exchangeImporter) ImportTemplate(ctx context.Context, operation ExchangeImportOperation, candidate Template) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeOperation(operation)
	if err != nil || candidate.OrganizationID != "" && candidate.OrganizationID != i.service.organizationID || candidate.CreatedBy != "" || candidate.UpdatedBy != "" {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	candidate.OrganizationID, candidate.CreatedBy, candidate.UpdatedBy = i.service.organizationID, "system:exchange", "system:exchange"
	if !validExchangeTemplate(candidate) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	if existing, readErr := i.service.store.GetTemplate(ctx, i.service.organizationID, candidate.ID); readErr == nil {
		if !sameExchangeTemplate(existing, candidate) {
			return ExchangeImportResult{}, ErrConflict
		}
		err = i.service.auditExchange(ctx, operation, "reach.template.imported", "reach_template", existing.ID, map[string]string{"revision": strconv.FormatInt(existing.Revision, 10)})
		return ExchangeImportResult{Committed: true}, err
	} else if !errors.Is(readErr, ErrNotFound) {
		return ExchangeImportResult{}, readErr
	}
	persisted, created, writeErr := i.service.store.ImportTemplate(ctx, candidate)
	result := ExchangeImportResult{Committed: persisted.ID != "", Created: created}
	if writeErr != nil {
		if observed, readErr := i.service.store.GetTemplate(ctx, i.service.organizationID, candidate.ID); readErr == nil && sameExchangeTemplate(observed, candidate) {
			result.Committed = true
			auditErr := i.service.auditExchange(ctx, operation, "reach.template.imported", "reach_template", observed.ID, map[string]string{"revision": strconv.FormatInt(observed.Revision, 10)})
			return result, errors.Join(writeErr, auditErr)
		}
		return result, writeErr
	}
	if !sameExchangeTemplate(persisted, candidate) {
		return ExchangeImportResult{}, ErrConflict
	}
	err = i.service.auditExchange(ctx, operation, "reach.template.imported", "reach_template", persisted.ID, map[string]string{"revision": strconv.FormatInt(persisted.Revision, 10)})
	return ExchangeImportResult{Committed: true, Created: created}, err
}

func (i *exchangeImporter) ImportGroup(ctx context.Context, operation ExchangeImportOperation, candidate SubscriberGroup) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeOperation(operation)
	if err != nil || candidate.OrganizationID != "" && candidate.OrganizationID != i.service.organizationID || candidate.CreatedBy != "" || candidate.UpdatedBy != "" {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	candidate.OrganizationID, candidate.CreatedBy, candidate.UpdatedBy = i.service.organizationID, "system:exchange", "system:exchange"
	if !validExchangeGroup(candidate) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	if existing, readErr := i.service.store.GetGroup(ctx, i.service.organizationID, candidate.ID); readErr == nil {
		if !sameExchangeGroup(existing, candidate) {
			return ExchangeImportResult{}, ErrConflict
		}
		err = i.service.auditExchange(ctx, operation, "reach.group.imported", "reach_subscriber_group", existing.ID, map[string]string{
			"recipientCount": strconv.Itoa(len(existing.Recipients)), "revision": strconv.FormatInt(existing.Revision, 10),
		})
		return ExchangeImportResult{Committed: true}, err
	} else if !errors.Is(readErr, ErrNotFound) {
		return ExchangeImportResult{}, readErr
	}
	provider, err := i.service.store.GetProvider(ctx, i.service.organizationID, candidate.ProviderID)
	if errors.Is(err, ErrNotFound) {
		return ExchangeImportResult{}, ErrReferenceMissing
	}
	if err != nil {
		return ExchangeImportResult{}, err
	}
	if _, err := i.service.store.GetTemplate(ctx, i.service.organizationID, candidate.TemplateID); errors.Is(err, ErrNotFound) {
		return ExchangeImportResult{}, ErrReferenceMissing
	} else if err != nil {
		return ExchangeImportResult{}, err
	}
	if !portableRecipientCompatibility(provider.Kind, candidate.Recipients) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	persisted, created, writeErr := i.service.store.ImportGroup(ctx, candidate)
	result := ExchangeImportResult{Committed: persisted.ID != "", Created: created}
	if writeErr != nil {
		if observed, readErr := i.service.store.GetGroup(ctx, i.service.organizationID, candidate.ID); readErr == nil && sameExchangeGroup(observed, candidate) {
			result.Committed = true
			auditErr := i.service.auditExchange(ctx, operation, "reach.group.imported", "reach_subscriber_group", observed.ID, map[string]string{
				"recipientCount": strconv.Itoa(len(observed.Recipients)), "revision": strconv.FormatInt(observed.Revision, 10),
			})
			return result, errors.Join(writeErr, auditErr)
		}
		return result, writeErr
	}
	if !sameExchangeGroup(persisted, candidate) {
		return ExchangeImportResult{}, ErrConflict
	}
	err = i.service.auditExchange(ctx, operation, "reach.group.imported", "reach_subscriber_group", persisted.ID, map[string]string{
		"recipientCount": strconv.Itoa(len(persisted.Recipients)), "revision": strconv.FormatInt(persisted.Revision, 10),
	})
	return ExchangeImportResult{Committed: true, Created: created}, err
}

func normalizeExchangeOperation(operation ExchangeImportOperation) (ExchangeImportOperation, error) {
	operation.Token = strings.TrimSpace(operation.Token)
	operation.OccurredAt = portabletime.Normalize(operation.OccurredAt)
	if !stableIDPattern.MatchString(operation.Token) || !validExchangeTime(operation.OccurredAt) {
		return ExchangeImportOperation{}, ErrInvalidInput
	}
	return operation, nil
}

func validExchangeProvider(candidate Provider) bool {
	return stableIDPattern.MatchString(candidate.ID) && candidate.Name == strings.TrimSpace(candidate.Name) && validText(candidate.Name, 1, 160) &&
		validProviderKind(candidate.Kind) && validSender(candidate.Kind, candidate.Sender) && candidate.EndpointID == "" && candidate.SecretRef == "" &&
		!candidate.SecretConfigured && !candidate.Enabled && validExchangeRevisionTimes(candidate.Revision, candidate.CreatedAt, candidate.UpdatedAt)
}

func validExchangeTemplate(candidate Template) bool {
	return stableIDPattern.MatchString(candidate.ID) && candidate.Name == strings.TrimSpace(candidate.Name) && candidate.Subject == strings.TrimSpace(candidate.Subject) &&
		candidate.Body == strings.TrimSpace(candidate.Body) && validText(candidate.Name, 1, 160) && validateTemplate(candidate.Subject, candidate.Body) == nil &&
		validExchangeRevisionTimes(candidate.Revision, candidate.CreatedAt, candidate.UpdatedAt)
}

func validExchangeGroup(candidate SubscriberGroup) bool {
	normalized, err := normalizeRecipients(candidate.Recipients)
	return err == nil && stableIDPattern.MatchString(candidate.ID) && candidate.Name == strings.TrimSpace(candidate.Name) && validText(candidate.Name, 1, 160) &&
		stableIDPattern.MatchString(candidate.ProviderID) && stableIDPattern.MatchString(candidate.TemplateID) && slices.Equal(normalized, candidate.Recipients) &&
		validExchangeRevisionTimes(candidate.Revision, candidate.CreatedAt, candidate.UpdatedAt)
}

func portableRecipientCompatibility(kind ProviderKind, recipients []Recipient) bool {
	switch kind {
	case ProviderTeams:
		return len(recipients) == 1 && recipients[0].Kind == RecipientChannel && stableIDPattern.MatchString(recipients[0].Address)
	case ProviderSMTP, ProviderSES, ProviderGmail, ProviderOutlook:
		for _, recipient := range recipients {
			if recipient.Kind != RecipientEmail {
				return false
			}
		}
		return len(recipients) > 0
	case ProviderWebhook:
		return len(recipients) > 0
	default:
		return false
	}
}

func validExchangeRevisionTimes(revision int64, createdAt, updatedAt time.Time) bool {
	return revision > 0 && validExchangeTime(createdAt) && validExchangeTime(updatedAt) && !updatedAt.Before(createdAt) &&
		(revision != 1 || createdAt.Equal(updatedAt))
}

func validExchangeTime(value time.Time) bool {
	return !value.IsZero() && value.Year() >= 1970 && value.Year() <= 9999 && portabletime.IsCanonical(value)
}

func sameExchangeProvider(left, right Provider) bool {
	return left.ID == right.ID && left.Name == right.Name && left.Kind == right.Kind && left.Sender == right.Sender && left.EndpointID == "" &&
		left.SecretRef == "" && !left.SecretConfigured && !left.Enabled && left.Revision == right.Revision && left.CreatedAt.Equal(right.CreatedAt) && left.UpdatedAt.Equal(right.UpdatedAt)
}

func sameExchangeTemplate(left, right Template) bool {
	return left.ID == right.ID && left.Name == right.Name && left.Subject == right.Subject && left.Body == right.Body && left.Revision == right.Revision &&
		left.CreatedAt.Equal(right.CreatedAt) && left.UpdatedAt.Equal(right.UpdatedAt)
}

func sameExchangeGroup(left, right SubscriberGroup) bool {
	return left.ID == right.ID && left.Name == right.Name && left.ProviderID == right.ProviderID && left.TemplateID == right.TemplateID &&
		slices.Equal(left.Recipients, right.Recipients) && left.Revision == right.Revision && left.CreatedAt.Equal(right.CreatedAt) && left.UpdatedAt.Equal(right.UpdatedAt)
}

func (s *Service) auditExchange(ctx context.Context, operation ExchangeImportOperation, action, resourceType, resourceID string, metadata map[string]string) error {
	metadata["requirementId"], metadata["featureId"] = RequirementID, FeatureID
	scope := foundation.Scope{OrganizationID: s.organizationID, ActorID: "system:exchange", CorrelationID: operation.Token}
	ctx = foundation.WithScope(ctx, scope)
	return s.auditor.Record(ctx, foundation.AuditEvent{
		ID: exchangeAuditIdentity(s.organizationID, operation.Token, action, resourceType, resourceID), OrganizationID: s.organizationID,
		ActorID: scope.ActorID, CorrelationID: scope.CorrelationID, Action: action, ResourceType: resourceType, ResourceID: resourceID,
		OccurredAt: operation.OccurredAt, Metadata: metadata,
	})
}

func exchangeAuditIdentity(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}
