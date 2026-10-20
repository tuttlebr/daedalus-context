#!/usr/bin/env python3
"""Add MinIO placement that the pinned upstream chart cannot express."""

import sys

import yaml


def render(documents):
    matches = 0
    for document in documents:
        if not document or document.get("kind") != "StatefulSet":
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
    return documents


if __name__ == "__main__":
    try:
        documents = render(list(yaml.safe_load_all(sys.stdin)))
    except ValueError as error:
        sys.exit(str(error))
    yaml.safe_dump_all(documents, sys.stdout, sort_keys=False)
