package repository

import (
	"context"

	"github.com/maxlemke/stewardmesh/internal/domain"
)

// OrganizationRepository is the durable single-organization bootstrap seam.
// PostgreSQL is the first adapter; DynamoDB must conform to this same contract.
// Requirement: REQ-FOUNDATION-001.
type OrganizationRepository interface {
	GetOrganization(ctx context.Context, id string) (domain.Organization, error)
	BootstrapOrganization(ctx context.Context, organization domain.Organization) (domain.Organization, bool, error)
}

type TagRepository interface {
	ListTags(ctx context.Context) ([]domain.Tag, error)
}

type GoalRepository interface {
	ListGoals(ctx context.Context) ([]domain.Goal, error)
}
