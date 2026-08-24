#!/usr/bin/env python3
"""Verify the fixed, CIDR-restricted external MCP Service contract."""

from __future__ import annotations

import argparse
import json
import sys


def verify_service(service: dict, args: argparse.Namespace) -> list[str]:
    spec = service.get("spec", {})
    metadata = service.get("metadata", {})
    errors: list[str] = []
    if service.get("kind") != "Service":
        errors.append("resource is not a Service")
    if spec.get("type") != "LoadBalancer":
        errors.append("spec.type must be LoadBalancer")
    if spec.get("loadBalancerClass") != args.load_balancer_class:
        errors.append(
            f"spec.loadBalancerClass must be {args.load_balancer_class}"
        )
    if spec.get("allocateLoadBalancerNodePorts") is not True:
        errors.append("spec.allocateLoadBalancerNodePorts must be true")
    ports = [
        item
        for item in spec.get("ports", [])
        if item.get("name") == "http" and item.get("port") == args.service_port
    ]
    if len(ports) != 1 or ports[0].get("nodePort") != args.node_port:
        errors.append(
            f"http service port {args.service_port} must use nodePort {args.node_port}"
        )
    if spec.get("loadBalancerSourceRanges") != [args.source_range]:
        errors.append(
            f"spec.loadBalancerSourceRanges must contain only {args.source_range}"
        )
    if spec.get("sessionAffinity") != "ClientIP":
        errors.append("spec.sessionAffinity must be ClientIP")
    annotations = metadata.get("annotations", {})
    if annotations.get("service.cilium.io/src-ranges-policy") != "allow":
        errors.append("Cilium source-range enforcement annotation is missing")
    return errors


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--node-port", type=int, required=True)
    parser.add_argument("--source-range", required=True)
    parser.add_argument("--service-port", type=int, default=8080)
    parser.add_argument(
        "--load-balancer-class", default="daedalus.ai/nodeport-only"
    )
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    service = json.load(sys.stdin)
    errors = verify_service(service, args)
    if errors:
        for error in errors:
            print(f"ERROR: {error}", file=sys.stderr)
        raise SystemExit(1)
    print(
        "External MCP Service verified: "
        f"nodePort={args.node_port} sourceRange={args.source_range}"
    )


if __name__ == "__main__":
    main()
