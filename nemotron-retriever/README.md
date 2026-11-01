# Nemotron Retriever

This chart deploys independent embedding and reranking vLLM servers with
compatibility adapters for Daedalus and NV-Ingest. The active model identities,
revisions, dimensions, and API contracts are declared in
[stack/rag-schema.yaml](../stack/rag-schema.yaml). Images, serving flags,
resources, replicas, and caches are configured in [values.yaml](chart/values.yaml).

Both roles select their own runtime image. Check the role-specific `image`
setting rather than inferring the running version from the top-level fallback.
Keep chart settings, the retrieval schema, and the
[component lock](../stack/components.lock.yaml) aligned.

## Deployment

Run from the repository root after the
[configuration and context setup](../README.md#configuration):

```bash
make deploy COMPONENTS=retriever
make health-provider COMPONENTS=retriever
make health-dependencies COMPONENTS=retriever
make health-integration COMPONENTS=retriever
```

The root workflow applies [values-daedalus.yaml](chart/values-daedalus.yaml) in the
shared `daedalus` namespace. GPU placement, compatible kernels, available device
memory, model access, and cache storage must satisfy the selected serving
profile. Initial startup can require model downloads. Set
`huggingFace.existingSecret` when credentials are required; manage the token
outside Helm.

## Request contracts

Default in-cluster endpoints:

| API | Endpoint | Response |
| --- | --- | --- |
| Embeddings | `http://nemotron-retriever-embedder.daedalus.svc.cluster.local:8000/v1/embeddings` | `data[].embedding` |
| Daedalus reranking | `http://nemotron-retriever-reranker.daedalus.svc.cluster.local:8000/rerank` | `results[].relevance_score` |
| NIM reranking | `http://nemotron-retriever-reranker.daedalus.svc.cluster.local:8000/v1/ranking` | `rankings[].logit` |

Keep the compatibility adapter enabled for these contracts. Its implementation
is [files/nim_compat.py](chart/files/nim_compat.py). `/adapter-health` checks the adapter
and upstream model service.

The embedder's `v2` backend translates both NIM `input`/`input_type` and native
role-aware `messages` into the same `/v2/embed` request contract. Use role
`query` for search queries and `document` for passages. The model's prompt
template supplies role prefixes; do not add them a second time. Responses are
finite, normalized vectors in the configured retrieval space.

For local request testing, establish the root context first and forward the
Service:

```bash
make preflight
kubectl --namespace "$DAEDALUS_NAMESPACE" port-forward \
  service/nemotron-retriever-embedder 8000:8000
```

In another terminal, the adapter selects its configured model when `model` is
omitted:

```bash
curl --fail-with-body http://127.0.0.1:8000/v1/embeddings \
  --header 'Content-Type: application/json' \
  --data '{"input":"How is AI used in robotics?","input_type":"query"}'

curl --fail-with-body http://127.0.0.1:8000/v1/embeddings \
  --header 'Content-Type: application/json' \
  --data '{"input":["Robots use AI to perceive and plan."],"input_type":"document"}'
```

If using [smoke-test.sh](smoke-test.sh), explicitly set `EMBED_BASE_URL` and
`EMBED_MODEL` to the intended endpoint and the model in the retrieval schema.
The helper's standalone defaults are not the root deployment contract.

For ranking, forward the reranker Service to local port 8001 using the same
context setup, then call the native API:

```bash
curl --fail-with-body http://127.0.0.1:8001/rerank \
  --header 'Content-Type: application/json' \
  --data '{"query":"How is AI used in robotics?","documents":["Robots use AI to perceive and plan.","Bananas contain potassium."],"top_n":2}'
```

NIM callers use `/v1/ranking` with `query: {text: ...}` and `passages: [{text:
...}]`. The classification backend scores structured query/document pairs,
sorts logits, preserves source indices, and applies `top_n` after scoring.
Any failed pair fails the request. Raw logits can be negative; higher means
more relevant. Do not apply a probability cutoff without calibration.

Served aliases are listed in the role's `servedModelAliases` setting. An alias
selects the configured checkpoint; it does not select another vector space.
Native `/classify` requests can be proxied for multimodal ranking, while the
`/rerank` and `/v1/ranking` translation paths accept text documents.

## Capacity and availability

Consult the role values for context lengths, resource requests, GPU requirements,
and cache sizes. Cache capacity must accommodate the pinned weights and any
retained revisions. Do not delete model PVCs or assume they can shrink during
rollback. Model revisions and the vendored reranker template are pinned; license
and attribution files are retained in [files/nemotron-rerank-notices/](chart/files/nemotron-rerank-notices).

The base configuration uses one replica per role. The embedder has an ephemeral
cache and rolling updates; the reranker has a persistent cache and `Recreate`.
Reranking is optional and consumers must fail open when it is unavailable.
Model warmup and eligible GPU capacity affect recovery time.

For more than one replica, the chart requires `RollingUpdate` and either
disabled persistence or `persistence.multiReplicaSafe=true` backed by verified
cross-node mount behavior. Multi-replica renders add disruption budgets and hard
hostname spread. Replica counts alone do not establish availability.

The servers execute model code under `--trust-remote-code`; keep revisions
reviewed and pinned. Restrict model APIs to intended workloads and LAN clients,
including the ability of multimodal requests to fetch caller-supplied URLs.

## LAN access

The base chart uses ClusterIP. The root production overlay and
[custom-values.yaml](chart/custom-values.yaml) configure fixed NodePorts:

| Service | NodePort | API port |
| --- | --- | --- |
| `nemotron-retriever-embedder` | `30085` | `8000` |
| `nemotron-retriever-reranker` | `30086` | `8000` |

Clients use a node's LAN address with the appropriate NodePort and API path.
Use the node hosting the model for LAN requests. Although
`externalTrafficPolicy: Cluster` enables forwarding, the supplemental Cilium
policy permits only host/remote-node identities on that path. Cross-node LAN
traffic retaining the `world` identity is denied. Per-endpoint CIDRs allow
direct LAN traffic and namespace rules allow in-cluster consumers.

Cross-node source NAT can replace the original client identity with the node
identity. These plain NodePort CIDRs therefore do not enforce an original-client
allowlist on all forwarded traffic. A stricter boundary requires a reviewed
network-boundary or source-restricted LoadBalancer configuration.

Preserve existing endpoint names, ports, routes, and ordinary LAN clients.
Keep production and custom exposure overlays aligned. Before exposure changes,
retain rollback configuration and obtain approval for any compatibility break;
verify both API functionality and non-node LAN access afterward.

## Validation

From this directory:

```bash
make test
make validate
```

These checks exercise the adapter and render the chart locally. Root
`make validate-helm` checks integration contracts; executable changes also need
`make validate-local`. Cluster-facing acceptance uses the root guarded health
and validation targets. Helm test creates a cluster resource and is a separate
live operation.
