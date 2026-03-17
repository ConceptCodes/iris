![preview](assets/preview.png)

`iris` is a production-grade image search engine with a Google Images-style UI, featuring **hybrid semantic + contextual ranking** to match Google Images' result relevance.

### Core Features

- **Text-to-Image Search**: Find images using natural language queries.
- **Reverse Image Search**: Find visually similar images via upload or URL.
- **Smart Indexing**: Crawl remote URLs, domains, sitemaps, or local directories.
- **Multi-Encoder Retrieval**: Index with multiple vision encoders and select the encoder used at search time.
- **High Performance**: Powered by Python encoder sidecars and Qdrant vector database.
- **Hybrid Re-Ranking**: Beyond pure vector similarity—combines visual semantics with domain authority, image quality, and freshness signals (inspired by Google Images architecture).

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
                    gRPC                gRPC
                       │                   │
             ┌─────────▼───────┐   ┌───────▼─────────┐
             │ Python Encoders │   │    Qdrant       │
             │ CLIP + SigLIP2  │   │   vector DB     │
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
| `clip` | 8001 | CLIP Embedding gRPC Service |
| `siglip2` | 8002 | SigLIP2 Embedding gRPC Service |
| `postgres` | 5432 | Worker Job Store |
| `grafana` | 3000 | Observability Dashboard |
| `minio` | 9000 / 9001 | S3-Compatible Object Store |

Grafana defaults to `admin` / `admin` in local Docker unless you override
`GRAFANA_ADMIN_USER` or `GRAFANA_ADMIN_PASSWORD`.

## Encoder Transport

The Go services talk to encoder sidecars over gRPC.

- CLIP address: `CLIP_ADDR` (default `localhost:8001`)
- SigLIP2 address: `SIGLIP2_ADDR` (optional, no default)
- Default search encoder: `DEFAULT_SEARCH_ENCODER` (default `clip`)
- Qdrant stores one named vector per encoder for each image record

Current encoder names:

- `clip`
- `siglip2`

## Metadata Enrichment

Ingestion can optionally enrich image records with:

- EXIF metadata extracted in Go
- OCR text from the metadata gRPC sidecar
- caption-derived tags from the metadata gRPC sidecar

Set `METADATA_ADDR` to enable the sidecar-backed enrichment path. The Docker
stack wires this to `http://metadata:8003` by default, which is normalized to a
gRPC target internally.

The encoder protobuf contract currently lives at `proto/clip/v1/clip.proto` and is shared by both sidecars.

If you change the protobuf contract, regenerate stubs with:

```bash
just proto
```

This updates:

- Go stubs in `internal/clip/clipv1`
- Python stubs in `clip_service/clip/v1`

## Hybrid Re-Ranking Strategy

Unlike pure vector similarity search, **iris** applies a weighted hybrid ranking formula inspired by Google Images:

```
final_score = (0.50 × similarity) + (0.20 × authority) + (0.15 × quality) + (0.15 × freshness)
```

### Ranking Signals

| Signal | Weight | What It Measures |
|--------|--------|------------------|
| **Visual Similarity** | 50% | Cosine distance between image embeddings (CLIP/SigLIP2) |
| **Domain Authority** | 20% | Image count per source domain (crawler-derived, no external APIs) |
| **Image Quality** | 15% | Resolution, color depth, composition (entropy)—computed at index time |
| **Freshness** | 15% | Time-decay based on index date (1.0 if < 7 days, exponential decay after) |

### Key Tradeoffs

- **Why crawler-derived authority?** Avoids external APIs, works offline. Reflects domain *volume*, not *quality*.
- **Why compute quality at index time?** Zero search latency. Quality scores are static per image.
- **Why no user engagement signals?** Privacy-first approach (no tracking). Cannot learn user preferences from clicks.
- **Why no domain overrides?** Scales to millions of domains automatically. Cannot manually boost/demote specific sources.

### Tuning

All parameters live in `internal/constants/constants.go`:

```go
const (
	MaxImageCountPerDomain  = 10000        // Authority ceiling
	RankingWeightSimilarity = 0.50         // Locked by constraint
	RankingWeightAuthority  = 0.20
	RankingWeightQuality    = 0.15
	RankingWeightFreshness  = 0.15
	FreshnessDays           = 7
)
```

To adjust: modify weights (must sum to 1.0), rebuild, new images use updated values.



## Scaling Notes

- **< 1M images**: Single Qdrant node.
- **1M – 50M**: Qdrant with on-disk payload + HNSW.
- **High ingest volume**: Move worker jobs to Postgres and split workloads.
- **GPU available**: Run CLIP sidecar on CUDA for faster embeddings.
- **Multiple encoders**: Reindex after enabling a new encoder so all stored records receive every named vector.

## Project Structure

- `cmd/`: Entry points for server, indexer, and worker.
- `internal/`: Core logic (API, crawl, indexing, search).
- `web/`: Frontend templates and assets.
- `proto/`: Shared protobuf contracts.
- `clip_service/`: Python CLIP gRPC service.
- `siglip_service/`: Python SigLIP2 gRPC service.
- `metadata_service/`: Python Metadata gRPC service.
- `infra/`: Docker and environment configuration.
