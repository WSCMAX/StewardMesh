package exchange

// Requirements: REQ-EXCHANGE-001, REQ-REACH-001, REQ-PATTERNS-001.
// Features: migration.packages, messaging.delivery, templates.schemas. GitHub: #8, #9, #12.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/reach"
)

var reachRecordTypes = []string{"reach.provider", "reach.subscriber-group", "reach.template"}

type ReachProvider struct {
	service  *reach.Service
	importer reach.ExchangeImporter
}

// The provider payload deliberately has no endpoint, URL, secret reference,
// secret state, enablement, test history, delivery state, actor, or org field.
// Imported providers are always inert until an explicit local reconfiguration.
type reachProviderPayload struct {
	CreatedAt string             `json:"createdAt"`
	Kind      reach.ProviderKind `json:"kind"`
	Name      string             `json:"name"`
	Sender    string             `json:"sender,omitempty"`
	UpdatedAt string             `json:"updatedAt"`
}

type reachTemplatePayload struct {
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
	Name      string `json:"name"`
	Subject   string `json:"subject"`
	UpdatedAt string `json:"updatedAt"`
}

type reachGroupPayload struct {
	CreatedAt  string `json:"createdAt"`
	Name       string `json:"name"`
	ProviderID string `json:"providerId"`
	Recipients string `json:"recipients"`
	TemplateID string `json:"templateId"`
	UpdatedAt  string `json:"updatedAt"`
}

func NewReachProvider(service *reach.Service, importer reach.ExchangeImporter) (*ReachProvider, error) {
	if service == nil || importer == nil || !service.OwnsExchangeImporter(importer) {
		return nil, errors.New("Reach service and its construction-time Exchange importer are required")
	}
	return &ReachProvider{service: service, importer: importer}, nil
}

func (*ReachProvider) Types() []string { return append([]string(nil), reachRecordTypes...) }

func (p *ReachProvider) ListRecords(ctx context.Context) ([]Record, error) {
	snapshot, err := p.service.ExchangeSnapshot(ctx, MaximumRecords)
	if errors.Is(err, reach.ErrTooLarge) {
		return nil, ErrTooLarge
	}
	if err != nil {
		return nil, err
	}
	result := make([]Record, 0, len(snapshot.Providers)+len(snapshot.Templates)+len(snapshot.Groups))
	for _, item := range snapshot.Providers {
		if err := validatePortableInstants(1970, item.CreatedAt, item.UpdatedAt); err != nil {
			return nil, err
		}
		payload, err := marshalReachPayload(reachProviderPayload{
			CreatedAt: reachInstant(item.CreatedAt), Kind: item.Kind, Name: item.Name, Sender: item.Sender, UpdatedAt: reachInstant(item.UpdatedAt),
		})
		if err != nil {
			return nil, err
		}
		result = append(result, Record{Type: "reach.provider", ID: item.ID, Revision: item.Revision, Dependencies: []Reference{}, Ownership: OwnershipMetadata{State: "local"}, Payload: payload})
	}
	for _, item := range snapshot.Templates {
		if err := validatePortableInstants(1970, item.CreatedAt, item.UpdatedAt); err != nil {
			return nil, err
		}
		payload, err := marshalReachPayload(reachTemplatePayload{Body: item.Body, CreatedAt: reachInstant(item.CreatedAt), Name: item.Name, Subject: item.Subject, UpdatedAt: reachInstant(item.UpdatedAt)})
		if err != nil {
			return nil, err
		}
		result = append(result, Record{Type: "reach.template", ID: item.ID, Revision: item.Revision, Dependencies: []Reference{}, Ownership: OwnershipMetadata{State: "local"}, Payload: payload})
	}
	for _, item := range snapshot.Groups {
		if err := validatePortableInstants(1970, item.CreatedAt, item.UpdatedAt); err != nil {
			return nil, err
		}
		recipients, err := encodeReachRecipients(item.Recipients)
		if err != nil {
			return nil, err
		}
		payload, err := marshalReachPayload(reachGroupPayload{
			CreatedAt: reachInstant(item.CreatedAt), Name: item.Name, ProviderID: item.ProviderID, Recipients: recipients, TemplateID: item.TemplateID, UpdatedAt: reachInstant(item.UpdatedAt),
		})
		if err != nil {
			return nil, err
		}
		result = append(result, Record{Type: "reach.subscriber-group", ID: item.ID, Revision: item.Revision,
			Dependencies: reachGroupDependencies(item), Ownership: OwnershipMetadata{State: "local"}, Payload: payload})
	}
	sort.Slice(result, func(i, j int) bool {
		return (Reference{Type: result[i].Type, ID: result[i].ID}).Key() < (Reference{Type: result[j].Type, ID: result[j].ID}).Key()
	})
	return result, nil
}

func (p *ReachProvider) Exists(ctx context.Context, reference Reference) (bool, error) {
	var err error
	switch reference.Type {
	case "reach.provider":
		_, err = p.service.ExchangeProvider(ctx, reference.ID)
	case "reach.template":
		_, err = p.service.ExchangeTemplate(ctx, reference.ID)
	case "reach.subscriber-group":
		_, err = p.service.ExchangeGroup(ctx, reference.ID)
	default:
		return false, nil
	}
	if errors.Is(err, reach.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (p *ReachProvider) ImportRecordExists(ctx context.Context, record Record, file []byte) (bool, error) {
	if len(file) != 0 {
		return false, ErrInvalidInput
	}
	candidate, dependencies, err := decodeReachRecord(record)
	if err != nil || !slices.Equal(dependencies, record.Dependencies) {
		return false, ErrInvalidInput
	}
	switch item := candidate.(type) {
	case reach.Provider:
		current, err := p.service.ExchangeProvider(ctx, item.ID)
		if errors.Is(err, reach.ErrNotFound) {
			return false, nil
		}
		return err == nil && sameReachProvider(current, item), err
	case reach.Template:
		current, err := p.service.ExchangeTemplate(ctx, item.ID)
		if errors.Is(err, reach.ErrNotFound) {
			return false, nil
		}
		return err == nil && sameReachTemplate(current, item), err
	case reach.SubscriberGroup:
		current, err := p.service.ExchangeGroup(ctx, item.ID)
		if errors.Is(err, reach.ErrNotFound) {
			return false, nil
		}
		return err == nil && sameReachGroup(current, item), err
	default:
		return false, ErrInvalidInput
	}
}

func (p *ReachProvider) ImportRecord(ctx context.Context, operation ProviderImportOperation, _ string, record Record, file []byte) (ProviderImportResult, error) {
	if len(file) != 0 {
		return ProviderImportResult{}, ErrInvalidInput
	}
	if !operation.ExpectedCreated {
		exact, err := p.ImportRecordExists(ctx, record, nil)
		if err != nil {
			return ProviderImportResult{}, err
		}
		if !exact {
			return ProviderImportResult{}, ErrConflict
		}
		return ProviderImportResult{Committed: true}, nil
	}
	candidate, dependencies, err := decodeReachRecord(record)
	if err != nil || !slices.Equal(dependencies, record.Dependencies) {
		return ProviderImportResult{}, ErrInvalidInput
	}
	domainOperation := reach.ExchangeImportOperation{Token: operation.Token, OccurredAt: operation.OccurredAt}
	var result reach.ExchangeImportResult
	switch item := candidate.(type) {
	case reach.Provider:
		result, err = p.importer.ImportProvider(ctx, domainOperation, item)
	case reach.Template:
		result, err = p.importer.ImportTemplate(ctx, domainOperation, item)
	case reach.SubscriberGroup:
		result, err = p.importer.ImportGroup(ctx, domainOperation, item)
	default:
		return ProviderImportResult{}, ErrInvalidInput
	}
	providerResult := ProviderImportResult{Committed: result.Committed, Created: result.Created}
	switch {
	case errors.Is(err, reach.ErrInvalidInput):
		return providerResult, ErrInvalidInput
	case errors.Is(err, reach.ErrConflict):
		return providerResult, ErrConflict
	case errors.Is(err, reach.ErrReferenceMissing), errors.Is(err, reach.ErrNotFound):
		return providerResult, ErrDependencyMissing
	case errors.Is(err, reach.ErrTooLarge):
		return providerResult, ErrTooLarge
	default:
		return providerResult, err
	}
}

func decodeReachRecord(record Record) (any, []Reference, error) {
	if record.Revision < 1 || !stableIDPattern.MatchString(record.ID) || record.File != nil {
		return nil, nil, ErrInvalidInput
	}
	switch record.Type {
	case "reach.provider":
		payload, err := decodeReachPayload[reachProviderPayload](record.Payload)
		createdAt, updatedAt, timeErr := parseReachStateTimes(payload.CreatedAt, payload.UpdatedAt, record.Revision)
		if err != nil || timeErr != nil || !canonicalReachText(payload.Name, 1, 160) || !canonicalReachText(payload.Sender, 0, 320) || !validReachKind(payload.Kind) {
			return nil, nil, ErrInvalidInput
		}
		candidate := reach.Provider{ID: record.ID, Name: payload.Name, Kind: payload.Kind, Sender: payload.Sender, Enabled: false,
			Revision: record.Revision, CreatedAt: createdAt, UpdatedAt: updatedAt}
		if !canonicalReachPayload(record.Payload, payload) {
			return nil, nil, ErrInvalidInput
		}
		return candidate, []Reference{}, nil
	case "reach.template":
		payload, err := decodeReachPayload[reachTemplatePayload](record.Payload)
		createdAt, updatedAt, timeErr := parseReachStateTimes(payload.CreatedAt, payload.UpdatedAt, record.Revision)
		if err != nil || timeErr != nil || !canonicalReachText(payload.Name, 1, 160) || !canonicalReachText(payload.Subject, 1, 200) || !canonicalReachText(payload.Body, 1, 4000) ||
			strings.ContainsAny(payload.Subject, "\r\n") || !canonicalReachPayload(record.Payload, payload) {
			return nil, nil, ErrInvalidInput
		}
		return reach.Template{ID: record.ID, Name: payload.Name, Subject: payload.Subject, Body: payload.Body, Revision: record.Revision, CreatedAt: createdAt, UpdatedAt: updatedAt}, []Reference{}, nil
	case "reach.subscriber-group":
		payload, err := decodeReachPayload[reachGroupPayload](record.Payload)
		createdAt, updatedAt, timeErr := parseReachStateTimes(payload.CreatedAt, payload.UpdatedAt, record.Revision)
		recipients, recipientErr := decodeReachRecipients(payload.Recipients)
		candidate := reach.SubscriberGroup{ID: record.ID, Name: payload.Name, ProviderID: payload.ProviderID, TemplateID: payload.TemplateID,
			Recipients: recipients, Revision: record.Revision, CreatedAt: createdAt, UpdatedAt: updatedAt}
		if err != nil || timeErr != nil || recipientErr != nil || !canonicalReachText(payload.Name, 1, 160) ||
			!stableIDPattern.MatchString(payload.ProviderID) || !stableIDPattern.MatchString(payload.TemplateID) || !canonicalReachPayload(record.Payload, payload) {
			return nil, nil, ErrInvalidInput
		}
		return candidate, reachGroupDependencies(candidate), nil
	default:
		return nil, nil, ErrInvalidInput
	}
}

func decodeReachPayload[T any](payload []byte) (T, error) {
	var result T
	if len(payload) == 0 || len(payload) > MaximumPayloadBytes || !utf8.Valid(payload) {
		return result, ErrInvalidInput
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return result, ErrInvalidInput
	}
	return result, nil
}

func marshalReachPayload(value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil || len(payload) == 0 || len(payload) > MaximumPayloadBytes {
		return nil, ErrInvalidInput
	}
	return payload, nil
}

func canonicalReachPayload(payload []byte, value any) bool {
	return canonicalJSONEqual(payload, value)
}

func reachGroupDependencies(value reach.SubscriberGroup) []Reference {
	return []Reference{{Type: "reach.provider", ID: value.ProviderID}, {Type: "reach.template", ID: value.TemplateID}}
}

func encodeReachRecipients(recipients []reach.Recipient) (string, error) {
	payload, err := json.Marshal(recipients)
	if err != nil || len(payload) < 2 || len(payload) > 40_000 {
		return "", ErrInvalidInput
	}
	return string(payload), nil
}

func decodeReachRecipients(value string) ([]reach.Recipient, error) {
	if value != strings.TrimSpace(value) || len(value) < 2 || len(value) > 40_000 || !utf8.ValidString(value) {
		return nil, ErrInvalidInput
	}
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	var recipients []reach.Recipient
	if err := decoder.Decode(&recipients); err != nil || len(recipients) < 1 || len(recipients) > reach.MaximumRecipients {
		return nil, ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidInput
	}
	canonical, err := encodeReachRecipients(recipients)
	if err != nil || canonical != value {
		return nil, ErrInvalidInput
	}
	for _, recipient := range recipients {
		if recipient.Address != strings.TrimSpace(recipient.Address) || recipient.Address == "" || len(recipient.Address) > 320 ||
			(recipient.Kind != reach.RecipientEmail && recipient.Kind != reach.RecipientChannel) {
			return nil, ErrInvalidInput
		}
	}
	return recipients, nil
}

func reachInstant(value time.Time) string { return value.Format(time.RFC3339Nano) }

func parseReachStateTimes(created, updated string, revision int64) (time.Time, time.Time, error) {
	createdAt, err := parseReachInstant(created)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	updatedAt, err := parseReachInstant(updated)
	if err != nil || updatedAt.Before(createdAt) || revision == 1 && !createdAt.Equal(updatedAt) {
		return time.Time{}, time.Time{}, ErrInvalidInput
	}
	return createdAt, updatedAt, nil
}

func parseReachInstant(value string) (time.Time, error) {
	return parsePortableInstant(value, 1970)
}

func canonicalReachText(value string, minimum, maximum int) bool {
	return value == strings.TrimSpace(value) && utf8.ValidString(value) && utf8.RuneCountInString(value) >= minimum && utf8.RuneCountInString(value) <= maximum && !strings.ContainsRune(value, '\x00')
}

func validReachKind(kind reach.ProviderKind) bool {
	switch kind {
	case reach.ProviderSMTP, reach.ProviderSES, reach.ProviderGmail, reach.ProviderOutlook, reach.ProviderTeams, reach.ProviderWebhook:
		return true
	default:
		return false
	}
}

func sameReachProvider(left, right reach.Provider) bool {
	return left.ID == right.ID && left.Name == right.Name && left.Kind == right.Kind && left.Sender == right.Sender && left.EndpointID == "" &&
		left.SecretRef == "" && !left.SecretConfigured && !left.Enabled && left.Revision == right.Revision && left.CreatedAt.Equal(right.CreatedAt) && left.UpdatedAt.Equal(right.UpdatedAt)
}

func sameReachTemplate(left, right reach.Template) bool {
	return left.ID == right.ID && left.Name == right.Name && left.Subject == right.Subject && left.Body == right.Body && left.Revision == right.Revision &&
		left.CreatedAt.Equal(right.CreatedAt) && left.UpdatedAt.Equal(right.UpdatedAt)
}

func sameReachGroup(left, right reach.SubscriberGroup) bool {
	return left.ID == right.ID && left.Name == right.Name && left.ProviderID == right.ProviderID && left.TemplateID == right.TemplateID &&
		slices.Equal(left.Recipients, right.Recipients) && left.Revision == right.Revision && left.CreatedAt.Equal(right.CreatedAt) && left.UpdatedAt.Equal(right.UpdatedAt)
}
