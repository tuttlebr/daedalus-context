#!/usr/bin/env python3
"""Translate the NVIDIA NIM retrieval APIs used by Daedalus to vLLM."""

from __future__ import annotations

import json
import math
import os
import sys
import time
import urllib.error
import urllib.request
import uuid
from concurrent.futures import ThreadPoolExecutor
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any


UPSTREAM = os.getenv("UPSTREAM_URL", "http://127.0.0.1:8000").rstrip("/")
LISTEN_HOST = os.getenv("LISTEN_HOST", "0.0.0.0")
LISTEN_PORT = int(os.getenv("LISTEN_PORT", "8080"))
UPSTREAM_TIMEOUT_SECONDS = float(os.getenv("UPSTREAM_TIMEOUT_SECONDS", "300"))
MAX_REQUEST_BYTES = int(
    os.getenv("MAX_REQUEST_BYTES", str(64 * 1024 * 1024))
)
RANKING_BACKEND = os.getenv("RANKING_BACKEND", "rerank")
EMBEDDING_BACKEND = os.getenv("EMBEDDING_BACKEND", "embeddings")
EMBEDDING_MODEL = os.getenv("EMBEDDING_MODEL", "")
RERANKER_MODEL = os.getenv("RERANKER_MODEL", "nvidia/Nemotron-3.5-Rerank-8B-BF16")
# One shared pool bounds GPU concurrency across all incoming ranking requests.
CLASSIFY_EXECUTOR = ThreadPoolExecutor(
    max_workers=int(os.getenv("RANKING_CONCURRENCY", "4"))
)


class CompatibilityError(ValueError):
    """A caller supplied a payload that cannot be translated safely."""


class UpstreamResponseError(ValueError):
    """The classification server returned an invalid score or usage."""


def embedding_v2_request(payload: dict[str, Any]) -> dict[str, Any]:
    """Use the checkpoint's query/document chat template for every caller.

    Daedalus sends role-aware messages; NV-Ingest sends NIM input_type requests.
    Both must enter the SAME embedding space, including the prompt template.
    """
    role = payload.get("input_type", "document")
    inputs = payload.get("input")
    if "messages" in payload:
        messages = payload["messages"]
        if not isinstance(messages, list) or len(messages) != 1:
            raise CompatibilityError("embedding messages must contain one query or document")
        message = messages[0]
        if not isinstance(message, dict):
            raise CompatibilityError("embedding message must be an object")
        role = message.get("role")
        content = message.get("content")
        if isinstance(content, str):
            content = [{"type": "text", "text": content}]
        inputs = [{"content": content}]
    elif isinstance(inputs, str):
        inputs = [inputs]
    if role == "passage":
        role = "document"
    if role not in {"query", "document"}:
        raise CompatibilityError("embedding input_type must be query, document, or passage")
    if not isinstance(inputs, list) or not inputs:
        raise CompatibilityError("embedding input must be a non-empty string or list")
    normalized = []
    for item in inputs:
        if isinstance(item, str):
            item = {"content": [{"type": "text", "text": item}]}
        if not isinstance(item, dict) or not isinstance(item.get("content"), list) or not item["content"]:
            raise CompatibilityError("embedding inputs must contain text or multimodal content")
        normalized.append(item)
    if payload.get("encoding_format", "float") != "float":
        raise CompatibilityError("only float embeddings are supported")
    if payload.get("dimensions", 2048) != 2048:
        raise CompatibilityError("this retrieval contract requires 2048 dimensions")
    truncate = payload.get("truncate", "END")
    if truncate not in {"END", "NONE"}:
        raise CompatibilityError("truncate must be END or NONE")
    return {
        "model": payload.get("model") or EMBEDDING_MODEL,
        "input_type": role,
        "inputs": normalized,
        "embedding_types": ["float"],
        "truncate": truncate,
    }


def embed_via_v2(payload: dict[str, Any], authorization: str | None = None) -> dict[str, Any]:
    request_body = embedding_v2_request(payload)
    headers = {"Content-Type": "application/json"}
    if authorization:
        headers["Authorization"] = authorization
    request = urllib.request.Request(
        f"{UPSTREAM}/v2/embed", data=json.dumps(request_body).encode(), headers=headers,
    )
    with urllib.request.urlopen(request, timeout=UPSTREAM_TIMEOUT_SECONDS) as response:
        result = json.load(response)
    vectors = result.get("embeddings", {}).get("float")
    if not isinstance(vectors, list) or len(vectors) != len(request_body["inputs"]):
        raise UpstreamResponseError("embedding result count does not match input count")
    for vector in vectors:
        if not isinstance(vector, list) or len(vector) != 2048 or any(
            not isinstance(v, (int, float)) or isinstance(v, bool) or not math.isfinite(v)
            for v in vector
        ):
            raise UpstreamResponseError("upstream returned an invalid 2048-dimensional embedding")
        if not 0.99 <= sum(v * v for v in vector) <= 1.01:
            raise UpstreamResponseError("upstream embedding is not L2 normalized")
    return {
        "object": "list", "model": request_body["model"],
        "data": [{"object": "embedding", "index": i, "embedding": v} for i, v in enumerate(vectors)],
        "usage": result.get("usage", {}),
    }


def adapt_embedding_request(payload: dict[str, Any]) -> dict[str, Any]:
    """Convert NIM input_type prefixes to a native vLLM embedding request."""
    if "messages" in payload or "input_type" not in payload:
        return payload

    input_type = str(payload.get("input_type", "")).lower()
    prefixes = {
        "query": "query: ",
        "document": "passage: ",
        "passage": "passage: ",
    }
    if input_type not in prefixes:
        raise CompatibilityError(
            "embedding input_type must be query, document, or passage"
        )

    inputs = payload.get("input")
    was_scalar = isinstance(inputs, str)
    if was_scalar:
        text_inputs = [inputs]
    elif isinstance(inputs, list) and all(isinstance(item, str) for item in inputs):
        text_inputs = inputs
    else:
        raise CompatibilityError(
            "NIM-compatible embedding requests require string input"
        )

    adapted = {
        key: value
        for key, value in payload.items()
        if key not in {"input_type", "truncate"}
    }
    prefixed = [f"{prefixes[input_type]}{text}" for text in text_inputs]
    adapted["input"] = prefixed[0] if was_scalar else prefixed
    return adapted


def _text_value(value: Any, field: str) -> str:
    if isinstance(value, str):
        return value
    if isinstance(value, dict) and isinstance(value.get("text"), str):
        return value["text"]
    raise CompatibilityError(f"{field} must be a string or an object with text")


def adapt_ranking_request(payload: dict[str, Any]) -> dict[str, Any]:
    """Convert NVIDIA's /v1/ranking request to vLLM's /rerank request."""
    query = _text_value(payload.get("query"), "query")
    passages = payload.get("passages")
    if not isinstance(passages, list) or not passages:
        raise CompatibilityError("passages must be a non-empty list")

    documents = [
        _text_value(passage, f"passages[{index}]")
        for index, passage in enumerate(passages)
    ]
    top_n = payload.get("top_n", len(documents))
    if not isinstance(top_n, int) or isinstance(top_n, bool) or top_n < 1:
        raise CompatibilityError("top_n must be a positive integer")

    return {
        "model": payload.get("model"),
        "query": query,
        "documents": documents,
        "top_n": min(top_n, len(documents)),
    }


def adapt_ranking_response(payload: dict[str, Any]) -> dict[str, Any]:
    """Convert vLLM rerank results to the NVIDIA ranking response shape."""
    results = payload.get("results")
    if not isinstance(results, list):
        raise CompatibilityError("vLLM rerank response is missing results")

    rankings = []
    for result in results:
        if not isinstance(result, dict):
            raise CompatibilityError("vLLM rerank result must be an object")
        rankings.append(
            {
                "index": result.get("index"),
                # Existing Daedalus clients call this field logit and only use
                # it for ordering; vLLM exposes the score as relevance_score.
                "logit": result.get("relevance_score"),
            }
        )

    response: dict[str, Any] = {"rankings": rankings}
    if "usage" in payload:
        response["usage"] = payload["usage"]
    return response


def classification_requests(payload: dict[str, Any]) -> tuple[list[dict[str, Any]], list[str], int]:
    """Build the checkpoint's documented query/document classification inputs."""
    query = _text_value(payload.get("query"), "query")
    documents = payload.get("documents")
    if not isinstance(documents, list) or not documents:
        raise CompatibilityError("documents must be a non-empty list")
    texts = [_text_value(document, f"documents[{i}]") for i, document in enumerate(documents)]
    top_n = payload.get("top_n")
    if top_n is not None and (
        not isinstance(top_n, int) or isinstance(top_n, bool) or top_n < 0
    ):
        raise CompatibilityError("top_n must be a non-negative integer")
    model = payload.get("model") or RERANKER_MODEL
    if not isinstance(model, str):
        raise CompatibilityError("model must be a string")
    requests = []
    for text in texts:
        request = {
            "model": model,
            "messages": [
                {"role": "query", "content": [{"type": "text", "text": query}]},
                {"role": "document", "content": [{"type": "text", "text": text}]},
            ],
            "add_special_tokens": False,
            "use_activation": False,
        }
        for field in ("truncate_prompt_tokens", "user", "priority"):
            if field in payload:
                request[field] = payload[field]
        requests.append(request)
    return requests, texts, min(top_n or len(texts), len(texts))


def classification_score(payload: Any) -> tuple[float, dict[str, int]]:
    try:
        data = payload["data"]
        scores = data[0]["probs"]
        if len(data) != 1 or len(scores) != 1:
            raise UpstreamResponseError("expected one relevance logit per query/document pair")
        score = scores[0]
        if isinstance(score, bool) or not isinstance(score, (float, int)) or not math.isfinite(score):
            raise UpstreamResponseError("classification logit must be finite")
        usage = payload["usage"]
        counts = {key: usage[key] for key in ("prompt_tokens", "total_tokens")}
        if any(type(count) is not int or count < 0 for count in counts.values()):
            raise UpstreamResponseError("invalid classification token usage")
        return float(score), counts
    except (KeyError, IndexError, TypeError) as exc:
        raise UpstreamResponseError("invalid vLLM classification response") from exc


def classify_document(payload: dict[str, Any], authorization: str | None, deadline: float):
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        raise TimeoutError("reranking request deadline exceeded")
    headers = {"Accept": "application/json", "Content-Type": "application/json"}
    if authorization:
        headers["Authorization"] = authorization
    request = urllib.request.Request(
        f"{UPSTREAM}/classify", data=json.dumps(payload).encode("utf-8"), headers=headers
    )
    with urllib.request.urlopen(request, timeout=remaining) as response:
        try:
            return classification_score(json.load(response))
        except (UnicodeDecodeError, json.JSONDecodeError) as exc:
            raise UpstreamResponseError("classification response must be valid JSON") from exc


def rerank_via_classify(payload: dict[str, Any], authorization: str | None = None) -> dict[str, Any]:
    requests, documents, top_n = classification_requests(payload)
    deadline = time.monotonic() + UPSTREAM_TIMEOUT_SECONDS
    futures = [
        CLASSIFY_EXECUTOR.submit(classify_document, request, authorization, deadline)
        for request in requests
    ]
    results = []
    usage = {"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0}
    try:
        for index, future in enumerate(futures):
            score, counts = future.result(timeout=max(0, deadline - time.monotonic()))
            results.append({
                "index": index,
                "document": {"text": documents[index]},
                "relevance_score": score,
            })
            for key, count in counts.items():
                usage[key] += count
    finally:
        # Do not let queued work from a failed or timed-out request consume GPU time.
        for future in futures:
            future.cancel()
    # Stable sorting preserves the input order for tied scores.
    results.sort(key=lambda result: result["relevance_score"], reverse=True)
    return {
        "id": f"rerank-{uuid.uuid4().hex}",
        "model": requests[0]["model"],
        "results": results[:top_n],
        "usage": usage,
    }


class CompatibilityHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def _send(self, status: int, body: bytes, content_type: str) -> None:
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Connection", "close")
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(body)

    def _json_error(self, status: int, message: str) -> None:
        body = json.dumps(
            {
                "object": "error",
                "message": message,
                "type": "BadRequestError" if status < 500 else "ServerError",
            }
        ).encode("utf-8")
        self._send(status, body, "application/json")

    def _read_json(self) -> dict[str, Any]:
        try:
            content_length = int(self.headers.get("Content-Length", "0"))
        except ValueError as exc:
            raise CompatibilityError("invalid Content-Length") from exc
        if content_length < 1:
            raise CompatibilityError("request body is required")
        if content_length > MAX_REQUEST_BYTES:
            raise CompatibilityError("request body exceeds configured maximum")
        try:
            payload = json.loads(self.rfile.read(content_length))
        except (UnicodeDecodeError, json.JSONDecodeError) as exc:
            raise CompatibilityError("request body must be valid JSON") from exc
        if not isinstance(payload, dict):
            raise CompatibilityError("request body must be a JSON object")
        return payload

    def _proxy(
        self,
        path: str,
        body: bytes | None = None,
        transform_ranking_response: bool = False,
    ) -> None:
        headers = {"Accept": "application/json"}
        if body is not None:
            headers["Content-Type"] = "application/json"
        authorization = self.headers.get("Authorization")
        if authorization:
            headers["Authorization"] = authorization

        request = urllib.request.Request(
            f"{UPSTREAM}{path}",
            data=body,
            headers=headers,
            method=self.command,
        )
        try:
            with urllib.request.urlopen(
                request, timeout=UPSTREAM_TIMEOUT_SECONDS
            ) as response:
                response_body = response.read()
                status = response.status
                content_type = response.headers.get(
                    "Content-Type", "application/json"
                )
        except urllib.error.HTTPError as exc:
            response_body = exc.read()
            status = exc.code
            content_type = exc.headers.get("Content-Type", "application/json")
        except (urllib.error.URLError, TimeoutError) as exc:
            self._json_error(502, f"vLLM upstream request failed: {exc.reason if isinstance(exc, urllib.error.URLError) else exc}")
            return

        if transform_ranking_response and status < 400:
            try:
                response_payload = json.loads(response_body)
                response_body = json.dumps(
                    adapt_ranking_response(response_payload)
                ).encode("utf-8")
                content_type = "application/json"
            except (UnicodeDecodeError, json.JSONDecodeError, CompatibilityError) as exc:
                self._json_error(502, str(exc))
                return

        self._send(status, response_body, content_type)

    def do_GET(self) -> None:  # noqa: N802
        if self.path == "/adapter-health":
            self._send(200, b'{"status":"ok"}', "application/json")
            return
        self._proxy(self.path)

    def do_HEAD(self) -> None:  # noqa: N802
        if self.path == "/adapter-health":
            self._send(200, b"", "application/json")
            return
        self._proxy(self.path)

    def do_POST(self) -> None:  # noqa: N802
        try:
            payload = self._read_json()
            upstream_path = self.path
            transform_response = False
            if self.path == "/v1/embeddings":
                if EMBEDDING_BACKEND == "v2":
                    response = embed_via_v2(payload, self.headers.get("Authorization"))
                    self._send(200, json.dumps(response).encode(), "application/json")
                    return
                payload = adapt_embedding_request(payload)
            elif self.path == "/v1/ranking":
                payload = adapt_ranking_request(payload)
                upstream_path = "/rerank"
                transform_response = True
            if RANKING_BACKEND == "classify" and upstream_path in {"/rerank", "/v1/rerank", "/v2/rerank"}:
                response = rerank_via_classify(payload, self.headers.get("Authorization"))
                if transform_response:
                    response = adapt_ranking_response(response)
                self._send(200, json.dumps(response).encode("utf-8"), "application/json")
                return
            body = json.dumps(payload, separators=(",", ":")).encode("utf-8")
        except CompatibilityError as exc:
            self._json_error(400, str(exc))
            return
        except urllib.error.HTTPError as exc:
            self._send(exc.code, exc.read(), exc.headers.get("Content-Type", "application/json"))
            return
        except (urllib.error.URLError, TimeoutError, UpstreamResponseError) as exc:
            self._json_error(502, f"vLLM retrieval request failed: {exc}")
            return

        self._proxy(upstream_path, body, transform_response)

    def log_message(self, fmt: str, *args: Any) -> None:
        print(
            f'{self.client_address[0]} - {fmt % args}',
            file=sys.stderr,
            flush=True,
        )


def main() -> None:
    server = ThreadingHTTPServer((LISTEN_HOST, LISTEN_PORT), CompatibilityHandler)
    server.daemon_threads = True
    print(
        f"NIM compatibility adapter listening on {LISTEN_HOST}:{LISTEN_PORT}; upstream={UPSTREAM}",
        flush=True,
    )
    server.serve_forever()


if __name__ == "__main__":
    main()
