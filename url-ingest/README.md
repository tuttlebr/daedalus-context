# URL and PDF Refresh

This component reconciles source documents into Milvus for Daedalus retrieval.
The deployed entry point is [refresh.py](refresh.py), packaged in
[Dockerfile.runner](Dockerfile.runner). The [Helm chart](chart/README.md)
runs one daily CronJob per collection and a status/metrics service in the shared
`daedalus` namespace.

## Architecture and contracts

The worker discovers URLs and published PDFs, conditionally fetches content,
sends it to NV-Ingest for extraction, then chunks, embeds, and writes Milvus
rows. The Rust crawler in [src/main.rs](src/main.rs) is a standalone download
tool; the deployed refresh CronJobs do not run it as an init container.

| Maintained input | Responsibility |
| --- | --- |
| [Retrieval schema](../stack/rag-schema.yaml) | Model identity, vector dimensions, active database, API contracts |
| [Collection contract](../stack/collections.yaml) | Collection aliases, source types, chunking, schedules, freshness limits |
| [Chart values](chart/values.yaml) | Worker resources, pacing, retries, deadlines, capacity, schedule suspension |
| [Source lists](files/) | Direct URLs and sitemap inputs |
| [Contract renderer](../scripts/render_refresh_contract.py) | Combines those inputs into the deployed refresh contract |

Update authoritative inputs and reconcile `url-ingest` through the root
workflow. Editing generated `url-ingest/chart/files/refresh-contract.json` alone
does not survive the next deployment. Update collection schedules in both the
collection contract and chart values so validation and execution agree.

The collections are `nvidia`, `kubernetes`, `vetpartner`, `semianalysis`, and
`mentalhealth`. Their configured source types determine PDF discovery;
`mentalhealth` is PDF-only and `vetpartner` is URL-only. Source counts and
progress belong in runtime reports, not this guide.

## Deploy and run

Run the following from the repository root after the
[configuration and context setup](../README.md#configuration). Deploy Milvus,
retriever, NV-Ingest, and PDF server first, or verify that those dependencies
are healthy. Required Secrets are managed outside Helm in the shared namespace.

```bash
make deploy COMPONENTS=url-ingest
make health-provider COMPONENTS=url-ingest
```

Deployment regenerates the contract and reconciles templates. It does not start
refresh Jobs. Changes to CronJobs do not change existing Jobs. Initial schedules
are suspended; after collection acceptance, use `make enable-schedules` and
persist `URL_INGEST_SUSPEND=false` in the root `.env`.

Data operations are explicit:

```bash
# Publish PDFs under a unique source revision or digest.
make publish-corpus CORPUS_GENERATION=corpus-v1

# Submit refresh work using the deployed contract.
make refresh-run COLLECTIONS="nvidia kubernetes"
make refresh-status

# Bound a data-writing smoke test; it cannot establish full freshness.
make refresh-run COLLECTIONS=kubernetes REFRESH_LIMIT=1

# Submit a Job that reads and verifies every configured collection.
make refresh-verify
```

`refresh-run` returns after Job creation. `refresh-verify` also returns after
creating its Job; successful submission is not successful verification. Follow
the named Job's completion and logs, then check durable status and the Daedalus
consumer path. The verification worker checks model identity, query API parity,
vector normalization, collection fingerprints, and actual retrieval results.

`make run-ingest` aliases `refresh-run`. `make seed-data` publishes PDFs and
submits refresh Jobs for the explicitly selected `COLLECTIONS`; it does not
wait for them. `make stage-data` publishes PDFs, while URL fetches happen in the
refresh worker. An empty `COLLECTIONS` selects no refresh work.

## Source replacement and removal

The [PDF server](../pdf-server/README.md) owns corpus publication and the
`pdf-server-data` claim. Refresh workers mount that claim read-only. Publish a
new corpus generation before refreshing changed PDFs; do not copy into the
active corpus with an ad hoc pod.

Source hashes and deterministic chunk IDs identify each document version.
[refresh_milvus.py](refresh_milvus.py) verifies replacement rows before removing
the previous version. Failed or interrupted replacements remain resumable and
preserve the previous usable document. Refresh does not drop collections.

HTTP validators avoid downloading unchanged content. Known URLs are rechecked
even when omitted from a sitemap. Repeated 404/410 observations must satisfy the
deletion grace period before vectors are removed. An unavailable PDF directory
is an error. Pending removals preserve vectors and do not advance freshness.

## Concurrency, retries, and freshness

A shared filesystem lock allows one writer per collection, including manually
submitted Jobs. An overlapping attempt reports `skipped_active` without
replacing the owner's status. A manifest-PVC semaphore bounds extraction work
across collections; `URL_INGEST_GLOBAL_CAPACITY` configures the deployed capacity
and manual Jobs inherit it. Do not remove lock files to bypass an active worker.

Host requests are paced. Repeated blocking, throttling, and service-unavailable
responses activate durable cooldowns that honor `Retry-After` and survive pod
retries. Extraction and transport waits are bounded. The worker checkpoints
partial progress before its work budget expires, leaving room before the
Kubernetes deadline. Large backfills can require multiple passes.

Successful source checks can be reused within retries of the same Job. A new
scheduled Job revalidates sources. Failed sources and expired removal grace
periods receive priority. Partial passes, bounded smoke tests, unresolved
failures, and pending removals do not establish full-scan freshness.

## Diagnostics

```bash
make refresh-status
make health-dependencies COMPONENTS=url-ingest
make health-integration COMPONENTS=url-ingest
```

The manifest PVC stores state under `refresh/<generation>/`:

- `status/<collection>.json`: latest collection report;
- `runs/<collection>/`: attempt reports and source-failure journals;
- `collections/<collection>/`: source state, journal, and host cooldowns.

The monitor exposes `/status`, `/metrics`, and `/readyz` on
`url-ingest-monitor:8080`. Readiness describes the process; freshness describes
completed scans. Reports survive Kubernetes Job retention. Inspect raw state
files read-only: constructing `refresh_state.State` can replay and truncate its
journal. See [refresh diagnostics](../.agents/skills/operate-daedalus-context/references/refresh-operations.md)
for evidence and recovery decisions.

## Development

From this directory:

```bash
python3 -m unittest discover -s tests -v
cargo fmt --check
cargo clippy --locked -- -D warnings
cargo test --locked
```

`make test` runs the Python suite and the Docker build's Rust check stage when
the host toolchain is unsuitable. Source tests use fixtures and fake services;
live crawling or ingestion is a separate operation. The refresh worker reuses
bounded extraction helpers from `nv-ingest-ms-tasks.py`; that module is not the
deployed refresh entry point. Deployment uses the root Makefile and `chart/`.
Manual refresh Jobs are created from deployed CronJobs by the root refresh targets.

Build and publish runner changes through the root image workflow, then reconcile
`URL_INGEST_RUNNER_IMAGE`. Keep image publication, deployment, and data work
separate. Run `make validate-local` from the root for executable source changes.
