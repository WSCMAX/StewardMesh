package directoryexpansion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type HTTPConnector struct {
	provider        Provider
	client          *http.Client
	endpoint, token string
}

func NewHTTPConnector(provider Provider, endpoint, token string, client *http.Client) *HTTPConnector {
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTPConnector{provider: provider, endpoint: strings.TrimRight(endpoint, "/"), token: token, client: client}
}
func (c *HTTPConnector) Provider() Provider { return c.provider }
func (c *HTTPConnector) Pull(ctx context.Context) ([]ImportRecord, error) {
	if c.endpoint == "" {
		return nil, fmt.Errorf("%s endpoint is required", c.provider)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%s returned HTTP %d", c.provider, resp.StatusCode)
	}
	var records []ImportRecord
	if err := json.NewDecoder(resp.Body).Decode(&records); err != nil {
		return nil, fmt.Errorf("decode %s response: %w", c.provider, err)
	}
	return records, nil
}

// NewEntraConnector, NewSailPointConnector, NewGrouperConnector, and
// NewPeopleSoftConnector share the safe read-only HTTP seam. Production
// deployments can provide provider-specific request/response translators.
func NewEntraConnector(endpoint, token string, client *http.Client) Connector {
	return NewHTTPConnector(ProviderEntra, endpoint, token, client)
}
func NewSailPointConnector(endpoint, token string, client *http.Client) Connector {
	return NewHTTPConnector(ProviderSailPoint, endpoint, token, client)
}
func NewGrouperConnector(endpoint, token string, client *http.Client) Connector {
	return NewHTTPConnector(ProviderGrouper, endpoint, token, client)
}
func NewPeopleSoftConnector(endpoint, token string, client *http.Client) Connector {
	return NewHTTPConnector(ProviderPeopleSoft, endpoint, token, client)
}
