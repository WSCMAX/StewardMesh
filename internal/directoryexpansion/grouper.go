package directoryexpansion

// Requirement: REQ-DIRECTORY-EXPANSION-005. Feature: integrations.protocols.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	GrouperProvider             Provider = "grouper"
	DefaultGrouperPageSize               = 100
	MaximumGrouperPageSize               = 250
	DefaultGrouperResponseBytes int64    = 2 << 20
	MaximumGrouperResponseBytes int64    = 8 << 20
	DefaultGrouperTimeout                = 15 * time.Second
	MaximumGrouperTimeout                = 30 * time.Second
	MaximumGrouperRetries                = 3
)

type GrouperConnectorConfig struct {
	SourceSystemID       string
	BaseURL              string
	Username             string
	Password             string
	BearerToken          string
	ConfigRevision       string
	PageSize             int
	MaximumResponseBytes int64
	Timeout              time.Duration
	AllowPrivateNetwork  bool
	HTTPClient           *http.Client
	RetryDelay           func(context.Context, int) error
}

type GrouperConnector struct {
	system       SourceSystem
	baseURL      *url.URL
	username     string
	password     string
	bearerToken  string
	pageSize     int
	maximumBytes int64
	client       *http.Client
	retryDelay   func(context.Context, int) error
}

func NewGrouperConnector(config GrouperConnectorConfig) (*GrouperConnector, error) {
	config.SourceSystemID = strings.TrimSpace(config.SourceSystemID)
	config.ConfigRevision = strings.TrimSpace(config.ConfigRevision)
	config.Username = strings.TrimSpace(config.Username)
	if config.ConfigRevision == "" {
		config.ConfigRevision = "v1"
	}
	if config.PageSize == 0 {
		config.PageSize = DefaultGrouperPageSize
	}
	if config.MaximumResponseBytes == 0 {
		config.MaximumResponseBytes = DefaultGrouperResponseBytes
	}
	if config.Timeout == 0 {
		config.Timeout = DefaultGrouperTimeout
	}
	if !sourceSystemIDPattern.MatchString(config.SourceSystemID) || !sourceSystemIDPattern.MatchString(config.ConfigRevision) ||
		config.PageSize < 1 || config.PageSize > MaximumGrouperPageSize ||
		config.MaximumResponseBytes < 1024 || config.MaximumResponseBytes > MaximumGrouperResponseBytes ||
		config.Timeout < time.Second || config.Timeout > MaximumGrouperTimeout ||
		(config.BearerToken != "" && (config.Username != "" || config.Password != "")) ||
		(config.BearerToken == "" && (config.Username == "") != (config.Password == "")) ||
		!validGrouperHeaderValue(config.Username, 256) || !validGrouperHeaderValue(config.Password, 4096) ||
		!validGrouperHeaderValue(config.BearerToken, 4096) {
		return nil, errors.New("Grouper connector configuration is invalid")
	}
	baseURL, err := validateGrouperBaseURL(config.BaseURL, config.AllowPrivateNetwork)
	if err != nil {
		return nil, err
	}
	client := config.HTTPClient
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		transport.DialContext = guardedDialer(config.AllowPrivateNetwork).DialContext
		client = &http.Client{Transport: transport, Timeout: config.Timeout}
	} else if client.Timeout <= 0 || client.Timeout > MaximumGrouperTimeout {
		return nil, errors.New("Grouper HTTP client timeout is invalid")
	}
	clientCopy := *client
	clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	retryDelay := config.RetryDelay
	if retryDelay == nil {
		retryDelay = func(ctx context.Context, attempt int) error {
			delay := time.Duration(attempt) * 100 * time.Millisecond
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		}
	}
	return &GrouperConnector{system: SourceSystem{ID: config.SourceSystemID, Provider: GrouperProvider, ConfigRevision: config.ConfigRevision},
		baseURL: baseURL, username: config.Username, password: config.Password, bearerToken: config.BearerToken,
		pageSize: config.PageSize, maximumBytes: config.MaximumResponseBytes, client: &clientCopy, retryDelay: retryDelay}, nil
}

func (c *GrouperConnector) SourceSystem() SourceSystem { return c.system }

func (c *GrouperConnector) PullPage(ctx context.Context, cursor string) (Page, error) {
	startIndex := 1
	if cursor != "" {
		if !strings.HasPrefix(cursor, "groups:") {
			return Page{}, &ClassifiedError{Class: FailurePermanent, Message: "Grouper cursor is invalid", Cause: ErrInvalidInput}
		}
		parsed, err := strconv.Atoi(strings.TrimPrefix(cursor, "groups:"))
		if err != nil || parsed < 1 {
			return Page{}, &ClassifiedError{Class: FailurePermanent, Message: "Grouper cursor is invalid", Cause: ErrInvalidInput}
		}
		startIndex = parsed
	}
	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/Groups"
	query := endpoint.Query()
	query.Set("startIndex", strconv.Itoa(startIndex))
	query.Set("count", strconv.Itoa(c.pageSize))
	endpoint.RawQuery = query.Encode()
	response, err := c.get(ctx, endpoint.String())
	if err != nil {
		return Page{}, err
	}
	records, next, complete, err := translateGrouperPage(response, startIndex, c.pageSize)
	if err != nil {
		return Page{}, &ClassifiedError{Class: FailurePermanent, Message: "Grouper returned an invalid response", Cause: err}
	}
	return Page{Records: records, NextCursor: next, CompleteSnapshot: complete}, nil
}

func (c *GrouperConnector) get(ctx context.Context, endpoint string) ([]byte, error) {
	for attempt := 1; attempt <= MaximumGrouperRetries; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, &ClassifiedError{Class: FailurePermanent, Message: "Grouper request could not be created", Cause: err}
		}
		request.Header.Set("Accept", "application/scim+json, application/json")
		request.Header.Set("User-Agent", "StewardMesh-Grouper/1")
		if c.bearerToken != "" {
			request.Header.Set("Authorization", "Bearer "+c.bearerToken)
		} else if c.username != "" {
			request.SetBasicAuth(c.username, c.password)
		}
		response, err := c.client.Do(request)
		if err != nil {
			if attempt < MaximumGrouperRetries {
				if delayErr := c.retryDelay(ctx, attempt); delayErr != nil {
					return nil, &ClassifiedError{Class: FailureTransient, Retryable: true, Message: "Grouper request was interrupted", Cause: delayErr}
				}
				continue
			}
			return nil, &ClassifiedError{Class: FailureTransient, Retryable: true, Message: "Grouper is temporarily unavailable", Cause: err}
		}
		body, readErr := readBoundedResponse(response.Body, c.maximumBytes)
		response.Body.Close()
		if readErr != nil {
			return nil, &ClassifiedError{Class: FailurePermanent, Message: "Grouper response exceeded its safe limit", Cause: readErr}
		}
		if response.StatusCode == http.StatusOK {
			return body, nil
		}
		if isTransientGrouperStatus(response.StatusCode) && attempt < MaximumGrouperRetries {
			if delayErr := c.retryDelay(ctx, attempt); delayErr != nil {
				return nil, &ClassifiedError{Class: FailureTransient, Retryable: true, Message: "Grouper request was interrupted", Cause: delayErr}
			}
			continue
		}
		if isTransientGrouperStatus(response.StatusCode) {
			return nil, &ClassifiedError{Class: FailureTransient, Retryable: true, Message: "Grouper is temporarily unavailable", Cause: fmt.Errorf("Grouper status %d", response.StatusCode)}
		}
		return nil, &ClassifiedError{Class: FailurePermanent, Message: "Grouper rejected the read-only request", Cause: fmt.Errorf("Grouper status %d", response.StatusCode)}
	}
	return nil, &ClassifiedError{Class: FailureTransient, Retryable: true, Message: "Grouper is temporarily unavailable"}
}

type grouperListResponse struct {
	TotalResults int            `json:"totalResults"`
	StartIndex   int            `json:"startIndex"`
	ItemsPerPage int            `json:"itemsPerPage"`
	Resources    []grouperGroup `json:"Resources"`
}

type grouperGroup struct {
	ID          string            `json:"id"`
	ExternalID  string            `json:"externalId"`
	DisplayName string            `json:"displayName"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Active      *bool             `json:"active"`
	Metadata    map[string]string `json:"stewardMetadata"`
	Members     []grouperMember   `json:"members"`
}

type grouperMember struct {
	Value     string            `json:"value"`
	Reference string            `json:"$ref"`
	Display   string            `json:"display"`
	Type      string            `json:"type"`
	Active    *bool             `json:"active"`
	Metadata  map[string]string `json:"stewardMetadata"`
}

func translateGrouperPage(body []byte, requestedStart, pageSize int) ([]Record, string, bool, error) {
	var response grouperListResponse
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&response); err != nil {
		return nil, "", false, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, "", false, err
	}
	if response.TotalResults < 0 || response.StartIndex != requestedStart || response.ItemsPerPage < 0 ||
		response.ItemsPerPage > pageSize || len(response.Resources) > pageSize ||
		(response.TotalResults > 0 && len(response.Resources) == 0 && requestedStart <= response.TotalResults) {
		return nil, "", false, errors.New("Grouper pagination metadata is inconsistent")
	}
	records := make([]Record, 0, len(response.Resources)*2)
	for _, group := range response.Resources {
		groupID := strings.TrimSpace(group.ID)
		if groupID == "" {
			groupID = strings.TrimSpace(group.ExternalID)
		}
		name := strings.TrimSpace(group.Name)
		if name == "" {
			name = strings.TrimSpace(group.DisplayName)
		}
		displayName := strings.TrimSpace(group.DisplayName)
		if displayName == "" {
			displayName = name
		}
		status := grouperStatus(group.Active)
		records = append(records, Record{SourceRecordID: groupID, Kind: RecordGroup, GroupName: name,
			DisplayName: displayName, Description: group.Description, Status: status, NormalizedMetadata: group.Metadata})
		for _, member := range group.Members {
			memberID := strings.TrimSpace(member.Value)
			memberKind := MemberSubject
			if strings.EqualFold(strings.TrimSpace(member.Type), "group") || strings.Contains(strings.ToLower(member.Reference), "/groups/") {
				memberKind = MemberGroup
			}
			memberDisplay := strings.TrimSpace(member.Display)
			if memberDisplay == "" {
				memberDisplay = memberID
			}
			records = append(records, Record{SourceRecordID: grouperMembershipSourceID(groupID, memberID, memberKind),
				Kind: RecordMembership, DisplayName: memberDisplay, Status: grouperStatus(member.Active),
				GroupSourceID: groupID, MemberSourceID: memberID, MemberKind: memberKind, NormalizedMetadata: member.Metadata})
		}
	}
	consumed := requestedStart - 1 + len(response.Resources)
	if consumed > response.TotalResults {
		return nil, "", false, errors.New("Grouper result count is inconsistent")
	}
	complete := consumed >= response.TotalResults
	next := ""
	if !complete {
		next = "groups:" + strconv.Itoa(consumed+1)
	}
	return records, next, complete, nil
}

func grouperMembershipSourceID(groupID, memberID string, memberKind MemberKind) string {
	return "membership:" + digestStrings(groupID, string(memberKind), memberID)[:32]
}

func grouperStatus(active *bool) string {
	if active != nil && !*active {
		return "inactive"
	}
	return "active"
}

func readBoundedResponse(body io.Reader, maximum int64) ([]byte, error) {
	limited := &io.LimitedReader{R: body, N: maximum + 1}
	result, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(result)) > maximum {
		return nil, errors.New("response is too large")
	}
	return result, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("response contains multiple JSON values")
}

func validateGrouperBaseURL(raw string, allowPrivate bool) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) == 0 || len(raw) > 2048 || !utf8.ValidString(raw) {
		return nil, errors.New("Grouper URL must be an HTTPS SCIM base URL without credentials, query, or fragment")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("Grouper URL must be an HTTPS SCIM base URL without credentials, query, or fragment")
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	loopback := strings.EqualFold(host, "localhost") || ip != nil && ip.IsLoopback()
	if parsed.Scheme != "https" && !(allowPrivate && parsed.Scheme == "http" && loopback) {
		return nil, errors.New("Grouper URL must use HTTPS except for explicit loopback development")
	}
	if strings.TrimRight(parsed.EscapedPath(), "/") != "/grouper-ws/scim/v2" || parsed.RawPath != "" {
		return nil, errors.New("Grouper URL path must be /grouper-ws/scim/v2")
	}
	if ip != nil && !isAllowedGrouperIP(ip, allowPrivate) {
		return nil, errors.New("Grouper URL address is not permitted")
	}
	parsed.Path = "/grouper-ws/scim/v2"
	return parsed, nil
}

func guardedDialer(allowPrivate bool) *net.Dialer {
	return &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second, ControlContext: func(_ context.Context, _, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return errors.New("Grouper network address is invalid")
		}
		ip := net.ParseIP(host)
		if !isAllowedGrouperIP(ip, allowPrivate) {
			return errors.New("Grouper resolved to an address that is not permitted")
		}
		return nil
	}}
}

func isAllowedGrouperIP(ip net.IP, allowPrivate bool) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	if ip.IsLoopback() {
		return allowPrivate
	}
	if !ip.IsGlobalUnicast() {
		return false
	}
	if ip.IsPrivate() {
		return allowPrivate
	}
	return true
}

func isTransientGrouperStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

func validGrouperHeaderValue(value string, maximumBytes int) bool {
	if len(value) > maximumBytes || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
