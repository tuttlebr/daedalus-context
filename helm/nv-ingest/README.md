# Daedalus NV-Ingest chart

This wrapper pins NVIDIA NV-Ingest, keeps credentials outside Helm, and adds
the API disruption budget. The authenticated dependency and the reviewed
post-renderer are required:

```sh
helm dependency build helm/nv-ingest \
  --username '$oauthtoken' --password "$NGC_API_KEY"
helm upgrade --install nv-ingest helm/nv-ingest \
  --namespace daedalus --create-namespace \
  --post-renderer nv-ingest/helm-post-renderer.sh \
  --atomic --timeout 20m
```

Required existing Secrets:

- `ngc-secret`: `kubernetes.io/dockerconfigjson`
- `ngc-api`: `NGC_API_KEY`, `NGC_CLI_API_KEY`
- `nv-ingest-object-store`: `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY`
