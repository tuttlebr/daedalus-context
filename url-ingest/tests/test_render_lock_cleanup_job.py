from __future__ import annotations

import argparse
import importlib.util
import sys
import unittest

from pathlib import Path


path = Path(__file__).parents[1] / "k8s" / "render-lock-cleanup-job.py"
spec = importlib.util.spec_from_file_location("render_lock_cleanup_job", path)
MODULE = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = MODULE
spec.loader.exec_module(MODULE)


class RenderLockCleanupJobTests(unittest.TestCase):
    def test_job_inherits_runtime_contract_and_mounts_only_manifests(self):
        cronjob = {
            "apiVersion": "batch/v1",
            "kind": "CronJob",
            "metadata": {"labels": {"app.kubernetes.io/instance": "url-ingest"}},
            "spec": {
                "jobTemplate": {
                    "spec": {
                        "template": {
                            "spec": {
                                "serviceAccountName": "url-ingest",
                                "automountServiceAccountToken": False,
                                "securityContext": {"runAsNonRoot": True},
                                "imagePullSecrets": [{"name": "registry"}],
                                "affinity": {"nodeAffinity": {}},
                                "initContainers": [{"name": "crawl"}],
                                "containers": [
                                    {
                                        "name": "nv-ingest",
                                        "image": "runner:0.1.0",
                                        "imagePullPolicy": "IfNotPresent",
                                        "env": [{"name": "SECRET", "value": "no"}],
                                        "securityContext": {"runAsUser": 0},
                                        "volumeMounts": [
                                            {"name": "manifests", "mountPath": "/manifests"},
                                            {"name": "data", "mountPath": "/data"},
                                        ],
                                    }
                                ],
                                "volumes": [
                                    {
                                        "name": "manifests",
                                        "persistentVolumeClaim": {"claimName": "manifests"},
                                    },
                                    {
                                        "name": "data",
                                        "persistentVolumeClaim": {"claimName": "data"},
                                    },
                                ],
                            }
                        }
                    }
                }
            },
        }
        args = argparse.Namespace(
            name="url-ingest-capacity-lock-cleanup",
            namespace="daedalus",
            active_deadline=300,
            ttl_seconds=600,
        )

        job = MODULE.render_job(cronjob, args)
        pod_spec = job["spec"]["template"]["spec"]
        container = pod_spec["containers"][0]

        self.assertEqual("daedalus", job["metadata"]["namespace"])
        self.assertEqual("runner:0.1.0", container["image"])
        self.assertEqual("url-ingest", pod_spec["serviceAccountName"])
        self.assertFalse(pod_spec["automountServiceAccountToken"])
        self.assertEqual(["manifests"], [item["name"] for item in pod_spec["volumes"]])
        self.assertEqual(
            [{"name": "manifests", "mountPath": "/manifests"}],
            container["volumeMounts"],
        )
        self.assertNotIn("env", container)
        self.assertNotIn("initContainers", pod_spec)
        self.assertIn("global-ingest-capacity-*.lock", container["args"][0])
        self.assertIn('rmdir "${lock}"', container["args"][0])


if __name__ == "__main__":
    unittest.main()
