#!/usr/bin/env bash
set -euo pipefail

namespace="${DAEDALUS_NAMESPACE:-daedalus}"
kubectl_bin="${KUBECTL:-kubectl}"

# Hindsight owns its own Secret lifecycle in the adjacent repository. These
# are the default externally managed Secrets of the selected context charts.
components=("$@")
if ((${#components[@]} == 0)); then
  components=(milvus retriever nv-ingest pdf-server url-ingest k8s-mcp unifi-mcp)
fi
required=()
for component in "${components[@]}"; do
  case "$component" in
    hindsight|retriever|pdf-server) ;;
    milvus) required+=(milvus-minio-credentials milvus-root-credentials) ;;
    nv-ingest) required+=(ngc-api ngc-secret nv-ingest-object-store) ;;
    url-ingest) required+=(milvus-minio-credentials milvus-root-credentials nvcr-imagepullsecret url-ingest-hf) ;;
    k8s-mcp) required+=(k8s-mcp-auth) ;;
    unifi-mcp) required+=(unifi-network-mcp-auth unifi-network-mcp-unifi-api) ;;
    *) printf 'Unknown component: %s\n' "$component" >&2; exit 1 ;;
  esac
done
# Avoid querying shared dependencies twice.
if ((${#required[@]})); then
  mapfile -t required < <(printf '%s\n' "${required[@]}" | LC_ALL=C sort -u)
fi

missing=()
for secret in "${required[@]}"; do
  if ! "$kubectl_bin" --namespace "$namespace" get secret "$secret" >/dev/null 2>&1; then
    missing+=("$secret")
  fi
done

if ((${#missing[@]})); then
  printf 'Missing externally managed Secrets in namespace %s: %s\n' \
    "$namespace" "${missing[*]}" >&2
  printf 'Provision the selected chart Secrets using the deployment guide before retrying.\n' >&2
  exit 1
fi

printf 'Shared Secret contract: PASS (%s)\n' "$namespace"
