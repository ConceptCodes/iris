# Observability

Iris includes built-in observability with Prometheus metrics, Grafana dashboards, and distributed tracing.

## Metrics

The server exposes Prometheus metrics at `/metrics`.

### Available Metrics

| Metric | Type | Description |
| :--- | :--- | :--- |
| `iris_search_requests_total` | Counter | Search requests by type (text, image_upload, image_url) |
| `iris_search_errors_total` | Counter | Search errors by type |
| `iris_search_latency_seconds` | Histogram | Search latency distribution |
| `iris_index_requests_total` | Counter | Index requests by type (url, upload) |
| `iris_index_errors_total` | Counter | Index errors by type |
| `iris_index_latency_seconds` | Histogram | Index latency distribution |
| `iris_crawl_runs_queued_total` | Counter | Crawl runs triggered |
| `iris_crawl_jobs_discovered_total` | Counter | Images discovered during crawl |
| `iris_crawl_jobs_indexed_total` | Counter | Images successfully indexed |
| `iris_crawl_jobs_duplicate_total` | Counter | Images skipped because they already exist in the corpus |
| `iris_crawl_jobs_failed_total` | Counter | Failed crawl jobs |
| `iris_dedupe_events_total` | CounterVec | Corpus-wide dedupe decisions by match reason |
| `iris_crawl_skips_total` | CounterVec | Crawl skips by policy reason such as robots |
| `iris_crawl_budget_hits_total` | CounterVec | Crawl budget limit hits by budget type |
| `iris_scheduler_decisions_total` | CounterVec | Adaptive scheduler decisions by decision type |
| `iris_scheduler_next_run_seconds` | HistogramVec | Next-run delays selected by the scheduler |
| `iris_worker_jobs_succeeded_total` | Counter | Worker jobs completed successfully |
| `iris_worker_jobs_failed_total` | Counter | Worker jobs that failed |
| `iris_worker_job_latency_seconds` | Histogram | Worker job processing time |

### Grafana Dashboard

Access the pre-configured Grafana dashboard at [http://localhost:3000](http://localhost:3000) (anonymous access enabled for development).

The dashboard includes:

- **Overview**: Request rate, error rate, active crawl runs
- **Search**: Latency percentiles (p50, p95, p99) for text, image, and reverse search
- **Indexing**: Index operation rate and latency
- **Crawl**: Jobs discovered, indexed, and failed
- **Crawl Control**: Duplicate suppression, robots skips, and crawl budget hits
- **Scheduling**: Adaptive scheduler decisions and next-run delay distribution
- **Worker**: Jobs processed and success rate

### Prometheus

Access Prometheus directly at [http://localhost:9090](http://localhost:9090) for ad-hoc queries and alerting configuration.

## Distributed Tracing

Iris includes built-in distributed tracing using OpenTelemetry and Jaeger. Traces are automatically collected for key operations across the system.

### Jaeger UI

Access the Jaeger UI at [http://localhost:16686](http://localhost:16686) to visualize distributed traces.

### Traced Operations

The following operations are instrumented with spans:

**Search Operations:**

- `SearchByText` - Text-based image search
- `SearchByImageBytes` - Reverse image search from uploaded bytes
- `SearchByImageURL` - Reverse image search from URL
- `GetSimilar` - Finding similar images

**Index Operations:**

- `IndexFromURL` - Indexing images from remote URLs
- `IndexFromBytes` - Indexing images from uploaded bytes

**CLIP Embedding:**

- `EmbedText` - Generating text embeddings
- `EmbedImageBytes` - Generating image embeddings

**Qdrant Operations:**

- `Upsert` - Inserting/updating vector records
- `Search` - Vector similarity search
- `Delete` - Removing vector records

**Crawl Discovery:**

- `ExtractHTMLLinks` - Extracting links and images from HTML
- `ExtractSitemapLocs` - Extracting URLs from sitemaps

### Configuration

Tracing is controlled by the following environment variables:

| Variable | Default | Description |
| :--- | :--- | :--- |
| `OTEL_ENABLED` | `true` | Enable or disable tracing |
| `OTEL_ENDPOINT` | `localhost:4317` | OTLP endpoint for trace export (Jaeger gRPC port) |

To disable tracing, set `OTEL_ENABLED=false`:

```bash
OTEL_ENABLED=false go run ./cmd/server
```
