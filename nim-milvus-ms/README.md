# nim-milvus-ms

Thin Helm deployment wrapper for Milvus, used as vector database infrastructure for the `daedalus-agent` retrieval stack.

This repository does not contain application code. The active operational surface is the root-level shell scripts and Helm values files. The `milvus-deploy/` directory is an older vendored Milvus chart kept only for reference.

## Current Deployment Contract

- Routine deploys are non-destructive.
- Milvus metadata reset is a separate explicit operation.
- Release uninstall is a separate explicit operation.
- PVC deletion always requires explicit confirmation.
- The active chart is pinned to `zilliztech/milvus` chart `5.0.14`, matching Milvus `v2.6.11`.
- Services are internal `ClusterIP` by default.
- Stable NodePorts are opt-in through `nodeport-values.yaml`.
- MinIO credentials and the Milvus root password come from Kubernetes secrets; object-store credentials are never written to Helm values or the Milvus ConfigMap.
- Every active Milvus component and the MinIO server read the same MinIO Secret.
- Secret resource-version checksums are placed on pod templates so rerunning `deploy.sh` after rotation performs rolling restarts without exposing secret data.
- Milvus keeps the built-in `root` account on its supported superuser path (`rootShouldBindRole: false`), so an upgraded metadata store cannot lock root out because of a missing historical role binding. The wrapper does not mutate Milvus metadata directly in etcd.
- The steady-state Milvus application topology is three proxies, three query nodes, three data nodes, three streaming nodes, and one mix coordinator. etcd has three members and distributed MinIO has four.
- The five Milvus application Deployments use best-effort hostname topology spread. etcd and MinIO each have a PodDisruptionBudget with `maxUnavailable: 1`.
- Distributed MinIO starts with four replicas and explicitly selects a configurable StorageClass.
- A new release starts etcd and MinIO in a dependency-only Helm revision before the five Milvus component types are rolled out, avoiding first-boot DNS and readiness races.
- MinIO creates both `milvus-bucket` and the configurable `nv-ingest` document bucket through an idempotent Helm hook.
- etcd and MinIO PVC templates receive release and component labels through the Helm post-renderer so reset and destroy operations have precise selectors.
- Rendered manifests are validated by `bash validate.sh`.

## Files

| File | Purpose |
|---|---|
| `deploy.sh` | Safe `helm upgrade --install` path for the `milvus` release. |
| `destroy.sh` | Explicitly confirmed uninstall path for the Milvus release. PVCs are preserved unless separately confirmed. |
| `reset-etcd.sh` | Explicitly confirmed destructive reset path for Milvus metadata. Deletes etcd PVCs only. |
| `validate.sh` | Renders the pinned chart and checks deployment guardrails. |
| `custom-values.yaml` | Active Helm values overlay. |
| `nodeport-values.yaml` | Optional stable NodePort overlay. |
| `monitoring-values.yaml` | Optional ServiceMonitor overlay for Prometheus Operator clusters. |
| `kube-prometheus-stack-values.yaml` | Global monitoring release reconciliation; preserves the active Prometheus volume and Service route. |
| `deploy-monitoring.sh` | Guarded reconciliation of the existing kube-prometheus-stack Helm release. |
| `pvc-label-post-renderer.sh` | Adds release/component lifecycle labels to etcd and MinIO PVC templates. |
| `default-values.yaml` | Reference snapshot from upstream chart `5.0.14`. Not applied by deploy. |

## Runtime Topology And Availability

| Workload | Replicas | Availability control |
|---|---:|---|
| Proxy | 3 | Best-effort hostname spread |
| Query node | 3 | Best-effort hostname spread |
| Data node | 3 | Best-effort hostname spread |
| Streaming node | 3 | Best-effort hostname spread |
| Mix coordinator | 1 | Best-effort hostname spread; single control-plane replica |
| etcd | 3 | PDB with `maxUnavailable: 1` |
| MinIO | 4 by default; minimum 4 | PDB with `maxUnavailable: 1` |

The application topology constraint uses `whenUnsatisfiable: ScheduleAnyway`. Kubernetes will prefer to spread the five Milvus Deployment types across `kubernetes.io/hostname`, but it may co-locate replicas when cluster capacity or placement constraints require it. Its selector groups all `app.kubernetes.io/name=milvus` pods, so it balances the application fleet in aggregate rather than guaranteeing that every replica of each component lands on a different node. No application-component PDBs currently render; only etcd and MinIO have explicit voluntary-disruption protection. PDBs do not protect against node loss, storage failure, or simultaneous involuntary outages.

The single mix coordinator is an availability limitation. The three- and four-replica counts improve process and node-level resilience only when the scheduler can place replicas on independent nodes and the storage backend does not remain a shared failure domain.

## Prerequisites

- `helm`
- `kubectl`
- Access to the target Kubernetes context
- Helm repo entry for `zilliztech`
- Required Kubernetes secrets in the target namespace

Check the Helm repo:

```bash
helm repo list
helm repo add zilliztech https://zilliztech.github.io/milvus-helm/
helm repo update
```

## Required Secrets

Create secrets before the first real deploy:

```bash
kubectl create namespace milvus

kubectl -n milvus create secret generic milvus-minio-credentials \
  --from-literal=accesskey='<access-key>' \
  --from-literal=secretkey='<secret-key>'

kubectl -n milvus create secret generic milvus-root-credentials \
  --from-literal=password='<root-password>'
```

The MinIO secret must contain non-empty, non-default keys named `accesskey` and `secretkey`; `minioadmin` is rejected. The Milvus root secret must contain a non-empty key named `password`. `deploy.sh` checks all three keys before invoking Helm.

Pre-provisioning the Secrets is preferred. If either Secret is absent, `deploy.sh` can create it from `MINIO_ACCESS_KEY`/`MINIO_SECRET_KEY` or `MILVUS_ROOT_PASSWORD`; those values should live only in an untracked `.env` file or the caller's environment. Secrets created this way are applied directly with `kubectl`, not owned by the Helm release. Deploy, rollback, reset, and uninstall therefore do not delete or restore them.

Custom secret names are supported:

```bash
MINIO_SECRET_NAME=my-minio-secret \
MILVUS_ROOT_SECRET_NAME=my-milvus-root-secret \
bash deploy.sh
```

The wrapper hashes only Secret names and Kubernetes resource versions—not credential values—and writes those SHA-256 checksums to the affected pod templates. After changing the MinIO Secret, rerun `bash deploy.sh`; this rolls both MinIO and all Milvus components. Treat MinIO rotation as a coordinated maintenance operation until the rollout and both buckets have been verified from Milvus, NV-Ingest, and a RAG consumer.

The checksum mechanism is rollout plumbing, not a complete Milvus user-password rotation workflow. For a persisted Milvus deployment, editing `milvus-root-credentials` does not invoke Milvus's required `update_password` operation. Rotate root in a controlled maintenance window:

1. Retain the old credential securely and ensure a tested administrative recovery identity exists.
2. Authenticate with the old credential and call the Milvus `update_password` API for `root`.
3. Verify a new authenticated connection can list and describe a known collection.
4. Update `milvus-root-credentials` and every authorized namespace-local consumer copy, then rerun `bash deploy.sh` and the consumer deployment.
5. Re-run end-to-end health and retrieval checks before restoring normal traffic.

The official Milvus documentation describes [`defaultRootPassword` and `rootShouldBindRole`](https://milvus.io/docs/configure_common.md) and the supported [`update_password` operation](https://milvus.io/docs/users_and_roles.md). Never treat a Secret-only edit as proof that the persisted Milvus credential changed.

Kubernetes Secrets are namespace-scoped. Consumers in `daedalus` cannot reference the `milvus` namespace Secrets directly; the consumer deployment must synchronize equivalent credentials into its namespace through its approved Secret-management path. Do not put copied credentials into Helm values or committed environment files.

## Configuration And Precedence

Copy `.env.example` to an untracked `.env` only when local configuration is needed. `deploy.sh` loads configuration in this order, with later sources winning:

1. The workspace `.env` at `../.env`.
2. This directory's `.env`.
3. Environment variables explicitly passed by the caller.

For example, `DRY_RUN=true bash deploy.sh` overrides either file. `destroy.sh` and `reset-etcd.sh` intentionally do **not** load these files. When a release uses a non-default namespace or release name, pass both values explicitly to destructive commands as shown below.

`milvus-bucket` is fixed as Milvus's segment bucket. `MINIO_DOCUMENT_BUCKET` configures the second bucket used for source documents and defaults to `nv-ingest`. Changing `MINIO_DOCUMENT_BUCKET` creates or selects a different bucket on the next deploy; it does not migrate objects from the old bucket or remove that bucket. Update all producers and RAG consumers together before changing it.

Use one Milvus release per namespace. Several chart resources and selectors use fixed names, and the optional NodePorts are cluster-wide. Separate test or recovery releases into their own namespaces and choose different NodePorts when exposure is required.

## Validate

Run validation before deploying:

```bash
bash validate.sh
```

Validation checks that:

- The chart renders as `milvus-5.0.14`.
- Milvus renders as `v2.6.11`.
- MinIO uses an existing secret instead of generated default credentials.
- The rendered Milvus ConfigMap contains no MinIO credential values and no `minioadmin` defaults.
- All five active Milvus component types source both MinIO credential variables from the configured Secret.
- All active Milvus and MinIO pod templates carry safe Secret-revision checksums for rotation-triggered rollouts.
- Distributed MinIO renders at least four replicas.
- etcd and MinIO PVC templates select their configured StorageClasses and carry lifecycle labels.
- The MinIO bootstrap hook creates `milvus-bucket` and the configured document bucket.
- The dependency-only cold-start render keeps the three-member etcd cluster and four-member MinIO cluster while scaling all five Milvus application component types to zero.
- Milvus auth is enabled.
- Milvus and etcd use named service accounts.
- Default services are not `LoadBalancer`.
- Default services do not expose stable NodePorts.
- The optional NodePort overlay renders Milvus `30416` and MinIO `30900`.
- The vendored reference chart still passes `helm lint`.

## Deploy Or Upgrade

Default deployment:

```bash
bash deploy.sh
```

Common overrides:

```bash
NAMESPACE=dev bash deploy.sh
DRY_RUN=true bash deploy.sh
HELM_TIMEOUT=30m bash deploy.sh
MINIO_REPLICAS=4 bash deploy.sh
MINIO_STORAGE_CLASS=nfs-client ETCD_STORAGE_CLASS=local-etcd bash deploy.sh
MINIO_DOCUMENT_BUCKET=nv-ingest bash deploy.sh
COLD_START_DEPENDENCY_BOOTSTRAP=false bash deploy.sh  # opt out only when dependencies are managed elsewhere
```

`deploy.sh` uses:

- `helm upgrade --install`
- `--version 5.0.14`
- `--wait`
- `--atomic` for real deployments
- a dependency-first, two-revision cold start for brand-new releases
- `custom-values.yaml`
- `pvc-label-post-renderer.sh`

It does not uninstall the release, delete PVCs, or write raw Milvus metadata into etcd. Root role binding is controlled declaratively by Milvus configuration.

`rootShouldBindRole: false` is intentional. It keeps Milvus's built-in `root` identity on the built-in superuser path while authorization remains enabled and public privileges remain disabled. Setting it to `true` makes root authorization depend on persisted RBAC role membership; metadata upgraded from an older deployment may not contain that binding and can reject collection describe, ingestion, and RAG operations. Do not change this setting without first designing and testing an explicit RBAC migration with a second administrative identity. This setting controls authorization behavior; it does not replace password authentication or rotate the root credential.

On the first install only, the default `COLD_START_DEPENDENCY_BOOTSTRAP=true` creates an atomic revision with all five Milvus application replica counts at zero while etcd and MinIO become ready. A second atomic revision then starts Milvus. Routine upgrades remain a single revision. If the second revision fails, Helm rolls back to the healthy dependency-only revision, and rerunning `deploy.sh` retries the full rollout.

The deployment explicitly splits storage by workload: the small, latency-sensitive etcd claims use the dedicated non-default `local-etcd` class. A new MinIO release requires an explicit reviewed `MINIO_STORAGE_CLASS` and rejects `local-etcd`, because Milvus segments and document objects are bulk data.

For an existing release, `deploy.sh` reuses the immutable StorageClass from the live MinIO volume claim template when `MINIO_STORAGE_CLASS` is omitted. It refuses a different configured class because changing it requires a separately planned data migration. Legacy MinIO claims on `local-etcd` remain deployable with a warning so a routine reconcile never replaces or resets stored data.

MinIO's bulk object PVCs remain subject to their selected storage failure domain, so its replica count and PDB do not by themselves provide storage-level high availability. The three etcd members use separate node-local volumes. Maintain tested backups outside both failure domains, and avoid interpreting the current replica topology as full storage-level high availability.

## Dedicated Node-Local etcd Storage

`deploy-local-storage.sh` installs Rancher local-path-provisioner from its pinned
OCI Helm chart and creates a dedicated `local-etcd` StorageClass:

```bash
bash deploy-local-storage.sh
kubectl get storageclass local-etcd
```

The class is non-default, uses `WaitForFirstConsumer`, retains PVs after PVC
deletion, and provisions only on `daedalus-02`, `daedalus-03`, and
`daedalus-01` under `/var/lib/daedalus-local-path/etcd`. The etcd StatefulSet
uses hard pod anti-affinity so its three local claims land on three distinct
nodes.

The provisioner creates node-local directories dynamically but does not enforce
the requested PVC capacity. Monitor node filesystem free space and kubelet
disk-pressure conditions. A node-local PV is unavailable while its node is
down, so keep all three etcd members on distinct nodes and store tested etcd
snapshots outside those nodes.

Installing the StorageClass does not change existing PVCs or restart Milvus.
An existing bound PVC's StorageClass cannot be changed in place. Moving the
current etcd cluster requires a coordinated snapshot, new local PVCs, restore,
and authenticated Milvus validation; do not copy a live etcd data directory.

## Monitoring Helm Reconciliation

The existing `monitoring/kube-prometheus-stack` release uses chart version
`82.18.0`. Its active `kube-prometheus-maintenance` Prometheus resource and
existing Service are persisted through the chart's `extraManifests` value.
The native chart-created Prometheus remains at zero replicas. Both resources
omit hostname selectors. New `local-fast` volumes can provision on
`daedalus-01`, `daedalus-02`, `daedalus-03`, or `daedalus-06`; an existing local
volume stays tied to its original node.

To reconcile the existing release while preserving its other user settings:

```bash
EXPECTED_CONTEXT=kubernetes-admin@kubernetes bash deploy-monitoring.sh
```

The helper requires Helm, kubectl, Python 3, and PyYAML. It captures current
user values in a private temporary file, combines them with
`kube-prometheus-stack-values.yaml`, and removes the temporary file on exit.
It handles live resource adoption and waits for the active StatefulSet to be
Ready without a hostname selector. Edit the active Prometheus object under
`extraManifests` for changes to its scrape, storage, or retention settings;
`prometheus.prometheusSpec` controls the inactive chart-created resource.
The Service UID, ClusterIP, NodePorts, and Grafana Service DNS are preserved.

## Optional NodePort Exposure

By default, Milvus and MinIO services are internal `ClusterIP` services.

To expose stable NodePorts declaratively:

```bash
EXPOSE_NODEPORTS=true bash deploy.sh
```

This applies `nodeport-values.yaml`:

- Milvus: `30416`
- MinIO: `30900`

Use this only for trusted networks or clusters with compensating access controls.

## Optional ServiceMonitor

If the Prometheus Operator CRD is installed and the monitoring stack selects `release=prometheus`, include the monitoring overlay:

```bash
ENABLE_MONITORING=true bash deploy.sh
```

Do not apply `monitoring-values.yaml` on clusters that do not have the `ServiceMonitor` CRD.

## Health Checks

After deploy:

```bash
kubectl get pods -n milvus
kubectl get svc -n milvus
kubectl rollout status deploy/milvus-proxy -n milvus
kubectl rollout status deploy/milvus-querynode -n milvus
kubectl rollout status deploy/milvus-datanode -n milvus
kubectl rollout status deploy/milvus-streamingnode -n milvus
kubectl rollout status deploy/milvus-mixcoord -n milvus
kubectl rollout status statefulset/milvus-etcd -n milvus
kubectl rollout status statefulset/milvus-minio -n milvus
```

For API-level verification, connect from an allowed workload or temporary debug pod using the Milvus root credentials from `milvus-root-credentials`.

An authorization check must do more than open TCP port `19530` or call `list_collections`. It should also call `has_collection` or describe a known collection so the check exercises the `DescribeCollection` privilege that ingestion and RAG metadata discovery require. From the workspace root, the integrated consumer-side check is:

```bash
make healthcheck
```

## Reset Milvus Metadata

Metadata reset is destructive. It uninstalls Milvus and deletes etcd PVCs. MinIO PVCs are preserved.
The script resolves and prints the exact labeled etcd PVCs before uninstalling. If a release or release-labeled PVCs exist but the etcd selector matches nothing, it refuses to proceed so an unlabeled legacy PVC cannot be silently left behind.

Deleting etcd removes Milvus collection schemas, indexes, RBAC metadata, and the references that make stored segments usable. Preserved MinIO objects are not automatically reattached to a fresh metadata store, and the document bucket is not repopulated by this command. Take and test a coordinated etcd/MinIO backup before resetting. After redeploy, run the repository's seed workflow to recreate and verify the required collections.

```bash
CONFIRM_RESET=milvus/milvus bash reset-etcd.sh
bash deploy.sh
```

For another namespace:

```bash
NAMESPACE=dev RELEASE_NAME=dev-milvus \
CONFIRM_RESET=dev-milvus/dev bash reset-etcd.sh
NAMESPACE=dev RELEASE_NAME=dev-milvus bash deploy.sh
```

The reset selector only matches PVCs carrying the lifecycle labels added by `pvc-label-post-renderer.sh`. Audit and migrate legacy or manually created PVCs before using the reset script; never assume an unlabeled claim will be selected.

## Uninstall

Uninstall preserves PVCs by default:

```bash
CONFIRM_DESTROY=milvus/milvus bash destroy.sh
```

To also delete all PVCs for the Milvus release:

```bash
CONFIRM_DESTROY=milvus/milvus \
DELETE_PVCS=true \
CONFIRM_DELETE_PVCS=milvus/milvus \
bash destroy.sh
```

Both confirmations are checked and the exact labeled PVC names are printed before Helm uninstall runs. If PVC deletion is requested for an existing release but its lifecycle selector matches nothing, the script stops without changing the release.

For non-default deployments, include `NAMESPACE`, `RELEASE_NAME`, and the matching `<release>/<namespace>` confirmation on the same command. PVC deletion is subject to each PersistentVolume's reclaim policy and may be irreversible. The external credential Secrets are retained.

## Rollback

Routine failed upgrades are already handled by Helm's `--atomic` behavior. Prefer correcting the configuration and rerunning the pinned `bash deploy.sh` path. Before a manual rollback, inspect the target revision and confirm that it uses external Secrets, the expected StorageClasses, and `rootShouldBindRole: false`:

```bash
helm history milvus -n milvus
helm get values milvus -n milvus --revision <revision>
helm rollback milvus <revision> -n milvus --wait --timeout 15m
```

Treat output from historical values and manifests as sensitive because a revision created by an older wrapper may have stored credentials in Helm release data. Do not roll back to a pre-hardening revision that renders credential values or changes the root authorization contract.

On a new installation, the first revision may be the intentional dependency-only bootstrap with every Milvus application replica set to zero. Rolling back to it leaves etcd and MinIO running but makes the Milvus API unavailable. After recovering dependencies, rerun `bash deploy.sh` to apply the complete steady-state revision.

Rollback does not restore Secret contents, deleted PVCs, etcd metadata, MinIO objects, or bucket contents. A rollback also restores old checksum annotations, so rerun `bash deploy.sh` after any credential rotation to reconcile pods to current Secret resource versions.

## What Changed From The Previous Wrapper

- `deploy.sh` no longer uninstalls Milvus before every deploy.
- `deploy.sh` no longer deletes etcd PVCs.
- `destroy.sh` no longer targets `nim-retrieval-ms` in the `nim` namespace.
- Destructive metadata reset moved to `reset-etcd.sh`.
- PVC deletion now requires explicit confirmation.
- Stable NodePorts moved from post-install `kubectl patch` commands into `nodeport-values.yaml`.
- Default services changed from externally exposed behavior to internal `ClusterIP`.
- MinIO default credentials are no longer rendered by the active deployment.
- Milvus and MinIO now consume one shared Secret without embedding its values in Helm release data.
- MinIO now uses four replicas and bootstraps both the segment and document buckets.
- StorageClasses are explicit and configurable.
- StatefulSet PVC templates now carry labels used by reset, destroy, and health checks.
- The raw etcd root-role mutation and misleading metadata-prefix override were removed.
- Milvus auth is enabled and root password is sourced from a Kubernetes secret.
- The upstream chart version is pinned.
- `default-values.yaml` was refreshed from chart `5.0.14`.
- Resource requests/limits, PDBs, topology spread, and named service accounts were added.
- Validation and operator documentation were added.

## Security Notes

- Do not commit credentials, kubeconfigs, TLS private keys, or base64-encoded secrets.
- Keep `nodeport-values.yaml` opt-in unless network access is explicitly approved.
- Preserve the MinIO `securityContext` UID/GID unless storage ownership has been verified.
- Rotate MinIO and Milvus root credentials through Kubernetes secrets.
- Use Milvus's password-update API as part of root rotation; a Secret-only change is not a persisted credential update.
- Prefer dedicated least-privileged users for routine ingestion and retrieval instead of sharing the built-in root identity indefinitely.
- Add NetworkPolicy or equivalent cluster controls in production-like environments.

## Remaining Environment Decisions

These are intentionally not hard-coded in this repo:

- Secret provisioning mechanism
- Service-specific, least-privileged Milvus users and rotation automation
- NetworkPolicy allow-list
- Monitoring label selectors
- Backup and restore policy
- Milvus capacity sizing
- Production-grade block/local StorageClasses for etcd and MinIO
- Production TLS termination
- Environment-specific overlays
