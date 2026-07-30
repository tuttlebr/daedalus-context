package tools

import (
	"github.com/mark3labs/mcp-go/mcp"
)

// HelmInstallTool returns the MCP tool definition for installing Helm charts.
func HelmInstallTool() mcp.Tool {
	return mcp.NewTool("helmInstall",
		mcp.WithDescription("Install a Helm chart to the Kubernetes cluster, creating a new release. "+
			"Equivalent to 'helm install'. The chart can be referenced by name from a configured repo "+
			"(e.g. \"nginx/nginx-ingress\"), a local path, an absolute URL, or an OCI reference. "+
			"Use helmList to check if a release already exists — use helmUpgrade for existing releases. "+
			"WRITE OPERATION: only available when server is not in read-only mode."),

		// Required
		mcp.WithString("releaseName", mcp.Required(),
			mcp.Description("Name for the Helm release. Must be unique within the namespace. "+
				"Example: \"my-nginx\", \"prometheus-stack\".")),
		mcp.WithString("chartName", mcp.Required(),
			mcp.Description("Chart reference: \"repo/chart\" (e.g. \"bitnami/nginx\"), local path, absolute URL, or OCI reference (oci://...).")),

		// Namespace
		mcp.WithString("namespace",
			mcp.Description("Kubernetes namespace to install the release into. Defaults to \"default\".")),

		// Chart version / source
		mcp.WithString("version",
			mcp.Description("Chart version constraint (e.g. \"1.2.3\" or \"^2.0.0\"). Defaults to latest stable.")),
		mcp.WithBoolean("devel",
			mcp.Description("Include development/pre-release versions. Equivalent to --devel. Ignored when version is set.")),
		mcp.WithString("repoURL",
			mcp.Description("Helm repository URL. Only needed if the repo has not been added via helmRepoAdd. "+
				"Example: \"https://charts.bitnami.com/bitnami\".")),
		mcp.WithString("username",
			mcp.Description("Username for chart repository authentication.")),
		mcp.WithString("password",
			mcp.Description("Password for chart repository authentication.")),
		mcp.WithString("caFile",
			mcp.Description("Path to a CA bundle file for verifying HTTPS certificates of the chart server.")),
		mcp.WithString("certFile",
			mcp.Description("Path to a client SSL certificate file for HTTPS authentication.")),
		mcp.WithString("keyFile",
			mcp.Description("Path to a client SSL key file for HTTPS authentication.")),
		mcp.WithBoolean("insecureSkipTLSVerify",
			mcp.Description("Skip TLS certificate verification for chart downloads.")),
		mcp.WithBoolean("passCredentials",
			mcp.Description("Pass repository credentials to all domains, not just the origin.")),
		mcp.WithBoolean("plainHTTP",
			mcp.Description("Use plain HTTP (instead of HTTPS) for chart downloads.")),
		mcp.WithBoolean("verify",
			mcp.Description("Verify the chart against its provenance file before installing.")),

		// Values
		mcp.WithObject("values",
			mcp.Description("Key-value pairs to override chart defaults. Equivalent to --set. "+
				"Example: {\"replicaCount\": 3, \"image.tag\": \"latest\"}.")),
		mcp.WithArray("valuesFiles",
			mcp.Description("Paths to YAML values files to use (equivalent to -f / --values). "+
				"Files are merged in order; later entries take precedence. Direct values override all files."),
			mcp.WithStringItems()),

		// Install identity
		mcp.WithBoolean("createNamespace",
			mcp.Description("Create the release namespace if it does not exist. Defaults to true.")),
		mcp.WithBoolean("generateName",
			mcp.Description("Auto-generate the release name from the chart name (--generate-name). When true, releaseName is ignored.")),
		mcp.WithString("nameTemplate",
			mcp.Description("Go template used to generate the release name when generateName is true.")),
		mcp.WithString("description",
			mcp.Description("Custom description to attach to the release metadata.")),
		mcp.WithObject("labels",
			mcp.Description("Labels to add to the release metadata (string values only). "+
				"Example: {\"env\": \"prod\", \"team\": \"platform\"}.")),
		mcp.WithBoolean("dependencyUpdate",
			mcp.Description("Run 'helm dependency update' before installing if chart dependencies are missing.")),

		// Deployment behavior
		mcp.WithBoolean("wait",
			mcp.Description("Wait until all Pods, PVCs, Services, and Deployments are ready before marking the release successful.")),
		mcp.WithBoolean("waitForJobs",
			mcp.Description("When wait is true, also wait for all Jobs to complete before marking success.")),
		mcp.WithString("timeout",
			mcp.Description("Timeout for Kubernetes operations (e.g. \"5m0s\", \"300s\"). Defaults to 5m0s.")),
		mcp.WithBoolean("atomic",
			mcp.Description("Delete the release on failure (implies --wait). Rolls back automatically on error.")),

		// Dry-run / testing
		mcp.WithString("dryRun",
			mcp.Description("Simulate the install without applying changes. "+
				"\"client\" performs a local render with no cluster connection; "+
				"\"server\" sends the manifests to the server for validation only.")),
		mcp.WithBoolean("hideSecret",
			mcp.Description("Suppress Kubernetes Secret values in dry-run output.")),

		// Resource handling
		mcp.WithBoolean("force",
			mcp.Description("Force resource updates via a delete/recreate strategy.")),
		mcp.WithBoolean("replace",
			mcp.Description("Re-use the given release name, but only if the previous release was deleted and remains in history. Unsafe in production.")),
		mcp.WithBoolean("skipCRDs",
			mcp.Description("Do not install CRDs. By default, CRDs are installed if not already present.")),
		mcp.WithBoolean("noHooks",
			mcp.Description("Disable pre/post-install hooks.")),
		mcp.WithBoolean("takeOwnership",
			mcp.Description("Take ownership of existing resources that match the chart manifests, ignoring Helm annotations.")),

		// Validation
		mcp.WithBoolean("skipSchemaValidation",
			mcp.Description("Disable JSON schema validation of chart values.")),
		mcp.WithBoolean("disableOpenAPIValidation",
			mcp.Description("Disable validation of rendered manifests against the Kubernetes OpenAPI schema.")),

		// Output / rendering
		mcp.WithBoolean("renderSubchartNotes",
			mcp.Description("Render notes from subcharts in addition to the parent chart notes.")),
		mcp.WithBoolean("hideNotes",
			mcp.Description("Suppress the NOTES.txt output after install.")),
		mcp.WithBoolean("enableDNS",
			mcp.Description("Enable DNS lookups when rendering chart templates.")),
	)
}

// HelmUpgradeTool returns the MCP tool definition for upgrading Helm releases.
func HelmUpgradeTool() mcp.Tool {
	return mcp.NewTool("helmUpgrade",
		mcp.WithDescription("Upgrade an existing Helm release to a new chart version or with new values. "+
			"Equivalent to 'helm upgrade'. The release must already exist — use helmInstall for new releases. "+
			"The chart can be referenced by repo name (e.g. \"bitnami/nginx\"), local path, absolute URL, or OCI reference (oci://...). "+
			"Use helmHistory to see previous revisions and helmRollback to revert if needed. "+
			"WRITE OPERATION: only available when server is not in read-only mode."),

		// Required
		mcp.WithString("releaseName", mcp.Required(),
			mcp.Description("Name of the existing Helm release to upgrade.")),
		mcp.WithString("chartName", mcp.Required(),
			mcp.Description("Chart reference: \"repo/chart\" (e.g. \"bitnami/nginx\"), local path, absolute URL, or OCI reference (oci://...).")),

		// Namespace
		mcp.WithString("namespace",
			mcp.Description("Kubernetes namespace where the release is installed. Defaults to \"default\" if omitted.")),

		// Chart version / source
		mcp.WithString("version",
			mcp.Description("Chart version to upgrade to (e.g. \"1.2.3\"). Required when pinning a specific version of an OCI or repo chart. Defaults to latest stable.")),
		mcp.WithBoolean("devel",
			mcp.Description("Include development/pre-release versions. Ignored when version is set.")),
		mcp.WithString("repoURL",
			mcp.Description("Helm repository URL. Only needed if the repo has not been added via helmRepoAdd. "+
				"Example: \"https://prometheus-community.github.io/helm-charts\".")),
		mcp.WithString("username",
			mcp.Description("Username for chart repository authentication.")),
		mcp.WithString("password",
			mcp.Description("Password for chart repository authentication.")),
		mcp.WithString("caFile",
			mcp.Description("Path to a CA bundle file for verifying HTTPS certificates of the chart server.")),
		mcp.WithString("certFile",
			mcp.Description("Path to a client SSL certificate file for HTTPS authentication.")),
		mcp.WithString("keyFile",
			mcp.Description("Path to a client SSL key file for HTTPS authentication.")),
		mcp.WithBoolean("insecureSkipTLSVerify",
			mcp.Description("Skip TLS certificate verification for chart downloads.")),
		mcp.WithBoolean("passCredentials",
			mcp.Description("Pass repository credentials to all domains, not just the origin.")),
		mcp.WithBoolean("plainHTTP",
			mcp.Description("Use plain HTTP (instead of HTTPS) for chart downloads.")),
		mcp.WithBoolean("verify",
			mcp.Description("Verify the chart against its provenance file before upgrading.")),

		// Values
		mcp.WithObject("values",
			mcp.Description("Key-value pairs to override chart values. Pass as a JSON object. "+
				"Example: {\"replicaCount\": 5, \"image.tag\": \"v2.0\"}.")),
		mcp.WithArray("valuesFiles",
			mcp.Description("Paths to YAML values files to use (equivalent to -f / --values). "+
				"Files are merged in order; later entries take precedence. Direct values override all files."),
			mcp.WithStringItems()),

		// Release metadata
		mcp.WithString("description",
			mcp.Description("Custom description to attach to the release metadata.")),
		mcp.WithObject("labels",
			mcp.Description("Labels to add to the release metadata (string values only). "+
				"Example: {\"env\": \"prod\", \"team\": \"platform\"}.")),

		// Deployment behavior
		mcp.WithBoolean("wait",
			mcp.Description("Wait until all Pods, PVCs, Services, and Deployments are ready before marking the upgrade successful.")),
		mcp.WithBoolean("waitForJobs",
			mcp.Description("When wait is true, also wait for all Jobs to complete before marking success.")),
		mcp.WithString("timeout",
			mcp.Description("Timeout for Kubernetes operations (e.g. \"5m0s\", \"300s\"). Defaults to 5m0s.")),
		mcp.WithBoolean("atomic",
			mcp.Description("Roll back the release on failure (implies --wait).")),
		mcp.WithBoolean("cleanupOnFail",
			mcp.Description("Delete new resources created during a failed upgrade.")),

		// Dry-run / testing
		mcp.WithString("dryRun",
			mcp.Description("Simulate the upgrade without applying changes. "+
				"\"client\" performs a local render with no cluster connection; "+
				"\"server\" sends the manifests to the server for validation only.")),

		// Resource handling
		mcp.WithBoolean("force",
			mcp.Description("Force resource updates via a delete/recreate strategy.")),
		mcp.WithBoolean("noHooks",
			mcp.Description("Disable pre/post-upgrade hooks.")),
		mcp.WithBoolean("skipCRDs",
			mcp.Description("Do not install CRDs during upgrade.")),

		// Values reuse strategy
		mcp.WithBoolean("reuseValues",
			mcp.Description("Reuse the last release's values and merge in any new values. Cannot be used with resetValues.")),
		mcp.WithBoolean("resetValues",
			mcp.Description("Reset all values to chart defaults, then apply any supplied overrides. Cannot be used with reuseValues.")),
		mcp.WithBoolean("resetThenReuseValues",
			mcp.Description("Reset chart values to defaults, then reuse the last release's values, then merge any supplied overrides.")),

		// Validation
		mcp.WithBoolean("disableOpenAPIValidation",
			mcp.Description("Disable validation of rendered manifests against the Kubernetes OpenAPI schema.")),

		// Output / rendering
		mcp.WithBoolean("renderSubchartNotes",
			mcp.Description("Render notes from subcharts in addition to the parent chart notes.")),
		mcp.WithBoolean("hideNotes",
			mcp.Description("Suppress the NOTES.txt output after upgrade.")),
		mcp.WithBoolean("enableDNS",
			mcp.Description("Enable DNS lookups when rendering chart templates.")),

		// History
		mcp.WithNumber("maxHistory",
			mcp.Description("Maximum number of release revisions to retain in history. 0 means no limit.")),
	)
}

// HelmUninstallTool returns the MCP tool definition for uninstalling Helm releases.
func HelmUninstallTool() mcp.Tool {
	return mcp.NewTool("helmUninstall",
		mcp.WithDescription("Uninstall a Helm release, removing all associated Kubernetes resources. "+
			"Equivalent to 'helm uninstall'. This is a destructive operation that cannot be undone. "+
			"Use helmGet to inspect the release before uninstalling. "+
			"WRITE OPERATION: only available when server is not in read-only mode."),
		mcp.WithString("releaseName", mcp.Required(),
			mcp.Description("Exact name of the Helm release to uninstall. Use helmList to discover release names.")),
		mcp.WithString("namespace", mcp.Required(),
			mcp.Description("Kubernetes namespace where the release is installed.")),
	)
}

// HelmListTool returns the MCP tool definition for listing Helm releases.
func HelmListTool() mcp.Tool {
	return mcp.NewTool("helmList",
		mcp.WithDescription("List Helm releases with their status, chart version, app version, and last update time. "+
			"Equivalent to 'helm list'. Use this to discover installed releases before running helmGet, helmUpgrade, or helmUninstall."),
		mcp.WithString("namespace", mcp.Required(),
			mcp.Description("Kubernetes namespace to list releases from. Pass an empty string \"\" to list releases across all namespaces.")),
	)
}

// HelmGetTool returns the MCP tool definition for getting Helm release details.
func HelmGetTool() mcp.Tool {
	return mcp.NewTool("helmGet",
		mcp.WithDescription("Get detailed information about a specific Helm release, including its chart, values, "+
			"and manifest. Equivalent to 'helm get all'. "+
			"For release revision history, use helmHistory instead."),
		mcp.WithString("releaseName", mcp.Required(),
			mcp.Description("Exact name of the Helm release.")),
		mcp.WithString("namespace", mcp.Required(),
			mcp.Description("Kubernetes namespace where the release is installed.")),
	)
}

// HelmHistoryTool returns the MCP tool definition for getting Helm release history.
func HelmHistoryTool() mcp.Tool {
	return mcp.NewTool("helmHistory",
		mcp.WithDescription("Get the revision history of a Helm release, showing all past upgrades and rollbacks. "+
			"Equivalent to 'helm history'. Shows revision number, status, chart version, and description for each revision. "+
			"Use the revision number with helmRollback to revert to a previous version."),
		mcp.WithString("releaseName", mcp.Required(),
			mcp.Description("Exact name of the Helm release.")),
		mcp.WithString("namespace", mcp.Required(),
			mcp.Description("Kubernetes namespace where the release is installed.")),
	)
}

// HelmRollbackTool returns the MCP tool definition for rolling back Helm releases.
func HelmRollbackTool() mcp.Tool {
	return mcp.NewTool("helmRollback",
		mcp.WithDescription("Rollback a Helm release to a previous revision. Equivalent to 'helm rollback'. "+
			"Use helmHistory first to see available revisions and their details. "+
			"WRITE OPERATION: only available when server is not in read-only mode."),
		mcp.WithString("releaseName", mcp.Required(),
			mcp.Description("Exact name of the Helm release to rollback.")),
		mcp.WithString("namespace", mcp.Required(),
			mcp.Description("Kubernetes namespace where the release is installed.")),
		mcp.WithNumber("revision", mcp.Required(),
			mcp.Description("Revision number to rollback to. Use 0 to rollback to the immediately previous revision. "+
				"Use helmHistory to discover available revision numbers."),
			mcp.Min(0)),
	)
}

// HelmRepoAddTool returns the MCP tool definition for adding Helm repositories.
func HelmRepoAddTool() mcp.Tool {
	return mcp.NewTool("helmRepoAdd",
		mcp.WithDescription("Add a Helm chart repository so that charts from it can be installed. "+
			"Equivalent to 'helm repo add'. After adding, use helmInstall with the repo name prefix (e.g. \"myrepo/mychart\"). "+
			"Use helmRepoList to see currently configured repositories. "+
			"WRITE OPERATION: only available when server is not in read-only mode."),
		mcp.WithString("repoName", mcp.Required(),
			mcp.Description("Local name for the repository. Used as a prefix when referencing charts (e.g. \"bitnami\"). "+
				"Example: \"bitnami\", \"prometheus-community\".")),
		mcp.WithString("repoURL", mcp.Required(),
			mcp.Description("URL of the Helm chart repository index. "+
				"Example: \"https://charts.bitnami.com/bitnami\", \"https://prometheus-community.github.io/helm-charts\".")),
	)
}

// HelmRepoListTool returns the MCP tool definition for listing configured Helm repositories.
func HelmRepoListTool() mcp.Tool {
	return mcp.NewTool("helmRepoList",
		mcp.WithDescription("List all configured Helm chart repositories with their names and URLs. "+
			"Equivalent to 'helm repo list'. Use this to verify which repositories are available "+
			"before installing charts with helmInstall."),
	)
}
