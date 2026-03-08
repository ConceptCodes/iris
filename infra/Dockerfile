# ── Build stage ───────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/server  ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/indexer ./cmd/indexer
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/worker ./cmd/worker

# ── Server runtime ────────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12 AS server
COPY --from=builder /bin/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]

# ── Indexer runtime ───────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12 AS indexer
COPY --from=builder /bin/indexer /indexer
ENTRYPOINT ["/indexer"]

# ── Worker runtime ────────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12 AS worker
COPY --from=builder /bin/worker /worker
ENTRYPOINT ["/worker"]

# ── CLIP service runtime ──────────────────────────────────────────────────────
# Default: CPU-only build (smaller image, runs anywhere)
# For GPU: docker build --build-arg TORCH_INDEX=https://download.pytorch.org/whl/cu121 ./clip_service
# For Apple Silicon: the MPS backend is included in standard torch, no special index needed

ARG PYTHON_VERSION=3.11
FROM python:${PYTHON_VERSION}-slim AS clip

# Build arg for PyTorch index (cpu, cu118, cu121, etc.)
# CPU: https://download.pytorch.org/whl/cpu (default)
# CUDA 11.8: https://download.pytorch.org/whl/cu118
# CUDA 12.1: https://download.pytorch.org/whl/cu121
ARG TORCH_INDEX=https://download.pytorch.org/whl/cpu

WORKDIR /app

# Install dependencies
RUN pip install --no-cache-dir \
    fastapi>=0.109.0 \
    uvicorn[standard]>=0.27.0 \
    open-clip-torch>=2.24.0 \
    pillow>=10.2.0 && \
    pip install --no-cache-dir torch --index-url ${TORCH_INDEX}

# Pre-download model weights at build time (speeds up container start)
RUN python -c "from open_clip import create_model_from_pretrained, get_tokenizer; \
    m, p = create_model_from_pretrained('hf-hub:laion/CLIP-ViT-B-32-laion2B-s34B-b79K'); \
    t = get_tokenizer('hf-hub:laion/CLIP-ViT-B-32-laion2B-s34B-b79K')"

COPY clip_service/main.py .

# Environment variables
ENV MODEL=CLIP-ViT-B-32-laion2B-s34B-b79K
ENV PYTHONUNBUFFERED=1

EXPOSE 8001

HEALTHCHECK --interval=10s --timeout=5s --start-period=30s --retries=3 \
    CMD curl -f http://localhost:8001/health || exit 1

ENTRYPOINT ["python", "main.py", "--model", "$MODEL", "--host", "0.0.0.0", "--port", "8001"]
