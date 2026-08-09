# URL Ingest Pipeline

A two-stage data ingestion pipeline that crawls websites, downloads content (HTML/MD/PDF), and uploads it as chunked, embedded vectors into Milvus.

## Architecture

```
                         +-----------------+
  files/*.txt ---------> | url-ingest      |----> PVC: url-ingest-data
  (URL lists/sitemaps)   | (Rust binary)   |      (downloaded HTML/MD)
                         +-----------------+
                                                         |
                         +-----------------+             v
  PVC: pdf-server-data   | nv-ingest-ms-   |----> Milvus (vector DB)
  (PDF files) ---------->| tasks (Python)  |----> MinIO  (object store)
                         +-----------------+
```

**Stage 1 — url-ingest (Rust):** Reads URL lists or sitemaps, downloads web pages,
and saves them to disk as HTML or Markdown files. Supports `--skip-existing` to
avoid re-downloading content already on disk.

**Stage 2 — nv-ingest-ms-tasks (Python):** Discovers downloaded files and PDFs,
sends them through the NV-Ingest pipeline (extract, split, embed, upload to Milvus).
Uses a manifest file to track already-ingested files and avoid duplicates.

## Remediation Summary

This repository has been hardened based on a platform review for use as
`daedalus-agent` support infrastructure. The main changes are:

- The Rust crawler now fails closed when any URL download fails unless
  `--allow-failures` is explicitly set.
- HTTP crawling now has explicit connect/total timeouts and bounded retries with
  exponential backoff.
- Downloaded files are written to temporary files and atomically renamed into
  place, so interrupted writes are not treated as valid existing files.
- NV-Ingest manifests are JSON-lines records keyed by source type, ingest
  version, path, size, and SHA-256 content hash instead of path-only markers.
- Empty input discovery is treated as a failure by default. Set
  `ALLOW_EMPTY_INPUTS=true` only for an intentional no-op source.
- Destructive Milvus collection drops require explicit confirmation and fail
  closed on unexpected drop errors.
- Manual ingest runs now emit run-specific persistent status and failure
  reports under the manifests PVC, including terminal dependency and signal
  failures.
- Manual staging and population use per-collection locks, temporarily suspend
  every affected schedule, and refuse to rewrite staged inputs while an ingest
  Job is active. Population waits for terminal Job conditions and verifies a
  non-empty Milvus collection before reporting success.
- Every ingest Job performs an authenticated MinIO put/stat/delete canary before
  submitting documents, catching credential and bucket drift immediately.
- NVIDIA bootstrap is deterministically sharded, so a transient dependency
  failure retries a bounded subset instead of restarting the entire corpus.
- Kubernetes workloads use dedicated service accounts, disabled service account
  token automounting, security contexts, resource limits, and non-root runtime
  settings where supported.
- The `url-ingest` image reference is versioned as `0.1.0` instead of `latest`.
- `Cargo.lock`, `.dockerignore`, `.gitignore`, and GitHub Actions CI checks were
  added for reproducibility and regression prevention.

Repository validation is mutation-free; release qualification also uses the
live Kubernetes, NV-Ingest, Milvus, and MinIO environment and the persistent
per-run evidence described below.

## Collections

| Collection     | URL Sources                          | PDFs | Schedule      |
|----------------|--------------------------------------|------|---------------|
| nvidia         | nvidiadocs sitemaps, nvidia sitemaps  | Yes  | Sun 2:00 AM   |
| kubernetes     | Direct URL list (1,744 URLs)         | Yes  | Sun 3:00 AM   |
| vetpartner     | Direct URL list (2,547 URLs)         | Yes  | Sun 4:00 AM   |
| semianalysis   | Semianalysis sitemaps                | Yes  | Sun 5:00 AM   |
| mentalhealth   | None (PDF only)                      | Yes  | Sun 6:00 AM   |

## Prerequisites

- Kubernetes cluster with `nfs-client` storage class
- `nv-ingest` service running in the `nv-ingest` namespace
- `milvus` and `milvus-minio` services running in the `milvus` namespace
- Container images pushed to the registry:
  - `daedalus.ddns.me:5050/btuttle/url-ingest:0.1.0`
  - `nvcr.io/nvidia/nemo-microservices/nv-ingest:26.3.0`
- `nvcr-imagepullsecret` present in the `default` namespace for pulling from `nvcr.io`
- Milvus root and MinIO credential Secrets already created by the Milvus rollout
- `.env` file containing `HF_ACCESS_TOKEN` for the first URL-ingest deployment

## Deployment

### Recommended workspace workflow

Use the root Makefile for normal stack operations. It applies the Kubernetes
context guard, passes the shared Milvus/NV-Ingest configuration, and keeps data
mutations separate from routine deployment:

```bash
cd /volume2/daedalus/datasets/daedalus-context

make show-config
make stage-data
make seed-data
make run-ingest COLLECTIONS="nvidia kubernetes"
```

- `make stage-data` reconciles URL-ingest resources and copies repository URL
  and PDF inputs to their PVCs without submitting documents.
- `make seed-data` stages the repository snapshot, runs all five collections,
  waits for every Job, and verifies every resulting Milvus collection.
- `make seed-data COLLECTIONS="..."` still stages the complete shared snapshot,
  then populates only the named collections.
- `make run-ingest` skips staging and operates on the snapshot already present
  on the PVCs. Use it to resume a failed population without rewriting inputs.

These commands are synchronous: a zero exit means the requested Jobs reached
`Complete` and the final verification saw a non-empty collection. Job failure,
timeout, missing input, dependency failure, or empty verification returns
non-zero and prints Job, Pod, log, and event diagnostics.

### 1. Build and push the url-ingest image

```bash
make -C .. images-build
make -C .. images-publish
```

The canonical image repository is
`daedalus.ddns.me:5050/btuttle/url-ingest`. Configure
`URL_INGEST_IMAGE=repository:MAJOR.MINOR.PATCH` in the root `.env`; scheduled
crawler containers reject `latest` and non-SemVer deployment tags.

### 2. Deploy to Kubernetes

All resources deploy to the `milvus` namespace. The deploy script copies
`nvcr-imagepullsecret` from `default` into `milvus` and attaches it to the
dedicated `url-ingest` service account before creating workloads.

```bash
# Deploy URL-ingest-owned PVCs, ConfigMaps, Secret, and CronJobs
./deploy.sh

# Copy local repository data to the PVCs without populating Milvus
./deploy.sh --stage-data

# Stage URL inputs, populate Milvus, wait, and verify every collection
./deploy.sh --seed-data

# Populate selected collections from an already staged snapshot
./deploy.sh --run-now nvidia kubernetes
```

`--seed-data` is the reliable bootstrap operation: it stages the repository
snapshot, launches bounded Jobs, waits for success or failure, prints diagnostics
on failure, and verifies that every requested collection contains entities.
`--stage-data` preserves the copy-only primitive. These direct script options
are the lower-level equivalents of the root Make targets above.

Seed source host paths can be overridden when needed:

```bash
URL_INGEST_SOURCE_HOST_PATH=/path/to/url-ingest \
./deploy.sh --seed-data
```

This is a Kubernetes `hostPath` value. The staging Job is pinned with
`URL_INGEST_SOURCE_NODE` so the local paths are never scheduled on a node that
does not contain the checkout. By default, this workspace uses node
`daedalus-01` and paths:

- `/volume2/daedalus/datasets/daedalus-context/url-ingest`

### Reliable population and locking contract

Staging and manual population coordinate through ConfigMap locks and CronJob
suspension:

1. Staging acquires every collection lock plus a staging lock because it rewrites
   shared input PVCs. A run-only operation locks only its requested collections.
2. The affected CronJobs are suspended before manual Jobs start. Their prior
   `spec.suspend` values are recorded rather than assumed to be `false`.
3. Staging refuses to run while any collection has a non-terminal ingest Job.
4. Manual Jobs have an active deadline, are polled for terminal conditions, and
   are followed by a separate collection-verification Job.
5. EXIT cleanup terminates Jobs created by a failed invocation, restores every
   original CronJob state, and then releases the locks.

If a Job cannot be stopped or a CronJob state cannot be restored, the script
intentionally retains the locks and suspension to prevent overlap. Inspect the
active Jobs before manually deleting a `url-ingest-<collection>-run-lock`
ConfigMap. A separate deployment reconcile also respects existing run locks and
will not silently resume their schedules.

### 3. Verify

```bash
# Check CronJobs
kubectl get cronjobs -n milvus -l app.kubernetes.io/name=url-ingest

# Manually trigger a test run (semianalysis is the smallest collection)
kubectl create job url-ingest-test --from=cronjob/url-ingest-semianalysis -n milvus

# Watch logs (init container first, then main)
kubectl logs -f job/url-ingest-test -n milvus --all-containers
```

## PDF Corpus Contract

The independent `../pdf-server` release is the sole owner of nginx, the PVC,
publication Job, PDB, and NetworkPolicy. URL ingest only consumes the existing
`pdf-server-data` claim in the same namespace.

The PDF PVC is organized by collection:

```
pdf-server-data/
  nvidia/          # NVIDIA research papers
  semianalysis/    # SemiAnalysis reports
  kubernetes/      # Kubernetes documentation
  mentalhealth/    # Mental health clinical resources
```

### Publishing the PDF PVC

Use `make publish-corpus` at the workspace root or set
`TARGET=kubernetes PUBLISH_CORPUS=true` when running
`../pdf-server/deploy.sh`. Publication writes and verifies an immutable
generation before switching collection pointers; it does not prune the live
tree first. A legacy physical directory requires an explicit migration.

### Adding new PDFs

Add PDFs to the source corpus, choose a new `CORPUS_GENERATION`, publish it
through `pdf-server`, and then run the affected collection. Do not mutate the
live PVC with an ad hoc helper Pod.

Legacy helper example (for migration planning only):

```bash
# Start a helper pod with the PVC mounted
kubectl run pdf-upload --rm -it \
  --image=alpine:3.21 \
  --overrides='{
    "spec": {
      "containers": [{
        "name": "pdf-upload",
        "image": "alpine:3.21",
        "command": ["sh"],
        "stdin": true,
        "tty": true,
        "volumeMounts": [{
          "name": "pdf-data",
          "mountPath": "/pdfs"
        }]
      }],
      "volumes": [{
        "name": "pdf-data",
        "persistentVolumeClaim": {
          "claimName": "pdf-server-data"
        }
      }]
    }
  }' \
  -n milvus

# From inside the pod:
# Files are at /pdfs/<collection>/
```

Or copy directly with `kubectl cp`:

```bash
# Copy a PDF into the nvidia collection
kubectl cp my-paper.pdf <pdf-upload-pod>:/pdfs/nvidia/my-paper.pdf -n milvus
```

New PDFs will be picked up on the next CronJob run. The manifest ensures only
new files are ingested.

### Browsing PDFs

The pdf-server Service is available within the cluster:

```
http://pdf-server.milvus.svc.cluster.local/pdfs/
```

Directory listing is enabled, so you can browse by collection.

## Idempotency

The pipeline uses two layers to prevent duplicate work:

1. **url-ingest `--skip-existing`**: If the output file already exists on the
   PVC, the HTTP request is skipped entirely. This avoids redundant downloads
   and is respectful of the crawled websites. Downloads are written to a
   temporary file and atomically renamed into place so incomplete writes are not
   treated as valid existing files.

2. **Manifest files**: As submitted files are confirmed complete, the Python
   script appends JSON-lines records to `/manifests/<collection>.ingested`. Each
   record includes path, source type, file size, hash, and ingest version. On
   subsequent runs, files already in the manifest with the same identity are
   filtered out before processing. A local file is never recorded merely because
   a client call returned without an error: its submitted job and completed
   status must map unambiguously back to that exact path. Missing or ambiguous
   accounting fails the chunk and leaves that file eligible for retry.

   Concurrent deterministic shards serialize manifest appends with a file lock,
   refresh only records appended since the last batch, and `fsync` each append.
   Seed/staging Jobs must not create these manifests; they are written only after
   a successful Milvus upload.

Legacy path-only manifest lines are upgraded lazily when encountered so existing
deployments do not re-ingest every historical file at once. For strict
content-hash correctness on previously ingested data, reset the affected Milvus
collection and delete its legacy manifest before rerunning the CronJob.

### Resetting a collection

Normal timeout and partial-failure recovery does **not** require a reset. Rerun
`make run-ingest COLLECTIONS="<collection>"`; the exact completion manifest
preserves successful files and retries unfinished work.

A full rebuild is destructive. Use it only after draining ingestion, confirming
that no collection run lock or non-terminal Job exists, and taking a coordinated
Milvus/manifest backup. To rebuild `nvidia` from the staged snapshot:

```bash
# 1. Back up, then drop only the reviewed Milvus collection with an
# authenticated administrative client.

# 2. Archive the manifest, then remove only that collection's ledger.
kubectl run manifest-reset --rm -it --restart=Never --image=alpine:3.21 \
  --overrides='{
    "spec": {
      "containers": [{
        "name": "reset",
        "image": "alpine:3.21",
        "command": ["rm", "-f", "/manifests/nvidia.ingested"],
        "volumeMounts": [{
          "name": "manifests",
          "mountPath": "/manifests"
        }]
      }],
      "volumes": [{
        "name": "manifests",
        "persistentVolumeClaim": {
          "claimName": "url-ingest-manifests"
        }
      }]
    }
  }' \
  -n milvus

# 3. Use the coordinated manual path so locking, deadlines, diagnostics,
# sharding, and final verification remain active.
cd /volume2/daedalus/datasets/daedalus-context
make run-ingest COLLECTIONS="nvidia"
```

The example removes the ledger only; archive it through your approved backup
path before deletion. To rebuild from a changed repository snapshot, run
`make stage-data` before `make run-ingest`. Do not delete the PVC directory as a
routine retry mechanism.

If Milvus was reset or redeployed and the manifests still exist, the current
ingest script clears a stale manifest automatically when the target collection
is missing. On older deployments, delete the affected manifest files before
triggering the CronJobs; otherwise the ingest container will treat the files as
already uploaded and skip them.

### Milvus drop safety

`DROP_COLLECTIONS_BEFORE_INGEST=true` is intentionally guarded. To use it, also
set:

```bash
CONFIRM_DROP_COLLECTIONS=true
NEVER_DROP_COLLECTIONS=false
```

Only confirmed not-found errors are ignored. Permission errors, connection
failures, timeouts, or unknown Milvus errors stop the job before ingest begins.

## Rate Limiting

| Control                  | Value         | Effect                                    |
|--------------------------|---------------|-------------------------------------------|
| `RAYON_NUM_THREADS`      | `2`           | Max 2 concurrent HTTP connections          |
| `--delay-ms 100`         | 100ms/request | ~10 requests/second per thread             |
| `--skip-existing`        | —             | Zero HTTP requests for already-downloaded  |
| `--request-timeout-secs` | `60`          | Total timeout per HTTP request             |
| `--retries`              | `2`           | Bounded retry attempts for HTTP failures   |
| `--retry-backoff-ms`     | `500`         | Initial retry backoff, doubled per retry   |
| `--allow-failures`       | unset         | Must be explicit to accept partial crawls  |
| `concurrencyPolicy`      | `Forbid`      | No overlapping runs per collection         |
| Staggered schedules      | 1 hour apart  | Collections don't crawl simultaneously     |

NV-Ingest submission has a separate set of bounds. For manual population the
defaults are a 60-second HTTP read timeout, 120 job-result attempts, a 5-second
delay for HTTP 202/not-ready responses, and a six-hour Job deadline. The
connect timeout is capped at five seconds and the transport performs one retry
for transient connection loss. `CLIENT_CONCURRENCY_LIMIT` configures both the
NV-Ingest client worker pool and `Ingestor.ingest`, so it is an actual in-flight
limit rather than only a library hint.

The overall Job deadline remains the final upper bound: each result attempt can
consume its request timeout before the retry delay. Increasing
`MAX_PARALLEL_INGEST_SHARDS` multiplies the per-Job client concurrency and should
be treated as load testing against NV-Ingest, not as a free throughput increase.

## Environment Variables

Set shared values in the workspace root `.env` and URL-ingest overrides in
`url-ingest/.env`. The deployment mirrors the authoritative Milvus credential
Secrets into the workload namespace; credentials are not duplicated in these
files. Root Make variables take precedence over both files. Scheduled
collection batching and concurrency remain explicit in the CronJob manifests;
the deployment-time controls below are injected into immutable manual Jobs.

| Variable                        | Description                                   |
|---------------------------------|-----------------------------------------------|
| `HF_ACCESS_TOKEN`               | HuggingFace token for tokenizer access        |
| `NAMESPACE`                     | URL-ingest workload namespace                 |
| `MILVUS_NAMESPACE`              | Namespace containing Milvus/MinIO             |
| `NV_INGEST_NAMESPACE`           | Namespace containing NV-Ingest                |
| `MINIO_SOURCE_SECRET_NAMESPACE` | Namespace of the authoritative MinIO Secret  |
| `MINIO_SOURCE_SECRET_NAME`      | Authoritative MinIO Secret name               |
| `MINIO_SOURCE_ACCESS_KEY`       | Access-key field in the authoritative Secret  |
| `MINIO_SOURCE_SECRET_KEY`       | Secret-key field in the authoritative Secret  |
| `MILVUS_ROOT_SECRET_NAME`       | Authoritative Milvus root Secret name         |
| `MILVUS_URI`                    | Milvus endpoint                               |
| `MINIO_ENDPOINT`                | MinIO endpoint                                |
| `MINIO_BUCKET`                  | Shared NV-Ingest document bucket              |
| `NV_INGEST_MESSAGE_HOST`        | NV-Ingest service hostname                    |
| `NV_INGEST_MESSAGE_PORT`        | NV-Ingest service port                        |
| `NV_INGEST_VERSION`             | Pinned manual-job client image version        |
| `URL_INGEST_SOURCE_HOST_PATH`   | Host URL snapshot copied during staging       |
| `PDF_SOURCE_HOST_PATH`          | Host PDF snapshot copied during staging       |
| `URL_INGEST_SOURCE_NODE`        | Kubernetes node containing both host paths    |
| `PRUNE_STAGED_DATA`             | Clear each destination collection before copy |
| `READINESS_TIMEOUT_SECONDS`     | Dependency endpoint readiness deadline        |
| `STAGE_URL_TIMEOUT_SECONDS`     | URL staging Job wait deadline                 |
| `STAGE_PDF_TIMEOUT_SECONDS`     | PDF staging Job wait deadline                 |
| `INGEST_JOB_DEADLINE_SECONDS`   | Kubernetes active deadline for a manual Job   |
| `INGEST_WAIT_TIMEOUT_SECONDS`   | Operator-side wait deadline for manual Jobs   |
| `INGEST_POLL_SECONDS`           | Job condition polling interval                |
| `COLLECTION_VERIFY_TIMEOUT_SECONDS` | Non-empty collection verification deadline |
| `NVIDIA_INGEST_SHARDS`          | Deterministic NVIDIA manual-population shards |
| `MAX_PARALLEL_INGEST_SHARDS`    | Maximum simultaneous NVIDIA shard Jobs        |
| `MANUAL_JOB_BACKOFF_LIMIT`      | Kubernetes retry count for a manual Job       |
| `CHUNK_SIZE`                    | Token chunk size (default: 1024)              |
| `CHUNK_OVERLAP`                 | Token overlap between chunks (default: 64)    |
| `EMBEDDING_DENSE_DIM`           | Embedding vector dimensions (default: 2048)   |
| `TOKENIZER`                     | HuggingFace tokenizer model                   |
| `FILE_CHUNK_SIZE`               | Fallback outer file batch size                |
| `URL_FILE_CHUNK_SIZE`           | Outer file batch size for HTML/Markdown       |
| `PDF_FILE_CHUNK_SIZE`           | Outer file batch size for PDFs                |
| `CLIENT_BATCH_SIZE`             | NV-Ingest client internal batch size          |
| `CLIENT_CONCURRENCY_LIMIT`      | Max in-flight NV-Ingest client jobs           |
| `CLIENT_REQUEST_TIMEOUT_SECONDS`| Per-request connect/read timeout               |
| `CLIENT_MAX_JOB_RETRIES`        | Bounded not-ready retries per NV-Ingest job   |
| `CLIENT_RETRY_DELAY_SECONDS`    | Delay between job-result retries              |
| `SAVE_RESULTS_TO_DISK`          | `true` to spool ingest results under `/tmp`   |
| `RESULTS_BASE_PATH`             | Directory for disk-spooled ingest results     |
| `FAILURES_REPORT_PATH`          | JSON report path for ingest failures          |
| `STATUS_REPORT_PATH`            | JSON report path for final run status         |
| `INGEST_MODE`                   | `all` or `single`                             |
| `SINGLE_COLLECTION`             | Collection name (when `INGEST_MODE=single`)   |
| `INCLUDE_PDFS`                  | `true` to also process PDFs in single mode    |
| `MANIFEST_BASE_PATH`            | Directory for `.ingested` manifest files       |
| `INGEST_VERSION`                | Version key included in manifest records      |
| `ALLOW_EMPTY_INPUTS`            | Explicitly allow no-input source runs          |
| `NEVER_DROP_COLLECTIONS`        | `true` to prevent accidental collection drops  |
| `DROP_COLLECTIONS_BEFORE_INGEST`| `1` to wipe collections (local dev only)       |
| `CONFIRM_DROP_COLLECTIONS`      | Required with collection-drop mode             |

The workspace-root `.env` supplies image references and deployment inputs. In
Kubernetes, `URL_FILE_CHUNK_SIZE`, `PDF_FILE_CHUNK_SIZE`, `CLIENT_BATCH_SIZE`,
and `CLIENT_CONCURRENCY_LIMIT` are collection-specific values in
`k8s/cronjob-*.yaml`; changing only `.env` does not override them. Manual Jobs
inherit those collection-specific values and add the deployment-time timeout,
retry, shard, endpoint, and verification overrides before the Pod is created.

## Batching and Failure Behavior

The Python ingest job separates outer file batching from NV-Ingest client
batching. Keep `URL_FILE_CHUNK_SIZE` and `PDF_FILE_CHUNK_SIZE` small enough that
one `Ingestor.files(...)` call fits comfortably in memory, then tune
`CLIENT_BATCH_SIZE` and `CLIENT_CONCURRENCY_LIMIT` for throughput against the
NV-Ingest service. Current Kubernetes defaults favor stability: NVIDIA uses
smaller URL/PDF batches than the smaller collections, and all jobs spool results
to an `emptyDir` scratch volume.

The ingestion entry point includes a compatibility guard for NV-Ingest client
26.3.0: asynchronous result fetches are forced to use the configured read
timeout and HTTP 202 retries are paced by `CLIENT_RETRY_DELAY_SECONDS`. This
prevents an API pod rollout from leaving a seed Job blocked forever on a stale
connection. Its HTTP session pool is sized to the configured worker pool, and
submission errors fail the chunk instead of entering an unbounded wait.

Before extraction or embedding, every ingest and verify Job performs two
authenticated data-plane checks:

- MinIO must contain `MINIO_BUCKET`, and the configured credentials must
  successfully put, stat, and remove a small run-specific canary object.
- The configured Milvus identity must be able to call `has_collection` for
  every target collection using a bounded RPC timeout.

The deploy script separately waits for the Kubernetes Milvus, MinIO, and
NV-Ingest health endpoints. Those endpoint checks prove service readiness; the
in-Job checks prove the mirrored credentials and configured storage path.

Any returned chunk failures are written to `FAILURES_REPORT_PATH` when set, and
the job exits non-zero after processing all chunks. Kubernetes jobs write
failure and status reports under the manifests PVC. Successful chunks, and
partial successes that can be mapped from NV-Ingest status, are still recorded
in `/manifests/<collection>.ingested` so retries focus on unfinished files.

The Rust crawler fails closed when any URL download fails. Use
`--allow-failures` only when a partial crawl is intentional.

### NVIDIA sharding and resume

Manual NVIDIA population defaults to eight deterministic shards. A SHA-256 of
source type plus normalized path assigns each file to exactly one shard, so the
partition is stable across retries. Shard zero runs alone first to establish
collection/manifest state; the remaining shards run in batches limited by
`MAX_PARALLEL_INGEST_SHARDS` (default `1`). Scheduled crawls remain unsharded.

Each shard receives a unique run ID and persistent status/failure report. If a
shard or dependency fails, rerun:

```bash
make run-ingest COLLECTIONS="nvidia"
```

The rerun reconstructs the same shard partition. Exact completion manifests
filter already uploaded files, so completed work is retained and unfinished or
unaccounted files are retried. After every shard succeeds, a separate unsharded
verify Job waits for a non-empty NVIDIA collection. Do not restage while
resuming unless the source snapshot intentionally changed.

## Operational Contracts

### Crawler contract

Inputs:

- Text file of direct URLs or sitemap URLs.
- CLI options controlling sitemap age filtering, country filtering, markdown
  fallback, retry behavior, and output path.

Outputs and side effects:

- HTML or Markdown files under `downloaded_urls/<collection>/` or the configured
  PVC path.
- Non-zero exit when input is invalid, no URLs are found, or downloads fail
  without `--allow-failures`.

Failure behavior:

- HTTP failures are retried up to `--retries`.
- Partial download files are removed and never promoted to final paths.
- Existing final files are skipped only after they were previously completed.

### NV-Ingest contract

Inputs:

- URL files under `URL_BASE_PATH/<collection>/`.
- PDF files under `PDF_BASE_PATH/<collection>/`.
- Required Milvus, MinIO, NV-Ingest, tokenizer, and chunking configuration.

Outputs and side effects:

- Milvus collection writes through NV-Ingest.
- MinIO artifacts through NV-Ingest.
- JSON-lines manifests at `/manifests/<collection>.ingested`.
- Optional failure report at `FAILURES_REPORT_PATH`.
- Optional status report at `STATUS_REPORT_PATH`.

Failure behavior:

- Invalid config fails before side effects begin.
- Empty source discovery fails unless `ALLOW_EMPTY_INPUTS=true`, except that a missing PDF directory is skipped when the same collection also has URL inputs.
- Chunk failures are collected, written to the failure report, and cause a
  non-zero process exit after all chunks are attempted.
- Partial successes are recorded only when the pinned client ledger maps an
  exact submitted and completed file. Locally skipped or ambiguous basenames
  fail the chunk rather than being recorded as complete.

### Kubernetes contract

Inputs:

- Kubernetes manifests under `k8s/`.
- Root/local `.env` values for non-database settings plus authoritative Milvus
  credential Secrets mirrored by `deploy.sh` into `url-ingest-secret`.
- Optional host-path overrides for seed jobs.

Outputs and side effects:

- PVCs for crawled URL data, manifests, and PDF data.
- Collection CronJobs with non-overlapping schedules.
- Dedicated `url-ingest` and `pdf-server` service accounts.
- Optional manual Jobs created from CronJobs.

Failure behavior:

- Seed jobs fail when expected source data is missing or zero files are copied.
- Staging refuses to rewrite PVC inputs while any collection Job is non-terminal.
- Manual run collection names and numeric timeout/shard controls are validated
  before Jobs are created.
- Dependency health, authenticated MinIO write access, and authenticated Milvus
  metadata access must pass before extraction begins.
- Failed and timed-out Jobs emit diagnostics; persistent run-specific status and
  failure reports remain on the manifests PVC.
- Original CronJob suspension states are restored only after created Jobs stop.
  Locks are retained when safe restoration cannot be proven.

## Kubernetes Hardening

The manifests now enforce the following baseline:

- Dedicated service accounts instead of mutating the namespace default service
  account.
- `automountServiceAccountToken: false` for URL ingest and PDF server workloads.
- Pod-level `runAsNonRoot`, `runAsUser`, `runAsGroup`, `fsGroup`, and
  `RuntimeDefault` seccomp profiles where supported.
- Container-level `allowPrivilegeEscalation: false` and dropped Linux
  capabilities.
- Resource requests and limits on CronJobs, seed Jobs, and the PDF server.
- Versioned `url-ingest:0.1.0` image references.
- `nginx:1.27.4-alpine` for the PDF server.
- Readiness and liveness probes for the PDF server.
- `ttlSecondsAfterFinished` for seed Jobs.

## Local Development

### Rust crawler

```bash
cargo build
cargo run -- \
  --input files/k8s_url_links.txt \
  --input-type url \
  --output downloaded_urls/kubernetes \
  --skip-existing \
  --delay-ms 100
```

The crawler exits non-zero if any download fails. For local exploration only,
use `--allow-failures` to accept a partial crawl.

### Image build

```bash
make -C .. images-build
```

The image runs as a non-root `app` user. `.dockerignore` excludes local data,
secrets, Python cache, and build output from the Docker context.

## CI and Validation

GitHub Actions runs:

- `cargo fmt --check`
- `cargo clippy -- -D warnings`
- `cargo test --locked`
- Python compilation and `unittest` discovery for the ingest and Job helpers
- Kubernetes manifest validation with kubeconform
- Bash syntax validation for deployment and Job-control scripts

The workspace-level validation is the preferred local entry point. It checks
the active kubectl context, shell/Python syntax, URL-ingest unit tests,
rendered-manifest client dry-runs, pinned NV-Ingest image/bucket wiring, and the
other retrieval-stack components without launching ingestion Jobs:

```bash
cd /volume2/daedalus/datasets/daedalus-context
export EXPECTED_CONTEXT=kubernetes-admin@kubernetes
make validate
```

Focused URL-ingest checks are:

```bash
cd url-ingest
python3 -m unittest discover -s tests -v
python3 -m py_compile nv-ingest-ms-tasks.py
bash -n k8s/deploy.sh k8s/lib/job-control.sh

cargo fmt --check
cargo clippy --locked -- -D warnings
cargo test --locked

docker run --rm -v "$PWD":/work -w /work \
  ghcr.io/yannh/kubeconform:v0.6.7 \
  -strict -ignore-missing-schemas k8s

make -C .. images-validate
```

The Rust test suite currently covers fail-closed download behavior, atomic file
writes, path sanitization, retry behavior, and Markdown URL variant generation.
Python tests cover bounded NV-Ingest HTTP behavior, retry pacing, real worker
pool sizing, authenticated Milvus/MinIO preflight, deterministic sharding,
locked incremental manifests, exact completed-file accounting, manual Job
rendering, and immediate failed-Job diagnostics.

## File Structure

```
url-ingest/
  src/main.rs                  # Rust url-ingest binary
  Cargo.toml
  Cargo.lock                   # Locked Rust dependency graph
  Dockerfile                   # Builds url-ingest image
  nv-ingest-ms-tasks.py        # Python ingest runner, baked into Dockerfile.runner
  Dockerfile.runner            # Digest-pinned NV-Ingest base plus runner source
  entrypoint-url-ingest.sh     # Image entrypoint source
  ../docker-compose.yaml       # Workspace image build specification
  .dockerignore                # Excludes local data/secrets from Docker builds
  .github/workflows/ci.yaml    # CI guardrails
  .env                         # Local env vars (also used by deploy.sh for secrets)
  deploy.sh                    # Root deployment entry point
  files/                       # URL lists per domain
  downloaded_urls/             # Local download output
  REVIEW_ACTION_PLAN.md        # Review register and remediation roadmap
  tests/                       # Python regression tests for ingest/job control
  k8s/
    deploy.sh                  # Kubernetes deployment implementation
    lib/job-control.sh         # Bounded Job waits and failure diagnostics
    render-manual-job.py       # Immutable manual Job renderer
    pvc.yaml                   # PVCs for downloads and manifests
    secret.yaml                # Template (actual secret created by deploy.sh)
    configmap-url-lists.yaml   # Small URL lists (inline)
    cronjob-nvidia.yaml        # 2 init containers (nvidiadocs + nvidia sitemaps)
    cronjob-kubernetes.yaml
    cronjob-vetpartner.yaml
    cronjob-semianalysis.yaml
    cronjob-mentalhealth.yaml  # PDF-only (no URL crawl)
    seed-data-job.yaml         # One-off: copy downloaded URLs to PVC
    seed-pdf-job.yaml          # One-off: copy PDFs to PVC
```
