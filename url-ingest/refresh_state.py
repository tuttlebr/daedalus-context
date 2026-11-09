"""Durable refresh state, independent of Kubernetes Job retention.

One flock-protected writer owns each collection. Atomic snapshots are replaced
only after fsync; the journal records committed document state between snapshots.
"""
from __future__ import annotations

import contextlib
import fcntl
import hashlib
import json
import os
import time
import threading
import uuid
from pathlib import Path


def digest(value: bytes | str) -> str:
    return hashlib.sha256(value.encode() if isinstance(value, str) else value).hexdigest()


def atomic_json(path: Path, value: object) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(f".{path.name}.{uuid.uuid4().hex}.tmp")
    try:
        with temporary.open("x") as stream:
            json.dump(value, stream, sort_keys=True, allow_nan=False)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
        descriptor = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(descriptor)
        finally:
            os.close(descriptor)
    finally:
        temporary.unlink(missing_ok=True)


class CollectionBusy(BlockingIOError):
    """Another worker owns this collection; this attempt must not change it."""


@contextlib.contextmanager
def collection_lock(root: Path, collection: str):
    root.mkdir(parents=True, exist_ok=True)
    # Keep the lock inode permanently. Unlinking it permits two independent locks.
    with (root / f"{collection}.lock").open("a+") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise CollectionBusy(f"another refresh owns {collection}") from error
        try:
            yield
        finally:
            fcntl.flock(lock, fcntl.LOCK_UN)


class State:
    def __init__(self, root: Path, collection: str, fingerprint: str):
        self.root = root / collection
        self.root.mkdir(parents=True, exist_ok=True)
        self.path = self.root / "state.json"
        self.journal = self.root / "journal.jsonl"
        self.data = {"fingerprint": fingerprint, "documents": {}, "last_success": 0}
        if self.path.exists():
            self.data = json.loads(self.path.read_text())
        else:
            atomic_json(self.path, self.data)
        if self.data["fingerprint"] != fingerprint:
            raise ValueError("state belongs to a different retrieval contract; use a new generation")
        if self.journal.exists():
            records = self.journal.read_bytes().splitlines(keepends=True)
            for index, line in enumerate(records):
                try:
                    entry = json.loads(line)
                except json.JSONDecodeError:
                    # A killed writer can leave a partial final append, never a
                    # partial interior record. Discard only that uncommitted tail.
                    if index == len(records) - 1 and not line.endswith(b"\n"):
                        break
                    raise
                self.data["documents"][entry["id"]] = entry["document"]
            # Snapshot replay before any new append, removing a partial tail.
            self.checkpoint()

    def record(self, source: str, document: dict) -> None:
        entry = {"id": source, "document": document}
        with self.journal.open("a") as stream:
            stream.write(json.dumps(entry, sort_keys=True, allow_nan=False) + "\n")
            stream.flush()
            os.fsync(stream.fileno())
        self.data["documents"][source] = document

    def checkpoint(self) -> None:
        atomic_json(self.path, self.data)
        # Replaying an old journal is idempotent if killed between these writes.
        with self.journal.open("w") as stream:
            stream.flush()
            os.fsync(stream.fileno())


def contract_fingerprint(contract: dict) -> str:
    # Endpoint addresses and run schedules do not define the vector space.
    identity = {key: contract[key] for key in ("schema_version", "embedding", "chunking", "milvus_database")}
    return digest(json.dumps(identity, sort_keys=True, separators=(",", ":")))


class RunReport:
    def __init__(self, root: Path, collection: str, fingerprint: str, run_id: str):
        self.path = root / "runs" / collection / f"{run_id}.json"
        self.latest = root / "status" / f"{collection}.json"
        self.data = {"run_id": run_id, "collection": collection, "fingerprint": fingerprint,
                     "started_at": time.time(), "updated_at": time.time(), "state": "running",
                     "checked": 0, "unchanged": 0, "updated": 0, "deleted": 0,
                     "failures": [], "failure_count": 0, "pending_removals": 0,
                     "reused_checks": 0, "last_success": 0}
        self.failures_path = self.path.with_suffix(".failures.jsonl")
        self._last_write = 0
        self.write()

    def write(self, force: bool = False) -> None:
        if not force and time.monotonic() - self._last_write < 5:
            return
        self.data["updated_at"] = time.time()
        atomic_json(self.path, self.data)
        atomic_json(self.latest, self.data)
        self._last_write = time.monotonic()

    def fail(self, source: str, error: Exception) -> None:
        entry = {"source": source, "error_type": type(error).__name__,
                 "error": str(error)[:1000], "at": time.time()}
        with self.failures_path.open("a") as stream:
            stream.write(json.dumps(entry) + "\n")
            stream.flush()
            os.fsync(stream.fileno())
        self.data["failure_count"] += 1
        if len(self.data["failures"]) < 100:
            self.data["failures"].append(entry)
        # The next request may wait in host backoff for minutes. Publish this
        # failure now rather than leaving a healthy-looking throttled report.
        self.write(force=True)


@contextlib.contextmanager
def heartbeat(root: Path, collection: str):
    stopped = threading.Event()
    path = root / "heartbeats" / f"{collection}.json"
    def beat():
        while not stopped.is_set():
            atomic_json(path, {"at": time.time(), "pod": os.getenv("POD_NAME", "local")})
            stopped.wait(30)
    thread = threading.Thread(target=beat, daemon=True)
    thread.start()
    try:
        yield
    finally:
        stopped.set()
        thread.join(timeout=35)


@contextlib.contextmanager
def capacity_slot(root: Path, capacity: int):
    """Kernel-owned locks release automatically after a crash or pod eviction."""
    if capacity < 1:
        raise ValueError("capacity must be positive")
    with contextlib.ExitStack() as resources:
        root.mkdir(parents=True, exist_ok=True)
        handles = [resources.enter_context((root / f"extract-{i}.lock").open("a+")) for i in range(capacity)]
        while True:
            for handle in handles:
                try:
                    fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
                except BlockingIOError:
                    continue
                try:
                    yield
                finally:
                    fcntl.flock(handle, fcntl.LOCK_UN)
                return
            time.sleep(5)


def refresh_owned(name: str, description: str | None, contract_collections) -> bool:
    """Whether promotion may archive a `default` collection.

    Only collections produced by this refresh system are moved: a prior
    generation of a contract collection or anything carrying a refresh
    fingerprint. The Agent shares the same database for `meta` and private
    `user_uploads*` collections; those are never refresh-owned.
    """
    if name in set(contract_collections):
        return True
    return bool(description) and description.startswith("daedalus-refresh:")
