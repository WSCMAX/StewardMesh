//go:build !campusdemo

package application

// Requirement: REQ-DIRECTORY-EXPANSION-007. Feature: platform.foundation.

import (
	"context"
	"errors"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/config"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/horizon"
	"github.com/maxlemke/stewardmesh/internal/patterns"
	"github.com/maxlemke/stewardmesh/internal/reach"
	"github.com/maxlemke/stewardmesh/internal/stack"
	"github.com/maxlemke/stewardmesh/internal/storage"
)

const CampusDemoIncluded = false

var errCampusDemoExcluded = errors.New("campus demo package is not included in this build; run the campus-demo image")

func campusDemoUnavailable(cfg config.Config, options Options) error {
	if options.RunCampusSeed || cfg.SeedCampus {
		return errCampusDemoExcluded
	}
	return nil
}

func seedCampusCore(_ context.Context, cfg config.Config, options Options, _ foundationRuntime, _ *storage.Service, _ *guard.Service, _ *atlas.Service, _ *stack.Service) error {
	return campusDemoUnavailable(cfg, options)
}

func seedCampusLifecycle(_ context.Context, cfg config.Config, options Options, _ foundationRuntime, _ *storage.Service, _ *horizon.Service) error {
	return campusDemoUnavailable(cfg, options)
}

func seedCampusExtended(_ context.Context, cfg config.Config, options Options, _ foundationRuntime, _ *storage.Service, _ *atlas.Service, _ *stack.Service, _ *horizon.Service, _ *patterns.Service, _ *reach.EndpointCatalog, _ *reach.TransportRegistry, _ reach.SecretResolver, _ []reach.Endpoint) error {
	return campusDemoUnavailable(cfg, options)
}
