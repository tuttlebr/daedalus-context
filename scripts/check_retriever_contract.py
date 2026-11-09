#!/usr/bin/env python3
"""Exercise the native Daedalus and NVIDIA-compatible retriever contracts."""

import json
import math
import os
import urllib.request


def post_json(url: str, payload: dict) -> dict:
    request = urllib.request.Request(
        url,
        data=json.dumps(payload).encode("utf-8"),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(request, timeout=120) as response:
        return json.load(response)


embedding_url = os.environ["EMBEDDING_URL"]
native_reranker_url = os.environ["NATIVE_RERANKER_URL"]
nim_reranker_url = os.environ["NIM_RERANKER_URL"]
embedding_model = os.environ["EMBEDDING_MODEL"]
reranker_model = os.environ["RERANKER_MODEL"]
expected_dimension = int(os.environ.get("EXPECTED_DIMENSION", "2048"))

# Match both actual consumers: the Agent sends role-aware messages and
# NV-Ingest sends input/input_type. The model template owns role prefixes.
probe_text = "Daedalus retriever health probe"
for label, request in (
    ("native", {"messages": [{"role": "query", "content": [
        {"type": "text", "text": probe_text}
    ]}]}),
    ("nim", {"input": probe_text, "input_type": "query"}),
):
    embedding = post_json(embedding_url, {"model": embedding_model, **request})
    data = embedding.get("data")
    if not isinstance(data, list) or len(data) != 1 or not isinstance(data[0], dict):
        raise RuntimeError(f"{label} embedding response must contain one result")
    vector = data[0].get("embedding")
    if not isinstance(vector, list) or len(vector) != expected_dimension:
        raise RuntimeError(f"{label} embedding dimension mismatch: expected {expected_dimension}")
    if any(
        not isinstance(value, (int, float)) or isinstance(value, bool)
        or not math.isfinite(value) for value in vector
    ):
        raise RuntimeError(f"{label} embedding contains invalid values")
    if not 0.99 <= sum(value * value for value in vector) <= 1.01:
        raise RuntimeError(f"{label} embedding is not L2 normalized")

native_ranking = post_json(
    native_reranker_url,
    {
        "model": reranker_model,
        "query": "Which document describes a health probe?",
        "documents": ["A health probe checks a service.", "Bananas contain potassium."],
        "top_n": 2,
    },
)
if not native_ranking.get("results"):
    raise RuntimeError("native /rerank response has no results")
if "relevance_score" not in native_ranking["results"][0]:
    raise RuntimeError("native /rerank response is missing relevance_score")

nim_ranking = post_json(
    nim_reranker_url,
    {
        "model": reranker_model,
        "query": {"text": "Which passage describes a health probe?"},
        "passages": [
            {"text": "A health probe checks a service."},
            {"text": "Bananas contain potassium."},
        ],
        "top_n": 2,
    },
)
if not nim_ranking.get("rankings"):
    raise RuntimeError("NIM /v1/ranking response has no rankings")
if "logit" not in nim_ranking["rankings"][0]:
    raise RuntimeError("NIM /v1/ranking response is missing logit")

print(
    json.dumps(
        {
            "embeddingDimension": len(vector),
            "nativeEmbeddings": "ok",
            "nimEmbeddings": "ok",
            "nativeReranker": "ok",
            "nimReranker": "ok",
        },
        sort_keys=True,
    )
)
