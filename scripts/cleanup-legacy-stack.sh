#!/usr/bin/env bash
set -euo pipefail

required_confirmation="delete-legacy-daedalus-context"
if [[ "${CONFIRM_LEGACY_CLEANUP:-}" != "$required_confirmation" ]]; then
  printf 'Set CONFIRM_LEGACY_CLEANUP=%s to remove the legacy stack.\n' \
    "$required_confirmation" >&2
  exit 1
fi

kubectl_bin="${KUBECTL:-kubectl}"
helm_bin="${HELM:-helm}"
legacy_namespaces=(
  daedalus-hindsight
  milvus
  daedalus-retriever
  nv-ingest
  k8s-mcp-server
  unifi-network-mcp-server
)
temporary_directory="$(mktemp -d)"
chmod 700 "$temporary_directory"
snapshot="$temporary_directory/local-pvs.tsv"
trap 'rm -rf -- "$temporary_directory"' EXIT

for namespace in "${legacy_namespaces[@]}"; do
  if ! "$kubectl_bin" get namespace "$namespace" >/dev/null 2>&1; then
    continue
  fi
  if "$kubectl_bin" --namespace "$namespace" get jobs -o json |
    jq -e '.items[] | select((.status.active // 0) > 0)' >/dev/null; then
    printf 'Active Job found in legacy namespace %s; refusing cleanup.\n' "$namespace" >&2
    exit 1
  fi
done

"$kubectl_bin" get pv -o json | jq -r --argjson namespaces \
  "$(printf '%s\n' "${legacy_namespaces[@]}" | jq -R . | jq -s .)" '
    .items[]
    | select(.spec.claimRef.namespace? as $namespace | $namespaces | index($namespace))
    | select(.spec.local.path? != null)
    | [
        .metadata.name,
        .spec.claimRef.namespace,
        .spec.claimRef.name,
        ([.spec.nodeAffinity.required.nodeSelectorTerms[]?.matchExpressions[]?
          | select(.key == "kubernetes.io/hostname")
          | .values[]?][0] // ""),
        .spec.local.path
      ]
    | @tsv
  ' >"$snapshot"

while IFS=$'\t' read -r pv namespace claim node path; do
  [[ -n "$pv" ]] || continue
  [[ "$node" =~ ^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$ ]] || {
    printf 'PV %s has no safe hostname affinity.\n' "$pv" >&2
    exit 1
  }
  [[ "$path" =~ ^/var/lib/daedalus-local-path/etcd/pvc-[0-9a-f-]+_[a-z0-9-]+_[a-z0-9-]+$ ]] || {
    printf 'PV %s has unexpected local path %s.\n' "$pv" "$path" >&2
    exit 1
  }
  printf 'Captured retained PV %s: %s/%s node=%s path=%s\n' \
    "$pv" "$namespace" "$claim" "$node" "$path"
done <"$snapshot"

for release_spec in \
  daedalus-hindsight:hindsight \
  milvus:milvus \
  daedalus-retriever:nemotron-retriever \
  nv-ingest:nv-ingest; do
  namespace="${release_spec%%:*}"
  release="${release_spec##*:}"
  if "$helm_bin" --namespace "$namespace" status "$release" >/dev/null 2>&1; then
    "$helm_bin" --namespace "$namespace" uninstall "$release" --wait --timeout 20m
  fi
done

for namespace in "${legacy_namespaces[@]}"; do
  "$kubectl_bin" delete namespace "$namespace" --ignore-not-found --wait=true --timeout=20m
done

"$kubectl_bin" delete clusterrole k8s-mcp-server-readonly --ignore-not-found
"$kubectl_bin" delete clusterrolebinding k8s-mcp-server-readonly --ignore-not-found

while IFS=$'\t' read -r pv namespace claim node path; do
  [[ -n "$pv" ]] || continue
  phase="$($kubectl_bin get pv "$pv" -o jsonpath='{.status.phase}')"
  if [[ "$phase" != Released && "$phase" != Available ]]; then
    printf 'PV %s is %s, not Released or Available.\n' "$pv" "$phase" >&2
    exit 1
  fi
  if "$kubectl_bin" get volumeattachment -o json |
    jq -e --arg pv "$pv" '.items[] | select(.spec.source.persistentVolumeName == $pv)' >/dev/null; then
    printf 'PV %s still has a VolumeAttachment.\n' "$pv" >&2
    exit 1
  fi

  cleaner="legacy-pv-cleaner-${pv#pvc-}"
  cleaner="${cleaner:0:63}"
  suffix="${path##*/}"
  jq -n --arg name "$cleaner" --arg namespace daedalus --arg node "$node" \
    --arg suffix "$suffix" --arg expected "$path" '
      {
        apiVersion: "v1",
        kind: "Pod",
        metadata: {name: $name, namespace: $namespace},
        spec: {
          nodeName: $node,
          restartPolicy: "Never",
          automountServiceAccountToken: false,
          containers: [{
            name: "cleaner",
            image: "alpine:3.21.7",
            command: ["sh", "-ceu"],
            args: [
              "host_path=/host-etcd/$1; expected=$2; [ ! -L \"$host_path\" ] || exit 32; if [ -e \"$host_path\" ]; then [ -d \"$host_path\" ] || exit 33; if awk -v target=\"$expected\" '\''$5 == target {found=1} END {exit !found}'\'' /host-mountinfo; then exit 34; fi; find \"$host_path\" -mindepth 1 -delete; rmdir \"$host_path\"; fi",
              "cleaner",
              $suffix,
              $expected
            ],
            securityContext: {
              allowPrivilegeEscalation: false,
              capabilities: {drop: ["ALL"], add: ["DAC_OVERRIDE", "FOWNER"]},
              runAsUser: 0,
              seccompProfile: {type: "RuntimeDefault"}
            },
            volumeMounts: [
              {name: "etcd-root", mountPath: "/host-etcd"},
              {name: "mountinfo", mountPath: "/host-mountinfo", readOnly: true}
            ]
          }],
          volumes: [
            {name: "etcd-root", hostPath: {path: "/var/lib/daedalus-local-path/etcd", type: "Directory"}},
            {name: "mountinfo", hostPath: {path: "/proc/1/mountinfo", type: "File"}}
          ]
        }
      }
    ' | "$kubectl_bin" create -f - >/dev/null
  if ! "$kubectl_bin" --namespace daedalus wait pod/"$cleaner" \
    --for=jsonpath='{.status.phase}'=Succeeded --timeout=5m; then
    "$kubectl_bin" --namespace daedalus logs "$cleaner" >&2 || true
    exit 1
  fi
  "$kubectl_bin" delete pv "$pv" --wait=true --timeout=5m
  "$kubectl_bin" --namespace daedalus delete pod "$cleaner" --wait=true --timeout=2m >/dev/null
  printf 'Wiped retained local path on %s: %s\n' "$node" "$path"
done <"$snapshot"

printf 'Legacy Daedalus context namespaces, RBAC, retained PVs, and exact local paths removed.\n'
