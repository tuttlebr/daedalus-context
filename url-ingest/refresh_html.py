"""Materialize public NVIDIA component content that is absent from HTML text.

Only known component schemas are supported. Do not execute JavaScript or turn
an arbitrary empty page into a successful title-only document.
"""
from __future__ import annotations

import html
import json
from html.parser import HTMLParser
from urllib.parse import urlparse, parse_qs


class Components(HTMLParser):
    def __init__(self):
        super().__init__()
        self.items = []

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        name = attrs.get("data-react-class")
        if name in {"LearningPathway", "LearningPathwayCollection", "NestedOptionSelector"}:
            self.items.append((name, json.loads(attrs["data-react-props"])))


def public_response(client, url):
    parsed = urlparse(url)
    if parsed.scheme != "https" or parsed.hostname not in {
        "developer.nvidia.com", "developer.nvidia.cn", "developer.download.nvidia.com"
    } or parsed.username or parsed.password:
        raise ValueError("unsupported NVIDIA component content URL")
    response = client.get(url, timeout=(10, 60))
    response.raise_for_status()
    if not response.content:
        raise ValueError("empty NVIDIA component content")
    return response


def materialize(client, url: str, body: bytes) -> bytes | None:
    host = urlparse(url).hostname
    if host == "blogs.nvidia.com" and urlparse(url).path == "/wp-json/wp/v2/posts":
        post = json.loads(body)
        slug = parse_qs(urlparse(url).query).get("slug", [])
        if len(slug) != 1 or not isinstance(post, list) or len(post) != 1 or post[0].get("slug") != slug[0]:
            raise ValueError("WordPress response did not identify the requested article")
        post = post[0]
        content = post["content"]
        if content.get("protected") or not content.get("rendered", "").strip():
            raise ValueError("WordPress post has no public article content")
        return ("<html><body><article><h1>" + post["title"]["rendered"] + "</h1>"
                + content["rendered"] + "</article></body></html>").encode()
    if host not in {"developer.nvidia.com", "developer.nvidia.cn"} or b"data-react-props" not in body:
        return None
    parser = Components()
    parser.feed(body.decode("utf-8"))
    sections = []
    for name, props in parser.items:
        if name == "NestedOptionSelector":
            releases = props.get("pageData", {}).get("releases", {})
            details = list(dict.fromkeys(item["details"] for item in releases.values() if item.get("details")))
            if not details:
                raise ValueError("download component has no installation details")
            sections.extend(details)
            continue
        field, endpoint = (("pathwayID", "learning_pathways") if name == "LearningPathway"
                           else ("collectionID", "learning_pathway_collections"))
        identifier = props[field]
        if type(identifier) is not int or identifier < 1:
            raise ValueError("invalid learning pathway identifier")
        data = public_response(client, f"https://{host}/{endpoint}/{identifier}.json").json()
        if name == "LearningPathwayCollection":
            sections.append(f'<h1>{html.escape(data["name"])}</h1><p>{html.escape(data["description"])}</p>')
            for item in data["learning_pathways"]:
                sections.append(f'<h2>{html.escape(item["name"])}</h2><p>{html.escape(item["description"])}</p>')
        else:
            definition = data["definition"]
            sections.append(f'<h1>{html.escape(props["title"])}</h1><p>{html.escape(definition["description"])}</p>')
            files = 0
            for step in definition["steps"]:
                sections.append(f'<h2>{html.escape(step["title"])}</h2>')
                for key in ("file", "footer"):
                    if step.get(key):
                        text = public_response(client, step[key]).content.decode("utf-8")
                        sections.append(f"<pre>{html.escape(text)}</pre>")
                        files += 1
            if not files:
                raise ValueError("learning pathway has no lesson content")
    if not sections:
        return None
    return ("<html><body><article>" + "\n".join(sections) + "</article></body></html>").encode()
