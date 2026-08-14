package postgres

// PostgreSQL Patterns adapter. Requirement: REQ-PATTERNS-001. Feature: templates.schemas.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxlemke/stewardmesh/internal/patterns"
)

type PatternsStore struct{ database *sql.DB }

func NewPatternsStore(database *sql.DB) (*PatternsStore, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &PatternsStore{database: database}, nil
}

const patternsColumns = `organization_id, id, record_type, name, description, version, status, fields, created_by, created_at`

func (s *PatternsStore) ListTemplates(ctx context.Context, organizationID string, query patterns.ListQuery) ([]patterns.Template, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT `+patternsColumns+` FROM pattern_template_versions candidate
		WHERE organization_id = $1 AND ($2 = '' OR record_type = $2)
		  AND ($3 OR status <> 'retired')
		  AND ($4 OR version = (SELECT MAX(latest.version) FROM pattern_template_versions latest
		    WHERE latest.organization_id = candidate.organization_id AND latest.id = candidate.id)
		  )
		ORDER BY record_type, lower(name), id, version DESC`, organizationID, query.RecordType, query.IncludeRetired, query.IncludeVersions)
	if err != nil {
		return nil, fmt.Errorf("list Patterns templates: %w", err)
	}
	defer rows.Close()
	items := make([]patterns.Template, 0)
	for rows.Next() {
		item, err := scanPatternsTemplate(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Patterns template: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Patterns templates: %w", err)
	}
	return items, nil
}

func (s *PatternsStore) GetTemplate(ctx context.Context, organizationID, id string, version int64) (patterns.Template, error) {
	if version == 0 {
		return scanPatternsRead("get latest Patterns template", s.database.QueryRowContext(ctx,
			`SELECT `+patternsColumns+` FROM pattern_template_versions WHERE organization_id = $1 AND id = $2 ORDER BY version DESC LIMIT 1`, organizationID, id))
	}
	return scanPatternsRead("get Patterns template", s.database.QueryRowContext(ctx,
		`SELECT `+patternsColumns+` FROM pattern_template_versions WHERE organization_id = $1 AND id = $2 AND version = $3`, organizationID, id, version))
}

func (s *PatternsStore) CreateTemplate(ctx context.Context, template patterns.Template) (patterns.Template, error) {
	return s.insertTemplate(ctx, template)
}

func (s *PatternsStore) CreateVersion(ctx context.Context, template patterns.Template) (patterns.Template, error) {
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return patterns.Template{}, fmt.Errorf("begin Patterns template version: %w", err)
	}
	defer transaction.Rollback()
	var currentVersion int64
	var recordType, name string
	if err := transaction.QueryRowContext(ctx, `SELECT version, record_type, name FROM pattern_template_versions
		WHERE organization_id = $1 AND id = $2 ORDER BY version DESC LIMIT 1 FOR UPDATE`, template.OrganizationID, template.ID).
		Scan(&currentVersion, &recordType, &name); errors.Is(err, sql.ErrNoRows) {
		return patterns.Template{}, patterns.ErrNotFound
	} else if err != nil {
		return patterns.Template{}, fmt.Errorf("lock Patterns template: %w", err)
	}
	if currentVersion+1 != template.Version || recordType != template.RecordType || name != template.Name {
		return patterns.Template{}, patterns.ErrConflict
	}
	created, err := insertPatternsTemplate(ctx, transaction, template)
	if err != nil {
		return patterns.Template{}, err
	}
	if err := transaction.Commit(); err != nil {
		return patterns.Template{}, fmt.Errorf("commit Patterns template version: %w", err)
	}
	return created, nil
}

func (s *PatternsStore) ImportTemplateHistory(ctx context.Context, organizationID string, history []patterns.Template) error {
	if len(history) == 0 || len(history) > patterns.MaximumExchangeVersions {
		return patterns.ErrInvalidInput
	}
	first := history[0]
	if first.OrganizationID != organizationID {
		return patterns.ErrInvalidInput
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Patterns Exchange history import: %w", err)
	}
	defer transaction.Rollback()
	// The lock serializes both stable-ID and organization-local normalized-name
	// decisions before any version is written, so partial histories cannot leak.
	if _, err := transaction.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1 || chr(31) || $2, 8108))`, organizationID, first.ID); err != nil {
		return fmt.Errorf("lock Patterns Exchange history import: %w", err)
	}
	var existingCount int
	if err := transaction.QueryRowContext(ctx, `SELECT count(*) FROM pattern_template_versions WHERE organization_id=$1 AND id=$2`, organizationID, first.ID).Scan(&existingCount); err != nil {
		return fmt.Errorf("inspect Patterns Exchange history: %w", err)
	}
	if existingCount > 0 {
		if existingCount != len(history) {
			return patterns.ErrConflict
		}
		for _, expected := range history {
			observed, err := scanPatternsRead("read Patterns Exchange history", transaction.QueryRowContext(ctx,
				`SELECT `+patternsColumns+` FROM pattern_template_versions WHERE organization_id=$1 AND id=$2 AND version=$3`, organizationID, first.ID, expected.Version))
			if err != nil || !samePostgresPatternsTemplate(observed, expected) {
				return patterns.ErrConflict
			}
		}
		return transaction.Commit()
	}
	for index, template := range history {
		if template.OrganizationID != organizationID || template.ID != first.ID || template.RecordType != first.RecordType ||
			template.Name != first.Name || template.Version != int64(index+1) || template.BuiltIn {
			return patterns.ErrInvalidInput
		}
		if _, err := insertPatternsTemplate(ctx, transaction, template); err != nil {
			return err
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit Patterns Exchange history import: %w", err)
	}
	return nil
}

func samePostgresPatternsTemplate(left, right patterns.Template) bool {
	leftFields, leftErr := json.Marshal(left.Fields)
	rightFields, rightErr := json.Marshal(right.Fields)
	return leftErr == nil && rightErr == nil && left.ID == right.ID && left.OrganizationID == right.OrganizationID &&
		left.RecordType == right.RecordType && left.Name == right.Name && left.Description == right.Description &&
		left.Version == right.Version && left.Status == right.Status && left.CreatedBy == right.CreatedBy &&
		left.CreatedAt.Equal(right.CreatedAt) && string(leftFields) == string(rightFields)
}

func (s *PatternsStore) insertTemplate(ctx context.Context, template patterns.Template) (patterns.Template, error) {
	return insertPatternsTemplate(ctx, s.database, template)
}

type patternsQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func insertPatternsTemplate(ctx context.Context, query patternsQuerier, template patterns.Template) (patterns.Template, error) {
	fields, err := json.Marshal(template.Fields)
	if err != nil {
		return patterns.Template{}, fmt.Errorf("marshal Patterns fields: %w", err)
	}
	created, err := scanPatternsTemplate(query.QueryRowContext(ctx, `INSERT INTO pattern_template_versions
		(organization_id, id, record_type, name, description, version, status, fields, created_by, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING `+patternsColumns,
		template.OrganizationID, template.ID, template.RecordType, template.Name, template.Description,
		template.Version, template.Status, fields, template.CreatedBy, template.CreatedAt))
	return created, translatePatternsWriteError("create Patterns template", err)
}

type patternsScanner interface{ Scan(...any) error }

func scanPatternsTemplate(row patternsScanner) (patterns.Template, error) {
	var template patterns.Template
	var rawFields []byte
	err := row.Scan(&template.OrganizationID, &template.ID, &template.RecordType, &template.Name, &template.Description,
		&template.Version, &template.Status, &rawFields, &template.CreatedBy, &template.CreatedAt)
	if err != nil {
		return patterns.Template{}, err
	}
	template.BuiltIn = false
	if err := json.Unmarshal(rawFields, &template.Fields); err != nil {
		return patterns.Template{}, fmt.Errorf("decode Patterns fields: %w", err)
	}
	return template, nil
}

func scanPatternsRead(operation string, row patternsScanner) (patterns.Template, error) {
	template, err := scanPatternsTemplate(row)
	if errors.Is(err, sql.ErrNoRows) {
		return patterns.Template{}, patterns.ErrNotFound
	}
	if err != nil {
		return patterns.Template{}, fmt.Errorf("%s: %w", operation, err)
	}
	return template, nil
}

func translatePatternsWriteError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505":
			return patterns.ErrConflict
		case "23502", "23514", "22P02":
			return patterns.ErrInvalidInput
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
