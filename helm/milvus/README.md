# Daedalus Milvus chart

This wrapper pins the upstream Milvus chart and adds Daedalus-owned disruption
budgets. It never creates credentials. Install into the application namespace:

```sh
helm dependency build helm/milvus
helm upgrade --install milvus helm/milvus --namespace daedalus --create-namespace --atomic --timeout 20m
```

Required existing Secrets:

- `milvus-root-credentials`: `password`
- `milvus-minio-credentials`: `accesskey`, `secretkey`
