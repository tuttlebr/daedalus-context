#!/usr/bin/env python3
"""Render an observable, immutable manual Job from a url-ingest CronJob.

The CronJob is read as JSON on stdin.  Keeping this transformation in Python
avoids a dependency on jq and, importantly, lets us set all runtime overrides
before Kubernetes creates a Pod.
"""

from __future__ import annotations

import argparse
import copy
import json
import sys


def _set_env(container: dict, name: str, value: str) -> None:
    env = container.setdefault("env", [])
    env[:] = [item for item in env if item.get("name") != name]
    env.append({"name": name, "value": value})


def render_job(cronjob: dict, args: argparse.Namespace) -> dict:
    if cronjob.get("kind") != "CronJob":
        raise ValueError("input must be a batch/v1 CronJob")

    job_spec = copy.deepcopy(cronjob["spec"]["jobTemplate"]["spec"])
    pod_template = job_spec["template"]
    pod_spec = pod_template["spec"]
    if not args.run_crawl:
        pod_spec.pop("initContainers", None)

    containers = pod_spec.get("containers", [])
    try:
        container = next(item for item in containers if item.get("name") == "nv-ingest")
    except StopIteration as exc:
        raise ValueError("CronJob has no nv-ingest container") from exc

    container["image"] = args.client_image
    overrides = {
        "SINGLE_COLLECTION": args.collection,
        "INGEST_RUN_ID": args.run_id,
        "INGEST_SHARD_COUNT": str(args.shard_count),
        "INGEST_SHARD_INDEX": str(args.shard_index),
        "INGEST_VERIFY_ONLY": str(args.verify_only).lower(),
        "GLOBAL_INGEST_CAPACITY": str(args.global_ingest_capacity),
        "MILVUS_URI": args.milvus_uri,
        "MINIO_ENDPOINT": args.minio_endpoint,
        "MINIO_BUCKET": args.minio_bucket,
        "NV_INGEST_MESSAGE_HOST": args.nv_ingest_host,
        "NV_INGEST_MESSAGE_PORT": str(args.nv_ingest_port),
        "CLIENT_REQUEST_TIMEOUT_SECONDS": str(args.client_timeout),
        "CLIENT_MAX_JOB_RETRIES": str(args.client_max_retries),
        "CLIENT_RETRY_DELAY_SECONDS": str(args.client_retry_delay),
        "COLLECTION_VERIFY_TIMEOUT_SECONDS": str(args.verify_timeout),
        "STATUS_REPORT_PATH": (
            f"/manifests/status/{args.collection}-{args.run_id}-status.json"
        ),
        "FAILURES_REPORT_PATH": (
            f"/manifests/failures/{args.collection}-{args.run_id}-failures.json"
        ),
    }
    for name, value in overrides.items():
        _set_env(container, name, value)

    job_spec["activeDeadlineSeconds"] = args.active_deadline
    job_spec["backoffLimit"] = args.backoff_limit
    job_spec["ttlSecondsAfterFinished"] = args.ttl_seconds

    labels = copy.deepcopy(cronjob.get("metadata", {}).get("labels", {}))
    labels.update(
        {
            "app.kubernetes.io/name": "url-ingest",
            "app.kubernetes.io/component": args.collection,
            "app.kubernetes.io/managed-by": "url-ingest-deploy",
            "manual-run": "true",
            "ingest.daedalus/run-id": args.run_id,
        }
    )
    pod_template.setdefault("metadata", {}).setdefault("labels", {}).update(labels)

    return {
        "apiVersion": "batch/v1",
        "kind": "Job",
        "metadata": {
            "name": args.name,
            "namespace": args.namespace,
            "labels": labels,
        },
        "spec": job_spec,
    }


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--name", required=True)
    parser.add_argument("--namespace", required=True)
    parser.add_argument("--collection", required=True)
    parser.add_argument("--run-id", required=True)
    parser.add_argument("--shard-count", type=int, default=1)
    parser.add_argument("--shard-index", type=int, default=0)
    parser.add_argument("--verify-only", action="store_true")
    parser.add_argument("--run-crawl", action="store_true")
    parser.add_argument("--global-ingest-capacity", type=int, default=1)
    parser.add_argument("--client-image", required=True)
    parser.add_argument("--milvus-uri", required=True)
    parser.add_argument("--minio-endpoint", required=True)
    parser.add_argument("--minio-bucket", required=True)
    parser.add_argument("--nv-ingest-host", required=True)
    parser.add_argument("--nv-ingest-port", type=int, required=True)
    parser.add_argument("--client-timeout", type=int, required=True)
    parser.add_argument("--client-max-retries", type=int, required=True)
    parser.add_argument("--client-retry-delay", type=float, required=True)
    parser.add_argument("--verify-timeout", type=int, required=True)
    parser.add_argument("--active-deadline", type=int, required=True)
    parser.add_argument("--backoff-limit", type=int, default=1)
    parser.add_argument("--ttl-seconds", type=int, default=604800)
    args = parser.parse_args()
    if args.shard_count < 1:
        parser.error("--shard-count must be >= 1")
    if args.global_ingest_capacity < 1:
        parser.error("--global-ingest-capacity must be >= 1")
    if not 0 <= args.shard_index < args.shard_count:
        parser.error("--shard-index must be in [0, shard-count)")
    for name in (
        "client_timeout",
        "client_max_retries",
        "verify_timeout",
        "active_deadline",
        "ttl_seconds",
    ):
        if getattr(args, name) < 1:
            parser.error(f"--{name.replace('_', '-')} must be >= 1")
    if args.client_retry_delay <= 0:
        parser.error("--client-retry-delay must be > 0")
    return args


def main() -> None:
    args = parse_args()
    cronjob = json.load(sys.stdin)
    json.dump(render_job(cronjob, args), sys.stdout)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
