#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

RELEASE_NAME=${RELEASE_NAME:-"local-etcd-provisioner"}
NAMESPACE=${NAMESPACE:-"local-path-storage"}
CHART=${CHART:-"oci://ghcr.io/rancher/local-path-provisioner/charts/local-path-provisioner"}
CHART_VERSION=${CHART_VERSION:-"0.0.36"}
STORAGE_CLASS=${STORAGE_CLASS:-"local-etcd"}
HELM_TIMEOUT=${HELM_TIMEOUT:-"5m"}

for command_name in helm kubectl; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "Missing required command: ${command_name}" >&2
    exit 1
  fi
done

for node_name in daedalus-02 daedalus-03 daedalus-04; do
  kubectl get node "${node_name}" >/dev/null
done

helm upgrade --install "${RELEASE_NAME}" "${CHART}" \
  --version "${CHART_VERSION}" \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  --values "${SCRIPT_DIR}/local-path-provisioner-values.yaml" \
  --wait \
  --atomic \
  --timeout "${HELM_TIMEOUT}"

kubectl get storageclass "${STORAGE_CLASS}" \
  -o custom-columns='NAME:.metadata.name,PROVISIONER:.provisioner,RECLAIM:.reclaimPolicy,BINDING:.volumeBindingMode,DEFAULT:.metadata.annotations.storageclass\.kubernetes\.io/is-default-class'
