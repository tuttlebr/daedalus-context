#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

RELEASE_NAME=${RELEASE_NAME:-"nfs-subdir-external-provisioner"}
NAMESPACE=${NAMESPACE:-"nfs-client"}
CHART=${CHART:-"nfs-subdir-external-provisioner/nfs-subdir-external-provisioner"}
CHART_VERSION=${CHART_VERSION:-"4.0.18"}
HELM_TIMEOUT=${HELM_TIMEOUT:-"5m"}
EXPECTED_CONTEXT=${EXPECTED_CONTEXT:?Set EXPECTED_CONTEXT to the approved literal Kubernetes context}

for command_name in helm kubectl; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "Missing required command: ${command_name}" >&2
    exit 1
  fi
done

current_context="$(kubectl config current-context)"
if [[ "${current_context}" != "${EXPECTED_CONTEXT}" ]]; then
  echo "Refusing context '${current_context}'; expected '${EXPECTED_CONTEXT}'." >&2
  exit 1
fi

helm upgrade --install "${RELEASE_NAME}" "${CHART}" \
  --version "${CHART_VERSION}" \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  --values "${SCRIPT_DIR}/nfs-subdir-external-provisioner-values.yaml" \
  --wait \
  --atomic \
  --timeout "${HELM_TIMEOUT}"

kubectl get storageclass nfs-client \
  -o custom-columns='NAME:.metadata.name,PROVISIONER:.provisioner,RECLAIM:.reclaimPolicy,BINDING:.volumeBindingMode,DEFAULT:.metadata.annotations.storageclass\.kubernetes\.io/is-default-class'
