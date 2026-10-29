# Local document parser

The NV-Ingest chart deploys NVIDIA Nemotron 3.5 Parse on one GPU. Model source
and weights are pinned in the [Dockerfile](Dockerfile) and
[component lock](../../stack/components.lock.yaml). The image uses NVIDIA's
[Transformers inference contract](https://huggingface.co/nvidia/Nemotron-3.5-Parse)
and official postprocessing helpers. Tables retain HTML spans and reading order.

Daedalus inline attachments, Markdown downloads, uploaded knowledge-base
documents, and scheduled PDF ingestion continue to use NV-Ingest. The
[PDF engine](../../helm/nv-ingest/files/local_pdf.py) renders every page and calls
the internal parser. Both the `pdfium` and `nemotron_parse` engine entry points
are replaced for the pinned NV-Ingest 26.3 image; Office `render_as_pdf` uses the
same engine. Plain text and HTML retain their normal NV-Ingest handling.

The parser produces page Markdown, including tables, charts, and text
inside pictures. This semantic content travels in NV-Ingest text records even
when a client's legacy request disables separate table/image extraction.
Binary image crops and captions are not emitted by this document pipeline.

The engine does not read native PDF text and never calls hosted extraction
models. Parser errors and incomplete generations fail the job. The model image
contains all weights and Python model code and starts with offline Hub access.
Pages are rendered at up to 1664 by 2048 pixels while preserving aspect ratio.
GPU inference is serialized; concurrent requests queue without concurrent model
allocations. Readiness succeeds after model loading.

The upstream pipeline validates legacy OCR, layout, caption, and Nemotron NIM
addresses even when document extraction replaces those engines. The root
deployment always sets these addresses to the local parser's unimplemented
`/legacy-disabled` path; old external addresses in `.env` are ignored. Empty
strings break YAML substitution, and null values fail stage validation. A
legacy image-only task fails explicitly instead of calling an external model.

Large picture regions receive bounded crop refinement. Continuous white gutters
identify separate form columns, which are parsed in reading order. This recovers
dense text that the first pass can omit when a form is classified as a picture.
Ordinary text and table blocks are preserved. Refinement uses only this model,
maps block coordinates back to the original page, and retains the original
prediction when a crop does not recover additional content.

Use the root image and deployment targets documented in
[DEPLOYMENT.md](../DEPLOYMENT.md). `LOCAL_DOCUMENT_PARSE_ENDPOINT` selects the
internal HTTP service. The root deployment includes a source checksum in the
NV-Ingest environment, restarting workers when the mounted engine changes.
When upgrading NV-Ingest, revalidate both mounted Python module paths, the
extractor signatures, metadata construction, and Office conversion through the
actual new image. Do not remove these mounts without replacing the extraction
policy; that would restore upstream native-text and remote-model behavior.

The parser's `/v1/parse` endpoint accepts a base64 page image in `image` and
returns `markdown`, normalized `blocks`, warnings, and model revision. It is an
internal ClusterIP service. No document bytes or extracted content are logged.
