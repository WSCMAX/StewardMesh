// Package sailpoint provides StewardMesh's optional read-only SailPoint
// Identity Security Cloud directory connector.
// Requirement: REQ-DIRECTORY-EXPANSION-004. Feature: identity.directory.
package sailpoint

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/directoryexpansion"
)

const (
	RequirementID = "REQ-DIRECTORY-EXPANSION-004"
	FeatureID     = "identity.directory"

	defaultSourceSystemID  = "sailpoint"
	apiVersionPath         = "/v2025"
	defaultHTTPTimeout     = 15 * time.Second
	maximumHTTPTimeout     = 30 * time.Second
	maximumResponseBytes   = 2 << 20
	maximumProviderPages   = 500
	maximumProviderRecords = 5000
	maximumMemberships     = 20000
	maximumRetries         = 3
	maximumRetryDelay      = 2 * time.Second
	pageSize               = 50
)

var (
	sourceSystemIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	providerIDPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,254}$`)
	clientIDPattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{15,255}$`)
	tenantPattern         = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	bearerTokenPattern    = regexp.MustCompile(`^[A-Za-z0-9._~+/=-]+$`)
)

// Config contains deployment-only SailPoint endpoint and client credentials.
// It must never be serialized or returned by an application API.
type Config struct {
	SourceSystemID string
	BaseURL        string
	ClientID       string
	ClientSecret   string
}

// Options provides bounded transport seams for deterministic tests. Production
// callers should use the zero value. The base URL override accepts loopback only.
type Options struct {
	HTTPClient     *http.Client
	Sleep          func(context.Context, time.Duration) error
	Now            func() time.Time
	MaximumPages   int
	MaximumRecords int
	MaximumMembers int
	MaximumRetries int
	baseURL        string
	pageSize       int
}

type Connector struct {
	system         directoryexpansion.SourceSystem
	baseURL        *url.URL
	clientID       string
	clientSecret   string
	httpClient     *http.Client
	sleep          func(context.Context, time.Duration) error
	now            func() time.Time
	maximumPages   int
	maximumRecords int
	maximumMembers int
	maximumRetries int
	pageSize       int
}

var _ directoryexpansion.Connector = (*Connector)(nil)

func ValidateConfig(configuration Config) error {
	_, _, err := validateConfig(configuration, "")
	return err
}

func NewConnector(configuration Config, options Options) (*Connector, error) {
	normalized, endpoint, err := validateConfig(configuration, options.baseURL)
	if err != nil {
		return nil, err
	}
	client := &http.Client{}
	if options.HTTPClient != nil {
		copy := *options.HTTPClient
		client = &copy
	}
	if client.Timeout <= 0 {
		client.Timeout = defaultHTTPTimeout
	}
	if client.Timeout > maximumHTTPTimeout {
		client.Timeout = maximumHTTPTimeout
	}
	if client.Transport == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		// Directory credentials must not be routed through ambient proxy
		// configuration. The configured tenant endpoint is the only destination.
		transport.Proxy = nil
		client.Transport = transport
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if options.Sleep == nil {
		options.Sleep = sleepContext
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	if options.MaximumPages == 0 {
		options.MaximumPages = maximumProviderPages
	}
	if options.MaximumRecords == 0 {
		options.MaximumRecords = maximumProviderRecords
	}
	if options.MaximumMembers == 0 {
		options.MaximumMembers = maximumMemberships
	}
	if options.MaximumRetries == 0 {
		options.MaximumRetries = maximumRetries
	}
	if options.pageSize == 0 {
		options.pageSize = pageSize
	}
	if options.MaximumPages < 1 || options.MaximumPages > maximumProviderPages ||
		options.MaximumRecords < 1 || options.MaximumRecords > maximumProviderRecords ||
		options.MaximumMembers < 1 || options.MaximumMembers > maximumMemberships ||
		options.MaximumRetries < 1 || options.MaximumRetries > maximumRetries || options.pageSize < 1 || options.pageSize > pageSize {
		return nil, errors.New("SailPoint connector bounds are invalid")
	}
	return &Connector{
		system: directoryexpansion.SourceSystem{
			ID: normalized.SourceSystemID, Provider: directoryexpansion.SailPointProvider,
			ConfigRevision: revisionFor(normalized),
		},
		baseURL: endpoint, clientID: normalized.ClientID, clientSecret: normalized.ClientSecret,
		httpClient: client, sleep: options.Sleep, now: options.Now,
		maximumPages: options.MaximumPages, maximumRecords: options.MaximumRecords,
		maximumMembers: options.MaximumMembers, maximumRetries: options.MaximumRetries, pageSize: options.pageSize,
	}, nil
}

func (c *Connector) SourceSystem() directoryexpansion.SourceSystem { return c.system }

// PullPage obtains one complete bounded snapshot. SailPoint collection offsets
// are consumed internally so records can be de-duplicated and sorted before the
// provider-neutral exact plan is persisted.
func (c *Connector) PullPage(ctx context.Context, cursor string) (directoryexpansion.Page, error) {
	if c == nil || strings.TrimSpace(cursor) != "" {
		return directoryexpansion.Page{}, permanent("SailPoint connector cursor is invalid", nil)
	}
	if ctx == nil {
		return directoryexpansion.Page{}, permanent("SailPoint request context is required", nil)
	}
	token, err := c.accessToken(ctx)
	if err != nil {
		return directoryexpansion.Page{}, err
	}
	pageCount := 0
	identities, err := collect[sailPointIdentity](ctx, c, token, apiVersionPath+"/identities", &pageCount, c.maximumRecords)
	if err != nil {
		return directoryexpansion.Page{}, err
	}
	accounts, err := collect[sailPointAccount](ctx, c, token, apiVersionPath+"/accounts", &pageCount, c.maximumRecords)
	if err != nil {
		return directoryexpansion.Page{}, err
	}
	workgroups, err := collect[sailPointWorkgroup](ctx, c, token, apiVersionPath+"/workgroups", &pageCount, c.maximumRecords)
	if err != nil {
		return directoryexpansion.Page{}, err
	}
	roles, err := collect[sailPointRole](ctx, c, token, apiVersionPath+"/roles", &pageCount, c.maximumRecords)
	if err != nil {
		return directoryexpansion.Page{}, err
	}

	records := make([]directoryexpansion.Record, 0, min(c.maximumRecords, len(identities)+len(accounts)+len(workgroups)+len(roles)))
	seenRecords := make(map[string]struct{})
	memberIndexes := make(map[string]int)
	identityDepartments := make(map[string]string)
	identityAccountCounts := make(map[string][2]int)
	for _, identity := range identities {
		record, _, err := normalizeIdentity(identity)
		if err != nil {
			return directoryexpansion.Page{}, err
		}
		if err := appendRecord(&records, seenRecords, record, c.maximumRecords); err != nil {
			return directoryexpansion.Page{}, err
		}
		memberIndexes[record.SourceRecordID] = len(records) - 1
		if record.Department != "" {
			identityDepartments[record.SourceRecordID] = record.Department
		}
	}

	accountSources := make(map[string]string)
	for _, account := range accounts {
		record, sourceID, ownerID, err := normalizeAccount(account)
		if err != nil {
			return directoryexpansion.Page{}, err
		}
		if err := appendRecord(&records, seenRecords, record, c.maximumRecords); err != nil {
			return directoryexpansion.Page{}, err
		}
		memberIndexes[record.SourceRecordID] = len(records) - 1
		sourceName := record.DirectoryAttributes["source-name"]
		if existing, ok := accountSources[sourceID]; ok && existing != sourceName {
			return directoryexpansion.Page{}, permanent("SailPoint accounts disagree about a source identity", nil)
		}
		accountSources[sourceID] = sourceName
		if ownerID != "" {
			counts := identityAccountCounts["identity:"+ownerID]
			counts[0]++
			if record.Status == "inactive" {
				counts[1]++
			}
			identityAccountCounts["identity:"+ownerID] = counts
		}
	}
	for sourceRecordID, counts := range identityAccountCounts {
		if index, ok := memberIndexes[sourceRecordID]; ok {
			if records[index].DirectoryAttributes == nil {
				records[index].DirectoryAttributes = map[string]string{}
			}
			records[index].DirectoryAttributes["account-count"] = strconv.Itoa(counts[0])
			records[index].DirectoryAttributes["inactive-account-count"] = strconv.Itoa(counts[1])
		}
	}

	seenMemberships := make(map[string]struct{})
	membershipCount := 0
	for _, sourceID := range sortedKeys(accountSources) {
		groupID := "account-source:" + sourceID
		displayName := accountSources[sourceID]
		if displayName == "" {
			displayName = sourceID
		}
		group := directoryexpansion.Record{SourceRecordID: groupID, Kind: directoryexpansion.RecordGroup,
			GroupName: groupID, DisplayName: displayName, Status: "active",
			NormalizedMetadata: map[string]string{"directory-object-kind": "account-source"}}
		if err := appendRecord(&records, seenRecords, group, c.maximumRecords); err != nil {
			return directoryexpansion.Page{}, err
		}
		for index := range records {
			if records[index].Kind != directoryexpansion.RecordIdentity || records[index].DirectoryAttributes["source-id"] != sourceID {
				continue
			}
			if err := addMembership(&records, seenRecords, seenMemberships, memberIndexes, groupID,
				records[index].SourceRecordID, records[index].DisplayName, records[index].Status,
				map[string]string{"directory-object-kind": "account-source-membership"}, &membershipCount,
				c.maximumMembers, c.maximumRecords); err != nil {
				return directoryexpansion.Page{}, err
			}
		}
	}

	departments := make(map[string]string)
	for _, department := range identityDepartments {
		normalized := strings.ToLower(department)
		if existing, present := departments[normalized]; !present || department < existing {
			departments[normalized] = department
		}
	}
	for _, normalized := range sortedKeys(departments) {
		department := departments[normalized]
		groupID := "department:" + shortDigest(normalized)
		group := directoryexpansion.Record{SourceRecordID: groupID, Kind: directoryexpansion.RecordGroup,
			GroupName: department, DisplayName: department, Status: "active",
			NormalizedMetadata: map[string]string{"directory-object-kind": "department"}}
		if err := appendRecord(&records, seenRecords, group, c.maximumRecords); err != nil {
			return directoryexpansion.Page{}, err
		}
		for memberID, memberDepartment := range identityDepartments {
			if !strings.EqualFold(memberDepartment, department) {
				continue
			}
			index := memberIndexes[memberID]
			if err := addMembership(&records, seenRecords, seenMemberships, memberIndexes, groupID, memberID,
				records[index].DisplayName, records[index].Status,
				map[string]string{"directory-object-kind": "department-membership"}, &membershipCount,
				c.maximumMembers, c.maximumRecords); err != nil {
				return directoryexpansion.Page{}, err
			}
		}
	}

	sort.Slice(workgroups, func(i, j int) bool { return strings.ToLower(workgroups[i].ID) < strings.ToLower(workgroups[j].ID) })
	for _, workgroup := range workgroups {
		group, providerID, err := normalizeWorkgroup(workgroup)
		if err != nil {
			return directoryexpansion.Page{}, err
		}
		if err := appendRecord(&records, seenRecords, group, c.maximumRecords); err != nil {
			return directoryexpansion.Page{}, err
		}
		members, err := collect[sailPointReference](ctx, c, token,
			apiVersionPath+"/workgroups/"+url.PathEscape(providerID)+"/members", &pageCount, c.maximumMembers)
		if err != nil {
			return directoryexpansion.Page{}, err
		}
		for _, member := range members {
			memberID, displayName, err := normalizeReference(member)
			if err != nil {
				return directoryexpansion.Page{}, permanent("SailPoint returned a malformed governance-group membership", err)
			}
			memberSourceID := "identity:" + memberID
			status := "active"
			if index, ok := memberIndexes[memberSourceID]; ok {
				status = records[index].Status
			}
			if err := addMembership(&records, seenRecords, seenMemberships, memberIndexes, group.SourceRecordID,
				memberSourceID, displayName, status, map[string]string{"directory-object-kind": "governance-group-membership"},
				&membershipCount, c.maximumMembers, c.maximumRecords); err != nil {
				return directoryexpansion.Page{}, err
			}
		}
	}

	sort.Slice(roles, func(i, j int) bool { return strings.ToLower(roles[i].ID) < strings.ToLower(roles[j].ID) })
	for _, role := range roles {
		group, providerID, err := normalizeRole(role)
		if err != nil {
			return directoryexpansion.Page{}, err
		}
		if err := appendRecord(&records, seenRecords, group, c.maximumRecords); err != nil {
			return directoryexpansion.Page{}, err
		}
		members, err := collect[sailPointReference](ctx, c, token,
			apiVersionPath+"/roles/"+url.PathEscape(providerID)+"/assigned-identities", &pageCount, c.maximumMembers)
		if err != nil {
			return directoryexpansion.Page{}, err
		}
		for _, member := range members {
			memberID, displayName, err := normalizeReference(member)
			if err != nil {
				return directoryexpansion.Page{}, permanent("SailPoint returned a malformed role assignment", err)
			}
			memberSourceID := "identity:" + memberID
			status := "active"
			if index, ok := memberIndexes[memberSourceID]; ok {
				status = records[index].Status
			}
			if err := addMembership(&records, seenRecords, seenMemberships, memberIndexes, group.SourceRecordID,
				memberSourceID, displayName, status, map[string]string{"directory-object-kind": "role-assignment"},
				&membershipCount, c.maximumMembers, c.maximumRecords); err != nil {
				return directoryexpansion.Page{}, err
			}
		}
	}

	for index := range records {
		records[index].GroupSourceIDs = uniqueSorted(records[index].GroupSourceIDs)
		if len(records[index].GroupSourceIDs) > directoryexpansion.MaximumGroupLinks {
			return directoryexpansion.Page{}, permanent("SailPoint directory object exceeds the group membership limit", nil)
		}
	}
	sort.Slice(records, func(i, j int) bool {
		left, right := recordKindOrder(records[i].Kind), recordKindOrder(records[j].Kind)
		if left != right {
			return left < right
		}
		return records[i].SourceRecordID < records[j].SourceRecordID
	})
	return directoryexpansion.Page{Records: records, CompleteSnapshot: true}, nil
}

type sailPointIdentity struct {
	ID              string                     `json:"id"`
	Name            string                     `json:"name"`
	Alias           string                     `json:"alias"`
	Email           string                     `json:"email"`
	EmailAddress    string                     `json:"emailAddress"`
	Department      string                     `json:"department"`
	Status          string                     `json:"status"`
	IdentityStatus  string                     `json:"identityStatus"`
	CloudStatus     string                     `json:"cloudStatus"`
	ProcessingState string                     `json:"processingState"`
	Enabled         *bool                      `json:"enabled"`
	Inactive        *bool                      `json:"inactive"`
	Disabled        *bool                      `json:"disabled"`
	Locked          *bool                      `json:"locked"`
	LifecycleState  *sailPointLifecycleState   `json:"lifecycleState"`
	Attributes      map[string]json.RawMessage `json:"attributes"`
}

type sailPointLifecycleState struct {
	StateName string `json:"stateName"`
}

type sailPointReference struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Alias        string `json:"alias"`
	DisplayName  string `json:"displayName"`
	Email        string `json:"email"`
	EmailAddress string `json:"emailAddress"`
}

type sailPointAccount struct {
	ID             string              `json:"id"`
	Name           string              `json:"name"`
	NativeIdentity string              `json:"nativeIdentity"`
	SourceID       string              `json:"sourceId"`
	SourceName     string              `json:"sourceName"`
	IdentityID     string              `json:"identityId"`
	Source         *sailPointReference `json:"source"`
	Identity       *sailPointReference `json:"identity"`
	Disabled       *bool               `json:"disabled"`
	Locked         *bool               `json:"locked"`
	Uncorrelated   *bool               `json:"uncorrelated"`
}

type sailPointWorkgroup struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Owner       *sailPointReference `json:"owner"`
}

type sailPointRole struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Owner       *sailPointReference `json:"owner"`
	Enabled     *bool               `json:"enabled"`
	Requestable *bool               `json:"requestable"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

func (c *Connector) accessToken(ctx context.Context) (string, error) {
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {c.clientID}, "client_secret": {c.clientSecret}}
	endpoint := *c.baseURL
	endpoint.Path = "/oauth/token"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return "", permanent("SailPoint token request could not be constructed", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("User-Agent", "StewardMesh-SailPoint/1")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", transient("SailPoint token endpoint is temporarily unavailable", err)
	}
	if response.StatusCode != http.StatusOK {
		_, _ = io.CopyN(io.Discard, response.Body, 4096)
		_ = response.Body.Close()
		if retryableStatus(response.StatusCode) {
			return "", transient("SailPoint token endpoint is temporarily unavailable", nil)
		}
		return "", permanent("SailPoint credentials or endpoint were rejected", nil)
	}
	var token tokenResponse
	if err := decodeResponse(response, &token); err != nil || !validBearerToken(token.AccessToken) ||
		!strings.EqualFold(token.TokenType, "bearer") || token.ExpiresIn < 1 {
		return "", permanent("SailPoint returned a malformed token response", err)
	}
	return token.AccessToken, nil
}

func collect[T any](ctx context.Context, connector *Connector, token, path string, pageCount *int, maximumItems int) ([]T, error) {
	result := make([]T, 0)
	offset := 0
	total := -1
	for {
		(*pageCount)++
		if *pageCount > connector.maximumPages {
			return nil, permanent("SailPoint snapshot exceeds the page limit", nil)
		}
		endpoint := *connector.baseURL
		endpoint.Path = path
		query := endpoint.Query()
		query.Set("limit", strconv.Itoa(connector.pageSize))
		query.Set("offset", strconv.Itoa(offset))
		query.Set("count", "true")
		endpoint.RawQuery = query.Encode()
		var page []T
		header, err := connector.get(ctx, token, endpoint.String(), &page)
		if err != nil {
			return nil, err
		}
		if page == nil || len(page) > connector.pageSize {
			return nil, permanent("SailPoint returned malformed collection pagination", nil)
		}
		if rawTotal := strings.TrimSpace(header.Get("X-Total-Count")); rawTotal != "" {
			parsed, parseErr := strconv.Atoi(rawTotal)
			if parseErr != nil || parsed < 0 || parsed < offset+len(page) || (total >= 0 && parsed != total) {
				return nil, permanent("SailPoint returned malformed collection pagination", parseErr)
			}
			total = parsed
		}
		result = append(result, page...)
		if len(result) > maximumItems {
			return nil, permanent("SailPoint collection exceeds the item limit", nil)
		}
		offset += len(page)
		if total >= 0 {
			if offset == total {
				return result, nil
			}
			if len(page) == 0 || len(page) < connector.pageSize {
				return nil, permanent("SailPoint returned malformed collection pagination", nil)
			}
		} else if len(page) < connector.pageSize {
			return result, nil
		}
	}
}

func (c *Connector) get(ctx context.Context, token, endpoint string, destination any) (http.Header, error) {
	for attempt := 0; attempt < c.maximumRetries; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, permanent("SailPoint request could not be constructed", err)
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("User-Agent", "StewardMesh-SailPoint/1")
		request.Header.Set("X-SailPoint-Experimental", "true")
		response, err := c.httpClient.Do(request)
		if err != nil {
			if attempt+1 < c.maximumRetries {
				if sleepErr := c.sleep(ctx, retryDelay(attempt, "", c.now())); sleepErr != nil {
					return nil, transient("SailPoint request was canceled", sleepErr)
				}
				continue
			}
			return nil, transient("SailPoint could not be reached after bounded retries", err)
		}
		if response.StatusCode == http.StatusOK {
			header := response.Header.Clone()
			if err := decodeResponse(response, destination); err != nil {
				return nil, permanent("SailPoint returned a malformed response", err)
			}
			return header, nil
		}
		retryAfter := response.Header.Get("Retry-After")
		_, _ = io.CopyN(io.Discard, response.Body, 4096)
		_ = response.Body.Close()
		if retryableStatus(response.StatusCode) && attempt+1 < c.maximumRetries {
			if sleepErr := c.sleep(ctx, retryDelay(attempt, retryAfter, c.now())); sleepErr != nil {
				return nil, transient("SailPoint request was canceled", sleepErr)
			}
			continue
		}
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return nil, permanent("SailPoint credentials or read permissions were rejected", nil)
		case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway,
			http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return nil, transient("SailPoint remained unavailable after bounded retries", nil)
		default:
			return nil, permanent("SailPoint returned an unexpected response", nil)
		}
	}
	return nil, transient("SailPoint remained unavailable after bounded retries", nil)
}

func decodeResponse(response *http.Response, destination any) error {
	defer response.Body.Close()
	if response.ContentLength > maximumResponseBytes {
		return errors.New("response exceeds limit")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maximumResponseBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maximumResponseBytes {
		return errors.New("response exceeds limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("response contains trailing content")
	}
	return nil
}

func normalizeIdentity(identity sailPointIdentity) (directoryexpansion.Record, string, error) {
	id, ok := normalizedProviderID(identity.ID)
	if !ok {
		return directoryexpansion.Record{}, "", permanent("SailPoint returned a malformed identity", nil)
	}
	displayAttribute, err := attributeString(identity.Attributes, "displayName")
	if err != nil {
		return directoryexpansion.Record{}, "", permanent("SailPoint returned a malformed identity attribute", err)
	}
	displayName := firstNonempty(identity.Name, displayAttribute, identity.Alias)
	displayName, ok = boundedText(displayName, 200, true)
	if !ok {
		return directoryexpansion.Record{}, "", permanent("SailPoint returned a malformed identity", nil)
	}
	emailAttribute, err := firstAttributeString(identity.Attributes, "email", "workEmail")
	if err != nil {
		return directoryexpansion.Record{}, "", permanent("SailPoint returned a malformed identity email", err)
	}
	email, ok := normalizedEmail(firstNonempty(identity.EmailAddress, identity.Email, emailAttribute))
	if !ok || email == "" {
		return directoryexpansion.Record{}, "", permanent("SailPoint identity is missing a valid email", nil)
	}
	departmentAttribute, err := firstAttributeString(identity.Attributes, "department", "departmentName")
	if err != nil {
		return directoryexpansion.Record{}, "", permanent("SailPoint returned a malformed department attribute", err)
	}
	department, ok := boundedText(firstNonempty(identity.Department, departmentAttribute), 200, false)
	if !ok {
		return directoryexpansion.Record{}, "", permanent("SailPoint returned a malformed department attribute", nil)
	}
	lifecycle, err := firstAttributeString(identity.Attributes, "cloudLifecycleState", "lifecycleState")
	if err != nil {
		return directoryexpansion.Record{}, "", permanent("SailPoint returned a malformed lifecycle attribute", err)
	}
	if identity.LifecycleState != nil {
		lifecycle = firstNonempty(lifecycle, identity.LifecycleState.StateName)
	}
	attributes := map[string]string{}
	for key, candidate := range map[string]string{
		"alias": identity.Alias, "cloud-status": identity.CloudStatus,
		"lifecycle-state": lifecycle, "processing-state": identity.ProcessingState,
	} {
		value, valid := boundedText(candidate, 500, false)
		if !valid {
			return directoryexpansion.Record{}, "", permanent("SailPoint returned a malformed identity attribute", nil)
		}
		if value != "" {
			attributes[key] = value
		}
	}
	status := identityStatus(firstNonempty(identity.IdentityStatus, identity.Status), identity.CloudStatus, lifecycle,
		identity.Enabled, identity.Inactive, identity.Disabled, identity.Locked)
	return directoryexpansion.Record{SourceRecordID: "identity:" + id, Kind: directoryexpansion.RecordIdentity,
		IdentityKind: "person", DisplayName: displayName, Email: email, Status: status,
		Department: department, DirectoryAttributes: attributes}, id, nil
}

func normalizeAccount(account sailPointAccount) (directoryexpansion.Record, string, string, error) {
	id, ok := normalizedProviderID(account.ID)
	if !ok {
		return directoryexpansion.Record{}, "", "", permanent("SailPoint returned a malformed account", nil)
	}
	sourceID := account.SourceID
	sourceName := account.SourceName
	if account.Source != nil {
		sourceID = firstNonempty(sourceID, account.Source.ID)
		sourceName = firstNonempty(sourceName, account.Source.Name, account.Source.DisplayName)
	}
	sourceID, ok = normalizedProviderID(sourceID)
	if !ok {
		return directoryexpansion.Record{}, "", "", permanent("SailPoint returned a malformed account source", nil)
	}
	displayName, ok := boundedText(account.Name, 200, true)
	if !ok {
		return directoryexpansion.Record{}, "", "", permanent("SailPoint returned a malformed account", nil)
	}
	sourceName, ok = boundedText(sourceName, 200, true)
	if !ok {
		return directoryexpansion.Record{}, "", "", permanent("SailPoint returned a malformed account source", nil)
	}
	nativeIdentity, ok := boundedText(account.NativeIdentity, 500, true)
	if !ok || account.Disabled == nil || account.Locked == nil || account.Uncorrelated == nil {
		return directoryexpansion.Record{}, "", "", permanent("SailPoint returned a malformed account", nil)
	}
	ownerID := account.IdentityID
	if account.Identity != nil {
		ownerID = firstNonempty(ownerID, account.Identity.ID)
	}
	if ownerID != "" {
		ownerID, ok = normalizedProviderID(ownerID)
		if !ok {
			return directoryexpansion.Record{}, "", "", permanent("SailPoint returned a malformed account owner", nil)
		}
	}
	attributes := map[string]string{"source-id": sourceID, "source-name": sourceName, "native-identity": nativeIdentity}
	for key, candidate := range map[string]string{"owner-identity-id": ownerID} {
		value, valid := boundedText(candidate, 500, false)
		if !valid {
			return directoryexpansion.Record{}, "", "", permanent("SailPoint returned a malformed account attribute", nil)
		}
		if value != "" {
			attributes[key] = value
		}
	}
	attributes["locked"] = strconv.FormatBool(*account.Locked)
	attributes["uncorrelated"] = strconv.FormatBool(*account.Uncorrelated)
	status := "active"
	if *account.Disabled {
		status = "inactive"
	}
	return directoryexpansion.Record{SourceRecordID: "account:" + id, Kind: directoryexpansion.RecordIdentity,
		IdentityKind: "shared", DisplayName: displayName, Status: status, DirectoryAttributes: attributes}, sourceID, ownerID, nil
}

func normalizeWorkgroup(group sailPointWorkgroup) (directoryexpansion.Record, string, error) {
	id, ok := normalizedProviderID(group.ID)
	if !ok {
		return directoryexpansion.Record{}, "", permanent("SailPoint returned a malformed governance group", nil)
	}
	name, ok := boundedText(group.Name, 200, true)
	if !ok {
		return directoryexpansion.Record{}, "", permanent("SailPoint returned a malformed governance group", nil)
	}
	description, ok := boundedTextAllowTab(group.Description, 2000, false)
	if !ok {
		return directoryexpansion.Record{}, "", permanent("SailPoint returned a malformed governance group", nil)
	}
	metadata := map[string]string{"directory-object-kind": "governance-group"}
	if group.Owner != nil {
		ownerID, valid := normalizedProviderID(group.Owner.ID)
		if !valid {
			return directoryexpansion.Record{}, "", permanent("SailPoint returned a malformed governance group owner", nil)
		}
		metadata["owner-identity-id"] = ownerID
	}
	return directoryexpansion.Record{SourceRecordID: "workgroup:" + id, Kind: directoryexpansion.RecordGroup,
		GroupName: name, DisplayName: name, Description: description, Status: "active", NormalizedMetadata: metadata}, id, nil
}

func normalizeRole(role sailPointRole) (directoryexpansion.Record, string, error) {
	id, ok := normalizedProviderID(role.ID)
	if !ok {
		return directoryexpansion.Record{}, "", permanent("SailPoint returned a malformed role", nil)
	}
	name, ok := boundedText(role.Name, 200, true)
	if !ok {
		return directoryexpansion.Record{}, "", permanent("SailPoint returned a malformed role", nil)
	}
	description, ok := boundedTextAllowTab(role.Description, 2000, false)
	if !ok {
		return directoryexpansion.Record{}, "", permanent("SailPoint returned a malformed role", nil)
	}
	if role.Owner == nil {
		return directoryexpansion.Record{}, "", permanent("SailPoint returned a malformed role owner", nil)
	}
	ownerID, valid := normalizedProviderID(role.Owner.ID)
	if !valid {
		return directoryexpansion.Record{}, "", permanent("SailPoint returned a malformed role owner", nil)
	}
	requestable := false
	if role.Requestable != nil {
		requestable = *role.Requestable
	}
	metadata := map[string]string{"directory-object-kind": "role", "owner-identity-id": ownerID,
		"requestable": strconv.FormatBool(requestable)}
	status := "inactive"
	if role.Enabled != nil && *role.Enabled {
		status = "active"
	}
	return directoryexpansion.Record{SourceRecordID: "role:" + id, Kind: directoryexpansion.RecordGroup,
		GroupName: name, DisplayName: name, Description: description, Status: status, NormalizedMetadata: metadata}, id, nil
}

func normalizeReference(reference sailPointReference) (string, string, error) {
	id, ok := normalizedProviderID(reference.ID)
	if !ok {
		return "", "", errors.New("reference id is invalid")
	}
	displayName, ok := boundedText(firstNonempty(reference.DisplayName, reference.Name, reference.Alias, id), 200, true)
	if !ok {
		return "", "", errors.New("reference display name is invalid")
	}
	return id, displayName, nil
}

func appendRecord(records *[]directoryexpansion.Record, seen map[string]struct{}, record directoryexpansion.Record, maximum int) error {
	if _, duplicate := seen[record.SourceRecordID]; duplicate {
		return permanent("SailPoint response contained duplicate records", nil)
	}
	if len(*records) >= maximum {
		return permanent("SailPoint snapshot exceeds the record limit", nil)
	}
	seen[record.SourceRecordID] = struct{}{}
	*records = append(*records, record)
	return nil
}

func addMembership(records *[]directoryexpansion.Record, seenRecords, seenMemberships map[string]struct{}, memberIndexes map[string]int,
	groupID, memberID, displayName, status string, metadata map[string]string, count *int, maximumMembers, maximumRecords int) error {
	key := groupID + "\x00" + memberID
	if _, duplicate := seenMemberships[key]; duplicate {
		return permanent("SailPoint response contained duplicate memberships", nil)
	}
	seenMemberships[key] = struct{}{}
	*count = *count + 1
	if *count > maximumMembers {
		return permanent("SailPoint snapshot exceeds the membership limit", nil)
	}
	record := directoryexpansion.Record{SourceRecordID: "membership:" + shortDigest(key), Kind: directoryexpansion.RecordMembership,
		DisplayName: displayName, Status: status, GroupSourceID: groupID, MemberSourceID: memberID,
		MemberKind: directoryexpansion.MemberSubject, NormalizedMetadata: metadata}
	if err := appendRecord(records, seenRecords, record, maximumRecords); err != nil {
		return err
	}
	if index, ok := memberIndexes[memberID]; ok {
		(*records)[index].GroupSourceIDs = append((*records)[index].GroupSourceIDs, groupID)
	}
	return nil
}

func recordKindOrder(kind directoryexpansion.RecordKind) int {
	switch kind {
	case directoryexpansion.RecordIdentity:
		return 0
	case directoryexpansion.RecordGroup:
		return 1
	case directoryexpansion.RecordMembership:
		return 2
	default:
		return 3
	}
}

func validateConfig(configuration Config, override string) (Config, *url.URL, error) {
	configuration.SourceSystemID = strings.TrimSpace(configuration.SourceSystemID)
	if configuration.SourceSystemID == "" {
		configuration.SourceSystemID = defaultSourceSystemID
	}
	configuration.BaseURL = strings.TrimRight(strings.TrimSpace(configuration.BaseURL), "/")
	configuration.ClientID = strings.TrimSpace(configuration.ClientID)
	if !sourceSystemIDPattern.MatchString(configuration.SourceSystemID) ||
		!clientIDPattern.MatchString(configuration.ClientID) || !validSecret(configuration.ClientSecret) {
		return Config{}, nil, errors.New("SailPoint endpoint and client credential configuration is invalid")
	}
	production, err := validateProductionEndpoint(configuration.BaseURL)
	if err != nil {
		return Config{}, nil, errors.New("SailPoint endpoint and client credential configuration is invalid")
	}
	endpoint := production
	if strings.TrimSpace(override) != "" {
		endpoint, err = validateLoopbackEndpoint(override)
		if err != nil {
			return Config{}, nil, errors.New("SailPoint test endpoint is invalid")
		}
	}
	return configuration, endpoint, nil
}

func validateProductionEndpoint(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.RawQuery != "" || parsed.ForceQuery ||
		parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") || parsed.RawPath != "" {
		return nil, errors.New("invalid endpoint")
	}
	host := strings.ToLower(parsed.Hostname())
	const suffix = ".api.identitynow.com"
	if !strings.HasSuffix(host, suffix) || !tenantPattern.MatchString(strings.TrimSuffix(host, suffix)) {
		return nil, errors.New("endpoint is not a SailPoint tenant API host")
	}
	parsed.Path = ""
	return parsed, nil
}

func validateLoopbackEndpoint(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery ||
		parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") || !isLoopback(parsed.Hostname()) {
		return nil, errors.New("invalid loopback endpoint")
	}
	parsed.Path = ""
	return parsed, nil
}

func revisionFor(configuration Config) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{"sailpoint-v2025", strings.ToLower(configuration.BaseURL),
		strings.ToLower(configuration.ClientID), configuration.SourceSystemID}, "\x00")))
	return "sailpoint-" + hex.EncodeToString(sum[:8])
}

func attributeString(attributes map[string]json.RawMessage, name string) (string, error) {
	var matched json.RawMessage
	found := false
	for key, raw := range attributes {
		if !strings.EqualFold(key, name) {
			continue
		}
		if found {
			return "", errors.New("attribute name is ambiguous")
		}
		found = true
		matched = raw
	}
	if !found || len(matched) == 0 || bytes.Equal(bytes.TrimSpace(matched), []byte("null")) {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(matched, &value); err != nil {
		return "", err
	}
	return value, nil
}

func firstAttributeString(attributes map[string]json.RawMessage, names ...string) (string, error) {
	for _, name := range names {
		value, err := attributeString(attributes, name)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(value) != "" {
			return value, nil
		}
	}
	return "", nil
}

func identityStatus(status, cloudStatus, lifecycle string, enabled, inactive, disabled, locked *bool) string {
	if enabled != nil && !*enabled || inactive != nil && *inactive || disabled != nil && *disabled || locked != nil && *locked {
		return "inactive"
	}
	for _, value := range []string{status, cloudStatus, lifecycle} {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "inactive", "inactive_short_term", "inactive_long_term", "disabled", "deactivated", "deleted", "terminated", "suspended", "locked", "error":
			return "inactive"
		}
	}
	return "active"
}

func normalizedProviderID(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	return value, providerIDPattern.MatchString(value)
}

func validSecret(value string) bool {
	if len(value) < 16 || len(value) > 4096 || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func validBearerToken(value string) bool {
	return len(value) >= 1 && len(value) <= 16384 && bearerTokenPattern.MatchString(value)
}

func boundedText(value string, maximum int, required bool) (string, bool) {
	value = strings.TrimSpace(value)
	if (required && value == "") || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maximum {
		return "", false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", false
		}
	}
	return value, true
}

func boundedTextAllowTab(value string, maximum int, required bool) (string, bool) {
	value = strings.TrimSpace(value)
	if (required && value == "") || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maximum {
		return "", false
	}
	for _, character := range value {
		if unicode.IsControl(character) && character != '\t' {
			return "", false
		}
	}
	return value, true
}

func normalizedEmail(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "", true
	}
	address, err := mail.ParseAddress(value)
	return value, err == nil && address.Address == value && len(value) <= 320
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func uniqueSorted(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	sort.Strings(values)
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func shortDigest(values ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(sum[:16])
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusInternalServerError || status == http.StatusBadGateway ||
		status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

func retryDelay(attempt int, retryAfter string, now time.Time) time.Duration {
	delay := time.Duration(1<<attempt) * 100 * time.Millisecond
	if seconds, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && seconds >= 0 {
		delay = time.Duration(seconds) * time.Second
	} else if parsed, err := http.ParseTime(strings.TrimSpace(retryAfter)); err == nil && parsed.After(now) {
		delay = parsed.Sub(now)
	}
	if delay > maximumRetryDelay {
		return maximumRetryDelay
	}
	return delay
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func permanent(message string, cause error) error {
	return &directoryexpansion.ClassifiedError{Class: directoryexpansion.FailurePermanent, Message: message, Cause: cause}
}

func transient(message string, cause error) error {
	return &directoryexpansion.ClassifiedError{Class: directoryexpansion.FailureTransient, Retryable: true, Message: message, Cause: cause}
}
