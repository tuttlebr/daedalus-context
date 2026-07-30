#!/usr/bin/env bash
set -euo pipefail

# Deploy url-ingest CronJobs to Kubernetes
# Usage: ./deploy.sh [--stage-data] [--seed-data] [--run-now [COLLECTION...]]
#
# Examples:
#   ./deploy.sh                          # Deploy only
#   ./deploy.sh --stage-data             # Deploy and copy repository data to PVCs
#   ./deploy.sh --seed-data              # Stage, ingest, wait, and verify all collections
#   ./deploy.sh --run-now semianalysis   # Ingest staged data, wait, and verify one collection

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
WORKSPACE_DIR="$(cd "${PROJECT_DIR}/.." && pwd)"
# shellcheck source=lib/job-control.sh
source "$SCRIPT_DIR/lib/job-control.sh"
# shellcheck source=../../scripts/image-reference.sh
source "$WORKSPACE_DIR/scripts/image-reference.sh"
ORIGINAL_ENV_FILE="$(mktemp)"
trap 'rm -f "${ORIGINAL_ENV_FILE}"' EXIT
export -p >"${ORIGINAL_ENV_FILE}"
NAMESPACE="${NAMESPACE:-milvus}"
IMAGE_PULL_SECRET="${IMAGE_PULL_SECRET:-nvcr-imagepullsecret}"
IMAGE_PULL_SECRET_SOURCE_NAMESPACE="${IMAGE_PULL_SECRET_SOURCE_NAMESPACE:-default}"
URL_INGEST_SOURCE_HOST_PATH="${URL_INGEST_SOURCE_HOST_PATH:-$PROJECT_DIR}"
ROLLOUT_TIMEOUT="${ROLLOUT_TIMEOUT:-10m}"

ALL_COLLECTIONS="nvidia kubernetes vetpartner semianalysis mentalhealth"
STAGE_DATA=false
RUN_NOW=false
RUN_CRAWL=false
RENDER_ONLY=false
RUN_COLLECTIONS=""

load_env_file() {
    local file="$1"
    if [[ -f "$file" ]]; then
        echo "Loading ${file#"${WORKSPACE_DIR}/"}" >&2
        set -a
        # shellcheck source=/dev/null
        source "$file"
        set +a
    fi
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        --stage-data)
            STAGE_DATA=true
            shift
            ;;
        --seed-data)
            STAGE_DATA=true
            RUN_NOW=true
            shift
            ;;
        --run-crawl)
            RUN_CRAWL=true
            shift
            ;;
        --render-only)
            RENDER_ONLY=true
            shift
            ;;
        --run-now)
            RUN_NOW=true
            shift
            # Collect collection names until next flag or end of args
            while [[ $# -gt 0 && ! "$1" =~ ^-- ]]; do
                RUN_COLLECTIONS="$RUN_COLLECTIONS $1"
                shift
            done
            # Default to all collections if none specified
            if [ -z "$RUN_COLLECTIONS" ]; then
                RUN_COLLECTIONS="$ALL_COLLECTIONS"
            fi
            ;;
        *)
            echo "Unknown option: $1"
            echo "Usage: $0 [--stage-data] [--seed-data] [--run-now [COLLECTION...]] [--run-crawl] [--render-only]"
            echo "Collections: $ALL_COLLECTIONS"
            exit 1
            ;;
    esac
done

if [[ "$RUN_NOW" == true && -z "$RUN_COLLECTIONS" ]]; then
    RUN_COLLECTIONS="$ALL_COLLECTIONS"
fi

command -v python3 >/dev/null || {
    echo "ERROR: python3 is required to render normalized Kubernetes manifests." >&2
    exit 1
}

load_env_file "$WORKSPACE_DIR/.env"
load_env_file "$PROJECT_DIR/.env"
# Explicit caller values (including root Makefile controls) take precedence.
# shellcheck source=/dev/null
source "${ORIGINAL_ENV_FILE}"
rm -f "${ORIGINAL_ENV_FILE}"
trap - EXIT

NAMESPACE="${NAMESPACE:-milvus}"
MILVUS_NAMESPACE="${MILVUS_NAMESPACE:-milvus}"
NV_INGEST_NAMESPACE="${NV_INGEST_NAMESPACE:-nv-ingest}"
MILVUS_SERVICE="${MILVUS_SERVICE:-milvus}"
MINIO_SERVICE="${MINIO_SERVICE:-milvus-minio}"
NV_INGEST_SERVICE="${NV_INGEST_SERVICE:-nv-ingest}"
MILVUS_URI="${MILVUS_URI:-http://${MILVUS_SERVICE}.${MILVUS_NAMESPACE}.svc.cluster.local:19530}"
MINIO_ENDPOINT="${MINIO_ENDPOINT:-${MINIO_SERVICE}.${MILVUS_NAMESPACE}.svc.cluster.local:9000}"
MINIO_BUCKET="${MINIO_BUCKET:-nv-ingest}"
NV_INGEST_MESSAGE_HOST="${NV_INGEST_MESSAGE_HOST:-${NV_INGEST_SERVICE}.${NV_INGEST_NAMESPACE}.svc.cluster.local}"
NV_INGEST_MESSAGE_PORT="${NV_INGEST_MESSAGE_PORT:-7670}"
NV_INGEST_VERSION="${NV_INGEST_VERSION:-26.3.0}"
URL_INGEST_IMAGE="${URL_INGEST_IMAGE:-}"
URL_INGEST_RUNNER_IMAGE="${URL_INGEST_RUNNER_IMAGE:-}"
MINIO_SOURCE_SECRET_NAMESPACE="${MINIO_SOURCE_SECRET_NAMESPACE:-$MILVUS_NAMESPACE}"
MINIO_SOURCE_SECRET_NAME="${MINIO_SOURCE_SECRET_NAME:-milvus-minio-credentials}"
MINIO_SOURCE_ACCESS_KEY="${MINIO_SOURCE_ACCESS_KEY:-accesskey}"
MINIO_SOURCE_SECRET_KEY="${MINIO_SOURCE_SECRET_KEY:-secretkey}"
MILVUS_ROOT_SECRET_NAME="${MILVUS_ROOT_SECRET_NAME:-milvus-root-credentials}"
URL_INGEST_SOURCE_NODE="${URL_INGEST_SOURCE_NODE:-daedalus-01}"
PRUNE_STAGED_DATA="${PRUNE_STAGED_DATA:-true}"
READINESS_TIMEOUT_SECONDS="${READINESS_TIMEOUT_SECONDS:-600}"
STAGE_URL_TIMEOUT_SECONDS="${STAGE_URL_TIMEOUT_SECONDS:-3600}"
INGEST_JOB_DEADLINE_SECONDS="${INGEST_JOB_DEADLINE_SECONDS:-21600}"
INGEST_WAIT_TIMEOUT_SECONDS="${INGEST_WAIT_TIMEOUT_SECONDS:-22200}"
INGEST_POLL_SECONDS="${INGEST_POLL_SECONDS:-10}"
NVIDIA_INGEST_SHARDS="${NVIDIA_INGEST_SHARDS:-8}"
MAX_PARALLEL_INGEST_SHARDS="${MAX_PARALLEL_INGEST_SHARDS:-1}"
CLIENT_REQUEST_TIMEOUT_SECONDS="${CLIENT_REQUEST_TIMEOUT_SECONDS:-60}"
CLIENT_MAX_JOB_RETRIES="${CLIENT_MAX_JOB_RETRIES:-120}"
CLIENT_RETRY_DELAY_SECONDS="${CLIENT_RETRY_DELAY_SECONDS:-5}"
COLLECTION_VERIFY_TIMEOUT_SECONDS="${COLLECTION_VERIFY_TIMEOUT_SECONDS:-300}"
MANUAL_JOB_BACKOFF_LIMIT="${MANUAL_JOB_BACKOFF_LIMIT:-1}"
RUN_ID="$(date -u +%Y%m%d%H%M%S)-$$"

if [[ "$RENDER_ONLY" == true ]]; then
    URL_INGEST_IMAGE="${URL_INGEST_IMAGE:-example.invalid/url-ingest:0.0.0}"
    URL_INGEST_RUNNER_IMAGE="${URL_INGEST_RUNNER_IMAGE:-example.invalid/url-ingest-runner:0.0.0}"
fi
require_semver_image_ref "URL_INGEST_IMAGE" "$URL_INGEST_IMAGE"
require_semver_image_ref "URL_INGEST_RUNNER_IMAGE" "$URL_INGEST_RUNNER_IMAGE"

for numeric_var in READINESS_TIMEOUT_SECONDS STAGE_URL_TIMEOUT_SECONDS \
    INGEST_JOB_DEADLINE_SECONDS INGEST_WAIT_TIMEOUT_SECONDS INGEST_POLL_SECONDS \
    NVIDIA_INGEST_SHARDS MAX_PARALLEL_INGEST_SHARDS CLIENT_REQUEST_TIMEOUT_SECONDS \
    CLIENT_MAX_JOB_RETRIES COLLECTION_VERIFY_TIMEOUT_SECONDS MANUAL_JOB_BACKOFF_LIMIT; do
    if ! [[ "${!numeric_var}" =~ ^[0-9]+$ ]]; then
        echo "ERROR: $numeric_var must be a non-negative integer." >&2
        exit 1
    fi
done
if (( NVIDIA_INGEST_SHARDS < 1 || MAX_PARALLEL_INGEST_SHARDS < 1 || INGEST_POLL_SECONDS < 1 )); then
    echo "ERROR: shard counts and INGEST_POLL_SECONDS must be at least 1." >&2
    exit 1
fi
if ! [[ "$CLIENT_RETRY_DELAY_SECONDS" =~ ^[0-9]+([.][0-9]+)?$ ]]; then
    echo "ERROR: CLIENT_RETRY_DELAY_SECONDS must be a positive number." >&2
    exit 1
fi
if [[ "$RUN_CRAWL" == true && "$NVIDIA_INGEST_SHARDS" != 1 ]]; then
    echo "ERROR: --run-crawl requires NVIDIA_INGEST_SHARDS=1 to prevent concurrent crawlers." >&2
    exit 1
fi
if [[ "$RUN_NOW" == true ]]; then
    declare -A requested_collections=()
    normalized_collections=""
    for collection in $RUN_COLLECTIONS; do
        if [[ ! " $ALL_COLLECTIONS " =~ " $collection " ]]; then
            echo "ERROR: unknown collection: $collection" >&2
            echo "Collections: $ALL_COLLECTIONS" >&2
            exit 1
        fi
        if [[ -z "${requested_collections[$collection]:-}" ]]; then
            requested_collections[$collection]=true
            normalized_collections+=" $collection"
        fi
    done
    RUN_COLLECTIONS="$normalized_collections"
fi

# Staging rewrites the shared input PVC for every collection, so it must own
# every collection lock even when the caller will ingest only a subset. A
# run-only operation needs locks only for its requested collections.
OPERATION_COLLECTIONS="$RUN_COLLECTIONS"
if [[ "$STAGE_DATA" == true ]]; then
    OPERATION_COLLECTIONS="$ALL_COLLECTIONS"
fi

echo "=== Deploying url-ingest to namespace: $NAMESPACE ==="

escape_sed_replacement() {
    printf '%s' "$1" | sed -e 's/[&|\\]/\\&/g'
}

render_manifest() {
    local namespace milvus_uri minio_endpoint minio_bucket nv_ingest_host crawler_image client_image source_node prune
    local collection="" suspend_for_operation=false
    namespace="$(escape_sed_replacement "$NAMESPACE")"
    milvus_uri="$(escape_sed_replacement "$MILVUS_URI")"
    minio_endpoint="$(escape_sed_replacement "$MINIO_ENDPOINT")"
    minio_bucket="$(escape_sed_replacement "$MINIO_BUCKET")"
    nv_ingest_host="$(escape_sed_replacement "$NV_INGEST_MESSAGE_HOST")"
    crawler_image="$(escape_sed_replacement "$URL_INGEST_IMAGE")"
    client_image="$(escape_sed_replacement "$URL_INGEST_RUNNER_IMAGE")"
    source_node="$(escape_sed_replacement "$URL_INGEST_SOURCE_NODE")"
    prune="$(escape_sed_replacement "$PRUNE_STAGED_DATA")"
    if [[ "$(basename "$1")" =~ ^cronjob-([a-z0-9_-]+)\.yaml$ ]]; then
        collection="${BASH_REMATCH[1]}"
        if [[ "$RUN_NOW" == true && " $RUN_COLLECTIONS " =~ " $collection " ]]; then
            suspend_for_operation=true
        elif [[ "$RENDER_ONLY" != true ]] && kubectl --namespace="$NAMESPACE" get \
            "configmap/url-ingest-${collection}-run-lock" >/dev/null 2>&1; then
            # A separate reconcile must not resume a schedule owned by an
            # active manual population run.
            suspend_for_operation=true
        fi
    fi
    sed \
        -e "s|^\([[:space:]]*\)namespace: milvus$|\1namespace: ${namespace}|" \
        -e "s|http://milvus\.milvus\.svc\.cluster\.local:19530|${milvus_uri}|g" \
        -e "s|milvus-minio\.milvus\.svc\.cluster\.local:9000|${minio_endpoint}|g" \
        -e "s|nv-ingest\.nv-ingest\.svc\.cluster\.local|${nv_ingest_host}|g" \
        -e "s|value: \"\(milvus-bucket\|nv-ingest\)\"|value: \"${minio_bucket}\"|g" \
        -e "s|__URL_INGEST_IMAGE__|${crawler_image}|g" \
        -e "s|__URL_INGEST_RUNNER_IMAGE__|${client_image}|g" \
        -e "s|__SOURCE_NODE__|${source_node}|g" \
        -e "s|__PRUNE_STAGED_DATA__|${prune}|g" \
        "$1" | {
            if [[ "$suspend_for_operation" == true ]]; then
                sed 's/^  suspend: false$/  suspend: true/'
            else
                cat
            fi
        } | python3 "$SCRIPT_DIR/normalize-manifest.py"
}

apply_manifest() {
    render_manifest "$1" | kubectl --namespace="${NAMESPACE}" apply -f -
}

if [[ "$RENDER_ONLY" == true ]]; then
    first=true
    for manifest in serviceaccount.yaml pvc.yaml configmap-url-lists.yaml \
        cronjob-nvidia.yaml cronjob-kubernetes.yaml cronjob-vetpartner.yaml \
        cronjob-semianalysis.yaml cronjob-mentalhealth.yaml seed-data-job.yaml; do
        if [[ "$first" != true ]]; then
            echo '---'
        fi
        first=false
        render_manifest "$SCRIPT_DIR/$manifest"
    done
    exit 0
fi

require_env() {
    local name="$1"
    if [ -z "${!name:-}" ]; then
        echo "ERROR: required environment variable $name is not set."
        exit 1
    fi
}

secret_value() {
    local namespace="$1" secret="$2" key="$3" encoded
    if ! encoded="$(kubectl get secret "$secret" --namespace="$namespace" \
        --output="jsonpath={.data.${key}}" 2>/dev/null)" || [[ -z "$encoded" ]]; then
        echo "ERROR: secret $namespace/$secret must contain non-empty key $key." >&2
        return 1
    fi
    printf '%s' "$encoded" | base64 --decode
}

# The workload Secret is a synchronized consumer copy. The Milvus deployment
# remains authoritative for database and object-store credentials.
kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -

if [[ -z "${HF_ACCESS_TOKEN:-}" ]] && kubectl get secret url-ingest-secret \
    --namespace="$NAMESPACE" >/dev/null 2>&1; then
    HF_ACCESS_TOKEN="$(secret_value "$NAMESPACE" url-ingest-secret HF_ACCESS_TOKEN)"
fi
MINIO_ACCESS_KEY="$(secret_value \
    "$MINIO_SOURCE_SECRET_NAMESPACE" "$MINIO_SOURCE_SECRET_NAME" "$MINIO_SOURCE_ACCESS_KEY")"
MINIO_SECRET_KEY="$(secret_value \
    "$MINIO_SOURCE_SECRET_NAMESPACE" "$MINIO_SOURCE_SECRET_NAME" "$MINIO_SOURCE_SECRET_KEY")"
require_env HF_ACCESS_TOKEN
require_env MINIO_ACCESS_KEY
require_env MINIO_SECRET_KEY

MILVUS_USERNAME="${MILVUS_USERNAME:-root}"
MILVUS_ROOT_PASSWORD="$(secret_value \
    "$MILVUS_NAMESPACE" "$MILVUS_ROOT_SECRET_NAME" password)"
require_env MILVUS_ROOT_PASSWORD

# 1. Copy NGC image pull credentials into the workload namespace
echo "Creating image pull secret..."
if ! kubectl get secret "$IMAGE_PULL_SECRET" \
    --namespace="$IMAGE_PULL_SECRET_SOURCE_NAMESPACE" >/dev/null 2>&1; then
    echo "ERROR: secret $IMAGE_PULL_SECRET not found in namespace $IMAGE_PULL_SECRET_SOURCE_NAMESPACE."
    exit 1
fi

source_secret_type="$(kubectl get secret "$IMAGE_PULL_SECRET" \
    --namespace="$IMAGE_PULL_SECRET_SOURCE_NAMESPACE" \
    --output=jsonpath='{.type}')"

case "$source_secret_type" in
    kubernetes.io/dockerconfigjson)
        kubectl create secret generic "$IMAGE_PULL_SECRET" \
            --namespace="$NAMESPACE" \
            --from-file=.dockerconfigjson=<(kubectl get secret "$IMAGE_PULL_SECRET" \
                --namespace="$IMAGE_PULL_SECRET_SOURCE_NAMESPACE" \
                --output=jsonpath='{.data.\.dockerconfigjson}' | base64 --decode) \
            --type=kubernetes.io/dockerconfigjson \
            --dry-run=client -o yaml | kubectl apply -f -
        ;;
    kubernetes.io/dockercfg)
        kubectl create secret generic "$IMAGE_PULL_SECRET" \
            --namespace="$NAMESPACE" \
            --from-file=.dockercfg=<(kubectl get secret "$IMAGE_PULL_SECRET" \
                --namespace="$IMAGE_PULL_SECRET_SOURCE_NAMESPACE" \
                --output=jsonpath='{.data.\.dockercfg}' | base64 --decode) \
            --type=kubernetes.io/dockercfg \
            --dry-run=client -o yaml | kubectl apply -f -
        ;;
    *)
        echo "ERROR: secret $IMAGE_PULL_SECRET has unsupported type: $source_secret_type"
        exit 1
        ;;
esac

echo "Configuring workload service accounts..."
apply_manifest "$SCRIPT_DIR/serviceaccount.yaml"

# 2. Create URL-ingest-owned PVCs. The pdf-server release independently owns
# its ServiceAccount, PVC, Deployment, Service, PDB, and NetworkPolicy.
echo "Creating PVCs..."
apply_manifest "$SCRIPT_DIR/pvc.yaml"

# 3. Create secret (from .env values)
echo "Creating secret..."
kubectl create secret generic url-ingest-secret \
    --namespace="$NAMESPACE" \
    --from-literal="HF_ACCESS_TOKEN=${HF_ACCESS_TOKEN}" \
    --from-literal="MINIO_ACCESS_KEY=${MINIO_ACCESS_KEY}" \
    --from-literal="MINIO_SECRET_KEY=${MINIO_SECRET_KEY}" \
    --from-literal="MILVUS_USERNAME=${MILVUS_USERNAME}" \
    --from-literal="MILVUS_PASSWORD=${MILVUS_ROOT_PASSWORD}" \
    --dry-run=client -o yaml | kubectl apply -f -

# 4. Create ConfigMaps for URL lists
echo "Creating URL list ConfigMaps..."
# Small lists (inline YAML)
apply_manifest "$SCRIPT_DIR/configmap-url-lists.yaml"

# Large lists (from file, using server-side apply to avoid annotation size limit)
kubectl create configmap url-ingest-k8s-urls \
    --namespace="$NAMESPACE" \
    --from-file=k8s_url_links.txt="$PROJECT_DIR/files/k8s_url_links.txt" \
    --dry-run=client -o yaml | kubectl apply --server-side -f -

kubectl create configmap url-ingest-vetpartner-urls \
    --namespace="$NAMESPACE" \
    --from-file=vetpartner_url_links.txt="$PROJECT_DIR/files/vetpartner_url_links.txt" \
    --dry-run=client -o yaml | kubectl apply --server-side -f -

# Install cleanup before applying temporarily suspended CronJobs so a partial
# reconcile cannot leave the schedules disabled.
declare -a OPERATION_LOCKS=()
declare -a RESTORE_CRONJOBS=()
declare -a RUN_JOBS=()
declare -A RESTORE_CRONJOB_STATES=()
SHOW_COMPLETION_STATUS=false

cleanup_operations() {
    local original_status=$? cronjob lock job conditions pods desired_state safe_to_release=true
    trap - EXIT
    set +e
    if (( original_status != 0 )); then
        for job in "${RUN_JOBS[@]}"; do
            if ! kubectl --namespace="$NAMESPACE" get "job/$job" >/dev/null 2>&1; then
                continue
            fi
            conditions="$(kubectl --namespace="$NAMESPACE" get "job/$job" \
                -o jsonpath='{range .status.conditions[*]}{.type}={.status}{"\n"}{end}' 2>/dev/null)"
            if grep -Eq '^(Complete|Failed)=True' <<<"$conditions"; then
                continue
            fi
            echo "Stopping non-terminal job/$job before releasing ingestion locks..." >&2
            if ! kubectl --namespace="$NAMESPACE" delete "job/$job" \
                --ignore-not-found --wait=true --timeout=2m >/dev/null; then
                safe_to_release=false
                echo "ERROR: could not stop job/$job." >&2
            fi
            pods="$(kubectl --namespace="$NAMESPACE" get pods \
                --selector="job-name=$job" -o name 2>/dev/null)"
            # shellcheck disable=SC2086
            if [[ -n "$pods" ]] && ! kubectl --namespace="$NAMESPACE" wait \
                --for=delete $pods --timeout=2m >/dev/null; then
                safe_to_release=false
                echo "ERROR: pods for job/$job did not terminate." >&2
            fi
        done
    fi
    if [[ "$safe_to_release" != true ]]; then
        echo "Ingestion locks and CronJob suspension were retained to prevent overlap." >&2
        echo "Resolve the active Jobs, then remove the run-lock ConfigMaps and resume schedules explicitly." >&2
        exit "$original_status"
    fi
    for cronjob in "${RESTORE_CRONJOBS[@]}"; do
        desired_state="${RESTORE_CRONJOB_STATES[$cronjob]}"
        if ! kubectl --namespace="$NAMESPACE" patch "cronjob/$cronjob" \
            --type=merge --patch "{\"spec\":{\"suspend\":${desired_state}}}" >/dev/null; then
            echo "ERROR: could not restore cronjob/$cronjob suspend=$desired_state." >&2
            safe_to_release=false
            (( original_status == 0 )) && original_status=1
        fi
    done
    if [[ "$safe_to_release" != true ]]; then
        echo "Run-lock ConfigMaps were retained because schedule restoration failed." >&2
        exit "$original_status"
    fi
    for lock in "${OPERATION_LOCKS[@]}"; do
        if ! kubectl --namespace="$NAMESPACE" delete "configmap/$lock" \
            --ignore-not-found --wait=false >/dev/null; then
            echo "ERROR: could not release configmap/$lock." >&2
            (( original_status == 0 )) && original_status=1
        fi
    done
    if (( original_status == 0 )) && [[ "$SHOW_COMPLETION_STATUS" == true ]]; then
        echo ""
        echo "=== Deployment complete ==="
        echo ""
        echo "CronJobs after schedule restoration:"
        kubectl get cronjobs -n "$NAMESPACE" -l app.kubernetes.io/name=url-ingest
    fi
    exit "$original_status"
}
trap cleanup_operations EXIT

acquire_operation_lock() {
    local lock="$1"
    if ! kubectl --namespace="$NAMESPACE" create configmap "$lock" \
        --from-literal="run-id=$RUN_ID" \
        --from-literal="created-at=$(date -u +%Y-%m-%dT%H:%M:%SZ)" >/dev/null; then
        echo "ERROR: operation lock configmap/$lock already exists in $NAMESPACE." >&2
        echo "Inspect active Jobs before removing a stale lock." >&2
        return 1
    fi
    OPERATION_LOCKS+=("$lock")
}

# Acquire every collection lock before this invocation changes schedule state.
# A concurrent invocation that loses the lock race therefore cannot resume a
# schedule owned by the active run during its EXIT cleanup.
if [[ -n "$OPERATION_COLLECTIONS" ]]; then
    for collection in $OPERATION_COLLECTIONS; do
        acquire_operation_lock "url-ingest-${collection}-run-lock"
    done
    for collection in $OPERATION_COLLECTIONS; do
        cronjob="url-ingest-${collection}"
        previous_suspend="$(kubectl --namespace="$NAMESPACE" get "cronjob/$cronjob" \
            -o jsonpath='{.spec.suspend}' 2>/dev/null || true)"
        [[ "$previous_suspend" == true ]] || previous_suspend=false
        RESTORE_CRONJOBS+=("$cronjob")
        RESTORE_CRONJOB_STATES["$cronjob"]="$previous_suspend"
    done
fi

# 6. Create CronJobs
echo "Creating CronJobs..."
apply_manifest "$SCRIPT_DIR/cronjob-nvidia.yaml"
apply_manifest "$SCRIPT_DIR/cronjob-kubernetes.yaml"
apply_manifest "$SCRIPT_DIR/cronjob-vetpartner.yaml"
apply_manifest "$SCRIPT_DIR/cronjob-semianalysis.yaml"
apply_manifest "$SCRIPT_DIR/cronjob-mentalhealth.yaml"

# Operation coordination ----------------------------------------------------

suspend_cronjob() {
    local cronjob="$1" suspended
    suspended="$(kubectl --namespace="$NAMESPACE" get "cronjob/$cronjob" \
        -o jsonpath='{.spec.suspend}')"
    if [[ "$suspended" != "true" ]]; then
        kubectl --namespace="$NAMESPACE" patch "cronjob/$cronjob" \
            --type=merge --patch '{"spec":{"suspend":true}}' >/dev/null
    fi
}

nonterminal_jobs() {
    local collection="$1"
    kubectl --namespace="$NAMESPACE" get jobs \
        --selector="app.kubernetes.io/name=url-ingest,app.kubernetes.io/component=$collection" \
        -o json | python3 -c '
import json, sys
for item in json.load(sys.stdin).get("items", []):
    terminal = any(
        c.get("type") in {"Complete", "Failed"} and c.get("status") == "True"
        for c in item.get("status", {}).get("conditions", [])
    )
    if not terminal:
        print(item["metadata"]["name"])
'
}

wait_for_api_endpoint() {
    local label="$1" path="$2" started_at="$SECONDS" output=""
    until output="$(kubectl get --raw "$path" 2>&1)"; do
        if (( SECONDS - started_at >= READINESS_TIMEOUT_SECONDS )); then
            echo "ERROR: $label was not ready within ${READINESS_TIMEOUT_SECONDS}s: $output" >&2
            return 1
        fi
        sleep 5
    done
    echo "Ready: $label"
}

wait_for_ingest_dependencies() {
    echo "=== Waiting for ingestion dependencies ==="
    kubectl --namespace="$MILVUS_NAMESPACE" get "service/$MILVUS_SERVICE" >/dev/null
    kubectl --namespace="$MILVUS_NAMESPACE" get "service/$MINIO_SERVICE" >/dev/null
    kubectl --namespace="$NV_INGEST_NAMESPACE" get "service/$NV_INGEST_SERVICE" >/dev/null
    wait_for_api_endpoint "Milvus" \
        "/api/v1/namespaces/${MILVUS_NAMESPACE}/services/http:${MILVUS_SERVICE}:9091/proxy/healthz"
    wait_for_api_endpoint "MinIO write readiness" \
        "/api/v1/namespaces/${MILVUS_NAMESPACE}/services/http:${MINIO_SERVICE}:9000/proxy/minio/health/ready"
    wait_for_api_endpoint "NV-Ingest" \
        "/api/v1/namespaces/${NV_INGEST_NAMESPACE}/services/http:${NV_INGEST_SERVICE}:7670/proxy/v1/health/ready"
}

replace_terminal_job() {
    local job="$1" conditions
    if ! kubectl --namespace="$NAMESPACE" get "job/$job" >/dev/null 2>&1; then
        return 0
    fi
    conditions="$(kubectl --namespace="$NAMESPACE" get "job/$job" \
        -o jsonpath='{range .status.conditions[*]}{.type}={.status}{"\n"}{end}')"
    if ! grep -Eq '^(Complete|Failed)=True' <<<"$conditions"; then
        echo "ERROR: refusing to replace non-terminal job/$job." >&2
        return 1
    fi
    kubectl --namespace="$NAMESPACE" delete "job/$job" --wait=true >/dev/null
}

create_manual_job() {
    local name="$1" collection="$2" run_id="$3" shard_index="$4" shard_count="$5" verify_only="$6"
    local -a options=(
        --name "$name"
        --namespace "$NAMESPACE"
        --collection "$collection"
        --run-id "$run_id"
        --shard-index "$shard_index"
        --shard-count "$shard_count"
        --client-image "$URL_INGEST_RUNNER_IMAGE"
        --milvus-uri "$MILVUS_URI"
        --minio-endpoint "$MINIO_ENDPOINT"
        --minio-bucket "$MINIO_BUCKET"
        --nv-ingest-host "$NV_INGEST_MESSAGE_HOST"
        --nv-ingest-port "$NV_INGEST_MESSAGE_PORT"
        --client-timeout "$CLIENT_REQUEST_TIMEOUT_SECONDS"
        --client-max-retries "$CLIENT_MAX_JOB_RETRIES"
        --client-retry-delay "$CLIENT_RETRY_DELAY_SECONDS"
        --verify-timeout "$COLLECTION_VERIFY_TIMEOUT_SECONDS"
        --active-deadline "$INGEST_JOB_DEADLINE_SECONDS"
        --backoff-limit "$MANUAL_JOB_BACKOFF_LIMIT"
    )
    [[ "$verify_only" == true ]] && options+=(--verify-only)
    [[ "$RUN_CRAWL" == true && "$verify_only" != true ]] && options+=(--run-crawl)
    kubectl --namespace="$NAMESPACE" get "cronjob/url-ingest-${collection}" -o json | \
        python3 "$SCRIPT_DIR/render-manual-job.py" "${options[@]}" | \
        kubectl --namespace="$NAMESPACE" create -f - >/dev/null
    RUN_JOBS+=("$name")
    echo "Created job/$name"
}

run_collection() {
    local collection="$1" shard_count=1 shard_index shard_id job_name verify_name start_index=0
    local -a batch=()
    if [[ "$collection" == nvidia ]]; then
        shard_count="$NVIDIA_INGEST_SHARDS"
    fi
    echo "=== Populating collection: $collection (shards=$shard_count) ==="
    # Establish the collection and perform any stale-manifest reset before
    # optional parallel shards begin writing the shared manifest.
    if (( shard_count > 1 )); then
        shard_id=00
        job_name="url-ingest-${collection}-${RUN_ID}-s${shard_id}"
        create_manual_job "$job_name" "$collection" \
            "${RUN_ID}-s${shard_id}" 0 "$shard_count" false
        wait_for_jobs "$NAMESPACE" "$INGEST_WAIT_TIMEOUT_SECONDS" "$INGEST_POLL_SECONDS" "$job_name"
        start_index=1
    fi
    for ((shard_index = start_index; shard_index < shard_count; shard_index++)); do
        printf -v shard_id '%02d' "$shard_index"
        job_name="url-ingest-${collection}-${RUN_ID}-s${shard_id}"
        create_manual_job "$job_name" "$collection" \
            "${RUN_ID}-s${shard_id}" "$shard_index" "$shard_count" false
        batch+=("$job_name")
        if (( ${#batch[@]} >= MAX_PARALLEL_INGEST_SHARDS || shard_index + 1 == shard_count )); then
            wait_for_jobs "$NAMESPACE" "$INGEST_WAIT_TIMEOUT_SECONDS" "$INGEST_POLL_SECONDS" "${batch[@]}"
            batch=()
        fi
    done

    verify_name="url-ingest-${collection}-${RUN_ID}-verify"
    create_manual_job "$verify_name" "$collection" "${RUN_ID}-verify" 0 1 true
    wait_for_jobs "$NAMESPACE" "$INGEST_WAIT_TIMEOUT_SECONDS" "$INGEST_POLL_SECONDS" "$verify_name"
    echo "Verified collection: $collection"
}

# 7. Optionally stage data from the repository snapshot. The hostPath exists on
# one known node, so explicitly pin both copy Jobs to that configurable node.
if [[ "$STAGE_DATA" == true ]]; then
    echo ""
    echo "=== Staging repository data on ingestion PVCs ==="
    acquire_operation_lock url-ingest-stage-lock
    for collection in $ALL_COLLECTIONS; do
        active="$(nonterminal_jobs "$collection")"
        if [[ -n "$active" ]]; then
            echo "ERROR: refusing to stage while collection $collection has non-terminal Jobs:" >&2
            printf '%s\n' "$active" >&2
            exit 1
        fi
    done
    kubectl get "node/$URL_INGEST_SOURCE_NODE" >/dev/null
    node_ready="$(kubectl get "node/$URL_INGEST_SOURCE_NODE" \
        -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}')"
    if [[ "$node_ready" != True ]]; then
        echo "ERROR: source node $URL_INGEST_SOURCE_NODE is not Ready." >&2
        exit 1
    fi

    replace_terminal_job url-ingest-seed-data
    url_source="$(escape_sed_replacement "$URL_INGEST_SOURCE_HOST_PATH")"
    render_manifest "$SCRIPT_DIR/seed-data-job.yaml" | \
        sed "s|/volume2/daedalus/datasets/daedalus-context/url-ingest|${url_source}|g" | \
        kubectl --namespace="$NAMESPACE" create -f - >/dev/null
    RUN_JOBS+=(url-ingest-seed-data)
    wait_for_jobs "$NAMESPACE" "$STAGE_URL_TIMEOUT_SECONDS" "$INGEST_POLL_SECONDS" \
        url-ingest-seed-data

    echo "URL repository data staged successfully."
fi

# 8. Trigger bounded Jobs, synchronously wait, and verify each requested
# collection. No successful command can silently skip a missing CronJob.
if [[ "$RUN_NOW" == true ]]; then
    wait_for_ingest_dependencies
    for collection in $RUN_COLLECTIONS; do
        cronjob="url-ingest-${collection}"
        kubectl --namespace="$NAMESPACE" get "cronjob/$cronjob" >/dev/null
        suspend_cronjob "$cronjob"
        active="$(nonterminal_jobs "$collection")"
        if [[ -n "$active" ]]; then
            echo "ERROR: collection $collection already has non-terminal Jobs:" >&2
            printf '%s\n' "$active" >&2
            exit 1
        fi
    done

    for collection in $RUN_COLLECTIONS; do
        run_collection "$collection"
    done
    echo "All requested collections were populated and verified."
fi

# The EXIT cleanup restores schedules and releases locks before printing the
# final state, avoiding a misleading success summary with temporary suspension.
SHOW_COMPLETION_STATUS=true
