package postgres

// PostgreSQL Reach adapter. Requirement: REQ-REACH-001. Feature: messaging.delivery. GitHub: #12.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxlemke/stewardmesh/internal/reach"
)

type ReachStore struct{ database *sql.DB }

func NewReachStore(database *sql.DB) (*ReachStore, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &ReachStore{database: database}, nil
}

const reachProviderColumns = `organization_id,id,name,kind,endpoint_id,sender,secret_ref,enabled,revision,created_by,updated_by,created_at,updated_at`
const reachTemplateColumns = `organization_id,id,name,subject,body,revision,created_by,updated_by,created_at,updated_at`
const reachGroupColumns = `organization_id,id,name,provider_id,template_id,recipients,revision,created_by,updated_by,created_at,updated_at`
const reachMessageColumns = `organization_id,id,group_id,provider_id,template_id,source_kind,source_id,subject,body,recipients,status,attempts,next_attempt_at,last_error_code,claim_token,claimed_at,created_by,created_at,updated_at`
const reachAttemptColumns = `organization_id,id,message_id,attempt,outcome,error_code,retryable,next_attempt_at,occurred_at`
const reachProviderTestColumns = `organization_id,id,provider_id,outcome,error_code,tested_by,tested_at`

func (s *ReachStore) ListProviders(ctx context.Context, organizationID string) ([]reach.Provider, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT `+reachProviderColumns+` FROM reach_providers WHERE organization_id=$1 ORDER BY lower(name),id`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list Reach providers: %w", err)
	}
	defer rows.Close()
	items := []reach.Provider{}
	for rows.Next() {
		item, err := scanReachProvider(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Reach provider: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *ReachStore) GetProvider(ctx context.Context, organizationID, id string) (reach.Provider, error) {
	item, err := scanReachProvider(s.database.QueryRowContext(ctx, `SELECT `+reachProviderColumns+` FROM reach_providers WHERE organization_id=$1 AND id=$2`, organizationID, id))
	return item, reachReadError("get Reach provider", err)
}

func (s *ReachStore) CreateProvider(ctx context.Context, item reach.Provider) (reach.Provider, error) {
	created, err := scanReachProvider(s.database.QueryRowContext(ctx, `INSERT INTO reach_providers (organization_id,id,name,kind,endpoint_id,sender,secret_ref,enabled,revision,created_by,updated_by,created_at,updated_at)
		SELECT $1,$2,$3,$4,$5,NULLIF($6,''),$7,$8,$9,$10,$11,$12,$13 WHERE (SELECT count(*) FROM reach_providers WHERE organization_id=$1) < $14 RETURNING `+reachProviderColumns,
		item.OrganizationID, item.ID, item.Name, item.Kind, item.EndpointID, item.Sender, item.SecretRef, item.Enabled, item.Revision, item.CreatedBy, item.UpdatedBy, item.CreatedAt, item.UpdatedAt, reach.MaximumProviders))
	if errors.Is(err, sql.ErrNoRows) {
		return reach.Provider{}, reach.ErrConflict
	}
	return created, reachWriteError("create Reach provider", err)
}

func (s *ReachStore) UpdateProvider(ctx context.Context, item reach.Provider, expectedRevision int64) (reach.Provider, error) {
	updated, err := scanReachProvider(s.database.QueryRowContext(ctx, `UPDATE reach_providers SET name=$3,sender=NULLIF($4,''),secret_ref=$5,enabled=$6,revision=$7,updated_by=$8,updated_at=$9 WHERE organization_id=$1 AND id=$2 AND revision=$10 RETURNING `+reachProviderColumns,
		item.OrganizationID, item.ID, item.Name, item.Sender, item.SecretRef, item.Enabled, item.Revision, item.UpdatedBy, item.UpdatedAt, expectedRevision))
	if errors.Is(err, sql.ErrNoRows) {
		return reach.Provider{}, reachMissingOrConflict(ctx, s.database, "reach_providers", item.OrganizationID, item.ID)
	}
	return updated, reachWriteError("update Reach provider", err)
}

func (s *ReachStore) ListTemplates(ctx context.Context, organizationID string) ([]reach.Template, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT `+reachTemplateColumns+` FROM reach_templates WHERE organization_id=$1 ORDER BY lower(name),id`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list Reach templates: %w", err)
	}
	defer rows.Close()
	items := []reach.Template{}
	for rows.Next() {
		item, err := scanReachTemplate(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *ReachStore) GetTemplate(ctx context.Context, organizationID, id string) (reach.Template, error) {
	item, err := scanReachTemplate(s.database.QueryRowContext(ctx, `SELECT `+reachTemplateColumns+` FROM reach_templates WHERE organization_id=$1 AND id=$2`, organizationID, id))
	return item, reachReadError("get Reach template", err)
}

func (s *ReachStore) CreateTemplate(ctx context.Context, item reach.Template) (reach.Template, error) {
	created, err := scanReachTemplate(s.database.QueryRowContext(ctx, `INSERT INTO reach_templates (organization_id,id,name,subject,body,revision,created_by,updated_by,created_at,updated_at)
		SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10 WHERE (SELECT count(*) FROM reach_templates WHERE organization_id=$1) < $11 RETURNING `+reachTemplateColumns,
		item.OrganizationID, item.ID, item.Name, item.Subject, item.Body, item.Revision, item.CreatedBy, item.UpdatedBy, item.CreatedAt, item.UpdatedAt, reach.MaximumTemplates))
	if errors.Is(err, sql.ErrNoRows) {
		return reach.Template{}, reach.ErrConflict
	}
	return created, reachWriteError("create Reach template", err)
}

func (s *ReachStore) UpdateTemplate(ctx context.Context, item reach.Template, expectedRevision int64) (reach.Template, error) {
	updated, err := scanReachTemplate(s.database.QueryRowContext(ctx, `UPDATE reach_templates SET name=$3,subject=$4,body=$5,revision=$6,updated_by=$7,updated_at=$8 WHERE organization_id=$1 AND id=$2 AND revision=$9 RETURNING `+reachTemplateColumns,
		item.OrganizationID, item.ID, item.Name, item.Subject, item.Body, item.Revision, item.UpdatedBy, item.UpdatedAt, expectedRevision))
	if errors.Is(err, sql.ErrNoRows) {
		return reach.Template{}, reachMissingOrConflict(ctx, s.database, "reach_templates", item.OrganizationID, item.ID)
	}
	return updated, reachWriteError("update Reach template", err)
}

func (s *ReachStore) ListGroups(ctx context.Context, organizationID string) ([]reach.SubscriberGroup, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT `+reachGroupColumns+` FROM reach_subscriber_groups WHERE organization_id=$1 ORDER BY lower(name),id`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list Reach subscriber groups: %w", err)
	}
	defer rows.Close()
	items := []reach.SubscriberGroup{}
	for rows.Next() {
		item, err := scanReachGroup(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *ReachStore) GetGroup(ctx context.Context, organizationID, id string) (reach.SubscriberGroup, error) {
	item, err := scanReachGroup(s.database.QueryRowContext(ctx, `SELECT `+reachGroupColumns+` FROM reach_subscriber_groups WHERE organization_id=$1 AND id=$2`, organizationID, id))
	return item, reachReadError("get Reach subscriber group", err)
}

func (s *ReachStore) CreateGroup(ctx context.Context, item reach.SubscriberGroup) (reach.SubscriberGroup, error) {
	recipients, err := json.Marshal(item.Recipients)
	if err != nil {
		return reach.SubscriberGroup{}, reach.ErrInvalidInput
	}
	created, err := scanReachGroup(s.database.QueryRowContext(ctx, `INSERT INTO reach_subscriber_groups (organization_id,id,name,provider_id,template_id,recipients,revision,created_by,updated_by,created_at,updated_at)
		SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11 WHERE (SELECT count(*) FROM reach_subscriber_groups WHERE organization_id=$1) < $12 RETURNING `+reachGroupColumns,
		item.OrganizationID, item.ID, item.Name, item.ProviderID, item.TemplateID, recipients, item.Revision, item.CreatedBy, item.UpdatedBy, item.CreatedAt, item.UpdatedAt, reach.MaximumGroups))
	if errors.Is(err, sql.ErrNoRows) {
		return reach.SubscriberGroup{}, reach.ErrConflict
	}
	return created, reachWriteError("create Reach subscriber group", err)
}

func (s *ReachStore) UpdateGroup(ctx context.Context, item reach.SubscriberGroup, expectedRevision int64) (reach.SubscriberGroup, error) {
	recipients, err := json.Marshal(item.Recipients)
	if err != nil {
		return reach.SubscriberGroup{}, reach.ErrInvalidInput
	}
	updated, err := scanReachGroup(s.database.QueryRowContext(ctx, `UPDATE reach_subscriber_groups SET name=$3,provider_id=$4,template_id=$5,recipients=$6,revision=$7,updated_by=$8,updated_at=$9 WHERE organization_id=$1 AND id=$2 AND revision=$10 RETURNING `+reachGroupColumns,
		item.OrganizationID, item.ID, item.Name, item.ProviderID, item.TemplateID, recipients, item.Revision, item.UpdatedBy, item.UpdatedAt, expectedRevision))
	if errors.Is(err, sql.ErrNoRows) {
		return reach.SubscriberGroup{}, reachMissingOrConflict(ctx, s.database, "reach_subscriber_groups", item.OrganizationID, item.ID)
	}
	return updated, reachWriteError("update Reach subscriber group", err)
}

func (s *ReachStore) ListMessages(ctx context.Context, organizationID string, limit int) ([]reach.Message, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT `+reachMessageColumns+` FROM reach_messages WHERE organization_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2`, organizationID, limit)
	if err != nil {
		return nil, fmt.Errorf("list Reach messages: %w", err)
	}
	defer rows.Close()
	items := []reach.Message{}
	for rows.Next() {
		item, err := scanReachMessage(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *ReachStore) GetMessage(ctx context.Context, organizationID, id string) (reach.Message, error) {
	item, err := scanReachMessage(s.database.QueryRowContext(ctx, `SELECT `+reachMessageColumns+` FROM reach_messages WHERE organization_id=$1 AND id=$2`, organizationID, id))
	return item, reachReadError("get Reach message", err)
}

func (s *ReachStore) CreateMessage(ctx context.Context, item reach.Message) (reach.Message, bool, error) {
	recipients, err := json.Marshal(item.Recipients)
	if err != nil {
		return reach.Message{}, false, reach.ErrInvalidInput
	}
	created, err := scanReachMessage(s.database.QueryRowContext(ctx, `INSERT INTO reach_messages (organization_id,id,group_id,provider_id,template_id,source_kind,source_id,subject,body,recipients,status,attempts,next_attempt_at,last_error_code,claim_token,claimed_at,created_by,created_at,updated_at)
		SELECT $1,$2,NULLIF($3,''),$4,NULLIF($5,''),$6,NULLIF($7,''),$8,$9,$10,$11,$12,$13,NULLIF($14,''),NULL,NULL,$15,$16,$17
		WHERE (SELECT count(*) FROM reach_messages WHERE organization_id=$1) < $18 ON CONFLICT (organization_id,id) DO NOTHING RETURNING `+reachMessageColumns,
		item.OrganizationID, item.ID, item.GroupID, item.ProviderID, item.TemplateID, item.SourceKind, item.SourceID, item.Subject, item.Body, recipients,
		item.Status, item.Attempts, item.NextAttemptAt, item.LastErrorCode, item.CreatedBy, item.CreatedAt, item.UpdatedAt, reach.MaximumMessages))
	if errors.Is(err, sql.ErrNoRows) {
		existing, getErr := s.GetMessage(ctx, item.OrganizationID, item.ID)
		if getErr != nil {
			return reach.Message{}, false, reach.ErrConflict
		}
		if existing.SourceKind != item.SourceKind || existing.SourceID != item.SourceID || existing.GroupID != item.GroupID || existing.ProviderID != item.ProviderID {
			return reach.Message{}, false, reach.ErrConflict
		}
		return existing, false, nil
	}
	return created, err == nil, reachWriteError("create Reach message", err)
}

func (s *ReachStore) ClaimMessage(ctx context.Context, organizationID, id, expectedStatus string, expectedAttempts int, claimToken string, claimedAt, staleBefore time.Time) (reach.Message, error) {
	claimed, err := scanReachMessage(s.database.QueryRowContext(ctx, `UPDATE reach_messages
		SET claim_token=$5,claimed_at=$6,updated_at=$6
		WHERE organization_id=$1 AND id=$2 AND status=$3 AND attempts=$4
		  AND (claim_token IS NULL OR claimed_at <= $7)
		RETURNING `+reachMessageColumns, organizationID, id, expectedStatus, expectedAttempts, claimToken, claimedAt, staleBefore))
	if errors.Is(err, sql.ErrNoRows) {
		if missing := reachMissingOrConflict(ctx, s.database, "reach_messages", organizationID, id); errors.Is(missing, reach.ErrNotFound) {
			return reach.Message{}, missing
		}
		return reach.Message{}, reach.ErrConflict
	}
	return claimed, reachWriteError("claim Reach message", err)
}

func (s *ReachStore) RecordAttempt(ctx context.Context, item reach.Message, expectedAttempts int, attempt reach.DeliveryAttempt) (reach.Message, error) {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return reach.Message{}, err
	}
	defer tx.Rollback()
	updated, err := scanReachMessage(tx.QueryRowContext(ctx, `UPDATE reach_messages SET status=$3,attempts=$4,next_attempt_at=$5,last_error_code=NULLIF($6,''),claim_token=NULL,claimed_at=NULL,updated_at=$7 WHERE organization_id=$1 AND id=$2 AND attempts=$8 AND claim_token=$9 RETURNING `+reachMessageColumns,
		item.OrganizationID, item.ID, item.Status, item.Attempts, item.NextAttemptAt, item.LastErrorCode, item.UpdatedAt, expectedAttempts, item.ClaimToken))
	if errors.Is(err, sql.ErrNoRows) {
		if missing := reachMissingOrConflict(ctx, tx, "reach_messages", item.OrganizationID, item.ID); errors.Is(missing, reach.ErrNotFound) {
			return reach.Message{}, missing
		}
		return reach.Message{}, reach.ErrConflict
	}
	if err != nil {
		return reach.Message{}, reachWriteError("update Reach message attempt", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO reach_delivery_attempts (organization_id,id,message_id,attempt,outcome,error_code,retryable,next_attempt_at,occurred_at) VALUES($1,$2,$3,$4,$5,NULLIF($6,''),$7,$8,$9)`,
		attempt.OrganizationID, attempt.ID, attempt.MessageID, attempt.Attempt, attempt.Outcome, attempt.ErrorCode, attempt.Retryable, attempt.NextAttemptAt, attempt.OccurredAt)
	if err != nil {
		return reach.Message{}, reachWriteError("record Reach delivery attempt", err)
	}
	if err := tx.Commit(); err != nil {
		return reach.Message{}, fmt.Errorf("commit Reach delivery attempt: %w", err)
	}
	return updated, nil
}

func (s *ReachStore) ListAttempts(ctx context.Context, organizationID, messageID string) ([]reach.DeliveryAttempt, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT `+reachAttemptColumns+` FROM reach_delivery_attempts WHERE organization_id=$1 AND message_id=$2 ORDER BY attempt,id`, organizationID, messageID)
	if err != nil {
		return nil, fmt.Errorf("list Reach delivery attempts: %w", err)
	}
	defer rows.Close()
	items := []reach.DeliveryAttempt{}
	for rows.Next() {
		item, err := scanReachAttempt(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		if _, err := s.GetMessage(ctx, organizationID, messageID); err != nil {
			return nil, err
		}
	}
	return items, rows.Err()
}

func (s *ReachStore) CreateProviderTest(ctx context.Context, item reach.ProviderTest) (reach.ProviderTest, error) {
	created, err := scanReachProviderTest(s.database.QueryRowContext(ctx, `INSERT INTO reach_provider_tests (organization_id,id,provider_id,outcome,error_code,tested_by,tested_at) VALUES($1,$2,$3,$4,NULLIF($5,''),$6,$7) RETURNING `+reachProviderTestColumns,
		item.OrganizationID, item.ID, item.ProviderID, item.Outcome, item.ErrorCode, item.TestedBy, item.TestedAt))
	return created, reachWriteError("record Reach provider test", err)
}

func (s *ReachStore) ListProviderTests(ctx context.Context, organizationID, providerID string) ([]reach.ProviderTest, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT `+reachProviderTestColumns+` FROM reach_provider_tests WHERE organization_id=$1 AND provider_id=$2 ORDER BY tested_at DESC,id LIMIT 100`, organizationID, providerID)
	if err != nil {
		return nil, fmt.Errorf("list Reach provider tests: %w", err)
	}
	defer rows.Close()
	items := []reach.ProviderTest{}
	for rows.Next() {
		item, err := scanReachProviderTest(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		if _, err := s.GetProvider(ctx, organizationID, providerID); err != nil {
			return nil, err
		}
	}
	return items, rows.Err()
}

type reachScanner interface{ Scan(...any) error }

func scanReachProvider(row reachScanner) (reach.Provider, error) {
	var item reach.Provider
	var sender sql.NullString
	err := row.Scan(&item.OrganizationID, &item.ID, &item.Name, &item.Kind, &item.EndpointID, &sender, &item.SecretRef, &item.Enabled, &item.Revision, &item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt)
	item.Sender, item.SecretConfigured = sender.String, item.SecretRef != ""
	return item, err
}

func scanReachTemplate(row reachScanner) (reach.Template, error) {
	var item reach.Template
	err := row.Scan(&item.OrganizationID, &item.ID, &item.Name, &item.Subject, &item.Body, &item.Revision, &item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func scanReachGroup(row reachScanner) (reach.SubscriberGroup, error) {
	var item reach.SubscriberGroup
	var recipients []byte
	err := row.Scan(&item.OrganizationID, &item.ID, &item.Name, &item.ProviderID, &item.TemplateID, &recipients, &item.Revision, &item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt)
	if err == nil {
		err = json.Unmarshal(recipients, &item.Recipients)
	}
	return item, err
}

func scanReachMessage(row reachScanner) (reach.Message, error) {
	var item reach.Message
	var groupID, templateID, sourceID, errorCode, claimToken sql.NullString
	var next, claimed sql.NullTime
	var recipients []byte
	err := row.Scan(&item.OrganizationID, &item.ID, &groupID, &item.ProviderID, &templateID, &item.SourceKind, &sourceID, &item.Subject, &item.Body, &recipients,
		&item.Status, &item.Attempts, &next, &errorCode, &claimToken, &claimed, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt)
	if err == nil {
		err = json.Unmarshal(recipients, &item.Recipients)
	}
	item.GroupID, item.TemplateID, item.SourceID, item.LastErrorCode, item.ClaimToken = groupID.String, templateID.String, sourceID.String, errorCode.String, claimToken.String
	if next.Valid {
		item.NextAttemptAt = &next.Time
	}
	if claimed.Valid {
		item.ClaimedAt = &claimed.Time
	}
	return item, err
}

func scanReachAttempt(row reachScanner) (reach.DeliveryAttempt, error) {
	var item reach.DeliveryAttempt
	var errorCode sql.NullString
	var next sql.NullTime
	err := row.Scan(&item.OrganizationID, &item.ID, &item.MessageID, &item.Attempt, &item.Outcome, &errorCode, &item.Retryable, &next, &item.OccurredAt)
	item.ErrorCode = errorCode.String
	if next.Valid {
		item.NextAttemptAt = &next.Time
	}
	return item, err
}

func scanReachProviderTest(row reachScanner) (reach.ProviderTest, error) {
	var item reach.ProviderTest
	var errorCode sql.NullString
	err := row.Scan(&item.OrganizationID, &item.ID, &item.ProviderID, &item.Outcome, &errorCode, &item.TestedBy, &item.TestedAt)
	item.ErrorCode = errorCode.String
	return item, err
}

type reachQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func reachMissingOrConflict(ctx context.Context, queryer reachQueryer, table, organizationID, id string) error {
	var exists bool
	if err := queryer.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE organization_id=$1 AND id=$2)`, organizationID, id).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return reach.ErrConflict
	}
	return reach.ErrNotFound
}

func reachReadError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return reach.ErrNotFound
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func reachWriteError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23505":
			return reach.ErrConflict
		case "23503":
			return reach.ErrNotFound
		case "23502", "23514", "22P02":
			return reach.ErrInvalidInput
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
