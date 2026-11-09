"""Selection and site overlays must work without unrelated cluster providers."""

import importlib.util
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("capacity", ROOT / "scripts/check_cluster_capacity.py")
capacity = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(capacity)


class SelectedValidationTests(unittest.TestCase):
    def test_tool_only_capacity_never_reads_cluster_nodes_or_pods(self):
        with mock.patch("sys.argv", ["capacity", "--components", "k8s-mcp"]):
            with mock.patch.object(capacity, "kubectl_json") as query:
                self.assertEqual(capacity.main(), 0)
        query.assert_not_called()

    def test_site_capacity_values_override_chart_defaults(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "values.yaml"
            path.write_text("embedder:\n  replicaCount: 3\n")
            with mock.patch.dict(os.environ, {"RETRIEVER_VALUES_FILE": str(path)}):
                values = capacity.chart_values("nemotron-retriever", "RETRIEVER_VALUES_FILE")
        self.assertEqual(values["embedder"]["replicaCount"], 3)
        self.assertIn("model", values["embedder"])

    def test_selected_secret_check_does_not_require_unrelated_credentials(self):
        with tempfile.TemporaryDirectory() as directory:
            fake = Path(directory) / "kubectl"
            log = Path(directory) / "calls"
            fake.write_text('#!/usr/bin/env bash\nprintf "%s\\n" "$*" >> "$TEST_CALLS"\n')
            fake.chmod(0o755)
            env = {**os.environ, "KUBECTL": str(fake), "TEST_CALLS": str(log)}
            subprocess.run(["bash", str(ROOT / "scripts/check-shared-secrets.sh"), "k8s-mcp"],
                           env=env, check=True, capture_output=True)
            self.assertEqual(log.read_text().splitlines(), ["--namespace daedalus get secret k8s-mcp-auth"])

    def run_cluster_chart_validation(self, failure=""):
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            log = directory / "calls"
            client_log = directory / "client-calls"
            kubectl = directory / "kubectl"
            kubectl.write_text(
                '#!/usr/bin/env bash\n'
                'if [[ "$*" == "config current-context" ]]; then echo test-context; '
                'else printf "%s\\n" "$*" >> "$TEST_CLIENT_CALLS"; cat >/dev/null; '
                'if [[ "${TEST_FAILURE:-}" == client ]]; then echo "fixture schema rejected" >&2; exit 8; fi; fi\n'
            )
            kubectl.chmod(0o755)
            helm = directory / "helm"
            helm.write_text(
                '#!/usr/bin/env bash\n'
                'printf "%s\\n" "$*" >> "$TEST_CALLS"\n'
                'echo "apiVersion: v1"\n'
                'if [[ "${TEST_FAILURE:-}" == renderer ]]; then echo "fixture render failed" >&2; exit 7; fi\n'
            )
            helm.chmod(0o755)
            overlay = directory / "site values.yaml"
            overlay.write_text("replicaCount: 1\n")
            result = subprocess.run(
                ["make", "--no-print-directory", "-o", "validate-helm", "-o", "preflight",
                 "validate-cluster", "COMPONENTS=k8s-mcp", "EXPECTED_CONTEXT=test-context",
                 f"KUBECTL={kubectl}", f"HELM={helm}", f"K8S_MCP_VALUES_FILE={overlay}",
                 "HINDSIGHT_ROOT=/does-not-exist"],
                cwd=ROOT, env={**os.environ, "TEST_CALLS": str(log),
                               "TEST_CLIENT_CALLS": str(client_log), "TEST_FAILURE": failure},
                capture_output=True, text=True,
            )
            calls = log.read_text().splitlines()
            self.assertEqual(len(calls), 1)
            self.assertIn("template k8s-mcp-server", calls[0])
            self.assertIn(f"--values {overlay}", calls[0])
            self.assertEqual(client_log.read_text().splitlines(), [
                "--namespace=daedalus create --dry-run=client --validate=strict -f -"
            ])
            return result

    def test_cluster_chart_validation_does_not_apply_to_helm_managed_objects(self):
        result = self.run_cluster_chart_validation()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Selected chart validation passed.", result.stdout)

    def test_cluster_chart_validation_preserves_render_and_schema_failures(self):
        for failure, error in (("renderer", "fixture render failed"),
                               ("client", "fixture schema rejected")):
            with self.subTest(failure=failure):
                result = self.run_cluster_chart_validation(failure)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(error, result.stderr)
                self.assertNotIn("Selected chart validation passed.", result.stdout)

    def test_image_commands_honor_explicit_environment_file(self):
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            config = directory / "custom.env"
            config.write_text("# explicitly selected non-secret test configuration\n")
            log = directory / "calls"
            compose = directory / "compose"
            compose.write_text(
                '#!/usr/bin/env bash\n'
                'printf "%s\n" "$*" >> "$TEST_CALLS"\n'
                'if [[ "$*" == *"config --images" ]]; then echo example.invalid/image:1.0.0; fi\n'
            )
            compose.chmod(0o755)
            docker = directory / "docker"
            docker.write_text("#!/usr/bin/env bash\nexit 0\n")
            docker.chmod(0o755)
            result = subprocess.run(
                ["make", "--no-print-directory", "images-validate", f"COMPOSE={compose}",
                 f"ENV_FILE={config}"], cwd=ROOT,
                env={**os.environ, "TEST_CALLS": str(log), "PATH": f"{directory}:{os.environ['PATH']}"},
                capture_output=True, text=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            calls = log.read_text().splitlines()
            self.assertEqual(calls[0], "version")
            for invocation in calls[1:]:
                self.assertIn(f"--env-file {config}", invocation)

    def test_deploy_uses_each_site_overlay(self):
        for component, variable in (
            ("milvus", "MILVUS"), ("nv-ingest", "NV_INGEST"),
            ("pdf-server", "PDF_SERVER"), ("url-ingest", "URL_INGEST"),
            ("unifi-mcp", "UNIFI_MCP"),
        ):
            with self.subTest(component=component):
                result = subprocess.run(
                    ["make", "--no-print-directory", "-n", f"deploy-{component}",
                     f"{variable}_VALUES_FILE=/tmp/site values.yaml"],
                    cwd=ROOT, capture_output=True, text=True, check=True,
                )
                self.assertIn('--values "/tmp/site values.yaml"', result.stdout)


if __name__ == "__main__":
    unittest.main()
