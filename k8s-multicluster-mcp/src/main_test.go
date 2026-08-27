package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

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

func TestBoundToolResultCapsPlainText(t *testing.T) {
	result := mcp.NewToolResultText(strings.Repeat("verbose output\n", 10000))
	boundToolResult(result)

	content, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("unexpected content type %T", result.Content[0])
	}
	if len(content.Text) > 64*1024 {
		t.Fatalf("bounded response has %d bytes", len(content.Text))
	}
	if !strings.Contains(content.Text, "OUTPUT TRUNCATED") {
		t.Fatal("bounded response is missing truncation metadata")
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) >= 128*1024 {
		t.Fatalf("MCP result envelope has %d bytes, want less than 128KiB", len(encoded))
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
		t.Fatal("expected next bearer token to authorize during rotation")
	}

	req.Header.Del("Authorization")
	req.Header.Set("X-MCP-Token", "expected")
	if !authorizedRequest(req, "expected") {
		t.Fatal("expected X-MCP-Token to authorize")
	}
}

func TestHealthPathsAreUnauthenticatedAndLocal(t *testing.T) {
	for _, path := range []string{"/healthz", "/livez", "/readyz"} {
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
				t.Fatalf("expected %s to return 200, got %d", path, response.Code)
			}
		})
	}
}

func TestNonHealthPathStillRequiresAuth(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	response := httptest.NewRecorder()
	handler := authMiddleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		[]string{"secret"},
	)

	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.Code)
	}
}
