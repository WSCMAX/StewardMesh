// Package bootstrap initializes the durable StewardMesh installation state.
// Requirement: REQ-FOUNDATION-001.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

var organizationIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type Organization = domain.Organization

type OrganizationService struct {
	repository repository.OrganizationRepository
	now        func() time.Time
}

func NewOrganizationService(organizationRepository repository.OrganizationRepository) (*OrganizationService, error) {
	if organizationRepository == nil {
		return nil, errors.New("organization repository is required")
	}
	return &OrganizationService{repository: organizationRepository, now: time.Now}, nil
}

// EnsureOrganization creates the configured organization once and safely
// refreshes its display name on later starts without changing its identity.
func (s *OrganizationService) EnsureOrganization(ctx context.Context, id, name string) (Organization, bool, error) {
	organization, err := NewOrganization(id, name)
	if err != nil {
		return Organization{}, false, err
	}
	now := s.now().UTC()
	organization.CreatedAt = now
	organization.UpdatedAt = now
	persisted, created, err := s.repository.BootstrapOrganization(ctx, organization)
	if err != nil {
		return Organization{}, false, fmt.Errorf("bootstrap organization: %w", err)
	}
	return persisted, created, nil
}

func NewOrganization(id, name string) (Organization, error) {
	id = strings.TrimSpace(id)
	name = strings.TrimSpace(name)
	if !organizationIDPattern.MatchString(id) {
		return Organization{}, errors.New("organization id must be 1-128 letters, numbers, dots, underscores, or hyphens")
	}
	if name == "" || len(name) > 200 {
		return Organization{}, errors.New("organization name must be 1-200 characters")
	}
	return Organization{ID: id, Name: name}, nil
}
