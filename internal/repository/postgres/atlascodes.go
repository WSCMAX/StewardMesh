package postgres

// PostgreSQL Atlas Codes adapter.
// Requirement: REQ-ATLAS-CODES-001. Feature: inventory.identifiers.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxlemke/stewardmesh/internal/atlascodes"
)

var postgresAtlasCodesStableIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type AtlasCodesStore struct{ database *sql.DB }

var _ atlascodes.Store = (*AtlasCodesStore)(nil)

func NewAtlasCodesStore(database *sql.DB) (*AtlasCodesStore, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &AtlasCodesStore{database: database}, nil
}

const atlasCodesIdentifierColumns = `organization_id, id, asset_id, symbology, normalized_value,
	display_value, source, is_primary, status, COALESCE(supersedes_id, ''),
	COALESCE(replaced_by_id, ''), revision, created_by, created_correlation_id,
	updated_by, updated_correlation_id, created_at, updated_at, deactivated_at`

func (s *AtlasCodesStore) ListIdentifiers(ctx context.Context, organizationID, assetID string) ([]atlascodes.Identifier, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT `+atlasCodesIdentifierColumns+`
		FROM atlas_asset_identifiers
		WHERE organization_id = $1 AND asset_id = $2
		ORDER BY created_at, id`, organizationID, assetID)
	if err != nil {
		return nil, fmt.Errorf("list Atlas Codes identifiers: %w", err)
	}
	defer rows.Close()
	items := make([]atlascodes.Identifier, 0)
	for rows.Next() {
		item, err := scanAtlasCodesIdentifier(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Atlas Codes identifier: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Atlas Codes identifiers: %w", err)
	}
	return items, nil
}

func (s *AtlasCodesStore) GetIdentifier(ctx context.Context, organizationID, assetID, identifierID string) (atlascodes.Identifier, error) {
	item, err := scanAtlasCodesIdentifier(s.database.QueryRowContext(ctx, `SELECT `+atlasCodesIdentifierColumns+`
		FROM atlas_asset_identifiers
		WHERE organization_id = $1 AND asset_id = $2 AND id = $3`, organizationID, assetID, identifierID))
	return item, translateAtlasCodesReadError("get Atlas Codes identifier", err)
}

func (s *AtlasCodesStore) ResolveIdentifier(ctx context.Context, organizationID string, symbology atlascodes.Symbology, normalizedValue string) (atlascodes.Identifier, error) {
	item, err := scanAtlasCodesIdentifier(s.database.QueryRowContext(ctx, `SELECT `+atlasCodesIdentifierColumns+`
		FROM atlas_asset_identifiers
		WHERE organization_id = $1 AND symbology = $2 AND normalized_value = $3 AND status = 'active'`,
		organizationID, symbology, normalizedValue))
	return item, translateAtlasCodesReadError("resolve Atlas Codes identifier", err)
}

func (s *AtlasCodesStore) CreateIdentifier(ctx context.Context, item atlascodes.Identifier) (atlascodes.Identifier, bool, error) {
	if !validNewPostgresAtlasCodesIdentifier(item, false) {
		return atlascodes.Identifier{}, false, atlascodes.ErrInvalidInput
	}
	created, err := scanAtlasCodesIdentifier(s.database.QueryRowContext(ctx, `
		INSERT INTO atlas_asset_identifiers (
			organization_id, id, asset_id, symbology, normalized_value, display_value, source,
			is_primary, status, supersedes_id, replaced_by_id, revision, created_by,
			created_correlation_id, updated_by, updated_correlation_id, created_at, updated_at, deactivated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, ''), NULLIF($11, ''), $12, $13, $14, $15, $16, $17, $18, $19)
		ON CONFLICT (organization_id, id) DO NOTHING
		RETURNING `+atlasCodesIdentifierColumns,
		item.OrganizationID, item.ID, item.AssetID, item.Symbology, item.NormalizedValue, item.DisplayValue,
		item.Source, item.Primary, item.Status, item.SupersedesID, item.ReplacedByID, item.Revision,
		item.CreatedBy, item.CreatedCorrelationID, item.UpdatedBy, item.UpdatedCorrelationID,
		item.CreatedAt, item.UpdatedAt, item.DeactivatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		existing, loadErr := s.getIdentifierByID(ctx, item.OrganizationID, item.ID)
		if loadErr != nil {
			return atlascodes.Identifier{}, false, translateAtlasCodesReadError("read existing Atlas Codes identifier", loadErr)
		}
		if samePostgresAtlasCodesIntent(existing, item) {
			return existing, false, nil
		}
		return atlascodes.Identifier{}, false, atlascodes.ErrConflict
	}
	if err != nil {
		return atlascodes.Identifier{}, false, translateAtlasCodesWriteError("create Atlas Codes identifier", err)
	}
	return created, true, nil
}

func (s *AtlasCodesStore) ReplaceIdentifier(
	ctx context.Context,
	organizationID, assetID, identifierID string,
	expectedRevision int64,
	replacement atlascodes.Identifier,
	changedAt time.Time,
) (atlascodes.Identifier, bool, error) {
	if strings.TrimSpace(organizationID) == "" || strings.TrimSpace(assetID) == "" || strings.TrimSpace(identifierID) == "" ||
		expectedRevision < 1 || changedAt.IsZero() || replacement.OrganizationID != organizationID ||
		replacement.AssetID != assetID || replacement.SupersedesID != identifierID || !validNewPostgresAtlasCodesIdentifier(replacement, true) {
		return atlascodes.Identifier{}, false, atlascodes.ErrInvalidInput
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return atlascodes.Identifier{}, false, fmt.Errorf("begin Atlas Codes replacement: %w", err)
	}
	defer transaction.Rollback()
	previous, err := scanAtlasCodesIdentifier(transaction.QueryRowContext(ctx, `SELECT `+atlasCodesIdentifierColumns+`
		FROM atlas_asset_identifiers
		WHERE organization_id = $1 AND asset_id = $2 AND id = $3
		FOR UPDATE`, organizationID, assetID, identifierID))
	if err != nil {
		return atlascodes.Identifier{}, false, translateAtlasCodesReadError("get Atlas Codes identifier for replacement", err)
	}
	if previous.Status == atlascodes.StatusReplaced {
		if previous.Revision <= 1 || expectedRevision != previous.Revision-1 || previous.ReplacedByID != replacement.ID {
			return atlascodes.Identifier{}, false, atlascodes.ErrConflict
		}
		existing, err := scanAtlasCodesIdentifier(transaction.QueryRowContext(ctx, `SELECT `+atlasCodesIdentifierColumns+`
			FROM atlas_asset_identifiers WHERE organization_id = $1 AND asset_id = $2 AND id = $3`,
			organizationID, assetID, replacement.ID))
		if errors.Is(err, sql.ErrNoRows) {
			return atlascodes.Identifier{}, false, atlascodes.ErrConflict
		}
		if err != nil {
			return atlascodes.Identifier{}, false, translateAtlasCodesReadError("read existing Atlas Codes replacement", err)
		}
		if !samePostgresAtlasCodesIntent(existing, replacement) {
			return atlascodes.Identifier{}, false, atlascodes.ErrConflict
		}
		return existing, false, nil
	}
	if previous.Status != atlascodes.StatusActive || previous.Revision != expectedRevision {
		return atlascodes.Identifier{}, false, atlascodes.ErrConflict
	}
	if changedAt.Before(previous.UpdatedAt) {
		return atlascodes.Identifier{}, false, atlascodes.ErrInvalidInput
	}
	result, err := transaction.ExecContext(ctx, `
		UPDATE atlas_asset_identifiers
		SET status = 'replaced', replaced_by_id = $4, revision = revision + 1,
			updated_at = $5, deactivated_at = $5, updated_by = $7, updated_correlation_id = $8
		WHERE organization_id = $1 AND asset_id = $2 AND id = $3 AND revision = $6 AND status = 'active'`,
		organizationID, assetID, identifierID, replacement.ID, changedAt, expectedRevision,
		replacement.UpdatedBy, replacement.UpdatedCorrelationID)
	if err != nil {
		return atlascodes.Identifier{}, false, translateAtlasCodesWriteError("replace prior Atlas Codes identifier", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return atlascodes.Identifier{}, false, fmt.Errorf("inspect Atlas Codes replacement: %w", err)
	}
	if rowsAffected != 1 {
		return atlascodes.Identifier{}, false, atlascodes.ErrConflict
	}
	created, err := insertAtlasCodesIdentifier(ctx, transaction, replacement)
	if err != nil {
		return atlascodes.Identifier{}, false, err
	}
	if err := transaction.Commit(); err != nil {
		return atlascodes.Identifier{}, false, translateAtlasCodesWriteError("commit Atlas Codes replacement", err)
	}
	return created, true, nil
}

func (s *AtlasCodesStore) DeactivateIdentifier(
	ctx context.Context,
	organizationID, assetID, identifierID string,
	expectedRevision int64,
	deactivatedAt time.Time,
	actorID, correlationID string,
) (atlascodes.Identifier, bool, error) {
	if strings.TrimSpace(organizationID) == "" || strings.TrimSpace(assetID) == "" || strings.TrimSpace(identifierID) == "" ||
		expectedRevision < 1 || deactivatedAt.IsZero() || !validPostgresAtlasCodesProvenance(actorID, correlationID) {
		return atlascodes.Identifier{}, false, atlascodes.ErrInvalidInput
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return atlascodes.Identifier{}, false, fmt.Errorf("begin Atlas Codes deactivation: %w", err)
	}
	defer transaction.Rollback()
	item, err := scanAtlasCodesIdentifier(transaction.QueryRowContext(ctx, `SELECT `+atlasCodesIdentifierColumns+`
		FROM atlas_asset_identifiers
		WHERE organization_id = $1 AND asset_id = $2 AND id = $3
		FOR UPDATE`, organizationID, assetID, identifierID))
	if err != nil {
		return atlascodes.Identifier{}, false, translateAtlasCodesReadError("get Atlas Codes identifier for deactivation", err)
	}
	if item.Status == atlascodes.StatusDeactivated {
		if item.Revision <= 1 || expectedRevision != item.Revision-1 {
			return atlascodes.Identifier{}, false, atlascodes.ErrConflict
		}
		return item, false, nil
	}
	if item.Status != atlascodes.StatusActive || item.Revision != expectedRevision {
		return atlascodes.Identifier{}, false, atlascodes.ErrConflict
	}
	if deactivatedAt.Before(item.UpdatedAt) {
		return atlascodes.Identifier{}, false, atlascodes.ErrInvalidInput
	}
	deactivated, err := scanAtlasCodesIdentifier(transaction.QueryRowContext(ctx, `
		UPDATE atlas_asset_identifiers
		SET status = 'deactivated', revision = revision + 1, updated_at = $4, deactivated_at = $4,
			updated_by = $6, updated_correlation_id = $7
		WHERE organization_id = $1 AND asset_id = $2 AND id = $3 AND revision = $5 AND status = 'active'
		RETURNING `+atlasCodesIdentifierColumns,
		organizationID, assetID, identifierID, deactivatedAt, expectedRevision,
		strings.TrimSpace(actorID), strings.TrimSpace(correlationID)))
	if errors.Is(err, sql.ErrNoRows) {
		return atlascodes.Identifier{}, false, atlascodes.ErrConflict
	}
	if err != nil {
		return atlascodes.Identifier{}, false, translateAtlasCodesWriteError("deactivate Atlas Codes identifier", err)
	}
	if err := transaction.Commit(); err != nil {
		return atlascodes.Identifier{}, false, translateAtlasCodesWriteError("commit Atlas Codes deactivation", err)
	}
	return deactivated, true, nil
}

func (s *AtlasCodesStore) getIdentifierByID(ctx context.Context, organizationID, identifierID string) (atlascodes.Identifier, error) {
	return scanAtlasCodesIdentifier(s.database.QueryRowContext(ctx, `SELECT `+atlasCodesIdentifierColumns+`
		FROM atlas_asset_identifiers WHERE organization_id = $1 AND id = $2`, organizationID, identifierID))
}

func insertAtlasCodesIdentifier(ctx context.Context, transaction *sql.Tx, item atlascodes.Identifier) (atlascodes.Identifier, error) {
	created, err := scanAtlasCodesIdentifier(transaction.QueryRowContext(ctx, `
		INSERT INTO atlas_asset_identifiers (
			organization_id, id, asset_id, symbology, normalized_value, display_value, source,
			is_primary, status, supersedes_id, replaced_by_id, revision, created_by,
			created_correlation_id, updated_by, updated_correlation_id, created_at, updated_at, deactivated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, ''), NULLIF($11, ''), $12, $13, $14, $15, $16, $17, $18, $19)
		RETURNING `+atlasCodesIdentifierColumns,
		item.OrganizationID, item.ID, item.AssetID, item.Symbology, item.NormalizedValue, item.DisplayValue,
		item.Source, item.Primary, item.Status, item.SupersedesID, item.ReplacedByID, item.Revision,
		item.CreatedBy, item.CreatedCorrelationID, item.UpdatedBy, item.UpdatedCorrelationID,
		item.CreatedAt, item.UpdatedAt, item.DeactivatedAt))
	if err != nil {
		return atlascodes.Identifier{}, translateAtlasCodesWriteError("create replacement Atlas Codes identifier", err)
	}
	return created, nil
}

type atlasCodesScanner interface{ Scan(...any) error }

func scanAtlasCodesIdentifier(row atlasCodesScanner) (atlascodes.Identifier, error) {
	var item atlascodes.Identifier
	var deactivatedAt sql.NullTime
	err := row.Scan(
		&item.OrganizationID, &item.ID, &item.AssetID, &item.Symbology, &item.NormalizedValue,
		&item.DisplayValue, &item.Source, &item.Primary, &item.Status, &item.SupersedesID,
		&item.ReplacedByID, &item.Revision, &item.CreatedBy, &item.CreatedCorrelationID,
		&item.UpdatedBy, &item.UpdatedCorrelationID, &item.CreatedAt, &item.UpdatedAt, &deactivatedAt,
	)
	if deactivatedAt.Valid {
		value := deactivatedAt.Time
		item.DeactivatedAt = &value
	}
	return item, err
}

func translateAtlasCodesReadError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return atlascodes.ErrNotFound
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func translateAtlasCodesWriteError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505":
			return atlascodes.ErrConflict
		case "23503":
			return atlascodes.ErrReferenceMissing
		case "22001", "22P02", "23502", "23514":
			return atlascodes.ErrInvalidInput
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func validNewPostgresAtlasCodesIdentifier(item atlascodes.Identifier, replacement bool) bool {
	if strings.TrimSpace(item.OrganizationID) == "" || !postgresAtlasCodesStableIDPattern.MatchString(item.ID) ||
		!postgresAtlasCodesStableIDPattern.MatchString(item.AssetID) || !validPostgresAtlasCodesEncodedValue(item.Symbology, item.NormalizedValue) ||
		!validPostgresAtlasCodesDisplayValue(item.DisplayValue) || strings.TrimSpace(item.CreatedBy) == "" ||
		utf8.RuneCountInString(item.CreatedBy) > 128 ||
		!validPostgresAtlasCodesProvenance(item.UpdatedBy, item.UpdatedCorrelationID) ||
		!validPostgresAtlasCodesCorrelationID(item.CreatedCorrelationID) ||
		item.CreatedBy != item.UpdatedBy || item.CreatedCorrelationID != item.UpdatedCorrelationID ||
		item.Revision != 1 || item.Status != atlascodes.StatusActive ||
		item.CreatedAt.IsZero() || item.UpdatedAt.IsZero() ||
		item.UpdatedAt.Before(item.CreatedAt) || item.DeactivatedAt != nil || item.ReplacedByID != "" {
		return false
	}
	if item.Symbology != atlascodes.SymbologyCode128 && item.Symbology != atlascodes.SymbologyQR {
		return false
	}
	if item.Source != atlascodes.SourceImported && item.Source != atlascodes.SourceUserEntered && item.Source != atlascodes.SourceGenerated {
		return false
	}
	if replacement {
		return strings.TrimSpace(item.SupersedesID) != "" && item.SupersedesID != item.ID
	}
	return item.SupersedesID == ""
}

func validPostgresAtlasCodesProvenance(actorID, correlationID string) bool {
	return actorID != "" && actorID == strings.TrimSpace(actorID) &&
		utf8.RuneCountInString(actorID) <= 128 && validPostgresAtlasCodesCorrelationID(correlationID)
}

func validPostgresAtlasCodesCorrelationID(value string) bool {
	return value == strings.TrimSpace(value) && postgresAtlasCodesStableIDPattern.MatchString(value)
}

func validPostgresAtlasCodesEncodedValue(symbology atlascodes.Symbology, value string) bool {
	if value == "" || strings.TrimSpace(value) != value || !printablePostgresAtlasCodesText(value) {
		return false
	}
	switch symbology {
	case atlascodes.SymbologyCode128:
		if len(value) > 128 {
			return false
		}
		for index := range len(value) {
			if value[index] < 0x20 || value[index] > 0x7e {
				return false
			}
		}
		return true
	case atlascodes.SymbologyQR:
		return len(value) <= 512
	default:
		return false
	}
}

func validPostgresAtlasCodesDisplayValue(value string) bool {
	return value != "" && len(value) <= 512 && strings.TrimSpace(value) == value && printablePostgresAtlasCodesText(value)
}

func printablePostgresAtlasCodesText(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || !unicode.IsPrint(character) {
			return false
		}
	}
	return true
}

func samePostgresAtlasCodesIntent(left, right atlascodes.Identifier) bool {
	return left.ID == right.ID && left.OrganizationID == right.OrganizationID && left.AssetID == right.AssetID &&
		left.Symbology == right.Symbology && left.NormalizedValue == right.NormalizedValue &&
		left.DisplayValue == right.DisplayValue && left.Source == right.Source && left.Primary == right.Primary &&
		left.Status == right.Status && left.SupersedesID == right.SupersedesID
}
