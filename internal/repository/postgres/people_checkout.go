package postgres

// Requirement: REQ-PEOPLE-001. Feature: identity.directory.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"github.com/maxlemke/stewardmesh/internal/people"
)

func (s *PeopleStore) CreateCheckoutGroup(ctx context.Context, group people.CheckoutGroup) (people.CheckoutGroup, error) {
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return people.CheckoutGroup{}, fmt.Errorf("begin checkout group: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO people_checkout_groups (
			id, organization_id, name, description, status, revision, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, group.ID, group.OrganizationID, group.Name, group.Description, group.Status, group.Revision, group.CreatedAt, group.UpdatedAt); err != nil {
		return people.CheckoutGroup{}, mapPeopleStoreError("create checkout group", err)
	}
	for _, memberID := range group.MemberIDs {
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO people_checkout_group_members (organization_id, group_id, identity_id, created_at)
			VALUES ($1, $2, $3, $4)
		`, group.OrganizationID, group.ID, memberID, group.CreatedAt); err != nil {
			return people.CheckoutGroup{}, mapPeopleStoreError("create checkout group member", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return people.CheckoutGroup{}, fmt.Errorf("commit checkout group: %w", err)
	}
	return s.GetCheckoutGroup(ctx, group.OrganizationID, group.ID)
}

func (s *PeopleStore) GetCheckoutGroup(ctx context.Context, organizationID, id string) (people.CheckoutGroup, error) {
	group, err := scanCheckoutGroup(s.database.QueryRowContext(ctx, `
		SELECT id, organization_id, name, description, status, revision, created_at, updated_at
		FROM people_checkout_groups
		WHERE organization_id = $1 AND id = $2
	`, organizationID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return people.CheckoutGroup{}, people.ErrNotFound
	}
	if err != nil {
		return people.CheckoutGroup{}, fmt.Errorf("get checkout group: %w", err)
	}
	members, err := s.listCheckoutGroupMembers(ctx, organizationID, id)
	if err != nil {
		return people.CheckoutGroup{}, err
	}
	group.MemberIDs = members
	return group, nil
}

func (s *PeopleStore) ListCheckoutGroups(ctx context.Context, organizationID string) ([]people.CheckoutGroup, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT id, organization_id, name, description, status, revision, created_at, updated_at
		FROM people_checkout_groups
		WHERE organization_id = $1
		ORDER BY name, id
	`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list checkout groups: %w", err)
	}
	defer rows.Close()
	result := make([]people.CheckoutGroup, 0)
	for rows.Next() {
		group, err := scanCheckoutGroup(rows)
		if err != nil {
			return nil, fmt.Errorf("scan checkout group: %w", err)
		}
		result = append(result, group)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate checkout groups: %w", err)
	}
	for index := range result {
		members, err := s.listCheckoutGroupMembers(ctx, organizationID, result[index].ID)
		if err != nil {
			return nil, err
		}
		result[index].MemberIDs = members
	}
	return result, nil
}

func (s *PeopleStore) AddCheckoutGroupMember(ctx context.Context, organizationID, groupID, identityID string) (people.CheckoutGroup, error) {
	if _, err := s.database.ExecContext(ctx, `
		INSERT INTO people_checkout_group_members (organization_id, group_id, identity_id, created_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (group_id, identity_id) DO NOTHING
	`, organizationID, groupID, identityID); err != nil {
		return people.CheckoutGroup{}, mapPeopleStoreError("add checkout group member", err)
	}
	if _, err := s.database.ExecContext(ctx, `
		UPDATE people_checkout_groups
		SET revision = revision + 1, updated_at = NOW()
		WHERE organization_id = $1 AND id = $2
	`, organizationID, groupID); err != nil {
		return people.CheckoutGroup{}, fmt.Errorf("touch checkout group: %w", err)
	}
	return s.GetCheckoutGroup(ctx, organizationID, groupID)
}

func (s *PeopleStore) listCheckoutGroupMembers(ctx context.Context, organizationID, groupID string) ([]string, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT identity_id FROM people_checkout_group_members
		WHERE organization_id = $1 AND group_id = $2
		ORDER BY identity_id
	`, organizationID, groupID)
	if err != nil {
		return nil, fmt.Errorf("list checkout group members: %w", err)
	}
	defer rows.Close()
	members := make([]string, 0)
	for rows.Next() {
		var identityID string
		if err := rows.Scan(&identityID); err != nil {
			return nil, fmt.Errorf("scan checkout group member: %w", err)
		}
		members = append(members, identityID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate checkout group members: %w", err)
	}
	sort.Strings(members)
	return members, nil
}

func (s *PeopleStore) CreateBulkCheckout(ctx context.Context, item people.BulkCheckout) (people.BulkCheckout, error) {
	row := s.database.QueryRowContext(ctx, `
		INSERT INTO people_bulk_checkouts (
			id, organization_id, assignee_kind, assignee_id, purpose, effective_from, due_at,
			event_summary, label_definition_id, label_value, requested_count, created_by, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id, organization_id, assignee_kind, assignee_id, purpose, effective_from, due_at,
		          event_summary, label_definition_id, label_value, requested_count, created_by, created_at
	`, item.ID, item.OrganizationID, item.AssigneeKind, item.AssigneeID, item.Purpose, item.EffectiveFrom, item.DueAt,
		item.EventSummary, item.LabelDefinitionID, item.LabelValue, item.RequestedCount, item.CreatedBy, item.CreatedAt)
	created, err := scanBulkCheckout(row)
	if err != nil {
		return people.BulkCheckout{}, mapPeopleStoreError("create bulk checkout", err)
	}
	return created, nil
}

func (s *PeopleStore) GetBulkCheckout(ctx context.Context, organizationID, id string) (people.BulkCheckout, error) {
	item, err := scanBulkCheckout(s.database.QueryRowContext(ctx, `
		SELECT id, organization_id, assignee_kind, assignee_id, purpose, effective_from, due_at,
		       event_summary, label_definition_id, label_value, requested_count, created_by, created_at
		FROM people_bulk_checkouts
		WHERE organization_id = $1 AND id = $2
	`, organizationID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return people.BulkCheckout{}, people.ErrNotFound
	}
	if err != nil {
		return people.BulkCheckout{}, fmt.Errorf("get bulk checkout: %w", err)
	}
	return item, nil
}

func (s *PeopleStore) ListBulkCheckouts(ctx context.Context, organizationID string) ([]people.BulkCheckout, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT id, organization_id, assignee_kind, assignee_id, purpose, effective_from, due_at,
		       event_summary, label_definition_id, label_value, requested_count, created_by, created_at
		FROM people_bulk_checkouts
		WHERE organization_id = $1
		ORDER BY created_at DESC, id DESC
	`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list bulk checkouts: %w", err)
	}
	defer rows.Close()
	result := make([]people.BulkCheckout, 0)
	for rows.Next() {
		item, err := scanBulkCheckout(rows)
		if err != nil {
			return nil, fmt.Errorf("scan bulk checkout: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bulk checkouts: %w", err)
	}
	return result, nil
}

func scanCheckoutGroup(row peopleRowScanner) (people.CheckoutGroup, error) {
	var group people.CheckoutGroup
	err := row.Scan(&group.ID, &group.OrganizationID, &group.Name, &group.Description,
		&group.Status, &group.Revision, &group.CreatedAt, &group.UpdatedAt)
	group.MemberIDs = []string{}
	return group, err
}

func scanBulkCheckout(row peopleRowScanner) (people.BulkCheckout, error) {
	var item people.BulkCheckout
	var dueAt sql.NullTime
	err := row.Scan(&item.ID, &item.OrganizationID, &item.AssigneeKind, &item.AssigneeID, &item.Purpose,
		&item.EffectiveFrom, &dueAt, &item.EventSummary, &item.LabelDefinitionID, &item.LabelValue,
		&item.RequestedCount, &item.CreatedBy, &item.CreatedAt)
	if dueAt.Valid {
		item.DueAt = &dueAt.Time
	}
	return item, err
}
