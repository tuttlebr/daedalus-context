#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FAKE_BIN="${SCRIPT_DIR}/tests/fakes"

fail() {
  echo "test-deploy: $*" >&2
  exit 1
}

test_root="$(mktemp -d)"
cleanup() {
  rm -rf -- "${test_root}"
}
trap cleanup EXIT

run_deploy() {
  env \
    PATH="${FAKE_BIN}:${PATH}" \
    FAKE_DEPLOY_LOG="${FAKE_DEPLOY_LOG}" \
    FAKE_DEPLOY_STATE="${FAKE_DEPLOY_STATE}" \
    FAKE_RELEASE_EXISTS=true \
    FAKE_MANIFEST_SECRET_FREE="${FAKE_MANIFEST_SECRET_FREE:-false}" \
    FAKE_NGC_UNMANAGED="${FAKE_NGC_UNMANAGED:-false}" \
    FAKE_HELM_OWNER_MALFORMED="${FAKE_HELM_OWNER_MALFORMED:-false}" \
    FAKE_NGC_SECRET_RESOURCE_VERSION="${FAKE_NGC_SECRET_RESOURCE_VERSION:-}" \
    FAKE_NGC_API_RESOURCE_VERSION="${FAKE_NGC_API_RESOURCE_VERSION:-}" \
    FAKE_FAIL_SECRET_FREE_FINAL="${FAKE_FAIL_SECRET_FREE_FINAL:-false}" \
    FAKE_ACTIVE_INGEST="${FAKE_ACTIVE_INGEST:-false}" \
    FAKE_SOURCE_INVALID="${FAKE_SOURCE_INVALID:-false}" \
    DRY_RUN=false \
    EMBEDDING_NIM_ENDPOINT=http://embedding.test:8000/v1 \
    IMAGE_STORAGE_URI="${IMAGE_STORAGE_URI:-s3://daedalus-documents/nv-ingest/images/}" \
    MILVUS_ENDPOINT=http://milvus.milvus.svc.cluster.local:19530 \
    MINIO_BUCKET=nv-ingest \
    MINIO_CREDENTIAL_SECRET_NAME="${TEST_MINIO_DESTINATION:-nv-ingest-object-store}" \
    MINIO_INTERNAL_ADDRESS=http://milvus-minio.milvus.svc.cluster.local:9000 \
    MINIO_PUBLIC_ADDRESS=http://milvus-minio.milvus.svc.cluster.local:9000 \
    MINIO_SOURCE_SECRET_NAME="${TEST_MINIO_SOURCE_NAME:-minio-source}" \
    MINIO_SOURCE_SECRET_NAMESPACE="${TEST_MINIO_SOURCE_NAMESPACE:-milvus}" \
    NAMESPACE=nv-ingest \
    URL_INGEST_NAMESPACE=milvus \
    NEMOTRON_PARSE_HTTP_ENDPOINT=http://parse.test:8000/v1/infer \
    NGC_API_KEY=test-ngc-key \
    NV_INGEST_CHART_URL=/test/nv-ingest-26.3.0.tgz \
    NV_INGEST_IMAGE_TAG=26.3.0 \
    NV_INGEST_NODE_HOSTNAMES= \
    NV_INGEST_NODE_PORT= \
    NV_INGEST_SERVICE_TYPE=ClusterIP \
    OCR_HTTP_ENDPOINT=http://ocr.test:8000/v1/infer \
    RELEASE_NAME=nv-ingest \
    VLM_CAPTION_ENDPOINT=http://vlm.test:8000/v1/chat/completions \
    YOLOX_GRAPHIC_ELEMENTS_HTTP_ENDPOINT=http://graphics.test:8000/v1/infer \
    YOLOX_HTTP_ENDPOINT=http://page-elements.test:8000/v1/infer \
    YOLOX_TABLE_STRUCTURE_HTTP_ENDPOINT=http://tables.test:8000/v1/infer \
    "${SCRIPT_DIR}/deploy.sh"
}

# A multi-replica release must never use pod-local artifact storage. This
# validation occurs before Kubernetes reads or writes.
FAKE_DEPLOY_LOG="${test_root}/local-artifacts.log"
FAKE_DEPLOY_STATE="${test_root}/local-artifacts-state"
mkdir -p "${FAKE_DEPLOY_STATE}"
: >"${FAKE_DEPLOY_LOG}"
set +e
IMAGE_STORAGE_URI=file:///workspace/data/artifacts/store/images run_deploy \
  >"${test_root}/local-artifacts.out" 2>&1
local_artifacts_status=$?
set -e
[[ ${local_artifacts_status} -ne 0 ]] || fail "multi-replica file:// storage unexpectedly succeeded"
grep -q 'Multiple API replicas must share' "${test_root}/local-artifacts.out" || \
  fail "multi-replica file:// refusal was not actionable"
if [[ -s "${FAKE_DEPLOY_LOG}" ]]; then
  fail "multi-replica file:// refusal performed external work"
fi

# A routine rollout must stop before mutation while a seed Job/operation lock
# is active, because ready replicas do not preserve in-flight Ray work.
FAKE_DEPLOY_LOG="${test_root}/active-ingest.log"
FAKE_DEPLOY_STATE="${test_root}/active-ingest-state"
mkdir -p "${FAKE_DEPLOY_STATE}"
: >"${FAKE_DEPLOY_LOG}"
FAKE_ACTIVE_INGEST=true
set +e
run_deploy >"${test_root}/active-ingest.out" 2>&1
active_ingest_status=$?
set -e
[[ ${active_ingest_status} -ne 0 ]] || fail "rollout during active ingestion unexpectedly succeeded"
grep -q 'Refusing to roll out NV-Ingest while URL ingestion is active' \
  "${test_root}/active-ingest.out" || fail "active-ingestion refusal was not actionable"
if grep -Eq '^(MUTATE|HELM_UPGRADE)' "${FAKE_DEPLOY_LOG}"; then
  fail "active-ingestion refusal occurred after a mutation"
fi
FAKE_ACTIVE_INGEST=false

# Invalid source Secret content must stop before any namespace, Secret, or Helm
# upgrade mutation.
FAKE_DEPLOY_LOG="${test_root}/invalid-source.log"
FAKE_DEPLOY_STATE="${test_root}/invalid-source-state"
mkdir -p "${FAKE_DEPLOY_STATE}"
: >"${FAKE_DEPLOY_LOG}"
FAKE_SOURCE_INVALID=true
set +e
run_deploy >"${test_root}/invalid-source.out" 2>&1
invalid_status=$?
set -e
[[ ${invalid_status} -ne 0 ]] || fail "invalid source Secret unexpectedly succeeded"
grep -q 'must contain accesskey and secretkey' "${test_root}/invalid-source.out" || \
  fail "invalid source Secret did not produce a useful error"
if grep -Eq '^(MUTATE|HELM_UPGRADE|EXTERNALIZED)' "${FAKE_DEPLOY_LOG}"; then
  fail "invalid source Secret caused a mutation"
fi

# A release whose current manifest owns the two NGC Secrets must receive a
# keep-policy transition before the final externally managed release.
FAKE_DEPLOY_LOG="${test_root}/migration.log"
FAKE_DEPLOY_STATE="${test_root}/migration-state"
mkdir -p "${FAKE_DEPLOY_STATE}"
: >"${FAKE_DEPLOY_LOG}"
FAKE_SOURCE_INVALID=false
if ! run_deploy >"${test_root}/migration.out" 2>&1; then
  cat "${test_root}/migration.out" >&2
  fail "migration deploy failed"
fi

mapfile -t upgrades < <(grep '^HELM_UPGRADE' "${FAKE_DEPLOY_LOG}")
[[ $(grep -c '^HELM_RENDER' "${FAKE_DEPLOY_LOG}") -eq 1 ]] || fail "expected one read-only render preflight"
[[ ${#upgrades[@]} -eq 3 ]] || fail "expected transition, finalization, and checksum reconciliation upgrades"
[[ "${upgrades[0]}" == *'pull=true api=true post=true nodePort=false password=true bucket=nv-ingest atomic=true'* ]] || \
  fail "transition revision did not render kept NGC Secrets with expected values"
[[ "${upgrades[1]}" == *'pull=false api=false post=true nodePort=false password=false bucket=nv-ingest atomic=false'* ]] || \
  fail "final revision was not secret-free or included a ClusterIP nodePort"
[[ "${upgrades[2]}" == *'pull=false api=false post=true nodePort=false password=false bucket=nv-ingest atomic=true'* ]] || \
  fail "post-migration checksum reconciliation was not a Secret-free atomic revision"

transition_ngc_checksum="${upgrades[0]#*ngc_checksum=}"
transition_ngc_checksum="${transition_ngc_checksum%% *}"
final_ngc_checksum="${upgrades[1]#*ngc_checksum=}"
final_ngc_checksum="${final_ngc_checksum%% *}"
[[ "${transition_ngc_checksum}" != "unset" && "${transition_ngc_checksum}" == "${final_ngc_checksum}" ]] || \
  fail "the ownership-only transition unexpectedly changed the credential rollout checksum"
reconciled_ngc_checksum="${upgrades[2]#*ngc_checksum=}"
reconciled_ngc_checksum="${reconciled_ngc_checksum%% *}"
[[ "${reconciled_ngc_checksum}" != "${final_ngc_checksum}" ]] || \
  fail "post-migration revision did not reconcile the changed Secret resourceVersion"

first_upgrade_line="$(grep -n '^HELM_UPGRADE' "${FAKE_DEPLOY_LOG}" | head -n 1 | cut -d: -f1)"
externalized_line="$(grep -n '^EXTERNALIZED ngc-secret$' "${FAKE_DEPLOY_LOG}" | cut -d: -f1)"
final_upgrade_line="$(grep -n '^HELM_UPGRADE' "${FAKE_DEPLOY_LOG}" | sed -n '2p' | cut -d: -f1)"
reconcile_upgrade_line="$(grep -n '^HELM_UPGRADE' "${FAKE_DEPLOY_LOG}" | tail -n 1 | cut -d: -f1)"
[[ "${first_upgrade_line}" -lt "${final_upgrade_line}" && "${final_upgrade_line}" -lt "${externalized_line}" && \
   "${externalized_line}" -lt "${reconcile_upgrade_line}" ]] || \
  fail "NGC ownership was removed before the forward-only Secret-free revision succeeded"
grep -q '^EXTERNALIZED ngc-api$' "${FAKE_DEPLOY_LOG}" || fail "ngc-api was not externalized"

# Existing, unmarked Secrets must not be silently adopted and overwritten.
FAKE_DEPLOY_LOG="${test_root}/unmanaged.log"
FAKE_DEPLOY_STATE="${test_root}/unmanaged-state"
mkdir -p "${FAKE_DEPLOY_STATE}"
: >"${FAKE_DEPLOY_LOG}"
FAKE_NGC_UNMANAGED=true
set +e
run_deploy >"${test_root}/unmanaged.out" 2>&1
unmanaged_status=$?
set -e
[[ ${unmanaged_status} -ne 0 ]] || fail "unmanaged Secret was silently adopted"
grep -q 'Refusing to overwrite existing unmanaged Secret' "${test_root}/unmanaged.out" || \
  fail "unmanaged Secret refusal was not actionable"
if grep -Eq '^(MUTATE|HELM_UPGRADE|EXTERNALIZED)' "${FAKE_DEPLOY_LOG}"; then
  fail "unmanaged Secret refusal occurred after a mutation"
fi
FAKE_NGC_UNMANAGED=false

# A Helm label without complete ownership annotations is not enough to adopt
# an existing Secret unless our explicit interrupted-migration markers exist.
FAKE_DEPLOY_LOG="${test_root}/malformed-owner.log"
FAKE_DEPLOY_STATE="${test_root}/malformed-owner-state"
mkdir -p "${FAKE_DEPLOY_STATE}"
: >"${FAKE_DEPLOY_LOG}"
FAKE_HELM_OWNER_MALFORMED=true
set +e
run_deploy >"${test_root}/malformed-owner.out" 2>&1
malformed_owner_status=$?
set -e
[[ ${malformed_owner_status} -ne 0 ]] || fail "malformed Helm ownership was silently adopted"
grep -q 'managed by Helm' "${test_root}/malformed-owner.out" || \
  fail "malformed Helm ownership refusal was not actionable"
if grep -Eq '^(MUTATE|HELM_UPGRADE|EXTERNALIZED)' "${FAKE_DEPLOY_LOG}"; then
  fail "malformed Helm ownership refusal occurred after a mutation"
fi
FAKE_HELM_OWNER_MALFORMED=false

# The authoritative source Secret must never also be the destination Secret.
FAKE_DEPLOY_LOG="${test_root}/same-secret.log"
FAKE_DEPLOY_STATE="${test_root}/same-secret-state"
mkdir -p "${FAKE_DEPLOY_STATE}"
: >"${FAKE_DEPLOY_LOG}"
TEST_MINIO_DESTINATION=minio-source
TEST_MINIO_SOURCE_NAME=minio-source
TEST_MINIO_SOURCE_NAMESPACE=nv-ingest
set +e
run_deploy >"${test_root}/same-secret.out" 2>&1
same_secret_status=$?
set -e
[[ ${same_secret_status} -ne 0 ]] || fail "identical MinIO source and destination Secrets were accepted"
grep -q 'source Secret must not be any destination Secret' "${test_root}/same-secret.out" || \
  fail "same-Secret refusal was not actionable"
[[ ! -s "${FAKE_DEPLOY_LOG}" ]] || fail "same-Secret refusal occurred after cluster access or mutation"
unset TEST_MINIO_DESTINATION TEST_MINIO_SOURCE_NAME TEST_MINIO_SOURCE_NAMESPACE

# If the forward-only finalization fails, the latest manifest is Secret-free
# but the last deployed revision still owns the kept Secrets. The retry must
# remain forward-only until a Secret-free revision has successfully deployed.
FAKE_DEPLOY_LOG="${test_root}/failed-final.log"
FAKE_DEPLOY_STATE="${test_root}/failed-final-state"
mkdir -p "${FAKE_DEPLOY_STATE}"
: >"${FAKE_DEPLOY_LOG}"
FAKE_FAIL_SECRET_FREE_FINAL=true
FAKE_MANIFEST_SECRET_FREE=false
set +e
run_deploy >"${test_root}/failed-final.out" 2>&1
failed_final_status=$?
set -e
[[ ${failed_final_status} -ne 0 ]] || fail "simulated migration finalization unexpectedly succeeded"
[[ $(grep -c '^HELM_UPGRADE' "${FAKE_DEPLOY_LOG}") -eq 2 ]] || \
  fail "failed finalization did not run transition then final revision"
if grep -q '^EXTERNALIZED ngc-' "${FAKE_DEPLOY_LOG}"; then
  fail "NGC Secrets were externalized before finalization succeeded"
fi

FAKE_DEPLOY_LOG="${test_root}/failed-final-retry.log"
: >"${FAKE_DEPLOY_LOG}"
FAKE_FAIL_SECRET_FREE_FINAL=false
FAKE_MANIFEST_SECRET_FREE=true
run_deploy >"${test_root}/failed-final-retry.out" 2>&1 || fail "failed finalization retry did not recover"
mapfile -t retry_upgrades < <(grep '^HELM_UPGRADE' "${FAKE_DEPLOY_LOG}")
[[ ${#retry_upgrades[@]} -eq 2 ]] || fail "retry did not finalize then reconcile the checksum"
retry_upgrade="${retry_upgrades[0]}"
[[ "${retry_upgrade}" == *'pull=false'* && "${retry_upgrade}" == *'api=false'* && \
   "${retry_upgrade}" == *'atomic=false'* ]] || \
  fail "failed finalization retry crossed the ownership boundary atomically"
[[ "${retry_upgrades[1]}" == *'atomic=true'* ]] || fail "retry checksum reconciliation was not atomic"
grep -q '^EXTERNALIZED ngc-secret$' "${FAKE_DEPLOY_LOG}" || \
  fail "successful retry did not externalize ngc-secret"
FAKE_MANIFEST_SECRET_FREE=false

# Routine Secret-free upgrades stay atomic, and changing a Secret
# resourceVersion must alter the checksum carried by the Deployment template.
FAKE_DEPLOY_STATE="${test_root}/routine-state"
mkdir -p "${FAKE_DEPLOY_STATE}"
: >"${FAKE_DEPLOY_STATE}/object-created"
: >"${FAKE_DEPLOY_STATE}/nv-ingest-object-store-external"
: >"${FAKE_DEPLOY_STATE}/ngc-secret-external"
: >"${FAKE_DEPLOY_STATE}/ngc-api-external"
FAKE_MANIFEST_SECRET_FREE=true
FAKE_DEPLOY_LOG="${test_root}/routine-1.log"
: >"${FAKE_DEPLOY_LOG}"
FAKE_NGC_SECRET_RESOURCE_VERSION=401
FAKE_NGC_API_RESOURCE_VERSION=402
run_deploy >"${test_root}/routine-1.out" 2>&1 || fail "first routine deploy failed"
routine_upgrade_1="$(grep '^HELM_UPGRADE' "${FAKE_DEPLOY_LOG}")"
[[ "${routine_upgrade_1}" == *'atomic=true'* ]] || fail "routine upgrade was not atomic"
grep -q '^MUTATE rollout-strategy nv-ingest$' "${FAKE_DEPLOY_LOG}" || \
  fail "routine upgrade did not make required anti-affinity rollout-safe"
routine_checksum_1="${routine_upgrade_1#*ngc_checksum=}"
routine_checksum_1="${routine_checksum_1%% *}"

FAKE_DEPLOY_LOG="${test_root}/routine-2.log"
: >"${FAKE_DEPLOY_LOG}"
FAKE_NGC_SECRET_RESOURCE_VERSION=501
run_deploy >"${test_root}/routine-2.out" 2>&1 || fail "second routine deploy failed"
routine_upgrade_2="$(grep '^HELM_UPGRADE' "${FAKE_DEPLOY_LOG}")"
routine_checksum_2="${routine_upgrade_2#*ngc_checksum=}"
routine_checksum_2="${routine_checksum_2%% *}"
[[ "${routine_checksum_1}" != "${routine_checksum_2}" ]] || \
  fail "Secret resourceVersion change did not alter the rollout checksum"
FAKE_MANIFEST_SECRET_FREE=false
unset FAKE_NGC_SECRET_RESOURCE_VERSION FAKE_NGC_API_RESOURCE_VERSION

# The post-renderer must add the no-surge strategy to the main Deployment and
# the keep policy only to the two fixed NGC Secret documents used by chart
# 26.3.0.
post_rendered="$(printf '%s\n' \
  '---' \
  'apiVersion: v1' \
  'kind: Secret' \
  'metadata:' \
  '  name: ngc-secret  # fixed chart name' \
  '---' \
  'apiVersion: v1' \
  'kind: Secret' \
  'metadata:' \
  '  name: unrelated-secret' \
  '---' \
  'apiVersion: v1' \
  'kind: Secret' \
  'metadata:' \
  '  name: ngc-api  # fixed chart name' \
  '---' \
  'apiVersion: apps/v1' \
  'kind: Deployment' \
  'metadata:' \
  '  name: nv-ingest' \
  '  labels:' \
  '    helm.sh/chart: nv-ingest-26.3.0' \
  '    app.kubernetes.io/name: nv-ingest' \
  'spec:' \
  '  replicas: 2' | "${SCRIPT_DIR}/helm-post-renderer.sh")"
keep_count="$(grep -c 'helm.sh/resource-policy: keep' <<<"${post_rendered}")"
[[ "${keep_count}" -eq 2 ]] || fail "post-renderer did not annotate exactly two NGC Secrets"
grep -A5 '^  strategy:$' <<<"${post_rendered}" | grep -q '^      maxSurge: 0$' || \
  fail "post-renderer did not add the no-surge rollout strategy"

echo "NV-Ingest deploy behavior tests passed."
