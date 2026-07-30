#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHART_VERSION="26.3.0"
CHART_URL="https://helm.ngc.nvidia.com/nvidia/nemo-microservices/charts/nv-ingest-${CHART_VERSION}.tgz"

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

fail() {
  echo "NV-Ingest render validation failed: $*" >&2
  exit 1
}

require_command helm
require_command awk
require_command grep

chart="${NV_INGEST_VALIDATION_CHART:-}"
if [[ -z "${chart}" ]]; then
  helm_repository_cache="$(helm env HELM_REPOSITORY_CACHE)"
  helm_repository_cache="${helm_repository_cache%\"}"
  helm_repository_cache="${helm_repository_cache#\"}"
  cached_chart="${helm_repository_cache}/nv-ingest-${CHART_VERSION}.tgz"
  if [[ -f "${cached_chart}" ]]; then
    chart="${cached_chart}"
  else
    chart="${CHART_URL}"
  fi
fi

if [[ "${chart}" == http://* || "${chart}" == https://* ]]; then
  if [[ -z "${NGC_API_KEY:-}" ]]; then
    fail "set NGC_API_KEY or NV_INGEST_VALIDATION_CHART to an authenticated/cached ${CHART_VERSION} chart"
  fi
  validation_ngc_key="${NGC_API_KEY}"
else
  [[ -f "${chart}" ]] || fail "chart not found: ${chart}"
  validation_ngc_key="render-validation-ngc-key"
fi

render_file="$(mktemp)"
manifest_file="$(mktemp)"
cleanup() {
  rm -f -- "${render_file}" "${manifest_file}"
}
trap cleanup EXIT
chmod 600 "${render_file}" "${manifest_file}"

env \
  COMPONENTS_TO_READY_CHECK= \
  DRY_RUN=true \
  EMBEDDING_NIM_ENDPOINT=http://embedding.validation.svc:8000/v1 \
  IMAGE_STORAGE_URI=s3://render-validation/nv-ingest/images/ \
  MILVUS_ENDPOINT=http://milvus.milvus.svc.cluster.local:19530 \
  MINIO_ACCESS_KEY=render-validation-minio-access \
  MINIO_BUCKET=nv-ingest \
  MINIO_CREDENTIAL_SECRET_NAME=nv-ingest-render-validation-object-store \
  MINIO_INTERNAL_ADDRESS=http://milvus-minio.milvus.svc.cluster.local:9000 \
  MINIO_PUBLIC_ADDRESS=http://milvus-minio.milvus.svc.cluster.local:9000 \
  MINIO_SECRET_KEY=render-validation-minio-secret \
  MINIO_SOURCE_SECRET_NAME= \
  MINIO_SOURCE_SECRET_NAMESPACE= \
  NAMESPACE=nv-ingest-render-validation \
  NEMOTRON_PARSE_HTTP_ENDPOINT=http://parse.validation.svc:8000/v1/infer \
  NGC_API_KEY="${validation_ngc_key}" \
  NV_INGEST_CHART_URL="${chart}" \
  NV_INGEST_IMAGE_REPOSITORY=nvcr.io/nvidia/nemo-microservices/nv-ingest \
  NV_INGEST_IMAGE_TAG="${CHART_VERSION}" \
  NV_INGEST_REPLICA_COUNT=3 \
  NV_INGEST_NODE_HOSTNAMES= \
  NV_INGEST_NODE_PORT= \
  NV_INGEST_SERVICE_TYPE=ClusterIP \
  OCR_HTTP_ENDPOINT=http://ocr.validation.svc:8000/v1/infer \
  RELEASE_NAME=nv-ingest-render-validation \
  VLM_CAPTION_ENDPOINT=http://vlm.validation.svc:8000/v1/chat/completions \
  YOLOX_GRAPHIC_ELEMENTS_HTTP_ENDPOINT=http://graphics.validation.svc:8000/v1/infer \
  YOLOX_HTTP_ENDPOINT=http://page-elements.validation.svc:8000/v1/infer \
  YOLOX_TABLE_STRUCTURE_HTTP_ENDPOINT=http://tables.validation.svc:8000/v1/infer \
  "${SCRIPT_DIR}/deploy.sh" >"${render_file}"

awk 'found { print } /^MANIFEST:$/ { found = 1 }' "${render_file}" >"${manifest_file}"
[[ -s "${manifest_file}" ]] || fail "Helm produced no manifest"

main_service="$(awk '
  /^# Source: nv-ingest\/templates\/service.yaml$/ { selected = 1; next }
  selected && /^---$/ { exit }
  selected { print }
' "${manifest_file}")"
[[ -n "${main_service}" ]] || fail "main Service was not rendered"
grep -q '^  type: ClusterIP$' <<<"${main_service}" || fail "main Service is not ClusterIP"
if grep -q 'nodePort:' <<<"${main_service}"; then
  fail "main ClusterIP Service contains nodePort"
fi

main_deployment="$(awk '
  /^# Source: nv-ingest\/templates\/deployment.yaml$/ { selected = 1; next }
  selected && /^---$/ { exit }
  selected { print }
' "${manifest_file}")"
[[ -n "${main_deployment}" ]] || fail "main Deployment was not rendered"

if grep -q '^kind: Secret$' "${manifest_file}"; then
  fail "chart rendered a Helm-managed Secret"
fi
grep -q 'name: ngc-api' <<<"${main_deployment}" || fail "Deployment does not reference ngc-api"
grep -q 'name: ngc-secret' <<<"${main_deployment}" || fail "Deployment does not reference ngc-secret"
grep -q 'name: nv-ingest-render-validation-object-store' <<<"${main_deployment}" || \
  fail "Deployment does not import the external object-store Secret"
grep -q 'image: "nvcr.io/nvidia/nemo-microservices/nv-ingest:26.3.0"' <<<"${main_deployment}" || \
  fail "Deployment image is not pinned to 26.3.0"
grep -q 'value: "nv-ingest"' <<<"${main_deployment}" || fail "document bucket is not nv-ingest"
grep -q 'value: "nvidia/llama-nemotron-embed-vl-1b-v2"' <<<"${main_deployment}" || \
  fail "Deployment does not use the Nemotron VL embedder"
grep -q 'value: "http://embedding.validation.svc:8000/v1"' <<<"${main_deployment}" || \
  fail "Deployment does not contain the configured embedding endpoint"
grep -q 'podAntiAffinity:' <<<"${main_deployment}" || \
  fail "main Deployment does not require replicas on distinct nodes"
grep -q 'requiredDuringSchedulingIgnoredDuringExecution:' <<<"${main_deployment}" || \
  fail "main Deployment anti-affinity is not required"
grep -q 'topologyKey: kubernetes.io/hostname' <<<"${main_deployment}" || \
  fail "main Deployment anti-affinity does not use node topology"
grep -q 'app.kubernetes.io/instance: nv-ingest-render-validation' <<<"${main_deployment}" || \
  fail "main Deployment anti-affinity is not scoped to the Helm release"

grep -Eq 'daedalus.nvidia.com/ngc-secret-revision-checksum: [a-f0-9]{64}' <<<"${main_deployment}" || \
  fail "NGC Secret revision checksum annotation is missing"
grep -Eq 'daedalus.nvidia.com/object-store-secret-revision-checksum: [a-f0-9]{64}' <<<"${main_deployment}" || \
  fail "object-store Secret revision checksum annotation is missing"

if grep -q 'MINIO_ACCESS_KEY\|MINIO_SECRET_KEY' <<<"${main_deployment}"; then
  fail "Deployment contains literal MinIO credential environment entries"
fi
if grep -q 'render-validation-minio-access\|render-validation-minio-secret' "${render_file}"; then
  fail "render leaked MinIO credentials"
fi
if [[ "${validation_ngc_key}" == "render-validation-ngc-key" ]] && \
   grep -q 'render-validation-ngc-key' "${render_file}"; then
  fail "render leaked the NGC credential"
fi

grep -q 'helm.sh/chart: nv-ingest-26.3.0' "${manifest_file}" || \
  fail "render did not use chart 26.3.0"

pdb="$(
  sed \
    -e 's|__NAMESPACE__|nv-ingest-render-validation|g' \
    -e 's|__RELEASE_NAME__|nv-ingest-render-validation|g' \
    -e 's|__MIN_AVAILABLE__|2|g' \
    "${SCRIPT_DIR}/k8s/pdb.yaml"
)"
grep -q '^  minAvailable: 2$' <<<"${pdb}" || fail "PDB does not retain two API replicas"
grep -q 'app.kubernetes.io/version: "26.3.0"' <<<"${pdb}" || fail "PDB lacks version provenance"
if grep -q '__[A-Z_]*__' <<<"${pdb}"; then
  fail "PDB render contains an unresolved placeholder"
fi

echo "NV-Ingest 26.3.0 render validation passed."
