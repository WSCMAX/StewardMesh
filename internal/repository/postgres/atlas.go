package postgres

// Requirements: REQ-ATLAS-001, REQ-ATLAS-MODELS-001, REQ-DIRECTORY-EXPANSION-008. Features: inventory.assets, inventory.models, threads.relationships.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

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
	organization_id, id, model_id, model_context, name, kind, asset_tag, serial_number, hostname,
	deployment_notes, site_id, building_id, room_id, department_id, user_id, status,
	purchase_date, revision, created_at, updated_at`

const atlasModelColumns = `
	organization_id, id, manufacturer, name, model_number, kind, vendor_identifier,
	specifications, support_url, warranty_months, useful_life_months, status,
	source_system_id, source_record_id, revision, created_at, updated_at`

func (s *AtlasStore) ListModels(ctx context.Context, organizationID string, query atlas.ModelQuery) ([]domain.AssetModel, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT `+atlasModelColumns+`, (
			SELECT count(*) FROM atlas_assets AS asset
			WHERE asset.organization_id = model.organization_id AND asset.model_id = model.id
		)
		FROM atlas_models AS model
		WHERE organization_id = $1
		  AND ($2 = '' OR strpos(normalized_manufacturer, $2) > 0 OR strpos(normalized_name, $2) > 0
		       OR strpos(normalized_model_number, $2) > 0 OR strpos(lower(vendor_identifier), $2) > 0)
		  AND ($3 = '' OR kind = $3)
		  AND ($4 = '' OR status = $4)
		ORDER BY normalized_manufacturer, normalized_name, normalized_model_number, id
		LIMIT $5
	`, organizationID, query.Search, query.Kind, query.Status, query.Limit)
	if err != nil {
		return nil, fmt.Errorf("list Atlas models: %w", err)
	}
	defer rows.Close()
	items := make([]domain.AssetModel, 0)
	for rows.Next() {
		model, err := scanAtlasModel(rows, true)
		if err != nil {
			return nil, fmt.Errorf("scan Atlas model: %w", err)
		}
		items = append(items, model)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Atlas models: %w", err)
	}
	return items, nil
}

func (s *AtlasStore) GetModel(ctx context.Context, organizationID, id string) (domain.AssetModel, error) {
	model, err := scanAtlasModel(s.database.QueryRowContext(ctx, `
		SELECT `+atlasModelColumns+`, (
			SELECT count(*) FROM atlas_assets AS asset
			WHERE asset.organization_id = model.organization_id AND asset.model_id = model.id
		)
		FROM atlas_models AS model
		WHERE organization_id = $1 AND id = $2
	`, organizationID, id), true)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AssetModel{}, atlas.ErrNotFound
	}
	if err != nil {
		return domain.AssetModel{}, fmt.Errorf("get Atlas model: %w", err)
	}
	return model, nil
}

func (s *AtlasStore) ResolveModel(ctx context.Context, organizationID string, identity atlas.ModelIdentity) (domain.AssetModel, error) {
	model, err := scanAtlasModel(s.database.QueryRowContext(ctx, `
		SELECT `+atlasModelColumns+`, (
			SELECT count(*) FROM atlas_assets AS asset
			WHERE asset.organization_id = model.organization_id AND asset.model_id = model.id
		)
		FROM atlas_models AS model
		WHERE organization_id = $1 AND normalized_manufacturer = $2
		  AND normalized_name = $3 AND normalized_model_number = $4
	`, organizationID, identity.Manufacturer, identity.Name, identity.ModelNumber), true)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AssetModel{}, atlas.ErrNotFound
	}
	if err != nil {
		return domain.AssetModel{}, fmt.Errorf("resolve Atlas model: %w", err)
	}
	return model, nil
}

func (s *AtlasStore) CreateModel(ctx context.Context, model domain.AssetModel) (domain.AssetModel, error) {
	specifications, err := marshalAtlasSpecifications(model.Specifications)
	if err != nil {
		return domain.AssetModel{}, fmt.Errorf("marshal Atlas model specifications: %w", err)
	}
	created, err := scanAtlasModel(s.database.QueryRowContext(ctx, `
		INSERT INTO atlas_models (
			organization_id, id, manufacturer, name, model_number, normalized_manufacturer,
			normalized_name, normalized_model_number, kind, vendor_identifier, specifications,
			support_url, warranty_months, useful_life_months, status, source_system_id,
			source_record_id, revision, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, lower(btrim($3)), lower(btrim($4)), lower(btrim($5)), $6, $7,
			$8, $9, $10, $11, $12, $13, $14, $15, $16, $17
		)
		RETURNING `+atlasModelColumns+`, 0
	`, model.OrganizationID, model.ID, model.Manufacturer, model.Name, model.ModelNumber, model.Kind,
		model.VendorIdentifier, specifications, model.SupportURL, model.WarrantyMonths, model.UsefulLifeMonths,
		model.Status, model.SourceSystemID, model.SourceRecordID, model.Revision, model.CreatedAt, model.UpdatedAt), true)
	if err != nil {
		return domain.AssetModel{}, translateAtlasWriteError("create Atlas model", err)
	}
	return created, nil
}

func (s *AtlasStore) UpdateModel(ctx context.Context, model domain.AssetModel, expectedRevision int64) (domain.AssetModel, error) {
	specifications, err := marshalAtlasSpecifications(model.Specifications)
	if err != nil {
		return domain.AssetModel{}, fmt.Errorf("marshal Atlas model specifications: %w", err)
	}
	updated, err := scanAtlasModel(s.database.QueryRowContext(ctx, `
		UPDATE atlas_models
		SET manufacturer = $3, name = $4, model_number = $5, normalized_manufacturer = lower(btrim($3)),
			normalized_name = lower(btrim($4)), normalized_model_number = lower(btrim($5)),
			kind = $6, vendor_identifier = $7, specifications = $8, support_url = $9,
			warranty_months = $10, useful_life_months = $11, source_system_id = $12,
			source_record_id = $13, revision = revision + 1, updated_at = $14
		WHERE organization_id = $1 AND id = $2 AND revision = $15 AND status = 'active'
		RETURNING `+atlasModelColumns+`, (
			SELECT count(*) FROM atlas_assets AS asset
			WHERE asset.organization_id = atlas_models.organization_id AND asset.model_id = atlas_models.id
		)
	`, model.OrganizationID, model.ID, model.Manufacturer, model.Name, model.ModelNumber, model.Kind,
		model.VendorIdentifier, specifications, model.SupportURL, model.WarrantyMonths, model.UsefulLifeMonths,
		model.SourceSystemID, model.SourceRecordID, model.UpdatedAt, expectedRevision), true)
	if errors.Is(err, sql.ErrNoRows) {
		if exists, checkErr := s.modelExists(ctx, model.OrganizationID, model.ID); checkErr != nil {
			return domain.AssetModel{}, checkErr
		} else if !exists {
			return domain.AssetModel{}, atlas.ErrNotFound
		}
		return domain.AssetModel{}, atlas.ErrConflict
	}
	if err != nil {
		return domain.AssetModel{}, translateAtlasWriteError("update Atlas model", err)
	}
	return updated, nil
}

func (s *AtlasStore) RetireModel(ctx context.Context, organizationID, id string, expectedRevision int64, retiredAt time.Time) (domain.AssetModel, error) {
	retired, err := scanAtlasModel(s.database.QueryRowContext(ctx, `
		UPDATE atlas_models
		SET status = 'retired', revision = revision + 1, updated_at = $4
		WHERE organization_id = $1 AND id = $2 AND revision = $3 AND status = 'active'
		RETURNING `+atlasModelColumns+`, (
			SELECT count(*) FROM atlas_assets AS asset
			WHERE asset.organization_id = atlas_models.organization_id AND asset.model_id = atlas_models.id
		)
	`, organizationID, id, expectedRevision, retiredAt), true)
	if errors.Is(err, sql.ErrNoRows) {
		if exists, checkErr := s.modelExists(ctx, organizationID, id); checkErr != nil {
			return domain.AssetModel{}, checkErr
		} else if !exists {
			return domain.AssetModel{}, atlas.ErrNotFound
		}
		return domain.AssetModel{}, atlas.ErrConflict
	}
	if err != nil {
		return domain.AssetModel{}, translateAtlasWriteError("retire Atlas model", err)
	}
	return retired, nil
}

const atlasAssetFilterWhere = `
		WHERE organization_id = $1
		  AND ($2 = '' OR strpos(lower(name), $2) > 0 OR strpos(normalized_asset_tag, $2) > 0
		       OR strpos(normalized_serial_number, $2) > 0 OR strpos(hostname, $2) > 0)
		  AND ($3 = '' OR kind = $3)
		  AND ($4 = '' OR status = $4)
		  AND ($5 = '' OR model_id = $5)
		  AND ($6 = '' OR site_id = $6)
		  AND ($7 = '' OR department_id = $7)
		  AND ($8 = '' OR user_id = $8)
		  AND ($9 = '' OR strpos(lower(hostname), $9) > 0 OR strpos(lower(deployment_notes), $9) > 0)`

func (s *AtlasStore) GetModelInventory(ctx context.Context, organizationID, modelID string, query atlas.ModelInventoryQuery) (atlas.ModelInventory, error) {
	tx, err := s.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return atlas.ModelInventory{}, fmt.Errorf("begin Atlas model inventory: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM atlas_models WHERE organization_id = $1 AND id = $2
	)`, organizationID, modelID).Scan(&exists); err != nil {
		return atlas.ModelInventory{}, fmt.Errorf("check Atlas model inventory: %w", err)
	}
	if !exists {
		return atlas.ModelInventory{}, atlas.ErrNotFound
	}
	assetQuery := atlas.Query{
		Status: query.Status, ModelID: modelID, SiteID: query.SiteID, DepartmentID: query.DepartmentID,
		UserID: query.UserID, DeploymentContext: query.DeploymentContext, Limit: query.Limit,
	}
	result := atlas.ModelInventory{
		ModelID: modelID, GroupBy: query.GroupBy, Groups: []atlas.ModelInventoryGroup{}, Items: []domain.Asset{},
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM atlas_assets WHERE organization_id = $1 AND model_id = $2`, organizationID, modelID).Scan(&result.TotalCount); err != nil {
		return atlas.ModelInventory{}, fmt.Errorf("count Atlas model instances: %w", err)
	}
	filterArguments := atlasAssetFilterArguments(organizationID, assetQuery)
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM atlas_assets `+atlasAssetFilterWhere, filterArguments...).Scan(&result.FilteredCount); err != nil {
		return atlas.ModelInventory{}, fmt.Errorf("count filtered Atlas model instances: %w", err)
	}
	if query.GroupBy != "" {
		expression := atlasModelInventoryGroupExpression(query.GroupBy)
		rows, err := tx.QueryContext(ctx, `SELECT `+expression+`, count(*) FROM atlas_assets `+atlasAssetFilterWhere+
			` GROUP BY `+expression+` ORDER BY count(*) DESC, lower(`+expression+`), `+expression, filterArguments...)
		if err != nil {
			return atlas.ModelInventory{}, fmt.Errorf("group Atlas model instances: %w", err)
		}
		for rows.Next() {
			var group atlas.ModelInventoryGroup
			if err := rows.Scan(&group.Key, &group.Count); err != nil {
				_ = rows.Close()
				return atlas.ModelInventory{}, fmt.Errorf("scan Atlas model inventory group: %w", err)
			}
			result.Groups = append(result.Groups, group)
		}
		if err := rows.Close(); err != nil {
			return atlas.ModelInventory{}, fmt.Errorf("close Atlas model inventory groups: %w", err)
		}
		if err := rows.Err(); err != nil {
			return atlas.ModelInventory{}, fmt.Errorf("iterate Atlas model inventory groups: %w", err)
		}
	}
	result.Items, err = listAtlasAssets(ctx, tx, organizationID, assetQuery)
	if err != nil {
		return atlas.ModelInventory{}, err
	}
	if err := tx.Commit(); err != nil {
		return atlas.ModelInventory{}, fmt.Errorf("complete Atlas model inventory: %w", err)
	}
	return result, nil
}

func (s *AtlasStore) ListAssets(ctx context.Context, organizationID string, query atlas.Query) ([]domain.Asset, error) {
	return listAtlasAssets(ctx, s.database, organizationID, query)
}

func (s *AtlasStore) ListGraphAssets(ctx context.Context, organizationID string, filter atlas.GraphAssetQuery) ([]domain.Asset, error) {
	if organizationID == "" || !filter.Valid() {
		return nil, atlas.ErrInvalidInput
	}
	query := strings.Builder{}
	query.WriteString("SELECT " + atlasAssetColumns + " FROM atlas_assets WHERE organization_id = $1")
	arguments := []any{organizationID}
	if filter.LabelSearch != "" {
		arguments = append(arguments, strings.ToLower(filter.LabelSearch))
		query.WriteString(fmt.Sprintf(" AND strpos(lower(name), $%d) > 0", len(arguments)))
	}
	if !filter.Visibility.All {
		visibility := make([]string, 0, 3)
		if len(filter.Visibility.ResourceIDs) > 0 {
			visibility = append(visibility, atlasInPredicate("id", filter.Visibility.ResourceIDs, &arguments))
		}
		if len(filter.Visibility.SiteIDs) > 0 {
			visibility = append(visibility, atlasInPredicate("site_id", filter.Visibility.SiteIDs, &arguments))
		}
		if len(filter.Visibility.DepartmentIDs) > 0 {
			visibility = append(visibility, atlasInPredicate("department_id", filter.Visibility.DepartmentIDs, &arguments))
		}
		if len(visibility) == 0 {
			return nil, atlas.ErrInvalidInput
		}
		query.WriteString(" AND (" + strings.Join(visibility, " OR ") + ")")
	}
	references := make([]string, 0, 6)
	for _, reference := range []struct {
		column string
		values []string
	}{
		{"id", filter.References.ResourceIDs}, {"site_id", filter.References.SiteIDs},
		{"building_id", filter.References.BuildingIDs}, {"room_id", filter.References.RoomIDs},
		{"department_id", filter.References.DepartmentIDs}, {"user_id", filter.References.UserIDs},
	} {
		if len(reference.values) > 0 {
			references = append(references, atlasInPredicate(reference.column, reference.values, &arguments))
		}
	}
	if len(references) > 0 {
		sort.Strings(references)
		query.WriteString(" AND (" + strings.Join(references, " OR ") + ")")
	}
	arguments = append(arguments, filter.Limit)
	query.WriteString(fmt.Sprintf(" ORDER BY lower(name), id LIMIT $%d", len(arguments)))
	rows, err := s.database.QueryContext(ctx, query.String(), arguments...)
	if err != nil {
		return nil, fmt.Errorf("list graph assets: %w", err)
	}
	defer rows.Close()
	items := make([]domain.Asset, 0)
	for rows.Next() {
		asset, err := scanAtlasAsset(rows)
		if err != nil {
			return nil, fmt.Errorf("scan graph asset: %w", err)
		}
		items = append(items, asset)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate graph assets: %w", err)
	}
	return items, nil
}

func atlasInPredicate(column string, values []string, arguments *[]any) string {
	placeholders := make([]string, 0, len(values))
	for _, value := range values {
		*arguments = append(*arguments, value)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(*arguments)))
	}
	return column + " IN (" + strings.Join(placeholders, ", ") + ")"
}

type atlasRowsQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func listAtlasAssets(ctx context.Context, queryer atlasRowsQueryer, organizationID string, query atlas.Query) ([]domain.Asset, error) {
	arguments := append(atlasAssetFilterArguments(organizationID, query), query.Limit)
	rows, err := queryer.QueryContext(ctx, `
		SELECT `+atlasAssetColumns+`
		FROM atlas_assets `+atlasAssetFilterWhere+`
		ORDER BY lower(name), id
		LIMIT $10
	`, arguments...)
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

func atlasAssetFilterArguments(organizationID string, query atlas.Query) []any {
	return []any{
		organizationID, strings.ToLower(query.Search), query.Kind, query.Status, query.ModelID,
		query.SiteID, query.DepartmentID, query.UserID, strings.ToLower(query.DeploymentContext),
	}
}

func atlasModelInventoryGroupExpression(groupBy string) string {
	switch groupBy {
	case atlas.ModelInventoryGroupStatus:
		return "status"
	case atlas.ModelInventoryGroupSite:
		return "COALESCE(site_id, '')"
	case atlas.ModelInventoryGroupDepartment:
		return "COALESCE(department_id, '')"
	case atlas.ModelInventoryGroupUser:
		return "COALESCE(user_id, '')"
	case atlas.ModelInventoryGroupDeployment:
		return "CASE WHEN btrim(deployment_notes) <> '' THEN deployment_notes ELSE hostname END"
	default:
		return "''"
	}
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
	created, err := s.CreateAssets(ctx, []domain.Asset{asset}, []domain.AssetLifecycleEvent{initialEvent})
	if err != nil {
		return domain.Asset{}, err
	}
	return created[0], nil
}

func (s *AtlasStore) CreateAssets(ctx context.Context, assets []domain.Asset, initialEvents []domain.AssetLifecycleEvent) ([]domain.Asset, error) {
	if len(assets) == 0 || len(assets) != len(initialEvents) {
		return nil, atlas.ErrInvalidInput
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin Atlas asset creation: %w", err)
	}
	defer transaction.Rollback()
	created := make([]domain.Asset, 0, len(assets))
	for index, asset := range assets {
		if initialEvents[index].OrganizationID != asset.OrganizationID || initialEvents[index].AssetID != asset.ID {
			return nil, atlas.ErrInvalidInput
		}
		modelContext, marshalErr := marshalAtlasModelContext(asset.ModelContext)
		if marshalErr != nil {
			return nil, marshalErr
		}
		item, createErr := scanAtlasAsset(transaction.QueryRowContext(ctx, `
		INSERT INTO atlas_assets (
			organization_id, id, model_id, model_context, name, kind, asset_tag, normalized_asset_tag,
			serial_number, normalized_serial_number, hostname, deployment_notes, site_id, building_id,
			room_id, department_id, user_id, status, purchase_date, revision, created_at, updated_at
		) VALUES (
			$1, $2, NULLIF($3, ''), $4, $5, $6, $7, lower(btrim($7)), $8, lower(btrim($8)), $9,
			$10, NULLIF($11, ''), NULLIF($12, ''), NULLIF($13, ''), NULLIF($14, ''), NULLIF($15, ''),
			$16, $17, $18, $19, $20
		)
		RETURNING `+atlasAssetColumns,
			asset.OrganizationID, asset.ID, asset.ModelID, modelContext, asset.Name, asset.Kind, asset.AssetTag, asset.SerialNumber,
			asset.Hostname, asset.DeploymentNotes, asset.SiteID, asset.BuildingID, asset.RoomID, asset.DepartmentID, asset.UserID,
			asset.Status, asset.PurchaseDate, asset.Revision, asset.CreatedAt, asset.UpdatedAt,
		))
		if createErr != nil {
			return nil, translateAtlasWriteError("create Atlas asset", createErr)
		}
		if eventErr := insertLifecycleEvent(ctx, transaction, initialEvents[index]); eventErr != nil {
			return nil, eventErr
		}
		created = append(created, item)
	}
	if err := transaction.Commit(); err != nil {
		return nil, fmt.Errorf("commit Atlas asset creation: %w", err)
	}
	return created, nil
}

func (s *AtlasStore) UpdateAsset(ctx context.Context, asset domain.Asset, expectedRevision int64, lifecycleEvent *domain.AssetLifecycleEvent) (domain.Asset, error) {
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return domain.Asset{}, fmt.Errorf("begin Atlas asset update: %w", err)
	}
	defer transaction.Rollback()
	modelContext, err := marshalAtlasModelContext(asset.ModelContext)
	if err != nil {
		return domain.Asset{}, err
	}
	updated, err := scanAtlasAsset(transaction.QueryRowContext(ctx, `
		UPDATE atlas_assets
		SET model_id = NULLIF($3, ''), model_context = $4, name = $5, kind = $6,
			asset_tag = $7, normalized_asset_tag = lower(btrim($7)), serial_number = $8,
			normalized_serial_number = lower(btrim($8)), hostname = $9, deployment_notes = $10,
			site_id = NULLIF($11, ''), building_id = NULLIF($12, ''), room_id = NULLIF($13, ''),
			department_id = NULLIF($14, ''), user_id = NULLIF($15, ''), status = $16,
			purchase_date = $17, revision = revision + 1, updated_at = $18
		WHERE organization_id = $1 AND id = $2 AND revision = $19
		RETURNING `+atlasAssetColumns,
		asset.OrganizationID, asset.ID, asset.ModelID, modelContext, asset.Name, asset.Kind, asset.AssetTag, asset.SerialNumber,
		asset.Hostname, asset.DeploymentNotes, asset.SiteID, asset.BuildingID, asset.RoomID, asset.DepartmentID, asset.UserID,
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
	var modelID, siteID, buildingID, roomID, departmentID, userID sql.NullString
	var rawModelContext []byte
	var purchaseDate sql.NullTime
	if err := scanner.Scan(
		&asset.OrganizationID, &asset.ID, &modelID, &rawModelContext, &asset.Name, &asset.Kind, &asset.AssetTag,
		&asset.SerialNumber, &asset.Hostname, &asset.DeploymentNotes, &siteID, &buildingID, &roomID, &departmentID,
		&userID, &asset.Status, &purchaseDate, &asset.Revision, &asset.CreatedAt, &asset.UpdatedAt,
	); err != nil {
		return domain.Asset{}, err
	}
	asset.ModelID = modelID.String
	if len(rawModelContext) > 0 && string(rawModelContext) != "{}" {
		asset.ModelContext = &domain.AssetModelContext{}
		if err := json.Unmarshal(rawModelContext, asset.ModelContext); err != nil {
			return domain.Asset{}, fmt.Errorf("decode Atlas asset model context: %w", err)
		}
	}
	asset.SiteID, asset.BuildingID, asset.RoomID = siteID.String, buildingID.String, roomID.String
	asset.DepartmentID, asset.UserID = departmentID.String, userID.String
	if purchaseDate.Valid {
		value := purchaseDate.Time.UTC()
		asset.PurchaseDate = &value
	}
	return asset, nil
}

func marshalAtlasModelContext(value *domain.AssetModelContext) ([]byte, error) {
	if value == nil {
		return []byte(`{}`), nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal Atlas asset model context: %w", err)
	}
	return encoded, nil
}

func marshalAtlasSpecifications(value map[string]string) ([]byte, error) {
	if len(value) == 0 {
		return []byte(`{}`), nil
	}
	return json.Marshal(value)
}

func scanAtlasModel(scanner atlasScanner, withCount bool) (domain.AssetModel, error) {
	var model domain.AssetModel
	var rawSpecifications []byte
	destinations := []any{
		&model.OrganizationID, &model.ID, &model.Manufacturer, &model.Name, &model.ModelNumber, &model.Kind,
		&model.VendorIdentifier, &rawSpecifications, &model.SupportURL, &model.WarrantyMonths,
		&model.UsefulLifeMonths, &model.Status, &model.SourceSystemID, &model.SourceRecordID,
		&model.Revision, &model.CreatedAt, &model.UpdatedAt,
	}
	if withCount {
		destinations = append(destinations, &model.InstanceCount)
	}
	if err := scanner.Scan(destinations...); err != nil {
		return domain.AssetModel{}, err
	}
	if len(rawSpecifications) > 0 {
		if err := json.Unmarshal(rawSpecifications, &model.Specifications); err != nil {
			return domain.AssetModel{}, fmt.Errorf("decode Atlas model specifications: %w", err)
		}
	}
	if len(model.Specifications) == 0 {
		model.Specifications = nil
	}
	return model, nil
}

func (s *AtlasStore) modelExists(ctx context.Context, organizationID, id string) (bool, error) {
	var exists bool
	if err := s.database.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM atlas_models WHERE organization_id = $1 AND id = $2
	)`, organizationID, id).Scan(&exists); err != nil {
		return false, fmt.Errorf("check Atlas model existence: %w", err)
	}
	return exists, nil
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
