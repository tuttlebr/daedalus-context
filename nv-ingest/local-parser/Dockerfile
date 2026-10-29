FROM python:3.12.11-slim-bookworm
ARG MODEL_REVISION=4035ffb1d8f8bafcdb867dfaf6f7514f101464f9
ENV PYTHONUNBUFFERED=1 PYTHONDONTWRITEBYTECODE=1 \
    HF_HOME=/tmp/huggingface MODEL_PATH=/opt/model \
    MODEL_REVISION=${MODEL_REVISION} TOKENIZERS_PARALLELISM=false
RUN pip install --no-cache-dir torch==2.11.0 torchvision==0.26.0 --index-url https://download.pytorch.org/whl/cu128
RUN pip install --no-cache-dir transformers==5.14.1 timm==1.0.22 \
    huggingface-hub==1.24.0 beautifulsoup4==4.13.3 einops==0.8.1 \
    fastapi==0.128.0 uvicorn==0.40.0 pillow==12.2.0
RUN hf download nvidia/Nemotron-3.5-Parse --revision ${MODEL_REVISION} \
    --include '*.py' --include '*.json' --include '*.safetensors' \
    --include 'LICENSE' --include 'THIRD_PARTY_NOTICES.md' --include 'TOKENIZER_ATTRIBUTION.md' \
    --exclude 'reference/*' --exclude 'tests/*' --exclude 'examples/*' --local-dir /opt/model \
    && rm -rf /opt/model/.cache
ENV HF_HUB_OFFLINE=1 TRANSFORMERS_OFFLINE=1 PYTHONPATH=/opt/model
ENV TORCHINDUCTOR_CACHE_DIR=/tmp/torchinductor TRITON_CACHE_DIR=/tmp/triton TORCH_HOME=/tmp/torch
WORKDIR /app
COPY server.py test_server.py ./
RUN useradd --uid 65532 --no-create-home --shell /usr/sbin/nologin parser \
    && python -m unittest discover -p 'test_*.py' && rm test_server.py \
    && python -c 'import torch; from transformers import AutoModel, AutoProcessor; from postprocessing import postprocess_document; AutoProcessor.from_pretrained("/opt/model", trust_remote_code=True, local_files_only=True); AutoModel.from_pretrained("/opt/model", trust_remote_code=True, local_files_only=True, dtype=torch.bfloat16); assert postprocess_document("")["markdown"] == ""'
USER 65532:65532
EXPOSE 8000
ENTRYPOINT ["python", "-m", "uvicorn", "server:app", "--host", "0.0.0.0", "--port", "8000", "--no-access-log"]
