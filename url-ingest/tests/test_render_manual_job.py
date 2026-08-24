from __future__ import annotations

import argparse
import importlib.util
import sys
import unittest

from pathlib import Path


path = Path(__file__).parents[1] / "k8s" / "render-manual-job.py"
spec = importlib.util.spec_from_file_location("render_manual_job", path)
MODULE = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = MODULE
spec.loader.exec_module(MODULE)


class RenderManualJobTests(unittest.TestCase):
    def test_all_overrides_are_applied_before_pod_creation(self):
        cronjob = {
            "apiVersion": "batch/v1",
            "kind": "CronJob",
            "metadata": {"labels": {"app.kubernetes.io/component": "nvidia"}},
            "spec": {
                "jobTemplate": {
                    "spec": {
                        "activeDeadlineSeconds": 86400,
                        "template": {
                            "metadata": {"labels": {}},
                            "spec": {
                                "initContainers": [{"name": "crawl"}],
                                "containers": [
                                    {
                                        "name": "nv-ingest",
                                        "image": "old:tag",
                                        "env": [{"name": "MILVUS_URI", "value": "old"}],
                                    }
                                ],
                            },
                        },
                    }
                }
            },
        }
        args = argparse.Namespace(
            name="url-ingest-nvidia-run-s00",
            namespace="retrieval",
            collection="nvidia",
            run_id="run-s00",
            shard_count=8,
            shard_index=0,
            verify_only=False,
            run_crawl=False,
            global_ingest_capacity=2,
            client_image="nv-ingest:26.3.0",
            milvus_uri="http://custom-milvus:19530",
            minio_endpoint="custom-minio:9000",
            minio_bucket="documents",
            nv_ingest_host="custom-ingest",
            nv_ingest_port=7671,
            client_timeout=60,
            client_max_retries=120,
            client_retry_delay=5.0,
            verify_timeout=300,
            active_deadline=21600,
            backoff_limit=1,
            ttl_seconds=604800,
        )

        job = MODULE.render_job(cronjob, args)
        pod_spec = job["spec"]["template"]["spec"]
        container = pod_spec["containers"][0]
        env = {item["name"]: item.get("value") for item in container["env"]}
        self.assertNotIn("initContainers", pod_spec)
        self.assertEqual("nv-ingest:26.3.0", container["image"])
        self.assertEqual("http://custom-milvus:19530", env["MILVUS_URI"])
        self.assertEqual("custom-minio:9000", env["MINIO_ENDPOINT"])
        self.assertEqual("custom-ingest", env["NV_INGEST_MESSAGE_HOST"])
        self.assertEqual("8", env["INGEST_SHARD_COUNT"])
        self.assertEqual("2", env["GLOBAL_INGEST_CAPACITY"])
        self.assertIn("run-s00", env["STATUS_REPORT_PATH"])
        self.assertIn("run-s00", env["FAILURES_REPORT_PATH"])
        self.assertEqual(21600, job["spec"]["activeDeadlineSeconds"])


if __name__ == "__main__":
    unittest.main()
