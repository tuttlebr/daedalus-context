"""Exercise source deployment with executable fake clients, never a registry/cluster."""

import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "scripts/deploy_source.py"
CLIENT = '''#!/usr/bin/env python3
import json, os, pathlib, sys
kind = pathlib.Path(sys.argv[0]).name
args = sys.argv[1:]
event = {"kind": kind, "args": args, "suspend": os.environ.get("URL_INGEST_SUSPEND")}
if kind == "compose":
    for index, arg in enumerate(args[:-1]):
        if arg == "-f" and args[index + 1].endswith("images.json"):
            event["override"] = json.loads(pathlib.Path(args[index + 1]).read_text())
with open(os.environ["TEST_CALLS"], "a") as output:
    output.write(json.dumps(event) + "\\n")
if os.environ.get("FAIL_STAGE") in args:
    sys.exit(9)
'''


class SourceDeployTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.log = self.directory / "calls"
        for name in ("make", "compose"):
            path = self.directory / name
            path.write_text(CLIENT)
            path.chmod(0o755)
        self.env_file = self.directory / "selected.env"
        self.env_file.write_text("URL_INGEST_SUSPEND=false\n")

    def run_deploy(self, components, fail="", image="example.invalid/k8s:1.2.3"):
        self.log.unlink(missing_ok=True)
        result = subprocess.run(
            [sys.executable, str(SCRIPT), "--make", str(self.directory / "make"),
             "--compose", str(self.directory / "compose"), "--env-file", str(self.env_file),
             "--components", *components, "--image", f"K8S_MCP_IMAGE={image}",
             "--image", "URL_INGEST_IMAGE=example.invalid/crawler:1.0.0-rc.2",
             "--image", "URL_INGEST_RUNNER_IMAGE=example.invalid/runner:2.1.5"],
            env={**os.environ, "TEST_CALLS": str(self.log), "FAIL_STAGE": fail,
                 "URL_INGEST_SUSPEND": "false"},
            text=True, capture_output=True, timeout=20,
        )
        events = [json.loads(line) for line in self.log.read_text().splitlines()] if self.log.exists() else []
        return result, events

    def test_selected_sources_publish_before_exact_refs_reach_reconciliation(self):
        result, events = self.run_deploy(["k8s-mcp", "url-ingest"])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual([event["args"][-1] for event in events[:4]],
                         ["doctor", "show-config", "validate-cluster", "verify-shared-secrets"])
        build = next(event for event in events if "build" in event["args"])
        push = next(event for event in events if "push" in event["args"])
        self.assertEqual(build["args"][build["args"].index("build") + 1:],
                         ["--pull", "k8s-mcp-server-read-only", "url-ingest", "nv-ingest-ms-tasks"])
        self.assertEqual(push["override"], build["override"])
        self.assertTrue(all(service["platform"] == "linux/amd64"
                            for service in push["override"]["services"].values()))
        self.assertIn(str(self.env_file), push["args"])
        reconcile = events[-1]
        self.assertIn("deploy-reconcile", reconcile["args"])
        self.assertIn("COMPONENTS=k8s-mcp url-ingest", reconcile["args"])
        for service, variable in (("k8s-mcp-server-read-only", "K8S_MCP_IMAGE"),
                                  ("url-ingest", "URL_INGEST_IMAGE"),
                                  ("nv-ingest-ms-tasks", "URL_INGEST_RUNNER_IMAGE")):
            image = push["override"]["services"][service]["image"]
            self.assertIn("deploy.", image)
            self.assertIn(f"{variable}={image}", reconcile["args"])
        self.assertTrue(all(event["suspend"] == "false" for event in events))
        self.assertEqual(self.env_file.read_text(), "URL_INGEST_SUSPEND=false\n")
        self.assertNotRegex(json.dumps(events), r"uninstall|delete|seed-data|refresh-run|reset")

    def test_gate_build_and_push_failures_stop_before_dependent_side_effects(self):
        for stage in ("doctor", "validate-cluster", "verify-shared-secrets", "build", "push"):
            with self.subTest(stage=stage):
                result, events = self.run_deploy(["k8s-mcp"], fail=stage)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(any("deploy-reconcile" in event["args"] for event in events))
                if stage in ("doctor", "validate-cluster", "verify-shared-secrets"):
                    self.assertFalse(any(event["kind"] == "compose" for event in events))
                if stage == "build":
                    self.assertFalse(any("push" in event["args"] for event in events))

    def test_upstream_and_external_owners_are_reconciled_without_context_builds(self):
        result, events = self.run_deploy(["milvus", "retriever", "hindsight"])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(any(event["kind"] == "compose" for event in events))
        self.assertIn("COMPONENTS=milvus retriever hindsight", events[-1]["args"])

    def test_repeated_deploys_get_new_tags_and_preserve_selected_registry(self):
        images = []
        for _ in range(2):
            result, events = self.run_deploy(["k8s-mcp"], image="localhost:5050/custom/k8s:v1.2.3-rc.1")
            self.assertEqual(result.returncode, 0, result.stderr)
            assignment = next(value for value in events[-1]["args"] if value.startswith("K8S_MCP_IMAGE="))
            self.assertTrue(assignment.startswith("K8S_MCP_IMAGE=localhost:5050/custom/k8s:v1.2.3-rc.1.deploy."))
            images.append(assignment)
        self.assertNotEqual(*images)

    def test_bad_selection_or_image_cannot_publish_or_reconcile(self):
        for components, image in ((["unknown"], "example.invalid/k8s:1.0.0"),
                                  (["k8s-mcp"], "example.invalid/k8s:latest"),
                                  (["k8s-mcp"], "")):
            result, events = self.run_deploy(components, image=image)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(events, [])

    def test_plain_make_loads_env_preserves_cli_choices_and_delivers_fresh_refs_to_helm(self):
        # Run the actual root Makefile, source builder and dependency orchestrator
        # in a disposable checkout. Only read-only gates and external clients are
        # replaced, leaving recursive Make precedence and Helm recipes intact.
        checkout = self.directory / "checkout"
        binaries = self.directory / "clients"
        (checkout / "scripts").mkdir(parents=True)
        (checkout / "stack").mkdir()
        binaries.mkdir()
        for name in ("contextctl", "contextctl.py", "deploy_source.py"):
            shutil.copy2(ROOT / "scripts" / name, checkout / "scripts" / name)
        shutil.copy2(ROOT / "stack/topology.yaml", checkout / "stack/topology.yaml")
        makefile = (ROOT / "Makefile").read_text()
        makefile = re.sub(r"^validate-cluster:.*$", "validate-cluster:", makefile, flags=re.MULTILINE)
        makefile += '\ndoctor show-config validate-cluster verify-shared-secrets health-provider:\n\t@"$(TEST_GATE)" $@\n'
        (checkout / "Makefile").write_text(makefile)
        env = ("EXPECTED_CONTEXT=approved-fixture\nCOMPONENTS=milvus\n"
               "K8S_MCP_IMAGE=example.invalid/from-env:1.0.0\nURL_INGEST_SUSPEND=false\n"
               "IMAGE_PLATFORM=linux/amd64\n")
        (checkout / ".env").write_text(env)
        for name in ("compose", "helm", "gate", "kubectl"):
            client = binaries / name
            content = CLIENT
            if name == "kubectl":
                content += '\nif args == ["config", "current-context"]:\n    print("approved-fixture")\n'
            client.write_text(content)
            client.chmod(0o755)
        result = subprocess.run(
            ["make", "--no-print-directory", "deploy", "COMPONENTS=k8s-mcp",
             "K8S_MCP_IMAGE=localhost:5050/from-cli:v1.2.3-rc.1",
             "IMAGE_PLATFORM=linux/arm64",
             f"SHELL={shutil.which('bash')}", f"COMPOSE={binaries / 'compose'}",
             f"HELM={binaries / 'helm'}", f"KUBECTL={binaries / 'kubectl'}",
             f"TEST_GATE={binaries / 'gate'}"],
            cwd=checkout, env={**os.environ, "TEST_CALLS": str(self.log)},
            text=True, capture_output=True, timeout=20,
        )
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        events = [json.loads(line) for line in self.log.read_text().splitlines()]
        helm = [event for event in events if event["kind"] == "helm"]
        self.assertEqual(len(helm), 1)
        self.assertEqual(helm[0]["args"][:3], ["upgrade", "--install", "k8s-mcp-server"])
        push = next(event for event in events if "push" in event["args"])
        image = push["override"]["services"]["k8s-mcp-server-read-only"]["image"]
        self.assertEqual(push["override"]["services"]["k8s-mcp-server-read-only"]["platform"],
                         "linux/arm64")
        repository, tag = image.rsplit(":", 1)
        self.assertEqual(repository, "localhost:5050/from-cli")
        self.assertTrue(tag.startswith("v1.2.3-rc.1.deploy."))
        self.assertIn(f"image.repository={repository}", helm[0]["args"])
        self.assertIn(f"image.tag={tag}", helm[0]["args"])
        self.assertTrue(all(event["suspend"] == "false" for event in events))
        self.assertEqual((checkout / ".env").read_text(), env)
        self.assertLess(events.index(push), events.index(helm[0]))


if __name__ == "__main__":
    unittest.main()
