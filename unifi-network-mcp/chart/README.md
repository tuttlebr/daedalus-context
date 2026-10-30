# UniFi Network MCP Chart

This chart owns the MCP Deployment, Service, disruption budget, controller
configuration, and Cilium policy. [values.yaml](values.yaml) defines existing
auth/controller Secrets and verified TLS settings. Production uses a fixed
controller; callers cannot override its address or credentials.

After the [root configuration setup](../../README.md#configuration), deploy
with root `make deploy COMPONENTS=unifi-mcp`. Use a public CA bundle and a
certificate-matching hostname for private controller certificates. Configure
`controller.hostAliases` through the root hostname/IP settings if DNS needs an
explicit mapping.

The base Service is ClusterIP. External access uses a fixed NodePort on a
LoadBalancer Service with an unclaimed class, without a separate ingress
address. Follow the [external MCP workflow](../../README.md#external-mcp-network-prerequisite),
preserve established client access, and verify the API from a non-node LAN
client after exposure changes.

See [server configuration and development](../README.md).
Validate the chart through root `make validate-helm`.
