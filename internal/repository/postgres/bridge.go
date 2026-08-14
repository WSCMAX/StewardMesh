package postgres

// PostgreSQL Bridge adapter. Requirements: REQ-API-001, SEC-MCP-001, REQ-EXCHANGE-001.
// Features: integrations.protocols, migration.packages. GitHub: #9, #14.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxlemke/stewardmesh/internal/bridge"
)

type BridgeStore struct{ database *sql.DB }

func NewBridgeStore(database *sql.DB) (*BridgeStore, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &BridgeStore{database: database}, nil
}

const bridgeClientColumns = `organization_id,id,name,redirect_uris::text,array_to_json(allowed_scopes)::text,created_by,created_at,revoked_at`
const bridgeAuthorizationColumns = `organization_id,id,client_id,actor_id,redirect_uri,resource_uri,array_to_json(scopes)::text,oauth_state,code_challenge,created_at,expires_at,decided_at,approved`
const bridgeGrantColumns = `g.organization_id,g.id,g.client_id,c.name,g.actor_id,g.resource_uri,array_to_json(g.scopes)::text,g.access_token_hash,g.refresh_token_hash,g.access_expires_at,g.refresh_expires_at,g.created_at,g.last_used_at,g.revoked_at`

func (s *BridgeStore) CreateClient(ctx context.Context, client bridge.Client) (bridge.Client, error) {
	redirects, err := json.Marshal(client.RedirectURIs)
	if err != nil {
		return bridge.Client{}, bridge.ErrInvalidInput
	}
	scopes := scopeStrings(client.AllowedScopes)
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return bridge.Client{}, fmt.Errorf("begin Bridge OAuth client creation: %w", err)
	}
	defer tx.Rollback()
	// Serialize the organization-local capacity check with an xact-scoped
	// advisory lock. A plain count in INSERT ... SELECT is racy when concurrent
	// registrations observe the same pre-insert total.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 1414))`, client.OrganizationID); err != nil {
		return bridge.Client{}, fmt.Errorf("lock Bridge OAuth client capacity: %w", err)
	}
	created, err := scanBridgeClient(tx.QueryRowContext(ctx, `INSERT INTO bridge_oauth_clients (organization_id,id,name,redirect_uris,allowed_scopes,created_by,created_at)
		SELECT $1,$2,$3,$4::jsonb,$5,$6,$7 WHERE (SELECT count(*) FROM bridge_oauth_clients WHERE organization_id=$1 AND revoked_at IS NULL) < $8
		RETURNING `+bridgeClientColumns, client.OrganizationID, client.ID, client.Name, string(redirects), scopes, client.CreatedBy, client.CreatedAt, bridge.MaximumClients))
	if errors.Is(err, sql.ErrNoRows) {
		return bridge.Client{}, bridge.ErrConflict
	}
	if err := bridgeWriteError("create Bridge OAuth client", err); err != nil {
		return bridge.Client{}, err
	}
	if err := tx.Commit(); err != nil {
		return bridge.Client{}, fmt.Errorf("commit Bridge OAuth client creation: %w", err)
	}
	return created, nil
}

func (s *BridgeStore) ListClients(ctx context.Context, organizationID string, page bridge.PageRequest) ([]bridge.Client, error) {
	if page.Limit < 1 || page.Limit > bridge.MaximumAdministrationPageSize {
		return nil, bridge.ErrInvalidInput
	}
	if page.Cursor != "" {
		var exists bool
		if err := s.database.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM bridge_oauth_clients WHERE organization_id=$1 AND id=$2)`, organizationID, page.Cursor).Scan(&exists); err != nil {
			return nil, fmt.Errorf("validate Bridge OAuth client cursor: %w", err)
		}
		if !exists {
			return nil, bridge.ErrInvalidInput
		}
	}
	rows, err := s.database.QueryContext(ctx, `SELECT `+bridgeClientColumns+` FROM bridge_oauth_clients
		WHERE organization_id=$1 AND ($2='' OR (lower(name),id) > (
			SELECT lower(cursor_client.name),cursor_client.id FROM bridge_oauth_clients cursor_client WHERE cursor_client.organization_id=$1 AND cursor_client.id=$2
		)) ORDER BY lower(name),id LIMIT $3`, organizationID, page.Cursor, page.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list Bridge OAuth clients: %w", err)
	}
	defer rows.Close()
	items := []bridge.Client{}
	for rows.Next() {
		item, err := scanBridgeClient(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Bridge OAuth client: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *BridgeStore) ListExchangeClients(ctx context.Context, organizationID string, limit int) ([]bridge.Client, error) {
	if limit < 1 {
		return nil, bridge.ErrInvalidInput
	}
	tx, err := s.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin Bridge Exchange client snapshot: %w", err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT `+bridgeClientColumns+` FROM bridge_oauth_clients WHERE organization_id=$1 ORDER BY id LIMIT $2`, organizationID, limit+1)
	if err != nil {
		return nil, fmt.Errorf("list Bridge Exchange clients: %w", err)
	}
	items := make([]bridge.Client, 0)
	for rows.Next() {
		item, scanErr := scanBridgeClient(rows)
		if scanErr != nil {
			rows.Close()
			return nil, fmt.Errorf("scan Bridge Exchange client: %w", scanErr)
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close Bridge Exchange client snapshot: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Bridge Exchange clients: %w", err)
	}
	if len(items) > limit {
		return nil, bridge.ErrTooLarge
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit Bridge Exchange client snapshot: %w", err)
	}
	return items, nil
}

func (s *BridgeStore) GetClient(ctx context.Context, organizationID, clientID string) (bridge.Client, error) {
	client, err := scanBridgeClient(s.database.QueryRowContext(ctx, `SELECT `+bridgeClientColumns+` FROM bridge_oauth_clients WHERE organization_id=$1 AND id=$2`, organizationID, clientID))
	return client, bridgeReadError("get Bridge OAuth client", err)
}

func (s *BridgeStore) ImportExchangeClient(ctx context.Context, client bridge.Client) (bridge.Client, bool, error) {
	redirects, err := json.Marshal(client.RedirectURIs)
	if err != nil {
		return bridge.Client{}, false, bridge.ErrInvalidInput
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return bridge.Client{}, false, fmt.Errorf("begin Bridge Exchange client import: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 1414))`, client.OrganizationID); err != nil {
		return bridge.Client{}, false, fmt.Errorf("lock Bridge Exchange client import: %w", err)
	}
	existing, err := scanBridgeClient(tx.QueryRowContext(ctx, `SELECT `+bridgeClientColumns+` FROM bridge_oauth_clients WHERE organization_id=$1 AND id=$2`, client.OrganizationID, client.ID))
	if err == nil {
		if !sameBridgeExchangeClient(existing, client) {
			return bridge.Client{}, false, bridge.ErrConflict
		}
		if err := tx.Commit(); err != nil {
			return bridge.Client{}, false, fmt.Errorf("commit Bridge Exchange client replay: %w", err)
		}
		return existing, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return bridge.Client{}, false, bridgeReadError("inspect Bridge Exchange client", err)
	}
	created, err := scanBridgeClient(tx.QueryRowContext(ctx, `INSERT INTO bridge_oauth_clients (organization_id,id,name,redirect_uris,allowed_scopes,created_by,created_at,revoked_at)
		SELECT $1,$2,$3,$4::jsonb,$5,$6,$7,$8 WHERE $8::timestamptz IS NOT NULL OR
			(SELECT count(*) FROM bridge_oauth_clients WHERE organization_id=$1 AND revoked_at IS NULL) < $9
		RETURNING `+bridgeClientColumns, client.OrganizationID, client.ID, client.Name, string(redirects), scopeStrings(client.AllowedScopes), client.CreatedBy, client.CreatedAt, client.RevokedAt, bridge.MaximumClients))
	if errors.Is(err, sql.ErrNoRows) {
		return bridge.Client{}, false, bridge.ErrConflict
	}
	if err := bridgeWriteError("import Bridge Exchange client", err); err != nil {
		return bridge.Client{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return bridge.Client{}, false, fmt.Errorf("commit Bridge Exchange client import: %w", err)
	}
	return created, true, nil
}

func sameBridgeExchangeClient(left, right bridge.Client) bool {
	if left.ID != right.ID || left.OrganizationID != right.OrganizationID || left.Name != right.Name || left.CreatedBy != right.CreatedBy ||
		!left.CreatedAt.Equal(right.CreatedAt) || len(left.RedirectURIs) != len(right.RedirectURIs) || len(left.AllowedScopes) != len(right.AllowedScopes) {
		return false
	}
	for index := range left.RedirectURIs {
		if left.RedirectURIs[index] != right.RedirectURIs[index] {
			return false
		}
	}
	for index := range left.AllowedScopes {
		if left.AllowedScopes[index] != right.AllowedScopes[index] {
			return false
		}
	}
	return left.RevokedAt == nil && right.RevokedAt == nil || left.RevokedAt != nil && right.RevokedAt != nil && left.RevokedAt.Equal(*right.RevokedAt)
}

func (s *BridgeStore) RevokeClient(ctx context.Context, organizationID, clientID string, revokedAt time.Time) (bridge.Client, error) {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return bridge.Client{}, err
	}
	defer tx.Rollback()
	client, err := scanBridgeClient(tx.QueryRowContext(ctx, `UPDATE bridge_oauth_clients SET revoked_at=COALESCE(revoked_at,$3) WHERE organization_id=$1 AND id=$2 RETURNING `+bridgeClientColumns, organizationID, clientID, revokedAt))
	if err != nil {
		return bridge.Client{}, bridgeReadError("revoke Bridge OAuth client", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bridge_oauth_grants SET revoked_at=COALESCE(revoked_at,$3) WHERE organization_id=$1 AND client_id=$2`, organizationID, clientID, revokedAt); err != nil {
		return bridge.Client{}, fmt.Errorf("revoke Bridge OAuth client grants: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return bridge.Client{}, err
	}
	return client, nil
}

func (s *BridgeStore) CreateAuthorizationRequest(ctx context.Context, request bridge.AuthorizationRequest) error {
	_, err := s.database.ExecContext(ctx, `INSERT INTO bridge_oauth_authorization_requests (organization_id,id,client_id,actor_id,redirect_uri,resource_uri,scopes,oauth_state,code_challenge,created_at,expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9,$10,$11)`, request.OrganizationID, request.ID, request.ClientID, request.ActorID,
		request.RedirectURI, request.ResourceURI, scopeStrings(request.Scopes), request.State, request.CodeChallenge, request.CreatedAt, request.ExpiresAt)
	return bridgeWriteError("create Bridge authorization request", err)
}

func (s *BridgeStore) GetAuthorizationRequest(ctx context.Context, organizationID, requestID string) (bridge.AuthorizationRequest, error) {
	request, err := scanBridgeAuthorization(s.database.QueryRowContext(ctx, `SELECT `+bridgeAuthorizationColumns+` FROM bridge_oauth_authorization_requests WHERE organization_id=$1 AND id=$2`, organizationID, requestID))
	return request, bridgeReadError("get Bridge authorization request", err)
}

func (s *BridgeStore) DecideAuthorizationRequest(ctx context.Context, request bridge.AuthorizationRequest, code *bridge.AuthorizationCode) error {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE bridge_oauth_authorization_requests SET decided_at=$3,approved=$4
		WHERE organization_id=$1 AND id=$2 AND decided_at IS NULL AND client_id=$5 AND actor_id=$6 AND redirect_uri=$7 AND resource_uri=$8 AND code_challenge=$9`,
		request.OrganizationID, request.ID, request.DecidedAt, request.Approved, request.ClientID, request.ActorID, request.RedirectURI, request.ResourceURI, request.CodeChallenge)
	if err != nil {
		return bridgeWriteError("decide Bridge authorization request", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return bridge.ErrReplay
	}
	if code != nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO bridge_oauth_authorization_codes (organization_id,id,request_id,client_id,actor_id,redirect_uri,resource_uri,scopes,code_hash,code_challenge,created_at,expires_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, code.OrganizationID, code.ID, code.RequestID, code.ClientID, code.ActorID,
			code.RedirectURI, code.ResourceURI, scopeStrings(code.Scopes), code.CodeHash, code.CodeChallenge, code.CreatedAt, code.ExpiresAt)
		if err != nil {
			return bridgeWriteError("create Bridge authorization code", err)
		}
	}
	return tx.Commit()
}

func (s *BridgeStore) ExchangeAuthorizationCode(ctx context.Context, organizationID string, codeHash []byte, clientID, redirectURI, resourceURI, codeChallenge string, now time.Time, grant bridge.Grant) (bridge.Grant, error) {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return bridge.Grant{}, err
	}
	defer tx.Rollback()
	var actorID string
	var scopesJSON []byte
	err = tx.QueryRowContext(ctx, `UPDATE bridge_oauth_authorization_codes code SET consumed_at=$7
		FROM bridge_oauth_clients client
		WHERE code.organization_id=$1 AND code.code_hash=$2 AND code.client_id=$3 AND code.redirect_uri=$4 AND code.resource_uri=$5 AND code.code_challenge=$6
		AND code.consumed_at IS NULL AND code.expires_at>$7 AND client.organization_id=code.organization_id AND client.id=code.client_id AND client.revoked_at IS NULL
		RETURNING code.actor_id,array_to_json(code.scopes)`, organizationID, codeHash, clientID, redirectURI, resourceURI, codeChallenge, now).Scan(&actorID, &scopesJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return bridge.Grant{}, bridge.ErrUnauthorized
	}
	if err != nil {
		return bridge.Grant{}, fmt.Errorf("consume Bridge authorization code: %w", err)
	}
	grant.OrganizationID, grant.ClientID, grant.ActorID, grant.ResourceURI = organizationID, clientID, actorID, resourceURI
	if err := decodeScopes(scopesJSON, &grant.Scopes); err != nil {
		return bridge.Grant{}, err
	}
	if err := insertBridgeGrant(ctx, tx, grant); err != nil {
		return bridge.Grant{}, err
	}
	if err := tx.Commit(); err != nil {
		return bridge.Grant{}, err
	}
	return grant, nil
}

func (s *BridgeStore) RotateRefreshToken(ctx context.Context, organizationID string, refreshHash []byte, clientID, resourceURI string, now time.Time, replacement bridge.Grant) (bridge.Grant, error) {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return bridge.Grant{}, err
	}
	defer tx.Rollback()
	var actorID string
	var scopesJSON []byte
	var refreshExpiresAt time.Time
	err = tx.QueryRowContext(ctx, `UPDATE bridge_oauth_grants grant_row SET revoked_at=$5
		FROM bridge_oauth_clients client
		WHERE grant_row.organization_id=$1 AND grant_row.refresh_token_hash=$2 AND grant_row.client_id=$3 AND grant_row.resource_uri=$4
		AND grant_row.revoked_at IS NULL AND grant_row.refresh_expires_at>$5 AND client.organization_id=grant_row.organization_id AND client.id=grant_row.client_id AND client.revoked_at IS NULL
		RETURNING grant_row.actor_id,array_to_json(grant_row.scopes),grant_row.refresh_expires_at`, organizationID, refreshHash, clientID, resourceURI, now).Scan(&actorID, &scopesJSON, &refreshExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return bridge.Grant{}, bridge.ErrUnauthorized
	}
	if err != nil {
		return bridge.Grant{}, fmt.Errorf("rotate Bridge refresh token: %w", err)
	}
	replacement.OrganizationID, replacement.ClientID, replacement.ActorID, replacement.ResourceURI = organizationID, clientID, actorID, resourceURI
	replacement.RefreshExpiresAt = refreshExpiresAt
	if replacement.AccessExpiresAt.After(refreshExpiresAt) {
		replacement.AccessExpiresAt = refreshExpiresAt
	}
	if err := decodeScopes(scopesJSON, &replacement.Scopes); err != nil {
		return bridge.Grant{}, err
	}
	if err := insertBridgeGrant(ctx, tx, replacement); err != nil {
		return bridge.Grant{}, err
	}
	if err := tx.Commit(); err != nil {
		return bridge.Grant{}, err
	}
	return replacement, nil
}

func insertBridgeGrant(ctx context.Context, tx *sql.Tx, grant bridge.Grant) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO bridge_oauth_grants (organization_id,id,client_id,actor_id,resource_uri,scopes,access_token_hash,refresh_token_hash,access_expires_at,refresh_expires_at,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, grant.OrganizationID, grant.ID, grant.ClientID, grant.ActorID, grant.ResourceURI,
		scopeStrings(grant.Scopes), grant.AccessTokenHash, grant.RefreshTokenHash, grant.AccessExpiresAt, grant.RefreshExpiresAt, grant.CreatedAt)
	return bridgeWriteError("create Bridge OAuth grant", err)
}

func (s *BridgeStore) AuthenticateAccessToken(ctx context.Context, organizationID string, accessHash []byte, resourceURI string, now time.Time) (bridge.Grant, error) {
	grant, err := scanBridgeGrant(s.database.QueryRowContext(ctx, `UPDATE bridge_oauth_grants g SET last_used_at=$4 FROM bridge_oauth_clients c
		WHERE g.organization_id=$1 AND g.access_token_hash=$2 AND g.resource_uri=$3 AND g.revoked_at IS NULL AND g.access_expires_at>$4
		AND c.organization_id=g.organization_id AND c.id=g.client_id AND c.revoked_at IS NULL RETURNING `+bridgeGrantColumns,
		organizationID, accessHash, resourceURI, now))
	if errors.Is(err, sql.ErrNoRows) {
		return bridge.Grant{}, bridge.ErrUnauthorized
	}
	return grant, bridgeReadError("authenticate Bridge access token", err)
}

func (s *BridgeStore) ListGrants(ctx context.Context, organizationID string, page bridge.PageRequest) ([]bridge.Grant, error) {
	if page.Limit < 1 || page.Limit > bridge.MaximumAdministrationPageSize {
		return nil, bridge.ErrInvalidInput
	}
	if page.Cursor != "" {
		var exists bool
		if err := s.database.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM bridge_oauth_grants WHERE organization_id=$1 AND id=$2)`, organizationID, page.Cursor).Scan(&exists); err != nil {
			return nil, fmt.Errorf("validate Bridge OAuth grant cursor: %w", err)
		}
		if !exists {
			return nil, bridge.ErrInvalidInput
		}
	}
	rows, err := s.database.QueryContext(ctx, `SELECT `+bridgeGrantColumns+` FROM bridge_oauth_grants g JOIN bridge_oauth_clients c ON c.organization_id=g.organization_id AND c.id=g.client_id
		WHERE g.organization_id=$1 AND ($2='' OR (g.created_at < (
			SELECT cursor_grant.created_at FROM bridge_oauth_grants cursor_grant WHERE cursor_grant.organization_id=$1 AND cursor_grant.id=$2
		) OR (g.created_at = (
			SELECT cursor_grant.created_at FROM bridge_oauth_grants cursor_grant WHERE cursor_grant.organization_id=$1 AND cursor_grant.id=$2
		) AND g.id > $2))) ORDER BY g.created_at DESC,g.id LIMIT $3`, organizationID, page.Cursor, page.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("list Bridge OAuth grants: %w", err)
	}
	defer rows.Close()
	items := []bridge.Grant{}
	for rows.Next() {
		item, err := scanBridgeGrant(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *BridgeStore) RevokeGrant(ctx context.Context, organizationID, grantID string, revokedAt time.Time) (bridge.Grant, error) {
	grant, err := scanBridgeGrant(s.database.QueryRowContext(ctx, `UPDATE bridge_oauth_grants g SET revoked_at=COALESCE(g.revoked_at,$3) FROM bridge_oauth_clients c
		WHERE g.organization_id=$1 AND g.id=$2 AND c.organization_id=g.organization_id AND c.id=g.client_id RETURNING `+bridgeGrantColumns,
		organizationID, grantID, revokedAt))
	return grant, bridgeReadError("revoke Bridge OAuth grant", err)
}

func (s *BridgeStore) RevokeToken(ctx context.Context, organizationID string, tokenHash []byte, revokedAt time.Time) error {
	result, err := s.database.ExecContext(ctx, `UPDATE bridge_oauth_grants SET revoked_at=COALESCE(revoked_at,$3) WHERE organization_id=$1 AND (access_token_hash=$2 OR refresh_token_hash=$2)`, organizationID, tokenHash, revokedAt)
	if err != nil {
		return fmt.Errorf("revoke Bridge OAuth token: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return bridge.ErrNotFound
	}
	return nil
}

func (s *BridgeStore) CreateConfirmation(ctx context.Context, confirmation bridge.Confirmation) error {
	_, err := s.database.ExecContext(ctx, `INSERT INTO bridge_mcp_confirmations (organization_id,id,actor_id,action,arguments_hash,token_hash,created_at,expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, confirmation.OrganizationID, confirmation.ID, confirmation.ActorID, confirmation.Action,
		confirmation.ArgumentsHash, confirmation.TokenHash, confirmation.CreatedAt, confirmation.ExpiresAt)
	return bridgeWriteError("create Bridge MCP confirmation", err)
}

func (s *BridgeStore) ConsumeConfirmation(ctx context.Context, organizationID, actorID, action string, argumentsHash, tokenHash []byte, now time.Time) (bridge.Confirmation, error) {
	var confirmation bridge.Confirmation
	var consumed sql.NullTime
	err := s.database.QueryRowContext(ctx, `UPDATE bridge_mcp_confirmations SET consumed_at=$6
		WHERE organization_id=$1 AND actor_id=$2 AND action=$3 AND arguments_hash=$4 AND token_hash=$5 AND consumed_at IS NULL AND expires_at>$6
		RETURNING organization_id,id,actor_id,action,arguments_hash,token_hash,created_at,expires_at,consumed_at`,
		organizationID, actorID, action, argumentsHash, tokenHash, now).Scan(&confirmation.OrganizationID, &confirmation.ID, &confirmation.ActorID,
		&confirmation.Action, &confirmation.ArgumentsHash, &confirmation.TokenHash, &confirmation.CreatedAt, &confirmation.ExpiresAt, &consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return bridge.Confirmation{}, bridge.ErrUnauthorized
	}
	if consumed.Valid {
		confirmation.ConsumedAt = &consumed.Time
	}
	return confirmation, bridgeReadError("consume Bridge MCP confirmation", err)
}

func (s *BridgeStore) AllowRate(ctx context.Context, organizationID string, keyHash [sha256.Size]byte, windowStart time.Time, limit int) (bool, error) {
	var count int
	err := s.database.QueryRowContext(ctx, `WITH expired AS (
		DELETE FROM bridge_rate_windows WHERE organization_id=$1 AND window_start < $3::timestamptz - INTERVAL '1 hour'
	)
	INSERT INTO bridge_rate_windows (organization_id,key_hash,window_start,count) VALUES ($1,$2,$3,1)
		ON CONFLICT (organization_id,key_hash,window_start) DO UPDATE SET count=bridge_rate_windows.count+1 WHERE bridge_rate_windows.count<$4
		RETURNING count`, organizationID, keyHash[:], windowStart, limit).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("update Bridge rate window: %w", err)
	}
	return count <= limit, nil
}

type bridgeScanner interface{ Scan(...any) error }

func scanBridgeClient(row bridgeScanner) (bridge.Client, error) {
	var client bridge.Client
	var redirectsJSON, scopesJSON []byte
	var revoked sql.NullTime
	err := row.Scan(&client.OrganizationID, &client.ID, &client.Name, &redirectsJSON, &scopesJSON, &client.CreatedBy, &client.CreatedAt, &revoked)
	if err == nil {
		err = json.Unmarshal(redirectsJSON, &client.RedirectURIs)
	}
	if err == nil {
		err = decodeScopes(scopesJSON, &client.AllowedScopes)
	}
	if revoked.Valid {
		client.RevokedAt = &revoked.Time
	}
	return client, err
}

func scanBridgeAuthorization(row bridgeScanner) (bridge.AuthorizationRequest, error) {
	var request bridge.AuthorizationRequest
	var scopesJSON []byte
	var state sql.NullString
	var decided sql.NullTime
	err := row.Scan(&request.OrganizationID, &request.ID, &request.ClientID, &request.ActorID, &request.RedirectURI, &request.ResourceURI,
		&scopesJSON, &state, &request.CodeChallenge, &request.CreatedAt, &request.ExpiresAt, &decided, &request.Approved)
	if err == nil {
		err = decodeScopes(scopesJSON, &request.Scopes)
	}
	request.State = state.String
	if decided.Valid {
		request.DecidedAt = &decided.Time
	}
	return request, err
}

func scanBridgeGrant(row bridgeScanner) (bridge.Grant, error) {
	var grant bridge.Grant
	var scopesJSON []byte
	var used, revoked sql.NullTime
	err := row.Scan(&grant.OrganizationID, &grant.ID, &grant.ClientID, &grant.ClientName, &grant.ActorID, &grant.ResourceURI, &scopesJSON,
		&grant.AccessTokenHash, &grant.RefreshTokenHash, &grant.AccessExpiresAt, &grant.RefreshExpiresAt, &grant.CreatedAt, &used, &revoked)
	if err == nil {
		err = decodeScopes(scopesJSON, &grant.Scopes)
	}
	if used.Valid {
		grant.LastUsedAt = &used.Time
	}
	if revoked.Valid {
		grant.RevokedAt = &revoked.Time
	}
	return grant, err
}

func scopeStrings(scopes []bridge.Scope) []string {
	values := make([]string, len(scopes))
	for index, scope := range scopes {
		values[index] = string(scope)
	}
	return values
}

func decodeScopes(encoded []byte, destination *[]bridge.Scope) error {
	var values []string
	if err := json.Unmarshal(encoded, &values); err != nil {
		return err
	}
	*destination = make([]bridge.Scope, len(values))
	for index, value := range values {
		(*destination)[index] = bridge.Scope(value)
	}
	return nil
}

func bridgeReadError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return bridge.ErrNotFound
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func bridgeWriteError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505":
			return bridge.ErrConflict
		case "23503":
			return bridge.ErrNotFound
		case "23502", "23514", "22P02":
			return bridge.ErrInvalidInput
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
