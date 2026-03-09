# iris

`iris` is a Go image search engine with a Google Images-style UI.

- **Text-to-Image Search**: Find images using natural language queries.
- **Reverse Image Search**: Find visually similar images via upload or URL.
- **Smart Indexing**: Crawl remote URLs, domains, sitemaps, or local directories.
- **High Performance**: Powered by a Python CLIP sidecar and Qdrant vector database.

## Architecture

```text
┌──────────────────────────────────────────────────────────┐
│                         Client                           │
└───────────────┬──────────────────────────────┬───────────┘
                │                              │
          Search traffic                 Index/admin traffic
                │                              │
        ┌───────▼──────────────────────────────▼───────┐
        │                Go API Server                 │
        │                  cmd/server                  │
        └──────────────┬───────────────────┬───────────┘
                       │                   │
                    HTTP                gRPC
                       │                   │
             ┌─────────▼───────┐   ┌───────▼─────────┐
             │  Python CLIP    │   │    Qdrant       │
             │  clip_service   │   │   vector DB     │
             └─────────────────┘   └─────────────────┘

        ┌──────────────────────────────────────────────┐
        │            Shared Ingestion Pipeline         │
        │              internal/indexing               │
        └──────────────┬───────────────────────┬───────┘
                       │                       │
                  cmd/indexer              cmd/worker
```

## Quick Start

```bash
docker compose -f infra/docker-compose.yml up --build
```

Or, with `just` installed:

```bash
just dev
```

Visit [http://localhost:8080](http://localhost:8080) to use the UI.

### Services

| Service | Port | Purpose |
| :--- | :--- | :--- |
| `server` | 8080 | Main API and Web UI |
| `qdrant` | 6333 | Vector Database |
| `clip` | 8001 | CLIP Embedding Service |
| `postgres` | 5432 | Worker Job Store |
| `grafana` | 3000 | Observability Dashboard |


## Scaling Notes

- **< 1M images**: Single Qdrant node.
- **1M – 50M**: Qdrant with on-disk payload + HNSW.
- **High ingest volume**: Move worker jobs to Postgres and split workloads.
- **GPU available**: Run CLIP sidecar on CUDA for faster embeddings.

## Project Structure

- `cmd/`: Entry points for server, indexer, and worker.
- `internal/`: Core logic (API, crawl, indexing, search).
- `web/`: Frontend templates and assets.
- `clip_service/`: Python CLIP sidecar.
- `infra/`: Docker and environment configuration.
