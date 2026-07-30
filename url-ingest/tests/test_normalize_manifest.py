from __future__ import annotations

import pathlib
import subprocess
import sys
import unittest

import yaml


SCRIPT = pathlib.Path(__file__).parents[1] / "k8s" / "normalize-manifest.py"


class NormalizeManifestTests(unittest.TestCase):
    def test_adds_common_labels_to_cronjob_and_pod_template(self):
        source = """
apiVersion: batch/v1
kind: CronJob
metadata:
  name: example
spec:
  jobTemplate:
    spec:
      template:
        metadata: {}
        spec:
          restartPolicy: Never
          containers: []
"""
        result = subprocess.run(
            [sys.executable, str(SCRIPT)],
            input=source,
            text=True,
            capture_output=True,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        document = yaml.safe_load(result.stdout)

        for labels in (
            document["metadata"]["labels"],
            document["spec"]["jobTemplate"]["spec"]["template"]["metadata"]["labels"],
        ):
            self.assertEqual(labels["app.kubernetes.io/name"], "url-ingest")
            self.assertEqual(labels["app.kubernetes.io/instance"], "url-ingest")
            self.assertEqual(labels["app.kubernetes.io/version"], "0.1.0")
            self.assertEqual(labels["app.kubernetes.io/component"], "scheduler")
            self.assertEqual(labels["app.kubernetes.io/part-of"], "daedalus-context")
            self.assertEqual(labels["daedalus.ai/tier"], "ingestion")


if __name__ == "__main__":
    unittest.main()
