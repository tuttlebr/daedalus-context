"""Pinned local chunking and explicit document embeddings for extracted records.

NV-Ingest performs extraction. This worker owns the tokenizer revision, chunk
boundaries, embedding model, validation, and commit. Server defaults cannot
silently change the vector contract or discard oversized unchunked records.
"""
from __future__ import annotations

import copy
import os
from functools import lru_cache

import requests
from requests.adapters import HTTPAdapter
from urllib3.util.retry import Retry


@lru_cache(maxsize=2)
def tokenizer(model: str, revision: str):
    from huggingface_hub import hf_hub_download
    from tokenizers import Tokenizer
    path = hf_hub_download(model, "tokenizer.json", revision=revision,
                           token=os.getenv("HF_ACCESS_TOKEN") or None)
    encoder = Tokenizer.from_file(path)
    encoder.no_truncation()
    encoder.no_padding()
    return encoder


def chunks(text: str, encoder, size: int, overlap: int):
    if not 0 <= overlap < size:
        raise ValueError("chunk overlap must be smaller than size")
    encoded = encoder.encode(text, add_special_tokens=False)
    offsets = encoded.offsets
    start = 0
    while start < len(offsets):
        end = min(start + size, len(offsets))
        piece = text[offsets[start][0]:offsets[end - 1][1]]
        if piece.strip():
            if len(piece.encode()) > 65535:
                raise ValueError("tokenized chunk exceeds Milvus text limit")
            yield piece
        if end == len(offsets):
            break
        start = end - overlap


def extracted_text(element: dict) -> str:
    metadata = element.get("metadata", {})
    kind = element.get("document_type")
    if metadata.get("error") or element.get("error"):
        raise ValueError("NV-Ingest returned an extraction error")
    if kind == "text":
        return metadata.get("content") or ""
    if kind == "structured":
        return metadata.get("table_metadata", {}).get("table_content") or ""
    if kind == "image":
        image = metadata.get("image_metadata", {})
        return image.get("text") or image.get("caption") or ""
    return ""


def embed_records(results: list, contract: dict) -> list:
    model = contract["embedding"]
    config = contract["chunking"]
    encoder = tokenizer(model["model"], model["revision"])
    records = []
    for group in results:
        for element in ([group] if isinstance(group, dict) else group or []):
            text = extracted_text(element)
            if not text.strip():
                continue
            original = element["metadata"]
            for index, chunk in enumerate(chunks(text, encoder, config["size"], config["overlap"])):
                records.append({"document_type": "text", "metadata": {
                    "content": chunk,
                    "source_metadata": copy.deepcopy(original.get("source_metadata", {})),
                    "content_metadata": {**copy.deepcopy(original.get("content_metadata", {})),
                                         "chunk_index": index, "original_document_type": element["document_type"]},
                }})
    endpoint = os.environ["EMBEDDING_NIM_ENDPOINT"].rstrip("/") + "/embeddings"
    retry = Retry(total=3, backoff_factor=1, status_forcelist=[408, 429, 500, 502, 503, 504],
                  allowed_methods=["POST"], respect_retry_after_header=True)
    with requests.Session() as http:
        http.mount("http://", HTTPAdapter(max_retries=retry))
        http.mount("https://", HTTPAdapter(max_retries=retry))
        for start in range(0, len(records), 16):
            batch = records[start:start + 16]
            response = http.post(endpoint, json={
                "model": model["model"], "input_type": "passage", "truncate": "NONE",
                "dimensions": model["dimension"], "encoding_format": "float",
                "input": [r["metadata"]["content"] for r in batch],
            }, timeout=(10, 180))
            response.raise_for_status()
            data = response.json().get("data", [])
            if len(data) != len(batch) or {r.get("index") for r in data} != set(range(len(batch))):
                raise ValueError("embedding response did not account for every chunk")
            for item in data:
                batch[item["index"]]["metadata"]["embedding"] = item["embedding"]
    return [records]
