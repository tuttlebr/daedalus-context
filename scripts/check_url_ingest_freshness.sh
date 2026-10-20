#!/usr/bin/env bash
set -euo pipefail
: "${NAMESPACE:?NAMESPACE is required}"
: "${COLLECTIONS:?COLLECTIONS is required}"
KUBECTL="${KUBECTL:-kubectl}"
failed=0
for collection in ${COLLECTIONS}; do
  suspended="$("${KUBECTL}" -n "${NAMESPACE}" get cronjob "url-ingest-${collection}" -o jsonpath='{.spec.suspend}')"
  if [[ "${suspended}" == true ]]; then
    echo "${collection}=suspended" >&2
    failed=1
  fi
done
snapshot="$("${KUBECTL}" -n "${NAMESPACE}" exec deployment/url-ingest-monitor -- python3 -c 'import urllib.request; print(urllib.request.urlopen("http://localhost:8080/status",timeout=10).read().decode())')"
for collection in ${COLLECTIONS}; do
  if ! jq -e --arg collection "${collection}" '.collections[$collection].healthy == true' <<<"${snapshot}" >/dev/null; then
    failed=1
  fi
  jq --arg collection "${collection}" '.collections[$collection] | {collection:$collection,state,fresh,last_success,age_seconds,failure_count,stalled}' <<<"${snapshot}"
done
exit "${failed}"
