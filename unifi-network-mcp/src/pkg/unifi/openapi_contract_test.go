package unifi

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

const openAPIContractVersion = "10.4.57"
const openAPIContractSHA256 = "3773947b4572b1bdc84272d3d2b9ebb5cad0d9aaa2640c3418568df97c9df59d"

type openAPIDocument struct {
	Info struct {
		Version string `json:"version"`
	} `json:"info"`
	Paths map[string]openAPIPath `json:"paths"`
}

type openAPIPath struct {
	Get *openAPIOperation `json:"get"`
}

type openAPIOperation struct {
	Parameters []openAPIParameter `json:"parameters"`
}

type openAPIParameter struct {
	In       string `json:"in"`
	Name     string `json:"name"`
	Required bool   `json:"required"`
	Schema   struct {
		Type    string `json:"type"`
		Default *int   `json:"default"`
		Minimum *int   `json:"minimum"`
		Maximum *int   `json:"maximum"`
	} `json:"schema"`
}

type expectedQueryParameter struct {
	Required bool
	Type     string
	Default  *int
	Minimum  *int
	Maximum  *int
}

func TestEndpointsMatchOpenAPIReadOnlyContract(t *testing.T) {
	contractPath := filepath.Join("..", "..", "..", "network_v10.4.57_openapi.json")
	data, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatalf("read OpenAPI contract %s: %v", contractPath, err)
	}
	if checksum := fmt.Sprintf("%x", sha256.Sum256(data)); checksum != openAPIContractSHA256 {
		t.Fatalf("OpenAPI checksum = %s, want %s", checksum, openAPIContractSHA256)
	}

	var document openAPIDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode OpenAPI contract: %v", err)
	}
	if document.Info.Version != openAPIContractVersion {
		t.Fatalf("OpenAPI version = %q, want %q", document.Info.Version, openAPIContractVersion)
	}

	registry := make(map[string]Endpoint, len(endpoints))
	for _, endpoint := range Endpoints() {
		if _, exists := registry[endpoint.Path]; exists {
			t.Fatalf("duplicate endpoint path %q", endpoint.Path)
		}
		registry[endpoint.Path] = endpoint
	}

	getCount := 0
	for path, pathItem := range document.Paths {
		if pathItem.Get == nil {
			continue
		}
		getCount++
		endpoint, ok := registry[path]
		if !ok {
			t.Errorf("OpenAPI GET %s is not exposed as an MCP tool", path)
			continue
		}
		delete(registry, path)
		compareOpenAPIParameters(t, endpoint, pathItem.Get.Parameters)
	}

	if getCount != len(endpoints) {
		t.Errorf("OpenAPI GET count = %d, endpoint count = %d", getCount, len(endpoints))
	}
	for path, endpoint := range registry {
		t.Errorf("MCP tool %s exposes %s, which is not an OpenAPI GET operation", endpoint.Name, path)
	}
}

func compareOpenAPIParameters(t *testing.T, endpoint Endpoint, parameters []openAPIParameter) {
	t.Helper()

	var actualPathParams []string
	actualQueryParams := make(map[string]openAPIParameter)
	for _, parameter := range parameters {
		switch parameter.In {
		case "path":
			if !parameter.Required {
				t.Errorf("%s path parameter %s is not required in OpenAPI", endpoint.Path, parameter.Name)
			}
			actualPathParams = append(actualPathParams, parameter.Name)
		case "query":
			actualQueryParams[parameter.Name] = parameter
		}
	}

	expectedPathParams := append([]string(nil), endpoint.PathParams...)
	sort.Strings(expectedPathParams)
	sort.Strings(actualPathParams)
	if !reflect.DeepEqual(actualPathParams, expectedPathParams) {
		t.Errorf("%s path parameters = %v, MCP registry has %v", endpoint.Path, actualPathParams, expectedPathParams)
	}

	expectedQueryParams := make(map[string]expectedQueryParameter)
	if endpoint.SupportsPagination {
		expectedQueryParams["offset"] = expectedQueryParameter{
			Type:    "integer",
			Default: intPointer(0),
			Minimum: intPointer(0),
		}
		expectedQueryParams["limit"] = expectedQueryParameter{
			Type:    "integer",
			Default: intPointer(endpoint.PaginationDefaultLimit()),
			Minimum: intPointer(0),
			Maximum: intPointer(endpoint.PaginationMaximumLimit()),
		}
	}
	if endpoint.SupportsFilter {
		expectedQueryParams["filter"] = expectedQueryParameter{Type: "string"}
	}
	for _, parameter := range endpoint.QueryParams {
		expectedQueryParams[parameter.Name] = expectedQueryParameter{
			Required: parameter.Required,
			Type:     "string",
		}
	}

	for name, expected := range expectedQueryParams {
		actual, ok := actualQueryParams[name]
		if !ok {
			t.Errorf("%s MCP query parameter %s is absent from OpenAPI", endpoint.Path, name)
			continue
		}
		delete(actualQueryParams, name)
		if actual.Required != expected.Required {
			t.Errorf("%s query parameter %s required = %t, want %t", endpoint.Path, name, actual.Required, expected.Required)
		}
		actualType := actual.Schema.Type
		// The published device-tags filter omits its schema type; all filter
		// expressions are still represented as strings by the MCP contract.
		if endpoint.Path == "/v1/sites/{siteId}/device-tags" && name == "filter" && actualType == "" {
			actualType = "string"
		}
		if actualType != expected.Type {
			t.Errorf("%s query parameter %s type = %q, want %q", endpoint.Path, name, actualType, expected.Type)
		}
		if !reflect.DeepEqual(actual.Schema.Default, expected.Default) {
			t.Errorf("%s query parameter %s default = %v, want %v", endpoint.Path, name, actual.Schema.Default, expected.Default)
		}
		if !reflect.DeepEqual(actual.Schema.Minimum, expected.Minimum) {
			t.Errorf("%s query parameter %s minimum = %v, want %v", endpoint.Path, name, actual.Schema.Minimum, expected.Minimum)
		}
		if !reflect.DeepEqual(actual.Schema.Maximum, expected.Maximum) {
			t.Errorf("%s query parameter %s maximum = %v, want %v", endpoint.Path, name, actual.Schema.Maximum, expected.Maximum)
		}
	}

	for name := range actualQueryParams {
		t.Errorf("OpenAPI GET %s query parameter %s is not exposed by the MCP tool", endpoint.Path, name)
	}
}

func intPointer(value int) *int {
	return &value
}
