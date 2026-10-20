#!/usr/bin/env python3
"""Inspect recovery constraints and restart only explicitly owned Helm workloads."""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys


class Kubectl:
    def __init__(self, context: str, namespace: str):
        if not context:
            raise ValueError("EXPECTED_CONTEXT is required")
        executable = os.environ.get("KUBECTL", "kubectl")
        active = subprocess.check_output(
            [executable, "config", "current-context"], text=True
        ).strip()
        if active != context:
            raise ValueError(f"Refusing context {active!r}; expected {context!r}")
        self.command = [executable, f"--context={context}", f"--namespace={namespace}"]

    def run(self, *args):
        return subprocess.check_output([*self.command, *args], text=True)

    def get(self, resource):
        return json.loads(self.run("get", resource, "-o", "json"))


def owned_workloads(items, releases, namespace):
    return [
        item for item in items
        if item["metadata"].get("annotations", {}).get("meta.helm.sh/release-name") in releases
        and item["metadata"].get("annotations", {}).get("meta.helm.sh/release-namespace") == namespace
        and item["metadata"].get("namespace") == namespace
    ]


def controller_uid(item):
    return next((owner["uid"] for owner in item["metadata"].get("ownerReferences", [])
                 if owner.get("controller")), None)


def ready(node):
    return any(condition.get("type") == "Ready" and condition.get("status") == "True"
               for condition in node.get("status", {}).get("conditions", []))


def inspect(client, releases, namespace):
    items = client.get("deployments,statefulsets,replicasets,pods,persistentvolumeclaims")["items"]
    workloads = owned_workloads(
        [item for item in items if item["kind"] in {"Deployment", "StatefulSet"}],
        releases, namespace,
    )
    owners = {item["metadata"]["uid"]: item for item in workloads}
    for item in items:
        if item["kind"] == "ReplicaSet" and controller_uid(item) in owners:
            owners[item["metadata"]["uid"]] = owners[controller_uid(item)]
    nodes = {node["metadata"]["name"]: node for node in client.get("nodes")["items"]}
    claims = {item["metadata"]["name"]: item for item in items
              if item["kind"] == "PersistentVolumeClaim"}
    volumes = {item["metadata"]["name"]: item for item in client.get("pv")["items"]}
    pods = [item for item in items if item["kind"] == "Pod" and controller_uid(item) in owners]
    blockers = []
    warnings = []
    leftovers = []
    storage = {}
    for pod in pods:
        name = pod["metadata"]["name"]
        owner = owners[controller_uid(pod)]
        if pod.get("status", {}).get("phase") in {"Succeeded", "Failed"}:
            leftovers.append(name)
            continue
        node_name = pod["spec"].get("nodeName")
        if node_name and (node_name not in nodes or not ready(nodes[node_name])):
            if owner["kind"] == "StatefulSet":
                blockers.append(f"{name}: node {node_name} is unavailable; StatefulSet replacement "
                                "may require confirmed fencing")
            else:
                warnings.append(f"{name}: node {node_name} is unavailable; check replacement replicas")
        for volume in pod["spec"].get("volumes", []):
            claim_name = volume.get("persistentVolumeClaim", {}).get("claimName")
            if claim_name:
                storage[claim_name] = claims.get(claim_name)
    # Include explicitly named claims even when no pod exists yet.
    for workload in workloads:
        for volume in workload["spec"]["template"]["spec"].get("volumes", []):
            claim_name = volume.get("persistentVolumeClaim", {}).get("claimName")
            if claim_name:
                storage[claim_name] = claims.get(claim_name)
        for template in workload["spec"].get("volumeClaimTemplates", []):
            for ordinal in range(workload["spec"].get("ordinals", {}).get("start", 0),
                                 workload["spec"].get("ordinals", {}).get("start", 0) + workload["spec"].get("replicas", 1)):
                claim_name = f"{template['metadata']['name']}-{workload['metadata']['name']}-{ordinal}"
                storage[claim_name] = claims.get(claim_name)
    storage_report = []
    for name, claim in sorted(storage.items()):
        if not claim or claim.get("status", {}).get("phase") != "Bound":
            blockers.append(f"PVC {name}: missing or not Bound; restart cannot restore data")
            continue
        pv_name = claim["spec"].get("volumeName")
        pv = volumes.get(pv_name)
        if not pv:
            blockers.append(f"PVC {name}: bound PV {pv_name} is missing")
            continue
        spec = pv["spec"]
        storage_report.append({"claim": name, "volume": pv_name,
                               "storageClass": spec.get("storageClassName"),
                               "nodeAffinity": spec.get("nodeAffinity"),
                               "local": bool(spec.get("local") or spec.get("hostPath"))})
        # Report unavailable fixed-host local storage without attempting to
        # rewrite PV affinity, create empty replacement data, or delete claims.
        terms = spec.get("nodeAffinity", {}).get("required", {}).get("nodeSelectorTerms", [])
        host_sets = []
        for term in terms:
            hosts = next((e.get("values", []) for e in term.get("matchExpressions", [])
                          if e.get("key") == "kubernetes.io/hostname" and e.get("operator") == "In"), None)
            host_sets.append(hosts)
        if host_sets and all(hosts is not None for hosts in host_sets):
            eligible = set().union(*map(set, host_sets))
            live_hosts = {n["metadata"].get("labels", {}).get("kubernetes.io/hostname")
                          for n in nodes.values() if ready(n) and not n.get("spec", {}).get("unschedulable")}
            if not eligible & live_hosts:
                blockers.append(f"PVC {name}: pinned to unavailable host(s) {sorted(eligible)}; "
                                "restore the node or perform reviewed data recovery")
    present = {w["metadata"]["annotations"]["meta.helm.sh/release-name"] for w in workloads}
    for release in sorted(set(releases) - present):
        blockers.append(f"Release {release}: no owned Deployment/StatefulSet; use make deploy to reconcile it")
    return workloads, {
        "workloads": [{"kind": w["kind"], "name": w["metadata"]["name"],
                       "desired": w["spec"].get("replicas", 1),
                       "ready": w.get("status", {}).get("readyReplicas", 0)} for w in workloads],
        "blockers": sorted(set(blockers)), "warnings": sorted(set(warnings)),
        "terminalPods": sorted(leftovers), "storage": storage_report,
    }


def restart(client, workloads, report, timeout):
    if report["blockers"]:
        raise ValueError("Recovery blockers found; inspect make recovery-status before restarting")
    ordered = sorted(workloads, key=lambda w: (w["kind"] != "StatefulSet", w["metadata"]["name"]))
    for workload in ordered:
        spec = workload["spec"]
        if spec.get("replicas", 1) == 0:
            continue
        if spec.get("paused"):
            raise ValueError(f"{workload['metadata']['name']} is paused")
        if workload["kind"] == "StatefulSet":
            strategy = spec.get("updateStrategy", {})
            if strategy.get("type", "RollingUpdate") != "RollingUpdate" or strategy.get("rollingUpdate", {}).get("partition", 0):
                raise ValueError(f"{workload['metadata']['name']} does not permit a complete rolling restart")
    for workload in ordered:
        if workload["spec"].get("replicas", 1) == 0:
            continue
        resource = f"{workload['kind'].lower()}/{workload['metadata']['name']}"
        current = client.get(resource)
        if current["metadata"]["uid"] != workload["metadata"]["uid"] or current["metadata"].get("annotations") != workload["metadata"].get("annotations"):
            raise ValueError(f"{resource} changed during inspection; rerun the command")
        before = current["spec"]["template"].get("metadata", {}).get("annotations", {}).get(
            "kubectl.kubernetes.io/restartedAt"
        )
        print(client.run("rollout", "restart", resource), end="", flush=True)
        print(client.run("rollout", "status", resource, f"--timeout={timeout}"), end="", flush=True)
        updated = client.get(resource)
        after = updated["spec"]["template"].get("metadata", {}).get("annotations", {}).get(
            "kubectl.kubernetes.io/restartedAt"
        )
        if updated["metadata"]["uid"] != current["metadata"]["uid"] or not after or after == before:
            raise ValueError(f"{resource}: restart was not observed on the original workload")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("status", "restart"))
    parser.add_argument("--release", action="append", required=True)
    parser.add_argument("--namespace", default="daedalus")
    parser.add_argument("--context", default=os.environ.get("EXPECTED_CONTEXT", ""))
    parser.add_argument("--timeout", default="20m")
    args = parser.parse_args()
    try:
        client = Kubectl(args.context, args.namespace)
        workloads, report = inspect(client, args.release, args.namespace)
        print(json.dumps(report, indent=2), flush=True)
        if args.action == "restart":
            restart(client, workloads, report, args.timeout)
            return 0
        return int(bool(report["blockers"]) or any(w["ready"] < w["desired"] for w in report["workloads"]))
    except (ValueError, subprocess.CalledProcessError) as error:
        print(str(error), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
