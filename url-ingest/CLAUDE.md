# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project layout

This is a two-stage RAG ingestion pipeline composed of two independent programs that share a filesystem:

- **`src/main.rs`** — A Rust binary (`url_ingest`) that downloads HTML/Markdown from URL lists or sitemaps to disk. Single source file. Built with `cargo` and packaged via `Dockerfile`.
- **`nv-ingest-ms-tasks.py`** — A Python script that discovers files on disk, sends them through the NV-Ingest pipeline (extract → split → embed → upload), and writes manifests. `Dockerfile.runner` bakes it into a release image based on the SemVer-tagged NV-Ingest runtime.
- **`k8s/`** — Manifests for the production deployment. The two programs run as one CronJob per collection: the Rust crawler is an `initContainer`, the Python ingest is the main `container`, and they share PVCs.
- **`files/*.txt`** — Input URL lists / sitemap lists (one URL per line, `#` comments allowed).

The workspace `docker-compose.yaml` defines image builds only. Kubernetes is the runtime deployment path.

## Common commands

### Building the Rust crawler

```bash
# Local debug build
cargo build

# Local release build
cargo build --release

# Workspace image build and publish
make -C .. images-build
make -C .. images-publish
```

There is no test suite (no `tests/`, no `#[test]` functions, no pytest config). `cargo check` and `cargo clippy` are the fastest correctness signals.

### Running the crawler locally

The binary takes a URL list or sitemap list and writes files into a host-subdirectory tree:

```bash
cargo run --release -- \
  --input files/semianalysis_url_links.txt \
  --output downloaded_urls/semianalysis \
  --input-type sitemap \
  --max-age-days 365 \
  --skip-existing \
  --delay-ms 100
```

Key flags worth knowing:
- `--input-type auto|sitemap|url` — `auto` guesses based on the file content (lines containing `sitemap` or ending in `.xml` are treated as sitemaps).
- `--try-markdown` — Probe for a `.md` variant before falling back to `.html`. Used for `nvidiadocs` (which serves `.md` source files).
- `--skip-existing` — Skip the HTTP request entirely if the target file is already on disk. Essential for idempotency; always set in production.
- `--max-age-days N` — Sitemap-only filter: drop URLs whose `<lastmod>` is older than N days. URLs with missing/unparseable lastmod are kept.
- `--allow-all-countries` — Disable the default TLD allowlist (`.us/.gov/.mil/.edu/.com/.net/.org/.io/.ai/.dev/.tech/.space/.info`).

`RAYON_NUM_THREADS` controls concurrent HTTP workers. Production sets `2`; combined with `--delay-ms 100` this yields ~20 req/s upper bound.

### Deploying to Kubernetes

All deploys go through `k8s/deploy.sh`. The script is the source of truth for which ConfigMaps, Secrets, and Jobs exist — there is no Helm chart or kustomization.

```bash
cd k8s
./deploy.sh                          # Apply manifests only
./deploy.sh --seed-data              # Also copy local downloads/PDFs into PVCs
./deploy.sh --run-now                # Trigger all collections immediately after deploy
./deploy.sh --run-now semianalysis   # Trigger one collection (smallest, good for smoke tests)
./deploy.sh --seed-data --run-now    # Full bootstrap: deploy, seed PVCs, kick ingest
```

The target namespace is **`milvus`** (the script hardcodes it). The script also copies `nvcr-imagepullsecret` from the `default` namespace into `milvus` on every run, so it must exist there.

### Triggering and observing jobs

```bash
# List the deployed CronJobs
kubectl get cronjobs -n milvus -l app.kubernetes.io/name=url-ingest

# Manually trigger any collection
kubectl create job url-ingest-test --from=cronjob/url-ingest-semianalysis -n milvus

# Follow both init and main container logs
kubectl logs -f job/url-ingest-test -n milvus --all-containers
```

## Architecture: how the pieces fit together

### Two-container CronJob shape

Every CronJob has the same shape: one or more `initContainers` running `url_ingest` write into the `url-ingest-data` PVC, then a single `container` running `nv-ingest-ms-tasks.py` reads from that PVC plus the `pdf-server-data` PVC and writes to Milvus + MinIO. The init container is the only writer to the URL PVC; the main container mounts it read-only.

`cronjob-nvidia.yaml` is the most complex example — it has **two** init containers (one for `nvidiadocs` sitemaps with `--try-markdown`, one for `nvidia` sitemaps) that both write into `/downloaded_urls/nvidia/`. The mental-health collection has **no** init container at all (PDF-only).

### Single-collection ingest mode

In K8s, each CronJob runs the Python script with `INGEST_MODE=single` and `SINGLE_COLLECTION=<name>` so it only touches one Milvus collection. `INCLUDE_PDFS=true` adds PDF discovery for that collection. Without these vars, the script iterates over `URL_COLLECTIONS` and `PDF_COLLECTIONS` (`kubernetes,mentalhealth,nvidia,semianalysis,vetpartner`) and processes everything.

### Idempotency: two layers, do not break

1. **Crawl layer**: `--skip-existing` in the Rust binary skips the HTTP request when the output file exists. The file naming scheme (`url_to_path_with_ext` in `src/main.rs`) is `<host>/<path-with-slashes-as-underscores><sanitized-query>.<html|md>`. Changing this naming breaks dedup across runs.
2. **Ingest layer**: `IngestManifest` (in `nv-ingest-ms-tasks.py`) appends absolute file paths to `/manifests/<collection>.ingested` on the `url-ingest-manifests` PVC *only after a chunk completes with zero failures*. A chunk with any failure is not marked, so it will be retried on the next run. **Seed jobs must not write manifests** — only successful ingests should.

When a Milvus collection is missing but its manifest exists (e.g. after a Milvus reset), `build_manifest()` clears the stale manifest automatically. There's no equivalent "PVC files exist but manifest is gone" guard — re-ingestion will re-upload everything.

### Python script delivery

`nv-ingest-ms-tasks.py` is baked into `Dockerfile.runner`. Editing it requires a
new runner build, test, publish, and
`URL_INGEST_RUNNER_IMAGE=repository:MAJOR.MINOR.PATCH`. Crawler changes similarly
require `URL_INGEST_IMAGE=repository:MAJOR.MINOR.PATCH`. Deploy refuses
unversioned or missing crawler and runner images.

### Production safety rails

- `NEVER_DROP_COLLECTIONS=true` is set in every K8s CronJob. This overrides `DROP_COLLECTIONS_BEFORE_INGEST` and prevents accidental wipes.
- `concurrencyPolicy: Forbid` on every CronJob — overlapping runs are skipped, not queued.
- `backoffLimit: 0` — a failed job does not auto-retry; it waits for the next scheduled run.
- Schedules stagger one hour apart (Sun 2-6am) so the crawlers don't compete for NV-Ingest GPU time.

### URL list delivery

Small lists (`nvidiadocs`, `nvidia`, `semianalysis`) ship inline in `k8s/configmap-url-lists.yaml`. The large lists (`k8s_url_links.txt`, `vetpartner_url_links.txt`) are too big for the annotation size limit on a client-side apply, so `deploy.sh` creates them via **server-side apply**. If you add a new large list, follow that pattern.

## Sources of truth and gotchas

- `.env` at the project root is required for `deploy.sh`; it supplies image settings and the HF/MinIO credentials used to create the Kubernetes Secret.
- `MINIO_BUCKET` is set in the root `.env` and rendered into the Kubernetes Jobs (currently `nv-ingest`).
- Sitemap parsing tolerates malformed `lastmod` and missing entries; the parser tries RFC 3339, then `%Y-%m-%dT%H:%M:%S`, then `%Y-%m-%d`. URLs with unparseable lastmods bypass `--max-age-days` filtering.
- The `is_us_host` function (despite the name) actually permits common gTLDs (`.com`, `.io`, `.ai`, etc.) — it is a coarse non-CCTLD allowlist, not a US-only filter.
- The crawler's `User-Agent` is hardcoded as `nv-ingest/26.1.2` in `src/main.rs`.
