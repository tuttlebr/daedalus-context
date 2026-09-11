package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/tuttlebr/unifi-network-mcp/handlers"
	"github.com/tuttlebr/unifi-network-mcp/pkg/unifi"
	"github.com/tuttlebr/unifi-network-mcp/tools"
)

func TestInvalidSiteReturnsMCPToolErrorEnvelope(t *testing.T) {
	s := server.NewMCPServer("test", "test")
	endpoint, _ := unifi.EndpointByName("listAdoptedDevices")
	addAuditedTool(s, tools.EndpointTool(endpoint, tools.ToolDefaults{ConnectionLocked: true}), "get", handlers.EndpointHandler(endpoint, handlers.ClientDefaults{
		BaseURL: "https://controller.invalid", APIKey: "test-key", Locked: true,
	}))
	response := s.HandleMessage(context.Background(), json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"listAdoptedDevices","arguments":{"siteId":"default"}}}`))
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Error  json.RawMessage     `json:"error"`
		Result *mcp.CallToolResult `json:"result"`
	}
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Error) > 0 || envelope.Result == nil || !envelope.Result.IsError {
		t.Fatalf("expected result.isError instead of JSON-RPC internal error: %s", encoded)
	}
	if !strings.Contains(string(encoded), "data[].id") {
		t.Fatalf("corrective guidance missing from MCP response: %s", encoded)
	}
}

func TestGetEnvBoolDefault(t *testing.T) {
	t.Setenv("BOOL_TRUE", "true")
	t.Setenv("BOOL_FALSE", "0")
	t.Setenv("BOOL_INVALID", "maybe")

	if !getEnvBoolDefault("BOOL_TRUE", false) {
		t.Fatal("expected true env value")
	}
	if getEnvBoolDefault("BOOL_FALSE", true) {
		t.Fatal("expected false env value")
	}
	if !getEnvBoolDefault("BOOL_MISSING", true) {
		t.Fatal("expected missing env to use default")
	}
	if !getEnvBoolDefault("BOOL_INVALID", true) {
		t.Fatal("expected invalid env to use default")
	}
}

func TestAuthorizedRequest(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer expected")

	if !authorizedRequest(req, "expected") {
		t.Fatal("expected bearer token to authorize")
	}
	if authorizedRequest(req, "other") {
		t.Fatal("wrong bearer token authorized")
	}
	if !authorizedRequest(req, "old", "expected") {
		t.Fatal("expected next token to authorize during rotation")
	}

	req.Header.Del("Authorization")
	req.Header.Set("X-MCP-Token", "expected")
	if !authorizedRequest(req, "expected") {
		t.Fatal("expected X-MCP-Token to authorize")
	}
}

func TestHealthPathsBypassAuth(t *testing.T) {
	for _, path := range []string{"/healthz", "/livez", "/readyz", "/dependencyz"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			response := httptest.NewRecorder()
			handler := authMiddleware(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					handleHealth(w, r, nil)
				}),
				[]string{"secret"},
			)
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", response.Code)
			}
		})
	}
}
