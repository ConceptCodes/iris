# Benchmarking

This project now has a first-pass microbenchmark suite for the main in-process hot paths.

## Coverage

- `internal/search`: search and indexing engine methods with mocked CLIP and vector store dependencies
- `internal/indexing`: upload and local-file indexing paths, including duplicate suppression and local asset persistence
- `internal/crawl`: cached fetcher hot-cache and cold-fetch behavior
- `internal/jobs`: memory job-store lease cost and SQL lease-query construction cost
- `internal/jobs`: opt-in real Postgres job-store benchmarks via `JOB_STORE_BENCH_DSN`
- `internal/api`: request-level handler benchmarks for search and indexing endpoints
- `internal/clip`: opt-in real CLIP integration benchmarks via `CLIP_BENCH_ADDR`
- `internal/store`: opt-in real Qdrant integration benchmarks via `QDRANT_BENCH_ADDR`

These benchmarks are intentionally isolated from external services. They answer:

- how much overhead the Go code adds before CLIP or Qdrant are involved
- how much duplicate suppression saves versus full indexing
- how much caching changes fetch cost
- how job leasing cost scales in the in-memory path

## Commands

Run the current benchmark suite:

```bash
go test -run '^$' -bench . -benchmem ./internal/search ./internal/indexing ./internal/crawl ./internal/jobs ./internal/api
```

Run one package:

```bash
go test -run '^$' -bench . -benchmem ./internal/search
```

Generate a CPU profile for one package:

```bash
go test -run '^$' -bench BenchmarkPipeline -benchmem -cpuprofile cpu.out ./internal/indexing
go tool pprof cpu.out
```

Run opt-in integration benchmarks:

```bash
CLIP_BENCH_ADDR=localhost:8001 go test -run '^$' -bench Integration -benchmem ./internal/clip
QDRANT_BENCH_ADDR=localhost:6334 go test -run '^$' -bench Integration -benchmem ./internal/store
JOB_STORE_BENCH_DSN='postgres://iris:iris@localhost:5432/iris?sslmode=disable' go test -run '^$' -bench PostgresStoreIntegration -benchmem ./internal/jobs
JOB_STORE_BENCH_DSN='postgres://iris:iris@localhost:5432/iris?sslmode=disable' go test -run '^$' -bench PostgresStoreConcurrentIntegration -benchmem ./internal/jobs
```

## Next Step

After you have baseline numbers, add integration benchmarks for:

- API endpoint load tests for `/search/text`, `/search/image`, `/index/upload`
- worker throughput with the Postgres job store under multiple worker processes
- end-to-end server load tests with live CLIP and Qdrant behind the HTTP API
