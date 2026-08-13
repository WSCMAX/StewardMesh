package directoryexpansion

// Requirement: REQ-DIRECTORY-EXPANSION-006. Feature: integrations.protocols.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	PeopleSoftProvider             Provider = "peoplesoft"
	PeopleSoftRequirementID                 = "REQ-DIRECTORY-EXPANSION-006"
	DefaultPeopleSoftMaximumRows            = 500
	MaximumPeopleSoftRows                   = 1250
	DefaultPeopleSoftResponseBytes int64    = 2 << 20
	MaximumPeopleSoftResponseBytes int64    = 8 << 20
	DefaultPeopleSoftTimeout                = 15 * time.Second
	MaximumPeopleSoftTimeout                = 30 * time.Second
	MaximumPeopleSoftRetries                = 3
)

var (
	peopleSoftQueryPattern    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)
	peopleSoftSelectorPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}\.[A-Za-z][A-Za-z0-9_]{0,63}$`)
	peopleSoftAliasPattern    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_:-]{0,127}$`)
	peopleSoftPathPattern     = regexp.MustCompile(`^/PSIGW/RESTListeningConnector/[A-Za-z0-9_-]{1,64}/ExecuteQuery\.v1$`)
	peopleSoftBearerPattern   = regexp.MustCompile(`^[A-Za-z0-9._~+/=-]+$`)
)

// PeopleSoftConnectorConfig contains deployment-owned Query Access Service
// configuration. FieldMappingsJSON is intentionally server-side and maps each
// institution's query aliases into StewardMesh's bounded normalized model.
type PeopleSoftConnectorConfig struct {
	SourceSystemID       string
	BaseURL              string
	Username             string
	Password             string
	BearerToken          string
	QueryOwner           string
	OrganizationQuery    string
	LocationQuery        string
	BuildingQuery        string
	DepartmentQuery      string
	FieldMappingsJSON    string
	MaximumRows          int
	MaximumResponseBytes int64
	Timeout              time.Duration
	AllowPrivateNetwork  bool
	HTTPClient           *http.Client
	RetryDelay           func(context.Context, int) error
}

type PeopleSoftFieldMappings struct {
	Organization PeopleSoftFieldMapping `json:"organization"`
	Location     PeopleSoftFieldMapping `json:"location"`
	Building     PeopleSoftFieldMapping `json:"building"`
	Department   PeopleSoftFieldMapping `json:"department"`
}

type PeopleSoftFieldMapping struct {
	SetID          PeopleSoftMappedField `json:"setId"`
	ID             PeopleSoftMappedField `json:"id"`
	Name           PeopleSoftMappedField `json:"name"`
	Status         PeopleSoftMappedField `json:"status"`
	Description    PeopleSoftMappedField `json:"description,omitempty"`
	OrganizationID PeopleSoftMappedField `json:"organizationId,omitempty"`
	LocationID     PeopleSoftMappedField `json:"locationId,omitempty"`
	AddressLine1   PeopleSoftMappedField `json:"addressLine1,omitempty"`
	AddressLine2   PeopleSoftMappedField `json:"addressLine2,omitempty"`
	City           PeopleSoftMappedField `json:"city,omitempty"`
	Region         PeopleSoftMappedField `json:"region,omitempty"`
	PostalCode     PeopleSoftMappedField `json:"postalCode,omitempty"`
	Country        PeopleSoftMappedField `json:"country,omitempty"`
	ActiveValues   []string              `json:"activeValues,omitempty"`
	InactiveValues []string              `json:"inactiveValues,omitempty"`
}

// PeopleSoftMappedField separates the QAS field selector from the JSON row
// key. Oracle requires filterfields values such as A.SETID, while JSON/NONFILE
// returns the unqualified alias SETID.
type PeopleSoftMappedField struct {
	Selector string `json:"selector"`
	Alias    string `json:"alias"`
}

type peopleSoftQuery struct {
	kind    string
	name    string
	mapping PeopleSoftFieldMapping
}

type peopleSoftRelation struct {
	setID, parentKind, parentID, childKind, childID, displayName, status string
}

type peopleSoftObject struct {
	sourceID       string
	organizationID string
}

type PeopleSoftConnector struct {
	system       SourceSystem
	baseURL      *url.URL
	username     string
	password     string
	bearerToken  string
	queryOwner   string
	queries      []peopleSoftQuery
	maximumRows  int
	maximumBytes int64
	client       *http.Client
	retryDelay   func(context.Context, int) error
}

var _ Connector = (*PeopleSoftConnector)(nil)

func NewPeopleSoftConnector(config PeopleSoftConnectorConfig) (*PeopleSoftConnector, error) {
	config.SourceSystemID = strings.TrimSpace(config.SourceSystemID)
	config.Username = strings.TrimSpace(config.Username)
	config.QueryOwner = strings.ToLower(strings.TrimSpace(config.QueryOwner))
	if config.QueryOwner == "" {
		config.QueryOwner = "public"
	}
	if config.MaximumRows == 0 {
		config.MaximumRows = DefaultPeopleSoftMaximumRows
	}
	if config.MaximumResponseBytes == 0 {
		config.MaximumResponseBytes = DefaultPeopleSoftResponseBytes
	}
	if config.Timeout == 0 {
		config.Timeout = DefaultPeopleSoftTimeout
	}
	if !sourceSystemIDPattern.MatchString(config.SourceSystemID) ||
		(config.QueryOwner != "public" && config.QueryOwner != "private") ||
		config.MaximumRows < 1 || config.MaximumRows > MaximumPeopleSoftRows ||
		config.MaximumResponseBytes < 1024 || config.MaximumResponseBytes > MaximumPeopleSoftResponseBytes ||
		config.Timeout < time.Second || config.Timeout > MaximumPeopleSoftTimeout ||
		(config.BearerToken != "" && (config.Username != "" || config.Password != "")) ||
		(config.BearerToken == "" && (config.Username == "" || config.Password == "")) ||
		!validPeopleSoftBasicUsername(config.Username) || !validPeopleSoftHeaderValue(config.Password, 4096) ||
		!validPeopleSoftBearer(config.BearerToken) {
		return nil, errors.New("PeopleSoft connector configuration is invalid")
	}
	queryNames := []string{config.OrganizationQuery, config.LocationQuery, config.BuildingQuery, config.DepartmentQuery}
	seenQueries := make(map[string]struct{}, len(queryNames))
	for _, queryName := range queryNames {
		queryName = strings.TrimSpace(queryName)
		if !peopleSoftQueryPattern.MatchString(queryName) {
			return nil, errors.New("PeopleSoft query configuration is invalid")
		}
		key := strings.ToLower(queryName)
		if _, duplicate := seenQueries[key]; duplicate {
			return nil, errors.New("PeopleSoft query configuration is invalid")
		}
		seenQueries[key] = struct{}{}
	}
	mappings, canonicalMappings, err := parsePeopleSoftMappings(config.FieldMappingsJSON)
	if err != nil {
		return nil, errors.New("PeopleSoft field mapping configuration is invalid")
	}
	baseURL, err := validatePeopleSoftBaseURL(config.BaseURL, config.AllowPrivateNetwork)
	if err != nil {
		return nil, err
	}
	client := config.HTTPClient
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		transport.DialContext = peopleSoftGuardedDialer(config.AllowPrivateNetwork).DialContext
		client = &http.Client{Transport: transport, Timeout: config.Timeout}
	} else if client.Timeout <= 0 || client.Timeout > MaximumPeopleSoftTimeout {
		return nil, errors.New("PeopleSoft HTTP client timeout is invalid")
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
	revision := peopleSoftRevision(config, canonicalMappings)
	return &PeopleSoftConnector{
		system:  SourceSystem{ID: config.SourceSystemID, Provider: PeopleSoftProvider, ConfigRevision: revision},
		baseURL: baseURL, username: config.Username, password: config.Password, bearerToken: config.BearerToken,
		queryOwner: config.QueryOwner,
		queries: []peopleSoftQuery{
			{kind: "organization", name: strings.TrimSpace(config.OrganizationQuery), mapping: mappings.Organization},
			{kind: "location", name: strings.TrimSpace(config.LocationQuery), mapping: mappings.Location},
			{kind: "building", name: strings.TrimSpace(config.BuildingQuery), mapping: mappings.Building},
			{kind: "department", name: strings.TrimSpace(config.DepartmentQuery), mapping: mappings.Department},
		},
		maximumRows: config.MaximumRows, maximumBytes: config.MaximumResponseBytes,
		client: &clientCopy, retryDelay: retryDelay,
	}, nil
}

func (c *PeopleSoftConnector) SourceSystem() SourceSystem { return c.system }

// PullPage returns one deterministically ordered composite result. QAS and the
// authenticated user's query-security profile can each impose an undisclosed
// row cap, so the result deliberately remains a partial snapshot and can never
// drive implicit deactivation. The four configured query responses are still
// consumed atomically so hierarchy ownership and duplicates can be validated.
func (c *PeopleSoftConnector) PullPage(ctx context.Context, cursor string) (Page, error) {
	if c == nil || ctx == nil || strings.TrimSpace(cursor) != "" {
		return Page{}, peopleSoftPermanent("PeopleSoft connector cursor is invalid", ErrInvalidInput)
	}
	groups := make([]Record, 0)
	relations := make([]Record, 0)
	known := map[string]map[string]peopleSoftObject{
		"organization": {}, "location": {}, "building": {}, "department": {},
	}
	pending := make([]peopleSoftRelation, 0)
	seenRecords := make(map[string]struct{})
	for _, query := range c.queries {
		rows, err := c.executeQuery(ctx, query)
		if err != nil {
			return Page{}, err
		}
		for _, row := range rows {
			record, relationRows, setID, rawID, organizationID, err := normalizePeopleSoftRow(query.kind, query.mapping, row)
			if err != nil {
				return Page{}, peopleSoftPermanent("PeopleSoft returned an invalid mapped row", err)
			}
			key := peopleSoftScopeKey(setID, rawID)
			if _, duplicate := known[query.kind][key]; duplicate {
				return Page{}, peopleSoftPermanent("PeopleSoft returned a duplicate source record", ErrConflict)
			}
			known[query.kind][key] = peopleSoftObject{sourceID: record.SourceRecordID, organizationID: organizationID}
			if _, duplicate := seenRecords[record.SourceRecordID]; duplicate {
				return Page{}, peopleSoftPermanent("PeopleSoft returned a duplicate source record", ErrConflict)
			}
			seenRecords[record.SourceRecordID] = struct{}{}
			groups = append(groups, record)
			pending = append(pending, relationRows...)
		}
	}
	for _, relation := range pending {
		parent, present := known[relation.parentKind][peopleSoftScopeKey(relation.setID, relation.parentID)]
		if !present {
			return Page{}, peopleSoftPermanent("PeopleSoft returned an unknown hierarchy parent", ErrInvalidInput)
		}
		child, present := known[relation.childKind][peopleSoftScopeKey(relation.setID, relation.childID)]
		if !present {
			return Page{}, peopleSoftPermanent("PeopleSoft returned an unknown hierarchy child", ErrInvalidInput)
		}
		if relation.parentKind == "location" && relation.childKind == "department" &&
			(parent.organizationID == "" || child.organizationID == "" || parent.organizationID != child.organizationID) {
			return Page{}, peopleSoftPermanent("PeopleSoft returned a cross-organization hierarchy relationship", ErrConflict)
		}
		parentSourceID := parent.sourceID
		childSourceID := child.sourceID
		record := Record{
			SourceRecordID: "membership:" + digestStrings(parentSourceID, childSourceID)[:32],
			Kind:           RecordMembership, DisplayName: relation.displayName, Status: relation.status,
			GroupSourceID: parentSourceID, MemberSourceID: childSourceID, MemberKind: MemberGroup,
			NormalizedMetadata: map[string]string{"directory-object-kind": relation.childKind + "-hierarchy"},
		}
		if _, duplicate := seenRecords[record.SourceRecordID]; duplicate {
			return Page{}, peopleSoftPermanent("PeopleSoft returned a duplicate hierarchy relationship", ErrConflict)
		}
		seenRecords[record.SourceRecordID] = struct{}{}
		relations = append(relations, record)
	}
	if len(groups)+len(relations) > MaximumRecords {
		return Page{}, peopleSoftPermanent("PeopleSoft snapshot exceeds the safe record limit", ErrInvalidInput)
	}
	return Page{Records: append(groups, relations...), CompleteSnapshot: false}, nil
}

func (c *PeopleSoftConnector) executeQuery(ctx context.Context, query peopleSoftQuery) ([]map[string]json.RawMessage, error) {
	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/" + c.queryOwner + "/" + query.name + "/JSON/NONFILE"
	values := endpoint.Query()
	values.Set("isconnectedquery", "N")
	values.Set("maxrows", strconv.Itoa(c.maximumRows+1))
	values.Set("filterfields", strings.Join(peopleSoftMappedFields(query.mapping), ","))
	values.Set("json_resp", "true")
	endpoint.RawQuery = values.Encode()
	for attempt := 1; attempt <= MaximumPeopleSoftRetries; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			return nil, peopleSoftPermanent("PeopleSoft request could not be created", err)
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("User-Agent", "StewardMesh-PeopleSoft/1")
		if c.bearerToken != "" {
			request.Header.Set("Authorization", "Bearer "+c.bearerToken)
		} else {
			request.SetBasicAuth(c.username, c.password)
		}
		response, err := c.client.Do(request)
		if err != nil {
			if ctx.Err() != nil {
				return nil, peopleSoftTransient("PeopleSoft request was interrupted", ctx.Err())
			}
			if attempt < MaximumPeopleSoftRetries {
				if delayErr := c.retryDelay(ctx, attempt); delayErr != nil {
					return nil, peopleSoftTransient("PeopleSoft request was interrupted", delayErr)
				}
				continue
			}
			return nil, peopleSoftTransient("PeopleSoft is temporarily unavailable", err)
		}
		body, readErr := readBoundedResponse(response.Body, c.maximumBytes)
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil {
			if errors.Is(readErr, errDirectoryResponseTooLarge) {
				return nil, peopleSoftPermanent("PeopleSoft response exceeded its safe limit", readErr)
			}
			if (response.StatusCode == http.StatusOK || peopleSoftTransientStatus(response.StatusCode)) && attempt < MaximumPeopleSoftRetries {
				if delayErr := c.retryDelay(ctx, attempt); delayErr != nil {
					return nil, peopleSoftTransient("PeopleSoft request was interrupted", delayErr)
				}
				continue
			}
			if response.StatusCode == http.StatusOK || peopleSoftTransientStatus(response.StatusCode) {
				return nil, peopleSoftTransient("PeopleSoft response could not be read", errors.Join(readErr, closeErr))
			}
			return nil, peopleSoftPermanent("PeopleSoft rejected the read-only query", fmt.Errorf("PeopleSoft status %d", response.StatusCode))
		}
		if response.StatusCode == http.StatusOK {
			contentType := strings.ToLower(response.Header.Get("Content-Type"))
			if !strings.Contains(contentType, "application/json") {
				return nil, peopleSoftPermanent("PeopleSoft returned an invalid response type", ErrInvalidInput)
			}
			return decodePeopleSoftQuery(body, query.name, c.maximumRows)
		}
		if peopleSoftTransientStatus(response.StatusCode) && attempt < MaximumPeopleSoftRetries {
			if delayErr := c.retryDelay(ctx, attempt); delayErr != nil {
				return nil, peopleSoftTransient("PeopleSoft request was interrupted", delayErr)
			}
			continue
		}
		if peopleSoftTransientStatus(response.StatusCode) {
			return nil, peopleSoftTransient("PeopleSoft is temporarily unavailable", fmt.Errorf("PeopleSoft status %d", response.StatusCode))
		}
		return nil, peopleSoftPermanent("PeopleSoft rejected the read-only query", fmt.Errorf("PeopleSoft status %d", response.StatusCode))
	}
	return nil, peopleSoftTransient("PeopleSoft is temporarily unavailable", nil)
}

type peopleSoftQueryResponse struct {
	Status    string          `json:"status"`
	Warning   json.RawMessage `json:"warning"`
	Warnings  json.RawMessage `json:"warnings"`
	Truncated *bool           `json:"truncated"`
	Data      struct {
		Warning  json.RawMessage `json:"warning"`
		Warnings json.RawMessage `json:"warnings"`
		Query    struct {
			NumRows         int                          `json:"numrows"`
			QueryName       string                       `json:"queryname"`
			LegacyQueryName string                       `json:"queryname="`
			Rows            []map[string]json.RawMessage `json:"rows"`
			Warning         json.RawMessage              `json:"warning"`
			Warnings        json.RawMessage              `json:"warnings"`
			Truncated       *bool                        `json:"truncated"`
			MoreRows        *bool                        `json:"moreRows"`
		} `json:"query"`
	} `json:"data"`
}

func decodePeopleSoftQuery(body []byte, expectedQuery string, maximumRows int) ([]map[string]json.RawMessage, error) {
	var response peopleSoftQueryResponse
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return nil, peopleSoftPermanent("PeopleSoft returned malformed JSON", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, peopleSoftPermanent("PeopleSoft returned malformed JSON", err)
	}
	queryName := strings.TrimSpace(response.Data.Query.QueryName)
	if queryName == "" {
		queryName = strings.TrimSpace(response.Data.Query.LegacyQueryName)
	}
	if peopleSoftJSONMarkerPresent(response.Warning) || peopleSoftJSONMarkerPresent(response.Warnings) || peopleSoftTrue(response.Truncated) ||
		peopleSoftJSONMarkerPresent(response.Data.Warning) || peopleSoftJSONMarkerPresent(response.Data.Warnings) ||
		peopleSoftJSONMarkerPresent(response.Data.Query.Warning) || peopleSoftJSONMarkerPresent(response.Data.Query.Warnings) ||
		peopleSoftTrue(response.Data.Query.Truncated) || peopleSoftTrue(response.Data.Query.MoreRows) {
		return nil, peopleSoftPermanent("PeopleSoft reported an incomplete query response", ErrInvalidInput)
	}
	if !strings.EqualFold(strings.TrimSpace(response.Status), "success") ||
		!strings.EqualFold(queryName, expectedQuery) || response.Data.Query.NumRows != len(response.Data.Query.Rows) ||
		response.Data.Query.Rows == nil || response.Data.Query.NumRows < 0 || len(response.Data.Query.Rows) > maximumRows {
		return nil, peopleSoftPermanent("PeopleSoft returned an inconsistent query response", ErrInvalidInput)
	}
	return response.Data.Query.Rows, nil
}

func normalizePeopleSoftRow(kind string, mapping PeopleSoftFieldMapping, row map[string]json.RawMessage) (Record, []peopleSoftRelation, string, string, string, error) {
	setID, err := peopleSoftRequiredValue(row, mapping.SetID, 200)
	if err != nil || !validPeopleSoftIdentifier(setID) {
		return Record{}, nil, "", "", "", errors.New("mapped SETID is invalid")
	}
	id, err := peopleSoftRequiredValue(row, mapping.ID, 200)
	if err != nil || !validPeopleSoftIdentifier(id) {
		return Record{}, nil, "", "", "", errors.New("mapped id is invalid")
	}
	name, err := peopleSoftRequiredValue(row, mapping.Name, 200)
	if err != nil {
		return Record{}, nil, "", "", "", errors.New("mapped name is invalid")
	}
	statusValue, err := peopleSoftRequiredValue(row, mapping.Status, 64)
	if err != nil {
		return Record{}, nil, "", "", "", errors.New("mapped status is invalid")
	}
	status, err := normalizePeopleSoftStatus(statusValue, mapping)
	if err != nil {
		return Record{}, nil, "", "", "", err
	}
	description, err := peopleSoftOptionalValue(row, mapping.Description, 2000)
	if err != nil {
		return Record{}, nil, "", "", "", errors.New("mapped description is invalid")
	}
	metadata := map[string]string{"directory-object-kind": kind, "source-object-id": id, "source-setid": setID}
	for key, field := range map[string]PeopleSoftMappedField{
		"address-line-1": mapping.AddressLine1, "address-line-2": mapping.AddressLine2,
		"city": mapping.City, "region": mapping.Region, "postal-code": mapping.PostalCode, "country": mapping.Country,
	} {
		value, valueErr := peopleSoftOptionalValue(row, field, 500)
		if valueErr != nil {
			return Record{}, nil, "", "", "", errors.New("mapped location attribute is invalid")
		}
		if value != "" {
			metadata[key] = value
		}
	}
	sourceID := peopleSoftSourceID(kind, setID, id)
	if !validSourceRecordID(sourceID) {
		return Record{}, nil, "", "", "", errors.New("mapped composite id is invalid")
	}
	record := Record{SourceRecordID: sourceID, Kind: RecordGroup, GroupName: sourceID,
		DisplayName: name, Description: description, Status: status, NormalizedMetadata: metadata}
	relations := make([]peopleSoftRelation, 0, 2)
	organizationID, err := peopleSoftOptionalValue(row, mapping.OrganizationID, 200)
	if err != nil || organizationID != "" && !validPeopleSoftIdentifier(organizationID) {
		return Record{}, nil, "", "", "", errors.New("mapped organization id is invalid")
	}
	locationID, err := peopleSoftOptionalValue(row, mapping.LocationID, 200)
	if err != nil || locationID != "" && !validPeopleSoftIdentifier(locationID) {
		return Record{}, nil, "", "", "", errors.New("mapped location id is invalid")
	}
	objectOrganizationID := organizationID
	switch kind {
	case "organization":
		if organizationID != "" || locationID != "" {
			return Record{}, nil, "", "", "", errors.New("organization parent fields are invalid")
		}
		objectOrganizationID = id
	case "location":
		if organizationID == "" || locationID != "" {
			return Record{}, nil, "", "", "", errors.New("location organization is required")
		}
		relations = append(relations, peopleSoftRelation{setID, "organization", organizationID, kind, id, name, status})
	case "building":
		if locationID == "" {
			return Record{}, nil, "", "", "", errors.New("building location is required")
		}
		relations = append(relations, peopleSoftRelation{setID, "location", locationID, kind, id, name, status})
	case "department":
		if organizationID == "" {
			return Record{}, nil, "", "", "", errors.New("department organization is required")
		}
		relations = append(relations, peopleSoftRelation{setID, "organization", organizationID, kind, id, name, status})
		if locationID != "" {
			relations = append(relations, peopleSoftRelation{setID, "location", locationID, kind, id, name, status})
		}
	default:
		return Record{}, nil, "", "", "", errors.New("mapped object kind is invalid")
	}
	return record, relations, setID, id, objectOrganizationID, nil
}

func parsePeopleSoftMappings(raw string) (PeopleSoftFieldMappings, []byte, error) {
	if len(raw) == 0 || len(raw) > 32<<10 || !utf8.ValidString(raw) {
		return PeopleSoftFieldMappings{}, nil, ErrInvalidInput
	}
	var mappings PeopleSoftFieldMappings
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&mappings); err != nil {
		return PeopleSoftFieldMappings{}, nil, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return PeopleSoftFieldMappings{}, nil, err
	}
	for kind, mapping := range map[string]*PeopleSoftFieldMapping{
		"organization": &mappings.Organization, "location": &mappings.Location,
		"building": &mappings.Building, "department": &mappings.Department,
	} {
		if err := validatePeopleSoftMapping(kind, mapping); err != nil {
			return PeopleSoftFieldMappings{}, nil, err
		}
	}
	canonical, err := json.Marshal(mappings)
	return mappings, canonical, err
}

func validatePeopleSoftMapping(kind string, mapping *PeopleSoftFieldMapping) error {
	if mapping == nil {
		return ErrInvalidInput
	}
	fields := []*PeopleSoftMappedField{&mapping.SetID, &mapping.ID, &mapping.Name, &mapping.Status, &mapping.Description, &mapping.OrganizationID,
		&mapping.LocationID, &mapping.AddressLine1, &mapping.AddressLine2, &mapping.City, &mapping.Region,
		&mapping.PostalCode, &mapping.Country}
	selectors := make(map[string]string, len(fields))
	aliases := make(map[string]string, len(fields))
	for index, field := range fields {
		field.Selector = strings.TrimSpace(field.Selector)
		field.Alias = strings.TrimSpace(field.Alias)
		required := index < 4
		if !field.configured() {
			if required {
				return ErrInvalidInput
			}
			continue
		}
		if !peopleSoftSelectorPattern.MatchString(field.Selector) || !peopleSoftAliasPattern.MatchString(field.Alias) {
			return ErrInvalidInput
		}
		selectorKey := strings.ToLower(field.Selector)
		aliasKey := strings.ToLower(field.Alias)
		if existing, present := selectors[selectorKey]; present && !strings.EqualFold(existing, field.Alias) {
			return ErrInvalidInput
		}
		if existing, present := aliases[aliasKey]; present && !strings.EqualFold(existing, field.Selector) {
			return ErrInvalidInput
		}
		selectors[selectorKey], aliases[aliasKey] = field.Alias, field.Selector
	}
	if (kind == "location" && (!mapping.OrganizationID.configured() || mapping.LocationID.configured())) ||
		(kind == "building" && !mapping.LocationID.configured()) ||
		(kind == "department" && !mapping.OrganizationID.configured()) ||
		(kind == "organization" && (mapping.OrganizationID.configured() || mapping.LocationID.configured())) {
		return ErrInvalidInput
	}
	active, err := normalizePeopleSoftStatusValues(mapping.ActiveValues, []string{"A", "active", "enabled", "true", "1"})
	if err != nil {
		return err
	}
	inactive, err := normalizePeopleSoftStatusValues(mapping.InactiveValues, []string{"I", "inactive", "disabled", "false", "0"})
	if err != nil {
		return err
	}
	for _, value := range active {
		for _, inactiveValue := range inactive {
			if strings.EqualFold(value, inactiveValue) {
				return ErrInvalidInput
			}
		}
	}
	mapping.ActiveValues, mapping.InactiveValues = active, inactive
	return nil
}

func normalizePeopleSoftStatusValues(values, fallback []string) ([]string, error) {
	if len(values) == 0 {
		values = fallback
	}
	if len(values) > 16 {
		return nil, ErrInvalidInput
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{})
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" || !validOptionalGrouperText(value, 64) {
			return nil, ErrInvalidInput
		}
		if _, duplicate := seen[key]; duplicate {
			return nil, ErrInvalidInput
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i]) < strings.ToLower(result[j]) })
	return result, nil
}

func normalizePeopleSoftStatus(value string, mapping PeopleSoftFieldMapping) (string, error) {
	for _, active := range mapping.ActiveValues {
		if strings.EqualFold(value, active) {
			return "active", nil
		}
	}
	for _, inactive := range mapping.InactiveValues {
		if strings.EqualFold(value, inactive) {
			return "inactive", nil
		}
	}
	return "", errors.New("mapped status value is unknown")
}

func peopleSoftMappedFields(mapping PeopleSoftFieldMapping) []string {
	values := []PeopleSoftMappedField{mapping.SetID, mapping.ID, mapping.Name, mapping.Status, mapping.Description, mapping.OrganizationID, mapping.LocationID,
		mapping.AddressLine1, mapping.AddressLine2, mapping.City, mapping.Region, mapping.PostalCode, mapping.Country}
	seen := make(map[string]struct{})
	result := make([]string, 0, len(values))
	for _, value := range values {
		if !value.configured() {
			continue
		}
		if _, present := seen[value.Selector]; present {
			continue
		}
		seen[value.Selector] = struct{}{}
		result = append(result, value.Selector)
	}
	sort.Strings(result)
	return result
}

func peopleSoftRequiredValue(row map[string]json.RawMessage, field PeopleSoftMappedField, maximum int) (string, error) {
	value, err := peopleSoftOptionalValue(row, field, maximum)
	if err != nil || value == "" {
		return "", ErrInvalidInput
	}
	return value, nil
}

func peopleSoftOptionalValue(row map[string]json.RawMessage, field PeopleSoftMappedField, maximum int) (string, error) {
	if !field.configured() {
		return "", nil
	}
	raw, present := row[field.Alias]
	if !present {
		return "", ErrInvalidInput
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", nil
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	var text string
	switch typed := value.(type) {
	case string:
		text = typed
	case json.Number:
		text = typed.String()
	case bool:
		text = strconv.FormatBool(typed)
	default:
		return "", ErrInvalidInput
	}
	text = strings.TrimSpace(text)
	if !validOptionalGrouperText(text, maximum) {
		return "", ErrInvalidInput
	}
	return text, nil
}

func (field PeopleSoftMappedField) configured() bool {
	return field.Selector != "" || field.Alias != ""
}

func peopleSoftSourceID(kind, setID, id string) string {
	return kind + ":" + setID + ":" + id
}

func peopleSoftScopeKey(setID, id string) string {
	return setID + "\x00" + id
}

func peopleSoftJSONMarkerPresent(value json.RawMessage) bool {
	trimmed := bytes.TrimSpace(value)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) && !bytes.Equal(trimmed, []byte(`""`)) &&
		!bytes.Equal(trimmed, []byte("[]")) && !bytes.Equal(trimmed, []byte("{}"))
}

func peopleSoftTrue(value *bool) bool {
	return value != nil && *value
}

func validPeopleSoftIdentifier(value string) bool {
	return value != "" && len(value) <= 200 && !strings.Contains(value, ":") && validOptionalGrouperText(value, 200)
}

func validatePeopleSoftBaseURL(raw string, allowPrivate bool) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) == 0 || len(raw) > 2048 || !utf8.ValidString(raw) {
		return nil, errors.New("PeopleSoft URL must be a fixed HTTPS Query Access Service endpoint")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" ||
		!peopleSoftPathPattern.MatchString(parsed.EscapedPath()) {
		return nil, errors.New("PeopleSoft URL must be a fixed HTTPS Query Access Service endpoint")
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	loopback := strings.EqualFold(host, "localhost") || ip != nil && ip.IsLoopback()
	if parsed.Scheme != "https" && !(allowPrivate && parsed.Scheme == "http" && loopback) {
		return nil, errors.New("PeopleSoft URL must use HTTPS except for explicit loopback development")
	}
	if ip != nil && !isAllowedGrouperIP(ip, allowPrivate) {
		return nil, errors.New("PeopleSoft URL address is not permitted")
	}
	return parsed, nil
}

func peopleSoftGuardedDialer(allowPrivate bool) *net.Dialer {
	return &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second, ControlContext: func(_ context.Context, _, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return errors.New("PeopleSoft network address is invalid")
		}
		if !isAllowedGrouperIP(net.ParseIP(host), allowPrivate) {
			return errors.New("PeopleSoft resolved to an address that is not permitted")
		}
		return nil
	}}
}

func peopleSoftRevision(config PeopleSoftConnectorConfig, mappings []byte) string {
	principal := "basic\x00" + strings.TrimSpace(config.Username)
	if config.BearerToken != "" {
		principal = "bearer\x00" + config.BearerToken
	}
	principalDigest := sha256.Sum256([]byte(principal))
	parts := []string{strings.TrimSpace(config.BaseURL), strings.ToLower(strings.TrimSpace(config.QueryOwner)),
		strings.TrimSpace(config.OrganizationQuery), strings.TrimSpace(config.LocationQuery),
		strings.TrimSpace(config.BuildingQuery), strings.TrimSpace(config.DepartmentQuery),
		strconv.Itoa(config.MaximumRows), hex.EncodeToString(principalDigest[:]), string(mappings)}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "peoplesoft-" + hex.EncodeToString(digest[:])
}

func peopleSoftTransientStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500 && status <= 599
}

func validPeopleSoftHeaderValue(value string, maximumBytes int) bool {
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

// Basic authentication uses the first colon as the username/password boundary.
// Rejecting it keeps the validated and revision-bound principal unambiguous.
func validPeopleSoftBasicUsername(value string) bool {
	return !strings.Contains(value, ":") && validPeopleSoftHeaderValue(value, 256)
}

func validPeopleSoftBearer(value string) bool {
	return value == "" || len(value) <= 4096 && peopleSoftBearerPattern.MatchString(value)
}

func peopleSoftPermanent(message string, cause error) error {
	return &ClassifiedError{Class: FailurePermanent, Message: message, Cause: cause}
}

func peopleSoftTransient(message string, cause error) error {
	return &ClassifiedError{Class: FailureTransient, Retryable: true, Message: message, Cause: cause}
}
