package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/tuttlebr/unifi-network-mcp/pkg/unifi"
)

func TestEndpointHandlerRejectsInvalidSiteBeforeControllerRequest(t *testing.T) {
	var calls atomic.Int32
	controller := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer controller.Close()
	insecure := true
	for _, name := range []string{"listAdoptedDevices", "deleteNetwork"} {
		endpoint, _ := unifi.EndpointByName(name)
		handler := EndpointHandler(endpoint, ClientDefaults{
			BaseURL: controller.URL, APIKey: "test-key", InsecureSkipVerify: &insecure, Locked: true,
		})
		for _, value := range []any{nil, "", "default", "Default", 42, "11111111111141118111111111111111", "urn:uuid:11111111-1111-4111-8111-111111111111", "11111111-1111-4111-8111-11111111111z"} {
			result, err := handler(context.Background(), mcp.CallToolRequest{
				Params: mcp.CallToolParams{Arguments: map[string]any{
					"siteId": value, "networkId": "22222222-2222-4222-8222-222222222222",
				}},
			})
			if err != nil || result == nil || !result.IsError {
				t.Fatalf("%s siteId=%v: want tool error, got result=%#v err=%v", name, value, result, err)
			}
			message := result.Content[0].(mcp.TextContent).Text
			for _, guidance := range []string{"invalid siteId", "listSites", "data[].id", "internalReference", "before retrying"} {
				if !strings.Contains(message, guidance) {
					t.Errorf("%s siteId=%v: error omitted %q: %s", name, value, guidance, message)
				}
			}
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid site IDs reached controller %d times", calls.Load())
	}
}

func TestUUIDParameterValidation(t *testing.T) {
	endpoint, _ := unifi.EndpointByName("getAdoptedDeviceDetails")
	if _, err := buildPath(endpoint, map[string]any{
		"siteId": "11111111-1111-4111-8111-111111111111", "deviceId": "device-name",
	}); err == nil || !strings.Contains(err.Error(), "deviceId must be a UUID") {
		t.Fatalf("invalid device UUID was accepted: %v", err)
	}
	ordering, _ := unifi.EndpointByName("getFirewallPolicyOrdering")
	if _, err := buildQuery(ordering, map[string]any{
		"sourceFirewallZoneId": "source-zone", "destinationFirewallZoneId": "55555555-5555-4555-8555-555555555555",
	}); err == nil || !strings.Contains(err.Error(), "sourceFirewallZoneId must be a UUID") {
		t.Fatalf("invalid query UUID was accepted: %v", err)
	}
	for _, value := range []string{
		"11111111-1111-4111-8111-111111111111",
		"abcdefab-cdef-3abc-abcd-abcdefabcdef",
		"ABCDEFAB-CDEF-3ABC-ABCD-ABCDEFABCDEF",
	} {
		got, _, err := getParameterTextArg(map[string]any{"id": value}, "id", unifi.ParameterSchema{Type: "string", Format: "uuid"}, true)
		if err != nil || got != value {
			t.Errorf("valid UUID %q rejected or rewritten: %q, %v", value, got, err)
		}
	}
}

func TestEndpointHandlerReturnsControllerFailureAsToolError(t *testing.T) {
	controller := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"Unsupported filter"}`))
	}))
	defer controller.Close()
	insecure := true
	endpoint, _ := unifi.EndpointByName("listSites")
	handler := EndpointHandler(endpoint, ClientDefaults{BaseURL: controller.URL, APIKey: "test-key", InsecureSkipVerify: &insecure})
	result, err := handler(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{}}})
	if err != nil || result == nil || !result.IsError {
		t.Fatalf("want tool error instead of internal MCP error: result=%#v err=%v", result, err)
	}
	if text := result.Content[0].(mcp.TextContent).Text; !strings.Contains(text, "400 Bad Request") || !strings.Contains(text, "Unsupported filter") {
		t.Fatalf("controller error context lost: %s", text)
	}
}

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
		"sourceFirewallZoneId":      "44444444-4444-4444-8444-444444444444",
		"destinationFirewallZoneId": "55555555-5555-4555-8555-555555555555",
	}
	query, err := buildQuery(endpoint, args)
	if err != nil {
		t.Fatalf("buildQuery failed: %v", err)
	}
	if query.Get("sourceFirewallZoneId") != "44444444-4444-4444-8444-444444444444" || query.Get("destinationFirewallZoneId") != "55555555-5555-4555-8555-555555555555" {
		t.Fatalf("query = %q", query.Encode())
	}
	delete(args, "destinationFirewallZoneId")
	if _, err := buildQuery(endpoint, args); err == nil {
		t.Fatal("expected missing destination firewall zone to fail")
	}
}

func TestEndpointHandlerSendsMutationMethodQueryAndBody(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("method = %s, want PUT", r.Method)
		}
		if r.URL.Path != "/v1/sites/11111111-1111-4111-8111-111111111111/networks/22222222-2222-4222-8222-222222222222" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["name"] != "Updated network" {
			t.Fatalf("body = %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"22222222-2222-4222-8222-222222222222","name":"Updated network"}`))
	}))
	defer server.Close()

	endpoint, ok := unifi.EndpointByName("updateNetwork")
	if !ok {
		t.Fatal("updateNetwork endpoint missing")
	}
	insecureSkipVerify := true
	handler := EndpointHandler(endpoint, ClientDefaults{
		BaseURL:            server.URL,
		APIKey:             "test-key",
		InsecureSkipVerify: &insecureSkipVerify,
	})
	result, err := handler(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]interface{}{
			"siteId":    "11111111-1111-4111-8111-111111111111",
			"networkId": "22222222-2222-4222-8222-222222222222",
			"body": map[string]interface{}{
				"name": "Updated network",
			},
		}},
	})
	if err != nil {
		t.Fatalf("handler failed: %v", err)
	}
	if result == nil || !strings.Contains(result.Content[0].(mcp.TextContent).Text, `"id":"22222222-2222-4222-8222-222222222222"`) {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestBuildPathUsesIntegerPathParameter(t *testing.T) {
	endpoint, ok := unifi.EndpointByName("executePortAction")
	if !ok {
		t.Fatal("executePortAction endpoint missing")
	}
	path, err := buildPath(endpoint, map[string]interface{}{
		"siteId":   "11111111-1111-4111-8111-111111111111",
		"deviceId": "33333333-3333-4333-8333-333333333333",
		"portIdx":  float64(7),
	})
	if err != nil {
		t.Fatalf("buildPath failed: %v", err)
	}
	if path != "/v1/sites/11111111-1111-4111-8111-111111111111/devices/33333333-3333-4333-8333-333333333333/interfaces/ports/7/actions" {
		t.Fatalf("path = %q", path)
	}
	if _, err := buildPath(endpoint, map[string]interface{}{
		"siteId":   "11111111-1111-4111-8111-111111111111",
		"deviceId": "33333333-3333-4333-8333-333333333333",
		"portIdx":  "seven",
	}); err == nil {
		t.Fatal("expected non-integer portIdx error")
	}
}

func TestBuildQuerySupportsBooleanAndRequiredMutationParameters(t *testing.T) {
	deleteNetwork, ok := unifi.EndpointByName("deleteNetwork")
	if !ok {
		t.Fatal("deleteNetwork endpoint missing")
	}
	query, err := buildQuery(deleteNetwork, map[string]interface{}{"force": true})
	if err != nil {
		t.Fatalf("buildQuery failed: %v", err)
	}
	if query.Get("force") != "true" {
		t.Fatalf("force = %q, want true", query.Get("force"))
	}

	deleteVouchers, ok := unifi.EndpointByName("deleteVouchers")
	if !ok {
		t.Fatal("deleteVouchers endpoint missing")
	}
	if _, err := buildQuery(deleteVouchers, map[string]interface{}{}); err == nil {
		t.Fatal("expected required filter error")
	}
	query, err = buildQuery(deleteVouchers, map[string]interface{}{"filter": "expired.eq(true)"})
	if err != nil {
		t.Fatalf("buildQuery failed: %v", err)
	}
	if query.Get("filter") != "expired.eq(true)" {
		t.Fatalf("filter = %q", query.Get("filter"))
	}
}

func TestBuildRequestBodyEnforcesOperationContract(t *testing.T) {
	createNetwork, ok := unifi.EndpointByName("createNetwork")
	if !ok {
		t.Fatal("createNetwork endpoint missing")
	}
	if _, err := buildRequestBody(createNetwork, map[string]interface{}{}); err == nil {
		t.Fatal("expected missing body error")
	}
	if _, err := buildRequestBody(createNetwork, map[string]interface{}{"body": "not-an-object"}); err == nil {
		t.Fatal("expected non-object body error")
	}
	if _, err := buildRequestBody(createNetwork, map[string]interface{}{"body": map[string]interface{}{}}); err != nil {
		t.Fatalf("valid object body rejected: %v", err)
	}

	listSites, ok := unifi.EndpointByName("listSites")
	if !ok {
		t.Fatal("listSites endpoint missing")
	}
	if _, err := buildRequestBody(listSites, map[string]interface{}{"body": map[string]interface{}{}}); err == nil {
		t.Fatal("expected body rejection for GET endpoint")
	}
}
