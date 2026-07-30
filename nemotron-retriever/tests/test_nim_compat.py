import importlib.util
import pathlib
import unittest


MODULE_PATH = pathlib.Path(__file__).parents[1] / "files" / "nim_compat.py"
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


if __name__ == "__main__":
    unittest.main()
