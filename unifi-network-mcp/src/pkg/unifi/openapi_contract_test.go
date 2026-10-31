package unifi

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
)

type openAPITestDocument struct {
	Info struct {
		Version string `json:"version"`
	} `json:"info"`
	Paths map[string]map[string]json.RawMessage `json:"paths"`
}

type openAPITestOperation struct {
	OperationID string                  `json:"operationId"`
	Parameters  []openAPITestParameter  `json:"parameters"`
	RequestBody *openAPITestRequestBody `json:"requestBody"`
}

type openAPITestRequestBody struct {
	Required bool `json:"required"`
	Content  map[string]struct {
		Schema map[string]any `json:"schema"`
	} `json:"content"`
}

type openAPITestParameter struct {
	In       string `json:"in"`
	Name     string `json:"name"`
	Required bool   `json:"required"`
	Schema   struct {
		Type    string   `json:"type"`
		Format  string   `json:"format"`
		Default any      `json:"default"`
		Minimum *float64 `json:"minimum"`
		Maximum *float64 `json:"maximum"`
	} `json:"schema"`
}

type expectedParameter struct {
	Required bool
	Type     string
	Format   string
	Default  any
	Minimum  *float64
	Maximum  *float64
}

func TestEmbeddedOpenAPIContractMatchesCanonicalArtifact(t *testing.T) {
	data := readCanonicalOpenAPIContract(t)
	if !bytes.Equal(data, embeddedOpenAPIContract) {
		t.Fatal("embedded OpenAPI contract differs from canonical repository artifact")
	}
}

func TestSuppliedNetwork10567CaptureMatchesOperationContract(t *testing.T) {
	capturePath := filepath.Join("testdata", "network_v10.5.67_operations.json")
	capture, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("read UniFi Network 10.5.67 capture %s: %v", capturePath, err)
	}
	var captured struct {
		Version    string   `json:"version"`
		Operations []string `json:"operations"`
	}
	if err := json.Unmarshal(capture, &captured); err != nil {
		t.Fatalf("decode captured operation fixture: %v", err)
	}
	if captured.Version != "10.5.67" {
		t.Fatal("supplied API capture does not identify UniFi Network 10.5.67")
	}

	actual := map[string]struct{}{}
	for _, operation := range captured.Operations {
		actual[operation] = struct{}{}
	}

	var document openAPITestDocument
	if err := json.Unmarshal(embeddedOpenAPIContract, &document); err != nil {
		t.Fatalf("decode embedded OpenAPI contract: %v", err)
	}
	expected := map[string]struct{}{}
	for path, pathItem := range document.Paths {
		for method := range pathItem {
			if isSupportedMethod(strings.ToUpper(method)) {
				expected[endpointContractKey(method, path)] = struct{}{}
			}
		}
	}
	if !reflect.DeepEqual(actual, expected) {
		for operation := range expected {
			if _, ok := actual[operation]; !ok {
				t.Errorf("10.5.67 capture is missing %s", operation)
			}
		}
		for operation := range actual {
			if _, ok := expected[operation]; !ok {
				t.Errorf("10.5.67 capture adds undocumented contract operation %s", operation)
			}
		}
	}
}

func TestEndpointsMatchCompleteOpenAPIContract(t *testing.T) {
	data := readCanonicalOpenAPIContract(t)

	var document openAPITestDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode OpenAPI contract: %v", err)
	}
	if document.Info.Version != openAPIContractVersion {
		t.Fatalf("OpenAPI version = %q, want %q", document.Info.Version, openAPIContractVersion)
	}

	registry := make(map[string]Endpoint, len(endpoints))
	for _, endpoint := range Endpoints() {
		key := endpointContractKey(endpoint.Method, endpoint.Path)
		if _, exists := registry[key]; exists {
			t.Fatalf("duplicate endpoint %s", key)
		}
		registry[key] = endpoint
	}

	methodCounts := map[string]int{}
	for path, pathItem := range document.Paths {
		for method, rawOperation := range pathItem {
			method = strings.ToUpper(method)
			if !isSupportedMethod(method) {
				continue
			}
			methodCounts[method]++

			var operation openAPITestOperation
			if err := json.Unmarshal(rawOperation, &operation); err != nil {
				t.Fatalf("decode OpenAPI %s %s: %v", method, path, err)
			}
			key := endpointContractKey(method, path)
			endpoint, ok := registry[key]
			if !ok {
				t.Errorf("OpenAPI %s is not exposed as an MCP tool", key)
				continue
			}
			delete(registry, key)

			if method != methodGET && endpoint.Name != operation.OperationID {
				t.Errorf("%s tool name = %q, want operationId %q", key, endpoint.Name, operation.OperationID)
			}
			compareOpenAPIParameters(t, endpoint, operation.Parameters)
			compareOpenAPIRequestBody(t, endpoint, operation.RequestBody)
		}
	}

	wantCounts := map[string]int{
		methodGET:    41,
		methodPOST:   12,
		methodPUT:    9,
		methodPATCH:  1,
		methodDELETE: 10,
	}
	if !reflect.DeepEqual(methodCounts, wantCounts) {
		t.Errorf("OpenAPI method counts = %v, want %v", methodCounts, wantCounts)
	}
	for key, endpoint := range registry {
		t.Errorf("MCP tool %s exposes %s, which is absent from OpenAPI", endpoint.Name, key)
	}
}

func readCanonicalOpenAPIContract(t *testing.T) []byte {
	t.Helper()
	contractPath := "network_v10.4.57_openapi.json"
	data, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatalf("read OpenAPI contract %s: %v", contractPath, err)
	}
	if checksum := fmt.Sprintf("%x", sha256.Sum256(data)); checksum != openAPIContractSHA256 {
		t.Fatalf("OpenAPI checksum = %s, want %s", checksum, openAPIContractSHA256)
	}
	return data
}

func endpointContractKey(method, path string) string {
	return strings.ToUpper(method) + " " + path
}

func compareOpenAPIParameters(t *testing.T, endpoint Endpoint, parameters []openAPITestParameter) {
	t.Helper()

	actualPathParams := make(map[string]openAPITestParameter)
	actualQueryParams := make(map[string]openAPITestParameter)
	for _, parameter := range parameters {
		switch parameter.In {
		case "path":
			if !parameter.Required {
				t.Errorf("%s path parameter %s is not required in OpenAPI", endpoint.Path, parameter.Name)
			}
			actualPathParams[parameter.Name] = parameter
		case "query":
			actualQueryParams[parameter.Name] = parameter
		}
	}

	expectedPathParams := append([]string(nil), endpoint.PathParams...)
	actualPathNames := make([]string, 0, len(actualPathParams))
	for name := range actualPathParams {
		actualPathNames = append(actualPathNames, name)
	}
	sort.Strings(expectedPathParams)
	sort.Strings(actualPathNames)
	if !slices.Equal(actualPathNames, expectedPathParams) {
		t.Errorf("%s path parameters = %v, MCP registry has %v", endpoint.Path, actualPathNames, expectedPathParams)
	}
	for name, actual := range actualPathParams {
		expected := endpoint.PathParameterSchema(name)
		if actual.Schema.Type != expected.Type {
			t.Errorf("%s path parameter %s type = %q, want %q", endpoint.Path, name, actual.Schema.Type, expected.Type)
		}
		if actual.Schema.Format != expected.Format {
			t.Errorf("%s path parameter %s format = %q, want %q", endpoint.Path, name, actual.Schema.Format, expected.Format)
		}
	}

	expectedQueryParams := make(map[string]expectedParameter)
	if endpoint.SupportsPagination {
		expectedQueryParams["offset"] = expectedParameter{
			Type:    "integer",
			Format:  "int32",
			Default: float64(0),
			Minimum: floatPointer(0),
		}
		expectedQueryParams["limit"] = expectedParameter{
			Type:    "integer",
			Format:  "int32",
			Default: float64(endpoint.PaginationDefaultLimit()),
			Minimum: floatPointer(0),
			Maximum: floatPointer(float64(endpoint.PaginationMaximumLimit())),
		}
	}
	if endpoint.SupportsFilter {
		expectedQueryParams["filter"] = expectedParameter{Type: "string"}
	}
	for _, parameter := range endpoint.QueryParams {
		schema := parameter.Schema.withDefaults()
		expectedQueryParams[parameter.Name] = expectedParameter{
			Required: parameter.Required,
			Type:     schema.Type,
			Format:   schema.Format,
			Default:  schema.Default,
			Minimum:  schema.Minimum,
			Maximum:  schema.Maximum,
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
		if actual.Schema.Format != expected.Format {
			t.Errorf("%s query parameter %s format = %q, want %q", endpoint.Path, name, actual.Schema.Format, expected.Format)
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
		t.Errorf("OpenAPI %s %s query parameter %s is not exposed by the MCP tool", endpoint.Method, endpoint.Path, name)
	}
}

func compareOpenAPIRequestBody(t *testing.T, endpoint Endpoint, actual *openAPITestRequestBody) {
	t.Helper()
	if actual == nil {
		if endpoint.RequestBody != nil {
			t.Errorf("%s %s exposes an undocumented request body", endpoint.Method, endpoint.Path)
		}
		return
	}
	if endpoint.RequestBody == nil {
		t.Errorf("%s %s omits its documented request body", endpoint.Method, endpoint.Path)
		return
	}
	if endpoint.RequestBody.Required != actual.Required {
		t.Errorf("%s %s request body required = %t, want %t", endpoint.Method, endpoint.Path, endpoint.RequestBody.Required, actual.Required)
	}
	if _, ok := actual.Content["application/json"]; !ok {
		t.Errorf("%s %s OpenAPI request body has no application/json schema", endpoint.Method, endpoint.Path)
	}
	assertNoComponentReferences(t, endpoint.RequestBody.Schema)
	assertNoComponentReferences(t, endpoint.RequestBody.Definitions)
	if len(endpoint.RequestBody.Definitions) == 0 {
		t.Errorf("%s %s request body has no local schema definitions", endpoint.Method, endpoint.Path)
	}
}

func assertNoComponentReferences(t *testing.T, value any) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		for _, child := range typed {
			assertNoComponentReferences(t, child)
		}
	case []any:
		for _, child := range typed {
			assertNoComponentReferences(t, child)
		}
	case string:
		if strings.HasPrefix(typed, componentSchemaPrefix) {
			t.Errorf("request schema contains unresolved OpenAPI reference %q", typed)
		}
	}
}

func floatPointer(value float64) *float64 {
	return &value
}
