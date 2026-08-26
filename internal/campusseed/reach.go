package campusseed

import (
	"context"
	"errors"
	"fmt"

	"github.com/maxlemke/stewardmesh/internal/reach"
)

func (s *ExtendedSeeder) seedReach(ctx context.Context) error {
	endpointID := s.reachWebhookEndpointID
	if endpointID == "" {
		endpointID = "operations-hook"
	}
	enabled := true
	provider, err := s.reach.CreateProvider(ctx, reach.CreateProviderInput{
		ID: "campus-demo-webhook", Name: "Campus demo IT operations webhook",
		Kind: reach.ProviderWebhook, EndpointID: endpointID,
		SecretRef: "env:CAMPUS_DEMO_WEBHOOK_SECRET", Enabled: &enabled,
	})
	if err != nil && !errors.Is(err, reach.ErrConflict) {
		return fmt.Errorf("create campus demo Reach provider: %w", err)
	}
	providerID := "campus-demo-webhook"
	if err == nil {
		providerID = provider.ID
	}
	template, err := s.reach.CreateTemplate(ctx, reach.CreateTemplateInput{
		ID: "campus-demo-alert-template", Name: "Campus demo alert",
		Subject: "{{severity}}: {{title}}", Body: "{{summary}}\nAlert {{record_id}} for Riverside Community College.",
	})
	if err != nil && !errors.Is(err, reach.ErrConflict) {
		return fmt.Errorf("create campus demo Reach template: %w", err)
	}
	templateID := "campus-demo-alert-template"
	if err == nil {
		templateID = template.ID
	}
	_, err = s.reach.CreateGroup(ctx, reach.CreateGroupInput{
		ID: "campus-demo-it-ops", Name: "Campus demo IT operations",
		ProviderID: providerID, TemplateID: templateID,
		Recipients: []reach.Recipient{
			{Kind: reach.RecipientEmail, Address: "it-director@riverside-demo.invalid"},
			{Kind: reach.RecipientEmail, Address: "helpdesk-tech@riverside-demo.invalid"},
		},
	})
	if err != nil && !errors.Is(err, reach.ErrConflict) {
		return fmt.Errorf("create campus demo Reach group: %w", err)
	}
	return nil
}
