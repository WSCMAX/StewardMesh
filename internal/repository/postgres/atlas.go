package postgres

// Requirement: REQ-ATLAS-001. Feature: inventory.assets.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/domain"
)

type AtlasStore struct {
	database *sql.DB
}

var _ atlas.Store = (*AtlasStore)(nil)

func NewAtlasStore(database *sql.DB) (*AtlasStore, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &AtlasStore{database: database}, nil
}

const atlasAssetColumns = `
	organization_id, id, name, kind, asset_tag, serial_number, hostname,
	site_id, building_id, room_id, department_id, user_id, status,
	purchase_date, revision, created_at, updated_at`

func (s *AtlasStore) ListAssets(ctx context.Context, organizationID string, query atlas.Query) ([]domain.Asset, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT `+atlasAssetColumns+`
		FROM atlas_assets
		WHERE organization_id = $1
		  AND ($2 = '' OR strpos(lower(name), $2) > 0 OR strpos(normalized_asset_tag, $2) > 0
		       OR strpos(normalized_serial_number, $2) > 0 OR strpos(hostname, $2) > 0)
		  AND ($3 = '' OR kind = $3)
		  AND ($4 = '' OR status = $4)
		  AND ($5 = '' OR site_id = $5)
		  AND ($6 = '' OR department_id = $6)
		  AND ($7 = '' OR user_id = $7)
		ORDER BY lower(name), id
		LIMIT $8
	`, organizationID, strings.ToLower(query.Search), query.Kind, query.Status, query.SiteID, query.DepartmentID, query.UserID, query.Limit)
	if err != nil {
		return nil, fmt.Errorf("list Atlas assets: %w", err)
	}
	defer rows.Close()
	items := make([]domain.Asset, 0)
	for rows.Next() {
		asset, err := scanAtlasAsset(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Atlas asset: %w", err)
		}
		items = append(items, asset)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Atlas assets: %w", err)
	}
	return items, nil
}

func (s *AtlasStore) GetAsset(ctx context.Context, organizationID, id string) (domain.Asset, error) {
	asset, err := scanAtlasAsset(s.database.QueryRowContext(ctx, `
		SELECT `+atlasAssetColumns+`
		FROM atlas_assets
		WHERE organization_id = $1 AND id = $2
	`, organizationID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Asset{}, atlas.ErrNotFound
	}
	if err != nil {
		return domain.Asset{}, fmt.Errorf("get Atlas asset: %w", err)
	}
	return asset, nil
}

func (s *AtlasStore) CreateAsset(ctx context.Context, asset domain.Asset, initialEvent domain.AssetLifecycleEvent) (domain.Asset, error) {
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return domain.Asset{}, fmt.Errorf("begin Atlas asset creation: %w", err)
	}
	defer transaction.Rollback()
	created, err := scanAtlasAsset(transaction.QueryRowContext(ctx, `
		INSERT INTO atlas_assets (
			organization_id, id, name, kind, asset_tag, normalized_asset_tag,
			serial_number, normalized_serial_number, hostname, site_id, building_id,
			room_id, department_id, user_id, status, purchase_date, revision, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, lower(btrim($5)), $6, lower(btrim($6)), $7,
			NULLIF($8, ''), NULLIF($9, ''), NULLIF($10, ''), NULLIF($11, ''), NULLIF($12, ''),
			$13, $14, $15, $16, $17
		)
		RETURNING `+atlasAssetColumns,
		asset.OrganizationID, asset.ID, asset.Name, asset.Kind, asset.AssetTag, asset.SerialNumber,
		asset.Hostname, asset.SiteID, asset.BuildingID, asset.RoomID, asset.DepartmentID, asset.UserID,
		asset.Status, asset.PurchaseDate, asset.Revision, asset.CreatedAt, asset.UpdatedAt,
	))
	if err != nil {
		return domain.Asset{}, translateAtlasWriteError("create Atlas asset", err)
	}
	if err := insertLifecycleEvent(ctx, transaction, initialEvent); err != nil {
		return domain.Asset{}, err
	}
	if err := transaction.Commit(); err != nil {
		return domain.Asset{}, fmt.Errorf("commit Atlas asset creation: %w", err)
	}
	return created, nil
}

func (s *AtlasStore) UpdateAsset(ctx context.Context, asset domain.Asset, expectedRevision int64, lifecycleEvent *domain.AssetLifecycleEvent) (domain.Asset, error) {
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return domain.Asset{}, fmt.Errorf("begin Atlas asset update: %w", err)
	}
	defer transaction.Rollback()
	updated, err := scanAtlasAsset(transaction.QueryRowContext(ctx, `
		UPDATE atlas_assets
		SET name = $3, kind = $4, asset_tag = $5, normalized_asset_tag = lower(btrim($5)),
			serial_number = $6, normalized_serial_number = lower(btrim($6)), hostname = $7,
			site_id = NULLIF($8, ''), building_id = NULLIF($9, ''), room_id = NULLIF($10, ''),
			department_id = NULLIF($11, ''), user_id = NULLIF($12, ''), status = $13,
			purchase_date = $14, revision = revision + 1, updated_at = $15
		WHERE organization_id = $1 AND id = $2 AND revision = $16
		RETURNING `+atlasAssetColumns,
		asset.OrganizationID, asset.ID, asset.Name, asset.Kind, asset.AssetTag, asset.SerialNumber,
		asset.Hostname, asset.SiteID, asset.BuildingID, asset.RoomID, asset.DepartmentID, asset.UserID,
		asset.Status, asset.PurchaseDate, asset.UpdatedAt, expectedRevision,
	))
	if errors.Is(err, sql.ErrNoRows) {
		var exists bool
		if checkErr := transaction.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM atlas_assets WHERE organization_id = $1 AND id = $2
		)`, asset.OrganizationID, asset.ID).Scan(&exists); checkErr != nil {
			return domain.Asset{}, fmt.Errorf("check Atlas asset update conflict: %w", checkErr)
		}
		if !exists {
			return domain.Asset{}, atlas.ErrNotFound
		}
		return domain.Asset{}, atlas.ErrConflict
	}
	if err != nil {
		return domain.Asset{}, translateAtlasWriteError("update Atlas asset", err)
	}
	if lifecycleEvent != nil {
		if err := insertLifecycleEvent(ctx, transaction, *lifecycleEvent); err != nil {
			return domain.Asset{}, err
		}
	}
	if err := transaction.Commit(); err != nil {
		return domain.Asset{}, fmt.Errorf("commit Atlas asset update: %w", err)
	}
	return updated, nil
}

func (s *AtlasStore) ListAssetLifecycle(ctx context.Context, organizationID, assetID string) ([]domain.AssetLifecycleEvent, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT organization_id, id, asset_id, from_status, to_status, note, revision, actor_id, occurred_at
		FROM atlas_asset_lifecycle_events
		WHERE organization_id = $1 AND asset_id = $2
		ORDER BY revision, occurred_at, id
	`, organizationID, assetID)
	if err != nil {
		return nil, fmt.Errorf("list Atlas lifecycle: %w", err)
	}
	defer rows.Close()
	items := make([]domain.AssetLifecycleEvent, 0)
	for rows.Next() {
		var event domain.AssetLifecycleEvent
		if err := rows.Scan(&event.OrganizationID, &event.ID, &event.AssetID, &event.FromStatus, &event.ToStatus,
			&event.Note, &event.Revision, &event.ActorID, &event.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan Atlas lifecycle: %w", err)
		}
		items = append(items, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Atlas lifecycle: %w", err)
	}
	return items, nil
}

type atlasScanner interface {
	Scan(dest ...any) error
}

func scanAtlasAsset(scanner atlasScanner) (domain.Asset, error) {
	var asset domain.Asset
	var siteID, buildingID, roomID, departmentID, userID sql.NullString
	var purchaseDate sql.NullTime
	if err := scanner.Scan(
		&asset.OrganizationID, &asset.ID, &asset.Name, &asset.Kind, &asset.AssetTag,
		&asset.SerialNumber, &asset.Hostname, &siteID, &buildingID, &roomID, &departmentID,
		&userID, &asset.Status, &purchaseDate, &asset.Revision, &asset.CreatedAt, &asset.UpdatedAt,
	); err != nil {
		return domain.Asset{}, err
	}
	asset.SiteID, asset.BuildingID, asset.RoomID = siteID.String, buildingID.String, roomID.String
	asset.DepartmentID, asset.UserID = departmentID.String, userID.String
	if purchaseDate.Valid {
		value := purchaseDate.Time.UTC()
		asset.PurchaseDate = &value
	}
	return asset, nil
}

func insertLifecycleEvent(ctx context.Context, transaction *sql.Tx, event domain.AssetLifecycleEvent) error {
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO atlas_asset_lifecycle_events (
			organization_id, id, asset_id, from_status, to_status, note, revision, actor_id, occurred_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, event.OrganizationID, event.ID, event.AssetID, event.FromStatus, event.ToStatus,
		event.Note, event.Revision, event.ActorID, event.OccurredAt); err != nil {
		return translateAtlasWriteError("record Atlas lifecycle", err)
	}
	return nil
}

func translateAtlasWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503":
			return fmt.Errorf("%s: %w", operation, atlas.ErrReferenceMissing)
		case "23505", "23514":
			return fmt.Errorf("%s: %w", operation, atlas.ErrConflict)
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
