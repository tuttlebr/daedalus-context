# URL Ingest Review Action Plan

## Executive Summary

Overall assessment: `url-ingest` is now hardened internal retrieval
infrastructure for `daedalus-agent`, with fail-closed crawling, exact completion
manifests, bounded ingestion Jobs, coordinated staging/population, and
reproducible validation.

Readiness: approved for controlled internal use. The original high-severity
release blockers are remediated and the small-collection live path has passed.
Broader production-like reliance still requires completion and review of the
active full NVIDIA corpus run, plus normal capacity/soak monitoring.

Current guarantees:

- Crawl and staging failures exit non-zero; incomplete download files are never
  promoted to final paths.
- JSON-lines manifests identify exact content and record only files mapped to
  completed NV-Ingest submissions.
- Manual stage/populate operations lock collections, preserve CronJob suspension
  state, wait synchronously, and verify non-empty Milvus collections.
- NV-Ingest HTTP waits, retries, concurrency, Kubernetes deadlines, and
  collection verification are bounded.
- Every ingest/verify Job proves authenticated MinIO write access and Milvus
  metadata access before extraction.

Residual operational risks:

1. Finish the active NVIDIA shard sequence and archive its final verify report.
2. Keep `MAX_PARALLEL_INGEST_SHARDS=1` until NV-Ingest capacity is measured;
   shard parallelism multiplies the per-Job worker pool.
3. Treat retained run-lock ConfigMaps as a safety signal: inspect/terminate
   non-terminal Jobs before manually restoring a schedule.
4. Continue periodic restore/rebuild and successful-rerun/no-duplicate drills.

Implementation status: `URL-001` through `URL-009` are complete. On 2026-07-21,
the live URL and PDF staging Jobs completed, a retried `mentalhealth` ingest and
its explicit verification Job completed, and NVIDIA shard `s00` completed before
`s01` began. At this documentation audit, the NVIDIA run remained active with
its collection lock present and CronJob suspended, which is the intended
non-overlap state. Workspace `make validate` also passed.

## Original Engineering Findings Register

The table below is retained as the historical finding register. Its evidence
locations describe the pre-remediation implementation; the disposition table
immediately following it is authoritative for current status.

| ID | Area | Finding | Impact | Severity | Confidence | Evidence | Recommendation | Owner | Effort | Dependency | Target release |
|---|---|---|---|---|---|---|---|---|---|---|---|
| URL-001 | NV-Ingest / crawl pipeline | Rust crawler logs failed downloads but exits successfully. | Kubernetes init containers can proceed to NV-Ingest with partial or stale crawl data. | High | High | `src/main.rs:193`, `src/main.rs:207` | Exit non-zero on failures by default; add an explicit `--allow-failures` or threshold option if partial success is intentional. | Platform / TBD | S | None | Immediate |
| URL-002 | Idempotency / data correctness | Downloads write directly to final paths while `--skip-existing` trusts file existence. | Killed or failed writes can leave partial files that future runs skip and ingest. | High | High | `src/main.rs:216`, `src/main.rs:277` | Write to temp files, flush, close, then atomic rename; optionally record size/hash metadata. | Platform / TBD | M | None | Immediate |
| URL-003 | Kubernetes / deployment | URL seed Job uses a host path that does not match this checkout and exits 0 when source data is missing. | `--seed-data` can silently seed nothing and still report success. | High | High | `k8s/seed-data-job.yaml:29`, `k8s/seed-data-job.yaml:68` | Make seed source paths configurable and fail closed when expected data is missing. | Platform / TBD | S | Cluster host-path convention | Immediate |
| URL-004 | Milvus / data safety | Collection drop failures are swallowed. | A failed reset can continue ingesting into an old or incompatible collection. | High | Medium | `nv-ingest-ms-tasks.py:232` | Ignore only confirmed not-found errors; otherwise fail before ingest begins. | Platform / TBD | S | Milvus error typing | Immediate |
| URL-005 | NV-Ingest / job contract | Empty discovered inputs return success. | Missing PVCs, bad paths, or bad collection names can look like successful ingests. | Medium | High | `nv-ingest-ms-tasks.py:354` | Add expected-source validation, `MIN_INPUT_FILES`, or explicit no-op mode. | Platform / TBD | S | Collection config decision | Near-term |
| URL-006 | Manifest / data correctness | Manifest tracks only file paths. | Updated content at the same path will not be re-ingested. | Medium | High | `nv-ingest-ms-tasks.py:48`, `nv-ingest-ms-tasks.py:77` | Store content hash, size, mtime, source type, and ingest version. | Platform / TBD | M | Manifest format migration | Near-term |
| URL-007 | Reliability | HTTP clients have no explicit request timeouts. | Bad remote hosts can hang until the 24-hour CronJob deadline. | Medium | High | `src/main.rs:80`, `src/main.rs:262` | Add connect/read/total timeouts and bounded retries with backoff. | Platform / TBD | S | None | Near-term |
| URL-008 | Kubernetes security | Workloads use implicit/default service account behavior and lack security contexts. | Broader token and runtime permissions than needed; containers likely run as root. | Medium | High | `k8s/serviceaccount.yaml:4`, CronJob pod specs | Use dedicated service accounts, `automountServiceAccountToken: false`, and pod/container security contexts. | Platform / TBD | M | NGC image pull handling | Near-term |
| URL-009 | CI/CD / reproducibility | `Cargo.lock` is absent and `url-ingest` deploys as `latest`. | Builds and deployments are not fully repeatable. | Medium | High | `Dockerfile:17`, `k8s/cronjob-nvidia.yaml:28` | Commit `Cargo.lock`; deploy immutable tags or digests. | Platform / TBD | S | Registry tagging convention | Immediate |

### Current disposition and evidence

| ID | Status | Current evidence |
|---|---|---|
| URL-001 | Complete | Crawler is fail-closed by default with explicit `--allow-failures`; Rust regression coverage exercises failure exits. |
| URL-002 | Complete | Downloads use target-directory temporary files and atomic rename; interrupted/failed-write behavior is tested. |
| URL-003 | Complete | Source paths/node are configurable; staging requires non-empty copies, waits for both Jobs, and live URL/PDF staging completed on 2026-07-21. |
| URL-004 | Complete | Collection drops require confirmation and ignore only verified not-found conditions; other errors stop ingestion. |
| URL-005 | Complete | Empty discovery fails unless explicitly allowed for a valid no-input source/shard; exact file accounting also rejects local skips. |
| URL-006 | Complete | Locked JSON-lines records include path, source type, ingest version, size, mtime, and SHA-256; legacy entries upgrade lazily. |
| URL-007 | Complete | Crawler and NV-Ingest result paths have explicit bounded timeouts, retries, pacing, worker pools, and Job deadlines. |
| URL-008 | Complete | Dedicated service accounts, disabled token automounting, non-root/security contexts, resources, and pinned base images are present. |
| URL-009 | Complete | `Cargo.lock`, versioned `url-ingest:0.1.0`, pinned NV-Ingest 26.3.0, CI, unit tests, and rendered-manifest validation are present. |

## Historical Architectural Themes and Resolution

### Theme 1: Success state was too implicit

The original pipeline relied on process exit and log inspection rather than
explicit job states. This affected crawl completion, empty input handling,
partial ingestion, and manual seed jobs.

Resolution: manual runs now have unique IDs, persistent `running`, `failed`, and
`complete` reports, terminal Job conditions, exact failure reports, and a
separate Milvus verification Job. Further state vocabulary can evolve without
blocking current controlled use.

### Theme 2: Idempotency was path-based

`--skip-existing` and the original manifests reduced duplicate work, but both
assumed path existence equaled valid, previously ingested content.

Resolution: manifest identity now includes normalized path, source type, size,
SHA-256, and ingest version. Writes are locked and durable, and only exact
completed submissions are recorded. Stable source URI remains a future
cross-system enhancement rather than a release blocker.

### Theme 3: Kubernetes deployment needed hardening

The original manifests had schedules, resource requests, limits, PVCs, and
basic non-overlap controls, but lacked dedicated service accounts, security
contexts, versioned image references, explicit seed inputs, and reproducible
manifest validation.

Resolution: the URL-ingest/PDF workloads now use the shared hardening baseline,
bounded Jobs, immutable manual Job rendering, locks, restored CronJob state, and
versioned images.

### Theme 4: Configuration needed stronger validation

The original Python script loaded required environment variables but did not
fully validate numeric ranges, collection expectations, schema assumptions, or
dangerous flags before side effects.

Resolution: shell and Python entry points validate collection names, numeric
ranges, shard bounds, dangerous drop combinations, and required values before
document submission. Authenticated storage preflight then fails before costly
extraction/embedding.

## Historical Remediation Roadmap

The original roadmap is retained below for traceability. Its repo-level items
are implemented; remaining work is operational evidence/soak rather than
unimplemented release-blocking code.

### Immediate: 0 to 2 weeks

Goal: reduce data-loss and deployment-risk exposure.

- Fix `URL-001` so crawler failures fail the init container unless explicitly allowed.
- Fix `URL-002` with temp-file writes and atomic rename.
- Fix `URL-003` so seed jobs fail closed and use configurable source paths.
- Fix `URL-004` so failed Milvus drops halt ingestion.
- Fix `URL-009` by committing `Cargo.lock` and replacing `latest` with explicit SemVer image references.
- Add CI checks for `cargo fmt --check`, `cargo clippy -- -D warnings`, `cargo test`, `python3 -m py_compile`, and Kubernetes YAML validation.

### Near-term: 2 to 6 weeks

Goal: make the ingestion pipeline repeatable and supportable.

- Add config validation for numeric ranges, source expectations, collection names, and dangerous flags.
- Extend manifests to include hash, size, source URI, source type, and ingest config version.
- Add timeout and retry policy to crawler HTTP calls.
- Add dedicated Kubernetes service accounts and security contexts.
- Add integration tests that cover missing PVC/input, partial failure, retry after timeout, and stale manifest behavior.
- Add structured run identifiers to logs and failure reports.

### Strategic: 6 to 12 weeks

Goal: make this a reusable ingestion platform component.

- Introduce a versioned source-document model shared by crawl, ingest, and retrieval validation.
- Define Milvus schema/index/version metadata and a rebuild/migration path.
- Move duplicated CronJob env into Helm or Kustomize overlays.
- Add policy-as-code checks for image pinning, service accounts, security contexts, resource limits, and secret handling.
- Add an operator runbook for deploy, rollback, reset, re-ingest, and incident triage.

## Release Readiness Checklist

### Gate 1: Remediation review

- [x] High-severity findings `URL-001` through `URL-004` are fixed.
- [x] `URL-009` is fixed.
- [x] Fixes include focused tests and reproducible validation.
- [x] README and operational notes reflect changed behavior.

### Gate 2: Integration review

- [x] Deploy/reconcile manifests in the live internal namespace.
- [x] Run the staged `mentalhealth` collection through NV-Ingest.
- [x] Confirm the explicit verify Job observes a non-empty Milvus collection.
- [ ] Complete a controlled successful rerun and compare entity/manifest counts
      to add live no-duplicate evidence beyond regression tests.
- [x] Cover missing input and incomplete file accounting with fail-closed tests.
- [x] Cover partial crawl failure with fail-closed Rust tests.
- [x] Confirm reports/logging include collection, run ID, shard/chunk, source
      type, elapsed time, and failure details.
- [ ] Complete the active full NVIDIA shard sequence and archive the final
      verification report.

### Gate 3: Wider internal usage

- [x] Security posture reviewed.
- [x] Dedicated service accounts and security contexts are in place.
- [x] CI/CD is reproducible.
- [x] Failure cleanup and re-ingest/resume procedures are documented.
- [x] Data reset/rebuild process is documented.
- [x] Known residual risks are recorded above for controlled internal use.

## Historical GitHub Execution Backlog

All issues below are complete and retained as the original implementation
briefs. The current disposition table links them to present evidence.

### Issue: Fail closed when URL crawl has download failures

Labels: `severity/high`, `area/nv-ingest`, `area/kubernetes`, `type/bug`, `release-blocker`

Problem: The Rust crawler exits successfully even when one or more URL downloads fail.

Desired outcome: Kubernetes init containers fail by default when crawl output is incomplete.

Acceptance criteria:

- A crawl with any failed download exits non-zero by default.
- An explicit option can allow partial success if needed.
- Summary logs report total, succeeded, skipped, and failed counts.
- Unit or fixture-based test covers failure-exit behavior.

### Issue: Make downloaded file writes atomic and skip-safe

Labels: `severity/high`, `area/nv-ingest`, `type/bug`, `release-blocker`

Problem: Direct writes to final output paths can leave partial files that future `--skip-existing` runs trust.

Desired outcome: Only complete files appear at final output paths.

Acceptance criteria:

- Downloads write to a temporary path in the target directory.
- Successful downloads are atomically renamed to the final path.
- Failed downloads remove temporary files.
- Test covers interrupted or failed write behavior where practical.

### Issue: Make seed jobs fail closed and configurable

Labels: `severity/high`, `area/kubernetes`, `type/bug`, `release-blocker`

Problem: Seed jobs rely on fixed host paths and can exit successfully with no copied data.

Desired outcome: Seed jobs either copy expected data or fail with a clear reason.

Acceptance criteria:

- Source paths are configurable.
- Missing expected source data fails the job unless explicit no-op mode is set.
- Logs show source path, destination path, collection counts, and copied totals.

### Issue: Harden Milvus collection reset behavior

Labels: `severity/high`, `area/milvus`, `area/nv-ingest`, `type/bug`, `release-blocker`

Problem: Collection drop errors are swallowed before ingest proceeds.

Desired outcome: Failed destructive operations block ingestion unless the error is confirmed benign.

Acceptance criteria:

- Not-found drop errors are handled idempotently.
- Permission, connection, timeout, or unknown drop failures exit non-zero.
- Test or documented validation covers both paths.

### Issue: Add platform CI guardrails

Labels: `severity/high`, `area/ci-cd`, `type/test`, `release-blocker`

Problem: The repo has no enforced validation path for Rust, Python, manifests, or reproducible dependencies.

Desired outcome: Obvious regressions fail before deployment.

Acceptance criteria:

- CI runs Rust format, lint, and tests.
- CI runs Python syntax checks.
- CI validates Kubernetes YAML.
- `Cargo.lock` is committed.
- Deployable images use controlled tags or digests.
