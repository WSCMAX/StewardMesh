package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

type OrganizationRepository struct {
	database *sql.DB
}

var _ repository.OrganizationRepository = (*OrganizationRepository)(nil)

func NewOrganizationRepository(database *sql.DB) (*OrganizationRepository, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &OrganizationRepository{database: database}, nil
}

func (r *OrganizationRepository) GetOrganization(ctx context.Context, id string) (domain.Organization, error) {
	var organization domain.Organization
	err := r.database.QueryRowContext(ctx, `
		SELECT id, name, created_at, updated_at
		FROM organizations
		WHERE id = $1
	`, id).Scan(
		&organization.ID,
		&organization.Name,
		&organization.CreatedAt,
		&organization.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Organization{}, repository.ErrNotFound
	}
	if err != nil {
		return domain.Organization{}, fmt.Errorf("get organization: %w", err)
	}
	return organization, nil
}

func (r *OrganizationRepository) BootstrapOrganization(ctx context.Context, organization domain.Organization) (domain.Organization, bool, error) {
	if organization.ID == "" || organization.Name == "" || organization.CreatedAt.IsZero() || organization.UpdatedAt.IsZero() {
		return domain.Organization{}, false, errors.New("organization identity and timestamps are required")
	}
	transaction, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return domain.Organization{}, false, fmt.Errorf("begin organization bootstrap: %w", err)
	}
	defer transaction.Rollback()

	var persisted domain.Organization
	err = transaction.QueryRowContext(ctx, `
		INSERT INTO organizations (id, name, created_at, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO NOTHING
		RETURNING id, name, created_at, updated_at
	`, organization.ID, organization.Name, organization.CreatedAt, organization.UpdatedAt).Scan(
		&persisted.ID,
		&persisted.Name,
		&persisted.CreatedAt,
		&persisted.UpdatedAt,
	)
	created := err == nil
	if errors.Is(err, sql.ErrNoRows) {
		err = transaction.QueryRowContext(ctx, `
			UPDATE organizations
			SET name = $2, updated_at = $3
			WHERE id = $1
			RETURNING id, name, created_at, updated_at
		`, organization.ID, organization.Name, organization.UpdatedAt).Scan(
			&persisted.ID,
			&persisted.Name,
			&persisted.CreatedAt,
			&persisted.UpdatedAt,
		)
	}
	if err != nil {
		return domain.Organization{}, false, fmt.Errorf("persist organization: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return domain.Organization{}, false, fmt.Errorf("commit organization bootstrap: %w", err)
	}
	return persisted, created, nil
}
