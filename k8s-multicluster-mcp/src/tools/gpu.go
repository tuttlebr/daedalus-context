// Package tools provides MCP tool definitions for GPU debugging and remediation.
package tools

import (
	"github.com/mark3labs/mcp-go/mcp"
)

// GetGPUClusterOverviewTool creates a tool for getting a comprehensive GPU status overview across the cluster.
func GetGPUClusterOverviewTool() mcp.Tool {
	return mcp.NewTool(
		"getGPUClusterOverview",
		mcp.WithDescription("Get a comprehensive overview of GPU resources across the entire Kubernetes cluster. "+
			"Returns nodes with GPU capacity (nvidia.com/gpu), GPU labels and taints, NVIDIA device plugin daemonset status, "+
			"GPU operator deployment status, all NVIDIA-related pods, GPU-related CRDs, and GPU resource allocation summary. "+
			"This is the first tool to call when investigating GPU issues on a local vanilla Kubernetes cluster. "+
			"For deeper diagnostics on a specific pod, use diagnoseGPUScheduling. "+
			"For NVIDIA operator and device plugin health details, use getGPUOperatorHealth."),
		mcp.WithBoolean("includeNonGPUNodes",
			mcp.Description("Include nodes that have no GPU capacity in the results. Useful for understanding the full cluster topology. Default: false."),
			mcp.DefaultBool(false)),
		mcp.WithBoolean("includeEvents",
			mcp.Description("Include recent GPU-related warning events from across the cluster. Default: true."),
			mcp.DefaultBool(true)),
	)
}

// DiagnoseGPUSchedulingTool creates a tool for diagnosing GPU scheduling issues for a specific pod.
func DiagnoseGPUSchedulingTool() mcp.Tool {
	return mcp.NewTool(
		"diagnoseGPUScheduling",
		mcp.WithDescription("Diagnose why a specific pod cannot schedule on or use GPU nodes. "+
			"Checks the pod's GPU resource requests, node binding, scheduler constraints, tolerations versus node taints, "+
			"label selector mismatches, and available GPU capacity on candidate nodes. "+
			"Returns a structured diagnosis with identified issues and suggested fixes. "+
			"Use getGPUClusterOverview first to understand the cluster GPU landscape, "+
			"then use this tool to investigate specific failing pods."),
		mcp.WithString("podName", mcp.Required(),
			mcp.Description("Exact name of the pod to diagnose. Example: \"my-gpu-workload-7d9f5b4c6-x2k4m\".")),
		mcp.WithString("namespace", mcp.Required(),
			mcp.Description("Namespace where the pod is running or pending. Example: \"default\", \"gpu-workloads\".")),
	)
}

// GetGPUOperatorHealthTool creates a tool for checking NVIDIA GPU operator and device plugin health.
func GetGPUOperatorHealthTool() mcp.Tool {
	return mcp.NewTool(
		"getGPUOperatorHealth",
		mcp.WithDescription("Get a deep health check of the NVIDIA GPU operator and device plugin stack. "+
			"Checks the device plugin daemonset status and recent logs, GPU operator deployment status and recent logs, "+
			"GPU-related CRDs, and scans for known error patterns (driver/library version mismatch, failed device allocation, "+
			"invalid device function, GPU not available after reboot, persistent attach failures). "+
			"Returns detected issues with NVIDIA-recommended remediation steps from the GPU operator troubleshooting guide. "+
			"Use this after getGPUClusterOverview reveals operator or device plugin problems."),
		mcp.WithString("devicePluginNamespace",
			mcp.Description("Namespace where the NVIDIA device plugin daemonset runs. Default: \"kube-system\"."),
			mcp.DefaultString("kube-system")),
		mcp.WithString("gpuOperatorNamespace",
			mcp.Description("Namespace where the NVIDIA GPU operator is deployed. Default: \"gpu-operator\"."),
			mcp.DefaultString("gpu-operator")),
		mcp.WithNumber("logLines",
			mcp.Description("Number of recent log lines to retrieve from device plugin and operator pods. Default: 100."),
			mcp.DefaultNumber(100)),
	)
}

// RemediateGPUIssueTool creates a tool for performing GPU remediation actions.
func RemediateGPUIssueTool() mcp.Tool {
	return mcp.NewTool(
		"remediateGPUIssue",
		mcp.WithDescription("Perform a specific GPU remediation action on the cluster. "+
			"Supports the following actions: "+
			"'restartDevicePlugin' — rollout restart the NVIDIA device plugin daemonset; "+
			"'restartGPUOperator' — rollout restart the GPU operator deployment; "+
			"'removeTaint' — remove a GPU-blocking taint from a node; "+
			"'addGPULabel' — add the GPU presence label to a node; "+
			"'annotateNodeForOperator' — add annotations to a node to tell the NVIDIA operator to skip managed-provider checks. "+
			"WRITE OPERATION: only available when server is not in read-only mode."),
		mcp.WithString("action", mcp.Required(),
			mcp.Description("The remediation action to perform. Must be one of: "+
				"\"restartDevicePlugin\", \"restartGPUOperator\", \"removeTaint\", \"addGPULabel\", \"annotateNodeForOperator\".")),
		mcp.WithString("nodeName",
			mcp.Description("Name of the target node. Required for actions: removeTaint, addGPULabel, annotateNodeForOperator. "+
				"Example: \"gpu-worker-01\".")),
		mcp.WithString("taintKey",
			mcp.Description("The taint key to remove from the node. Required for action: removeTaint. "+
				"Example: \"nvidia.com/gpuiomanager\". The taint effect defaults to NoSchedule.")),
		mcp.WithString("taintEffect",
			mcp.Description("The taint effect to match when removing a taint. Default: \"NoSchedule\". "+
				"Must be one of: \"NoSchedule\", \"PreferNoSchedule\", \"NoExecute\"."),
			mcp.DefaultString("NoSchedule")),
		mcp.WithString("devicePluginNamespace",
			mcp.Description("Namespace of the NVIDIA device plugin daemonset. Default: \"kube-system\"."),
			mcp.DefaultString("kube-system")),
		mcp.WithString("gpuOperatorNamespace",
			mcp.Description("Namespace of the GPU operator deployment. Default: \"gpu-operator\"."),
			mcp.DefaultString("gpu-operator")),
	)
}
