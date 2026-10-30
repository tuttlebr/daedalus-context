"""NV-Ingest 26.3 PDF engine backed exclusively by the local document parser.

Mounted over both PDFium and Nemotron engine entry points so existing clients,
including Office render_as_pdf and scheduled ingestion, share the same policy.
PDFium only renders pages; it never supplies the extracted document text.
"""

import base64
from contextlib import closing
from io import BytesIO
import logging
import os
import time

import requests

LOGGER = logging.getLogger(__name__)


def parse_page(image, session):
    encoded = BytesIO()
    image.save(encoded, format="PNG")
    endpoint = os.environ["LOCAL_DOCUMENT_PARSE_ENDPOINT"].rstrip("/")
    response = session.post(
        endpoint + "/v1/parse",
        json={"image": base64.b64encode(encoded.getvalue()).decode("ascii")},
        timeout=(10, 600),
    )
    response.raise_for_status()
    result = response.json()
    if not isinstance(result.get("markdown"), str):
        raise ValueError("Local parser returned invalid Markdown")
    return result["markdown"]


def pdfium_extractor(
    pdf_stream, extract_text, extract_images, extract_infographics,
    extract_tables, extract_charts, extract_page_as_image, extractor_config,
    execution_trace_log=None,
):
    import pypdfium2 as pdfium
    from nv_ingest_api.internal.enums.common import AccessLevelEnum, TextTypeEnum
    from nv_ingest_api.util.metadata.aggregators import construct_text_metadata, extract_pdf_metadata

    row = extractor_config["row_data"]
    source_id = row["source_id"]
    base = row.get(extractor_config.get("metadata_column", "metadata"), {}) or {}
    previous = base.get("source_metadata", {})
    depth = TextTypeEnum.PAGE if extractor_config.get("text_depth", "page") == "page" else TextTypeEnum.DOCUMENT
    extracted, texts = [], []
    started = time.monotonic()
    with closing(pdfium.PdfDocument(pdf_stream)) as doc, requests.Session() as session:
        metadata = extract_pdf_metadata(doc, source_id)
        source = {
            "source_name": metadata.filename, "source_id": source_id,
            "source_location": previous.get("source_location", ""),
            "source_type": metadata.source_type,
            "collection_id": previous.get("collection_id", ""),
            "date_created": metadata.date_created, "last_modified": metadata.last_modified,
            "summary": "", "partition_id": previous.get("partition_id", -1),
            "access_level": previous.get("access_level", AccessLevelEnum.UNKNOWN),
        }
        for index in range(len(doc)):
            with closing(doc.get_page(index)) as page:
                width, height = page.get_size()
                # Preserve aspect ratio and supply the model's recommended resolution.
                scale = min(1664 / width, 2048 / height)
                with closing(page.render(scale=scale)) as bitmap:
                    image = bitmap.to_pil().convert("RGB")
                try:
                    text = parse_page(image, session)
                finally:
                    image.close()
            texts.append(text)
            if depth == TextTypeEnum.PAGE:
                extracted.append(construct_text_metadata(
                    [text], metadata.keywords, index, -1, -1, -1, len(doc),
                    depth, source, base,
                ))
        if depth == TextTypeEnum.DOCUMENT:
            extracted.append(construct_text_metadata(
                texts, metadata.keywords, -1, -1, -1, -1, len(doc), depth, source, base,
            ))
    LOGGER.info("Local document extraction completed: pages=%d seconds=%.2f", len(texts), time.monotonic() - started)
    return extracted


def nemotron_parse_extractor(
    pdf_stream, extract_text, extract_images, extract_infographics,
    extract_tables, extract_charts, extractor_config, execution_trace_log=None,
):
    return pdfium_extractor(
        pdf_stream, extract_text, extract_images, extract_infographics,
        extract_tables, extract_charts, False, extractor_config, execution_trace_log,
    )
