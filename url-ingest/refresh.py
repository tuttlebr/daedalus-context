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
import re
import sys
import time
import uuid
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

from refresh_fetch import SourceRemoved, discover, fetch, session
from refresh_milvus import Store, StoreUnavailable, group_results
from refresh_state import (RunReport, State, CollectionBusy, atomic_json, collection_lock,
                           contract_fingerprint, digest, heartbeat, capacity_slot)
from refresh_http import HostGovernor


class RefreshFailed(RuntimeError):
    """Source failures are already recorded; the Job should retry without a traceback."""


def pending_removal(report: RunReport, source: str) -> None:
    report.data["pending_removals"] += 1
    samples = report.data.setdefault("pending_removal_samples", [])
    if len(samples) < 10:
        samples.append(source)


def download_source(item, previous: dict, cache: Path, governor: HostGovernor, job_id: str):
    source, entry = item
    try:
        cached = Path(previous['cache_path']) if previous.get('cache_path') else None
        if previous.get('last_checked_job') == job_id and cached and cached.is_file():
            return source, cached, {**previous, '_reused_check': True}, None
        with session(governor) as http:
            path, document = fetch(http, source, previous, cache,
                                   try_markdown=entry.get('try_markdown', False),
                                   url_override=entry.get('fetch_url'))
        document.pop('last_error', None)
        return source, path, {**document, 'last_checked_job': job_id}, None
    except Exception as error:
        return source, None, previous, error


def source_priority(item, documents: dict, now: float, grace_seconds: float):
    source = item[0]
    document = documents.get(source, {})
    attempted = document.get('last_attempt', 0)
    missing = document.get('missing_since', 0)
    unfinished = (document.get('last_error') or not document.get('sha256') or
                  document.get('sha256') != document.get('ingested_hash'))
    retry_due = attempted and not document.get('deleted') and (
        (missing and now - missing >= grace_seconds) or (not missing and unfinished))
    return (0 if retry_due else 1 if not attempted else 2, attempted, source)


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
    # PDF extraction can outlive a short HTTP read on a thousand-page manual.
    # Keep transport waits bounded without treating normal extraction time as
    # a terminal failure. HTML requests retain the shorter deadline.
    request_timeout = 600 if kind == 'pdf' else 60
    client = old.BoundedNvIngestClient(
        request_timeout_seconds=request_timeout, retry_delay_seconds=5,
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
            concurrency_limit=4, timeout=request_timeout, max_job_retries=120,
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
        for source, _, document in batch:
            state.record(source, {**document, 'last_error': type(error).__name__})
            report.fail(source, error)
        return
    for source, path, document in batch:
        try:
            if str(path.resolve()) not in completed:
                raise RuntimeError("NV-Ingest did not confirm completion for this source")
            rows = store.replace(source, document["sha256"], grouped.get(source, []))
            document.pop('last_error', None)
            state.record(source, {**document, "ingested_hash": document["sha256"],
                                  "row_count": rows, "ingested_at": time.time(), "deleted": False})
            report.data["updated"] += 1
            report.write()
        except Exception as error:
            state.record(source, {**document, 'last_error': type(error).__name__})
            report.fail(source, error)
            if isinstance(error, StoreUnavailable):
                raise RefreshFailed(str(error)) from error


def run(contract: dict, alias: str, state_root: Path, cache: Path,
        pdf_root: Path, *, bootstrap_cache: Path | None = None, limit: int = 0,
        budget_seconds: float = 75600) -> dict:
    collection = contract["collections"][alias]
    fingerprint = contract_fingerprint(contract)
    generation = contract["generation"]
    root = state_root / generation
    run_id = os.getenv("POD_NAME") or f"{int(time.time())}-{uuid.uuid4().hex[:12]}"
    job_id = os.getenv("JOB_NAME") or run_id
    with contextlib.ExitStack() as owned:
        try:
            owned.enter_context(collection_lock(root / "locks", alias))
        except CollectionBusy:
            # A manual backfill is outside CronJob Forbid's ownership. Preserve
            # its report, heartbeat, lock inode, and freshness timestamp.
            skipped = {"run_id": run_id, "collection": alias, "state": "skipped_active",
                       "reason": "another refresh owns the collection", "at": time.time()}
            atomic_json(root / "runs" / alias / f"{run_id}.json", skipped)
            print(json.dumps(skipped), flush=True)
            return skipped
        owned.enter_context(heartbeat(root, alias))
        from pymilvus import MilvusClient
        state = State(root / "collections", alias, fingerprint)
        report = RunReport(root, alias, fingerprint, run_id)
        report.data['job_id'] = job_id
        governor = HostGovernor(state.root / 'http-hosts',
                                collection.get('http_min_interval_seconds', 0.5))
        deadline = time.monotonic() + budget_seconds
        report.data['budget_exhausted'] = False
        def budget_available():
            if time.monotonic() >= deadline:
                report.data['budget_exhausted'] = True
                return False
            return True
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
            with session(governor) as http:
                sources, discovery_errors = discover(http, collection.get("urls", []))
            for error in discovery_errors:
                report.fail(error["source"], RuntimeError(error["error"]))
            # Revalidate known sources even when they age out of a sitemap.
            # A sitemap omission is never permission to delete indexed content.
            for source, document in state.data["documents"].items():
                if source.startswith(("http://", "https://")) and not document.get("deleted"):
                    sources.setdefault(source, {"url": source, "kind": "url"})
            # Exclusions are reviewed source policy, not evidence of deletion.
            # Apply to known sources too, retaining their state and old vectors.
            excluded = [source for source in sources if any(re.search(pattern, source)
                        for pattern in collection.get('exclude_url_patterns', []))]
            for source in excluded:
                del sources[source]
            report.data['excluded_urls'] = len(excluded)
            for source, target in collection.get('fetch_overrides', {}).items():
                if source in sources:
                    sources[source] = {**sources[source], 'fetch_url': target}
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
                    if not budget_available():
                        break
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
                                pending_removal(report, source)

            def download(item):
                return download_source(item, state.data['documents'].get(item[0], {}),
                                       cache / generation / alias, governor, job_id)

            # Resume unfinished scans fairly after a deadline or eviction.
            now = time.time()
            grace = contract.get('deletion_grace_hours', 48) * 3600
            items = sorted(sources.items(), key=lambda item: source_priority(
                item, state.data['documents'], now, grace))
            if limit:
                items = items[:limit]
            batch = []
            # executor.map on Python 3.12 eagerly submits its input; chunk the
            # source list to bound queued HTTP work and memory on large corpora.
            with ThreadPoolExecutor(max_workers=4) as pool:
                for start in range(0, len(items), 32):
                    if not budget_available():
                        break
                    for source, path, document, error in pool.map(download, items[start:start + 32]):
                        report.data["checked"] += 1
                        report.data['reused_checks'] += int(document.pop('_reused_check', False))
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
                                pending_removal(report, source)
                        elif error:
                            state.record(source, {**document, 'last_error': type(error).__name__})
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

            if report.data["checked"] == 0 and not report.data['budget_exhausted']:
                raise RuntimeError("no sources were checked")
            count = client.query(store.name, filter="", output_fields=["count(*)"],
                                  consistency_level="Strong", timeout=60)[0]["count(*)"]
            if count < collection.get("minimum_records", 1):
                raise RuntimeError("collection does not meet its minimum record count")
            report.data["record_count"] = count
            report.data["state"] = ("partial" if report.data['budget_exhausted'] else
                                     "failed" if report.data["failure_count"] else
                                     "partial" if limit else
                                     "pending_removals" if report.data["pending_removals"] else "complete")
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
            raise RefreshFailed(f"refresh has {report.data['failure_count']} unresolved failures")
        return report.data


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--contract", default="/etc/refresh/contract.json")
    parser.add_argument("--collection", required=True)
    parser.add_argument("--limit", type=int, default=0, help="bounded validation; never marks a collection fresh")
    args = parser.parse_args()
    try:
        run(json.loads(Path(args.contract).read_text()), args.collection,
            Path(os.getenv("REFRESH_STATE_ROOT", "/manifests/refresh")),
            Path(os.getenv("REFRESH_CACHE_ROOT", "/workspace/urls/refresh")),
            Path(os.getenv("PDF_BASE_PATH", "/workspace/pdf-server")), limit=args.limit,
            budget_seconds=float(os.getenv('REFRESH_RUN_BUDGET_SECONDS', '75600')))
    except RefreshFailed as error:
        print(json.dumps({'event': 'refresh_failed', 'error': str(error),
                          'collection': args.collection}), flush=True)
        raise SystemExit(1) from None


if __name__ == "__main__":
    main()
