# UniFi Network MCP Server

[Model Context Protocol](https://modelcontextprotocol.io/) server for the local UniFi Network integration API on a UDM/UniFi console. It exposes the complete 73-operation surface documented by the supplied [UniFi Network 10.5.67 API capture](./UniFi-Network-API.md) and authenticates to UniFi with `X-API-KEY`. Typed request schemas come from the checksum-pinned [UniFi Network 10.4.57 OpenAPI contract](https://developer.ui.com/network/v10.4.57/openapi.json), whose method, path, and operation ID set matches the 10.5.67 capture.

By default the server starts without a UniFi device URL, API key, or device credentials. MCP clients can provide `baseUrl` and `apiKey` on each tool call, or private deployments can configure server-side defaults with environment variables.

## Features

- 73 UniFi Network tools: 41 `GET`, 12 `POST`, 9 `PUT`, 1 `PATCH`, and 10 `DELETE` operations for sites, devices, clients, networks, Wi-Fi, hotspot vouchers, firewall, ACL, switching, DNS, WAN, VPN, RADIUS, DPI, and countries.
- Mutation tools include typed JSON request bodies, discriminator variants, typed path/query parameters, and MCP read-only, destructive, and idempotent annotations.
- `stdio`, `sse`, and `streamable-http` transports using the same Go MCP stack as the reference Kubernetes MCP server.
- Optional HTTP bearer-token auth for MCP clients.
- `/livez` and `/readyz` are local-only health; `/dependencyz` performs one
  bounded controller call. `/healthz` remains a local compatibility alias.
- TLS verification is enabled by default. Clients can set `insecureSkipVerify=true` per tool call, or trusted deployments can configure `UNIFI_INSECURE_SKIP_VERIFY=true`.
- Sanitizes secret-like keys and rejects oversized tool responses with a compact JSON error.

## Mutation Safety

Authenticated MCP callers can create, update, act on, or delete controller-managed objects. MCP annotations tell compatible clients which tools are read-only or destructive, but those annotations are advisory and are not an additional authorization boundary. Restrict `MCP_AUTH_TOKEN`, use `UNIFI_FIXED_CONTROLLER=true` in production, review mutation arguments before approval, and grant the configured UniFi API key only the privileges its callers need.

Validation and health checks use `GET /v1/info`; the test suite uses local test servers and does not send mutation traffic to a UniFi controller.

## Validate UniFi API Access

Generate an API key at [unifi.ui.com](https://unifi.ui.com/) (Settings → API Keys), then make one safe read-only request directly to the local Network integration API:

```bash
curl --fail --silent --show-error \
  --header 'X-API-KEY: YOUR_API_KEY' \
  --header 'Accept: application/json' \
  'https://<unifi-console>/proxy/network/integration/v1/info'
```

Keep TLS verification enabled. If the console uses a private CA, trust that CA rather than disabling certificate verification.

## Quick Start

```bash
cd src
go mod download
CGO_ENABLED=0 go build -o unifi-network-mcp-server main.go

./unifi-network-mcp-server --mode streamable-http
```

MCP endpoint:

```text
http://localhost:8080/mcp
```

Health checks:

```bash
curl http://localhost:8080/livez
curl http://localhost:8080/readyz
curl http://localhost:8080/dependencyz
```

Example tool-call arguments for `listSites`:

```json
{
  "baseUrl": "https://<unifi-console>/proxy/network/integration",
  "apiKey": "YOUR_API_KEY",
  "insecureSkipVerify": true,
  "limit": 25
}
```

Mutation tools accept a `body` object when the API operation defines a request body. For example, `patchFirewallPolicy` changes controller state and accepts:

```json
{
  "siteId": "SITE_UUID",
  "firewallPolicyId": "POLICY_UUID",
  "body": {
    "loggingEnabled": true
  }
}
```

## Configuration

| Flag | Environment Variable | Default | Description |
| --- | --- | --- | --- |
| `--mode` | `SERVER_MODE` | `streamable-http` | Transport: `stdio`, `sse`, or `streamable-http` |
| `--port` | `SERVER_PORT` | `8080` | HTTP listen port |
| - | `MCP_AUTH_TOKEN` | empty | Optional bearer token for MCP HTTP requests |
| - | `MCP_AUTH_TOKEN_NEXT` | empty | Optional overlap token for zero-downtime rotation |
| - | `MCP_REQUIRE_AUTH` | `false` | Fail startup for HTTP modes when `MCP_AUTH_TOKEN` is unset |
| - | `UNIFI_BASE_URL` | empty | Optional server default for `baseUrl` |
| - | `UNIFI_INSECURE_SKIP_VERIFY` | unset | Optional server default for `insecureSkipVerify` |
| - | `UNIFI_API_KEY` | empty | Optional server default for `apiKey`; use only for private/trusted deployments |
| - | `UNIFI_FIXED_CONTROLLER` | `false` | Remove per-call controller/key/TLS overrides from tool schemas and reject supplied overrides |

Every UniFi tool accepts these connection arguments. `baseUrl` and `apiKey` are required only when no server-side default is configured.

| Argument | Required | Description |
| --- | --- | --- |
| `baseUrl` | yes | UniFi Network local integration API base URL, such as `https://<unifi-console>/proxy/network/integration` |
| `apiKey` | yes | UniFi integration API key sent as `X-API-KEY` |
| `insecureSkipVerify` | no | Set `true` only for self-signed local console certificates |
| `timeoutSeconds` | no | Per-call outbound timeout, default `30`, max `120` |

Endpoint-specific path/query arguments and mutation `body` schemas are included directly in each MCP tool definition.

## Image Release

```bash
cd ..
make images-build
make images-release-refs
```

The workspace-root `docker-compose.yaml` is the canonical image specification.
Kubernetes is the supported runtime deployment path.

## Kubernetes Deployment

Set distinct `UNIFI_MCP_AUTH_TOKEN`, a SemVer-tagged `UNIFI_MCP_IMAGE`,
`UNIFI_BASE_URL`, `UNIFI_API_KEY`, and the exact `UNIFI_CONTROLLER_CIDR` in
`.env`. Production enables fixed-controller mode and restricts egress to that
CIDR.

```bash
cp ../.env.example ../.env
# Edit the required UNIFI_* values and image version.
./deploy.sh
```

The default manifest creates a two-replica Deployment, PDB, hard hostname
spread, default-deny policy, and authenticated fixed NodePort on `30081` in
namespace `unifi-network-mcp-server`. Its Cilium policy permits NodePort
forwarding, while the NodePort-backed `LoadBalancer` Service restricts external
sources to `MCP_EXTERNAL_CLIENT_CIDR` (default `192.168.1.0/24`) and bearer
authentication remains mandatory. The deploy wrapper fails closed unless Cilium has
`bpf.lbSourceRangeAllTypes=true`, which is required to enforce that allow-list
for NodePort traffic. The unclaimed `daedalus.ai/nodeport-only` class prevents
MetalLB from assigning a separate address. Config and token checksums roll pods
only when effective configuration changes.

## Development

```bash
make test
make build
```

The tests compare all 73 MCP operations and parameters with
`network_v10.4.57_openapi.json`, verify the embedded contract byte-for-byte,
exercise mutation request/response handling against local TLS servers, and
ensure the complete serialized tool registry remains bounded.
