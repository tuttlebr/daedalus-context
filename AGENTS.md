# Daedalus Context Agent Guidance

Use `$operate-daedalus-context` for full repository or focused component code
review, documentation alignment, bug identification and remediation, root
deployment, image release, health, ingestion, Daedalus integration, and recovery
tasks. `README.md` and the root `Makefile` are the canonical human and command
interfaces.

Kubernetes is the only runtime deployment path. The root Compose file defines
images for validation, building, and publishing only.

Before any Kubernetes command, source the root `.env` with export enabled and
set an approved literal `EXPECTED_CONTEXT`. Do not derive the expected value
from the active context in the same operation.

Preserve unrelated working-tree changes. Do not run registry writes, cluster
writes, data jobs, PVC deletion, etcd resets, or collection rebuilds unless the
user explicitly requests the corresponding action.

For API verification from a non-Kubernetes LAN client, always try SSH to
`daedalus-07` first.
