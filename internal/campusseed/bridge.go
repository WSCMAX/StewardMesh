package campusseed

import (
	"context"
	"errors"
	"fmt"

	"github.com/maxlemke/stewardmesh/internal/bridge"
)

func (s *ExtendedSeeder) seedBridgeClient(ctx context.Context) error {
	now := s.now()
	_, err := s.bridgeStore.CreateClient(ctx, bridge.Client{
		ID: campusDemoBridgeClientID, OrganizationID: s.organizationID,
		Name: "Campus demo MCP client",
		RedirectURIs: []string{"http://localhost:5173/oauth/callback"},
		AllowedScopes: []bridge.Scope{
			bridge.ScopeMCPResources, bridge.ScopeAssetsRead,
			bridge.ScopeDirectoryRead, bridge.ScopeSignalsRead,
		},
		CreatedBy: ActorID, CreatedAt: now,
	})
	if err != nil && !errors.Is(err, bridge.ErrConflict) {
		return fmt.Errorf("create campus demo bridge client: %w", err)
	}
	return nil
}
