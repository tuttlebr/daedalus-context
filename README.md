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
| Load configuration and restart all app services | `make restart` | Restarts existing Helm-owned workloads; retains storage |
| Explain node-loss and storage recovery blockers | `make recovery-status` | Read-only cluster access; loads `.env` |
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

Keep `MCP_EXTERNAL_ENABLED=false` to keep the authenticated MCP endpoints
inside the cluster. For operator access, use a localhost-bound `kubectl
port-forward` over the authenticated Kubernetes connection; the MCP token is
still required.

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
make restart    # Load .env and run redeploy (all components by default).
```

None of these targets uninstall releases or delete PVCs.
`restart` and `redeploy` operate only on this repository's selected releases,
including Hindsight. They do not restart Agent, Switchyard, Hue, or ESPN merely
because those applications share `daedalus`. Disabled replicas remain disabled.
URL-ingest runs a small status/metrics service and five daily CronJobs.
Use `update` to reconcile templates; `refresh-run` starts explicit data work.

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

The reranker uses `nvidia/Nemotron-3.5-Rerank-8B-BF16` through a vLLM 0.32.0
classification adapter, retaining both ranking contracts and the previous
model ID as a compatibility alias. The embedder keeps its existing model and
runtime. See `nemotron-retriever/README.md` for the pinned checkpoint, 40 GiB
cache requirement, and rollout checks.

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

### Manual restart with existing storage

```bash
# Both commands load the root .env; the explicit argument overrides its context.
make recovery-status EXPECTED_CONTEXT=kubernetes-admin@kubernetes
make restart EXPECTED_CONTEXT=kubernetes-admin@kubernetes

# Or limit the operation to specific services.
make restart EXPECTED_CONTEXT=kubernetes-admin@kubernetes COMPONENTS="pdf-server k8s-mcp unifi-mcp"
```

Restart discovers Deployments and StatefulSets using Helm release ownership,
including MinIO's older labels. It restarts stateful dependencies first, waits
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

Deployment controllers already replace pods after node-loss eviction. Milvus,
PDF, and MCP topology spread now honors node taints so unavailable nodes do not
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
| Hindsight PostgreSQL | Single database on `local-etcd`; automatic cross-node recovery requires database replication or a reviewed migration to suitable shared/replicated storage |
| NV-Ingest Redis | Fixed primary and replica on `local-fast`; a replica alone does not provide automatic primary promotion |
| Retriever | Replacement requires eligible GPU/CPU capacity and accessible model cache |
| MinIO, PDF corpus, URL manifests | Shared NFS claims can be reused on another node while NFS remains available |

Do not change a bound claim's storage class or PV node affinity to make local
data appear portable. Full automatic recovery for this stack requires a
separate backed-up migration/replication plan for PostgreSQL and Redis and a
fencing controller. These Makefile and placement changes do not claim HA or a
tested recovery-time objective. Use `make health-dependencies` and
`make health-integration` after workloads recover to verify data and consumers.

| Situation | Action |
| --- | --- |
| A release failed | Read the operation JSON, repair the failed component, and rerun the same root target |
| A collection Job failed | Check `make refresh-status`; retries are automatic. `make refresh-run COLLECTIONS="<name>"` resumes committed progress |
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

## Continuous refresh and embedding generations

`helm/milvus` owns durable vector storage. `nim-milvus-ms` is a legacy wrapper;
it does not schedule refreshes. `helm/url-ingest` owns the five daily refresh
Jobs, persistent source manifests, retry policy, and freshness monitoring.
`nemotron-retriever` serves the embedding and reranking APIs consumed by both
refresh workers and daedalus-agent.

The retrieval contract is `stack/rag-schema.yaml`; collection schedules and
freshness limits are in `stack/collections.yaml`. The root deployment renders
`helm/url-ingest/files/refresh-contract.json` from those files and the existing
source lists. The active vector space uses the pinned
`nvidia/Nemotron-3.5-Embed-1B-NVFP4` checkpoint, 2,048 normalized dimensions,
and vLLM 0.32 with Marlin on L40S. The adapter sends native agent query messages
and NV-Ingest-style document requests through the same `/v2/embed` prompt
contract. The worker uses the pinned tokenizer directly with truncation and
padding disabled, then makes 1,024-token chunks with 64-token overlap.

The current embedding generation is `nemotron35_nvfp4_20261020`. It was built
and verified in a staging database, then promoted into `default`, the database
supported by daedalus-agent. The five collection names remain `nvidia`,
`kubernetes`, `vetpartner`, `semianalysis`, and `mentalhealth`. Old collections
are retained in `legacy_pre_nemotron35_20261020` for rollback. Source fingerprints
identify the model generation independently of its physical database. The
consumer embedding model must match the active generation; `make render-agent-env`
prints the model and supported database. Uploaded-document indexes from the old
generation also require re-ingestion; their originals and old vectors are retained.
The agent's curated-domain `search_params.metric_type` must be `COSINE`, matching
the collection indexes. Its private-document search can use `search_params:
{params: {}}` to let each collection supply its index metric; a hard-coded `L2`
search fails against the curated cosine indexes.
The new reranker returns raw logits, so the previous probability-based
`reranker_min_score` cutoff must be unset until a threshold is calibrated for
this model. Ranking still orders the passages and returns the configured top five.

NV-Ingest extracts documents; the refresh worker owns chunking, embedding, and
Milvus writes. Source hashes and deterministic chunk IDs make retries idempotent.
Every replacement chunk must be visible before old chunks are removed. A failed
replacement keeps the previous version, and an interrupted partial replacement
is reconciled on retry. HTTP validators avoid downloading unchanged content.
Previously known URLs are checked even if omitted from a sitemap; removal
requires repeated 404/410 observations at least 48 hours apart. PDF removals use
the same grace period. A missing corpus directory is an error, not a deletion.

Schedules run daily at 02:00–06:00 UTC, with three Kubernetes retries and bounded
HTTP retries. Shared filesystem locks release when a worker dies. PDFs are
checked first; interrupted URL scans resume with the least recently attempted
sources to prevent starvation. Previously failed sources and expired deletion
grace periods take priority over new sources on the next pass. Workers checkpoint
and finish a partial pass after a 21-hour work budget, leaving two hours before
Kubernetes' 23-hour hard deadline for in-flight work to settle. Partial passes do
not advance freshness. Large initial backfills may span multiple daily runs.
An overlapping manual or scheduled attempt exits successfully with
`state: skipped_active`; it does not replace the active worker's report or mark
the collection fresh. This also covers manual backfills, which Kubernetes'
CronJob concurrency policy does not track.

Requests to each host are serialized and paced, including fast 304 responses.
The default minimum interval is 0.5 seconds; SemiAnalysis starts at 5 seconds and
the larger NVIDIA corpus uses 0.1 seconds.
Repeated HTTP 403 responses, 429 throttling, and 503 responses activate a
durable host cooldown that survives retry pods and honors `Retry-After`.
Blocked hosts also increase their persistent request interval, up to 30 seconds.
Errors are published immediately, and an active cooldown is explicitly unhealthy
with a separate Prometheus alert; an old success cannot hide the block.
NV-Ingest result requests use a bounded 10-minute read timeout for PDFs, allowing
large manuals to finish extraction, while URL extraction retains a one-minute
read timeout. Transport and Job retry limits still apply.
Retries of the same Kubernetes Job reuse its successful source checks; a new
daily Job revalidates them. HTTP access failures remain visible as failures.
404/410 observations within the 48-hour deletion grace period appear as
`pending_removals`, preserve existing vectors, and do not consume immediate
Job retries. They do not advance the collection's freshness timestamp.

```bash
set -a; source .env; set +a
export EXPECTED_CONTEXT='kubernetes-admin@kubernetes'
make refresh-run COLLECTIONS="nvidia kubernetes vetpartner semianalysis mentalhealth"
make refresh-status
make refresh-verify  # starts a read-only retrieval acceptance Job
make health-dependencies COMPONENTS=url-ingest
```

`REFRESH_LIMIT=1` runs a bounded smoke test and never marks a collection fresh.
`run-ingest` delegates to the new reconciler; the legacy append-only runner is
retired. Production `.env` must keep `URL_INGEST_SUSPEND=false` so upgrades do
not accidentally disable automation. A new installation defaults to suspended
until its model and corpus paths are validated.

Reports live on the manifest PVC under `refresh/<generation>/runs/<collection>/`
and `refresh/<generation>/status/`. Failure journals retain every source error;
status responses include the first 100 errors at most. `url-ingest-monitor:8080`
serves `/status`, `/metrics`, and `/readyz`. Process readiness is separate from
corpus freshness. Prometheus alerts cover stale collections (36 hours), failures,
lost worker heartbeats, suspended schedules, and missing monitoring. An initial
or partially failed rebuild remains visibly unhealthy until a complete scan
succeeds. Kubernetes Job TTL is 30 days; durable reports survive Job deletion.

For a model migration, build and validate a new generation, verify retrieval,
then archive the previous collections and promote the new generation into
`default` while refresh writers are stopped. Update the consumer model together
with promotion. Rollback reverses the recorded collection moves and restores
the previous serving model; do not reset etcd,
delete PVCs, or merge vectors from different models.
