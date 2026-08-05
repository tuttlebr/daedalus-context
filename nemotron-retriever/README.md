# Nemotron Retriever vLLM Helm chart

This chart deploys two independent, GPU-backed vLLM servers:

| Component | Model | API |
| --- | --- | --- |
| `embedder` | `nvidia/llama-nemotron-embed-vl-1b-v2` | `POST /v1/embeddings` |
| `reranker` | `nvidia/llama-nemotron-rerank-1b-v2` | native `POST /rerank`; NIM-compatible `POST /v1/ranking` |

Both Services are `ClusterIP` by default and can be exposed independently through a source-restricted node-address endpoint. They expose the NVIDIA-compatible adapter on port 8000; the adapter forwards requests to the native vLLM servers and translates the reranker API. The chart pins `vllm/vllm-openai:v0.17.0`, the first vLLM release identified by NVIDIA as supporting the embedder architecture. The reranker receives NVIDIA's required query/document score template through a ConfigMap. Each deployment requests one NVIDIA GPU and has its own persistent Hugging Face model cache.

Review and accept the model repositories' NVIDIA Open Model License and referenced Llama terms before deployment; those model terms are separate from vLLM's software license.

## Prerequisites

- Kubernetes 1.25 or newer.
- Helm 3.
- NVIDIA drivers, container runtime integration, and the NVIDIA device plugin or GPU Operator.
- At least two schedulable GPUs for the default simultaneous deployment. Disable one component or schedule them separately if fewer GPUs are available.
- Egress to Hugging Face during the initial model download. Image URL embedding also requires egress to each referenced image host.
- A default StorageClass, or an explicit `persistence.storageClass` for each model.

The model repositories currently permit anonymous downloads. If a token is needed, create it outside Helm so it is not stored in release values:

```bash
kubectl --namespace daedalus-retriever create secret generic huggingface-token \
  --from-literal=token='<HUGGING_FACE_TOKEN>'
```

Then set `huggingFace.existingSecret=huggingface-token`. Do not commit the token or put it directly in a values file.

## Install

From the repository root:

```bash
helm upgrade --install nemotron-retriever ./nemotron-retriever \
  --namespace daedalus-retriever \
  --create-namespace \
  --set huggingFace.existingSecret=huggingface-token \
  --wait \
  --timeout 30m
```

Omit the `huggingFace.existingSecret` setting for anonymous downloads. Resources use the Helm release namespace by default. `namespaceOverride` exists only for compatibility with an intentionally split legacy release and should remain empty for new deployments.

Check rollout and service health:

```bash
kubectl --namespace daedalus-retriever get deployments,pods,services,pvc
helm --namespace daedalus-retriever test nemotron-retriever
```

## Endpoints

Inside the cluster, the default service URLs are:

```text
http://nemotron-retriever-embedder.daedalus-retriever.svc.cluster.local:8000/v1/embeddings
http://nemotron-retriever-reranker.daedalus-retriever.svc.cluster.local:8000/rerank
http://nemotron-retriever-reranker.daedalus-retriever.svc.cluster.local:8000/v1/ranking
```

Daedalus uses native `/rerank` with `{query, documents, top_n}` and consumes
`results[].relevance_score`. NV-Ingest/NIM-compatible callers use
`/v1/ranking` with `{query: {text}, passages}` and consume
`rankings[].logit`. These endpoint names are intentionally distinct.

### Submit embedding requests

Embedding requests must use role `query` for search queries and role `document` for passages or document images. The model's bundled chat template adds the corresponding `query:` or `passage:` prefix.

For local testing, forward the embedder Service:

```bash
kubectl --namespace daedalus-retriever port-forward service/nemotron-retriever-embedder 8000:8000
```

Then, in another terminal, submit a query embedding request using the NVIDIA-compatible payload:

```bash
export EMBEDDER_ENDPOINT=http://127.0.0.1:8000/v1/embeddings

curl --fail-with-body "$EMBEDDER_ENDPOINT" \
  --header 'Content-Type: application/json' \
  --data '{
    "model": "nvidia/llama-nemotron-embed-vl-1b-v2",
    "input": "How is AI used in robotics?",
    "input_type": "query"
  }'
```

The embedding vector is returned in `data[0].embedding`. To embed documents, send a list of strings with `input_type: document` (the adapter converts this to the model's `passage:` prefix):

```bash
curl --fail-with-body "$EMBEDDER_ENDPOINT" \
  --header 'Content-Type: application/json' \
  --data '{
    "model": "nvidia/llama-nemotron-embed-vl-1b-v2",
    "input": [
      "AI helps robots perceive their environment, plan actions, and operate autonomously.",
      "Bananas are rich in potassium."
    ],
    "input_type": "document"
  }'
```

### Submit a reranking request

Forward the reranker Service:

```bash
kubectl --namespace daedalus-retriever port-forward service/nemotron-retriever-reranker 8001:8000
```

Then, in another terminal, submit one query and its candidate documents:

```bash
export RERANKER_ENDPOINT=http://127.0.0.1:8001/v1/ranking

curl --fail-with-body "$RERANKER_ENDPOINT" \
  --header 'Content-Type: application/json' \
  --data '{
    "model": "nvidia/llama-nemotron-rerank-1b-v2",
    "query": "How is AI used in robotics?",
    "passages": [
      {"text": "AI helps robots perceive, plan, and act."},
      {"text": "Bananas are rich in potassium."}
    ],
    "top_n": 2
  }'
```

The adapter returns a `rankings` array ordered by relevance. Each result includes the source document `index` and its `logit`; `top_n` limits the number of returned candidates.

When using the MetalLB endpoints from the configuration section instead of port forwarding, set the endpoints to `http://192.168.1.12:8000/v1/embeddings` and `http://192.168.1.13:8000/v1/ranking`.

## Configuration

Common production overrides include:

```yaml
reranker:
  revision: "<model-commit-sha>"
  nodeSelector:
    nvidia.com/gpu.product: NVIDIA-L40S
  persistence:
    storageClass: fast-rwo

embedder:
  revision: "<model-commit-sha>"
  nodeSelector:
    nvidia.com/gpu.product: NVIDIA-L40S
  persistence:
    storageClass: fast-rwo
```

Set `embedder.maxModelLen: 2048` only when serving image-only or text-only inputs and the lower memory footprint is preferred; combined image-and-text inputs require the default `10240`. Add supported vLLM flags with `reranker.extraArgs` or `embedder.extraArgs`.

The query embedder defaults to two replicas with hard hostname spread, a
rolling update, a PDB, and independent ephemeral model caches. Pre-pull or warm
the pinned model revision before a production rollout so a cold replacement
does not consume the whole recovery budget. The optional reranker remains one
replica with `Recreate` until another eligible GPU is allocated; callers must
therefore treat reranking as fail-open quality enhancement. For any additional
multi-replica role, use independently mountable caches or a verified
`ReadWriteMany` volume.

Both models require `--trust-remote-code`, which executes code downloaded with the model. Pin `revision` to a reviewed Hugging Face commit for production and restrict access to these internal Services to trusted workloads, particularly because multimodal requests can cause the embedder to fetch caller-supplied image URLs.

### Cilium LAN exposure

The repository's `custom-values.yaml` exposes both APIs through the local
MetalLB `first-pool` while restricting the original client source to
`192.168.1.0/24`. The embedder receives `192.168.1.12` and the reranker receives
`192.168.1.13`. The fixed NodePorts remain available on each node as a secondary
access path.

Kubernetes only permits `loadBalancerSourceRanges` on `LoadBalancer` Services.
Cilium must have `bpf.lbSourceRangeAllTypes=true` so those ranges are enforced
on the allocated NodePorts. The relevant configuration is:

```yaml
reranker:
  service:
    type: LoadBalancer
    port: 8000
    clusterIP: 10.106.42.103
    nodePort: 30086
    annotations:
      service.cilium.io/src-ranges-policy: allow
      metallb.io/address-pool: first-pool
      metallb.io/loadBalancerIPs: 192.168.1.13
    allocateLoadBalancerNodePorts: true
    loadBalancerSourceRanges:
      - 192.168.1.0/24

embedder:
  service:
    type: LoadBalancer
    port: 8000
    clusterIP: 10.105.200.146
    nodePort: 30085
    annotations:
      service.cilium.io/src-ranges-policy: allow
      metallb.io/address-pool: first-pool
      metallb.io/loadBalancerIPs: 192.168.1.12
    allocateLoadBalancerNodePorts: true
    loadBalancerSourceRanges:
      - 192.168.1.0/24

networkPolicy:
  ciliumNodePort:
    enabled: true
  ingressCIDRs:
    reranker:
      - 192.168.1.0/24
    embedder:
      - 192.168.1.0/24
```

The Service source ranges apply before cross-node source NAT. The supplemental
`CiliumNetworkPolicy` then permits the `host` and `remote-node` identities that
Cilium presents to the destination pod after forwarding. The ordinary
`NetworkPolicy` continues to allow the configured namespaces and direct CIDR
sources. These two controls are intentionally paired: allowing `remote-node`
without pre-NAT Service filtering would make the unauthenticated APIs reachable
from every source that can reach a node address.

The chart renders a separate ingress policy for each endpoint. The CIDR lists
above are independent, so the embedder and reranker can be exposed to different
source networks. `networkPolicy.ingressNamespaces` continues to allow the
listed namespaces to reach both endpoints; set it to `[]` for CIDR-only access.
An endpoint with no namespace or CIDR sources denies all ingress. CIDR matching
uses the source address observed by the cluster network plugin, which can differ
from the client address when NodePort traffic is source-NATed.

The root Makefile applies `nemotron-retriever/custom-values.yaml` by default.
Override `RETRIEVER_VALUES_FILE=` to retain the chart's internal-only
`ClusterIP` defaults.

For more than one replica, the chart fails closed unless the update strategy is
`RollingUpdate` and the cache is disabled or explicitly marked
`persistence.multiReplicaSafe=true` after its cross-node mount behavior has
been verified. Multi-replica renders add a PDB and hard hostname spread.

## Validate

```bash
helm lint ./nemotron-retriever
helm template nemotron-retriever ./nemotron-retriever \
  --namespace daedalus-retriever > /tmp/nemotron-retriever.yaml
kubectl apply --dry-run=client --filename /tmp/nemotron-retriever.yaml
```
