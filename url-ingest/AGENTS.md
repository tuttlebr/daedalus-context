# URL Ingestion Agent Guidance

Follow the root [AGENTS.md](../AGENTS.md) and
[operations skill](../.agents/skills/operate-daedalus-context/SKILL.md).
[README.md](README.md) describes the current component; the root Makefile owns
normal deployment and data workflows.

## Ownership

- `refresh.py` is the deployed URL/PDF reconciler. `refresh_fetch.py`,
  `refresh_http.py`, `refresh_embedding.py`, `refresh_milvus.py`, and
  `refresh_state.py` implement discovery, pacing, embeddings, storage, and state.
- `refresh_monitor.py` serves status and metrics; `refresh_verify.py` verifies
  query compatibility and retrieval.
- `src/main.rs` is the standalone Rust crawler. Refresh CronJobs do not run it
  as an init container.
- `nv-ingest-ms-tasks.py` supplies extraction/client helpers reused by refresh.
- `chart/`, `../stack/`, and `../scripts/render_refresh_contract.py`
  own deployment and rendered contracts. Update those inputs rather than only
  generated JSON. `files/` contains source lists.

## Invariants

Preserve one writer per collection, durable resume, deterministic replacements,
and verification of new rows before removing old rows. Failed replacements
retain usable data. Partial scans and smoke tests must not mark collections
fresh. A sitemap omission or inaccessible PDF directory is not deletion proof.
Keep the consumer and worker in the same embedding space.

Use fixtures, temporary state, and fake services for tests. Do not instantiate
`refresh_state.State` against production files for read-only inspection: it can
replay and checkpoint journals. Do not start crawls or ingestion to test source
changes without authorization. Preserve source data and unrelated changes.

## Validation

From this directory, run `python3 -m unittest discover -s tests -v`. For Rust
changes, use `cargo fmt --check`, `cargo clippy --locked -- -D warnings`, and
`cargo test --locked`; `make test` also provides the Docker Rust check stage.
Run the root validation gates appropriate to the change. Documentation-only
edits need link and command checks, not live services.

Use Rust 2021 idioms and rustfmt. Python uses four-space indentation and
explicit error handling. Keep secrets out of source, manifests, logs, and test
fixtures. Keep documentation about current behavior; use Git history for
completed reviews and migration narratives.
