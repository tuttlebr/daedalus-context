#!/usr/bin/env python3
"""Read-only capacity preflight for hard placement constraints."""

from __future__ import annotations

import json
import os
import subprocess
from pathlib import Path

import yaml


ROOT = Path(__file__).resolve().parents[1]
KUBECTL = os.environ.get("KUBECTL", "kubectl")


def kubectl_json(*arguments: str) -> dict:
    result = subprocess.run(
        [KUBECTL, *arguments, "-o", "json"],
        check=True,
        capture_output=True,
        text=True,
    )
    return json.loads(result.stdout)


def gpu_request(container: dict) -> int:
    value = (
        container.get("resources", {})
        .get("requests", {})
        .get("nvidia.com/gpu", 0)
    )
    return int(value)


nodes = kubectl_json("get", "nodes")["items"]
ready_nodes = {
    node["metadata"]["name"]: int(
        node.get("status", {}).get("allocatable", {}).get("nvidia.com/gpu", 0)
    )
    for node in nodes
    if not node.get("spec", {}).get("unschedulable", False)
    and any(
        condition.get("type") == "Ready" and condition.get("status") == "True"
        for condition in node.get("status", {}).get("conditions", [])
    )
}

milvus = yaml.safe_load(
    (ROOT / "nim-milvus-ms/chart/values.yaml").read_text(encoding="utf-8")
)["milvus"]
required_general_domains = max(
    milvus[role]["replicas"]
    for role in ("proxy", "queryNode", "dataNode", "streamingNode", "mixCoordinator")
)
if len(ready_nodes) < required_general_domains:
    raise SystemExit(
        f"hard Milvus placement needs {required_general_domains} Ready schedulable nodes; "
        f"found {len(ready_nodes)}"
    )

retriever = yaml.safe_load(
    (ROOT / "nemotron-retriever" / "chart" / "values.yaml").read_text(encoding="utf-8")
)
topology = yaml.safe_load(
    (ROOT / "stack" / "topology.yaml").read_text(encoding="utf-8")
)
retriever_namespace = topology["releases"]["retriever"]["namespace"]
embedder_replicas = retriever["embedder"]["replicaCount"]
gpu_nodes = {name for name, count in ready_nodes.items() if count > 0}
if len(gpu_nodes) < embedder_replicas:
    raise SystemExit(
        f"embedder hard spread needs {embedder_replicas} GPU nodes; found {len(gpu_nodes)}"
    )

pods = kubectl_json("get", "pods", "--all-namespaces")["items"]
used_by_node = {name: 0 for name in ready_nodes}
live_embedder_nodes: set[str] = set()
live_embedders = 0
for pod in pods:
    if pod.get("status", {}).get("phase") in {"Succeeded", "Failed"}:
        continue
    node = pod.get("spec", {}).get("nodeName")
    if node not in used_by_node:
        continue
    containers = pod.get("spec", {}).get("containers", [])
    init_containers = pod.get("spec", {}).get("initContainers", [])
    requested = max(
        sum(gpu_request(container) for container in containers),
        max((gpu_request(container) for container in init_containers), default=0),
    )
    used_by_node[node] += requested
    labels = pod.get("metadata", {}).get("labels", {})
    if (
        pod.get("metadata", {}).get("namespace") == retriever_namespace
        and labels.get("app.kubernetes.io/component") == "embedder"
    ):
        live_embedders += 1
        live_embedder_nodes.add(node)

additional_embedders = max(0, embedder_replicas - live_embedders)
free_by_node = {
    name: ready_nodes[name] - used_by_node[name]
    for name in gpu_nodes
}
eligible_new_nodes = [
    name
    for name, free in free_by_node.items()
    if free > 0 and name not in live_embedder_nodes
]
if len(eligible_new_nodes) < additional_embedders:
    raise SystemExit(
        f"embedder rollout needs {additional_embedders} additional distinct GPU nodes; "
        f"eligible={eligible_new_nodes or 'none'}"
    )

print(
    json.dumps(
        {
            "readyNodes": len(ready_nodes),
            "gpuNodes": len(gpu_nodes),
            "liveEmbedders": live_embedders,
            "targetEmbedders": embedder_replicas,
            "eligibleNewEmbedderNodes": eligible_new_nodes,
        },
        sort_keys=True,
    )
)
