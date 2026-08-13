package reach

// Requirement: REQ-REACH-001. Feature: messaging.delivery. GitHub: #12.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

var stableIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type EndpointCatalog struct {
	items map[string]Endpoint
}

func NewEndpointCatalog(endpoints []Endpoint) (*EndpointCatalog, error) {
	catalog := &EndpointCatalog{items: make(map[string]Endpoint, len(endpoints))}
	for _, endpoint := range endpoints {
		endpoint.ID, endpoint.Label = strings.TrimSpace(endpoint.ID), strings.TrimSpace(endpoint.Label)
		endpoint.URL, endpoint.TestURL = strings.TrimSpace(endpoint.URL), strings.TrimSpace(endpoint.TestURL)
		endpoint.Address, endpoint.ServerName, endpoint.Region = strings.TrimSpace(endpoint.Address), strings.TrimSpace(endpoint.ServerName), strings.TrimSpace(endpoint.Region)
		if !stableIDPattern.MatchString(endpoint.ID) || !validText(endpoint.Label, 1, 160) || !validProviderKind(endpoint.Kind) {
			return nil, fmt.Errorf("invalid Reach endpoint metadata")
		}
		if _, exists := catalog.items[endpoint.ID]; exists {
			return nil, fmt.Errorf("duplicate Reach endpoint id %q", endpoint.ID)
		}
		if endpoint.Kind == ProviderSMTP {
			if err := validateSMTPEndpoint(endpoint); err != nil {
				return nil, err
			}
		} else {
			if err := validateFixedURL(endpoint.URL, endpoint.AllowLocalHTTP); err != nil {
				return nil, fmt.Errorf("invalid fixed URL for Reach endpoint %q: %w", endpoint.ID, err)
			}
			if endpoint.TestURL != "" {
				if err := validateFixedURL(endpoint.TestURL, endpoint.AllowLocalHTTP); err != nil {
					return nil, fmt.Errorf("invalid fixed test URL for Reach endpoint %q: %w", endpoint.ID, err)
				}
			}
			if endpoint.Kind == ProviderSES && !regexp.MustCompile(`^[a-z]{2}(?:-gov)?-[a-z]+-\d$`).MatchString(endpoint.Region) {
				return nil, fmt.Errorf("Reach SES endpoint %q requires a valid AWS region", endpoint.ID)
			}
		}
		catalog.items[endpoint.ID] = endpoint
	}
	return catalog, nil
}

func (c *EndpointCatalog) Get(id string, kind ProviderKind) (Endpoint, error) {
	if c == nil {
		return Endpoint{}, ErrEndpointUnavailable
	}
	endpoint, ok := c.items[strings.TrimSpace(id)]
	if !ok || endpoint.Kind != kind {
		return Endpoint{}, ErrEndpointUnavailable
	}
	return endpoint, nil
}

func (c *EndpointCatalog) List() []Endpoint {
	if c == nil {
		return []Endpoint{}
	}
	items := make([]Endpoint, 0, len(c.items))
	for _, endpoint := range c.items {
		// Endpoint's route fields are JSON-excluded. Clear them anyway so a
		// future serializer change cannot accidentally expose infrastructure.
		endpoint.URL, endpoint.TestURL, endpoint.Address, endpoint.ServerName, endpoint.Region = "", "", "", "", ""
		endpoint.RequireTLS, endpoint.AllowLocalHTTP = false, false
		items = append(items, endpoint)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

// LoadEndpointsFile reads deployment-owned, credential-free endpoint
// metadata. API input never controls this path or any address inside the file.
func LoadEndpointsFile(path string) ([]Endpoint, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return []Endpoint{}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("open Reach endpoint configuration")
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	type endpointFileRecord struct {
		ID             string       `json:"id"`
		Label          string       `json:"label"`
		Kind           ProviderKind `json:"kind"`
		URL            string       `json:"url,omitempty"`
		TestURL        string       `json:"testUrl,omitempty"`
		Address        string       `json:"address,omitempty"`
		ServerName     string       `json:"serverName,omitempty"`
		Region         string       `json:"region,omitempty"`
		RequireTLS     bool         `json:"requireTls,omitempty"`
		AllowLocalHTTP bool         `json:"allowLocalHttp,omitempty"`
	}
	var records []endpointFileRecord
	if err := decoder.Decode(&records); err != nil {
		return nil, errors.New("decode Reach endpoint configuration")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("Reach endpoint configuration must contain one JSON value")
	}
	if len(records) > MaximumProviders {
		return nil, errors.New("Reach endpoint configuration exceeds the supported limit")
	}
	endpoints := make([]Endpoint, 0, len(records))
	for _, record := range records {
		endpoints = append(endpoints, Endpoint{
			ID: record.ID, Label: record.Label, Kind: record.Kind, URL: record.URL, TestURL: record.TestURL,
			Address: record.Address, ServerName: record.ServerName, Region: record.Region,
			RequireTLS: record.RequireTLS, AllowLocalHTTP: record.AllowLocalHTTP,
		})
	}
	if _, err := NewEndpointCatalog(endpoints); err != nil {
		return nil, err
	}
	return endpoints, nil
}

func validateSMTPEndpoint(endpoint Endpoint) error {
	host, port, err := net.SplitHostPort(endpoint.Address)
	if err != nil || host == "" || port == "" || endpoint.ServerName == "" || strings.ContainsAny(endpoint.ServerName, `/\\@`) {
		return fmt.Errorf("Reach SMTP endpoint %q requires a fixed address and server name", endpoint.ID)
	}
	if !endpoint.RequireTLS && !endpoint.AllowLocalHTTP {
		return fmt.Errorf("Reach SMTP endpoint %q must require TLS", endpoint.ID)
	}
	if endpoint.AllowLocalHTTP && !isLoopbackHost(host) {
		return fmt.Errorf("Reach SMTP endpoint %q may relax TLS only for loopback fixtures", endpoint.ID)
	}
	return nil
}

func validateFixedURL(value string, allowLocal bool) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("URL must be absolute and exclude credentials, query, and fragment")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme == "http" && allowLocal && isLoopbackHost(parsed.Hostname()) {
		return nil
	}
	return errors.New("URL must use HTTPS; HTTP is limited to explicit loopback fixtures")
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type EnvironmentSecretResolver struct {
	prefix string
}

func NewEnvironmentSecretResolver(prefix string) (*EnvironmentSecretResolver, error) {
	prefix = strings.TrimSpace(prefix)
	if !regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,63}$`).MatchString(prefix) {
		return nil, errors.New("Reach secret environment prefix is invalid")
	}
	return &EnvironmentSecretResolver{prefix: prefix}, nil
}

func (r *EnvironmentSecretResolver) Resolve(ctx context.Context, reference string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reference = strings.TrimSpace(reference)
	if r == nil || !strings.HasPrefix(reference, "env:") {
		return nil, errors.New("Reach secret reference is invalid")
	}
	reference = strings.TrimPrefix(reference, "env:")
	if !stableIDPattern.MatchString(reference) {
		return nil, errors.New("Reach secret reference is invalid")
	}
	key := strings.ToUpper(reference)
	key = strings.NewReplacer("-", "_", ".", "_", ":", "_").Replace(key)
	value, exists := os.LookupEnv(r.prefix + key)
	if !exists || len(value) < 16 || len(value) > 16<<10 {
		return nil, errors.New("Reach secret is unavailable")
	}
	return []byte(value), nil
}

type TransportRegistry struct {
	items map[ProviderKind]Transport
}

func NewTransportRegistry(items map[ProviderKind]Transport) (*TransportRegistry, error) {
	registry := &TransportRegistry{items: make(map[ProviderKind]Transport, len(items))}
	for kind, transport := range items {
		if !validProviderKind(kind) || transport == nil {
			return nil, errors.New("invalid Reach transport registry")
		}
		registry.items[kind] = transport
	}
	return registry, nil
}

func (r *TransportRegistry) Get(kind ProviderKind) (Transport, error) {
	if r == nil || r.items[kind] == nil {
		return nil, ErrEndpointUnavailable
	}
	return r.items[kind], nil
}

func DefaultTransportRegistry(client *http.Client) (*TransportRegistry, error) {
	if client == nil {
		client = &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	httpTransport := &HTTPTransport{client: client, now: func() time.Time { return time.Now().UTC() }}
	return NewTransportRegistry(map[ProviderKind]Transport{
		ProviderSMTP:    &SMTPTransport{dialTimeout: 10 * time.Second},
		ProviderSES:     httpTransport,
		ProviderGmail:   httpTransport,
		ProviderOutlook: httpTransport,
		ProviderTeams:   httpTransport,
		ProviderWebhook: httpTransport,
	})
}

func validProviderKind(kind ProviderKind) bool {
	switch kind {
	case ProviderSMTP, ProviderSES, ProviderGmail, ProviderOutlook, ProviderTeams, ProviderWebhook:
		return true
	default:
		return false
	}
}
