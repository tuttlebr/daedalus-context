package tools

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/tuttlebr/kubernetes-mcp-server/pkg/k8s"
)

func TestHighVolumeToolsExposeCompactDefaults(t *testing.T) {
	tests := []struct {
		tool         mcp.Tool
		propertyName string
		defaultValue int
		maximum      int
	}{
		{tool: GetAPIResourcesTool(), propertyName: "limit", defaultValue: k8s.DefaultAPIResourceLimit, maximum: k8s.MaxAPIResourceLimit},
		{tool: ListResourcesTool(), propertyName: "limit", defaultValue: k8s.DefaultResourceListLimit, maximum: k8s.MaxResourceListLimit},
		{tool: GetEventsTool(), propertyName: "limit", defaultValue: k8s.DefaultEventLimit, maximum: k8s.MaxEventLimit},
	}

	for _, test := range tests {
		t.Run(test.tool.Name, func(t *testing.T) {
			requireBoundedLogProperty(t, &test.tool, test.propertyName, test.defaultValue, test.maximum)
		})
	}

	includeDetails, ok := ListResourcesTool().InputSchema.Properties["includeDetails"].(map[string]interface{})
	if !ok || includeDetails["default"] != false {
		t.Fatalf("listResources.includeDetails default = %v, want false", includeDetails["default"])
	}

	includeNamespaceDetails, ok := GetClusterSummaryTool().InputSchema.Properties["includeNamespaceDetails"].(map[string]interface{})
	if !ok || includeNamespaceDetails["default"] != false {
		t.Fatalf("getClusterSummary.includeNamespaceDetails default = %v, want false", includeNamespaceDetails["default"])
	}
}
