# Daedalus Context Operations

This repository builds and operates the Kubernetes services that provide
retrieval, ingestion, and infrastructure tools to
[daedalus-agent](https://github.com/tuttlebr/daedalus-agent).

Kubernetes is the only supported deployment runtime. Use the root `Makefile`
for deployment, updates, health checks, and data operations. The root
`docker-compose.yaml` is an image specification used to validate, build, and
publish the five locally maintained images. It is not a second deployment
path.

## Start Here

Run commands from the repository root. The normal first deployment is:

```bash
cd /volume2/daedalus/datasets/daedalus-context

test -e .env || cp .env.example .env
${EDITOR:-vi} .env

make verify-shared-secrets
make deploy
```

Set `ENVIRONMENT` and the approved literal `EXPECTED_CONTEXT` in `.env` before
running the command. Do not populate `EXPECTED_CONTEXT` by reading the current
context. That would defeat the wrong-cluster guard. `make deploy` loads `.env`,
runs doctor and non-secret configuration checks, validates the cluster,
reconciles all eight Helm releases in `daedalus`, and checks provider health.
For the one-time conversion from the legacy namespaces, run
`make migrate-legacy-secrets` before the verification command. It copies only
the required Secret data and refuses to overwrite a differing destination.
If Hindsight already exists in `daedalus-hindsight`, run `make
migrate-hindsight` before the first full deployment. That guarded workflow
copies its Secret and PostgreSQL data, compares source and target fingerprints,
and leaves the old database/PVC available for rollback.

After the providers are ready:

```bash
# Existing cluster: prove that persisted collections are still healthy.
make health-dependencies

# Fresh cluster only: publish, ingest, wait, and verify all required data.
make seed-data CORPUS_GENERATION=release-2026-08-09
```

Do not seed an existing cluster merely because services were redeployed.

Render the Daedalus consumer configuration, deploy Daedalus from its own
repository, and then verify the complete path:

```bash
make render-agent-env
make health-stack
```

`make help` is the command index. Every target includes a side-effect label.

## Choose a Task

| Goal | Command | Effect |
| --- | --- | --- |
| Check workstation, configuration, context, and cluster access | `make doctor` | Read-only cluster access |
| Inspect effective non-secret settings | `make show-config` | Read-only local output |
| Validate the image specification | `make images-validate` | Read-only local validation |
| Build the five local images | `make images-build` | Writes local image state |
| Build and publish a versioned image release | `make images-publish` | Writes local image and remote registry state |
| Run source tests and renders | `make validate-local` | Read-only validation plus disposable build output |
| Validate all eight chart contracts | `make validate-helm` | Read-only local Helm lint and render |
| Validate manifests and cluster capacity | `make validate-cluster` | Read-only cluster access and client dry-runs |
| Validate, reconcile, and check all selected services | `make deploy` | Reads `.env` and writes Kubernetes resources |
| Reconcile an already-published update | `make update` | Writes Kubernetes resources |
| Restart selected long-running workloads | `make redeploy` | Restarts pods; does not delete PVCs |
| Check provider workloads | `make health-provider` | Read-only cluster access |
| Check dependencies and data freshness | `make health-dependencies` | Read-only cluster access |
| Probe providers from Daedalus | `make health-integration` | Read-only cluster access |
| Publish or ingest data | `make seed-data` or `make run-ingest` | Writes corpus, Jobs, and collection data |
| Remove an accepted legacy stack | `make cleanup-legacy` | Destructively removes only the documented legacy targets |

Every command that reads or writes Kubernetes requires `EXPECTED_CONTEXT` and
refuses to continue when it does not match the active context.

## Stack Model

The dependency graph is declared in `stack/topology.yaml`.

| Component | Owning directory | Namespace | Role |
| --- | --- | --- | --- |
| `hindsight` | `../daedalus-hindsight/charts/daedalus-hindsight/` | `daedalus` | Durable memory API, control plane, and PostgreSQL |
| `milvus` | `helm/milvus/` | `daedalus` | Vector database and MinIO object storage |
| `retriever` | `nemotron-retriever/` | `daedalus` | Embedding and optional reranking inference |
| `nv-ingest` | `helm/nv-ingest/` | `daedalus` | Document extraction, chunking, and storage |
| `pdf-server` | `helm/pdf-server/` | `daedalus` | Versioned, read-only source PDF corpus |
| `url-ingest` | `helm/url-ingest/` | `daedalus` | Scheduled and manual collection population |
| `k8s-mcp` | `helm/k8s-mcp-server/` | `daedalus` | Authenticated Kubernetes operations tools |
| `unifi-mcp` | `helm/unifi-network-mcp-server/` | `daedalus` | Authenticated UniFi read and mutation tools |

```text
hindsight   (independent durable-memory service)

milvus -----+
             +--> nv-ingest --+
retriever --+                 |
                               +--> url-ingest
pdf-server -------------------+

k8s-mcp    (independent)
unifi-mcp  (independent)
```

The root orchestrator deploys releases serially in dependency order. It starts
a dependent component only after its selected dependencies succeed. A failed
component blocks only its dependents. The final JSON report identifies every
`succeeded`, `failed`, and `blockedBy` component.

The runtime failure policy follows the same boundaries:

- Core chat does not require this repository's services.
- Existing document search requires Milvus and embedding inference. It does
  not require the ingestion scheduler.
- Reranking is optional and fails open.
- Failed ingestion affects new content, not already indexed content.
- Kubernetes and UniFi MCP failures do not take retrieval down.

## Configuration

The root `.env` is the canonical shared configuration file. It is ignored by
Git. Context-owned charts do not discover component-local `.env` files. The
`hindsight` lifecycle targets deliberately delegate to its adjacent owning
repository, whose `.env` supplies its database and API credentials; explicit
caller values still take final precedence.

`make deploy` loads `.env` automatically for its complete deployment workflow.
Export `.env` before invoking other cluster targets directly:

```bash
set -a
source .env
set +a
```

`make doctor` checks that the file exists, parses as shell syntax, and has been
exported to Make. It never prints secret values.

Review at least these settings before deployment:

| Area | Required configuration |
| --- | --- |
| Images | Published SemVer image references and `DAEDALUS_CONTEXT_IMAGE_VERSION` |
| Context | Exact approved `EXPECTED_CONTEXT` in `.env` or supplied through the operator environment |
| Hindsight | Adjacent `HINDSIGHT_ROOT` plus its repository-owned runtime credentials |
| Storage | Reviewed `MINIO_STORAGE_CLASS`, `ETCD_STORAGE_CLASS`, and `IMAGE_STORAGE_URI` |
| Provider auth | Distinct Kubernetes MCP and UniFi MCP current tokens |
| External MCP access | `MCP_EXTERNAL_ENABLED`, stable NodePorts, and `MCP_EXTERNAL_CLIENT_CIDR` |
| UniFi | Controller URL, exact controller CIDR, API key, and TLS policy |
| NVIDIA and models | Required NGC, NVIDIA API, and Hugging Face credentials |
| NV-Ingest extraction | Approved OCR, page/graphic/table layout, VLM caption, and Nemotron Parse HTTP endpoints and protocols |
| Stateful credentials | Strong MinIO and Milvus credentials or authoritative Secrets |
| Ingestion | Source paths, source node, collections, bucket, and job limits |

All eight releases use `DAEDALUS_NAMESPACE=daedalus`. The chart values use
same-namespace service names; there is no split-namespace production mode.

Charts reference, but never create, production credentials. Create and manage
the following Secrets through the approved Secret-management path:

- `milvus-minio-credentials`, `milvus-root-credentials`;
- `ngc-api`, `ngc-secret`, `nv-ingest-object-store`, `nvcr-imagepullsecret`;
- `url-ingest-hf`;
- `k8s-mcp-auth`; and
- `unifi-network-mcp-auth`, `unifi-network-mcp-unifi-api`.

Hindsight references `hindsight-runtime`, which its adjacent owning repository
creates from `../daedalus-hindsight/.env` without placing credentials in Helm
values. `make deploy-hindsight`, `make healthcheck-hindsight`, and `make
migrate-hindsight` are the root entry points.

Never commit `.env`, kubeconfigs, tokens, keys, credentials, or model data.

## Image Lifecycle

The root `docker-compose.yaml` defines build contexts and versioned image names
for:

- Kubernetes MCP;
- UniFi MCP;
- PDF server;
- URL crawler; and
- the URL-ingest NV-Ingest task runner.

It does not build the Kubernetes-only Milvus, retriever, or distributed
NV-Ingest control planes.

Validate and build images without Kubernetes access:

```bash
make images-validate
make images-build
```

Publish a new workspace release only after selecting a new immutable SemVer
tag in `.env`:

```bash
make images-publish
```

This builds and pushes all five images. Its final output contains the
`K8S_MCP_IMAGE`, `UNIFI_MCP_IMAGE`, `PDF_SERVER_IMAGE`, `URL_INGEST_IMAGE`, and
`URL_INGEST_RUNNER_IMAGE` assignments used by Kubernetes deployment.

Print the assignments without rebuilding or pushing:

```bash
make images-release-refs
```

Published version tags must not be overwritten. `make deploy` and
`make update` never build or push images.

## Release and Cluster Gates

`stack/components.lock.yaml` is the release inventory. Before a rollout:

1. Replace `pending-uncommitted-working-tree` source revisions with reviewed commits.
2. Publish the selected local image release.
3. Record reviewed versions for upstream images and model artifacts.
4. Keep the retriever revisions and serving contracts aligned with
   `stack/rag-schema.yaml`.
5. Record the approved storage and acceptance decisions in the selected
   environment file.

Run contract validation:

```bash
scripts/contextctl validate --environment "$ENVIRONMENT"
```

`Contract validation passed` means the YAML contracts agree. It is not release
approval when a following line begins with `BLOCKED:`.

The checked-in production environment permits personal, single-user non-HA
operation with recorded accepted risks. `make validate-ha` intentionally
rejects that policy. Shared production HA requires approved numeric node-loss,
failover, RPO, and RTO criteria.

### External MCP Network Prerequisite

External MCP Services are disabled during bootstrap. When enabled, their fixed
NodePorts use `loadBalancerSourceRanges`. Cilium must enforce
source ranges for all load balancer types:

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

Use the exact installed Cilium version. After internal provider and consumer
checks pass, run `make enable-external-mcp`. The intentionally unclaimed load
balancer class prevents an address controller from advertising the Service
while retaining fixed, CIDR-restricted NodePorts. Because that Service never
receives load-balancer ingress status, this target verifies the Deployment
rollout and the exact NodePort, source range, Cilium annotation, and session
affinity directly instead of waiting for an ingress address.

## Deploy and Update

All eight components are selected by default. This one command loads `.env`,
runs the deployment gates, reconciles the components, and checks provider
health:

```bash
make deploy
```

Deployment is idempotent. It does not restart unchanged workloads, publish a
corpus, or launch ingestion Jobs. URL CronJobs default to suspended and MCP
Services default to ClusterIP for a safe fresh installation.

Limit any operation with the allowlisted `COMPONENTS` variable:

```bash
COMPONENTS="k8s-mcp unifi-mcp" make update
COMPONENTS="milvus retriever nv-ingest" make deploy
```

Dependencies are ordered only when selected. When operating on `nv-ingest` or
`url-ingest` alone, include its dependencies or first prove that they are
healthy.

Use the lifecycle targets according to intent:

```bash
make deploy     # Load config, validate, reconcile, and check provider health.
make update     # Reconcile already-published versioned releases.
make redeploy   # Deliberately restart selected long-running workloads.
```

None of these targets uninstall releases or delete PVCs.

After the new stack, fresh corpus, Agent consumer path, schedules, and external
MCP endpoints have all passed acceptance, the one-time legacy cleanup is:

```bash
CONFIRM_LEGACY_CLEANUP=delete-legacy-daedalus-context make cleanup-legacy
```

The command targets only `daedalus-hindsight`, `milvus`,
`daedalus-retriever`, `nv-ingest`, `k8s-mcp-server`, and
`unifi-network-mcp-server`, plus the old
`k8s-mcp-server-readonly` cluster role/binding. Before wiping any retained
local-etcd directory it snapshots the PV/node/path tuple, requires the old
claim to be gone and the PV to be Released or Available, rejects attachments,
symlinks, mountpoints, and paths outside the exact local-etcd PVC prefix.

Each component is an ordinary Helm release and can be inspected with familiar
commands such as `helm status`, `helm get values`, and `helm get manifest` in
the `daedalus` namespace. See `helm/README.md` for release names, direct Helm
commands, values, and Secret contracts. The root workflow remains the canonical
way to preserve dependency order and safety gates.

## Data Operations

Data changes are separate from service deployment.

```bash
# Publish PDFs, stage URL inputs, ingest all collections, wait, and verify.
make seed-data CORPUS_GENERATION=release-2026-08-09

# Publish a verified PDF generation and atomically select it.
make publish-corpus CORPUS_GENERATION=release-2026-08-09

# Publish PDFs and stage URL inputs without submitting documents.
make stage-data CORPUS_GENERATION=release-2026-08-09

# Resume selected staged collections. Previously successful files are skipped.
make run-ingest COLLECTIONS="nvidia kubernetes"
```

`pdf-server` solely owns PDF Kubernetes resources and corpus publication.
`url-ingest` consumes the selected corpus generation and owns its CronJobs,
manifests, and ingestion Jobs. Scheduled and manual ingestion share a capacity
semaphore on the manifest PVC. `URL_INGEST_GLOBAL_CAPACITY` controls scheduled
jobs and defaults to one; manual runs use `MAX_PARALLEL_INGEST_SHARDS` for both
their shard batch size and immutable Job slot count. Keep the two values within
the capacity of the shared extraction services. After a fresh seed and
collection validation, run `make enable-schedules`.

Normal seed and resume operations never drop a Milvus collection. Do not delete
PVCs or collections to diagnose persistence.

## Daedalus Integration

Generate the non-secret consumer contract:

```bash
make render-agent-env
```

Merge that output through the approved Daedalus deployment configuration.
Synchronize provider tokens and stateful credentials through the approved
Secret-management path. Do not edit
`stack/generated/production-daedalus-context.env` by hand.

The native Daedalus reranker endpoint is `/rerank`. NIM-compatible clients use
`/v1/ranking`. Keep those contracts distinct.

## Health and Diagnostics

Use the narrowest health layer that answers the question:

```bash
make health-provider       # Workloads, rollouts, probes, and Kubernetes state.
make health-dependencies   # Upstreams, PVCs, collections, and freshness.
make health-integration    # Requests from the configured Daedalus workload.
make health-stack          # Combined availability, freshness, and redundancy.
```

Set `CHECK_CONSUMER=false` only before Daedalus exists and only for targets that
support that mode. Provider health alone does not prove that Daedalus can reach
or authenticate to a service.

`make health-stack` prints JSON and requires the context guard. For automation
that must distinguish health states, use the direct command after a guarded
preflight:

```bash
make preflight
scripts/contextctl health-stack \
  --environment "$ENVIRONMENT" \
  --components milvus retriever nv-ingest pdf-server url-ingest k8s-mcp unifi-mcp
```

The direct command exits:

- `0` for healthy;
- `1` for required availability loss; and
- `2` for degraded, stale, or at-risk state.

GNU Make collapses nonzero recipe statuses, so use `contextctl` when the
difference between `1` and `2` matters.

## Recovery

Treat Milvus, etcd, MinIO, PDF generations, URL manifests, and Daedalus storage
as durable data.

| Situation | Action |
| --- | --- |
| A release failed | Read the operation JSON, repair the failed component, and rerun the same root target |
| A collection Job failed | Rerun `make run-ingest COLLECTIONS="<name>"`; successful files remain recorded |
| Source URLs changed | Run `make stage-data`, then ingest only affected collections |
| PDFs changed | Publish a new corpus generation, then ingest affected collections |
| Existing retrieval failed | Check Milvus counts and retriever health before involving ingestion |
| New ingestion failed | Check NV-Ingest, Redis, MinIO, Milvus, and retriever dependencies |
| Daedalus cannot list MCP tools | Check provider Deployment, Service, current/next token, generated URL, and consumer probe |
| A full collection rebuild is required | Back up Milvus, drain ingestion, and use the reviewed component recovery procedure |

Use `nim-milvus-ms/reset-etcd.sh` only after backup review and its explicit
confirmation. Never use destructive cleanup as a routine health check.

## Component Development

Use component-local tests when changing one service:

```bash
cd k8s-multicluster-mcp && make check
cd nv-ingest && PYTHONPATH=src:api/src:client/src pytest
cd url-ingest && cargo test && cargo clippy -- -D warnings
cd unifi-network-mcp && make test
```

Return to the root and run `make validate-local` before a stack-level handoff.

Codex automatically discovers the repository skill at
`.agents/skills/operate-daedalus-context/`. Invoke it explicitly as
`$operate-daedalus-context` when a task involves deployment, health, ingestion,
or recovery.

## Repository Hygiene

Check the complete working tree before staging:

```bash
git status --short
git diff --check
```

Preserve unrelated work. Do not add generated datasets, downloaded URLs,
caches, `.env` files, kubeconfigs, or credentials.
