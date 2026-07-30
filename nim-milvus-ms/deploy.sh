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

NAMESPACE=${NAMESPACE:-"milvus"}
RELEASE_NAME=${RELEASE_NAME:-"milvus"}
CHART=${CHART:-"zilliztech/milvus"}
CHART_VERSION=${CHART_VERSION:-"5.0.14"}
MINIO_SECRET_NAME=${MINIO_SECRET_NAME:-"milvus-minio-credentials"}
MILVUS_ROOT_SECRET_NAME=${MILVUS_ROOT_SECRET_NAME:-"milvus-root-credentials"}
MINIO_REPLICAS=${MINIO_REPLICAS:-"4"}
MINIO_STORAGE_CLASS=${MINIO_STORAGE_CLASS:-}
ETCD_STORAGE_CLASS=${ETCD_STORAGE_CLASS:-"local-etcd"}
MINIO_DOCUMENT_BUCKET=${MINIO_DOCUMENT_BUCKET:-"nv-ingest"}
HELM_TIMEOUT=${HELM_TIMEOUT:-"15m"}
DRY_RUN=${DRY_RUN:-"false"}
EXPOSE_NODEPORTS=${EXPOSE_NODEPORTS:-"false"}
ENABLE_MONITORING=${ENABLE_MONITORING:-"false"}
COLD_START_DEPENDENCY_BOOTSTRAP=${COLD_START_DEPENDENCY_BOOTSTRAP:-"true"}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

create_secret_or_fail() {
  local secret_name=$1
  shift
  local missing_message=$1
  shift

  if kubectl -n "${NAMESPACE}" get secret "${secret_name}" >/dev/null 2>&1; then
    return
  fi

  if "$@"; then
    return
  fi

  cat >&2 <<EOF
Missing required secret '${secret_name}' in namespace '${NAMESPACE}'.

${missing_message}
EOF
  exit 1
}

create_minio_secret_from_env() {
  if [[ -z "${MINIO_ACCESS_KEY:-}" || -z "${MINIO_SECRET_KEY:-}" ]]; then
    return 1
  fi

  if [[ "${MINIO_ACCESS_KEY}" == "minioadmin" || "${MINIO_SECRET_KEY}" == "minioadmin" ]]; then
    echo "Refusing to create a MinIO Secret with the upstream minioadmin default." >&2
    exit 1
  fi

  echo "Creating MinIO credential secret ${MINIO_SECRET_NAME} from environment..."
  kubectl -n "${NAMESPACE}" create secret generic "${MINIO_SECRET_NAME}" \
    --from-literal=accesskey="${MINIO_ACCESS_KEY}" \
    --from-literal=secretkey="${MINIO_SECRET_KEY}" \
    --dry-run=client -o yaml | kubectl apply -f -
}

create_milvus_root_secret_from_env() {
  if [[ -z "${MILVUS_ROOT_PASSWORD:-}" ]]; then
    return 1
  fi

  echo "Creating Milvus root credential secret ${MILVUS_ROOT_SECRET_NAME} from environment..."
  kubectl -n "${NAMESPACE}" create secret generic "${MILVUS_ROOT_SECRET_NAME}" \
    --from-literal=password="${MILVUS_ROOT_PASSWORD}" \
    --dry-run=client -o yaml | kubectl apply -f -
}

require_secret_key() {
  local secret_name=$1
  local key=$2
  local encoded

  encoded=$(kubectl -n "${NAMESPACE}" get secret "${secret_name}" \
    -o "jsonpath={.data.${key}}")
  if [[ -z "${encoded}" ]]; then
    echo "Secret '${secret_name}' in namespace '${NAMESPACE}' must contain a non-empty '${key}' key." >&2
    exit 1
  fi
}

reject_secret_value() {
  local secret_name=$1
  local key=$2
  local forbidden_encoded=$3
  local encoded

  encoded=$(kubectl -n "${NAMESPACE}" get secret "${secret_name}" \
    -o "jsonpath={.data.${key}}")
  if [[ "${encoded}" == "${forbidden_encoded}" ]]; then
    echo "Secret '${secret_name}' in namespace '${NAMESPACE}' uses a forbidden default value for '${key}'." >&2
    exit 1
  fi
}

is_true() {
  case "$1" in
    true|TRUE|1|yes|YES) return 0 ;;
    *) return 1 ;;
  esac
}

if ! [[ "${MINIO_REPLICAS}" =~ ^[0-9]+$ ]] || (( MINIO_REPLICAS < 4 )); then
  echo "MINIO_REPLICAS must be an integer greater than or equal to 4." >&2
  exit 1
fi

if [[ -z "${ETCD_STORAGE_CLASS}" ]]; then
  echo "ETCD_STORAGE_CLASS must be set." >&2
  exit 1
fi

if ! [[ "${MINIO_DOCUMENT_BUCKET}" =~ ^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$ ]]; then
  echo "MINIO_DOCUMENT_BUCKET must be a valid 3-63 character S3 bucket name." >&2
  exit 1
fi

require_command helm
require_command kubectl
require_command sha256sum

cd "${SCRIPT_DIR}"

helm show chart "${CHART}" --version "${CHART_VERSION}" >/dev/null

# StatefulSet volumeClaimTemplates are immutable. A routine reconcile must
# preserve the storage class of an existing MinIO release; changing it requires
# an explicit data migration. local-etcd remains forbidden for a new MinIO
# release, but an already-bound legacy release can still be reconciled safely.
if ! is_true "${DRY_RUN}"; then
  existing_minio_storage_class="$(
    kubectl -n "${NAMESPACE}" get statefulset "${RELEASE_NAME}-minio" \
      -o jsonpath='{.spec.volumeClaimTemplates[0].spec.storageClassName}' \
      2>/dev/null || true
  )"
  if [[ -n "${existing_minio_storage_class}" ]]; then
    if [[ -z "${MINIO_STORAGE_CLASS}" ]]; then
      MINIO_STORAGE_CLASS="${existing_minio_storage_class}"
      echo "Reusing existing MinIO storage class ${MINIO_STORAGE_CLASS}."
    elif [[ "${MINIO_STORAGE_CLASS}" != "${existing_minio_storage_class}" ]]; then
      cat >&2 <<EOF
Refusing to change MinIO storage class from ${existing_minio_storage_class} to ${MINIO_STORAGE_CLASS}.
The existing StatefulSet and PVCs require a separately planned data migration.
EOF
      exit 1
    fi
    if [[ "${MINIO_STORAGE_CLASS}" == "local-etcd" ]]; then
      echo "WARNING: preserving local-etcd for the existing MinIO release; do not use it for a new bulk object-store deployment." >&2
    fi
  else
    if [[ -z "${MINIO_STORAGE_CLASS}" ]]; then
      echo "MINIO_STORAGE_CLASS is required for a new release; select a reviewed shared object-storage class." >&2
      exit 1
    fi
    if [[ "${MINIO_STORAGE_CLASS}" == "local-etcd" ]]; then
      echo "Refusing local-etcd for a new MinIO bulk object-store deployment." >&2
      exit 1
    fi
  fi
elif [[ -z "${MINIO_STORAGE_CLASS}" ]]; then
  echo "MINIO_STORAGE_CLASS must be set for a dry-run render." >&2
  exit 1
fi

render_pdbs() {
  sed \
    -e "s|__NAMESPACE__|${NAMESPACE}|g" \
    -e "s|__RELEASE_NAME__|${RELEASE_NAME}|g" \
    k8s/pdb.yaml
}

values_args=(-f custom-values.yaml)
if is_true "${EXPOSE_NODEPORTS}"; then
  values_args+=(-f nodeport-values.yaml)
fi
if is_true "${ENABLE_MONITORING}"; then
  values_args+=(-f monitoring-values.yaml)
  if ! is_true "${DRY_RUN}" && ! kubectl get crd servicemonitors.monitoring.coreos.com >/dev/null 2>&1; then
    echo "ENABLE_MONITORING requires the ServiceMonitor CRD." >&2
    exit 1
  fi
fi

MINIO_SECRET_REVISION_CHECKSUM=$(printf '%s' "dry-run:${MINIO_SECRET_NAME}" | sha256sum | awk '{print $1}')
MILVUS_CREDENTIALS_REVISION_CHECKSUM=$(
  printf '%s' "dry-run:${MINIO_SECRET_NAME}:${MILVUS_ROOT_SECRET_NAME}" | sha256sum | awk '{print $1}'
)

if ! is_true "${DRY_RUN}"; then
  if ! kubectl get namespace "${NAMESPACE}" >/dev/null 2>&1; then
    kubectl create namespace "${NAMESPACE}"
  fi

  create_secret_or_fail "${MINIO_SECRET_NAME}" \
    "Set MINIO_ACCESS_KEY and MINIO_SECRET_KEY in ${WORKSPACE_DIR}/.env, ${SCRIPT_DIR}/.env, or create the secret manually." \
    create_minio_secret_from_env

  create_secret_or_fail "${MILVUS_ROOT_SECRET_NAME}" \
    "Set MILVUS_ROOT_PASSWORD in ${WORKSPACE_DIR}/.env, ${SCRIPT_DIR}/.env, or create the secret manually." \
    create_milvus_root_secret_from_env

  require_secret_key "${MINIO_SECRET_NAME}" accesskey
  require_secret_key "${MINIO_SECRET_NAME}" secretkey
  require_secret_key "${MILVUS_ROOT_SECRET_NAME}" password
  reject_secret_value "${MINIO_SECRET_NAME}" accesskey bWluaW9hZG1pbg==
  reject_secret_value "${MINIO_SECRET_NAME}" secretkey bWluaW9hZG1pbg==

  minio_secret_resource_version=$(kubectl -n "${NAMESPACE}" get secret "${MINIO_SECRET_NAME}" \
    -o jsonpath='{.metadata.resourceVersion}')
  root_secret_resource_version=$(kubectl -n "${NAMESPACE}" get secret "${MILVUS_ROOT_SECRET_NAME}" \
    -o jsonpath='{.metadata.resourceVersion}')
  MINIO_SECRET_REVISION_CHECKSUM=$(
    printf '%s' "${MINIO_SECRET_NAME}:${minio_secret_resource_version}" | sha256sum | awk '{print $1}'
  )
  MILVUS_CREDENTIALS_REVISION_CHECKSUM=$(
    printf '%s' \
      "${MINIO_SECRET_NAME}:${minio_secret_resource_version}:${MILVUS_ROOT_SECRET_NAME}:${root_secret_resource_version}" |
      sha256sum | awk '{print $1}'
  )

  kubectl get storageclass "${MINIO_STORAGE_CLASS}" >/dev/null
  kubectl get storageclass "${ETCD_STORAGE_CLASS}" >/dev/null
fi

helm_args=(
  upgrade --install "${RELEASE_NAME}" "${CHART}"
  --version "${CHART_VERSION}"
  --namespace "${NAMESPACE}"
  --create-namespace
  "${values_args[@]}"
  --set "minio.existingSecret=${MINIO_SECRET_NAME}"
  --set-string "minio.accessKey="
  --set-string "minio.secretKey="
  --set "minio.replicas=${MINIO_REPLICAS}"
  --set-string "minio.persistence.storageClass=${MINIO_STORAGE_CLASS}"
  --set-string "etcd.persistence.storageClass=${ETCD_STORAGE_CLASS}"
  --set-string "proxy.topologySpreadConstraints[0].labelSelector.matchLabels.app\\.kubernetes\\.io/instance=${RELEASE_NAME}"
  --set-string "queryNode.topologySpreadConstraints[0].labelSelector.matchLabels.app\\.kubernetes\\.io/instance=${RELEASE_NAME}"
  --set-string "dataNode.topologySpreadConstraints[0].labelSelector.matchLabels.app\\.kubernetes\\.io/instance=${RELEASE_NAME}"
  --set-string "streamingNode.topologySpreadConstraints[0].labelSelector.matchLabels.app\\.kubernetes\\.io/instance=${RELEASE_NAME}"
  --set-string "mixCoordinator.topologySpreadConstraints[0].labelSelector.matchLabels.app\\.kubernetes\\.io/instance=${RELEASE_NAME}"
  --set-string "minio.buckets[1].name=${MINIO_DOCUMENT_BUCKET}"
  --set-string "annotations.daedalus\\.nvidia\\.com/credentials-revision-checksum=${MILVUS_CREDENTIALS_REVISION_CHECKSUM}"
  --set-string "minio.podAnnotations.daedalus\\.nvidia\\.com/credentials-revision-checksum=${MINIO_SECRET_REVISION_CHECKSUM}"
  --set "milvusAuthEnv[1].valueFrom.secretKeyRef.name=${MILVUS_ROOT_SECRET_NAME}"
  --set "milvusAuthEnv[4].valueFrom.secretKeyRef.name=${MINIO_SECRET_NAME}"
  --set "milvusAuthEnv[5].valueFrom.secretKeyRef.name=${MINIO_SECRET_NAME}"
  --set "proxy.extraEnv[1].valueFrom.secretKeyRef.name=${MILVUS_ROOT_SECRET_NAME}"
  --set "proxy.extraEnv[4].valueFrom.secretKeyRef.name=${MINIO_SECRET_NAME}"
  --set "proxy.extraEnv[5].valueFrom.secretKeyRef.name=${MINIO_SECRET_NAME}"
  --set "queryNode.extraEnv[1].valueFrom.secretKeyRef.name=${MILVUS_ROOT_SECRET_NAME}"
  --set "queryNode.extraEnv[4].valueFrom.secretKeyRef.name=${MINIO_SECRET_NAME}"
  --set "queryNode.extraEnv[5].valueFrom.secretKeyRef.name=${MINIO_SECRET_NAME}"
  --set "dataNode.extraEnv[1].valueFrom.secretKeyRef.name=${MILVUS_ROOT_SECRET_NAME}"
  --set "dataNode.extraEnv[4].valueFrom.secretKeyRef.name=${MINIO_SECRET_NAME}"
  --set "dataNode.extraEnv[5].valueFrom.secretKeyRef.name=${MINIO_SECRET_NAME}"
  --set "streamingNode.extraEnv[1].valueFrom.secretKeyRef.name=${MILVUS_ROOT_SECRET_NAME}"
  --set "streamingNode.extraEnv[4].valueFrom.secretKeyRef.name=${MINIO_SECRET_NAME}"
  --set "streamingNode.extraEnv[5].valueFrom.secretKeyRef.name=${MINIO_SECRET_NAME}"
  --set "mixCoordinator.extraEnv[1].valueFrom.secretKeyRef.name=${MILVUS_ROOT_SECRET_NAME}"
  --set "mixCoordinator.extraEnv[4].valueFrom.secretKeyRef.name=${MINIO_SECRET_NAME}"
  --set "mixCoordinator.extraEnv[5].valueFrom.secretKeyRef.name=${MINIO_SECRET_NAME}"
  --post-renderer "${SCRIPT_DIR}/pvc-label-post-renderer.sh"
  --wait
  --timeout "${HELM_TIMEOUT}"
)

if is_true "${DRY_RUN}"; then
  helm_args+=(--dry-run --debug)
else
  helm_args+=(--atomic)
fi

export MILVUS_PVC_RELEASE_NAME="${RELEASE_NAME}"

# On a brand-new release, bring up etcd and MinIO before starting Milvus.  The
# chart otherwise schedules all components at once, and the Milvus processes
# can restart while the etcd headless service is still becoming resolvable.
# The dependency-only revision is also a safe rollback target if the full
# application rollout fails.
if ! is_true "${DRY_RUN}" && is_true "${COLD_START_DEPENDENCY_BOOTSTRAP}" && \
  ! helm status "${RELEASE_NAME}" --namespace "${NAMESPACE}" >/dev/null 2>&1; then
  echo "Bootstrapping etcd and MinIO before the first Milvus application rollout..."
  bootstrap_args=(
    "${helm_args[@]}"
    --set proxy.replicas=0
    --set queryNode.replicas=0
    --set dataNode.replicas=0
    --set streamingNode.replicas=0
    --set mixCoordinator.replicas=0
    --atomic
  )
  helm "${bootstrap_args[@]}"
fi

helm "${helm_args[@]}"
if is_true "${DRY_RUN}"; then
  render_pdbs | kubectl --namespace="${NAMESPACE}" apply --dry-run=client -f -
else
  render_pdbs | kubectl --namespace="${NAMESPACE}" apply -f -
fi
