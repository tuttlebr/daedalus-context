import importlib.util
from io import BytesIO
import os
from pathlib import Path
import unittest
from unittest.mock import Mock, patch


ENGINE_PATH = os.environ.get("LOCAL_PDF_PATH")
if not ENGINE_PATH:
    ENGINE_PATH = Path(__file__).resolve().parents[2] / "nv-ingest/chart/files/local_pdf.py"
SPEC = importlib.util.spec_from_file_location(
    "local_pdf", ENGINE_PATH
)
ENGINE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(ENGINE)


class LocalParserContractTests(unittest.TestCase):
    @unittest.skipUnless(importlib.util.find_spec("nv_ingest"), "requires the pinned NV-Ingest server")
    def test_local_only_endpoints_validate_every_pipeline_stage(self):
        import importlib
        from nv_ingest.pipeline.config.loaders import load_default_pipeline_config

        names = ["OCR_HTTP_ENDPOINT", "YOLOX_HTTP_ENDPOINT", "YOLOX_GRAPHIC_ELEMENTS_HTTP_ENDPOINT",
                 "YOLOX_TABLE_STRUCTURE_HTTP_ENDPOINT", "VLM_CAPTION_ENDPOINT", "NEMOTRON_PARSE_HTTP_ENDPOINT"]
        # Blank YAML substitutions and null endpoints fail different startup checks.
        with patch.dict(os.environ, dict.fromkeys(names, "http://nv-ingest-parser:8000/legacy-disabled")):
            config = load_default_pipeline_config()
        pipeline = importlib.import_module("nv_ingest.pipeline.ingest_pipeline")
        for stage in config.stages:
            with self.subTest(stage=stage.name):
                if stage.callable:
                    function = pipeline.resolve_callable_from_path(stage.callable, signature_schema=pipeline.ingest_stage_callable_signature)
                    schema = pipeline.find_pydantic_config_schema_unified(function, param_name="stage_config")
                else:
                    base = {"source": pipeline.RayActorSourceStage, "sink": pipeline.RayActorSinkStage,
                            "stage": pipeline.RayActorStage}[stage.type.value]
                    actor = pipeline.resolve_actor_class_from_path(stage.actor, base)
                    schema = pipeline.find_pydantic_config_schema(actor, base)
                if schema:
                    schema(**stage.config)

    def test_semantic_markdown_is_preserved_without_native_text_fallback(self):
        image = Mock()
        image.save.side_effect = lambda output, **kwargs: output.write(b"image")
        session = Mock()
        markdown = "# Heading\n<table><tr><td colspan=2>Exact content</td></tr></table>"
        session.post.return_value.json.return_value = {"markdown": markdown}
        with patch.dict(os.environ, {"LOCAL_DOCUMENT_PARSE_ENDPOINT": "http://parser:8000/"}):
            self.assertEqual(ENGINE.parse_page(image, session), markdown)
        self.assertEqual(session.post.call_args.args, ("http://parser:8000/v1/parse",))
        self.assertEqual(session.post.call_args.kwargs["json"], {"image": "aW1hZ2U="})
        session.post.return_value.raise_for_status.assert_called_once()

    def test_parser_failure_is_not_replaced_with_native_text(self):
        image, session = Mock(), Mock()
        session.post.return_value.raise_for_status.side_effect = RuntimeError("parser failed")
        with patch.dict(os.environ, {"LOCAL_DOCUMENT_PARSE_ENDPOINT": "http://parser:8000"}):
            with self.assertRaisesRegex(RuntimeError, "parser failed"):
                ENGINE.parse_page(image, session)
        session.post.return_value.json.assert_not_called()

    def test_malformed_success_fails(self):
        session = Mock()
        session.post.return_value.json.return_value = {"markdown": None}
        with patch.dict(os.environ, {"LOCAL_DOCUMENT_PARSE_ENDPOINT": "http://parser:8000"}):
            with self.assertRaisesRegex(ValueError, "invalid Markdown"):
                ENGINE.parse_page(Mock(), session)

    def test_both_legacy_pdf_methods_use_the_same_local_engine(self):
        with patch.object(ENGINE, "pdfium_extractor", return_value=["parsed"]) as parser:
            self.assertEqual(ENGINE.nemotron_parse_extractor(
                b"pdf", True, False, False, False, False, {"text_depth": "page"},
            ), ["parsed"])
        parser.assert_called_once_with(
            b"pdf", True, False, False, False, False, False, {"text_depth": "page"}, None,
        )

    @unittest.skipUnless(importlib.util.find_spec("nv_ingest_api"), "requires the pinned NV-Ingest runtime")
    def test_pdf_rendering_keeps_page_order_and_complete_semantic_content(self):
        import pypdfium2 as pdfium

        document = pdfium.PdfDocument.new()
        for _ in range(2):
            document.new_page(612, 792).close()
        data = BytesIO()
        document.save(data)
        document.close()
        data.seek(0)
        expected = ["# Page one\n<table><tr><td colspan=2>A</td></tr></table>", "# Page two\nExact text"]
        with patch.object(ENGINE, "parse_page", side_effect=expected) as parse:
            result = ENGINE.pdfium_extractor(
                data, True, False, False, False, False, False,
                {"row_data": {"source_id": "fixture.pdf", "metadata": {}}, "text_depth": "page"},
            )
        self.assertEqual(parse.call_count, 2)
        self.assertEqual([item[0] for item in result], ["text", "text"])
        self.assertEqual([item[1]["content"] for item in result], expected)
        self.assertEqual([item[1]["content_metadata"]["page_number"] for item in result], [0, 1])


if __name__ == "__main__":
    unittest.main()
