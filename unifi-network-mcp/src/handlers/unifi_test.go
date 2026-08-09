package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/tuttlebr/unifi-network-mcp/pkg/unifi"
)

func TestClientFromArgsRequiresClientSuppliedConnection(t *testing.T) {
	if _, err := clientFromArgs(map[string]interface{}{}, ClientDefaults{}); err == nil {
		t.Fatal("expected missing baseUrl error")
	}
	if _, err := clientFromArgs(map[string]interface{}{
		"baseUrl": "https://example.test/proxy/network/integration",
	}, ClientDefaults{}); err == nil {
		t.Fatal("expected missing apiKey error")
	}
}

func TestClientFromArgsUsesPerCallOptions(t *testing.T) {
	client, err := clientFromArgs(map[string]interface{}{
		"baseUrl":            "https://example.test/proxy/network/integration",
		"apiKey":             "secret",
		"insecureSkipVerify": true,
		"timeoutSeconds":     float64(5),
	}, ClientDefaults{})
	if err != nil {
		t.Fatalf("clientFromArgs failed: %v", err)
	}
	if client.Timeout().Seconds() != 5 {
		t.Fatalf("timeout = %s, want 5s", client.Timeout())
	}
}

func TestClientFromArgsUsesServerDefaults(t *testing.T) {
	insecureSkipVerify := true
	client, err := clientFromArgs(map[string]interface{}{}, ClientDefaults{
		BaseURL:            "https://example.test/proxy/network/integration",
		APIKey:             "secret",
		InsecureSkipVerify: &insecureSkipVerify,
	})
	if err != nil {
		t.Fatalf("clientFromArgs failed: %v", err)
	}
	if client.Timeout().Seconds() != 30 {
		t.Fatalf("timeout = %s, want 30s", client.Timeout())
	}
}

func TestClientFromArgsRejectsConnectionOverridesWhenLocked(t *testing.T) {
	_, err := clientFromArgs(map[string]interface{}{
		"baseUrl": "https://attacker.example/proxy/network/integration",
	}, ClientDefaults{
		BaseURL: "https://controller.example/proxy/network/integration",
		APIKey:  "secret",
		Locked:  true,
	})
	if err == nil || !strings.Contains(err.Error(), "cannot be overridden") {
		t.Fatalf("expected fixed-controller override rejection, got %v", err)
	}
}

func TestEndpointHandlerPerCallBaseURLOverridesDefault(t *testing.T) {
	defaultServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("default server should not be called")
	}))
	defer defaultServer.Close()

	overrideHit := false
	overrideServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		overrideHit = true
		if r.Header.Get("X-API-KEY") != "call-key" {
			t.Fatalf("X-API-KEY = %q, want call-key", r.Header.Get("X-API-KEY"))
		}
		if r.URL.Path != "/v1/info" {
			t.Fatalf("path = %q, want /v1/info", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer overrideServer.Close()

	insecureSkipVerify := true
	handler := EndpointHandler(unifi.Endpoint{
		Name:   "getInfo",
		Method: "GET",
		Path:   "/v1/info",
	}, ClientDefaults{
		BaseURL:            defaultServer.URL,
		APIKey:             "default-key",
		InsecureSkipVerify: &insecureSkipVerify,
	})

	result, err := handler(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]interface{}{
				"baseUrl": overrideServer.URL,
				"apiKey":  "call-key",
			},
		},
	})
	if err != nil {
		t.Fatalf("handler failed: %v", err)
	}
	if !overrideHit {
		t.Fatal("override server was not called")
	}
	if result == nil || !strings.Contains(result.Content[0].(mcp.TextContent).Text, `"ok":true`) {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestBuildPathEscapesRequiredParams(t *testing.T) {
	endpoint := unifi.Endpoint{
		Path:       "/v1/sites/{siteId}/devices/{deviceId}",
		PathParams: []string{"siteId", "deviceId"},
	}

	path, err := buildPath(endpoint, map[string]interface{}{
		"siteId":   "site one",
		"deviceId": "aa/bb",
	})
	if err != nil {
		t.Fatalf("buildPath failed: %v", err)
	}
	if path != "/v1/sites/site%20one/devices/aa%2Fbb" {
		t.Fatalf("path = %q", path)
	}
}

func TestBuildPathRequiresParams(t *testing.T) {
	endpoint := unifi.Endpoint{
		Path:       "/v1/sites/{siteId}/devices",
		PathParams: []string{"siteId"},
	}
	if _, err := buildPath(endpoint, map[string]interface{}{}); err == nil {
		t.Fatal("expected missing path parameter error")
	}
}

func TestBuildQueryPaginationAndFilter(t *testing.T) {
	endpoint := unifi.Endpoint{
		SupportsPagination: true,
		SupportsFilter:     true,
	}
	query, err := buildQuery(endpoint, map[string]interface{}{
		"offset": float64(10),
		"limit":  float64(25),
		"filter": "name.eq('Default')",
	})
	if err != nil {
		t.Fatalf("buildQuery failed: %v", err)
	}
	want := url.Values{"offset": {"10"}, "limit": {"25"}, "filter": {"name.eq('Default')"}}
	if query.Encode() != want.Encode() {
		t.Fatalf("query = %q, want %q", query.Encode(), want.Encode())
	}
}

func TestBuildQueryRejectsInvalidLimit(t *testing.T) {
	endpoint := unifi.Endpoint{SupportsPagination: true}
	if _, err := buildQuery(endpoint, map[string]interface{}{"limit": float64(201)}); err == nil {
		t.Fatal("expected invalid limit error")
	}
}

func TestBuildQueryUsesEndpointPaginationMaximum(t *testing.T) {
	endpoint, ok := unifi.EndpointByName("listVouchers")
	if !ok {
		t.Fatal("listVouchers endpoint missing")
	}
	query, err := buildQuery(endpoint, map[string]interface{}{"limit": float64(1000)})
	if err != nil {
		t.Fatalf("buildQuery rejected documented voucher limit: %v", err)
	}
	if query.Get("limit") != "1000" {
		t.Fatalf("limit = %q, want 1000", query.Get("limit"))
	}
	if _, err := buildQuery(endpoint, map[string]interface{}{"limit": float64(1001)}); err == nil {
		t.Fatal("expected voucher limit above 1000 to fail")
	}
}

func TestBuildQueryRequiresFirewallPolicyOrderingZonePair(t *testing.T) {
	endpoint, ok := unifi.EndpointByName("getFirewallPolicyOrdering")
	if !ok {
		t.Fatal("getFirewallPolicyOrdering endpoint missing")
	}
	args := map[string]interface{}{
		"sourceFirewallZoneId":      "source-zone",
		"destinationFirewallZoneId": "destination-zone",
	}
	query, err := buildQuery(endpoint, args)
	if err != nil {
		t.Fatalf("buildQuery failed: %v", err)
	}
	if query.Get("sourceFirewallZoneId") != "source-zone" || query.Get("destinationFirewallZoneId") != "destination-zone" {
		t.Fatalf("query = %q", query.Encode())
	}
	delete(args, "destinationFirewallZoneId")
	if _, err := buildQuery(endpoint, args); err == nil {
		t.Fatal("expected missing destination firewall zone to fail")
	}
}
