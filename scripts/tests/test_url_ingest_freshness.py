import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parents[1] / "check_url_ingest_freshness.sh"

class UrlIngestFreshnessTests(unittest.TestCase):
    def check(self, healthy, suspended=False):
        with tempfile.TemporaryDirectory() as tmp:
            cli = Path(tmp) / "kubectl"
            cli.write_text("#!/usr/bin/env python3\nimport os,sys\nprint(os.environ['SUSPENDED'] if 'get' in sys.argv else os.environ['SNAPSHOT'])\n")
            cli.chmod(0o755)
            return subprocess.run(["bash", str(SCRIPT)], capture_output=True, text=True, env={**os.environ,
                "KUBECTL": str(cli), "NAMESPACE": "daedalus", "COLLECTIONS": "nvidia",
                "SUSPENDED": str(suspended).lower(), "SNAPSHOT": json.dumps({"collections": {"nvidia": {"healthy": healthy}}})})

    def test_durable_healthy_report_passes(self):
        self.assertEqual(self.check(True).returncode, 0)

    def test_stale_or_failed_report_fails(self):
        self.assertEqual(self.check(False).returncode, 1)

    def test_suspension_fails_even_with_recent_success(self):
        result = self.check(True, True)
        self.assertEqual(result.returncode, 1)
        self.assertIn("suspended", result.stderr)
