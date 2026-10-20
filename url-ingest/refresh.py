#!/usr/bin/env python3
"""Reconcile source documents into a model-specific Milvus generation.

Successful sources commit independently. A failed source keeps its previous
document version and is retried; the collection is never reported fresh while
unresolved failures remain. No operation drops a collection or resets storage.
"""
from __future__ import annotations

import argparse
import contextlib
import importlib.util
import json
import os
import sys
import time
import uuid
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

from refresh_fetch import SourceRemoved, discover, fetch, session
from refresh_milvus import Store, group_results
from refresh_state import RunReport, State, collection_lock, contract_fingerprint, digest, heartbeat, capacity_slot


def legacy_client():
    name = "daedalus_ingest_client"
    if name not in sys.modules:
        spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name("nv-ingest-ms-tasks.py"))
        module = importlib.util.module_from_spec(spec)
        sys.modules[name] = module
        spec.loader.exec_module(module)
    return sys.modules[name]


def extract_batch(paths: list[str], contract: dict, kind: str) -> tuple[list, list[str]]:
    from nv_ingest_client.client import Ingestor
    old = legacy_client()
    embedding = contract["embedding"]
    chunking = contract["chunking"]
    client = old.BoundedNvIngestClient(
        request_timeout_seconds=60, retry_delay_seconds=5,
        message_client_hostname=os.getenv("NV_INGEST_MESSAGE_HOST", "nv-ingest"),
        message_client_port=int(os.getenv("NV_INGEST_MESSAGE_PORT", "7670")),
        worker_pool_size=4,
    )
    builder = Ingestor(client=client).files(paths)
    extract_options = {"extract_text": True, "extract_images": False,
                       "extract_tables": True, "extract_charts": True,
                       "extract_infographics": True} if kind == "pdf" else {}
    builder = builder.extract(**extract_options)
    with builder as ingestor:
        results, failures = ingestor.ingest(
            show_progress=False, return_failures=True, batch_size=4,
            concurrency_limit=4, timeout=60, max_job_retries=120,
            retry_delay=5, fail_on_submit_error=True,
        )
        completed = old._submitted_completed_files(paths, ingestor, failures)
        from refresh_embedding import embed_records
        return embed_records(results, contract), completed


def physical_collection(alias: str, generation: str) -> str:
    import re
    if not re.fullmatch(r"[a-z][a-z0-9_]*", alias) or not re.fullmatch(r"[a-z][a-z0-9_]*", generation):
        raise ValueError("collection alias and generation must be lowercase Milvus identifiers")
    # The model-specific database isolates vector spaces; consumers retain
    # stable collection names inside that database.
    return alias


def process_batch(batch: list[tuple[str, Path, dict]], contract: dict, kind: str,
                  state: State, report: RunReport, store: Store) -> None:
    if not batch:
        return
    try:
        with capacity_slot(state.root.parent.parent / "locks", int(os.getenv("GLOBAL_INGEST_CAPACITY", "1"))):
            results, completed = extract_batch([str(path) for _, path, _ in batch], contract, kind)
        completed = {str(Path(p).resolve()) for p in completed}
        grouped = group_results(results, {str(path): source for source, path, _ in batch})
    except Exception as error:
        for source, _, _ in batch:
            report.fail(source, error)
        return
    for source, path, document in batch:
        try:
            if str(path.resolve()) not in completed:
                raise RuntimeError("NV-Ingest did not confirm completion for this source")
            rows = store.replace(source, document["sha256"], grouped.get(source, []))
            state.record(source, {**document, "ingested_hash": document["sha256"],
                                  "row_count": rows, "ingested_at": time.time(), "deleted": False})
            report.data["updated"] += 1
            report.write()
        except Exception as error:
            report.fail(source, error)


def run(contract: dict, alias: str, state_root: Path, cache: Path,
        pdf_root: Path, *, bootstrap_cache: Path | None = None, limit: int = 0) -> dict:
    from pymilvus import MilvusClient
    collection = contract["collections"][alias]
    fingerprint = contract_fingerprint(contract)
    generation = contract["generation"]
    root = state_root / generation
    with collection_lock(root / "locks", alias), heartbeat(root, alias):
        state = State(root / "collections", alias, fingerprint)
        run_id = os.getenv("POD_NAME") or f"{int(time.time())}-{uuid.uuid4().hex[:12]}"
        report = RunReport(root, alias, fingerprint, run_id)
        report.data["last_success"] = state.data.get("last_success", 0)
        report.data["physical_collection"] = physical_collection(alias, generation)
        report.data["milvus_database"] = contract.get("active_database", contract["milvus_database"])
        client = None
        try:
            client = MilvusClient(uri=os.environ["MILVUS_URI"],
                                  token=f'{os.environ["MILVUS_USERNAME"]}:{os.environ["MILVUS_PASSWORD"]}')
            database = report.data["milvus_database"]
            if database not in client.list_databases():
                try:
                    client.create_database(database)
                except Exception:
                    if database not in client.list_databases():
                        raise
            client.using_database(database)
            store = Store(client, report.data["physical_collection"], fingerprint, contract["embedding"]["dimension"])
            store.ensure()
            with session() as http:
                sources, discovery_errors = discover(http, collection.get("urls", []))
            for error in discovery_errors:
                report.fail(error["source"], RuntimeError(error["error"]))
            # Revalidate known sources even when they age out of a sitemap.
            # A sitemap omission is never permission to delete indexed content.
            for source, document in state.data["documents"].items():
                if source.startswith(("http://", "https://")) and not document.get("deleted"):
                    sources.setdefault(source, {"url": source, "kind": "url"})
            report.data["discovered_urls"] = len(sources)
            report.write()

            if collection.get("pdfs"):
                directory = pdf_root / alias
                if not directory.is_dir():
                    raise RuntimeError(f"required PDF directory is missing: {alias}")
                else:
                    pdfs = sorted(directory.rglob("*.pdf"))
                report.data["discovered_pdfs"] = len(pdfs)
                for path in pdfs[:limit or None]:
                    source = f"pdf://{alias}/{path.relative_to(directory)}"
                    previous = state.data["documents"].get(source, {})
                    body = path.read_bytes()
                    content_hash = digest(body)
                    report.data["checked"] += 1
                    if previous.get("ingested_hash") == content_hash and not previous.get("deleted"):
                        state.record(source, {**previous, "last_checked": time.time(), "missing_since": 0})
                        report.data["unchanged"] += 1
                        continue
                    document = {**previous, "sha256": content_hash, "last_checked": time.time(), "missing_since": 0}
                    state.record(source, document)
                    # Freeze the exact bytes: the corpus publisher may switch
                    # its current symlink while NV-Ingest is reading a file.
                    immutable = cache / generation / alias / digest(source) / (content_hash + ".pdf")
                    immutable.parent.mkdir(parents=True, exist_ok=True)
                    if not immutable.exists():
                        temp = immutable.with_suffix(".tmp")
                        with temp.open("wb") as stream:
                            stream.write(body)
                            stream.flush()
                            os.fsync(stream.fileno())
                        temp.replace(immutable)
                    process_batch([(source, immutable, document)], contract, "pdf", state, report, store)
                # PDF removals require two observations separated by the grace
                # period. A missing entire mount/directory is an error above.
                existing = {f"pdf://{alias}/{p.relative_to(directory)}" for p in pdfs}
                if directory.is_dir() and not limit:
                    for source, previous in list(state.data["documents"].items()):
                        if source.startswith(f"pdf://{alias}/") and source not in existing and not previous.get("deleted"):
                            missing_since = previous.get("missing_since") or time.time()
                            if time.time() - missing_since >= contract.get("deletion_grace_hours", 48) * 3600:
                                store.delete(source)
                                state.record(source, {**previous, "missing_since": missing_since, "deleted": True})
                                report.data["deleted"] += 1
                            else:
                                state.record(source, {**previous, "missing_since": missing_since})
                                report.fail(source, SourceRemoved("PDF missing; deletion grace period"))

            def download(item):
                source, entry = item
                previous = state.data["documents"].get(source, {})
                try:
                    with session() as http:
                        path, document = fetch(http, source, previous, cache / generation / alias,
                                               try_markdown=entry.get("try_markdown", False))
                    return source, path, document, None
                except Exception as error:
                    return source, None, previous, error

            # Resume unfinished scans fairly after a deadline or eviction.
            items = sorted(sources.items(), key=lambda pair: (
                state.data["documents"].get(pair[0], {}).get("last_attempt", 0), pair[0]))
            if limit:
                items = items[:limit]
            batch = []
            # executor.map on Python 3.12 eagerly submits its input; chunk the
            # source list to bound queued HTTP work and memory on large corpora.
            with ThreadPoolExecutor(max_workers=4) as pool:
                for start in range(0, len(items), 32):
                    for source, path, document, error in pool.map(download, items[start:start + 32]):
                        report.data["checked"] += 1
                        document = {**document, "last_attempt": time.time()}
                        state.record(source, document)
                        if isinstance(error, SourceRemoved):
                            previous = state.data["documents"].get(source, {})
                            missing_since = previous.get("missing_since") or time.time()
                            if time.time() - missing_since >= contract.get("deletion_grace_hours", 48) * 3600:
                                store.delete(source)
                                state.record(source, {**previous, "missing_since": missing_since, "deleted": True})
                                report.data["deleted"] += 1
                            else:
                                state.record(source, {**previous, "missing_since": missing_since})
                                report.fail(source, error)
                        elif error:
                            report.fail(source, error)
                        elif document.get("ingested_hash") == document["sha256"] and not document.get("deleted"):
                            state.record(source, document)
                            report.data["unchanged"] += 1
                        else:
                            state.record(source, document)
                            batch.append((source, path, document))
                        if len(batch) >= 8:
                            process_batch(batch, contract, "url", state, report, store)
                            batch = []
                    report.write()
                process_batch(batch, contract, "url", state, report, store)

            if report.data["checked"] == 0:
                raise RuntimeError("no sources were checked")
            count = client.query(store.name, filter="", output_fields=["count(*)"],
                                  consistency_level="Strong", timeout=60)[0]["count(*)"]
            if count < collection.get("minimum_records", 1):
                raise RuntimeError("collection does not meet its minimum record count")
            report.data["record_count"] = count
            report.data["state"] = "failed" if report.data["failure_count"] else "partial" if limit else "complete"
            if report.data["state"] == "complete":
                state.data["last_success"] = time.time()
                report.data["last_success"] = state.data["last_success"]
            state.checkpoint()
        except BaseException as error:
            report.data["state"] = "failed"
            report.fail(alias, error)
            raise
        finally:
            report.data["finished_at"] = time.time()
            report.write(force=True)
            if client:
                client.close()
        print(json.dumps({k: v for k, v in report.data.items() if k != "failures"}), flush=True)
        if report.data["state"] == "failed":
            raise RuntimeError(f"refresh has {report.data['failure_count']} unresolved failures")
        return report.data


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--contract", default="/etc/refresh/contract.json")
    parser.add_argument("--collection", required=True)
    parser.add_argument("--limit", type=int, default=0, help="bounded validation; never marks a collection fresh")
    args = parser.parse_args()
    run(json.loads(Path(args.contract).read_text()), args.collection,
        Path(os.getenv("REFRESH_STATE_ROOT", "/manifests/refresh")),
        Path(os.getenv("REFRESH_CACHE_ROOT", "/workspace/urls/refresh")),
        Path(os.getenv("PDF_BASE_PATH", "/workspace/pdf-server")), limit=args.limit)


if __name__ == "__main__":
    main()
