import importlib.util
import json
import pathlib
import threading
import time
import unittest
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest.mock import patch


MODULE_PATH = pathlib.Path(__file__).parents[1] / "chart" / "files" / "nim_compat.py"
SPEC = importlib.util.spec_from_file_location("nim_compat", MODULE_PATH)
nim_compat = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(nim_compat)


class EmbeddingCompatibilityTests(unittest.TestCase):
    def test_nim_passage_batch_is_prefixed_for_vllm(self):
        payload = {
            "model": "embedder",
            "input": ["first", "second"],
            "input_type": "passage",
            "truncate": "END",
            "encoding_format": "float",
        }

        adapted = nim_compat.adapt_embedding_request(payload)

        self.assertEqual(adapted["input"], ["passage: first", "passage: second"])
        self.assertNotIn("input_type", adapted)
        self.assertNotIn("truncate", adapted)

    def test_nim_query_scalar_is_prefixed_for_vllm(self):
        adapted = nim_compat.adapt_embedding_request(
            {"model": "embedder", "input": "question", "input_type": "query"}
        )

        self.assertEqual(adapted["input"], "query: question")

    def test_native_chat_request_is_unchanged(self):
        payload = {"model": "embedder", "messages": [{"role": "query"}]}

        self.assertIs(nim_compat.adapt_embedding_request(payload), payload)

    def test_unknown_input_type_is_rejected(self):
        with self.assertRaisesRegex(nim_compat.CompatibilityError, "input_type"):
            nim_compat.adapt_embedding_request(
                {"input": ["text"], "input_type": "classification"}
            )


class RankingCompatibilityTests(unittest.TestCase):
    def test_nvidia_ranking_request_maps_to_vllm(self):
        adapted = nim_compat.adapt_ranking_request(
            {
                "model": "reranker",
                "query": {"text": "question"},
                "passages": [{"text": "first"}, {"text": "second"}],
            }
        )

        self.assertEqual(
            adapted,
            {
                "model": "reranker",
                "query": "question",
                "documents": ["first", "second"],
                "top_n": 2,
            },
        )

    def test_vllm_response_maps_to_nvidia_rankings(self):
        adapted = nim_compat.adapt_ranking_response(
            {
                "results": [
                    {"index": 1, "relevance_score": 0.9},
                    {"index": 0, "relevance_score": 0.2},
                ],
                "usage": {"total_tokens": 12},
            }
        )

        self.assertEqual(
            adapted["rankings"],
            [{"index": 1, "logit": 0.9}, {"index": 0, "logit": 0.2}],
        )
        self.assertEqual(adapted["usage"], {"total_tokens": 12})


class ClassificationCompatibilityTests(unittest.TestCase):
    """Exercise both public APIs through the adapter against an HTTP upstream."""

    def setUp(self):
        self.requests = []
        self.active = 0
        self.peak = 0
        self.lock = threading.Lock()
        testcase = self

        class Upstream(BaseHTTPRequestHandler):
            def do_POST(self):
                payload = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                with testcase.lock:
                    testcase.requests.append((self.path, payload, self.headers.get("Authorization")))
                    testcase.active += 1
                    testcase.peak = max(testcase.peak, testcase.active)
                try:
                    status = 200
                    if self.path != "/classify":
                        response = {"echo": payload}
                    else:
                        text = payload["messages"][1]["content"][0]["text"]
                        time.sleep(0.01)
                        if text == "error":
                            status, response = 503, {"error": "upstream unavailable"}
                        elif text == "malformed":
                            response = {"data": []}
                        elif payload["model"] == "unknown":
                            status, response = 404, {"error": "unknown model"}
                        else:
                            response = {
                                "data": [{"probs": [float(text)]}],
                                "usage": {"prompt_tokens": 5, "total_tokens": 5},
                            }
                    body = json.dumps(response).encode()
                    self.send_response(status)
                    self.send_header("Content-Type", "application/json")
                    self.send_header("Content-Length", str(len(body)))
                    self.end_headers()
                    self.wfile.write(body)
                finally:
                    with testcase.lock:
                        testcase.active -= 1

            def log_message(self, *args):
                pass

        class Adapter(nim_compat.CompatibilityHandler):
            def log_message(self, *args):
                pass

        def start(handler):
            server = ThreadingHTTPServer(("127.0.0.1", 0), handler)
            thread = threading.Thread(target=server.serve_forever, kwargs={"poll_interval": 0.01}, daemon=True)
            thread.start()
            self.addCleanup(server.server_close)
            self.addCleanup(server.shutdown)
            return f"http://127.0.0.1:{server.server_port}"

        upstream = start(Upstream)
        self.base_url = start(Adapter)
        settings = patch.multiple(
            nim_compat, UPSTREAM=upstream, RANKING_BACKEND="classify",
            RERANKER_MODEL="nvidia/Nemotron-3.5-Rerank-8B-BF16",
        )
        settings.start()
        self.addCleanup(settings.stop)

    def post(self, path, payload):
        request = urllib.request.Request(self.base_url + path, data=json.dumps(payload).encode(),
                                         headers={"Content-Type": "application/json", "Authorization": "Bearer test-token"})
        try:
            response = urllib.request.urlopen(request, timeout=5)
        except urllib.error.HTTPError as exc:
            response = exc
        with response:
            return response.status, json.load(response)

    def test_native_rerank_preserves_logits_indices_top_n_and_usage(self):
        status, body = self.post("/rerank", {
            "query": "Question [IMG]", "documents": ["-4.5", "-1.25", "-2.0"], "top_n": 2,
        })
        self.assertEqual(status, 200)
        self.assertEqual([r["index"] for r in body["results"]], [1, 2])
        self.assertEqual([r["relevance_score"] for r in body["results"]], [-1.25, -2.0])
        self.assertEqual(body["results"][0]["document"], {"text": "-1.25"})
        self.assertEqual(body["usage"], {"prompt_tokens": 15, "completion_tokens": 0, "total_tokens": 15})
        for path, request, authorization in self.requests:
            self.assertEqual(path, "/classify")
            self.assertEqual(authorization, "Bearer test-token")
            self.assertFalse(request["use_activation"])
            self.assertFalse(request["add_special_tokens"])
            self.assertEqual(request["model"], "nvidia/Nemotron-3.5-Rerank-8B-BF16")
            self.assertEqual(request["messages"][0], {
                "role": "query", "content": [{"type": "text", "text": "Question [IMG]"}],
            })

    def test_nim_ranking_accepts_legacy_model_name_and_returns_logits(self):
        legacy = "nvidia/llama-nemotron-rerank-1b-v2"
        status, body = self.post("/v1/ranking", {
            "model": legacy, "query": {"text": "Question"},
            "passages": [{"text": "-3"}, {"text": "2"}], "top_n": 1,
        })
        self.assertEqual(status, 200)
        self.assertEqual(body["rankings"], [{"index": 1, "logit": 2.0}])
        self.assertTrue(all(request[1]["model"] == legacy for request in self.requests))

    def test_tied_scores_keep_original_order_and_zero_top_n_returns_all(self):
        status, body = self.post("/v1/rerank", {
            "query": "Q", "documents": ["2", "2", "-1"], "top_n": 0,
        })
        self.assertEqual(status, 200)
        self.assertEqual([r["index"] for r in body["results"]], [0, 1, 2])

    def test_invalid_input_rejected_before_upstream_work(self):
        for changes in ({"documents": []}, {"documents": [42]}, {"query": None},
                        {"top_n": True}, {"top_n": -1}, {"top_n": "1"}, {"model": ["bad"]}):
            with self.subTest(changes=changes):
                payload = {"query": "Q", "documents": ["1"], **changes}
                status, _ = self.post("/rerank", payload)
                self.assertEqual(status, 400)
        self.assertEqual(self.requests, [])

    def test_upstream_failures_do_not_return_partial_rankings(self):
        for text, model, expected in (("error", None, 503), ("malformed", None, 502),
                                      ("nan", None, 502), ("1", "unknown", 404)):
            with self.subTest(text=text, model=model):
                status, body = self.post("/rerank", {
                    "query": "Q", "documents": ["2", text], "model": model,
                })
                self.assertEqual(status, expected)
                self.assertNotIn("results", body)

    def test_gpu_concurrency_is_bounded_across_requests(self):
        with ThreadPoolExecutor(max_workers=2) as clients:
            futures = [clients.submit(self.post, "/rerank", {
                "query": "Q", "documents": [str(i) for i in range(8)],
            }) for _ in range(2)]
            self.assertTrue(all(f.result()[0] == 200 for f in futures))
        self.assertGreater(self.peak, 1)
        self.assertLessEqual(self.peak, 4)

    def test_deadline_expires_before_queued_work_contacts_upstream(self):
        with self.assertRaises(TimeoutError):
            nim_compat.classify_document({}, None, time.monotonic() - 1)
        self.assertEqual(self.requests, [])

    def test_embedding_and_legacy_backend_still_proxy(self):
        status, body = self.post("/v1/embeddings", {"input": "text", "input_type": "query"})
        self.assertEqual(status, 200)
        self.assertEqual(body["echo"]["input"], "query: text")
        with patch.object(nim_compat, "RANKING_BACKEND", "rerank"):
            status, body = self.post("/rerank", {"query": "Q", "documents": ["text"]})
        self.assertEqual(status, 200)
        self.assertEqual(body["echo"]["documents"], ["text"])


if __name__ == "__main__":
    unittest.main()

class EmbeddingV2Tests(unittest.TestCase):
    def test_agent_and_ingestion_documents_use_identical_prompt_contract(self):
        common = {"model": "embedder"}
        native = nim_compat.embedding_v2_request({**common, "messages": [{"role": "document", "content": [{"type": "text", "text": "same passage"}]}]})
        nim = nim_compat.embedding_v2_request({**common, "input": ["same passage"], "input_type": "passage"})
        self.assertEqual(native, nim)
        self.assertEqual(native["input_type"], "document")
        self.assertEqual(native["inputs"][0]["content"][0]["text"], "same passage")

    def test_query_role_is_preserved_without_double_prefixing(self):
        request = nim_compat.embedding_v2_request({"messages": [{"role": "query", "content": "question"}]})
        self.assertEqual(request["input_type"], "query")
        self.assertEqual(request["inputs"], [{"content": [{"type": "text", "text": "question"}]}])

    def test_rejects_incompatible_contract_parameters(self):
        for extra in ({"dimensions": 1024}, {"encoding_format": "base64"}, {"input_type": "classification"}):
            with self.assertRaises(nim_compat.CompatibilityError):
                nim_compat.embedding_v2_request({"input": "text", **extra})

    def test_invalid_upstream_vectors_are_rejected(self):
        import io
        for vectors in ([], [[1.0]], [[float("nan")] * 2048], [[1.0] * 2048]):
            with patch.object(nim_compat.urllib.request, "urlopen", return_value=io.StringIO(json.dumps({"embeddings": {"float": vectors}}))):
                with self.assertRaises(nim_compat.UpstreamResponseError):
                    nim_compat.embed_via_v2({"input": "text"})

    def test_normalized_vectors_preserve_input_order(self):
        import io
        vectors = [[1.0] + [0.0] * 2047, [0.0, 1.0] + [0.0] * 2046]
        with patch.object(nim_compat.urllib.request, "urlopen", return_value=io.StringIO(json.dumps({"embeddings": {"float": vectors}}))):
            response = nim_compat.embed_via_v2({"input": ["a", "b"]})
        self.assertEqual([x["index"] for x in response["data"]], [0, 1])
        self.assertEqual([x["embedding"] for x in response["data"]], vectors)
