package postgres

// Requirements: REQ-DIRECTORY-EXPANSION-002, REQ-DIRECTORY-EXPANSION-005, REQ-DIRECTORY-EXPANSION-008, REQ-EXCHANGE-001. Features: integrations.protocols, threads.relationships, migration.packages.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxlemke/stewardmesh/internal/directoryexpansion"
)

type DirectoryImportStore struct{ database *sql.DB }

var _ directoryexpansion.Store = (*DirectoryImportStore)(nil)
var _ directoryexpansion.GroupTargetStore = (*DirectoryImportStore)(nil)
var _ directoryexpansion.GroupExchangeStore = (*DirectoryImportStore)(nil)

func NewDirectoryImportStore(database *sql.DB) (*DirectoryImportStore, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &DirectoryImportStore{database: database}, nil
}

const managedGroupColumns = `id,organization_id,source_system_id,source_record_id,name,display_name,description,status,metadata,revision,created_at,updated_at`
const managedMembershipColumns = `id,organization_id,source_system_id,source_record_id,group_id,group_source_id,member_id,member_source_id,member_kind,member_display_name,status,metadata,revision,created_at,updated_at`

func (s *DirectoryImportStore) GetManagedGroup(ctx context.Context, organizationID, id string) (directoryexpansion.ManagedGroup, error) {
	return scanManagedGroup(s.database.QueryRowContext(ctx, `SELECT `+managedGroupColumns+` FROM directory_managed_groups WHERE organization_id=$1 AND id=$2`, organizationID, id))
}

func (s *DirectoryImportStore) GetManagedGroupBySource(ctx context.Context, organizationID, sourceSystemID, sourceRecordID string) (directoryexpansion.ManagedGroup, error) {
	return scanManagedGroup(s.database.QueryRowContext(ctx, `SELECT `+managedGroupColumns+` FROM directory_managed_groups
		WHERE organization_id=$1 AND source_system_id=$2 AND source_record_id=$3`, organizationID, sourceSystemID, sourceRecordID))
}

func (s *DirectoryImportStore) CreateManagedGroup(ctx context.Context, group directoryexpansion.ManagedGroup) (directoryexpansion.ManagedGroup, error) {
	metadata, err := marshalDirectoryMetadata(group.Metadata)
	if err != nil {
		return directoryexpansion.ManagedGroup{}, fmt.Errorf("encode managed group metadata: %w", err)
	}
	_, err = s.database.ExecContext(ctx, `INSERT INTO directory_managed_groups
		(id,organization_id,source_system_id,source_record_id,name,display_name,description,status,metadata,revision,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, group.ID, group.OrganizationID, group.SourceSystemID,
		group.SourceRecordID, group.Name, group.DisplayName, group.Description, group.Status, metadata, group.Revision, group.CreatedAt, group.UpdatedAt)
	if err != nil {
		return directoryexpansion.ManagedGroup{}, translateDirectoryWrite("create managed group", err)
	}
	return group, nil
}

func (s *DirectoryImportStore) ReconcileManagedGroup(ctx context.Context, group directoryexpansion.ManagedGroup, expectedRevision uint64) (directoryexpansion.ManagedGroup, error) {
	metadata, err := marshalDirectoryMetadata(group.Metadata)
	if err != nil {
		return directoryexpansion.ManagedGroup{}, fmt.Errorf("encode managed group metadata: %w", err)
	}
	result, err := s.database.ExecContext(ctx, `UPDATE directory_managed_groups SET name=$6,display_name=$7,description=$8,status=$9,
		metadata=$10,revision=$11,updated_at=$12 WHERE organization_id=$1 AND id=$2 AND source_system_id=$3 AND source_record_id=$4 AND revision=$5`,
		group.OrganizationID, group.ID, group.SourceSystemID, group.SourceRecordID, expectedRevision, group.Name, group.DisplayName,
		group.Description, group.Status, metadata, group.Revision, group.UpdatedAt)
	if err != nil {
		return directoryexpansion.ManagedGroup{}, translateDirectoryWrite("reconcile managed group", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return directoryexpansion.ManagedGroup{}, directoryexpansion.ErrConflict
	}
	return group, nil
}

func (s *DirectoryImportStore) DeleteManagedGroup(ctx context.Context, organizationID, id string, expectedRevision uint64) error {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin managed group delete: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM directory_managed_groups WHERE organization_id=$1 AND id=$2 AND revision=$3`, organizationID, id, expectedRevision)
	if err != nil {
		return translateDirectoryWrite("delete managed group", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return directoryexpansion.ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM directory_managed_memberships
		WHERE organization_id=$1 AND member_kind=$2 AND member_id=$3`, organizationID, directoryexpansion.MemberGroup, id); err != nil {
		return translateDirectoryWrite("delete nested managed group memberships", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit managed group delete: %w", err)
	}
	return nil
}

func (s *DirectoryImportStore) GetManagedMembership(ctx context.Context, organizationID, id string) (directoryexpansion.ManagedMembership, error) {
	return scanManagedMembership(s.database.QueryRowContext(ctx, `SELECT `+managedMembershipColumns+` FROM directory_managed_memberships WHERE organization_id=$1 AND id=$2`, organizationID, id))
}

func (s *DirectoryImportStore) GetManagedMembershipBySource(ctx context.Context, organizationID, sourceSystemID, sourceRecordID string) (directoryexpansion.ManagedMembership, error) {
	return scanManagedMembership(s.database.QueryRowContext(ctx, `SELECT `+managedMembershipColumns+` FROM directory_managed_memberships
		WHERE organization_id=$1 AND source_system_id=$2 AND source_record_id=$3`, organizationID, sourceSystemID, sourceRecordID))
}

func (s *DirectoryImportStore) CreateManagedMembership(ctx context.Context, membership directoryexpansion.ManagedMembership) (directoryexpansion.ManagedMembership, error) {
	metadata, err := marshalDirectoryMetadata(membership.Metadata)
	if err != nil {
		return directoryexpansion.ManagedMembership{}, fmt.Errorf("encode managed membership metadata: %w", err)
	}
	_, err = s.database.ExecContext(ctx, `INSERT INTO directory_managed_memberships
		(id,organization_id,source_system_id,source_record_id,group_id,group_source_id,member_id,member_source_id,member_kind,member_display_name,status,metadata,revision,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, membership.ID, membership.OrganizationID,
		membership.SourceSystemID, membership.SourceRecordID, membership.GroupID, membership.GroupSourceID, membership.MemberID,
		membership.MemberSourceID, membership.MemberKind, membership.MemberDisplayName, membership.Status, metadata,
		membership.Revision, membership.CreatedAt, membership.UpdatedAt)
	if err != nil {
		return directoryexpansion.ManagedMembership{}, translateDirectoryWrite("create managed membership", err)
	}
	return membership, nil
}

func (s *DirectoryImportStore) ReconcileManagedMembership(ctx context.Context, membership directoryexpansion.ManagedMembership, expectedRevision uint64) (directoryexpansion.ManagedMembership, error) {
	metadata, err := marshalDirectoryMetadata(membership.Metadata)
	if err != nil {
		return directoryexpansion.ManagedMembership{}, fmt.Errorf("encode managed membership metadata: %w", err)
	}
	result, err := s.database.ExecContext(ctx, `UPDATE directory_managed_memberships SET group_id=$6,group_source_id=$7,member_id=$8,
		member_source_id=$9,member_kind=$10,member_display_name=$11,status=$12,metadata=$13,revision=$14,updated_at=$15
		WHERE organization_id=$1 AND id=$2 AND source_system_id=$3 AND source_record_id=$4 AND revision=$5`, membership.OrganizationID,
		membership.ID, membership.SourceSystemID, membership.SourceRecordID, expectedRevision, membership.GroupID,
		membership.GroupSourceID, membership.MemberID, membership.MemberSourceID, membership.MemberKind,
		membership.MemberDisplayName, membership.Status, metadata, membership.Revision, membership.UpdatedAt)
	if err != nil {
		return directoryexpansion.ManagedMembership{}, translateDirectoryWrite("reconcile managed membership", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return directoryexpansion.ManagedMembership{}, directoryexpansion.ErrConflict
	}
	return membership, nil
}

func marshalDirectoryMetadata(metadata map[string]string) ([]byte, error) {
	if metadata == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(metadata)
}

func (s *DirectoryImportStore) DeleteManagedMembership(ctx context.Context, organizationID, id string, expectedRevision uint64) error {
	result, err := s.database.ExecContext(ctx, `DELETE FROM directory_managed_memberships WHERE organization_id=$1 AND id=$2 AND revision=$3`, organizationID, id, expectedRevision)
	if err != nil {
		return translateDirectoryWrite("delete managed membership", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return directoryexpansion.ErrConflict
	}
	return nil
}

func (s *DirectoryImportStore) ListManagedGroups(ctx context.Context, organizationID string) ([]directoryexpansion.ManagedGroup, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT `+managedGroupColumns+` FROM directory_managed_groups WHERE organization_id=$1 ORDER BY id`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list managed groups: %w", err)
	}
	defer rows.Close()
	groups := make([]directoryexpansion.ManagedGroup, 0)
	for rows.Next() {
		group, scanErr := scanManagedGroup(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate managed groups: %w", err)
	}
	return groups, nil
}

func (s *DirectoryImportStore) ListManagedMemberships(ctx context.Context, organizationID string) ([]directoryexpansion.ManagedMembership, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT `+managedMembershipColumns+` FROM directory_managed_memberships WHERE organization_id=$1 ORDER BY id`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list managed memberships: %w", err)
	}
	defer rows.Close()
	memberships := make([]directoryexpansion.ManagedMembership, 0)
	for rows.Next() {
		membership, scanErr := scanManagedMembership(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		memberships = append(memberships, membership)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate managed memberships: %w", err)
	}
	return memberships, nil
}

func (s *DirectoryImportStore) ExchangeSnapshot(ctx context.Context, organizationID string, maximum int) (directoryexpansion.ExchangeSnapshot, error) {
	if strings.TrimSpace(organizationID) == "" || maximum < 1 {
		return directoryexpansion.ExchangeSnapshot{}, directoryexpansion.ErrInvalidInput
	}
	tx, err := s.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return directoryexpansion.ExchangeSnapshot{}, fmt.Errorf("begin Directory Exchange snapshot: %w", err)
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM directory_managed_groups WHERE organization_id=$1) +
		(SELECT count(*) FROM directory_managed_memberships WHERE organization_id=$1)`, organizationID).Scan(&count); err != nil {
		return directoryexpansion.ExchangeSnapshot{}, fmt.Errorf("count Directory Exchange snapshot: %w", err)
	}
	if count > maximum {
		return directoryexpansion.ExchangeSnapshot{}, directoryexpansion.ErrTooLarge
	}
	result := directoryexpansion.ExchangeSnapshot{Groups: []directoryexpansion.ManagedGroup{}, Memberships: []directoryexpansion.ManagedMembership{}}
	groupRows, err := tx.QueryContext(ctx, `SELECT `+managedGroupColumns+` FROM directory_managed_groups WHERE organization_id=$1 ORDER BY id`, organizationID)
	if err != nil {
		return directoryexpansion.ExchangeSnapshot{}, fmt.Errorf("list Directory Exchange groups: %w", err)
	}
	for groupRows.Next() {
		group, scanErr := scanManagedGroup(groupRows)
		if scanErr != nil {
			groupRows.Close()
			return directoryexpansion.ExchangeSnapshot{}, scanErr
		}
		result.Groups = append(result.Groups, group)
	}
	if err := groupRows.Err(); err != nil {
		groupRows.Close()
		return directoryexpansion.ExchangeSnapshot{}, fmt.Errorf("iterate Directory Exchange groups: %w", err)
	}
	groupRows.Close()
	membershipRows, err := tx.QueryContext(ctx, `SELECT `+managedMembershipColumns+` FROM directory_managed_memberships WHERE organization_id=$1 ORDER BY id`, organizationID)
	if err != nil {
		return directoryexpansion.ExchangeSnapshot{}, fmt.Errorf("list Directory Exchange memberships: %w", err)
	}
	for membershipRows.Next() {
		membership, scanErr := scanManagedMembership(membershipRows)
		if scanErr != nil {
			membershipRows.Close()
			return directoryexpansion.ExchangeSnapshot{}, scanErr
		}
		result.Memberships = append(result.Memberships, membership)
	}
	if err := membershipRows.Err(); err != nil {
		membershipRows.Close()
		return directoryexpansion.ExchangeSnapshot{}, fmt.Errorf("iterate Directory Exchange memberships: %w", err)
	}
	membershipRows.Close()
	if err := tx.Commit(); err != nil {
		return directoryexpansion.ExchangeSnapshot{}, fmt.Errorf("commit Directory Exchange snapshot: %w", err)
	}
	return result, nil
}

func (s *DirectoryImportStore) ImportManagedGroup(ctx context.Context, candidate directoryexpansion.ManagedGroup) (directoryexpansion.ManagedGroup, bool, error) {
	metadata, err := marshalDirectoryMetadata(candidate.Metadata)
	if err != nil {
		return directoryexpansion.ManagedGroup{}, false, fmt.Errorf("encode imported managed group metadata: %w", err)
	}
	created, err := scanManagedGroup(s.database.QueryRowContext(ctx, `INSERT INTO directory_managed_groups
		(id,organization_id,source_system_id,source_record_id,name,display_name,description,status,metadata,revision,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (organization_id,id) DO NOTHING RETURNING `+managedGroupColumns, candidate.ID, candidate.OrganizationID, candidate.SourceSystemID,
		candidate.SourceRecordID, candidate.Name, candidate.DisplayName, candidate.Description, candidate.Status, metadata,
		candidate.Revision, candidate.CreatedAt, candidate.UpdatedAt))
	if err == nil {
		return created, true, nil
	}
	if !errors.Is(err, directoryexpansion.ErrNotFound) {
		// A different record may already own this provider/source tuple. That
		// conflict is not an idempotent replay of the requested resource ID.
		return directoryexpansion.ManagedGroup{}, false, translateDirectoryWrite("import managed group", err)
	}
	existing, readErr := s.GetManagedGroup(ctx, candidate.OrganizationID, candidate.ID)
	if readErr != nil {
		return directoryexpansion.ManagedGroup{}, false, readErr
	}
	return existing, false, nil
}

func (s *DirectoryImportStore) ImportManagedMembership(ctx context.Context, candidate directoryexpansion.ManagedMembership) (directoryexpansion.ManagedMembership, bool, error) {
	metadata, err := marshalDirectoryMetadata(candidate.Metadata)
	if err != nil {
		return directoryexpansion.ManagedMembership{}, false, fmt.Errorf("encode imported managed membership metadata: %w", err)
	}
	tx, err := s.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return directoryexpansion.ManagedMembership{}, false, fmt.Errorf("begin managed membership import: %w", err)
	}
	defer tx.Rollback()
	var parentSourceSystemID, parentSourceRecordID string
	err = tx.QueryRowContext(ctx, `SELECT source_system_id,source_record_id FROM directory_managed_groups
		WHERE organization_id=$1 AND id=$2`, candidate.OrganizationID, candidate.GroupID).Scan(&parentSourceSystemID, &parentSourceRecordID)
	if errors.Is(err, sql.ErrNoRows) {
		return directoryexpansion.ManagedMembership{}, false, directoryexpansion.ErrReferenceMissing
	}
	if err != nil {
		return directoryexpansion.ManagedMembership{}, false, fmt.Errorf("validate managed membership parent: %w", err)
	}
	if parentSourceSystemID != candidate.SourceSystemID || parentSourceRecordID != candidate.GroupSourceID {
		return directoryexpansion.ManagedMembership{}, false, directoryexpansion.ErrConflict
	}
	if candidate.MemberKind == directoryexpansion.MemberGroup {
		var memberSourceSystemID, memberSourceRecordID string
		err = tx.QueryRowContext(ctx, `SELECT source_system_id,source_record_id FROM directory_managed_groups
			WHERE organization_id=$1 AND id=$2`, candidate.OrganizationID, candidate.MemberID).Scan(&memberSourceSystemID, &memberSourceRecordID)
		if errors.Is(err, sql.ErrNoRows) {
			return directoryexpansion.ManagedMembership{}, false, directoryexpansion.ErrReferenceMissing
		}
		if err != nil {
			return directoryexpansion.ManagedMembership{}, false, fmt.Errorf("validate managed membership member group: %w", err)
		}
		if memberSourceSystemID != candidate.SourceSystemID || memberSourceRecordID != candidate.MemberSourceID {
			return directoryexpansion.ManagedMembership{}, false, directoryexpansion.ErrConflict
		}
	}
	created, err := scanManagedMembership(tx.QueryRowContext(ctx, `INSERT INTO directory_managed_memberships
		(id,organization_id,source_system_id,source_record_id,group_id,group_source_id,member_id,member_source_id,member_kind,member_display_name,status,metadata,revision,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		ON CONFLICT (organization_id,id) DO NOTHING RETURNING `+managedMembershipColumns, candidate.ID, candidate.OrganizationID,
		candidate.SourceSystemID, candidate.SourceRecordID, candidate.GroupID, candidate.GroupSourceID, candidate.MemberID,
		candidate.MemberSourceID, candidate.MemberKind, candidate.MemberDisplayName, candidate.Status, metadata,
		candidate.Revision, candidate.CreatedAt, candidate.UpdatedAt))
	if err == nil {
		if err := tx.Commit(); err != nil {
			return directoryexpansion.ManagedMembership{}, false, fmt.Errorf("commit managed membership import: %w", err)
		}
		return created, true, nil
	}
	if !errors.Is(err, directoryexpansion.ErrNotFound) {
		return directoryexpansion.ManagedMembership{}, false, translateDirectoryWrite("import managed membership", err)
	}
	existing, readErr := scanManagedMembership(tx.QueryRowContext(ctx, `SELECT `+managedMembershipColumns+` FROM directory_managed_memberships WHERE organization_id=$1 AND id=$2`, candidate.OrganizationID, candidate.ID))
	if readErr != nil {
		return directoryexpansion.ManagedMembership{}, false, readErr
	}
	if err := tx.Commit(); err != nil {
		return directoryexpansion.ManagedMembership{}, false, fmt.Errorf("commit managed membership replay: %w", err)
	}
	return existing, false, nil
}

func (s *DirectoryImportStore) ListGraphManagedGroups(ctx context.Context, organizationID string, filter directoryexpansion.ManagedGroupGraphQuery) ([]directoryexpansion.ManagedGroup, error) {
	if strings.TrimSpace(organizationID) == "" || !filter.Valid() {
		return nil, directoryexpansion.ErrInvalidInput
	}
	query := strings.Builder{}
	query.WriteString(`SELECT ` + managedGroupColumns + ` FROM directory_managed_groups WHERE organization_id=$1 AND status='active'`)
	arguments := []any{organizationID}
	if filter.LabelSearch != "" {
		arguments = append(arguments, strings.ToLower(filter.LabelSearch))
		query.WriteString(fmt.Sprintf(" AND strpos(lower(display_name), $%d) > 0", len(arguments)))
	}
	if len(filter.GroupIDs) > 0 {
		query.WriteString(" AND " + inPredicate("id", filter.GroupIDs, &arguments))
	}
	arguments = append(arguments, filter.Limit)
	query.WriteString(fmt.Sprintf(" ORDER BY lower(display_name), id LIMIT $%d", len(arguments)))
	rows, err := s.database.QueryContext(ctx, query.String(), arguments...)
	if err != nil {
		return nil, fmt.Errorf("list graph managed groups: %w", err)
	}
	defer rows.Close()
	groups := make([]directoryexpansion.ManagedGroup, 0)
	for rows.Next() {
		group, scanErr := scanManagedGroup(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate graph managed groups: %w", err)
	}
	return groups, nil
}

func (s *DirectoryImportStore) ListGraphManagedMemberships(ctx context.Context, organizationID string, filter directoryexpansion.ManagedMembershipGraphQuery) ([]directoryexpansion.ManagedMembership, error) {
	if strings.TrimSpace(organizationID) == "" || !filter.Valid() {
		return nil, directoryexpansion.ErrInvalidInput
	}
	query := strings.Builder{}
	query.WriteString(`SELECT ` + managedMembershipColumns + ` FROM directory_managed_memberships WHERE organization_id=$1 AND status='active'`)
	arguments := []any{organizationID}
	if filter.LabelSearch != "" {
		arguments = append(arguments, strings.ToLower(filter.LabelSearch))
		query.WriteString(fmt.Sprintf(" AND strpos(lower(member_display_name), $%d) > 0", len(arguments)))
	}
	selectors := make([]string, 0, 2)
	if len(filter.GroupIDs) > 0 {
		selectors = append(selectors, inPredicate("group_id", filter.GroupIDs, &arguments))
	}
	if len(filter.MemberIDs) > 0 {
		selectors = append(selectors, inPredicate("member_id", filter.MemberIDs, &arguments))
	}
	if len(selectors) > 0 {
		query.WriteString(" AND (" + strings.Join(selectors, " OR ") + ")")
	}
	arguments = append(arguments, filter.Limit)
	query.WriteString(fmt.Sprintf(" ORDER BY lower(member_display_name), id LIMIT $%d", len(arguments)))
	rows, err := s.database.QueryContext(ctx, query.String(), arguments...)
	if err != nil {
		return nil, fmt.Errorf("list graph managed memberships: %w", err)
	}
	defer rows.Close()
	memberships := make([]directoryexpansion.ManagedMembership, 0)
	for rows.Next() {
		membership, scanErr := scanManagedMembership(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		memberships = append(memberships, membership)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate graph managed memberships: %w", err)
	}
	return memberships, nil
}

const directoryBatchColumns = `id, organization_id, source_system_id, provider, config_revision, status,
	complete_snapshot, created_count, updated_count, unchanged_count, deactivated_count, conflict_count, error_count,
	lease_token, lease_expires_at, created_at, updated_at, completed_at`

const directoryItemColumns = `id, organization_id, batch_id, ordinal, record, target_id, expected_revision,
	source_digest, observed_target_digest, planned_target_digest, action, outcome, failure_class, retryable, error_message, updated_at`

const directoryAttemptColumns = `id, organization_id, batch_id, operation, idempotency_hash, request_fingerprint,
	attempt_number, status, failure_class, retryable, error_message, actor_id, correlation_id, result, started_at, completed_at`

func (s *DirectoryImportStore) FindAttempt(ctx context.Context, organizationID string, operation directoryexpansion.Operation, hash string) (directoryexpansion.Attempt, error) {
	return scanDirectoryAttempt(s.database.QueryRowContext(ctx, `SELECT `+directoryAttemptColumns+`
		FROM directory_import_attempts WHERE organization_id=$1 AND operation=$2 AND idempotency_hash=$3`, organizationID, operation, hash))
}

func (s *DirectoryImportStore) CreatePreview(ctx context.Context, batch directoryexpansion.Batch, items []directoryexpansion.Item, attempt directoryexpansion.Attempt) (directoryexpansion.OperationResult, bool, error) {
	existing, findErr := s.FindAttempt(ctx, batch.OrganizationID, attempt.Operation, attempt.IdempotencyHash)
	if findErr == nil {
		if existing.RequestFingerprint != attempt.RequestFingerprint {
			return directoryexpansion.OperationResult{}, false, directoryexpansion.ErrConflict
		}
		if existing.Result == nil {
			return directoryexpansion.OperationResult{}, false, directoryexpansion.ErrBusy
		}
		return *existing.Result, true, nil
	}
	if !errors.Is(findErr, directoryexpansion.ErrNotFound) {
		return directoryexpansion.OperationResult{}, false, findErr
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return directoryexpansion.OperationResult{}, false, fmt.Errorf("begin directory preview: %w", err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO directory_import_batches
		(id,organization_id,provider,dry_run,status,created_count,updated_count,conflict_count,error_count,created_at,
		 source_system_id,config_revision,complete_snapshot,unchanged_count,deactivated_count,updated_at,completed_at)
		VALUES ($1,$2,$3,TRUE,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		batch.ID, batch.OrganizationID, batch.Provider, batch.Status, batch.Counts.Created, batch.Counts.Updated,
		batch.Counts.Conflicts, batch.Counts.Failed, batch.CreatedAt, batch.SourceSystemID, batch.ConfigRevision,
		batch.CompleteSnapshot, batch.Counts.Unchanged, batch.Counts.Deactivated, batch.UpdatedAt, batch.CompletedAt)
	if err != nil {
		return directoryexpansion.OperationResult{}, false, translateDirectoryWrite("create preview batch", err)
	}
	resultJSON, err := json.Marshal(attempt.Result)
	if err != nil {
		return directoryexpansion.OperationResult{}, false, fmt.Errorf("encode preview result: %w", err)
	}
	inserted, err := tx.ExecContext(ctx, `INSERT INTO directory_import_attempts
		(organization_id,batch_id,id,operation,idempotency_hash,request_fingerprint,attempt_number,status,
		 failure_class,retryable,error_message,actor_id,correlation_id,result,started_at,completed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT (organization_id,operation,idempotency_hash) DO NOTHING`,
		attempt.OrganizationID, attempt.BatchID, attempt.ID, attempt.Operation, attempt.IdempotencyHash,
		attempt.RequestFingerprint, attempt.Number, attempt.Status, attempt.FailureClass, attempt.Retryable,
		attempt.ErrorMessage, attempt.ActorID, attempt.CorrelationID, resultJSON, attempt.StartedAt, attempt.CompletedAt)
	if err != nil {
		return directoryexpansion.OperationResult{}, false, translateDirectoryWrite("create preview attempt", err)
	}
	rows, err := inserted.RowsAffected()
	if err != nil {
		return directoryexpansion.OperationResult{}, false, err
	}
	if rows == 0 {
		_ = tx.Rollback()
		existing, findErr := s.FindAttempt(ctx, batch.OrganizationID, attempt.Operation, attempt.IdempotencyHash)
		if findErr != nil {
			return directoryexpansion.OperationResult{}, false, findErr
		}
		if existing.RequestFingerprint != attempt.RequestFingerprint {
			return directoryexpansion.OperationResult{}, false, directoryexpansion.ErrConflict
		}
		if existing.Result == nil {
			return directoryexpansion.OperationResult{}, false, directoryexpansion.ErrBusy
		}
		return *existing.Result, true, nil
	}
	for _, item := range items {
		if err := insertDirectoryItem(ctx, tx, item); err != nil {
			return directoryexpansion.OperationResult{}, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return directoryexpansion.OperationResult{}, false, fmt.Errorf("commit directory preview: %w", err)
	}
	return directoryexpansion.OperationResult{Batch: batch}, false, nil
}

func (s *DirectoryImportStore) GetBatch(ctx context.Context, organizationID, batchID string) (directoryexpansion.BatchDetail, error) {
	batch, err := scanDirectoryBatch(s.database.QueryRowContext(ctx, `SELECT `+directoryBatchColumns+` FROM directory_import_batches WHERE organization_id=$1 AND id=$2`, organizationID, batchID))
	if err != nil {
		return directoryexpansion.BatchDetail{}, err
	}
	items, err := s.listItems(ctx, organizationID, batchID)
	if err != nil {
		return directoryexpansion.BatchDetail{}, err
	}
	attempts, err := s.listAttempts(ctx, organizationID, batchID)
	if err != nil {
		return directoryexpansion.BatchDetail{}, err
	}
	return directoryexpansion.BatchDetail{Batch: batch, Items: items, Attempts: attempts}, nil
}

func (s *DirectoryImportStore) ListBatches(ctx context.Context, organizationID string, query directoryexpansion.ListQuery) (directoryexpansion.BatchPage, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT `+directoryBatchColumns+` FROM directory_import_batches candidate
		WHERE organization_id=$1 AND ($2='' OR (created_at,id) < (
			SELECT created_at,id FROM directory_import_batches WHERE organization_id=$1 AND id=$2))
		ORDER BY created_at DESC,id DESC LIMIT $3`, organizationID, query.Cursor, query.Limit+1)
	if err != nil {
		return directoryexpansion.BatchPage{}, fmt.Errorf("list directory batches: %w", err)
	}
	defer rows.Close()
	batches := make([]directoryexpansion.Batch, 0, query.Limit+1)
	for rows.Next() {
		batch, scanErr := scanDirectoryBatch(rows)
		if scanErr != nil {
			return directoryexpansion.BatchPage{}, scanErr
		}
		batches = append(batches, batch)
	}
	if err := rows.Err(); err != nil {
		return directoryexpansion.BatchPage{}, fmt.Errorf("iterate directory batches: %w", err)
	}
	page := directoryexpansion.BatchPage{Batches: batches}
	if len(batches) > query.Limit {
		page.Batches = batches[:query.Limit]
		page.NextCursor = page.Batches[len(page.Batches)-1].ID
	}
	return page, nil
}

func (s *DirectoryImportStore) ListMappings(ctx context.Context, organizationID, sourceSystemID string) ([]directoryexpansion.Mapping, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT organization_id,source_system_id,provider,source_record_id,kind,target_id,
		source_digest,applied_target_digest,last_record,active,last_seen_batch_id,last_applied_batch_id,updated_at
		FROM directory_import_mappings WHERE organization_id=$1 AND source_system_id=$2 ORDER BY source_record_id`, organizationID, sourceSystemID)
	if err != nil {
		return nil, fmt.Errorf("list directory mappings: %w", err)
	}
	defer rows.Close()
	result := make([]directoryexpansion.Mapping, 0)
	for rows.Next() {
		mapping, scanErr := scanDirectoryMapping(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, mapping)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate directory mappings: %w", err)
	}
	return result, nil
}

func (s *DirectoryImportStore) BeginOperation(ctx context.Context, organizationID, batchID string, attempt directoryexpansion.Attempt, leaseToken string, leaseStartedAt, leaseUntil time.Time) (directoryexpansion.BatchDetail, *directoryexpansion.OperationResult, error) {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return directoryexpansion.BatchDetail{}, nil, fmt.Errorf("begin directory operation: %w", err)
	}
	defer tx.Rollback()
	existingPending := false
	existing, err := scanDirectoryAttempt(tx.QueryRowContext(ctx, `SELECT `+directoryAttemptColumns+` FROM directory_import_attempts
		WHERE organization_id=$1 AND operation=$2 AND idempotency_hash=$3`, organizationID, attempt.Operation, attempt.IdempotencyHash))
	if err == nil {
		if existing.RequestFingerprint != attempt.RequestFingerprint {
			return directoryexpansion.BatchDetail{}, nil, directoryexpansion.ErrConflict
		}
		if existing.Result != nil {
			result := *existing.Result
			return directoryexpansion.BatchDetail{}, &result, nil
		}
		existingPending = true
	}
	if err != nil && !errors.Is(err, directoryexpansion.ErrNotFound) {
		return directoryexpansion.BatchDetail{}, nil, err
	}
	batch, err := scanDirectoryBatch(tx.QueryRowContext(ctx, `SELECT `+directoryBatchColumns+` FROM directory_import_batches
		WHERE organization_id=$1 AND id=$2 FOR UPDATE`, organizationID, batchID))
	if err != nil {
		return directoryexpansion.BatchDetail{}, nil, err
	}
	if batch.LeaseToken != "" && batch.LeaseExpiresAt != nil && batch.LeaseExpiresAt.After(leaseStartedAt) {
		return directoryexpansion.BatchDetail{}, nil, directoryexpansion.ErrBusy
	}
	if !existingPending {
		_, err = tx.ExecContext(ctx, `INSERT INTO directory_import_attempts
		(organization_id,batch_id,id,operation,idempotency_hash,request_fingerprint,attempt_number,status,
		 failure_class,retryable,error_message,actor_id,correlation_id,started_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, attempt.OrganizationID, attempt.BatchID,
			attempt.ID, attempt.Operation, attempt.IdempotencyHash, attempt.RequestFingerprint, attempt.Number, attempt.Status,
			attempt.FailureClass, attempt.Retryable, attempt.ErrorMessage, attempt.ActorID, attempt.CorrelationID, attempt.StartedAt)
		if err != nil {
			return directoryexpansion.BatchDetail{}, nil, translateDirectoryWrite("create directory attempt", err)
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE directory_import_batches SET status=$4,lease_token=$3,lease_expires_at=$5,updated_at=$6,completed_at=NULL
		WHERE organization_id=$1 AND id=$2`, organizationID, batchID, leaseToken, directoryexpansion.BatchApplying, leaseUntil, leaseStartedAt)
	if err != nil {
		return directoryexpansion.BatchDetail{}, nil, fmt.Errorf("lease directory batch: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return directoryexpansion.BatchDetail{}, nil, fmt.Errorf("commit directory lease: %w", err)
	}
	detail, err := s.GetBatch(ctx, organizationID, batchID)
	return detail, nil, err
}

func (s *DirectoryImportStore) SaveItem(ctx context.Context, organizationID, batchID, leaseToken string, item directoryexpansion.Item, mapping *directoryexpansion.Mapping) error {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin directory item result: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE directory_import_items SET outcome=$5,failure_class=$6,retryable=$7,error_message=$8,updated_at=$9
		WHERE organization_id=$1 AND batch_id=$2 AND id=$3 AND EXISTS (
			SELECT 1 FROM directory_import_batches WHERE organization_id=$1 AND id=$2 AND lease_token=$4)`,
		organizationID, batchID, item.ID, leaseToken, item.Outcome, item.FailureClass, item.Retryable, item.ErrorMessage, item.UpdatedAt)
	if err != nil {
		return fmt.Errorf("save directory item result: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return directoryexpansion.ErrLeaseLost
	}
	if mapping != nil {
		recordJSON, marshalErr := json.Marshal(mapping.LastRecord)
		if marshalErr != nil {
			return fmt.Errorf("encode directory mapping record: %w", marshalErr)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO directory_import_mappings
			(organization_id,source_system_id,provider,source_record_id,kind,target_id,source_digest,applied_target_digest,
			 last_record,active,last_seen_batch_id,last_applied_batch_id,updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
			ON CONFLICT (organization_id,source_system_id,source_record_id) DO UPDATE SET
			provider=EXCLUDED.provider,kind=EXCLUDED.kind,target_id=EXCLUDED.target_id,source_digest=EXCLUDED.source_digest,
			applied_target_digest=EXCLUDED.applied_target_digest,last_record=EXCLUDED.last_record,active=EXCLUDED.active,
			last_seen_batch_id=EXCLUDED.last_seen_batch_id,last_applied_batch_id=EXCLUDED.last_applied_batch_id,updated_at=EXCLUDED.updated_at`,
			mapping.OrganizationID, mapping.SourceSystemID, mapping.Provider, mapping.SourceRecordID, mapping.Kind, mapping.TargetID,
			mapping.SourceDigest, mapping.AppliedTargetDigest, recordJSON, mapping.Active, mapping.LastSeenBatchID,
			mapping.LastAppliedBatchID, mapping.UpdatedAt)
		if err != nil {
			return translateDirectoryWrite("save directory mapping", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit directory item result: %w", err)
	}
	return nil
}

func (s *DirectoryImportStore) SavePlan(ctx context.Context, organizationID, batchID, leaseToken string, completeSnapshot bool, items []directoryexpansion.Item) error {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin directory preview recovery: %w", err)
	}
	defer tx.Rollback()
	var validLease bool
	if err := tx.QueryRowContext(ctx, `SELECT lease_token = $3 FROM directory_import_batches WHERE organization_id=$1 AND id=$2 FOR UPDATE`, organizationID, batchID, leaseToken).Scan(&validLease); errors.Is(err, sql.ErrNoRows) {
		return directoryexpansion.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("lock directory preview recovery: %w", err)
	}
	if !validLease {
		return directoryexpansion.ErrLeaseLost
	}
	var itemCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM directory_import_items WHERE organization_id=$1 AND batch_id=$2`, organizationID, batchID).Scan(&itemCount); err != nil {
		return fmt.Errorf("inspect directory preview recovery: %w", err)
	}
	if itemCount != 0 {
		return directoryexpansion.ErrConflict
	}
	for _, item := range items {
		if err := insertDirectoryItem(ctx, tx, item); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE directory_import_batches SET complete_snapshot=$4 WHERE organization_id=$1 AND id=$2 AND lease_token=$3`, organizationID, batchID, leaseToken, completeSnapshot); err != nil {
		return fmt.Errorf("save recovered preview completeness: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit directory preview recovery: %w", err)
	}
	return nil
}

func (s *DirectoryImportStore) FinishOperation(ctx context.Context, organizationID, batchID, leaseToken string, attempt directoryexpansion.Attempt, result directoryexpansion.OperationResult) error {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin finish directory operation: %w", err)
	}
	defer tx.Rollback()
	updated, err := tx.ExecContext(ctx, `UPDATE directory_import_batches SET status=$4,created_count=$5,updated_count=$6,
		unchanged_count=$7,deactivated_count=$8,conflict_count=$9,error_count=$10,lease_token='',lease_expires_at=NULL,
		updated_at=$11,completed_at=$12,complete_snapshot=$13 WHERE organization_id=$1 AND id=$2 AND lease_token=$3`, organizationID, batchID,
		leaseToken, result.Batch.Status, result.Batch.Counts.Created, result.Batch.Counts.Updated, result.Batch.Counts.Unchanged,
		result.Batch.Counts.Deactivated, result.Batch.Counts.Conflicts, result.Batch.Counts.Failed, result.Batch.UpdatedAt,
		result.Batch.CompletedAt, result.Batch.CompleteSnapshot)
	if err != nil {
		return fmt.Errorf("finish directory batch: %w", err)
	}
	rows, _ := updated.RowsAffected()
	if rows == 0 {
		return directoryexpansion.ErrLeaseLost
	}
	result.Batch.LeaseToken, result.Batch.LeaseExpiresAt = "", nil
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode directory operation result: %w", err)
	}
	updated, err = tx.ExecContext(ctx, `UPDATE directory_import_attempts SET status=$4,failure_class=$5,retryable=$6,
		error_message=$7,result=$8,completed_at=$9 WHERE organization_id=$1 AND batch_id=$2 AND id=$3`,
		organizationID, batchID, attempt.ID, attempt.Status, attempt.FailureClass, attempt.Retryable,
		attempt.ErrorMessage, resultJSON, attempt.CompletedAt)
	if err != nil {
		return fmt.Errorf("finish directory attempt: %w", err)
	}
	rows, _ = updated.RowsAffected()
	if rows == 0 {
		return directoryexpansion.ErrLeaseLost
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit directory operation: %w", err)
	}
	return nil
}

func insertDirectoryItem(ctx context.Context, tx *sql.Tx, item directoryexpansion.Item) error {
	recordJSON, err := json.Marshal(item.Record)
	if err != nil {
		return fmt.Errorf("encode directory item: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO directory_import_items
		(organization_id,batch_id,id,ordinal,source_record_id,record,target_id,expected_revision,source_digest,
		 observed_target_digest,planned_target_digest,action,outcome,failure_class,retryable,error_message,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`, item.OrganizationID, item.BatchID,
		item.ID, item.Ordinal, item.Record.SourceRecordID, recordJSON, item.TargetID, item.ExpectedRevision, item.SourceDigest,
		item.ObservedTargetDigest, item.PlannedTargetDigest, item.Action, item.Outcome, item.FailureClass,
		item.Retryable, item.ErrorMessage, item.UpdatedAt)
	return translateDirectoryWrite("create directory item", err)
}

func (s *DirectoryImportStore) listItems(ctx context.Context, organizationID, batchID string) ([]directoryexpansion.Item, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT `+directoryItemColumns+` FROM directory_import_items WHERE organization_id=$1 AND batch_id=$2 ORDER BY ordinal`, organizationID, batchID)
	if err != nil {
		return nil, fmt.Errorf("list directory items: %w", err)
	}
	defer rows.Close()
	result := make([]directoryexpansion.Item, 0)
	for rows.Next() {
		item, scanErr := scanDirectoryItem(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate directory items: %w", err)
	}
	return result, nil
}

func (s *DirectoryImportStore) listAttempts(ctx context.Context, organizationID, batchID string) ([]directoryexpansion.Attempt, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT `+directoryAttemptColumns+` FROM directory_import_attempts WHERE organization_id=$1 AND batch_id=$2 ORDER BY attempt_number`, organizationID, batchID)
	if err != nil {
		return nil, fmt.Errorf("list directory attempts: %w", err)
	}
	defer rows.Close()
	result := make([]directoryexpansion.Attempt, 0)
	for rows.Next() {
		attempt, scanErr := scanDirectoryAttempt(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, attempt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate directory attempts: %w", err)
	}
	return result, nil
}

type directoryScanner interface{ Scan(...any) error }

func scanManagedGroup(row directoryScanner) (directoryexpansion.ManagedGroup, error) {
	var group directoryexpansion.ManagedGroup
	var metadata []byte
	err := row.Scan(&group.ID, &group.OrganizationID, &group.SourceSystemID, &group.SourceRecordID, &group.Name,
		&group.DisplayName, &group.Description, &group.Status, &metadata, &group.Revision, &group.CreatedAt, &group.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return directoryexpansion.ManagedGroup{}, directoryexpansion.ErrNotFound
	}
	if err != nil {
		return directoryexpansion.ManagedGroup{}, fmt.Errorf("scan managed group: %w", err)
	}
	if len(metadata) > 0 && string(metadata) != "null" {
		if err := json.Unmarshal(metadata, &group.Metadata); err != nil {
			return directoryexpansion.ManagedGroup{}, fmt.Errorf("decode managed group metadata: %w", err)
		}
	}
	return group, nil
}

func scanManagedMembership(row directoryScanner) (directoryexpansion.ManagedMembership, error) {
	var membership directoryexpansion.ManagedMembership
	var metadata []byte
	err := row.Scan(&membership.ID, &membership.OrganizationID, &membership.SourceSystemID, &membership.SourceRecordID,
		&membership.GroupID, &membership.GroupSourceID, &membership.MemberID, &membership.MemberSourceID,
		&membership.MemberKind, &membership.MemberDisplayName, &membership.Status, &metadata, &membership.Revision,
		&membership.CreatedAt, &membership.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return directoryexpansion.ManagedMembership{}, directoryexpansion.ErrNotFound
	}
	if err != nil {
		return directoryexpansion.ManagedMembership{}, fmt.Errorf("scan managed membership: %w", err)
	}
	if len(metadata) > 0 && string(metadata) != "null" {
		if err := json.Unmarshal(metadata, &membership.Metadata); err != nil {
			return directoryexpansion.ManagedMembership{}, fmt.Errorf("decode managed membership metadata: %w", err)
		}
	}
	return membership, nil
}

func scanDirectoryBatch(row directoryScanner) (directoryexpansion.Batch, error) {
	var batch directoryexpansion.Batch
	var leaseExpiresAt, completedAt sql.NullTime
	err := row.Scan(&batch.ID, &batch.OrganizationID, &batch.SourceSystemID, &batch.Provider, &batch.ConfigRevision, &batch.Status,
		&batch.CompleteSnapshot, &batch.Counts.Created, &batch.Counts.Updated, &batch.Counts.Unchanged, &batch.Counts.Deactivated,
		&batch.Counts.Conflicts, &batch.Counts.Failed, &batch.LeaseToken, &leaseExpiresAt, &batch.CreatedAt, &batch.UpdatedAt, &completedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return directoryexpansion.Batch{}, directoryexpansion.ErrNotFound
	}
	if err != nil {
		return directoryexpansion.Batch{}, fmt.Errorf("scan directory batch: %w", err)
	}
	if leaseExpiresAt.Valid {
		batch.LeaseExpiresAt = &leaseExpiresAt.Time
	}
	if completedAt.Valid {
		batch.CompletedAt = &completedAt.Time
	}
	return batch, nil
}

func scanDirectoryItem(row directoryScanner) (directoryexpansion.Item, error) {
	var item directoryexpansion.Item
	var recordJSON []byte
	err := row.Scan(&item.ID, &item.OrganizationID, &item.BatchID, &item.Ordinal, &recordJSON, &item.TargetID,
		&item.ExpectedRevision, &item.SourceDigest, &item.ObservedTargetDigest, &item.PlannedTargetDigest,
		&item.Action, &item.Outcome, &item.FailureClass, &item.Retryable, &item.ErrorMessage, &item.UpdatedAt)
	if err != nil {
		return directoryexpansion.Item{}, fmt.Errorf("scan directory item: %w", err)
	}
	if err := json.Unmarshal(recordJSON, &item.Record); err != nil {
		return directoryexpansion.Item{}, fmt.Errorf("decode directory item: %w", err)
	}
	return item, nil
}

func scanDirectoryAttempt(row directoryScanner) (directoryexpansion.Attempt, error) {
	var attempt directoryexpansion.Attempt
	var resultJSON []byte
	var completedAt sql.NullTime
	err := row.Scan(&attempt.ID, &attempt.OrganizationID, &attempt.BatchID, &attempt.Operation, &attempt.IdempotencyHash,
		&attempt.RequestFingerprint, &attempt.Number, &attempt.Status, &attempt.FailureClass, &attempt.Retryable,
		&attempt.ErrorMessage, &attempt.ActorID, &attempt.CorrelationID, &resultJSON, &attempt.StartedAt, &completedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return directoryexpansion.Attempt{}, directoryexpansion.ErrNotFound
	}
	if err != nil {
		return directoryexpansion.Attempt{}, fmt.Errorf("scan directory attempt: %w", err)
	}
	if completedAt.Valid {
		attempt.CompletedAt = &completedAt.Time
	}
	if len(resultJSON) > 0 {
		var result directoryexpansion.OperationResult
		if err := json.Unmarshal(resultJSON, &result); err != nil {
			return directoryexpansion.Attempt{}, fmt.Errorf("decode directory attempt result: %w", err)
		}
		attempt.Result = &result
	}
	return attempt, nil
}

func scanDirectoryMapping(row directoryScanner) (directoryexpansion.Mapping, error) {
	var mapping directoryexpansion.Mapping
	var recordJSON []byte
	err := row.Scan(&mapping.OrganizationID, &mapping.SourceSystemID, &mapping.Provider, &mapping.SourceRecordID,
		&mapping.Kind, &mapping.TargetID, &mapping.SourceDigest, &mapping.AppliedTargetDigest, &recordJSON,
		&mapping.Active, &mapping.LastSeenBatchID, &mapping.LastAppliedBatchID, &mapping.UpdatedAt)
	if err != nil {
		return directoryexpansion.Mapping{}, fmt.Errorf("scan directory mapping: %w", err)
	}
	if err := json.Unmarshal(recordJSON, &mapping.LastRecord); err != nil {
		return directoryexpansion.Mapping{}, fmt.Errorf("decode directory mapping: %w", err)
	}
	return mapping, nil
}

func translateDirectoryWrite(operation string, err error) error {
	if err == nil {
		return nil
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && (postgresError.Code == "23505" || postgresError.Code == "23514" || postgresError.Code == "23503") {
		return fmt.Errorf("%w: %s", directoryexpansion.ErrConflict, operation)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

var _ = time.Time{}
