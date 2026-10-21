# Daedalus context Helm releases

Every contextual aid is a separate Helm release in namespace `daedalus`.
There is no umbrella release, so each provider has independent history,
rollback, values, and status while sharing same-namespace service discovery.

| Release | Chart | Depends on |
| --- | --- | --- |
| `hindsight` | `../daedalus-hindsight/charts/daedalus-hindsight` | `hindsight-runtime`, local-etcd storage, Switchyard |
| `milvus` | `helm/milvus` | externally managed Milvus and MinIO Secrets |
| `nemotron-retriever` | `nemotron-retriever` | model access and cache storage |
| `nv-ingest` | `helm/nv-ingest` | Milvus, MinIO, retriever, NGC Secrets |
| `pdf-server` | `helm/pdf-server` | NFS storage |
| `url-ingest` | `helm/url-ingest` | all ingestion providers and existing Secrets |
| `k8s-mcp-server` | `helm/k8s-mcp-server` | Configurable read/write RBAC, auth Secret |
| `unifi-network-mcp-server` | `helm/unifi-network-mcp-server` | UniFi controller and auth Secrets |

The charts follow standard Helm conventions: configuration is in
`values.yaml`, values have JSON schemas, reusable names and labels are helpers,
credentials use `existingSecret` references, and every resource receives the
release instance and `app.kubernetes.io/part-of=daedalus-context` labels.
Persistent corpus and database claims carry `helm.sh/resource-policy: keep`.

## Canonical deployment

Use the root workflow for dependency ordering, context protection, dynamic
image references, URL-list files, and health checks:

```bash
make validate-helm
make verify-shared-secrets
make deploy
```

The root workflow installs releases serially in this order: Hindsight, Milvus,
retriever, NV-Ingest, PDF server, URL ingest, Kubernetes MCP, and UniFi MCP.
URL schedules and external MCP access remain disabled until their explicit
enable targets.

## Direct Helm operation

An operator who needs a focused release can use normal Helm commands. Source
the root `.env` first, use the approved namespace, and preserve the values used
by the root Makefile. Examples:

```bash
make deploy-hindsight

helm upgrade --install milvus helm/milvus \
  --namespace daedalus --create-namespace --atomic --wait

helm upgrade --install nemotron-retriever nemotron-retriever \
  --namespace daedalus --values nemotron-retriever/values-daedalus.yaml \
  --atomic --wait

helm upgrade --install url-ingest helm/url-ingest \
  --namespace daedalus \
  --set-file urlLists.kubernetes=url-ingest/files/k8s_url_links.txt \
  --set-file urlLists.vetpartner=url-ingest/files/vetpartner_url_links.txt \
  --atomic --wait
```

Use `make show-config` and `make -n deploy-<component>` to inspect the exact
environment-specific flags for the other releases. Common inspection commands:

```bash
helm list --namespace daedalus
helm status --namespace daedalus milvus
helm get values --namespace daedalus nv-ingest --all
helm get manifest --namespace daedalus k8s-mcp-server
helm history --namespace daedalus url-ingest
helm status --namespace daedalus hindsight
```

Do not place tokens or passwords in `--set` or values files. The required
Secret names are documented in the root README and each chart README.
Hindsight Secret creation and database-password safety checks remain owned by
the adjacent repository, so use `make deploy-hindsight` instead of putting
those credentials on a Helm command line.

## Dependency locks

`helm/milvus` pins upstream Milvus `5.0.14`; `helm/nv-ingest` pins NVIDIA
NV-Ingest `26.3.0`. Their `Chart.lock` files and vendored chart archives make
the reviewed dependency graph reproducible. To deliberately refresh a lock,
review the version first, then run `helm dependency update` for that wrapper.
The NGC chart repository requires the configured NGC credentials.
