package postgres

// PostgreSQL Stack adapter. Requirement: REQ-STACK-001. Feature: software.licenses.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxlemke/stewardmesh/internal/stack"
)

type StackStore struct{ database *sql.DB }

func NewStackStore(database *sql.DB) (*StackStore, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &StackStore{database: database}, nil
}

type stackQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const stackProductSelect = `SELECT organization_id, id, name, publisher, category, status,
	COALESCE(source_system_id, ''), COALESCE(source_record_id, ''), revision, created_at, updated_at FROM stack_products`
const stackVersionSelect = `SELECT organization_id, id, product_id, name, released_on, status,
	COALESCE(source_system_id, ''), COALESCE(source_record_id, ''), revision, created_at, updated_at FROM stack_versions`
const stackInstallationSelect = `SELECT organization_id, id, version_id, asset_id, status, usage_state, installed_at,
	last_used_at, removed_at, COALESCE(source_system_id, ''), COALESCE(source_record_id, ''), revision, created_at, updated_at FROM stack_installations`
const stackLicenseSelect = `SELECT organization_id, id, product_id, COALESCE(version_id, ''), name, entitlement_metric, quantity, status,
	starts_on, expires_on, COALESCE(vendor_id, ''), COALESCE(purchase_order_id, ''), COALESCE(contract_id, ''),
	COALESCE(cost_record_id, ''), document_ids, COALESCE(source_system_id, ''), COALESCE(source_record_id, ''), revision, created_at, updated_at FROM stack_licenses`
const stackAssignmentSelect = `SELECT organization_id, id, license_id, assignee_kind, assignee_id, seats, usage_state, assigned_at,
	last_used_at, ended_at, COALESCE(source_system_id, ''), COALESCE(source_record_id, ''), revision, created_at, updated_at FROM stack_assignments`

func (s *StackStore) Snapshot(ctx context.Context, organizationID string) (stack.Snapshot, error) {
	tx, err := s.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return stack.Snapshot{}, fmt.Errorf("begin Stack snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result := stack.Snapshot{}
	if result.Products, err = listStackProducts(ctx, tx, organizationID); err != nil {
		return stack.Snapshot{}, err
	}
	if result.Versions, err = listStackVersions(ctx, tx, organizationID); err != nil {
		return stack.Snapshot{}, err
	}
	if result.Installations, err = listStackInstallations(ctx, tx, organizationID); err != nil {
		return stack.Snapshot{}, err
	}
	if result.Licenses, err = listStackLicenses(ctx, tx, organizationID); err != nil {
		return stack.Snapshot{}, err
	}
	if result.Assignments, err = listStackAssignments(ctx, tx, organizationID); err != nil {
		return stack.Snapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return stack.Snapshot{}, fmt.Errorf("commit Stack snapshot: %w", err)
	}
	return result, nil
}

func (s *StackStore) GetProduct(ctx context.Context, organizationID, id string) (stack.Product, error) {
	item, err := scanStackProduct(s.database.QueryRowContext(ctx, stackProductSelect+` WHERE organization_id = $1 AND id = $2`, organizationID, id))
	return item, translateStackReadError("get Stack product", err)
}

func (s *StackStore) GetVersion(ctx context.Context, organizationID, id string) (stack.Version, error) {
	item, err := scanStackVersion(s.database.QueryRowContext(ctx, stackVersionSelect+` WHERE organization_id = $1 AND id = $2`, organizationID, id))
	return item, translateStackReadError("get Stack version", err)
}

func (s *StackStore) GetInstallation(ctx context.Context, organizationID, id string) (stack.Installation, error) {
	item, err := scanStackInstallation(s.database.QueryRowContext(ctx, stackInstallationSelect+` WHERE organization_id = $1 AND id = $2`, organizationID, id))
	return item, translateStackReadError("get Stack installation", err)
}

func (s *StackStore) GetLicense(ctx context.Context, organizationID, id string) (stack.License, error) {
	item, err := scanStackLicense(s.database.QueryRowContext(ctx, stackLicenseSelect+` WHERE organization_id = $1 AND id = $2`, organizationID, id))
	return item, translateStackReadError("get Stack license", err)
}

func (s *StackStore) GetAssignment(ctx context.Context, organizationID, id string) (stack.Assignment, error) {
	item, err := scanStackAssignment(s.database.QueryRowContext(ctx, stackAssignmentSelect+` WHERE organization_id = $1 AND id = $2`, organizationID, id))
	return item, translateStackReadError("get Stack assignment", err)
}

func (s *StackStore) CreateProduct(ctx context.Context, item stack.Product) (stack.Product, bool, error) {
	created, err := scanStackProduct(s.database.QueryRowContext(ctx, `
		INSERT INTO stack_products (organization_id, id, name, publisher, category, status, source_system_id, source_record_id, revision, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''),$9,$10,$11) ON CONFLICT DO NOTHING
		RETURNING organization_id,id,name,publisher,category,status,COALESCE(source_system_id,''),COALESCE(source_record_id,''),revision,created_at,updated_at
	`, item.OrganizationID, item.ID, item.Name, item.Publisher, item.Category, item.Status, item.SourceSystemID, item.SourceRecordID, item.Revision, item.CreatedAt, item.UpdatedAt))
	if err == nil {
		return created, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return stack.Product{}, false, translateStackWriteError("create Stack product", err)
	}
	existing, foundErr := findStackProduct(ctx, s.database, item.OrganizationID, item.ID, item.SourceSystemID, item.SourceRecordID)
	if foundErr == nil && equalPostgresStackProduct(existing, item) {
		return existing, false, nil
	}
	return stack.Product{}, false, resolveStackConflict(foundErr)
}

func (s *StackStore) CreateVersion(ctx context.Context, item stack.Version) (stack.Version, bool, error) {
	created, err := scanStackVersion(s.database.QueryRowContext(ctx, `
		INSERT INTO stack_versions (organization_id,id,product_id,name,released_on,status,source_system_id,source_record_id,revision,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''),$9,$10,$11) ON CONFLICT DO NOTHING
		RETURNING organization_id,id,product_id,name,released_on,status,COALESCE(source_system_id,''),COALESCE(source_record_id,''),revision,created_at,updated_at
	`, item.OrganizationID, item.ID, item.ProductID, item.Name, item.ReleasedOn, item.Status, item.SourceSystemID, item.SourceRecordID, item.Revision, item.CreatedAt, item.UpdatedAt))
	if err == nil {
		return created, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return stack.Version{}, false, translateStackWriteError("create Stack version", err)
	}
	existing, foundErr := findStackVersion(ctx, s.database, item.OrganizationID, item.ID, item.SourceSystemID, item.SourceRecordID)
	if foundErr == nil && equalPostgresStackVersion(existing, item) {
		return existing, false, nil
	}
	return stack.Version{}, false, resolveStackConflict(foundErr)
}

func (s *StackStore) CreateInstallation(ctx context.Context, item stack.Installation) (stack.Installation, bool, error) {
	created, err := scanStackInstallation(s.database.QueryRowContext(ctx, `
		INSERT INTO stack_installations (organization_id,id,version_id,asset_id,status,usage_state,installed_at,last_used_at,removed_at,source_system_id,source_record_id,revision,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,''),NULLIF($11,''),$12,$13,$14) ON CONFLICT DO NOTHING
		RETURNING organization_id,id,version_id,asset_id,status,usage_state,installed_at,last_used_at,removed_at,COALESCE(source_system_id,''),COALESCE(source_record_id,''),revision,created_at,updated_at
	`, item.OrganizationID, item.ID, item.VersionID, item.AssetID, item.Status, item.UsageState, item.InstalledAt, item.LastUsedAt, item.RemovedAt, item.SourceSystemID, item.SourceRecordID, item.Revision, item.CreatedAt, item.UpdatedAt))
	if err == nil {
		return created, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return stack.Installation{}, false, translateStackWriteError("create Stack installation", err)
	}
	existing, foundErr := findStackInstallation(ctx, s.database, item.OrganizationID, item.ID, item.SourceSystemID, item.SourceRecordID)
	if foundErr == nil && equalPostgresStackInstallation(existing, item) {
		return existing, false, nil
	}
	return stack.Installation{}, false, resolveStackConflict(foundErr)
}

func (s *StackStore) CreateLicense(ctx context.Context, item stack.License) (stack.License, bool, error) {
	documents, err := json.Marshal(item.DocumentIDs)
	if err != nil {
		return stack.License{}, false, fmt.Errorf("encode Stack license documents: %w", err)
	}
	created, err := scanStackLicense(s.database.QueryRowContext(ctx, `
		INSERT INTO stack_licenses (organization_id,id,product_id,version_id,name,entitlement_metric,quantity,status,starts_on,expires_on,vendor_id,purchase_order_id,contract_id,cost_record_id,document_ids,source_system_id,source_record_id,revision,created_at,updated_at)
		VALUES ($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8,$9,$10,NULLIF($11,''),NULLIF($12,''),NULLIF($13,''),NULLIF($14,''),$15::jsonb,NULLIF($16,''),NULLIF($17,''),$18,$19,$20) ON CONFLICT DO NOTHING
		RETURNING organization_id,id,product_id,COALESCE(version_id,''),name,entitlement_metric,quantity,status,starts_on,expires_on,COALESCE(vendor_id,''),COALESCE(purchase_order_id,''),COALESCE(contract_id,''),COALESCE(cost_record_id,''),document_ids,COALESCE(source_system_id,''),COALESCE(source_record_id,''),revision,created_at,updated_at
	`, item.OrganizationID, item.ID, item.ProductID, item.VersionID, item.Name, item.EntitlementMetric, item.Quantity, item.Status, item.StartsOn, item.ExpiresOn, item.VendorID, item.PurchaseOrderID, item.ContractID, item.CostRecordID, string(documents), item.SourceSystemID, item.SourceRecordID, item.Revision, item.CreatedAt, item.UpdatedAt))
	if err == nil {
		return created, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return stack.License{}, false, translateStackWriteError("create Stack license", err)
	}
	existing, foundErr := findStackLicense(ctx, s.database, item.OrganizationID, item.ID, item.SourceSystemID, item.SourceRecordID)
	if foundErr == nil && equalPostgresStackLicense(existing, item) {
		return existing, false, nil
	}
	return stack.License{}, false, resolveStackConflict(foundErr)
}

func (s *StackStore) CreateAssignment(ctx context.Context, item stack.Assignment) (stack.Assignment, bool, error) {
	created, err := scanStackAssignment(s.database.QueryRowContext(ctx, `
		INSERT INTO stack_assignments (organization_id,id,license_id,assignee_kind,assignee_id,seats,usage_state,assigned_at,last_used_at,ended_at,source_system_id,source_record_id,revision,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''),NULLIF($12,''),$13,$14,$15) ON CONFLICT DO NOTHING
		RETURNING organization_id,id,license_id,assignee_kind,assignee_id,seats,usage_state,assigned_at,last_used_at,ended_at,COALESCE(source_system_id,''),COALESCE(source_record_id,''),revision,created_at,updated_at
	`, item.OrganizationID, item.ID, item.LicenseID, item.AssigneeKind, item.AssigneeID, item.Seats, item.UsageState, item.AssignedAt, item.LastUsedAt, item.EndedAt, item.SourceSystemID, item.SourceRecordID, item.Revision, item.CreatedAt, item.UpdatedAt))
	if err == nil {
		return created, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return stack.Assignment{}, false, translateStackWriteError("create Stack assignment", err)
	}
	existing, foundErr := findStackAssignment(ctx, s.database, item.OrganizationID, item.ID, item.SourceSystemID, item.SourceRecordID)
	if foundErr == nil && equalPostgresStackAssignment(existing, item) {
		return existing, false, nil
	}
	return stack.Assignment{}, false, resolveStackConflict(foundErr)
}

func (s *StackStore) UpdateAssignment(ctx context.Context, item stack.Assignment, expectedRevision int64) (stack.Assignment, error) {
	existing, err := s.GetAssignment(ctx, item.OrganizationID, item.ID)
	if err != nil {
		return stack.Assignment{}, err
	}
	if !validStackAssignmentUpdate(existing, item, expectedRevision) {
		return stack.Assignment{}, stack.ErrConflict
	}
	updated, err := scanStackAssignment(s.database.QueryRowContext(ctx, `
		UPDATE stack_assignments SET usage_state=$3,last_used_at=$4,ended_at=$5,revision=$6,updated_at=$7
		WHERE organization_id=$1 AND id=$2 AND revision=$8
		RETURNING organization_id,id,license_id,assignee_kind,assignee_id,seats,usage_state,assigned_at,last_used_at,ended_at,COALESCE(source_system_id,''),COALESCE(source_record_id,''),revision,created_at,updated_at
	`, item.OrganizationID, item.ID, item.UsageState, item.LastUsedAt, item.EndedAt, item.Revision, item.UpdatedAt, expectedRevision))
	if errors.Is(err, sql.ErrNoRows) {
		if _, getErr := s.GetAssignment(ctx, item.OrganizationID, item.ID); errors.Is(getErr, stack.ErrNotFound) {
			return stack.Assignment{}, stack.ErrNotFound
		}
		return stack.Assignment{}, stack.ErrConflict
	}
	return updated, translateStackWriteError("update Stack assignment", err)
}

func (s *StackStore) UpdateProduct(ctx context.Context, item stack.Product, expectedRevision int64) (stack.Product, error) {
	existing, err := s.GetProduct(ctx, item.OrganizationID, item.ID)
	if err != nil {
		return stack.Product{}, err
	}
	if !validStackProductUpdate(existing, item, expectedRevision) {
		return stack.Product{}, stack.ErrConflict
	}
	updated, err := scanStackProduct(s.database.QueryRowContext(ctx, `
		UPDATE stack_products SET status=$3,revision=$4,updated_at=$5 WHERE organization_id=$1 AND id=$2 AND revision=$6
		RETURNING organization_id,id,name,publisher,category,status,COALESCE(source_system_id,''),COALESCE(source_record_id,''),revision,created_at,updated_at
	`, item.OrganizationID, item.ID, item.Status, item.Revision, item.UpdatedAt, expectedRevision))
	if errors.Is(err, sql.ErrNoRows) {
		if _, getErr := s.GetProduct(ctx, item.OrganizationID, item.ID); errors.Is(getErr, stack.ErrNotFound) {
			return stack.Product{}, stack.ErrNotFound
		}
		return stack.Product{}, stack.ErrConflict
	}
	return updated, translateStackWriteError("update Stack product", err)
}

func (s *StackStore) UpdateVersion(ctx context.Context, item stack.Version, expectedRevision int64) (stack.Version, error) {
	existing, err := s.GetVersion(ctx, item.OrganizationID, item.ID)
	if err != nil {
		return stack.Version{}, err
	}
	if !validStackVersionUpdate(existing, item, expectedRevision) {
		return stack.Version{}, stack.ErrConflict
	}
	updated, err := scanStackVersion(s.database.QueryRowContext(ctx, `
		UPDATE stack_versions SET status=$3,revision=$4,updated_at=$5 WHERE organization_id=$1 AND id=$2 AND revision=$6
		RETURNING organization_id,id,product_id,name,released_on,status,COALESCE(source_system_id,''),COALESCE(source_record_id,''),revision,created_at,updated_at
	`, item.OrganizationID, item.ID, item.Status, item.Revision, item.UpdatedAt, expectedRevision))
	if errors.Is(err, sql.ErrNoRows) {
		if _, getErr := s.GetVersion(ctx, item.OrganizationID, item.ID); errors.Is(getErr, stack.ErrNotFound) {
			return stack.Version{}, stack.ErrNotFound
		}
		return stack.Version{}, stack.ErrConflict
	}
	return updated, translateStackWriteError("update Stack version", err)
}

func (s *StackStore) UpdateInstallation(ctx context.Context, item stack.Installation, expectedRevision int64) (stack.Installation, error) {
	existing, err := s.GetInstallation(ctx, item.OrganizationID, item.ID)
	if err != nil {
		return stack.Installation{}, err
	}
	if !validStackInstallationUpdate(existing, item, expectedRevision) {
		return stack.Installation{}, stack.ErrConflict
	}
	updated, err := scanStackInstallation(s.database.QueryRowContext(ctx, `
		UPDATE stack_installations SET status=$3,usage_state=$4,last_used_at=$5,removed_at=$6,revision=$7,updated_at=$8
		WHERE organization_id=$1 AND id=$2 AND revision=$9
		RETURNING organization_id,id,version_id,asset_id,status,usage_state,installed_at,last_used_at,removed_at,COALESCE(source_system_id,''),COALESCE(source_record_id,''),revision,created_at,updated_at
	`, item.OrganizationID, item.ID, item.Status, item.UsageState, item.LastUsedAt, item.RemovedAt, item.Revision, item.UpdatedAt, expectedRevision))
	if errors.Is(err, sql.ErrNoRows) {
		if _, getErr := s.GetInstallation(ctx, item.OrganizationID, item.ID); errors.Is(getErr, stack.ErrNotFound) {
			return stack.Installation{}, stack.ErrNotFound
		}
		return stack.Installation{}, stack.ErrConflict
	}
	return updated, translateStackWriteError("update Stack installation", err)
}

func (s *StackStore) UpdateLicense(ctx context.Context, item stack.License, expectedRevision int64) (stack.License, error) {
	existing, err := s.GetLicense(ctx, item.OrganizationID, item.ID)
	if err != nil {
		return stack.License{}, err
	}
	if !validStackLicenseUpdate(existing, item, expectedRevision) {
		return stack.License{}, stack.ErrConflict
	}
	updated, err := scanStackLicense(s.database.QueryRowContext(ctx, `
		UPDATE stack_licenses SET quantity=$3,status=$4,starts_on=$5,expires_on=$6,revision=$7,updated_at=$8
		WHERE organization_id=$1 AND id=$2 AND revision=$9
		RETURNING organization_id,id,product_id,COALESCE(version_id,''),name,entitlement_metric,quantity,status,starts_on,expires_on,COALESCE(vendor_id,''),COALESCE(purchase_order_id,''),COALESCE(contract_id,''),COALESCE(cost_record_id,''),document_ids,COALESCE(source_system_id,''),COALESCE(source_record_id,''),revision,created_at,updated_at
	`, item.OrganizationID, item.ID, item.Quantity, item.Status, item.StartsOn, item.ExpiresOn, item.Revision, item.UpdatedAt, expectedRevision))
	if errors.Is(err, sql.ErrNoRows) {
		if _, getErr := s.GetLicense(ctx, item.OrganizationID, item.ID); errors.Is(getErr, stack.ErrNotFound) {
			return stack.License{}, stack.ErrNotFound
		}
		return stack.License{}, stack.ErrConflict
	}
	return updated, translateStackWriteError("update Stack license", err)
}

func listStackProducts(ctx context.Context, q stackQueryer, organizationID string) ([]stack.Product, error) {
	rows, err := q.QueryContext(ctx, stackProductSelect+` WHERE organization_id=$1 ORDER BY id`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list Stack products: %w", err)
	}
	defer rows.Close()
	result := []stack.Product{}
	for rows.Next() {
		item, scanErr := scanStackProduct(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan Stack product: %w", scanErr)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
func listStackVersions(ctx context.Context, q stackQueryer, organizationID string) ([]stack.Version, error) {
	rows, err := q.QueryContext(ctx, stackVersionSelect+` WHERE organization_id=$1 ORDER BY id`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list Stack versions: %w", err)
	}
	defer rows.Close()
	result := []stack.Version{}
	for rows.Next() {
		item, scanErr := scanStackVersion(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan Stack version: %w", scanErr)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
func listStackInstallations(ctx context.Context, q stackQueryer, organizationID string) ([]stack.Installation, error) {
	rows, err := q.QueryContext(ctx, stackInstallationSelect+` WHERE organization_id=$1 ORDER BY id`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list Stack installations: %w", err)
	}
	defer rows.Close()
	result := []stack.Installation{}
	for rows.Next() {
		item, scanErr := scanStackInstallation(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan Stack installation: %w", scanErr)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
func listStackLicenses(ctx context.Context, q stackQueryer, organizationID string) ([]stack.License, error) {
	rows, err := q.QueryContext(ctx, stackLicenseSelect+` WHERE organization_id=$1 ORDER BY id`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list Stack licenses: %w", err)
	}
	defer rows.Close()
	result := []stack.License{}
	for rows.Next() {
		item, scanErr := scanStackLicense(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan Stack license: %w", scanErr)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
func listStackAssignments(ctx context.Context, q stackQueryer, organizationID string) ([]stack.Assignment, error) {
	rows, err := q.QueryContext(ctx, stackAssignmentSelect+` WHERE organization_id=$1 ORDER BY id`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list Stack assignments: %w", err)
	}
	defer rows.Close()
	result := []stack.Assignment{}
	for rows.Next() {
		item, scanErr := scanStackAssignment(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan Stack assignment: %w", scanErr)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

type stackScanner interface{ Scan(...any) error }

func scanStackProduct(row stackScanner) (stack.Product, error) {
	var v stack.Product
	err := row.Scan(&v.OrganizationID, &v.ID, &v.Name, &v.Publisher, &v.Category, &v.Status, &v.SourceSystemID, &v.SourceRecordID, &v.Revision, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}
func scanStackVersion(row stackScanner) (stack.Version, error) {
	var v stack.Version
	err := row.Scan(&v.OrganizationID, &v.ID, &v.ProductID, &v.Name, &v.ReleasedOn, &v.Status, &v.SourceSystemID, &v.SourceRecordID, &v.Revision, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}
func scanStackInstallation(row stackScanner) (stack.Installation, error) {
	var v stack.Installation
	err := row.Scan(&v.OrganizationID, &v.ID, &v.VersionID, &v.AssetID, &v.Status, &v.UsageState, &v.InstalledAt, &v.LastUsedAt, &v.RemovedAt, &v.SourceSystemID, &v.SourceRecordID, &v.Revision, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}
func scanStackLicense(row stackScanner) (stack.License, error) {
	var v stack.License
	var docs []byte
	err := row.Scan(&v.OrganizationID, &v.ID, &v.ProductID, &v.VersionID, &v.Name, &v.EntitlementMetric, &v.Quantity, &v.Status, &v.StartsOn, &v.ExpiresOn, &v.VendorID, &v.PurchaseOrderID, &v.ContractID, &v.CostRecordID, &docs, &v.SourceSystemID, &v.SourceRecordID, &v.Revision, &v.CreatedAt, &v.UpdatedAt)
	if err == nil {
		err = json.Unmarshal(docs, &v.DocumentIDs)
	}
	if v.DocumentIDs == nil {
		v.DocumentIDs = []string{}
	}
	return v, err
}
func scanStackAssignment(row stackScanner) (stack.Assignment, error) {
	var v stack.Assignment
	err := row.Scan(&v.OrganizationID, &v.ID, &v.LicenseID, &v.AssigneeKind, &v.AssigneeID, &v.Seats, &v.UsageState, &v.AssignedAt, &v.LastUsedAt, &v.EndedAt, &v.SourceSystemID, &v.SourceRecordID, &v.Revision, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}

func findStackProduct(ctx context.Context, q stackQueryer, org, id, system, record string) (stack.Product, error) {
	v, e := scanStackProduct(q.QueryRowContext(ctx, stackProductSelect+` WHERE organization_id=$1 AND id=$2`, org, id))
	if !errors.Is(e, sql.ErrNoRows) || system == "" {
		return v, e
	}
	return scanStackProduct(q.QueryRowContext(ctx, stackProductSelect+` WHERE organization_id=$1 AND lower(source_system_id)=lower($2) AND source_record_id=$3`, org, system, record))
}
func findStackVersion(ctx context.Context, q stackQueryer, org, id, system, record string) (stack.Version, error) {
	v, e := scanStackVersion(q.QueryRowContext(ctx, stackVersionSelect+` WHERE organization_id=$1 AND id=$2`, org, id))
	if !errors.Is(e, sql.ErrNoRows) || system == "" {
		return v, e
	}
	return scanStackVersion(q.QueryRowContext(ctx, stackVersionSelect+` WHERE organization_id=$1 AND lower(source_system_id)=lower($2) AND source_record_id=$3`, org, system, record))
}
func findStackInstallation(ctx context.Context, q stackQueryer, org, id, system, record string) (stack.Installation, error) {
	v, e := scanStackInstallation(q.QueryRowContext(ctx, stackInstallationSelect+` WHERE organization_id=$1 AND id=$2`, org, id))
	if !errors.Is(e, sql.ErrNoRows) || system == "" {
		return v, e
	}
	return scanStackInstallation(q.QueryRowContext(ctx, stackInstallationSelect+` WHERE organization_id=$1 AND lower(source_system_id)=lower($2) AND source_record_id=$3`, org, system, record))
}
func findStackLicense(ctx context.Context, q stackQueryer, org, id, system, record string) (stack.License, error) {
	v, e := scanStackLicense(q.QueryRowContext(ctx, stackLicenseSelect+` WHERE organization_id=$1 AND id=$2`, org, id))
	if !errors.Is(e, sql.ErrNoRows) || system == "" {
		return v, e
	}
	return scanStackLicense(q.QueryRowContext(ctx, stackLicenseSelect+` WHERE organization_id=$1 AND lower(source_system_id)=lower($2) AND source_record_id=$3`, org, system, record))
}
func findStackAssignment(ctx context.Context, q stackQueryer, org, id, system, record string) (stack.Assignment, error) {
	v, e := scanStackAssignment(q.QueryRowContext(ctx, stackAssignmentSelect+` WHERE organization_id=$1 AND id=$2`, org, id))
	if !errors.Is(e, sql.ErrNoRows) || system == "" {
		return v, e
	}
	return scanStackAssignment(q.QueryRowContext(ctx, stackAssignmentSelect+` WHERE organization_id=$1 AND lower(source_system_id)=lower($2) AND source_record_id=$3`, org, system, record))
}

func resolveStackConflict(err error) error {
	if err == nil || errors.Is(err, sql.ErrNoRows) {
		return stack.ErrConflict
	}
	return translateStackReadError("resolve Stack conflict", err)
}
func translateStackReadError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return stack.ErrNotFound
	}
	return fmt.Errorf("%s: %w", operation, err)
}
func translateStackWriteError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23503":
			return stack.ErrReferenceMissing
		case "23505":
			return stack.ErrConflict
		case "23514", "22001", "22P02":
			return stack.ErrInvalidInput
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func validStackProductUpdate(existing, updated stack.Product, expectedRevision int64) bool {
	return existing.Revision == expectedRevision && updated.Revision == expectedRevision+1 &&
		existing.ID == updated.ID && existing.OrganizationID == updated.OrganizationID && existing.Name == updated.Name &&
		existing.Publisher == updated.Publisher && existing.Category == updated.Category && existing.SourceSystemID == updated.SourceSystemID &&
		existing.SourceRecordID == updated.SourceRecordID && existing.CreatedAt.Equal(updated.CreatedAt)
}

func validStackVersionUpdate(existing, updated stack.Version, expectedRevision int64) bool {
	return existing.Revision == expectedRevision && updated.Revision == expectedRevision+1 &&
		existing.ID == updated.ID && existing.OrganizationID == updated.OrganizationID && existing.ProductID == updated.ProductID &&
		existing.Name == updated.Name && equalPostgresStackTime(existing.ReleasedOn, updated.ReleasedOn) &&
		existing.SourceSystemID == updated.SourceSystemID && existing.SourceRecordID == updated.SourceRecordID && existing.CreatedAt.Equal(updated.CreatedAt)
}

func validStackInstallationUpdate(existing, updated stack.Installation, expectedRevision int64) bool {
	return existing.Revision == expectedRevision && updated.Revision == expectedRevision+1 &&
		existing.ID == updated.ID && existing.OrganizationID == updated.OrganizationID && existing.VersionID == updated.VersionID &&
		existing.AssetID == updated.AssetID && existing.InstalledAt.Equal(updated.InstalledAt) && existing.SourceSystemID == updated.SourceSystemID &&
		existing.SourceRecordID == updated.SourceRecordID && existing.CreatedAt.Equal(updated.CreatedAt)
}

func validStackLicenseUpdate(existing, updated stack.License, expectedRevision int64) bool {
	return existing.Revision == expectedRevision && updated.Revision == expectedRevision+1 &&
		existing.ID == updated.ID && existing.OrganizationID == updated.OrganizationID && existing.ProductID == updated.ProductID &&
		existing.VersionID == updated.VersionID && existing.Name == updated.Name && existing.EntitlementMetric == updated.EntitlementMetric &&
		existing.VendorID == updated.VendorID && existing.PurchaseOrderID == updated.PurchaseOrderID && existing.ContractID == updated.ContractID &&
		existing.CostRecordID == updated.CostRecordID && reflect.DeepEqual(existing.DocumentIDs, updated.DocumentIDs) &&
		existing.SourceSystemID == updated.SourceSystemID && existing.SourceRecordID == updated.SourceRecordID && existing.CreatedAt.Equal(updated.CreatedAt)
}

func validStackAssignmentUpdate(existing, updated stack.Assignment, expectedRevision int64) bool {
	return existing.Revision == expectedRevision && updated.Revision == expectedRevision+1 &&
		existing.ID == updated.ID && existing.OrganizationID == updated.OrganizationID && existing.LicenseID == updated.LicenseID &&
		existing.AssigneeKind == updated.AssigneeKind && existing.AssigneeID == updated.AssigneeID && existing.Seats == updated.Seats &&
		existing.AssignedAt.Equal(updated.AssignedAt) && existing.SourceSystemID == updated.SourceSystemID &&
		existing.SourceRecordID == updated.SourceRecordID && existing.CreatedAt.Equal(updated.CreatedAt)
}

func equalPostgresStackProduct(a, b stack.Product) bool {
	return a.ID == b.ID && a.OrganizationID == b.OrganizationID && a.Name == b.Name && a.Publisher == b.Publisher && a.Category == b.Category && a.Status == b.Status && a.SourceSystemID == b.SourceSystemID && a.SourceRecordID == b.SourceRecordID
}
func equalPostgresStackVersion(a, b stack.Version) bool {
	return a.ID == b.ID && a.OrganizationID == b.OrganizationID && a.ProductID == b.ProductID && a.Name == b.Name && equalPostgresStackTime(a.ReleasedOn, b.ReleasedOn) && a.Status == b.Status && a.SourceSystemID == b.SourceSystemID && a.SourceRecordID == b.SourceRecordID
}
func equalPostgresStackInstallation(a, b stack.Installation) bool {
	return a.ID == b.ID && a.OrganizationID == b.OrganizationID && a.VersionID == b.VersionID && a.AssetID == b.AssetID && a.Status == b.Status && a.UsageState == b.UsageState && a.InstalledAt.Equal(b.InstalledAt) && equalPostgresStackTime(a.LastUsedAt, b.LastUsedAt) && equalPostgresStackTime(a.RemovedAt, b.RemovedAt) && a.SourceSystemID == b.SourceSystemID && a.SourceRecordID == b.SourceRecordID
}
func equalPostgresStackLicense(a, b stack.License) bool {
	return a.ID == b.ID && a.OrganizationID == b.OrganizationID && a.ProductID == b.ProductID && a.VersionID == b.VersionID && a.Name == b.Name && a.EntitlementMetric == b.EntitlementMetric && a.Quantity == b.Quantity && a.Status == b.Status && equalPostgresStackTime(a.StartsOn, b.StartsOn) && equalPostgresStackTime(a.ExpiresOn, b.ExpiresOn) && a.VendorID == b.VendorID && a.PurchaseOrderID == b.PurchaseOrderID && a.ContractID == b.ContractID && a.CostRecordID == b.CostRecordID && reflect.DeepEqual(a.DocumentIDs, b.DocumentIDs) && a.SourceSystemID == b.SourceSystemID && a.SourceRecordID == b.SourceRecordID
}
func equalPostgresStackAssignment(a, b stack.Assignment) bool {
	return a.ID == b.ID && a.OrganizationID == b.OrganizationID && a.LicenseID == b.LicenseID && a.AssigneeKind == b.AssigneeKind && a.AssigneeID == b.AssigneeID && a.Seats == b.Seats && a.UsageState == b.UsageState && a.AssignedAt.Equal(b.AssignedAt) && equalPostgresStackTime(a.LastUsedAt, b.LastUsedAt) && equalPostgresStackTime(a.EndedAt, b.EndedAt) && a.SourceSystemID == b.SourceSystemID && a.SourceRecordID == b.SourceRecordID
}
func equalPostgresStackTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}
