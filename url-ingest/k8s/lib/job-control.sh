#!/usr/bin/env bash

# Shared Job wait/diagnostic helpers. The caller is expected to enable strict
# shell mode. KUBECTL_BIN may be replaced by a test double.
KUBECTL_BIN="${KUBECTL_BIN:-kubectl}"
WAIT_FOR_ANY_COMPLETED_JOB=""

show_job_diagnostics() {
    local namespace="$1" job="$2" pod
    echo "--- Diagnostics for job/$job ---" >&2
    "$KUBECTL_BIN" --namespace="$namespace" describe "job/$job" >&2 || true
    while IFS= read -r pod; do
        [[ -n "$pod" ]] || continue
        "$KUBECTL_BIN" --namespace="$namespace" logs "$pod" \
            --all-containers --prefix --tail=300 >&2 || true
        "$KUBECTL_BIN" --namespace="$namespace" describe "$pod" >&2 || true
    done < <("$KUBECTL_BIN" --namespace="$namespace" get pods \
        --selector="job-name=$job" -o name 2>/dev/null || true)
    "$KUBECTL_BIN" --namespace="$namespace" get events \
        --field-selector="involvedObject.name=$job" \
        --sort-by=.lastTimestamp >&2 || true
}

wait_for_jobs() {
    local namespace="$1" timeout_seconds="$2" poll_seconds="$3"
    shift 3
    local -a pending=("$@") next=()
    local started_at="$SECONDS" job conditions
    if (( ${#pending[@]} == 0 )); then
        echo "ERROR: wait_for_jobs requires at least one Job." >&2
        return 2
    fi

    while (( ${#pending[@]} > 0 )); do
        next=()
        for job in "${pending[@]}"; do
            if ! conditions="$("$KUBECTL_BIN" --namespace="$namespace" get "job/$job" \
                -o jsonpath='{range .status.conditions[*]}{.type}={.status}{" "}{.reason}{"\n"}{end}' 2>&1)"; then
                echo "ERROR: could not inspect job/$job: $conditions" >&2
                show_job_diagnostics "$namespace" "$job"
                return 1
            fi
            if grep -q '^Complete=True' <<<"$conditions"; then
                echo "Job completed: $job"
                continue
            fi
            if grep -q '^Failed=True' <<<"$conditions"; then
                echo "ERROR: job/$job failed." >&2
                show_job_diagnostics "$namespace" "$job"
                return 1
            fi
            next+=("$job")
        done
        pending=("${next[@]}")
        (( ${#pending[@]} == 0 )) && return 0
        if (( SECONDS - started_at >= timeout_seconds )); then
            echo "ERROR: timed out after ${timeout_seconds}s waiting for: ${pending[*]}" >&2
            for job in "${pending[@]}"; do
                show_job_diagnostics "$namespace" "$job"
            done
            return 1
        fi
        sleep "$poll_seconds"
    done
}

wait_for_any_job() {
    local namespace="$1" timeout_seconds="$2" poll_seconds="$3"
    shift 3
    local -a pending=("$@")
    local started_at="$SECONDS" job conditions
    WAIT_FOR_ANY_COMPLETED_JOB=""
    if (( ${#pending[@]} == 0 )); then
        echo "ERROR: wait_for_any_job requires at least one Job." >&2
        return 2
    fi

    while true; do
        for job in "${pending[@]}"; do
            if ! conditions="$("$KUBECTL_BIN" --namespace="$namespace" get "job/$job" \
                -o jsonpath='{range .status.conditions[*]}{.type}={.status}{" "}{.reason}{"\n"}{end}' 2>&1)"; then
                echo "ERROR: could not inspect job/$job: $conditions" >&2
                show_job_diagnostics "$namespace" "$job"
                return 1
            fi
            if grep -q '^Failed=True' <<<"$conditions"; then
                echo "ERROR: job/$job failed." >&2
                show_job_diagnostics "$namespace" "$job"
                return 1
            fi
            if grep -q '^Complete=True' <<<"$conditions"; then
                WAIT_FOR_ANY_COMPLETED_JOB="$job"
                echo "Job completed: $job"
                return 0
            fi
        done
        if (( SECONDS - started_at >= timeout_seconds )); then
            echo "ERROR: timed out after ${timeout_seconds}s waiting for one of: ${pending[*]}" >&2
            for job in "${pending[@]}"; do
                show_job_diagnostics "$namespace" "$job"
            done
            return 1
        fi
        sleep "$poll_seconds"
    done
}
