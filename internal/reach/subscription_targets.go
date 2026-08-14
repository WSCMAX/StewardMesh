package reach

// Requirements: REQ-SIGNALS-001, REQ-REACH-001. Features: alerts.rules,
// messaging.delivery. GitHub: #11, #12.

import (
	"context"
	"errors"
	"strings"

	"github.com/maxlemke/stewardmesh/internal/signals"
)

// SubscriptionTargetCatalog adapts Reach's organization-scoped configuration
// into the provider-neutral catalog consumed by Signals. A target is listed
// only while its provider is enabled and every referenced configuration record
// remains valid.
type SubscriptionTargetCatalog struct {
	store     Store
	endpoints *EndpointCatalog
}

func NewSubscriptionTargetCatalog(store Store, endpoints *EndpointCatalog) (*SubscriptionTargetCatalog, error) {
	if store == nil || endpoints == nil {
		return nil, errors.New("Reach store and endpoints are required for subscription targets")
	}
	return &SubscriptionTargetCatalog{store: store, endpoints: endpoints}, nil
}

func (c *SubscriptionTargetCatalog) ListSubscriptionTargets(ctx context.Context, organizationID string) ([]signals.SubscriptionTarget, error) {
	organizationID = strings.TrimSpace(organizationID)
	if c == nil || c.store == nil || c.endpoints == nil || !stableIDPattern.MatchString(organizationID) {
		return nil, ErrInvalidInput
	}
	providers, err := c.store.ListProviders(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	templates, err := c.store.ListTemplates(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	groups, err := c.store.ListGroups(ctx, organizationID)
	if err != nil {
		return nil, err
	}

	providerByID := make(map[string]Provider, len(providers))
	endpointByProviderID := make(map[string]Endpoint, len(providers))
	result := make([]signals.SubscriptionTarget, 0, len(groups)+len(providers))
	for _, provider := range providers {
		endpoint, ok := c.configuredEndpoint(provider)
		if !ok {
			continue
		}
		providerByID[provider.ID], endpointByProviderID[provider.ID] = provider, endpoint
		if provider.Kind == ProviderWebhook && validText(provider.Name, 1, 160) {
			result = append(result, signals.SubscriptionTarget{TargetKind: "webhook", TargetID: provider.ID, Label: provider.Name})
		}
	}
	templateByID := make(map[string]Template, len(templates))
	for _, template := range templates {
		if stableIDPattern.MatchString(template.ID) {
			templateByID[template.ID] = template
		}
	}
	for _, group := range groups {
		provider, providerOK := providerByID[group.ProviderID]
		endpoint, endpointOK := endpointByProviderID[group.ProviderID]
		_, templateOK := templateByID[group.TemplateID]
		if !providerOK || !endpointOK || !templateOK || !stableIDPattern.MatchString(group.ID) || !validText(group.Name, 1, 160) ||
			!compatibleRecipients(provider.Kind, endpoint, group.Recipients) {
			continue
		}
		result = append(result, signals.SubscriptionTarget{TargetKind: "group", TargetID: group.ID, Label: group.Name})
	}
	return result, nil
}

// SubscriptionTargetReferenceExists is the migration-only existence view.
// Imported providers deliberately remain disabled and unconfigured, but their
// stable records may still be referenced by an imported Signals subscription.
func (c *SubscriptionTargetCatalog) SubscriptionTargetReferenceExists(ctx context.Context, organizationID, targetKind, targetID string) (bool, error) {
	organizationID, targetKind, targetID = strings.TrimSpace(organizationID), strings.ToLower(strings.TrimSpace(targetKind)), strings.TrimSpace(targetID)
	if c == nil || c.store == nil || !stableIDPattern.MatchString(organizationID) || !stableIDPattern.MatchString(targetID) {
		return false, ErrInvalidInput
	}
	switch targetKind {
	case "webhook":
		provider, err := c.store.GetProvider(ctx, organizationID, targetID)
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		return err == nil && provider.Kind == ProviderWebhook && validSubscriptionReferenceProvider(provider), err
	case "group":
		group, err := c.store.GetGroup(ctx, organizationID, targetID)
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !validExchangeGroup(group) {
			return false, nil
		}
		provider, err := c.store.GetProvider(ctx, organizationID, group.ProviderID)
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		if err != nil || !validSubscriptionReferenceProvider(provider) {
			return false, err
		}
		template, err := c.store.GetTemplate(ctx, organizationID, group.TemplateID)
		if errors.Is(err, ErrNotFound) {
			return false, nil
		} else if err != nil {
			return false, err
		}
		if !validExchangeTemplate(template) || !portableRecipientCompatibility(provider.Kind, group.Recipients) {
			return false, nil
		}
		return true, nil
	default:
		return false, ErrInvalidInput
	}
}

func validSubscriptionReferenceProvider(provider Provider) bool {
	return stableIDPattern.MatchString(provider.ID) && provider.Name == strings.TrimSpace(provider.Name) && validText(provider.Name, 1, 160) &&
		validProviderKind(provider.Kind) && validSender(provider.Kind, provider.Sender) &&
		validExchangeRevisionTimes(provider.Revision, provider.CreatedAt, provider.UpdatedAt)
}

func (c *SubscriptionTargetCatalog) configuredEndpoint(provider Provider) (Endpoint, bool) {
	if !provider.Enabled || !provider.SecretConfigured || !secretReferencePattern.MatchString(provider.SecretRef) ||
		!stableIDPattern.MatchString(provider.ID) || !validProviderKind(provider.Kind) {
		return Endpoint{}, false
	}
	endpoint, err := c.endpoints.Get(provider.EndpointID, provider.Kind)
	return endpoint, err == nil
}
