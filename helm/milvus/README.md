# Daedalus Milvus chart

This wrapper pins the upstream Milvus chart and adds Daedalus-owned disruption
budgets. It never creates credentials. Install into the application namespace:

```sh
helm dependency build helm/milvus
helm upgrade --install milvus helm/milvus --namespace daedalus --create-namespace --post-renderer ./helm/milvus/post-render.py --atomic --timeout 20m
```

Required existing Secrets:

- `milvus-root-credentials`: `password`
- `milvus-minio-credentials`: `accesskey`, `secretkey`

Prefer root `make deploy COMPONENTS=milvus`, which always uses the post-renderer.
The pinned MinIO subchart cannot express topology spread. `post-render.py` adds
taint-aware hostname spread without changing storage, credentials, or replica
counts. Four eligible nodes permit one member per node; three permit 2/1/1
placement to restore all members, with reduced tolerance to further node loss.
Use the same post-renderer on every install, upgrade, and render.
