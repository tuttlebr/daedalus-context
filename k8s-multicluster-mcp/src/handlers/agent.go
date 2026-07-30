package handlers

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/tuttlebr/kubernetes-mcp-server/pkg/agent"
)

// DevopsAgent returns a handler function for the devopsAgent tool.
func DevopsAgent(agentClient *agent.Client, forceReadOnly bool) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		prompt, err := getRequiredStringArg(args, "prompt")
		if err != nil {
			return nil, err
		}

		namespace := getStringArg(args, "namespace", "")
		model := getStringArg(args, "model", "")
		readOnly := getBoolArg(args, "readOnly", false)
		if forceReadOnly {
			readOnly = true
		}

		timeout := 300
		if val, ok := args["timeout"]; ok {
			switch v := val.(type) {
			case float64:
				timeout = int(v)
			case int:
				timeout = v
			}
		}

		params := agent.RunParams{
			Prompt:    prompt,
			Namespace: namespace,
			Model:     model,
			Timeout:   timeout,
			ReadOnly:  readOnly,
		}

		result, err := agentClient.Run(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("devops agent run failed: %w", err)
		}

		jsonResponse, err := marshalSafe(result)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize agent response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}
