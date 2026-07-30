#!/usr/bin/env bash

set -euo pipefail

NAMESPACE=${NAMESPACE:-"milvus"}
RELEASE_NAME=${RELEASE_NAME:-"milvus"}
DELETE_PVCS=${DELETE_PVCS:-"false"}
EXPECTED_CONFIRM="${RELEASE_NAME}/${NAMESPACE}"
pvc_selector="daedalus.nvidia.com/milvus-storage=true,app.kubernetes.io/instance=${RELEASE_NAME}"
pvc_names=()

if [[ "${CONFIRM_DESTROY:-}" != "${EXPECTED_CONFIRM}" ]]; then
  cat >&2 <<EOF
Refusing to uninstall without explicit confirmation.

This removes Helm release '${RELEASE_NAME}' from namespace '${NAMESPACE}'.
Run again with:
  CONFIRM_DESTROY=${EXPECTED_CONFIRM} bash destroy.sh

PVCs are preserved by default. To delete all PVCs for this release too, also set:
  DELETE_PVCS=true CONFIRM_DELETE_PVCS=${EXPECTED_CONFIRM}
EOF
  exit 1
fi

if [[ "${DELETE_PVCS}" == "true" ]]; then
  if [[ "${CONFIRM_DELETE_PVCS:-}" != "${EXPECTED_CONFIRM}" ]]; then
    cat >&2 <<EOF
Refusing to delete PVCs without explicit confirmation.

Run again with:
  CONFIRM_DESTROY=${EXPECTED_CONFIRM} DELETE_PVCS=true CONFIRM_DELETE_PVCS=${EXPECTED_CONFIRM} bash destroy.sh
EOF
    exit 1
  fi

  if kubectl get namespace "${NAMESPACE}" >/dev/null 2>&1; then
    mapfile -t pvc_names < <(
      kubectl -n "${NAMESPACE}" get pvc -l "${pvc_selector}" -o name
    )
  fi

  if [[ "${#pvc_names[@]}" -eq 0 ]] && \
    helm status "${RELEASE_NAME}" -n "${NAMESPACE}" >/dev/null 2>&1; then
    cat >&2 <<EOF
Refusing to uninstall while PVC deletion was requested but no PVCs matched:
  ${pvc_selector}

The release exists, so an empty match may indicate legacy or missing PVC labels.
Inspect and label the intended PVCs before retrying; no resources were changed.
EOF
    exit 1
  fi

  echo "PVCs selected for deletion before uninstall:"
  if [[ "${#pvc_names[@]}" -eq 0 ]]; then
    echo "  (none)"
  else
    printf '  %s\n' "${pvc_names[@]}"
  fi
else
  echo "PVC deletion was not requested; all release PVCs will be preserved."
fi

echo "Uninstalling Helm release '${RELEASE_NAME}' from namespace '${NAMESPACE}'."
helm uninstall "${RELEASE_NAME}" -n "${NAMESPACE}" --ignore-not-found

if [[ "${DELETE_PVCS}" == "true" && "${#pvc_names[@]}" -gt 0 ]]; then
  kubectl -n "${NAMESPACE}" delete "${pvc_names[@]}" --ignore-not-found
fi
