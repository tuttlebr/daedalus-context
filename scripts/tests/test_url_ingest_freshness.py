from __future__ import annotations

import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).resolve().parents[1] / "check_url_ingest_freshness.sh"


class UrlIngestFreshnessTests(unittest.TestCase):
    def run_check(
        self, cronjob: dict[str, object], jobs: dict[str, object]
    ) -> subprocess.CompletedProcess[str]:
        with tempfile.TemporaryDirectory() as directory:
            fake_kubectl = Path(directory) / "kubectl"
            fake_kubectl.write_text(
                """#!/usr/bin/env python3
import os
import sys

resource = sys.argv[sys.argv.index("get") + 1]
key = "FAKE_CRONJOB_JSON" if resource == "cronjob" else "FAKE_JOBS_JSON"
print(os.environ[key])
""",
                encoding="utf-8",
            )
            fake_kubectl.chmod(0o755)
            return subprocess.run(
                ["bash", str(SCRIPT)],
                check=False,
                text=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                env={
                    **os.environ,
                    "KUBECTL": str(fake_kubectl),
                    "NAMESPACE": "milvus",
                    "COLLECTIONS": "nvidia",
                    "FAKE_CRONJOB_JSON": json.dumps(cronjob),
                    "FAKE_JOBS_JSON": json.dumps(jobs),
                },
            )

    def test_successful_manual_job_satisfies_never_scheduled_cronjob(self):
        result = self.run_check(
            {"spec": {"suspend": False}, "status": {}},
            {
                "items": [
                    {
                        "metadata": {
                            "name": "url-ingest-nvidia-manual-verify",
                            "creationTimestamp": "2026-07-30T03:18:52Z",
                        },
                        "status": {
                            "completionTime": "2026-07-30T03:18:58Z",
                            "conditions": [
                                {"type": "Complete", "status": "True"}
                            ],
                        },
                    }
                ]
            },
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("schedule=none success=2026-07-30T03:18:58Z", result.stdout)

    def test_never_scheduled_cronjob_without_jobs_still_fails(self):
        result = self.run_check(
            {"spec": {"suspend": False}, "status": {}},
            {"items": []},
        )

        self.assertEqual(result.returncode, 1)
        self.assertIn("nvidia=never-scheduled", result.stderr)

    def test_latest_failed_manual_job_still_fails(self):
        result = self.run_check(
            {"spec": {"suspend": False}, "status": {}},
            {
                "items": [
                    {
                        "metadata": {
                            "name": "url-ingest-nvidia-failed",
                            "creationTimestamp": "2026-07-30T04:00:00Z",
                        },
                        "status": {
                            "completionTime": "2026-07-30T04:01:00Z",
                            "conditions": [{"type": "Failed", "status": "True"}],
                        },
                    }
                ]
            },
        )

        self.assertEqual(result.returncode, 1)
        self.assertIn(
            "nvidia=failed:url-ingest-nvidia-failed",
            result.stderr,
        )


if __name__ == "__main__":
    unittest.main()
