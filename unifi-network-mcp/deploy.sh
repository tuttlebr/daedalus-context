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
# Preserve values explicitly supplied by the caller's environment while still
# allowing the repo-local .env to override the workspace .env.
# shellcheck source=/dev/null
source "${ORIGINAL_ENV_FILE}"
rm -f "${ORIGINAL_ENV_FILE}"
trap - EXIT

NAMESPACE="${UNIFI_MCP_NAMESPACE:-unifi-network-mcp-server}"
MANIFEST="${UNIFI_MCP_MANIFEST:-${SCRIPT_DIR}/deploy/unifi-network-mcp-server.yaml}"
UNIFI_MCP_IMAGE="${UNIFI_MCP_IMAGE:-}"
BUILD_IMAGE="${BUILD_IMAGE:-false}"
PUSH_IMAGE="${PUSH_IMAGE:-false}"
DRY_RUN="${DRY_RUN:-false}"
ROLLOUT_RESTART="${ROLLOUT_RESTART:-false}"
ROLLOUT_TIMEOUT="${ROLLOUT_TIMEOUT:-10m}"
UNIFI_BASE_URL="${UNIFI_BASE_URL:-}"
UNIFI_INSECURE_SKIP_VERIFY="${UNIFI_INSECURE_SKIP_VERIFY:-}"
UNIFI_API_KEY="${UNIFI_API_KEY:-}"
UNIFI_CONTROLLER_CIDR="${UNIFI_CONTROLLER_CIDR:-}"
UNIFI_MCP_NODE_PORT="${UNIFI_MCP_NODE_PORT:-30081}"
MCP_EXTERNAL_CLIENT_CIDR="${MCP_EXTERNAL_CLIENT_CIDR:-192.168.1.0/24}"
EXPLICIT_MCP_AUTH_TOKEN="${UNIFI_MCP_AUTH_TOKEN:-}"
MCP_AUTH_TOKEN="${UNIFI_MCP_AUTH_TOKEN:-${MCP_AUTH_TOKEN:-}}"
MCP_AUTH_TOKEN_NEXT="${UNIFI_MCP_AUTH_TOKEN_NEXT:-${MCP_AUTH_TOKEN_NEXT:-}}"
PRESERVE_EXISTING_MCP_AUTH_SECRET=false
PRESERVE_EXISTING_UNIFI_API_SECRET=false

require_command kubectl
require_command python3
if ! is_true "${DRY_RUN}" && { is_true "${BUILD_IMAGE}" || is_true "${PUSH_IMAGE}"; }; then
  require_command docker
fi

cd "${SCRIPT_DIR}"

# A routine reconcile may preserve the already-managed fixed-controller
# configuration. New deployments still require every value explicitly.
if ! is_true "${DRY_RUN}" && \
   kubectl -n "${NAMESPACE}" get deployment unifi-network-mcp-server >/dev/null 2>&1; then
  if [[ -z "${UNIFI_BASE_URL}" ]]; then
    UNIFI_BASE_URL="$(
      kubectl -n "${NAMESPACE}" get configmap unifi-network-mcp-defaults \
        -o jsonpath='{.data.base-url}'
    )"
  fi
  if [[ -z "${UNIFI_INSECURE_SKIP_VERIFY}" ]]; then
    UNIFI_INSECURE_SKIP_VERIFY="$(
      kubectl -n "${NAMESPACE}" get configmap unifi-network-mcp-defaults \
        -o jsonpath='{.data.insecure-skip-verify}'
    )"
  fi
  if [[ -z "${UNIFI_CONTROLLER_CIDR}" ]]; then
    if kubectl -n "${NAMESPACE}" get ciliumnetworkpolicy \
        unifi-network-mcp-server-netpol >/dev/null 2>&1; then
      UNIFI_CONTROLLER_CIDR="$(
        kubectl -n "${NAMESPACE}" get ciliumnetworkpolicy unifi-network-mcp-server-netpol \
          -o jsonpath='{range .spec.egress[*].toCIDR[*]}{.}{"\n"}{end}' | \
          sed '/^$/d' | head -n 1
      )"
    else
      UNIFI_CONTROLLER_CIDR="$(
        kubectl -n "${NAMESPACE}" get networkpolicy unifi-network-mcp-server-netpol \
          -o jsonpath='{range .spec.egress[*].to[*]}{.ipBlock.cidr}{"\n"}{end}' | \
          sed '/^$/d' | head -n 1
      )"
    fi
  fi
  if [[ -z "${UNIFI_API_KEY}" ]] && \
     kubectl -n "${NAMESPACE}" get secret unifi-network-mcp-unifi-api >/dev/null 2>&1; then
    PRESERVE_EXISTING_UNIFI_API_SECRET=true
  fi
  if [[ -z "${EXPLICIT_MCP_AUTH_TOKEN}" ]] && \
     kubectl -n "${NAMESPACE}" get secret unifi-network-mcp-auth >/dev/null 2>&1; then
    PRESERVE_EXISTING_MCP_AUTH_SECRET=true
  fi
fi

if [[ -z "${UNIFI_MCP_IMAGE}" ]] && ! is_true "${DRY_RUN}"; then
  echo "UNIFI_MCP_IMAGE is required and must use a SemVer tag (repository:MAJOR.MINOR.PATCH)." >&2
  exit 1
fi
if [[ -z "${UNIFI_MCP_IMAGE}" ]]; then
  UNIFI_MCP_IMAGE="example.invalid/unifi-network-mcp:0.0.0"
fi
require_semver_image_ref "UNIFI_MCP_IMAGE" "${UNIFI_MCP_IMAGE}"
APP_VERSION="$(semver_image_tag "${UNIFI_MCP_IMAGE}")"
if ! [[ "${UNIFI_MCP_NODE_PORT}" =~ ^[0-9]+$ ]] || \
   (( UNIFI_MCP_NODE_PORT < 30000 || UNIFI_MCP_NODE_PORT > 32767 )); then
  echo "UNIFI_MCP_NODE_PORT must be in the Kubernetes NodePort range 30000-32767." >&2
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
if [[ -z "${UNIFI_CONTROLLER_CIDR}" ]] && ! is_true "${DRY_RUN}"; then
  echo "UNIFI_CONTROLLER_CIDR is required to restrict production egress." >&2
  exit 1
fi
UNIFI_CONTROLLER_CIDR="${UNIFI_CONTROLLER_CIDR:-192.0.2.1/32}"
if ! python3 - "${UNIFI_CONTROLLER_CIDR}" <<'PY'
import ipaddress
import sys

try:
    ipaddress.ip_network(sys.argv[1], strict=False)
except ValueError:
    raise SystemExit(1)
PY
then
  echo "UNIFI_CONTROLLER_CIDR must be a valid IPv4 or IPv6 CIDR." >&2
  exit 1
fi
if [[ -z "${UNIFI_BASE_URL}" || \
      ( -z "${UNIFI_API_KEY}" && "${PRESERVE_EXISTING_UNIFI_API_SECRET}" != "true" ) ]] && \
   ! is_true "${DRY_RUN}"; then
  echo "UNIFI_BASE_URL and UNIFI_API_KEY are required in fixed-controller production mode." >&2
  exit 1
fi

if is_true "${PRESERVE_EXISTING_MCP_AUTH_SECRET}"; then
  AUTH_CHECKSUM="$(
    kubectl -n "${NAMESPACE}" get secret unifi-network-mcp-auth \
      -o jsonpath='{.metadata.resourceVersion}' | sha256sum | awk '{print $1}'
  )"
else
  AUTH_CHECKSUM="$(
    printf 'current=%s\nnext=%s\n' "${MCP_AUTH_TOKEN}" "${MCP_AUTH_TOKEN_NEXT}" | \
      sha256sum | awk '{print $1}'
  )"
fi
if is_true "${PRESERVE_EXISTING_UNIFI_API_SECRET}"; then
  unifi_api_key_checksum_material="$(
    kubectl -n "${NAMESPACE}" get secret unifi-network-mcp-unifi-api \
      -o jsonpath='{.metadata.resourceVersion}'
  )"
else
  unifi_api_key_checksum_material="${UNIFI_API_KEY}"
fi
CONTROLLER_CHECKSUM="$(
  printf 'url=%s\nkey-revision=%s\ninsecure=%s\ncidr=%s\n' \
    "${UNIFI_BASE_URL}" "${unifi_api_key_checksum_material}" \
    "${UNIFI_INSECURE_SKIP_VERIFY}" "${UNIFI_CONTROLLER_CIDR}" | \
    sha256sum | awk '{print $1}'
)"

RENDERED_MANIFEST="$(mktemp)"
trap 'rm -f "${RENDERED_MANIFEST}"' EXIT
awk \
  -v image="${UNIFI_MCP_IMAGE}" \
  -v app_version="${APP_VERSION}" \
  -v target_namespace="${NAMESPACE}" \
  -v auth_checksum="${AUTH_CHECKSUM}" \
  -v controller_checksum="${CONTROLLER_CHECKSUM}" \
  -v controller_cidr="${UNIFI_CONTROLLER_CIDR}" \
  -v node_port="${UNIFI_MCP_NODE_PORT}" \
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
    gsub(/__UNIFI_MCP_IMAGE__/, image)
    gsub(/__APP_VERSION__/, app_version)
    gsub(/__AUTH_CHECKSUM__/, auth_checksum)
    gsub(/__CONTROLLER_CHECKSUM__/, controller_checksum)
    gsub(/__UNIFI_CONTROLLER_CIDR__/, controller_cidr)
    gsub(/__UNIFI_MCP_NODE_PORT__/, node_port)
    gsub(/__MCP_EXTERNAL_CLIENT_CIDR__/, external_client_cidr)
  }
  { print }
' "${MANIFEST}" >"${RENDERED_MANIFEST}"

if [[ -z "${MCP_AUTH_TOKEN:-}" ]] && ! is_true "${PRESERVE_EXISTING_MCP_AUTH_SECRET}"; then
  cat >&2 <<EOF
MCP_AUTH_TOKEN is required for the default authenticated Kubernetes deployment.
Set it in ${WORKSPACE_DIR}/.env, ${SCRIPT_DIR}/.env, or the current environment.
EOF
  exit 1
fi

if ! is_true "${DRY_RUN}" && is_true "${BUILD_IMAGE}"; then
  UNIFI_MCP_COMPOSE_IMAGE="${UNIFI_MCP_COMPOSE_IMAGE:-daedalus.ddns.me:5050/btuttle/unifi-network-mcp-server:0.2.1}" \
    docker compose --project-directory "${WORKSPACE_DIR}" \
      --file "${WORKSPACE_DIR}/docker-compose.yaml" \
      build unifi-network-mcp-server
fi

if ! is_true "${DRY_RUN}" && is_true "${PUSH_IMAGE}"; then
  UNIFI_MCP_COMPOSE_IMAGE="${UNIFI_MCP_COMPOSE_IMAGE:-daedalus.ddns.me:5050/btuttle/unifi-network-mcp-server:0.2.1}" \
    docker compose --project-directory "${WORKSPACE_DIR}" \
      --file "${WORKSPACE_DIR}/docker-compose.yaml" \
      push unifi-network-mcp-server
fi

AUTH_ENV_FILE="$(mktemp)"
API_ENV_FILE="$(mktemp)"
chmod 600 "${AUTH_ENV_FILE}" "${API_ENV_FILE}"
printf 'token=%s\n' "${MCP_AUTH_TOKEN}" >"${AUTH_ENV_FILE}"
if [[ -n "${MCP_AUTH_TOKEN_NEXT}" ]]; then
  printf 'next-token=%s\n' "${MCP_AUTH_TOKEN_NEXT}" >>"${AUTH_ENV_FILE}"
fi
printf 'api-key=%s\n' "${UNIFI_API_KEY}" >"${API_ENV_FILE}"
trap 'rm -f "${RENDERED_MANIFEST}" "${AUTH_ENV_FILE}" "${API_ENV_FILE}"' EXIT

if is_true "${DRY_RUN}"; then
  kubectl apply --dry-run=client -f "${RENDERED_MANIFEST}"
  kubectl -n "${NAMESPACE}" create configmap unifi-network-mcp-defaults \
    --from-literal=base-url="${UNIFI_BASE_URL}" \
    --from-literal=insecure-skip-verify="${UNIFI_INSECURE_SKIP_VERIFY}" \
    --dry-run=client -o yaml >/dev/null
  kubectl -n "${NAMESPACE}" create secret generic unifi-network-mcp-auth \
    --from-env-file="${AUTH_ENV_FILE}" \
    --dry-run=client -o yaml >/dev/null
  if [[ -n "${UNIFI_API_KEY}" ]]; then
    kubectl -n "${NAMESPACE}" create secret generic unifi-network-mcp-unifi-api \
      --from-env-file="${API_ENV_FILE}" \
      --dry-run=client -o yaml >/dev/null
  fi
  exit 0
fi

kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

echo "Syncing UniFi server defaults..."
kubectl -n "${NAMESPACE}" create configmap unifi-network-mcp-defaults \
  --from-literal=base-url="${UNIFI_BASE_URL}" \
  --from-literal=insecure-skip-verify="${UNIFI_INSECURE_SKIP_VERIFY}" \
  --dry-run=client -o yaml | kubectl apply -f -

if is_true "${PRESERVE_EXISTING_MCP_AUTH_SECRET}"; then
  echo "Preserving existing MCP auth Secret."
else
  echo "Syncing MCP auth secret..."
  kubectl -n "${NAMESPACE}" create secret generic unifi-network-mcp-auth \
    --from-env-file="${AUTH_ENV_FILE}" \
    --dry-run=client -o yaml | kubectl apply -f -
fi

if is_true "${PRESERVE_EXISTING_UNIFI_API_SECRET}"; then
  echo "Preserving existing UniFi API key Secret."
elif [[ -n "${UNIFI_API_KEY}" ]]; then
  echo "Syncing optional UniFi API key default secret..."
  kubectl -n "${NAMESPACE}" create secret generic unifi-network-mcp-unifi-api \
    --from-env-file="${API_ENV_FILE}" \
    --dry-run=client -o yaml | kubectl apply -f -
else
  echo "Removing optional UniFi API key default secret (UNIFI_API_KEY is empty)..."
  kubectl -n "${NAMESPACE}" delete secret unifi-network-mcp-unifi-api --ignore-not-found
fi

kubectl apply -f "${RENDERED_MANIFEST}"
# The NodePort-aware Cilium policy supersedes deployments created before this
# service was restored to external-by-default operation.
kubectl -n "${NAMESPACE}" delete networkpolicy \
  unifi-network-mcp-server-netpol --ignore-not-found

if is_true "${ROLLOUT_RESTART}"; then
  kubectl -n "${NAMESPACE}" rollout restart deployment/unifi-network-mcp-server
fi
kubectl -n "${NAMESPACE}" rollout status deployment/unifi-network-mcp-server --timeout="${ROLLOUT_TIMEOUT}"
kubectl -n "${NAMESPACE}" get all -o wide
