"""Conditional source discovery and downloads with explicit failure accounting."""
from __future__ import annotations

import gzip
import time
import xml.etree.ElementTree as ET
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import urldefrag, urlparse

import requests
from requests.adapters import HTTPAdapter
from urllib3.util.retry import Retry

from refresh_state import digest


class SourceRemoved(Exception):
    pass


def session() -> requests.Session:
    client = requests.Session()
    client.headers["User-Agent"] = "DaedalusContextRefresh/2.0"
    retry = Retry(total=3, backoff_factor=1, status_forcelist=[408, 429, 500, 502, 503, 504],
                  allowed_methods=["GET"], respect_retry_after_header=True)
    client.mount("https://", HTTPAdapter(max_retries=retry))
    client.mount("http://", HTTPAdapter(max_retries=retry))
    return client


def normalize_url(value: str) -> str:
    value = urldefrag(value.strip())[0]
    parsed = urlparse(value)
    if parsed.scheme not in {"http", "https"} or not parsed.hostname or parsed.username or parsed.password:
        raise ValueError("source must be an HTTP(S) URL without embedded credentials")
    return value


def discover(client: requests.Session, entries: list[dict]) -> tuple[dict[str, dict], list[dict]]:
    urls, failures, seen = {}, [], set()
    queue = list(entries)
    while queue:
        entry = queue.pop()
        url = normalize_url(entry["url"])
        if entry["kind"] == "url":
            urls[url] = entry
            continue
        if url in seen:
            continue
        seen.add(url)
        try:
            response = client.get(url, timeout=(10, 60))
            response.raise_for_status()
            body = response.content
            if body.startswith(b"\x1f\x8b"):
                body = gzip.decompress(body)
            root = ET.fromstring(body)
            kind = root.tag.rsplit("}", 1)[-1]
            if kind not in {"sitemapindex", "urlset"}:
                raise ValueError("response is not a sitemap")
            for child in root:
                values = {node.tag.rsplit("}", 1)[-1]: node.text for node in child}
                if not values.get("loc"):
                    raise ValueError("sitemap entry lacks loc")
                item = {**entry, "url": normalize_url(values["loc"])}
                if kind == "sitemapindex":
                    queue.append(item)
                else:
                    # Age filters limit initial discovery, but previously known
                    # URLs remain in the reconciler and are still revalidated.
                    if entry.get("max_age_days") and values.get("lastmod"):
                        try:
                            modified = datetime.fromisoformat(values["lastmod"].replace("Z", "+00:00"))
                            if modified.tzinfo is None:
                                modified = modified.replace(tzinfo=timezone.utc)
                            if time.time() - modified.timestamp() > entry["max_age_days"] * 86400:
                                continue
                        except ValueError:
                            pass
                    item["kind"] = "url"
                    urls[item["url"]] = item
        except (requests.RequestException, ET.ParseError, ValueError, OSError) as exc:
            failures.append({"source": url, "error": str(exc)[:1000], "error_type": type(exc).__name__})
    return urls, failures


def fetch(client: requests.Session, url: str, previous: dict, cache: Path,
          *, try_markdown: bool = False) -> tuple[Path, dict]:
    cached = Path(previous["cache_path"]) if previous.get("cache_path") else None
    headers = {}
    if cached and cached.is_file():
        if previous.get("etag"):
            headers["If-None-Match"] = previous["etag"]
        if previous.get("last_modified"):
            headers["If-Modified-Since"] = previous["last_modified"]
    effective_url = previous.get("fetch_url", url)
    if try_markdown and not previous.get("fetch_url") and url.endswith(".html"):
        effective_url = url[:-5] + ".md"
    response = client.get(effective_url, headers=headers, timeout=(10, 60))
    if effective_url != url and response.status_code in {404, 410, 403}:
        effective_url = url
        response = client.get(url, timeout=(10, 60))
    if response.status_code == 304:
        if not cached or not cached.is_file():
            raise ValueError("304 received without cached source")
        return cached, {**previous, "last_checked": time.time(), "missing_since": 0}
    if response.status_code in {404, 410}:
        raise SourceRemoved(f"HTTP {response.status_code}")
    response.raise_for_status()
    body = response.content
    if not body:
        raise ValueError("source returned an empty body")
    if len(body) > 64 * 1024 * 1024:
        raise ValueError("source exceeds the 64 MiB download limit")
    content_hash = digest(body)
    extension = ".pdf" if body.startswith(b"%PDF-") else ".md" if effective_url.endswith(".md") else ".html"
    # Preserve immutable bytes while extraction is in flight. A changed source
    # cannot accidentally be marked complete for an earlier download.
    path = cache / digest(url) / (content_hash + extension)
    path.parent.mkdir(parents=True, exist_ok=True)
    if not path.exists():
        temp = path.with_suffix(path.suffix + ".tmp")
        with temp.open("wb") as stream:
            import os
            stream.write(body)
            stream.flush()
            os.fsync(stream.fileno())
        temp.replace(path)
    return path, {**previous, "cache_path": str(path), "sha256": content_hash,
                  "etag": response.headers.get("ETag", ""),
                  "last_modified": response.headers.get("Last-Modified", ""),
                  "fetch_url": effective_url, "last_checked": time.time(), "missing_since": 0}
