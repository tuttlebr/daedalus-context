# UniFi Network MCP Helm chart

This chart owns the UniFi MCP Deployment, Service, PDB, ConfigMap, and Cilium
policy. Authentication and controller API credentials are existing Secrets.

The service is ClusterIP by default. Set `service.external.enabled=true` only
after internal acceptance to enable the fixed NodePort-backed LoadBalancer.
The default controller-less load-balancer class intentionally has no ingress
address; the root `make enable-external-mcp` target verifies its rollout and
fixed CIDR-restricted NodePort explicitly.
