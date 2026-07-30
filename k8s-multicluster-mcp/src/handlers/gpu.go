// Package handlers provides MCP tool handlers for GPU debugging and remediation.
package handlers

import (
	"context"
	"fmt"

	"github.com/tuttlebr/kubernetes-mcp-server/pkg/k8s"

	"github.com/mark3labs/mcp-go/mcp"
)

// GetGPUClusterOverview returns a handler for the getGPUClusterOverview tool.
func GetGPUClusterOverview(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			args = map[string]interface{}{}
		}

		includeNonGPUNodes := getBoolArg(args, "includeNonGPUNodes", false)
		includeEvents := getBoolArg(args, "includeEvents", true)

		overview, err := client.GetGPUClusterOverview(ctx, includeNonGPUNodes, includeEvents)
		if err != nil {
			return nil, fmt.Errorf("failed to get GPU cluster overview: %w", err)
		}

		jsonResponse, err := marshalSafe(overview)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// DiagnoseGPUScheduling returns a handler for the diagnoseGPUScheduling tool.
func DiagnoseGPUScheduling(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		podName, err := getRequiredStringArg(args, "podName")
		if err != nil {
			return nil, err
		}

		namespace, err := getRequiredStringArg(args, "namespace")
		if err != nil {
			return nil, err
		}

		diagnosis, err := client.DiagnoseGPUScheduling(ctx, podName, namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to diagnose GPU scheduling for pod '%s': %w", podName, err)
		}

		jsonResponse, err := marshalSafe(diagnosis)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// GetGPUOperatorHealth returns a handler for the getGPUOperatorHealth tool.
func GetGPUOperatorHealth(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			args = map[string]interface{}{}
		}

		devicePluginNamespace := getStringArg(args, "devicePluginNamespace", "kube-system")
		gpuOperatorNamespace := getStringArg(args, "gpuOperatorNamespace", "gpu-operator")

		// Handle logLines as either float64 or int
		logLines := 100
		if val, ok := args["logLines"]; ok {
			switch v := val.(type) {
			case float64:
				logLines = int(v)
			case int:
				logLines = v
			}
		}

		health, err := client.GetGPUOperatorHealth(ctx, devicePluginNamespace, gpuOperatorNamespace, logLines)
		if err != nil {
			return nil, fmt.Errorf("failed to get GPU operator health: %w", err)
		}

		jsonResponse, err := marshalSafe(health)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// RemediateGPUIssue returns a handler for the remediateGPUIssue tool.
func RemediateGPUIssue(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		action, err := getRequiredStringArg(args, "action")
		if err != nil {
			return nil, err
		}

		nodeName := getStringArg(args, "nodeName", "")
		taintKey := getStringArg(args, "taintKey", "")
		taintEffect := getStringArg(args, "taintEffect", "NoSchedule")
		devicePluginNamespace := getStringArg(args, "devicePluginNamespace", "kube-system")
		gpuOperatorNamespace := getStringArg(args, "gpuOperatorNamespace", "gpu-operator")

		result, err := client.RemediateGPUIssue(ctx, action, nodeName, taintKey, taintEffect, devicePluginNamespace, gpuOperatorNamespace)
		if err != nil {
			return nil, fmt.Errorf("failed to remediate GPU issue (action: %s): %w", action, err)
		}

		jsonResponse, err := marshalSafe(result)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}
