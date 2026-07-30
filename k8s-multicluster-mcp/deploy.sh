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

NAMESPACE="${K8S_MCP_NAMESPACE:-k8s-mcp-server}"
MANIFEST="${K8S_MCP_MANIFEST:-${SCRIPT_DIR}/deploy/k8s-mcp-server.yaml}"
BUILD_IMAGE="${BUILD_IMAGE:-false}"
PUSH_IMAGE="${PUSH_IMAGE:-false}"
DRY_RUN="${DRY_RUN:-false}"
ROLLOUT_RESTART="${ROLLOUT_RESTART:-false}"
ROLLOUT_TIMEOUT="${ROLLOUT_TIMEOUT:-10m}"
MCP_AUTH_TOKEN="${K8S_MCP_AUTH_TOKEN:-${MCP_AUTH_TOKEN:-}}"
MCP_AUTH_TOKEN_NEXT="${K8S_MCP_AUTH_TOKEN_NEXT:-${MCP_AUTH_TOKEN_NEXT:-}}"
K8S_MCP_IMAGE="${K8S_MCP_IMAGE:-}"
K8S_MCP_NODE_PORT="${K8S_MCP_NODE_PORT:-30080}"
MCP_EXTERNAL_CLIENT_CIDR="${MCP_EXTERNAL_CLIENT_CIDR:-192.168.1.0/24}"

require_command kubectl
require_command python3
if ! is_true "${DRY_RUN}" && { is_true "${BUILD_IMAGE}" || is_true "${PUSH_IMAGE}"; }; then
  require_command docker
fi

cd "${SCRIPT_DIR}"

if [[ -z "${K8S_MCP_IMAGE}" ]] && ! is_true "${DRY_RUN}"; then
  cat >&2 <<EOF
K8S_MCP_IMAGE is required and must use a SemVer tag (repository:MAJOR.MINOR.PATCH).
Build and publish the versioned image before deploying it.
EOF
  exit 1
fi
if [[ -z "${K8S_MCP_IMAGE}" ]]; then
  K8S_MCP_IMAGE="example.invalid/k8s-mcp-server:0.0.0"
fi
require_semver_image_ref "K8S_MCP_IMAGE" "${K8S_MCP_IMAGE}"
APP_VERSION="$(semver_image_tag "${K8S_MCP_IMAGE}")"
if ! [[ "${K8S_MCP_NODE_PORT}" =~ ^[0-9]+$ ]] || \
   (( K8S_MCP_NODE_PORT < 30000 || K8S_MCP_NODE_PORT > 32767 )); then
  echo "K8S_MCP_NODE_PORT must be in the Kubernetes NodePort range 30000-32767." >&2
  exit 1
fi
if ! python3 - "${MCP_EXTERNAL_CLIENT_CIDR}" <<'PY'
import ipaddress
import sys

try:
    ipaddress.ip_network(sys.argv[1])
except ValueError:
    raise SystemExit(1)
PY
then
  echo "MCP_EXTERNAL_CLIENT_CIDR must be a valid network CIDR." >&2
  exit 1
fi
if ! is_true "${DRY_RUN}"; then
  cilium_source_ranges="$(
    kubectl -n kube-system get configmap cilium-config \
      -o jsonpath='{.data.bpf-lb-source-range-all-types}' 2>/dev/null || true
  )"
  if [[ "${cilium_source_ranges}" != "true" ]]; then
    cat >&2 <<'EOF'
Cilium must set bpf.lbSourceRangeAllTypes=true before deploying an MCP NodePort.
Without it, loadBalancerSourceRanges does not enforce the external client CIDR
on NodePort traffic.
EOF
    exit 1
  fi
fi
AUTH_CHECKSUM="$(
  printf 'current=%s\nnext=%s\n' "${MCP_AUTH_TOKEN}" "${MCP_AUTH_TOKEN_NEXT}" | sha256sum | awk '{print $1}'
)"
AGENT_CONFIG_CHECKSUM="$(
  printf 'base=%s\nkey=%s\nmodel=%s\n' \
    "${OPENCODE_BASE_URL:-}" "${OPENCODE_API_KEY:-}" "${OPENCODE_MODEL:-}" | sha256sum | awk '{print $1}'
)"

RENDERED_MANIFEST="$(mktemp)"
trap 'rm -f "${RENDERED_MANIFEST}"' EXIT
awk \
  -v target_namespace="${NAMESPACE}" \
  -v image="${K8S_MCP_IMAGE}" \
  -v app_version="${APP_VERSION}" \
  -v auth_checksum="${AUTH_CHECKSUM}" \
  -v agent_config_checksum="${AGENT_CONFIG_CHECKSUM}" \
  -v node_port="${K8S_MCP_NODE_PORT}" \
  -v external_client_cidr="${MCP_EXTERNAL_CLIENT_CIDR}" '
  /^---[[:space:]]*$/ {
    kind = ""
    namespace_name_replaced = 0
    print
    next
  }
  $1 == "kind:" {
    kind = $2
    print
    next
  }
  kind == "Namespace" && $1 == "name:" && !namespace_name_replaced {
    indent = substr($0, 1, index($0, "name:") - 1)
    print indent "name: " target_namespace
    namespace_name_replaced = 1
    next
  }
  $1 == "namespace:" {
    indent = substr($0, 1, index($0, "namespace:") - 1)
    print indent "namespace: " target_namespace
    next
  }
  {
    gsub(/__K8S_MCP_IMAGE__/, image)
    gsub(/__APP_VERSION__/, app_version)
    gsub(/__AUTH_CHECKSUM__/, auth_checksum)
    gsub(/__AGENT_CONFIG_CHECKSUM__/, agent_config_checksum)
    gsub(/__K8S_MCP_NODE_PORT__/, node_port)
    gsub(/__MCP_EXTERNAL_CLIENT_CIDR__/, external_client_cidr)
  }
  { print }
' "${MANIFEST}" >"${RENDERED_MANIFEST}"

if [[ -z "${MCP_AUTH_TOKEN:-}" ]]; then
  cat >&2 <<EOF
MCP_AUTH_TOKEN is required for the default authenticated Kubernetes deployment.
Set it in ${WORKSPACE_DIR}/.env, ${SCRIPT_DIR}/.env, or the current environment.
EOF
  exit 1
fi

if ! is_true "${DRY_RUN}" && is_true "${BUILD_IMAGE}"; then
  K8S_MCP_COMPOSE_IMAGE="${K8S_MCP_COMPOSE_IMAGE:-daedalus.ddns.me:5050/btuttle/k8s-mcp-server:0.1.0}" \
    docker compose --project-directory "${WORKSPACE_DIR}" \
      --file "${WORKSPACE_DIR}/docker-compose.yaml" \
      build k8s-mcp-server-read-only
fi

if ! is_true "${DRY_RUN}" && is_true "${PUSH_IMAGE}"; then
  K8S_MCP_COMPOSE_IMAGE="${K8S_MCP_COMPOSE_IMAGE:-daedalus.ddns.me:5050/btuttle/k8s-mcp-server:0.1.0}" \
    docker compose --project-directory "${WORKSPACE_DIR}" \
      --file "${WORKSPACE_DIR}/docker-compose.yaml" \
      push k8s-mcp-server-read-only
fi

AUTH_ENV_FILE="$(mktemp)"
AGENT_ENV_FILE="$(mktemp)"
chmod 600 "${AUTH_ENV_FILE}" "${AGENT_ENV_FILE}"
printf 'token=%s\n' "${MCP_AUTH_TOKEN}" >"${AUTH_ENV_FILE}"
if [[ -n "${MCP_AUTH_TOKEN_NEXT}" ]]; then
  printf 'next-token=%s\n' "${MCP_AUTH_TOKEN_NEXT}" >>"${AUTH_ENV_FILE}"
fi
printf 'base-url=%s\napi-key=%s\nmodel=%s\n' \
  "${OPENCODE_BASE_URL:-}" "${OPENCODE_API_KEY:-}" "${OPENCODE_MODEL:-}" >"${AGENT_ENV_FILE}"
trap 'rm -f "${RENDERED_MANIFEST}" "${AUTH_ENV_FILE}" "${AGENT_ENV_FILE}"' EXIT

if is_true "${DRY_RUN}"; then
  kubectl apply --dry-run=client -f "${RENDERED_MANIFEST}"
  kubectl -n "${NAMESPACE}" create secret generic k8s-mcp-auth \
    --from-env-file="${AUTH_ENV_FILE}" \
    --dry-run=client -o yaml >/dev/null
  if [[ -n "${OPENCODE_BASE_URL:-}" || -n "${OPENCODE_API_KEY:-}" || -n "${OPENCODE_MODEL:-}" ]]; then
    kubectl -n "${NAMESPACE}" create secret generic k8s-mcp-agent-config \
      --from-env-file="${AGENT_ENV_FILE}" \
      --dry-run=client -o yaml >/dev/null
  fi
  exit 0
fi

kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

echo "Syncing MCP auth secret..."
kubectl -n "${NAMESPACE}" create secret generic k8s-mcp-auth \
  --from-env-file="${AUTH_ENV_FILE}" \
  --dry-run=client -o yaml | kubectl apply -f -

if [[ -n "${OPENCODE_BASE_URL:-}" || -n "${OPENCODE_API_KEY:-}" || -n "${OPENCODE_MODEL:-}" ]]; then
  echo "Syncing agent-config secret..."
  kubectl -n "${NAMESPACE}" create secret generic k8s-mcp-agent-config \
    --from-env-file="${AGENT_ENV_FILE}" \
    --dry-run=client -o yaml | kubectl apply -f -
else
  echo "Removing agent-config secret (no OPENCODE_* vars set)..."
  kubectl -n "${NAMESPACE}" delete secret k8s-mcp-agent-config --ignore-not-found
fi

kubectl apply -f "${RENDERED_MANIFEST}"

if is_true "${ROLLOUT_RESTART}"; then
  kubectl -n "${NAMESPACE}" rollout restart deployment/k8s-mcp-server
fi
kubectl -n "${NAMESPACE}" rollout status deployment/k8s-mcp-server --timeout="${ROLLOUT_TIMEOUT}"
if kubectl -n "${NAMESPACE}" get daemonset k8s-mcp-server >/dev/null 2>&1; then
  echo "Removing ready Deployment's legacy DaemonSet..."
  kubectl -n "${NAMESPACE}" delete daemonset k8s-mcp-server --wait=true
fi
kubectl -n "${NAMESPACE}" get all -o wide
