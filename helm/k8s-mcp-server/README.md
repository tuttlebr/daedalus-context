# Kubernetes MCP Helm chart

This chart owns the Kubernetes MCP Deployment, Service, PDB, Cilium
policies, and namespace-qualified cluster RBAC. Authentication and optional
agent configuration are supplied by existing Secrets.

The default observer role excludes Kubernetes Secret data. Enabling Helm adds Secret reads for release metadata. Disabling read-only
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
`K8S_MCP_VALUES_FILE=helm/k8s-mcp-server/values-daedalus.yaml`. The
[override](values-daedalus.yaml) references the existing backend Secret's model
URL/API-key fields and selects `switchyard/daedalus/local` (provider/model).
It adds ingress and egress between this MCP server and Switchyard on port 4000.
Existing client access rules remain in place. Keep this model selection aligned
with the autonomous model configured by the Daedalus owner.

OpenCode uses writable ephemeral config, state, data, and cache volumes with a
read-only container filesystem. An init container copies the image's bundled
configuration and skills into the config volume before startup.

The service is ClusterIP by default. Set `service.external.enabled=true` only
after internal acceptance to enable the fixed NodePort-backed LoadBalancer.
The default controller-less load-balancer class intentionally has no ingress
address; the root `make enable-external-mcp` target verifies its rollout and
fixed CIDR-restricted NodePort explicitly.
