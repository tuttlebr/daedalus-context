// Package k8s provides GPU debugging and remediation methods for the Kubernetes client.
package k8s

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// gpuResourceName is the extended resource name for NVIDIA GPUs.
const gpuResourceName corev1.ResourceName = "nvidia.com/gpu"

// Common NVIDIA-related label prefixes and keys.
var gpuLabelPrefixes = []string{
	"nvidia.com/",
	"cloud.google.com/gke-accelerator",
	"accelerator",
	"gpu.intel.com/",
	"amd.com/gpu",
}

// knownDevicePluginLabels are label selectors used to find the NVIDIA device plugin pods.
var knownDevicePluginLabels = []string{
	"app=nvidia-device-plugin",
	"k8s-app=nvidia-device-plugin",
	"app.kubernetes.io/name=nvidia-device-plugin",
	"name=nvidia-device-plugin-ds",
}

// knownGPUOperatorLabels are label selectors used to find the NVIDIA GPU operator pods.
var knownGPUOperatorLabels = []string{
	"app.kubernetes.io/component=gpu-operator",
	"app=gpu-operator",
	"app.kubernetes.io/name=gpu-operator",
}

// gpuErrorPatterns maps known GPU error strings to NVIDIA-recommended remediation.
var gpuErrorPatterns = []struct {
	Pattern     string
	ErrorType   string
	Remediation string
}{
	{
		Pattern:   "driver/library version mismatch",
		ErrorType: "GPU driver mismatch",
		Remediation: "Update the host driver to the version reported by nvidia-smi. " +
			"Ensure the driverVersion value in the GPU operator Helm chart matches the installed driver.",
	},
	{
		Pattern:   "Failed to allocate GPU device",
		ErrorType: "Device plugin allocation failure",
		Remediation: "Restart the device plugin daemonset using remediateGPUIssue with action 'restartDevicePlugin'. " +
			"Verify that the node's NVIDIA driver version meets the plugin's required minimum.",
	},
	{
		Pattern:   "invalid device function",
		ErrorType: "CUDA/driver incompatibility",
		Remediation: "Check that the CUDA runtime image used in the operator matches the driver. " +
			"Use a compatible base image such as nvcr.io/nvidia/cuda:12.2.0-runtime-ubuntu22.04 " +
			"and set CUDA_VERSION in the operator values.",
	},
	{
		Pattern:   "GPU is not available",
		ErrorType: "GPU unavailable after reboot",
		Remediation: "Run 'describeResource' on the node and check for NodeReady conditions with GPU pressure. " +
			"Ensure the kubelet has DevicePlugins feature gate enabled. " +
			"Use remediateGPUIssue with action 'restartGPUOperator' to re-probe GPUs.",
	},
	{
		Pattern:   "Failed to attach",
		ErrorType: "Persistent attach failure",
		Remediation: "Confirm that the kernel module nvidia_uvm is loaded on the host (modprobe nvidia_uvm). " +
			"Restart the node to allow the GPU operator to re-probe the GPU.",
	},
	{
		Pattern:   "no GPU devices found",
		ErrorType: "No GPU devices detected",
		Remediation: "Verify that the NVIDIA driver is installed on the host node. " +
			"Check that nvidia-smi runs successfully on the host. " +
			"Ensure the NVIDIA container toolkit is installed and the container runtime is restarted.",
	},
	{
		Pattern:   "device plugin not ready",
		ErrorType: "Device plugin not ready",
		Remediation: "Wait for the device plugin to become ready. If it remains unready, " +
			"use remediateGPUIssue with action 'restartDevicePlugin' and check the device plugin logs.",
	},
}

// GetGPUClusterOverview returns a comprehensive overview of GPU resources across the cluster.
func (c *Client) GetGPUClusterOverview(ctx context.Context, includeNonGPUNodes, includeEvents bool) (map[string]interface{}, error) {
	result := map[string]interface{}{}

	// 1. Get all nodes and check for GPU capacity/labels/taints
	nodes, err := c.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	gpuNodes := []map[string]interface{}{}
	nonGPUNodes := []string{}
	totalGPUs := int64(0)
	allocatableGPUs := int64(0)

	for i := range nodes.Items {
		node := &nodes.Items[i]
		nodeInfo := map[string]interface{}{
			"name": node.Name,
		}

		// Check GPU capacity
		gpuCapacity := node.Status.Capacity[gpuResourceName]
		gpuAllocatable := node.Status.Allocatable[gpuResourceName]
		capVal := gpuCapacity.Value()
		allocVal := gpuAllocatable.Value()

		if capVal == 0 && allocVal == 0 {
			nonGPUNodes = append(nonGPUNodes, node.Name)
			continue
		}

		totalGPUs += capVal
		allocatableGPUs += allocVal

		nodeInfo["gpuCapacity"] = capVal
		nodeInfo["gpuAllocatable"] = allocVal

		// Check node readiness
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady {
				nodeInfo["ready"] = condition.Status == corev1.ConditionTrue
				break
			}
		}

		// Check if unschedulable
		if node.Spec.Unschedulable {
			nodeInfo["unschedulable"] = true
		}

		// Collect GPU-related labels
		gpuLabels := map[string]string{}
		for key, val := range node.Labels {
			for _, prefix := range gpuLabelPrefixes {
				if strings.HasPrefix(key, prefix) || strings.Contains(key, "gpu") {
					gpuLabels[key] = val
					break
				}
			}
		}
		if len(gpuLabels) > 0 {
			nodeInfo["gpuLabels"] = gpuLabels
		}

		// Collect GPU-related taints
		gpuTaints := []map[string]string{}
		for _, taint := range node.Spec.Taints {
			if strings.Contains(taint.Key, "nvidia") || strings.Contains(taint.Key, "gpu") {
				gpuTaints = append(gpuTaints, map[string]string{
					"key":    taint.Key,
					"value":  taint.Value,
					"effect": string(taint.Effect),
				})
			}
		}
		if len(gpuTaints) > 0 {
			nodeInfo["gpuTaints"] = gpuTaints
		}

		gpuNodes = append(gpuNodes, nodeInfo)
	}

	result["gpuNodes"] = gpuNodes
	result["gpuSummary"] = map[string]interface{}{
		"gpuNodeCount":    len(gpuNodes),
		"nonGPUNodeCount": len(nonGPUNodes),
		"totalGPUs":       totalGPUs,
		"allocatableGPUs": allocatableGPUs,
	}

	if includeNonGPUNodes && len(nonGPUNodes) > 0 {
		result["nonGPUNodes"] = nonGPUNodes
	}

	// 2. Check NVIDIA device plugin daemonset
	dpStatus := c.findNVIDIADevicePlugin(ctx)
	result["devicePlugin"] = dpStatus

	// 3. Check GPU operator
	opStatus := c.findGPUOperator(ctx)
	result["gpuOperator"] = opStatus

	// 4. Find all NVIDIA-related pods across all namespaces
	allPods, err := c.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err == nil {
		nvidiaPods := []map[string]interface{}{}
		for i := range allPods.Items {
			pod := &allPods.Items[i]
			if isNVIDIARelatedPod(pod) {
				podInfo := map[string]interface{}{
					"name":      pod.Name,
					"namespace": pod.Namespace,
					"phase":     string(pod.Status.Phase),
					"nodeName":  pod.Spec.NodeName,
				}
				// Check container readiness
				ready := true
				for j := range pod.Status.ContainerStatuses {
					if !pod.Status.ContainerStatuses[j].Ready {
						ready = false
						break
					}
				}
				podInfo["ready"] = ready
				nvidiaPods = append(nvidiaPods, podInfo)
			}
		}
		result["nvidiaPods"] = nvidiaPods
	}

	// 5. Calculate GPU allocation (pods requesting GPUs)
	if allPods != nil {
		usedGPUs := int64(0)
		gpuWorkloads := []map[string]interface{}{}
		for i := range allPods.Items {
			pod := &allPods.Items[i]
			if pod.Status.Phase != corev1.PodRunning && pod.Status.Phase != corev1.PodPending {
				continue
			}
			for j := range pod.Spec.Containers {
				container := &pod.Spec.Containers[j]
				if gpuReq, ok := container.Resources.Limits[gpuResourceName]; ok {
					reqVal := gpuReq.Value()
					usedGPUs += reqVal
					gpuWorkloads = append(gpuWorkloads, map[string]interface{}{
						"pod":       pod.Name,
						"namespace": pod.Namespace,
						"container": container.Name,
						"gpuCount":  reqVal,
						"phase":     string(pod.Status.Phase),
						"nodeName":  pod.Spec.NodeName,
					})
				}
			}
		}
		result["gpuAllocation"] = map[string]interface{}{
			"requestedGPUs":   usedGPUs,
			"allocatableGPUs": allocatableGPUs,
			"workloads":       gpuWorkloads,
		}
	}

	// 6. GPU-related events
	if includeEvents {
		gpuEvents := c.getGPURelatedEvents(ctx)
		if len(gpuEvents) > 0 {
			result["recentGPUEvents"] = gpuEvents
		}
	}

	return result, nil
}

// DiagnoseGPUScheduling diagnoses why a specific pod cannot schedule on or access GPU nodes.
func (c *Client) DiagnoseGPUScheduling(ctx context.Context, podName, namespace string) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"podName":     podName,
		"namespace":   namespace,
		"issues":      []map[string]interface{}{},
		"suggestions": []string{},
	}

	// Get the pod
	pod, err := c.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod '%s' in namespace '%s': %w", podName, namespace, err)
	}

	result["phase"] = string(pod.Status.Phase)
	result["nodeName"] = pod.Spec.NodeName

	issues := []map[string]interface{}{}
	suggestions := []string{}

	// 1. Check if the pod requests GPUs
	gpuRequested := int64(0)
	containerGPUs := []map[string]interface{}{}
	for i := range pod.Spec.Containers {
		container := &pod.Spec.Containers[i]
		if gpuLimit, ok := container.Resources.Limits[gpuResourceName]; ok {
			val := gpuLimit.Value()
			gpuRequested += val
			containerGPUs = append(containerGPUs, map[string]interface{}{
				"container": container.Name,
				"gpuLimit":  val,
			})
		}
		if gpuReq, ok := container.Resources.Requests[gpuResourceName]; ok {
			val := gpuReq.Value()
			if gpuRequested == 0 {
				gpuRequested += val
			}
			// Check if request exists but no limit
			if _, hasLimit := container.Resources.Limits[gpuResourceName]; !hasLimit {
				issues = append(issues, map[string]interface{}{
					"type":    "missingGPULimit",
					"message": fmt.Sprintf("Container '%s' has GPU request (%d) but no GPU limit", container.Name, val),
				})
			}
		}
	}

	if gpuRequested == 0 {
		issues = append(issues, map[string]interface{}{
			"type":    "noGPURequest",
			"message": "No containers in this pod request nvidia.com/gpu resources",
		})
		suggestions = append(suggestions, "Add resources.limits.nvidia.com/gpu: \"1\" to the pod container spec")
	}
	result["gpuResources"] = containerGPUs

	// 2. Check pod conditions for scheduling issues
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodScheduled && condition.Status != corev1.ConditionTrue {
			issues = append(issues, map[string]interface{}{
				"type":    "schedulingFailed",
				"message": condition.Message,
				"reason":  condition.Reason,
			})
		}
	}

	// 3. Check container statuses for waiting reasons
	for i := range pod.Status.ContainerStatuses {
		cs := &pod.Status.ContainerStatuses[i]
		if cs.State.Waiting != nil {
			issues = append(issues, map[string]interface{}{
				"type":      "containerWaiting",
				"container": cs.Name,
				"reason":    cs.State.Waiting.Reason,
				"message":   cs.State.Waiting.Message,
			})
		}
		if cs.State.Terminated != nil && cs.State.Terminated.ExitCode != 0 {
			issues = append(issues, map[string]interface{}{
				"type":      "containerTerminated",
				"container": cs.Name,
				"reason":    cs.State.Terminated.Reason,
				"message":   cs.State.Terminated.Message,
				"exitCode":  cs.State.Terminated.ExitCode,
			})
		}
	}

	// 4. Get pod tolerations
	tolerations := []map[string]string{}
	for _, toleration := range pod.Spec.Tolerations {
		tolerations = append(tolerations, map[string]string{
			"key":      toleration.Key,
			"operator": string(toleration.Operator),
			"value":    toleration.Value,
			"effect":   string(toleration.Effect),
		})
	}
	result["tolerations"] = tolerations

	// 5. Get pod node selector and affinity
	if pod.Spec.NodeSelector != nil {
		result["nodeSelector"] = pod.Spec.NodeSelector
	}
	if pod.Spec.Affinity != nil && pod.Spec.Affinity.NodeAffinity != nil {
		result["hasNodeAffinity"] = true
	}

	// 6. Check GPU nodes for taint/label mismatches
	nodes, err := c.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err == nil {
		gpuNodeAnalysis := []map[string]interface{}{}
		for i := range nodes.Items {
			node := &nodes.Items[i]
			gpuAllocatable := node.Status.Allocatable[gpuResourceName]
			if gpuAllocatable.Value() == 0 {
				continue
			}

			nodeAnalysis := map[string]interface{}{
				"name":           node.Name,
				"gpuAllocatable": gpuAllocatable.Value(),
				"schedulable":    !node.Spec.Unschedulable,
			}

			// Check if node is ready
			for _, cond := range node.Status.Conditions {
				if cond.Type == corev1.NodeReady {
					nodeAnalysis["ready"] = cond.Status == corev1.ConditionTrue
					break
				}
			}

			// Check for GPU-related taints that the pod doesn't tolerate
			blockingTaints := []map[string]string{}
			for _, taint := range node.Spec.Taints {
				if !podTolerateTaint(pod, &taint) {
					if strings.Contains(taint.Key, "nvidia") || strings.Contains(taint.Key, "gpu") {
						blockingTaints = append(blockingTaints, map[string]string{
							"key":    taint.Key,
							"value":  taint.Value,
							"effect": string(taint.Effect),
						})
					}
				}
			}
			if len(blockingTaints) > 0 {
				nodeAnalysis["blockingGPUTaints"] = blockingTaints
				issues = append(issues, map[string]interface{}{
					"type":    "taintBlocking",
					"node":    node.Name,
					"message": fmt.Sprintf("Node '%s' has GPU taints that the pod does not tolerate", node.Name),
					"taints":  blockingTaints,
				})
				suggestions = append(suggestions,
					fmt.Sprintf("Add a toleration for key '%s' with effect '%s', or remove the taint: "+
						"use remediateGPUIssue with action 'removeTaint'",
						blockingTaints[0]["key"], blockingTaints[0]["effect"]))
			}

			// Check node selector mismatch
			if pod.Spec.NodeSelector != nil {
				for key, val := range pod.Spec.NodeSelector {
					if nodeVal, ok := node.Labels[key]; !ok || nodeVal != val {
						nodeAnalysis["nodeSelectorMismatch"] = true
						issues = append(issues, map[string]interface{}{
							"type":     "labelMismatch",
							"node":     node.Name,
							"message":  fmt.Sprintf("Pod requires label %s=%s but node has %s=%s", key, val, key, nodeVal),
							"label":    key,
							"expected": val,
							"actual":   nodeVal,
						})
					}
				}
			}

			gpuNodeAnalysis = append(gpuNodeAnalysis, nodeAnalysis)
		}
		result["gpuNodeAnalysis"] = gpuNodeAnalysis
	}

	// 7. Check for GPU-related events for this pod
	events, err := c.clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.name=%s", podName),
	})
	if err == nil && len(events.Items) > 0 {
		podEvents := []map[string]interface{}{}
		for i := range events.Items {
			event := &events.Items[i]
			podEvents = append(podEvents, map[string]interface{}{
				"type":      event.Type,
				"reason":    event.Reason,
				"message":   event.Message,
				"count":     event.Count,
				"timestamp": event.LastTimestamp.Time,
			})

			// Check for GPU-specific event messages
			msg := strings.ToLower(event.Message)
			if strings.Contains(msg, "insufficient nvidia.com/gpu") ||
				strings.Contains(msg, "insufficient gpu") {
				issues = append(issues, map[string]interface{}{
					"type":    "insufficientGPU",
					"message": event.Message,
				})
				suggestions = append(suggestions,
					"No GPU nodes have enough free GPUs. Check GPU allocation with getGPUClusterOverview "+
						"or scale down other GPU workloads")
			}
		}
		result["events"] = podEvents
	}

	result["issues"] = issues
	result["suggestions"] = suggestions

	return result, nil
}

// GetGPUOperatorHealth checks the health of the NVIDIA GPU operator and device plugin.
func (c *Client) GetGPUOperatorHealth(ctx context.Context, devicePluginNamespace, gpuOperatorNamespace string, logLines int) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"healthy":        true,
		"detectedIssues": []map[string]interface{}{},
	}

	detectedIssues := []map[string]interface{}{}

	// 1. Device plugin daemonset health
	dpInfo := c.getDevicePluginHealth(ctx, devicePluginNamespace, logLines)
	result["devicePlugin"] = dpInfo
	if dpHealthy, ok := dpInfo["healthy"].(bool); ok && !dpHealthy {
		result["healthy"] = false
	}
	if dpIssues, ok := dpInfo["detectedIssues"].([]map[string]interface{}); ok {
		detectedIssues = append(detectedIssues, dpIssues...)
	}

	// 2. GPU operator deployment health
	opInfo := c.getGPUOperatorDeploymentHealth(ctx, gpuOperatorNamespace, logLines)
	result["gpuOperator"] = opInfo
	if opHealthy, ok := opInfo["healthy"].(bool); ok && !opHealthy {
		result["healthy"] = false
	}
	if opIssues, ok := opInfo["detectedIssues"].([]map[string]interface{}); ok {
		detectedIssues = append(detectedIssues, opIssues...)
	}

	// 3. Check GPU-related CRDs
	crdInfo := c.checkGPUCRDs(ctx)
	result["crds"] = crdInfo

	// 4. Per-node GPU device status
	nodeGPUStatus := c.getPerNodeGPUStatus(ctx)
	result["nodeGPUStatus"] = nodeGPUStatus

	// 5. GPU operator ready condition
	opReady := c.checkGPUOperatorReadyCondition(ctx, gpuOperatorNamespace)
	result["operatorReady"] = opReady

	result["detectedIssues"] = detectedIssues

	return result, nil
}

// RemediateGPUIssue performs a specific GPU remediation action.
func (c *Client) RemediateGPUIssue(ctx context.Context, action, nodeName, taintKey, taintEffect, devicePluginNamespace, gpuOperatorNamespace string) (map[string]interface{}, error) {
	switch action {
	case "restartDevicePlugin":
		return c.restartDevicePlugin(ctx, devicePluginNamespace)
	case "restartGPUOperator":
		return c.restartGPUOperator(ctx, gpuOperatorNamespace)
	case "removeTaint":
		if nodeName == "" {
			return nil, fmt.Errorf("nodeName is required for removeTaint action")
		}
		if taintKey == "" {
			return nil, fmt.Errorf("taintKey is required for removeTaint action")
		}
		return c.removeNodeTaint(ctx, nodeName, taintKey, taintEffect)
	case "addGPULabel":
		if nodeName == "" {
			return nil, fmt.Errorf("nodeName is required for addGPULabel action")
		}
		return c.addGPULabel(ctx, nodeName)
	case "annotateNodeForOperator":
		if nodeName == "" {
			return nil, fmt.Errorf("nodeName is required for annotateNodeForOperator action")
		}
		return c.annotateNodeForOperator(ctx, nodeName)
	default:
		return nil, fmt.Errorf("unknown action: %s. Must be one of: restartDevicePlugin, restartGPUOperator, removeTaint, addGPULabel, annotateNodeForOperator", action)
	}
}

// --- Internal helper methods ---

// findNVIDIADevicePlugin locates the NVIDIA device plugin daemonset.
func (c *Client) findNVIDIADevicePlugin(ctx context.Context) map[string]interface{} {
	status := map[string]interface{}{
		"found": false,
	}

	// Search common namespaces with common label selectors
	searchNamespaces := []string{"kube-system", "gpu-operator", "nvidia-device-plugin"}

	for _, ns := range searchNamespaces {
		for _, labelSelector := range knownDevicePluginLabels {
			pods, err := c.clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
				LabelSelector: labelSelector,
			})
			if err != nil || len(pods.Items) == 0 {
				continue
			}

			status["found"] = true
			status["namespace"] = ns
			status["labelSelector"] = labelSelector
			status["podCount"] = len(pods.Items)

			podStatuses := []map[string]interface{}{}
			readyCount := 0
			for i := range pods.Items {
				pod := &pods.Items[i]
				ready := true
				for j := range pod.Status.ContainerStatuses {
					if !pod.Status.ContainerStatuses[j].Ready {
						ready = false
						break
					}
				}
				if ready {
					readyCount++
				}
				podStatuses = append(podStatuses, map[string]interface{}{
					"name":     pod.Name,
					"nodeName": pod.Spec.NodeName,
					"phase":    string(pod.Status.Phase),
					"ready":    ready,
				})
			}
			status["readyCount"] = readyCount
			status["pods"] = podStatuses
			return status
		}
	}

	// Fallback: search for daemonsets with "nvidia" in the name
	for _, ns := range searchNamespaces {
		dsList, err := c.clientset.AppsV1().DaemonSets(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			continue
		}
		for i := range dsList.Items {
			ds := &dsList.Items[i]
			if strings.Contains(strings.ToLower(ds.Name), "nvidia-device-plugin") {
				status["found"] = true
				status["namespace"] = ns
				status["daemonsetName"] = ds.Name
				status["desiredScheduled"] = ds.Status.DesiredNumberScheduled
				status["currentScheduled"] = ds.Status.CurrentNumberScheduled
				status["numberReady"] = ds.Status.NumberReady
				status["numberAvailable"] = ds.Status.NumberAvailable
				return status
			}
		}
	}

	return status
}

// findGPUOperator locates the NVIDIA GPU operator deployment.
func (c *Client) findGPUOperator(ctx context.Context) map[string]interface{} {
	status := map[string]interface{}{
		"found": false,
	}

	searchNamespaces := []string{"gpu-operator", "nvidia-gpu-operator", "kube-system"}

	for _, ns := range searchNamespaces {
		for _, labelSelector := range knownGPUOperatorLabels {
			pods, err := c.clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
				LabelSelector: labelSelector,
			})
			if err != nil || len(pods.Items) == 0 {
				continue
			}

			status["found"] = true
			status["namespace"] = ns
			status["labelSelector"] = labelSelector

			for i := range pods.Items {
				pod := &pods.Items[i]
				ready := true
				for j := range pod.Status.ContainerStatuses {
					if !pod.Status.ContainerStatuses[j].Ready {
						ready = false
						break
					}
				}
				status["podName"] = pod.Name
				status["phase"] = string(pod.Status.Phase)
				status["ready"] = ready
				return status
			}
		}
	}

	// Fallback: search for deployments with "gpu-operator" in the name
	for _, ns := range searchNamespaces {
		depList, err := c.clientset.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			continue
		}
		for i := range depList.Items {
			dep := &depList.Items[i]
			if strings.Contains(strings.ToLower(dep.Name), "gpu-operator") {
				status["found"] = true
				status["namespace"] = ns
				status["deploymentName"] = dep.Name
				if dep.Spec.Replicas != nil {
					status["desiredReplicas"] = *dep.Spec.Replicas
				}
				status["readyReplicas"] = dep.Status.ReadyReplicas
				status["availableReplicas"] = dep.Status.AvailableReplicas
				return status
			}
		}
	}

	return status
}

// isNVIDIARelatedPod checks if a pod is related to NVIDIA GPU infrastructure.
func isNVIDIARelatedPod(pod *corev1.Pod) bool {
	// Check pod name
	nameLower := strings.ToLower(pod.Name)
	if strings.Contains(nameLower, "nvidia") || strings.Contains(nameLower, "gpu-operator") ||
		strings.Contains(nameLower, "dcgm") || strings.Contains(nameLower, "gpu-feature-discovery") {
		return true
	}

	// Check container images
	for i := range pod.Spec.Containers {
		img := strings.ToLower(pod.Spec.Containers[i].Image)
		if strings.Contains(img, "nvidia") || strings.Contains(img, "nvcr.io") {
			return true
		}
	}
	for i := range pod.Spec.InitContainers {
		img := strings.ToLower(pod.Spec.InitContainers[i].Image)
		if strings.Contains(img, "nvidia") || strings.Contains(img, "nvcr.io") {
			return true
		}
	}

	// Check labels
	for key := range pod.Labels {
		if strings.Contains(key, "nvidia") {
			return true
		}
	}

	return false
}

// podTolerateTaint checks if a pod tolerates a specific taint.
func podTolerateTaint(pod *corev1.Pod, taint *corev1.Taint) bool {
	for _, toleration := range pod.Spec.Tolerations {
		if toleration.Key == "" && toleration.Operator == corev1.TolerationOpExists {
			return true // Tolerates everything
		}
		if toleration.Key == taint.Key {
			if toleration.Operator == corev1.TolerationOpExists {
				if toleration.Effect == "" || toleration.Effect == taint.Effect {
					return true
				}
			}
			if toleration.Operator == corev1.TolerationOpEqual || toleration.Operator == "" {
				if toleration.Value == taint.Value {
					if toleration.Effect == "" || toleration.Effect == taint.Effect {
						return true
					}
				}
			}
		}
	}
	return false
}

// getGPURelatedEvents returns recent GPU-related warning events.
func (c *Client) getGPURelatedEvents(ctx context.Context) []map[string]interface{} {
	events, err := c.clientset.CoreV1().Events("").List(ctx, metav1.ListOptions{
		FieldSelector: "type!=Normal",
	})
	if err != nil {
		return nil
	}

	cutoff := time.Now().Add(-1 * time.Hour)
	gpuEvents := []map[string]interface{}{}

	for i := range events.Items {
		event := &events.Items[i]
		if !event.LastTimestamp.After(cutoff) {
			continue
		}
		msg := strings.ToLower(event.Message + " " + event.Reason)
		if strings.Contains(msg, "gpu") || strings.Contains(msg, "nvidia") ||
			strings.Contains(msg, "device plugin") || strings.Contains(msg, "accelerator") {
			gpuEvents = append(gpuEvents, map[string]interface{}{
				"type":      event.Type,
				"reason":    event.Reason,
				"message":   event.Message,
				"object":    fmt.Sprintf("%s/%s", event.InvolvedObject.Kind, event.InvolvedObject.Name),
				"namespace": event.Namespace,
				"count":     event.Count,
				"timestamp": event.LastTimestamp.Time,
			})
		}
	}

	if len(gpuEvents) > 50 {
		gpuEvents = gpuEvents[:50]
	}

	return gpuEvents
}

// getDevicePluginHealth checks the NVIDIA device plugin health with logs.
func (c *Client) getDevicePluginHealth(ctx context.Context, namespace string, logLines int) map[string]interface{} {
	info := map[string]interface{}{
		"healthy": true,
	}

	// Find device plugin pods
	var dpPods *corev1.PodList
	for _, labelSelector := range knownDevicePluginLabels {
		pods, err := c.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err == nil && len(pods.Items) > 0 {
			dpPods = pods
			info["labelSelector"] = labelSelector
			break
		}
	}

	if dpPods == nil || len(dpPods.Items) == 0 {
		info["healthy"] = false
		info["error"] = fmt.Sprintf("No NVIDIA device plugin pods found in namespace '%s'", namespace)
		return info
	}

	info["podCount"] = len(dpPods.Items)
	podDetails := []map[string]interface{}{}
	detectedIssues := []map[string]interface{}{}

	tailLines := int64(logLines)

	for i := range dpPods.Items {
		pod := &dpPods.Items[i]
		detail := map[string]interface{}{
			"name":     pod.Name,
			"nodeName": pod.Spec.NodeName,
			"phase":    string(pod.Status.Phase),
		}

		ready := true
		for j := range pod.Status.ContainerStatuses {
			cs := &pod.Status.ContainerStatuses[j]
			if !cs.Ready {
				ready = false
				info["healthy"] = false
			}
			if cs.RestartCount > 0 {
				detail["restartCount"] = cs.RestartCount
			}
			if cs.State.Waiting != nil {
				detail["waiting"] = map[string]string{
					"reason":  cs.State.Waiting.Reason,
					"message": cs.State.Waiting.Message,
				}
			}
		}
		detail["ready"] = ready

		// Get recent logs
		logReq := c.clientset.CoreV1().Pods(namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
			TailLines: &tailLines,
		})
		logStream, err := logReq.Stream(ctx)
		if err == nil {
			buf := make([]byte, 64*1024)
			n, _ := logStream.Read(buf)
			logStream.Close()
			if n > 0 {
				logContent := string(buf[:n])
				detail["recentLogs"] = logContent

				// Scan for known error patterns
				for _, pattern := range gpuErrorPatterns {
					if strings.Contains(strings.ToLower(logContent), strings.ToLower(pattern.Pattern)) {
						detectedIssues = append(detectedIssues, map[string]interface{}{
							"source":      "devicePlugin",
							"pod":         pod.Name,
							"errorType":   pattern.ErrorType,
							"pattern":     pattern.Pattern,
							"remediation": pattern.Remediation,
						})
					}
				}
			}
		}

		podDetails = append(podDetails, detail)
	}

	info["pods"] = podDetails
	info["detectedIssues"] = detectedIssues

	return info
}

// getGPUOperatorDeploymentHealth checks the GPU operator deployment health with logs.
func (c *Client) getGPUOperatorDeploymentHealth(ctx context.Context, namespace string, logLines int) map[string]interface{} {
	info := map[string]interface{}{
		"healthy": true,
	}

	// Find GPU operator pods
	var opPods *corev1.PodList
	for _, labelSelector := range knownGPUOperatorLabels {
		pods, err := c.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err == nil && len(pods.Items) > 0 {
			opPods = pods
			info["labelSelector"] = labelSelector
			break
		}
	}

	if opPods == nil || len(opPods.Items) == 0 {
		// Not necessarily unhealthy; operator may not be installed
		info["installed"] = false
		info["note"] = fmt.Sprintf("No GPU operator pods found in namespace '%s'. "+
			"This is expected if the GPU operator is not installed.", namespace)
		return info
	}

	info["installed"] = true
	info["podCount"] = len(opPods.Items)
	podDetails := []map[string]interface{}{}
	detectedIssues := []map[string]interface{}{}

	tailLines := int64(logLines)

	for i := range opPods.Items {
		pod := &opPods.Items[i]
		detail := map[string]interface{}{
			"name":  pod.Name,
			"phase": string(pod.Status.Phase),
		}

		ready := true
		for j := range pod.Status.ContainerStatuses {
			cs := &pod.Status.ContainerStatuses[j]
			if !cs.Ready {
				ready = false
				info["healthy"] = false
			}
			if cs.RestartCount > 0 {
				detail["restartCount"] = cs.RestartCount
			}
		}
		detail["ready"] = ready

		// Get recent logs
		logReq := c.clientset.CoreV1().Pods(namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
			TailLines: &tailLines,
		})
		logStream, err := logReq.Stream(ctx)
		if err == nil {
			buf := make([]byte, 64*1024)
			n, _ := logStream.Read(buf)
			logStream.Close()
			if n > 0 {
				logContent := string(buf[:n])
				detail["recentLogs"] = logContent

				// Scan for known error patterns
				for _, pattern := range gpuErrorPatterns {
					if strings.Contains(strings.ToLower(logContent), strings.ToLower(pattern.Pattern)) {
						detectedIssues = append(detectedIssues, map[string]interface{}{
							"source":      "gpuOperator",
							"pod":         pod.Name,
							"errorType":   pattern.ErrorType,
							"pattern":     pattern.Pattern,
							"remediation": pattern.Remediation,
						})
					}
				}
			}
		}

		podDetails = append(podDetails, detail)
	}

	info["pods"] = podDetails
	info["detectedIssues"] = detectedIssues

	return info
}

// checkGPUCRDs checks for GPU-related Custom Resource Definitions.
func (c *Client) checkGPUCRDs(ctx context.Context) map[string]interface{} {
	info := map[string]interface{}{
		"found": []string{},
	}

	// Use discovery to find GPU-related CRDs
	resourceLists, err := c.discoveryClient.ServerPreferredResources()
	if err != nil {
		info["error"] = fmt.Sprintf("Failed to discover API resources: %v", err)
		return info
	}

	gpuCRDs := []string{}
	for _, resourceList := range resourceLists {
		for i := range resourceList.APIResources {
			nameLower := strings.ToLower(resourceList.APIResources[i].Name + " " + resourceList.GroupVersion)
			if strings.Contains(nameLower, "gpu") || strings.Contains(nameLower, "nvidia") ||
				strings.Contains(nameLower, "dcgm") || strings.Contains(nameLower, "nfd") {
				gpuCRDs = append(gpuCRDs, fmt.Sprintf("%s (%s)", resourceList.APIResources[i].Name, resourceList.GroupVersion))
			}
		}
	}

	info["found"] = gpuCRDs
	return info
}

// getPerNodeGPUStatus returns GPU capacity and allocatable per node.
func (c *Client) getPerNodeGPUStatus(ctx context.Context) []map[string]interface{} {
	nodes, err := c.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}

	nodeStatuses := []map[string]interface{}{}
	for i := range nodes.Items {
		node := &nodes.Items[i]
		gpuCap := node.Status.Capacity[gpuResourceName]
		gpuAlloc := node.Status.Allocatable[gpuResourceName]

		if gpuCap.Value() == 0 && gpuAlloc.Value() == 0 {
			continue
		}

		nodeStatus := map[string]interface{}{
			"name":           node.Name,
			"gpuCapacity":    gpuCap.Value(),
			"gpuAllocatable": gpuAlloc.Value(),
			"unschedulable":  node.Spec.Unschedulable,
		}

		// Check readiness
		for _, cond := range node.Status.Conditions {
			if cond.Type == corev1.NodeReady {
				nodeStatus["ready"] = cond.Status == corev1.ConditionTrue
				break
			}
		}

		// Check for GPU-related conditions
		for _, cond := range node.Status.Conditions {
			condType := string(cond.Type)
			if strings.Contains(strings.ToLower(condType), "gpu") ||
				strings.Contains(strings.ToLower(condType), "device") {
				nodeStatus["gpuCondition"] = map[string]interface{}{
					"type":    condType,
					"status":  string(cond.Status),
					"message": cond.Message,
				}
			}
		}

		nodeStatuses = append(nodeStatuses, nodeStatus)
	}

	return nodeStatuses
}

// checkGPUOperatorReadyCondition checks if the GPU operator deployment reports Available=True.
func (c *Client) checkGPUOperatorReadyCondition(ctx context.Context, namespace string) map[string]interface{} {
	info := map[string]interface{}{
		"available": false,
	}

	depList, err := c.clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		info["error"] = fmt.Sprintf("Failed to list deployments in namespace '%s': %v", namespace, err)
		return info
	}

	for i := range depList.Items {
		dep := &depList.Items[i]
		if strings.Contains(strings.ToLower(dep.Name), "gpu-operator") {
			info["deploymentName"] = dep.Name
			for _, cond := range dep.Status.Conditions {
				if string(cond.Type) == "Available" {
					info["available"] = cond.Status == corev1.ConditionTrue
					info["message"] = cond.Message
					return info
				}
			}
		}
	}

	return info
}

// restartDevicePlugin performs a rollout restart of the NVIDIA device plugin daemonset.
func (c *Client) restartDevicePlugin(ctx context.Context, namespace string) (map[string]interface{}, error) {
	// Find the device plugin daemonset
	dsList, err := c.clientset.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list daemonsets in namespace '%s': %w", namespace, err)
	}

	for i := range dsList.Items {
		ds := &dsList.Items[i]
		if strings.Contains(strings.ToLower(ds.Name), "nvidia-device-plugin") ||
			strings.Contains(strings.ToLower(ds.Name), "nvidia-device-plugin-ds") {

			// Rollout restart by patching the pod template annotation
			patch := []byte(fmt.Sprintf(
				`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":%q}}}}}`,
				time.Now().Format(time.RFC3339),
			))

			gvr, err := c.getCachedGVR("DaemonSet")
			if err != nil {
				return nil, fmt.Errorf("failed to get GVR for DaemonSet: %w", err)
			}

			_, err = c.dynamicClient.Resource(*gvr).Namespace(namespace).Patch(
				ctx, ds.Name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to restart device plugin daemonset '%s': %w", ds.Name, err)
			}

			return map[string]interface{}{
				"action":    "restartDevicePlugin",
				"daemonset": ds.Name,
				"namespace": namespace,
				"message":   fmt.Sprintf("Successfully triggered rollout restart of daemonset '%s'", ds.Name),
			}, nil
		}
	}

	return nil, fmt.Errorf("no NVIDIA device plugin daemonset found in namespace '%s'", namespace)
}

// restartGPUOperator performs a rollout restart of the GPU operator deployment.
func (c *Client) restartGPUOperator(ctx context.Context, namespace string) (map[string]interface{}, error) {
	depList, err := c.clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments in namespace '%s': %w", namespace, err)
	}

	for i := range depList.Items {
		dep := &depList.Items[i]
		if strings.Contains(strings.ToLower(dep.Name), "gpu-operator") {
			patch := []byte(fmt.Sprintf(
				`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":%q}}}}}`,
				time.Now().Format(time.RFC3339),
			))

			gvr, err := c.getCachedGVR("Deployment")
			if err != nil {
				return nil, fmt.Errorf("failed to get GVR for Deployment: %w", err)
			}

			_, err = c.dynamicClient.Resource(*gvr).Namespace(namespace).Patch(
				ctx, dep.Name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to restart GPU operator deployment '%s': %w", dep.Name, err)
			}

			return map[string]interface{}{
				"action":     "restartGPUOperator",
				"deployment": dep.Name,
				"namespace":  namespace,
				"message":    fmt.Sprintf("Successfully triggered rollout restart of deployment '%s'", dep.Name),
			}, nil
		}
	}

	return nil, fmt.Errorf("no GPU operator deployment found in namespace '%s'", namespace)
}

// removeNodeTaint removes a specific taint from a node.
func (c *Client) removeNodeTaint(ctx context.Context, nodeName, taintKey, taintEffect string) (map[string]interface{}, error) {
	node, err := c.clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get node '%s': %w", nodeName, err)
	}

	// Find and remove the matching taint
	newTaints := []corev1.Taint{}
	removed := false
	for _, taint := range node.Spec.Taints {
		if taint.Key == taintKey && (taintEffect == "" || string(taint.Effect) == taintEffect) {
			removed = true
			continue
		}
		newTaints = append(newTaints, taint)
	}

	if !removed {
		return map[string]interface{}{
			"action":  "removeTaint",
			"node":    nodeName,
			"message": fmt.Sprintf("Taint '%s:%s' not found on node '%s'", taintKey, taintEffect, nodeName),
			"changed": false,
		}, nil
	}

	node.Spec.Taints = newTaints
	_, err = c.clientset.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to update node '%s': %w", nodeName, err)
	}

	return map[string]interface{}{
		"action":  "removeTaint",
		"node":    nodeName,
		"removed": fmt.Sprintf("%s:%s", taintKey, taintEffect),
		"message": fmt.Sprintf("Successfully removed taint '%s:%s' from node '%s'", taintKey, taintEffect, nodeName),
		"changed": true,
	}, nil
}

// addGPULabel adds the standard GPU presence label to a node.
func (c *Client) addGPULabel(ctx context.Context, nodeName string) (map[string]interface{}, error) {
	patch := []byte(`{"metadata":{"labels":{"node.kubernetes.io/gpu":"present"}}}`)

	_, err := c.clientset.CoreV1().Nodes().Patch(ctx, nodeName, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to add GPU label to node '%s': %w", nodeName, err)
	}

	return map[string]interface{}{
		"action":  "addGPULabel",
		"node":    nodeName,
		"label":   "node.kubernetes.io/gpu=present",
		"message": fmt.Sprintf("Successfully added label 'node.kubernetes.io/gpu=present' to node '%s'", nodeName),
	}, nil
}

// annotateNodeForOperator adds annotations to prevent the NVIDIA operator from running
// managed-provider checks on a self-hosted cluster node.
func (c *Client) annotateNodeForOperator(ctx context.Context, nodeName string) (map[string]interface{}, error) {
	patch := []byte(`{"metadata":{"annotations":{` +
		`"kubeconfig.k8sbyexample.com/is-managed-node":"true",` +
		`"kubelet.kubernetes.io/ignored-verifications":"DockerContainerdNetworkInterface"` +
		`}}}`)

	_, err := c.clientset.CoreV1().Nodes().Patch(ctx, nodeName, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to annotate node '%s': %w", nodeName, err)
	}

	return map[string]interface{}{
		"action": "annotateNodeForOperator",
		"node":   nodeName,
		"annotations": map[string]string{
			"kubeconfig.k8sbyexample.com/is-managed-node": "true",
			"kubelet.kubernetes.io/ignored-verifications": "DockerContainerdNetworkInterface",
		},
		"message": fmt.Sprintf("Successfully annotated node '%s' to skip managed-provider checks", nodeName),
	}, nil
}
