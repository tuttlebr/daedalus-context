from __future__ import annotations

import importlib.util
import json
import sys
import tempfile
import types
import unittest

from pathlib import Path


def load_module():
    client_module = types.ModuleType("nv_ingest_client.client")
    client_module.Ingestor = object

    class FakeNvIngestClient:
        def __init__(self, **kwargs):
            self.init_kwargs = kwargs
            self.fetch_calls = []
            self.fetch_result = object()
            self.fetch_error = None

        def fetch_job_result_async(self, job_ids, data_only=True, timeout=None):
            self.fetch_calls.append((job_ids, data_only, timeout))
            return {"timeout": timeout}

        def _fetch_job_result(self, *args, **kwargs):
            if self.fetch_error is not None:
                raise self.fetch_error
            return self.fetch_result

    client_module.NvIngestClient = FakeNvIngestClient
    sys.modules.setdefault("nv_ingest_client", types.ModuleType("nv_ingest_client"))
    sys.modules["nv_ingest_client.client"] = client_module

    logging_module = types.ModuleType("nv_ingest_api.util.logging.configuration")
    logging_module.configure_logging = lambda _level: None
    for name in ("nv_ingest_api", "nv_ingest_api.util", "nv_ingest_api.util.logging"):
        sys.modules.setdefault(name, types.ModuleType(name))
    sys.modules["nv_ingest_api.util.logging.configuration"] = logging_module

    pymilvus = types.ModuleType("pymilvus")
    pymilvus.MilvusClient = object
    sys.modules["pymilvus"] = pymilvus
    minio = types.ModuleType("minio")
    minio.Minio = object
    sys.modules["minio"] = minio

    path = Path(__file__).parents[1] / "nv-ingest-ms-tasks.py"
    spec = importlib.util.spec_from_file_location("nv_ingest_tasks", path)
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


MODULE = load_module()


class ManifestRecorder:
    def __init__(self):
        self.completed = []

    def filter_new(self, files, _source_type, _ingest_version):
        return files

    def mark_ingested(self, files, _source_type, _ingest_version):
        self.completed.extend(files)


class FakeIngestor:
    statuses = {}
    submitted_source_names = []

    def __init__(self, client):
        self.client = client
        self._job_ids = [f"job-{index}" for index, _ in enumerate(self.submitted_source_names)]
        specs = {
            job_id: types.SimpleNamespace(source_name=source_name)
            for job_id, source_name in zip(self._job_ids, self.submitted_source_names)
        }
        self._client = types.SimpleNamespace(_job_index_to_job_spec=specs)

    def files(self, _files):
        return self

    def extract(self, **_kwargs):
        return self

    def split(self, **_kwargs):
        return self

    def embed(self):
        return self

    def vdb_upload(self, **_kwargs):
        return self

    def __enter__(self):
        return self

    def __exit__(self, *_args):
        return False

    def ingest(self, **_kwargs):
        return [object()], []

    def get_status(self):
        return self.statuses


class NvIngestTaskTests(unittest.TestCase):
    def test_http_session_pool_matches_the_worker_pool(self):
        session = MODULE._http_session_allocator(16)()
        try:
            self.assertEqual(
                16,
                session.get_adapter("http://").poolmanager.connection_pool_kw[
                    "maxsize"
                ],
            )
            self.assertIs(
                session.get_adapter("http://"),
                session.get_adapter("https://"),
            )
        finally:
            session.close()

    def test_async_fetch_uses_a_bounded_transport_timeout(self):
        client = MODULE.BoundedNvIngestClient(
            request_timeout_seconds=60,
            retry_delay_seconds=5,
            message_client_hostname="nv-ingest",
            message_client_port=7670,
            worker_pool_size=16,
        )

        result = client.fetch_job_result_async(["job-1"], data_only=False)

        self.assertEqual({"timeout": (5.0, 60.0)}, result)
        self.assertEqual(
            (["job-1"], False, (5.0, 60.0)),
            client.fetch_calls[-1],
        )
        message_client_kwargs = dict(client.init_kwargs["message_client_kwargs"])
        allocator = message_client_kwargs.pop("http_allocator")
        self.assertTrue(callable(allocator))
        self.assertEqual("bounded_http_session", allocator.__name__)
        self.assertEqual(
            {
                "default_connect_timeout": 5.0,
                "default_read_timeout": 60.0,
                "max_retries": 1,
            },
            message_client_kwargs,
        )
        self.assertEqual(16, client.init_kwargs["worker_pool_size"])

    def test_async_fetch_preserves_an_explicit_timeout(self):
        client = MODULE.BoundedNvIngestClient(
            request_timeout_seconds=60,
            retry_delay_seconds=5,
        )

        client.fetch_job_result_async("job-1", timeout=(2, 15))

        self.assertEqual(("job-1", True, (2, 15)), client.fetch_calls[-1])

    def test_not_ready_fetch_honors_retry_delay(self):
        client = MODULE.BoundedNvIngestClient(
            request_timeout_seconds=60,
            retry_delay_seconds=5,
        )
        client.fetch_error = TimeoutError("not ready")
        sleeps = []
        original_sleep = MODULE.time.sleep
        MODULE.time.sleep = sleeps.append
        try:
            with self.assertRaises(TimeoutError):
                client._fetch_job_result("job-1")
        finally:
            MODULE.time.sleep = original_sleep

        self.assertEqual([5.0], sleeps)

    def test_manifest_builder_uses_an_isolated_client_and_closes_it(self):
        events = []
        fake_client = types.SimpleNamespace(close=lambda: events.append("close"))
        expected_manifest = object()
        config = types.SimpleNamespace(
            milvus_uri="http://milvus:19530",
            client_request_timeout_seconds=60,
            milvus_username="root",
            milvus_password="password",
            manifest_base_path="/manifests",
        )

        original_client_factory = MODULE.create_milvus_client
        original_build_manifest = MODULE.build_manifest
        MODULE.create_milvus_client = lambda received: (
            events.append(("create", received)) or fake_client
        )
        MODULE.build_manifest = lambda client, collection, path: (
            events.append(("build", client, collection, path)) or expected_manifest
        )
        try:
            actual_manifest = MODULE.build_manifest_with_fresh_client(config, "nvidia")
        finally:
            MODULE.create_milvus_client = original_client_factory
            MODULE.build_manifest = original_build_manifest

        self.assertIs(expected_manifest, actual_manifest)
        self.assertEqual(
            [
                ("create", config),
                ("build", fake_client, "nvidia", "/manifests"),
                "close",
            ],
            events,
        )

    def test_milvus_preflight_checks_every_target_collection(self):
        calls = []

        class FakeMilvus:
            def has_collection(self, collection, timeout):
                calls.append((collection, timeout))
                return False

        MODULE.verify_milvus_access(FakeMilvus(), ["mentalhealth", "nvidia"], 60)
        self.assertEqual([("mentalhealth", 30), ("nvidia", 30)], calls)

    def test_milvus_preflight_propagates_authorization_failure(self):
        class ForbiddenMilvus:
            def has_collection(self, _collection, timeout):
                raise PermissionError(f"denied after {timeout}s")

        with self.assertRaises(PermissionError):
            MODULE.verify_milvus_access(ForbiddenMilvus(), ["mentalhealth"], 60)

    def test_object_store_readiness_performs_authenticated_write_and_cleanup(self):
        operations = []

        class FakeMinio:
            def __init__(self, endpoint, **kwargs):
                operations.append(("connect", endpoint, kwargs))

            def bucket_exists(self, bucket):
                operations.append(("bucket", bucket))
                return True

            def put_object(self, bucket, name, stream, length, content_type):
                operations.append(("put", bucket, name, stream.read(), length, content_type))

            def stat_object(self, bucket, name):
                operations.append(("stat", bucket, name))
                return types.SimpleNamespace(size=5)

            def remove_object(self, bucket, name):
                operations.append(("remove", bucket, name))

        original = MODULE.Minio
        MODULE.Minio = FakeMinio
        try:
            MODULE.verify_object_store(
                types.SimpleNamespace(
                    minio_endpoint="minio:9000",
                    access_key="access",
                    secret_key="secret",
                    bucket_name="nv-ingest",
                    run_id="test-run",
                )
            )
        finally:
            MODULE.Minio = original
        self.assertEqual("put", operations[2][0])
        self.assertEqual("remove", operations[-1][0])

    def test_shards_are_disjoint_deterministic_and_complete(self):
        files = [f"/workspace/urls/nvidia/{index}.html" for index in range(250)]
        shards = [MODULE._select_shard(files, "url", 8, index) for index in range(8)]
        flattened = [item for shard in shards for item in shard]
        self.assertEqual(set(files), set(flattened))
        self.assertEqual(len(files), len(flattened))
        self.assertEqual(shards[3], MODULE._select_shard(files, "url", 8, 3))

    def test_ambiguous_basename_is_not_treated_as_completed(self):
        files = ["/one/guide.html", "/two/guide.html"]
        self.assertEqual([], MODULE._status_to_completed_files(files, {"guide.html": "completed"}))
        status = {"/one/guide.html": "completed", "guide.html": "completed"}
        self.assertEqual(["/one/guide.html"], MODULE._status_to_completed_files(files, status))

    def test_legacy_manifest_is_compacted_after_upgrade(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / "doc.html"
            source.write_text("content")
            manifest_path = Path(directory) / "nvidia.ingested"
            manifest_path.write_text(f"{source}\n")
            manifest = MODULE.IngestManifest("nvidia", directory)

            self.assertEqual([], manifest.filter_new([str(source)], "url", "v1"))
            lines = manifest_path.read_text().splitlines()
            self.assertTrue(lines)
            self.assertTrue(all(line.startswith("{") for line in lines))
            record = json.loads(lines[0])
            self.assertEqual(str(source), record["path"])
            self.assertEqual(0, len(manifest._legacy_paths))

    def test_manifest_refresh_reads_only_records_appended_since_last_batch(self):
        with tempfile.TemporaryDirectory() as directory:
            sources = [Path(directory) / f"doc-{index}.html" for index in range(3)]
            for index, source in enumerate(sources):
                source.write_text(f"content-{index}")

            manifest = MODULE.IngestManifest("nvidia", directory)
            manifest.mark_ingested([str(sources[0])], "url", "v1")

            externally_appended = MODULE.build_manifest_record(
                str(sources[1]), "url", "v1"
            )
            with open(manifest.path, "a") as manifest_file:
                manifest_file.write(json.dumps(externally_appended, sort_keys=True) + "\n")

            inspected_paths = []
            original_record_key = manifest._record_key

            def record_key(record):
                inspected_paths.append(record["path"])
                return original_record_key(record)

            manifest._record_key = record_key
            manifest.mark_ingested([str(sources[2])], "url", "v1")

            self.assertEqual([str(sources[1]), str(sources[2])], inspected_paths)
            self.assertEqual(3, manifest.count)
            self.assertEqual(Path(manifest.path).stat().st_size, manifest._read_offset)

    def test_unaccounted_local_file_fails_chunk_and_is_not_manifested(self):
        original_ingestor = MODULE.Ingestor
        original_client = MODULE.NvIngestClient
        MODULE.Ingestor = FakeIngestor
        MODULE.NvIngestClient = lambda **_kwargs: object()
        try:
            with tempfile.TemporaryDirectory() as directory:
                first = Path(directory) / "first.html"
                second = Path(directory) / "second.html"
                first.write_text("first")
                second.write_text("second")
                FakeIngestor.statuses = {}
                FakeIngestor.submitted_source_names = [first.name]
                config = types.SimpleNamespace(
                    pdf_file_chunk_size=2,
                    url_file_chunk_size=2,
                    split_source_types=["html"],
                    hf_access_token="token",
                    message_client_port=7670,
                    message_client_hostname="nv-ingest",
                    run_id="test",
                    embedding_dense_dim=2048,
                    milvus_uri="http://milvus:19530",
                    minio_endpoint="minio:9000",
                    bucket_name="milvus-bucket",
                    access_key="access",
                    secret_key="secret",
                    milvus_username="root",
                    milvus_password="password",
                    tokenizer="tokenizer",
                    chunk_size=1024,
                    chunk_overlap=64,
                    save_results_to_disk=False,
                    client_batch_size=2,
                    client_concurrency_limit=2,
                    client_request_timeout_seconds=60,
                    client_max_job_retries=3,
                    client_retry_delay_seconds=1,
                    ingest_version="v1",
                    allow_empty_inputs=False,
                )
                manifest = ManifestRecorder()
                failures = MODULE.run_ingest_for_collection(
                    config,
                    "nvidia",
                    [str(first), str(second)],
                    "url",
                    "url",
                    manifest,
                )
                self.assertEqual([str(first)], manifest.completed)
                self.assertTrue(
                    any(
                        failure.get("exception_type") == "IncompleteFileAccountingError"
                        for failure in failures
                    )
                )
        finally:
            MODULE.Ingestor = original_ingestor
            MODULE.NvIngestClient = original_client


if __name__ == "__main__":
    unittest.main()
