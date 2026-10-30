# NV-Ingest Deployment

NV-Ingest provides document extraction for the Daedalus retrieval stack.
[URL ingestion](../url-ingest/README.md) owns refresh scheduling, chunking,
embedding, and collection writes. The active release is
[NV-Ingest chart](chart/README.md) in the shared `daedalus` namespace.

## Configuration and operation

Use the root `.env` and the [root configuration workflow](../README.md#configuration).
The [chart values](chart/values.yaml),
[component lock](../stack/components.lock.yaml), and root [Makefile](../Makefile)
own image versions, replica counts, placement, endpoints, and storage settings.
Milvus, MinIO, and the retriever are external dependencies of this chart.

Existing Secrets provide NGC pull/API credentials and object-store credentials.
The chart references them without putting credential values in Helm. Configure
`IMAGE_STORAGE_URI`, `LOCAL_DOCUMENT_PARSE_ENDPOINT`, and `NV_INGEST_PARSER_IMAGE`.
PDF extraction uses the local parser deployed by this chart. Keep the embedding model aligned
with [stack/rag-schema.yaml](../stack/rag-schema.yaml).

Build and publish the parser separately with
`make images-publish IMAGE_BUILD_SERVICES=nv-ingest-parser`. The image contains
the pinned model weights and custom model code; inference runs with Hugging Face
network access disabled. See [local parser](local-parser/README.md) for its contract.

Run from the repository root after exporting `.env` and the approved literal
`EXPECTED_CONTEXT`:

```bash
make deploy COMPONENTS="milvus retriever nv-ingest"
make health-provider COMPONENTS=nv-ingest
make health-dependencies COMPONENTS=nv-ingest
make health-integration COMPONENTS=nv-ingest
```

If dependencies are already healthy, limit reconciliation to `nv-ingest`.
Before a rollout, inspect active ingestion Jobs and coordinate their completion:
replacing API pods can interrupt their in-flight extraction work. Coordinate this before using the root deployment workflow.

Routine deployment does not publish images or populate collections. Submit
refresh work separately through `make refresh-run COLLECTIONS="..."` when data
writes are intended.

## Readiness and diagnostics

The API readiness endpoint is `/v1/health/ready`. Process readiness, extraction
dependencies, and a completed refresh are different evidence.

The local parser exposes `/readyz` and `/healthz` on its internal Service. Keep
`nv-ingest.envVars.COMPONENTS_TO_READY_CHECK` empty: the legacy NIM checks do not
describe this parser. Verify an extraction through NV-Ingest as well as the
parser probe. Inference failures propagate to the job; there is no remote-model
or native-text fallback.

For a stalled refresh, inspect the refresh report and Job, followed by API and
Redis logs. A rollout is not a substitute for diagnosing request timeouts,
upstream failures, or capacity limits. Redis storage and recovery boundaries
are described in the [root recovery guide](../README.md#recovery).

## Development and validation

Use `make render` or `make validate-chart` in this folder for local inspection
of `chart/`, including its required post-renderer. Run `make test` for parser
and rendered-chart tests. The root `make validate-helm` checks every active
chart, and `make validate-local` runs executable-source validation. Neither
requires a deployment.
