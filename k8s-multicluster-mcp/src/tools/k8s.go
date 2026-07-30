// Package tools provides MCP tool definitions for interacting with Kubernetes.
package tools

import (
	"github.com/mark3labs/mcp-go/mcp"
)

// GetAPIResourcesTool creates a tool for discovering available API resource types in the cluster.
func GetAPIResourcesTool() mcp.Tool {
	return mcp.NewTool(
		"getAPIResources",
		mcp.WithDescription("Discover all available API resource types (kinds) in the Kubernetes cluster. "+
			"Returns resource names, short names, API group, whether they are namespaced, and supported verbs. "+
			"Use this tool first when you need to know what resource types exist or what the correct resource name is. "+
			"Prefer getNamespaceResources if you want to list actual resource instances inside a namespace."),
		mcp.WithBoolean("includeNamespaceScoped",
			mcp.Description("Include namespace-scoped resource types in the results. Default: true."),
			mcp.DefaultBool(true)),
		mcp.WithBoolean("includeClusterScoped",
			mcp.Description("Include cluster-scoped resource types (e.g. nodes, namespaces, clusterroles) in the results. Default: true."),
			mcp.DefaultBool(true)),
	)
}

// ListResourcesTool creates a tool for listing resources of a specific type.
func ListResourcesTool() mcp.Tool {
	return mcp.NewTool(
		"listResources",
		mcp.WithDescription("List Kubernetes resources of a specific type. Returns resource names, namespaces, and status. "+
			"Use lowercase plural resource names (e.g. \"deployments\" not \"Deployment\"). "+
			"Omit namespace to search across all namespaces. "+
			"For listing all resource types within a single namespace, use getNamespaceResources instead."),
		mcp.WithString("kind", mcp.Required(),
			mcp.Description("Resource type in lowercase plural form. "+
				"Examples: \"deployments\", \"pods\", \"services\", \"statefulsets\", \"daemonsets\", "+
				"\"replicasets\", \"configmaps\", \"secrets\", \"jobs\", \"cronjobs\", \"ingresses\", "+
				"\"nodes\", \"namespaces\", \"persistentvolumeclaims\", \"persistentvolumes\", "+
				"\"serviceaccounts\", \"roles\", \"rolebindings\", \"clusterroles\", \"clusterrolebindings\". "+
				"Do NOT use PascalCase like \"Deployment\" or singular like \"pod\".")),
		mcp.WithString("namespace",
			mcp.Description("Kubernetes namespace to list from. Omit or leave empty to list across all namespaces. "+
				"Example: \"default\", \"kube-system\".")),
		mcp.WithString("labelSelector",
			mcp.Description("Kubernetes label selector to filter resources. "+
				"Uses standard label selector syntax. Examples: \"app=nginx\", \"env=production,tier=frontend\", \"app in (web,api)\". "+
				"Omit to return all resources of the specified kind.")),
		mcp.WithString("fieldSelector",
			mcp.Description("Kubernetes field selector to filter resources by field values. "+
				"Examples: \"status.phase=Running\", \"metadata.name=my-pod\", \"spec.nodeName=node1\". "+
				"Not all fields are supported for all resource types. Omit to skip field filtering.")),
	)
}

// GetResourcesTool creates a tool for getting a specific resource by name.
func GetResourcesTool() mcp.Tool {
	return mcp.NewTool(
		"getResource",
		mcp.WithDescription("Get the full JSON representation of a single Kubernetes resource by kind and name. "+
			"Returns the complete resource spec, status, metadata, and labels. "+
			"Use lowercase plural for kind. For a human-friendly summary, use describeResource instead. "+
			"For YAML output, use getResourceYAML."),
		mcp.WithString("kind", mcp.Required(),
			mcp.Description("Resource type in lowercase plural form. "+
				"Examples: \"deployments\", \"pods\", \"services\", \"configmaps\", \"secrets\". "+
				"Do NOT use PascalCase like \"Deployment\" or singular like \"pod\".")),
		mcp.WithString("name", mcp.Required(),
			mcp.Description("Exact name of the resource to retrieve. Example: \"my-nginx-deployment\".")),
		mcp.WithString("namespace",
			mcp.Description("Namespace of the resource. Required for namespace-scoped resources. "+
				"Omit for cluster-scoped resources like nodes, namespaces, or clusterroles.")),
	)
}

// DescribeResourcesTool creates a tool for describing a resource in a human-friendly format.
func DescribeResourcesTool() mcp.Tool {
	return mcp.NewTool(
		"describeResource",
		mcp.WithDescription("Get a human-friendly description of a Kubernetes resource, similar to 'kubectl describe'. "+
			"Includes events, conditions, and related information. "+
			"Use this for troubleshooting or understanding resource state. "+
			"For raw JSON output, use getResource instead."),
		mcp.WithString("kind", mcp.Required(),
			mcp.Description("Resource type in lowercase plural form. "+
				"Examples: \"deployments\", \"pods\", \"services\", \"configmaps\". "+
				"Do NOT use PascalCase like \"Deployment\" or singular like \"pod\".")),
		mcp.WithString("name", mcp.Required(),
			mcp.Description("Exact name of the resource to describe. Example: \"my-nginx-deployment\".")),
		mcp.WithString("namespace",
			mcp.Description("Namespace of the resource. Required for namespace-scoped resources. "+
				"Omit for cluster-scoped resources like nodes, namespaces, or clusterroles.")),
	)
}

// GetPodsLogsTools creates a tool for getting pod logs.
func GetPodsLogsTools() mcp.Tool {
	return mcp.NewTool(
		"getPodsLogs",
		mcp.WithDescription("Retrieve stdout/stderr logs from a specific pod. "+
			"Returns plain-text log output. If the pod has multiple containers, specify containerName "+
			"to target a specific container, otherwise logs from the default container are returned. "+
			"For comprehensive pod debugging including logs, events, and conditions, use getPodDebugInfo instead."),
		mcp.WithString("name", mcp.Required(),
			mcp.Description("Exact name of the pod. Example: \"my-app-7d9f5b4c6-x2k4m\".")),
		mcp.WithString("containerName",
			mcp.Description("Name of the container within the pod to get logs from. "+
				"Required if the pod has multiple containers (e.g. sidecars, init containers). "+
				"Omit to use the pod's default container.")),
		mcp.WithString("namespace", mcp.Required(),
			mcp.Description("Namespace where the pod is running. Example: \"default\", \"kube-system\".")),
	)
}

// GetNodeMetricsTools creates a tool for getting node-level resource usage metrics.
func GetNodeMetricsTools() mcp.Tool {
	return mcp.NewTool(
		"getNodeMetrics",
		mcp.WithDescription("Get CPU and memory resource usage metrics for a specific node. "+
			"Requires the metrics-server to be installed in the cluster. "+
			"For a cluster-wide view of node resource usage, use getTopNodes instead."),
		mcp.WithString("name", mcp.Required(),
			mcp.Description("Exact name of the node. Example: \"worker-node-01\". "+
				"Use listResources with kind \"nodes\" to discover node names.")),
	)
}

// GetPodMetricsTool creates a tool for getting pod-level resource usage metrics.
func GetPodMetricsTool() mcp.Tool {
	return mcp.NewTool(
		"getPodMetrics",
		mcp.WithDescription("Get CPU and memory usage metrics for a specific pod and its containers. "+
			"Requires the metrics-server to be installed in the cluster. "+
			"For a cluster-wide view of top resource-consuming pods, use getTopPods instead."),
		mcp.WithString("namespace", mcp.Required(),
			mcp.Description("Namespace where the pod is running. Example: \"default\".")),
		mcp.WithString("podName", mcp.Required(),
			mcp.Description("Exact name of the pod. Example: \"my-app-7d9f5b4c6-x2k4m\".")),
	)
}

// GetEventsTool creates a tool for getting cluster events.
func GetEventsTool() mcp.Tool {
	return mcp.NewTool(
		"getEvents",
		mcp.WithDescription("List Kubernetes events, which record state changes and errors for resources. "+
			"Events include information about scheduling, pulling images, container crashes, OOM kills, and more. "+
			"Useful for troubleshooting pods that won't start or resources that are failing. "+
			"Omit namespace to get events across all namespaces."),
		mcp.WithString("namespace",
			mcp.Description("Namespace to get events from. Omit or leave empty for events across all namespaces. "+
				"Example: \"default\", \"kube-system\".")),
		mcp.WithString("labelSelector",
			mcp.Description("Label selector to filter events. "+
				"Examples: \"app=nginx\", \"involvedObject.kind=Pod\". Omit to return all events.")),
	)
}

// CreateOrUpdateResourceJSONTool creates a tool for creating/updating resources from JSON manifests.
func CreateOrUpdateResourceJSONTool() mcp.Tool {
	return mcp.NewTool(
		"createResource",
		mcp.WithDescription("Create or update a Kubernetes resource from a JSON manifest string. "+
			"Performs a server-side apply (create-or-update). The manifest must be valid Kubernetes JSON. "+
			"For YAML manifests, use createResourceYAML instead, which has better YAML-specific error handling. "+
			"WRITE OPERATION: only available when server is not in read-only mode."),
		mcp.WithString("kind", mcp.Required(),
			mcp.Description("Resource type in lowercase plural form. "+
				"Examples: \"deployments\", \"services\", \"configmaps\". "+
				"Must match the kind in the JSON manifest.")),
		mcp.WithString("namespace",
			mcp.Description("Target namespace for the resource. Overrides namespace in the manifest if provided. "+
				"Omit for cluster-scoped resources.")),
		mcp.WithString("manifest", mcp.Required(),
			mcp.Description("Complete Kubernetes resource manifest as a JSON string. "+
				"Must include apiVersion, kind, metadata.name, and spec. "+
				"Example: {\"apiVersion\":\"v1\",\"kind\":\"ConfigMap\",\"metadata\":{\"name\":\"my-config\"},\"data\":{\"key\":\"value\"}}")),
	)
}

// CreateOrUpdateResourceYAMLTool creates a tool for creating/updating resources from YAML manifests.
func CreateOrUpdateResourceYAMLTool() mcp.Tool {
	return mcp.NewTool(
		"createResourceYAML",
		mcp.WithDescription("Create or update a Kubernetes resource from a YAML manifest string. "+
			"Performs a server-side apply (create-or-update). Optimized for YAML input with better error handling "+
			"for YAML parsing issues. Preferred over createResource for YAML manifests. "+
			"WRITE OPERATION: only available when server is not in read-only mode."),
		mcp.WithString("kind",
			mcp.Description("Resource type in lowercase plural form. Optional — will be inferred from the YAML manifest if not provided. "+
				"Examples: \"deployments\", \"services\", \"configmaps\".")),
		mcp.WithString("namespace",
			mcp.Description("Target namespace. Overrides the namespace specified in the YAML manifest if provided. "+
				"Omit for cluster-scoped resources.")),
		mcp.WithString("yamlManifest", mcp.Required(),
			mcp.Description("Complete Kubernetes resource manifest as a YAML string. "+
				"Must include apiVersion, kind, metadata.name, and spec. Must be valid Kubernetes YAML format.")),
	)
}

// DeleteResourceTool creates a tool for deleting resources.
func DeleteResourceTool() mcp.Tool {
	return mcp.NewTool(
		"deleteResource",
		mcp.WithDescription("Delete a Kubernetes resource by kind and name. This is a destructive operation that cannot be undone. "+
			"WRITE OPERATION: only available when server is not in read-only mode."),
		mcp.WithString("kind", mcp.Required(),
			mcp.Description("Resource type in lowercase plural form. "+
				"Examples: \"deployments\", \"pods\", \"services\", \"configmaps\", \"secrets\". "+
				"Do NOT use PascalCase like \"Deployment\" or singular like \"pod\".")),
		mcp.WithString("name", mcp.Required(),
			mcp.Description("Exact name of the resource to delete.")),
		mcp.WithString("namespace",
			mcp.Description("Namespace of the resource. Required for namespace-scoped resources. "+
				"Omit for cluster-scoped resources.")),
	)
}

// GetIngressesTool creates a tool for finding ingresses by hostname.
func GetIngressesTool() mcp.Tool {
	return mcp.NewTool(
		"getIngresses",
		mcp.WithDescription("Find Kubernetes Ingress resources that match a given hostname. "+
			"Returns ingress rules, paths, backends, and TLS configuration. "+
			"Useful for understanding how external traffic is routed to services."),
		mcp.WithString("host", mcp.Required(),
			mcp.Description("Hostname to search for in ingress rules. Example: \"app.example.com\". "+
				"Searches across all namespaces for ingresses matching this host.")),
	)
}

// RolloutRestartTool creates a tool for restarting workloads.
func RolloutRestartTool() mcp.Tool {
	return mcp.NewTool(
		"rolloutRestart",
		mcp.WithDescription("Perform a rolling restart on a workload, equivalent to 'kubectl rollout restart'. "+
			"Triggers a new rollout by updating the pod template annotation, causing pods to be recreated gradually. "+
			"WRITE OPERATION: only available when server is not in read-only mode."),
		mcp.WithString("kind", mcp.Required(),
			mcp.Description("Workload type to restart. Must be one of: \"Deployment\", \"DaemonSet\", \"StatefulSet\", \"ReplicaSet\". "+
				"Use PascalCase singular form exactly as shown.")),
		mcp.WithString("name", mcp.Required(),
			mcp.Description("Exact name of the workload resource to restart.")),
		mcp.WithString("namespace", mcp.Required(),
			mcp.Description("Namespace where the workload is running.")),
	)
}

// GetResourceYAMLTool creates a tool for exporting resources as YAML.
func GetResourceYAMLTool() mcp.Tool {
	return mcp.NewTool(
		"getResourceYAML",
		mcp.WithDescription("Export a Kubernetes resource as clean YAML, similar to 'kubectl get -o yaml'. "+
			"Useful for inspecting the full resource definition, creating backups, or preparing manifests for modification. "+
			"For JSON output, use getResource instead. For a human-friendly summary, use describeResource."),
		mcp.WithString("kind", mcp.Required(),
			mcp.Description("Resource type in lowercase plural form. "+
				"Examples: \"deployments\", \"pods\", \"services\", \"configmaps\". "+
				"Do NOT use PascalCase like \"Deployment\" or singular like \"pod\".")),
		mcp.WithString("name", mcp.Required(),
			mcp.Description("Exact name of the resource to export.")),
		mcp.WithString("namespace",
			mcp.Description("Namespace of the resource. Required for namespace-scoped resources. "+
				"Omit for cluster-scoped resources like nodes or namespaces.")),
	)
}

// GetResourceDiffTool creates a tool for comparing resource states.
func GetResourceDiffTool() mcp.Tool {
	return mcp.NewTool(
		"getResourceDiff",
		mcp.WithDescription("Compare the current state of a Kubernetes resource with a previous version or another resource. "+
			"Shows a unified diff of the resource specs. Useful for understanding what changed after an update or comparing configurations."),
		mcp.WithString("kind", mcp.Required(),
			mcp.Description("Resource type in lowercase plural form. "+
				"Examples: \"deployments\", \"pods\", \"services\", \"configmaps\". "+
				"Do NOT use PascalCase like \"Deployment\" or singular like \"pod\".")),
		mcp.WithString("name", mcp.Required(),
			mcp.Description("Exact name of the resource to compare.")),
		mcp.WithString("namespace",
			mcp.Description("Namespace of the resource. Required for namespace-scoped resources.")),
		mcp.WithString("compareWith",
			mcp.Description("What to compare the resource against. "+
				"Use \"previous\" to compare with the last-applied-configuration annotation (default). "+
				"Use \"resource:<name>\" to compare with another resource of the same kind and namespace. "+
				"Example: \"resource:my-other-deployment\"."),
			mcp.DefaultString("previous")),
	)
}

// GetNamespaceResourcesTool creates a tool for listing all resources in a namespace.
func GetNamespaceResourcesTool() mcp.Tool {
	return mcp.NewTool(
		"getNamespaceResources",
		mcp.WithDescription("List all resource instances within a specific namespace, optionally filtered by resource type. "+
			"Returns a summary of each resource (name, kind, status). "+
			"Useful for getting an overview of what's running in a namespace. "+
			"For listing resources of a single specific type across namespaces, use listResources instead."),
		mcp.WithString("namespace", mcp.Required(),
			mcp.Description("Namespace to scan for resources. Example: \"default\", \"production\".")),
		mcp.WithString("types",
			mcp.Description("Comma-separated list of resource types to include, in lowercase plural form. "+
				"Examples: \"pods,services\", \"deployments,configmaps,secrets\". "+
				"Omit to include all resource types.")),
		mcp.WithBoolean("includeSecrets",
			mcp.Description("Whether to include Secret resources in the results. Secrets are excluded by default for security. Default: false."),
			mcp.DefaultBool(false)),
	)
}

// GetResourceOwnersTool creates a tool for tracing resource ownership chains.
func GetResourceOwnersTool() mcp.Tool {
	return mcp.NewTool(
		"getResourceOwners",
		mcp.WithDescription("Trace the ownership chain of a Kubernetes resource up through its owners. "+
			"For example, Pod -> ReplicaSet -> Deployment. "+
			"Helps understand resource dependencies and which controller manages a given resource. "+
			"Optionally shows child resources owned by the target resource."),
		mcp.WithString("kind", mcp.Required(),
			mcp.Description("Resource type in lowercase plural form. "+
				"Examples: \"pods\", \"replicasets\", \"deployments\". "+
				"Do NOT use PascalCase like \"Pod\" or singular like \"pod\".")),
		mcp.WithString("name", mcp.Required(),
			mcp.Description("Exact name of the resource to trace ownership for.")),
		mcp.WithString("namespace",
			mcp.Description("Namespace of the resource. Required for namespace-scoped resources.")),
		mcp.WithBoolean("includeChildren",
			mcp.Description("Also show resources that are owned by (children of) this resource. "+
				"For example, showing the ReplicaSets and Pods owned by a Deployment. Default: false."),
			mcp.DefaultBool(false)),
	)
}

// GetClusterHealthTool creates a tool for checking overall cluster health.
func GetClusterHealthTool() mcp.Tool {
	return mcp.NewTool(
		"getClusterHealth",
		mcp.WithDescription("Get a comprehensive cluster health report including node status, control plane component health, "+
			"and critical pod status. Optionally includes resource utilization metrics and recent warning/error events. "+
			"Use this as a first step when investigating cluster-wide issues."),
		mcp.WithBoolean("includeMetrics",
			mcp.Description("Include CPU and memory utilization metrics for nodes. Requires metrics-server. Default: true."),
			mcp.DefaultBool(true)),
		mcp.WithBoolean("includeEvents",
			mcp.Description("Include recent Warning and Error events from across the cluster. Default: true."),
			mcp.DefaultBool(true)),
	)
}

// GetResourceQuotasTool creates a tool for listing resource quotas.
func GetResourceQuotasTool() mcp.Tool {
	return mcp.NewTool(
		"getResourceQuotas",
		mcp.WithDescription("List ResourceQuota objects and their current usage vs. limits. "+
			"Shows how much of allocated CPU, memory, and object count quotas are consumed. "+
			"Useful for diagnosing why resources cannot be created (quota exceeded)."),
		mcp.WithString("namespace",
			mcp.Description("Namespace to check quotas for. Omit or leave empty to check all namespaces.")),
		mcp.WithBoolean("showPercentage",
			mcp.Description("Show usage as a percentage of the quota limit. Default: true."),
			mcp.DefaultBool(true)),
	)
}

// GetLimitRangesTool creates a tool for getting limit ranges.
func GetLimitRangesTool() mcp.Tool {
	return mcp.NewTool(
		"getLimitRanges",
		mcp.WithDescription("List LimitRange objects that define default and maximum resource requests/limits for containers in a namespace. "+
			"Useful for understanding why pods get default resource settings or are rejected for exceeding limits."),
		mcp.WithString("namespace",
			mcp.Description("Namespace to check limit ranges for. Omit or leave empty to check all namespaces.")),
	)
}

// GetTopPodsTool creates a tool for getting top pods by resource usage.
func GetTopPodsTool() mcp.Tool {
	return mcp.NewTool(
		"getTopPods",
		mcp.WithDescription("Get the top resource-consuming pods, similar to 'kubectl top pods'. "+
			"Returns pods sorted by CPU or memory usage. Requires metrics-server to be installed. "+
			"For metrics on a single specific pod, use getPodMetrics instead."),
		mcp.WithString("namespace",
			mcp.Description("Namespace to check. Omit or leave empty to check across all namespaces.")),
		mcp.WithString("sortBy",
			mcp.Description("Sort results by resource type. Must be one of: \"cpu\", \"memory\". Default: \"cpu\"."),
			mcp.DefaultString("cpu")),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of pods to return. Default: 10."),
			mcp.DefaultNumber(10)),
	)
}

// GetTopNodesTool creates a tool for getting top nodes by resource utilization.
func GetTopNodesTool() mcp.Tool {
	return mcp.NewTool(
		"getTopNodes",
		mcp.WithDescription("Get nodes ranked by resource utilization, similar to 'kubectl top nodes'. "+
			"Returns CPU usage, memory usage, and pod count per node. Requires metrics-server. "+
			"For metrics on a single specific node, use getNodeMetrics instead."),
		mcp.WithString("sortBy",
			mcp.Description("Sort results by resource type. Must be one of: \"cpu\", \"memory\", \"pods\". Default: \"cpu\"."),
			mcp.DefaultString("cpu")),
		mcp.WithBoolean("includeConditions",
			mcp.Description("Include node conditions (Ready, MemoryPressure, DiskPressure, etc.) in the output. Default: true."),
			mcp.DefaultBool(true)),
	)
}

// GetPodDebugInfoTool creates a tool for comprehensive pod debugging.
func GetPodDebugInfoTool() mcp.Tool {
	return mcp.NewTool(
		"getPodDebugInfo",
		mcp.WithDescription("Get comprehensive debugging information for a pod in a single call. "+
			"Includes pod conditions, container statuses (running/waiting/terminated with reasons), "+
			"recent events, and optionally recent logs from all containers. "+
			"This is the best tool for diagnosing why a pod is not running or is crashing. "+
			"For logs only, use getPodsLogs instead."),
		mcp.WithString("name", mcp.Required(),
			mcp.Description("Exact name of the pod to debug. Example: \"my-app-7d9f5b4c6-x2k4m\".")),
		mcp.WithString("namespace", mcp.Required(),
			mcp.Description("Namespace where the pod is running.")),
		mcp.WithBoolean("includeLogs",
			mcp.Description("Include recent log lines from all containers in the pod. Default: true."),
			mcp.DefaultBool(true)),
		mcp.WithNumber("logLines",
			mcp.Description("Number of recent log lines to include per container. Only used when includeLogs is true. Default: 50."),
			mcp.DefaultNumber(50)),
	)
}

// GetServiceEndpointsTool creates a tool for listing service endpoints.
func GetServiceEndpointsTool() mcp.Tool {
	return mcp.NewTool(
		"getServiceEndpoints",
		mcp.WithDescription("List all endpoints (backing pods) for a Kubernetes Service, with their IP addresses, ports, and health status. "+
			"Useful for verifying that a service has healthy backend pods and traffic can be routed correctly."),
		mcp.WithString("name", mcp.Required(),
			mcp.Description("Exact name of the Service. Example: \"my-api-service\".")),
		mcp.WithString("namespace", mcp.Required(),
			mcp.Description("Namespace where the service is defined.")),
		mcp.WithBoolean("checkHealth",
			mcp.Description("Check the health/readiness status of each backing pod. Default: true."),
			mcp.DefaultBool(true)),
	)
}

// GetNetworkPoliciesTool creates a tool for listing network policies.
func GetNetworkPoliciesTool() mcp.Tool {
	return mcp.NewTool(
		"getNetworkPolicies",
		mcp.WithDescription("List NetworkPolicy objects in a namespace and show which pods they affect. "+
			"Optionally filter to policies affecting a specific pod. "+
			"Useful for diagnosing network connectivity issues between pods or from external sources."),
		mcp.WithString("namespace", mcp.Required(),
			mcp.Description("Namespace to check for network policies.")),
		mcp.WithString("podName",
			mcp.Description("If specified, only show network policies that select/affect this pod. "+
				"Omit to show all network policies in the namespace.")),
		mcp.WithBoolean("includeDetails",
			mcp.Description("Include the full ingress/egress rule specifications for each policy. Default: false."),
			mcp.DefaultBool(false)),
	)
}

// GetSecurityContextTool creates a tool for inspecting pod security contexts.
func GetSecurityContextTool() mcp.Tool {
	return mcp.NewTool(
		"getSecurityContext",
		mcp.WithDescription("Inspect the security context configuration for a pod and all its containers. "+
			"Shows runAsUser, runAsGroup, fsGroup, capabilities, privileged mode, read-only root filesystem, "+
			"and other security settings. Useful for security auditing and debugging permission issues."),
		mcp.WithString("name", mcp.Required(),
			mcp.Description("Exact name of the pod to inspect.")),
		mcp.WithString("namespace", mcp.Required(),
			mcp.Description("Namespace where the pod is running.")),
		mcp.WithBoolean("includeDefaults",
			mcp.Description("Include default security settings that are not explicitly set in the pod spec. Default: false."),
			mcp.DefaultBool(false)),
	)
}

// GetResourceHistoryTool creates a tool for getting recent changes and events for a resource.
func GetResourceHistoryTool() mcp.Tool {
	return mcp.NewTool(
		"getResourceHistory",
		mcp.WithDescription("Get recent events and changes for a specific resource over a time window. "+
			"Shows events associated with the resource such as scaling, updates, failures, and scheduling decisions. "+
			"Useful for understanding what happened to a resource recently."),
		mcp.WithString("kind", mcp.Required(),
			mcp.Description("Resource type in lowercase plural form. "+
				"Examples: \"deployments\", \"pods\", \"services\". "+
				"Do NOT use PascalCase like \"Deployment\" or singular like \"pod\".")),
		mcp.WithString("name", mcp.Required(),
			mcp.Description("Exact name of the resource.")),
		mcp.WithString("namespace",
			mcp.Description("Namespace of the resource. Required for namespace-scoped resources.")),
		mcp.WithNumber("hours",
			mcp.Description("Number of hours to look back for events and changes. Default: 24."),
			mcp.DefaultNumber(24)),
	)
}

// ValidateManifestTool creates a tool for dry-run validation of manifests.
func ValidateManifestTool() mcp.Tool {
	return mcp.NewTool(
		"validateManifest",
		mcp.WithDescription("Validate a Kubernetes manifest without applying it (server-side dry-run). "+
			"Checks for schema errors, missing required fields, and invalid values. "+
			"Use this before createResource or createResourceYAML to catch errors."),
		mcp.WithString("manifest", mcp.Required(),
			mcp.Description("The YAML or JSON manifest string to validate. Must be a complete Kubernetes resource definition.")),
		mcp.WithString("format",
			mcp.Description("Manifest format. Must be one of: \"yaml\", \"json\". Auto-detected if omitted.")),
		mcp.WithBoolean("strict",
			mcp.Description("Enable strict validation that rejects unknown fields. Default: true."),
			mcp.DefaultBool(true)),
	)
}

// ExecInPodTool creates a tool for executing commands in pod containers.
func ExecInPodTool() mcp.Tool {
	return mcp.NewTool(
		"execInPod",
		mcp.WithDescription("Execute a command inside a running pod container, similar to 'kubectl exec'. "+
			"Returns the command's stdout and stderr. Use for debugging, inspecting container filesystems, "+
			"or running one-off commands. The command is split into argv without shell interpretation. "+
			"DANGEROUS OPERATION: only available when server is not in read-only mode and exec capability is explicitly enabled."),
		mcp.WithString("name", mcp.Required(),
			mcp.Description("Exact name of the pod.")),
		mcp.WithString("namespace", mcp.Required(),
			mcp.Description("Namespace where the pod is running.")),
		mcp.WithString("command", mcp.Required(),
			mcp.Description("Command to execute as a single string. Shell pipes, redirects, and expansions are not interpreted. "+
				"Examples: \"ls -la /app\", \"cat /etc/config/settings.yaml\", \"printenv DATABASE_URL\".")),
		mcp.WithString("container",
			mcp.Description("Target container name within the pod. Required if the pod has multiple containers. "+
				"Omit if the pod has only one container.")),
		mcp.WithBoolean("stdin",
			mcp.Description("Pass stdin to the container. Typically not needed for one-off commands. Default: false."),
			mcp.DefaultBool(false)),
		mcp.WithBoolean("tty",
			mcp.Description("Allocate a pseudo-TTY. Typically not needed for one-off commands. Default: false."),
			mcp.DefaultBool(false)),
	)
}

// PortForwardTool creates a tool for port forwarding to pods.
func PortForwardTool() mcp.Tool {
	return mcp.NewTool(
		"portForward",
		mcp.WithDescription("Set up temporary port forwarding from the MCP server host to a pod, similar to 'kubectl port-forward'. "+
			"Creates a tunnel for a limited duration. Useful for accessing pod services that are not externally exposed. "+
			"WRITE OPERATION: only available when server is not in read-only mode."),
		mcp.WithString("name", mcp.Required(),
			mcp.Description("Exact name of the pod to forward to.")),
		mcp.WithString("namespace", mcp.Required(),
			mcp.Description("Namespace where the pod is running.")),
		mcp.WithString("ports", mcp.Required(),
			mcp.Description("Port mapping in the format \"localPort:remotePort\" or just \"port\" (same local and remote). "+
				"Examples: \"8080:80\" (local 8080 -> pod 80), \"5432\" (local 5432 -> pod 5432).")),
		mcp.WithNumber("duration",
			mcp.Description("Duration in seconds to keep the port forward active before it automatically closes. Default: 60."),
			mcp.DefaultNumber(60)),
	)
}

// CopyFromPodTool creates a tool for copying files from pod containers.
func CopyFromPodTool() mcp.Tool {
	return mcp.NewTool(
		"copyFromPod",
		mcp.WithDescription("Copy a file or directory from a pod container to the local filesystem, similar to 'kubectl cp'. "+
			"WRITE OPERATION: only available when server is not in read-only mode."),
		mcp.WithString("name", mcp.Required(),
			mcp.Description("Exact name of the pod to copy from.")),
		mcp.WithString("namespace", mcp.Required(),
			mcp.Description("Namespace where the pod is running.")),
		mcp.WithString("srcPath", mcp.Required(),
			mcp.Description("Absolute path inside the container to copy from. Example: \"/var/log/app.log\", \"/tmp/data/\".")),
		mcp.WithString("destPath", mcp.Required(),
			mcp.Description("Destination path on the MCP server's local filesystem. Example: \"/tmp/app.log\".")),
		mcp.WithString("container",
			mcp.Description("Target container name within the pod. Required if the pod has multiple containers.")),
	)
}

// ListNamespacesTool creates a tool for listing all namespaces.
func ListNamespacesTool() mcp.Tool {
	return mcp.NewTool(
		"listNamespaces",
		mcp.WithDescription("List all namespaces in the Kubernetes cluster with their status (Active/Terminating) and labels. "+
			"Use this to discover available namespaces before querying resources within them."),
		mcp.WithString("labelSelector",
			mcp.Description("Label selector to filter namespaces. "+
				"Examples: \"env=production\", \"team=backend\". Omit to list all namespaces.")),
	)
}

// GetRolloutStatusTool creates a tool for checking workload rollout status.
func GetRolloutStatusTool() mcp.Tool {
	return mcp.NewTool(
		"getRolloutStatus",
		mcp.WithDescription("Get the rollout status of a workload, similar to 'kubectl rollout status'. "+
			"Shows replica counts (desired, ready, available, updated), conditions, and whether the rollout is complete. "+
			"Use this to monitor deployments after an update or scale operation."),
		mcp.WithString("kind", mcp.Required(),
			mcp.Description("Workload type. Must be one of: \"Deployment\", \"StatefulSet\", \"DaemonSet\". "+
				"Use PascalCase singular form exactly as shown.")),
		mcp.WithString("name", mcp.Required(),
			mcp.Description("Exact name of the workload resource.")),
		mcp.WithString("namespace", mcp.Required(),
			mcp.Description("Namespace where the workload is running.")),
	)
}

// ScaleResourceTool creates a tool for scaling workloads.
func ScaleResourceTool() mcp.Tool {
	return mcp.NewTool(
		"scaleResource",
		mcp.WithDescription("Scale a workload to a desired number of replicas, similar to 'kubectl scale'. "+
			"Set replicas to 0 to scale down completely. Use getRolloutStatus to monitor the scaling progress afterward. "+
			"WRITE OPERATION: only available when server is not in read-only mode."),
		mcp.WithString("kind", mcp.Required(),
			mcp.Description("Workload type to scale. Must be one of: \"Deployment\", \"StatefulSet\", \"ReplicaSet\". "+
				"Use PascalCase singular form exactly as shown.")),
		mcp.WithString("name", mcp.Required(),
			mcp.Description("Exact name of the workload resource to scale.")),
		mcp.WithString("namespace", mcp.Required(),
			mcp.Description("Namespace where the workload is running.")),
		mcp.WithNumber("replicas", mcp.Required(),
			mcp.Description("Desired number of replicas. Must be a non-negative integer (0 or greater)."),
			mcp.Min(0)),
	)
}

// ListContextsTool creates a tool for listing available kubeconfig contexts.
func ListContextsTool() mcp.Tool {
	return mcp.NewTool(
		"listContexts",
		mcp.WithDescription("List all available kubeconfig contexts and identify which one is currently active. "+
			"Each context represents a different cluster/user/namespace combination. "+
			"Use this to discover available clusters before switching with switchContext."),
	)
}

// GetClusterSummaryTool creates a tool for getting a concise, high-level cluster status overview.
func GetClusterSummaryTool() mcp.Tool {
	return mcp.NewTool(
		"getClusterSummary",
		mcp.WithDescription("Get a concise, high-level summary of the entire Kubernetes cluster's current status in a single call. "+
			"Returns the active context, server version, node health, namespace listing, workload health per namespace "+
			"(deployments, statefulsets, daemonsets, jobs), pod phase counts, control plane status, and recent warning events. "+
			"This is the best tool to call when the user asks general questions like 'What is the status of my cluster?', "+
			"'Is everything healthy?', or 'Give me an overview'. "+
			"For deeper investigation of a specific issue found in the summary, follow up with getClusterHealth, "+
			"getPodDebugInfo, or getNamespaceResources."),
		mcp.WithBoolean("includeNamespaceDetails",
			mcp.Description("Include per-namespace workload breakdown (deployments, statefulsets, daemonsets, jobs with health status). "+
				"Set to false for a faster, lighter summary that only lists namespace names. Default: true."),
			mcp.DefaultBool(true)),
	)
}

// RunKubectlCommandTool creates a tool for executing direct kubectl commands.
func RunKubectlCommandTool() mcp.Tool {
	return mcp.NewTool(
		"runKubectlCommand",
		mcp.WithDescription("Execute a direct kubectl command on the MCP server host. "+
			"Requires the kubectl binary to be available in PATH. "+
			"The command must start with 'kubectl' and is executed without shell interpretation, "+
			"so pipes, redirects, shell expansions, and command chaining are not supported. "+
			"Use this when the structured MCP tools do not expose the exact data you need. "+
			"Examples: "+
			"\"kubectl get pods -A --field-selector=status.phase=Pending -o wide\", "+
			"\"kubectl top nodes --sort-by=cpu\". "+
			"DANGEROUS OPERATION: only available when server is not in read-only mode and kubectl capability is explicitly enabled."),
		mcp.WithString("command", mcp.Required(),
			mcp.Description("The full kubectl command to execute. Must start with 'kubectl'. "+
				"Shell syntax such as pipes and redirects is not supported.")),
		mcp.WithNumber("timeout",
			mcp.Description("Maximum execution time in seconds. Default: 30. Max: 300."),
			mcp.DefaultNumber(30)),
	)
}

// SwitchContextTool creates a tool for switching the active kubeconfig context.
func SwitchContextTool() mcp.Tool {
	return mcp.NewTool(
		"switchContext",
		mcp.WithDescription("Deprecated and not registered by default. Process-global context switching is unsafe for concurrent HTTP MCP usage. "+
			"Run separate server instances or use a request/session-scoped multi-cluster model instead."),
		mcp.WithString("context", mcp.Required(),
			mcp.Description("Exact name of the kubeconfig context to switch to. "+
				"Use listContexts to discover available context names.")),
	)
}
