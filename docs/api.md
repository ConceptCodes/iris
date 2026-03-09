# API Reference

## Search and Index APIs

### Index an image from a URL

```bash
curl -X POST http://localhost:8080/index/url \
  -H 'Content-Type: application/json' \
  -d '{
    "url": "https://example.com/dog.jpg",
    "filename": "dog.jpg",
    "tags": ["dog", "animal"],
    "meta": { "category": "pets", "source": "remote" }
  }'
```

### Index an image by upload

```bash
curl -X POST http://localhost:8080/index/upload \
  -F image=@/path/to/image.jpg \
  -F tags="dog,animal" \
  -F meta_category=pets
```

### Text search

```bash
curl -X POST http://localhost:8080/search/text \
  -H 'Content-Type: application/json' \
  -d '{
    "query": "a golden retriever on a beach",
    "top_k": 10,
    "filters": { "meta_category": "pets" }
  }'
```

### Reverse image search by upload

```bash
curl -X POST http://localhost:8080/search/image \
  -F image=@/path/to/query.jpg \
  -F top_k=10
```

### Reverse image search by URL

```bash
curl -X POST http://localhost:8080/search/image/url \
  -H 'Content-Type: application/json' \
  -d '{ "url": "https://example.com/query.jpg", "top_k": 10 }'
```

## Admin Crawl APIs

### Create a crawl source

`local_dir` source:

```bash
curl -X POST http://localhost:8080/admin/sources \
  -H 'Content-Type: application/json' \
  -H 'X-Admin-Key: dev-admin-key' \
  -d '{
    "kind": "local_dir",
    "local_path": "./images"
  }'
```

`url_list` source:

```bash
curl -X POST http://localhost:8080/admin/sources \
  -H 'Content-Type: application/json' \
  -H 'X-Admin-Key: dev-admin-key' \
  -d '{
    "kind": "url_list",
    "seed_url": "https://example.com/list.txt"
  }'
```

`domain` source:

```bash
curl -X POST http://localhost:8080/admin/sources \
  -H 'Content-Type: application/json' \
  -H 'X-Admin-Key: dev-admin-key' \
  -d '{
    "kind": "domain",
    "seed_url": "https://example.com",
    "max_depth": 1,
    "rate_limit_rps": 2,
    "max_pages_per_run": 100,
    "max_images_per_run": 500,
    "allowed_domains": ["example.com"]
  }'
```

`sitemap` source:

```bash
curl -X POST http://localhost:8080/admin/sources \
  -H 'Content-Type: application/json' \
  -H 'X-Admin-Key: dev-admin-key' \
  -d '{
    "kind": "sitemap",
    "seed_url": "https://example.com/sitemap.xml",
    "rate_limit_rps": 2,
    "allowed_domains": ["example.com"]
  }'
```

### Trigger a run

```bash
curl -X POST http://localhost:8080/admin/sources/<source-id>/run \
  -H 'Content-Type: application/json' \
  -H 'X-Admin-Key: dev-admin-key' \
  -d '{"trigger":"manual"}'
```

### Inspect runs

```bash
curl -H 'X-Admin-Key: dev-admin-key' http://localhost:8080/admin/runs
curl -H 'X-Admin-Key: dev-admin-key' http://localhost:8080/admin/runs/<run-id>
```

### Bulk re-index images

```bash
curl -X POST http://localhost:8080/admin/reindex \
  -H 'Content-Type: application/json' \
  -H 'X-Admin-Key: dev-admin-key' \
  -d '{}'
```
