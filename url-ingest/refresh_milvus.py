"""Versioned, retry-safe document replacement using the Daedalus row schema."""
from __future__ import annotations

import json
import math
import time
from collections import defaultdict

from refresh_state import digest


class StoreUnavailable(RuntimeError):
    """A persistent dependency failure must stop the pass, not poison every source."""


def storage_retry(operation):
    for attempt in range(3):
        try:
            return operation()
        except Exception as error:
            message = str(error).lower()
            transient = isinstance(error, TimeoutError) or any(part in message for part in (
                "closed channel", "channel closed", "channel not available",
                "channel distribution is not serviceable", "unavailable",
                "deadline exceeded", "fail connecting to server"))
            if not transient:
                raise
            if attempt == 2:
                raise StoreUnavailable(f"Milvus unavailable after 3 attempts: {error}") from error
            time.sleep(5 * (attempt + 1))


def prepare_rows(source: str, content_hash: str, fingerprint: str, records: list[dict], dimension: int) -> list[dict]:
    if not records:
        raise ValueError("document produced no searchable records")
    source_key = digest(source)
    rows = []
    for index, record in enumerate(records):
        vector = record.get("vector")
        if not isinstance(vector, list) or len(vector) != dimension or any(
            not isinstance(v, (float, int)) or isinstance(v, bool) or not math.isfinite(v) for v in vector
        ):
            raise ValueError("invalid embedding vector")
        if not 0.99 <= sum(v * v for v in vector) <= 1.01:
            raise ValueError("embedding is not L2 normalized")
        text = record.get("text")
        if not isinstance(text, str) or not text.strip() or len(text.encode()) > 65535:
            raise ValueError("empty or oversized searchable text")
        rows.append({**record, "pk": digest(f"{fingerprint}:{source_key}:{content_hash}:{index}"),
                     "source_key": source_key, "content_hash": content_hash,
                     "source": {**record.get("source", {}), "source_id": source, "source_name": source},
                     "content_metadata": {**(record.get("content_metadata") or {}),
                                          "retrieval_contract": fingerprint, "source_url": source}})
    return rows


class Store:
    def __init__(self, client, name: str, fingerprint: str, dimension: int):
        self.client, self.name, self.fingerprint, self.dimension = client, name, fingerprint, dimension

    def ensure(self) -> None:
        from pymilvus import DataType, MilvusClient
        if not self.client.has_collection(self.name, timeout=30):
            schema = MilvusClient.create_schema(auto_id=False, enable_dynamic_field=False,
                                                description=f"daedalus-refresh:{self.fingerprint}")
            schema.add_field("pk", DataType.VARCHAR, is_primary=True, max_length=64)
            schema.add_field("vector", DataType.FLOAT_VECTOR, dim=self.dimension)
            schema.add_field("text", DataType.VARCHAR, max_length=65535)
            schema.add_field("source", DataType.JSON)
            schema.add_field("content_metadata", DataType.JSON)
            schema.add_field("source_key", DataType.VARCHAR, max_length=64)
            schema.add_field("content_hash", DataType.VARCHAR, max_length=64)
            indexes = self.client.prepare_index_params()
            indexes.add_index(field_name="vector", index_type="HNSW", metric_type="COSINE",
                              params={"M": 16, "efConstruction": 200})
            indexes.add_index(field_name="source_key", index_type="INVERTED")
            self.client.create_collection(self.name, schema=schema, index_params=indexes,
                                          consistency_level="Strong", timeout=60)
        description = self.client.describe_collection(self.name, timeout=30)
        if description.get("description") != f"daedalus-refresh:{self.fingerprint}":
            raise ValueError(f"refusing to write incompatible collection {self.name}")
        fields = {f["name"]: f for f in description["fields"]}
        if int(fields["vector"]["params"]["dim"]) != self.dimension or description.get("auto_id"):
            raise ValueError("collection schema differs from retrieval contract")
        self.client.load_collection(self.name, timeout=60)

    def replace(self, source: str, content_hash: str, records: list[dict]) -> int:
        rows = prepare_rows(source, content_hash, self.fingerprint, records, self.dimension)
        # Replay the entire deterministic replacement if a query node moves or
        # the client reconnects. Old rows survive until verification succeeds.
        return storage_retry(lambda: self._replace(source, content_hash, rows))

    def _replace(self, source: str, content_hash: str, rows: list[dict]) -> int:
        source_key = digest(source)
        clause = f'source_key == "{source_key}" and content_hash == "{content_hash}"'
        for start in range(0, len(rows), 128):
            self.client.upsert(self.name, rows[start:start + 128], timeout=60)
        # Strong reads establish that every expected primary key is visible.
        for start in range(0, len(rows), 128):
            ids = [r["pk"] for r in rows[start:start + 128]]
            visible = self.client.get(self.name, ids, output_fields=["pk"],
                                      consistency_level="Strong", timeout=60)
            if {r["pk"] for r in visible} != set(ids):
                raise RuntimeError("Milvus did not make every replacement chunk visible")
        # An interrupted attempt can leave extra chunks of this same content
        # version. Remove those before committing its manifest, too.
        expected = {r["pk"] for r in rows}
        iterator = self.client.query_iterator(self.name, filter=clause,
                                               output_fields=["pk"], batch_size=512,
                                               consistency_level="Strong", timeout=60)
        try:
            while batch := iterator.next():
                obsolete = [r["pk"] for r in batch if r["pk"] not in expected]
                if obsolete:
                    self.client.delete(self.name, ids=obsolete, timeout=60)
        finally:
            iterator.close()
        # Retain the previous version until the complete replacement is visible.
        self.client.delete(self.name, filter=f'source_key == "{source_key}" and content_hash != "{content_hash}"', timeout=60)
        return len(rows)

    def delete(self, source: str) -> None:
        storage_retry(lambda: self.client.delete(self.name, filter=f'source_key == "{digest(source)}"', timeout=60))


def group_results(records: list, source_paths: dict[str, str]) -> dict[str, list]:
    """Require unambiguous NV-Ingest provenance before marking any source done."""
    from nv_ingest_client.util.milvus import cleanup_records
    from pathlib import Path
    aliases = defaultdict(set)
    for path, source in source_paths.items():
        for alias in (path, str(Path(path).resolve()), Path(path).name):
            aliases[alias].add(source)
    grouped = defaultdict(list)
    for result in records:
        for element in ([result] if isinstance(result, dict) else result or []):
            metadata = element.get("metadata", {})
            text = metadata.get("content") if element.get("document_type") == "text" else metadata.get("table_metadata", {}).get("table_content")
            if text and metadata.get("embedding") is None:
                raise ValueError("NV-Ingest returned searchable text without its embedding")
            if isinstance(text, str) and len(text.encode()) > 65535:
                raise ValueError("NV-Ingest returned oversized text; refusing a partially indexed document")
    for row in cleanup_records(records):
        metadata = row.get("source", {})
        candidates = set()
        for field in ("source_id", "source_name", "source_location"):
            value = metadata.get(field)
            if isinstance(value, str):
                candidates.update(aliases.get(value, set()))
        if len(candidates) != 1:
            raise ValueError("NV-Ingest result has missing or ambiguous source provenance")
        grouped[candidates.pop()].append(row)
    return dict(grouped)
