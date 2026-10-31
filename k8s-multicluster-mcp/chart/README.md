# Kubernetes MCP Chart

This chart owns the MCP Deployment, Service, disruption budget,
Cilium policies, and namespace-qualified RBAC. Authentication and
optional agent settings use existing Secrets declared in
[values.yaml](values.yaml). The default observer role excludes Kubernetes Secret
data. Enabling Helm adds Secret reads for release metadata. Disabling read-only
mode adds cluster-wide resource CRUD permissions, including Secrets and custom
resources; it does not grant RBAC `bind`, `escalate`, or `impersonate` verbs.

Persist the requested capabilities in the root `.env` before deploying:

```bash
K8S_MCP_READ_ONLY=false
K8S_MCP_NO_HELM=false
K8S_MCP_ENABLE_EXEC=true
K8S_MCP_ENABLE_KUBECTL=true
K8S_MCP_ENABLE_AGENT_WRITE=true
```

These map to `server` values in the chart. Exec, kubectl, and agent writes each
default off and require write-enabled server mode. The agent also requires its
existing configuration Secret. Set `K8S_MCP_READ_ONLY=true` and redeploy to remove
the write role/binding and disable all write tools.

For Daedalus's existing Switchyard autonomous model, persist
`K8S_MCP_VALUES_FILE=k8s-multicluster-mcp/chart/values-daedalus.yaml`. The
[override](values-daedalus.yaml) references the existing backend Secret's model
URL/API-key fields. Set `agent.model` in that file to an OpenCode
`provider/model` identifier: `switchyard/daedalus` selects the backend model
`daedalus`. The provider prefix names the OpenCode provider; it is not part of
the model ID sent to the backend. The chart rejects bare model names; an empty
override uses the existing Secret's model key, which must also contain a
`provider/model` identifier.
It adds ingress and egress between this MCP server and Switchyard on port 4000.
Existing client access rules remain in place. Keep this model selection aligned
with the autonomous model configured by the Daedalus owner.

OpenCode uses writable ephemeral config, state, data, and cache volumes with a
read-only container filesystem. An init container copies the image's bundled
configuration and skills into the config volume before startup.

After the [root configuration setup](../../README.md#configuration), deploy
with root `make deploy COMPONENTS=k8s-mcp`. The base Service is ClusterIP.
External access uses the configured fixed NodePort on a LoadBalancer Service
with an unclaimed class; no separate ingress address is expected.

Use the [external MCP workflow](../../README.md#external-mcp-network-prerequisite)
for Cilium source-range enforcement and rollout checks. Preserve the established
port and full client CIDR on updates. Verify the authenticated API from a
non-node LAN client; Service-field checks alone do not establish compatibility.

See [server configuration and development](../README.md).
Validate the chart through root `make validate-helm`.
