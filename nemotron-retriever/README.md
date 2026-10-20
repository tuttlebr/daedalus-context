# Nemotron Retriever vLLM Helm chart

This chart deploys two independent, GPU-backed vLLM servers:

| Component | Model | API |
| --- | --- | --- |
| `embedder` | `nvidia/llama-nemotron-embed-vl-1b-v2` | `POST /v1/embeddings` |
| `reranker` | `nvidia/Nemotron-3.5-Rerank-8B-BF16` | native `POST /rerank`; NIM-compatible `POST /v1/ranking` |

Both Services are `ClusterIP` by default and can be exposed independently through a source-restricted node-address endpoint. They expose the compatibility adapter on port 8000. The embedder retains `vllm/vllm-openai:v0.17.0`; the reranker overrides `reranker.image` with `vllm/vllm-openai:v0.32.0`, following [NVIDIA's serving example](https://huggingface.co/nvidia/Nemotron-3.5-Rerank-8B-BF16/blob/39aa01525864f736e63f1d5742fc4cf59e754abd/README.md#vllm-usage). Each deployment requests one NVIDIA GPU. The reranker uses a persistent Hugging Face cache; the embedder uses an independent ephemeral cache by default.

The reranker pins checkpoint `39aa01525864f736e63f1d5742fc4cf59e754abd` and its config SHA-256 in the chart, `stack/rag-schema.yaml`, and `stack/components.lock.yaml`. Its unmodified upstream `chat_template.jinja` is vendored as `files/nemotron-rerank.jinja`, with upstream license and attribution files in `files/nemotron-rerank-notices/`. A render test checks its hash and control-token escaping. Set `reranker.chatTemplate` only to deliberately override that template.

The new checkpoint uses OpenMDW-1.1 with a Ministral base under Apache-2.0. Consult each model repository's license and notices for its applicable terms.

## Prerequisites

- Kubernetes 1.25 or newer.
- Helm 3.
- NVIDIA drivers, container runtime integration, and the NVIDIA device plugin or GPU Operator.
- At least two schedulable GPUs for the default simultaneous deployment. Disable one component or schedule them separately if fewer GPUs are available.
- Egress to Hugging Face during the initial model download. Image URL embedding also requires egress to each referenced image host.
- A default StorageClass, or an explicit `persistence.storageClass` for each model.

The model repositories currently permit anonymous downloads. If a token is needed, create it outside Helm so it is not stored in release values:

```bash
kubectl --namespace daedalus create secret generic huggingface-token \
  --from-literal=token='<HUGGING_FACE_TOKEN>'
```

Then set `huggingFace.existingSecret=huggingface-token`. Do not commit the token or put it directly in a values file.

## Install

From the repository root:

```bash
helm upgrade --install nemotron-retriever ./nemotron-retriever \
  --namespace daedalus \
  --create-namespace \
  --set huggingFace.existingSecret=huggingface-token \
  --wait \
  --timeout 30m
```

Omit the `huggingFace.existingSecret` setting for anonymous downloads. Resources use the Helm release namespace by default. `namespaceOverride` exists only for compatibility with an intentionally split legacy release and should remain empty for new deployments.

Check rollout and service health:

```bash
kubectl --namespace daedalus get deployments,pods,services,pvc
helm --namespace daedalus test nemotron-retriever
```

## Endpoints

Inside the cluster, the default service URLs are:

```text
http://nemotron-retriever-embedder.daedalus.svc.cluster.local:8000/v1/embeddings
http://nemotron-retriever-reranker.daedalus.svc.cluster.local:8000/rerank
http://nemotron-retriever-reranker.daedalus.svc.cluster.local:8000/v1/ranking
```

Daedalus uses native `/rerank` with `{query, documents, top_n}` and consumes
`results[].relevance_score`. NV-Ingest/NIM-compatible callers use
`/v1/ranking` with `{query: {text}, passages}` and consume
`rankings[].logit`. These endpoint names are intentionally distinct.

With `reranker.rankingBackend: classify`, both text ranking APIs call vLLM
`/classify` once per query/document pair using structured `query` and `document`
messages. BF16, mean pooling, `use_activation: false`, and
`add_special_tokens: false` match NVIDIA's example. The adapter sorts the raw
logits, preserves original document indices, sums token usage for every scored
pair, and applies `top_n` after scoring. It limits classification concurrency to
four across requests and returns an error if any pair fails. Scores may be
negative; larger values indicate greater relevance.

The old model ID `nvidia/llama-nemotron-rerank-1b-v2` remains a served alias for
existing clients. It selects the new checkpoint. New clients should send
`nvidia/Nemotron-3.5-Rerank-8B-BF16`, or omit `model` on `/rerank`. Keep the
compatibility adapter enabled to retain these contracts. Multimodal callers
can use the proxied `/classify` endpoint with NVIDIA's documented image content
blocks; the existing `/rerank` and `/v1/ranking` adapters accept text documents.

### Submit embedding requests

Embedding requests must use role `query` for search queries and role `document` for passages or document images. The model's bundled chat template adds the corresponding `query:` or `passage:` prefix.

For local testing, forward the embedder Service:

```bash
kubectl --namespace daedalus port-forward service/nemotron-retriever-embedder 8000:8000
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
kubectl --namespace daedalus port-forward service/nemotron-retriever-reranker 8001:8000
```

Then, in another terminal, submit one query and its candidate documents:

```bash
export RERANKER_ENDPOINT=http://127.0.0.1:8001/v1/ranking

curl --fail-with-body "$RERANKER_ENDPOINT" \
  --header 'Content-Type: application/json' \
  --data '{
    "model": "nvidia/Nemotron-3.5-Rerank-8B-BF16",
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

Run the standalone embedder smoke test against the default MetalLB endpoint:

```bash
./nemotron-retriever/smoke-test.sh
```

Set `EMBED_BASE_URL`, `EMBED_MODEL`, `EMBED_API_KEY`, or
`EMBED_TIMEOUT_SECONDS` to override its defaults.

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

The reranker serves up to 32,768 tokens per query/document pair, including image
tokens for direct `/classify` calls. Its BF16 weights total 16,762,318,848 bytes;
the chart requests a 40 GiB cache, 16 GiB host RAM (32 GiB limit), and one GPU.
GPU memory must also accommodate activations and the vision encoder. The
cluster's 48 GB L40S GPUs are the intended deployment target; confirm real
inference and memory usage during rollout.

For an existing installation, check that the cache PVC can grow from 10 GiB to
40 GiB before running the root `make update COMPONENTS=retriever` workflow.
`persistence.existingClaim` must provide that capacity when set. Keep the old
checkpoint cached for rollback; do not delete the PVC. Rollback values should
retain the expanded PVC size because Kubernetes cannot shrink it. Preserve
the previous Helm values/manifests, and verify both ranking APIs from Daedalus
and a non-node LAN client after rollout. Service names, port 8000, NodePorts
30085/30086, and LAN access settings are unchanged by this model migration.

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

The repository's `custom-values.yaml` exposes both APIs as `NodePort` Services
with fixed ports:

| Service | NodePort | Service port |
| --- | --- | --- |
| `nemotron-retriever-embedder` | `30085` | `8000` |
| `nemotron-retriever-reranker` | `30086` | `8000` |

Connect to `http://<node-IP>:30085/v1/embeddings` or
`http://<node-IP>:30086/rerank` (NIM-compatible: `/v1/ranking`). Both Services
use `externalTrafficPolicy: Cluster`, so any node can forward requests to a
ready backend, even when the pod runs on another node. The enabled supplemental
`CiliumNetworkPolicy` permits `host` and `remote-node` traffic only to each
retriever's API port, allowing cross-node forwarding after source NAT.

```yaml
embedder:
  service:
    type: NodePort
    port: 8000
    externalTrafficPolicy: Cluster
    nodePort: 30085

reranker:
  service:
    type: NodePort
    port: 8000
    externalTrafficPolicy: Cluster
    nodePort: 30086

networkPolicy:
  ciliumNodePort:
    enabled: true
  ingressCIDRs:
    embedder:
      - 192.168.1.0/24
    reranker:
      - 192.168.1.0/24
```

The chart renders a separate ingress policy for each endpoint. The CIDR lists
allow direct LAN traffic, and `networkPolicy.ingressNamespaces` continues to
allow the listed namespaces. With cross-node source NAT, Cilium sees the node
identity instead of the original client address; the supplemental forwarding
policy permits that traffic. Consequently, these CIDRs do not enforce a strict
original-client allowlist on forwarded NodePort traffic. If one is required,
enforce it before forwarding at the network boundary or use the chart's
source-restricted LoadBalancer profile.

The root Makefile applies `nemotron-retriever/values-daedalus.yaml` by default;
it carries the same NodePorts and LAN ingress settings alongside production
scheduling settings. Keep its exposure settings aligned with
`custom-values.yaml`. The chart's base `values.yaml` retains internal-only
`ClusterIP` defaults.

For more than one replica, the chart fails closed unless the update strategy is
`RollingUpdate` and the cache is disabled or explicitly marked
`persistence.multiReplicaSafe=true` after its cross-node mount behavior has
been verified. Multi-replica renders add a PDB and hard hostname spread.

## Validate

```bash
helm lint ./nemotron-retriever
helm template nemotron-retriever ./nemotron-retriever \
  --namespace daedalus > /tmp/nemotron-retriever.yaml
kubectl apply --dry-run=client --filename /tmp/nemotron-retriever.yaml
```
