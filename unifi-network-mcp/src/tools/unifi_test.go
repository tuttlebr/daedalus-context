package tools

import (
	"testing"

	"github.com/tuttlebr/unifi-network-mcp/pkg/unifi"
)

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

func requiredContains(required []string, name string) bool {
	for _, item := range required {
		if item == name {
			return true
		}
	}
	return false
}
