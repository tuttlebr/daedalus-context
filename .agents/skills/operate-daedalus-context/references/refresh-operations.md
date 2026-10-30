# Refresh Operations

Read this for URL/PDF refresh, stale collections, ingestion recovery, or embedding
contract changes. Resolve links relative to this file; run commands at the repository
root using the context guard in [SKILL.md](../SKILL.md).

## Establish the Current Contract

- [README: continuous refresh](../../../../README.md#continuous-refresh-and-embedding-generations)
  describes ownership and generation compatibility. Milvus stores vectors;
  `url-ingest` schedules and reconciles sources; NV-Ingest extracts content;
  the refresh worker chunks, embeds, and writes it. The retriever serves both
  refresh workers and daedalus-agent.
- [Retrieval schema](../../../../stack/rag-schema.yaml) and
  [collection contract](../../../../stack/collections.yaml) own the model,
  vector space, collection names, schedules, and freshness limits.
- [Chart values](../../../../url-ingest/chart/values.yaml) and
  [CronJob template](../../../../url-ingest/chart/templates/cronjobs.yaml) own
  pacing, retries, work budgets, hard deadlines, and schedule suspension.
- [Contract renderer](../../../../scripts/render_refresh_contract.py) combines
  those inputs with source lists. Update authoritative inputs and regenerate
  the deployed contract through the root workflow; editing generated JSON alone
  is not a durable fix. Check effective schedule suspension during upgrades.

Do not infer model support from checkpoint precision alone. Check the selected
serving backend, GPU architecture, supported kernels, and actual embedding
request path before changing placement or switching model variants.

## Diagnose Before Restarting

Start with `make refresh-status` and
`make health-dependencies COMPONENTS=url-ingest`. Correlate each collection's
durable report, heartbeat, run ID, and last successful scan with its current Job
and logs. Record the affected Job's image and contract generation: updating a
CronJob template does not update Jobs already created from it. Old failed pods
alone do not establish a current outage, and a ready process does not establish
fresh data.

Resolve `REFRESH_STATE_ROOT` (the worker's default is `/manifests/refresh`) and
the active generation from the deployed configuration. Within that root:

- `<generation>/status/<collection>.json` is the latest report.
- `<generation>/runs/<collection>/` retains per-attempt reports and
  `*.failures.jsonl` journals beyond Kubernetes Job retention.
- `<generation>/collections/<collection>/` holds source state, its
  journal, and `http-hosts/` cooldown state. Inspect these read-only during
  diagnosis; do not clear them to obtain a green status.

Read the raw JSON during diagnosis. Constructing
[refresh_state.State](../../../../url-ingest/refresh_state.py) can replay,
checkpoint, and truncate the journal; it is not a read-only inspection helper.

The monitor's `/status` summarizes collections; detailed source errors are in
the run failure journals. Correlate journal errors with worker/dependency logs
to locate the failure stage (discovery, fetch, extraction, embedding, or storage).
Identify response/error type, affected host/source, and whether the same failure
persists across attempts. Separate pipeline defects from upstream access
failures and report unresolved causes explicitly.

| Evidence | Operational decision |
| --- | --- |
| Lock contention or `skipped_active` | Check the owning worker and heartbeat. CronJob `Forbid` does not cover manually created Jobs. A skip must preserve the owner's report and must not advance freshness. Do not unlink a lock file or kill a progressing backfill merely to start another job. |
| Repeated 403, 429, or 503; `waiting_http` | Inspect durable host cooldown and `Retry-After`. Let bounded pacing/backoff operate; manual restarts must not bypass it. A healthy heartbeat during cooldown differs from a dead worker. |
| 404/410 or `pending_removals` | Preserve previous vectors through the configured repeated-observation grace period. Sitemap omission and an unavailable PDF directory are insufficient evidence for deleting documents. |
| Partial pass, limit, or work budget exhausted | Confirm committed progress remains resumable. These states do not advance full-scan freshness; a large initial backfill may need multiple passes. |
| Extraction timeout or empty result | Examine the specific source, extraction result, dependency logs, and configured request timeout before changing it. Large PDFs may need longer than HTML. Keep retries bounded; empty text is not a successful replacement. |
| Retry pod after a source failure | Successful checks are reusable within the same Job; a new scheduled Job must revalidate. Preserve Job identity, manifests, and cached bytes when verifying retry behavior. |

## Recover and Verify Within Scope

After an authorized fix, use `make refresh-run COLLECTIONS="<affected alias>"`
to resume committed progress when needed. `REFRESH_LIMIT=1` bounds a smoke test
but still writes data and never establishes full freshness. Avoid creating
manual retries while an owner is progressing or a host cooldown is active.
Stop manual retrying when the same unresolved cause recurs; retain evidence,
diagnose that cause, and leave bounded automatic retry/backoff in control.

For runner changes, exercise the relevant tests in
[url-ingest/tests](../../../../url-ingest/tests/) and the root validation gates.
Then inspect an actual affected path: e.g. overlap leaves the owner intact,
retry reuses checks, or a formerly failing source reaches verified Milvus rows.
Use `make refresh-verify` only with authorization to create its verification Job;
it checks embedding/retrieval data without ingesting new documents. Both refresh
and verification submission return before completion. Inspect the created Job's
terminal state, logs, and durable reports to establish the outcome. Follow with
the applicable Daedalus integration check when consumer behavior changed.

Preserve the safety invariants in [refresh.py](../../../../url-ingest/refresh.py)
and [refresh_milvus.py](../../../../url-ingest/refresh_milvus.py): one writer per
collection, durable resume, deterministic replacements, and verification of new
chunks before old chunks are removed. A failure must retain the previous usable
data and remain visible; do not equate Job completion or unchanged record counts
with a successful full refresh.

For embedding contract changes, verify consumer database support, embedding
identity, index metric, and reranker score semantics together. Data changes
require explicit authorization and a recovery plan. Never mix vector spaces or
discard the active data before replacement retrieval has been verified.
