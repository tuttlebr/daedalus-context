#!/usr/bin/env bash
# Invoked by root make publish-corpus after its context preflight.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
: "${NAMESPACE:?Run make publish-corpus from the repository root}"
PDF_SOURCE_HOST_PATH="${PDF_SOURCE_HOST_PATH:-${SCRIPT_DIR}}"
PDF_SOURCE_NODE="${PDF_SOURCE_NODE:-daedalus-01}"
CORPUS_GENERATION="${CORPUS_GENERATION:-}"
PUBLISH_DEADLINE_SECONDS="${PUBLISH_DEADLINE_SECONDS:-900}"
cd "${SCRIPT_DIR}"

escape_sed_replacement() {
  printf '%s' "$1" | sed -e 's/[&|\\]/\\&/g'
}

render_publisher() {
  local run_id generation publish_fence
  run_id="$(date -u +%Y%m%d%H%M%S)"
  publish_fence="$(date -u +%s%N)"
  generation="${CORPUS_GENERATION:-${run_id}}"
  [[ "${generation}" =~ ^[a-zA-Z0-9._-]+$ ]] || {
    echo "CORPUS_GENERATION may contain only letters, numbers, dot, underscore, and dash." >&2
    exit 1
  }
  sed \
    -e "s|__NAMESPACE__|$(escape_sed_replacement "${NAMESPACE}")|g" \
    -e "s|__RUN_ID__|${run_id}|g" \
    -e "s|__DEADLINE_SECONDS__|${PUBLISH_DEADLINE_SECONDS}|g" \
    -e "s|__CORPUS_GENERATION__|${generation}|g" \
    -e "s|__PUBLISH_FENCE__|${publish_fence}|g" \
    -e "s|__SOURCE_NODE__|$(escape_sed_replacement "${PDF_SOURCE_NODE}")|g" \
    -e "s|__SOURCE_PATH__|$(escape_sed_replacement "${PDF_SOURCE_HOST_PATH}")|g" \
    k8s/publish-corpus-job.yaml
}

kubectl -n "${NAMESPACE}" get deployment/pdf-server service/pdf-server \
  persistentvolumeclaim/pdf-server-data >/dev/null
publish_manifest="$(mktemp)"
trap 'rm -f "${publish_manifest}"' EXIT
render_publisher >"${publish_manifest}"
publish_job="$(awk '/^kind: Job$/{found=1} found && /^  name:/{print $2; exit}' "${publish_manifest}")"
kubectl --namespace="${NAMESPACE}" create -f "${publish_manifest}"
kubectl --namespace="${NAMESPACE}" wait --for=condition=complete \
  "job/${publish_job}" --timeout="${PUBLISH_DEADLINE_SECONDS}s"
