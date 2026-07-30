#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

NAMESPACE=${NAMESPACE:-"milvus"}
RELEASE_NAME=${RELEASE_NAME:-"milvus"}
CHART=${CHART:-"zilliztech/milvus"}
CHART_VERSION=${CHART_VERSION:-"5.0.14"}
MINIO_SECRET_NAME=${MINIO_SECRET_NAME:-"milvus-minio-credentials"}
MILVUS_ROOT_SECRET_NAME=${MILVUS_ROOT_SECRET_NAME:-"milvus-root-credentials"}
MINIO_REPLICAS=${MINIO_REPLICAS:-"4"}
MINIO_STORAGE_CLASS=${MINIO_STORAGE_CLASS:-"nfs-client"}
ETCD_STORAGE_CLASS=${ETCD_STORAGE_CLASS:-"local-etcd"}
MINIO_DOCUMENT_BUCKET=${MINIO_DOCUMENT_BUCKET:-"nv-ingest"}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

require_present() {
  local pattern=$1
  local file=$2
  local message=$3

  if ! grep -Eq "${pattern}" "${file}"; then
    echo "Validation failed: ${message}" >&2
    exit 1
  fi
}

require_absent() {
  local pattern=$1
  local file=$2
  local message=$3

  if grep -Eq "${pattern}" "${file}"; then
    echo "Validation failed: ${message}" >&2
    exit 1
  fi
}

require_fixed_count() {
  local needle=$1
  local expected=$2
  local file=$3
  local message=$4
  local actual

  actual=$(grep -Fc -- "${needle}" "${file}" || true)
  if [[ "${actual}" -ne "${expected}" ]]; then
    echo "Validation failed: ${message} (expected ${expected}, found ${actual})" >&2
    exit 1
  fi
}

require_secret_env_refs() {
  local env_name=$1
  local secret_name=$2
  local secret_key=$3
  local expected=$4
  local file=$5
  local message=$6

  if ! awk -v env_name="${env_name}" -v secret_name="${secret_name}" \
    -v secret_key="${secret_key}" -v expected="${expected}" '
      function is_active_component(value) {
        return value == "datanode" || value == "mixcoord" || value == "proxy" || \
          value == "querynode" || value == "streamingnode"
      }

      function reset_document() {
        is_deployment = 0
        component = ""
        in_reference = 0
        valid_references = 0
      }

      function finish_document() {
        if (is_deployment && is_active_component(component)) {
          components++
          if (valid_references == 1) {
            valid_components++
          }
        }
        reset_document()
      }

      BEGIN {
        reset_document()
      }

      /^---[[:space:]]*$/ {
        finish_document()
        next
      }

      /^kind:[[:space:]]*Deployment[[:space:]]*$/ {
        is_deployment = 1
      }

      is_deployment && $1 == "component:" && component == "" {
        component = $2
        gsub(/"/, "", component)
      }

      is_deployment && $0 ~ "^[[:space:]]*- name:[[:space:]]*" env_name "[[:space:]]*$" {
        in_reference = 1
        saw_key = 0
        saw_name = 0
        next
      }

      in_reference && $1 == "key:" && $2 == secret_key {
        saw_key = 1
      }

      in_reference && $1 == "name:" && $2 == secret_name {
        saw_name = 1
      }

      in_reference && saw_key && saw_name {
        valid_references++
        in_reference = 0
      }

      END {
        finish_document()
        exit components == expected && valid_components == expected ? 0 : 1
      }
    ' "${file}"; then
    echo "Validation failed: ${message}" >&2
    exit 1
  fi
}

require_literal_env_values() {
  local env_name=$1
  local expected_value=$2
  local expected_components=$3
  local file=$4
  local message=$5

  if ! awk -v env_name="${env_name}" -v expected_value="${expected_value}" \
    -v expected_components="${expected_components}" '
      function is_active_component(value) {
        return value == "datanode" || value == "mixcoord" || value == "proxy" || \
          value == "querynode" || value == "streamingnode"
      }
      function reset_document() {
        is_deployment = 0
        component = ""
        awaiting_value = 0
        valid_values = 0
      }
      function finish_document() {
        if (is_deployment && is_active_component(component)) {
          components++
          if (valid_values == 1) {
            valid_components++
          }
        }
        reset_document()
      }
      BEGIN { reset_document() }
      /^---[[:space:]]*$/ { finish_document(); next }
      /^kind:[[:space:]]*Deployment[[:space:]]*$/ { is_deployment = 1 }
      is_deployment && $1 == "component:" && component == "" {
        component = $2
        gsub(/"/, "", component)
      }
      is_deployment && $0 ~ "^[[:space:]]*- name:[[:space:]]*" env_name "[[:space:]]*$" {
        awaiting_value = 1
        next
      }
      awaiting_value && $1 == "value:" {
        value = $2
        gsub(/"/, "", value)
        if (value == expected_value) {
          valid_values++
        }
        awaiting_value = 0
      }
      END {
        finish_document()
        exit components == expected_components && valid_components == expected_components ? 0 : 1
      }
    ' "${file}"; then
    echo "Validation failed: ${message}" >&2
    exit 1
  fi
}

require_minio_replicas() {
  local minimum=$1
  local file=$2
  local message=$3

  if ! awk -v minimum="${minimum}" '
      function reset_document() {
        is_statefulset = 0
        is_minio = 0
      }

      BEGIN {
        reset_document()
      }

      /^---[[:space:]]*$/ {
        reset_document()
        next
      }

      /^kind:[[:space:]]*StatefulSet[[:space:]]*$/ {
        is_statefulset = 1
      }

      is_statefulset && /^[[:space:]]+app:[[:space:]]+minio[[:space:]]*$/ {
        is_minio = 1
      }

      is_statefulset && is_minio && /^  replicas:[[:space:]]*/ {
        found++
        if ($2 >= minimum) {
          valid++
        }
      }

      END {
        exit found == 1 && valid == 1 ? 0 : 1
      }
    ' "${file}"; then
    echo "Validation failed: ${message}" >&2
    exit 1
  fi
}

require_labeled_pvc_storage() {
  local component=$1
  local storage_class=$2
  local file=$3
  local message=$4

  if ! awk -v component="${component}" -v storage_class="${storage_class}" '
      function finish_document() {
        if (lifecycle_label && component_label && matching_storage) {
          valid++
        }
        lifecycle_label = 0
        component_label = 0
        matching_storage = 0
      }

      /^---[[:space:]]*$/ {
        finish_document()
        next
      }

      /daedalus\.nvidia\.com\/milvus-storage:[[:space:]]*"true"/ {
        lifecycle_label = 1
      }

      $1 == "app.kubernetes.io/name:" {
        value = $2
        gsub(/"/, "", value)
        if (value == component) {
          component_label = 1
        }
      }

      $1 == "storageClassName:" && $2 == storage_class {
        matching_storage = 1
      }

      END {
        finish_document()
        exit valid == 1 ? 0 : 1
      }
    ' "${file}"; then
    echo "Validation failed: ${message}" >&2
    exit 1
  fi
}

require_active_component_annotation() {
  local annotation_value=$1
  local expected=$2
  local file=$3
  local message=$4

  if ! awk -v annotation_value="${annotation_value}" -v expected="${expected}" '
      function is_active_component(value) {
        return value == "datanode" || value == "mixcoord" || value == "proxy" || \
          value == "querynode" || value == "streamingnode"
      }

      function reset_document() {
        is_deployment = 0
        component = ""
        found_annotation = 0
      }

      function finish_document() {
        if (is_deployment && is_active_component(component)) {
          components++
          if (found_annotation) {
            valid_components++
          }
        }
        reset_document()
      }

      BEGIN {
        reset_document()
      }

      /^---[[:space:]]*$/ {
        finish_document()
        next
      }

      /^kind:[[:space:]]*Deployment[[:space:]]*$/ {
        is_deployment = 1
      }

      is_deployment && $1 == "component:" && component == "" {
        component = $2
        gsub(/"/, "", component)
      }

      is_deployment && $1 == "daedalus.nvidia.com/credentials-revision-checksum:" && \
        $2 == annotation_value {
        found_annotation = 1
      }

      END {
        finish_document()
        exit components == expected && valid_components == expected ? 0 : 1
      }
    ' "${file}"; then
    echo "Validation failed: ${message}" >&2
    exit 1
  fi
}

require_active_component_replicas() {
  local expected_replicas=$1
  local expected_components=$2
  local file=$3
  local message=$4

  if ! awk -v expected_replicas="${expected_replicas}" -v expected_components="${expected_components}" '
      function is_active_component(value) {
        return value == "datanode" || value == "mixcoord" || value == "proxy" || \
          value == "querynode" || value == "streamingnode"
      }

      function reset_document() {
        is_deployment = 0
        component = ""
        replicas = ""
      }

      function finish_document() {
        if (is_deployment && is_active_component(component)) {
          components++
          if (replicas == expected_replicas) {
            valid_components++
          }
        }
        reset_document()
      }

      BEGIN {
        reset_document()
      }

      /^---[[:space:]]*$/ {
        finish_document()
        next
      }

      /^kind:[[:space:]]*Deployment[[:space:]]*$/ {
        is_deployment = 1
      }

      is_deployment && $1 == "component:" && component == "" {
        component = $2
        gsub(/"/, "", component)
      }

      is_deployment && /^  replicas:[[:space:]]*/ {
        replicas = $2
      }

      END {
        finish_document()
        exit components == expected_components && valid_components == expected_components ? 0 : 1
      }
    ' "${file}"; then
    echo "Validation failed: ${message}" >&2
    exit 1
  fi
}

require_no_pdb_conflicts() {
  local file=$1
  local message=$2

  if ! awk '
    function finish_document() {
      if (is_pdb && min_available && max_unavailable) {
        print pdb_name > "/dev/stderr"
        found_conflict = 1
      }

      is_pdb = 0
      pdb_name = "<unnamed>"
      min_available = 0
      max_unavailable = 0
    }

    BEGIN {
      pdb_name = "<unnamed>"
    }

    /^---[[:space:]]*$/ {
      finish_document()
      next
    }

    /^kind:[[:space:]]*PodDisruptionBudget[[:space:]]*$/ {
      is_pdb = 1
    }

    is_pdb && /^[[:space:]]+name:[[:space:]]*/ && pdb_name == "<unnamed>" {
      pdb_name = $2
      gsub(/"/, "", pdb_name)
    }

    is_pdb && /^[[:space:]]*minAvailable:/ {
      min_available = 1
    }

    is_pdb && /^[[:space:]]*maxUnavailable:/ {
      max_unavailable = 1
    }

    END {
      finish_document()
      exit found_conflict ? 1 : 0
    }
  ' "${file}"; then
    echo "Validation failed: ${message}" >&2
    exit 1
  fi
}

render_default=$(mktemp)
render_nodeport=$(mktemp)
render_bootstrap=$(mktemp)
trap 'rm -f "${render_default}" "${render_nodeport}" "${render_bootstrap}"' EXIT

require_command helm
require_command sha256sum

MINIO_SECRET_REVISION_CHECKSUM=$(printf '%s' validation-minio-secret | sha256sum | awk '{print $1}')
MILVUS_CREDENTIALS_REVISION_CHECKSUM=$(printf '%s' validation-milvus-secrets | sha256sum | awk '{print $1}')

if ! [[ "${MINIO_REPLICAS}" =~ ^[0-9]+$ ]] || (( MINIO_REPLICAS < 4 )); then
  echo "Validation failed: MINIO_REPLICAS must be an integer greater than or equal to 4" >&2
  exit 1
fi

if [[ -z "${MINIO_STORAGE_CLASS}" || -z "${ETCD_STORAGE_CLASS}" ]]; then
  echo "Validation failed: MINIO_STORAGE_CLASS and ETCD_STORAGE_CLASS must both be set" >&2
  exit 1
fi

if ! [[ "${MINIO_DOCUMENT_BUCKET}" =~ ^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$ ]]; then
  echo "Validation failed: MINIO_DOCUMENT_BUCKET must be a valid 3-63 character S3 bucket name" >&2
  exit 1
fi

cd "${SCRIPT_DIR}"
export MILVUS_PVC_RELEASE_NAME="${RELEASE_NAME}"

helm_overrides=(
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
)

helm template "${RELEASE_NAME}" "${CHART}" \
  --version "${CHART_VERSION}" \
  --namespace "${NAMESPACE}" \
  -f custom-values.yaml \
  "${helm_overrides[@]}" \
  > "${render_default}"

require_present 'helm.sh/chart: milvus-5\.0\.14' "${render_default}" "Milvus chart version must be pinned to 5.0.14"
require_present 'app.kubernetes.io/version: "2\.6\.11"' "${render_default}" "Milvus app version must render as 2.6.11"
require_present 'image: "milvusdb/milvus:v2\.6\.11"' "${render_default}" "Milvus image must render as v2.6.11"
require_present "name: ${MINIO_SECRET_NAME}" "${render_default}" "MinIO pods must reference the configured existing secret"
require_present "name: ${MILVUS_ROOT_SECRET_NAME}" "${render_default}" "Milvus pods must reference the configured root credential secret"
require_secret_env_refs MINIO_ACCESS_KEY_ID "${MINIO_SECRET_NAME}" accesskey 5 "${render_default}" \
  "Every active Milvus component must source MINIO_ACCESS_KEY_ID from ${MINIO_SECRET_NAME}/accesskey"
require_secret_env_refs MINIO_SECRET_ACCESS_KEY "${MINIO_SECRET_NAME}" secretkey 5 "${render_default}" \
  "Every active Milvus component must source MINIO_SECRET_ACCESS_KEY from ${MINIO_SECRET_NAME}/secretkey"
require_active_component_annotation "${MILVUS_CREDENTIALS_REVISION_CHECKSUM}" 5 "${render_default}" \
  "Every active Milvus component must carry the credentials revision checksum"
require_literal_env_values COMMON_SECURITY_ROOTSHOULDBINDROLE false 5 "${render_default}" \
  "Every active Milvus component must keep root on the supported superuser path"
require_fixed_count "daedalus.nvidia.com/credentials-revision-checksum: ${MINIO_SECRET_REVISION_CHECKSUM}" 1 \
  "${render_default}" "The MinIO pod template must carry its Secret revision checksum"
require_present 'authorizationEnabled: true' "${render_default}" "Milvus auth must be enabled in user config"
require_present 'rootShouldBindRole: false' "${render_default}" \
  "Milvus root must not depend on persisted role membership"
require_absent 'rootShouldBindRole: true' "${render_default}" \
  "Milvus root must not be bound to potentially missing persisted role membership"
require_present 'serviceAccountName: milvus' "${render_default}" "Milvus pods must not use the default service account"
require_present 'serviceAccountName: "milvus-etcd"' "${render_default}" "etcd pods must not use the default service account"
require_absent 'type: LoadBalancer' "${render_default}" "Default render must not expose LoadBalancer services"
require_absent 'nodePort: 30416|nodePort: 30900' "${render_default}" "Default render must not expose stable NodePorts"
require_absent 'minioadmin' "${render_default}" "Default MinIO credentials must not appear anywhere in the render"
require_present '^[[:space:]]+accessKeyID:[[:space:]]*$' "${render_default}" "Milvus ConfigMap access key must remain empty"
require_present '^[[:space:]]+secretAccessKey:[[:space:]]*$' "${render_default}" "Milvus ConfigMap secret key must remain empty"
require_minio_replicas 4 "${render_default}" "Distributed MinIO must render at least four replicas"
require_fixed_count 'daedalus.nvidia.com/milvus-storage: "true"' 2 "${render_default}" \
  "Both etcd and MinIO PVC templates must carry the lifecycle label"
require_fixed_count "app.kubernetes.io/instance: \"${RELEASE_NAME}\"" 2 "${render_default}" \
  "Both PVC templates must carry the rendered release label"
require_labeled_pvc_storage minio "${MINIO_STORAGE_CLASS}" "${render_default}" \
  "The labeled MinIO PVC template must explicitly select ${MINIO_STORAGE_CLASS}"
require_labeled_pvc_storage etcd "${ETCD_STORAGE_CLASS}" "${render_default}" \
  "The labeled etcd PVC template must explicitly select ${ETCD_STORAGE_CLASS}"
require_fixed_count 'requiredDuringSchedulingIgnoredDuringExecution:' 1 "${render_default}" \
  "The three-member etcd cluster must use hard pod anti-affinity"
require_fixed_count 'whenUnsatisfiable: DoNotSchedule' 5 "${render_default}" \
  "Every replicated Milvus application component must use hard topology spread"
require_present 'name: milvus-mixcoord' "${render_default}" \
  "MixCoordinator must be rendered"
require_present 'enableActiveStandby: true' "${render_default}" \
  "MixCoordinator replication must use active-standby"
require_present 'createBucket milvus-bucket none false' "${render_default}" \
  "The MinIO hook must idempotently create the Milvus segment bucket"
require_present "createBucket ${MINIO_DOCUMENT_BUCKET} none false" "${render_default}" \
  "The MinIO hook must idempotently create the document bucket"
require_absent 'serviceAccountName: default|serviceAccountName: "default"' "${render_default}" "Rendered pods must not use default service account"
require_no_pdb_conflicts "${render_default}" "Rendered PodDisruptionBudgets must not set both minAvailable and maxUnavailable"

helm template "${RELEASE_NAME}" "${CHART}" \
  --version "${CHART_VERSION}" \
  --namespace "${NAMESPACE}" \
  -f custom-values.yaml \
  "${helm_overrides[@]}" \
  --set proxy.replicas=0 \
  --set queryNode.replicas=0 \
  --set dataNode.replicas=0 \
  --set streamingNode.replicas=0 \
  --set mixCoordinator.replicas=0 \
  > "${render_bootstrap}"

require_active_component_replicas 0 5 "${render_bootstrap}" \
  "The cold-start bootstrap render must scale all five Milvus component types to zero"
require_minio_replicas 4 "${render_bootstrap}" \
  "The cold-start bootstrap render must retain distributed MinIO"
require_present 'replicas: 3' "${render_bootstrap}" \
  "The cold-start bootstrap render must retain the three-member etcd cluster"

helm template "${RELEASE_NAME}" "${CHART}" \
  --version "${CHART_VERSION}" \
  --namespace "${NAMESPACE}" \
  -f custom-values.yaml \
  -f nodeport-values.yaml \
  "${helm_overrides[@]}" \
  > "${render_nodeport}"

require_present 'type: NodePort' "${render_nodeport}" "NodePort overlay must render NodePort services"
require_present 'nodePort: 30416' "${render_nodeport}" "NodePort overlay must pin Milvus to 30416"
require_present 'nodePort: 30900' "${render_nodeport}" "NodePort overlay must pin MinIO to 30900"

helm lint milvus-deploy

echo "Validation passed."
