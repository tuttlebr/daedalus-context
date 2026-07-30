from __future__ import annotations

import os
import subprocess
import tempfile
import textwrap
import unittest

from pathlib import Path


class JobControlTests(unittest.TestCase):
    def test_failed_job_returns_immediately_with_diagnostics(self):
        library = Path(__file__).parents[1] / "k8s" / "lib" / "job-control.sh"
        with tempfile.TemporaryDirectory() as directory:
            fake_kubectl = Path(directory) / "kubectl"
            fake_kubectl.write_text(
                textwrap.dedent(
                    """\
                    #!/usr/bin/env bash
                    if [[ "$*" == *"get job/failed"* ]]; then
                      printf 'Failed=True BackoffLimitExceeded\\n'
                    fi
                    exit 0
                    """
                )
            )
            fake_kubectl.chmod(0o755)
            command = f"source {library!s}; wait_for_jobs test 30 1 failed"
            env = os.environ.copy()
            env["KUBECTL_BIN"] = str(fake_kubectl)
            result = subprocess.run(
                ["bash", "-euo", "pipefail", "-c", command],
                env=env,
                text=True,
                capture_output=True,
                timeout=5,
                check=False,
            )
        self.assertNotEqual(0, result.returncode)
        self.assertIn("job/failed failed", result.stderr)
        self.assertIn("Diagnostics for job/failed", result.stderr)


if __name__ == "__main__":
    unittest.main()
