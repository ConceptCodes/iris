ARG PYTHON_VERSION=3.11
# ── Build stage ───────────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/server  ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/indexer ./cmd/indexer
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/worker ./cmd/worker

# ── Server runtime ────────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12 AS server
WORKDIR /app
COPY --from=builder /bin/server /server
COPY --from=builder /app/web/static /app/web/static
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

FROM python:${PYTHON_VERSION}-slim AS clip

# Build arg for PyTorch index (cpu, cu118, cu121, etc.)
# CPU: https://download.pytorch.org/whl/cpu (default)
# CUDA 11.8: https://download.pytorch.org/whl/cu118
# CUDA 12.1: https://download.pytorch.org/whl/cu121
ARG TORCH_INDEX=https://download.pytorch.org/whl/cpu

WORKDIR /app

# Install dependencies
COPY clip_service/requirements.txt /app/clip_service/requirements.txt

RUN pip install --no-cache-dir \
    -r clip_service/requirements.txt && \
    pip install --no-cache-dir torch --index-url ${TORCH_INDEX}

# Pre-download model weights at build time (speeds up container start)
RUN python -c "from open_clip import create_model_from_pretrained, get_tokenizer; \
    m, p = create_model_from_pretrained('hf-hub:laion/CLIP-ViT-B-32-laion2B-s34B-b79K'); \
    t = get_tokenizer('hf-hub:laion/CLIP-ViT-B-32-laion2B-s34B-b79K')"

COPY clip_service/main.py .
COPY clip_service/clip /app/clip

# Environment variables
ENV CLIP_MODEL=CLIP-ViT-B-32-laion2B-s34B-b79K
ENV PYTHONUNBUFFERED=1

EXPOSE 8001

HEALTHCHECK --interval=10s --timeout=5s --start-period=30s --retries=3 \
    CMD python -c "import grpc; from grpc_health.v1 import health_pb2, health_pb2_grpc; channel = grpc.insecure_channel('localhost:8001'); response = health_pb2_grpc.HealthStub(channel).Check(health_pb2.HealthCheckRequest(service='clip.v1.ClipService')); raise SystemExit(0 if response.status == health_pb2.HealthCheckResponse.SERVING else 1)" || exit 1

ENTRYPOINT ["sh", "-c", "python main.py --model $CLIP_MODEL --host 0.0.0.0 --port 8001"]

# ── SigLIP2 service runtime ───────────────────────────────────────────────────
FROM python:${PYTHON_VERSION}-slim AS siglip2

ARG TORCH_INDEX=https://download.pytorch.org/whl/cpu

WORKDIR /app

COPY siglip_service/requirements.txt /app/siglip_service/requirements.txt

RUN pip install --no-cache-dir \
    -r siglip_service/requirements.txt && \
    pip install --no-cache-dir torch --index-url ${TORCH_INDEX}

RUN python -c "from transformers import AutoModel, AutoProcessor; \
    AutoProcessor.from_pretrained('google/siglip2-base-patch16-224'); \
    AutoModel.from_pretrained('google/siglip2-base-patch16-224')"

COPY siglip_service/main.py .
COPY clip_service/clip /app/clip

ENV SIGLIP2_MODEL=google/siglip2-base-patch16-224
ENV PYTHONUNBUFFERED=1

EXPOSE 8002

HEALTHCHECK --interval=10s --timeout=5s --start-period=30s --retries=3 \
    CMD python -c "import grpc; from grpc_health.v1 import health_pb2, health_pb2_grpc; channel = grpc.insecure_channel('localhost:8002'); response = health_pb2_grpc.HealthStub(channel).Check(health_pb2.HealthCheckRequest(service='clip.v1.ClipService')); raise SystemExit(0 if response.status == health_pb2.HealthCheckResponse.SERVING else 1)" || exit 1

ENTRYPOINT ["sh", "-c", "python main.py --model $SIGLIP2_MODEL --host 0.0.0.0 --port 8002"]
