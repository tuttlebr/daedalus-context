import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

import yaml


ROOT = Path(__file__).resolve().parents[2]
MAKEFILE = ROOT / "Makefile"
README = ROOT / "README.md"


def make_targets() -> set[str]:
    targets: set[str] = set()
    for line in MAKEFILE.read_text(encoding="utf-8").splitlines():
        match = re.match(r"^([A-Za-z0-9_.-]+):(?:\s|$)", line)
        if match and match.group(1) != ".PHONY":
            targets.add(match.group(1))
    return targets


class RootInterfaceTests(unittest.TestCase):
    def test_local_parser_mounts_are_in_the_rendered_nv_ingest_deployment(self) -> None:
        import shutil

        if not shutil.which("helm"):
            self.skipTest("helm is required for the rendered deployment contract")
        rendered = subprocess.check_output(
            ["helm", "template", "nv-ingest", str(ROOT / "helm/nv-ingest"),
             "--namespace", "daedalus", "--post-renderer", str(ROOT / "nv-ingest/helm-post-renderer.sh")],
            text=True,
        )
        resources = [d for d in yaml.safe_load_all(rendered) if d]
        deployment = next(d for d in resources if d["kind"] == "Deployment" and d["metadata"]["name"] == "nv-ingest")
        mounts = deployment["spec"]["template"]["spec"]["containers"][0]["volumeMounts"]
        for engine in ["pdfium", "nemotron_parse"]:
            mount = next(m for m in mounts if m["mountPath"].endswith(f"/engines/{engine}.py"))
            self.assertEqual(mount["subPath"], "local_pdf.py")
            self.assertTrue(mount["readOnly"])
        self.assertTrue(any(d["kind"] == "Deployment" and d["metadata"]["name"] == "nv-ingest-parser" for d in resources))

    def test_nv_ingest_deploy_disables_external_extraction_dependencies(self) -> None:
        makefile = MAKEFILE.read_text(encoding="utf-8")
        example = (ROOT / ".env.example").read_text(encoding="utf-8")
        required = (
            "OCR_HTTP_ENDPOINT",
            "YOLOX_HTTP_ENDPOINT",
            "YOLOX_GRAPHIC_ELEMENTS_HTTP_ENDPOINT",
            "YOLOX_TABLE_STRUCTURE_HTTP_ENDPOINT",
            "VLM_CAPTION_ENDPOINT",
            "NEMOTRON_PARSE_HTTP_ENDPOINT",
        )
        for variable in required:
            self.assertIn(
                f'nv-ingest.envVars.{variable}="$(LOCAL_DOCUMENT_PARSE_ENDPOINT)/legacy-disabled"',
                makefile,
            )
            self.assertRegex(example, rf"(?m)^{re.escape(variable)}=.*$")

        url_ingest = yaml.safe_load(
            (ROOT / "helm/url-ingest/values.yaml").read_text(encoding="utf-8")
        )
        for collection, values in url_ingest["collections"].items():
            self.assertLessEqual(values["pdfFileChunkSize"], 2, collection)

        cronjobs = (
            ROOT / "helm/url-ingest/templates/cronjobs.yaml"
        ).read_text(encoding="utf-8")
        renderer = (
            ROOT / "url-ingest/k8s/render-manual-job.py"
        ).read_text(encoding="utf-8")
        cleanup_renderer = (
            ROOT / "url-ingest/k8s/render-lock-cleanup-job.py"
        ).read_text(encoding="utf-8")
        deploy = (ROOT / "url-ingest/k8s/deploy.sh").read_text(encoding="utf-8")
        self.assertIn("GLOBAL_INGEST_CAPACITY", cronjobs)
        self.assertIn("/opt/daedalus/refresh.py", cronjobs)
        self.assertIn('"GLOBAL_INGEST_CAPACITY"', renderer)
        self.assertIn(
            '--global-ingest-capacity "$MAX_PARALLEL_INGEST_SHARDS"', deploy
        )
        self.assertIn("rmdir", cleanup_renderer)
        self.assertIn("clear_stale_capacity_locks", deploy)

    def test_all_context_releases_share_daedalus_namespace_and_have_charts(self) -> None:
        topology = yaml.safe_load(
            (ROOT / "stack/topology.yaml").read_text(encoding="utf-8")
        )
        self.assertEqual(
            {release["namespace"] for release in topology["releases"].values()},
            {"daedalus"},
        )
        charts = (
            ROOT / "../daedalus-hindsight/charts/daedalus-hindsight",
            ROOT / "helm/milvus",
            ROOT / "nemotron-retriever",
            ROOT / "helm/nv-ingest",
            ROOT / "helm/pdf-server",
            ROOT / "helm/url-ingest",
            ROOT / "helm/k8s-mcp-server",
            ROOT / "helm/unifi-network-mcp-server",
        )
        for chart in charts:
            self.assertTrue((chart / "Chart.yaml").is_file(), chart)
            self.assertTrue((chart / "values.yaml").is_file(), chart)

    def test_incompatible_workloads_exclude_daedalus_06(self) -> None:
        paths = (
            ROOT / "pdf-server/k8s/pdf-server.yaml",
            ROOT / "k8s-multicluster-mcp/deploy/k8s-mcp-server.yaml",
            ROOT / "unifi-network-mcp/deploy/unifi-network-mcp-server.yaml",
        )
        for path in paths:
            deployments = [
                document
                for document in yaml.safe_load_all(path.read_text(encoding="utf-8"))
                if document and document.get("kind") == "Deployment"
            ]
            self.assertEqual(len(deployments), 1, path)
            affinity = deployments[0]["spec"]["template"]["spec"]["affinity"]
            expressions = affinity["nodeAffinity"][
                "requiredDuringSchedulingIgnoredDuringExecution"
            ]["nodeSelectorTerms"][0]["matchExpressions"]
            self.assertIn(
                {
                    "key": "kubernetes.io/hostname",
                    "operator": "NotIn",
                    "values": ["daedalus-06"],
                },
                expressions,
                path,
            )

        milvus_values = yaml.safe_load(
            (ROOT / "nim-milvus-ms/custom-values.yaml").read_text(encoding="utf-8")
        )
        expected_exclusion = {
            "key": "kubernetes.io/hostname",
            "operator": "NotIn",
            "values": ["daedalus-06"],
        }
        expressions = milvus_values["minio"]["affinity"]["nodeAffinity"][
            "requiredDuringSchedulingIgnoredDuringExecution"
        ]["nodeSelectorTerms"][0]["matchExpressions"]
        self.assertIn(expected_exclusion, expressions)

    def test_multiarch_milvus_tolerates_the_arm64_gpu_worker(self) -> None:
        expected_toleration = {
            "key": "nvidia.com/gpu",
            "operator": "Equal",
            "value": "true",
            "effect": "NoSchedule",
        }
        for path, root_key in (
            (ROOT / "nim-milvus-ms/custom-values.yaml", None),
            (ROOT / "helm/milvus/values.yaml", "milvus"),
        ):
            values = yaml.safe_load(path.read_text(encoding="utf-8"))
            if root_key is not None:
                values = values[root_key]
            self.assertIn(expected_toleration, values["tolerations"], path)
            self.assertNotIn("affinity", values, path)

    def test_every_root_readme_make_command_exists(self) -> None:
        commands = set(
            re.findall(
                r"(?m)^\s*"
                r"(?:[A-Z][A-Z0-9_]*=(?:\"[^\"]*\"|'[^']*'|\S+)\s+)*"
                r"make(?:\s+--no-print-directory)?\s+([a-z][a-z0-9-]+)",
                README.read_text(encoding="utf-8"),
            )
        )
        missing = sorted(commands - make_targets())
        self.assertEqual(missing, [], f"README references missing Make targets: {missing}")

    def test_help_is_grouped_and_hides_internal_targets(self) -> None:
        result = subprocess.run(
            ["make", "--no-print-directory", "help"],
            cwd=ROOT,
            check=True,
            capture_output=True,
            text=True,
        )
        for heading in (
            "Getting started",
            "Image lifecycle",
            "Kubernetes lifecycle",
            "Health and diagnostics",
            "Data operations",
            "Validation and integration",
        ):
            self.assertIn(heading, result.stdout)
        for target in (
            "doctor",
            "images-publish",
            "deploy",
            "health-stack",
            "seed-data",
            "validate-cluster",
        ):
            self.assertRegex(result.stdout, rf"(?m)^  {re.escape(target)}\s")
        for hidden in ("deploy-milvus", "compose-config", "healthcheck-agent"):
            self.assertNotRegex(result.stdout, rf"(?m)^  {re.escape(hidden)}\s")

    def test_doctor_uses_the_invoking_make_when_make_is_overridden(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            temp_root = Path(directory)
            env_file = temp_root / ".env"
            env_file.write_text("# Test configuration.\n", encoding="utf-8")

            kubectl = temp_root / "kubectl"
            kubectl.write_text(
                "#!/usr/bin/env bash\n"
                "set -euo pipefail\n"
                "if [[ \"$*\" == \"config current-context\" ]]; then\n"
                "  printf '%s\\n' test-context\n"
                "fi\n",
                encoding="utf-8",
            )
            kubectl.chmod(0o755)

            docker = temp_root / "docker"
            docker.write_text("#!/usr/bin/env bash\nexit 0\n", encoding="utf-8")
            docker.chmod(0o755)

            environment = os.environ.copy()
            environment.update(
                {
                    "PATH": f"{temp_root}:{environment['PATH']}",
                    "MAKE": "/bin/false",
                    "K8S_MCP_IMAGE": "example.invalid/k8s-mcp:1.0.0",
                    "URL_INGEST_IMAGE": "example.invalid/url-ingest:1.0.0",
                }
            )
            result = subprocess.run(
                [
                    "make",
                    "--no-print-directory",
                    "doctor",
                    f"ENV_FILE={env_file}",
                    f"KUBECTL={kubectl}",
                    "JQ=true",
                    "HELM=true",
                    "COMPOSE=true",
                    "EXPECTED_CONTEXT=test-context",
                ],
                cwd=ROOT,
                env=environment,
                capture_output=True,
                text=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn("Doctor checks passed", result.stdout)

    def test_deploy_loads_root_env_before_running_the_workflow(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            temp_root = Path(directory)
            env_file = temp_root / ".env"
            env_file.write_text(
                "EXPECTED_CONTEXT=test-context\n"
                "K8S_MCP_IMAGE=example.invalid/k8s-mcp:1.0.0\n",
                encoding="utf-8",
            )

            invocation = temp_root / "invocation"
            context = temp_root / "context"
            image = temp_root / "image"
            fake_make = temp_root / "make"
            fake_make.write_text(
                "#!/usr/bin/env bash\n"
                "set -euo pipefail\n"
                "printf '%s\\n' \"$*\" > \"$TEST_INVOCATION\"\n"
                "printf '%s\\n' \"${EXPECTED_CONTEXT:-}\" > \"$TEST_CONTEXT\"\n"
                "printf '%s\\n' \"${K8S_MCP_IMAGE:-}\" > \"$TEST_IMAGE\"\n",
                encoding="utf-8",
            )
            fake_make.chmod(0o755)

            environment = os.environ.copy()
            environment.update(
                {
                    "TEST_INVOCATION": str(invocation),
                    "TEST_CONTEXT": str(context),
                    "TEST_IMAGE": str(image),
                }
            )
            result = subprocess.run(
                [
                    "make",
                    "--no-print-directory",
                    "deploy",
                    f"ENV_FILE={env_file}",
                    f"MAKE_COMMAND={fake_make}",
                ],
                cwd=ROOT,
                env=environment,
                capture_output=True,
                text=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(
                invocation.read_text(encoding="utf-8").strip(),
                "--no-print-directory deploy-workflow",
            )
            self.assertEqual(context.read_text(encoding="utf-8").strip(), "test-context")
            self.assertEqual(
                image.read_text(encoding="utf-8").strip(),
                "example.invalid/k8s-mcp:1.0.0",
            )

    def test_compose_runtime_targets_are_removed(self) -> None:
        removed = {"compose-up", "compose-down", "compose-crawl", "compose-ingest"}
        self.assertTrue(removed.isdisjoint(make_targets()))

        tracked = subprocess.run(
            ["git", "ls-files", "*.md"],
            cwd=ROOT,
            check=True,
            capture_output=True,
            text=True,
        ).stdout.splitlines()
        markdown_paths = [ROOT / path for path in tracked]
        markdown_paths.extend(
            [
                ROOT / "AGENTS.md",
                ROOT / ".agents/skills/operate-daedalus-context/SKILL.md",
            ]
        )
        markdown = "\n".join(
            path.read_text(encoding="utf-8") for path in markdown_paths
        )
        for target in removed:
            self.assertNotIn(target, markdown)

    def test_legacy_cleanup_wipes_exact_path_before_deleting_pv(self) -> None:
        cleanup = (ROOT / "scripts/cleanup-legacy-stack.sh").read_text(
            encoding="utf-8"
        )
        wait_for_cleaner = 'wait pod/"$cleaner"'
        delete_pv = 'delete pv "$pv"'
        self.assertIn('add: ["DAC_OVERRIDE", "FOWNER"]', cleanup)
        self.assertLess(cleanup.index(wait_for_cleaner), cleanup.index(delete_pv))

    def test_image_compatibility_aliases_are_explicit(self) -> None:
        source = MAKEFILE.read_text(encoding="utf-8")
        aliases = {
            "compose-config": "images-validate",
            "compose-build": "images-build",
            "compose-push": "images-push",
            "compose-release-refs": "images-release-refs",
            "compose-publish": "images-publish",
        }
        for old, new in aliases.items():
            self.assertRegex(source, rf"(?m)^{old}: {new}$")
        self.assertNotIn("stage-compose-kubeconfig", source)

    def test_context_guard_fails_closed(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            kubectl = Path(directory) / "kubectl"
            kubectl.write_text(
                "#!/usr/bin/env bash\n"
                "set -euo pipefail\n"
                "if [[ \"$*\" == \"config current-context\" ]]; then\n"
                "  printf '%s\\n' \"${FAKE_CURRENT_CONTEXT:-test-context}\"\n"
                "  exit 0\n"
                "fi\n"
                "exit 0\n",
                encoding="utf-8",
            )
            kubectl.chmod(0o755)
            environment = os.environ.copy()
            environment["FAKE_CURRENT_CONTEXT"] = "actual-context"

            missing = subprocess.run(
                [
                    "make",
                    "--no-print-directory",
                    "require-context",
                    f"KUBECTL={kubectl}",
                    "EXPECTED_CONTEXT=",
                ],
                cwd=ROOT,
                env=environment,
                capture_output=True,
                text=True,
            )
            self.assertNotEqual(missing.returncode, 0)
            self.assertIn("EXPECTED_CONTEXT is required", missing.stderr)

            for target in ("health-stack", "deploy-retriever"):
                guarded = subprocess.run(
                    [
                        "make",
                        "--no-print-directory",
                        target,
                        f"KUBECTL={kubectl}",
                        "EXPECTED_CONTEXT=",
                    ],
                    cwd=ROOT,
                    env=environment,
                    capture_output=True,
                    text=True,
                )
                self.assertNotEqual(guarded.returncode, 0)
                self.assertIn("EXPECTED_CONTEXT is required", guarded.stderr)

            mismatch = subprocess.run(
                [
                    "make",
                    "--no-print-directory",
                    "require-context",
                    f"KUBECTL={kubectl}",
                    "EXPECTED_CONTEXT=wrong-context",
                ],
                cwd=ROOT,
                env=environment,
                capture_output=True,
                text=True,
            )
            self.assertNotEqual(mismatch.returncode, 0)
            self.assertIn("Refusing context 'actual-context'", mismatch.stderr)

            matched = subprocess.run(
                [
                    "make",
                    "--no-print-directory",
                    "require-context",
                    f"KUBECTL={kubectl}",
                    "EXPECTED_CONTEXT=actual-context",
                ],
                cwd=ROOT,
                env=environment,
                capture_output=True,
                text=True,
            )
            self.assertEqual(matched.returncode, 0, matched.stderr)

    def test_skill_has_no_scaffold_placeholders(self) -> None:
        skill = ROOT / ".agents/skills/operate-daedalus-context/SKILL.md"
        metadata = ROOT / ".agents/skills/operate-daedalus-context/agents/openai.yaml"
        self.assertTrue(skill.is_file())
        self.assertTrue(metadata.is_file())
        self.assertNotIn("TODO", skill.read_text(encoding="utf-8"))
        self.assertIn(
            "allow_implicit_invocation: true", metadata.read_text(encoding="utf-8")
        )


if __name__ == "__main__":
    unittest.main()
