"""Check the deployed NV-Ingest contract, including its required post-renderer."""
from pathlib import Path
import subprocess
import unittest

import yaml


ROOT = Path(__file__).resolve().parents[1]


class ChartRenderTests(unittest.TestCase):
    def test_workers_preserve_no_surge_and_use_external_credentials(self):
        output = subprocess.check_output([
            "helm", "template", "nv-ingest", str(ROOT / "chart"),
            "--namespace", "daedalus", "--post-renderer", str(ROOT / "helm-post-renderer.sh"),
        ], text=True)
        resources = [item for item in yaml.safe_load_all(output) if item]
        self.assertFalse(any(item["kind"] == "Secret" for item in resources))
        deployment = next(item for item in resources
                          if item["kind"] == "Deployment" and item["metadata"]["name"] == "nv-ingest")
        self.assertEqual(deployment["spec"]["strategy"], {
            "type": "RollingUpdate", "rollingUpdate": {"maxUnavailable": 1, "maxSurge": 0},
        })
        pod = deployment["spec"]["template"]["spec"]
        self.assertIn({"name": "ngc-secret"}, pod["imagePullSecrets"])
        env = pod["containers"][0]["env"]
        secret_names = {item["valueFrom"]["secretKeyRef"]["name"] for item in env
                        if "secretKeyRef" in item.get("valueFrom", {})}
        self.assertIn("ngc-api", secret_names)
        self.assertIn({"secretRef": {"name": "nv-ingest-object-store"}},
                      pod["containers"][0]["envFrom"])

    def test_post_renderer_rejects_missing_worker(self):
        result = subprocess.run([str(ROOT / "helm-post-renderer.sh")],
                                input="apiVersion: v1\nkind: ConfigMap\nmetadata: {name: other}\n",
                                capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)


if __name__ == "__main__":
    unittest.main()
