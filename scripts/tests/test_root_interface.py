import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest


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
