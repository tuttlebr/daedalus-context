"""Local Nemotron Parse inference. Documents never leave the cluster."""

import asyncio
import base64
import binascii
from contextlib import asynccontextmanager
from io import BytesIO
import logging
import math
import os
import time

from fastapi import FastAPI, HTTPException, Request
from PIL import Image, UnidentifiedImageError
from pydantic import BaseModel

LOGGER = logging.getLogger("nemotron_parse")
logging.basicConfig(level=logging.INFO)
PROMPT = "</s><s><predict_bbox><predict_classes><output_markdown><predict_text_in_pic>"
MAX_BODY_BYTES = 24 * 1024 * 1024
MAX_IMAGE_PIXELS = 16 * 1024 * 1024
MAX_NEW_TOKENS = 8744


def column_boxes(image: Image.Image) -> list[tuple[int, int, int, int]]:
    """Find continuous white gutters inside a large predicted picture region.

    Headers and footers are excluded from the projection. Only wide, almost
    completely clear gutters qualify; ordinary inter-word spaces do not.
    """
    import numpy as np

    width, height = image.size
    gray = np.asarray(image.convert("L"))
    ink = (gray[height // 10:height * 9 // 10] < 230).mean(axis=0)
    cuts, start = [0], None
    for index, clear in enumerate([*(ink < 0.005), False]):
        if clear and start is None:
            start = index
        if not clear and start is not None:
            middle = (start + index) // 2
            if (3 <= index - start <= width * 0.1 and start > 0 and index < width
                    and middle - cuts[-1] >= width * 0.18 and width - middle >= width * 0.18):
                cuts.append(middle)
            start = None
    cuts.append(width)
    if not 3 <= len(cuts) <= 5:
        return []
    return [(left, 0, right, height) for left, right in zip(cuts, cuts[1:])]


def decode_image(encoded: str) -> Image.Image:
    try:
        data = base64.b64decode(encoded, validate=True)
        with Image.open(BytesIO(data)) as image:
            if image.width * image.height > MAX_IMAGE_PIXELS:
                raise HTTPException(413, "Page image is too large.")
            return image.convert("RGB")
    except (binascii.Error, ValueError, UnidentifiedImageError, OSError) as exc:
        raise HTTPException(400, "Invalid page image.") from exc


class Parser:
    def __init__(self):
        import torch
        from transformers import AutoModel, AutoProcessor, GenerationConfig

        path = os.environ.get("MODEL_PATH", "/opt/model")
        self.torch = torch
        self.model = AutoModel.from_pretrained(
            path, trust_remote_code=True, local_files_only=True, dtype=torch.bfloat16
        ).to("cuda").eval()
        self.processor = AutoProcessor.from_pretrained(
            path, trust_remote_code=True, local_files_only=True
        )
        self.generation = GenerationConfig.from_pretrained(path, local_files_only=True)
        self.generation.max_new_tokens = MAX_NEW_TOKENS
        self.generation.do_sample = False
        self.generation.repetition_penalty = 1.1

    def _predict(self, image: Image.Image) -> dict:
        from postprocessing import postprocess_document

        inputs = self.processor(
            images=[image], text=PROMPT, return_tensors="pt", add_special_tokens=False
        ).to("cuda")
        with self.torch.inference_mode():
            outputs = self.model.generate(**inputs, generation_config=self.generation)
        eos = self.generation.eos_token_id
        eos = eos if isinstance(eos, list) else [eos]
        if outputs[0, -1].item() not in eos:
            raise HTTPException(422, "Page exceeds the parser output limit; extraction is incomplete.")
        raw = self.processor.batch_decode(outputs, skip_special_tokens=False)[0]
        return postprocess_document(raw, image_size=image.size)

    def _refine(self, image, result, depth, budget):
        from nemotron_parse.engine.merge import block_with_raw, rebuild_blocks
        from postprocessing import postprocess_document

        blocks = []
        changed = False
        width, height = image.size
        for block in result["blocks"]:
            x0, y0, x1, y1 = block["bbox"]
            box = (max(0, math.floor(x0 * width)), max(0, math.floor(y0 * height)),
                   min(width, math.ceil(x1 * width)), min(height, math.ceil(y1 * height)))
            original = block_with_raw(tuple(block["bbox"]), block["class"], block["text"])
            if (block["class"] != "Picture" or depth >= 2 or budget[0] == 0
                    or (x1 - x0) * (y1 - y0) < 0.2
                    or min(box[2] - box[0], box[3] - box[1]) < 128):
                blocks.append(original)
                continue
            with image.crop(box) as region:
                # First remove page margins. A second pass can separate dense
                # form columns that the model mislabeled as one large picture.
                crops = [(0, 0, *region.size)] if depth == 0 else column_boxes(region)
                if not crops or len(crops) > budget[0]:
                    blocks.append(original)
                    continue
                replacements = []
                for crop in crops:
                    budget[0] -= 1
                    with region.crop(crop) as tile:
                        recovered = self._predict(tile)
                        recovered = self._refine(tile, recovered, depth + 1, budget)
                        for child in recovered["blocks"]:
                            a, b, c, d = child["bbox"]
                            bbox = ((box[0] + crop[0] + a * tile.width) / width,
                                    (box[1] + crop[1] + b * tile.height) / height,
                                    (box[0] + crop[0] + c * tile.width) / width,
                                    (box[1] + crop[1] + d * tile.height) / height)
                            replacements.append(block_with_raw(bbox, child["class"], child["text"]))
            if sum(len(child.body) for child in replacements) > len(block["text"]) + 16:
                blocks.extend(replacements)
                changed = True
            else:
                blocks.append(original)
        if not changed:
            return result
        seen, deduplicated = set(), []
        for block in blocks:
            key = (block.cls, block.body.strip())
            if block.cls in {"Page-header", "Page-footer"} and key in seen:
                continue
            seen.add(key)
            deduplicated.append(block)
        return postprocess_document(rebuild_blocks(deduplicated), image_size=image.size)

    def parse(self, image: Image.Image) -> dict:
        started = time.monotonic()
        result = self._refine(image, self._predict(image), 0, [8])
        LOGGER.info("Parsed page in %.2fs (%d blocks)", time.monotonic() - started, len(result["blocks"]))
        return {
            "markdown": result["markdown"],
            "blocks": result["blocks"],
            "warnings": result["warnings"],
            "model": "nvidia/Nemotron-3.5-Parse",
            "revision": os.environ.get("MODEL_REVISION", ""),
        }


@asynccontextmanager
async def lifespan(app: FastAPI):
    app.state.parser = await asyncio.to_thread(Parser)
    app.state.inference = asyncio.Semaphore(1)
    yield


app = FastAPI(lifespan=lifespan)


@app.middleware("http")
async def bound_request_body(request: Request, call_next):
    # Count streamed bytes too; Content-Length alone does not bound chunked bodies.
    if request.method == "POST":
        body = bytearray()
        async for chunk in request.stream():
            body.extend(chunk)
            if len(body) > MAX_BODY_BYTES:
                from fastapi.responses import JSONResponse
                return JSONResponse({"detail": "Page request is too large."}, status_code=413)
        request._body = bytes(body)
    return await call_next(request)


class Page(BaseModel):
    image: str


@app.get("/healthz")
@app.get("/readyz")
def ready():
    return {"status": "ready", "model": "nvidia/Nemotron-3.5-Parse"}


@app.post("/v1/parse")
async def parse(page: Page, request: Request):
    image = decode_image(page.image)
    # Keep GPU ownership until inference finishes, including on client disconnect.
    async with request.app.state.inference:
        task = asyncio.create_task(asyncio.to_thread(request.app.state.parser.parse, image))
        try:
            return await asyncio.shield(task)
        except asyncio.CancelledError:
            await task
            raise
