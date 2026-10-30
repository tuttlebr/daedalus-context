#!/usr/bin/env python3
"""Adapt the pinned upstream MinIO chart for placement and its bundled client."""

import sys

import yaml


def render(documents):
    matches = 0
    initializers = 0
    for document in documents:
        if not document:
            continue
        if (document.get("kind") == "ConfigMap"
                and document.get("metadata", {}).get("labels", {}).get("app") == "minio"):
            script = document.get("data", {}).get("initialize", "")
            old = "${MC} policy set $POLICY myminio/$BUCKET"
            new = "${MC} anonymous set $POLICY myminio/$BUCKET"
            if script.count(old) + script.count(new) != 1:
                raise ValueError("Upstream MinIO bucket initializer changed; review the post-renderer")
            document["data"]["initialize"] = script.replace(old, new)
            initializers += 1
        if document.get("kind") != "StatefulSet":
            continue
        labels = document["spec"]["template"]["metadata"].get("labels", {})
        if labels.get("app") != "minio":
            continue
        release = labels.get("release")
        if not release:
            raise ValueError("MinIO is missing its release label")
        spec = document["spec"]["template"]["spec"]
        constraint = {
            "maxSkew": 1,
            "topologyKey": "kubernetes.io/hostname",
            "whenUnsatisfiable": "DoNotSchedule",
            "nodeTaintsPolicy": "Honor",
            "labelSelector": {"matchLabels": {"app": "minio", "release": release}},
        }
        existing = spec.get("topologySpreadConstraints")
        if existing and existing != [constraint]:
            raise ValueError("Upstream MinIO now defines topology spread; review the post-renderer")
        spec["topologySpreadConstraints"] = [constraint]
        matches += 1
    if matches != 1:
        raise ValueError(f"Expected one MinIO StatefulSet, found {matches}")
    if initializers != 1:
        raise ValueError(f"Expected one MinIO bucket initializer, found {initializers}")
    return documents


if __name__ == "__main__":
    try:
        documents = render(list(yaml.safe_load_all(sys.stdin)))
    except ValueError as error:
        sys.exit(str(error))
    yaml.safe_dump_all(documents, sys.stdout, sort_keys=False)
