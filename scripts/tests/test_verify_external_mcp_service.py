from __future__ import annotations

import argparse
import importlib.util
import sys
import unittest

from pathlib import Path


path = Path(__file__).parents[1] / "verify-external-mcp-service.py"
spec = importlib.util.spec_from_file_location("verify_external_mcp_service", path)
MODULE = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = MODULE
spec.loader.exec_module(MODULE)


class VerifyExternalMcpServiceTests(unittest.TestCase):
    def setUp(self) -> None:
        self.args = argparse.Namespace(
            node_port=30080,
            source_range="192.168.1.0/24",
            service_port=8080,
            load_balancer_class="daedalus.ai/nodeport-only",
        )
        self.service = {
            "apiVersion": "v1",
            "kind": "Service",
            "metadata": {
                "annotations": {"service.cilium.io/src-ranges-policy": "allow"}
            },
            "spec": {
                "type": "LoadBalancer",
                "loadBalancerClass": "daedalus.ai/nodeport-only",
                "allocateLoadBalancerNodePorts": True,
                "loadBalancerSourceRanges": ["192.168.1.0/24"],
                "sessionAffinity": "ClientIP",
                "ports": [{"name": "http", "port": 8080, "nodePort": 30080}],
            },
        }

    def test_accepts_exact_controllerless_nodeport_contract(self) -> None:
        self.assertEqual([], MODULE.verify_service(self.service, self.args))

    def test_rejects_wrong_nodeport_and_broad_source_range(self) -> None:
        self.service["spec"]["ports"][0]["nodePort"] = 30082
        self.service["spec"]["loadBalancerSourceRanges"] = ["0.0.0.0/0"]
        errors = MODULE.verify_service(self.service, self.args)
        self.assertTrue(any("nodePort 30080" in error for error in errors))
        self.assertTrue(any("192.168.1.0/24" in error for error in errors))


if __name__ == "__main__":
    unittest.main()
