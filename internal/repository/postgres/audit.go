package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/maxlemke/stewardmesh/internal/foundation"
)

type Auditor struct {
	database *sql.DB
}

var _ foundation.Auditor = (*Auditor)(nil)

func NewAuditor(database *sql.DB) (*Auditor, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &Auditor{database: database}, nil
}

// Record accepts an exact retry and rejects reuse of an event ID with
// different immutable content.
// Requirement: REQ-ATLAS-CODES-001. Feature: inventory.identifiers.
func (a *Auditor) Record(ctx context.Context, event foundation.AuditEvent) error {
	if event.ID == "" || event.OrganizationID == "" || event.CorrelationID == "" ||
		event.Action == "" || event.ResourceType == "" || event.ResourceID == "" || event.OccurredAt.IsZero() {
		return errors.New("complete audit identity, action, resource, and timestamp are required")
	}
	if event.Metadata == nil {
		event.Metadata = map[string]string{}
	}
	metadata, err := json.Marshal(event.Metadata)
	if err != nil {
		return fmt.Errorf("encode audit metadata: %w", err)
	}
	result, err := a.database.ExecContext(ctx, `
		INSERT INTO audit_events (
			id, organization_id, actor_id, correlation_id, action,
			resource_type, resource_id, occurred_at, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO NOTHING
	`,
		event.ID,
		event.OrganizationID,
		event.ActorID,
		event.CorrelationID,
		event.Action,
		event.ResourceType,
		event.ResourceID,
		event.OccurredAt,
		string(metadata),
	)
	if err != nil {
		return fmt.Errorf("record audit event: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect audit event write: %w", err)
	}
	if rowsAffected == 1 {
		return nil
	}
	var identical bool
	err = a.database.QueryRowContext(ctx, `
		SELECT organization_id = $2
			AND actor_id = $3
			AND correlation_id = $4
			AND action = $5
			AND resource_type = $6
			AND resource_id = $7
			AND occurred_at = $8
			AND metadata = $9::jsonb
		FROM audit_events
		WHERE id = $1
	`,
		event.ID,
		event.OrganizationID,
		event.ActorID,
		event.CorrelationID,
		event.Action,
		event.ResourceType,
		event.ResourceID,
		event.OccurredAt,
		string(metadata),
	).Scan(&identical)
	if err != nil {
		return fmt.Errorf("verify audit event replay: %w", err)
	}
	if !identical {
		return errors.New("audit event id conflicts with different immutable content")
	}
	return nil
}
