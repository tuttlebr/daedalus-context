package unifi

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	maxReadBytes      = 4 * 1024 * 1024
	maxRequestBytes   = 1 * 1024 * 1024
	maxErrorBodyBytes = 16 * 1024
	userAgent         = "unifi-network-mcp/0.2.0"
	defaultTimeout    = 30 * time.Second
)

// Config contains all per-request settings required to connect to the UniFi
// Network integration API. MCP clients provide these values on each tool call.
type Config struct {
	BaseURL            string
	APIKey             string
	Timeout            time.Duration
	InsecureSkipVerify bool
}

// Client is a small JSON client for the UniFi Network local integration API.
type Client struct {
	baseURL    *url.URL
	apiKey     string
	httpClient *http.Client
}

// NewClient validates configuration and creates a UniFi Network API client.
func NewClient(cfg Config) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("apiKey is required")
	}
	if baseURL == "" {
		return nil, fmt.Errorf("baseUrl is required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid baseUrl: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("baseUrl must include scheme and host")
	}
	if parsed.Scheme != "https" {
		return nil, fmt.Errorf("baseUrl must use https")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("baseUrl must not include user info")
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify} //nolint:gosec

	return &Client{
		baseURL: parsed,
		apiKey:  strings.TrimSpace(cfg.APIKey),
		httpClient: &http.Client{
			Timeout:   cfg.Timeout,
			Transport: transport,
		},
	}, nil
}

// GetJSON performs an authenticated GET and returns sanitized, bounded JSON.
func (c *Client) GetJSON(ctx context.Context, path string, query url.Values) ([]byte, error) {
	return c.DoJSON(ctx, http.MethodGet, path, query, nil)
}

// DoJSON performs an authenticated UniFi API operation and returns sanitized,
// bounded JSON. Empty successful responses are normalized to a small JSON
// status object so every MCP tool result remains machine-readable.
func (c *Client) DoJSON(ctx context.Context, method, path string, query url.Values, requestBody any) ([]byte, error) {
	method = strings.ToUpper(strings.TrimSpace(method))
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return nil, fmt.Errorf("unsupported unifi api method %q", method)
	}

	target, err := c.buildURL(path, query)
	if err != nil {
		return nil, err
	}

	var encodedRequest []byte
	if requestBody != nil {
		encodedRequest, err = json.Marshal(requestBody)
		if err != nil {
			return nil, fmt.Errorf("failed serializing unifi api request for %s: %w", path, err)
		}
		if len(encodedRequest) > maxRequestBytes {
			return nil, fmt.Errorf("unifi api request for %s exceeds %d bytes", path, maxRequestBytes)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(encodedRequest))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-KEY", c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("unifi api %s %s failed: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return nil, fmt.Errorf("unifi api %s %s returned %s: %s", method, path, resp.Status, SanitizeText(string(body)))
	}

	body, err := readBounded(resp.Body, maxReadBytes)
	if err != nil {
		return nil, fmt.Errorf("failed reading unifi api response for %s: %w", path, err)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return json.Marshal(map[string]any{
			"status":     "success",
			"statusCode": resp.StatusCode,
		})
	}

	var decoded interface{}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("unifi api %s %s returned invalid JSON: %w", method, path, err)
	}

	data, err := json.Marshal(SanitizeForOutput(decoded))
	if err != nil {
		return nil, fmt.Errorf("failed serializing sanitized response: %w", err)
	}

	return TruncateJSON(data), nil
}

// CheckConnection verifies that the UniFi API is reachable and authenticated.
func (c *Client) CheckConnection(ctx context.Context) error {
	_, err := c.GetJSON(ctx, "/v1/info", nil)
	return err
}

func (c *Client) buildURL(path string, query url.Values) (string, error) {
	if !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("api path must start with /: %q", path)
	}

	target := *c.baseURL
	target.Path = strings.TrimRight(target.Path, "/") + path
	target.RawQuery = query.Encode()
	return target.String(), nil
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	limited := io.LimitReader(reader, limit+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return data, nil
}

// Timeout exposes the configured HTTP timeout for tests and diagnostics.
func (c *Client) Timeout() time.Duration {
	return c.httpClient.Timeout
}
