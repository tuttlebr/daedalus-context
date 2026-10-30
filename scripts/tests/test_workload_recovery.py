import copy
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

import yaml


ROOT = Path(__file__).resolve().parents[2]


def module(path):
    spec = importlib.util.spec_from_file_location(path.stem, path)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


recovery = module(ROOT / "scripts/workload_recovery.py")
renderer = module(ROOT / "nim-milvus-ms/chart/post-render.py")


def workload(name, kind="Deployment", release="milvus"):
    return {
        "kind": kind,
        "metadata": {
            "name": name, "namespace": "daedalus", "uid": name + "-uid",
            "annotations": {"meta.helm.sh/release-name": release,
                            "meta.helm.sh/release-namespace": "daedalus"},
        },
        "spec": {"replicas": 1, "template": {"spec": {}}},
        "status": {"readyReplicas": 1},
    }


class FakeClient:
    def __init__(self, items):
        self.items = items
        self.writes = []
        self.nodes = [{"metadata": {"name": "failed", "labels": {"kubernetes.io/hostname": "failed"}},
                       "status": {"conditions": [{"type": "Ready", "status": "Unknown"}]}}]
        self.volumes = []

    def get(self, resource):
        if resource == "nodes":
            return {"items": self.nodes}
        if resource == "pv":
            return {"items": self.volumes}
        if "," in resource:
            return {"items": self.items}
        return next(i for i in self.items if f"{i['kind'].lower()}/{i['metadata']['name']}" == resource)

    def run(self, *args):
        self.writes.append(args)
        if args[:2] == ("rollout", "restart"):
            self.get(args[2])["spec"]["template"].setdefault("metadata", {}).setdefault("annotations", {})[
                "kubectl.kubernetes.io/restartedAt"] = str(len(self.writes))
        return ""


class RecoveryTests(unittest.TestCase):
    def test_make_restart_loads_env_restarts_every_owned_service_and_skips_jobs(self):
        items = [workload(name, kind, release) for name, kind, release in (
            ("hindsight-api", "Deployment", "hindsight"),
            ("hindsight-postgresql", "StatefulSet", "hindsight"),
            ("milvus-minio", "StatefulSet", "milvus"),
            ("milvus-proxy", "Deployment", "milvus"),
            ("retriever-embedder", "Deployment", "nemotron-retriever"),
            ("nv-ingest", "Deployment", "nv-ingest"),
            ("nv-ingest-redis-master", "StatefulSet", "nv-ingest"),
            ("pdf-server", "Deployment", "pdf-server"),
            ("k8s-mcp-server", "Deployment", "k8s-mcp-server"),
            ("unifi-network-mcp-server", "Deployment", "unifi-network-mcp-server"),
            ("agent", "Deployment", "daedalus"),
        )]
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)
            fixture = path / "items.json"
            fixture.write_text(json.dumps(items))
            env_file = path / ".env"
            env_file.write_text("EXPECTED_CONTEXT=test-context\nRECOVERY_TEST_LOADED=yes\n")
            log = path / "calls.jsonl"
            kubectl = path / "kubectl"
            kubectl.write_text('''#!/usr/bin/env python3
import json, os, sys
from pathlib import Path
assert os.environ.get("RECOVERY_TEST_LOADED") == "yes", "root .env was not loaded"
args = [a for a in sys.argv[1:] if not a.startswith(("--context=", "--namespace="))]
with Path(os.environ["RECOVERY_TEST_LOG"]).open("a") as stream:
    stream.write(json.dumps(args) + "\\n")
items = json.loads(Path(os.environ["RECOVERY_TEST_ITEMS"]).read_text())
if args == ["config", "current-context"]:
    print("test-context")
elif args[0] == "get":
    if args[1] in ("nodes", "pv"):
        print('{"items": []}')
    elif "," in args[1]:
        print(json.dumps({"items": items}))
    else:
        print(json.dumps(next(i for i in items if i["kind"].lower() + "/" + i["metadata"]["name"] == args[1])))
elif args[:2] == ["rollout", "restart"]:
    item = next(i for i in items if i["kind"].lower() + "/" + i["metadata"]["name"] == args[2])
    item["spec"]["template"]["metadata"] = {"annotations": {"kubectl.kubernetes.io/restartedAt": "new-time"}}
    Path(os.environ["RECOVERY_TEST_ITEMS"]).write_text(json.dumps(items))
elif args[0] not in ("cluster-info", "rollout"):
    sys.exit("Unexpected command: " + str(args))
''')
            kubectl.chmod(0o755)
            environment = {**os.environ, "RECOVERY_TEST_LOG": str(log), "RECOVERY_TEST_ITEMS": str(fixture)}
            environment.pop("MAKEFLAGS", None)
            result = subprocess.run(
                ["make", "--no-print-directory", "restart", f"ENV_FILE={env_file}", f"KUBECTL={kubectl}",
                 "COMPONENTS=hindsight milvus retriever nv-ingest pdf-server url-ingest k8s-mcp unifi-mcp"],
                cwd=ROOT, env=environment, text=True, capture_output=True,
            )
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            calls = [json.loads(line) for line in log.read_text().splitlines()]
            restarted = [call[2] for call in calls if call[:2] == ["rollout", "restart"]]
            self.assertEqual(set(restarted), {i["kind"].lower() + "/" + i["metadata"]["name"] for i in items[:-1]})
            self.assertEqual(len(restarted), len(items) - 1)
            self.assertFalse(any(call[0] in {"delete", "apply", "patch", "create"} for call in calls))
            self.assertIn("CronJobs have no persistent pods", result.stdout)

    def test_ownership_includes_legacy_minio_but_excludes_other_apps(self):
        minio = workload("milvus-minio", "StatefulSet")
        minio["metadata"]["labels"] = {"app": "minio", "release": "milvus"}
        agent = workload("daedalus-backend", release="daedalus")
        forged = workload("forged")
        forged["metadata"]["annotations"]["meta.helm.sh/release-namespace"] = "another"
        items = [minio, agent, forged]
        self.assertEqual(recovery.owned_workloads(items, ["milvus"], "daedalus"), [minio])

    def test_restart_waits_for_stateful_dependencies_and_never_touches_storage(self):
        deployment = workload("milvus-proxy")
        stateful = workload("milvus-minio", "StatefulSet")
        client = FakeClient([deployment, stateful])
        workloads, report = recovery.inspect(client, ["milvus"], "daedalus")
        recovery.restart(client, workloads, report, "2m")
        self.assertEqual(client.writes, [
            ("rollout", "restart", "statefulset/milvus-minio"),
            ("rollout", "status", "statefulset/milvus-minio", "--timeout=2m"),
            ("rollout", "restart", "deployment/milvus-proxy"),
            ("rollout", "status", "deployment/milvus-proxy", "--timeout=2m"),
        ])

    def test_lost_node_blocks_stateful_restart_before_any_mutation(self):
        stateful = workload("minio", "StatefulSet")
        pod = {"kind": "Pod", "metadata": {"name": "minio-0", "ownerReferences": [
            {"uid": "minio-uid", "controller": True}]},
            "spec": {"nodeName": "failed"}, "status": {"phase": "Running"}}
        client = FakeClient([stateful, pod])
        workloads, report = recovery.inspect(client, ["milvus"], "daedalus")
        self.assertIn("fencing", report["blockers"][0])
        with self.assertRaises(ValueError):
            recovery.restart(client, workloads, report, "2m")
        self.assertEqual(client.writes, [])

    def test_local_pvc_on_failed_node_is_reported_even_for_pending_pod(self):
        stateful = workload("etcd", "StatefulSet")
        pod = {"kind": "Pod", "metadata": {"name": "etcd-2", "ownerReferences": [
            {"uid": "etcd-uid", "controller": True}]},
            "spec": {"volumes": [{"persistentVolumeClaim": {"claimName": "etcd-data"}}]},
            "status": {"phase": "Pending"}}
        claim = {"kind": "PersistentVolumeClaim", "metadata": {"name": "etcd-data"},
                 "spec": {"volumeName": "local-pv"}, "status": {"phase": "Bound"}}
        client = FakeClient([stateful, pod, claim])
        client.volumes = [{"metadata": {"name": "local-pv"}, "spec": {
            "local": {"path": "/data"}, "nodeAffinity": {"required": {"nodeSelectorTerms": [
                {"matchExpressions": [{"key": "kubernetes.io/hostname", "operator": "In", "values": ["failed"]}]}]}}}}]
        _, report = recovery.inspect(client, ["milvus"], "daedalus")
        self.assertIn("pinned to unavailable", report["blockers"][0])
        self.assertTrue(report["storage"][0]["local"])

    def test_missing_release_is_not_silently_successful(self):
        _, report = recovery.inspect(FakeClient([]), ["hindsight"], "daedalus")
        self.assertIn("no owned Deployment/StatefulSet", report["blockers"][0])

    def test_successful_command_without_observed_restart_is_rejected(self):
        items = [workload("api")]
        client = FakeClient(items)
        client.run = lambda *args: ""
        with self.assertRaisesRegex(ValueError, "restart was not observed"):
            recovery.restart(client, items, {"blockers": []}, "2m")

    def test_ondelete_and_paused_workloads_fail_before_first_restart(self):
        for spec in ({"paused": True}, {"updateStrategy": {"type": "OnDelete"}},
                     {"updateStrategy": {"rollingUpdate": {"partition": 1}}}):
            with self.subTest(spec=spec):
                items = [workload("first"), workload("last", "StatefulSet")]
                items[-1]["spec"].update(spec)
                client = FakeClient(items)
                with self.assertRaises(ValueError):
                    recovery.restart(client, items, {"blockers": []}, "2m")
                self.assertEqual(client.writes, [])

    def test_recreated_workload_is_not_restarted_using_stale_identity(self):
        original = workload("api")
        replacement = copy.deepcopy(original)
        replacement["metadata"]["uid"] = "different-uid"
        client = FakeClient([replacement])
        with self.assertRaises(ValueError):
            recovery.restart(client, [original], {"blockers": []}, "2m")
        self.assertEqual(client.writes, [])


class PlacementTests(unittest.TestCase):
    def test_minio_render_changes_only_placement_and_bucket_policy_command(self):
        rendered = subprocess.check_output(
            ["helm", "template", "custom-milvus", "nim-milvus-ms/chart"], cwd=ROOT, text=True)
        original = list(yaml.safe_load_all(rendered))
        updated = renderer.render(copy.deepcopy(original))
        changed = 0
        for before, after in zip(original, updated):
            if before == after:
                continue
            changed += 1
            self.assertEqual(after["metadata"]["name"], "custom-milvus-minio")
            if after["kind"] == "ConfigMap":
                script = after["data"]["initialize"]
                self.assertIn("${MC} anonymous set $POLICY myminio/$BUCKET", script)
                self.assertNotIn("${MC} policy set", script)
                after["data"]["initialize"] = script.replace("${MC} anonymous set", "${MC} policy set")
            else:
                self.assertEqual(after["kind"], "StatefulSet")
                spread = after["spec"]["template"]["spec"].pop("topologySpreadConstraints")
                self.assertEqual(spread[0]["nodeTaintsPolicy"], "Honor")
                self.assertEqual(spread[0]["labelSelector"]["matchLabels"]["release"], "custom-milvus")
            self.assertEqual(after, before)
        self.assertEqual(changed, 2)

    def test_bucket_hook_uses_mirrored_image_with_bundled_client(self):
        rendered = subprocess.check_output(
            ["helm", "template", "milvus", "nim-milvus-ms/chart", "--post-renderer",
             str(ROOT / "nim-milvus-ms/chart/post-render.py")], cwd=ROOT, text=True)
        documents = list(yaml.safe_load_all(rendered))
        server = next(d for d in documents if d and d["kind"] == "StatefulSet"
                      and d["metadata"]["name"] == "milvus-minio")
        hook = next(d for d in documents if d and d["kind"] == "Job"
                    and d["metadata"]["name"] == "milvus-minio-make-bucket-job")
        container = hook["spec"]["template"]["spec"]["containers"][0]
        self.assertEqual(container["image"], server["spec"]["template"]["spec"]["containers"][0]["image"])
        self.assertTrue(container["image"].startswith("daedalus.ddns.me:5050/btuttle/minio:"))
        self.assertIn("@sha256:", container["image"])
        self.assertEqual(container["command"], ["/bin/sh", "/config/initialize"])
        self.assertEqual(renderer.render(copy.deepcopy(documents)), documents)
        initializer = next(d for d in documents if d and d["kind"] == "ConfigMap"
                           and d["metadata"]["name"] == "milvus-minio")
        initializer["data"]["initialize"] = "unexpected upstream script"
        with self.assertRaisesRegex(ValueError, "initializer changed"):
            renderer.render(documents)

    def test_stateless_spread_excludes_untolerated_failed_nodes(self):
        for chart in ("milvus", "pdf-server", "k8s-mcp-server", "unifi-network-mcp-server"):
            output = subprocess.check_output(["helm", "template", chart, {"milvus": "nim-milvus-ms/chart", "pdf-server": "pdf-server/chart",
                 "k8s-mcp-server": "k8s-multicluster-mcp/chart",
                 "unifi-network-mcp-server": "unifi-network-mcp/chart"}[chart]], cwd=ROOT, text=True)
            for item in yaml.safe_load_all(output):
                if item and item.get("kind") == "Deployment":
                    for constraint in item["spec"]["template"]["spec"].get("topologySpreadConstraints", []):
                        self.assertEqual(constraint["nodeTaintsPolicy"], "Honor")

    def test_renderer_fails_closed_if_minio_disappears(self):
        with self.assertRaises(ValueError):
            renderer.render([])


if __name__ == "__main__":
    unittest.main()
