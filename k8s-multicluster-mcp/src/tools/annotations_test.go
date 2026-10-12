package tools

import (
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestDaedalusReadToolsPublishNonDestructiveAnnotations(t *testing.T) {
	for _, tool := range []mcp.Tool{
		GetAPIResourcesTool(), ListResourcesTool(), GetResourcesTool(),
		GetPodsLogsTools(), GetEventsTool(), GetResourceOwnersTool(),
		GetClusterSummaryTool(), ListContextsTool(),
	} {
		t.Run(tool.Name, func(t *testing.T) {
			// Verify the wire representation: mcp-go otherwise publishes its
			// destructive defaults even for read handlers.
			data, err := json.Marshal(tool)
			if err != nil {
				t.Fatal(err)
			}
			var wire struct {
				Annotations map[string]bool `json:"annotations"`
			}
			if err := json.Unmarshal(data, &wire); err != nil {
				t.Fatal(err)
			}
			read, readPresent := wire.Annotations["readOnlyHint"]
			destructive, destructivePresent := wire.Annotations["destructiveHint"]
			if !readPresent || !read || !destructivePresent || destructive {
				t.Fatalf("unexpected read annotations: %s", data)
			}
		})
	}
}

func TestMutationToolsRetainDestructiveAnnotations(t *testing.T) {
	for _, tool := range []mcp.Tool{ScaleResourceTool(), ExecInPodTool(), RunKubectlCommandTool(), SwitchContextTool()} {
		if tool.Annotations.ReadOnlyHint == nil || *tool.Annotations.ReadOnlyHint ||
			tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint {
			t.Fatalf("%s must retain destructive metadata", tool.Name)
		}
	}
}
