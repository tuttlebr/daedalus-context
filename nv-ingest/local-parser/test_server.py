import base64
import importlib.util
from io import BytesIO
import unittest
from unittest.mock import Mock, patch

from fastapi import HTTPException
from PIL import Image, ImageDraw

import server


class ParserTests(unittest.TestCase):
    @unittest.skipUnless(importlib.util.find_spec("nemotron_parse"), "runs with the pinned model package in the image build")
    def test_large_picture_refinement_preserves_column_reading_order(self):
        parser = server.Parser.__new__(server.Parser)

        def result(text, kind="Picture"):
            return {"markdown": text, "blocks": [{"class": kind, "bbox": [0, 0, 1, 1], "text": text}]}

        original = result("Header only")
        columns = ["Left office and candidates " * 4, "Middle office and candidates " * 4, "Right office and candidates " * 4]
        parser._predict = Mock(side_effect=[result("Sparse picture"), *map(result, columns)])
        with patch.object(server, "column_boxes", return_value=[(0, 0, 200, 1000), (200, 0, 400, 1000), (400, 0, 600, 1000)]):
            refined = parser._refine(Image.new("RGB", (600, 1000)), original, 0, [4])
        self.assertEqual(parser._predict.call_count, 4)
        self.assertLess(refined["markdown"].index("Left office"), refined["markdown"].index("Middle office"))
        self.assertLess(refined["markdown"].index("Middle office"), refined["markdown"].index("Right office"))
        parser._predict = Mock(side_effect=AssertionError("ordinary tables must not be split"))
        table = result("<table><tr><td>Exact value</td></tr></table>", "Table")
        self.assertIs(parser._refine(Image.new("RGB", (600, 1000)), table, 0, [8]), table)

    def test_picture_columns_split_only_at_clear_gutters(self):
        image = Image.new("RGB", (600, 1000), "white")
        draw = ImageDraw.Draw(image)
        for left, right in [(5, 190), (205, 390), (405, 595)]:
            draw.rectangle((left, 30, right, 950), fill="black")
        boxes = server.column_boxes(image)
        self.assertEqual(len(boxes), 3)
        self.assertTrue(190 < boxes[0][2] < 205)
        self.assertTrue(390 < boxes[1][2] < 405)
        self.assertEqual(boxes[-1][2], image.width)
        self.assertEqual(server.column_boxes(Image.new("RGB", (600, 1000), "white")), [])

    def test_page_image_roundtrip(self):
        data = BytesIO()
        Image.new("RGB", (12, 16), "white").save(data, format="PNG")
        image = server.decode_image(base64.b64encode(data.getvalue()).decode())
        self.assertEqual(image.size, (12, 16))
        self.assertEqual(image.mode, "RGB")

    def test_invalid_image_is_a_client_error(self):
        for data in ["invalid!", base64.b64encode(b"not an image").decode()]:
            with self.subTest(data=data), self.assertRaises(HTTPException) as caught:
                server.decode_image(data)
            self.assertEqual(caught.exception.status_code, 400)

    def test_incomplete_generation_is_never_returned_as_success(self):
        parser = server.Parser.__new__(server.Parser)
        parser.processor = Mock()
        parser.generation = Mock(eos_token_id=2)
        parser.model = Mock()
        output = Mock()
        output.__getitem__ = Mock(return_value=Mock(item=Mock(return_value=123)))
        parser.model.generate.return_value = output
        parser.torch = Mock()
        parser.torch.inference_mode.return_value = unittest.mock.MagicMock()
        parser.processor.return_value.to.return_value = {}
        with patch.dict("sys.modules", {"postprocessing": Mock()}):
            with self.assertRaises(HTTPException) as caught:
                parser._predict(Image.new("RGB", (12, 16)))
        self.assertEqual(caught.exception.status_code, 422)
        parser.processor.batch_decode.assert_not_called()


if __name__ == "__main__":
    unittest.main()
