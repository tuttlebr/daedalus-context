# Kubernetes and Helm MCP Server

This Go server exposes Kubernetes and Helm tools through MCP, with stdio, SSE,
and streamable HTTP transports. It is the Kubernetes tool provider in the
Daedalus Context monorepo. The root deployment defaults to authenticated,
read-only Kubernetes access. Optional write tools and the opencode-backed
DevOps agent have explicit capability gates.

## Deployment

Use the [root configuration setup](../docs/deployment.md#configuration), then run from
the repository root:

```bash
make deploy COMPONENTS=k8s-mcp
make health-provider COMPONENTS=k8s-mcp
make health-dependencies COMPONENTS=k8s-mcp
make health-integration COMPONENTS=k8s-mcp
```

The [active Helm chart](chart/README.md) deploys in `daedalus`.
It uses in-cluster ServiceAccount credentials. By default, the chart enables
`SERVER_READ_ONLY`, `SERVER_NO_HELM`, and `MCP_REQUIRE_AUTH`, with observer RBAC
that excludes Secret data. Persist `K8S_MCP_READ_ONLY=false` and
`K8S_MCP_NO_HELM=false` in the root `.env` to enable Kubernetes and Helm writes
with cluster-wide resource CRUD permissions. Helm also requires Secret access
for release state. The independent `K8S_MCP_ENABLE_EXEC`,
`K8S_MCP_ENABLE_KUBECTL`, and `K8S_MCP_ENABLE_AGENT_WRITE` settings default to
`false`; set them to `true` to expose those capabilities in write-enabled mode.
The [chart documentation](chart/README.md) describes the RBAC
and rollback behavior. HTTP authentication remains mandatory in every mode.

The in-cluster endpoint is
`http://k8s-mcp-server.daedalus.svc.cluster.local:8080/mcp`. HTTP clients supply
`Authorization: Bearer <token>` or `X-MCP-Token: <token>`. Existing Secrets
provide current/next auth tokens and optional agent configuration.

External access defaults off. When enabled, the root workflow uses the configured
fixed NodePort and client CIDR. Follow the
[external MCP prerequisite](../docs/deployment.md#external-mcp-network-prerequisite),
preserve working LAN clients and routes, and verify the authenticated API from
a non-node LAN source. The root workflow deploys the service-owned `chart/`;
`make render` in this folder renders that same chart locally.

## Local Development

Build from this component directory using the Go toolchain in
[src/go.mod](src/go.mod):

```bash
make build
./src/k8s-mcp-server --mode stdio --read-only
```

Running the server accesses the configured cluster. Before doing so, use the
root environment/context setup and `make preflight`. CPU and memory tools also
require a metrics API. The binary defaults differ from the root chart; select
read-only mode explicitly for inspection.

Build and publish images through the root image workflow. The
[Dockerfile](src/Dockerfile) owns runtime dependencies and bundled tooling;
[go.mod](src/go.mod) and `go.sum` own library versions.
The component `docker` and `docker-push` targets delegate to the root image
commands with `IMAGE_BUILD_SERVICES=k8s-mcp-server-read-only`; the historical
Compose service name does not override the chart's capability settings.

## Server Configuration

| Flag          | Environment Variable | Default | Description                                      |
| ------------- | -------------------- | ------- | ------------------------------------------------ |
| `--mode`      | `SERVER_MODE`        | `sse`   | Transport: `stdio`, `sse`, or `streamable-http`  |
| `--port`      | `SERVER_PORT`        | `8080`  | HTTP listen port (SSE and streamable-http modes) |
| `--read-only` | `SERVER_READ_ONLY`   | `false` | Disable all write operations                     |
| `--no-k8s`    | —                    | `false` | Disable all Kubernetes tools                     |
| `--no-helm`   | `SERVER_NO_HELM`      | `false` | Disable all Helm tools                           |
| `--no-agent`  | —                    | `false` | Disable the DevOps agent tool                    |
| `--enable-exec` | `MCP_ENABLE_EXEC` | `false` | Expose the dangerous `execInPod` capability when not read-only |
| `--enable-kubectl` | `MCP_ENABLE_KUBECTL` | `false` | Expose the dangerous `runKubectlCommand` capability when not read-only |
| `--enable-agent-write` | `MCP_ENABLE_AGENT_WRITE` | `false` | Allow `devopsAgent` to run write-capable child MCP sessions when not read-only |

`--no-k8s` and `--no-helm` cannot be used together.

### HTTP Authentication

For HTTP transports, set `MCP_AUTH_TOKEN` to require `Authorization: Bearer
<token>` or `X-MCP-Token: <token>` on MCP requests. Set
`MCP_AUTH_TOKEN_NEXT` during a rotation overlap. Health endpoints remain
unauthenticated for Kubernetes probes.

Set `MCP_REQUIRE_AUTH=true` to fail startup when an HTTP transport is selected without `MCP_AUTH_TOKEN`. The root Helm chart requires this mode.

### DevOps Agent Environment Variables

These are **optional**. When all three are set, the `devopsAgent` tool is registered. In `--read-only` server mode, or when `MCP_ENABLE_AGENT_WRITE` is not enabled, the agent is still available but is forced into inspection-only mode.

| Variable           | Description                                                         | Example                                |
| ------------------ | ------------------------------------------------------------------- | -------------------------------------- |
| `OPENCODE_BASE_URL` | OpenAI-compatible API base URL                                     | `http://llm.local:8080/v1`            |
| `OPENCODE_API_KEY`  | API key for the LLM provider                                      | `sk-...`                               |
| `OPENCODE_MODEL`    | Model ID in `provider/model` format                                | `private-llm/llama-3.1-70b`           |

### Kubeconfig Resolution Order

1. Explicit path via `KUBECONFIG` environment variable
2. In-cluster ServiceAccount config (when running inside Kubernetes)
3. `~/.kube/config` fallback

### Health Check

All HTTP modes expose `GET /livez` and `/readyz`, which check only the server
process and do not query Kubernetes.
`GET /dependencyz` performs one bounded Kubernetes API request. `/healthz`
remains a local compatibility alias. Upstream slowness never turns liveness
into a restart loop.

## Operational Contracts

- Read-only mode omits Kubernetes and Helm mutations and forces agent inspection.
  Exec, direct kubectl, and agent writes require their separate capability gates
  as well as write-enabled server mode and appropriate RBAC.
- Every registered tool passes through the audit wrapper. Logs include tool,
  capability, status, duration, and errors.
- Structured and text responses pass through secret redaction and output bounds.
  Kubernetes Secret values, logs, Helm results, and agent output use those paths.
  Bounds and compaction are implemented in the server and response helpers.
- Resource creation uses server-side apply with field manager `k8s-mcp-server`.
  Handlers validate identity fields before submitting changes.
- Resource listing is paginated; discovery and summary tools return bounded
  output. Narrow filters when a response indicates truncation.
- Health endpoints distinguish the local process from upstream Kubernetes
  connectivity. A local readiness response is not a cluster health assessment.
- Process-global context switching is not exposed. Each server instance retains
  its configured cluster identity.

## DevOps Agent

`devopsAgent` launches `opencode run` with a child stdio MCP server. It requires
`OPENCODE_BASE_URL`, `OPENCODE_API_KEY`, and `OPENCODE_MODEL`. The chart optionally
reads those settings from the Secret named by `agent.existingSecret`, with keys
`base-url`, `api-key`, and `model`; provision it through the approved Secret path.

The agent is inspection-only unless the parent is write-enabled and
`MCP_ENABLE_AGENT_WRITE=true`. Child sessions inherit applicable exec/kubectl
capability gates. A tool prompt does not expand the server's authority.

| Parameter | Requirement | Meaning |
| --- | --- | --- |
| `prompt` | Required | Task to perform |
| `namespace` | Optional | Investigation scope |
| `model` | Optional | Override the configured model |
| `timeout` | Optional | Bounded execution time, as defined in the tool schema |
| `readOnly` | Optional | Request inspection-only behavior; server restrictions take precedence |

The image copies `src/skills/` into the opencode configuration directory. The
tracked tree contains a placeholder; image builders must supply the intended
skill set or use a reviewed runtime volume mount. Do not assume a named skill is
bundled without inspecting the build inputs. Keep reusable instructions in each
skill's `SKILL.md` and validate them when changed.

## Tool Reference

### Kubernetes — Read-Only Tools

#### Discovery & Core

| Tool                    | Description                                                 | Key Parameters                                          |
| ----------------------- | ----------------------------------------------------------- | ------------------------------------------------------- |
| `getAPIResources`       | Search available API resource types with a bounded result    | `includeNamespaceScoped`, `includeClusterScoped`, `query`, `limit` |
| `listResources`         | List compact resource summaries, optionally filtered         | `kind`\*, `namespace`, `labelSelector`, `fieldSelector`, `includeDetails`, `limit` |
| `getResource`           | Get full JSON of a single resource                          | `kind`\*, `name`\*, `namespace`                         |
| `describeResource`      | Human-friendly description with events and owner refs       | `kind`\*, `name`\*, `namespace`                         |
| `getResourceYAML`       | Export resource as clean YAML                               | `kind`\*, `name`\*, `namespace`                         |
| `getResourceDiff`       | Compare current state with previous or another resource     | `kind`\*, `name`\*, `namespace`, `compareWith`          |
| `getNamespaceResources` | List all resources in a namespace                           | `namespace`\*, `types`, `includeSecrets`                |
| `getResourceOwners`     | Trace ownership chain (e.g., Pod → ReplicaSet → Deployment) | `kind`\*, `name`\*, `namespace`, `includeChildren`      |
| `getIngresses`          | Find ingresses matching a hostname                          | `host`\*                                                |
| `listNamespaces`        | List all namespaces with status and labels                  | `labelSelector`                                         |
| `listContexts`          | List available kubeconfig contexts                          | —                                                       |

#### Monitoring & Observability

| Tool                | Description                                          | Key Parameters                    |
| ------------------- | ---------------------------------------------------- | --------------------------------- |
| `getClusterHealth`  | Cluster health report (nodes, control plane, events) | `includeMetrics`, `includeEvents` |
| `getClusterSummary` | Concise cluster status overview; namespace detail is opt-in | `includeNamespaceDetails`         |
| `getTopPods`        | Top pods by CPU or memory usage                      | `namespace`, `sortBy`, `limit`    |
| `getTopNodes`       | Top nodes by resource utilization                    | `sortBy`, `includeConditions`     |
| `getNodeMetrics`    | CPU/memory metrics for a specific node               | `name`\*                          |
| `getPodMetrics`     | CPU/memory metrics for a specific pod                | `namespace`\*, `podName`\*        |
| `getResourceQuotas` | Resource quotas and usage                            | `namespace`, `showPercentage`     |
| `getLimitRanges`    | Limit ranges in namespaces                           | `namespace`                       |
| `getEvents`         | Newest cluster events, optionally filtered and bounded | `namespace`, `labelSelector`, `limit` |
| `getRolloutStatus`  | Rollout status for Deployment/StatefulSet/DaemonSet  | `kind`\*, `name`\*, `namespace`\* |

#### Debugging & Troubleshooting

| Tool                  | Description                                        | Key Parameters                                     |
| --------------------- | -------------------------------------------------- | -------------------------------------------------- |
| `getPodsLogs`         | Time-bounded pod logs with timestamps              | `name`\*, `namespace`\*, `containerName`, `sinceSeconds`, `tailLines` |
| `getPodDebugInfo`     | Comprehensive pod debug (conditions, events, logs) | `name`\*, `namespace`\*, `includeLogs`, `logLines`, `sinceSeconds` |
| `getServiceEndpoints` | Service endpoints with pod health status           | `name`\*, `namespace`\*, `checkHealth`             |
| `getNetworkPolicies`  | Network policies affecting a namespace or pod      | `namespace`\*, `podName`, `includeDetails`         |
| `getSecurityContext`  | Pod and container security contexts                | `name`\*, `namespace`\*, `includeDefaults`         |
| `getResourceHistory`  | Recent events and changes for a resource           | `kind`\*, `name`\*, `namespace`, `hours`           |
| `validateManifest`    | Dry-run validation of YAML/JSON manifests          | `manifest`\*, `format`, `strict`                   |

Log-returning tools default to the last hour, include Kubernetes timestamps,
and apply a per-container line cap. Set `sinceSeconds` explicitly when a
different window is required; the supported range is one second through seven
days.

#### GPU Debugging

| Tool                     | Description                                      | Key Parameters                                      |
| ------------------------ | ------------------------------------------------ | --------------------------------------------------- |
| `getGPUClusterOverview`  | Cluster-wide GPU resource and workload overview  | `includeNonGPUNodes`, `includeEvents`               |
| `diagnoseGPUScheduling`  | Diagnose GPU scheduling for a specific pod       | `podName`\*, `namespace`\*                          |
| `getGPUOperatorHealth`   | NVIDIA operator and device plugin health check   | `devicePluginNamespace`, `gpuOperatorNamespace`, `logLines`, `sinceSeconds` |

### Kubernetes — Write Tools

Disabled when `--read-only` is set. `execInPod` and `runKubectlCommand` also require explicit dangerous capability gates.

| Tool                 | Description                               | Key Parameters                                    |
| -------------------- | ----------------------------------------- | ------------------------------------------------- |
| `createResource`     | Create/update resource from JSON manifest | `kind`\*, `manifest`\*, `namespace`               |
| `createResourceYAML` | Create/update resource from YAML manifest | `yamlManifest`\*, `kind`, `namespace`             |
| `deleteResource`     | Delete a resource by kind and name        | `kind`\*, `name`\*, `namespace`                   |
| `rolloutRestart`     | Rolling restart of a workload             | `kind`\*, `name`\*, `namespace`\*                 |
| `scaleResource`      | Scale a workload's replica count          | `kind`\*, `name`\*, `namespace`\*, `replicas`\*   |
| `remediateGPUIssue`  | Apply GPU remediation actions             | `action`\*, `nodeName`, `taintKey`                |
| `execInPod`          | Execute a command in a pod container. Requires `MCP_ENABLE_EXEC=true`. | `name`\*, `namespace`\*, `command`\*, `container` |
| `runKubectlCommand`  | Execute a direct kubectl command. Requires `kubectl` in PATH and `MCP_ENABLE_KUBECTL=true`. | `command`\*, `timeout` |

`switchContext` is intentionally not registered. Process-global context switching is unsafe for concurrent HTTP MCP usage; use separate server instances for different cluster identities.

### Agent Tool

| Tool          | Description                                                          | Key Parameters                                               |
| ------------- | -------------------------------------------------------------------- | ------------------------------------------------------------ |
| `devopsAgent` | Autonomous opencode-powered DevOps agent. Forced read-only when the parent server is read-only. | `prompt`\*, `namespace`, `model`, `timeout`, `readOnly` |

### Helm — Read-Only Tools (4)

| Tool           | Description                                   | Key Parameters                 |
| -------------- | --------------------------------------------- | ------------------------------ |
| `helmList`     | List Helm releases                            | `namespace`\*                  |
| `helmGet`      | Get release details (chart, values, manifest) | `releaseName`\*, `namespace`\* |
| `helmHistory`  | Release revision history                      | `releaseName`\*, `namespace`\* |
| `helmRepoList` | List configured chart repositories            | —                              |

### Helm — Write Tools (5)

Disabled when `--read-only` is set.

| Tool            | Description                     | Key Parameters                                                   |
| --------------- | ------------------------------- | ---------------------------------------------------------------- |
| `helmInstall`   | Install a Helm chart            | `releaseName`\*, `chartName`\*, `namespace`, `repoURL`, `values` |
| `helmUpgrade`   | Upgrade an existing release     | `releaseName`\*, `chartName`\*, `namespace`, `repoURL`, `values` |
| `helmUninstall` | Uninstall a release             | `releaseName`\*, `namespace`\*                                   |
| `helmRollback`  | Rollback to a previous revision | `releaseName`\*, `namespace`\*, `revision`\*                     |
| `helmRepoAdd`   | Add a chart repository          | `repoName`\*, `repoURL`\*                                        |

\* = required parameter

### Resource Identifier Flexibility

The `kind` parameter across all tools accepts multiple formats. All of these resolve to the same resource:

| Format             | Example       |
| ------------------ | ------------- |
| PascalCase Kind    | `Deployment`  |
| Lowercase plural   | `deployments` |
| Lowercase singular | `deployment`  |
| Short name         | `deploy`      |

This works for any resource type, including CRDs.

## Development and Validation

| Path | Responsibility |
| --- | --- |
| [src/main.go](src/main.go) | Transport, authentication, tool registration, audit/output bounds |
| [src/tools](src/tools/) | MCP schemas |
| [src/handlers](src/handlers/) | Request handling and safe response serialization |
| [src/pkg](src/pkg/) | Kubernetes, Helm, and agent clients |
| [Makefile](Makefile) | Build and validation commands |
| [Helm chart](chart) | Root deployment contract |

From this directory:

```bash
make check
```

This runs formatting checks, vet, tests, and a binary build. `make lint` uses
`golangci-lint` when installed. Tests cover API/handler behavior, authentication,
redaction, bounds, and capability propagation using fixtures or fake services.
Use root `make validate-local` for executable source changes and
`make validate-cluster` for authorized cluster-facing acceptance.

### Adding a New Tool

1. **Define the schema** in `src/tools/k8s.go` (or `src/tools/helm.go`):

```go
func MyNewTool() mcp.Tool {
    return mcp.NewTool("myNewTool",
        mcp.WithDescription("What this tool does"),
        mcp.WithString("param", mcp.Required(), mcp.Description("...")),
    )
}
```

2. **Implement the client method** in `src/pkg/k8s/client.go` (or `src/pkg/helm/client.go`)

3. **Create the handler** in `src/handlers/k8s.go` (or `src/handlers/helm.go`)

4. **Register** in `src/main.go` through the audited wrapper:

```go
addAuditedTool(s, tools.MyNewTool(), "read", handlers.MyNewHandler(client))
```

Use the correct capability string for the tool, such as `read`, `read/logs`, `write`, `write/destructive`, `exec`, `kubectl`, or `agent`. Kubernetes handlers that return cluster data should use the existing `marshalSafe` path or `k8s.SanitizeText` so large responses and secrets are handled consistently.
