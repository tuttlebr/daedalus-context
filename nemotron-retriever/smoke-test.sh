#!/usr/bin/env bash
set -euo pipefail

EMBED_API_KEY="${EMBED_API_KEY:-not-used}"
EMBED_MODEL="${EMBED_MODEL:-}"
EMBED_BASE_URL="${EMBED_BASE_URL:-http://127.0.0.1:8000/v1}"
EMBED_TIMEOUT_SECONDS="${EMBED_TIMEOUT_SECONDS:-300}"

payload="$(python3 - "${EMBED_MODEL}" <<'PY'
import json
import sys

request = {
    "input": ["NVIDIA Dynamo optimizes distributed LLM inference."],
    "input_type": "query",
    "encoding_format": "float",
}
if sys.argv[1]:
    request["model"] = sys.argv[1]
print(json.dumps(request))
PY
)"

curl --fail-with-body --silent --show-error \
  --max-time "${EMBED_TIMEOUT_SECONDS}" \
  --output /dev/null \
  "${EMBED_BASE_URL%/}/embeddings" \
  --header "Authorization: Bearer ${EMBED_API_KEY}" \
  --header "Content-Type: application/json" \
  --data "${payload}"

printf 'Embedding smoke test passed: %s/embeddings\n' "${EMBED_BASE_URL%/}"
