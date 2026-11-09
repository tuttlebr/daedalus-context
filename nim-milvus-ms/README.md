# Milvus and Object Storage

The active Daedalus Milvus deployment is [Milvus chart](chart/README.md),
operated by the [root Makefile](../Makefile) in the shared `daedalus` namespace.
This directory owns `chart/`, local rendering commands, and explicit storage
and recovery utilities. Milvus stores retrieval vectors and metadata; its MinIO
service stores segments and the document artifacts shared with NV-Ingest and
Daedalus. The directory name is retained for existing paths and tooling.

## Normal operations

Follow the [root configuration setup](../docs/deployment.md#configuration), then run
from the repository root:

```bash
make deploy COMPONENTS=milvus
make health-provider COMPONENTS=milvus
make health-dependencies COMPONENTS=milvus
make health-integration COMPONENTS=milvus
```

The [active chart values](chart/values.yaml) and
[component lock](../stack/components.lock.yaml) own versions and topology.
The root recipe supplies storage classes and the active MinIO post-renderer.
Review node affinity, storage provisioning, and resource requests in the chart
before deploying outside the reference cluster. The bundled provisioning values
describe that cluster's nodes and storage paths.
Use [root recovery](../docs/operations.md#recovery) for restarts with existing storage.

## Component tools

| File | Purpose |
| --- | --- |
| [deploy-local-storage.sh](deploy-local-storage.sh) | Cluster storage provisioning utility |
| [deploy-local-fast-storage.sh](deploy-local-fast-storage.sh) | Provision the node-local cache class through root `make deploy-local-fast-storage` |
| [deploy-nfs-locking.sh](deploy-nfs-locking.sh) | Configure future NFS mounts through root `make deploy-nfs-locking` |
| [patch-unas-pv-locking.sh](patch-unas-pv-locking.sh) | Update existing PV mount options through root `make patch-nfs-pv-locking` |
| [deploy-monitoring.sh](deploy-monitoring.sh) | Reconciles the separate monitoring release |
| [destroy.sh](destroy.sh) | Confirmed release uninstall; PVC deletion requires an additional confirmation |
| [reset-etcd.sh](reset-etcd.sh) | Confirmed destructive metadata reset |

Read the selected utility before using it. Storage provisioning, monitoring,
and destructive recovery are explicit operations separate from service deployment.
Export the root environment and specify the approved context, release, and
namespace when using a recovery utility.

## Credentials and durable data

Milvus and MinIO use externally managed Secrets. Keep values out of Helm flags,
committed configuration, and logs. Coordinate object-store rotation across
Milvus, NV-Ingest, and consumers, and verify both object access and retrieval.

A Secret edit alone does not change a Milvus password already persisted in its
metadata. Use Milvus's supported password-update operation, synchronize the
consumer credentials through the approved Secret path, and verify metadata
access and a real search with the new credential. A TCP connection or collection
list alone is insufficient evidence of ingestion permissions.

Routine deploys and restarts must preserve PVCs and working endpoints. Metadata
reset removes the collection schemas and references needed to use stored
segments; retaining MinIO objects alone does not recover those references.
Destructive scripts require explicit authorization, a coordinated backup, and
exact release/namespace confirmation. Helm rollback does not restore database
contents, deleted volumes, or credential values.

Node-local volumes remain tied to their nodes. Replicas and disruption budgets
do not eliminate shared storage failure domains. Preserve ordinary LAN clients
when reviewing optional external access. Monitoring and storage provisioning
are separate cluster operations, not prerequisites to rerun on every app update.

## Validation

Use root `make validate-helm` for all active releases. `make render` and
`make validate-chart` in this folder inspect the same chart and post-renderer
locally, using the vendored upstream dependency.
