package repository

// In-memory Bridge adapter. Requirements: REQ-API-001, SEC-MCP-001, REQ-EXCHANGE-001.
// Features: integrations.protocols, migration.packages. GitHub: #9, #14.

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/maxlemke/stewardmesh/internal/bridge"
)

type MemoryBridgeStore struct {
	mu             sync.Mutex
	clients        map[string]bridge.Client
	authorizations map[string]bridge.AuthorizationRequest
	codes          map[string]bridge.AuthorizationCode
	grants         map[string]bridge.Grant
	confirmations  map[string]bridge.Confirmation
	rateWindows    map[string]bridge.RateWindow
}

func NewMemoryBridgeStore() *MemoryBridgeStore {
	return &MemoryBridgeStore{
		clients: map[string]bridge.Client{}, authorizations: map[string]bridge.AuthorizationRequest{}, codes: map[string]bridge.AuthorizationCode{},
		grants: map[string]bridge.Grant{}, confirmations: map[string]bridge.Confirmation{}, rateWindows: map[string]bridge.RateWindow{},
	}
}

func bridgeKey(organizationID, id string) string { return organizationID + "\x00" + id }

func (s *MemoryBridgeStore) CreateClient(_ context.Context, client bridge.Client) (bridge.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, activeCount := bridgeKey(client.OrganizationID, client.ID), 0
	if _, exists := s.clients[key]; exists {
		return bridge.Client{}, bridge.ErrConflict
	}
	for _, existing := range s.clients {
		if existing.OrganizationID != client.OrganizationID || existing.RevokedAt != nil {
			continue
		}
		activeCount++
		if strings.EqualFold(existing.Name, client.Name) {
			return bridge.Client{}, bridge.ErrConflict
		}
	}
	if activeCount >= bridge.MaximumClients {
		return bridge.Client{}, bridge.ErrConflict
	}
	s.clients[key] = cloneBridgeClient(client)
	return cloneBridgeClient(client), nil
}

func (s *MemoryBridgeStore) ListClients(_ context.Context, organizationID string, page bridge.PageRequest) ([]bridge.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if page.Limit < 1 || page.Limit > bridge.MaximumAdministrationPageSize {
		return nil, bridge.ErrInvalidInput
	}
	items := []bridge.Client{}
	for _, client := range s.clients {
		if client.OrganizationID == organizationID {
			items = append(items, cloneBridgeClient(client))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if strings.EqualFold(items[i].Name, items[j].Name) {
			return items[i].ID < items[j].ID
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	return bridgeClientPage(items, page)
}

func (s *MemoryBridgeStore) ListExchangeClients(_ context.Context, organizationID string, limit int) ([]bridge.Client, error) {
	if limit < 1 {
		return nil, bridge.ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]bridge.Client, 0)
	for _, client := range s.clients {
		if client.OrganizationID == organizationID {
			items = append(items, cloneBridgeClient(client))
		}
	}
	if len(items) > limit {
		return nil, bridge.ErrTooLarge
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (s *MemoryBridgeStore) GetClient(_ context.Context, organizationID, clientID string) (bridge.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	client, ok := s.clients[bridgeKey(organizationID, clientID)]
	if !ok {
		return bridge.Client{}, bridge.ErrNotFound
	}
	return cloneBridgeClient(client), nil
}

func (s *MemoryBridgeStore) ImportExchangeClient(_ context.Context, client bridge.Client) (bridge.Client, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := bridgeKey(client.OrganizationID, client.ID)
	if existing, exists := s.clients[key]; exists {
		if !reflect.DeepEqual(existing, client) {
			return bridge.Client{}, false, bridge.ErrConflict
		}
		return cloneBridgeClient(existing), false, nil
	}
	activeCount := 0
	if client.RevokedAt == nil {
		for _, existing := range s.clients {
			if existing.OrganizationID != client.OrganizationID || existing.RevokedAt != nil {
				continue
			}
			activeCount++
			if strings.EqualFold(existing.Name, client.Name) {
				return bridge.Client{}, false, bridge.ErrConflict
			}
		}
		if activeCount >= bridge.MaximumClients {
			return bridge.Client{}, false, bridge.ErrConflict
		}
	}
	s.clients[key] = cloneBridgeClient(client)
	return cloneBridgeClient(client), true, nil
}

func (s *MemoryBridgeStore) RevokeClient(_ context.Context, organizationID, clientID string, revokedAt time.Time) (bridge.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := bridgeKey(organizationID, clientID)
	client, ok := s.clients[key]
	if !ok {
		return bridge.Client{}, bridge.ErrNotFound
	}
	if client.RevokedAt == nil {
		at := revokedAt
		client.RevokedAt = &at
		s.clients[key] = client
	}
	for grantKey, grant := range s.grants {
		if grant.OrganizationID == organizationID && grant.ClientID == clientID && grant.RevokedAt == nil {
			at := revokedAt
			grant.RevokedAt = &at
			s.grants[grantKey] = grant
		}
	}
	return cloneBridgeClient(client), nil
}

func (s *MemoryBridgeStore) CreateAuthorizationRequest(_ context.Context, request bridge.AuthorizationRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := bridgeKey(request.OrganizationID, request.ID)
	if _, exists := s.authorizations[key]; exists {
		return bridge.ErrConflict
	}
	s.authorizations[key] = cloneAuthorizationRequest(request)
	return nil
}

func (s *MemoryBridgeStore) GetAuthorizationRequest(_ context.Context, organizationID, requestID string) (bridge.AuthorizationRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	request, ok := s.authorizations[bridgeKey(organizationID, requestID)]
	if !ok {
		return bridge.AuthorizationRequest{}, bridge.ErrNotFound
	}
	return cloneAuthorizationRequest(request), nil
}

func (s *MemoryBridgeStore) DecideAuthorizationRequest(_ context.Context, request bridge.AuthorizationRequest, code *bridge.AuthorizationCode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := bridgeKey(request.OrganizationID, request.ID)
	existing, ok := s.authorizations[key]
	if !ok {
		return bridge.ErrNotFound
	}
	if existing.DecidedAt != nil {
		return bridge.ErrReplay
	}
	expected := cloneAuthorizationRequest(request)
	expected.DecidedAt, expected.Approved = nil, false
	actual := cloneAuthorizationRequest(existing)
	actual.DecidedAt, actual.Approved = nil, false
	if !reflect.DeepEqual(expected, actual) {
		return bridge.ErrConflict
	}
	if request.DecidedAt == nil || request.Approved != (code != nil) {
		return bridge.ErrInvalidInput
	}
	if code != nil {
		codeKey := bridgeKey(code.OrganizationID, hex.EncodeToString(code.CodeHash))
		if _, exists := s.codes[codeKey]; exists {
			return bridge.ErrConflict
		}
		s.codes[codeKey] = cloneAuthorizationCode(*code)
	}
	s.authorizations[key] = cloneAuthorizationRequest(request)
	return nil
}

func (s *MemoryBridgeStore) ExchangeAuthorizationCode(_ context.Context, organizationID string, codeHash []byte, clientID, redirectURI, resourceURI, codeChallenge string, now time.Time, grant bridge.Grant) (bridge.Grant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := bridgeKey(organizationID, hex.EncodeToString(codeHash))
	code, ok := s.codes[key]
	if !ok || code.ClientID != clientID || code.RedirectURI != redirectURI || code.ResourceURI != resourceURI ||
		subtle.ConstantTimeCompare([]byte(code.CodeChallenge), []byte(codeChallenge)) != 1 {
		return bridge.Grant{}, bridge.ErrUnauthorized
	}
	if code.ConsumedAt != nil {
		return bridge.Grant{}, bridge.ErrReplay
	}
	if !code.ExpiresAt.After(now) {
		return bridge.Grant{}, bridge.ErrExpired
	}
	client, ok := s.clients[bridgeKey(organizationID, clientID)]
	if !ok || client.RevokedAt != nil {
		return bridge.Grant{}, bridge.ErrUnauthorized
	}
	at := now
	code.ConsumedAt = &at
	s.codes[key] = code
	grant.OrganizationID, grant.ClientID, grant.ActorID, grant.ResourceURI = code.OrganizationID, code.ClientID, code.ActorID, code.ResourceURI
	grant.Scopes = append([]bridge.Scope(nil), code.Scopes...)
	grantKey := bridgeKey(organizationID, grant.ID)
	if _, exists := s.grants[grantKey]; exists {
		return bridge.Grant{}, bridge.ErrConflict
	}
	s.grants[grantKey] = cloneBridgeGrant(grant)
	return cloneBridgeGrant(grant), nil
}

func (s *MemoryBridgeStore) RotateRefreshToken(_ context.Context, organizationID string, refreshHash []byte, clientID, resourceURI string, now time.Time, replacement bridge.Grant) (bridge.Grant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var oldKey string
	var old bridge.Grant
	for key, grant := range s.grants {
		if grant.OrganizationID == organizationID && subtle.ConstantTimeCompare(grant.RefreshTokenHash, refreshHash) == 1 {
			oldKey, old = key, grant
			break
		}
	}
	if oldKey == "" || old.ClientID != clientID || old.ResourceURI != resourceURI {
		return bridge.Grant{}, bridge.ErrUnauthorized
	}
	if old.RevokedAt != nil {
		return bridge.Grant{}, bridge.ErrReplay
	}
	if !old.RefreshExpiresAt.After(now) {
		return bridge.Grant{}, bridge.ErrExpired
	}
	client, ok := s.clients[bridgeKey(organizationID, clientID)]
	if !ok || client.RevokedAt != nil {
		return bridge.Grant{}, bridge.ErrUnauthorized
	}
	at := now
	old.RevokedAt = &at
	s.grants[oldKey] = old
	replacement.OrganizationID, replacement.ClientID, replacement.ActorID, replacement.ResourceURI = old.OrganizationID, old.ClientID, old.ActorID, old.ResourceURI
	replacement.Scopes = append([]bridge.Scope(nil), old.Scopes...)
	replacement.RefreshExpiresAt = old.RefreshExpiresAt
	if replacement.AccessExpiresAt.After(replacement.RefreshExpiresAt) {
		replacement.AccessExpiresAt = replacement.RefreshExpiresAt
	}
	newKey := bridgeKey(organizationID, replacement.ID)
	if _, exists := s.grants[newKey]; exists {
		return bridge.Grant{}, bridge.ErrConflict
	}
	s.grants[newKey] = cloneBridgeGrant(replacement)
	return cloneBridgeGrant(replacement), nil
}

func (s *MemoryBridgeStore) AuthenticateAccessToken(_ context.Context, organizationID string, accessHash []byte, resourceURI string, now time.Time) (bridge.Grant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, grant := range s.grants {
		if grant.OrganizationID != organizationID || subtle.ConstantTimeCompare(grant.AccessTokenHash, accessHash) != 1 {
			continue
		}
		if grant.ResourceURI != resourceURI || grant.RevokedAt != nil || !grant.AccessExpiresAt.After(now) {
			return bridge.Grant{}, bridge.ErrUnauthorized
		}
		client, ok := s.clients[bridgeKey(organizationID, grant.ClientID)]
		if !ok || client.RevokedAt != nil {
			return bridge.Grant{}, bridge.ErrUnauthorized
		}
		used := now
		grant.LastUsedAt = &used
		s.grants[key] = grant
		return cloneBridgeGrant(grant), nil
	}
	return bridge.Grant{}, bridge.ErrUnauthorized
}

func (s *MemoryBridgeStore) ListGrants(_ context.Context, organizationID string, page bridge.PageRequest) ([]bridge.Grant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if page.Limit < 1 || page.Limit > bridge.MaximumAdministrationPageSize {
		return nil, bridge.ErrInvalidInput
	}
	items := []bridge.Grant{}
	for _, grant := range s.grants {
		if grant.OrganizationID != organizationID {
			continue
		}
		grant.ClientName = s.clients[bridgeKey(organizationID, grant.ClientID)].Name
		items = append(items, cloneBridgeGrant(grant))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return bridgeGrantPage(items, page)
}

func bridgeClientPage(items []bridge.Client, page bridge.PageRequest) ([]bridge.Client, error) {
	start := 0
	if page.Cursor != "" {
		start = -1
		for index, item := range items {
			if item.ID == page.Cursor {
				start = index + 1
				break
			}
		}
		if start < 0 {
			return nil, bridge.ErrInvalidInput
		}
	}
	end := min(len(items), start+page.Limit+1)
	return append([]bridge.Client(nil), items[start:end]...), nil
}

func bridgeGrantPage(items []bridge.Grant, page bridge.PageRequest) ([]bridge.Grant, error) {
	start := 0
	if page.Cursor != "" {
		start = -1
		for index, item := range items {
			if item.ID == page.Cursor {
				start = index + 1
				break
			}
		}
		if start < 0 {
			return nil, bridge.ErrInvalidInput
		}
	}
	end := min(len(items), start+page.Limit+1)
	return append([]bridge.Grant(nil), items[start:end]...), nil
}

func (s *MemoryBridgeStore) RevokeGrant(_ context.Context, organizationID, grantID string, revokedAt time.Time) (bridge.Grant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := bridgeKey(organizationID, grantID)
	grant, ok := s.grants[key]
	if !ok {
		return bridge.Grant{}, bridge.ErrNotFound
	}
	if grant.RevokedAt == nil {
		at := revokedAt
		grant.RevokedAt = &at
		s.grants[key] = grant
	}
	grant.ClientName = s.clients[bridgeKey(organizationID, grant.ClientID)].Name
	return cloneBridgeGrant(grant), nil
}

func (s *MemoryBridgeStore) RevokeToken(_ context.Context, organizationID string, tokenHash []byte, revokedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, grant := range s.grants {
		if grant.OrganizationID != organizationID || subtle.ConstantTimeCompare(grant.AccessTokenHash, tokenHash) != 1 && subtle.ConstantTimeCompare(grant.RefreshTokenHash, tokenHash) != 1 {
			continue
		}
		if grant.RevokedAt == nil {
			at := revokedAt
			grant.RevokedAt = &at
			s.grants[key] = grant
		}
		return nil
	}
	return bridge.ErrNotFound
}

func (s *MemoryBridgeStore) CreateConfirmation(_ context.Context, confirmation bridge.Confirmation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := bridgeKey(confirmation.OrganizationID, hex.EncodeToString(confirmation.TokenHash))
	if _, exists := s.confirmations[key]; exists {
		return bridge.ErrConflict
	}
	s.confirmations[key] = cloneBridgeConfirmation(confirmation)
	return nil
}

func (s *MemoryBridgeStore) ConsumeConfirmation(_ context.Context, organizationID, actorID, action string, argumentsHash, tokenHash []byte, now time.Time) (bridge.Confirmation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := bridgeKey(organizationID, hex.EncodeToString(tokenHash))
	confirmation, ok := s.confirmations[key]
	if !ok || confirmation.ActorID != actorID || confirmation.Action != action || subtle.ConstantTimeCompare(confirmation.ArgumentsHash, argumentsHash) != 1 {
		return bridge.Confirmation{}, bridge.ErrUnauthorized
	}
	if confirmation.ConsumedAt != nil {
		return bridge.Confirmation{}, bridge.ErrReplay
	}
	if !confirmation.ExpiresAt.After(now) {
		return bridge.Confirmation{}, bridge.ErrExpired
	}
	at := now
	confirmation.ConsumedAt = &at
	s.confirmations[key] = confirmation
	return cloneBridgeConfirmation(confirmation), nil
}

func (s *MemoryBridgeStore) AllowRate(_ context.Context, organizationID string, keyHash [sha256.Size]byte, windowStart time.Time, limit int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, window := range s.rateWindows {
		if window.WindowStart.Before(windowStart.Add(-time.Hour)) {
			delete(s.rateWindows, key)
		}
	}
	key := organizationID + "\x00" + hex.EncodeToString(keyHash[:]) + "\x00" + windowStart.UTC().Format(time.RFC3339Nano)
	window := s.rateWindows[key]
	if window.Count >= limit {
		return false, nil
	}
	window.KeyHash, window.WindowStart, window.Count = keyHash, windowStart.UTC(), window.Count+1
	s.rateWindows[key] = window
	return true, nil
}

func cloneBridgeClient(value bridge.Client) bridge.Client {
	value.RedirectURIs = append([]string(nil), value.RedirectURIs...)
	value.AllowedScopes = append([]bridge.Scope(nil), value.AllowedScopes...)
	if value.RevokedAt != nil {
		at := *value.RevokedAt
		value.RevokedAt = &at
	}
	return value
}

func cloneAuthorizationRequest(value bridge.AuthorizationRequest) bridge.AuthorizationRequest {
	value.Scopes = append([]bridge.Scope(nil), value.Scopes...)
	if value.DecidedAt != nil {
		at := *value.DecidedAt
		value.DecidedAt = &at
	}
	return value
}

func cloneAuthorizationCode(value bridge.AuthorizationCode) bridge.AuthorizationCode {
	value.Scopes = append([]bridge.Scope(nil), value.Scopes...)
	value.CodeHash = append([]byte(nil), value.CodeHash...)
	if value.ConsumedAt != nil {
		at := *value.ConsumedAt
		value.ConsumedAt = &at
	}
	return value
}

func cloneBridgeGrant(value bridge.Grant) bridge.Grant {
	value.Scopes = append([]bridge.Scope(nil), value.Scopes...)
	value.AccessTokenHash = append([]byte(nil), value.AccessTokenHash...)
	value.RefreshTokenHash = append([]byte(nil), value.RefreshTokenHash...)
	if value.LastUsedAt != nil {
		at := *value.LastUsedAt
		value.LastUsedAt = &at
	}
	if value.RevokedAt != nil {
		at := *value.RevokedAt
		value.RevokedAt = &at
	}
	return value
}

func cloneBridgeConfirmation(value bridge.Confirmation) bridge.Confirmation {
	value.ArgumentsHash = append([]byte(nil), value.ArgumentsHash...)
	value.TokenHash = append([]byte(nil), value.TokenHash...)
	if value.ConsumedAt != nil {
		at := *value.ConsumedAt
		value.ConsumedAt = &at
	}
	return value
}
