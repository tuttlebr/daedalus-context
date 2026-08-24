#!/usr/bin/env python3
"""Render a least-privilege stale capacity-lock cleanup Job.

The source CronJob is read as JSON on stdin so the cleanup Pod inherits the
deployed runner image, scheduling constraints, security settings, pull
credentials, and manifest PVC. The caller is responsible for suspending every
url-ingest CronJob and proving there are no non-terminal ingestion Jobs before
creating the rendered Job.
"""

from __future__ import annotations

import argparse
import copy
import json
import sys


LOCK_CLEANUP_SCRIPT = """\
set -eu
removed=0
for lock in /manifests/global-ingest-capacity.lock /manifests/global-ingest-capacity-*.lock; do
  [ -d "${lock}" ] || continue
  rmdir "${lock}"
  removed=$((removed + 1))
done
echo "Removed ${removed} stale ingestion capacity lock(s)."
"""


def render_job(cronjob: dict, args: argparse.Namespace) -> dict:
    if cronjob.get("kind") != "CronJob":
        raise ValueError("input must be a batch/v1 CronJob")

    source_pod = cronjob["spec"]["jobTemplate"]["spec"]["template"]["spec"]
    try:
        source_container = next(
            item
            for item in source_pod.get("containers", [])
            if item.get("name") == "nv-ingest"
        )
    except StopIteration as exc:
        raise ValueError("CronJob has no nv-ingest container") from exc
    try:
        manifest_volume = next(
            item
            for item in source_pod.get("volumes", [])
            if item.get("name") == "manifests"
        )
    except StopIteration as exc:
        raise ValueError("CronJob has no manifests volume") from exc

    labels = copy.deepcopy(cronjob.get("metadata", {}).get("labels", {}))
    labels.update(
        {
            "app.kubernetes.io/name": "url-ingest",
            "app.kubernetes.io/component": "capacity-lock-cleanup",
            "app.kubernetes.io/managed-by": "url-ingest-deploy",
        }
    )
    pod_spec = {
        field: copy.deepcopy(source_pod[field])
        for field in (
            "serviceAccountName",
            "automountServiceAccountToken",
            "securityContext",
            "imagePullSecrets",
            "nodeSelector",
            "affinity",
            "tolerations",
        )
        if field in source_pod
    }
    container = {
        "name": "capacity-lock-cleanup",
        "image": source_container["image"],
        "imagePullPolicy": source_container.get("imagePullPolicy", "IfNotPresent"),
        "command": ["/bin/sh", "-c"],
        "args": [LOCK_CLEANUP_SCRIPT],
        "volumeMounts": [{"name": "manifests", "mountPath": "/manifests"}],
    }
    if "securityContext" in source_container:
        container["securityContext"] = copy.deepcopy(
            source_container["securityContext"]
        )
    pod_spec.update(
        {
            "restartPolicy": "Never",
            "containers": [container],
            "volumes": [copy.deepcopy(manifest_volume)],
        }
    )

    return {
        "apiVersion": "batch/v1",
        "kind": "Job",
        "metadata": {
            "name": args.name,
            "namespace": args.namespace,
            "labels": labels,
        },
        "spec": {
            "activeDeadlineSeconds": args.active_deadline,
            "backoffLimit": 0,
            "ttlSecondsAfterFinished": args.ttl_seconds,
            "template": {
                "metadata": {"labels": labels},
                "spec": pod_spec,
            },
        },
    }


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--name", required=True)
    parser.add_argument("--namespace", required=True)
    parser.add_argument("--active-deadline", type=int, default=300)
    parser.add_argument("--ttl-seconds", type=int, default=600)
    args = parser.parse_args()
    if args.active_deadline < 1:
        parser.error("--active-deadline must be >= 1")
    if args.ttl_seconds < 1:
        parser.error("--ttl-seconds must be >= 1")
    return args


def main() -> None:
    args = parse_args()
    cronjob = json.load(sys.stdin)
    json.dump(render_job(cronjob, args), sys.stdout)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
