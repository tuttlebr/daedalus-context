#!/usr/bin/env python3
"""Translate the NVIDIA NIM retrieval APIs used by Daedalus to vLLM."""

from __future__ import annotations

import json
import os
import sys
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any


UPSTREAM = os.getenv("UPSTREAM_URL", "http://127.0.0.1:8000").rstrip("/")
LISTEN_HOST = os.getenv("LISTEN_HOST", "0.0.0.0")
LISTEN_PORT = int(os.getenv("LISTEN_PORT", "8080"))
UPSTREAM_TIMEOUT_SECONDS = float(os.getenv("UPSTREAM_TIMEOUT_SECONDS", "300"))
MAX_REQUEST_BYTES = int(
    os.getenv("MAX_REQUEST_BYTES", str(64 * 1024 * 1024))
)


class CompatibilityError(ValueError):
    """A caller supplied a payload that cannot be translated safely."""


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
                payload = adapt_embedding_request(payload)
            elif self.path == "/v1/ranking":
                payload = adapt_ranking_request(payload)
                upstream_path = "/rerank"
                transform_response = True
            body = json.dumps(payload, separators=(",", ":")).encode("utf-8")
        except CompatibilityError as exc:
            self._json_error(400, str(exc))
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
