#!/usr/bin/env bash
set -euo pipefail

namespace="${DAEDALUS_NAMESPACE:-daedalus}"
kubectl_bin="${KUBECTL:-kubectl}"

required=(
  milvus-minio-credentials
  milvus-root-credentials
  ngc-api
  ngc-secret
  nv-ingest-object-store
  nvcr-imagepullsecret
  url-ingest-hf
  k8s-mcp-auth
  unifi-network-mcp-auth
  unifi-network-mcp-unifi-api
)

missing=()
for secret in "${required[@]}"; do
  if ! "$kubectl_bin" --namespace "$namespace" get secret "$secret" >/dev/null 2>&1; then
    missing+=("$secret")
  fi
done

if ((${#missing[@]})); then
  printf 'Missing externally managed Secrets in namespace %s: %s\n' \
    "$namespace" "${missing[*]}" >&2
  printf 'For the one-time legacy cutover, run make migrate-legacy-secrets.\n' >&2
  exit 1
fi

printf 'Shared Secret contract: PASS (%s)\n' "$namespace"
