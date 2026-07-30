#!/usr/bin/env bash
set -euo pipefail

: "${NAMESPACE:?NAMESPACE is required}"
: "${COLLECTIONS:?COLLECTIONS is required}"

KUBECTL="${KUBECTL:-kubectl}"
RUNNING_GRACE_SECONDS="${RUNNING_GRACE_SECONDS:-86400}"
now_epoch="$(date +%s)"
failures=()

for collection in ${COLLECTIONS}; do
  cronjob="url-ingest-${collection}"
  selector="app.kubernetes.io/name=url-ingest,app.kubernetes.io/component=${collection}"
  cronjob_json="$("${KUBECTL}" --namespace="${NAMESPACE}" get cronjob "${cronjob}" -o json)"
  jobs_json="$("${KUBECTL}" --namespace="${NAMESPACE}" get jobs --selector="${selector}" -o json)"

  suspended="$(jq -r '.spec.suspend // false' <<<"${cronjob_json}")"
  last_schedule="$(jq -r '.status.lastScheduleTime // empty' <<<"${cronjob_json}")"
  last_success="$(jq -r '.status.lastSuccessfulTime // empty' <<<"${cronjob_json}")"
  active_count="$(jq '[.items[] | select((.status.active // 0) > 0)] | length' <<<"${jobs_json}")"
  newest_active_start="$(jq -r '
    [.items[]
      | select((.status.active // 0) > 0)
      | (.status.startTime // .metadata.creationTimestamp)]
    | sort
    | last // empty
  ' <<<"${jobs_json}")"
  latest_job_success="$(jq -r '
    [.items[]
      | select(any(.status.conditions[]?;
          .type == "Complete" and .status == "True"))
      | (.status.completionTime // .metadata.creationTimestamp)]
    | sort
    | last // empty
  ' <<<"${jobs_json}")"
  effective_success="${last_success}"
  if [[ -n "${latest_job_success}" ]] \
    && { [[ -z "${effective_success}" ]] || [[ "${latest_job_success}" > "${effective_success}" ]]; }; then
    effective_success="${latest_job_success}"
  fi

  active_within_grace=false
  if (( active_count > 0 )) && [[ -n "${newest_active_start}" ]]; then
    active_epoch="$(date --date="${newest_active_start}" +%s)"
    if (( now_epoch - active_epoch <= RUNNING_GRACE_SECONDS )); then
      active_within_grace=true
    fi
  fi

  if [[ "${suspended}" == "true" ]]; then
    lock="url-ingest-${collection}-run-lock"
    if ! "${KUBECTL}" --namespace="${NAMESPACE}" get configmap "${lock}" >/dev/null 2>&1 \
      || (( active_count == 0 )); then
      failures+=("${collection}=suspended-without-active-lock")
    fi
  fi

  if [[ -z "${last_schedule}" ]]; then
    if [[ -z "${effective_success}" && "${active_within_grace}" != "true" ]]; then
      failures+=("${collection}=never-scheduled")
    fi
  elif [[ -z "${effective_success}" ]]; then
    if [[ "${active_within_grace}" != "true" ]]; then
      failures+=("${collection}=never-succeeded")
    fi
  elif [[ "${effective_success}" < "${last_schedule}" && "${active_within_grace}" != "true" ]]; then
    failures+=("${collection}=latest-schedule-not-successful")
  fi

  latest_terminal="$(jq -r '
    [.items[]
      | select(any(.status.conditions[]?;
          (.type == "Complete" or .type == "Failed") and .status == "True"))]
    | sort_by(.metadata.creationTimestamp)
    | last // empty
  ' <<<"${jobs_json}")"
  if [[ -n "${latest_terminal}" ]]; then
    latest_failed="$(jq -r '
      any(.status.conditions[]?; .type == "Failed" and .status == "True")
    ' <<<"${latest_terminal}")"
    if [[ "${latest_failed}" == "true" ]]; then
      latest_name="$(jq -r '.metadata.name' <<<"${latest_terminal}")"
      failures+=("${collection}=failed:${latest_name}")
    fi
  fi

  printf '%s schedule=%s success=%s active=%s grace=%s\n' \
    "${collection}" "${last_schedule:-none}" "${effective_success:-none}" \
    "${active_count}" "${active_within_grace}"
done

if (( ${#failures[@]} )); then
  printf 'URL ingest freshness gate failed: %s\n' "${failures[*]}" >&2
  exit 1
fi
