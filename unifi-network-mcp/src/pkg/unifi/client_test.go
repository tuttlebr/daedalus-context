package unifi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestNewClientRequiresHTTPSBaseURLAndAPIKey(t *testing.T) {
	if _, err := NewClient(Config{BaseURL: "", APIKey: "key"}); err == nil {
		t.Fatal("expected missing baseUrl error")
	}
	if _, err := NewClient(Config{BaseURL: "http://example.test", APIKey: "key"}); err == nil {
		t.Fatal("expected non-https baseUrl error")
	}
	if _, err := NewClient(Config{BaseURL: "https://user:pass@example.test", APIKey: "key"}); err == nil {
		t.Fatal("expected URL user info error")
	}
	if _, err := NewClient(Config{BaseURL: "https://example.test", APIKey: ""}); err == nil {
		t.Fatal("expected missing apiKey error")
	}
}

func TestGetJSONAddsHeadersAndQuery(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.Header.Get("X-API-KEY") != "test-key" {
			t.Fatalf("X-API-KEY = %q", r.Header.Get("X-API-KEY"))
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Fatalf("Accept = %q", r.Header.Get("Accept"))
		}
		if r.URL.Path != "/proxy/network/integration/v1/sites" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("limit") != "25" || r.URL.Query().Get("filter") != "name.eq('Default')" {
			t.Fatalf("query = %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"site-1","apiKey":"leak"}]}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:            server.URL + "/proxy/network/integration",
		APIKey:             "test-key",
		Timeout:            time.Second,
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	query := url.Values{}
	query.Set("limit", "25")
	query.Set("filter", "name.eq('Default')")
	data, err := client.GetJSON(context.Background(), "/v1/sites", query)
	if err != nil {
		t.Fatalf("GetJSON failed: %v", err)
	}
	if strings.Contains(string(data), "leak") {
		t.Fatalf("sensitive value leaked in %s", data)
	}
	if !strings.Contains(string(data), "[REDACTED") {
		t.Fatalf("redacted marker missing in %s", data)
	}
}

func TestGetJSONReportsHTTPErrorWithRedaction(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"apiKey":"super-secret","error":"denied"}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:            server.URL,
		APIKey:             "test-key",
		Timeout:            time.Second,
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	_, err = client.GetJSON(context.Background(), "/v1/info", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("sensitive value leaked in error: %v", err)
	}
}

func TestDoJSONSendsMutationBodyAndNormalizesEmptySuccess(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("method = %s, want PUT", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
		}
		if r.URL.Path != "/proxy/network/integration/v1/sites/site-1/networks/network-1" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["name"] != "Updated" {
			t.Fatalf("body = %#v", body)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:            server.URL + "/proxy/network/integration",
		APIKey:             "test-key",
		Timeout:            time.Second,
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	data, err := client.DoJSON(
		context.Background(),
		http.MethodPut,
		"/v1/sites/site-1/networks/network-1",
		nil,
		map[string]interface{}{"name": "Updated"},
	)
	if err != nil {
		t.Fatalf("DoJSON failed: %v", err)
	}
	if string(data) != `{"status":"success","statusCode":204}` {
		t.Fatalf("data = %s", data)
	}
}

func TestDoJSONRejectsUnsupportedMethod(t *testing.T) {
	client, err := NewClient(Config{BaseURL: "https://example.test", APIKey: "key"})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if _, err := client.DoJSON(context.Background(), http.MethodOptions, "/v1/info", nil, nil); err == nil {
		t.Fatal("expected unsupported method error")
	}
}
