"""Verify Helm keeps server capabilities and Kubernetes permissions aligned."""

from pathlib import Path
import subprocess
import unittest

import yaml


ROOT = Path(__file__).resolve().parents[2]
CHART = ROOT / "k8s-multicluster-mcp/chart"


def render(*settings: str, values: Path | None = None) -> list[dict]:
    command = ["helm", "template", "k8s-mcp-server", str(CHART), "-n", "daedalus"]
    if values:
        command.extend(["--values", str(values)])
    for setting in settings:
        command.extend(["--set", setting])
    result = subprocess.run(command, check=True, capture_output=True, text=True)
    return [item for item in yaml.safe_load_all(result.stdout) if item]


class KubernetesMCPAccessTests(unittest.TestCase):
    def test_default_observer_cannot_write_or_read_secrets(self) -> None:
        resources = render()
        roles = [item for item in resources if item["kind"] == "ClusterRole"]
        self.assertEqual(len(roles), 1)
        for rule in roles[0]["rules"]:
            self.assertLessEqual(set(rule["verbs"]), {"get", "list", "watch"})
            self.assertNotIn("secrets", rule.get("resources", []))
            self.assertNotIn("*", rule.get("resources", []))
        deployment = next(item for item in resources if item["kind"] == "Deployment")
        env = {item["name"]: item.get("value") for item in
               deployment["spec"]["template"]["spec"]["containers"][0]["env"]}
        self.assertEqual(env["SERVER_READ_ONLY"], "true")
        self.assertEqual(env["SERVER_NO_HELM"], "true")
        for name in ("MCP_ENABLE_EXEC", "MCP_ENABLE_KUBECTL", "MCP_ENABLE_AGENT_WRITE"):
            self.assertEqual(env[name], "false")

    def test_write_mode_grants_resource_operations_without_changing_network(self) -> None:
        defaults = render()
        resources = render("server.readOnly=false", "server.noHelm=false",
                           "server.enableExec=true", "server.enableKubectl=true",
                           "server.enableAgentWrite=true")
        network_kinds = {"Service", "CiliumNetworkPolicy"}
        self.assertEqual([x for x in defaults if x["kind"] in network_kinds],
                         [x for x in resources if x["kind"] in network_kinds])
        writer = next(x for x in resources if x["kind"] == "ClusterRole"
                      and x["metadata"]["name"].endswith("-readwrite"))
        self.assertEqual(writer["rules"], [{"apiGroups": ["*"], "resources": ["*"],
                         "verbs": ["get", "list", "watch", "create", "update",
                                   "patch", "delete", "deletecollection"]}])
        binding = next(x for x in resources if x["kind"] == "ClusterRoleBinding"
                       and x["roleRef"]["name"] == writer["metadata"]["name"])
        self.assertEqual(binding["subjects"], [{"kind": "ServiceAccount",
                         "name": "k8s-mcp-server", "namespace": "daedalus"}])
        deployment = next(x for x in resources if x["kind"] == "Deployment")
        env = {x["name"]: x.get("value") for x in
               deployment["spec"]["template"]["spec"]["containers"][0]["env"]}
        self.assertEqual(env["SERVER_READ_ONLY"], "false")
        self.assertEqual(env["SERVER_NO_HELM"], "false")
        for name in ("MCP_REQUIRE_AUTH", "MCP_ENABLE_EXEC", "MCP_ENABLE_KUBECTL",
                     "MCP_ENABLE_AGENT_WRITE"):
            self.assertEqual(env[name], "true")

    def test_read_only_helm_can_read_release_state_without_write_binding(self) -> None:
        resources = render("server.noHelm=false", "server.enableExec=true",
                           "server.enableKubectl=true", "server.enableAgentWrite=true")
        self.assertFalse(any(x["metadata"]["name"].endswith("-readwrite")
                             for x in resources))
        role = next(x for x in resources if x["kind"] == "ClusterRole")
        self.assertIn({"apiGroups": [""], "resources": ["secrets"],
                       "verbs": ["get", "list", "watch"]}, role["rules"])
        self.assertTrue(all(set(x["verbs"]) <= {"get", "list", "watch"}
                            for x in role["rules"]))

    def test_daedalus_agent_reuses_credentials_and_preserves_existing_ingress(self) -> None:
        defaults = render()
        values_file = CHART / "values-daedalus.yaml"
        configured = yaml.safe_load(values_file.read_text())
        resources = render(values=values_file)
        deployment = next(x for x in resources if x["kind"] == "Deployment")
        env = {x["name"]: x for x in
               deployment["spec"]["template"]["spec"]["containers"][0]["env"]}
        self.assertEqual(env["OPENCODE_MODEL"]["value"], configured["agent"]["model"])
        pod_spec = deployment["spec"]["template"]["spec"]
        self.assertIn({"name": "opencode-state", "mountPath": "/home/appuser/.local/state"},
                      pod_spec["containers"][0]["volumeMounts"])
        self.assertIn({"name": "opencode-state", "emptyDir": {}}, pod_spec["volumes"])
        self.assertIn({"name": "opencode-config", "mountPath": "/home/appuser/.config/opencode"},
                      pod_spec["containers"][0]["volumeMounts"])
        self.assertIn({"name": "opencode-config", "emptyDir": {}}, pod_spec["volumes"])
        self.assertEqual(pod_spec["initContainers"][0]["command"],
                         ["sh", "-c", "cp -R /home/appuser/.config/opencode/. /opencode-config/"])
        self.assertEqual(env["OPENCODE_API_KEY"]["valueFrom"]["secretKeyRef"], {
            "name": "daedalus-backend-env", "key": "AUTONOMOUS_LLM_MODEL_API_KEY",
            "optional": True,
        })
        before = next(x for x in defaults if x["metadata"]["name"] == "k8s-mcp-server-netpol")
        after = next(x for x in resources if x["metadata"]["name"] == "k8s-mcp-server-netpol")
        self.assertEqual(before["spec"]["ingress"], after["spec"]["ingress"])
        for rule in before["spec"]["egress"]:
            self.assertIn(rule, after["spec"]["egress"])
        backend = next(x for x in resources if x["metadata"]["name"] == "k8s-mcp-server-agent-backend")
        self.assertEqual(backend["spec"]["endpointSelector"]["matchLabels"], {
            "app.kubernetes.io/instance": "switchyard", "app.kubernetes.io/name": "switchyard",
        })
        self.assertEqual(backend["spec"]["ingress"][0]["toPorts"], [
            {"ports": [{"port": "4000", "protocol": "TCP"}]},
        ])

    def test_agent_backend_rejects_an_empty_selector(self) -> None:
        with self.assertRaises(subprocess.CalledProcessError):
            render("networkPolicy.agentBackend.enabled=true")

    def test_agent_model_overrides_preserve_provider_and_backend_model(self) -> None:
        for model in ("switchyard/daedalus", "custom-provider/organization/model"):
            with self.subTest(model=model):
                resources = render(f"agent.model={model}", values=CHART / "values-daedalus.yaml")
                deployment = next(x for x in resources if x["kind"] == "Deployment")
                env = deployment["spec"]["template"]["spec"]["containers"][0]["env"]
                model_env = next(x for x in env if x["name"] == "OPENCODE_MODEL")
                self.assertEqual(model_env, {"name": "OPENCODE_MODEL", "value": model})

    def test_agent_model_rejects_incomplete_opencode_identifiers(self) -> None:
        for model in ("daedalus", "/daedalus", "switchyard/", "switchyard/model name"):
            with self.subTest(model=model), self.assertRaises(subprocess.CalledProcessError) as error:
                render(f"agent.model={model}")
            self.assertIn("agent.model", error.exception.stderr)

    def test_empty_agent_model_keeps_the_existing_secret_reference(self) -> None:
        resources = render()
        deployment = next(x for x in resources if x["kind"] == "Deployment")
        env = deployment["spec"]["template"]["spec"]["containers"][0]["env"]
        model_env = next(x for x in env if x["name"] == "OPENCODE_MODEL")
        self.assertEqual(model_env["valueFrom"]["secretKeyRef"], {
            "name": "k8s-mcp-agent-config", "key": "model", "optional": True,
        })
        self.assertNotIn("value", model_env)


if __name__ == "__main__":
    unittest.main()
