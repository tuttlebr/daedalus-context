# UniFi Network MCP Server

A Model Context Protocol server for the local UniFi Network integration API.
It authenticates to the controller with `X-API-KEY`. Tool operations and typed
request schemas follow the checksum-pinned
[OpenAPI contract](src/pkg/unifi/network_v10.4.57_openapi.json), embedded by
[src/pkg/unifi/contract.go](src/pkg/unifi/contract.go).

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

Provision a controller API key through its administration interface, then
verify access with a read-only request to the local integration API:

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

Call `listSites` and wait for its response before issuing site-scoped calls.
Use the selected site's UUID from `data[].id` as `siteId`; `name` and
`internalReference` (for example, `default`) are not site IDs. Reuse an ID
already confirmed for the intended controller and site.

UUID path and query parameters are validated before contacting the controller.
Invalid arguments and controller failures return MCP tool results with
`isError: true` and an error message. Correct invalid arguments before retrying;
do not repeat an unchanged failed request.

Example tool-call arguments for `listSites`:

```json
{
  "baseUrl": "https://<unifi-console>/proxy/network/integration",
  "apiKey": "YOUR_API_KEY",
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

When fixed-controller mode is disabled, tools accept the connection arguments
below. `baseUrl` and `apiKey` are required only without server defaults.
Fixed-controller mode rejects controller, credential, and TLS overrides.

| Argument | Required | Description |
| --- | --- | --- |
| `baseUrl` | yes | UniFi Network local integration API base URL, such as `https://<unifi-console>/proxy/network/integration` |
| `apiKey` | yes | UniFi integration API key sent as `X-API-KEY` |
| `insecureSkipVerify` | no | Disables TLS verification; prefer a trusted private CA bundle |
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

Use the [root configuration setup](../README.md#configuration) and run from
the repository root:

```bash
make deploy COMPONENTS=unifi-mcp
make health-provider COMPONENTS=unifi-mcp
make health-dependencies COMPONENTS=unifi-mcp
make health-integration COMPONENTS=unifi-mcp
```

The [Helm chart](chart/README.md) deploys to `daedalus`
with fixed-controller mode, existing auth/controller Secrets, verified TLS,
and restricted controller egress. Configure `UNIFI_BASE_URL`,
`UNIFI_CONTROLLER_CIDR`, and the published `UNIFI_MCP_IMAGE` in the root `.env`.
For private certificates, use `UNIFI_CONTROLLER_CA_FILE` and a hostname matching
the certificate; the root workflow also supports explicit hostname/IP mapping.

The in-cluster MCP endpoint is
`http://unifi-network-mcp-server.daedalus.svc.cluster.local:8080/mcp`.
External exposure defaults off. When enabled, the root workflow preserves the
configured fixed NodePort and client CIDR; see the
[external MCP prerequisite](../README.md#external-mcp-network-prerequisite).
Verify existing ordinary LAN clients after network changes. Use root deployment
targets and `make render` in this folder for local chart inspection.

## Development

```bash
make test
make build
```

The tests compare all 73 MCP operations and parameters with
the pinned OpenAPI artifact, verify the embedded contract byte-for-byte,
exercise mutation request/response handling against local TLS servers, and
ensure the complete serialized tool registry remains bounded.
