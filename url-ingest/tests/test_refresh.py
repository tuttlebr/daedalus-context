import importlib.util
import json
import math
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from refresh_state import State, RunReport, collection_lock, contract_fingerprint, atomic_json
from refresh_milvus import Store, prepare_rows
from refresh_fetch import fetch, SourceRemoved
from refresh_monitor import status, metrics


class PersistenceTests(unittest.TestCase):
    def test_replays_commits_and_discards_only_partial_final_write(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            s = State(root, "c", "model-a")
            s.record("url", {"ingested_hash": "abc"})
            with s.journal.open("ab") as stream:
                stream.write(b'{"id": "torn')
            restored = State(root, "c", "model-a")
            self.assertEqual(restored.data["documents"]["url"]["ingested_hash"], "abc")
            restored.record("new", {"ok": True})
            self.assertIn("new", State(root, "c", "model-a").data["documents"])
            with self.assertRaises(ValueError):
                State(root, "c", "model-b")

    def test_corrupt_interior_record_does_not_disappear(self):
        with tempfile.TemporaryDirectory() as tmp:
            s = State(Path(tmp), "c", "model-a")
            s.journal.write_text('not-json\n{"id":"x","document":{}}\n')
            with self.assertRaises(ValueError):
                State(Path(tmp), "c", "model-a")

    def test_lock_excludes_second_process_and_releases_on_crash(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            code = 'import sys,time; from pathlib import Path; from refresh_state import collection_lock\nwith collection_lock(Path(sys.argv[1]),"c"):\n print("locked",flush=True);time.sleep(60)'
            worker = subprocess.Popen([sys.executable, "-c", code, tmp], stdout=subprocess.PIPE, text=True,
                                      env={**os.environ, "PYTHONPATH": str(Path(__file__).resolve().parents[1])})
            try:
                self.assertEqual(worker.stdout.readline().strip(), "locked")
                with self.assertRaises(BlockingIOError):
                    with collection_lock(root, "c"):
                        pass
            finally:
                worker.kill(); worker.wait(); worker.stdout.close()
            with collection_lock(root, "c"):
                pass

    def test_failure_history_survives_latest_report_replacement(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            first = RunReport(root, "c", "fingerprint", "one")
            first.fail("url", RuntimeError("unavailable"))
            first.write(force=True)
            second = RunReport(root, "c", "fingerprint", "two")
            self.assertEqual(json.loads(first.path.read_text())["failure_count"], 1)
            self.assertEqual(json.loads(second.latest.read_text())["run_id"], "two")
            self.assertIn("unavailable", first.failures_path.read_text())


class ReplacementTests(unittest.TestCase):
    def row(self, text="hello"):
        return {"text": text, "vector": [1.0, 0.0], "source": {}, "content_metadata": {}}

    def client(self):
        client = Mock()
        client.get.side_effect = lambda name, ids, **kw: [{"pk": pk} for pk in ids]
        client.query_iterator.return_value.next.return_value = []
        return client

    def test_retry_has_same_ids_and_changed_version_has_different_ids(self):
        row = self.row()
        a = prepare_rows("source", "hash-a", "contract", [row], 2)
        self.assertEqual(a, prepare_rows("source", "hash-a", "contract", [row], 2))
        self.assertNotEqual(a[0]["pk"], prepare_rows("source", "hash-b", "contract", [row], 2)[0]["pk"])

    def test_failed_write_preserves_previous_document(self):
        client = self.client(); client.upsert.side_effect = RuntimeError("unavailable")
        with self.assertRaises(RuntimeError):
            Store(client, "collection", "contract", 2).replace("source", "hash", [self.row()])
        client.delete.assert_not_called()

    def test_invisible_replacement_never_deletes_previous_version(self):
        client = self.client(); client.get.side_effect = None; client.get.return_value = []
        with self.assertRaises(RuntimeError):
            Store(client, "collection", "contract", 2).replace("source", "hash", [self.row()])
        client.delete.assert_not_called()

    def test_complete_replacement_removes_old_chunks(self):
        client = self.client()
        self.assertEqual(Store(client, "collection", "contract", 2).replace("source", "hash", [self.row()]), 1)
        self.assertIn('content_hash != "hash"', client.delete.call_args.kwargs["filter"])

    def test_rejects_bad_vectors_before_any_writes(self):
        for vector in ([float("nan"), 0], [2, 0], [1], [True, 0]):
            client = self.client(); row = self.row(); row["vector"] = vector
            with self.assertRaises(ValueError):
                Store(client, "collection", "contract", 2).replace("source", "hash", [row])
            client.upsert.assert_not_called()


class FetchTests(unittest.TestCase):
    def response(self, code, body=b"content", headers=None):
        response = Mock(status_code=code, content=body, headers=headers or {})
        return response

    def test_conditional_304_requires_cached_bytes(self):
        with tempfile.TemporaryDirectory() as tmp:
            client = Mock(); client.get.return_value = self.response(200, headers={"ETag": "one"})
            path, doc = fetch(client, "https://example.com/a", {}, Path(tmp))
            doc["ingested_hash"] = doc["sha256"]
            client.get.return_value = self.response(304)
            same, new = fetch(client, "https://example.com/a", doc, Path(tmp))
            self.assertEqual(same, path)
            self.assertEqual(new["ingested_hash"], doc["sha256"])
            self.assertEqual(client.get.call_args.kwargs["headers"]["If-None-Match"], "one")
            path.unlink()
            with self.assertRaises(ValueError):
                fetch(client, "https://example.com/a", doc, Path(tmp))

    def test_removed_page_is_distinct_from_network_failure(self):
        client = Mock(); client.get.return_value = self.response(410)
        with self.assertRaises(SourceRemoved):
            fetch(client, "https://example.com/a", {}, Path("/unused"))

    def test_content_change_keeps_previous_immutable_bytes(self):
        with tempfile.TemporaryDirectory() as tmp:
            client = Mock(); client.get.return_value = self.response(200, b"old")
            old, doc = fetch(client, "https://example.com/a", {}, Path(tmp))
            client.get.return_value = self.response(200, b"new")
            new, _ = fetch(client, "https://example.com/a", doc, Path(tmp))
            self.assertNotEqual(old, new); self.assertEqual(old.read_bytes(), b"old")


class FreshnessTests(unittest.TestCase):
    def test_running_job_cannot_mask_expired_success(self):
        contract = {"generation": "g", "collections": {"c": {"freshness_maximum_age_hours": 36}}}
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            atomic_json(root / "g/status/c.json", {"state": "running", "last_success": 100, "updated_at": 150000})
            atomic_json(root / "g/heartbeats/c.json", {"at": 150000})
            result = status(contract, root, now=150000)
            self.assertFalse(result["healthy"])
            self.assertFalse(result["collections"]["c"]["stalled"])
            self.assertIn('daedalus_refresh_fresh{collection="c",generation="g"} 0', metrics(result))

    def test_never_run_collection_is_unhealthy(self):
        with tempfile.TemporaryDirectory() as tmp:
            result = status({"generation": "g", "collections": {"c": {"freshness_maximum_age_hours": 36}}}, Path(tmp))
            self.assertFalse(result["healthy"])


if __name__ == "__main__":
    unittest.main()

class ChunkingTests(unittest.TestCase):
    def test_chunks_cover_entire_document_with_overlap(self):
        from refresh_embedding import chunks
        encoder = Mock()
        encoder.encode.return_value.offsets = [(i, i + 1) for i in range(13)]
        self.assertEqual(list(chunks("abcdefghijklm", encoder, 5, 2)),
                         ["abcde", "defgh", "ghijk", "jklm"])
        encoder.encode.assert_called_once_with("abcdefghijklm", add_special_tokens=False)

    def test_empty_text_is_not_indexed(self):
        from refresh_embedding import chunks
        encoder = Mock(); encoder.encode.return_value.offsets = []
        self.assertEqual(list(chunks("", encoder, 5, 2)), [])

    def test_extraction_errors_cannot_become_successful_empty_documents(self):
        from refresh_embedding import extracted_text
        with self.assertRaises(ValueError):
            extracted_text({"error": "failed"})
        self.assertEqual(extracted_text({"document_type": "structured", "metadata": {
            "table_metadata": {"table_content": "A | B"}}}), "A | B")
