# Operations

Use the [deployment guide](deployment.md) for initial setup and the
[root command index](../Makefile) (`make help`) for supported commands.
Run commands from the repository root after exporting `.env` and setting an
approved literal `EXPECTED_CONTEXT`.

## Deploy and Update

All eight components are selected by default. This one command loads `.env`,
runs the deployment gates, builds and pushes selected locally owned images from
the current checkout, reconciles the components, and checks provider health:

```bash
make deploy
```

Each source deployment derives unique SemVer tags from the configured runtime
image references. The tags are printed and passed directly to Helm through the
existing component recipes; `.env` keeps its base references. This makes new
local source take effect even when the configured base tag is unchanged.
To reuse that build with `make update`, record the printed image assignments in
the runtime configuration first. Otherwise `update` uses the configured base
references, which may select an older published image. Helm history retains the
deployed references; another `make deploy` always builds fresh deployment tags.
Milvus, retriever and the upstream NV-Ingest images retain their pinned versions.
Hindsight delegates to its external owner's existing deployment workflow.

Deployment does not publish a corpus or launch ingestion Jobs. `update` also
restarts the PDF server to reload the selected corpus; other components roll
only when their workload templates change. URL CronJobs default to suspended and MCP
Services default to ClusterIP for a safe fresh installation.

Limit any operation with the allowlisted `COMPONENTS` variable:

```bash
make update COMPONENTS="k8s-mcp unifi-mcp"
make deploy COMPONENTS="milvus retriever nv-ingest"
```

Dependencies are ordered only when selected. When operating on `nv-ingest` or
`url-ingest` alone, include its dependencies or first prove that they are
healthy.

Use the lifecycle targets according to intent:

```bash
make deploy     # Validate, build/push selected local sources, reconcile, and check health.
make update     # Reconcile already-published versioned releases.
make redeploy   # Deliberately restart selected long-running workloads.
make restart    # Load .env and run redeploy (all components by default).
```

None of these targets uninstall releases or delete PVCs.
`deploy` keeps configured storage, source state, collection data and schedule
selection. Running ingestion Jobs continue with their original images; the
updated CronJob template applies to future runs. Selected dependency releases
are reconciled in dependency order; omitted components are neither built nor
restarted. Failed validation/build stops publication, and failed publication
stops reconciliation. Successfully pushed images may remain if a later push or
rollout fails; inspect Helm history and the printed image references for rollback.
`restart` and `redeploy` operate only on this repository's selected releases,
including Hindsight when selected. They do not restart other applications
merely because those applications share `daedalus`. Disabled replicas remain disabled.
URL-ingest runs a small status/metrics service and five daily CronJobs.
Use `update` to reconcile templates; `refresh-run` starts explicit data work.

Each component is an ordinary Helm release and can be inspected with familiar
commands such as `helm status`, `helm get values`, and `helm get manifest` in
the `daedalus` namespace. The [stack model](../README.md#stack-model) lists the owning
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
and reports separately. Specify an explicit collection selection for every data-writing run.

`pdf-server` owns corpus publication and its Kubernetes resources. `url-ingest`
consumes the selected corpus and owns refresh Jobs, schedules, and durable
source state. Scheduled and manual refresh Jobs use the same collection locks
and shared manifest-PVC capacity slots. `URL_INGEST_GLOBAL_CAPACITY` configures
the capacity of deployed CronJobs, which manual refresh Jobs inherit.

After initial collection acceptance, use `make enable-schedules` and persist
`URL_INGEST_SUSPEND=false` in `.env`. Normal refresh operations preserve existing
collections and reconcile changed sources. Do not delete PVCs or collections to
diagnose persistence. See [URL ingestion](../url-ingest/README.md) for source,
retry, and freshness behavior.


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

These integration targets run synthetic provider requests from the Agent pod;
they do not invoke the Rust tool runtime or establish end-to-end answer quality.
The retriever probe exercises the Agent's role-aware embedding request, NIM
embedding input, native reranking and NIM ranking. It checks finite normalized
vectors and API response shape, without writing documents. Model templates add
query/document prefixes; the probe must send raw text just like the consumers.
Milvus connectivity and collection counts alone do not prove that a query
returns relevant passages with intact metadata and citations. Validate that
separate path through the Agent when retrieval behavior changes, then check its
model continuation through Switchyard when model input or continuation changes.

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
# Both commands load the root .env, including your approved context.
make recovery-status
make restart

# Or limit the operation to specific services.
make restart COMPONENTS="pdf-server k8s-mcp unifi-mcp"
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
data appear portable. Full automatic recovery for this stack requires
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


## Continuous refresh and embedding generations

The retrieval and source contracts are maintained in
[stack/rag-schema.yaml](../stack/rag-schema.yaml),
[stack/collections.yaml](../stack/collections.yaml), and the
[URL-ingest chart values](../url-ingest/chart/values.yaml). The root deployment
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
Use [URL ingestion](../url-ingest/README.md) for the maintained operational guide
and [refresh diagnostics](../.agents/skills/operate-daedalus-context/references/refresh-operations.md)
for failure analysis.

A generation identifies the model and source-processing contract. The active
database is selected by the retrieval schema. Never mix embeddings from
different vector spaces or infer compatibility from matching dimensions alone.
