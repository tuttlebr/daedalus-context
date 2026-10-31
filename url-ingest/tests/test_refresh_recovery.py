import html
import json
import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

import requests

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from refresh import RefreshFailed, process_batch, run
from refresh_fetch import fetch, RedirectParser
from refresh_html import materialize
from refresh_milvus import Store, StoreUnavailable
from refresh_state import State, RunReport, contract_fingerprint, digest
from test_refresh_retries import contract


def response(body, url="https://example.com/start", code=200, headers=None):
    result = requests.Response()
    result.status_code, result.url, result.headers = code, url, headers or {}
    result._content = body.encode() if isinstance(body, str) else body
    return result


class RecoveryTests(unittest.TestCase):
    def test_short_delayed_redirects_are_followed_but_periodic_refreshes_are_not(self):
        for delay, expected in [("0", "/guide"), ("1", "/guide"), ("5.0", "/guide"), ("60", None)]:
            parser = RedirectParser()
            parser.feed(f'<meta http-equiv="refresh" content="{delay}; url=/guide">')
            self.assertEqual(parser.target, expected)

    def test_redirect_chain_indexes_target_bytes_under_original_identity(self):
        with tempfile.TemporaryDirectory() as tmp:
            http = Mock()
            http.get.side_effect = [
                response('<meta http-equiv="Refresh" content="0; URL=../guide/">', "https://example.com/archive/index.html"),
                response("<article>Actual guide</article>", "https://example.com/guide/"),
            ]
            path, doc = fetch(http, "https://example.com/archive/index.html", {}, Path(tmp))
            self.assertEqual(path.read_text(), "<article>Actual guide</article>")
            self.assertEqual(path.parent.name, digest("https://example.com/archive/index.html"))
            self.assertEqual(doc["fetch_url"], "https://example.com/guide/")

    def test_redirect_cycle_and_non_http_target_fail_without_cached_success(self):
        for target in ("/start", "file:///etc/passwd"):
            with tempfile.TemporaryDirectory() as tmp:
                http = Mock()
                http.get.return_value = response(f'<meta http-equiv="refresh" content="0; url={target}">')
                with self.assertRaises(ValueError):
                    fetch(http, "https://example.com/start", {}, Path(tmp))
                self.assertEqual(list(Path(tmp).iterdir()), [])

    def test_failed_cached_redirect_is_refetched_without_validators(self):
        with tempfile.TemporaryDirectory() as tmp:
            cached = Path(tmp) / "old.html"
            cached.write_text('<meta http-equiv="refresh" content="0; url=/guide">')
            http = Mock()
            http.get.side_effect = [response(cached.read_text()), response("guide text")]
            path, _ = fetch(http, "https://example.com/start", {
                "cache_path": str(cached), "etag": "old", "last_error": "ValueError"
            }, Path(tmp))
            self.assertEqual(http.get.call_args_list[0].kwargs["headers"], {})
            self.assertEqual(path.read_text(), "guide text")

    def test_official_markdown_override_preserves_source_identity(self):
        with tempfile.TemporaryDirectory() as tmp:
            cached = Path(tmp) / "old.html"
            cached.write_text("old")
            http = Mock()
            target = "https://raw.githubusercontent.com/kubernetes/website/main/page.md"
            http.get.return_value = response("# Reset defaults\nOfficial command help", target)
            source = "https://kubernetes.io/page"
            path, doc = fetch(http, source, {"cache_path": str(cached), "etag": "old"},
                              Path(tmp), url_override=target)
            self.assertEqual(path.suffix, ".md")
            self.assertEqual(path.parent.name, digest(source))
            self.assertEqual(doc["fetch_url"], target)
            self.assertEqual(http.get.call_args.kwargs["headers"], {})

    def test_transient_storage_failure_replays_verified_replacement(self):
        client = Mock()
        client.get.side_effect = [RuntimeError("channel not available"), [{"pk": "id"}]]
        client.query_iterator.return_value.next.return_value = []
        row = {"pk": "id"}
        with patch("refresh_milvus.prepare_rows", return_value=[row]), patch("refresh_milvus.time.sleep"):
            self.assertEqual(Store(client, "c", "f", 2).replace("source", "new", [{}]), 1)
        self.assertEqual(client.upsert.call_count, 2)
        self.assertEqual(client.delete.call_count, 1)

    def test_persistent_storage_failure_stops_before_poisoning_other_sources(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            state, report = State(root / "collections", "c", "f"), RunReport(root, "c", "f", "run")
            client = Mock()
            client.upsert.side_effect = RuntimeError("Cannot invoke RPC on closed channel!")
            store = Store(client, "c", "f", 2)
            batch = [("first", root / "one", {"sha256": "h", "ingested_hash": "old"}),
                     ("second", root / "two", {"sha256": "h"})]
            with patch("refresh.extract_batch", return_value=([], [str(root / "one"), str(root / "two")])), \
                 patch("refresh.group_results", return_value={"first": [{}], "second": [{}]}), \
                 patch("refresh_milvus.prepare_rows", return_value=[{"pk": "id"}]), \
                 patch("refresh_milvus.time.sleep"):
                with self.assertRaises(RefreshFailed):
                    process_batch(batch, {}, "url", state, report, store)
            self.assertEqual(client.upsert.call_count, 3)
            client.delete.assert_not_called()
            self.assertEqual(state.data["documents"]["first"]["ingested_hash"], "old")
            self.assertNotIn("second", state.data["documents"])

    def test_known_excluded_source_retains_vectors_and_state(self):
        with tempfile.TemporaryDirectory() as tmp:
            root, cfg = Path(tmp), contract()
            cfg["collections"]["c"]["exclude_url_patterns"] = [r"/_static/"]
            source = "https://example.com/_static/asset.html"
            state = State(root / "g/collections", "c", contract_fingerprint(cfg))
            state.record(source, {"ingested_hash": "old"})
            state.checkpoint()
            client = Mock()
            client.list_databases.return_value = ["default"]
            client.query.return_value = [{"count(*)": 1}]
            with patch.dict(sys.modules, {"pymilvus": Mock(MilvusClient=Mock(return_value=client))}), \
                 patch.dict(os.environ, {"MILVUS_URI": "unused", "MILVUS_USERNAME": "u", "MILVUS_PASSWORD": "p"}), \
                 patch("refresh.Store") as store, \
                 patch("refresh.discover", return_value=({"https://example.com/guide": {}}, [])), \
                 patch("refresh.download_source") as download:
                result = run(cfg, "c", root, root / "cache", root / "pdf", budget_seconds=0)
            self.assertEqual(result["excluded_urls"], 1)
            download.assert_not_called()
            store.return_value.delete.assert_not_called()
            state = State(root / "g/collections", "c", contract_fingerprint(cfg))
            self.assertEqual(state.data["documents"][source]["ingested_hash"], "old")


class ComponentTests(unittest.TestCase):
    def body(self, name, props):
        return f'<div data-react-class="{name}" data-react-props="{html.escape(json.dumps(props), quote=True)}"></div>'.encode()

    def test_learning_pathway_fetches_lesson_and_footer_content(self):
        http = Mock()
        http.get.side_effect = [response(json.dumps({"definition": {
            "description": "A course", "steps": [{"title": "Lesson one",
            "file": "https://developer.download.nvidia.com/step.md",
            "footer": "https://developer.download.nvidia.com/extra.md"}]}})),
            response("Actual lesson content"), response("Extra lesson content")]
        result = materialize(http, "https://developer.nvidia.com/course", self.body(
            "LearningPathway", {"pathwayID": 1, "title": "Course"}))
        self.assertIn(b"Actual lesson content", result)
        self.assertIn(b"Extra lesson content", result)
        self.assertEqual(http.get.call_count, 3)

    def test_incomplete_lesson_fails_instead_of_indexing_only_title(self):
        http = Mock()
        http.get.return_value = response('{"definition":{"description":"course","steps":[]}}')
        with self.assertRaises(ValueError):
            materialize(http, "https://developer.nvidia.com/course", self.body(
                "LearningPathway", {"pathwayID": 1, "title": "Course"}))

    def test_download_component_preserves_installation_instructions(self):
        result = materialize(Mock(), "https://developer.nvidia.cn/download", self.body(
            "NestedOptionSelector", {"pageData": {"releases": {"linux": {"details": "<pre>install package</pre>"}}}}))
        self.assertIn(b"install package", result)

    def test_component_schema_is_not_applied_to_unrelated_sites(self):
        http = Mock()
        self.assertIsNone(materialize(http, "https://example.com", self.body("LearningPathway", {})))
        http.get.assert_not_called()

    def test_public_wordpress_article_uses_content_and_keeps_protected_posts_as_failures(self):
        url = "https://blogs.nvidia.com/wp-json/wp/v2/posts?slug=article"
        post = {"slug": "article", "title": {"rendered": "Article"}, "content": {"rendered": "<p>Original content</p>"}}
        self.assertIn(b"Original content", materialize(Mock(), url, json.dumps([post]).encode()))
        post["content"]["protected"] = True
        with self.assertRaises(ValueError):
            materialize(Mock(), url, json.dumps([post]).encode())
        with self.assertRaises(ValueError):
            materialize(Mock(), url, b"[]")

    def test_materialized_content_revalidates_when_shell_etag_is_unchanged(self):
        with tempfile.TemporaryDirectory() as tmp:
            http = Mock()
            url = "https://developer.nvidia.cn/download"
            for text in ("old instructions", "new instructions"):
                http.get.return_value = response(self.body("NestedOptionSelector", {
                    "pageData": {"releases": {"linux": {"details": text}}}}), url, headers={"ETag": "same-shell"})
                previous = doc if text.startswith("new") else {}
                path, doc = fetch(http, url, previous, Path(tmp))
                self.assertEqual(http.get.call_args.kwargs["headers"], {})
                self.assertIn(text, path.read_text())
                if previous:
                    self.assertNotEqual(previous["sha256"], doc["sha256"])


if __name__ == "__main__":
    unittest.main()
