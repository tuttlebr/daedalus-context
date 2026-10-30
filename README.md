# Daedalus Context Operations

This repository builds and operates the Kubernetes services that provide
retrieval, ingestion, and infrastructure tools to
[daedalus-agent](https://github.com/tuttlebr/daedalus-agent).

Kubernetes is the only supported deployment runtime. Use the root `Makefile`
for deployment, updates, health checks, and data operations. The root
`docker-compose.yaml` is an image specification used to validate, build, and
publish the locally maintained images. It is not a second deployment
path.

## Start Here

Run commands from the repository root. The normal first deployment is:

```bash
cd /volume2/daedalus/datasets/daedalus-context

test -e .env || cp .env.example .env
${EDITOR:-vi} .env

set -a
source .env
set +a
export EXPECTED_CONTEXT='kubernetes-admin@kubernetes'  # Use the approved literal for your cluster.

make verify-shared-secrets
make deploy
```

Set `ENVIRONMENT` and the approved literal `EXPECTED_CONTEXT` in `.env` before
running the command. Do not populate `EXPECTED_CONTEXT` by reading the current
context. That would defeat the wrong-cluster guard. `make deploy` loads `.env`,
runs doctor and non-secret configuration checks, validates the cluster,
reconciles all eight Helm releases in `daedalus`, and checks provider health.

After the providers are ready:

```bash
# Existing cluster: prove that persisted collections are still healthy.
make health-dependencies

# Fresh corpus: publish PDFs and submit refresh Jobs for the selected collections.
make seed-data CORPUS_GENERATION=corpus-v1 \
  COLLECTIONS="nvidia kubernetes vetpartner semianalysis mentalhealth"
make refresh-status
```

Job submission does not establish ingestion completion. Follow the durable
refresh status and verify retrieval after the Jobs finish. Do not seed an
existing cluster merely because services were redeployed.

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
| Build the local images | `make images-build` | Writes local image state |
| Build and publish a versioned image release | `make images-publish` | Writes local image and remote registry state |
| Run source tests and renders | `make validate-local` | Read-only validation plus disposable build output |
| Validate all eight chart contracts | `make validate-helm` | Read-only local Helm lint and render |
| Validate manifests and cluster capacity | `make validate-cluster` | Read-only cluster access and client dry-runs |
| Validate, reconcile, and check all selected services | `make deploy` | Reads `.env` and writes Kubernetes resources |
| Reconcile an already-published update | `make update` | Writes Kubernetes resources |
| Restart selected long-running workloads | `make redeploy` | Restarts pods; does not delete PVCs |
| Load configuration and restart all app services | `make restart` | Restarts existing Helm-owned workloads; retains storage |
| Explain node-loss and storage recovery blockers | `make recovery-status` | Read-only cluster access; loads `.env` |
| Check provider workloads | `make health-provider` | Read-only cluster access |
| Check dependencies and data freshness | `make health-dependencies` | Read-only cluster access |
| Probe providers from Daedalus | `make health-integration` | Read-only cluster access |
| Publish PDF inputs | `make publish-corpus` | Writes a corpus generation and switches its pointer |
| Publish PDFs and submit refresh Jobs | `make seed-data COLLECTIONS="..."` | Writes corpus data and creates Jobs; returns after submission |
| Submit refresh Jobs | `make refresh-run COLLECTIONS="..."` or `make run-ingest COLLECTIONS="..."` | Creates Jobs that write collection data |
| Read refresh progress | `make refresh-status` | Reads durable status |
| Submit retrieval verification | `make refresh-verify` | Creates a Job that reads retrieval data |

Every command that reads or writes Kubernetes requires `EXPECTED_CONTEXT` and
refuses to continue when it does not match the active context.

## Stack Model

Each service owns its implementation, tests, and `chart/` in one root folder.
The root Makefile is the deployment interface for every service:
`make deploy COMPONENTS=<component>`, `make update COMPONENTS=<component>`, and
`make restart COMPONENTS=<component>`. Component Makefiles provide local
`render` and `validate-chart` commands through `scripts/service-chart.mk`;
these commands do not contact Kubernetes. Hindsight is owned by its adjacent
repository and remains an integration dependency.

Chart values, templates, schemas, and tests must describe the same contract.
The Milvus and NV-Ingest charts pin upstream dependencies with `Chart.lock`
and vendored archives. Dependency updates are explicit version changes.
Production credentials remain in existing Secrets. Helm rollback does not
restore Secret contents or database data. `docker-compose.yaml` contains only
image build and publication settings. Shared CI lives in `.github/workflows/`;
install development hooks from the root with `pre-commit install`.

The dependency graph and service contracts are declared in
[stack/topology.yaml](stack/topology.yaml). Component guides are linked below.

| Component | Owning directory | Namespace | Role |
| --- | --- | --- | --- |
| `hindsight` | `../daedalus-hindsight/charts/daedalus-hindsight/` | `daedalus` | Durable memory API, control plane, and PostgreSQL |
| [milvus](nim-milvus-ms/chart/README.md) | `nim-milvus-ms/chart/` | `daedalus` | Vector database and MinIO object storage |
| [retriever](nemotron-retriever/README.md) | `nemotron-retriever/chart/` | `daedalus` | Embedding and optional reranking inference |
| [nv-ingest](nv-ingest/DEPLOYMENT.md) | `nv-ingest/chart/` | `daedalus` | Document extraction service |
| [pdf-server](pdf-server/README.md) | `pdf-server/chart/` | `daedalus` | Versioned, read-only source PDF corpus |
| [url-ingest](url-ingest/README.md) | `url-ingest/chart/` | `daedalus` | Scheduled and manual collection population |
| [k8s-mcp](k8s-multicluster-mcp/README.md) | `k8s-multicluster-mcp/chart/` | `daedalus` | Authenticated Kubernetes operations tools |
| [unifi-mcp](unifi-network-mcp/README.md) | `unifi-network-mcp/chart/` | `daedalus` | Authenticated UniFi read and mutation tools |

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

Fresh installations default to internal MCP Services. Preserve the deployed
`MCP_EXTERNAL_ENABLED`, fixed NodePorts, ingress routes, and client CIDRs on
updates. The home LAN includes ordinary clients as well as nodes and storage;
a node-only allowlist does not preserve LAN access. Before changing exposure or
authentication, inventory consumers, retain rollback configuration, and verify
the API from an ordinary non-node LAN client. Compatibility breaks require
explicit approval. A localhost-bound port-forward also requires the MCP token.

For a UniFi controller using a private or self-signed certificate, set
`UNIFI_CONTROLLER_CA_FILE` to a reviewed public PEM certificate bundle and keep
`UNIFI_INSECURE_SKIP_VERIFY=false`. The UniFi chart mounts that bundle through a
ConfigMap and supplies it to Go's TLS verifier through `SSL_CERT_FILE`.
`UNIFI_BASE_URL` must use a hostname in the certificate. If local DNS does not
resolve that name, set both `UNIFI_CONTROLLER_HOSTNAME` and
`UNIFI_CONTROLLER_IP` to add an explicit pod host mapping. Reconcile certificate
renewals with `make update COMPONENTS=unifi-mcp` after reviewing the replacement
certificate; never put a controller private key in this repository.

Charts reference, but never create, production credentials. Create and manage
the following Secrets through the approved Secret-management path:

- `milvus-minio-credentials`, `milvus-root-credentials`;
- `ngc-api`, `ngc-secret`, `nv-ingest-object-store`, `nvcr-imagepullsecret`;
- `url-ingest-hf`;
- `k8s-mcp-auth`; and
- `unifi-network-mcp-auth`, `unifi-network-mcp-unifi-api`.

Hindsight references `hindsight-runtime`, which its adjacent owning repository
creates from `../daedalus-hindsight/.env` without placing credentials in Helm
values. `make deploy COMPONENTS=hindsight` and the root health targets are its
entry points.

Never commit `.env`, kubeconfigs, tokens, keys, credentials, or model data.

## Image Lifecycle

The root `docker-compose.yaml` defines build contexts and versioned image names
for:

- Kubernetes MCP;
- UniFi MCP;
- PDF server;
- URL crawler;
- the URL-ingest NV-Ingest task runner; and
- the local Nemotron document parser.

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

This builds and pushes all local images. Its final output contains the
`K8S_MCP_IMAGE`, `UNIFI_MCP_IMAGE`, `PDF_SERVER_IMAGE`, `URL_INGEST_IMAGE`,
`URL_INGEST_RUNNER_IMAGE`, and `NV_INGEST_PARSER_IMAGE` assignments used by
Kubernetes deployment.

Print the assignments without rebuilding or pushing:

```bash
make images-release-refs
```

Published version tags must not be overwritten. `make deploy` and
`make update` never build or push images.

## Release and Cluster Gates

`stack/components.lock.yaml` is the release inventory. Before a rollout:

1. Record reviewed source commits for the release.
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

When external MCP access is enabled, the fixed NodePorts use
`loadBalancerSourceRanges`. The cluster's Cilium configuration must enable
`bpf.lbSourceRangeAllTypes` to enforce those ranges on NodePort traffic. Manage
that prerequisite through the cluster's configuration workflow while preserving
existing LAN clients and routes.

After internal provider and consumer checks pass, `make enable-external-mcp`
reconciles the external Services. Persist `MCP_EXTERNAL_ENABLED=true` and the
approved ports/CIDR in the root `.env` so later updates preserve access. The
unclaimed load-balancer class retains fixed NodePorts without requesting a
separate advertised address. The target checks the Deployment rollout and
Service fields directly, including port, source range, and Cilium annotation.
Those checks do not replace an authenticated API probe from a non-node LAN client.

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
make restart    # Load .env and run redeploy (all components by default).
```

None of these targets uninstall releases or delete PVCs.
`restart` and `redeploy` operate only on this repository's selected releases,
including Hindsight. They do not restart Agent, Switchyard, Hue, or ESPN merely
because those applications share `daedalus`. Disabled replicas remain disabled.
URL-ingest runs a small status/metrics service and five daily CronJobs.
Use `update` to reconcile templates; `refresh-run` starts explicit data work.

Each component is an ordinary Helm release and can be inspected with familiar
commands such as `helm status`, `helm get values`, and `helm get manifest` in
the `daedalus` namespace. The [stack model](#stack-model) lists the owning
service folders and charts. The root workflow preserves dependency order and
safety gates.

## Data Operations

Data changes are separate from service deployment. Set `CORPUS_GENERATION` to
a unique source revision or digest when publishing PDFs, and specify
`COLLECTIONS` when submitting refresh work:

```bash
# Publish and select a verified PDF generation.
make publish-corpus CORPUS_GENERATION=corpus-v1

# Submit resumable refresh Jobs using the deployed source/model contract.
make refresh-run COLLECTIONS="nvidia kubernetes"
make refresh-status

# Submit a retrieval acceptance Job after refresh has completed.
make refresh-verify
```

`make stage-data` publishes the PDF corpus; URL sources are fetched by the
refresh worker. `make seed-data` publishes PDFs and submits refresh Jobs for
`COLLECTIONS`. `make run-ingest` is an alias for `refresh-run`. Refresh submission
and verification commands return after creating Jobs; inspect their completion
and reports separately. No collections are selected when `COLLECTIONS` is empty.

`pdf-server` owns corpus publication and its Kubernetes resources. `url-ingest`
consumes the selected corpus and owns refresh Jobs, schedules, and durable
source state. Scheduled and manual refresh Jobs use the same collection locks
and shared manifest-PVC capacity slots. `URL_INGEST_GLOBAL_CAPACITY` configures
the capacity of deployed CronJobs, which manual refresh Jobs inherit.

After initial collection acceptance, use `make enable-schedules` and persist
`URL_INGEST_SUSPEND=false` in `.env`. Normal refresh operations preserve existing
collections and reconcile changed sources. Do not delete PVCs or collections to
diagnose persistence. See [URL ingestion](url-ingest/README.md) for source,
retry, and freshness behavior.

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

The embedding model, dimensions, database, and ranking contracts are declared
in [stack/rag-schema.yaml](stack/rag-schema.yaml). The serving configuration is
in [nemotron-retriever/values.yaml](nemotron-retriever/chart/values.yaml). Keep query
and document embeddings in the same vector space and match each collection's
index metric. Curated-domain indexes use `COSINE`.

Reranker scores are raw logits, which can be negative. A probability-based
minimum-score threshold is inappropriate unless calibrated for the selected
model. Reranking remains optional under the topology's failure policy.

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
  --components hindsight milvus retriever nv-ingest pdf-server url-ingest k8s-mcp unifi-mcp
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

### Manual restart with existing storage

```bash
# Both commands load the root .env; the explicit argument overrides its context.
make recovery-status EXPECTED_CONTEXT=kubernetes-admin@kubernetes
make restart EXPECTED_CONTEXT=kubernetes-admin@kubernetes

# Or limit the operation to specific services.
make restart EXPECTED_CONTEXT=kubernetes-admin@kubernetes COMPONENTS="pdf-server k8s-mcp unifi-mcp"
```

Restart discovers Deployments and StatefulSets using Helm release ownership,
including the labels used by the MinIO subchart. It restarts stateful dependencies first, waits
for each rollout, and then restarts Deployments. It refuses missing releases,
missing/unbound storage, unavailable fixed-host volumes, paused workloads,
partitioned/OnDelete StatefulSets, and stateful pods stranded on unavailable
nodes. The JSON report explains these blockers; a nonzero recovery-status
result also signals insufficient ready replicas. Terminal pod records are
reported separately because an old failure record can coexist with a healthy
replacement.

If resources are missing, `make deploy` reconciles the charts under the same
release names and retains existing claims. It can create a new claim when one
is absent; a new empty volume does not recover old data. Check recovery-status
and the retained PVs before treating this as data recovery. Restart never
installs a release, changes credentials, deletes claims, or seeds data.

### Automatic recovery and its limits

Deployment controllers replace pods after node-loss eviction. Milvus,
PDF, and MCP topology spread honors node taints so unavailable nodes do not
keep replacement pods Pending solely to satisfy spread. Existing probes and
controllers handle process restarts. Replacements still require compatible
CPU/GPU capacity, reachable dependencies, and accessible storage.

The root Milvus Helm post-renderer spreads MinIO members with maximum skew one
across eligible, healthy scheduling domains. With four eligible nodes it keeps
one of the four members per node. With three, it permits a 2/1/1 distribution
so all four existing volumes can return to service; losing the node containing
two members can then interrupt quorum. All MinIO members still depend on the
same NFS appliance. Updating a template affects replacement/rollout placement;
it does not rebalance already-running pods automatically.

Abrupt node loss has a separate stateful safety boundary: Kubernetes can leave
StatefulSet pods Terminating until the old kubelet returns. A NotReady node
might still be writing to storage. After independently confirming that the
node is powered off or fenced, the cluster operator can use Kubernetes'
[non-graceful shutdown procedure](https://kubernetes.io/docs/concepts/cluster-administration/node-shutdown/#non-graceful-node-shutdown)
with the `node.kubernetes.io/out-of-service` taint. This is a node-wide action
affecting other applications, so the app restart target does not apply it.
Automatic stateful replacement across this failure requires a fencing
integration that can establish that safety condition; timeout alone is
insufficient. Remove the taint only after the node has safely recovered.

Local volumes remain tied to their original node even after pod deletion:

| Service | Current storage/failover boundary |
| --- | --- |
| Milvus etcd | Three members on `local-etcd`; two can retain quorum after one node loss, but the lost member needs its node/data restored or a reviewed membership replacement |
| Hindsight PostgreSQL | Single database on `local-etcd`; cross-node recovery requires database replication or accessible replicated storage |
| NV-Ingest Redis | Fixed primary and replica on `local-fast`; a replica alone does not provide automatic primary promotion |
| Retriever | Replacement requires eligible GPU/CPU capacity and accessible model cache |
| MinIO, PDF corpus, URL manifests | Shared NFS claims can be reused on another node while NFS remains available |

Do not change a bound claim's storage class or PV node affinity to make local
data appear portable. Full automatic recovery for this stack requires a
storage replication and recovery plans for PostgreSQL and Redis, plus a
fencing controller. Replica placement alone does not establish HA or a tested
recovery-time objective. Use `make health-dependencies` and
`make health-integration` after workloads recover to verify data and consumers.

| Situation | Action |
| --- | --- |
| A release failed | Read the operation JSON, repair the failed component, and rerun the same root target |
| A collection Job failed | Check `make refresh-status`; retries are automatic. `make refresh-run COLLECTIONS="<name>"` resumes committed progress |
| Source URLs changed | Update the source lists, reconcile `url-ingest` to regenerate its contract, then refresh affected collections |
| PDFs changed | Publish a new corpus generation, then ingest affected collections |
| Existing retrieval failed | Check Milvus counts and retriever health before involving ingestion |
| New ingestion failed | Check NV-Ingest, Redis, MinIO, Milvus, and retriever dependencies |
| Daedalus cannot list MCP tools | Check provider Deployment, Service, current/next token, generated URL, and consumer probe |
| A full collection rebuild is required | Prepare a separately authorized backup and recovery plan; preserve the active vector space until replacement retrieval is verified |

Use `nim-milvus-ms/reset-etcd.sh` only after backup review and its explicit
confirmation. Never use destructive cleanup as a routine health check.

## Component Development

Use component-local tests when changing one service:

```bash
make -C k8s-multicluster-mcp check
make -C nv-ingest test
make -C url-ingest test
make -C unifi-network-mcp test
make -C pdf-server test
make -C nemotron-retriever test
```

`make validate-local` runs the full source, image, and chart checks, including
Docker builds and validation in the adjacent Hindsight repository. Cluster-facing
changes also need `make validate-cluster` with the approved context. For
documentation-only edits, check links and command references; no cluster access
or image build is needed.

Codex automatically discovers the repository skill at
`.agents/skills/operate-daedalus-context/`. Invoke it explicitly as
`$operate-daedalus-context` for repository or component code review, documentation
alignment, bug fixes, deployment, health, ingestion, or recovery.

## Repository Hygiene

Check the complete working tree before staging:

```bash
git status --short
git diff --check
```

Preserve unrelated work. Do not add generated datasets, downloaded URLs,
caches, `.env` files, kubeconfigs, or credentials.

Keep documentation focused on current behavior and reusable procedures. Put
versions and environment settings in their canonical configuration files and
link to them. Use Git history for completed reviews, incident reports, migration
narratives, and changelog material; do not retain those as operating guides.

## Continuous refresh and embedding generations

The retrieval and source contracts are maintained in
[stack/rag-schema.yaml](stack/rag-schema.yaml),
[stack/collections.yaml](stack/collections.yaml), and the
[URL-ingest chart values](url-ingest/chart/values.yaml). The root deployment
renders them and the source lists into the refresh contract. Update these
inputs instead of editing generated JSON alone. Model revisions, schedules,
freshness limits, retries, and work budgets belong in those configuration files.

NV-Ingest extracts content. The refresh worker chunks, embeds, and writes
Milvus rows using the same embedding contract as Daedalus queries. Source hashes
and deterministic chunk IDs make replacements resumable. New chunks must be
visible before old chunks are removed; failed replacements preserve the
previous document. HTTP validators avoid downloading unchanged content. Missing
URLs require repeated removal evidence across the deletion grace period; a
sitemap omission or unavailable PDF directory cannot justify deleting vectors.

Each collection has a filesystem lock shared by scheduled and manual Jobs.
Overlapping attempts report `skipped_active` without replacing the active
worker's status. Host pacing and durable cooldowns survive retries. Partial,
limited, failed, or pending-removal passes do not advance full-scan freshness.
`REFRESH_LIMIT=1` bounds a smoke test but still writes data.

Durable reports live under `refresh/<generation>/runs/<collection>/` and
`refresh/<generation>/status/` on the manifest PVC. The monitor serves `/status`,
`/metrics`, and `/readyz` at `url-ingest-monitor:8080`. Process readiness, Job
completion, corpus freshness, and consumer retrieval are separate signals.
Use [URL ingestion](url-ingest/README.md) for the maintained operational guide
and [refresh diagnostics](.agents/skills/operate-daedalus-context/references/refresh-operations.md)
for failure analysis.

A generation identifies the model and source-processing contract. The active
database is selected by the retrieval schema. Never mix embeddings from
different vector spaces or infer compatibility from matching dimensions alone.
