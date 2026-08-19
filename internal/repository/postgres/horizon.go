package postgres

// PostgreSQL Horizon adapter. Requirement: REQ-HORIZON-001. Feature: lifecycle.planning.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxlemke/stewardmesh/internal/horizon"
)

type HorizonStore struct{ database *sql.DB }

func NewHorizonStore(database *sql.DB) (*HorizonStore, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &HorizonStore{database: database}, nil
}

const horizonPlanColumns = `organization_id, id, asset_id, scenario, expected_useful_life_months,
	replacement_date, lifecycle_stage, replacement_cost_minor, currency, effective_from, revision, created_at, updated_at`

func (s *HorizonStore) ListPlans(ctx context.Context, organizationID string, query horizon.ListPlansQuery) ([]horizon.Plan, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT `+horizonPlanColumns+` FROM horizon_plans
		WHERE organization_id = $1 AND ($2 = '' OR asset_id = $2) AND ($3 = '' OR scenario = $3)
		ORDER BY asset_id, scenario, id`, organizationID, query.AssetID, query.Scenario)
	if err != nil {
		return nil, fmt.Errorf("list Horizon plans: %w", err)
	}
	defer rows.Close()
	items := make([]horizon.Plan, 0)
	for rows.Next() {
		item, err := scanHorizonPlan(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Horizon plan: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Horizon plans: %w", err)
	}
	return items, nil
}

func (s *HorizonStore) GetPlan(ctx context.Context, organizationID, id string) (horizon.Plan, error) {
	item, err := scanHorizonPlan(s.database.QueryRowContext(ctx, `SELECT `+horizonPlanColumns+` FROM horizon_plans WHERE organization_id = $1 AND id = $2`, organizationID, id))
	return item, translateHorizonReadError("get Horizon plan", err)
}

func (s *HorizonStore) CreatePlan(ctx context.Context, item horizon.Plan, version horizon.PlanVersion) (horizon.Plan, error) {
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return horizon.Plan{}, fmt.Errorf("begin Horizon plan creation: %w", err)
	}
	defer transaction.Rollback()
	created, err := scanHorizonPlan(transaction.QueryRowContext(ctx, `
		INSERT INTO horizon_plans (organization_id, id, asset_id, scenario, expected_useful_life_months,
			replacement_date, lifecycle_stage, replacement_cost_minor, currency, effective_from, revision, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING `+horizonPlanColumns,
		item.OrganizationID, item.ID, item.AssetID, item.Scenario, item.ExpectedUsefulLifeMonths, item.ReplacementDate,
		item.LifecycleStage, item.ReplacementCostMinor, item.Currency, item.EffectiveFrom, item.Revision, item.CreatedAt, item.UpdatedAt))
	if err != nil {
		return horizon.Plan{}, translateHorizonWriteError("create Horizon plan", err)
	}
	if err := insertHorizonVersion(ctx, transaction, version); err != nil {
		return horizon.Plan{}, err
	}
	if err := transaction.Commit(); err != nil {
		return horizon.Plan{}, fmt.Errorf("commit Horizon plan creation: %w", err)
	}
	return created, nil
}

func (s *HorizonStore) UpdatePlan(ctx context.Context, item horizon.Plan, expectedRevision int64, version horizon.PlanVersion) (horizon.Plan, error) {
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return horizon.Plan{}, fmt.Errorf("begin Horizon plan update: %w", err)
	}
	defer transaction.Rollback()
	updated, err := scanHorizonPlan(transaction.QueryRowContext(ctx, `
		UPDATE horizon_plans SET expected_useful_life_months = $3, replacement_date = $4, lifecycle_stage = $5,
			replacement_cost_minor = $6, currency = $7, effective_from = $8, revision = $9, updated_at = $10
		WHERE organization_id = $1 AND id = $2 AND revision = $11
		RETURNING `+horizonPlanColumns,
		item.OrganizationID, item.ID, item.ExpectedUsefulLifeMonths, item.ReplacementDate, item.LifecycleStage,
		item.ReplacementCostMinor, item.Currency, item.EffectiveFrom, item.Revision, item.UpdatedAt, expectedRevision))
	if errors.Is(err, sql.ErrNoRows) {
		var exists bool
		if checkErr := transaction.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM horizon_plans WHERE organization_id = $1 AND id = $2)`, item.OrganizationID, item.ID).Scan(&exists); checkErr != nil {
			return horizon.Plan{}, fmt.Errorf("check Horizon revision conflict: %w", checkErr)
		}
		if exists {
			return horizon.Plan{}, horizon.ErrConflict
		}
		return horizon.Plan{}, horizon.ErrNotFound
	}
	if err != nil {
		return horizon.Plan{}, translateHorizonWriteError("update Horizon plan", err)
	}
	if err := insertHorizonVersion(ctx, transaction, version); err != nil {
		return horizon.Plan{}, err
	}
	if err := transaction.Commit(); err != nil {
		return horizon.Plan{}, fmt.Errorf("commit Horizon plan update: %w", err)
	}
	return updated, nil
}

func (s *HorizonStore) ListPlanVersions(ctx context.Context, organizationID, planID string) ([]horizon.PlanVersion, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT organization_id, plan_id, asset_id, scenario, expected_useful_life_months, replacement_date,
			lifecycle_stage, replacement_cost_minor, currency, effective_from, revision, actor_id, recorded_at
		FROM horizon_plan_versions WHERE organization_id = $1 AND plan_id = $2 ORDER BY revision DESC`, organizationID, planID)
	if err != nil {
		return nil, fmt.Errorf("list Horizon plan versions: %w", err)
	}
	defer rows.Close()
	items := make([]horizon.PlanVersion, 0)
	for rows.Next() {
		item, err := scanHorizonVersion(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Horizon plan version: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Horizon plan versions: %w", err)
	}
	if len(items) == 0 {
		if _, err := s.GetPlan(ctx, organizationID, planID); err != nil {
			return nil, err
		}
	}
	return items, nil
}

type horizonScanner interface{ Scan(...any) error }

func scanHorizonPlan(row horizonScanner) (horizon.Plan, error) {
	var item horizon.Plan
	var replacement sql.NullTime
	err := row.Scan(&item.OrganizationID, &item.ID, &item.AssetID, &item.Scenario, &item.ExpectedUsefulLifeMonths,
		&replacement, &item.LifecycleStage, &item.ReplacementCostMinor, &item.Currency, &item.EffectiveFrom,
		&item.Revision, &item.CreatedAt, &item.UpdatedAt)
	if replacement.Valid {
		item.ReplacementDate = &replacement.Time
	}
	return item, err
}

func scanHorizonVersion(row horizonScanner) (horizon.PlanVersion, error) {
	var item horizon.PlanVersion
	var replacement sql.NullTime
	err := row.Scan(&item.OrganizationID, &item.PlanID, &item.AssetID, &item.Scenario, &item.ExpectedUsefulLifeMonths,
		&replacement, &item.LifecycleStage, &item.ReplacementCostMinor, &item.Currency, &item.EffectiveFrom,
		&item.Revision, &item.ActorID, &item.RecordedAt)
	if replacement.Valid {
		item.ReplacementDate = &replacement.Time
	}
	return item, err
}

const horizonKindDefaultColumns = `organization_id, asset_kind, scenario, expected_useful_life_months, replacement_model_id, revision, created_at, updated_at`

func (s *HorizonStore) ListKindDefaults(ctx context.Context, organizationID, scenario string) ([]horizon.KindDefault, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT `+horizonKindDefaultColumns+`
		FROM horizon_kind_defaults WHERE organization_id = $1 AND ($2 = '' OR scenario = $2)
		ORDER BY asset_kind, scenario`, organizationID, scenario)
	if err != nil {
		return nil, fmt.Errorf("list Horizon kind defaults: %w", err)
	}
	defer rows.Close()
	items := make([]horizon.KindDefault, 0)
	for rows.Next() {
		item, err := scanHorizonKindDefault(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Horizon kind default: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *HorizonStore) UpsertKindDefault(ctx context.Context, item horizon.KindDefault) (horizon.KindDefault, error) {
	saved, err := scanHorizonKindDefault(s.database.QueryRowContext(ctx, `
		INSERT INTO horizon_kind_defaults (
			organization_id, asset_kind, scenario, expected_useful_life_months, replacement_model_id, revision, created_at, updated_at
		) VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7, $8)
		ON CONFLICT (organization_id, asset_kind, scenario) DO UPDATE SET
			expected_useful_life_months = EXCLUDED.expected_useful_life_months,
			replacement_model_id = EXCLUDED.replacement_model_id,
			revision = horizon_kind_defaults.revision + 1,
			updated_at = EXCLUDED.updated_at
		RETURNING `+horizonKindDefaultColumns,
		item.OrganizationID, item.AssetKind, item.Scenario, item.ExpectedUsefulLifeMonths, item.ReplacementModelID,
		item.Revision, item.CreatedAt, item.UpdatedAt))
	if err != nil {
		return horizon.KindDefault{}, translateHorizonWriteError("upsert Horizon kind default", err)
	}
	return saved, nil
}

func scanHorizonKindDefault(row horizonScanner) (horizon.KindDefault, error) {
	var item horizon.KindDefault
	var replacementModelID sql.NullString
	err := row.Scan(&item.OrganizationID, &item.AssetKind, &item.Scenario, &item.ExpectedUsefulLifeMonths,
		&replacementModelID, &item.Revision, &item.CreatedAt, &item.UpdatedAt)
	item.ReplacementModelID = replacementModelID.String
	return item, err
}

func insertHorizonVersion(ctx context.Context, transaction *sql.Tx, item horizon.PlanVersion) error {
	_, err := transaction.ExecContext(ctx, `
		INSERT INTO horizon_plan_versions (organization_id, plan_id, asset_id, scenario, expected_useful_life_months,
			replacement_date, lifecycle_stage, replacement_cost_minor, currency, effective_from, revision, actor_id, recorded_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		item.OrganizationID, item.PlanID, item.AssetID, item.Scenario, item.ExpectedUsefulLifeMonths, item.ReplacementDate,
		item.LifecycleStage, item.ReplacementCostMinor, item.Currency, item.EffectiveFrom, item.Revision, item.ActorID, item.RecordedAt)
	return translateHorizonWriteError("record Horizon plan version", err)
}

func translateHorizonReadError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return horizon.ErrNotFound
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func translateHorizonWriteError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505":
			return horizon.ErrConflict
		case "23503":
			return horizon.ErrReferenceMissing
		case "23502", "23514", "22P02":
			return horizon.ErrInvalidInput
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
