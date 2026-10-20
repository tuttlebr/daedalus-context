import pathlib
import hashlib
import shutil
import subprocess
import unittest

import yaml
from jinja2 import Environment


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
            self.assertEqual(labels.get("app.kubernetes.io/version"), "0.32.0")
            self.assertIsNotNone(labels.get("app.kubernetes.io/component"))
            self.assertEqual(labels.get("app.kubernetes.io/part-of"), "daedalus-context")
            self.assertEqual(labels.get("daedalus.ai/tier"), "online")
            self.assertEqual(labels.get("daedalus.ai/capability"), "retrieval")

        services = [resource for resource in resources if resource["kind"] == "Service"]
        self.assertEqual(len(services), 2)
        self.assertTrue(all(service["spec"]["type"] == "ClusterIP" for service in services))
        self.assertTrue(
            all("externalTrafficPolicy" not in service["spec"] for service in services)
        )
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
                "vllm/vllm-openai:v0.32.0",
            )
            self.assertEqual(images["nim-compat"], "python:3.12.11-alpine3.22")
            self.assertTrue(all("@sha256:" not in image for image in images.values()))

    def test_reranker_classification_runtime_and_checkpoint_template(self):
        result = helm_template()
        self.assertEqual(result.returncode, 0, result.stderr)
        resources = [r for r in yaml.safe_load_all(result.stdout) if r]
        deployment = next(r for r in resources if r["kind"] == "Deployment"
                          and r["metadata"]["labels"]["app.kubernetes.io/component"] == "reranker")
        containers = deployment["spec"]["template"]["spec"]["containers"]
        server = next(c for c in containers if c["name"] == "reranker")
        args = server["args"]
        self.assertEqual(args[0], "nvidia/Nemotron-3.5-Rerank-8B-BF16")
        self.assertIn("--revision=39aa01525864f736e63f1d5742fc4cf59e754abd", args)
        self.assertIn("nvidia/llama-nemotron-rerank-1b-v2", args)
        for flag in ('--runner=pooling', '--dtype=bfloat16', '--max-model-len=32768',
                     '--chat-template-content-format=openai',
                     '--pooler-config={"seq_pooling_type":"MEAN","use_activation":false}'):
            self.assertIn(flag, args)
        adapter = next(c for c in containers if c["name"] == "nim-compat")
        env = {item["name"]: item.get("value") for item in adapter["env"]}
        self.assertEqual(env["RANKING_BACKEND"], "classify")
        self.assertEqual(env["RANKING_CONCURRENCY"], "4")
        cache = next(r for r in resources if r["kind"] == "PersistentVolumeClaim")
        self.assertEqual(cache["spec"]["resources"]["requests"]["storage"], "40Gi")

        source = (CHART_DIR / "files" / "nemotron-rerank.jinja").read_bytes()
        self.assertEqual(hashlib.sha256(source).hexdigest(),
                         "f2fb4bb2c72df2255ca5b7047d8dc38842302fed3860b67ec161144cb71e39e3")
        template = next(r["data"]["nemotron-rerank.jinja"] for r in resources
                        if r["kind"] == "ConfigMap" and "nemotron-rerank.jinja" in r["data"])
        render = Environment().from_string(template).render
        self.assertEqual(render(messages=[
            {"role": "query", "content": [{"type": "text", "text": "What is [IMG]?"}]},
            {"role": "document", "content": [{"type": "text", "text": "Literal <s>."}]},
        ]), "query: What is [\u200cIMG]?\n\npassage: Literal <\u200cs>.")
        self.assertEqual(render(messages=[
            {"role": "query", "content": [{"type": "text", "text": "Question"}]},
            {"role": "document", "content": [{"type": "image"}, {"type": "text", "text": "Text"}]},
        ]), "[IMG] query: Question\n\npassage: Text")

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

    def test_any_node_exposure_uses_fixed_nodeports_and_scoped_forwarding(self):
        for values_file in ("custom-values.yaml", "values-daedalus.yaml"):
            with self.subTest(values_file=values_file):
                result = helm_template("--values", str(CHART_DIR / values_file))
                self.assertEqual(result.returncode, 0, result.stderr)

                resources = [
                    resource for resource in yaml.safe_load_all(result.stdout) if resource
                ]
                services = {
                    resource["metadata"]["labels"]["app.kubernetes.io/component"]: resource
                    for resource in resources
                    if resource["kind"] == "Service"
                }
                self.assertEqual(set(services), {"embedder", "reranker"})

                for component, node_port in {"embedder": 30085, "reranker": 30086}.items():
                    service = services[component]
                    self.assertEqual(service["spec"]["type"], "NodePort")
                    self.assertEqual(service["spec"]["externalTrafficPolicy"], "Cluster")
                    self.assertEqual(service["spec"]["ports"][0]["nodePort"], node_port)
                    self.assertNotIn("clusterIP", service["spec"])
                    self.assertNotIn("loadBalancerClass", service["spec"])
                    self.assertNotIn("allocateLoadBalancerNodePorts", service["spec"])
                    self.assertNotIn("loadBalancerSourceRanges", service["spec"])
                    self.assertNotIn("annotations", service["metadata"])

                policies = [r for r in resources if r["kind"] == "NetworkPolicy"]
                self.assertEqual(len(policies), 2)
                for policy in policies:
                    self.assertIn(
                        {"ipBlock": {"cidr": "192.168.1.0/24"}},
                        policy["spec"]["ingress"][0]["from"],
                    )
                forwarding = {
                    r["metadata"]["labels"]["daedalus.ai/target-component"]: r
                    for r in resources
                    if r["kind"] == "CiliumNetworkPolicy"
                }
                self.assertEqual(set(forwarding), {"embedder", "reranker"})
                for component, policy in forwarding.items():
                    self.assertEqual(
                        policy["spec"]["endpointSelector"]["matchLabels"],
                        services[component]["spec"]["selector"],
                    )
                    self.assertEqual(
                        policy["spec"]["ingress"],
                        [{
                            "fromEntities": ["host", "remote-node"],
                            "toPorts": [{"ports": [{"port": "8080", "protocol": "TCP"}]}],
                        }],
                    )

    def test_node_forwarding_only_targets_exposed_services_and_their_api_port(self):
        result = helm_template(
            "--set", "networkPolicy.ciliumNodePort.enabled=true",
            "--set", "embedder.service.type=NodePort",
            "--set", "nimCompatibility.enabled=false",
            "--set", "embedder.port=8100",
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        policies = [
            r for r in yaml.safe_load_all(result.stdout)
            if r and r["kind"] == "CiliumNetworkPolicy"
        ]
        self.assertEqual(len(policies), 1)
        self.assertEqual(
            policies[0]["spec"]["endpointSelector"]["matchLabels"][
                "app.kubernetes.io/component"
            ], "embedder",
        )
        self.assertEqual(
            policies[0]["spec"]["ingress"][0]["toPorts"][0]["ports"],
            [{"port": "8100", "protocol": "TCP"}],
        )

    def test_load_balancer_forwarding_still_requires_source_filtering(self):
        result = helm_template(
            "--set", "networkPolicy.ciliumNodePort.enabled=true",
            "--set", "embedder.service.type=LoadBalancer",
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("loadBalancerSourceRanges must contain", result.stderr)

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
