// Package handlers provides MCP tool handlers for interacting with Kubernetes.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tuttlebr/kubernetes-mcp-server/pkg/k8s"

	"github.com/mark3labs/mcp-go/mcp"
)

// marshalSafe serializes a value to JSON and applies size truncation to
// prevent oversized responses from being sent to the LLM.
func marshalSafe(v interface{}) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	var decoded interface{}
	if err := json.Unmarshal(data, &decoded); err == nil {
		data, err = json.Marshal(k8s.SanitizeForOutput(decoded))
		if err != nil {
			return nil, err
		}
	}

	return k8s.TruncateJSON(data), nil
}

// Helper functions for consistent parameter extraction
func getStringArg(args map[string]interface{}, key string, defaultValue string) string {
	if val, ok := args[key].(string); ok {
		return val
	}
	return defaultValue
}

func getBoolArg(args map[string]interface{}, key string, defaultValue bool) bool {
	if val, ok := args[key].(bool); ok {
		return val
	}
	return defaultValue
}

func getRequiredStringArg(args map[string]interface{}, key string) (string, error) {
	val, ok := args[key].(string)
	if !ok || val == "" {
		return "", fmt.Errorf("missing required parameter: %s", key)
	}
	return val, nil
}

func getIntArg(args map[string]interface{}, key string, defaultValue int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return defaultValue
}

func getStringArrayArg(args map[string]interface{}, key string) []string {
	val, ok := args[key]
	if !ok {
		return nil
	}
	arr, ok := val.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

// GetAPIResources returns a handler function for the getAPIResources tool.
// It retrieves API resources from the Kubernetes cluster based on the provided
// context and parameters (includeNamespaceScoped, includeClusterScoped).
// The result is serialized to JSON and returned.
func GetAPIResources(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Extract arguments
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		includeNamespaceScoped := getBoolArg(args, "includeNamespaceScoped", true)
		includeClusterScoped := getBoolArg(args, "includeClusterScoped", true)

		// Fetch API resources
		resources, err := client.GetAPIResources(ctx, includeNamespaceScoped, includeClusterScoped)
		if err != nil {
			return nil, fmt.Errorf("failed to get API resources: %w", err)
		}

		// Serialize response to JSON
		jsonResponse, err := marshalSafe(resources)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		// Return JSON response using NewToolResultText
		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// ListResources returns a handler function for the listResources tool.
// It lists resources in the Kubernetes cluster based on the provided kind,
// namespace, and labelSelector. The result is serialized to JSON and returned.
func ListResources(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Extract arguments - using capital K to match your tools definition
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		kind, err := getRequiredStringArg(args, "kind")
		if err != nil {
			return nil, err
		}

		namespace := getStringArg(args, "namespace", "")
		labelSelector := getStringArg(args, "labelSelector", "")
		fieldSelector := getStringArg(args, "fieldSelector", "")

		// Fetch resources
		resources, err := client.ListResources(ctx, kind, namespace, labelSelector, fieldSelector)
		if err != nil {
			return nil, fmt.Errorf("failed to list resources for kind '%s': %w", kind, err)
		}

		// Serialize response to JSON
		jsonResponse, err := marshalSafe(resources)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		// Return JSON response using NewToolResultText
		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// GetResources returns a handler function for the getResource tool.
// It retrieves a specific resource from the Kubernetes cluster based on the
// provided kind, name, and namespace. The result is serialized to JSON and returned.
func GetResources(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		kind, err := getRequiredStringArg(args, "kind")
		if err != nil {
			return nil, err
		}

		name, err := getRequiredStringArg(args, "name")
		if err != nil {
			return nil, err
		}

		namespace := getStringArg(args, "namespace", "")

		resource, err := client.GetResource(ctx, kind, name, namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to get resource '%s' of kind '%s': %w", name, kind, err)
		}

		jsonResponse, err := marshalSafe(resource)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// DescribeResources returns a handler function for the describeResource tool.
// It fetches the description (manifest) of a specific resource from the
// Kubernetes cluster based on the provided kind, name, and namespace.
// The result is serialized to JSON and returned.
func DescribeResources(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		kind, err := getRequiredStringArg(args, "kind")
		if err != nil {
			return nil, err
		}

		name, err := getRequiredStringArg(args, "name")
		if err != nil {
			return nil, err
		}

		namespace := getStringArg(args, "namespace", "")

		// Fetch resource description
		resourceDescription, err := client.DescribeResource(ctx, kind, name, namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to describe resource '%s' of kind '%s': %w", name, kind, err)
		}

		// Serialize response to JSON
		jsonResponse, err := marshalSafe(resourceDescription)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		// Return JSON response using NewToolResultText
		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// GetPodsLogs returns a handler function for the getPodsLogs tool.
// It retrieves logs for a specific pod from the Kubernetes cluster based on the
// provided name and namespace. The result is serialized to JSON and returned.
func GetPodsLogs(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		name, err := getRequiredStringArg(args, "name")
		if err != nil {
			return nil, err
		}

		namespace, err := getRequiredStringArg(args, "namespace")
		if err != nil {
			return nil, err
		}

		containerName := getStringArg(args, "containerName", "")

		logs, err := client.GetPodsLogs(ctx, namespace, containerName, name)
		if err != nil {
			return nil, fmt.Errorf("failed to get logs for pod '%s': %w", name, err)
		}

		// Return logs as plain text instead of JSON for better readability.
		return mcp.NewToolResultText(k8s.SanitizeText(logs)), nil
	}
}

// GetNodeMetrics returns a handler function for the getNodeMetrics tool.
// It retrieves resource usage metrics for a specific node from the Kubernetes
// cluster based on the provided node name. The result is serialized to JSON
// and returned.
func GetNodeMetrics(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		name, err := getRequiredStringArg(args, "name")
		if err != nil {
			return nil, err
		}

		resourceUsage, err := client.GetNodeMetrics(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("failed to get metrics for node '%s': %w", name, err)
		}

		jsonResponse, err := marshalSafe(resourceUsage)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// GetPodMetrics returns a handler function for the getPodMetrics tool.
// It retrieves CPU and Memory metrics for a specific pod from the Kubernetes
// cluster based on the provided namespace and pod name. The result is
// serialized to JSON and returned.
func GetPodMetrics(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		namespace, err := getRequiredStringArg(args, "namespace")
		if err != nil {
			return nil, err
		}

		podName, err := getRequiredStringArg(args, "podName")
		if err != nil {
			return nil, err
		}

		metrics, err := client.GetPodMetrics(ctx, namespace, podName)
		if err != nil {
			return nil, fmt.Errorf("failed to get metrics for pod '%s' in namespace '%s': %w", podName, namespace, err)
		}

		jsonResponse, err := marshalSafe(metrics)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize metrics response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// GetEvents returns a handler function for the getEvents tool.
// It retrieves events from the Kubernetes cluster based on the provided
// namespace and labelSelector. The result is serialized to JSON and returned.
func GetEvents(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		namespace := getStringArg(args, "namespace", "")
		labelSelector := getStringArg(args, "labelSelector", "")

		events, err := client.GetEvents(ctx, namespace, labelSelector)
		if err != nil {
			return nil, fmt.Errorf("failed to get events: %w", err)
		}

		jsonResponse, err := marshalSafe(events)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize events response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// CreateOrUpdateResource returns a handler function for the createOrUpdateResource tool.
// It creates or updates a resource in the Kubernetes cluster based on the provided
// namespace and manifest. The result is serialized to JSON and returned.
func CreateOrUpdateResourceJSON(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		manifest, err := getRequiredStringArg(args, "manifest")
		if err != nil {
			return nil, err
		}

		namespace := getStringArg(args, "namespace", "")
		kind := getStringArg(args, "kind", "")

		resource, err := client.CreateOrUpdateResourceJSON(ctx, namespace, manifest, kind)
		if err != nil {
			return nil, fmt.Errorf("failed to create or update resource: %w", err)
		}

		jsonResponse, err := marshalSafe(resource)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// CreateOrUpdateResourceYAML returns a handler function for the createOrUpdateResourceYAML tool.
// It creates or updates a resource in the Kubernetes cluster based on the provided
// namespace and YAML manifest. This function is specifically optimized for YAML input.
// The result is serialized to JSON and returned.
func CreateOrUpdateResourceYAML(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		yamlManifest, err := getRequiredStringArg(args, "yamlManifest")
		if err != nil {
			return nil, err
		}

		namespace := getStringArg(args, "namespace", "")
		kind := getStringArg(args, "kind", "")

		resource, err := client.CreateOrUpdateResourceYAML(ctx, namespace, yamlManifest, kind)
		if err != nil {
			return nil, fmt.Errorf("failed to create or update resource from YAML: %w", err)
		}

		jsonResponse, err := marshalSafe(resource)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// DeleteResource returns a handler function for the deleteResource tool.
// It deletes a resource in the Kubernetes cluster based on the provided
// namespace and kind. The result is serialized to JSON and returned.
func DeleteResource(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		kind, err := getRequiredStringArg(args, "kind")
		if err != nil {
			return nil, err
		}

		name, err := getRequiredStringArg(args, "name")
		if err != nil {
			return nil, err
		}

		namespace := getStringArg(args, "namespace", "")

		err = client.DeleteResource(ctx, kind, name, namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to delete resource: %w", err)
		}

		return mcp.NewToolResultText("Resource deleted successfully"), nil
	}
}

// getIngresses returns a handler function for the getIngresses tool.
// It retrieves ingress resources from the Kubernetes cluster based on the provided
// Host and Path. The result is serialized to JSON and returned.
func GetIngresses(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		host := getStringArg(args, "host", "")

		ingresses, err := client.GetIngresses(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("failed to get ingress resources: %w", err)
		}

		jsonResponse, err := marshalSafe(ingresses)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// RolloutRestartHandler returns a handler function for the rolloutRestart tool.
// It calls the Client.RolloutRestart method and serializes the result to JSON.
func RolloutRestart(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		kind := getStringArg(args, "kind", "")
		name := getStringArg(args, "name", "")
		namespace := getStringArg(args, "namespace", "")

		if kind == "" || name == "" || namespace == "" {
			return nil, fmt.Errorf("kind, name, and namespace are required")
		}

		result, err := client.RolloutRestart(ctx, kind, name, namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to rollout restart resource: %w", err)
		}

		jsonResponse, err := marshalSafe(result)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// Enhanced Resource Inspection Handlers

// GetResourceYAML returns a handler function for the getResourceYAML tool.
func GetResourceYAML(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		kind, err := getRequiredStringArg(args, "kind")
		if err != nil {
			return nil, err
		}

		name, err := getRequiredStringArg(args, "name")
		if err != nil {
			return nil, err
		}

		namespace := getStringArg(args, "namespace", "")

		yamlContent, err := client.GetResourceYAML(ctx, kind, name, namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to get resource YAML: %w", err)
		}

		return mcp.NewToolResultText(k8s.SanitizeText(yamlContent)), nil
	}
}

// GetResourceDiff returns a handler function for the getResourceDiff tool.
func GetResourceDiff(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		kind, err := getRequiredStringArg(args, "kind")
		if err != nil {
			return nil, err
		}

		name, err := getRequiredStringArg(args, "name")
		if err != nil {
			return nil, err
		}

		namespace := getStringArg(args, "namespace", "")
		compareWith := getStringArg(args, "compareWith", "previous")

		diff, err := client.GetResourceDiff(ctx, kind, name, namespace, compareWith)
		if err != nil {
			return nil, fmt.Errorf("failed to get resource diff: %w", err)
		}

		jsonResponse, err := marshalSafe(diff)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// GetNamespaceResources returns a handler function for the getNamespaceResources tool.
func GetNamespaceResources(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		namespace, err := getRequiredStringArg(args, "namespace")
		if err != nil {
			return nil, err
		}

		types := getStringArg(args, "types", "")
		includeSecrets := getBoolArg(args, "includeSecrets", false)

		resources, err := client.GetNamespaceResources(ctx, namespace, types, includeSecrets)
		if err != nil {
			return nil, fmt.Errorf("failed to get namespace resources: %w", err)
		}

		jsonResponse, err := marshalSafe(resources)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// GetResourceOwners returns a handler function for the getResourceOwners tool.
func GetResourceOwners(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		kind, err := getRequiredStringArg(args, "kind")
		if err != nil {
			return nil, err
		}

		name, err := getRequiredStringArg(args, "name")
		if err != nil {
			return nil, err
		}

		namespace := getStringArg(args, "namespace", "")
		includeChildren := getBoolArg(args, "includeChildren", false)

		owners, err := client.GetResourceOwners(ctx, kind, name, namespace, includeChildren)
		if err != nil {
			return nil, fmt.Errorf("failed to get resource owners: %w", err)
		}

		jsonResponse, err := marshalSafe(owners)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// Advanced Monitoring & Observability Handlers

// GetClusterHealth returns a handler function for the getClusterHealth tool.
func GetClusterHealth(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		includeMetrics := getBoolArg(args, "includeMetrics", true)
		includeEvents := getBoolArg(args, "includeEvents", true)

		health, err := client.GetClusterHealth(ctx, includeMetrics, includeEvents)
		if err != nil {
			return nil, fmt.Errorf("failed to get cluster health: %w", err)
		}

		jsonResponse, err := marshalSafe(health)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// GetResourceQuotas returns a handler function for the getResourceQuotas tool.
func GetResourceQuotas(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		namespace := getStringArg(args, "namespace", "")
		showPercentage := getBoolArg(args, "showPercentage", true)

		quotas, err := client.GetResourceQuotas(ctx, namespace, showPercentage)
		if err != nil {
			return nil, fmt.Errorf("failed to get resource quotas: %w", err)
		}

		jsonResponse, err := marshalSafe(quotas)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// GetLimitRanges returns a handler function for the getLimitRanges tool.
func GetLimitRanges(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		namespace := getStringArg(args, "namespace", "")

		limitRanges, err := client.GetLimitRanges(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to get limit ranges: %w", err)
		}

		jsonResponse, err := marshalSafe(limitRanges)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// GetTopPods returns a handler function for the getTopPods tool.
func GetTopPods(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		namespace := getStringArg(args, "namespace", "")
		sortBy := getStringArg(args, "sortBy", "cpu")

		// Handle limit as either float64 or int
		limit := 10
		if val, ok := args["limit"]; ok {
			switch v := val.(type) {
			case float64:
				limit = int(v)
			case int:
				limit = v
			}
		}

		topPods, err := client.GetTopPods(ctx, namespace, sortBy, limit)
		if err != nil {
			return nil, fmt.Errorf("failed to get top pods: %w", err)
		}

		jsonResponse, err := marshalSafe(topPods)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// GetTopNodes returns a handler function for the getTopNodes tool.
func GetTopNodes(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		sortBy := getStringArg(args, "sortBy", "cpu")
		includeConditions := getBoolArg(args, "includeConditions", true)

		topNodes, err := client.GetTopNodes(ctx, sortBy, includeConditions)
		if err != nil {
			return nil, fmt.Errorf("failed to get top nodes: %w", err)
		}

		jsonResponse, err := marshalSafe(topNodes)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// Debugging & Troubleshooting Handlers

// GetPodDebugInfo returns a handler function for the getPodDebugInfo tool.
func GetPodDebugInfo(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		name, err := getRequiredStringArg(args, "name")
		if err != nil {
			return nil, err
		}

		namespace, err := getRequiredStringArg(args, "namespace")
		if err != nil {
			return nil, err
		}

		includeLogs := getBoolArg(args, "includeLogs", true)

		// Handle logLines as either float64 or int
		logLines := 50
		if val, ok := args["logLines"]; ok {
			switch v := val.(type) {
			case float64:
				logLines = int(v)
			case int:
				logLines = v
			}
		}

		debugInfo, err := client.GetPodDebugInfo(ctx, name, namespace, includeLogs, logLines)
		if err != nil {
			return nil, fmt.Errorf("failed to get pod debug info: %w", err)
		}

		jsonResponse, err := marshalSafe(debugInfo)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// GetServiceEndpoints returns a handler function for the getServiceEndpoints tool.
func GetServiceEndpoints(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		name, err := getRequiredStringArg(args, "name")
		if err != nil {
			return nil, err
		}

		namespace, err := getRequiredStringArg(args, "namespace")
		if err != nil {
			return nil, err
		}

		checkHealth := getBoolArg(args, "checkHealth", true)

		endpoints, err := client.GetServiceEndpoints(ctx, name, namespace, checkHealth)
		if err != nil {
			return nil, fmt.Errorf("failed to get service endpoints: %w", err)
		}

		jsonResponse, err := marshalSafe(endpoints)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// GetNetworkPolicies returns a handler function for the getNetworkPolicies tool.
func GetNetworkPolicies(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		namespace, err := getRequiredStringArg(args, "namespace")
		if err != nil {
			return nil, err
		}

		podName := getStringArg(args, "podName", "")
		includeDetails := getBoolArg(args, "includeDetails", false)

		policies, err := client.GetNetworkPolicies(ctx, namespace, podName, includeDetails)
		if err != nil {
			return nil, fmt.Errorf("failed to get network policies: %w", err)
		}

		jsonResponse, err := marshalSafe(policies)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// GetSecurityContext returns a handler function for the getSecurityContext tool.
func GetSecurityContext(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		name, err := getRequiredStringArg(args, "name")
		if err != nil {
			return nil, err
		}

		namespace, err := getRequiredStringArg(args, "namespace")
		if err != nil {
			return nil, err
		}

		includeDefaults := getBoolArg(args, "includeDefaults", false)

		securityContext, err := client.GetSecurityContext(ctx, name, namespace, includeDefaults)
		if err != nil {
			return nil, fmt.Errorf("failed to get security context: %w", err)
		}

		jsonResponse, err := marshalSafe(securityContext)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// GetResourceHistory returns a handler function for the getResourceHistory tool.
func GetResourceHistory(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		kind, err := getRequiredStringArg(args, "kind")
		if err != nil {
			return nil, err
		}

		name, err := getRequiredStringArg(args, "name")
		if err != nil {
			return nil, err
		}

		namespace := getStringArg(args, "namespace", "")

		// Handle hours as either float64 or int
		hours := 24.0
		if val, ok := args["hours"]; ok {
			switch v := val.(type) {
			case float64:
				hours = v
			case int:
				hours = float64(v)
			}
		}

		history, err := client.GetResourceHistory(ctx, kind, name, namespace, hours)
		if err != nil {
			return nil, fmt.Errorf("failed to get resource history: %w", err)
		}

		jsonResponse, err := marshalSafe(history)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// ValidateManifest returns a handler function for the validateManifest tool.
func ValidateManifest(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		manifest, err := getRequiredStringArg(args, "manifest")
		if err != nil {
			return nil, err
		}

		format := getStringArg(args, "format", "")
		strict := getBoolArg(args, "strict", true)

		validation, err := client.ValidateManifest(ctx, manifest, format, strict)
		if err != nil {
			return nil, fmt.Errorf("failed to validate manifest: %w", err)
		}

		jsonResponse, err := marshalSafe(validation)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// Specialized Operations Handlers (Write - Only Available When Not Read-Only)

// ExecInPod returns a handler function for the execInPod tool.
func ExecInPod(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		name, err := getRequiredStringArg(args, "name")
		if err != nil {
			return nil, err
		}

		namespace, err := getRequiredStringArg(args, "namespace")
		if err != nil {
			return nil, err
		}

		command, err := getRequiredStringArg(args, "command")
		if err != nil {
			return nil, err
		}

		container := getStringArg(args, "container", "")
		stdin := getBoolArg(args, "stdin", false)
		tty := getBoolArg(args, "tty", false)

		result, err := client.ExecInPod(ctx, name, namespace, command, container, stdin, tty)
		if err != nil {
			return nil, fmt.Errorf("failed to execute command in pod: %w", err)
		}

		jsonResponse, err := marshalSafe(result)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// PortForward returns a handler function for the portForward tool.
func PortForward(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		name, err := getRequiredStringArg(args, "name")
		if err != nil {
			return nil, err
		}

		namespace, err := getRequiredStringArg(args, "namespace")
		if err != nil {
			return nil, err
		}

		ports, err := getRequiredStringArg(args, "ports")
		if err != nil {
			return nil, err
		}

		// Handle duration as either float64 or int
		duration := 60
		if val, ok := args["duration"]; ok {
			switch v := val.(type) {
			case float64:
				duration = int(v)
			case int:
				duration = v
			}
		}

		result, err := client.PortForward(ctx, name, namespace, ports, duration)
		if err != nil {
			return nil, fmt.Errorf("failed to set up port forwarding: %w", err)
		}

		jsonResponse, err := marshalSafe(result)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// CopyFromPod returns a handler function for the copyFromPod tool.
func CopyFromPod(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		name, err := getRequiredStringArg(args, "name")
		if err != nil {
			return nil, err
		}

		namespace, err := getRequiredStringArg(args, "namespace")
		if err != nil {
			return nil, err
		}

		srcPath, err := getRequiredStringArg(args, "srcPath")
		if err != nil {
			return nil, err
		}

		destPath, err := getRequiredStringArg(args, "destPath")
		if err != nil {
			return nil, err
		}

		container := getStringArg(args, "container", "")

		result, err := client.CopyFromPod(ctx, name, namespace, srcPath, destPath, container)
		if err != nil {
			return nil, fmt.Errorf("failed to copy from pod: %w", err)
		}

		jsonResponse, err := marshalSafe(result)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// CopyToPod returns a handler function for the copyToPod tool.
func CopyToPod(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		name, err := getRequiredStringArg(args, "name")
		if err != nil {
			return nil, err
		}

		namespace, err := getRequiredStringArg(args, "namespace")
		if err != nil {
			return nil, err
		}

		srcPath, err := getRequiredStringArg(args, "srcPath")
		if err != nil {
			return nil, err
		}

		destPath, err := getRequiredStringArg(args, "destPath")
		if err != nil {
			return nil, err
		}

		container := getStringArg(args, "container", "")

		result, err := client.CopyToPod(ctx, name, namespace, srcPath, destPath, container)
		if err != nil {
			return nil, fmt.Errorf("failed to copy to pod: %w", err)
		}

		jsonResponse, err := marshalSafe(result)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

func ListNamespaces(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			args = map[string]interface{}{}
		}

		labelSelector := getStringArg(args, "labelSelector", "")

		namespaces, err := client.ListNamespaces(ctx, labelSelector)
		if err != nil {
			return nil, fmt.Errorf("failed to list namespaces: %w", err)
		}

		jsonResponse, err := marshalSafe(namespaces)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

func GetRolloutStatus(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		kind, err := getRequiredStringArg(args, "kind")
		if err != nil {
			return nil, err
		}

		name, err := getRequiredStringArg(args, "name")
		if err != nil {
			return nil, err
		}

		namespace, err := getRequiredStringArg(args, "namespace")
		if err != nil {
			return nil, err
		}

		status, err := client.GetRolloutStatus(ctx, kind, name, namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to get rollout status: %w", err)
		}

		jsonResponse, err := marshalSafe(status)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

func ScaleResource(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		kind, err := getRequiredStringArg(args, "kind")
		if err != nil {
			return nil, err
		}

		name, err := getRequiredStringArg(args, "name")
		if err != nil {
			return nil, err
		}

		namespace, err := getRequiredStringArg(args, "namespace")
		if err != nil {
			return nil, err
		}

		var replicas int32
		if val, ok := args["replicas"]; ok {
			switch v := val.(type) {
			case float64:
				replicas = int32(v)
			case int:
				replicas = int32(v)
			default:
				return nil, fmt.Errorf("invalid replicas value")
			}
		} else {
			return nil, fmt.Errorf("missing required parameter: replicas")
		}

		result, err := client.ScaleResource(ctx, kind, name, namespace, replicas)
		if err != nil {
			return nil, fmt.Errorf("failed to scale resource: %w", err)
		}

		jsonResponse, err := marshalSafe(result)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

func GetClusterSummary(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			args = map[string]interface{}{}
		}

		includeNamespaceDetails := getBoolArg(args, "includeNamespaceDetails", true)

		summary, err := client.GetClusterSummary(ctx, includeNamespaceDetails)
		if err != nil {
			return nil, fmt.Errorf("failed to get cluster summary: %w", err)
		}

		jsonResponse, err := marshalSafe(summary)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

func ListContexts(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		contexts, err := client.ListContexts()
		if err != nil {
			return nil, fmt.Errorf("failed to list contexts: %w", err)
		}

		jsonResponse, err := marshalSafe(contexts)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// RunKubectlCommand returns a handler for the runKubectlCommand tool.
func RunKubectlCommand(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		command, err := getRequiredStringArg(args, "command")
		if err != nil {
			return nil, err
		}

		// Handle timeout as either float64 or int
		timeout := 30
		if val, ok := args["timeout"]; ok {
			switch v := val.(type) {
			case float64:
				timeout = int(v)
			case int:
				timeout = v
			}
		}

		result, err := client.RunKubectlCommand(ctx, command, timeout)
		if err != nil {
			return nil, fmt.Errorf("failed to run kubectl command: %w", err)
		}

		jsonResponse, err := marshalSafe(result)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

func SwitchContext(client *k8s.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		contextName, err := getRequiredStringArg(args, "context")
		if err != nil {
			return nil, err
		}

		result, err := client.SwitchContext(ctx, contextName)
		if err != nil {
			return nil, fmt.Errorf("failed to switch context: %w", err)
		}

		jsonResponse, err := marshalSafe(result)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}
