#!/usr/bin/env bash
set -euo pipefail

namespace="${DAEDALUS_NAMESPACE:-daedalus}"
kubectl_bin="${KUBECTL:-kubectl}"
jq_bin="${JQ:-jq}"
temporary_directory="$(mktemp -d)"
chmod 700 "$temporary_directory"
trap 'rm -rf -- "$temporary_directory"' EXIT

render_secret() {
  local source_namespace="$1"
  local source_name="$2"
  local target_name="$3"
  local data_filter="$4"

  "$kubectl_bin" --namespace "$source_namespace" get secret "$source_name" -o json |
    "$jq_bin" --arg namespace "$namespace" --arg name "$target_name" \
      --argjson data_filter "$data_filter" '
        del(
          .metadata.annotations,
          .metadata.creationTimestamp,
          .metadata.managedFields,
          .metadata.ownerReferences,
          .metadata.resourceVersion,
          .metadata.uid
        )
        | .metadata.namespace = $namespace
        | .metadata.name = $name
        | .metadata.labels = {
            "app.kubernetes.io/part-of": "daedalus-context",
            "app.kubernetes.io/managed-by": "legacy-secret-migration"
          }
        | .data as $source
        | .data = ($data_filter as $wanted
            | reduce ($wanted | keys[]) as $target ({};
                if (($source[$wanted[$target]] // "") | length) > 0
                then .[$target] = $source[$wanted[$target]]
                else . end))
      '
}

migrate_secret() {
  local source_namespace="$1"
  local source_name="$2"
  local target_name="$3"
  local data_filter="$4"
  local manifest="$temporary_directory/$target_name.json"

  render_secret "$source_namespace" "$source_name" "$target_name" "$data_filter" >"$manifest"
  "$jq_bin" -e '.data | type == "object" and length > 0 and all(.[]; type == "string" and length > 0)' \
    "$manifest" >/dev/null || {
      printf 'Source %s/%s lacks required data for %s.\n' \
        "$source_namespace" "$source_name" "$target_name" >&2
      exit 1
    }

  if "$kubectl_bin" --namespace "$namespace" get secret "$target_name" >/dev/null 2>&1; then
    local expected_hash actual_hash
    expected_hash="$($jq_bin -S '.data' "$manifest" | sha256sum | awk '{print $1}')"
    actual_hash="$($kubectl_bin --namespace "$namespace" get secret "$target_name" -o json |
      "$jq_bin" -S '.data' | sha256sum | awk '{print $1}')"
    if [[ "$expected_hash" != "$actual_hash" ]]; then
      printf 'Refusing to overwrite differing Secret %s/%s.\n' "$namespace" "$target_name" >&2
      exit 1
    fi
    printf 'Verified existing Secret %s/%s\n' "$namespace" "$target_name"
    return
  fi

  "$kubectl_bin" apply -f "$manifest" >/dev/null
  printf 'Migrated Secret %s/%s from %s/%s\n' \
    "$namespace" "$target_name" "$source_namespace" "$source_name"
}

identity='{"accesskey":"accesskey","secretkey":"secretkey"}'
migrate_secret milvus milvus-minio-credentials milvus-minio-credentials "$identity"
migrate_secret milvus milvus-root-credentials milvus-root-credentials '{"password":"password"}'
migrate_secret nv-ingest ngc-api ngc-api '{"NGC_API_KEY":"NGC_API_KEY"}'
migrate_secret nv-ingest ngc-secret ngc-secret '{".dockerconfigjson":".dockerconfigjson"}'
migrate_secret default nvcr-imagepullsecret nvcr-imagepullsecret '{".dockerconfigjson":".dockerconfigjson"}'
migrate_secret milvus url-ingest-secret url-ingest-hf '{"HF_ACCESS_TOKEN":"HF_ACCESS_TOKEN"}'
migrate_secret milvus milvus-minio-credentials nv-ingest-object-store \
  '{"MINIO_ACCESS_KEY":"accesskey","MINIO_SECRET_KEY":"secretkey"}'
migrate_secret k8s-mcp-server k8s-mcp-auth k8s-mcp-auth \
  '{"token":"token","next-token":"next-token"}'
migrate_secret unifi-network-mcp-server unifi-network-mcp-auth unifi-network-mcp-auth \
  '{"token":"token","next-token":"next-token"}'
migrate_secret unifi-network-mcp-server unifi-network-mcp-unifi-api unifi-network-mcp-unifi-api \
  '{"api-key":"api-key"}'
