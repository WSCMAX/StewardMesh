package postgres

// Requirement: REQ-THREADS-001. Feature: goals.tags.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxlemke/stewardmesh/internal/threads"
)

type ThreadsStore struct {
	database *sql.DB
}

var _ threads.Store = (*ThreadsStore)(nil)

func NewThreadsStore(database *sql.DB) (*ThreadsStore, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &ThreadsStore{database: database}, nil
}

func (s *ThreadsStore) Snapshot(ctx context.Context, organizationID string) (threads.Snapshot, error) {
	result := threads.Snapshot{Tags: []threads.Tag{}, Goals: []threads.Goal{}, TagRules: []threads.TagRule{}, GoalLinks: []threads.GoalLink{}}
	transaction, err := s.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return threads.Snapshot{}, fmt.Errorf("begin Threads snapshot: %w", err)
	}
	defer transaction.Rollback()
	tagRows, err := transaction.QueryContext(ctx, `
		SELECT organization_id, id, name, parent_id, inherit_by_default, revision, created_at, updated_at
		FROM threads_tags WHERE organization_id = $1 ORDER BY normalized_name, id
	`, organizationID)
	if err != nil {
		return threads.Snapshot{}, fmt.Errorf("snapshot Threads tags: %w", err)
	}
	for tagRows.Next() {
		item, scanErr := scanThreadsTag(tagRows)
		if scanErr != nil {
			tagRows.Close()
			return threads.Snapshot{}, fmt.Errorf("scan Threads tag snapshot: %w", scanErr)
		}
		result.Tags = append(result.Tags, item)
	}
	if err = tagRows.Err(); err != nil {
		tagRows.Close()
		return threads.Snapshot{}, fmt.Errorf("iterate Threads tag snapshot: %w", err)
	}
	tagRows.Close()
	goalRows, err := transaction.QueryContext(ctx, `
		SELECT organization_id, id, name, description, parent_id, revision, created_at, updated_at
		FROM threads_goals WHERE organization_id = $1 ORDER BY normalized_name, id
	`, organizationID)
	if err != nil {
		return threads.Snapshot{}, fmt.Errorf("snapshot Threads goals: %w", err)
	}
	for goalRows.Next() {
		item, scanErr := scanThreadsGoal(goalRows)
		if scanErr != nil {
			goalRows.Close()
			return threads.Snapshot{}, fmt.Errorf("scan Threads goal snapshot: %w", scanErr)
		}
		result.Goals = append(result.Goals, item)
	}
	if err = goalRows.Err(); err != nil {
		goalRows.Close()
		return threads.Snapshot{}, fmt.Errorf("iterate Threads goal snapshot: %w", err)
	}
	goalRows.Close()
	ruleRows, err := transaction.QueryContext(ctx, `
		SELECT organization_id, target_type, target_id, tag_id, mode, revision, updated_by, created_at, updated_at
		FROM threads_tag_rules WHERE organization_id = $1 ORDER BY target_type, target_id, tag_id
	`, organizationID)
	if err != nil {
		return threads.Snapshot{}, fmt.Errorf("snapshot Threads tag rules: %w", err)
	}
	for ruleRows.Next() {
		item, scanErr := scanThreadsTagRule(ruleRows)
		if scanErr != nil {
			ruleRows.Close()
			return threads.Snapshot{}, fmt.Errorf("scan Threads tag rule snapshot: %w", scanErr)
		}
		result.TagRules = append(result.TagRules, item)
	}
	if err = ruleRows.Err(); err != nil {
		ruleRows.Close()
		return threads.Snapshot{}, fmt.Errorf("iterate Threads tag rule snapshot: %w", err)
	}
	ruleRows.Close()
	linkRows, err := transaction.QueryContext(ctx, `
		SELECT organization_id, goal_id, target_type, target_id, created_by, created_at
		FROM threads_goal_links WHERE organization_id = $1 ORDER BY target_type, target_id, goal_id
	`, organizationID)
	if err != nil {
		return threads.Snapshot{}, fmt.Errorf("snapshot Threads goal links: %w", err)
	}
	for linkRows.Next() {
		item, scanErr := scanThreadsGoalLink(linkRows)
		if scanErr != nil {
			linkRows.Close()
			return threads.Snapshot{}, fmt.Errorf("scan Threads goal link snapshot: %w", scanErr)
		}
		result.GoalLinks = append(result.GoalLinks, item)
	}
	if err = linkRows.Err(); err != nil {
		linkRows.Close()
		return threads.Snapshot{}, fmt.Errorf("iterate Threads goal link snapshot: %w", err)
	}
	linkRows.Close()
	if err := transaction.Commit(); err != nil {
		return threads.Snapshot{}, fmt.Errorf("commit Threads snapshot: %w", err)
	}
	return result, nil
}

func (s *ThreadsStore) ListTags(ctx context.Context, organizationID string) ([]threads.Tag, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT organization_id, id, name, parent_id, inherit_by_default, revision, created_at, updated_at
		FROM threads_tags WHERE organization_id = $1 ORDER BY normalized_name, id
	`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list Threads tags: %w", err)
	}
	defer rows.Close()
	items := make([]threads.Tag, 0)
	for rows.Next() {
		tag, err := scanThreadsTag(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Threads tag: %w", err)
		}
		items = append(items, tag)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Threads tags: %w", err)
	}
	return items, nil
}

func (s *ThreadsStore) GetTag(ctx context.Context, organizationID, id string) (threads.Tag, error) {
	tag, err := scanThreadsTag(s.database.QueryRowContext(ctx, `
		SELECT organization_id, id, name, parent_id, inherit_by_default, revision, created_at, updated_at
		FROM threads_tags WHERE organization_id = $1 AND id = $2
	`, organizationID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return threads.Tag{}, threads.ErrNotFound
	}
	if err != nil {
		return threads.Tag{}, fmt.Errorf("get Threads tag: %w", err)
	}
	return tag, nil
}

func (s *ThreadsStore) CreateTag(ctx context.Context, tag threads.Tag) (threads.Tag, error) {
	created, err := scanThreadsTag(s.database.QueryRowContext(ctx, `
		INSERT INTO threads_tags (
			organization_id, id, name, normalized_name, parent_id, inherit_by_default,
			revision, created_at, updated_at
		) VALUES ($1, $2, $3, lower(btrim($3)), NULLIF($4, ''), $5, $6, $7, $8)
		RETURNING organization_id, id, name, parent_id, inherit_by_default, revision, created_at, updated_at
	`, tag.OrganizationID, tag.ID, tag.Name, tag.ParentID, tag.InheritByDefault, tag.Revision, tag.CreatedAt, tag.UpdatedAt))
	if err != nil {
		return threads.Tag{}, translateThreadsWriteError("create Threads tag", err)
	}
	return created, nil
}

func (s *ThreadsStore) UpdateTag(ctx context.Context, tag threads.Tag, expectedRevision int64) (threads.Tag, error) {
	updated, err := scanThreadsTag(s.database.QueryRowContext(ctx, `
		UPDATE threads_tags SET name = $3, normalized_name = lower(btrim($3)), parent_id = NULLIF($4, ''),
			inherit_by_default = $5, revision = revision + 1, updated_at = $6
		WHERE organization_id = $1 AND id = $2 AND revision = $7
		RETURNING organization_id, id, name, parent_id, inherit_by_default, revision, created_at, updated_at
	`, tag.OrganizationID, tag.ID, tag.Name, tag.ParentID, tag.InheritByDefault, tag.UpdatedAt, expectedRevision))
	if errors.Is(err, sql.ErrNoRows) {
		return threads.Tag{}, s.threadsConflictOrNotFound(ctx, "threads_tags", tag.OrganizationID, tag.ID)
	}
	if err != nil {
		return threads.Tag{}, translateThreadsWriteError("update Threads tag", err)
	}
	return updated, nil
}

func (s *ThreadsStore) ListGoals(ctx context.Context, organizationID string) ([]threads.Goal, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT organization_id, id, name, description, parent_id, revision, created_at, updated_at
		FROM threads_goals WHERE organization_id = $1 ORDER BY normalized_name, id
	`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list Threads goals: %w", err)
	}
	defer rows.Close()
	items := make([]threads.Goal, 0)
	for rows.Next() {
		goal, err := scanThreadsGoal(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Threads goal: %w", err)
		}
		items = append(items, goal)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Threads goals: %w", err)
	}
	return items, nil
}

func (s *ThreadsStore) GetGoal(ctx context.Context, organizationID, id string) (threads.Goal, error) {
	goal, err := scanThreadsGoal(s.database.QueryRowContext(ctx, `
		SELECT organization_id, id, name, description, parent_id, revision, created_at, updated_at
		FROM threads_goals WHERE organization_id = $1 AND id = $2
	`, organizationID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return threads.Goal{}, threads.ErrNotFound
	}
	if err != nil {
		return threads.Goal{}, fmt.Errorf("get Threads goal: %w", err)
	}
	return goal, nil
}

func (s *ThreadsStore) CreateGoal(ctx context.Context, goal threads.Goal) (threads.Goal, error) {
	created, err := scanThreadsGoal(s.database.QueryRowContext(ctx, `
		INSERT INTO threads_goals (
			organization_id, id, name, normalized_name, description, parent_id, revision, created_at, updated_at
		) VALUES ($1, $2, $3, lower(btrim($3)), $4, NULLIF($5, ''), $6, $7, $8)
		RETURNING organization_id, id, name, description, parent_id, revision, created_at, updated_at
	`, goal.OrganizationID, goal.ID, goal.Name, goal.Description, goal.ParentID, goal.Revision, goal.CreatedAt, goal.UpdatedAt))
	if err != nil {
		return threads.Goal{}, translateThreadsWriteError("create Threads goal", err)
	}
	return created, nil
}

func (s *ThreadsStore) UpdateGoal(ctx context.Context, goal threads.Goal, expectedRevision int64) (threads.Goal, error) {
	updated, err := scanThreadsGoal(s.database.QueryRowContext(ctx, `
		UPDATE threads_goals SET name = $3, normalized_name = lower(btrim($3)), description = $4,
			parent_id = NULLIF($5, ''), revision = revision + 1, updated_at = $6
		WHERE organization_id = $1 AND id = $2 AND revision = $7
		RETURNING organization_id, id, name, description, parent_id, revision, created_at, updated_at
	`, goal.OrganizationID, goal.ID, goal.Name, goal.Description, goal.ParentID, goal.UpdatedAt, expectedRevision))
	if errors.Is(err, sql.ErrNoRows) {
		return threads.Goal{}, s.threadsConflictOrNotFound(ctx, "threads_goals", goal.OrganizationID, goal.ID)
	}
	if err != nil {
		return threads.Goal{}, translateThreadsWriteError("update Threads goal", err)
	}
	return updated, nil
}

func (s *ThreadsStore) ListTagRules(ctx context.Context, organizationID string, targetType threads.TargetType, targetID string) ([]threads.TagRule, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT organization_id, target_type, target_id, tag_id, mode, revision, updated_by, created_at, updated_at
		FROM threads_tag_rules
		WHERE organization_id = $1 AND target_type = $2 AND target_id = $3
		ORDER BY tag_id
	`, organizationID, targetType, targetID)
	if err != nil {
		return nil, fmt.Errorf("list Threads tag rules: %w", err)
	}
	defer rows.Close()
	items := make([]threads.TagRule, 0)
	for rows.Next() {
		rule, err := scanThreadsTagRule(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Threads tag rule: %w", err)
		}
		items = append(items, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Threads tag rules: %w", err)
	}
	return items, nil
}

func (s *ThreadsStore) CreateTagRule(ctx context.Context, rule threads.TagRule) (threads.TagRule, error) {
	stored, err := scanThreadsTagRule(s.database.QueryRowContext(ctx, `
		INSERT INTO threads_tag_rules (
			organization_id, target_type, target_id, tag_id, mode, revision, updated_by, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING organization_id, target_type, target_id, tag_id, mode, revision, updated_by, created_at, updated_at
	`, rule.OrganizationID, rule.TargetType, rule.TargetID, rule.TagID, rule.Mode, rule.Revision, rule.UpdatedBy, rule.CreatedAt, rule.UpdatedAt))
	if err != nil {
		return threads.TagRule{}, translateThreadsWriteError("create Threads tag rule", err)
	}
	return stored, nil
}

func (s *ThreadsStore) PutTagRule(ctx context.Context, rule threads.TagRule, expectedRevision int64) (threads.TagRule, error) {
	var stored threads.TagRule
	var err error
	if expectedRevision == 0 {
		stored, err = scanThreadsTagRule(s.database.QueryRowContext(ctx, `
			INSERT INTO threads_tag_rules (
				organization_id, target_type, target_id, tag_id, mode, revision, updated_by, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, 1, $6, $7, $8)
			RETURNING organization_id, target_type, target_id, tag_id, mode, revision, updated_by, created_at, updated_at
		`, rule.OrganizationID, rule.TargetType, rule.TargetID, rule.TagID, rule.Mode, rule.UpdatedBy, rule.CreatedAt, rule.UpdatedAt))
	} else {
		stored, err = scanThreadsTagRule(s.database.QueryRowContext(ctx, `
			UPDATE threads_tag_rules SET mode = $5, revision = revision + 1, updated_by = $6, updated_at = $7
			WHERE organization_id = $1 AND target_type = $2 AND target_id = $3 AND tag_id = $4 AND revision = $8
			RETURNING organization_id, target_type, target_id, tag_id, mode, revision, updated_by, created_at, updated_at
		`, rule.OrganizationID, rule.TargetType, rule.TargetID, rule.TagID, rule.Mode, rule.UpdatedBy, rule.UpdatedAt, expectedRevision))
		if errors.Is(err, sql.ErrNoRows) {
			var exists bool
			checkErr := s.database.QueryRowContext(ctx, `SELECT EXISTS (
				SELECT 1 FROM threads_tag_rules WHERE organization_id = $1 AND target_type = $2 AND target_id = $3 AND tag_id = $4
			)`, rule.OrganizationID, rule.TargetType, rule.TargetID, rule.TagID).Scan(&exists)
			if checkErr != nil {
				return threads.TagRule{}, fmt.Errorf("check Threads tag rule conflict: %w", checkErr)
			}
			if !exists {
				return threads.TagRule{}, threads.ErrNotFound
			}
			return threads.TagRule{}, threads.ErrConflict
		}
	}
	if err != nil {
		return threads.TagRule{}, translateThreadsWriteError("put Threads tag rule", err)
	}
	return stored, nil
}

func (s *ThreadsStore) DeleteTagRule(ctx context.Context, organizationID string, targetType threads.TargetType, targetID, tagID string, expectedRevision int64) error {
	result, err := s.database.ExecContext(ctx, `
		DELETE FROM threads_tag_rules
		WHERE organization_id = $1 AND target_type = $2 AND target_id = $3 AND tag_id = $4 AND revision = $5
	`, organizationID, targetType, targetID, tagID, expectedRevision)
	if err != nil {
		return fmt.Errorf("delete Threads tag rule: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read Threads tag rule deletion: %w", err)
	}
	if affected == 1 {
		return nil
	}
	var exists bool
	if err := s.database.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM threads_tag_rules WHERE organization_id = $1 AND target_type = $2 AND target_id = $3 AND tag_id = $4
	)`, organizationID, targetType, targetID, tagID).Scan(&exists); err != nil {
		return fmt.Errorf("check Threads tag rule deletion: %w", err)
	}
	if exists {
		return threads.ErrConflict
	}
	return threads.ErrNotFound
}

func (s *ThreadsStore) ListGoalLinks(ctx context.Context, organizationID string, targetType threads.TargetType, targetID string) ([]threads.GoalLink, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT organization_id, goal_id, target_type, target_id, created_by, created_at
		FROM threads_goal_links
		WHERE organization_id = $1 AND target_type = $2 AND target_id = $3
		ORDER BY goal_id
	`, organizationID, targetType, targetID)
	if err != nil {
		return nil, fmt.Errorf("list Threads goal links: %w", err)
	}
	defer rows.Close()
	items := make([]threads.GoalLink, 0)
	for rows.Next() {
		link, err := scanThreadsGoalLink(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Threads goal link: %w", err)
		}
		items = append(items, link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Threads goal links: %w", err)
	}
	return items, nil
}

func (s *ThreadsStore) CreateGoalLink(ctx context.Context, link threads.GoalLink) (threads.GoalLink, bool, error) {
	created, err := scanThreadsGoalLink(s.database.QueryRowContext(ctx, `
		INSERT INTO threads_goal_links (organization_id, goal_id, target_type, target_id, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (organization_id, target_type, target_id, goal_id) DO NOTHING
		RETURNING organization_id, goal_id, target_type, target_id, created_by, created_at
	`, link.OrganizationID, link.GoalID, link.TargetType, link.TargetID, link.CreatedBy, link.CreatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		existing, getErr := scanThreadsGoalLink(s.database.QueryRowContext(ctx, `
			SELECT organization_id, goal_id, target_type, target_id, created_by, created_at
			FROM threads_goal_links WHERE organization_id = $1 AND target_type = $2 AND target_id = $3 AND goal_id = $4
		`, link.OrganizationID, link.TargetType, link.TargetID, link.GoalID))
		if getErr != nil {
			return threads.GoalLink{}, false, fmt.Errorf("get existing Threads goal link: %w", getErr)
		}
		return existing, false, nil
	}
	if err != nil {
		return threads.GoalLink{}, false, translateThreadsWriteError("create Threads goal link", err)
	}
	return created, true, nil
}

func (s *ThreadsStore) DeleteGoalLink(ctx context.Context, organizationID string, targetType threads.TargetType, targetID, goalID string) (bool, error) {
	result, err := s.database.ExecContext(ctx, `
		DELETE FROM threads_goal_links WHERE organization_id = $1 AND target_type = $2 AND target_id = $3 AND goal_id = $4
	`, organizationID, targetType, targetID, goalID)
	if err != nil {
		return false, fmt.Errorf("delete Threads goal link: %w", err)
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

type threadsScanner interface {
	Scan(dest ...any) error
}

func scanThreadsTag(scanner threadsScanner) (threads.Tag, error) {
	var tag threads.Tag
	var parentID sql.NullString
	err := scanner.Scan(&tag.OrganizationID, &tag.ID, &tag.Name, &parentID, &tag.InheritByDefault, &tag.Revision, &tag.CreatedAt, &tag.UpdatedAt)
	tag.ParentID = parentID.String
	return tag, err
}

func scanThreadsGoal(scanner threadsScanner) (threads.Goal, error) {
	var goal threads.Goal
	var parentID sql.NullString
	err := scanner.Scan(&goal.OrganizationID, &goal.ID, &goal.Name, &goal.Description, &parentID, &goal.Revision, &goal.CreatedAt, &goal.UpdatedAt)
	goal.ParentID = parentID.String
	return goal, err
}

func scanThreadsTagRule(scanner threadsScanner) (threads.TagRule, error) {
	var rule threads.TagRule
	err := scanner.Scan(&rule.OrganizationID, &rule.TargetType, &rule.TargetID, &rule.TagID, &rule.Mode,
		&rule.Revision, &rule.UpdatedBy, &rule.CreatedAt, &rule.UpdatedAt)
	return rule, err
}

func scanThreadsGoalLink(scanner threadsScanner) (threads.GoalLink, error) {
	var link threads.GoalLink
	err := scanner.Scan(&link.OrganizationID, &link.GoalID, &link.TargetType, &link.TargetID, &link.CreatedBy, &link.CreatedAt)
	return link, err
}

func (s *ThreadsStore) threadsConflictOrNotFound(ctx context.Context, table, organizationID, id string) error {
	query := "SELECT EXISTS (SELECT 1 FROM " + table + " WHERE organization_id = $1 AND id = $2)"
	var exists bool
	if err := s.database.QueryRowContext(ctx, query, organizationID, id).Scan(&exists); err != nil {
		return fmt.Errorf("check Threads update conflict: %w", err)
	}
	if exists {
		return threads.ErrConflict
	}
	return threads.ErrNotFound
}

func translateThreadsWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503":
			return fmt.Errorf("%s: %w", operation, threads.ErrNotFound)
		case "23505", "23514":
			return fmt.Errorf("%s: %w", operation, threads.ErrConflict)
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
