# Contributing

Daedalus Context is developed as one stack. A component change is complete when
its source, chart, root workflow, consumer contract, tests, and documentation
agree. Keep the component directories: their names are build contexts and chart
owners, not separate repositories to clone or deploy independently. The two Go modules
retain their existing module/import identities under their component `src/`
directories; builds use the local modules and do not require the former standalone
repositories.

## Local setup

Clone this repository and run commands from its root. Local development does
not require `.env`, Kubernetes credentials, a GPU, private registry access, or
the Agent/Hindsight checkouts. Dependency downloads require network access.

| Tool | Use |
| --- | --- |
| Bash and GNU Make | Root and component workflows (Linux reference environment) |
| Python 3.12 and `venv` | Contract tests, orchestration, ingestion, parser CPU tests |
| Helm 3 | Local chart lint/render; CI pins the version in its workflow |
| Go toolchain from each `src/go.mod` | Kubernetes and UniFi MCP source checks |
| Rust toolchain from CI / crawler Dockerfile | URL crawler formatting, Clippy, tests |
| Docker and Compose v2 | Image checks and the full local validation gate |
| `jq` | Image-reference output and cluster operations |

```bash
python3 -m venv .venv
source .venv/bin/activate
python3 -m pip install -r requirements-dev.txt
pre-commit install
make help
make validate-context
```

[requirements-dev.txt](requirements-dev.txt) contains host test dependencies,
not the production ingestion or model runtime. Those are pinned in component
Dockerfiles. Never substitute a host test environment for the pinned NV-Ingest
runtime when validating extraction compatibility.

## Validation layers

| Command | Scope and prerequisites |
| --- | --- |
| `make validate-context` | Root contracts/tests, retriever and refresh contract tests, seven owned charts; Python and Helm, no adjacent repository |
| `make validate-helm` | Context checks plus Hindsight validation when `hindsight` is selected; requires its configured checkout |
| `make validate-local` | Full local gate, including selected Hindsight validation, Python/Go checks, image specification, and Docker Rust check build |
| `python3 .github/scripts/check_docs.py` | Repository-local Markdown paths and section anchors; does not fetch external links |
| `make images-validate` | Compose build inventory and versioned image references; no push |
| `make validate-cluster` | Selected chart client dry-runs and capacity checks; exported `.env` and approved context required |

For the full local gate without Hindsight:

```bash
make validate-local COMPONENTS="milvus retriever nv-ingest pdf-server url-ingest k8s-mcp unifi-mcp"
```

`COMPONENTS` scopes runtime and external-integration work. The local context
contract gate still checks all seven owned charts so a shared change cannot
silently break another service. Cluster validation is read-only but requires a
real configured cluster; it does not install or prove runtime acceptance.

Use focused checks during development:

```bash
python3 -m unittest discover -s scripts/tests -v
make -C nemotron-retriever test
make -C nv-ingest test
python3 -m unittest discover -s nv-ingest/local-parser -v
python3 -m unittest discover -s url-ingest/tests -v
make -C pdf-server test
make -C k8s-multicluster-mcp check
make -C unifi-network-mcp test
make -C unifi-network-mcp build
```

For crawler changes, run `cargo fmt --check`, `cargo clippy --locked -- -D
warnings`, and `cargo test --locked` in `url-ingest`, or use its Docker `check`
stage. Component `make render` and `make validate-chart` inspect the same charts
used by the root deployment; no cluster access is involved.

The [CI workflow](.github/workflows/ci.yaml) runs owned contracts, Python, Go,
Rust, lint, selected image builds, and security checks. GPU/model inference and
tests that require the pinned NV-Ingest runtime remain separate integration
gates; a skipped runtime test is not evidence of extraction success. The
[parser guide](nv-ingest/local-parser/README.md) explains those checks.

## Change ownership

Use the [stack model](README.md#stack-model) and
[configuration map](README.md#configuration) to find the authoritative input.

- Change shared endpoint/model/collection contracts with their consumers and
  compatibility tests. Matching vector dimensions alone does not make a model
  change safe for existing data.
- Keep chart templates, schemas, values, and root command overrides consistent.
  Test behavior through rendered manifests, fake clients, and disposable state.
- Update source lists and collection policy at their inputs. Do not hand-edit
  generated copies as the only change.
- Keep context-only checks independent of the adjacent Hindsight and Agent
  repositories. Test external integration separately when those checkouts exist.
- Preserve existing LAN endpoints, source ranges, authentication contracts, and
  storage. A code-review task does not authorize deployment or data mutation.

### Generated and vendored files

| Output | Authority / regeneration |
| --- | --- |
| `stack/generated/*-daedalus-context.env` | `scripts/contextctl render-agent-env --environment <name> > <path>`; update both checked-in environments when shared consumer contracts change |
| `url-ingest/chart/files/refresh-contract.json` | `python3 scripts/render_refresh_contract.py --output url-ingest/chart/files/refresh-contract.json` from stack contracts and source lists |
| `url-ingest/chart/files/refresh_monitor.py` | Copy of `url-ingest/refresh_monitor.py`; synchronize when changing the monitor (root deployment also copies it) |
| Milvus/NV-Ingest `chart/charts/*.tgz` | Pinned upstream dependency matching `Chart.yaml` and `Chart.lock`; update intentionally with the lock |
| `stack/components.lock.yaml` | Reviewed release inventory, not a claim that every current source file was built from its recorded revision |

Run `make render-agent-env` to inspect consumer output without changing files.
Keep generated-file checks in CI rather than introducing a second hand-maintained
configuration. Treat vendored upstream material and model artifacts according
to their own provenance and notices.

## Pull requests

Describe the concrete trigger, resulting behavior, and affected components.
Include checks run and any remaining runtime gate. Separate image publication,
cluster rollout, and data acceptance from source-test results. Documentation-only
changes need link/command checks, not a deployment.

Before staging, inspect `git status --short` and `git diff --check`. Preserve
unrelated work. Do not commit credentials, `.env` files, kubeconfigs, downloaded
corpora, model caches, binaries, or local test output. Keep documentation about
current contracts and reusable procedures; Git retains completed review and
migration history.

The repository's original-code license is still undecided. Review the
[publication guide](docs/publication.md) before proposing licensing or
redistribution changes.
