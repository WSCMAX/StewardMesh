package postgres

// PostgreSQL Exchange adapter. Requirement: REQ-EXCHANGE-001. Feature: migration.packages.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxlemke/stewardmesh/internal/exchange"
)

type ExchangeStore struct{ database *sql.DB }

func NewExchangeStore(database *sql.DB) (*ExchangeStore, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &ExchangeStore{database: database}, nil
}

const exchangePackageSelect = `SELECT organization_id, package_id, direction, schema_version, source_system_id,
	archive_sha256, size_bytes, file_mode, status, record_count, file_count, created_count, unchanged_count,
	holding_count, records, COALESCE(error_code, ''), created_by, created_at, updated_at FROM exchange_packages`

func (s *ExchangeStore) ListPackages(ctx context.Context, organizationID string, limit int) ([]exchange.Package, error) {
	if organizationID == "" || limit < 1 || limit > exchange.MaximumHistory {
		return nil, exchange.ErrInvalidInput
	}
	rows, err := s.database.QueryContext(ctx, exchangePackageSelect+`
		WHERE organization_id = $1 ORDER BY created_at DESC, direction, package_id LIMIT $2`, organizationID, limit)
	if err != nil {
		return nil, fmt.Errorf("list Exchange packages: %w", err)
	}
	defer rows.Close()
	result := make([]exchange.Package, 0)
	for rows.Next() {
		value, err := scanExchangePackage(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Exchange package: %w", err)
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Exchange packages: %w", err)
	}
	return result, nil
}

func (s *ExchangeStore) GetPackage(ctx context.Context, organizationID string, direction exchange.PackageDirection, packageID string) (exchange.Package, error) {
	value, err := scanExchangePackage(s.database.QueryRowContext(ctx, exchangePackageSelect+`
		WHERE organization_id = $1 AND direction = $2 AND package_id = $3`, organizationID, direction, packageID))
	if errors.Is(err, sql.ErrNoRows) {
		return exchange.Package{}, exchange.ErrNotFound
	}
	if err != nil {
		return exchange.Package{}, fmt.Errorf("get Exchange package: %w", err)
	}
	return value, nil
}

func (s *ExchangeStore) CreatePackage(ctx context.Context, value exchange.Package) (exchange.Package, bool, error) {
	if err := value.Validate(); err != nil {
		return exchange.Package{}, false, err
	}
	records, err := json.Marshal(value.Records)
	if err != nil {
		return exchange.Package{}, false, exchange.ErrInvalidInput
	}
	created, err := scanExchangePackage(s.database.QueryRowContext(ctx, `
		INSERT INTO exchange_packages (
			organization_id, direction, package_id, schema_version, source_system_id, archive_sha256,
			size_bytes, file_mode, status, record_count, file_count, created_count, unchanged_count,
			holding_count, records, error_code, created_by, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15::jsonb,NULLIF($16,''),$17,$18,$19)
		ON CONFLICT DO NOTHING
		RETURNING organization_id, package_id, direction, schema_version, source_system_id, archive_sha256,
			size_bytes, file_mode, status, record_count, file_count, created_count, unchanged_count,
			holding_count, records, COALESCE(error_code, ''), created_by, created_at, updated_at
	`, value.OrganizationID, value.Direction, value.PackageID, value.SchemaVersion, value.SourceSystemID, value.ArchiveSHA256,
		value.SizeBytes, value.FileMode, value.Status, value.RecordCount, value.FileCount, value.CreatedCount, value.UnchangedCount,
		value.HoldingCount, string(records), value.ErrorCode, value.CreatedBy, value.CreatedAt, value.UpdatedAt))
	if err == nil {
		return created, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return exchange.Package{}, false, translateExchangeWriteError("create Exchange package", err)
	}
	existing, err := s.GetPackage(ctx, value.OrganizationID, value.Direction, value.PackageID)
	if err != nil {
		return exchange.Package{}, false, err
	}
	if !samePostgresExchangeArchiveIdentity(existing, value) {
		return exchange.Package{}, false, exchange.ErrConflict
	}
	return existing, false, nil
}

func samePostgresExchangeArchiveIdentity(left, right exchange.Package) bool {
	return left.ArchiveSHA256 == right.ArchiveSHA256 && left.SourceSystemID == right.SourceSystemID &&
		left.SchemaVersion == right.SchemaVersion && left.SizeBytes == right.SizeBytes && left.FileMode == right.FileMode &&
		left.RecordCount == right.RecordCount && left.FileCount == right.FileCount
}

func (s *ExchangeStore) UpdatePackage(ctx context.Context, value exchange.Package, expectedUpdatedAt time.Time) (exchange.Package, error) {
	if err := value.Validate(); err != nil || expectedUpdatedAt.IsZero() {
		if err != nil {
			return exchange.Package{}, err
		}
		return exchange.Package{}, exchange.ErrInvalidInput
	}
	existing, err := s.GetPackage(ctx, value.OrganizationID, value.Direction, value.PackageID)
	if err != nil {
		return exchange.Package{}, err
	}
	if !existing.UpdatedAt.Equal(expectedUpdatedAt) || !samePostgresExchangeIdentity(existing, value) || !validPostgresExchangeTransition(existing.Status, value.Status) {
		return exchange.Package{}, exchange.ErrConflict
	}
	records, err := json.Marshal(value.Records)
	if err != nil {
		return exchange.Package{}, exchange.ErrInvalidInput
	}
	updated, err := scanExchangePackage(s.database.QueryRowContext(ctx, `
		UPDATE exchange_packages SET status=$1, created_count=$2, unchanged_count=$3, holding_count=$4,
			records=$5::jsonb, error_code=NULLIF($6,''), updated_at=$7
		WHERE organization_id=$8 AND direction=$9 AND package_id=$10 AND updated_at=$11
		RETURNING organization_id, package_id, direction, schema_version, source_system_id, archive_sha256,
			size_bytes, file_mode, status, record_count, file_count, created_count, unchanged_count,
			holding_count, records, COALESCE(error_code, ''), created_by, created_at, updated_at
	`, value.Status, value.CreatedCount, value.UnchangedCount, value.HoldingCount, string(records), value.ErrorCode, value.UpdatedAt,
		value.OrganizationID, value.Direction, value.PackageID, expectedUpdatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		return exchange.Package{}, exchange.ErrConflict
	}
	if err != nil {
		return exchange.Package{}, translateExchangeWriteError("update Exchange package", err)
	}
	return updated, nil
}

type exchangeRowScanner interface{ Scan(...any) error }

func scanExchangePackage(row exchangeRowScanner) (exchange.Package, error) {
	var value exchange.Package
	var records []byte
	if err := row.Scan(
		&value.OrganizationID, &value.PackageID, &value.Direction, &value.SchemaVersion, &value.SourceSystemID,
		&value.ArchiveSHA256, &value.SizeBytes, &value.FileMode, &value.Status, &value.RecordCount, &value.FileCount,
		&value.CreatedCount, &value.UnchangedCount, &value.HoldingCount, &records, &value.ErrorCode,
		&value.CreatedBy, &value.CreatedAt, &value.UpdatedAt,
	); err != nil {
		return exchange.Package{}, err
	}
	if err := json.Unmarshal(records, &value.Records); err != nil {
		return exchange.Package{}, fmt.Errorf("decode Exchange outcomes: %w", err)
	}
	if value.Records == nil {
		value.Records = []exchange.RecordOutcome{}
	}
	for index := range value.Records {
		if value.Records[index].MissingDependencies == nil {
			value.Records[index].MissingDependencies = []exchange.Reference{}
		}
	}
	if err := value.Validate(); err != nil {
		return exchange.Package{}, fmt.Errorf("validate persisted Exchange package: %w", err)
	}
	return value, nil
}

func samePostgresExchangeIdentity(left, right exchange.Package) bool {
	return left.OrganizationID == right.OrganizationID && left.PackageID == right.PackageID && left.Direction == right.Direction &&
		left.SchemaVersion == right.SchemaVersion && left.SourceSystemID == right.SourceSystemID && left.ArchiveSHA256 == right.ArchiveSHA256 &&
		left.SizeBytes == right.SizeBytes && left.FileMode == right.FileMode && left.RecordCount == right.RecordCount &&
		left.FileCount == right.FileCount && left.CreatedBy == right.CreatedBy && left.CreatedAt.Equal(right.CreatedAt)
}

func validPostgresExchangeTransition(from, to exchange.PackageStatus) bool {
	if from == exchange.StatusProcessing {
		return to == exchange.StatusCompleted || to == exchange.StatusHolding || to == exchange.StatusFailed
	}
	return (from == exchange.StatusFailed || from == exchange.StatusHolding) && to == exchange.StatusProcessing
}

func translateExchangeWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505":
			return exchange.ErrConflict
		case "23503", "23514", "22P02":
			return exchange.ErrInvalidInput
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

var _ exchange.Store = (*ExchangeStore)(nil)
