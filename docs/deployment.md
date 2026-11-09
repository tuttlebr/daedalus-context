# Deployment

Run commands from the repository root. Kubernetes is the only deployment
runtime. Component Makefiles provide development and chart-inspection commands;
the root Makefile owns service deployment. See [operations](operations.md) for
updates, data jobs, diagnostics, and recovery after installation.

## Prerequisites

Local validation needs the tools in [Contributing](../CONTRIBUTING.md).
Deployment additionally needs `kubectl`, Helm 3, `jq`, cluster credentials,
permissions to manage the selected releases, and an explicitly approved
Kubernetes context. Image builds need Docker with Compose v2 and network access
to dependencies; publication needs a registry you control and cluster pull access.

Review these infrastructure dependencies before copying the reference profile:

| Dependency | What to prepare |
| --- | --- |
| Compute | Enough eligible nodes for affinities, anti-affinities, resource requests, and replicas in the selected charts |
| GPUs | NVIDIA runtime/device support and compatible hardware for the pinned retrieval and parser models; inspect the selected profile's GPU and cache requirements |
| Storage | Reviewed classes for etcd, MinIO, Redis, model caches, PDF corpus, and URL state; shared URL/PDF claims require suitable shared storage and working file locks |
| Network | Cluster DNS, NetworkPolicy enforcement, registry/model access, and the Cilium capabilities used by the reference LAN exposure |
| Images | Build/publish owned images or use accessible reviewed releases; obtain an accessible copy of the pinned MinIO mirror separately |
| Secrets | Populate the selected providers' externally managed Secrets before Helm reconciliation |
| Hindsight | Obtain the separately maintained `daedalus-hindsight` checkout and configure its own `.env`, or omit `hindsight` from `COMPONENTS` |
| Agent | Configure and deploy `daedalus-agent` separately before consumer health checks |
| Sources | Supply your own corpus and review source lists before scheduling ingestion |

The private registry in the reference files is not a public distribution
service. [The component lock](../stack/components.lock.yaml) records the tested
image/model inventory; it does not make those artifacts accessible. The MinIO
server and bucket helper use the same pinned mirror in
[Milvus values](../nim-milvus-ms/chart/values.yaml). Preserve that binary contract
when mirroring it; switching MinIO versions is a separate reviewed upgrade.

The reference installation is personal and non-HA. Its
[production acceptance policy](../stack/environments/production.yaml) records
that decision. `make validate-ha` deliberately fails without an approved numeric
HA policy. Selecting `development` does not resize workloads or remove node
selectors, Cilium policies, storage dependencies, or model requirements.

## Configuration

Create the root configuration without overwriting an existing installation:

```bash
test -e .env || cp .env.example .env
${EDITOR:-vi} .env
set -a
source .env
set +a
```

Set `EXPECTED_CONTEXT` to the exact approved context name as literal text in
`.env`. Never derive it from the active context: that defeats the wrong-cluster
guard. Before every Kubernetes or Helm cluster command, export `.env` and retain
that approved literal. `deploy`, `restart`, and `recovery-status` load `.env`
themselves; direct health, update, validation, and data targets require export.

The root `.env` is shell syntax as well as Compose configuration. Quote values
containing spaces. Keep credentials out of tracked values files. Make command-line
assignments take precedence over exported settings. Context-owned charts do not
load component-local `.env` files.

### Adapt chart values

Use reviewed override files for another cluster, keeping existing production
settings intact. Root deployment and chart validation accept:

| Component | Values file setting |
| --- | --- |
| Milvus and MinIO | `MILVUS_VALUES_FILE` |
| Retriever | `RETRIEVER_VALUES_FILE` (defaults to the Daedalus overlay) |
| NV-Ingest, Redis, parser | `NV_INGEST_VALUES_FILE` |
| PDF server | `PDF_SERVER_VALUES_FILE` |
| URL ingestion | `URL_INGEST_VALUES_FILE` |
| Kubernetes MCP | `K8S_MCP_VALUES_FILE` |
| UniFi MCP | `UNIFI_MCP_VALUES_FILE` |

Paths are resolved from the repository root. Base chart values are followed by
the selected values file; explicit root `--set` settings take precedence. Thus
set image references, storage variables exposed by Make, MCP ports/auth modes,
and other root settings in `.env`, and use chart overrides for placement,
resources, pull secrets, and other component-specific settings. Hindsight uses
its owning repository's configuration interface.

Inspect both the chart values and Make recipe for the selected component.
Replace required `daedalus-*` node selectors and review exclusions, GPU allocation,
resource requests, storage classes, and network ranges. `RETRIEVER_VALUES_FILE`
replaces the reference overlay; it is not appended after it. Keep models,
service names, embedding dimensions, and the shared `daedalus` namespace aligned
with the stack contracts. Namespace or collection-model redesign requires a
coordinated contract change, not only an environment-variable override.

The root config derives internal provider URLs. Use `make render-agent-env` for
consumer settings instead of maintaining a second endpoint list. Review
`MINIO_STORAGE_CLASS`, `ETCD_STORAGE_CLASS`, `PDF_STORAGE_CLASS`,
`IMAGE_STORAGE_URI`, source node/path settings, and selected collections. A PDF
publication host path is on the Kubernetes source node, not necessarily on the
machine running Make. [PDF publication](../pdf-server/README.md) specifies the
expected source layout and generation lifecycle.

### Secrets

Root `.env` credentials alone do not create context-owned Kubernetes Secrets.
Provision the namespace and Secrets through your normal secret-management
process before deployment. The default chart contract is:

| Secret | Required data keys | Used by |
| --- | --- | --- |
| `milvus-minio-credentials` | `accesskey`, `secretkey` | Milvus, MinIO, URL ingestion |
| `milvus-root-credentials` | `password` | Milvus, URL ingestion |
| `ngc-api` | `NGC_API_KEY` | NV-Ingest |
| `ngc-secret` | `.dockerconfigjson` (registry Secret) | NV-Ingest image pulls |
| `nv-ingest-object-store` | `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY` | NV-Ingest; match MinIO credentials |
| `nvcr-imagepullsecret` | `.dockerconfigjson` (registry Secret) | URL ingestion image pulls |
| `url-ingest-hf` | `HF_ACCESS_TOKEN` | URL ingestion |
| `k8s-mcp-auth` | `token`; optional `next-token` | Kubernetes MCP |
| `unifi-network-mcp-auth` | `token`; optional `next-token` | UniFi MCP |
| `unifi-network-mcp-unifi-api` | `api-key` | UniFi controller access |

Use distinct bearer tokens for the two MCP providers. Additional registry
credentials depend on where your images are hosted. Optional retriever HF auth
and Kubernetes MCP Agent configuration are described in their chart values.
Hindsight's owning repository creates `hindsight-runtime` from its own `.env`.
Do not copy those credentials into Helm values.

`make verify-shared-secrets COMPONENTS="..."` checks existence of the selected
default Secrets; it does not validate their keys or authenticate credentials.
Review customized Secret references too. The legacy migration targets
copy this deployment's old namespace layout; they are not a fresh-install
bootstrap and are not required for a new cluster.

### External MCP network prerequisite

Fresh MCP deployments default to internal Services. If enabling LAN access,
review `MCP_EXTERNAL_ENABLED`, the stable NodePorts, and
`MCP_EXTERNAL_CLIENT_CIDR`. Reference Services use an unclaimed load-balancer
class to retain NodePorts without allocating a new external address. Cilium
must enforce source ranges on those Services (`bpf.lbSourceRangeAllTypes`).

Preserve existing endpoints, ingress routes, and ordinary LAN clients during
updates. Retain rollback configuration before changing exposure or credentials.
Verify authenticated API calls from a non-node LAN client as well as the Agent;
for the author's cluster, try SSH to `daedalus-07` first. A cluster-only probe
does not establish LAN compatibility.

Retriever and parser LAN exposure is independent of `MCP_EXTERNAL_ENABLED`:

| Reference service | Fixed NodePort | API |
| --- | --- | --- |
| Embedding | `30085` | `/v1/embeddings` |
| Reranking | `30086` | `/rerank` or NIM-compatible `/v1/ranking` |
| Parsing | `30089` | `/v1/parse` |

The [retriever overlay](../nemotron-retriever/chart/values-daedalus.yaml) and
[parser values](../nv-ingest/chart/values.yaml) own these settings. The retriever
uses plain NodePorts and Cilium policies. Use the node hosting the model for
LAN requests: the committed forwarding policy permits host/remote-node identities
and blocks cross-node LAN traffic retaining the `world` identity. These plain
NodePorts do not enforce an original-client allowlist on every source-NAT path.
A source-restricted LoadBalancer profile needs a separately reviewed configuration.
Confirm the selected policy and actual client path; a NodePort allocation alone
does not establish forwarding or source enforcement. Internal Service DNS
remains the Agent's path. The model adapters' API-key placeholders do not provide
authentication; preserve their network access controls.

For a UniFi controller with a private CA, configure `UNIFI_CONTROLLER_CA_FILE`
and, when needed, the paired hostname/IP settings. Keep TLS verification enabled.
The tracked trust certificate is reference-cluster material, not a general CA
for another deployment; never add a controller private key.

## Build and publish images

Image workflows honor `ENV_FILE` just like deployment (default: root `.env`).
A clone without `.env` can still validate the default image specification.

Choose registry references for all owned images in `.env` and configure Docker
registry access before deployment. Anonymous LAN registries remain supported.
`make deploy` uses the Kubernetes runtime image variables as its authoritative
repositories and base SemVer tags, including explicit Make command-line
overrides. It builds only selected components' owned contexts and publishes
unique tags such as `1.2.3-deploy.<utc-time>.<nonce>`. Those exact references are
printed and sent to reconciliation without rewriting `.env`. Tags change on
each invocation, so a cached old image cannot prevent the new source rollout.
Source deployments use `IMAGE_PLATFORM=linux/amd64` for the reference cluster,
including on ARM developer hosts. Override it only with reviewed Dockerfile,
dependency and chart placement support for the selected components. This is a
single deployment platform; a multi-platform distribution is a separate image
release workflow.

For the standalone image commands below, keep Compose aliases aligned with the
runtime variables. For example, `K8S_MCP_COMPOSE_IMAGE` selects their build
destination and `K8S_MCP_IMAGE` selects the deployment reference. Use a new
immutable SemVer tag for each standalone release; changing
`DAEDALUS_CONTEXT_IMAGE_VERSION` alone does not replace explicit component tags.

```bash
make images-validate
make images-build
# When intentionally publishing to your registry:
make images-publish
make images-release-refs
```

Publication builds and pushes the six owned image contexts, then prints the
Kubernetes environment assignments. Copy the reviewed assignments into `.env`
and re-export it. `IMAGE_BUILD_SERVICES` limits build/push scope using Compose
service names; it is distinct from runtime `COMPONENTS`. The reference runner and
parser builds download pinned dependencies/models and can be large. See their
Dockerfiles and model notices before redistributing built images.

`make update` reconciles already-published image references without building or
publishing. `make deploy` includes publication after its context, chart and
credential gates. Build/push failures prevent cluster reconciliation. Upstream
images retain their pinned versions; only the local parser is built for
`nv-ingest`, and only the crawler/runner are built for `url-ingest`. Hindsight
uses its external owner's deployment workflow, not this repository's image
builder. Record reviewed
source revisions, image/model versions, and upstream dependency changes in the
[component lock](../stack/components.lock.yaml). Helm dependencies are pinned
and vendored; update `Chart.yaml`, `Chart.lock`, and the archive together.

## Deploy and accept

All eight components, including Hindsight, are selected by default. To deploy
only this repository's seven services, set an explicit selection in `.env`:

```bash
COMPONENTS="milvus retriever nv-ingest pdf-server url-ingest k8s-mcp unifi-mcp"
```

For a smaller first integration, `COMPONENTS="k8s-mcp"` avoids GPU, storage, and
UniFi dependencies; still configure its image, auth Secret, and placement.
Selections do not automatically include dependencies. The ingestion set is
`milvus retriever nv-ingest pdf-server url-ingest`.

After exporting the reviewed `.env`:

```bash
make doctor
make show-config
make verify-shared-secrets
make validate-cluster
make deploy
make health-provider
```

`deploy` repeats doctor, configuration, chart/cluster, and Secret gates, then
builds/pushes selected local images, reconciles releases in dependency order and
checks provider health. It retains PVCs, collections, corpus and refresh state;
it neither seeds data nor triggers refresh Jobs. Preserve the configured
`URL_INGEST_SUSPEND` setting when enabling or suspending schedules. Cluster
validation is read-only: local renders, client dry-runs, and capacity inspection.
It does not reproduce every deployment `--set` override, and is not proof of
schedulability, successful image pulls, or working data paths.
The capacity preflight checks selected Milvus and retriever constraints; inspect
other workloads and GPU-memory requirements separately.

For an existing installation, run `make health-dependencies` before deciding
whether any data work is needed. For a fresh corpus, follow
[data operations](operations.md#data-operations), wait for refresh completion,
and verify retrieval. Only then enable schedules and persist
`URL_INGEST_SUSPEND=false` in `.env`.

Generate the Agent configuration with `make render-agent-env` and supply
matching secrets. If the Agent requires nonempty model API-key fields, set
`EMBEDDING_API_KEY=local-vllm-no-auth` and `RERANKER_API_KEY=local-vllm-no-auth`
in its configuration; these placeholders do not authenticate the adapters.
Deploy the Agent from its own repository and run `make health-stack`.
`CHECK_CONSUMER=false` is only for supported checks before the Agent exists; it
is not consumer acceptance. Enable external MCP access after internal acceptance
with `make enable-external-mcp`, verify ordinary LAN clients, and persist the
approved exposure settings for subsequent updates.
