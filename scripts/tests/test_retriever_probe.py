"""Run the provider probe over HTTP without a model or cluster."""

import json
import os
from pathlib import Path
import subprocess
import sys
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


PROBE = Path(__file__).resolve().parents[1] / "check_retriever_contract.py"


class RetrieverProbeTests(unittest.TestCase):
    def setUp(self):
        self.requests = []
        self.vector = [1.0, 0.0]
        testcase = self

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self):
                body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                testcase.requests.append((self.path, body))
                if self.path == "/v1/embeddings":
                    response = {"data": [{"index": 0, "embedding": testcase.vector}]}
                elif self.path == "/rerank":
                    response = {"results": [{"index": 0, "relevance_score": -1.0}]}
                else:
                    response = {"rankings": [{"index": 0, "logit": -1.0}]}
                encoded = json.dumps(response).encode()
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(encoded)))
                self.end_headers()
                self.wfile.write(encoded)

            def log_message(self, *_args):
                pass

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(self.server.server_close)
        self.addCleanup(self.server.shutdown)

    def probe(self):
        base = f"http://127.0.0.1:{self.server.server_port}"
        return subprocess.run(
            [sys.executable, str(PROBE)], capture_output=True, text=True,
            env={**os.environ, "EMBEDDING_URL": base + "/v1/embeddings",
                 "NATIVE_RERANKER_URL": base + "/rerank",
                 "NIM_RERANKER_URL": base + "/v1/ranking",
                 "EMBEDDING_MODEL": "synthetic/embedder",
                 "RERANKER_MODEL": "synthetic/reranker", "EXPECTED_DIMENSION": "2"},
            timeout=10,
        )

    def test_both_embedding_consumers_use_raw_text_and_query_role(self):
        result = self.probe()
        self.assertEqual(result.returncode, 0, result.stderr)
        embeddings = [body for path, body in self.requests if path == "/v1/embeddings"]
        self.assertEqual(len(embeddings), 2)
        native, nim = embeddings
        message = native["messages"][0]
        self.assertEqual(message["role"], "query")
        self.assertEqual(nim["input_type"], "query")
        self.assertEqual(message["content"][0]["text"], nim["input"])
        self.assertFalse(nim["input"].startswith("query:"))
        self.assertEqual(json.loads(result.stdout)["nativeEmbeddings"], "ok")

    def test_matching_dimensions_do_not_hide_invalid_vectors(self):
        for vector in ([float("nan"), 0.0], [0.0, 0.0], [True, 0.0], ["1", 0.0]):
            with self.subTest(vector=vector):
                self.vector = vector
                result = self.probe()
                self.assertNotEqual(result.returncode, 0)
                self.assertNotIn('"nativeEmbeddings": "ok"', result.stdout)


if __name__ == "__main__":
    unittest.main()
