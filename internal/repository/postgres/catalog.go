package postgres

// Requirement: REQ-ATLAS-CATALOG-001. Feature: inventory.catalog. GitHub: #9.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxlemke/stewardmesh/internal/catalog"
)

type CatalogStore struct{ database *sql.DB }

var _ catalog.Store = (*CatalogStore)(nil)

func NewCatalogStore(database *sql.DB) (*CatalogStore, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &CatalogStore{database: database}, nil
}

const catalogConfigurationColumns = `organization_id, id, model_id, name, sku, status, specifications, revision, created_at, updated_at`
const catalogPriceColumns = `organization_id, id, model_id, configuration_id, price_kind, amount_minor, currency, effective_from, effective_to, source_reference, revision, created_at`
const catalogUpgradePathColumns = `organization_id, id, from_model_id, from_configuration_id, to_model_id, to_configuration_id, relationship_kind, effective_from, revision, created_at`

func (s *CatalogStore) Snapshot(ctx context.Context, organizationID string) (catalog.Snapshot, error) {
	transaction, err := s.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return catalog.Snapshot{}, fmt.Errorf("begin Atlas Catalog snapshot: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	result := catalog.Snapshot{}
	if result.Configurations, err = listCatalogConfigurations(ctx, transaction, organizationID, ""); err != nil {
		return catalog.Snapshot{}, err
	}
	if result.Prices, err = listCatalogPrices(ctx, transaction, organizationID, "", ""); err != nil {
		return catalog.Snapshot{}, err
	}
	if result.UpgradePaths, err = listCatalogUpgradePaths(ctx, transaction, organizationID, "", ""); err != nil {
		return catalog.Snapshot{}, err
	}
	if err := transaction.Commit(); err != nil {
		return catalog.Snapshot{}, fmt.Errorf("complete Atlas Catalog snapshot: %w", err)
	}
	return result, nil
}

func (s *CatalogStore) ListConfigurations(ctx context.Context, organizationID, modelID string) ([]catalog.Configuration, error) {
	return listCatalogConfigurations(ctx, s.database, organizationID, modelID)
}

func (s *CatalogStore) GetConfiguration(ctx context.Context, organizationID, configurationID string) (catalog.Configuration, error) {
	item, err := scanCatalogConfiguration(s.database.QueryRowContext(ctx, `
		SELECT `+catalogConfigurationColumns+` FROM atlas_catalog_configurations
		WHERE organization_id = $1 AND id = $2`, organizationID, configurationID))
	return item, translateCatalogRead("get Atlas Catalog configuration", err)
}

func (s *CatalogStore) CreateConfiguration(ctx context.Context, configuration catalog.Configuration) (catalog.Configuration, error) {
	specifications, err := marshalCatalogSpecifications(configuration.Specifications)
	if err != nil {
		return catalog.Configuration{}, err
	}
	item, err := scanCatalogConfiguration(s.database.QueryRowContext(ctx, `
		INSERT INTO atlas_catalog_configurations
			(organization_id, id, model_id, name, sku, status, specifications, revision, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING `+catalogConfigurationColumns,
		configuration.OrganizationID, configuration.ID, configuration.ModelID, configuration.Name, configuration.SKU,
		configuration.Status, specifications, configuration.Revision, configuration.CreatedAt, configuration.UpdatedAt))
	return item, translateCatalogWrite("create Atlas Catalog configuration", err)
}

func (s *CatalogStore) ListPrices(ctx context.Context, organizationID, modelID, configurationID string) ([]catalog.Price, error) {
	return listCatalogPrices(ctx, s.database, organizationID, modelID, configurationID)
}

func (s *CatalogStore) GetPrice(ctx context.Context, organizationID, priceID string) (catalog.Price, error) {
	item, err := scanCatalogPrice(s.database.QueryRowContext(ctx, `
		SELECT `+catalogPriceColumns+` FROM atlas_catalog_prices
		WHERE organization_id = $1 AND id = $2`, organizationID, priceID))
	return item, translateCatalogRead("get Atlas Catalog price", err)
}

func (s *CatalogStore) CreatePrice(ctx context.Context, price catalog.Price) (catalog.Price, error) {
	if price.Revision != 1 {
		return catalog.Price{}, catalog.ErrInvalidInput
	}
	item, err := scanCatalogPrice(s.database.QueryRowContext(ctx, `
		INSERT INTO atlas_catalog_prices
			(organization_id, id, model_id, configuration_id, price_kind, amount_minor, currency,
			 effective_from, effective_to, source_reference, revision, created_at)
		VALUES ($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING `+catalogPriceColumns,
		price.OrganizationID, price.ID, price.ModelID, price.ConfigurationID, price.Kind, price.AmountMinor,
		price.Currency, price.EffectiveFrom, price.EffectiveTo, price.SourceReference, price.Revision, price.CreatedAt))
	return item, translateCatalogWrite("create Atlas Catalog price", err)
}

func (s *CatalogStore) ListUpgradePaths(ctx context.Context, organizationID, fromModelID, fromConfigurationID string) ([]catalog.UpgradePath, error) {
	return listCatalogUpgradePaths(ctx, s.database, organizationID, fromModelID, fromConfigurationID)
}

func (s *CatalogStore) GetUpgradePath(ctx context.Context, organizationID, pathID string) (catalog.UpgradePath, error) {
	item, err := scanCatalogUpgradePath(s.database.QueryRowContext(ctx, `
		SELECT `+catalogUpgradePathColumns+` FROM atlas_catalog_upgrade_paths
		WHERE organization_id = $1 AND id = $2`, organizationID, pathID))
	return item, translateCatalogRead("get Atlas Catalog upgrade path", err)
}

func (s *CatalogStore) CreateUpgradePath(ctx context.Context, path catalog.UpgradePath) (catalog.UpgradePath, error) {
	if path.Revision != 1 {
		return catalog.UpgradePath{}, catalog.ErrInvalidInput
	}
	item, err := scanCatalogUpgradePath(s.database.QueryRowContext(ctx, `
		INSERT INTO atlas_catalog_upgrade_paths
			(organization_id, id, from_model_id, from_configuration_id, to_model_id, to_configuration_id,
			 relationship_kind, effective_from, revision, created_at)
		VALUES ($1,$2,$3,NULLIF($4,''),$5,NULLIF($6,''),$7,$8,$9,$10)
		RETURNING `+catalogUpgradePathColumns,
		path.OrganizationID, path.ID, path.FromModelID, path.FromConfigurationID, path.ToModelID,
		path.ToConfigurationID, path.Kind, path.EffectiveFrom, path.Revision, path.CreatedAt))
	return item, translateCatalogWrite("create Atlas Catalog upgrade path", err)
}

type catalogRowsQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func listCatalogConfigurations(ctx context.Context, queryer catalogRowsQueryer, organizationID, modelID string) ([]catalog.Configuration, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT `+catalogConfigurationColumns+` FROM atlas_catalog_configurations
		WHERE organization_id = $1 AND ($2 = '' OR model_id = $2)
		ORDER BY id`, organizationID, modelID)
	if err != nil {
		return nil, fmt.Errorf("list Atlas Catalog configurations: %w", err)
	}
	defer rows.Close()
	items := []catalog.Configuration{}
	for rows.Next() {
		item, err := scanCatalogConfiguration(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Atlas Catalog configuration: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Atlas Catalog configurations: %w", err)
	}
	return items, nil
}

func listCatalogPrices(ctx context.Context, queryer catalogRowsQueryer, organizationID, modelID, configurationID string) ([]catalog.Price, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT `+catalogPriceColumns+` FROM atlas_catalog_prices
		WHERE organization_id = $1 AND ($2 = '' OR model_id = $2)
		  AND ($3 = '' OR configuration_id = $3)
		ORDER BY id`, organizationID, modelID, configurationID)
	if err != nil {
		return nil, fmt.Errorf("list Atlas Catalog prices: %w", err)
	}
	defer rows.Close()
	items := []catalog.Price{}
	for rows.Next() {
		item, err := scanCatalogPrice(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Atlas Catalog price: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Atlas Catalog prices: %w", err)
	}
	return items, nil
}

func listCatalogUpgradePaths(ctx context.Context, queryer catalogRowsQueryer, organizationID, fromModelID, fromConfigurationID string) ([]catalog.UpgradePath, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT `+catalogUpgradePathColumns+` FROM atlas_catalog_upgrade_paths
		WHERE organization_id = $1 AND ($2 = '' OR from_model_id = $2)
		  AND ($3 = '' OR from_configuration_id = $3)
		ORDER BY id`, organizationID, fromModelID, fromConfigurationID)
	if err != nil {
		return nil, fmt.Errorf("list Atlas Catalog upgrade paths: %w", err)
	}
	defer rows.Close()
	items := []catalog.UpgradePath{}
	for rows.Next() {
		item, err := scanCatalogUpgradePath(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Atlas Catalog upgrade path: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Atlas Catalog upgrade paths: %w", err)
	}
	return items, nil
}

type catalogScanner interface{ Scan(...any) error }

func scanCatalogConfiguration(scanner catalogScanner) (catalog.Configuration, error) {
	var item catalog.Configuration
	var specifications []byte
	err := scanner.Scan(&item.OrganizationID, &item.ID, &item.ModelID, &item.Name, &item.SKU, &item.Status,
		&specifications, &item.Revision, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return catalog.Configuration{}, err
	}
	if err := json.Unmarshal(specifications, &item.Specifications); err != nil || item.Specifications == nil {
		return catalog.Configuration{}, fmt.Errorf("decode Atlas Catalog specifications: %w", err)
	}
	return item, nil
}

func scanCatalogPrice(scanner catalogScanner) (catalog.Price, error) {
	var item catalog.Price
	var configurationID sql.NullString
	err := scanner.Scan(&item.OrganizationID, &item.ID, &item.ModelID, &configurationID, &item.Kind,
		&item.AmountMinor, &item.Currency, &item.EffectiveFrom, &item.EffectiveTo, &item.SourceReference,
		&item.Revision, &item.CreatedAt)
	item.ConfigurationID = configurationID.String
	return item, err
}

func scanCatalogUpgradePath(scanner catalogScanner) (catalog.UpgradePath, error) {
	var item catalog.UpgradePath
	var fromConfigurationID, toConfigurationID sql.NullString
	err := scanner.Scan(&item.OrganizationID, &item.ID, &item.FromModelID, &fromConfigurationID,
		&item.ToModelID, &toConfigurationID, &item.Kind, &item.EffectiveFrom, &item.Revision, &item.CreatedAt)
	item.FromConfigurationID, item.ToConfigurationID = fromConfigurationID.String, toConfigurationID.String
	return item, err
}

func marshalCatalogSpecifications(specifications map[string]string) ([]byte, error) {
	if specifications == nil {
		specifications = map[string]string{}
	}
	encoded, err := json.Marshal(specifications)
	if err != nil {
		return nil, fmt.Errorf("encode Atlas Catalog specifications: %w", err)
	}
	return encoded, nil
}

func translateCatalogRead(operation string, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return catalog.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return nil
}

func translateCatalogWrite(operation string, err error) error {
	if err == nil {
		return nil
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505":
			return catalog.ErrConflict
		case "23503":
			return catalog.ErrNotFound
		case "23502", "23514", "22P02", "22001":
			return catalog.ErrInvalidInput
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
