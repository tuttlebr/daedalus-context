# Repository Guidelines

## Project Structure & Module Organization

This repository contains a two-stage URL/PDF ingestion pipeline. `src/main.rs` is the Rust `url_ingest` CLI that reads URL lists or sitemaps and writes `.html`/`.md` files. `nv-ingest-ms-tasks.py` is the Python NV-Ingest runner that chunks, embeds, and uploads files to Milvus. `files/` holds source lists, `downloaded_urls/<collection>/` holds crawl output, and `k8s/` contains deployment, PVC, ConfigMap, Secret, CronJob, and seed-job manifests. `Dockerfile`, `Dockerfile.runner`, and `entrypoint-url-ingest.sh` support local container runs through the workspace-root `../docker-compose.yaml`.

## Build, Test, and Development Commands

- `cargo build`: compile the Rust CLI for local development.
- `cargo run -- --input files/k8s_url_links.txt --input-type url --output downloaded_urls/kubernetes --skip-existing`: run a targeted local crawl.
- `cargo fmt --check`: verify Rust formatting before review.
- `cargo clippy -- -D warnings`: catch Rust lint issues.
- `cargo test`: run Rust unit tests; add tests as behavior is introduced.
- `python3 -m py_compile nv-ingest-ms-tasks.py`: quick syntax check for the Python ingest script.
- `docker compose -f ../docker-compose.yaml build url-ingest`: build the production-style Rust image.
- `./deploy.sh --seed-data --run-now`: deploy resources, seed PVCs, and trigger ingest jobs.

## Coding Style & Naming Conventions

Use Rust 2021 idioms and `rustfmt` defaults. Keep CLI flags explicit and compatible with the existing `clap` parser. Python uses 4-space indentation, type hints where practical, module-level constants for defaults, and `logging` for operational messages. Collection names are lowercase and must line up across `files/*_url_links.txt`, `downloaded_urls/<collection>/`, `k8s/cronjob-<collection>.yaml`, and `<collection>.ingested`.

## Testing Guidelines

No dedicated test suite is currently checked in. For Rust changes, add focused unit tests near changed functions in `src/main.rs` and run `cargo test`. For crawl behavior, prefer small fixture URL lists or temporary output directories so tests do not mutate downloaded data. For Python changes, at minimum run `py_compile`; integration validation requires NV-Ingest, Milvus, MinIO, and required environment variables.

## Commit & Pull Request Guidelines

This checkout has no local Git history, so no project-specific commit convention can be inferred. Use concise imperative commits such as `Add manifest reset handling` or `Update Kubernetes crawl schedule`. Pull requests should describe the affected collection or stage, list validation commands, call out Kubernetes or `.env` changes, and include relevant log snippets.

## Security & Configuration Tips

Treat `.env`, Kubernetes secrets, access tokens, MinIO credentials, and Hugging Face tokens as sensitive. Do not paste secrets into logs, examples, PR descriptions, or manifests. Use `--skip-existing` and conservative `RAYON_NUM_THREADS`/`--delay-ms` settings when testing crawls against external sites.
