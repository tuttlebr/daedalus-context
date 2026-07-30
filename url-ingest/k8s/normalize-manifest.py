#!/usr/bin/env python3
"""Apply the common Daedalus Context labels to URL-ingest-owned resources."""

from __future__ import annotations

import sys

import yaml


COMMON = {
    "app.kubernetes.io/name": "url-ingest",
    "app.kubernetes.io/instance": "url-ingest",
    "app.kubernetes.io/version": "0.1.0",
    "app.kubernetes.io/part-of": "daedalus-context",
    "app.kubernetes.io/managed-by": "contextctl",
    "daedalus.ai/tier": "ingestion",
    "daedalus.ai/capability": "collection-refresh",
}

COMPONENT_BY_KIND = {
    "ConfigMap": "configuration",
    "CronJob": "scheduler",
    "Job": "ingest-run",
    "PersistentVolumeClaim": "staging-storage",
    "ServiceAccount": "runner",
}


def add_labels(metadata: dict | None, component: str) -> None:
    if metadata is None:
        return
    labels = metadata.setdefault("labels", {})
    for key, value in COMMON.items():
        labels.setdefault(key, value)
    labels.setdefault("app.kubernetes.io/component", component)


def normalize(document: dict) -> dict:
    kind = document.get("kind")
    component = COMPONENT_BY_KIND.get(kind, "ingestion")
    add_labels(document.setdefault("metadata", {}), component)
    spec = document.get("spec", {})
    if kind in {"Deployment", "StatefulSet", "DaemonSet", "Job"}:
        add_labels(spec.get("template", {}).get("metadata"), component)
    elif kind == "CronJob":
        add_labels(
            spec.get("jobTemplate", {})
            .get("spec", {})
            .get("template", {})
            .get("metadata"),
            component,
        )
    return document


documents = [
    normalize(document)
    for document in yaml.safe_load_all(sys.stdin)
    if isinstance(document, dict)
]
yaml.safe_dump_all(documents, sys.stdout, explicit_start=False, sort_keys=False)
