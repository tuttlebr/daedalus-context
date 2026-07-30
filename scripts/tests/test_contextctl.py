from __future__ import annotations

import contextlib
import importlib.util
import io
import json
import subprocess
import unittest
from pathlib import Path
from unittest import mock


MODULE_PATH = Path(__file__).resolve().parents[1] / "contextctl.py"
SPEC = importlib.util.spec_from_file_location("contextctl_module", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
contextctl = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(contextctl)


class HealthAggregationTests(unittest.TestCase):
    def run_health_with(self, component: str, failing_target: str) -> tuple[int, dict]:
        def fake_run(command, **_kwargs):
            target = command[-1]
            if target == failing_target:
                return subprocess.CompletedProcess(
                    command,
                    1,
                    stdout="semantic provider failure\nmake[1]: *** [Makefile:1: target] Error 1\n",
                )
            return subprocess.CompletedProcess(command, 0, stdout="healthy\n")

        output = io.StringIO()
        with mock.patch.object(contextctl.subprocess, "run", side_effect=fake_run):
            with contextlib.redirect_stdout(output):
                status = contextctl.run_health("development", [component])
        return status, json.loads(output.getvalue())

    def test_required_failure_is_unavailable_and_strips_make_wrapper_noise(self):
        status, report = self.run_health_with("milvus", "health-provider-milvus")

        self.assertEqual(status, 1)
        self.assertEqual(report["availability"], "unavailable")
        self.assertEqual(report["failed"][0]["detail"], "semantic provider failure")

    def test_optional_tool_failure_is_degraded(self):
        status, report = self.run_health_with("k8s-mcp", "health-dependencies-k8s-mcp")

        self.assertEqual(status, 2)
        self.assertEqual(report["availability"], "degraded")
        self.assertEqual(report["failed"][0]["component"], "k8s-mcp")

    def test_healthy_personal_production_does_not_require_redundancy(self):
        def fake_run(command, **_kwargs):
            return subprocess.CompletedProcess(command, 0, stdout="healthy\n")

        output = io.StringIO()
        with mock.patch.object(contextctl.subprocess, "run", side_effect=fake_run):
            with contextlib.redirect_stdout(output):
                status = contextctl.run_health("production", ["milvus"])

        report = json.loads(output.getvalue())
        self.assertEqual(status, 0)
        self.assertEqual(report["availability"], "healthy")
        self.assertEqual(report["redundancy"], "not-required")
        self.assertEqual(report["operatingMode"], "personal-single-user")
        self.assertEqual(report["blockedBy"], [])


class AcceptancePolicyTests(unittest.TestCase):
    def test_non_ha_waiver_requires_the_full_approved_policy(self):
        acceptance = {
            "status": "approved",
            "operatingMode": "personal-single-user",
            "haRequired": False,
            "haPhaseBlocked": False,
            "acceptedRisks": [],
        }

        self.assertFalse(contextctl.is_approved_personal_non_ha(acceptance))

    def test_personal_non_ha_production_rejects_ha_qualification(self):
        with self.assertRaisesRegex(
            ValueError,
            "HA validation is disabled by the approved personal-single-user policy",
        ):
            contextctl.validate_contracts("production", require_ha=True)


class OperationGraphTests(unittest.TestCase):
    def test_failed_prerequisite_blocks_only_its_dependents(self):
        calls: list[str] = []

        def fake_run(command, **_kwargs):
            target = command[-1]
            calls.append(target)
            status = 1 if target == "deploy-milvus" else 0
            return subprocess.CompletedProcess(command, status, stdout=f"{target}\n")

        output = io.StringIO()
        with mock.patch.object(contextctl.subprocess, "run", side_effect=fake_run):
            with contextlib.redirect_stdout(output):
                status = contextctl.run_operation(
                    "deploy",
                    [
                        "milvus",
                        "retriever",
                        "nv-ingest",
                        "pdf-server",
                        "url-ingest",
                        "k8s-mcp",
                        "unifi-mcp",
                    ],
                )

        report = json.loads(output.getvalue()[output.getvalue().index('{\n  "action"') :])
        self.assertEqual(status, 1)
        self.assertNotIn("deploy-nv-ingest", calls)
        self.assertNotIn("deploy-url-ingest", calls)
        for independent in (
            "deploy-retriever",
            "deploy-pdf-server",
            "deploy-k8s-mcp",
            "deploy-unifi-mcp",
        ):
            self.assertIn(independent, calls)
        self.assertEqual(report["components"]["milvus"]["status"], "failed")
        self.assertEqual(report["components"]["nv-ingest"]["blockedBy"], ["milvus"])
        self.assertEqual(report["components"]["url-ingest"]["status"], "blocked")


if __name__ == "__main__":
    unittest.main()
