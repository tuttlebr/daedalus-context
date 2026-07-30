#!/usr/bin/env bash

set -euo pipefail

NAMESPACE=${NAMESPACE:-"milvus"}
RELEASE_NAME=${RELEASE_NAME:-"milvus"}
EXPECTED_CONFIRM="${RELEASE_NAME}/${NAMESPACE}"
pvc_selector="daedalus.nvidia.com/milvus-storage=true,app.kubernetes.io/instance=${RELEASE_NAME},app.kubernetes.io/name=etcd"
pvc_names=()
release_pvc_names=()
release_exists=false

if [[ "${CONFIRM_RESET:-}" != "${EXPECTED_CONFIRM}" ]]; then
  cat >&2 <<EOF
Refusing to reset Milvus metadata without explicit confirmation.

This uninstalls Helm release '${RELEASE_NAME}' from namespace '${NAMESPACE}' and deletes its etcd PVCs.
MinIO PVCs are left intact.

Run again with:
  CONFIRM_RESET=${EXPECTED_CONFIRM} bash reset-etcd.sh
EOF
  exit 1
fi

if kubectl get namespace "${NAMESPACE}" >/dev/null 2>&1; then
  mapfile -t pvc_names < <(
    kubectl -n "${NAMESPACE}" get pvc -l "${pvc_selector}" -o name
  )
  mapfile -t release_pvc_names < <(
    kubectl -n "${NAMESPACE}" get pvc \
      -l "daedalus.nvidia.com/milvus-storage=true,app.kubernetes.io/instance=${RELEASE_NAME}" \
      -o name
  )
fi

if helm status "${RELEASE_NAME}" -n "${NAMESPACE}" >/dev/null 2>&1; then
  release_exists=true
fi

if [[ "${#pvc_names[@]}" -eq 0 ]] && \
  { [[ "${release_exists}" == "true" ]] || [[ "${#release_pvc_names[@]}" -gt 0 ]]; }; then
  cat >&2 <<EOF
Refusing to uninstall because a release or its labeled PVCs exist, but no etcd PVCs matched:
  ${pvc_selector}

An empty match may indicate legacy or missing PVC labels. Inspect and label the
intended etcd PVCs before retrying; no resources were changed.
EOF
  exit 1
fi

echo "etcd PVCs selected for deletion before uninstall:"
if [[ "${#pvc_names[@]}" -eq 0 ]]; then
  echo "  (none)"
else
  printf '  %s\n' "${pvc_names[@]}"
fi

echo "Uninstalling Helm release '${RELEASE_NAME}' from namespace '${NAMESPACE}'."
helm uninstall "${RELEASE_NAME}" -n "${NAMESPACE}" --ignore-not-found

if [[ "${#pvc_names[@]}" -gt 0 ]]; then
  kubectl -n "${NAMESPACE}" delete "${pvc_names[@]}" --ignore-not-found
fi
