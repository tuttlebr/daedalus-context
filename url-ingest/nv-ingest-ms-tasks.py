from __future__ import annotations

import gc
import fcntl
import glob
import hashlib
import io
import json
import logging
import math
import os
import re
import signal
import tempfile
import time

from collections import Counter
from dataclasses import dataclass
from pathlib import Path
from typing import Literal

from nv_ingest_client.client import Ingestor, NvIngestClient
from minio import Minio
from nv_ingest_api.util.logging.configuration import (
    configure_logging as configure_local_logging,
)
from pymilvus import MilvusClient

try:
    from dotenv import load_dotenv
except ModuleNotFoundError:

    def load_dotenv(*_args, **_kwargs):
        return False


logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s][%(funcName)s]: %(message)s (%(filename)s:%(lineno)d)",
)
logger = logging.getLogger(__name__)

local_log_level = os.getenv("INGEST_LOG_LEVEL", "DEFAULT")
if local_log_level in ("DEFAULT",):
    local_log_level = "INFO"

configure_local_logging(local_log_level)

DEFAULT_FILE_CHUNK_SIZE = 128
DEFAULT_CLIENT_BATCH_SIZE = 16
DEFAULT_CLIENT_CONCURRENCY_LIMIT = 32
DEFAULT_RESULTS_BASE_PATH = "/tmp/nv-ingest-results"
DEFAULT_URL_COLLECTIONS = "kubernetes,nvidia,semianalysis,vetpartner"
DEFAULT_PDF_COLLECTIONS = "kubernetes,mentalhealth,nvidia,semianalysis,vetpartner"
DEFAULT_URL_BASE_PATH = "/workspace/urls"
DEFAULT_PDF_BASE_PATH = "/workspace/pdf-server"
DEFAULT_SPLIT_SOURCE_TYPES = ["text", "md", "html", "PDF"]
DEFAULT_INGEST_VERSION = "v1"
DEFAULT_CLIENT_REQUEST_TIMEOUT_SECONDS = 60
DEFAULT_CLIENT_MAX_JOB_RETRIES = 120
DEFAULT_CLIENT_RETRY_DELAY_SECONDS = 5.0
DEFAULT_COLLECTION_VERIFY_TIMEOUT_SECONDS = 300
DEFAULT_CLIENT_TRANSPORT_MAX_RETRIES = 1

URL_FILE_EXTENSIONS = ("*.html", "*.md")
COLLECTION_NAME_RE = re.compile(r"^[a-z0-9][a-z0-9_-]*$")

MANIFEST_BASE_PATH = os.getenv("MANIFEST_BASE_PATH", "")


def _http_session_allocator(pool_size: int):
    """Build a requests Session whose keep-alive pool matches the worker pool."""

    def allocate_http_session():
        import requests

        session = requests.Session()
        adapter = requests.adapters.HTTPAdapter(
            pool_connections=pool_size,
            pool_maxsize=pool_size,
        )
        session.mount("http://", adapter)
        session.mount("https://", adapter)
        return session

    # RestClient 26.3.0 logs allocator.__name__ after invoking it.
    allocate_http_session.__name__ = "bounded_http_session"
    return allocate_http_session


class BoundedNvIngestClient(NvIngestClient):
    """Apply the configured timeout to asynchronous REST result fetches.

    NV-Ingest client 26.3.0 stores the timeout passed to ``Ingestor.ingest`` but
    calls ``fetch_job_result_async(..., timeout=None)``. The client then expands
    that to ``(100, None)``, leaving the HTTP read unbounded. A server rollout
    can therefore strand a worker forever on a connection to a terminated pod.
    Keep the compatibility workaround local to this ingestion entry point and
    retain a small, bounded transport retry for transient connection loss.
    """

    def __init__(
        self,
        *,
        request_timeout_seconds: int,
        retry_delay_seconds: float,
        **kwargs,
    ):
        if request_timeout_seconds <= 0:
            raise ValueError("request_timeout_seconds must be greater than zero")
        if retry_delay_seconds < 0:
            raise ValueError("retry_delay_seconds must not be negative")

        self._bounded_request_timeout = (
            min(5.0, float(request_timeout_seconds)),
            float(request_timeout_seconds),
        )
        self._bounded_retry_delay = float(retry_delay_seconds)
        message_client_kwargs = dict(kwargs.pop("message_client_kwargs", {}) or {})
        worker_pool_size = int(kwargs.get("worker_pool_size", 8))
        message_client_kwargs.setdefault(
            "http_allocator", _http_session_allocator(worker_pool_size)
        )
        message_client_kwargs.setdefault(
            "default_connect_timeout", self._bounded_request_timeout[0]
        )
        message_client_kwargs.setdefault(
            "default_read_timeout", self._bounded_request_timeout[1]
        )
        # In the 26.3.0 RestClient, zero means unlimited retries despite its
        # public documentation describing zero as no retries.
        message_client_kwargs.setdefault(
            "max_retries", DEFAULT_CLIENT_TRANSPORT_MAX_RETRIES
        )
        super().__init__(message_client_kwargs=message_client_kwargs, **kwargs)

    def fetch_job_result_async(self, job_ids, data_only=True, timeout=None):
        effective_timeout = (
            self._bounded_request_timeout if timeout is None else timeout
        )
        return super().fetch_job_result_async(
            job_ids,
            data_only=data_only,
            timeout=effective_timeout,
        )

    def _fetch_job_result(self, *args, **kwargs):
        try:
            return super()._fetch_job_result(*args, **kwargs)
        except TimeoutError:
            # ConcurrentJobProcessor 26.3.0 also stores retry_delay but
            # immediately requeues HTTP 202 responses. Pace the returned
            # not-ready signal here so max_job_retries is a useful time bound
            # instead of a tight polling loop.
            if self._bounded_retry_delay:
                time.sleep(self._bounded_retry_delay)
            raise


class IngestManifest:
    """Tracks which files have been successfully ingested into Milvus.

    Stores JSON-lines records keyed by path, content hash, source type, and ingest
    version. Legacy path-only lines are upgraded lazily to avoid a duplicate storm
    on first deployment of the richer manifest format.
    """

    def __init__(self, collection: str, base_path: str = ""):
        self._enabled = bool(base_path)
        # Byte/cookie offset through the append-only manifest. Refreshing only
        # the unread tail keeps large seed runs from rescanning every prior
        # record for every completed client batch.
        self._read_offset = 0
        if not self._enabled:
            self._path = ""
            self._ingested_keys: set[str] = set()
            self._legacy_paths: set[str] = set()
            return
        self._path = os.path.join(base_path, f"{collection}.ingested")
        self._ingested_keys: set[str] = set()
        self._legacy_paths: set[str] = set()
        self._load()

    def _load(self) -> None:
        if not os.path.exists(self._path):
            return
        self._read_existing_records()
        logger.info(
            "Loaded manifest: path=%s records=%s legacy_paths=%s",
            self._path,
            len(self._ingested_keys),
            len(self._legacy_paths),
        )

    def _read_existing_records(self) -> None:
        with open(self._path, "r") as f:
            if self._read_offset > os.fstat(f.fileno()).st_size:
                # A legacy-entry compaction (or another safe rewrite) may
                # replace the file with a shorter copy.
                self._read_offset = 0
            f.seek(self._read_offset)
            while line := f.readline():
                stripped = line.strip()
                if not stripped:
                    continue
                if stripped.startswith("{"):
                    try:
                        record = json.loads(stripped)
                    except json.JSONDecodeError:
                        logger.warning(
                            "Ignoring invalid manifest JSON line: path=%s line=%s",
                            self._path,
                            stripped[:200],
                        )
                        continue
                    key = self._record_key(record)
                    if key:
                        self._ingested_keys.add(key)
                else:
                    self._legacy_paths.add(os.path.normpath(stripped))
            self._read_offset = f.tell()

    def filter_new(
        self, files: list[str], source_type: str, ingest_version: str
    ) -> list[str]:
        """Return only files not yet recorded in the manifest."""
        if not self._enabled:
            return files
        new_files: list[str] = []
        legacy_upgrades: list[str] = []
        for fp in files:
            record = build_manifest_record(fp, source_type, ingest_version)
            key = self._record_key(record)
            norm = os.path.normpath(fp)
            if key in self._ingested_keys:
                continue
            if norm in self._legacy_paths:
                legacy_upgrades.append(fp)
                continue
            new_files.append(fp)
        if legacy_upgrades:
            self.mark_ingested(legacy_upgrades, source_type, ingest_version)
            self._remove_legacy_paths(legacy_upgrades)
            logger.info(
                "Upgraded legacy manifest entries: path=%s count=%s",
                self._path,
                len(legacy_upgrades),
            )
        return new_files

    def _remove_legacy_paths(self, filepaths: list[str]) -> None:
        """Compact upgraded path-only entries out of the durable manifest."""
        if not self._enabled or not os.path.exists(self._path):
            return
        upgraded = {os.path.normpath(filepath) for filepath in filepaths}
        lock_path = f"{self._path}.lock"
        with open(lock_path, "a") as lock_file:
            fcntl.flock(lock_file.fileno(), fcntl.LOCK_EX)
            with open(self._path, "r") as source:
                lines = source.readlines()
            directory = os.path.dirname(self._path) or "."
            fd, temp_path = tempfile.mkstemp(prefix=".manifest-", dir=directory)
            try:
                with os.fdopen(fd, "w") as destination:
                    for line in lines:
                        stripped = line.strip()
                        if stripped and not stripped.startswith("{"):
                            if os.path.normpath(stripped) in upgraded:
                                continue
                        destination.write(line)
                    destination.flush()
                    os.fsync(destination.fileno())
                os.replace(temp_path, self._path)
                self._read_offset = os.path.getsize(self._path)
            except BaseException:
                try:
                    os.unlink(temp_path)
                except FileNotFoundError:
                    pass
                raise
            finally:
                fcntl.flock(lock_file.fileno(), fcntl.LOCK_UN)
        self._legacy_paths.difference_update(upgraded)

    def mark_ingested(
        self, filepaths: list[str], source_type: str, ingest_version: str
    ) -> None:
        """Append successfully ingested file paths to the manifest."""
        if not self._enabled or not filepaths:
            return
        os.makedirs(os.path.dirname(self._path), exist_ok=True)
        lock_path = f"{self._path}.lock"
        with open(lock_path, "a") as lock_file:
            fcntl.flock(lock_file.fileno(), fcntl.LOCK_EX)
            # Other deterministic shards may have appended records since this
            # process loaded the manifest. Refresh while holding the lock.
            if os.path.exists(self._path):
                self._read_existing_records()
            with open(self._path, "a") as f:
                for fp in filepaths:
                    record = build_manifest_record(fp, source_type, ingest_version)
                    key = self._record_key(record)
                    if key in self._ingested_keys:
                        continue
                    f.write(json.dumps(record, sort_keys=True) + "\n")
                    self._ingested_keys.add(key)
                f.flush()
                os.fsync(f.fileno())
                self._read_offset = f.tell()
            fcntl.flock(lock_file.fileno(), fcntl.LOCK_UN)

    @property
    def count(self) -> int:
        return len(self._ingested_keys) + len(self._legacy_paths)

    @property
    def enabled(self) -> bool:
        return self._enabled

    @property
    def path(self) -> str:
        return self._path

    def clear(self) -> None:
        """Clear a stale manifest when its Milvus collection is missing."""
        if not self._enabled:
            return
        os.makedirs(os.path.dirname(self._path), exist_ok=True)
        lock_path = f"{self._path}.lock"
        with open(lock_path, "a") as lock_file:
            fcntl.flock(lock_file.fileno(), fcntl.LOCK_EX)
            if os.path.exists(self._path):
                os.remove(self._path)
            self._ingested_keys.clear()
            self._legacy_paths.clear()
            self._read_offset = 0
            fcntl.flock(lock_file.fileno(), fcntl.LOCK_UN)

    @staticmethod
    def _record_key(record: dict) -> str:
        path = os.path.normpath(str(record.get("path", "")))
        source_type = str(record.get("source_type", ""))
        ingest_version = str(record.get("ingest_version", ""))
        sha256 = str(record.get("sha256", ""))
        size = str(record.get("size", ""))
        if not path or not source_type or not ingest_version or not sha256:
            return ""
        return "\t".join((source_type, ingest_version, path, size, sha256))


def file_sha256(filepath: str) -> str:
    digest = hashlib.sha256()
    with open(filepath, "rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def build_manifest_record(
    filepath: str, source_type: str, ingest_version: str
) -> dict:
    stat = os.stat(filepath)
    norm_path = os.path.normpath(filepath)
    return {
        "path": norm_path,
        "source_type": source_type,
        "ingest_version": ingest_version,
        "size": stat.st_size,
        "mtime_ns": stat.st_mtime_ns,
        "sha256": file_sha256(filepath),
        "recorded_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


def get_required_env(var_name: str) -> str:
    value = os.getenv(var_name)
    if not value:
        raise RuntimeError(f"Missing required environment variable: {var_name}")
    return value


def _parse_list(value: str) -> list[str]:
    return [s.strip() for s in value.split(",") if s.strip()]


def _parse_bool(value: str | None, default: bool = False) -> bool:
    if value is None or value == "":
        return default
    return value.lower() in ("1", "true", "yes", "on")


@dataclass
class Config:
    milvus_uri: str
    minio_endpoint: str
    bucket_name: str
    milvus_username: str | None
    milvus_password: str | None
    chunk_size: int
    chunk_overlap: int
    tokenizer: str
    embedding_dense_dim: int
    message_client_hostname: str
    message_client_port: int
    access_key: str
    secret_key: str
    split_source_types: list[str]
    hf_access_token: str | None
    drop_collections_before_ingest: bool
    never_drop_collections: bool
    url_collections: list[str]
    pdf_collections: list[str]
    url_base_path: str
    pdf_base_path: str
    file_chunk_size: int
    url_file_chunk_size: int
    pdf_file_chunk_size: int
    client_batch_size: int
    client_concurrency_limit: int
    client_request_timeout_seconds: int
    client_max_job_retries: int
    client_retry_delay_seconds: float
    save_results_to_disk: bool
    results_base_path: str
    manifest_base_path: str
    ingest_version: str
    allow_empty_inputs: bool
    confirm_drop_collections: bool
    status_report_path: str
    run_id: str
    shard_count: int
    shard_index: int
    verify_only: bool
    collection_verify_timeout_seconds: int
    failures_report_path: str = ""


def get_config() -> Config:
    load_dotenv()
    return Config(
        milvus_uri=get_required_env("MILVUS_URI"),
        minio_endpoint=get_required_env("MINIO_ENDPOINT"),
        bucket_name=get_required_env("MINIO_BUCKET"),
        milvus_username=os.getenv("MILVUS_USERNAME", "root"),
        milvus_password=os.getenv("MILVUS_PASSWORD") or os.getenv("MILVUS_ROOT_PASSWORD"),
        chunk_size=int(get_required_env("CHUNK_SIZE")),
        chunk_overlap=int(get_required_env("CHUNK_OVERLAP")),
        tokenizer=get_required_env("TOKENIZER"),
        embedding_dense_dim=int(get_required_env("EMBEDDING_DENSE_DIM")),
        message_client_hostname=get_required_env("NV_INGEST_MESSAGE_HOST"),
        message_client_port=int(get_required_env("NV_INGEST_MESSAGE_PORT")),
        access_key=get_required_env("MINIO_ACCESS_KEY"),
        secret_key=get_required_env("MINIO_SECRET_KEY"),
        split_source_types=DEFAULT_SPLIT_SOURCE_TYPES,
        hf_access_token=os.getenv("HF_ACCESS_TOKEN"),
        drop_collections_before_ingest=os.getenv(
            "DROP_COLLECTIONS_BEFORE_INGEST", ""
        ).lower()
        in ("1", "true", "yes"),
        never_drop_collections=os.getenv(
            "NEVER_DROP_COLLECTIONS", ""
        ).lower()
        in ("1", "true", "yes"),
        url_collections=_parse_list(
            os.getenv("URL_COLLECTIONS", DEFAULT_URL_COLLECTIONS)
        ),
        pdf_collections=_parse_list(
            os.getenv("PDF_COLLECTIONS", DEFAULT_PDF_COLLECTIONS)
        ),
        url_base_path=os.getenv("URL_BASE_PATH", DEFAULT_URL_BASE_PATH),
        pdf_base_path=os.getenv("PDF_BASE_PATH", DEFAULT_PDF_BASE_PATH),
        file_chunk_size=int(os.getenv("FILE_CHUNK_SIZE", str(DEFAULT_FILE_CHUNK_SIZE))),
        url_file_chunk_size=int(
            os.getenv(
                "URL_FILE_CHUNK_SIZE",
                os.getenv("FILE_CHUNK_SIZE", str(DEFAULT_FILE_CHUNK_SIZE)),
            )
        ),
        pdf_file_chunk_size=int(
            os.getenv(
                "PDF_FILE_CHUNK_SIZE",
                os.getenv("FILE_CHUNK_SIZE", str(DEFAULT_FILE_CHUNK_SIZE)),
            )
        ),
        client_batch_size=int(
            os.getenv("CLIENT_BATCH_SIZE", str(DEFAULT_CLIENT_BATCH_SIZE))
        ),
        client_concurrency_limit=int(
            os.getenv(
                "CLIENT_CONCURRENCY_LIMIT",
                str(DEFAULT_CLIENT_CONCURRENCY_LIMIT),
            )
        ),
        client_request_timeout_seconds=int(
            os.getenv(
                "CLIENT_REQUEST_TIMEOUT_SECONDS",
                str(DEFAULT_CLIENT_REQUEST_TIMEOUT_SECONDS),
            )
        ),
        client_max_job_retries=int(
            os.getenv(
                "CLIENT_MAX_JOB_RETRIES",
                str(DEFAULT_CLIENT_MAX_JOB_RETRIES),
            )
        ),
        client_retry_delay_seconds=float(
            os.getenv(
                "CLIENT_RETRY_DELAY_SECONDS",
                str(DEFAULT_CLIENT_RETRY_DELAY_SECONDS),
            )
        ),
        save_results_to_disk=_parse_bool(os.getenv("SAVE_RESULTS_TO_DISK"), True),
        results_base_path=os.getenv("RESULTS_BASE_PATH", DEFAULT_RESULTS_BASE_PATH),
        manifest_base_path=os.getenv("MANIFEST_BASE_PATH", MANIFEST_BASE_PATH),
        ingest_version=os.getenv("INGEST_VERSION", DEFAULT_INGEST_VERSION),
        allow_empty_inputs=_parse_bool(os.getenv("ALLOW_EMPTY_INPUTS"), False),
        confirm_drop_collections=_parse_bool(
            os.getenv("CONFIRM_DROP_COLLECTIONS"), False
        ),
        status_report_path=os.getenv("STATUS_REPORT_PATH", ""),
        run_id=os.getenv(
            "INGEST_RUN_ID",
            f"{time.strftime('%Y%m%dT%H%M%SZ', time.gmtime())}-{os.getpid()}",
        ),
        shard_count=int(os.getenv("INGEST_SHARD_COUNT", "1")),
        shard_index=int(os.getenv("INGEST_SHARD_INDEX", "0")),
        verify_only=_parse_bool(os.getenv("INGEST_VERIFY_ONLY"), False),
        collection_verify_timeout_seconds=int(
            os.getenv(
                "COLLECTION_VERIFY_TIMEOUT_SECONDS",
                str(DEFAULT_COLLECTION_VERIFY_TIMEOUT_SECONDS),
            )
        ),
        failures_report_path=os.getenv("FAILURES_REPORT_PATH", ""),
    )


def validate_config(config: Config) -> None:
    int_fields = {
        "chunk_size": config.chunk_size,
        "embedding_dense_dim": config.embedding_dense_dim,
        "message_client_port": config.message_client_port,
        "file_chunk_size": config.file_chunk_size,
        "url_file_chunk_size": config.url_file_chunk_size,
        "pdf_file_chunk_size": config.pdf_file_chunk_size,
        "client_batch_size": config.client_batch_size,
        "client_concurrency_limit": config.client_concurrency_limit,
        "client_request_timeout_seconds": config.client_request_timeout_seconds,
        "client_max_job_retries": config.client_max_job_retries,
        "collection_verify_timeout_seconds": config.collection_verify_timeout_seconds,
        "shard_count": config.shard_count,
    }
    for name, value in int_fields.items():
        if value < 1:
            raise RuntimeError(f"{name} must be >= 1")
    if config.chunk_overlap < 0:
        raise RuntimeError("chunk_overlap must be >= 0")
    if config.chunk_overlap >= config.chunk_size:
        raise RuntimeError("chunk_overlap must be smaller than chunk_size")
    if config.client_retry_delay_seconds <= 0:
        raise RuntimeError("client_retry_delay_seconds must be > 0")
    if not 0 <= config.shard_index < config.shard_count:
        raise RuntimeError("INGEST_SHARD_INDEX must be in [0, INGEST_SHARD_COUNT)")
    if not config.split_source_types:
        raise RuntimeError("split_source_types must not be empty")
    if not config.ingest_version.strip():
        raise RuntimeError("INGEST_VERSION must not be empty")
    if config.drop_collections_before_ingest and config.never_drop_collections:
        raise RuntimeError(
            "DROP_COLLECTIONS_BEFORE_INGEST and NEVER_DROP_COLLECTIONS cannot both be true"
        )
    if config.drop_collections_before_ingest and not config.confirm_drop_collections:
        raise RuntimeError(
            "CONFIRM_DROP_COLLECTIONS=true is required when DROP_COLLECTIONS_BEFORE_INGEST is enabled"
        )

    for collection in sorted(set(config.url_collections + config.pdf_collections)):
        if not COLLECTION_NAME_RE.match(collection):
            raise RuntimeError(
                f"Invalid collection name {collection!r}; use lowercase letters, digits, hyphen, or underscore"
            )
    if not config.url_collections and not config.pdf_collections:
        raise RuntimeError("At least one URL or PDF collection must be configured")

    for name, path in (
        ("URL_BASE_PATH", config.url_base_path),
        ("PDF_BASE_PATH", config.pdf_base_path),
    ):
        if path and not Path(path).is_absolute():
            raise RuntimeError(f"{name} must be an absolute path: {path}")


def drop_collections_if_requested(
    client: MilvusClient,
    collection_names: list[str],
) -> None:
    if not collection_names:
        return
    for name in sorted(collection_names):
        logger.info("Dropping collection: collection=%s", name)
        try:
            if hasattr(client, "has_collection") and not client.has_collection(name):
                logger.info("Collection already absent: collection=%s", name)
                continue
            client.drop_collection(name)
        except Exception as e:
            if _is_not_found_error(e):
                logger.info("Collection already absent: collection=%s", name)
                continue
            raise RuntimeError(
                f"Failed to drop collection {name!r}; refusing to ingest into possibly stale schema"
            ) from e


def _is_not_found_error(error: Exception) -> bool:
    values = [
        str(error),
        type(error).__name__,
        str(getattr(error, "code", "")),
        str(getattr(error, "error_code", "")),
    ]
    text = " ".join(values).lower()
    return any(
        marker in text
        for marker in (
            "not found",
            "not exist",
            "doesn't exist",
            "does not exist",
            "collectionnotexist",
        )
    )


def build_manifest(
    client: MilvusClient, collection: str, base_path: str
) -> IngestManifest:
    manifest = IngestManifest(collection, base_path)
    if not manifest.enabled or manifest.count == 0:
        return manifest

    existing_collections = set(client.list_collections())
    if collection in existing_collections:
        return manifest

    logger.warning(
        "Clearing stale manifest because Milvus collection is missing: "
        "collection=%s path=%s entries=%s",
        collection,
        manifest.path,
        manifest.count,
    )
    manifest.clear()
    return manifest


def _log_failures(
    source_type: str,
    collection: str,
    chunk_num: int,
    total_chunks: int,
    failures: list,
) -> None:
    if not failures:
        return
    n = len(failures)
    logger.warning(
        "Ingest failures: source=%s collection=%s chunk=%s/%s count=%s",
        source_type,
        collection,
        chunk_num,
        total_chunks,
        n,
    )
    sample = failures[:5]
    for i, f in enumerate(sample):
        logger.warning("  failure[%s]: %s", i, f)
    if n > 5:
        logger.warning("  ... and %s more", n - 5)


def _json_safe(value):
    try:
        json.dumps(value)
        return value
    except TypeError:
        return repr(value)


def _write_json_file(path: str, payload: dict | list) -> None:
    report_dir = os.path.dirname(path)
    if report_dir:
        os.makedirs(report_dir, exist_ok=True)
    target_dir = report_dir or "."
    fd, temp_path = tempfile.mkstemp(prefix=".ingest-report-", dir=target_dir)
    try:
        with os.fdopen(fd, "w") as f:
            json.dump(payload, f, indent=2, sort_keys=True)
            f.write("\n")
            f.flush()
            os.fsync(f.fileno())
        os.replace(temp_path, path)
    except BaseException:
        try:
            os.unlink(temp_path)
        except FileNotFoundError:
            pass
        raise


SourceType = Literal["url", "pdf"]


def _discover_files(
    base_path: str, collection: str, extensions: tuple[str, ...]
) -> list[str]:
    """Glob for files matching any of the given extensions under a collection directory."""
    files: list[str] = []
    for ext in extensions:
        files.extend(
            glob.glob(os.path.join(base_path, collection, "**", ext), recursive=True)
        )
    return sorted(set(files))


def _chunk_bytes(files: list[str]) -> int:
    total = 0
    for fp in files:
        try:
            total += os.path.getsize(fp)
        except OSError:
            logger.warning("Could not stat file for chunk byte count: %s", fp)
    return total


def _status_to_completed_files(
    files: list[str],
    status: dict[str, str],
) -> list[str]:
    if not status:
        return []

    normalized_status = {
        os.path.normpath(str(key)): str(value).lower()
        for key, value in status.items()
    }
    basename_counts = Counter(os.path.basename(os.path.normpath(fp)) for fp in files)
    completed: list[str] = []
    for fp in files:
        norm = os.path.normpath(fp)
        candidates = [norm, os.path.abspath(norm)]
        basename = os.path.basename(norm)
        # Some NV-Ingest client releases report only a basename. It is safe to
        # use that fallback only when it identifies exactly one submitted file.
        if basename_counts[basename] == 1:
            candidates.append(basename)
        if any(normalized_status.get(candidate) == "completed" for candidate in candidates):
            completed.append(fp)
    return completed


def _submitted_completed_files(files: list[str], ingestor, failures: list) -> list[str]:
    """Map the pinned 26.3 client's submitted successful jobs back to inputs.

    Synchronous ``Ingestor.ingest`` does not populate ``get_status()``, so use
    its submitted job IDs and source names as the accounting ledger. Files
    skipped locally before job creation are intentionally absent and therefore
    fail accounting instead of being added to the manifest.
    """
    completed = set(_status_to_completed_files(files, ingestor.get_status()))
    job_ids = list(getattr(ingestor, "_job_ids", None) or [])
    client = getattr(ingestor, "_client", None)
    job_specs = getattr(client, "_job_index_to_job_spec", {}) if client else {}
    if not job_ids or not isinstance(job_specs, dict):
        return sorted(completed)

    failed_job_ids: set[str] = set()
    unparseable_failure = False
    for failure in failures:
        if isinstance(failure, (list, tuple)) and failure:
            failed_job_ids.add(str(failure[0]).split(":", 1)[0])
        else:
            unparseable_failure = True
    if failures and unparseable_failure:
        return sorted(completed)

    submitted_status: dict[str, str] = {}
    for job_id in job_ids:
        spec = job_specs.get(job_id)
        source_name = getattr(spec, "source_name", "") if spec else ""
        if source_name:
            submitted_status[str(source_name)] = (
                "failed" if str(job_id) in failed_job_ids else "completed"
            )
    completed.update(_status_to_completed_files(files, submitted_status))
    return sorted(completed)


def _select_shard(
    files: list[str], source_type: SourceType, shard_count: int, shard_index: int
) -> list[str]:
    """Select a stable, disjoint subset of files for a retryable ingest shard."""
    if shard_count == 1:
        return files
    selected: list[str] = []
    for filepath in files:
        identity = f"{source_type}\0{os.path.normpath(filepath)}".encode("utf-8")
        bucket = int.from_bytes(hashlib.sha256(identity).digest()[:8], "big") % shard_count
        if bucket == shard_index:
            selected.append(filepath)
    return selected


def verify_collection(
    client: MilvusClient, collection: str, timeout_seconds: int
) -> int:
    """Wait for a populated collection and return its visible entity count."""
    deadline = time.monotonic() + timeout_seconds
    last_error = "collection is not visible"
    while time.monotonic() < deadline:
        try:
            rpc_timeout = min(30, max(1, timeout_seconds))
            if client.has_collection(collection, timeout=rpc_timeout):
                stats = client.get_collection_stats(collection, timeout=rpc_timeout)
                row_count = int(stats.get("row_count", 0))
                if row_count > 0:
                    logger.info(
                        "Verified populated Milvus collection: collection=%s entities=%s",
                        collection,
                        row_count,
                    )
                    return row_count
                last_error = "collection exists but row_count is zero"
            else:
                last_error = "collection does not exist"
        except Exception as exc:  # readiness is retried until the bounded deadline
            last_error = f"{type(exc).__name__}: {exc}"
        time.sleep(5)
    raise RuntimeError(
        f"Milvus collection {collection!r} was not populated within "
        f"{timeout_seconds}s ({last_error})"
    )


def verify_object_store(config: Config) -> None:
    """Prove the configured MinIO credentials, bucket, and write path."""
    endpoint = config.minio_endpoint.strip()
    secure = endpoint.startswith("https://")
    endpoint = endpoint.removeprefix("https://").removeprefix("http://")
    if "/" in endpoint:
        raise RuntimeError("MINIO_ENDPOINT must contain only host and port")
    client = Minio(
        endpoint,
        access_key=config.access_key,
        secret_key=config.secret_key,
        secure=secure,
    )
    if not client.bucket_exists(config.bucket_name):
        raise RuntimeError(f"MinIO bucket {config.bucket_name!r} does not exist")
    object_name = f"_daedalus-readiness/{config.run_id}.txt"
    payload = b"ready"
    client.put_object(
        config.bucket_name,
        object_name,
        io.BytesIO(payload),
        length=len(payload),
        content_type="text/plain",
    )
    try:
        stat = client.stat_object(config.bucket_name, object_name)
        if int(stat.size) != len(payload):
            raise RuntimeError("MinIO readiness object size did not match")
    finally:
        client.remove_object(config.bucket_name, object_name)
    logger.info(
        "Verified authenticated MinIO write path: endpoint=%s bucket=%s",
        config.minio_endpoint,
        config.bucket_name,
    )


def verify_milvus_access(
    client: MilvusClient, collections: list[str], timeout_seconds: int
) -> None:
    """Fail before extraction when the configured identity cannot inspect collections."""
    rpc_timeout = min(30, max(1, timeout_seconds))
    for collection in collections:
        # A missing collection returns False. An authorization, connectivity,
        # or credential defect raises here before NV-Ingest performs expensive
        # extraction and embedding work.
        client.has_collection(collection, timeout=rpc_timeout)
    logger.info(
        "Verified authenticated Milvus collection access: collections=%s",
        collections,
    )


def create_milvus_client(config: Config) -> MilvusClient:
    """Create an isolated metadata client that can be closed independently."""
    kwargs = {
        "uri": config.milvus_uri,
        "timeout": min(30, config.client_request_timeout_seconds),
    }
    if config.milvus_username and config.milvus_password:
        kwargs["token"] = f"{config.milvus_username}:{config.milvus_password}"
    return MilvusClient(**kwargs)


def close_milvus_client(client: MilvusClient) -> None:
    close = getattr(client, "close", None)
    if callable(close):
        try:
            close()
        except Exception as exc:
            logger.warning("Could not close Milvus metadata client: %s", exc)


def build_manifest_with_fresh_client(
    config: Config, collection: str
) -> IngestManifest:
    """Build manifest state without retaining a channel the uploader may close."""
    client = create_milvus_client(config)
    try:
        return build_manifest(client, collection, config.manifest_base_path)
    finally:
        close_milvus_client(client)


def run_ingest_for_collection(
    config: Config,
    collection: str,
    files: list[str],
    source_type: SourceType,
    msg_counter_suffix: str,
    manifest: IngestManifest | None = None,
    allow_empty_source: bool = False,
) -> list[dict]:
    if not files:
        logger.info(
            "No files found for collection=%s source=%s", collection, source_type
        )
        if config.allow_empty_inputs or allow_empty_source:
            return []
        return [
            {
                "collection": collection,
                "source_type": source_type,
                "error": "no input files discovered",
                "exception_type": "EmptyInputError",
            }
        ]

    # Filter out already-ingested files via manifest
    if manifest is not None:
        original_count = len(files)
        files = manifest.filter_new(files, source_type, config.ingest_version)
        skipped = original_count - len(files)
        if skipped:
            logger.info(
                "Manifest filtered: collection=%s total=%s new=%s skipped=%s",
                collection,
                original_count,
                len(files),
                skipped,
            )
        if not files:
            logger.info(
                "All files already ingested for collection=%s source=%s",
                collection,
                source_type,
            )
            return []

    logger.info(
        "Starting ingest: collection=%s source=%s files=%s",
        collection,
        source_type,
        len(files),
    )

    all_failures: list[dict] = []
    chunk_size = (
        config.pdf_file_chunk_size
        if source_type == "pdf"
        else config.url_file_chunk_size
    )
    if chunk_size < 1:
        raise RuntimeError(f"{source_type.upper()} file chunk size must be >= 1")

    total_chunks = math.ceil(len(files) / chunk_size)
    split_params = {
        "split_source_types": config.split_source_types,
        "hf_access_token": config.hf_access_token,
    }

    def _process_chunk(chunk_num: int, file_chunk: list[str]) -> list[dict]:
        """Process a single file chunk; returns a list of failure dicts."""
        logger.info(
            "%s chunk %s/%s: collection=%s files=%s",
            source_type.upper(),
            chunk_num,
            total_chunks,
            collection,
            len(file_chunk),
        )
        chunk_bytes = _chunk_bytes(file_chunk)
        logger.info(
            "%s chunk %s/%s size: collection=%s bytes=%s mib=%.2f",
            source_type.upper(),
            chunk_num,
            total_chunks,
            collection,
            chunk_bytes,
            chunk_bytes / 1024 / 1024,
        )

        try:
            client_kw = {
                "message_client_port": config.message_client_port,
                "message_client_hostname": config.message_client_hostname,
                "msg_counter_id": f"nv-ingest-{msg_counter_suffix}-{collection}-{config.run_id}",
                # NV-Ingest 26.3.0 accepts concurrency_limit in ingest() but
                # does not apply it to the client's executor. Configure the
                # actual worker pool so this operator-facing limit is real.
                "worker_pool_size": config.client_concurrency_limit,
            }
            vdb_kw = {
                "collection_name": collection,
                "milvus_uri": config.milvus_uri,
                "gpu_index": False,
                "gpu_search": False,
                "dense_dim": config.embedding_dense_dim,
                "recreate": False,
                "minio_endpoint": config.minio_endpoint,
                "bucket_name": config.bucket_name,
                "access_key": config.access_key,
                "secret_key": config.secret_key,
                "username": config.milvus_username,
                "password": config.milvus_password,
                "stream": True,
                "threshold": 6400,
                "filter_errors": True,
            }

            if source_type == "pdf":
                extract_kw = dict(
                    extract_text=True,
                    extract_tables=True,
                    extract_charts=False,
                    extract_images=False,
                    table_output_format="markdown",
                    extract_infographics=False,
                )
            else:
                extract_kw = {}

            builder = (
                Ingestor(
                    client=BoundedNvIngestClient(
                        request_timeout_seconds=config.client_request_timeout_seconds,
                        retry_delay_seconds=config.client_retry_delay_seconds,
                        **client_kw,
                    )
                )
                .files(file_chunk)
                .extract(**extract_kw)
                .split(
                    tokenizer=config.tokenizer,
                    chunk_size=config.chunk_size,
                    chunk_overlap=config.chunk_overlap,
                    params=split_params,
                )
                .embed()
                .vdb_upload(**vdb_kw)
            )

            if config.save_results_to_disk:
                results_dir = os.path.join(
                    config.results_base_path,
                    collection,
                    source_type,
                    f"chunk-{chunk_num}",
                )
                os.makedirs(results_dir, exist_ok=True)
                builder = builder.save_to_disk(
                    output_directory=results_dir,
                    cleanup=True,
                    compression="gzip",
                )

            chunk_failures: list[dict] = []
            with builder as ingestor:
                start_time = time.monotonic()
                results, failures = ingestor.ingest(
                    show_progress=True,
                    return_failures=True,
                    batch_size=config.client_batch_size,
                    concurrency_limit=config.client_concurrency_limit,
                    timeout=config.client_request_timeout_seconds,
                    max_job_retries=config.client_max_job_retries,
                    retry_delay=config.client_retry_delay_seconds,
                    fail_on_submit_error=True,
                )
                elapsed = time.monotonic() - start_time
                logger.info(
                    "%s chunk %s/%s done: collection=%s elapsed_seconds=%.2f successes=%s failures=%s",
                    source_type.upper(),
                    chunk_num,
                    total_chunks,
                    collection,
                    elapsed,
                    len(results),
                    len(failures),
                )
                _log_failures(
                    source_type, collection, chunk_num, total_chunks, failures
                )
                for f in failures:
                    chunk_failures.append(
                        {
                            "collection": collection,
                            "source_type": source_type,
                            "chunk": chunk_num,
                            "failure": _json_safe(f),
                        }
                    )

                completed_files = _submitted_completed_files(
                    file_chunk, ingestor, failures
                )
                completed_set = {os.path.normpath(filepath) for filepath in completed_files}
                unaccounted_files = [
                    filepath
                    for filepath in file_chunk
                    if os.path.normpath(filepath) not in completed_set
                ]
                if manifest is not None and completed_files:
                    manifest.mark_ingested(
                        completed_files,
                        source_type,
                        config.ingest_version,
                    )
                    logger.info(
                        "Manifest marked exact completed files: collection=%s "
                        "source=%s chunk=%s/%s count=%s",
                        collection,
                        source_type,
                        chunk_num,
                        total_chunks,
                        len(completed_files),
                    )
                if unaccounted_files:
                    # Empty returned failures are not proof that every local
                    # file was submitted. NV-Ingest may skip unreadable inputs
                    # before submission, so require a completed status for each.
                    chunk_failures.append(
                        {
                            "collection": collection,
                            "source_type": source_type,
                            "chunk": chunk_num,
                            "error": "submitted/completed file accounting mismatch",
                            "exception_type": "IncompleteFileAccountingError",
                            "unaccounted_files": unaccounted_files,
                        }
                    )
                    logger.error(
                        "Incomplete file accounting: collection=%s source=%s "
                        "chunk=%s/%s submitted=%s completed=%s unaccounted=%s",
                        collection,
                        source_type,
                        chunk_num,
                        total_chunks,
                        len(file_chunk),
                        len(completed_files),
                        len(unaccounted_files),
                    )

            return chunk_failures

        except (OSError, ConnectionError, ValueError) as e:
            logger.exception(
                "Expected error during ingest: collection=%s source=%s error=%s",
                collection,
                source_type,
                e,
            )
            return [
                {
                    "collection": collection,
                    "source_type": source_type,
                    "chunk": chunk_num,
                    "files": file_chunk,
                    "error": str(e),
                    "exception_type": type(e).__name__,
                }
            ]
        except Exception:
            logger.exception(
                "Unexpected error during ingest: collection=%s source=%s",
                collection,
                source_type,
            )
            raise

    for i in range(total_chunks):
        chunk = files[i * chunk_size : (i + 1) * chunk_size]
        all_failures.extend(_process_chunk(i + 1, chunk))
        gc.collect()

    return all_failures


def _write_run_reports(
    config: Config,
    state: str,
    collection_summaries: list[dict],
    all_failures: list[dict],
) -> None:
    if config.failures_report_path:
        _write_json_file(config.failures_report_path, all_failures)
        logger.info(
            "Wrote failures report: path=%s count=%s",
            config.failures_report_path,
            len(all_failures),
        )
    if config.status_report_path:
        _write_json_file(
            config.status_report_path,
            {
                "run_id": config.run_id,
                "ingest_version": config.ingest_version,
                "state": state,
                "failure_count": len(all_failures),
                "shard_count": config.shard_count,
                "shard_index": config.shard_index,
                "collections": collection_summaries,
                "updated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            },
        )
        logger.info(
            "Wrote status report: path=%s state=%s",
            config.status_report_path,
            state,
        )


def _run_main() -> None:
    config = get_config()
    configured_url_collections = set(config.url_collections)

    # Single-collection mode: override collections from env
    ingest_mode = os.getenv("INGEST_MODE", "all")
    if ingest_mode == "single":
        single_collection = os.getenv("SINGLE_COLLECTION", "")
        if not single_collection:
            raise RuntimeError(
                "SINGLE_COLLECTION is required when INGEST_MODE=single"
            )
        include_pdfs = os.getenv("INCLUDE_PDFS", "").lower() in ("1", "true", "yes")
        logger.info(
            "Single-collection mode: collection=%s include_pdfs=%s",
            single_collection,
            include_pdfs,
        )
        config.url_collections = (
            [single_collection] if single_collection in configured_url_collections else []
        )
        config.pdf_collections = [single_collection] if include_pdfs else []
    elif ingest_mode != "all":
        raise RuntimeError("INGEST_MODE must be 'all' or 'single'")

    validate_config(config)

    try:
        _write_run_reports(config, "running", [], [])
    except OSError as exc:
        logger.warning("Could not write initial run report: %s", exc)

    if not config.hf_access_token:
        logger.warning("HF_ACCESS_TOKEN is not set; embeddings may fail.")

    logger.info(
        "Ingest run configured: run_id=%s ingest_version=%s mode=%s "
        "url_collections=%s pdf_collections=%s shard=%s/%s verify_only=%s",
        config.run_id,
        config.ingest_version,
        ingest_mode,
        config.url_collections,
        config.pdf_collections,
        config.shard_index,
        config.shard_count,
        config.verify_only,
    )

    logger.info(
        "NvIngest client configured: host=%s port=%s",
        config.message_client_hostname,
        config.message_client_port,
    )
    logger.info(
        "Storage configured: milvus_uri=%s minio_endpoint=%s bucket=%s",
        config.milvus_uri,
        config.minio_endpoint,
        config.bucket_name,
    )
    logger.info("File chunk size: %s", config.file_chunk_size)
    logger.info("URL file chunk size: %s", config.url_file_chunk_size)
    logger.info("PDF file chunk size: %s", config.pdf_file_chunk_size)
    logger.info("Client batch size: %s", config.client_batch_size)
    logger.info("Client concurrency limit: %s", config.client_concurrency_limit)
    logger.info("Save results to disk: %s", config.save_results_to_disk)

    pipeline_collections = sorted(set(config.url_collections + config.pdf_collections))
    verify_object_store(config)
    metadata_client = create_milvus_client(config)
    try:
        verify_milvus_access(
            metadata_client,
            pipeline_collections,
            config.client_request_timeout_seconds,
        )

        if config.verify_only:
            collection_summaries = []
            for collection in pipeline_collections:
                row_count = verify_collection(
                    metadata_client,
                    collection,
                    config.collection_verify_timeout_seconds,
                )
                collection_summaries.append(
                    {
                        "collection": collection,
                        "source_type": "verification",
                        "entity_count": row_count,
                        "failure_count": 0,
                        "state": "complete",
                    }
                )
            _write_run_reports(config, "complete", collection_summaries, [])
            logger.info("Collection verification completed successfully")
            return

        if config.never_drop_collections:
            logger.info(
                "NEVER_DROP_COLLECTIONS is set; collection drops are disabled"
            )
        elif config.drop_collections_before_ingest:
            drop_collections_if_requested(metadata_client, pipeline_collections)
        else:
            logger.info("Skipping collection drop (DROP_COLLECTIONS_BEFORE_INGEST not set)")
    finally:
        close_milvus_client(metadata_client)

    all_failures: list[dict] = []
    collection_summaries: list[dict] = []

    # Ingest URL collections (.html + .md)
    for c in config.url_collections:
        manifest = build_manifest_with_fresh_client(config, c)
        discovered_files = _discover_files(config.url_base_path, c, URL_FILE_EXTENSIONS)
        files = _select_shard(
            discovered_files, "url", config.shard_count, config.shard_index
        )
        failures = run_ingest_for_collection(
            config,
            c,
            files,
            "url",
            "url",
            manifest,
            allow_empty_source=config.shard_count > 1,
        )
        all_failures.extend(failures)
        collection_summaries.append(
            {
                "collection": c,
                "source_type": "url",
                "discovered_files": len(discovered_files),
                "selected_files": len(files),
                "failure_count": len(failures),
                "state": "failed" if failures else "complete",
            }
        )

    # Ingest PDF collections
    url_collection_set = set(config.url_collections)
    for c in config.pdf_collections:
        manifest = build_manifest_with_fresh_client(config, c)
        discovered_files = _discover_files(config.pdf_base_path, c, ("*.pdf",))
        files = _select_shard(
            discovered_files, "pdf", config.shard_count, config.shard_index
        )
        failures = run_ingest_for_collection(
            config,
            c,
            files,
            "pdf",
            "pdf",
            manifest,
            allow_empty_source=c in url_collection_set or config.shard_count > 1,
        )
        all_failures.extend(failures)
        collection_summaries.append(
            {
                "collection": c,
                "source_type": "pdf",
                "discovered_files": len(discovered_files),
                "selected_files": len(files),
                "failure_count": len(failures),
                "state": "failed" if failures else "complete",
            }
        )

    if all_failures:
        try:
            _write_run_reports(config, "failed", collection_summaries, all_failures)
        except OSError as exc:
            logger.warning("Could not write failed run report: %s", exc)
        raise RuntimeError(f"Ingestion completed with {len(all_failures)} failure(s)")

    # A sharded run is followed by one explicit verification Job after every
    # shard succeeds. Scheduled and other unsharded runs verify inline.
    if config.shard_count == 1:
        verification_client = create_milvus_client(config)
        try:
            entity_counts = {
                collection: verify_collection(
                    verification_client,
                    collection,
                    config.collection_verify_timeout_seconds,
                )
                for collection in pipeline_collections
            }
        finally:
            close_milvus_client(verification_client)
        for summary in collection_summaries:
            summary["entity_count"] = entity_counts[summary["collection"]]

    try:
        _write_run_reports(config, "complete", collection_summaries, [])
    except OSError as exc:
        logger.warning("Could not write completed run report: %s", exc)

    logger.info("Ingestion completed successfully")


def _record_unhandled_failure(error: BaseException) -> None:
    """Persist a terminal report even when setup or a dependency raises."""
    status_path = os.getenv("STATUS_REPORT_PATH", "")
    failures_path = os.getenv("FAILURES_REPORT_PATH", "")
    failure = {
        "run_id": os.getenv("INGEST_RUN_ID", "unknown"),
        "error": str(error),
        "exception_type": type(error).__name__,
        "recorded_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }
    try:
        payload: dict = {}
        if status_path and os.path.exists(status_path):
            with open(status_path, "r") as f:
                existing = json.load(f)
                if isinstance(existing, dict):
                    payload = existing
        already_reported = payload.get("state") == "failed" and int(
            payload.get("failure_count", 0)
        ) > 0
        if failures_path and not already_reported:
            failures: list = []
            if os.path.exists(failures_path):
                with open(failures_path, "r") as f:
                    existing = json.load(f)
                    if isinstance(existing, list):
                        failures = existing
            failures.append(failure)
            _write_json_file(failures_path, failures)
        if status_path:
            payload.update(
                {
                    "run_id": failure["run_id"],
                    "state": "failed",
                    "terminal_error": failure,
                    "updated_at": failure["recorded_at"],
                }
            )
            payload["failure_count"] = max(1, int(payload.get("failure_count", 0)))
            _write_json_file(status_path, payload)
    except Exception as report_error:
        logger.error("Could not persist terminal failure report: %s", report_error)


def _raise_on_termination(signum, _frame) -> None:
    raise RuntimeError(f"Ingestion process received signal {signum}")


def main() -> None:
    previous_handler = signal.signal(signal.SIGTERM, _raise_on_termination)
    try:
        _run_main()
    except BaseException as error:
        _record_unhandled_failure(error)
        raise
    finally:
        signal.signal(signal.SIGTERM, previous_handler)


if __name__ == "__main__":
    raise SystemExit("Legacy append ingestion is retired. Use the root make refresh-run target.")
