#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

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

require_env() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    echo "Required environment variable ${name} is not set." >&2
    echo "Set it in ${WORKSPACE_DIR}/.env, ${SCRIPT_DIR}/.env, or the current environment." >&2
    exit 1
  fi
}

is_true() {
  case "${1:-}" in
    true|TRUE|1|yes|YES) return 0 ;;
    *) return 1 ;;
  esac
}

add_set_string() {
  helm_args+=(--set-string "$1=$2")
}

add_set() {
  helm_args+=(--set "$1=$2")
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --help|-h)
      cat <<EOF
Usage: ./deploy.sh

Deploys the NV-Ingest service.

This script does not trigger document ingestion. To run ingestion jobs after
NV-Ingest is deployed, use:
  cd ../url-ingest && ./deploy.sh --run-now [COLLECTION...]
EOF
      exit 0
      ;;
    --run-now)
      cat >&2 <<EOF
--run-now is not supported by nv-ingest/deploy.sh.

NV-Ingest is the service. URL/PDF ingestion jobs are triggered from url-ingest:
  cd ../url-ingest && ./deploy.sh --run-now [COLLECTION...]
EOF
      exit 2
      ;;
    *)
      echo "Unknown option: $1" >&2
      echo "Run ./deploy.sh --help for usage." >&2
      exit 2
      ;;
  esac
done

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

if [[ -z "${NGC_API_KEY:-}" && -n "${NVIDIA_API_KEY:-}" ]]; then
  NGC_API_KEY="${NVIDIA_API_KEY}"
fi

NAMESPACE="${NAMESPACE:-nv-ingest}"
RELEASE_NAME="${RELEASE_NAME:-nv-ingest}"
NV_INGEST_CHART_URL="${NV_INGEST_CHART_URL:-https://helm.ngc.nvidia.com/nvidia/nemo-microservices/charts/nv-ingest-26.3.0.tgz}"
NV_INGEST_IMAGE_REPOSITORY="${NV_INGEST_IMAGE_REPOSITORY:-nvcr.io/nvidia/nemo-microservices/nv-ingest}"
NV_INGEST_IMAGE_TAG="${NV_INGEST_IMAGE_TAG:-26.3.0}"
NV_INGEST_REPLICA_COUNT="${NV_INGEST_REPLICA_COUNT:-3}"
NV_INGEST_SERVICE_TYPE="${NV_INGEST_SERVICE_TYPE:-LoadBalancer}"
# Use '-' rather than ':-' so an explicitly empty caller value stays empty.
# This matters when the root stack selects ClusterIP and intentionally omits a
# nodePort.
NV_INGEST_NODE_PORT="${NV_INGEST_NODE_PORT-30767}"
HELM_TIMEOUT="${HELM_TIMEOUT:-20m}"
DRY_RUN="${DRY_RUN:-false}"
ADOPT_EXISTING_SECRETS="${ADOPT_EXISTING_SECRETS:-false}"
URL_INGEST_NAMESPACE="${URL_INGEST_NAMESPACE:-${MILVUS_NAMESPACE:-milvus}}"
ALLOW_DEPLOY_DURING_INGEST="${ALLOW_DEPLOY_DURING_INGEST:-false}"

# Keep object-store credentials out of Helm values and release metadata. The
# root stack points these at the authoritative Milvus MinIO Secret; direct
# deployments can continue to source MINIO_ACCESS_KEY/MINIO_SECRET_KEY from
# the environment.
MINIO_CREDENTIAL_SECRET_NAME="${MINIO_CREDENTIAL_SECRET_NAME:-${RELEASE_NAME}-object-store}"
MINIO_SOURCE_SECRET_NAMESPACE="${MINIO_SOURCE_SECRET_NAMESPACE:-}"
MINIO_SOURCE_SECRET_NAME="${MINIO_SOURCE_SECRET_NAME:-}"

MINIO_BUCKET="${MINIO_BUCKET:-nv-ingest}"
MILVUS_ENDPOINT="${MILVUS_ENDPOINT:-${MILVUS_URI:-http://milvus.milvus.svc.cluster.local:19530}}"
MINIO_INTERNAL_ADDRESS="${MINIO_INTERNAL_ADDRESS:-${MINIO_ENDPOINT:-http://milvus-minio.milvus.svc.cluster.local:9000}}"
MINIO_PUBLIC_ADDRESS="${MINIO_PUBLIC_ADDRESS:-${MINIO_ENDPOINT:-http://milvus-minio.milvus.svc.cluster.local:9000}}"
IMAGE_STORAGE_URI="${IMAGE_STORAGE_URI:-file:///workspace/data/artifacts/store/images}"

EMBEDDING_NIM_ENDPOINT="${EMBEDDING_NIM_ENDPOINT:-http://nemotron-retriever-embedder.daedalus-retriever.svc.cluster.local:8000/v1}"
EMBEDDING_NIM_MODEL_NAME="${EMBEDDING_NIM_MODEL_NAME:-nvidia/llama-nemotron-embed-vl-1b-v2}"
OCR_INFER_PROTOCOL="${OCR_INFER_PROTOCOL:-http}"
OCR_MODEL_NAME="${OCR_MODEL_NAME:-scene_text_ensemble}"
YOLOX_INFER_PROTOCOL="${YOLOX_INFER_PROTOCOL:-http}"
YOLOX_GRAPHIC_ELEMENTS_INFER_PROTOCOL="${YOLOX_GRAPHIC_ELEMENTS_INFER_PROTOCOL:-http}"
YOLOX_TABLE_STRUCTURE_INFER_PROTOCOL="${YOLOX_TABLE_STRUCTURE_INFER_PROTOCOL:-http}"
VLM_CAPTION_MODEL_NAME="${VLM_CAPTION_MODEL_NAME:-nvidia/NVIDIA-Nemotron-Nano-12B-v2}"
NEMOTRON_PARSE_INFER_PROTOCOL="${NEMOTRON_PARSE_INFER_PROTOCOL:-http}"
NEMOTRON_PARSE_MODEL_NAME="${NEMOTRON_PARSE_MODEL_NAME:-nvidia/nemotron-parse}"
COMPONENTS_TO_READY_CHECK="${COMPONENTS_TO_READY_CHECK:-}"

require_command helm
require_command kubectl
require_command sha256sum

require_env NGC_API_KEY
require_env EMBEDDING_NIM_ENDPOINT
require_env OCR_HTTP_ENDPOINT
require_env YOLOX_HTTP_ENDPOINT
require_env YOLOX_GRAPHIC_ELEMENTS_HTTP_ENDPOINT
require_env YOLOX_TABLE_STRUCTURE_HTTP_ENDPOINT
require_env VLM_CAPTION_ENDPOINT
require_env NEMOTRON_PARSE_HTTP_ENDPOINT

# Validate all caller-controlled settings and resolve the source credentials
# before changing anything in the destination namespace.
case "${NV_INGEST_SERVICE_TYPE}" in
  ClusterIP|NodePort|LoadBalancer) ;;
  *)
    echo "NV_INGEST_SERVICE_TYPE must be ClusterIP, NodePort, or LoadBalancer (got ${NV_INGEST_SERVICE_TYPE})." >&2
    exit 1
    ;;
esac

if [[ "${NV_INGEST_SERVICE_TYPE}" == "ClusterIP" && -n "${NV_INGEST_NODE_PORT}" ]]; then
  echo "NV_INGEST_NODE_PORT must be empty when NV_INGEST_SERVICE_TYPE=ClusterIP." >&2
  exit 1
fi
if [[ -n "${NV_INGEST_NODE_PORT}" && ! "${NV_INGEST_NODE_PORT}" =~ ^[0-9]+$ ]]; then
  echo "NV_INGEST_NODE_PORT must be an integer when set." >&2
  exit 1
fi
if ! [[ "${NV_INGEST_REPLICA_COUNT}" =~ ^[0-9]+$ ]] || (( NV_INGEST_REPLICA_COUNT < 1 )); then
  echo "NV_INGEST_REPLICA_COUNT must be an integer greater than zero." >&2
  exit 1
fi
if (( NV_INGEST_REPLICA_COUNT > 1 )) && [[ "${IMAGE_STORAGE_URI}" == file://* ]]; then
  cat >&2 <<EOF
IMAGE_STORAGE_URI=${IMAGE_STORAGE_URI} is pod-local, but NV_INGEST_REPLICA_COUNT=${NV_INGEST_REPLICA_COUNT}.
Multiple API replicas must share NVIDIA-supported durable artifact storage (for
example, an approved s3:// bucket/prefix). Refusing a deployment that can lose
artifacts when a pod is rescheduled.
EOF
  exit 1
fi
if [[ "${MINIO_CREDENTIAL_SECRET_NAME}" == "ngc-secret" || "${MINIO_CREDENTIAL_SECRET_NAME}" == "ngc-api" ]]; then
  echo "MINIO_CREDENTIAL_SECRET_NAME must not collide with ngc-secret or ngc-api." >&2
  exit 1
fi

if [[ -n "${MINIO_SOURCE_SECRET_NAMESPACE}" || -n "${MINIO_SOURCE_SECRET_NAME}" ]]; then
  if [[ -z "${MINIO_SOURCE_SECRET_NAMESPACE}" || -z "${MINIO_SOURCE_SECRET_NAME}" ]]; then
    echo "MINIO_SOURCE_SECRET_NAMESPACE and MINIO_SOURCE_SECRET_NAME must be set together." >&2
    exit 1
  fi

  if [[ "${MINIO_SOURCE_SECRET_NAMESPACE}" == "${NAMESPACE}" ]]; then
    case "${MINIO_SOURCE_SECRET_NAME}" in
      "${MINIO_CREDENTIAL_SECRET_NAME}"|ngc-secret|ngc-api)
        echo "The MinIO source Secret must not be any destination Secret managed by this deploy." >&2
        exit 1
        ;;
    esac
  fi

  # Read both credential fields from one Kubernetes object snapshot so a
  # concurrent source rotation cannot produce a torn access/secret key pair.
  source_credentials="$(kubectl -n "${MINIO_SOURCE_SECRET_NAMESPACE}" \
    get secret "${MINIO_SOURCE_SECRET_NAME}" \
    -o go-template='{{ index .data "accesskey" | base64decode }}{{ "\n" }}{{ index .data "secretkey" | base64decode }}')"
  if [[ "${source_credentials}" != *$'\n'* ]]; then
    echo "Unable to read a complete credential pair from ${MINIO_SOURCE_SECRET_NAMESPACE}/${MINIO_SOURCE_SECRET_NAME}." >&2
    exit 1
  fi
  MINIO_ACCESS_KEY="${source_credentials%%$'\n'*}"
  MINIO_SECRET_KEY="${source_credentials#*$'\n'}"
  if [[ -z "${MINIO_ACCESS_KEY}" || "${MINIO_ACCESS_KEY}" == "<no value>" || \
        -z "${MINIO_SECRET_KEY}" || "${MINIO_SECRET_KEY}" == "<no value>" ]]; then
    echo "Secret ${MINIO_SOURCE_SECRET_NAMESPACE}/${MINIO_SOURCE_SECRET_NAME} must contain accesskey and secretkey." >&2
    exit 1
  fi
fi

require_env MINIO_ACCESS_KEY
require_env MINIO_SECRET_KEY

cd "${SCRIPT_DIR}"

# Fetch and authenticate the chart before creating or changing Kubernetes
# resources. A bad chart URL or NGC credential therefore fails cleanly.
helm show chart "${NV_INGEST_CHART_URL}" \
  --username '$oauthtoken' \
  --password "${NGC_API_KEY}" >/dev/null

release_manifest_contains_secret() {
  local secret_name="$1"
  local release_manifest

  if ! release_manifest="$(helm get manifest "${RELEASE_NAME}" --namespace "${NAMESPACE}")"; then
    echo "Unable to inspect the current Helm manifest for ${NAMESPACE}/${RELEASE_NAME}." >&2
    exit 1
  fi

  awk -v target="${secret_name}" '
    function finish_document() {
      if (kind == "Secret" && name == target) {
        found = 1
      }
    }
    /^---[[:space:]]*$/ {
      finish_document()
      kind = ""
      name = ""
      in_metadata = 0
      next
    }
    /^kind:[[:space:]]*/ {
      kind = $0
      sub(/^kind:[[:space:]]*/, "", kind)
      next
    }
    /^metadata:[[:space:]]*$/ {
      in_metadata = 1
      next
    }
    in_metadata && /^[^[:space:]]/ {
      in_metadata = 0
    }
    in_metadata && /^  name:[[:space:]]*/ {
      name = $0
      sub(/^  name:[[:space:]]*/, "", name)
      sub(/[[:space:]]+#.*/, "", name)
      sub(/[[:space:]]+$/, "", name)
    }
    END {
      finish_document()
      exit(found ? 0 : 1)
    }
  ' <<<"${release_manifest}"
}

preflight_secret_ownership() {
  local secret_name="$1"
  local allow_current_release="$2"
  local existing_secret owner_name owner_namespace managed_by external_manager keep_policy

  existing_secret="$(kubectl -n "${NAMESPACE}" get secret "${secret_name}" \
    --ignore-not-found -o name)"
  if [[ -z "${existing_secret}" ]]; then
    return 0
  fi

  owner_name="$(kubectl -n "${NAMESPACE}" get secret "${secret_name}" \
    -o jsonpath='{.metadata.annotations.meta\.helm\.sh/release-name}')"
  owner_namespace="$(kubectl -n "${NAMESPACE}" get secret "${secret_name}" \
    -o jsonpath='{.metadata.annotations.meta\.helm\.sh/release-namespace}')"
  managed_by="$(kubectl -n "${NAMESPACE}" get secret "${secret_name}" \
    -o jsonpath='{.metadata.labels.app\.kubernetes\.io/managed-by}')"
  external_manager="$(kubectl -n "${NAMESPACE}" get secret "${secret_name}" \
    -o jsonpath='{.metadata.annotations.daedalus\.nvidia\.com/managed-by}')"
  keep_policy="$(kubectl -n "${NAMESPACE}" get secret "${secret_name}" \
    -o jsonpath='{.metadata.annotations.helm\.sh/resource-policy}')"

  if [[ -z "${owner_name}" && -z "${owner_namespace}" && "${managed_by}" != "Helm" ]]; then
    if [[ "${managed_by}" == "daedalus-context" && \
          "${external_manager}" == "nv-ingest-deploy" ]]; then
      return 0
    fi
    if is_true "${ADOPT_EXISTING_SECRETS}"; then
      echo "Adopting existing Secret ${NAMESPACE}/${secret_name} by explicit request."
      return 0
    fi
    cat >&2 <<EOF
Refusing to overwrite existing unmanaged Secret ${NAMESPACE}/${secret_name}.
Set ADOPT_EXISTING_SECRETS=true only if this deploy is intended to take ownership
and replace that Secret's credentials.
EOF
    exit 1
  fi

  if [[ "${allow_current_release}" == "true" && "${release_exists}" == "true" ]]; then
    if [[ "${owner_name}" == "${RELEASE_NAME}" && "${owner_namespace}" == "${NAMESPACE}" ]]; then
      return 0
    fi
    # Accept only our own explicitly marked, partially completed ownership
    # transfer. Merely having malformed or incomplete Helm metadata is not
    # sufficient authorization to adopt the Secret.
    if [[ "${keep_policy}" == "keep" && "${external_manager}" == "nv-ingest-deploy" && \
          ( -z "${owner_name}" || "${owner_name}" == "${RELEASE_NAME}" ) && \
          ( -z "${owner_namespace}" || "${owner_namespace}" == "${NAMESPACE}" ) ]]; then
      return 0
    fi
  fi

  echo "Refusing to overwrite Secret ${NAMESPACE}/${secret_name}: it is managed by Helm" >&2
  if [[ -n "${owner_namespace}" || -n "${owner_name}" ]]; then
    echo "for release ${owner_namespace:-<unknown>}/${owner_name:-<unknown>}." >&2
  else
    echo "but has no complete Helm ownership annotations." >&2
  fi
  exit 1
}

secret_owned_by_current_release() {
  local secret_name="$1"
  local owner_name owner_namespace

  owner_name="$(kubectl -n "${NAMESPACE}" get secret "${secret_name}" \
    -o jsonpath='{.metadata.annotations.meta\.helm\.sh/release-name}')"
  owner_namespace="$(kubectl -n "${NAMESPACE}" get secret "${secret_name}" \
    -o jsonpath='{.metadata.annotations.meta\.helm\.sh/release-namespace}')"
  [[ "${owner_name}" == "${RELEASE_NAME}" && "${owner_namespace}" == "${NAMESPACE}" ]]
}

secret_resource_version() {
  local secret_name="$1"
  local resource_version

  resource_version="$(kubectl -n "${NAMESPACE}" get secret "${secret_name}" \
    -o jsonpath='{.metadata.resourceVersion}')"
  if [[ -z "${resource_version}" ]]; then
    echo "Secret ${NAMESPACE}/${secret_name} has no resourceVersion." >&2
    exit 1
  fi
  printf '%s' "${resource_version}"
}

revision_checksum() {
  printf '%s\n' "$@" | sha256sum | awk '{print $1}'
}

mark_secret_external() {
  local secret_name="$1"
  local owner_name owner_namespace managed_by keep_policy external_manager

  owner_name="$(kubectl -n "${NAMESPACE}" get secret "${secret_name}" \
    -o jsonpath='{.metadata.annotations.meta\.helm\.sh/release-name}')"
  owner_namespace="$(kubectl -n "${NAMESPACE}" get secret "${secret_name}" \
    -o jsonpath='{.metadata.annotations.meta\.helm\.sh/release-namespace}')"
  managed_by="$(kubectl -n "${NAMESPACE}" get secret "${secret_name}" \
    -o jsonpath='{.metadata.labels.app\.kubernetes\.io/managed-by}')"
  keep_policy="$(kubectl -n "${NAMESPACE}" get secret "${secret_name}" \
    -o jsonpath='{.metadata.annotations.helm\.sh/resource-policy}')"
  external_manager="$(kubectl -n "${NAMESPACE}" get secret "${secret_name}" \
    -o jsonpath='{.metadata.annotations.daedalus\.nvidia\.com/managed-by}')"

  # Establish the keep/recovery metadata before removing either Helm owner
  # annotation. This ordering makes an interrupted migration safely resumable.
  if [[ "${keep_policy}" != "keep" ]]; then
    kubectl -n "${NAMESPACE}" annotate secret "${secret_name}" \
      helm.sh/resource-policy=keep --overwrite >/dev/null
  fi
  if [[ "${external_manager}" != "nv-ingest-deploy" ]]; then
    kubectl -n "${NAMESPACE}" annotate secret "${secret_name}" \
      daedalus.nvidia.com/managed-by=nv-ingest-deploy --overwrite >/dev/null
  fi
  if [[ "${managed_by}" != "daedalus-context" ]]; then
    kubectl -n "${NAMESPACE}" label secret "${secret_name}" \
      app.kubernetes.io/managed-by=daedalus-context --overwrite >/dev/null
  fi
  if [[ -n "${owner_name}" ]]; then
    kubectl -n "${NAMESPACE}" annotate secret "${secret_name}" \
      meta.helm.sh/release-name- >/dev/null
  fi
  if [[ -n "${owner_namespace}" ]]; then
    kubectl -n "${NAMESPACE}" annotate secret "${secret_name}" \
      meta.helm.sh/release-namespace- >/dev/null
  fi
}

refresh_revision_checksums() {
  local ngc_pull_resource_version ngc_api_resource_version object_store_resource_version

  ngc_pull_resource_version="$(secret_resource_version ngc-secret)"
  ngc_api_resource_version="$(secret_resource_version ngc-api)"
  object_store_resource_version="$(secret_resource_version "${MINIO_CREDENTIAL_SECRET_NAME}")"

  NGC_REVISION_CHECKSUM="$(revision_checksum \
    ngc-secret "${ngc_pull_resource_version}" \
    ngc-api "${ngc_api_resource_version}")"
  OBJECT_STORE_REVISION_CHECKSUM="$(revision_checksum \
    "${MINIO_CREDENTIAL_SECRET_NAME}" "${object_store_resource_version}")"
}

assert_no_active_ingestion() {
  local namespace_resource job_rows active_jobs configmap_rows active_locks

  if is_true "${ALLOW_DEPLOY_DURING_INGEST}"; then
    echo "WARNING: deploying NV-Ingest while ingestion may be active by explicit request." >&2
    return 0
  fi

  namespace_resource="$(kubectl get namespace "${URL_INGEST_NAMESPACE}" \
    --ignore-not-found -o name)"
  if [[ -z "${namespace_resource}" ]]; then
    return 0
  fi

  if ! job_rows="$(kubectl -n "${URL_INGEST_NAMESPACE}" get jobs \
      --selector='app.kubernetes.io/name=url-ingest' \
      -o custom-columns='NAME:.metadata.name,ACTIVE:.status.active' \
      --no-headers)"; then
    echo "Unable to inspect URL-ingest Jobs in ${URL_INGEST_NAMESPACE}; refusing an unsafe rollout." >&2
    exit 1
  fi
  active_jobs="$(awk '$2 ~ /^[1-9][0-9]*$/ {print $1}' <<<"${job_rows}")"

  if ! configmap_rows="$(kubectl -n "${URL_INGEST_NAMESPACE}" get configmaps -o name)"; then
    echo "Unable to inspect URL-ingest locks in ${URL_INGEST_NAMESPACE}; refusing an unsafe rollout." >&2
    exit 1
  fi
  active_locks="$(grep -E '^configmap/url-ingest-.+-run-lock$' \
    <<<"${configmap_rows}" || true)"

  if [[ -n "${active_jobs}" || -n "${active_locks}" ]]; then
    cat >&2 <<EOF
Refusing to roll out NV-Ingest while URL ingestion is active in ${URL_INGEST_NAMESPACE}.
Terminating NV-Ingest pods can orphan in-flight Ray jobs even when other replicas
remain ready. Wait for these Jobs/locks to clear, then retry the deployment.
Active Jobs:
${active_jobs:-<none>}
Run locks:
${active_locks:-<none>}
Set ALLOW_DEPLOY_DURING_INGEST=true only for an intentional emergency rollout.
EOF
    exit 1
  fi
}

configure_rollout_strategy() {
  local deployment_name="${RELEASE_NAME}"
  local deployment_resource

  deployment_resource="$(kubectl -n "${NAMESPACE}" get deployment "${deployment_name}" \
    --ignore-not-found -o name)"
  if [[ -z "${deployment_resource}" ]]; then
    return
  fi

  # The upstream chart does not expose Deployment strategy values. Required
  # hostname anti-affinity can therefore deadlock its default surge-first
  # rollout when every schedulable failure domain already contains an old
  # replica. Retire one replica before creating its replacement so the rollout
  # remains schedulable while keeping N-1 replicas available.
  kubectl -n "${NAMESPACE}" patch deployment "${deployment_name}" \
    --type=merge \
    --patch '{"spec":{"strategy":{"type":"RollingUpdate","rollingUpdate":{"maxUnavailable":1,"maxSurge":0}}}}'
}

build_helm_args() {
  local create_ngc_pull_secret="$1"
  local create_ngc_api_secret="$2"
  local ngc_revision_checksum="$3"
  local object_store_revision_checksum="$4"

  helm_args=(
    upgrade --install "${RELEASE_NAME}" "${NV_INGEST_CHART_URL}"
    --namespace "${NAMESPACE}"
    --create-namespace
    --username '$oauthtoken'
    --password "${NGC_API_KEY}"
    --post-renderer "${SCRIPT_DIR}/helm-post-renderer.sh"
    --wait
    --timeout "${HELM_TIMEOUT}"
  )

  add_set replicaCount "${NV_INGEST_REPLICA_COUNT}"
  add_set_string extraEnvVarsSecret "${MINIO_CREDENTIAL_SECRET_NAME}"
  add_set_string 'podAnnotations.daedalus\.nvidia\.com/object-store-secret-revision-checksum' \
    "${object_store_revision_checksum}"
  add_set_string 'podAnnotations.daedalus\.nvidia\.com/ngc-secret-revision-checksum' \
    "${ngc_revision_checksum}"
  add_set ngcImagePullSecret.create "${create_ngc_pull_secret}"
  add_set ngcApiSecret.create "${create_ngc_api_secret}"
  if [[ "${create_ngc_pull_secret}" == "true" ]]; then
    add_set_string ngcImagePullSecret.password "${NGC_API_KEY}"
  fi
  if [[ "${create_ngc_api_secret}" == "true" ]]; then
    add_set_string ngcApiSecret.password "${NGC_API_KEY}"
  fi
  add_set milvusDeployed false

  add_set_string envVars.EMBEDDING_NIM_ENDPOINT "${EMBEDDING_NIM_ENDPOINT}"
  add_set_string envVars.EMBEDDING_NIM_MODEL_NAME "${EMBEDDING_NIM_MODEL_NAME}"
  add_set_string envVars.OCR_HTTP_ENDPOINT "${OCR_HTTP_ENDPOINT}"
  add_set_string envVars.OCR_INFER_PROTOCOL "${OCR_INFER_PROTOCOL}"
  add_set_string envVars.OCR_MODEL_NAME "${OCR_MODEL_NAME}"
  add_set_string envVars.IMAGE_STORAGE_URI "${IMAGE_STORAGE_URI}"
  add_set_string envVars.YOLOX_HTTP_ENDPOINT "${YOLOX_HTTP_ENDPOINT}"
  add_set_string envVars.YOLOX_INFER_PROTOCOL "${YOLOX_INFER_PROTOCOL}"
  add_set_string envVars.YOLOX_GRAPHIC_ELEMENTS_HTTP_ENDPOINT "${YOLOX_GRAPHIC_ELEMENTS_HTTP_ENDPOINT}"
  add_set_string envVars.YOLOX_GRAPHIC_ELEMENTS_INFER_PROTOCOL "${YOLOX_GRAPHIC_ELEMENTS_INFER_PROTOCOL}"
  add_set_string envVars.YOLOX_TABLE_STRUCTURE_HTTP_ENDPOINT "${YOLOX_TABLE_STRUCTURE_HTTP_ENDPOINT}"
  add_set_string envVars.YOLOX_TABLE_STRUCTURE_INFER_PROTOCOL "${YOLOX_TABLE_STRUCTURE_INFER_PROTOCOL}"
  add_set_string envVars.VLM_CAPTION_MODEL_NAME "${VLM_CAPTION_MODEL_NAME}"
  add_set_string envVars.VLM_CAPTION_ENDPOINT "${VLM_CAPTION_ENDPOINT}"
  add_set_string envVars.MILVUS_ENDPOINT "${MILVUS_ENDPOINT}"
  add_set_string envVars.NEMOTRON_PARSE_HTTP_ENDPOINT "${NEMOTRON_PARSE_HTTP_ENDPOINT}"
  add_set_string envVars.NEMOTRON_PARSE_INFER_PROTOCOL "${NEMOTRON_PARSE_INFER_PROTOCOL}"
  add_set_string envVars.NEMOTRON_PARSE_MODEL_NAME "${NEMOTRON_PARSE_MODEL_NAME}"
  add_set_string envVars.COMPONENTS_TO_READY_CHECK "${COMPONENTS_TO_READY_CHECK}"
  # Remove the chart's insecure literal defaults. The same variables are
  # loaded from extraEnvVarsSecret above.
  add_set envVars.MINIO_ACCESS_KEY null
  add_set envVars.MINIO_SECRET_KEY null
  add_set_string envVars.MINIO_PUBLIC_ADDRESS "${MINIO_PUBLIC_ADDRESS}"
  add_set_string envVars.MINIO_INTERNAL_ADDRESS "${MINIO_INTERNAL_ADDRESS}"
  add_set_string envVars.MINIO_BUCKET "${MINIO_BUCKET}"

  add_set_string service.type "${NV_INGEST_SERVICE_TYPE}"
  if [[ -n "${NV_INGEST_NODE_PORT}" ]]; then
    add_set service.nodePort "${NV_INGEST_NODE_PORT}"
  fi
  add_set_string image.repository "${NV_INGEST_IMAGE_REPOSITORY}"
  add_set_string image.tag "${NV_INGEST_IMAGE_TAG}"

  # Admission replicas are useful only when they occupy distinct failure
  # domains. Refuse bin-packing by making hostname anti-affinity required.
  add_set_string affinity.podAntiAffinity.requiredDuringSchedulingIgnoredDuringExecution[0].topologyKey kubernetes.io/hostname
  add_set_string 'affinity.podAntiAffinity.requiredDuringSchedulingIgnoredDuringExecution[0].labelSelector.matchLabels.app\.kubernetes\.io/name' nv-ingest
  add_set_string 'affinity.podAntiAffinity.requiredDuringSchedulingIgnoredDuringExecution[0].labelSelector.matchLabels.app\.kubernetes\.io/instance' "${RELEASE_NAME}"

  if [[ -n "${NV_INGEST_NODE_HOSTNAMES:-}" ]]; then
    add_set_string affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].key kubernetes.io/hostname
    add_set_string affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].operator In

    IFS=',' read -r -a node_hostnames <<< "${NV_INGEST_NODE_HOSTNAMES}"
    node_index=0
    for node_hostname in "${node_hostnames[@]}"; do
      node_hostname="${node_hostname#"${node_hostname%%[![:space:]]*}"}"
      node_hostname="${node_hostname%"${node_hostname##*[![:space:]]}"}"
      if [[ -n "${node_hostname}" ]]; then
        add_set_string "affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].values[${node_index}]" "${node_hostname}"
        node_index=$((node_index + 1))
      fi
    done
  fi
}

# Replica availability does not drain or preserve NV-Ingest's in-flight Ray
# work. Enforce this before the render/mutation phase so a routine upgrade
# cannot strand a synchronous seed Job on a terminated API pod.
if ! is_true "${DRY_RUN}"; then
  assert_no_active_ingestion
fi

# Exercise chart fetching, value coercion, and manifest generation before any
# namespace or Secret mutation. Secret creation is disabled in this render, so
# sensitive values are not stored in Helm's dry-run output.
PREFLIGHT_NGC_REVISION_CHECKSUM="$(revision_checksum ngc-secret preflight ngc-api preflight)"
PREFLIGHT_OBJECT_STORE_REVISION_CHECKSUM="$(revision_checksum \
  "${MINIO_CREDENTIAL_SECRET_NAME}" preflight)"
build_helm_args false false \
  "${PREFLIGHT_NGC_REVISION_CHECKSUM}" "${PREFLIGHT_OBJECT_STORE_REVISION_CHECKSUM}"
helm "${helm_args[@]}" --dry-run >/dev/null

if is_true "${DRY_RUN}"; then
  # Dry renders make no cluster mutations. Stable, non-sensitive placeholders
  # keep the resulting Deployment semantically equivalent to a real rollout.
  NGC_REVISION_CHECKSUM="$(revision_checksum ngc-secret dry-run ngc-api dry-run)"
  OBJECT_STORE_REVISION_CHECKSUM="$(revision_checksum "${MINIO_CREDENTIAL_SECRET_NAME}" dry-run)"
  build_helm_args false false "${NGC_REVISION_CHECKSUM}" "${OBJECT_STORE_REVISION_CHECKSUM}"
  helm_args+=(--dry-run --debug)
  helm "${helm_args[@]}"
  printf '%s\n' '---'
  sed \
    -e "s|__NAMESPACE__|${NAMESPACE}|g" \
    -e "s|__RELEASE_NAME__|${RELEASE_NAME}|g" \
    -e "s|__MIN_AVAILABLE__|$(( NV_INGEST_REPLICA_COUNT > 1 ? NV_INGEST_REPLICA_COUNT - 1 : 1 ))|g" \
    "${SCRIPT_DIR}/k8s/pdb.yaml"
  exit 0
fi

# Determine whether the current release still owns either fixed-name NGC
# Secret. Helm deletes resources removed from a release, so these resources
# need a one-time keep-policy revision before ownership can be transferred.
release_exists=false
migrate_ngc_pull_secret=false
migrate_ngc_api_secret=false
namespace_resource="$(kubectl get namespace "${NAMESPACE}" --ignore-not-found -o name)"
if [[ -n "${namespace_resource}" ]]; then
  release_matches="$(helm list --all --short --filter "^${RELEASE_NAME}$" --namespace "${NAMESPACE}")"
  if [[ "${release_matches}" == "${RELEASE_NAME}" ]]; then
    release_exists=true
    if release_manifest_contains_secret ngc-secret; then
      migrate_ngc_pull_secret=true
    fi
    if release_manifest_contains_secret ngc-api; then
      migrate_ngc_api_secret=true
    fi
  fi
fi

preflight_secret_ownership ngc-secret true
preflight_secret_ownership ngc-api true
preflight_secret_ownership "${MINIO_CREDENTIAL_SECRET_NAME}" false

# All validations above are read-only. Mutations start here.
configure_rollout_strategy
kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "${NAMESPACE}" create secret docker-registry ngc-secret \
  --docker-server=nvcr.io \
  --docker-username='$oauthtoken' \
  --docker-password="${NGC_API_KEY}" \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "${NAMESPACE}" create secret generic ngc-api \
  --from-literal=NGC_API_KEY="${NGC_API_KEY}" \
  --from-literal=NGC_CLI_API_KEY="${NGC_API_KEY}" \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "${NAMESPACE}" create secret generic "${MINIO_CREDENTIAL_SECRET_NAME}" \
  --from-literal=MINIO_ACCESS_KEY="${MINIO_ACCESS_KEY}" \
  --from-literal=MINIO_SECRET_KEY="${MINIO_SECRET_KEY}" \
  --dry-run=client -o yaml | kubectl apply -f -
mark_secret_external "${MINIO_CREDENTIAL_SECRET_NAME}"

ownership_migration_in_progress=false
if [[ "${migrate_ngc_pull_secret}" == "true" || "${migrate_ngc_api_secret}" == "true" ]]; then
  echo "Migrating Helm-owned NGC Secrets to externally managed Secrets."
  refresh_revision_checksums
  build_helm_args "${migrate_ngc_pull_secret}" "${migrate_ngc_api_secret}" \
    "${NGC_REVISION_CHECKSUM}" "${OBJECT_STORE_REVISION_CHECKSUM}"
  helm_args+=(--atomic)
  helm "${helm_args[@]}"
  ownership_migration_in_progress=true
fi

refresh_revision_checksums

build_helm_args false false "${NGC_REVISION_CHECKSUM}" "${OBJECT_STORE_REVISION_CHECKSUM}"
# Helm cannot atomically roll back across a revision where kept resources are
# removed from the release manifest: the rollback diff has no current-manifest
# object to update. Keep this one finalization revision forward-only and retain
# Secret ownership until it succeeds. A failed finalization is safe to retry;
# ordinary Secret-free revisions remain atomic.
ownership_boundary_active=false
if secret_owned_by_current_release ngc-secret || secret_owned_by_current_release ngc-api; then
  ownership_boundary_active=true
fi
if [[ "${ownership_migration_in_progress}" == "true" || \
      "${ownership_boundary_active}" == "true" ]]; then
  echo "Finalizing the one-time NGC Secret migration (forward-only revision)."
else
  helm_args+=(--atomic)
fi
helm "${helm_args[@]}"

# Keep Helm ownership until the forward-only Secret-free revision succeeds. If
# that non-atomic upgrade fails, the kept Secrets retain their transition
# ownership and a retry can finish the boundary crossing safely. Once the final
# revision is deployed, they can safely become externally managed.
mark_secret_external ngc-secret
mark_secret_external ngc-api

# Externalization changes Secret resourceVersions. Reconcile once more only
# when that changed the checksum, now with both the current and rollback
# manifests safely Secret-free.
finalized_ngc_revision_checksum="${NGC_REVISION_CHECKSUM}"
refresh_revision_checksums
if [[ "${NGC_REVISION_CHECKSUM}" != "${finalized_ngc_revision_checksum}" ]]; then
  echo "Reconciling the post-migration NGC Secret revision checksum."
  build_helm_args false false "${NGC_REVISION_CHECKSUM}" "${OBJECT_STORE_REVISION_CHECKSUM}"
  helm_args+=(--atomic)
  helm "${helm_args[@]}"
fi

sed \
  -e "s|__NAMESPACE__|${NAMESPACE}|g" \
  -e "s|__RELEASE_NAME__|${RELEASE_NAME}|g" \
  -e "s|__MIN_AVAILABLE__|$(( NV_INGEST_REPLICA_COUNT > 1 ? NV_INGEST_REPLICA_COUNT - 1 : 1 ))|g" \
  "${SCRIPT_DIR}/k8s/pdb.yaml" | kubectl apply -f -

kubectl get pods,svc -n "${NAMESPACE}"
