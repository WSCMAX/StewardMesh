package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxlemke/stewardmesh/internal/labels"
)

type LabelsStore struct {
	database *sql.DB
}

var _ labels.Store = (*LabelsStore)(nil)

func NewLabelsStore(database *sql.DB) (*LabelsStore, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &LabelsStore{database: database}, nil
}

func (s *LabelsStore) Snapshot(ctx context.Context, organizationID string) (labels.Snapshot, error) {
	result := labels.Snapshot{Definitions: []labels.Definition{}, Assignments: []labels.Assignment{}}
	definitions, err := s.ListDefinitions(ctx, organizationID)
	if err != nil {
		return labels.Snapshot{}, err
	}
	result.Definitions = definitions
	rows, err := s.database.QueryContext(ctx, `
		SELECT organization_id, definition_id, record_type, record_id, value_text, values, revision, updated_by, created_at, updated_at
		FROM labels_assignments WHERE organization_id = $1 ORDER BY record_type, record_id, definition_id
	`, organizationID)
	if err != nil {
		return labels.Snapshot{}, fmt.Errorf("snapshot label assignments: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		item, scanErr := scanLabelAssignment(rows)
		if scanErr != nil {
			return labels.Snapshot{}, scanErr
		}
		result.Assignments = append(result.Assignments, item)
	}
	return result, rows.Err()
}

func (s *LabelsStore) ListDefinitions(ctx context.Context, organizationID string) ([]labels.Definition, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT organization_id, id, name, description, value_kind, applicable_record_types, options, parent_id, goal_id, status, revision, created_at, updated_at
		FROM labels_definitions WHERE organization_id = $1 ORDER BY normalized_name, id
	`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list label definitions: %w", err)
	}
	defer rows.Close()
	items := make([]labels.Definition, 0)
	for rows.Next() {
		item, scanErr := scanLabelDefinition(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *LabelsStore) GetDefinition(ctx context.Context, organizationID, id string) (labels.Definition, error) {
	item, err := scanLabelDefinition(s.database.QueryRowContext(ctx, `
		SELECT organization_id, id, name, description, value_kind, applicable_record_types, options, parent_id, goal_id, status, revision, created_at, updated_at
		FROM labels_definitions WHERE organization_id = $1 AND id = $2
	`, organizationID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return labels.Definition{}, labels.ErrNotFound
	}
	return item, err
}

func (s *LabelsStore) CreateDefinition(ctx context.Context, definition labels.Definition) (labels.Definition, error) {
	targets, err := json.Marshal(definition.ApplicableRecordTypes)
	if err != nil {
		return labels.Definition{}, err
	}
	options, err := json.Marshal(definition.Options)
	if err != nil {
		return labels.Definition{}, err
	}
	created, err := scanLabelDefinition(s.database.QueryRowContext(ctx, `
		INSERT INTO labels_definitions (
			organization_id, id, name, normalized_name, description, value_kind, applicable_record_types, options, parent_id, goal_id, status, revision, created_at, updated_at
		) VALUES ($1,$2,$3,lower(btrim($3)),$4,$5,$6,$7,NULLIF($8,''),NULLIF($9,''),$10,$11,$12,$13)
		RETURNING organization_id, id, name, description, value_kind, applicable_record_types, options, parent_id, goal_id, status, revision, created_at, updated_at
	`, definition.OrganizationID, definition.ID, definition.Name, definition.Description, definition.ValueKind, targets, options, definition.ParentID, definition.GoalID, definition.Status, definition.Revision, definition.CreatedAt, definition.UpdatedAt))
	if isUniqueViolation(err) {
		return labels.Definition{}, labels.ErrConflict
	}
	return created, err
}

func (s *LabelsStore) UpdateDefinition(ctx context.Context, definition labels.Definition, expectedRevision int64) (labels.Definition, error) {
	targets, err := json.Marshal(definition.ApplicableRecordTypes)
	if err != nil {
		return labels.Definition{}, err
	}
	options, err := json.Marshal(definition.Options)
	if err != nil {
		return labels.Definition{}, err
	}
	updated, err := scanLabelDefinition(s.database.QueryRowContext(ctx, `
		UPDATE labels_definitions
		SET name = $4, normalized_name = lower(btrim($4)), description = $5, value_kind = $6,
		    applicable_record_types = $7, options = $8, parent_id = NULLIF($9, ''), goal_id = NULLIF($10, ''),
		    status = $11, revision = $12, updated_at = $13
		WHERE organization_id = $1 AND id = $2 AND revision = $3
		RETURNING organization_id, id, name, description, value_kind, applicable_record_types, options, parent_id, goal_id, status, revision, created_at, updated_at
	`, definition.OrganizationID, definition.ID, expectedRevision, definition.Name, definition.Description, definition.ValueKind, targets, options, definition.ParentID, definition.GoalID, definition.Status, definition.Revision, definition.UpdatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		return labels.Definition{}, labels.ErrConflict
	}
	return updated, err
}

func (s *LabelsStore) DeleteDefinitions(ctx context.Context, organizationID, rootID string, rootExpectedRevision int64, definitionIDs []string, orphanRemainingChildren bool) error {
	if len(definitionIDs) == 0 {
		return labels.ErrInvalidInput
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin label definition delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC()
	if orphanRemainingChildren {
		if _, execErr := tx.ExecContext(ctx, `
			UPDATE labels_definitions
			SET parent_id = NULL, revision = revision + 1, updated_at = $3
			WHERE organization_id = $1 AND parent_id = $2
		`, organizationID, rootID, now); execErr != nil {
			return fmt.Errorf("orphan label child definitions: %w", execErr)
		}
	}
	for _, definitionID := range definitionIDs {
		if _, execErr := tx.ExecContext(ctx, `
			DELETE FROM labels_assignments WHERE organization_id = $1 AND definition_id = $2
		`, organizationID, definitionID); execErr != nil {
			return fmt.Errorf("delete label assignments: %w", execErr)
		}
	}
	for _, definitionID := range definitionIDs {
		var result sql.Result
		var execErr error
		if definitionID == rootID {
			result, execErr = tx.ExecContext(ctx, `
				DELETE FROM labels_definitions WHERE organization_id = $1 AND id = $2 AND revision = $3
			`, organizationID, definitionID, rootExpectedRevision)
		} else {
			result, execErr = tx.ExecContext(ctx, `
				DELETE FROM labels_definitions WHERE organization_id = $1 AND id = $2
			`, organizationID, definitionID)
		}
		if execErr != nil {
			return fmt.Errorf("delete label definition: %w", execErr)
		}
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return rowsErr
		}
		if rows == 0 {
			return labels.ErrConflict
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit label definition delete: %w", err)
	}
	return nil
}

func (s *LabelsStore) ListAssignments(ctx context.Context, organizationID, recordType, recordID string) ([]labels.Assignment, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT organization_id, definition_id, record_type, record_id, value_text, values, revision, updated_by, created_at, updated_at
		FROM labels_assignments WHERE organization_id = $1 AND record_type = $2 AND record_id = $3 ORDER BY definition_id
	`, organizationID, recordType, recordID)
	if err != nil {
		return nil, fmt.Errorf("list label assignments: %w", err)
	}
	defer rows.Close()
	items := make([]labels.Assignment, 0)
	for rows.Next() {
		item, scanErr := scanLabelAssignment(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *LabelsStore) ListAssignmentsForDefinition(ctx context.Context, organizationID, definitionID string) ([]labels.Assignment, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT organization_id, definition_id, record_type, record_id, value_text, values, revision, updated_by, created_at, updated_at
		FROM labels_assignments WHERE organization_id = $1 AND definition_id = $2 ORDER BY record_type, record_id
	`, organizationID, definitionID)
	if err != nil {
		return nil, fmt.Errorf("list label assignments for definition: %w", err)
	}
	defer rows.Close()
	items := make([]labels.Assignment, 0)
	for rows.Next() {
		item, scanErr := scanLabelAssignment(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *LabelsStore) GetAssignment(ctx context.Context, organizationID, definitionID, recordType, recordID string) (labels.Assignment, error) {
	item, err := scanLabelAssignment(s.database.QueryRowContext(ctx, `
		SELECT organization_id, definition_id, record_type, record_id, value_text, values, revision, updated_by, created_at, updated_at
		FROM labels_assignments WHERE organization_id = $1 AND definition_id = $2 AND record_type = $3 AND record_id = $4
	`, organizationID, definitionID, recordType, recordID))
	if errors.Is(err, sql.ErrNoRows) {
		return labels.Assignment{}, labels.ErrNotFound
	}
	return item, err
}

func (s *LabelsStore) PutAssignment(ctx context.Context, assignment labels.Assignment, expectedRevision int64) (labels.Assignment, error) {
	values, err := json.Marshal(assignment.Values)
	if err != nil {
		return labels.Assignment{}, err
	}
	if expectedRevision == 0 {
		created, createErr := scanLabelAssignment(s.database.QueryRowContext(ctx, `
			INSERT INTO labels_assignments (
				organization_id, definition_id, record_type, record_id, value_text, values, revision, updated_by, created_at, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			ON CONFLICT (organization_id, definition_id, record_type, record_id) DO NOTHING
			RETURNING organization_id, definition_id, record_type, record_id, value_text, values, revision, updated_by, created_at, updated_at
		`, assignment.OrganizationID, assignment.DefinitionID, assignment.RecordType, assignment.RecordID, assignment.ValueText, values, assignment.Revision, assignment.UpdatedBy, assignment.CreatedAt, assignment.UpdatedAt))
		if createErr == nil {
			return created, nil
		}
		if !errors.Is(createErr, sql.ErrNoRows) {
			return labels.Assignment{}, createErr
		}
		expectedRevision = assignment.Revision - 1
	}
	updated, err := scanLabelAssignment(s.database.QueryRowContext(ctx, `
		UPDATE labels_assignments
		SET value_text = $5, values = $6, revision = $7, updated_by = $8, updated_at = $9
		WHERE organization_id = $1 AND definition_id = $2 AND record_type = $3 AND record_id = $4 AND revision = $10
		RETURNING organization_id, definition_id, record_type, record_id, value_text, values, revision, updated_by, created_at, updated_at
	`, assignment.OrganizationID, assignment.DefinitionID, assignment.RecordType, assignment.RecordID, assignment.ValueText, values, assignment.Revision, assignment.UpdatedBy, assignment.UpdatedAt, expectedRevision))
	if errors.Is(err, sql.ErrNoRows) {
		return labels.Assignment{}, labels.ErrConflict
	}
	return updated, err
}

func (s *LabelsStore) DeleteAssignment(ctx context.Context, organizationID, definitionID, recordType, recordID string, expectedRevision int64) error {
	result, err := s.database.ExecContext(ctx, `
		DELETE FROM labels_assignments
		WHERE organization_id = $1 AND definition_id = $2 AND record_type = $3 AND record_id = $4 AND revision = $5
	`, organizationID, definitionID, recordType, recordID, expectedRevision)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return labels.ErrConflict
	}
	return nil
}

type labelDefinitionScanner interface {
	Scan(dest ...any) error
}

func scanLabelDefinition(row labelDefinitionScanner) (labels.Definition, error) {
	var item labels.Definition
	var targets, options []byte
	var parentID, goalID sql.NullString
	if err := row.Scan(&item.OrganizationID, &item.ID, &item.Name, &item.Description, &item.ValueKind, &targets, &options, &parentID, &goalID, &item.Status, &item.Revision, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return labels.Definition{}, err
	}
	item.ParentID = parentID.String
	item.GoalID = goalID.String
	if err := json.Unmarshal(targets, &item.ApplicableRecordTypes); err != nil {
		return labels.Definition{}, fmt.Errorf("decode applicable record types: %w", err)
	}
	if err := json.Unmarshal(options, &item.Options); err != nil {
		return labels.Definition{}, fmt.Errorf("decode label options: %w", err)
	}
	return item, nil
}

func scanLabelAssignment(row labelDefinitionScanner) (labels.Assignment, error) {
	var item labels.Assignment
	var values []byte
	if err := row.Scan(&item.OrganizationID, &item.DefinitionID, &item.RecordType, &item.RecordID, &item.ValueText, &values, &item.Revision, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return labels.Assignment{}, err
	}
	if err := json.Unmarshal(values, &item.Values); err != nil {
		return labels.Assignment{}, fmt.Errorf("decode label values: %w", err)
	}
	return item, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
