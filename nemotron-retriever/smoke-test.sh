#!/usr/bin/env bash
set -euo pipefail

EMBED_API_KEY="${EMBED_API_KEY:-not-used}"
EMBED_MODEL="${EMBED_MODEL:-nvidia/llama-nemotron-embed-vl-1b-v2}"
EMBED_BASE_URL="${EMBED_BASE_URL:-http://192.168.1.12:8000/v1}"
EMBED_TIMEOUT_SECONDS="${EMBED_TIMEOUT_SECONDS:-300}"

curl --fail-with-body --silent --show-error \
  --max-time "${EMBED_TIMEOUT_SECONDS}" \
  --output /dev/null \
  "${EMBED_BASE_URL%/}/embeddings" \
  --header "Authorization: Bearer ${EMBED_API_KEY}" \
  --header "Content-Type: application/json" \
  --data "{
    \"model\": \"${EMBED_MODEL}\",
    \"input\": [\"NVIDIA Dynamo optimizes distributed LLM inference.\"],
    \"input_type\": \"query\",
    \"encoding_format\": \"float\"
  }"

printf 'Embedding smoke test passed: %s/embeddings\n' "${EMBED_BASE_URL%/}"
