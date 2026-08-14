package postgres

// PostgreSQL Signals adapter. Requirement: REQ-SIGNALS-001. Feature: alerts.rules. GitHub: #11.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxlemke/stewardmesh/internal/signals"
)

type SignalsStore struct{ database *sql.DB }

func NewSignalsStore(database *sql.DB) (*SignalsStore, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &SignalsStore{database: database}, nil
}

const signalRuleColumns = `organization_id, id, name, condition, severity, enabled, to_json(threshold_days), fiscal_period, scenario, created_by, revision, created_at, updated_at`
const signalAlertColumns = `organization_id, id, rule_id, condition, severity, status, title, summary, target_type, target_id, due_at, threshold_days, deduplication_key, assigned_kind, assigned_id, acknowledged_by, acknowledged_at, first_detected_at, last_observed_at, resolved_at, revision`
const signalSubscriptionColumns = `organization_id, id, rule_id, target_kind, target_id, enabled, created_by, revision, created_at, updated_at`
const signalDeliveryColumns = `organization_id, id, alert_id, subscription_id, target_kind, target_id, status, attempts, next_attempt_at, last_error_code, created_at, updated_at`

func (s *SignalsStore) ExchangeSnapshot(ctx context.Context, organizationID string, maximum int) (signals.ExchangeSnapshot, error) {
	if maximum < 1 {
		return signals.ExchangeSnapshot{}, signals.ErrInvalidInput
	}
	tx, err := s.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return signals.ExchangeSnapshot{}, fmt.Errorf("begin Signals Exchange snapshot: %w", err)
	}
	defer tx.Rollback()
	var count int64
	if err := tx.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM signal_rules WHERE organization_id=$1) +
		(SELECT count(*) FROM signal_subscriptions WHERE organization_id=$1)`, organizationID).Scan(&count); err != nil {
		return signals.ExchangeSnapshot{}, fmt.Errorf("count Signals Exchange snapshot: %w", err)
	}
	if count > int64(maximum) {
		return signals.ExchangeSnapshot{}, signals.ErrTooLarge
	}
	result := signals.ExchangeSnapshot{Rules: []signals.Rule{}, Subscriptions: []signals.Subscription{}}
	ruleRows, err := tx.QueryContext(ctx, `SELECT `+signalRuleColumns+` FROM signal_rules WHERE organization_id=$1 ORDER BY id`, organizationID)
	if err != nil {
		return signals.ExchangeSnapshot{}, fmt.Errorf("list Signals Exchange rules: %w", err)
	}
	for ruleRows.Next() {
		item, scanErr := scanSignalRule(ruleRows)
		if scanErr != nil {
			ruleRows.Close()
			return signals.ExchangeSnapshot{}, fmt.Errorf("scan Signals Exchange rule: %w", scanErr)
		}
		result.Rules = append(result.Rules, item)
	}
	if err := ruleRows.Close(); err != nil {
		return signals.ExchangeSnapshot{}, fmt.Errorf("close Signals Exchange rules: %w", err)
	}
	if err := ruleRows.Err(); err != nil {
		return signals.ExchangeSnapshot{}, fmt.Errorf("iterate Signals Exchange rules: %w", err)
	}
	subscriptionRows, err := tx.QueryContext(ctx, `SELECT `+signalSubscriptionColumns+` FROM signal_subscriptions WHERE organization_id=$1 ORDER BY id`, organizationID)
	if err != nil {
		return signals.ExchangeSnapshot{}, fmt.Errorf("list Signals Exchange subscriptions: %w", err)
	}
	for subscriptionRows.Next() {
		item, scanErr := scanSignalSubscription(subscriptionRows)
		if scanErr != nil {
			subscriptionRows.Close()
			return signals.ExchangeSnapshot{}, fmt.Errorf("scan Signals Exchange subscription: %w", scanErr)
		}
		result.Subscriptions = append(result.Subscriptions, item)
	}
	if err := subscriptionRows.Close(); err != nil {
		return signals.ExchangeSnapshot{}, fmt.Errorf("close Signals Exchange subscriptions: %w", err)
	}
	if err := subscriptionRows.Err(); err != nil {
		return signals.ExchangeSnapshot{}, fmt.Errorf("iterate Signals Exchange subscriptions: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return signals.ExchangeSnapshot{}, fmt.Errorf("complete Signals Exchange snapshot: %w", err)
	}
	return result, nil
}

func (s *SignalsStore) ListRules(ctx context.Context, organizationID string) ([]signals.Rule, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT `+signalRuleColumns+` FROM signal_rules WHERE organization_id = $1 ORDER BY lower(name), id`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list Signals rules: %w", err)
	}
	defer rows.Close()
	items := []signals.Rule{}
	for rows.Next() {
		item, err := scanSignalRule(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Signals rule: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *SignalsStore) GetRule(ctx context.Context, organizationID, id string) (signals.Rule, error) {
	item, err := scanSignalRule(s.database.QueryRowContext(ctx, `SELECT `+signalRuleColumns+` FROM signal_rules WHERE organization_id=$1 AND id=$2`, organizationID, id))
	return item, signalReadError("get Signals rule", err)
}

func (s *SignalsStore) CreateRule(ctx context.Context, item signals.Rule) (signals.Rule, error) {
	created, err := scanSignalRule(s.database.QueryRowContext(ctx, `INSERT INTO signal_rules (organization_id,id,name,condition,severity,enabled,threshold_days,fiscal_period,scenario,created_by,revision,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),NULLIF($9,''),$10,$11,$12,$13) RETURNING `+signalRuleColumns,
		item.OrganizationID, item.ID, item.Name, item.Condition, item.Severity, item.Enabled, item.ThresholdDays, item.FiscalPeriod, item.Scenario, item.CreatedBy, item.Revision, item.CreatedAt, item.UpdatedAt))
	return created, signalWriteError("create Signals rule", err)
}

func (s *SignalsStore) UpdateRule(ctx context.Context, item signals.Rule, expectedRevision int64) (signals.Rule, error) {
	updated, err := scanSignalRule(s.database.QueryRowContext(ctx, `UPDATE signal_rules SET name=$3,condition=$4,severity=$5,enabled=$6,threshold_days=$7,fiscal_period=NULLIF($8,''),scenario=NULLIF($9,''),revision=$10,updated_at=$11
		WHERE organization_id=$1 AND id=$2 AND revision=$12 RETURNING `+signalRuleColumns,
		item.OrganizationID, item.ID, item.Name, item.Condition, item.Severity, item.Enabled, item.ThresholdDays, item.FiscalPeriod, item.Scenario, item.Revision, item.UpdatedAt, expectedRevision))
	if errors.Is(err, sql.ErrNoRows) {
		return signals.Rule{}, s.missingOrConflict(ctx, "signal_rules", item.OrganizationID, item.ID)
	}
	return updated, signalWriteError("update Signals rule", err)
}

func (s *SignalsStore) ImportRule(ctx context.Context, item signals.Rule) (signals.Rule, bool, error) {
	created, err := scanSignalRule(s.database.QueryRowContext(ctx, `INSERT INTO signal_rules (organization_id,id,name,condition,severity,enabled,threshold_days,fiscal_period,scenario,created_by,revision,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),NULLIF($9,''),$10,$11,$12,$13)
		ON CONFLICT (organization_id,id) DO NOTHING RETURNING `+signalRuleColumns,
		item.OrganizationID, item.ID, item.Name, item.Condition, item.Severity, item.Enabled, item.ThresholdDays, item.FiscalPeriod, item.Scenario, item.CreatedBy, item.Revision, item.CreatedAt, item.UpdatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		existing, getErr := s.GetRule(ctx, item.OrganizationID, item.ID)
		return existing, false, getErr
	}
	return created, err == nil, signalWriteError("import Signals rule", err)
}

func (s *SignalsStore) ListAlerts(ctx context.Context, organizationID string, query signals.AlertQuery) ([]signals.Alert, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT `+signalAlertColumns+` FROM signal_alerts WHERE organization_id=$1 AND ($2='' OR rule_id=$2) AND ($3='' OR status=$3) AND ($4='' OR severity=$4) AND ($5='' OR condition=$5)
		ORDER BY last_observed_at DESC,id LIMIT $6`, organizationID, query.RuleID, query.Status, query.Severity, query.Condition, query.Limit)
	if err != nil {
		return nil, fmt.Errorf("list Signals alerts: %w", err)
	}
	defer rows.Close()
	items := []signals.Alert{}
	for rows.Next() {
		item, err := scanSignalAlert(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Signals alert: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *SignalsStore) GetAlert(ctx context.Context, organizationID, id string) (signals.Alert, error) {
	item, err := scanSignalAlert(s.database.QueryRowContext(ctx, `SELECT `+signalAlertColumns+` FROM signal_alerts WHERE organization_id=$1 AND id=$2`, organizationID, id))
	return item, signalReadError("get Signals alert", err)
}

func (s *SignalsStore) GetAlertByDeduplicationKey(ctx context.Context, organizationID, dedup string) (signals.Alert, error) {
	item, err := scanSignalAlert(s.database.QueryRowContext(ctx, `SELECT `+signalAlertColumns+` FROM signal_alerts WHERE organization_id=$1 AND deduplication_key=$2`, organizationID, dedup))
	return item, signalReadError("get Signals alert by deduplication key", err)
}

func (s *SignalsStore) CreateAlert(ctx context.Context, alert signals.Alert, history signals.AlertHistory) (signals.Alert, error) {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return signals.Alert{}, err
	}
	defer tx.Rollback()
	created, err := scanSignalAlert(tx.QueryRowContext(ctx, `INSERT INTO signal_alerts (organization_id,id,rule_id,condition,severity,status,title,summary,target_type,target_id,due_at,threshold_days,deduplication_key,assigned_kind,assigned_id,acknowledged_by,acknowledged_at,first_detected_at,last_observed_at,resolved_at,revision)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,NULLIF($14,''),NULLIF($15,''),NULLIF($16,''),$17,$18,$19,$20,$21) RETURNING `+signalAlertColumns,
		alert.OrganizationID, alert.ID, alert.RuleID, alert.Condition, alert.Severity, alert.Status, alert.Title, alert.Summary, alert.TargetType, alert.TargetID, alert.DueAt, alert.ThresholdDays, alert.DeduplicationKey, alert.AssignedKind, alert.AssignedID, alert.AcknowledgedBy, alert.AcknowledgedAt, alert.FirstDetectedAt, alert.LastObservedAt, alert.ResolvedAt, alert.Revision))
	if err != nil {
		return signals.Alert{}, signalWriteError("create Signals alert", err)
	}
	if err := insertSignalHistory(ctx, tx, history); err != nil {
		return signals.Alert{}, err
	}
	if err := tx.Commit(); err != nil {
		return signals.Alert{}, fmt.Errorf("commit Signals alert creation: %w", err)
	}
	return created, nil
}

func (s *SignalsStore) UpdateAlert(ctx context.Context, alert signals.Alert, expectedRevision int64, history signals.AlertHistory) (signals.Alert, error) {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return signals.Alert{}, err
	}
	defer tx.Rollback()
	updated, err := scanSignalAlert(tx.QueryRowContext(ctx, `UPDATE signal_alerts SET condition=$3,severity=$4,status=$5,title=$6,summary=$7,due_at=$8,threshold_days=$9,assigned_kind=NULLIF($10,''),assigned_id=NULLIF($11,''),acknowledged_by=NULLIF($12,''),acknowledged_at=$13,last_observed_at=$14,resolved_at=$15,revision=$16
		WHERE organization_id=$1 AND id=$2 AND revision=$17 AND deduplication_key=$18 RETURNING `+signalAlertColumns,
		alert.OrganizationID, alert.ID, alert.Condition, alert.Severity, alert.Status, alert.Title, alert.Summary, alert.DueAt, alert.ThresholdDays, alert.AssignedKind, alert.AssignedID, alert.AcknowledgedBy, alert.AcknowledgedAt, alert.LastObservedAt, alert.ResolvedAt, alert.Revision, expectedRevision, alert.DeduplicationKey))
	if errors.Is(err, sql.ErrNoRows) {
		return signals.Alert{}, s.missingOrConflict(ctx, "signal_alerts", alert.OrganizationID, alert.ID)
	}
	if err != nil {
		return signals.Alert{}, signalWriteError("update Signals alert", err)
	}
	if err := insertSignalHistory(ctx, tx, history); err != nil {
		return signals.Alert{}, err
	}
	if err := tx.Commit(); err != nil {
		return signals.Alert{}, fmt.Errorf("commit Signals alert update: %w", err)
	}
	return updated, nil
}

func (s *SignalsStore) ListAlertHistory(ctx context.Context, organizationID, alertID string) ([]signals.AlertHistory, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT organization_id,id,alert_id,action,actor_id,occurred_at,revision FROM signal_alert_history WHERE organization_id=$1 AND alert_id=$2 ORDER BY revision DESC,id`, organizationID, alertID)
	if err != nil {
		return nil, fmt.Errorf("list Signals history: %w", err)
	}
	defer rows.Close()
	items := []signals.AlertHistory{}
	for rows.Next() {
		var item signals.AlertHistory
		if err := rows.Scan(&item.OrganizationID, &item.ID, &item.AlertID, &item.Action, &item.ActorID, &item.OccurredAt, &item.Revision); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		if _, err := s.GetAlert(ctx, organizationID, alertID); err != nil {
			return nil, err
		}
	}
	return items, rows.Err()
}

func (s *SignalsStore) ListSubscriptions(ctx context.Context, organizationID string) ([]signals.Subscription, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT `+signalSubscriptionColumns+` FROM signal_subscriptions WHERE organization_id=$1 ORDER BY id`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []signals.Subscription{}
	for rows.Next() {
		item, err := scanSignalSubscription(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *SignalsStore) GetSubscription(ctx context.Context, organizationID, id string) (signals.Subscription, error) {
	item, err := scanSignalSubscription(s.database.QueryRowContext(ctx, `SELECT `+signalSubscriptionColumns+` FROM signal_subscriptions WHERE organization_id=$1 AND id=$2`, organizationID, id))
	return item, signalReadError("get Signals subscription", err)
}

func (s *SignalsStore) CreateSubscription(ctx context.Context, item signals.Subscription) (signals.Subscription, error) {
	created, err := scanSignalSubscription(s.database.QueryRowContext(ctx, `INSERT INTO signal_subscriptions (organization_id,id,rule_id,target_kind,target_id,enabled,created_by,revision,created_at,updated_at) VALUES($1,$2,NULLIF($3,''),$4,$5,$6,$7,$8,$9,$10) RETURNING `+signalSubscriptionColumns, item.OrganizationID, item.ID, item.RuleID, item.TargetKind, item.TargetID, item.Enabled, item.CreatedBy, item.Revision, item.CreatedAt, item.UpdatedAt))
	return created, signalWriteError("create Signals subscription", err)
}

func (s *SignalsStore) ImportSubscription(ctx context.Context, item signals.Subscription) (signals.Subscription, bool, error) {
	created, err := scanSignalSubscription(s.database.QueryRowContext(ctx, `INSERT INTO signal_subscriptions (organization_id,id,rule_id,target_kind,target_id,enabled,created_by,revision,created_at,updated_at)
		VALUES($1,$2,NULLIF($3,''),$4,$5,$6,$7,$8,$9,$10) ON CONFLICT (organization_id,id) DO NOTHING RETURNING `+signalSubscriptionColumns,
		item.OrganizationID, item.ID, item.RuleID, item.TargetKind, item.TargetID, item.Enabled, item.CreatedBy, item.Revision, item.CreatedAt, item.UpdatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		existing, getErr := s.GetSubscription(ctx, item.OrganizationID, item.ID)
		return existing, false, getErr
	}
	return created, err == nil, signalWriteError("import Signals subscription", err)
}

func (s *SignalsStore) DeleteSubscription(ctx context.Context, organizationID, id string) (bool, error) {
	result, err := s.database.ExecContext(ctx, `DELETE FROM signal_subscriptions WHERE organization_id=$1 AND id=$2`, organizationID, id)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count > 0, err
}

func (s *SignalsStore) CreateDelivery(ctx context.Context, item signals.Delivery) (signals.Delivery, bool, error) {
	created, err := scanSignalDelivery(s.database.QueryRowContext(ctx, `INSERT INTO signal_deliveries (organization_id,id,alert_id,subscription_id,target_kind,target_id,status,attempts,next_attempt_at,last_error_code,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,''),$11,$12) ON CONFLICT (organization_id,id) DO NOTHING RETURNING `+signalDeliveryColumns, item.OrganizationID, item.ID, item.AlertID, item.SubscriptionID, item.TargetKind, item.TargetID, item.Status, item.Attempts, item.NextAttemptAt, item.LastErrorCode, item.CreatedAt, item.UpdatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		existing, getErr := s.getDelivery(ctx, item.OrganizationID, item.ID)
		if getErr != nil {
			return signals.Delivery{}, false, getErr
		}
		if existing.AlertID != item.AlertID || existing.SubscriptionID != item.SubscriptionID || existing.TargetKind != item.TargetKind || existing.TargetID != item.TargetID {
			return signals.Delivery{}, false, signals.ErrConflict
		}
		return existing, false, nil
	}
	return created, err == nil, signalWriteError("create Signals delivery", err)
}

func (s *SignalsStore) ListPendingDeliveries(ctx context.Context, organizationID string, asOf time.Time, limit int) ([]signals.Delivery, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT `+signalDeliveryColumns+` FROM signal_deliveries WHERE organization_id=$1 AND status='pending' AND (next_attempt_at IS NULL OR next_attempt_at <= $2) ORDER BY created_at,id LIMIT $3`, organizationID, asOf, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []signals.Delivery{}
	for rows.Next() {
		item, err := scanSignalDelivery(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *SignalsStore) UpdateDelivery(ctx context.Context, item signals.Delivery, expectedAttempts int) (signals.Delivery, error) {
	updated, err := scanSignalDelivery(s.database.QueryRowContext(ctx, `UPDATE signal_deliveries SET status=$3,attempts=$4,next_attempt_at=$5,last_error_code=NULLIF($6,''),updated_at=$7 WHERE organization_id=$1 AND id=$2 AND attempts=$8 RETURNING `+signalDeliveryColumns, item.OrganizationID, item.ID, item.Status, item.Attempts, item.NextAttemptAt, item.LastErrorCode, item.UpdatedAt, expectedAttempts))
	if errors.Is(err, sql.ErrNoRows) {
		return signals.Delivery{}, s.missingOrConflict(ctx, "signal_deliveries", item.OrganizationID, item.ID)
	}
	return updated, signalWriteError("update Signals delivery", err)
}

func (s *SignalsStore) getDelivery(ctx context.Context, organizationID, id string) (signals.Delivery, error) {
	item, err := scanSignalDelivery(s.database.QueryRowContext(ctx, `SELECT `+signalDeliveryColumns+` FROM signal_deliveries WHERE organization_id=$1 AND id=$2`, organizationID, id))
	return item, signalReadError("get Signals delivery", err)
}

type signalScanner interface{ Scan(...any) error }

func scanSignalRule(row signalScanner) (signals.Rule, error) {
	var item signals.Rule
	var period, scenario sql.NullString
	var thresholds []byte
	err := row.Scan(&item.OrganizationID, &item.ID, &item.Name, &item.Condition, &item.Severity, &item.Enabled, &thresholds, &period, &scenario, &item.CreatedBy, &item.Revision, &item.CreatedAt, &item.UpdatedAt)
	if err == nil {
		err = json.Unmarshal(thresholds, &item.ThresholdDays)
	}
	item.FiscalPeriod, item.Scenario = period.String, scenario.String
	return item, err
}
func scanSignalAlert(row signalScanner) (signals.Alert, error) {
	var item signals.Alert
	var due, ack, resolved sql.NullTime
	var kind, assigned, ackBy sql.NullString
	err := row.Scan(&item.OrganizationID, &item.ID, &item.RuleID, &item.Condition, &item.Severity, &item.Status, &item.Title, &item.Summary, &item.TargetType, &item.TargetID, &due, &item.ThresholdDays, &item.DeduplicationKey, &kind, &assigned, &ackBy, &ack, &item.FirstDetectedAt, &item.LastObservedAt, &resolved, &item.Revision)
	if due.Valid {
		item.DueAt = &due.Time
	}
	if ack.Valid {
		item.AcknowledgedAt = &ack.Time
	}
	if resolved.Valid {
		item.ResolvedAt = &resolved.Time
	}
	item.AssignedKind, item.AssignedID, item.AcknowledgedBy = kind.String, assigned.String, ackBy.String
	return item, err
}
func scanSignalSubscription(row signalScanner) (signals.Subscription, error) {
	var item signals.Subscription
	var ruleID sql.NullString
	err := row.Scan(&item.OrganizationID, &item.ID, &ruleID, &item.TargetKind, &item.TargetID, &item.Enabled, &item.CreatedBy, &item.Revision, &item.CreatedAt, &item.UpdatedAt)
	item.RuleID = ruleID.String
	return item, err
}
func scanSignalDelivery(row signalScanner) (signals.Delivery, error) {
	var item signals.Delivery
	var next sql.NullTime
	var code sql.NullString
	err := row.Scan(&item.OrganizationID, &item.ID, &item.AlertID, &item.SubscriptionID, &item.TargetKind, &item.TargetID, &item.Status, &item.Attempts, &next, &code, &item.CreatedAt, &item.UpdatedAt)
	if next.Valid {
		item.NextAttemptAt = &next.Time
	}
	item.LastErrorCode = code.String
	return item, err
}
func insertSignalHistory(ctx context.Context, tx *sql.Tx, item signals.AlertHistory) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO signal_alert_history (organization_id,id,alert_id,action,actor_id,occurred_at,revision) VALUES($1,$2,$3,$4,$5,$6,$7)`, item.OrganizationID, item.ID, item.AlertID, item.Action, item.ActorID, item.OccurredAt, item.Revision)
	return signalWriteError("record Signals alert history", err)
}
func (s *SignalsStore) missingOrConflict(ctx context.Context, table, organizationID, id string) error {
	var exists bool
	if err := s.database.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE organization_id=$1 AND id=$2)`, organizationID, id).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return signals.ErrConflict
	}
	return signals.ErrNotFound
}
func signalReadError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return signals.ErrNotFound
	}
	return fmt.Errorf("%s: %w", operation, err)
}
func signalWriteError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23505":
			return signals.ErrConflict
		case "23503":
			return signals.ErrNotFound
		case "23502", "23514", "22P02":
			return signals.ErrInvalidInput
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
