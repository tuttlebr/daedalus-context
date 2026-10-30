# Daedalus NV-Ingest Chart

This chart wraps the pinned NVIDIA dependency in [Chart.yaml](Chart.yaml) and
adds an API disruption budget and local Nemotron Parse inference.
[values.yaml](values.yaml) defines the API, parser GPU placement, Redis,
dependencies, and Secret references.

Use root `make deploy COMPONENTS="milvus retriever nv-ingest"` after the
[root configuration setup](../../README.md#configuration). The root recipe
passes the local parser endpoint and uses the required
[post-renderer](../helm-post-renderer.sh). A bare Helm install omits
those environment-specific inputs.

Required existing Secrets:

- `ngc-secret`: container registry authentication;
- `ngc-api`: `NGC_API_KEY`, `NGC_CLI_API_KEY`;
- `nv-ingest-object-store`: `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY`.

The chart does not create these credentials. See
[NV-Ingest operations](../DEPLOYMENT.md) for readiness, rollout,
and validation. Run root `make validate-helm` to check the active chart.
