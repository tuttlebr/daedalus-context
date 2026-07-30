#!/usr/bin/env bash
set -euo pipefail

source_path="${KUBECONFIG_SOURCE:-${KUBECONFIG:-${HOME}/.kube/config}}"
stage_gid="${KUBECONFIG_GROUP_ID:-$(id -g)}"
destination_path="${COMPOSE_KUBECONFIG_PATH:-/tmp/daedalus-context-compose-$(id -u)/kubeconfig}"
destination_dir="$(dirname -- "${destination_path}")"

if [[ "${source_path}" == *:* ]]; then
  echo "KUBECONFIG_SOURCE must name one kubeconfig file, not a path list." >&2
  exit 1
fi
if [[ ! -f "${source_path}" ]]; then
  echo "Kubeconfig source is not a regular file: ${source_path}" >&2
  exit 1
fi
if [[ ! "${stage_gid}" =~ ^[0-9]+$ ]]; then
  echo "KUBECONFIG_GROUP_ID must be a numeric group ID." >&2
  exit 1
fi
if [[ "${destination_path}" != /* ]]; then
  echo "COMPOSE_KUBECONFIG_PATH must be an absolute path." >&2
  exit 1
fi
if ! tr ' ' '\n' <<<"$(id -G)" | grep -Fxq "${stage_gid}"; then
  echo "Current user is not a member of kubeconfig group ${stage_gid}." >&2
  exit 1
fi
command -v kubectl >/dev/null || {
  echo "kubectl is required to flatten the Compose kubeconfig." >&2
  exit 1
}

mkdir -p -- "${destination_dir}"
chmod 0700 "${destination_dir}"
temporary_path="$(mktemp "${destination_dir}/.config.XXXXXX")"
cleanup() {
  rm -f -- "${temporary_path}"
}
trap cleanup EXIT

umask 077
kubectl --kubeconfig "${source_path}" config view --raw --flatten >"${temporary_path}"
if [[ ! -s "${temporary_path}" ]]; then
  echo "Flattened kubeconfig is empty: ${source_path}" >&2
  exit 1
fi
chgrp "${stage_gid}" "${temporary_path}"
chmod 0640 "${temporary_path}"
mv -f -- "${temporary_path}" "${destination_path}"
trap - EXIT

printf 'Staged Compose kubeconfig from %s\n' "${source_path}"
