package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tuttlebr/unifi-network-mcp/pkg/unifi"
)

func TestSiteScopedToolsExplainUUIDDiscovery(t *testing.T) {
	for _, tool := range EndpointTools() {
		property, scoped := tool.InputSchema.Properties["siteId"]
		if !scoped {
			continue
		}
		schema := property.(map[string]any)
		if schema["format"] != "uuid" || !requiredContains(tool.InputSchema.Required, "siteId") {
			t.Errorf("%s must require a site UUID", tool.Name)
		}
		description := schema["description"].(string)
		for _, fragment := range []string{"listSites", "wait for its result", "data[].id", "internalReference", "default"} {
			if !strings.Contains(description, fragment) {
				t.Errorf("%s siteId description omits %q", tool.Name, fragment)
			}
		}
	}
	endpoint, _ := unifi.EndpointByName("listSites")
	tool := EndpointTool(endpoint, ToolDefaults{})
	if !strings.Contains(tool.Description, "data[].id") || !strings.Contains(tool.Description, "internalReference") {
		t.Fatalf("site discovery omits ID selection guidance: %s", tool.Description)
	}
}

func TestEndpointToolAnnotationsAreReadOnly(t *testing.T) {
	endpoint, ok := unifi.EndpointByName("listSites")
	if !ok {
		t.Fatal("listSites endpoint missing")
	}
	tool := EndpointTool(endpoint, ToolDefaults{})
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Fatal("tool is not marked read-only")
	}
	if tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
		t.Fatal("tool is marked destructive")
	}
	if _, ok := tool.InputSchema.Properties["baseUrl"]; !ok {
		t.Fatal("baseUrl property missing")
	}
	if _, ok := tool.InputSchema.Properties["apiKey"]; !ok {
		t.Fatal("apiKey property missing")
	}
	if _, ok := tool.InputSchema.Properties["offset"]; !ok {
		t.Fatal("pagination offset property missing")
	}
	if _, ok := tool.InputSchema.Properties["filter"]; !ok {
		t.Fatal("filter property missing")
	}
}

func TestEndpointToolConnectionRequiredFieldsFollowDefaults(t *testing.T) {
	endpoint, ok := unifi.EndpointByName("listSites")
	if !ok {
		t.Fatal("listSites endpoint missing")
	}

	noDefaults := EndpointTool(endpoint, ToolDefaults{})
	if !requiredContains(noDefaults.InputSchema.Required, "baseUrl") {
		t.Fatal("baseUrl should be required without a default")
	}
	if !requiredContains(noDefaults.InputSchema.Required, "apiKey") {
		t.Fatal("apiKey should be required without a default")
	}

	withDefaults := EndpointTool(endpoint, ToolDefaults{
		HasBaseURLDefault: true,
		HasAPIKeyDefault:  true,
	})
	if requiredContains(withDefaults.InputSchema.Required, "baseUrl") {
		t.Fatal("baseUrl should not be required with a default")
	}
	if requiredContains(withDefaults.InputSchema.Required, "apiKey") {
		t.Fatal("apiKey should not be required with a default")
	}
}

func TestEndpointToolOmitsConnectionSecretsWhenLocked(t *testing.T) {
	endpoint, ok := unifi.EndpointByName("listSites")
	if !ok {
		t.Fatal("listSites endpoint missing")
	}
	tool := EndpointTool(endpoint, ToolDefaults{
		HasBaseURLDefault: true,
		HasAPIKeyDefault:  true,
		ConnectionLocked:  true,
	})
	for _, property := range []string{"baseUrl", "apiKey", "insecureSkipVerify"} {
		if _, exists := tool.InputSchema.Properties[property]; exists {
			t.Fatalf("%s must not be exposed in fixed-controller mode", property)
		}
	}
}

func TestEndpointToolsCountMatchesRegistry(t *testing.T) {
	if len(EndpointTools()) != len(unifi.Endpoints()) {
		t.Fatalf("tool count = %d, endpoint count = %d", len(EndpointTools()), len(unifi.Endpoints()))
	}
}

func TestEndpointToolUsesIntegerPaginationSchemaAndEndpointLimit(t *testing.T) {
	endpoint, ok := unifi.EndpointByName("listVouchers")
	if !ok {
		t.Fatal("listVouchers endpoint missing")
	}
	tool := EndpointTool(endpoint, ToolDefaults{})
	limit, ok := tool.InputSchema.Properties["limit"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected limit schema: %#v", tool.InputSchema.Properties["limit"])
	}
	if limit["type"] != "integer" {
		t.Fatalf("limit type = %v, want integer", limit["type"])
	}
	if limit["maximum"] != 1000 {
		t.Fatalf("limit maximum = %v, want 1000", limit["maximum"])
	}
}

func TestFirewallPolicyOrderingToolRequiresZonePair(t *testing.T) {
	endpoint, ok := unifi.EndpointByName("getFirewallPolicyOrdering")
	if !ok {
		t.Fatal("getFirewallPolicyOrdering endpoint missing")
	}
	tool := EndpointTool(endpoint, ToolDefaults{})
	for _, name := range []string{"sourceFirewallZoneId", "destinationFirewallZoneId"} {
		if _, ok := tool.InputSchema.Properties[name]; !ok {
			t.Fatalf("%s property missing", name)
		}
		if !requiredContains(tool.InputSchema.Required, name) {
			t.Fatalf("%s must be required", name)
		}
	}
}

func TestMutationToolAnnotationsAndRequestSchema(t *testing.T) {
	endpoint, ok := unifi.EndpointByName("createNetwork")
	if !ok {
		t.Fatal("createNetwork endpoint missing")
	}
	tool := EndpointTool(endpoint, ToolDefaults{})
	if tool.Annotations.ReadOnlyHint == nil || *tool.Annotations.ReadOnlyHint {
		t.Fatal("createNetwork must not be marked read-only")
	}
	if tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
		t.Fatal("createNetwork should be non-destructive because it creates rather than replaces state")
	}
	if tool.Annotations.IdempotentHint == nil || *tool.Annotations.IdempotentHint {
		t.Fatal("createNetwork POST must not be marked idempotent")
	}
	if _, ok := tool.InputSchema.Properties["body"]; !ok {
		t.Fatal("request body schema missing")
	}
	if !requiredContains(tool.InputSchema.Required, "body") {
		t.Fatal("request body must be required")
	}
	if len(tool.InputSchema.Defs) == 0 {
		t.Fatal("request body definitions missing")
	}
	body, ok := tool.InputSchema.Properties["body"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected body schema: %#v", tool.InputSchema.Properties["body"])
	}
	if _, ok := body["oneOf"]; !ok {
		t.Fatalf("discriminated network body schema has no oneOf: %#v", body)
	}
	if _, err := json.Marshal(tool); err != nil {
		t.Fatalf("marshal mutation tool: %v", err)
	}
}

func TestDestructiveToolAnnotationsAndTypedParameters(t *testing.T) {
	deleteNetwork, ok := unifi.EndpointByName("deleteNetwork")
	if !ok {
		t.Fatal("deleteNetwork endpoint missing")
	}
	tool := EndpointTool(deleteNetwork, ToolDefaults{})
	if tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint {
		t.Fatal("deleteNetwork must be marked destructive")
	}
	if tool.Annotations.IdempotentHint == nil || !*tool.Annotations.IdempotentHint {
		t.Fatal("deleteNetwork must be marked idempotent")
	}
	force, ok := tool.InputSchema.Properties["force"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected force schema: %#v", tool.InputSchema.Properties["force"])
	}
	if force["type"] != "boolean" || force["default"] != false {
		t.Fatalf("force schema = %#v", force)
	}

	portAction, ok := unifi.EndpointByName("executePortAction")
	if !ok {
		t.Fatal("executePortAction endpoint missing")
	}
	tool = EndpointTool(portAction, ToolDefaults{})
	portIdx, ok := tool.InputSchema.Properties["portIdx"].(map[string]any)
	if !ok || portIdx["type"] != "integer" {
		t.Fatalf("portIdx schema = %#v, want integer", portIdx)
	}
}

func TestEndpointToolSchemasStayBounded(t *testing.T) {
	data, err := json.Marshal(EndpointTools())
	if err != nil {
		t.Fatalf("marshal tools: %v", err)
	}
	if len(data) > 512*1024 {
		t.Fatalf("serialized tool registry = %d bytes, want <= 524288", len(data))
	}
	t.Logf("serialized tool registry: %d bytes", len(data))
}

func requiredContains(required []string, name string) bool {
	for _, item := range required {
		if item == name {
			return true
		}
	}
	return false
}
