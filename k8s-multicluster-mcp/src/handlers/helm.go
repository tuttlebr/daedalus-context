package handlers

import (
	"context"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/tuttlebr/kubernetes-mcp-server/pkg/helm"
)

// HelmInstall returns a handler function for the helmInstall tool

func HelmInstall(client *helm.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		releaseName, err := getRequiredStringArg(args, "releaseName")
		if err != nil {
			return nil, err
		}

		chartName, err := getRequiredStringArg(args, "chartName")
		if err != nil {
			return nil, err
		}

		namespace := getStringArg(args, "namespace", "default")

		values := make(map[string]interface{})
		if v, exists := args["values"]; exists {
			if valuesMap, ok := v.(map[string]interface{}); ok {
				values = valuesMap
			}
		}

		var timeout time.Duration
		if ts := getStringArg(args, "timeout", ""); ts != "" {
			if timeout, err = time.ParseDuration(ts); err != nil {
				return nil, fmt.Errorf("invalid timeout %q: %w", ts, err)
			}
		}

		var labels map[string]string
		if l, exists := args["labels"]; exists {
			if lMap, ok := l.(map[string]interface{}); ok && len(lMap) > 0 {
				labels = make(map[string]string, len(lMap))
				for k, v := range lMap {
					if sv, ok := v.(string); ok {
						labels[k] = sv
					}
				}
			}
		}

		opts := helm.InstallOptions{
			Version:                  getStringArg(args, "version", ""),
			Devel:                    getBoolArg(args, "devel", false),
			RepoURL:                  getStringArg(args, "repoURL", ""),
			Username:                 getStringArg(args, "username", ""),
			Password:                 getStringArg(args, "password", ""),
			CaFile:                   getStringArg(args, "caFile", ""),
			CertFile:                 getStringArg(args, "certFile", ""),
			KeyFile:                  getStringArg(args, "keyFile", ""),
			InsecureSkipTLSVerify:    getBoolArg(args, "insecureSkipTLSVerify", false),
			PassCredentials:          getBoolArg(args, "passCredentials", false),
			PlainHTTP:                getBoolArg(args, "plainHTTP", false),
			Verify:                   getBoolArg(args, "verify", false),
			ValuesFiles:              getStringArrayArg(args, "valuesFiles"),
			CreateNamespace:          getBoolArg(args, "createNamespace", true),
			GenerateName:             getBoolArg(args, "generateName", false),
			NameTemplate:             getStringArg(args, "nameTemplate", ""),
			Description:              getStringArg(args, "description", ""),
			Labels:                   labels,
			DependencyUpdate:         getBoolArg(args, "dependencyUpdate", false),
			Wait:                     getBoolArg(args, "wait", false),
			WaitForJobs:              getBoolArg(args, "waitForJobs", false),
			Timeout:                  timeout,
			Atomic:                   getBoolArg(args, "atomic", false),
			DryRunOption:             getStringArg(args, "dryRun", ""),
			HideSecret:               getBoolArg(args, "hideSecret", false),
			Force:                    getBoolArg(args, "force", false),
			Replace:                  getBoolArg(args, "replace", false),
			SkipCRDs:                 getBoolArg(args, "skipCRDs", false),
			DisableHooks:             getBoolArg(args, "noHooks", false),
			TakeOwnership:            getBoolArg(args, "takeOwnership", false),
			SkipSchemaValidation:     getBoolArg(args, "skipSchemaValidation", false),
			DisableOpenAPIValidation: getBoolArg(args, "disableOpenAPIValidation", false),
			SubNotes:                 getBoolArg(args, "renderSubchartNotes", false),
			HideNotes:                getBoolArg(args, "hideNotes", false),
			EnableDNS:                getBoolArg(args, "enableDNS", false),
		}

		release, err := client.InstallChart(ctx, namespace, releaseName, chartName, opts, values)
		if err != nil {
			return nil, fmt.Errorf("failed to install chart: %w", err)
		}

		jsonResponse, err := marshalSafe(release)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// HelmUpgrade returns a handler function for the helmUpgrade tool
func HelmUpgrade(client *helm.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		releaseName, err := getRequiredStringArg(args, "releaseName")
		if err != nil {
			return nil, err
		}

		chartName, err := getRequiredStringArg(args, "chartName")
		if err != nil {
			return nil, err
		}

		namespace := getStringArg(args, "namespace", "default")

		values := make(map[string]interface{})
		if v, exists := args["values"]; exists {
			if valuesMap, ok := v.(map[string]interface{}); ok {
				values = valuesMap
			}
		}

		var upgradeTimeout time.Duration
		if ts := getStringArg(args, "timeout", ""); ts != "" {
			if upgradeTimeout, err = time.ParseDuration(ts); err != nil {
				return nil, fmt.Errorf("invalid timeout %q: %w", ts, err)
			}
		}

		var labels map[string]string
		if l, exists := args["labels"]; exists {
			if lMap, ok := l.(map[string]interface{}); ok && len(lMap) > 0 {
				labels = make(map[string]string, len(lMap))
				for k, v := range lMap {
					if sv, ok := v.(string); ok {
						labels[k] = sv
					}
				}
			}
		}

		opts := helm.UpgradeOptions{
			Version:                  getStringArg(args, "version", ""),
			Devel:                    getBoolArg(args, "devel", false),
			RepoURL:                  getStringArg(args, "repoURL", ""),
			Username:                 getStringArg(args, "username", ""),
			Password:                 getStringArg(args, "password", ""),
			CaFile:                   getStringArg(args, "caFile", ""),
			CertFile:                 getStringArg(args, "certFile", ""),
			KeyFile:                  getStringArg(args, "keyFile", ""),
			InsecureSkipTLSVerify:    getBoolArg(args, "insecureSkipTLSVerify", false),
			PassCredentials:          getBoolArg(args, "passCredentials", false),
			PlainHTTP:                getBoolArg(args, "plainHTTP", false),
			Verify:                   getBoolArg(args, "verify", false),
			ValuesFiles:              getStringArrayArg(args, "valuesFiles"),
			Description:              getStringArg(args, "description", ""),
			Labels:                   labels,
			Wait:                     getBoolArg(args, "wait", false),
			WaitForJobs:              getBoolArg(args, "waitForJobs", false),
			Timeout:                  upgradeTimeout,
			Atomic:                   getBoolArg(args, "atomic", false),
			CleanupOnFail:            getBoolArg(args, "cleanupOnFail", false),
			DryRunOption:             getStringArg(args, "dryRun", ""),
			Force:                    getBoolArg(args, "force", false),
			DisableHooks:             getBoolArg(args, "noHooks", false),
			SkipCRDs:                 getBoolArg(args, "skipCRDs", false),
			ReuseValues:              getBoolArg(args, "reuseValues", false),
			ResetValues:              getBoolArg(args, "resetValues", false),
			ResetThenReuseValues:     getBoolArg(args, "resetThenReuseValues", false),
			DisableOpenAPIValidation: getBoolArg(args, "disableOpenAPIValidation", false),
			SubNotes:                 getBoolArg(args, "renderSubchartNotes", false),
			HideNotes:                getBoolArg(args, "hideNotes", false),
			EnableDNS:                getBoolArg(args, "enableDNS", false),
			MaxHistory:               getIntArg(args, "maxHistory", 0),
		}

		release, err := client.UpgradeChart(ctx, namespace, releaseName, chartName, opts, values)
		if err != nil {
			return nil, fmt.Errorf("failed to upgrade chart: %w", err)
		}

		jsonResponse, err := marshalSafe(release)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// HelmUninstall returns a handler function for the helmUninstall tool
func HelmUninstall(client *helm.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		releaseName, err := getRequiredStringArg(args, "releaseName")
		if err != nil {
			return nil, err
		}

		namespace := getStringArg(args, "namespace", "default")

		err = client.UninstallChart(ctx, namespace, releaseName)
		if err != nil {
			return nil, fmt.Errorf("failed to uninstall chart: %w", err)
		}

		response := map[string]string{
			"status":  "success",
			"message": fmt.Sprintf("Successfully uninstalled release '%s' from namespace '%s'", releaseName, namespace),
		}

		jsonResponse, err := marshalSafe(response)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// HelmList returns a handler function for the helmList tool
func HelmList(client *helm.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		namespace := getStringArg(args, "namespace", "")

		releases, err := client.ListReleases(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to list releases: %w", err)
		}

		jsonResponse, err := marshalSafe(releases)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// HelmGet returns a handler function for the helmGet tool
func HelmGet(client *helm.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		releaseName, err := getRequiredStringArg(args, "releaseName")
		if err != nil {
			return nil, err
		}

		namespace := getStringArg(args, "namespace", "default")

		release, err := client.GetRelease(ctx, namespace, releaseName)
		if err != nil {
			return nil, fmt.Errorf("failed to get release: %w", err)
		}

		jsonResponse, err := marshalSafe(release)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// HelmHistory returns a handler function for the helmHistory tool
func HelmHistory(client *helm.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		releaseName, err := getRequiredStringArg(args, "releaseName")
		if err != nil {
			return nil, err
		}

		namespace := getStringArg(args, "namespace", "default")

		history, err := client.GetReleaseHistory(ctx, namespace, releaseName)
		if err != nil {
			return nil, fmt.Errorf("failed to get release history: %w", err)
		}

		jsonResponse, err := marshalSafe(history)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// HelmRollback returns a handler function for the helmRollback tool
func HelmRollback(client *helm.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		releaseName, err := getRequiredStringArg(args, "releaseName")
		if err != nil {
			return nil, err
		}

		namespace := getStringArg(args, "namespace", "default")

		revision := getIntArg(args, "revision", 0)

		err = client.RollbackRelease(ctx, namespace, releaseName, revision)
		if err != nil {
			return nil, fmt.Errorf("failed to rollback release: %w", err)
		}

		response := map[string]interface{}{
			"status":   "success",
			"message":  fmt.Sprintf("Successfully rolled back release '%s' in namespace '%s'", releaseName, namespace),
			"revision": revision,
		}

		jsonResponse, err := marshalSafe(response)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

// HelmRepoAdd returns a handler function for the helmRepoAdd tool
func HelmRepoAdd(client *helm.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid arguments type: expected map[string]interface{}")
		}

		repoName, err := getRequiredStringArg(args, "repoName")
		if err != nil {
			return nil, err
		}

		repoURL, err := getRequiredStringArg(args, "repoURL")
		if err != nil {
			return nil, err
		}

		err = client.HelmRepoAdd(ctx, repoName, repoURL)
		if err != nil {
			return nil, fmt.Errorf("failed to add repository: %w", err)
		}

		response := map[string]interface{}{
			"status":  "success",
			"message": fmt.Sprintf("Successfully added repository '%s' with URL '%s'", repoName, repoURL),
		}

		jsonResponse, err := marshalSafe(response)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}

func HelmRepoList(client *helm.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		repos, err := client.HelmRepoList(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list repositories: %w", err)
		}

		jsonResponse, err := marshalSafe(repos)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize response: %w", err)
		}

		return mcp.NewToolResultText(string(jsonResponse)), nil
	}
}
