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
	if _, err := a.database.ExecContext(ctx, `
		INSERT INTO audit_events (
			id, organization_id, actor_id, correlation_id, action,
			resource_type, resource_id, occurred_at, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
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
	); err != nil {
		return fmt.Errorf("record audit event: %w", err)
	}
	return nil
}
