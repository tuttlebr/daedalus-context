#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
# shellcheck source=../scripts/image-reference.sh
source "${WORKSPACE_DIR}/scripts/image-reference.sh"

load_env_file() {
  local file="$1"
  if [[ -f "${file}" ]]; then
    echo "Loading ${file#"${WORKSPACE_DIR}/"}"
    set -a
    # shellcheck source=/dev/null
    source "${file}"
    set +a
  fi
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

is_true() {
  case "${1:-}" in
    true|TRUE|1|yes|YES) return 0 ;;
    *) return 1 ;;
  esac
}

ORIGINAL_ENV_FILE="$(mktemp)"
trap 'rm -f "${ORIGINAL_ENV_FILE}"' EXIT
export -p >"${ORIGINAL_ENV_FILE}"
load_env_file "${WORKSPACE_DIR}/.env"
load_env_file "${SCRIPT_DIR}/.env"
# Explicit caller values (including root Makefile controls) take precedence.
# shellcheck source=/dev/null
source "${ORIGINAL_ENV_FILE}"
rm -f "${ORIGINAL_ENV_FILE}"
trap - EXIT

TARGET="${TARGET:-local}"
NAMESPACE="${NAMESPACE:-milvus}"
DRY_RUN="${DRY_RUN:-false}"
ROLLOUT_TIMEOUT="${ROLLOUT_TIMEOUT:-10m}"
PDF_SERVER_IMAGE="${PDF_SERVER_IMAGE:-daedalus.ddns.me:5050/btuttle/pdf-server:0.1.0}"
PDF_STORAGE_CLASS="${PDF_STORAGE_CLASS:-nfs-client}"
PDF_STORAGE_SIZE="${PDF_STORAGE_SIZE:-10Gi}"
AGENT_NAMESPACE="${AGENT_NAMESPACE:-daedalus}"
URL_INGEST_NAMESPACE="${URL_INGEST_NAMESPACE:-${NAMESPACE}}"
PDF_SOURCE_HOST_PATH="${PDF_SOURCE_HOST_PATH:-${SCRIPT_DIR}}"
PDF_SOURCE_NODE="${PDF_SOURCE_NODE:-${URL_INGEST_SOURCE_NODE:-daedalus-01}}"
PUBLISH_CORPUS="${PUBLISH_CORPUS:-false}"
PUBLISH_ONLY="${PUBLISH_ONLY:-false}"
CORPUS_GENERATION="${CORPUS_GENERATION:-}"
PUBLISH_DEADLINE_SECONDS="${PUBLISH_DEADLINE_SECONDS:-900}"

cd "${SCRIPT_DIR}"

escape_sed_replacement() {
  printf '%s' "$1" | sed -e 's/[&|\\]/\\&/g'
}

render_server() {
  local checksum
  checksum="$(sha256sum k8s/pdf-server.yaml | awk '{print $1}')"
  sed \
    -e "s|__NAMESPACE__|$(escape_sed_replacement "${NAMESPACE}")|g" \
    -e "s|__STORAGE_CLASS__|$(escape_sed_replacement "${PDF_STORAGE_CLASS}")|g" \
    -e "s|__STORAGE_SIZE__|$(escape_sed_replacement "${PDF_STORAGE_SIZE}")|g" \
    -e "s|__PDF_SERVER_IMAGE__|$(escape_sed_replacement "${PDF_SERVER_IMAGE}")|g" \
    -e "s|__CONFIG_CHECKSUM__|${checksum}|g" \
    -e "s|__AGENT_NAMESPACE__|$(escape_sed_replacement "${AGENT_NAMESPACE}")|g" \
    -e "s|__URL_INGEST_NAMESPACE__|$(escape_sed_replacement "${URL_INGEST_NAMESPACE}")|g" \
    k8s/pdf-server.yaml
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

case "${TARGET}" in
  local)
    require_command docker
    docker compose \
      --project-directory "${WORKSPACE_DIR}" \
      --file "${WORKSPACE_DIR}/docker-compose.yaml" \
      up --detach pdf-server
    ;;
  kubernetes|k8s)
    require_command kubectl
    require_command sha256sum
    require_semver_image_ref "PDF_SERVER_IMAGE" "${PDF_SERVER_IMAGE}"
    if is_true "${DRY_RUN}"; then
      if ! is_true "${PUBLISH_ONLY}"; then
        render_server | kubectl --namespace="${NAMESPACE}" apply --dry-run=client -f -
      fi
      if is_true "${PUBLISH_CORPUS}"; then
        render_publisher | kubectl --namespace="${NAMESPACE}" apply --dry-run=client -f -
      fi
      exit 0
    fi
    if is_true "${PUBLISH_ONLY}"; then
      kubectl -n "${NAMESPACE}" get deployment/pdf-server service/pdf-server \
        persistentvolumeclaim/pdf-server-data >/dev/null
    else
      kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -
      render_server | kubectl --namespace="${NAMESPACE}" apply -f -
      kubectl -n "${NAMESPACE}" rollout status deploy/pdf-server --timeout="${ROLLOUT_TIMEOUT}"
      kubectl -n "${NAMESPACE}" get deploy,svc,pvc -l app.kubernetes.io/name=pdf-server
    fi
    if is_true "${PUBLISH_CORPUS}"; then
      publish_manifest="$(mktemp)"
      trap 'rm -f "${publish_manifest}"' EXIT
      render_publisher >"${publish_manifest}"
      publish_job="$(awk '/^kind: Job$/{found=1} found && /^  name:/{print $2; exit}' "${publish_manifest}")"
      kubectl --namespace="${NAMESPACE}" create -f "${publish_manifest}"
      kubectl --namespace="${NAMESPACE}" wait --for=condition=complete \
        "job/${publish_job}" --timeout="${PUBLISH_DEADLINE_SECONDS}s"
      rm -f "${publish_manifest}"
      trap - EXIT
    fi
    ;;
  *)
    cat >&2 <<EOF
Unknown TARGET '${TARGET}'.
Use TARGET=local for Docker Compose or TARGET=kubernetes for the cluster PDF server.
EOF
    exit 1
    ;;
esac
