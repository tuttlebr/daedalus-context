# Daedalus Milvus Chart

This is the active Milvus release for Daedalus. It wraps the pinned dependency
in [Chart.yaml](Chart.yaml), applies [values.yaml](values.yaml), and adds
application disruption budgets. The root workflow supplies the storage classes
and always uses [post-render.py](post-render.py).

After the [root configuration setup](../../README.md#configuration), run from
the repository root:

```bash
make deploy COMPONENTS=milvus
make health-provider COMPONENTS=milvus
make health-dependencies COMPONENTS=milvus
```

Required existing Secrets are `milvus-root-credentials` (`password`) and
`milvus-minio-credentials` (`accesskey`, `secretkey`). They remain externally
managed. Changing a Secret alone does not rotate a password already stored in
Milvus; coordinate the database credential and its consumers.

The post-renderer supplies taint-aware MinIO topology spread that the dependency
cannot express. Use it for every render or reconciliation. Replica placement
and disruption budgets do not remove shared storage failure domains. Consult
the [recovery guide](../../README.md#recovery) before restarting stateful
workloads. Never reset metadata or delete claims as a health check.

Validate through root `make validate-helm`. The component tools under
[nim-milvus-ms](../README.md) are not the root deployment path.
