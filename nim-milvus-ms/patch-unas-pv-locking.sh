#!/usr/bin/env bash

set -euo pipefail

NFS_SERVER=${NFS_SERVER:-"192.168.1.77"}
APPLY_NFS_PV_LOCKING=${APPLY_NFS_PV_LOCKING:-"false"}
EXPECTED_CONTEXT=${EXPECTED_CONTEXT:?Set EXPECTED_CONTEXT to the approved literal Kubernetes context}

for command_name in jq kubectl; do
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

legacy_options='["rw","noatime","rsize=1048576","wsize=1048576","hard","nolock","proto=tcp","timeo=600","retrans=2","sec=sys","fsc","local_lock=all"]'
desired_options='["rw","noatime","rsize=1048576","wsize=1048576","hard","lock","proto=tcp","timeo=600","retrans=2","sec=sys","fsc","local_lock=none"]'
desired_patch="$(jq -cn --argjson options "${desired_options}" '{spec:{mountOptions:$options}}')"

mapfile -t unas_pvs < <(
  kubectl get persistentvolumes -o json |
    jq -r --arg server "${NFS_SERVER}" '.items[] | select(.spec.nfs.server == $server) | .metadata.name' |
    sort
)

if [[ "${#unas_pvs[@]}" -eq 0 ]]; then
  echo "No NFS PersistentVolumes use ${NFS_SERVER}." >&2
  exit 1
fi

for pv_name in "${unas_pvs[@]}"; do
  current_options="$(kubectl get persistentvolume "${pv_name}" -o json | jq -c '.spec.mountOptions // []')"
  if [[ "${current_options}" != "${legacy_options}" && "${current_options}" != "${desired_options}" ]]; then
    echo "Refusing unexpected mountOptions on ${pv_name}: ${current_options}" >&2
    exit 1
  fi
done

if [[ "${APPLY_NFS_PV_LOCKING}" != "true" ]]; then
  printf 'Validated %d UNAS PersistentVolumes; set APPLY_NFS_PV_LOCKING=true to patch them.\n' "${#unas_pvs[@]}"
  printf '%s\n' "${unas_pvs[@]}"
  exit 0
fi

for pv_name in "${unas_pvs[@]}"; do
  kubectl patch persistentvolume "${pv_name}" --type merge --patch "${desired_patch}" >/dev/null
  printf 'patched %s\n' "${pv_name}"
done

for pv_name in "${unas_pvs[@]}"; do
  current_options="$(kubectl get persistentvolume "${pv_name}" -o json | jq -c '.spec.mountOptions // []')"
  if [[ "${current_options}" != "${desired_options}" ]]; then
    echo "PersistentVolume ${pv_name} did not retain the desired mountOptions." >&2
    exit 1
  fi
done

printf 'Verified server-coordinated locking options on %d UNAS PersistentVolumes.\n' "${#unas_pvs[@]}"
