package tools

import (
	"github.com/mark3labs/mcp-go/mcp"
)

// DevopsAgentTool creates a tool for an autonomous DevOps agent that manages Kubernetes clusters.
func DevopsAgentTool() mcp.Tool {
	return mcp.NewTool(
		"devopsAgent",
		mcp.WithDescription("Launch an autonomous DevOps agent for Kubernetes cluster management. "+
			"The agent can inspect, debug, and, when explicitly permitted, manage workloads using k8s and Helm MCP tools. "+
			"It runs headlessly via opencode, connecting to a child k8s-mcp-server instance for cluster access. "+
			"Provide a natural language description of what you need — the agent will autonomously execute the required operations "+
			"and produce a structured report. "+
			"Requires: opencode CLI installed, OPENCODE_BASE_URL, OPENCODE_API_KEY, and OPENCODE_MODEL env vars set. "+
			"When the parent MCP server runs in read-only mode or agent-write capability is disabled, this tool is forced into inspection-only mode."),
		mcp.WithString("prompt", mcp.Required(),
			mcp.Description("Natural language description of the Kubernetes task to perform. "+
				"Be specific: include resource names, namespaces, chart names, or error messages. "+
				"Examples: "+
				"\"Install the nginx-ingress Helm chart in namespace ingress-system with 3 replicas\", "+
				"\"Scale the api-gateway deployment in production to 5 replicas and verify rollout\", "+
				"\"Pods in namespace ml-training are stuck in Pending state with GPU resource requests\", "+
				"\"Upgrade the prometheus-stack Helm release to chart version 45.0.0 in namespace monitoring\".")),
		mcp.WithString("namespace",
			mcp.Description("Kubernetes namespace to focus operations on. "+
				"The agent will prioritize resources in this namespace but may inspect cluster-wide resources as needed.")),
		mcp.WithString("model",
			mcp.Description("Override the default LLM model for this agent run. "+
				"Format: \"provider-id/model-id\" matching the opencode provider configuration. "+
				"Uses OPENCODE_MODEL environment variable if omitted.")),
		mcp.WithNumber("timeout",
			mcp.Description("Maximum execution time in seconds for the agent run. "+
				"DevOps tasks typically take 1-5 minutes. Default: 300. Max: 900."),
			mcp.DefaultNumber(300)),
		mcp.WithBoolean("readOnly",
			mcp.Description("When true, the child k8s-mcp-server runs in read-only mode, "+
				"preventing the agent from making any cluster changes. "+
				"Defaults to false, but parent server read-only mode and disabled agent-write capability always override this to true."),
			mcp.DefaultBool(false)),
	)
}
