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
