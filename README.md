# Image Search Engine — CLIP + Qdrant + Go

A production-ready image search engine that supports both **text → image** and
**reverse image** (image → image) search, powered by OpenAI CLIP embeddings and
the Qdrant vector database.

---

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                      Client                             │
└───────────────┬────────────────────┬────────────────────┘
                │ REST               │ REST
        ┌───────▼───────┐    ┌───────▼──────┐
        │  POST /search │    │  POST /index │
        │  /text        │    │  /url        │
        │  /image       │    │  /upload     │
        │  /image/url   │    └──────┬───────┘
        └───────┬───────┘           │
                │                   │
        ┌───────▼───────────────────▼───────┐
        │           Go API Server           │  cmd/server
        │   internal/api  internal/search   │
        └──────┬───────────────────┬────────┘
               │ HTTP              │ gRPC
    ┌──────────▼──────┐  ┌─────────▼────────┐
    │  Python CLIP    │  │   Qdrant         │
    │  Sidecar        │  │   Vector DB      │
    │  clip_service/  │  │   (ANN search)   │
    └─────────────────┘  └──────────────────┘
```

### Why a sidecar for CLIP?

Go has no first-class PyTorch bindings. Calling CLIP via a lightweight FastAPI
sidecar keeps the Go codebase dependency-free while retaining the ability to
swap models (ViT-B/32 → ViT-L/14, or a fine-tuned variant) without recompiling
the server.

---

## Quick start

```bash
docker compose up --build
```

This starts:
| Service | Port | Purpose |
|---------|------|---------|
| `qdrant` | 6333 (REST), 6334 (gRPC) | Vector DB |
| `clip`   | 8001 | CLIP embedding sidecar |
| `server` | 8080 | Go API |

---

## API Reference

### Index an image from a URL

```bash
curl -X POST http://localhost:8080/index/url \
  -H 'Content-Type: application/json' \
  -d '{
    "url": "https://example.com/dog.jpg",
    "filename": "dog.jpg",
    "tags": ["dog", "animal"],
    "meta": { "category": "pets", "source": "unsplash" }
  }'
# → { "id": "uuid", "message": "indexed" }
```

### Index an image by upload

```bash
curl -X POST http://localhost:8080/index/upload \
  -F image=@/path/to/image.jpg \
  -F tags="dog,animal" \
  -F meta_category=pets
```

### Text → image search

```bash
curl -X POST http://localhost:8080/search/text \
  -H 'Content-Type: application/json' \
  -d '{
    "query": "a golden retriever on a beach",
    "top_k": 10,
    "filters": { "meta_category": "pets" }
  }'
```

### Reverse image search — upload

```bash
curl -X POST http://localhost:8080/search/image \
  -F image=@/path/to/query.jpg \
  -F top_k=10
```

### Reverse image search — URL

```bash
curl -X POST http://localhost:8080/search/image/url \
  -H 'Content-Type: application/json' \
  -d '{ "url": "https://example.com/query.jpg", "top_k": 10 }'
```

### Response shape

```json
{
  "results": [
    {
      "record": {
        "id": "...",
        "url": "https://...",
        "filename": "dog.jpg",
        "tags": ["dog"],
        "meta": { "category": "pets" }
      },
      "score": 0.94
    }
  ],
  "query": "a golden retriever on a beach",
  "took_ms": 12
}
```

---

## Batch indexing

```bash
# Index every image in a local directory
CLIP_ADDR=http://localhost:8001 \
QDRANT_ADDR=localhost:6334 \
CONCURRENCY=8 \
go run ./cmd/indexer -mode dir -input ./images

# Index from a URL list (one URL per line)
go run ./cmd/indexer -mode urls -input urls.txt
```

---

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `CLIP_ADDR` | `http://localhost:8001` | CLIP sidecar base URL |
| `QDRANT_ADDR` | `localhost:6334` | Qdrant gRPC address |
| `CLIP_DIM` | `512` | Embedding dimension (512 = ViT-B/32, 768 = ViT-L/14) |
| `HTTP_ADDR` | `:8080` | Go server listen address |
| `CONCURRENCY` | `4` | Indexer worker count |

---

## Scaling notes

| Scale | Recommendation |
|-------|---------------|
| < 1M images | Single Qdrant node, in-memory HNSW index |
| 1M – 50M | Qdrant with on-disk payload + HNSW, multiple shards |
| > 50M | Qdrant distributed cluster, or migrate to Milvus/Weaviate |
| High QPS | Run 2+ CLIP sidecar replicas behind a load balancer; add an embedding cache (Redis) keyed on image URL SHA256 |
| GPU available | Set `device=cuda` in sidecar; single A10G embeds ~500 img/s |

---

## Model selection

| Model | Dim | Quality | Speed (CPU) |
|-------|-----|---------|-------------|
| `ViT-B-32` | 512 | Good | ~20 img/s |
| `ViT-B-16` | 512 | Better | ~12 img/s |
| `ViT-L-14` | 768 | Best | ~5 img/s |

Change the model by setting the `MODEL` environment variable on the `clip`
container and updating `CLIP_DIM` on the `server` container.

---

## Project structure

```
├── cmd/
│   ├── server/main.go        HTTP API entry-point
│   └── indexer/main.go       Batch indexing CLI
├── internal/
│   ├── api/
│   │   ├── handler.go        HTTP handlers
│   │   └── router.go         Chi router + middleware
│   ├── clip/
│   │   └── client.go         CLIP sidecar HTTP client
│   ├── search/
│   │   └── engine.go         Search + index orchestration
│   └── store/
│       └── qdrant.go         Qdrant vector store wrapper
├── pkg/models/types.go       Shared types
├── clip_service/
│   ├── main.py               FastAPI CLIP sidecar
│   └── Dockerfile
├── docker-compose.yml
└── Dockerfile
```