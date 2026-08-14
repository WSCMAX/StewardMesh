// Package entra provides StewardMesh's optional read-only Microsoft Entra ID
// directory connector.
// Requirements: REQ-DIRECTORY-EXPANSION-003, REQ-DIRECTORY-EXPANSION-009.
// Features: identity.directory, experience.help.
package entra

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
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

const (
	RequirementID = "REQ-DIRECTORY-EXPANSION-003"
	FeatureID     = "identity.directory"

	defaultSourceSystemID = "entra"
	defaultGraphBaseURL   = "https://graph.microsoft.com/v1.0"
	defaultHTTPTimeout    = 15 * time.Second
	maximumHTTPTimeout    = 30 * time.Second
	maximumResponseBytes  = 2 << 20
	maximumGraphPages     = 100
	maximumGraphRecords   = 5000
	maximumMemberships    = 20000
	maximumRetries        = 3
	maximumRetryDelay     = 2 * time.Second
)

var (
	uuidPattern           = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
	sourceSystemIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
)

// Config contains deployment-only Microsoft Entra application credentials.
// It must never be serialized or returned by the application API.
type Config struct {
	SourceSystemID string
	TenantID       string
	ClientID       string
	ClientSecret   string
}

// Options provides bounded transport seams for deterministic tests. Production
// callers should use the zero value.
type Options struct {
	HTTPClient     *http.Client
	Sleep          func(context.Context, time.Duration) error
	Now            func() time.Time
	MaximumPages   int
	MaximumRecords int
	MaximumMembers int
	MaximumRetries int
	graphBaseURL   string
	tokenURL       string
}

type Connector struct {
	system         directoryexpansion.SourceSystem
	graphBaseURL   *url.URL
	credentials    clientcredentials.Config
	httpClient     *http.Client
	sleep          func(context.Context, time.Duration) error
	now            func() time.Time
	maximumPages   int
	maximumRecords int
	maximumMembers int
	maximumRetries int
}

var _ directoryexpansion.Connector = (*Connector)(nil)

func ValidateConfig(configuration Config) error {
	_, _, _, err := validateConfig(configuration, "", "")
	return err
}

func NewConnector(configuration Config, options Options) (*Connector, error) {
	normalized, graphURL, tokenURL, err := validateConfig(configuration, options.graphBaseURL, options.tokenURL)
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
	transport := client.Transport
	if transport == nil {
		defaultTransport, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			return nil, errors.New("Microsoft Entra HTTP transport is unavailable")
		}
		transport = defaultTransport.Clone()
		// Directory credentials and access tokens must not inherit HTTP(S)_PROXY.
		// The tenant token endpoint and Microsoft Graph hosts are fixed by
		// validated server configuration, so there is no legitimate ambient
		// proxy destination in the production path.
		transport.(*http.Transport).Proxy = nil
	}
	client.Transport = boundedResponseTransport{base: transport}
	// OAuth and Graph redirects are never followed. This keeps bearer tokens
	// pinned to the validated Microsoft hosts (or an explicit loopback test
	// server) and prevents next-link redirect based SSRF.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if options.Sleep == nil {
		options.Sleep = sleepContext
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	if options.MaximumPages == 0 {
		options.MaximumPages = maximumGraphPages
	}
	if options.MaximumRecords == 0 {
		options.MaximumRecords = maximumGraphRecords
	}
	if options.MaximumMembers == 0 {
		options.MaximumMembers = maximumMemberships
	}
	if options.MaximumRetries == 0 {
		options.MaximumRetries = maximumRetries
	}
	if options.MaximumPages < 1 || options.MaximumPages > maximumGraphPages ||
		options.MaximumRecords < 1 || options.MaximumRecords > maximumGraphRecords ||
		options.MaximumMembers < 1 || options.MaximumMembers > maximumMemberships ||
		options.MaximumRetries < 1 || options.MaximumRetries > maximumRetries {
		return nil, errors.New("Microsoft Entra connector bounds are invalid")
	}
	revision := revisionFor(normalized, graphURL.String())
	return &Connector{
		system:       directoryexpansion.SourceSystem{ID: normalized.SourceSystemID, Provider: directoryexpansion.EntraProvider, ConfigRevision: revision},
		graphBaseURL: graphURL,
		credentials: clientcredentials.Config{
			ClientID: normalized.ClientID, ClientSecret: normalized.ClientSecret, TokenURL: tokenURL.String(),
			Scopes: []string{"https://graph.microsoft.com/.default"}, AuthStyle: oauth2.AuthStyleInParams,
		},
		httpClient: client, sleep: options.Sleep, now: options.Now,
		maximumPages: options.MaximumPages, maximumRecords: options.MaximumRecords,
		maximumMembers: options.MaximumMembers, maximumRetries: options.MaximumRetries,
	}, nil
}

func (c *Connector) SourceSystem() directoryexpansion.SourceSystem { return c.system }

// PullPage performs one bounded complete Graph snapshot. Microsoft Graph's own
// @odata.nextLink pagination is followed internally so records can be sorted
// deterministically and group memberships can be folded into their member
// identity records before the provider-neutral plan is persisted.
func (c *Connector) PullPage(ctx context.Context, cursor string) (directoryexpansion.Page, error) {
	if c == nil || strings.TrimSpace(cursor) != "" {
		return directoryexpansion.Page{}, permanent("Microsoft Entra connector cursor is invalid", nil)
	}
	if ctx == nil {
		return directoryexpansion.Page{}, permanent("Microsoft Entra request context is required", nil)
	}
	tokenContext := context.WithValue(ctx, oauth2.HTTPClient, c.httpClient)
	token, err := c.credentials.Token(tokenContext)
	if err != nil || token == nil || !token.Valid() || !strings.EqualFold(token.TokenType, "bearer") || len(token.AccessToken) > 16384 {
		return directoryexpansion.Page{}, permanent("Microsoft Entra credentials or tenant configuration were rejected", err)
	}
	pageCount := 0
	users, err := collect[graphUser](ctx, c, token.AccessToken, c.collectionURL("users", userSelect), &pageCount, c.maximumRecords)
	if err != nil {
		return directoryexpansion.Page{}, err
	}
	groups, err := collect[graphGroup](ctx, c, token.AccessToken, c.collectionURL("groups", groupSelect), &pageCount, c.maximumRecords)
	if err != nil {
		return directoryexpansion.Page{}, err
	}
	if len(users)+len(groups) > c.maximumRecords {
		return directoryexpansion.Page{}, permanent("Microsoft Entra snapshot exceeds the record limit", nil)
	}

	records := make([]directoryexpansion.Record, 0, len(users)+len(groups))
	memberIndexes := make(map[string]int, len(users)+len(groups))
	seenProviderIDs := make(map[string]struct{}, len(users)+len(groups))
	for _, user := range users {
		record, err := normalizeUser(user)
		if err != nil {
			return directoryexpansion.Page{}, err
		}
		if _, duplicate := seenProviderIDs[user.ID]; duplicate {
			return directoryexpansion.Page{}, permanent("Microsoft Entra response contained duplicate directory object identifiers", nil)
		}
		seenProviderIDs[user.ID] = struct{}{}
		memberIndexes[user.ID] = len(records)
		records = append(records, record)
	}
	groupIDs := make([]string, 0, len(groups))
	for _, group := range groups {
		record, err := normalizeGroup(group)
		if err != nil {
			return directoryexpansion.Page{}, err
		}
		if _, duplicate := seenProviderIDs[group.ID]; duplicate {
			return directoryexpansion.Page{}, permanent("Microsoft Entra response contained duplicate directory object identifiers", nil)
		}
		seenProviderIDs[group.ID] = struct{}{}
		memberIndexes[group.ID] = len(records)
		groupIDs = append(groupIDs, group.ID)
		records = append(records, record)
	}
	sort.Strings(groupIDs)
	membershipCount := 0
	seenMemberships := make(map[string]struct{})
	for _, groupID := range groupIDs {
		members, err := collect[graphMember](ctx, c, token.AccessToken, c.membersURL(groupID), &pageCount, c.maximumMembers)
		if err != nil {
			return directoryexpansion.Page{}, err
		}
		for _, member := range members {
			if !uuidPattern.MatchString(member.ID) {
				return directoryexpansion.Page{}, permanent("Microsoft Entra response contained a malformed membership", nil)
			}
			membershipKey := groupID + "\x00" + member.ID
			if _, duplicate := seenMemberships[membershipKey]; duplicate {
				return directoryexpansion.Page{}, permanent("Microsoft Entra response contained duplicate memberships", nil)
			}
			seenMemberships[membershipKey] = struct{}{}
			membershipCount++
			if membershipCount > c.maximumMembers {
				return directoryexpansion.Page{}, permanent("Microsoft Entra snapshot exceeds the membership limit", nil)
			}
			index, supported := memberIndexes[member.ID]
			if !supported {
				// Service principals, devices, and contacts are outside this
				// identity-directory slice. Their identifiers are not retained.
				continue
			}
			records[index].GroupSourceIDs = append(records[index].GroupSourceIDs, "group:"+strings.ToLower(groupID))
			if len(records[index].GroupSourceIDs) > directoryexpansion.MaximumGroupLinks {
				return directoryexpansion.Page{}, permanent("Microsoft Entra directory object exceeds the group membership limit", nil)
			}
		}
	}
	for index := range records {
		sort.Strings(records[index].GroupSourceIDs)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].SourceRecordID < records[j].SourceRecordID })
	return directoryexpansion.Page{Records: records, CompleteSnapshot: true}, nil
}

const (
	userSelect  = "id,displayName,mail,userPrincipalName,accountEnabled,department,jobTitle,officeLocation,userType"
	groupSelect = "id,displayName,mail,mailEnabled,securityEnabled"
)

type graphUser struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName"`
	Mail              string `json:"mail"`
	UserPrincipalName string `json:"userPrincipalName"`
	AccountEnabled    *bool  `json:"accountEnabled"`
	Department        string `json:"department"`
	JobTitle          string `json:"jobTitle"`
	OfficeLocation    string `json:"officeLocation"`
	UserType          string `json:"userType"`
}

type graphGroup struct {
	ID              string `json:"id"`
	DisplayName     string `json:"displayName"`
	Mail            string `json:"mail"`
	MailEnabled     *bool  `json:"mailEnabled"`
	SecurityEnabled *bool  `json:"securityEnabled"`
}

type graphMember struct {
	ID string `json:"id"`
}

type graphEnvelope struct {
	Value    json.RawMessage `json:"value"`
	NextLink string          `json:"@odata.nextLink"`
}

type boundedResponseTransport struct{ base http.RoundTripper }

func (transport boundedResponseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	response.Body = limitedReadCloser{Reader: io.LimitReader(response.Body, maximumResponseBytes+1), Closer: response.Body}
	return response, nil
}

type limitedReadCloser struct {
	io.Reader
	io.Closer
}

func collect[T any](ctx context.Context, connector *Connector, token, firstURL string, pageCount *int, maximumItems int) ([]T, error) {
	result := make([]T, 0)
	next := firstURL
	first, err := url.Parse(firstURL)
	if err != nil || first.Path == "" {
		return nil, permanent("Microsoft Entra collection endpoint is invalid", err)
	}
	allowedPath := first.Path
	seenLinks := map[string]struct{}{firstURL: {}}
	for next != "" {
		(*pageCount)++
		if *pageCount > connector.maximumPages {
			return nil, permanent("Microsoft Entra snapshot exceeds the page limit", nil)
		}
		var envelope graphEnvelope
		if err := connector.get(ctx, token, next, &envelope); err != nil {
			return nil, err
		}
		if len(envelope.Value) == 0 || string(envelope.Value) == "null" {
			return nil, permanent("Microsoft Entra returned a malformed collection response", nil)
		}
		var values []T
		if err := json.Unmarshal(envelope.Value, &values); err != nil || values == nil {
			return nil, permanent("Microsoft Entra returned a malformed collection response", err)
		}
		result = append(result, values...)
		if len(result) > maximumItems {
			return nil, permanent("Microsoft Entra collection exceeds the item limit", nil)
		}
		next = strings.TrimSpace(envelope.NextLink)
		if next != "" {
			validated, err := connector.validateGraphURL(next, allowedPath)
			if err != nil {
				return nil, err
			}
			if _, duplicate := seenLinks[validated]; duplicate {
				return nil, permanent("Microsoft Entra returned a repeated pagination link", nil)
			}
			seenLinks[validated] = struct{}{}
			next = validated
		}
	}
	return result, nil
}

func (c *Connector) get(ctx context.Context, token, requestURL string, destination any) error {
	for attempt := 0; attempt < c.maximumRetries; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return permanent("Microsoft Entra request could not be constructed", err)
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Authorization", "Bearer "+token)
		response, err := c.httpClient.Do(request)
		if err != nil {
			if attempt+1 < c.maximumRetries {
				if sleepErr := c.sleep(ctx, retryDelay(attempt, "", c.now())); sleepErr != nil {
					return transient("Microsoft Entra request was canceled", sleepErr)
				}
				continue
			}
			return transient("Microsoft Entra could not be reached after bounded retries", err)
		}
		status := response.StatusCode
		if status == http.StatusOK {
			err := decodeResponse(response, destination)
			if err != nil {
				return permanent("Microsoft Entra returned a malformed response", err)
			}
			return nil
		}
		retryAfter := response.Header.Get("Retry-After")
		_, _ = io.CopyN(io.Discard, response.Body, 4096)
		_ = response.Body.Close()
		if retryableStatus(status) && attempt+1 < c.maximumRetries {
			if sleepErr := c.sleep(ctx, retryDelay(attempt, retryAfter, c.now())); sleepErr != nil {
				return transient("Microsoft Entra request was canceled", sleepErr)
			}
			continue
		}
		switch status {
		case http.StatusUnauthorized, http.StatusForbidden:
			return permanent("Microsoft Entra credentials or application permissions were rejected", nil)
		case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout,
			http.StatusInternalServerError:
			return transient("Microsoft Entra remained unavailable after bounded retries", nil)
		default:
			return permanent("Microsoft Entra returned an unexpected response", nil)
		}
	}
	return transient("Microsoft Entra remained unavailable after bounded retries", nil)
}

func decodeResponse(response *http.Response, destination any) error {
	defer response.Body.Close()
	if response.ContentLength > maximumResponseBytes {
		return errors.New("response exceeds limit")
	}
	reader := io.LimitReader(response.Body, maximumResponseBytes+1)
	data, err := io.ReadAll(reader)
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

func (c *Connector) collectionURL(collection, selectFields string) string {
	query := url.Values{"$select": {selectFields}, "$top": {"999"}}
	return c.graphBaseURL.String() + "/" + collection + "?" + query.Encode()
}

func (c *Connector) membersURL(groupID string) string {
	query := url.Values{"$select": {"id"}, "$top": {"999"}}
	return c.graphBaseURL.String() + "/groups/" + url.PathEscape(groupID) + "/members?" + query.Encode()
}

func (c *Connector) validateGraphURL(raw, allowedPath string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || parsed.Fragment != "" || parsed.Scheme != c.graphBaseURL.Scheme ||
		!strings.EqualFold(parsed.Host, c.graphBaseURL.Host) ||
		parsed.Path != allowedPath ||
		(parsed.Path != c.graphBaseURL.Path && !strings.HasPrefix(parsed.Path, c.graphBaseURL.Path+"/")) {
		return "", permanent("Microsoft Entra returned an unsafe pagination link", err)
	}
	return parsed.String(), nil
}

func normalizeUser(user graphUser) (directoryexpansion.Record, error) {
	if !uuidPattern.MatchString(user.ID) || user.AccountEnabled == nil {
		return directoryexpansion.Record{}, permanent("Microsoft Entra returned a malformed user record", nil)
	}
	displayName, ok := boundedText(user.DisplayName, 200, true)
	if !ok {
		return directoryexpansion.Record{}, permanent("Microsoft Entra returned a malformed user record", nil)
	}
	email, ok := normalizedEmail(user.Mail)
	if !ok || email == "" {
		email, ok = normalizedEmail(user.UserPrincipalName)
	}
	if !ok || email == "" {
		return directoryexpansion.Record{}, permanent("Microsoft Entra user is missing a valid mail or user principal name", nil)
	}
	department, ok := boundedText(user.Department, 200, false)
	if !ok {
		return directoryexpansion.Record{}, permanent("Microsoft Entra returned a malformed department attribute", nil)
	}
	attributes := map[string]string{}
	for key, candidate := range map[string]string{
		"job-title": user.JobTitle, "office-location": user.OfficeLocation, "user-type": user.UserType,
	} {
		value, valid := boundedText(candidate, 500, false)
		if !valid {
			return directoryexpansion.Record{}, permanent("Microsoft Entra returned a malformed directory attribute", nil)
		}
		if value != "" {
			attributes[key] = value
		}
	}
	status := "inactive"
	if *user.AccountEnabled {
		status = "active"
	}
	return directoryexpansion.Record{
		SourceRecordID: "user:" + strings.ToLower(user.ID), Kind: directoryexpansion.RecordIdentity,
		IdentityKind: "person", DisplayName: displayName, Email: email, Status: status,
		Department: department, DirectoryAttributes: attributes,
	}, nil
}

func normalizeGroup(group graphGroup) (directoryexpansion.Record, error) {
	if !uuidPattern.MatchString(group.ID) || group.MailEnabled == nil || group.SecurityEnabled == nil {
		return directoryexpansion.Record{}, permanent("Microsoft Entra returned a malformed group record", nil)
	}
	displayName, ok := boundedText(group.DisplayName, 200, true)
	if !ok {
		return directoryexpansion.Record{}, permanent("Microsoft Entra returned a malformed group record", nil)
	}
	email, ok := normalizedEmail(group.Mail)
	if !ok {
		return directoryexpansion.Record{}, permanent("Microsoft Entra returned a malformed group mail attribute", nil)
	}
	attributes := map[string]string{
		"mail-enabled": strconv.FormatBool(*group.MailEnabled), "security-enabled": strconv.FormatBool(*group.SecurityEnabled),
	}
	return directoryexpansion.Record{
		SourceRecordID: "group:" + strings.ToLower(group.ID), Kind: directoryexpansion.RecordIdentity,
		IdentityKind: "shared", DisplayName: displayName, Email: email, Status: "active", DirectoryAttributes: attributes,
	}, nil
}

func validateConfig(configuration Config, graphBaseURL, tokenEndpoint string) (Config, *url.URL, *url.URL, error) {
	configuration.SourceSystemID = strings.TrimSpace(configuration.SourceSystemID)
	if configuration.SourceSystemID == "" {
		configuration.SourceSystemID = defaultSourceSystemID
	}
	configuration.TenantID = strings.TrimSpace(configuration.TenantID)
	configuration.ClientID = strings.TrimSpace(configuration.ClientID)
	graphBaseURL = strings.TrimRight(strings.TrimSpace(graphBaseURL), "/")
	if graphBaseURL == "" {
		graphBaseURL = defaultGraphBaseURL
	}
	if !sourceSystemIDPattern.MatchString(configuration.SourceSystemID) || !uuidPattern.MatchString(configuration.TenantID) ||
		!uuidPattern.MatchString(configuration.ClientID) || !validSecret(configuration.ClientSecret) {
		return Config{}, nil, nil, errors.New("Microsoft Entra tenant and application credential configuration is invalid")
	}
	graphURL, err := validateEndpoint(graphBaseURL, "/v1.0", "graph.microsoft.com")
	if err != nil {
		return Config{}, nil, nil, errors.New("Microsoft Entra Graph endpoint configuration is invalid")
	}
	tokenEndpoint = strings.TrimSpace(tokenEndpoint)
	if tokenEndpoint == "" {
		tokenEndpoint = "https://login.microsoftonline.com/" + strings.ToLower(configuration.TenantID) + "/oauth2/v2.0/token"
	}
	tokenURL, err := validateEndpoint(tokenEndpoint, "/"+strings.ToLower(configuration.TenantID)+"/oauth2/v2.0/token", "login.microsoftonline.com")
	if err != nil {
		return Config{}, nil, nil, errors.New("Microsoft Entra token endpoint configuration is invalid")
	}
	return configuration, graphURL, tokenURL, nil
}

func validateEndpoint(raw, expectedPath, expectedHost string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Host == "" {
		return nil, errors.New("invalid endpoint")
	}
	loopback := isLoopback(parsed.Hostname())
	if loopback {
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil, errors.New("invalid loopback scheme")
		}
		if !strings.HasSuffix(parsed.Path, expectedPath) && expectedHost == "graph.microsoft.com" {
			return nil, errors.New("invalid Graph path")
		}
		return parsed, nil
	}
	if parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), expectedHost) || parsed.Port() != "" || parsed.Path != expectedPath {
		return nil, errors.New("endpoint is not allowlisted")
	}
	return parsed, nil
}

func revisionFor(configuration Config, graphURL string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		"entra-v1", strings.ToLower(configuration.TenantID), strings.ToLower(configuration.ClientID),
		configuration.SourceSystemID, graphURL, userSelect, groupSelect,
	}, "\x00")))
	return "entra-" + hex.EncodeToString(sum[:8])
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

func normalizedEmail(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "", true
	}
	address, err := mail.ParseAddress(value)
	return value, err == nil && address.Address == value && len(value) <= 320
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
