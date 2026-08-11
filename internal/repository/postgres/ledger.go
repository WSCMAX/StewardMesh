package postgres

// PostgreSQL Ledger adapter. Requirement: REQ-LEDGER-001.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxlemke/stewardmesh/internal/ledger"
)

type LedgerStore struct{ database *sql.DB }

func NewLedgerStore(database *sql.DB) (*LedgerStore, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &LedgerStore{database: database}, nil
}

func (s *LedgerStore) Snapshot(ctx context.Context, organizationID string) (ledger.Snapshot, error) {
	var result ledger.Snapshot
	var err error
	if result.Vendors, err = s.listVendors(ctx, organizationID); err != nil {
		return ledger.Snapshot{}, err
	}
	if result.PurchaseOrders, err = s.listPurchaseOrders(ctx, organizationID); err != nil {
		return ledger.Snapshot{}, err
	}
	if result.Contracts, err = s.listContracts(ctx, organizationID); err != nil {
		return ledger.Snapshot{}, err
	}
	if result.Commitments, err = s.listCommitments(ctx, organizationID); err != nil {
		return ledger.Snapshot{}, err
	}
	if result.Budgets, err = s.listBudgets(ctx, organizationID); err != nil {
		return ledger.Snapshot{}, err
	}
	if result.Costs, err = s.listCosts(ctx, organizationID); err != nil {
		return ledger.Snapshot{}, err
	}
	return result, nil
}

func (s *LedgerStore) GetVendor(ctx context.Context, organizationID, id string) (ledger.Vendor, error) {
	item, err := scanVendor(s.database.QueryRowContext(ctx, vendorSelect+` WHERE organization_id = $1 AND id = $2`, organizationID, id))
	return item, translateLedgerReadError("get Ledger vendor", err)
}

func (s *LedgerStore) CreateVendor(ctx context.Context, item ledger.Vendor) (ledger.Vendor, error) {
	created, err := scanVendor(s.database.QueryRowContext(ctx, `
		INSERT INTO ledger_vendors (organization_id, id, name, normalized_name, external_id, status, revision, created_at, updated_at)
		VALUES ($1, $2, $3, lower(btrim($3)), NULLIF($4, ''), $5, $6, $7, $8)
		RETURNING organization_id, id, name, COALESCE(external_id, ''), status, revision, created_at, updated_at
	`, item.OrganizationID, item.ID, item.Name, item.ExternalID, item.Status, item.Revision, item.CreatedAt, item.UpdatedAt))
	return created, translateLedgerWriteError("create Ledger vendor", err)
}

func (s *LedgerStore) GetPurchaseOrder(ctx context.Context, organizationID, id string) (ledger.PurchaseOrder, error) {
	item, err := scanPurchaseOrder(s.database.QueryRowContext(ctx, purchaseOrderSelect+` WHERE organization_id = $1 AND id = $2`, organizationID, id))
	return item, translateLedgerReadError("get Ledger purchase order", err)
}

func (s *LedgerStore) CreatePurchaseOrder(ctx context.Context, item ledger.PurchaseOrder) (ledger.PurchaseOrder, error) {
	created, err := scanPurchaseOrder(s.database.QueryRowContext(ctx, `
		INSERT INTO ledger_purchase_orders (
			organization_id, id, number, normalized_number, vendor_id, status, currency, total_minor, ordered_on,
			asset_ids, receipt_document_ids, revision, created_at, updated_at
		) VALUES ($1, $2, $3, lower(btrim($3)), $4, $5, $6, $7, $8,
			CASE WHEN $9 = '' THEN '{}'::text[] ELSE string_to_array($9, ',') END,
			CASE WHEN $10 = '' THEN '{}'::text[] ELSE string_to_array($10, ',') END, $11, $12, $13)
		RETURNING organization_id, id, number, vendor_id, status, currency, total_minor, ordered_on,
			array_to_json(asset_ids)::text, array_to_json(receipt_document_ids)::text, revision, created_at, updated_at
	`, item.OrganizationID, item.ID, item.Number, item.VendorID, item.Status, item.Currency, item.TotalMinor, item.OrderedOn,
		strings.Join(item.AssetIDs, ","), strings.Join(item.ReceiptDocumentIDs, ","), item.Revision, item.CreatedAt, item.UpdatedAt))
	return created, translateLedgerWriteError("create Ledger purchase order", err)
}

func (s *LedgerStore) UpdatePurchaseOrder(ctx context.Context, item ledger.PurchaseOrder, expectedRevision int64) (ledger.PurchaseOrder, error) {
	updated, err := scanPurchaseOrder(s.database.QueryRowContext(ctx, `
		UPDATE ledger_purchase_orders SET status = $3, revision = $4, updated_at = $5
		WHERE organization_id = $1 AND id = $2 AND revision = $6
		RETURNING organization_id, id, number, vendor_id, status, currency, total_minor, ordered_on,
			array_to_json(asset_ids)::text, array_to_json(receipt_document_ids)::text, revision, created_at, updated_at
	`, item.OrganizationID, item.ID, item.Status, item.Revision, item.UpdatedAt, expectedRevision))
	if errors.Is(err, sql.ErrNoRows) {
		return ledger.PurchaseOrder{}, s.resolveRevisionConflict(ctx, "ledger_purchase_orders", item.OrganizationID, item.ID)
	}
	return updated, translateLedgerWriteError("update Ledger purchase order", err)
}

func (s *LedgerStore) GetContract(ctx context.Context, organizationID, id string) (ledger.Contract, error) {
	item, err := scanContract(s.database.QueryRowContext(ctx, contractSelect+` WHERE organization_id = $1 AND id = $2`, organizationID, id))
	return item, translateLedgerReadError("get Ledger contract", err)
}

func (s *LedgerStore) CreateContract(ctx context.Context, item ledger.Contract) (ledger.Contract, error) {
	created, err := scanContract(s.database.QueryRowContext(ctx, `
		INSERT INTO ledger_contracts (
			organization_id, id, name, vendor_id, operational_status, financial_status, currency, ceiling_minor,
			starts_on, ends_on, renews_on, document_ids, revision, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
			CASE WHEN $12 = '' THEN '{}'::text[] ELSE string_to_array($12, ',') END, $13, $14, $15)
		RETURNING organization_id, id, name, vendor_id, operational_status, financial_status, currency, ceiling_minor,
			starts_on, ends_on, renews_on, array_to_json(document_ids)::text, revision, created_at, updated_at
	`, item.OrganizationID, item.ID, item.Name, item.VendorID, item.OperationalStatus, item.FinancialStatus, item.Currency,
		item.CeilingMinor, item.StartsOn, item.EndsOn, item.RenewsOn, strings.Join(item.DocumentIDs, ","), item.Revision, item.CreatedAt, item.UpdatedAt))
	return created, translateLedgerWriteError("create Ledger contract", err)
}

func (s *LedgerStore) UpdateContract(ctx context.Context, item ledger.Contract, expectedRevision int64) (ledger.Contract, error) {
	updated, err := scanContract(s.database.QueryRowContext(ctx, `
		UPDATE ledger_contracts SET operational_status = $3, financial_status = $4, revision = $5, updated_at = $6
		WHERE organization_id = $1 AND id = $2 AND revision = $7
		RETURNING organization_id, id, name, vendor_id, operational_status, financial_status, currency, ceiling_minor,
			starts_on, ends_on, renews_on, array_to_json(document_ids)::text, revision, created_at, updated_at
	`, item.OrganizationID, item.ID, item.OperationalStatus, item.FinancialStatus, item.Revision, item.UpdatedAt, expectedRevision))
	if errors.Is(err, sql.ErrNoRows) {
		return ledger.Contract{}, s.resolveRevisionConflict(ctx, "ledger_contracts", item.OrganizationID, item.ID)
	}
	return updated, translateLedgerWriteError("update Ledger contract", err)
}

func (s *LedgerStore) CreateCommitment(ctx context.Context, item ledger.Commitment) (ledger.Commitment, error) {
	created, err := scanCommitment(s.database.QueryRowContext(ctx, `
		INSERT INTO ledger_commitments (
			organization_id, id, contract_id, kind, description, currency, amount_minor, starts_on, ends_on,
			fiscal_period, scenario, revision, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING organization_id, id, contract_id, kind, description, currency, amount_minor, starts_on, ends_on,
			fiscal_period, scenario, revision, created_at, updated_at
	`, item.OrganizationID, item.ID, item.ContractID, item.Kind, item.Description, item.Currency, item.AmountMinor,
		item.StartsOn, item.EndsOn, item.FiscalPeriod, item.Scenario, item.Revision, item.CreatedAt, item.UpdatedAt))
	return created, translateLedgerWriteError("create Ledger commitment", err)
}

func (s *LedgerStore) CreateBudget(ctx context.Context, item ledger.Budget) (ledger.Budget, error) {
	created, err := scanBudget(s.database.QueryRowContext(ctx, `
		INSERT INTO ledger_budgets (
			organization_id, id, name, normalized_name, fiscal_period, scenario, department_id, site_id, currency,
			allocated_minor, revision, created_at, updated_at
		) VALUES ($1, $2, $3, lower(btrim($3)), $4, $5, NULLIF($6, ''), NULLIF($7, ''), $8, $9, $10, $11, $12)
		RETURNING organization_id, id, name, fiscal_period, scenario, COALESCE(department_id, ''), COALESCE(site_id, ''),
			currency, allocated_minor, revision, created_at, updated_at
	`, item.OrganizationID, item.ID, item.Name, item.FiscalPeriod, item.Scenario, item.DepartmentID, item.SiteID,
		item.Currency, item.AllocatedMinor, item.Revision, item.CreatedAt, item.UpdatedAt))
	return created, translateLedgerWriteError("create Ledger budget", err)
}

func (s *LedgerStore) GetCostBySource(ctx context.Context, organizationID, sourceSystemID, sourceRecordID string) (ledger.CostRecord, error) {
	item, err := scanCost(s.database.QueryRowContext(ctx, costSelect+`
		WHERE organization_id = $1 AND lower(source_system_id) = lower($2) AND source_record_id = $3
	`, organizationID, sourceSystemID, sourceRecordID))
	return item, translateLedgerReadError("get Ledger cost source", err)
}

func (s *LedgerStore) CreateCost(ctx context.Context, item ledger.CostRecord) (ledger.CostRecord, error) {
	created, err := scanCost(s.database.QueryRowContext(ctx, `
		INSERT INTO ledger_costs (
			organization_id, id, description, kind, currency, amount_minor, fiscal_period, scenario,
			purchase_order_id, contract_id, asset_id, department_id, site_id, document_id, external_reference,
			source_system_id, source_record_id, revision, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, ''), NULLIF($10, ''), NULLIF($11, ''),
			NULLIF($12, ''), NULLIF($13, ''), NULLIF($14, ''), NULLIF($15, ''), NULLIF($16, ''), NULLIF($17, ''), $18, $19, $20)
		RETURNING `+costColumns+`
	`, costArguments(item)...))
	return created, translateLedgerWriteError("create Ledger cost", err)
}

func (s *LedgerStore) UpdateCost(ctx context.Context, item ledger.CostRecord, expectedRevision int64) (ledger.CostRecord, error) {
	updated, err := scanCost(s.database.QueryRowContext(ctx, `
		UPDATE ledger_costs SET description = $3, kind = $4, currency = $5, amount_minor = $6,
			fiscal_period = $7, scenario = $8, purchase_order_id = NULLIF($9, ''), contract_id = NULLIF($10, ''),
			asset_id = NULLIF($11, ''), department_id = NULLIF($12, ''), site_id = NULLIF($13, ''), document_id = NULLIF($14, ''),
			external_reference = NULLIF($15, ''), revision = $18, updated_at = $19
		WHERE organization_id = $1 AND id = $2 AND source_system_id = NULLIF($16, '') AND source_record_id = NULLIF($17, '') AND revision = $20
		RETURNING `+costColumns+`
	`, item.OrganizationID, item.ID, item.Description, item.Kind, item.Currency, item.AmountMinor, item.FiscalPeriod,
		item.Scenario, item.PurchaseOrderID, item.ContractID, item.AssetID, item.DepartmentID, item.SiteID, item.DocumentID,
		item.ExternalReference, item.SourceSystemID, item.SourceRecordID, item.Revision, item.UpdatedAt, expectedRevision))
	if errors.Is(err, sql.ErrNoRows) {
		return ledger.CostRecord{}, s.resolveRevisionConflict(ctx, "ledger_costs", item.OrganizationID, item.ID)
	}
	return updated, translateLedgerWriteError("update Ledger cost", err)
}

const vendorSelect = `SELECT organization_id, id, name, COALESCE(external_id, ''), status, revision, created_at, updated_at FROM ledger_vendors`
const purchaseOrderSelect = `SELECT organization_id, id, number, vendor_id, status, currency, total_minor, ordered_on, array_to_json(asset_ids)::text, array_to_json(receipt_document_ids)::text, revision, created_at, updated_at FROM ledger_purchase_orders`
const contractSelect = `SELECT organization_id, id, name, vendor_id, operational_status, financial_status, currency, ceiling_minor, starts_on, ends_on, renews_on, array_to_json(document_ids)::text, revision, created_at, updated_at FROM ledger_contracts`
const commitmentSelect = `SELECT organization_id, id, contract_id, kind, description, currency, amount_minor, starts_on, ends_on, fiscal_period, scenario, revision, created_at, updated_at FROM ledger_commitments`
const budgetSelect = `SELECT organization_id, id, name, fiscal_period, scenario, COALESCE(department_id, ''), COALESCE(site_id, ''), currency, allocated_minor, revision, created_at, updated_at FROM ledger_budgets`
const costColumns = `organization_id, id, description, kind, currency, amount_minor, fiscal_period, scenario, COALESCE(purchase_order_id, ''), COALESCE(contract_id, ''), COALESCE(asset_id, ''), COALESCE(department_id, ''), COALESCE(site_id, ''), COALESCE(document_id, ''), COALESCE(external_reference, ''), COALESCE(source_system_id, ''), COALESCE(source_record_id, ''), revision, created_at, updated_at`
const costSelect = `SELECT ` + costColumns + ` FROM ledger_costs`

type ledgerScanner interface{ Scan(...any) error }

func scanVendor(row ledgerScanner) (ledger.Vendor, error) {
	var item ledger.Vendor
	err := row.Scan(&item.OrganizationID, &item.ID, &item.Name, &item.ExternalID, &item.Status, &item.Revision, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func scanPurchaseOrder(row ledgerScanner) (ledger.PurchaseOrder, error) {
	var item ledger.PurchaseOrder
	var orderedOn sql.NullTime
	var assetJSON, documentJSON string
	err := row.Scan(&item.OrganizationID, &item.ID, &item.Number, &item.VendorID, &item.Status, &item.Currency, &item.TotalMinor,
		&orderedOn, &assetJSON, &documentJSON, &item.Revision, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return ledger.PurchaseOrder{}, err
	}
	if orderedOn.Valid {
		item.OrderedOn = &orderedOn.Time
	}
	if err := json.Unmarshal([]byte(assetJSON), &item.AssetIDs); err != nil {
		return ledger.PurchaseOrder{}, fmt.Errorf("decode Ledger asset ids: %w", err)
	}
	if err := json.Unmarshal([]byte(documentJSON), &item.ReceiptDocumentIDs); err != nil {
		return ledger.PurchaseOrder{}, fmt.Errorf("decode Ledger receipt ids: %w", err)
	}
	return item, nil
}

func scanContract(row ledgerScanner) (ledger.Contract, error) {
	var item ledger.Contract
	var renewsOn sql.NullTime
	var documentJSON string
	err := row.Scan(&item.OrganizationID, &item.ID, &item.Name, &item.VendorID, &item.OperationalStatus, &item.FinancialStatus,
		&item.Currency, &item.CeilingMinor, &item.StartsOn, &item.EndsOn, &renewsOn, &documentJSON, &item.Revision, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return ledger.Contract{}, err
	}
	if renewsOn.Valid {
		item.RenewsOn = &renewsOn.Time
	}
	if err := json.Unmarshal([]byte(documentJSON), &item.DocumentIDs); err != nil {
		return ledger.Contract{}, fmt.Errorf("decode Ledger document ids: %w", err)
	}
	return item, nil
}

func scanCommitment(row ledgerScanner) (ledger.Commitment, error) {
	var item ledger.Commitment
	err := row.Scan(&item.OrganizationID, &item.ID, &item.ContractID, &item.Kind, &item.Description, &item.Currency,
		&item.AmountMinor, &item.StartsOn, &item.EndsOn, &item.FiscalPeriod, &item.Scenario, &item.Revision, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func scanBudget(row ledgerScanner) (ledger.Budget, error) {
	var item ledger.Budget
	err := row.Scan(&item.OrganizationID, &item.ID, &item.Name, &item.FiscalPeriod, &item.Scenario, &item.DepartmentID,
		&item.SiteID, &item.Currency, &item.AllocatedMinor, &item.Revision, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func scanCost(row ledgerScanner) (ledger.CostRecord, error) {
	var item ledger.CostRecord
	err := row.Scan(&item.OrganizationID, &item.ID, &item.Description, &item.Kind, &item.Currency, &item.AmountMinor,
		&item.FiscalPeriod, &item.Scenario, &item.PurchaseOrderID, &item.ContractID, &item.AssetID, &item.DepartmentID,
		&item.SiteID, &item.DocumentID, &item.ExternalReference, &item.SourceSystemID, &item.SourceRecordID,
		&item.Revision, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func costArguments(item ledger.CostRecord) []any {
	return []any{item.OrganizationID, item.ID, item.Description, item.Kind, item.Currency, item.AmountMinor, item.FiscalPeriod,
		item.Scenario, item.PurchaseOrderID, item.ContractID, item.AssetID, item.DepartmentID, item.SiteID, item.DocumentID,
		item.ExternalReference, item.SourceSystemID, item.SourceRecordID, item.Revision, item.CreatedAt, item.UpdatedAt}
}

func (s *LedgerStore) listVendors(ctx context.Context, organizationID string) ([]ledger.Vendor, error) {
	rows, err := s.database.QueryContext(ctx, vendorSelect+` WHERE organization_id = $1 ORDER BY normalized_name, id`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list Ledger vendors: %w", err)
	}
	defer rows.Close()
	items := make([]ledger.Vendor, 0)
	for rows.Next() {
		item, err := scanVendor(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Ledger vendor: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *LedgerStore) listPurchaseOrders(ctx context.Context, organizationID string) ([]ledger.PurchaseOrder, error) {
	rows, err := s.database.QueryContext(ctx, purchaseOrderSelect+` WHERE organization_id = $1 ORDER BY normalized_number, id`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list Ledger purchase orders: %w", err)
	}
	defer rows.Close()
	items := make([]ledger.PurchaseOrder, 0)
	for rows.Next() {
		item, err := scanPurchaseOrder(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Ledger purchase order: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *LedgerStore) listContracts(ctx context.Context, organizationID string) ([]ledger.Contract, error) {
	rows, err := s.database.QueryContext(ctx, contractSelect+` WHERE organization_id = $1 ORDER BY lower(name), id`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list Ledger contracts: %w", err)
	}
	defer rows.Close()
	items := make([]ledger.Contract, 0)
	for rows.Next() {
		item, err := scanContract(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Ledger contract: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *LedgerStore) listCommitments(ctx context.Context, organizationID string) ([]ledger.Commitment, error) {
	rows, err := s.database.QueryContext(ctx, commitmentSelect+` WHERE organization_id = $1 ORDER BY created_at, id`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list Ledger commitments: %w", err)
	}
	defer rows.Close()
	items := make([]ledger.Commitment, 0)
	for rows.Next() {
		item, err := scanCommitment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Ledger commitment: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *LedgerStore) listBudgets(ctx context.Context, organizationID string) ([]ledger.Budget, error) {
	rows, err := s.database.QueryContext(ctx, budgetSelect+` WHERE organization_id = $1 ORDER BY fiscal_period, scenario, lower(name), id`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list Ledger budgets: %w", err)
	}
	defer rows.Close()
	items := make([]ledger.Budget, 0)
	for rows.Next() {
		item, err := scanBudget(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Ledger budget: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *LedgerStore) listCosts(ctx context.Context, organizationID string) ([]ledger.CostRecord, error) {
	rows, err := s.database.QueryContext(ctx, costSelect+` WHERE organization_id = $1 ORDER BY created_at, id`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list Ledger costs: %w", err)
	}
	defer rows.Close()
	items := make([]ledger.CostRecord, 0)
	for rows.Next() {
		item, err := scanCost(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Ledger cost: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *LedgerStore) resolveRevisionConflict(ctx context.Context, table, organizationID, id string) error {
	var exists bool
	query := fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM %s WHERE organization_id = $1 AND id = $2)", table)
	if err := s.database.QueryRowContext(ctx, query, organizationID, id).Scan(&exists); err != nil {
		return fmt.Errorf("check Ledger revision conflict: %w", err)
	}
	if exists {
		return ledger.ErrConflict
	}
	return ledger.ErrNotFound
}

func translateLedgerReadError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ledger.ErrNotFound
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func translateLedgerWriteError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505":
			return ledger.ErrConflict
		case "23503":
			return ledger.ErrReferenceMissing
		case "23502", "23514", "22P02":
			return ledger.ErrInvalidInput
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
