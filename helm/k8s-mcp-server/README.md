# Kubernetes MCP Helm chart

This chart owns the read-only Kubernetes MCP Deployment, Service, PDB, Cilium
policies, and namespace-qualified cluster RBAC. Authentication and optional
agent configuration are supplied by existing Secrets.

The service is ClusterIP by default. Set `service.external.enabled=true` only
after internal acceptance to enable the fixed NodePort-backed LoadBalancer.
The default controller-less load-balancer class intentionally has no ingress
address; the root `make enable-external-mcp` target verifies its rollout and
fixed CIDR-restricted NodePort explicitly.
