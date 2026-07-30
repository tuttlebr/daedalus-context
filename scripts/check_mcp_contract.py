#!/usr/bin/env python3
"""Exercise authenticated MCP initialize and tools/list over streamable HTTP."""

from __future__ import annotations

import json
import os
import urllib.error
import urllib.request


def decode_response(payload: bytes, content_type: str) -> dict:
    text = payload.decode("utf-8")
    if "text/event-stream" in content_type:
        data_lines = [
            line.removeprefix("data:").strip()
            for line in text.splitlines()
            if line.startswith("data:")
        ]
        if not data_lines:
            raise RuntimeError("MCP SSE response contained no data event")
        text = data_lines[-1]
    value = json.loads(text)
    if not isinstance(value, dict):
        raise RuntimeError("MCP response was not an object")
    if "error" in value:
        raise RuntimeError(f"MCP returned JSON-RPC error: {value['error']}")
    return value


def request(url: str, token: str, method: str, params: dict, request_id: int, session: str = ""):
    body = json.dumps(
        {"jsonrpc": "2.0", "id": request_id, "method": method, "params": params}
    ).encode("utf-8")
    headers = {
        "Accept": "application/json, text/event-stream",
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/json",
    }
    if session:
        headers["Mcp-Session-Id"] = session
    response = urllib.request.urlopen(
        urllib.request.Request(url, data=body, headers=headers, method="POST"),
        timeout=20,
    )
    try:
        payload = decode_response(response.read(), response.headers.get("Content-Type", ""))
        return payload, response.headers.get("Mcp-Session-Id", session)
    finally:
        response.close()


def notify_initialized(url: str, token: str, session: str) -> None:
    body = json.dumps(
        {"jsonrpc": "2.0", "method": "notifications/initialized", "params": {}}
    ).encode("utf-8")
    headers = {
        "Accept": "application/json, text/event-stream",
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/json",
    }
    if session:
        headers["Mcp-Session-Id"] = session
    response = urllib.request.urlopen(
        urllib.request.Request(url, data=body, headers=headers, method="POST"),
        timeout=20,
    )
    response.close()


def main() -> None:
    url = os.environ["MCP_URL"]
    token_name = os.environ["MCP_TOKEN_ENV"]
    token = os.environ.get(token_name, "")
    if not token:
        raise RuntimeError(f"required MCP token environment variable is empty: {token_name}")

    initialized, session = request(
        url,
        token,
        "initialize",
        {
            "protocolVersion": "2025-03-26",
            "capabilities": {},
            "clientInfo": {"name": "daedalus-context-health", "version": "1"},
        },
        1,
    )
    result = initialized.get("result")
    if not isinstance(result, dict) or not isinstance(result.get("serverInfo"), dict):
        raise RuntimeError("initialize response omitted result.serverInfo")

    notify_initialized(url, token, session)
    listed, _session = request(url, token, "tools/list", {}, 2, session)
    tools = listed.get("result", {}).get("tools")
    if not isinstance(tools, list) or not tools:
        raise RuntimeError("tools/list returned no tools")
    names = [tool.get("name") for tool in tools]
    if any(not isinstance(name, str) or not name for name in names):
        raise RuntimeError("tools/list contained an invalid tool name")
    if len(names) != len(set(names)):
        raise RuntimeError("tools/list contained duplicate tool names")
    print(json.dumps({"server": result["serverInfo"], "toolCount": len(names)}, sort_keys=True))


if __name__ == "__main__":
    try:
        main()
    except (KeyError, RuntimeError, urllib.error.URLError, json.JSONDecodeError) as error:
        raise SystemExit(f"MCP contract check failed: {error}") from error
