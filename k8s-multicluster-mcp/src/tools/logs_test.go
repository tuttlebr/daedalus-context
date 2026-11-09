package tools

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/tuttlebr/kubernetes-mcp-server/pkg/k8s"
)

func requireBoundedLogProperty(t *testing.T, tool *mcp.Tool, propertyName string, defaultValue int, maximum int) {
	t.Helper()
	property, ok := tool.InputSchema.Properties[propertyName].(map[string]interface{})
	if !ok {
		t.Fatalf("%s schema is missing property %q", tool.Name, propertyName)
	}
	if property["default"] != float64(defaultValue) {
		t.Fatalf("%s.%s default = %v, want %d", tool.Name, propertyName, property["default"], defaultValue)
	}
	if property["minimum"] != float64(1) || property["maximum"] != float64(maximum) {
		t.Fatalf("%s.%s bounds = %v..%v, want 1..%d", tool.Name, propertyName, property["minimum"], property["maximum"], maximum)
	}
	if property["multipleOf"] != float64(1) {
		t.Fatalf("%s.%s must require whole numbers", tool.Name, propertyName)
	}
}

func TestLogToolsExposeBoundedTimeWindows(t *testing.T) {
	tests := []struct {
		tool        mcp.Tool
		lineArg     string
		defaultLine int
	}{
		{tool: GetPodsLogsTools(), lineArg: "tailLines", defaultLine: k8s.DefaultPodLogTailLines},
		{tool: GetPodDebugInfoTool(), lineArg: "logLines", defaultLine: k8s.DefaultDebugLogLines},
		{tool: GetGPUOperatorHealthTool(), lineArg: "logLines", defaultLine: k8s.DefaultPodLogTailLines},
	}

	for _, test := range tests {
		t.Run(test.tool.Name, func(t *testing.T) {
			requireBoundedLogProperty(t, &test.tool, "sinceSeconds", k8s.DefaultLogSinceSeconds, k8s.MaxLogSinceSeconds)
			requireBoundedLogProperty(t, &test.tool, test.lineArg, test.defaultLine, k8s.MaxLogTailLines)
		})
	}
}
