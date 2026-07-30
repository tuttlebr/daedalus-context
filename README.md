# Daedalus Context Operations

This workspace deploys the seven support services used by
[daedalus-agent](https://github.com/tuttlebr/daedalus-agent). Use the root
`Makefile` for normal Kubernetes operations. It applies the shared deployment
contract, preserves the existing namespace and PVC boundaries, and coordinates
dependencies without turning an optional service into a stack-wide failure.

## Stack Model

| Component | Owner | Namespace | Role |
| --- | --- | --- | --- |
| `milvus` | `nim-milvus-ms/` | `milvus` | Vector database and MinIO object storage |
| `retriever` | `nemotron-retriever/` | `daedalus-retriever` | Embedding and optional reranking inference |
| `nv-ingest` | `nv-ingest/` | `nv-ingest` | Document extraction, chunking, and storage |
| `pdf-server` | `pdf-server/` | `milvus` | Versioned, read-only source PDF corpus |
| `url-ingest` | `url-ingest/` | `milvus` | Scheduled and manual shared-collection population |
| `k8s-mcp` | `k8s-multicluster-mcp/` | `k8s-mcp-server` | Authenticated Kubernetes operations tools |
| `unifi-mcp` | `unifi-network-mcp/` | `unifi-network-mcp-server` | Authenticated, read-only UniFi tools |

The deployment graph is declared in `stack/topology.yaml`:

```text
milvus ─────┐
            ├──> nv-ingest ──┐
retriever ──┘                 │
                              ├──> url-ingest
pdf-server ───────────────────┘

k8s-mcp    (independent)
unifi-mcp  (independent)
```

The orchestrator deploys independent roots concurrently, then deploys
`nv-ingest` and `url-ingest` when their selected dependencies succeed. A failed
component blocks only its dependents; unrelated branches continue and the final
JSON report identifies every succeeded, failed, or blocked component.

This separation is also the runtime failure policy:

- Core chat does not require context services.
- Existing document search depends on Milvus and embedding inference, not on
  the ingestion scheduler.
- Reranking is optional and fails open.
- A failed NV-Ingest or URL refresh affects new content, not already indexed
  content.
- Kubernetes and UniFi MCP failures do not take retrieval or core chat down.

## Unified Local Docker Compose

The root `docker-compose.yaml` is the only Compose specification. It contains
the locally supported Kubernetes MCP, UniFi MCP, PDF server, URL crawler, and
NV-Ingest task containers. Component directories remain their build contexts
and data-mount owners; they no longer contain independent Compose files.

Start the three long-running local serving containers:

```bash
make compose-up
```

The default host ports are PDF `8080`, Kubernetes MCP `8081`, and UniFi MCP
`8082`. Override them in the untracked root `.env`. The Make targets flatten
the active kubeconfig into a mode-0700 per-user runtime directory under `/tmp`
with group-only file access for the container; they do not change the
permissions of the source kubeconfig. Set `KUBECONFIG_SOURCE` to use a file
other than `$KUBECONFIG` or `~/.kube/config`.

All five Compose images use
`daedalus.ddns.me:5050/btuttle/<service>:0.1.0` by default. Set
`DAEDALUS_CONTEXT_IMAGE_VERSION` in `.env` when publishing a new workspace
release:

```bash
make compose-publish
```

`compose-publish` builds and pushes Kubernetes MCP, UniFi MCP, PDF server, URL
crawler, and the NV-Ingest task runner. Its final output contains
`K8S_MCP_IMAGE`, `UNIFI_MCP_IMAGE`, `PDF_SERVER_IMAGE`, `URL_INGEST_IMAGE`, and
`URL_INGEST_RUNNER_IMAGE` as SemVer-tagged assignments. Use
`make compose-release-refs` to print the configured deployment references
without rebuilding.

The crawler and NV-Ingest task runner are batch/data-mutating services behind
the `ingest` profile. Run them explicitly:

```bash
make compose-crawl
make compose-ingest
```

`make compose-config` validates all five service specifications, including the
profiled jobs. The Compose project is a local workflow for the containers that
already supported Compose; it does not emulate the Kubernetes-only Milvus,
retriever, or distributed NV-Ingest control planes.

## Deployment Rules

- Run routine deploys from this workspace root. Child `deploy.sh` scripts are
  implementation details and scoped recovery tools, not an alternative
  seven-step deployment order.
- Build and publish images before deployment. `make deploy` and `make update`
  reconcile explicit SemVer-tagged releases; they do not silently build or
  push images. Published version tags must not be overwritten.
- Keep the existing independent namespaces. Do not set `STACK_NAMESPACE` for a
  routine deployment: changing namespace placement does not migrate PVC data,
  Secrets, or collections.
- Keep data mutations separate. Deploying services never publishes a corpus or
  launches ingestion Jobs.
- Set `EXPECTED_CONTEXT` to the approved literal kubectl context for every
  cluster operation. Do not derive it from the current context in the same
  command, because that defeats the guard.
- Never commit `.env`, kubeconfigs, tokens, TLS keys, API keys, or model data.

## Prerequisites

The operator needs:

- `bash`, GNU Make, Python 3, `jq`, `kubectl`, Helm, and Docker;
- access to the intended Kubernetes cluster and container registries;
- NGC/NVCR and Hugging Face credentials required by the pinned releases;
- `nvcr-imagepullsecret` in the source namespace used by the deployment
  wrappers;
- a reviewed storage class for MinIO bulk data; and
- NVIDIA GPU/operator capacity for the retriever and NV-Ingest workloads.

`make validate-cluster` performs read-only capacity and manifest checks. It
does not replace review of storage failure domains, backup policy, or the
production acceptance criteria in `stack/environments/production.yaml`.

## First Aligned Deployment

### 1. Configure the release

Start at the workspace root and create the untracked environment file:

```bash
cd /volume2/daedalus/datasets/daedalus-context
test -e .env || cp .env.example .env
${EDITOR:-vi} .env
```

At minimum, replace every example or empty production value for:

| Area | Required configuration |
| --- | --- |
| Storage | `MINIO_STORAGE_CLASS` and a supported shared `IMAGE_STORAGE_URI` |
| Provider images | `DAEDALUS_CONTEXT_IMAGE_VERSION` and the five derived provider image references, using an already-published SemVer tag |
| Provider auth | Distinct `K8S_MCP_AUTH_TOKEN` and `UNIFI_MCP_AUTH_TOKEN`; leave the corresponding `*_NEXT` value empty until rotation |
| External MCP access | Stable authenticated NodePorts `30080` and `30081`, restricted by `MCP_EXTERNAL_CLIENT_CIDR` (default `192.168.1.0/24`) |
| UniFi | `UNIFI_BASE_URL`, exact `UNIFI_CONTROLLER_CIDR`, `UNIFI_API_KEY`, and TLS verification policy |
| NVIDIA/model access | `NGC_API_KEY`, `NVIDIA_API_KEY`, and `HF_ACCESS_TOKEN` as required |
| Stateful credentials | Strong MinIO and Milvus credentials, or pre-provisioned authoritative Kubernetes Secrets |
| Ingestion | Source paths/node, required collections, bucket, and job timing appropriate for the cluster |

Each child wrapper reads the root `.env` and then its own `.env`, if present.
Explicit exported values take final precedence. Prefer the root file for shared
settings and use a child file only for an intentional component-specific
override.

The NodePort allow-list requires Cilium to have
`bpf.lbSourceRangeAllTypes=true`. Both MCP deployment wrappers fail closed when
that setting is absent so `loadBalancerSourceRanges` cannot appear configured
while remaining unenforced. Enable it with the exact installed Cilium chart
version before the first MCP deployment:

```bash
CILIUM_VERSION="$(
  helm list -n kube-system --filter '^cilium$' -o json |
    jq -r '.[0].chart | sub("^cilium-"; "")'
)"
helm upgrade cilium cilium/cilium \
  --namespace kube-system \
  --version "${CILIUM_VERSION}" \
  --reuse-values \
  --set bpf.lbSourceRangeAllTypes=true \
  --atomic --wait
```

Kubernetes permits `loadBalancerSourceRanges` only on a Service whose type is
`LoadBalancer`. The two MCP Services therefore use the intentionally unclaimed
class `daedalus.ai/nodeport-only` with NodePort allocation enabled. MetalLB does
not allocate or advertise an address for that class; external clients still use
the fixed node-address endpoints on `30080` and `30081`.

The root Makefile also evaluates settings such as `MINIO_STORAGE_CLASS` before
it invokes a child wrapper. Export the root file into the operator shell before
using Make:

```bash
set -a
source .env
set +a

export ENVIRONMENT=production
export EXPECTED_CONTEXT=kubernetes-admin@kubernetes
```

Replace the example context with the approved context for the target cluster.

### 2. Resolve versioned release and ownership gates

`stack/components.lock.yaml` is the release inventory. Before a production
rollout:

1. Replace every pending `sourceRevision` with the reviewed source commit.
2. Build and publish the Kubernetes MCP, UniFi MCP, and URL-ingest runner
   images with the selected `DAEDALUS_CONTEXT_IMAGE_VERSION`.
3. Record the reviewed SemVer versions for Milvus, NV-Ingest, and retriever
   images in the release inventory.
4. Keep the already pinned retriever models, model revisions, configuration
   hashes, and serving contracts aligned with `stack/rag-schema.yaml`.
5. Record the approved MinIO and document-artifact storage decisions in
   `stack/environments/production.yaml`.

Run the contract validator:

```bash
scripts/contextctl validate --environment "$ENVIRONMENT"
```

`Contract validation passed` means the YAML contracts agree with one another.
It is not release approval if any following line begins with `BLOCKED:`. Treat
pending source revisions as a production release blocker.

Production HA qualification is a separate, explicit gate:

```bash
make validate-ha ENVIRONMENT="$ENVIRONMENT"
```

This intentionally fails until owners approve numeric node-loss, failover,
RPO, and RTO objectives and resolve the storage decisions. An availability
rollout may be performed under an approved phased plan, but it must be reported
as `redundancy: at-risk`; do not describe it as HA-qualified.

### 3. Validate source and target cluster

Review the effective non-secret configuration, then run the complete
mutation-free validation:

```bash
make show-config \
  ENVIRONMENT="$ENVIRONMENT" \
  EXPECTED_CONTEXT="$EXPECTED_CONTEXT"

make validate-cluster \
  ENVIRONMENT="$ENVIRONMENT" \
  EXPECTED_CONTEXT="$EXPECTED_CONTEXT"
```

`validate-cluster` includes `validate-local`, unit tests, Go builds, shell and
Python syntax checks, Helm lint/template checks, client-side Kubernetes
dry-runs, the URL-ingest container check target, and a read-only cluster
capacity check. Fix failures before deployment.

### 4. Deploy all seven services

All seven allowlisted components are selected by default:

```bash
make deploy \
  ENVIRONMENT="$ENVIRONMENT" \
  EXPECTED_CONTEXT="$EXPECTED_CONTEXT"
```

The root orchestrator follows the dependency graph and emits a final operation
report. Success requires every selected component to report `succeeded`.
Deployment is idempotent and does not force restarts when effective state is
unchanged.

Immediately verify provider workloads:

```bash
make health-provider \
  ENVIRONMENT="$ENVIRONMENT" \
  EXPECTED_CONTEXT="$EXPECTED_CONTEXT"
```

On a fresh cluster, dependency health may still fail because the required
Milvus collections have not been populated. That is expected only until the
data bootstrap in the next step completes.

### 5. Publish and populate shared data

Data operations are explicit and synchronous:

```bash
# Fresh bootstrap: publish PDFs, stage URL inputs, ingest all collections,
# wait for every Job, and verify non-empty Milvus collections.
make seed-data \
  CORPUS_GENERATION=release-2026-07-29 \
  EXPECTED_CONTEXT="$EXPECTED_CONTEXT"
```

The supported alternatives are:

```bash
# Publish a verified PDF generation and atomically switch its current pointer.
make publish-corpus \
  CORPUS_GENERATION=release-2026-07-29 \
  EXPECTED_CONTEXT="$EXPECTED_CONTEXT"

# Publish PDFs and stage URL inputs without submitting documents.
make stage-data \
  CORPUS_GENERATION=release-2026-07-29 \
  EXPECTED_CONTEXT="$EXPECTED_CONTEXT"

# Resume from the staged snapshot; exact successful files are skipped.
make run-ingest \
  COLLECTIONS="nvidia kubernetes" \
  EXPECTED_CONTEXT="$EXPECTED_CONTEXT"
```

`pdf-server` is the sole owner of PDF Kubernetes resources and corpus
publication. URL-ingest consumes the published generation and owns its
CronJobs, manifests, and ingestion Jobs. Scheduled and manual ingestion share
a global capacity lock, and normal seed/resume operations never drop a Milvus
collection.

For a cluster with existing verified collections, do not seed merely because
services were redeployed. Use `make health-dependencies` to confirm the
persisted data instead.

### 6. Configure and deploy Daedalus Agent

Render the versioned, non-secret consumer contract:

```bash
make render-agent-env ENVIRONMENT="$ENVIRONMENT"
```

Merge that output into the Daedalus Agent deployment through its approved
configuration path. Add secrets separately:

- `KUBERNETES_MCP_TOKEN` must match the current or next Kubernetes MCP token.
- `UNIFI_MCP_TOKEN` must match the current or next UniFi MCP token.
- Milvus and document-object credentials must be synchronized into the
  Daedalus namespace through the approved Secret-management path.

Do not edit `stack/generated/production-daedalus-context.env` by hand. It is
generated from the topology/environment contracts, and `validate-local` fails
if it becomes stale.

Deploy Daedalus Agent from its own repository after the support endpoints and
credentials are configured.

### 7. Verify the complete path

Run the combined health report after Daedalus is ready:

```bash
make preflight EXPECTED_CONTEXT="$EXPECTED_CONTEXT"
scripts/contextctl health-stack \
  --environment "$ENVIRONMENT" \
  --components milvus retriever nv-ingest pdf-server url-ingest k8s-mcp unifi-mcp
```

The report separates three concerns:

- `availability`: required service/data-path failures make the stack
  `unavailable`; optional-tool failures make it `degraded`.
- `freshness`: stale URL-ingest state is reported independently from online
  query availability.
- `redundancy`: remains `at-risk` until the production HA acceptance gate is
  approved and met.

The direct command exits `0` for healthy, `1` for required availability loss,
and `2` for degraded, stale, or at-risk. Run the guarded `make preflight`
immediately before it because the direct health command itself is read-only and
does not apply the Make context guard. `make health-stack` prints the same JSON,
but GNU Make collapses a nonzero recipe exit; use `contextctl` when automation
must distinguish `1` from `2`.

Layer-specific diagnostics are also available:

```bash
make health-provider       # Workloads, rollouts, probes, and PDB-facing state
make health-dependencies   # Upstreams, PVCs, collections, and freshness
make health-integration    # Requests from the configured Daedalus workload
```

Set `CHECK_CONSUMER=false` only when Daedalus has not been deployed yet and the
requested target supports that mode. A healthy provider check alone does not
prove that Daedalus can reach or authenticate to the service.

## Updates, Restarts, and Partial Operations

Use:

```bash
make update                 # Reconcile already-published versioned releases
make redeploy               # Explicitly restart selected long-running workloads
make health-stack           # Recheck the complete selected path
```

Neither operation deletes PVCs or uninstalls Helm releases. `redeploy` is for a
deliberate restart; it is not the default way to apply configuration.

Limit an operation with the allowlisted `COMPONENTS` variable:

```bash
COMPONENTS="k8s-mcp unifi-mcp" make update
COMPONENTS="milvus retriever nv-ingest" make deploy
```

Dependencies are ordered only when they are selected. When operating on
`nv-ingest` or `url-ingest` alone, either include its dependencies as shown in
`stack/topology.yaml` or first prove that the existing dependencies are
healthy.

## Zero-Downtime MCP Token Rotation

Each MCP provider accepts a current and next token to permit overlap:

1. Keep the old token in `K8S_MCP_AUTH_TOKEN` or
   `UNIFI_MCP_AUTH_TOKEN`.
2. Put the new token in the matching `*_AUTH_TOKEN_NEXT` value.
3. Reconcile that provider with `make update`.
4. Change the Daedalus consumer token and verify `make health-integration`.
5. Promote the new token to the current value, clear `*_NEXT`, and reconcile
   the provider again.

Use distinct tokens for the two providers. Never copy one provider's token to
the other or expose provider tokens in generated non-secret environment files.

## Recovery and Durable State

Treat these as durable:

- Milvus, etcd, and MinIO PVCs;
- the PDF generation PVC;
- URL download and ingestion-manifest PVCs; and
- Daedalus application and Redis PVCs.

Do not delete PVCs as a cleanup shortcut.

| Situation | Correct action |
| --- | --- |
| A routine release failed | Read the operation JSON, repair the failed component, then rerun the same root target; independent successes remain intact |
| Collection Job failed or timed out | Rerun `make run-ingest COLLECTIONS="<name>"`; exact successful files remain recorded |
| Source files changed | Run `make stage-data`, then rerun only affected collections |
| PDFs changed | Run `make publish-corpus` with a new generation, then rerun affected collections |
| Entire collection must be rebuilt | Back up Milvus, drain ingestion, review the exact collection and manifest, then use the component recovery procedure; normal seed mode is non-destructive |
| Milvus metadata must be reset | Use `nim-milvus-ms/reset-etcd.sh` only with its explicit confirmation after backup review; MinIO is preserved |
| Daedalus cannot list Kubernetes tools | Check the `k8s-mcp-server` Deployment, Service, current/next token, generated URL, and consumer-side contract probe |
| Existing retrieval fails | Check Milvus collection counts and retriever adapter health before involving NV-Ingest |
| New document ingestion fails | Check NV-Ingest, Redis, MinIO, Milvus, and retriever dependencies without taking existing retrieval offline |

## Component-Level Development

Use nested project instructions and validation when changing an individual
component:

```bash
cd k8s-multicluster-mcp && make check
cd nv-ingest && PYTHONPATH=src:api/src:client/src pytest
cd url-ingest && cargo test && cargo clippy -- -D warnings
cd unifi-network-mcp && make test
```

Direct child deployment commands remain useful for focused debugging, render
inspection, and reviewed recovery. Return to the root workflow for a release so
the context guard, shared endpoints, dependency graph, and health model remain
consistent.

## Repository Hygiene

The workspace root may be an initialized Git worktree with no baseline commit,
while several child directories have their own histories. Check both scopes
before staging and never assume a root commit includes nested-repository
changes:

```bash
git status --short
git -C k8s-multicluster-mcp status --short
git -C nv-ingest status --short
git -C url-ingest status --short
```

Keep commits focused and do not add generated datasets, downloaded URLs,
caches, `.env` files, kubeconfigs, or credentials.
