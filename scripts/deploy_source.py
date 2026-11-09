#!/usr/bin/env python3
"""Build selected local sources, publish unique tags, then reconcile releases."""

from __future__ import annotations

import argparse
from datetime import datetime, timezone
import json
from pathlib import Path
import re
import shlex
import subprocess
import tempfile
import uuid


ROOT = Path(__file__).resolve().parents[1]
# Upstream Milvus, retriever and NV-Ingest images are deliberately absent.
# Hindsight retains its external owner's deployment workflow.
IMAGES = {
    "k8s-mcp": [("k8s-mcp-server-read-only", "K8S_MCP_IMAGE")],
    "unifi-mcp": [("unifi-network-mcp-server", "UNIFI_MCP_IMAGE")],
    "pdf-server": [("pdf-server", "PDF_SERVER_IMAGE")],
    "url-ingest": [("url-ingest", "URL_INGEST_IMAGE"),
                   ("nv-ingest-ms-tasks", "URL_INGEST_RUNNER_IMAGE")],
    "nv-ingest": [("nv-ingest-parser", "NV_INGEST_PARSER_IMAGE")],
}
COMPONENTS = {*IMAGES, "milvus", "retriever", "hindsight"}
VERSIONED_IMAGE = re.compile(r"[^\s@]+:v?\d+\.\d+\.\d+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?")


def deployment_image(image: str, suffix: str) -> str:
    if not VERSIONED_IMAGE.fullmatch(image):
        raise ValueError("selected runtime images must use repository:SemVer tags")
    repository, tag = image.rsplit(":", 1)
    tag += ("." if "-" in tag else "-") + suffix
    if len(tag) > 128:
        raise ValueError("deployment image tag exceeds Docker's 128-character limit")
    return f"{repository}:{tag}"


def deploy(args: argparse.Namespace) -> None:
    selected = list(dict.fromkeys(args.components))
    if not selected or set(selected) - COMPONENTS:
        raise ValueError("select supported, nonempty COMPONENTS")
    if not re.fullmatch(r"linux/[a-z0-9_]+(?:/[a-z0-9_]+)?", args.platform):
        raise ValueError("IMAGE_PLATFORM must select one Linux OS/architecture[/variant]")
    images = dict(item.split("=", 1) for item in args.image)
    owned = [pair for component in selected for pair in IMAGES.get(component, [])]
    suffix = "deploy." + datetime.now(timezone.utc).strftime("%Y%m%d%H%M%S") + ".r" + uuid.uuid4().hex[:12]
    releases = {variable: deployment_image(images.get(variable, ""), suffix)
                for _, variable in owned}
    make = [args.make, "--no-print-directory", "COMPONENTS=" + " ".join(selected)]

    def run(command: list[str], **kwargs):
        return subprocess.run(command, cwd=ROOT, check=True, **kwargs)

    # Validate context, selected charts, external owner and credentials before
    # building/publishing. Registry authentication uses Docker's existing config;
    # anonymous LAN registries remain supported. A failed push prevents rollout.
    for gate in ("doctor", "show-config", "validate-cluster", "verify-shared-secrets"):
        run([*make, gate])

    if owned:
        compose = [*shlex.split(args.compose), "--env-file", args.env_file,
                   "--project-directory", str(ROOT), "-f", str(ROOT / "docker-compose.yaml")]
        run([*shlex.split(args.compose), "version"])
        # Runtime image variables take precedence over legacy Compose aliases.
        # An override supplies exactly the new tags used by the subsequent Helm
        # reconciliation, including when Make command-line values override .env.
        with tempfile.TemporaryDirectory(prefix="daedalus-context-deploy-") as directory:
            override = Path(directory) / "images.json"
            override.write_text(json.dumps({"services": {
                service: {"image": releases[variable], "platform": args.platform}
                for service, variable in owned
            }}))
            compose += ["-f", str(override), "--profile", "ingest"]
            run([*compose, "config", "--quiet"])
            services = [service for service, _ in owned]
            run([*compose, "build", "--pull", *services])
            run([*compose, "push", *services])

    assignments = [f"{variable}={image}" for variable, image in releases.items()]
    for assignment in assignments:
        print(assignment, flush=True)
    # Pass exact references as command-line assignments, not environment-only
    # overrides: recursive Make preserves explicit user variable precedence.
    run([*make, "deploy-reconcile", *assignments])


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--components", nargs="+", required=True)
    parser.add_argument("--image", action="append", default=[])
    parser.add_argument("--make", default="make")
    parser.add_argument("--compose", default="docker compose")
    parser.add_argument("--platform", default="linux/amd64")
    parser.add_argument("--env-file", required=True)
    args = parser.parse_args()
    try:
        deploy(args)
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"Source deployment failed: {error}\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
