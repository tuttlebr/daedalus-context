# Kubernetes & Helm MCP Server

A [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) server for managing Kubernetes clusters and Helm releases through any MCP-compatible client. Built in Go for low resource overhead, it supports read-only operation by default in Kubernetes, in-cluster or kubeconfig-based authentication, explicit capability gates for dangerous tools, HTTP bearer-token auth, and an **autonomous AI-driven DevOps agent** via [opencode](https://github.com/anomalyco/opencode) integration.

## Table of Contents

- [Features](#features)
- [Architecture](#architecture)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [Operational Safety Model](#operational-safety-model)
- [DevOps Agent](#devops-agent)
- [Agent Skills](#agent-skills)
- [Tool Reference](#tool-reference)
- [Deployment](#deployment)
- [Security](#security)
- [Development](#development)
- [License](#license)

## Features

**Kubernetes Operations** — read, write, and explicitly gated dangerous tools covering the full lifecycle of cluster resources:

| Category                   | Capabilities                                                                                                                                       |
| -------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Discovery & Inspection** | List API resources, get/describe/export resources as JSON or YAML, diff resource states, trace ownership chains                                    |
| **Monitoring**             | Cluster health checks, top pods/nodes by CPU or memory, resource quotas, limit ranges                                                              |
| **Debugging**              | Pod debug info (conditions + events + logs), service endpoint health, network policy analysis, security context inspection, resource event history |
| **Mutations**              | Create/update resources from JSON or YAML, delete resources, scale workloads, rollout restarts, manifest dry-run validation, optional exec/kubectl |
| **DevOps Agent**           | Autonomous AI agent that inspects and, when explicitly permitted, manages cluster workloads using MCP tools, powered by any OpenAI-compatible LLM |
| **Navigation**             | List namespaces, list available kubeconfig contexts, rollout status                                                                                |

**Helm Operations** — 9 tools for chart lifecycle management:

| Category  | Capabilities                                                           |
| --------- | ---------------------------------------------------------------------- |
| **Read**  | List releases, get release details, release history, list repositories |
| **Write** | Install, upgrade, uninstall, rollback releases, add repositories       |

**Platform** — Flexible resource identifier resolution accepts PascalCase Kind (`Deployment`), lowercase plural (`deployments`), singular (`deployment`), or short names (`deploy`).

## Architecture

```
src/
├── main.go              # Server entry point, tool registration, transport setup
├── tools/
│   ├── k8s.go           # Kubernetes MCP tool definitions (schemas)
│   ├── helm.go          # Helm MCP tool definitions (schemas)
│   └── agent.go         # DevOps agent tool definition
├── handlers/
│   ├── k8s.go           # Kubernetes tool handlers (request → response)
│   ├── helm.go          # Helm tool handlers (request → response)
│   └── agent.go         # DevOps agent handler
└── pkg/
    ├── k8s/
    │   └── client.go    # Kubernetes client (dynamic, discovery, metrics, typed)
    ├── helm/
    │   └── client.go    # Helm client (install, upgrade, release management)
    └── agent/
        └── agent.go     # OpenCode agent orchestration (subprocess, config, parsing)
```

The server uses a **dynamic Kubernetes client** with a cached GVR (GroupVersionResource) resolver backed by the discovery API, so it works with any resource type — including CRDs — without hardcoded mappings.

## Quick Start

### Prerequisites

- Go 1.24+ (for building from source)
- Access to a Kubernetes cluster with a valid kubeconfig
- [metrics-server](https://github.com/kubernetes-sigs/metrics-server) installed for CPU/memory metrics tools

### Build from Source

```bash
cd src
go mod download
CGO_ENABLED=0 go build -ldflags="-w -s" -o k8s-mcp-server main.go
```

### Run Locally

```bash
# SSE transport (default)
./k8s-mcp-server

# Streamable HTTP transport — endpoint: http://localhost:8080/mcp
./k8s-mcp-server --mode streamable-http

# Stdio transport (for CLI/editor integrations)
./k8s-mcp-server --mode stdio

# Read-only mode (disables all write operations)
./k8s-mcp-server --read-only

# Disable Helm tools
./k8s-mcp-server --no-helm

# Combine flags
./k8s-mcp-server --mode sse --port 9090 --read-only --no-helm
```

### Container Image

```bash
# Build all locally maintained workspace images
make -C .. images-build

# Push and print the versioned deployment references
make -C .. images-publish
```

The workspace Compose file is an image specification. Deploy the published
image through the guarded root Kubernetes workflow described below.

## Configuration

### Command-Line Flags

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

Set `MCP_REQUIRE_AUTH=true` to fail startup when an HTTP transport is selected without `MCP_AUTH_TOKEN`. The default Kubernetes manifest uses this mode.

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

All HTTP modes expose local-only `GET /livez` and `/readyz`.
`GET /dependencyz` performs one bounded Kubernetes API request. `/healthz`
remains a local compatibility alias. Upstream slowness never turns liveness
into a restart loop.

## Operational Safety Model

This server is treated as an agent-facing platform component. The hardening work makes the operational contract explicit: default deployments are read-only, HTTP access can require authentication, dangerous capabilities are opt-in, outputs are sanitized, and side effects are auditable.

### Safe Defaults

- The Kubernetes deployment uses in-cluster ServiceAccount credentials instead of a mounted kubeconfig.
- The default manifest sets `SERVER_READ_ONLY=true`, so write tools and Helm mutations are not registered.
- The default manifest sets `MCP_REQUIRE_AUTH=true` and reads `MCP_AUTH_TOKEN` from the `k8s-mcp-auth` Secret.
- Health paths remain unauthenticated for probes; MCP HTTP traffic requires
  `Authorization: Bearer <token>` or `X-MCP-Token: <token>` when auth is set.

### Capability Gates

- Normal write tools require non-read-only mode.
- `execInPod` additionally requires `MCP_ENABLE_EXEC=true` or `--enable-exec`.
- `runKubectlCommand` additionally requires `MCP_ENABLE_KUBECTL=true` or `--enable-kubectl`.
- `devopsAgent` is inspection-only unless the parent server is write-enabled and `MCP_ENABLE_AGENT_WRITE=true`.
- Child opencode MCP sessions inherit `MCP_ENABLE_EXEC` and `MCP_ENABLE_KUBECTL` from the parent environment when those gates are set.
- `switchContext` is not registered because process-global kubeconfig context switching is unsafe for concurrent HTTP MCP usage.

### Audit Logging

Every registered tool goes through the audited registration wrapper in `main.go`. Tool calls log the tool name, capability class, status, duration, and error text when present. This gives operators a stable trail for read, write, destructive, exec, kubectl, log-reading, validation, Helm, and agent activity.

### Output Safety

- JSON tool responses pass through the shared sanitizer before returning to clients.
- Pod logs, resource YAML, Helm release output, GPU/operator logs, kubectl output, and agent output are redacted for secret-like values.
- Kubernetes Secret `data` and `stringData` values are redacted, including short base64 values.
- Sensitive key names such as passwords, tokens, authorization headers, certificates, and private keys are redacted recursively in structured output.
- Large responses are truncated before they are returned to the agent.

### Idempotent Apply Behavior

- `createResource` and `createResourceYAML` use Kubernetes server-side apply with field manager `k8s-mcp-server`.
- `createResourceYAML` can infer `kind` from the manifest when the caller omits it.
- Apply handlers validate `apiVersion`, `kind`, and metadata name before submitting side effects.
- Dynamic Kubernetes list calls use paginated API reads to avoid oversized list responses.

### Timeouts and Bounded Operations

- HTTP transports set read-header, read, idle, and graceful shutdown timeouts.
- Helm Kubernetes REST clients default to a 30-second timeout when no timeout is already configured.
- `runKubectlCommand` and `devopsAgent` use explicit per-call execution timeouts.

### Build Reproducibility

- The release Docker image bakes a curated DevOps-engineer skill set under `src/skills/` into `/home/appuser/.config/opencode/skills/`. The set is scoped to Kubernetes monitoring/remediation, shell scripting, and Python/Rust code work; see [Agent Skills](#agent-skills) for the full list.
- `src/.dockerignore` excludes build output, embedded `.git`, and `.DS_Store`. The `src/skills/` tree is intentionally not excluded so that the audited skill set ships with the image.
- Per-user or experimental skills can still be supplied without rebuilding by volume-mounting over `/home/appuser/.config/opencode/skills/` at runtime.

### Review Artifacts

The structured review deliverables live under `docs/review/`:

- `executive-summary.md` — readiness assessment and remaining risks.
- `risk-register.csv` — decision-ready findings with severity, evidence, owner, effort, and status.
- `architecture-themes.md` — cross-cutting design themes and platform standards.
- `remediation-roadmap.md` — immediate, near-term, and strategic work.
- `release-readiness-checklist.md` — verification gates before broader reliance.

## DevOps Agent

The `devopsAgent` tool launches an autonomous AI agent for Kubernetes cluster management. It uses [opencode](https://github.com/anomalyco/opencode) to run a headless agentic loop. By default the agent is inspection-only. If the parent server is not read-only and `MCP_ENABLE_AGENT_WRITE=true` is set, the agent can use write-capable k8s and Helm MCP tools to manage workloads autonomously, producing a structured report of actions taken and results.

### How It Works

1. You call `devopsAgent` with a natural language description of the task
2. The server spawns `opencode run` as a headless subprocess
3. OpenCode connects to a child k8s-mcp-server (stdio, `--no-agent`) for cluster access
4. The agent autonomously calls MCP tools to accomplish the task
5. Returns a structured report: objective, actions taken, current state, issues found, next steps

### Prerequisites

- [opencode](https://github.com/anomalyco/opencode) CLI installed (included in Docker image)
- An OpenAI-compatible LLM endpoint accessible from the server
- Environment variables set: `OPENCODE_BASE_URL`, `OPENCODE_API_KEY`, `OPENCODE_MODEL`

### Kubernetes Deployment

Create a Secret with your LLM provider credentials:

```bash
kubectl -n k8s-mcp-server create secret generic k8s-mcp-agent-config \
  --from-literal=base-url="http://llm.local:8080/v1" \
  --from-literal=api-key="sk-your-api-key" \
  --from-literal=model="private-llm/llama-3.1-70b" \
  --dry-run=client -o yaml | kubectl apply -f -
```

The deployment manifest references this Secret with `optional: true`, so the server works fine without it — the `devopsAgent` tool simply won't be registered.

### Workspace Image and Deployment

The workspace Compose file is an image specification, not a local deployment
path. Build the image from the workspace root, then deploy through the guarded
root Kubernetes workflow:

```bash
make -C .. images-build
COMPONENTS=k8s-mcp make -C .. deploy
```

### Tool Parameters

| Parameter  | Required | Default | Description                                                          |
| ---------- | -------- | ------- | -------------------------------------------------------------------- |
| `prompt`   | Yes      | —       | Natural language description of the task to perform                  |
| `namespace`| No       | —       | Namespace to focus the investigation on                              |
| `model`    | No       | env var | Override `OPENCODE_MODEL` for this run                               |
| `timeout`  | No       | `300`   | Max execution time in seconds (max: 900)                             |
| `readOnly` | No       | `false` | When true, restricts the agent to read-only inspection. Forced to true when the parent server runs with `--read-only` or `MCP_ENABLE_AGENT_WRITE` is not enabled |

## Agent Skills

[OpenCode skills](https://opencode.ai/docs/skills) are reusable instruction sets that extend the agent's behavior for specific domains — e.g., how to triage GPU workloads, how to interpret your team's alerting conventions, or domain-specific runbooks.

### Bundled Skill Set

The Docker image ships with a curated set of skills scoped to a DevOps-engineer workload: Kubernetes cluster monitoring and remediation, shell scripting, Python/Rust code work, and the NVIDIA NIM/RAG stack this server is typically deployed alongside. The bundled set covers:

- **Core DevOps / SRE** — `devops-engineer`, `kubernetes-specialist`, `sre-engineer`, `monitoring-expert`, `chaos-engineer`, `terraform-engineer`, `mcp-developer`
- **Languages** — `python-pro`, `rust-engineer`, `golang-pro`
- **Debugging / quality** — `debugging-wizard`, `code-reviewer`, `code-documenter`, `test-master`, `cli-developer`, `the-fool`
- **Security** — `secure-code-guardian`, `security-best-practices`, `security-reviewer`, `security-threat-model`
- **Data / RAG** — `database-optimizer`, `postgres-pro`, `sql-pro`, `pandas-pro`, `ml-pipeline`, `rag-architect`, `jupyter-notebook`, `prompt-engineer`
- **NVIDIA stack** — `nat-*` (NeMo Agent Toolkit: agent configuration, evaluation, installation, MCP+serving, optimization, path-checks, telemetry, tools/functions, user-rules, workflow-creation), `nvcf-ngc-cli-skill`, `nvcf-self-managed-cli`, `nvcf-self-managed-installation`, `nv-html`
- **Workflow / meta** — `gh-address-comments`, `gh-fix-ci`, `define-goal`, `skill-creator`, `skill-evolution`

The runtime image installs the toolchain those skills assume — `git`, `github-cli`, `python3`, `py3-yaml`, and `jq` — alongside the existing `helm`, `kubectl`, `bash`, and `curl`. Skill helper scripts (e.g., `skill-creator/scripts/quick_validate.py`, `gh-fix-ci/scripts/inspect_pr_checks.py`) run directly in the container without extra setup.

### Adding Skills

Each skill lives in its own subdirectory under `src/skills/` and requires a `SKILL.md` file with YAML frontmatter:

```
src/skills/
└── my-skill/
    └── SKILL.md
```

**`src/skills/my-skill/SKILL.md`**:

```markdown
---
name: my-skill
description: Brief description of what this skill teaches the agent
---

Your skill instructions here. Describe domain knowledge, investigation strategies,
or specialized runbooks that the agent should follow when relevant.
```

**Frontmatter fields:**

| Field         | Required | Description                                                        |
| ------------- | -------- | ------------------------------------------------------------------ |
| `name`        | Yes      | Lowercase letters/numbers/hyphens only; must match directory name  |
| `description` | Yes      | 1–1024 character description used by the agent to select the skill |
| `license`     | No       | Licensing information                                              |
| `metadata`    | No       | Custom string key-value pairs                                      |

The `name` must match its parent directory name exactly and follow the pattern `^[a-z0-9]+(-[a-z0-9]+)*$`.

### How Skills Are Loaded

**Container image / Kubernetes:** The standard image bakes the curated skill
set under `src/skills/` into `/home/appuser/.config/opencode/skills/` during
build. The set is audited, scoped to the DevOps-engineer role, and ships with
the runtime tools each skill assumes (see
[Bundled Skill Set](#bundled-skill-set)). Extend it through a reviewed image
release or Kubernetes volume configuration.

**Local development (without Docker):** Place skills in the global opencode config directory:

```
~/.config/opencode/skills/<name>/SKILL.md
```

## Tool Reference

### Kubernetes — Read-Only Tools (31)

#### Discovery & Core

| Tool                    | Description                                                 | Key Parameters                                          |
| ----------------------- | ----------------------------------------------------------- | ------------------------------------------------------- |
| `getAPIResources`       | List all available API resource types in the cluster        | `includeNamespaceScoped`, `includeClusterScoped`        |
| `listResources`         | List resources of a specific type, optionally filtered      | `kind`\*, `namespace`, `labelSelector`, `fieldSelector` |
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
| `getClusterSummary` | Concise cluster status overview                      | `includeNamespaceDetails`         |
| `getTopPods`        | Top pods by CPU or memory usage                      | `namespace`, `sortBy`, `limit`    |
| `getTopNodes`       | Top nodes by resource utilization                    | `sortBy`, `includeConditions`     |
| `getNodeMetrics`    | CPU/memory metrics for a specific node               | `name`\*                          |
| `getPodMetrics`     | CPU/memory metrics for a specific pod                | `namespace`\*, `podName`\*        |
| `getResourceQuotas` | Resource quotas and usage                            | `namespace`, `showPercentage`     |
| `getLimitRanges`    | Limit ranges in namespaces                           | `namespace`                       |
| `getEvents`         | Cluster events, optionally filtered                  | `namespace`, `labelSelector`      |
| `getRolloutStatus`  | Rollout status for Deployment/StatefulSet/DaemonSet  | `kind`\*, `name`\*, `namespace`\* |

#### Debugging & Troubleshooting

| Tool                  | Description                                        | Key Parameters                                     |
| --------------------- | -------------------------------------------------- | -------------------------------------------------- |
| `getPodsLogs`         | Pod logs (single or all containers)                | `name`\*, `namespace`\*, `containerName`           |
| `getPodDebugInfo`     | Comprehensive pod debug (conditions, events, logs) | `name`\*, `namespace`\*, `includeLogs`, `logLines` |
| `getServiceEndpoints` | Service endpoints with pod health status           | `name`\*, `namespace`\*, `checkHealth`             |
| `getNetworkPolicies`  | Network policies affecting a namespace or pod      | `namespace`\*, `podName`, `includeDetails`         |
| `getSecurityContext`  | Pod and container security contexts                | `name`\*, `namespace`\*, `includeDefaults`         |
| `getResourceHistory`  | Recent events and changes for a resource           | `kind`\*, `name`\*, `namespace`, `hours`           |
| `validateManifest`    | Dry-run validation of YAML/JSON manifests          | `manifest`\*, `format`, `strict`                   |

#### GPU Debugging

| Tool                     | Description                                      | Key Parameters                                      |
| ------------------------ | ------------------------------------------------ | --------------------------------------------------- |
| `getGPUClusterOverview`  | Cluster-wide GPU resource and workload overview  | `includeNonGPUNodes`, `includeEvents`               |
| `diagnoseGPUScheduling`  | Diagnose GPU scheduling for a specific pod       | `podName`\*, `namespace`\*                          |
| `getGPUOperatorHealth`   | NVIDIA operator and device plugin health check   | `devicePluginNamespace`, `gpuOperatorNamespace`     |

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

`switchContext` is intentionally not registered. Process-global context switching is unsafe for concurrent HTTP MCP usage; run separate server instances or use a request/session-scoped multi-cluster model instead.

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

## Deployment

### Kubernetes In-Cluster

The included manifest at `deploy/k8s-mcp-server.yaml` provides a safe-by-default in-cluster deployment. It uses the pod ServiceAccount for Kubernetes access, runs in read-only mode, and requires an MCP bearer token for HTTP MCP requests.

```bash
# Create the namespace first so the auth secret can be created.
kubectl create namespace k8s-mcp-server --dry-run=client -o yaml | kubectl apply -f -

# Create the HTTP MCP auth token secret.
kubectl -n k8s-mcp-server create secret generic k8s-mcp-auth \
  --from-literal=token="$(openssl rand -hex 32)" \
  --dry-run=client -o yaml | kubectl apply -f -

# Apply RBAC, Deployment, PDB, NetworkPolicy, and Service.
kubectl apply -f deploy/k8s-mcp-server.yaml
```

The manifest includes:

- Dedicated `k8s-mcp-server` namespace
- ServiceAccount with an observer ClusterRole that excludes Secret data
- Two-replica Deployment with hard hostname spread, PDB, and local probes
- `SERVER_READ_ONLY=true`, `SERVER_NO_HELM=true`, and `MCP_REQUIRE_AUTH=true`
- Hardened security context (non-root, read-only root filesystem, all capabilities dropped)
- Resource requests of 500m CPU/512Mi and limits of 4 CPU/8Gi
- Authenticated fixed NodePort on `30080`, with Cilium source-range enforcement
  restricted to `MCP_EXTERNAL_CLIENT_CIDR` (default `192.168.1.0/24`)

#### Versioned Deploy

```bash
./deploy.sh
```

The script requires `K8S_MCP_IMAGE=repository:MAJOR.MINOR.PATCH`, applies the
manifest, and synchronizes current/next auth plus optional agent configuration.
Build and publish are separate actions. Content checksums and the image version
trigger a rollout only when effective state changes.

The deploy wrapper refuses a non-dry-run deployment unless the cluster Cilium
release has `bpf.lbSourceRangeAllTypes=true`; this is required for the
Service's `loadBalancerSourceRanges` allow-list to apply to NodePort traffic.
Because Kubernetes accepts that field only for `LoadBalancer` Services, the
manifest uses the unclaimed class `daedalus.ai/nodeport-only` and retains the
fixed allocated NodePort. MetalLB therefore does not advertise a separate
address.

### MCP Client Configuration

For MCP clients that connect via streamable HTTP (e.g., in-cluster agents):

```
http://k8s-mcp-server.k8s-mcp-server.svc.cluster.local:8080/mcp
```

Include the auth token as `Authorization: Bearer <token>` or `X-MCP-Token: <token>` for HTTP transports.

#### Cursor / VS Code

Add to your editor's MCP settings (`settings.json`):

```json
{
  "mcp.mcpServers": {
    "k8s-mcp-server": {
      "command": "k8s-mcp-server",
      "args": ["--mode", "stdio"],
      "env": {
        "KUBECONFIG": "${env:HOME}/.kube/config"
      }
    }
  }
}
```

For read-only mode, add `"--read-only"` to the `args` array.

## Security

### Container Hardening

The Docker image runs with a minimal attack surface:

- **Non-root user** (`appuser`, UID 1001)
- **Read-only root filesystem**
- **All capabilities dropped**
- **Minimal base image** — Alpine 3.21 with `helm` v3.17.3 (SHA256-pinned), `kubectl` v1.34.1 (SHA256-pinned), the opencode/Bun runtime, and the DevOps-skill toolchain (`git`, `github-cli`, `python3`, `py3-yaml`, `jq`, `bash`, `curl`)
- **No privilege escalation** allowed

### Read-Only Mode

Use `--read-only` or `SERVER_READ_ONLY=true` to prevent cluster state changes through this server. This disables Kubernetes and Helm write tools and forces `devopsAgent` into inspection-only mode. `execInPod` and `runKubectlCommand` are additionally disabled unless their explicit capability gates are enabled.

The default Kubernetes manifest sets `SERVER_READ_ONLY=true` and uses read-only ServiceAccount RBAC.

### RBAC

The included ClusterRole (`k8s-mcp-server-readonly`) grants read access to standard Kubernetes API groups:

- **Core** — pods, services, configmaps, secrets, namespaces, nodes, events, endpoints, PVs, PVCs, resource quotas, limit ranges, service accounts
- **apps** — deployments, replicasets, statefulsets, daemonsets
- **batch** — jobs, cronjobs
- **networking.k8s.io** — ingresses, network policies, ingress classes
- **storage.k8s.io** — storage classes, volume attachments
- **rbac.authorization.k8s.io** — roles, rolebindings, clusterroles, clusterrolebindings
- **autoscaling** — horizontal pod autoscalers
- **policy** — pod disruption budgets
- **metrics.k8s.io** — pod and node metrics

For write operations, create a separate ClusterRole with the necessary verbs and bind it to the ServiceAccount.

## Development

### Project Structure

```
├── .github/workflows/       # CI pipeline (build, test, vet, lint)
├── deploy/                  # Kubernetes deployment manifests
├── docs/review/             # Review findings, roadmap, and release gates
├── scripts/                 # Git hooks
├── src/
│   ├── main.go              # Entry point, tool registration
│   ├── Dockerfile           # Multi-stage build
│   ├── .dockerignore        # Reproducible Docker build exclusions
│   ├── .golangci.yml        # Linter configuration
│   ├── tools/               # MCP tool schema definitions
│   ├── handlers/            # Tool request handlers
│   └── pkg/                 # Client libraries (k8s, helm, agent)
├── ../docker-compose.yaml   # Workspace image build specification
├── Makefile                 # Build, test, lint, format, deploy
├── deploy.sh                # Build + deploy script
└── redeploy.sh              # Backward-compatible wrapper for deploy.sh
```

### Makefile Targets

| Target      | Description                                            |
|-------------|--------------------------------------------------------|
| `make build`      | Build the binary                                 |
| `make test`       | Run all unit tests                               |
| `make vet`        | Run `go vet` static analysis                     |
| `make fmt`        | Auto-format all Go source files with `gofmt`     |
| `make fmt-check`  | Check formatting without modifying (used in CI)  |
| `make lint`       | Run `golangci-lint` (skips if not installed)      |
| `make check`      | Run all checks: format, vet, test, build         |
| `make docker`     | Build the Docker image                           |
| `make deploy`     | Full rebuild + deploy via `deploy.sh`            |
| `make clean`      | Remove build artifacts                           |
| `make hooks`      | Install the git pre-commit hook                  |

### Getting Started

```bash
# Install the pre-commit hook (runs fmt-check, vet, and tests before each commit)
make hooks

# Run the full check suite locally (same checks as CI)
make check
```

### Verification

The hardening pass was verified with:

- `make test`
- `make check`
- `make -C .. images-validate`
- `kubectl apply --dry-run=client -f deploy/k8s-mcp-server.yaml`
- `make -C .. images-build`

Test coverage was added for auth environment parsing, HTTP auth decisions, recursive output sanitization, safe handler marshaling, and agent capability-gate propagation.

### Adding a New Tool

1. **Define the schema** in `tools/k8s.go` (or `tools/helm.go`):

```go
func MyNewTool() mcp.Tool {
    return mcp.NewTool("myNewTool",
        mcp.WithDescription("What this tool does"),
        mcp.WithString("param", mcp.Required(), mcp.Description("...")),
    )
}
```

2. **Implement the client method** in `pkg/k8s/client.go` (or `pkg/helm/client.go`)

3. **Create the handler** in `handlers/k8s.go` (or `handlers/helm.go`)

4. **Register** in `main.go` through the audited wrapper:

```go
addAuditedTool(s, tools.MyNewTool(), "read", handlers.MyNewHandler(client))
```

Use the correct capability string for the tool, such as `read`, `read/logs`, `write`, `write/destructive`, `exec`, `kubectl`, or `agent`. Kubernetes handlers that return cluster data should use the existing `marshalSafe` path or `k8s.SanitizeText` so large responses and secrets are handled consistently.

### Key Dependencies

| Package                       | Version | Purpose                     |
| ----------------------------- | ------- | --------------------------- |
| `github.com/mark3labs/mcp-go` | v0.41.1 | MCP protocol implementation |
| `helm.sh/helm/v3`             | v3.19.0 | Helm client library         |
| `k8s.io/client-go`            | v0.34.1 | Kubernetes client           |
| `k8s.io/metrics`              | v0.34.1 | Metrics API client          |

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
