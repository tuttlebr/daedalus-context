#!/usr/bin/env python3
"""Exercise the native Daedalus and NVIDIA-compatible retriever contracts."""

import json
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

embedding = post_json(
    embedding_url,
    {
        "model": embedding_model,
        "input": "query: Daedalus retriever health probe",
        "input_type": "query",
    },
)
vector = embedding["data"][0]["embedding"]
if len(vector) != expected_dimension:
    raise RuntimeError(
        f"embedding dimension mismatch: expected {expected_dimension}, got {len(vector)}"
    )

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
            "nativeReranker": "ok",
            "nimReranker": "ok",
        },
        sort_keys=True,
    )
)
