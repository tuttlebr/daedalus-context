#!/usr/bin/env python3
"""Build-time contracts and non-binary health aggregation for Daedalus Context."""

from __future__ import annotations

import argparse
import concurrent.futures
import json
import os
import re
import subprocess
import sys
from pathlib import Path
from typing import Any

import jsonschema
import yaml


ROOT = Path(__file__).resolve().parents[1]
STACK = ROOT / "stack"


def load_yaml(path: Path) -> dict[str, Any]:
    with path.open(encoding="utf-8") as stream:
        value = yaml.safe_load(stream)
    if not isinstance(value, dict):
        raise ValueError(f"{path.relative_to(ROOT)} must contain one mapping")
    return value


def validate_schema(document: dict[str, Any], schema_name: str) -> None:
    schema_path = STACK / "schemas" / schema_name
    with schema_path.open(encoding="utf-8") as stream:
        schema = json.load(stream)
    jsonschema.Draft7Validator(schema).validate(document)


def assert_acyclic(releases: dict[str, Any]) -> None:
    visiting: set[str] = set()
    visited: set[str] = set()

    def visit(name: str) -> None:
        if name in visiting:
            raise ValueError(f"dependency cycle includes {name}")
        if name in visited:
            return
        visiting.add(name)
        for dependency in releases[name].get("dependsOn", []):
            if dependency not in releases:
                raise ValueError(f"{name} depends on unknown release {dependency}")
            visit(dependency)
        visiting.remove(name)
        visited.add(name)

    for release in releases:
        visit(release)


def is_approved_personal_non_ha(acceptance: dict[str, Any]) -> bool:
    risks = acceptance.get("acceptedRisks")
    return (
        acceptance.get("haRequired") is False
        and acceptance.get("status") == "approved"
        and acceptance.get("operatingMode") == "personal-single-user"
        and acceptance.get("haPhaseBlocked") is False
        and isinstance(risks, list)
        and bool(risks)
        and all(isinstance(risk, str) and bool(risk.strip()) for risk in risks)
        and not acceptance.get("requiredDecisions")
    )


def validate_contracts(environment_name: str, require_ha: bool) -> list[str]:
    topology = load_yaml(STACK / "topology.yaml")
    collections = load_yaml(STACK / "collections.yaml")
    rag = load_yaml(STACK / "rag-schema.yaml")
    component_lock = load_yaml(STACK / "components.lock.yaml")
    environment_path = STACK / "environments" / f"{environment_name}.yaml"
    environment = load_yaml(environment_path)

    validate_schema(topology, "topology.schema.json")
    validate_schema(collections, "collections.schema.json")
    validate_schema(rag, "rag-schema.schema.json")
    validate_schema(component_lock, "component-lock.schema.json")
    validate_schema(environment, "environment.schema.json")

    releases = topology["releases"]
    locked_components = component_lock.get("components", {})
    if set(locked_components) != set(releases):
        raise ValueError("components.lock.yaml must contain exactly the topology releases")
    assert_acyclic(releases)
    for profile, members in topology["profiles"].items():
        unknown = sorted(set(members) - set(releases))
        if unknown:
            raise ValueError(f"profile {profile} contains unknown releases: {unknown}")
    if environment["profile"] not in topology["profiles"]:
        raise ValueError(f"environment selects unknown profile {environment['profile']}")

    aliases = [item["alias"] for item in collections["collections"]]
    if len(aliases) != len(set(aliases)):
        raise ValueError("collection aliases must be unique")
    if collections["schemaVersion"] != rag["schemaVersion"]:
        raise ValueError("collections and retrieval schema versions differ")
    unknown_required = sorted(set(environment["requiredCollections"]) - set(aliases))
    if unknown_required:
        raise ValueError(f"required collections are not declared: {unknown_required}")

    contracts = rag["contracts"]
    embedding_profile = rag["embedding"]["endpointProfile"]
    native_profile = rag["reranker"]["daedalusEndpointProfile"]
    nim_profile = rag["reranker"]["nimEndpointProfile"]
    for profile in (embedding_profile, native_profile, nim_profile):
        if profile not in contracts:
            raise ValueError(f"retrieval endpoint profile is not declared: {profile}")
    if contracts[native_profile]["path"] == contracts[nim_profile]["path"]:
        raise ValueError("native and NIM reranker endpoint paths must remain distinct")

    retriever_values = load_yaml(ROOT / "nemotron-retriever" / "values.yaml")
    for role, contract_key in (("embedder", "embedding"), ("reranker", "reranker")):
        expected = rag[contract_key]
        actual = retriever_values[role]
        for field in ("model", "revision", "configSha256"):
            if actual[field] != expected[field]:
                raise ValueError(f"retriever {role}.{field} differs from rag-schema.yaml")
        if actual["maxModelLen"] != expected["maxTokens"]:
            raise ValueError(f"retriever {role}.maxModelLen differs from rag-schema.yaml")
        locked_model = component_lock["models"][contract_key]
        for field in ("repository", "revision", "configSha256"):
            expected_value = expected["model"] if field == "repository" else expected[field]
            if locked_model[field] != expected_value:
                raise ValueError(f"locked {contract_key} {field} differs from rag-schema.yaml")

    cronjob_dir = ROOT / "url-ingest" / "k8s"
    for item in collections["collections"]:
        manifest = load_yaml(cronjob_dir / f"cronjob-{item['alias']}.yaml")
        if manifest["metadata"]["name"] != f"url-ingest-{item['alias']}":
            raise ValueError(f"CronJob name differs for {item['alias']}")
        if manifest["spec"]["schedule"] != item["schedule"]:
            raise ValueError(f"CronJob schedule differs for {item['alias']}")
        if manifest["spec"].get("startingDeadlineSeconds", 0) <= 0:
            raise ValueError(f"CronJob lacks startingDeadlineSeconds for {item['alias']}")

    service_account = (cronjob_dir / "serviceaccount.yaml").read_text(encoding="utf-8")
    if "name: pdf-server" in service_account:
        raise ValueError("url-ingest must not own the pdf-server ServiceAccount")
    for removed in ("pdf-server.yaml", "seed-pdf-job.yaml"):
        if (cronjob_dir / removed).exists():
            raise ValueError(f"url-ingest still owns {removed}")

    acceptance = environment["acceptance"]
    warnings: list[str] = []
    pending_revisions = sorted(
        f"{component}.{field}"
        for component, values in locked_components.items()
        for field, value in values.items()
        if field == "sourceRevision"
        and isinstance(value, str)
        and "pending" in value
    )
    semver_image_pattern = re.compile(
        r"^[^\s@]+:v?[0-9]+\.[0-9]+\.[0-9]+"
        r"(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?$"
    )
    for component, values in locked_components.items():
        image = values.get("image")
        if image is not None and not semver_image_pattern.fullmatch(str(image)):
            raise ValueError(f"invalid SemVer image release: {component}.image")
        for image_name, image_ref in values.get("images", {}).items():
            if not semver_image_pattern.fullmatch(str(image_ref)):
                raise ValueError(
                    f"invalid SemVer image release: {component}.{image_name}"
                )
    if pending_revisions:
        warnings.append(
            f"release source revisions pending: {', '.join(pending_revisions)}"
        )
    if environment["production"]:
        if environment["namespaceMode"] != "independent":
            raise ValueError("production must retain independent namespaces")
        minio_class = environment.get("storage", {}).get("minioStorageClass", "")
        if minio_class == "local-etcd":
            raise ValueError("production MinIO must not use local-etcd")
        ha_required = acceptance.get("haRequired", True)
        if not ha_required:
            if not is_approved_personal_non_ha(acceptance):
                raise ValueError(
                    "non-HA production requires an approved personal-single-user "
                    "policy with accepted risks and no unresolved decisions"
                )
        elif acceptance["status"] != "approved":
            if not acceptance["haPhaseBlocked"]:
                raise ValueError("unapproved production acceptance must block the HA phase")
            warnings.append("production HA remains blocked pending owner decisions")
        if require_ha:
            if not ha_required:
                raise ValueError(
                    "HA validation is disabled by the approved personal-single-user policy"
                )
            validate_approved_acceptance(acceptance)
            if pending_revisions:
                raise ValueError(
                    "HA validation requires all release source revisions to resolve"
                )

    return warnings


def validate_approved_acceptance(acceptance: dict[str, Any]) -> None:
    if acceptance.get("status") != "approved" or acceptance.get("haPhaseBlocked"):
        raise ValueError("HA validation requires approved acceptance policy")
    required_numeric = {
        "nodeLoss": (
            "testDurationSeconds",
            "maxRecoverySeconds",
            "requestRatePerSecond",
            "minimumSuccessRate",
            "p95LatencyMilliseconds",
        ),
        "coordinatorFailover": ("maxLeaderRecoverySeconds",),
        "redisFailover": ("maxRecoverySeconds",),
        "statefulRecovery": ("rpoMinutes", "rtoMinutes"),
    }
    for section, fields in required_numeric.items():
        values = acceptance.get(section)
        if not isinstance(values, dict):
            raise ValueError(f"approved acceptance is missing {section}")
        for field in fields:
            value = values.get(field)
            if not isinstance(value, (int, float)) or isinstance(value, bool) or value <= 0:
                raise ValueError(f"approved acceptance requires positive numeric {section}.{field}")
    success_rate = acceptance["nodeLoss"]["minimumSuccessRate"]
    if success_rate > 1:
        raise ValueError("nodeLoss.minimumSuccessRate must be at most 1")


def agent_env(environment_name: str) -> str:
    topology = load_yaml(STACK / "topology.yaml")
    environment = load_yaml(STACK / "environments" / f"{environment_name}.yaml")
    releases = topology["releases"]

    def url(release: str, service: str, path: str = "") -> str:
        item = releases[release]
        endpoint = item["services"][service]
        return (
            f"http://{endpoint['name']}.{item['namespace']}.svc.cluster.local:"
            f"{endpoint['port']}{path}"
        )

    required = " ".join(environment["requiredCollections"])
    lines = [
        "# Generated from stack/topology.yaml and stack/environments/"
        f"{environment_name}.yaml; do not edit.",
        f"EMBEDDING_BASE_URL={url('retriever', 'embeddings', '/v1')}",
        f"RERANKER_BASE_URL={url('retriever', 'reranker', '/rerank')}",
        f"MILVUS_URI={url('milvus', 'api')}",
        f"NV_INGEST_ENDPOINT={url('nv-ingest', 'api')}",
        f"KUBERNETES_MCP_SERVER={url('k8s-mcp', 'mcp', '/mcp')}",
        f"UNIFI_MCP_SERVER={url('unifi-mcp', 'mcp', '/mcp')}",
        f"DAEDALUS_RAG_READINESS_MODE={environment['ragReadinessMode']}",
        f"DAEDALUS_REQUIRED_COLLECTIONS={required}",
    ]
    return "\n".join(lines) + "\n"


def run_operation(action: str, components: list[str]) -> int:
    topology = load_yaml(STACK / "topology.yaml")
    releases = topology["releases"]
    selected = components or topology["profiles"]["all"]
    invalid = sorted(set(selected) - set(releases))
    if invalid:
        raise ValueError(f"unknown operation components: {invalid}")

    selected_set = set(selected)
    pending = set(selected)
    outcomes: dict[str, dict[str, Any]] = {}

    def execute(component: str) -> tuple[str, subprocess.CompletedProcess[str]]:
        result = subprocess.run(
            ["make", "--no-print-directory", f"{action}-{component}"],
            cwd=ROOT,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            check=False,
            env=os.environ.copy(),
        )
        return component, result

    while pending:
        ready = [
            component
            for component in selected
            if component in pending
            and all(
                dependency not in selected_set or dependency in outcomes
                for dependency in releases[component].get("dependsOn", [])
            )
        ]
        if not ready:
            raise ValueError(f"operation dependency graph stalled on: {sorted(pending)}")

        runnable: list[str] = []
        for component in ready:
            blockers = [
                dependency
                for dependency in releases[component].get("dependsOn", [])
                if dependency in selected_set
                and outcomes[dependency]["status"] != "succeeded"
            ]
            if blockers:
                outcomes[component] = {"status": "blocked", "blockedBy": blockers}
            else:
                runnable.append(component)

        if runnable:
            with concurrent.futures.ThreadPoolExecutor(max_workers=min(3, len(runnable))) as executor:
                results = list(executor.map(execute, runnable))
            for component, result in results:
                output = result.stdout.strip()
                print(f"\n==> {action} {component}")
                if output:
                    print(output)
                status = "succeeded" if result.returncode == 0 else "failed"
                outcomes[component] = {"status": status}
                if result.returncode:
                    outcomes[component]["exitCode"] = result.returncode

        pending.difference_update(ready)

    report = {
        "action": action,
        "components": {component: outcomes[component] for component in selected},
    }
    print(json.dumps(report, indent=2, sort_keys=True))
    return 1 if any(item["status"] != "succeeded" for item in outcomes.values()) else 0


def run_health(environment_name: str, components: list[str]) -> int:
    topology = load_yaml(STACK / "topology.yaml")
    environment = load_yaml(STACK / "environments" / f"{environment_name}.yaml")
    releases = topology["releases"]
    selected = components or topology["profiles"][environment["profile"]]
    invalid = sorted(set(selected) - set(releases))
    if invalid:
        raise ValueError(f"unknown health components: {invalid}")

    checks = [
        (component, layer)
        for component in selected
        for layer in ("provider", "dependencies", "integration")
        if not (layer == "integration" and component == "url-ingest")
    ]

    def execute(check: tuple[str, str]) -> tuple[str, str, subprocess.CompletedProcess[str]]:
        component, layer = check
        result = subprocess.run(
            ["make", "--no-print-directory", f"health-{layer}-{component}"],
            cwd=ROOT,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            check=False,
            env={**os.environ, "CHECK_CONSUMER": "true"},
        )
        return component, layer, result

    with concurrent.futures.ThreadPoolExecutor(max_workers=min(4, len(checks) or 1)) as executor:
        results = list(executor.map(execute, checks))

    failed: list[dict[str, str]] = []
    optional_failed: list[dict[str, str]] = []
    freshness = "fresh"
    for component, layer, result in results:
        if result.returncode == 0:
            continue
        details = [
            line
            for line in result.stdout.strip().splitlines()
            if line.strip() and not re.match(r"^make(?:\[\d+\])?: \*\*\*", line)
        ]
        entry = {
            "component": component,
            "layer": layer,
            "detail": details[-1] if details else "check failed",
        }
        required = bool(releases[component]["requiredFor"])
        if component == "url-ingest" and layer == "dependencies":
            freshness = "stale"
            optional_failed.append(entry)
        elif required:
            failed.append(entry)
        else:
            optional_failed.append(entry)

    availability = "unavailable" if failed else ("degraded" if optional_failed else "healthy")
    acceptance = environment["acceptance"]
    ha_required = acceptance.get("haRequired", True)
    personal_non_ha = is_approved_personal_non_ha(acceptance)
    redundancy = (
        "not-required"
        if personal_non_ha
        else (
            "at-risk"
            if not ha_required or acceptance.get("haPhaseBlocked", True)
            else "met"
        )
    )
    report = {
        "availability": availability,
        "freshness": freshness,
        "redundancy": redundancy,
        "operatingMode": acceptance.get("operatingMode", "shared-production"),
        "failed": failed + optional_failed,
        "blockedBy": (
            []
            if personal_non_ha
            else acceptance.get("requiredDecisions", [])
            or (
                ["invalid personal non-HA acceptance policy"]
                if not ha_required
                else []
            )
        ),
    }
    print(json.dumps(report, indent=2, sort_keys=True))
    if availability == "unavailable":
        return 1
    if (
        availability == "degraded"
        or freshness != "fresh"
        or redundancy not in {"met", "not-required"}
    ):
        return 2
    return 0


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser(prog="contextctl")
    subparsers = result.add_subparsers(dest="command", required=True)

    validate = subparsers.add_parser("validate")
    validate.add_argument("--environment", default="production")
    validate.add_argument("--require-ha", action="store_true")

    show = subparsers.add_parser("show-config")
    show.add_argument("--environment", default="production")

    render = subparsers.add_parser("render-agent-env")
    render.add_argument("--environment", default="production")
    render.add_argument("--check", type=Path)

    health = subparsers.add_parser("health-stack")
    health.add_argument("--environment", default="production")
    health.add_argument("--components", nargs="*", default=[])

    operate = subparsers.add_parser("operate")
    operate.add_argument("--action", choices=("deploy", "update", "redeploy"), required=True)
    operate.add_argument("--components", nargs="*", default=[])
    return result


def main() -> int:
    args = parser().parse_args()
    try:
        if args.command == "validate":
            warnings = validate_contracts(args.environment, args.require_ha)
            print(f"Contract validation passed for {args.environment}.")
            for warning in warnings:
                print(f"BLOCKED: {warning}")
            return 0
        if args.command == "show-config":
            topology = load_yaml(STACK / "topology.yaml")
            environment = load_yaml(STACK / "environments" / f"{args.environment}.yaml")
            print(json.dumps({"environment": environment, "topology": topology}, indent=2, sort_keys=True))
            return 0
        if args.command == "render-agent-env":
            rendered = agent_env(args.environment)
            if args.check:
                actual = args.check.read_text(encoding="utf-8")
                if actual != rendered:
                    raise ValueError(f"generated output is stale: {args.check}")
            else:
                print(rendered, end="")
            return 0
        if args.command == "health-stack":
            return run_health(args.environment, args.components)
        if args.command == "operate":
            return run_operation(args.action, args.components)
    except (FileNotFoundError, KeyError, ValueError, jsonschema.ValidationError) as error:
        print(f"contextctl: {error}", file=sys.stderr)
        return 1
    return 64


if __name__ == "__main__":
    raise SystemExit(main())
