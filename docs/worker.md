# Worker and CLI Indexing

## CLI Indexing

```bash
# Index every image in a local directory
go run ./cmd/indexer -mode dir -input ./images

# Index from a URL list (one URL per line)
go run ./cmd/indexer -mode urls -input urls.txt

# Bootstrap a small demo corpus immediately
go run ./cmd/indexer -mode urls -input ./examples/demo-urls.txt
```

## Worker

The worker supports two modes and two storage backends:

```bash
go run ./cmd/worker
JOB_BACKEND=postgres go run ./cmd/worker -seed-url-file ./examples/demo-urls.txt
```

### Features

- Leases jobs from memory or Postgres
- Handles `fetch_image` and `index_local_file`
- Handles `discover_source` for `local_dir`, `url_list`, `domain`, and `sitemap` sources
- Dedupes downstream image and local-file jobs within a run
- Suppresses corpus-wide duplicates before asset persistence
- Applies per-source crawl throttling and budgets
- Respects `robots.txt` and `rel=canonical`
- Normalizes discovered URLs
- Caps concurrent fetches per host
- Adapts scheduled runs based on yield and failure history
- Prunes expired cache rows (TTL-based)
