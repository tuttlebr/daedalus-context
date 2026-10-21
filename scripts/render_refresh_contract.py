#!/usr/bin/env python3
"""Render the ingestion/consumer contract from the root retrieval inventory."""
import argparse
import json
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[1]


def render() -> dict:
    rag = yaml.safe_load((ROOT / "stack/rag-schema.yaml").read_text())
    collections = yaml.safe_load((ROOT / "stack/collections.yaml").read_text())
    values = yaml.safe_load((ROOT / "helm/url-ingest/values.yaml").read_text())
    if rag["schemaVersion"] != collections["schemaVersion"]:
        raise ValueError("collection and model contract versions disagree")
    result = {
        "schema_version": rag["schemaVersion"], "generation": rag["schemaVersion"],
        "milvus_database": rag["schemaVersion"],
        "active_database": rag.get("database", "default"),
        "embedding": {k: rag["embedding"][k] for k in ("model", "revision", "configSha256", "dimension")},
        "chunking": {"size": 1024, "overlap": 64},
        "deletion_grace_hours": 48, "collections": {},
    }
    for item in collections["collections"]:
        alias = item["alias"]
        settings = values["collections"][alias]
        if (item["chunkSize"], item["chunkOverlap"]) != (result["chunking"]["size"], result["chunking"]["overlap"]):
            raise ValueError(f"chunking contract mismatch: {alias}")
        urls = []
        for crawler in settings.get("crawlers", []):
            for line in (ROOT / "url-ingest/files" / crawler["file"]).read_text().splitlines():
                line = line.strip()
                if line and not line.startswith("#"):
                    urls.append({"url": line, "kind": crawler["inputType"],
                                 "max_age_days": crawler.get("maxAgeDays"),
                                 "try_markdown": crawler.get("tryMarkdown", False)})
        result["collections"][alias] = {
            "urls": urls, "pdfs": "pdf" in item["sources"],
            "schedule": item["schedule"],
            "freshness_maximum_age_hours": item["freshnessMaximumAgeHours"],
            "minimum_records": item["minimumRecords"],
            "http_min_interval_seconds": settings.get("httpMinIntervalSeconds", 0.5),
        }
    return result


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    text = json.dumps(render(), indent=2, sort_keys=True) + "\n"
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(text)
    else:
        print(text, end="")
