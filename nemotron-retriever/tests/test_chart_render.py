import pathlib
import shutil
import subprocess
import unittest

import yaml


CHART_DIR = pathlib.Path(__file__).parents[1]


def helm_template(*extra_args: str, namespace: str = "review-namespace"):
    command = [
        "helm",
        "template",
        "review",
        str(CHART_DIR),
        "--namespace",
        namespace,
        *extra_args,
    ]
    return subprocess.run(command, check=False, capture_output=True, text=True)


@unittest.skipUnless(shutil.which("helm"), "helm is required for chart render tests")
class ChartRenderTests(unittest.TestCase):
    def test_release_namespace_and_standard_labels_are_applied(self):
        result = helm_template()
        self.assertEqual(result.returncode, 0, result.stderr)

        resources = [resource for resource in yaml.safe_load_all(result.stdout) if resource]
        self.assertGreater(len(resources), 0)
        for resource in resources:
            metadata = resource.get("metadata", {})
            self.assertEqual(metadata.get("namespace"), "review-namespace")
            labels = metadata.get("labels", {})
            self.assertEqual(labels.get("app.kubernetes.io/name"), "nemotron-retriever")
            self.assertEqual(labels.get("app.kubernetes.io/instance"), "review")
            self.assertEqual(labels.get("app.kubernetes.io/version"), "0.17.0")
            self.assertIsNotNone(labels.get("app.kubernetes.io/component"))
            self.assertEqual(labels.get("app.kubernetes.io/part-of"), "daedalus-context")
            self.assertEqual(labels.get("daedalus.ai/tier"), "online")
            self.assertEqual(labels.get("daedalus.ai/capability"), "retrieval")

        services = [resource for resource in resources if resource["kind"] == "Service"]
        self.assertEqual(len(services), 2)
        self.assertTrue(all(service["spec"]["type"] == "ClusterIP" for service in services))
        self.assertFalse(
            any(resource["kind"] == "CiliumNetworkPolicy" for resource in resources)
        )

        network_policies = [
            resource for resource in resources if resource["kind"] == "NetworkPolicy"
        ]
        self.assertEqual(len(network_policies), 2)
        for policy in network_policies:
            component = policy["metadata"]["labels"]["daedalus.ai/target-component"]
            self.assertIn(component, {"embedder", "reranker"})
            self.assertEqual(
                policy["spec"]["podSelector"]["matchLabels"][
                    "app.kubernetes.io/component"
                ],
                component,
            )
            sources = policy["spec"]["ingress"][0]["from"]
            self.assertEqual(
                {
                    source["namespaceSelector"]["matchLabels"][
                        "kubernetes.io/metadata.name"
                    ]
                    for source in sources
                },
                {"daedalus", "nv-ingest"},
            )
            self.assertEqual(policy["spec"]["ingress"][0]["ports"][0]["port"], 8080)

        deployments = [
            resource for resource in resources if resource["kind"] == "Deployment"
        ]
        for deployment in deployments:
            pod_labels = deployment["spec"]["template"]["metadata"]["labels"]
            self.assertEqual(pod_labels["app.kubernetes.io/part-of"], "daedalus-context")
            images = {
                container["name"]: container["image"]
                for container in deployment["spec"]["template"]["spec"]["containers"]
            }
            self.assertEqual(
                images[deployment["metadata"]["labels"]["app.kubernetes.io/component"]],
                "vllm/vllm-openai:v0.17.0",
            )
            self.assertEqual(images["nim-compat"], "python:3.12.11-alpine3.22")
            self.assertTrue(all("@sha256:" not in image for image in images.values()))

    def test_component_specific_ingress_cidrs_are_rendered(self):
        result = helm_template(
            "--set",
            "networkPolicy.ingressCIDRs.embedder[0]=192.168.1.0/24",
            "--set",
            "networkPolicy.ingressCIDRs.reranker[0]=10.20.0.0/16",
        )
        self.assertEqual(result.returncode, 0, result.stderr)

        resources = [resource for resource in yaml.safe_load_all(result.stdout) if resource]
        policies = {
            resource["metadata"]["labels"]["daedalus.ai/target-component"]: resource
            for resource in resources
            if resource["kind"] == "NetworkPolicy"
        }
        self.assertEqual(set(policies), {"embedder", "reranker"})

        for component, expected_cidr in {
            "embedder": "192.168.1.0/24",
            "reranker": "10.20.0.0/16",
        }.items():
            sources = policies[component]["spec"]["ingress"][0]["from"]
            self.assertEqual(
                [source["ipBlock"]["cidr"] for source in sources if "ipBlock" in source],
                [expected_cidr],
            )

    def test_cilium_lan_exposure_filters_before_snat_and_allows_node_forwarding(self):
        result = helm_template(
            "--values",
            str(CHART_DIR / "custom-values.yaml"),
        )
        self.assertEqual(result.returncode, 0, result.stderr)

        resources = [resource for resource in yaml.safe_load_all(result.stdout) if resource]
        services = {
            resource["metadata"]["labels"]["app.kubernetes.io/component"]: resource
            for resource in resources
            if resource["kind"] == "Service"
        }
        self.assertEqual(set(services), {"embedder", "reranker"})

        for component, node_port in {"embedder": 30085, "reranker": 30086}.items():
            service = services[component]
            self.assertEqual(service["spec"]["type"], "LoadBalancer")
            self.assertEqual(
                service["spec"]["clusterIP"],
                {
                    "embedder": "10.105.200.146",
                    "reranker": "10.106.42.103",
                }[component],
            )
            self.assertNotIn("loadBalancerClass", service["spec"])
            self.assertTrue(service["spec"]["allocateLoadBalancerNodePorts"])
            self.assertEqual(
                service["spec"]["loadBalancerSourceRanges"], ["192.168.1.0/24"]
            )
            self.assertEqual(service["spec"]["ports"][0]["nodePort"], node_port)
            self.assertEqual(
                service["metadata"]["annotations"][
                    "service.cilium.io/src-ranges-policy"
                ],
                "allow",
            )
            self.assertEqual(
                service["metadata"]["annotations"]["metallb.io/address-pool"],
                "first-pool",
            )
            self.assertEqual(
                service["metadata"]["annotations"]["metallb.io/loadBalancerIPs"],
                {"embedder": "192.168.1.12", "reranker": "192.168.1.13"}[
                    component
                ],
            )

        cilium_policies = {
            resource["metadata"]["labels"]["daedalus.ai/target-component"]: resource
            for resource in resources
            if resource["kind"] == "CiliumNetworkPolicy"
        }
        self.assertEqual(set(cilium_policies), {"embedder", "reranker"})
        for component, policy in cilium_policies.items():
            self.assertEqual(
                policy["spec"]["endpointSelector"]["matchLabels"][
                    "app.kubernetes.io/component"
                ],
                component,
            )
            ingress = policy["spec"]["ingress"]
            self.assertEqual(ingress[0]["fromEntities"], ["host", "remote-node"])
            self.assertEqual(ingress[0]["toPorts"][0]["ports"][0]["port"], "8080")

    def test_cilium_node_forwarding_rejects_plain_nodeport(self):
        result = helm_template(
            "--set",
            "networkPolicy.ciliumNodePort.enabled=true",
            "--set",
            "embedder.service.type=NodePort",
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("NodePort is unsafe", result.stderr)

    def test_empty_ingress_sources_deny_all_ingress(self):
        result = helm_template("--set-json", "networkPolicy.ingressNamespaces=[]")
        self.assertEqual(result.returncode, 0, result.stderr)

        resources = [resource for resource in yaml.safe_load_all(result.stdout) if resource]
        policies = [
            resource for resource in resources if resource["kind"] == "NetworkPolicy"
        ]
        self.assertEqual(len(policies), 2)
        self.assertTrue(all(policy["spec"]["ingress"] == [] for policy in policies))

    def test_network_policy_targets_vllm_port_without_compatibility_adapter(self):
        result = helm_template(
            "--set",
            "nimCompatibility.enabled=false",
            "--set",
            "embedder.port=8100",
            "--set",
            "reranker.port=8200",
        )
        self.assertEqual(result.returncode, 0, result.stderr)

        resources = [resource for resource in yaml.safe_load_all(result.stdout) if resource]
        policy_ports = {
            resource["metadata"]["labels"]["daedalus.ai/target-component"]: resource[
                "spec"
            ]["ingress"][0]["ports"][0]["port"]
            for resource in resources
            if resource["kind"] == "NetworkPolicy"
        }
        self.assertEqual(policy_ports, {"embedder": 8100, "reranker": 8200})

    def test_unsafe_multi_replica_cache_is_rejected(self):
        result = helm_template(
            "--set",
            "embedder.replicaCount=2",
            "--set",
            "embedder.persistence.enabled=true",
            "--set",
            "embedder.strategy.type=RollingUpdate",
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("persistence.multiReplicaSafe=true", result.stderr)

    def test_safe_multi_replica_profile_renders_pdb_and_hard_spread(self):
        result = helm_template(
            "--set",
            "embedder.replicaCount=2",
            "--set",
            "embedder.persistence.enabled=false",
            "--set",
            "embedder.strategy.type=RollingUpdate",
        )
        self.assertEqual(result.returncode, 0, result.stderr)

        resources = [resource for resource in yaml.safe_load_all(result.stdout) if resource]
        pdb = next(
            resource
            for resource in resources
            if resource["kind"] == "PodDisruptionBudget"
            and resource["metadata"]["labels"]["app.kubernetes.io/component"] == "embedder"
        )
        self.assertEqual(pdb["spec"]["minAvailable"], 1)

        deployment = next(
            resource
            for resource in resources
            if resource["kind"] == "Deployment"
            and resource["metadata"]["labels"]["app.kubernetes.io/component"] == "embedder"
        )
        spread = deployment["spec"]["template"]["spec"]["topologySpreadConstraints"]
        self.assertEqual(spread[0]["whenUnsatisfiable"], "DoNotSchedule")


if __name__ == "__main__":
    unittest.main()
