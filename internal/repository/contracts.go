package repository

import (
	"context"

	"github.com/maxlemke/stewardmesh/internal/domain"
)

type AssetRepository interface {
	List(ctx context.Context) ([]domain.Asset, error)
	Get(ctx context.Context, id string) (domain.Asset, error)
	Create(ctx context.Context, asset domain.Asset) (domain.Asset, error)
}

// OrganizationRepository is the durable single-organization bootstrap seam.
// PostgreSQL is the first adapter; DynamoDB must conform to this same contract.
// Requirement: REQ-FOUNDATION-001.
type OrganizationRepository interface {
	GetOrganization(ctx context.Context, id string) (domain.Organization, error)
	BootstrapOrganization(ctx context.Context, organization domain.Organization) (domain.Organization, bool, error)
}

// Postgres and DynamoDB adapters will implement these contracts without
// leaking storage-specific behavior into application services.
type DepartmentRepository interface {
	ListDepartments(ctx context.Context) ([]domain.Department, error)
}

type UserRepository interface {
	ListUsers(ctx context.Context) ([]domain.User, error)
}

type TagRepository interface {
	ListTags(ctx context.Context) ([]domain.Tag, error)
}

type GoalRepository interface {
	ListGoals(ctx context.Context) ([]domain.Goal, error)
}
