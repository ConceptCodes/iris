# 🔍 Production Readiness Audit — iris

> Audit performed: 2026-03-12
> Scope: Full codebase (~47 Go source files, ~8,500 LOC)

---

## Executive Summary

The codebase is **well-structured overall** with good separation of concerns, proper interface abstractions, and thoughtful security measures (SSRF protection, rate limiting, admin auth). However, there are **several issues ranging from critical to moderate** that should be resolved before production deployment.

| Severity | Count | Category |
|----------|-------|----------|
| 🔴 Critical | 3 | Race conditions, memory safety, data loss |
| 🟠 High | 6 | Design flaws that cause subtle production bugs |
| 🟡 Medium | 8 | Operational & resilience concerns |
| 🔵 Low | 5 | Code quality & minor issues |

---

## 🔴 Critical Issues

### 1. Race Condition: Global Mutable State in Rate Limiter

**File:** [middleware.go](file:///Users/davidojo/Desktop/go-projects/google-images/internal/api/middleware.go#L17-L20)

```go
var (
    visitors = make(map[string]*visitor)
    mu       sync.Mutex
)
```

**Problem:** The rate limiter uses **package-level global mutable state** with a background goroutine launched from [init()](file:///Users/davidojo/Desktop/go-projects/google-images/internal/api/middleware.go#22-25). This creates multiple issues:

1. **Leaking goroutine in [init()](file:///Users/davidojo/Desktop/go-projects/google-images/internal/api/middleware.go#22-25)** — The [cleanupVisitors()](file:///Users/davidojo/Desktop/go-projects/google-images/internal/api/middleware.go#26-38) goroutine runs forever with no shutdown mechanism. It cannot be stopped during graceful shutdown, and in tests, it leaks across test runs.
2. **Global state prevents testing** — Multiple test cases sharing the same global visitors map will interfere with each other. You cannot run tests in parallel.
3. **No context propagation** — The cleanup goroutine has no awareness of application lifecycle.

**Fix:** Convert the rate limiter to an instance-based design injected via the router:
```diff
-var (
-    visitors = make(map[string]*visitor)
-    mu       sync.Mutex
-)
-
-func init() {
-    go cleanupVisitors()
-}
+type RateLimiter struct {
+    mu       sync.Mutex
+    visitors map[string]*visitor
+    done     chan struct{}
+}
+
+func NewRateLimiter() *RateLimiter {
+    rl := &RateLimiter{
+        visitors: make(map[string]*visitor),
+        done:     make(chan struct{}),
+    }
+    go rl.cleanupLoop()
+    return rl
+}
+
+func (rl *RateLimiter) Stop() { close(rl.done) }
```

---

### 2. Race Condition: CachedFetcher In-Memory Cache + Store Inconsistency

**File:** [http.go](file:///Users/davidojo/Desktop/go-projects/google-images/internal/crawl/http.go#L93-L144)

**Problem:** The [Fetch](file:///Users/davidojo/Desktop/go-projects/google-images/internal/crawl/http.go#93-145) method performs a **check-then-act** pattern across the in-memory cache and the persistent store that is **not atomic**:

```go
f.mu.Lock()
cached, ok := f.cache[normalizedURL]   // Step 1: check in-memory
f.mu.Unlock()                           // UNLOCK — window opens
if !ok {
    persisted, found, err := f.store.Get(ctx, normalizedURL)  // Step 2: check store (no lock)
    if found {
        f.mu.Lock()
        f.cache[normalizedURL] = persisted  // Step 3: populate in-memory
        f.mu.Unlock()
    }
}
```

When multiple goroutines fetch the same URL concurrently, they can all miss the in-memory cache, all query the persistent store, and all start separate HTTP requests for the same resource. This creates:
- **Thundering herd** on the origin server for the same URL
- **Wasted bandwidth and compute** for duplicate requests
- **No correctness issue** per se (eventual consistency is fine), but **real operational cost**

**Fix:** Use `sync.Map` or a `singleflight.Group` to coalesce concurrent lookups:
```go
import "golang.org/x/sync/singleflight"

type CachedFetcher struct {
    // ...existing fields...
    flight singleflight.Group
}

func (f *CachedFetcher) Fetch(ctx context.Context, rawURL string) (FetchResult, error) {
    v, err, _ := f.flight.Do(normalizedURL, func() (interface{}, error) {
        return f.fetchInternal(ctx, normalizedURL)
    })
    // ...
}
```

---

### 3. Unbounded Memory Growth: In-Memory Cache Never Evicts

**File:** [http.go](file:///Users/davidojo/Desktop/go-projects/google-images/internal/crawl/http.go#L31)

```go
cache       map[string]cachedResource
hostLimiter map[string]chan struct{}
```

**Problem:** Both [cache](file:///Users/davidojo/Desktop/go-projects/google-images/internal/crawl/http.go#35-41) and `hostLimiter` maps **grow without bound**. The [cache](file:///Users/davidojo/Desktop/go-projects/google-images/internal/crawl/http.go#35-41) map stores full HTTP response bodies (up to 10MB each). Over time in production:

- Crawling 10,000 pages ≈ 10,000 entries × avg 500KB = **~5GB of RAM consumed** that is never freed
- The `hostLimiter` map grows by one entry per unique host forever
- Expired entries remain in memory even after their `expiresAt` passes — they are only refreshed on the next access, never pruned

**Fix:**
1. Add a max-size LRU eviction policy to the in-memory cache
2. Periodically prune expired entries
3. Cap the `hostLimiter` map or use `sync.Map` with TTL

---

## 🟠 High Severity Issues

### 4. Postgres Job Store: MarkFailed Has a TOCTOU Race

**File:** [postgres.go (jobs)](file:///Users/davidojo/Desktop/go-projects/google-images/internal/jobs/postgres.go#L149-L176)

```go
func (s *PostgresStore) MarkFailed(ctx context.Context, id string, failure error, retryAt time.Time) (Status, error) {
    const selectQuery = `SELECT attempts, max_attempts FROM jobs WHERE id = $1`
    var attempts, maxAttempts int
    if err := s.db.QueryRowContext(ctx, selectQuery, id).Scan(&attempts, &maxAttempts); err != nil {
        return "", ...
    }
    status := StatusPending
    if attempts >= maxAttempts {
        status = StatusDeadLetter
    }
    _, err := s.db.ExecContext(ctx, `UPDATE jobs SET status = $2 ...`, id, string(status), ...)
```

**Problem:** The SELECT and UPDATE are two separate operations with no transaction. Between the SELECT and UPDATE, another worker could modify the same row. This is a classic **Time-of-Check-to-Time-of-Use (TOCTOU)** bug.

**Fix:** Use a single atomic SQL statement:
```sql
UPDATE jobs
SET status = CASE WHEN attempts >= max_attempts THEN 'dead_letter' ELSE 'pending' END,
    last_error = $2,
    available_at = $3,
    leased_until = NULL,
    updated_at = $4
WHERE id = $1
RETURNING status
```

---

### 5. Permanent Error Still Gets retryAt = time.Time{} But MarkFailed Still Retries

**File:** [cmd/worker/main.go](file:///Users/davidojo/Desktop/go-projects/google-images/cmd/worker/main.go#L266-L268)

```go
if errType == errorTypePermanent {
    retryAt = time.Time{} // No retry for permanent errors
}
markStatus, markErr := jobStore.MarkFailed(ctx, job.ID, err, retryAt)
```

**Problem:** Setting `retryAt = time.Time{}` (the zero time) doesn't actually prevent retries. In [MarkFailed](file:///Users/davidojo/Desktop/go-projects/google-images/internal/jobs/types.go#49-50), if `attempts < maxAttempts`, the job is set back to `StatusPending` with `available_at = '0001-01-01...'`, which is **always in the past** — meaning it will be picked up immediately by the next [LeaseNext](file:///Users/davidojo/Desktop/go-projects/google-images/internal/jobs/types.go#47-48) call. This creates an infinite retry loop for permanent errors until `maxAttempts` is exhausted.

**Fix:** For permanent errors, force the status to `StatusDeadLetter` directly:
```go
if errType == errorTypePermanent {
    jobStore.MarkFailed(ctx, job.ID, err, time.Time{}) // with forced dead_letter status
}
```
Or better, add a `MarkDeadLetter` method to the Store interface.

---

### 6. File Upload: Incomplete Read via `file.Read(buf)`

**File:** [handler.go](file:///Users/davidojo/Desktop/go-projects/google-images/internal/api/handler.go#L94-L95)

```go
buf := make([]byte, header.Size)
if _, err := file.Read(buf); err != nil {
```

**Problem:** `file.Read(buf)` is **not guaranteed to read all bytes** in a single call. Per the `io.Reader` contract, `Read` may return fewer bytes than `len(buf)` without error. For large uploads, this will silently produce **truncated images** that fail the embedding pipeline downstream with confusing errors.

This same bug exists at [line 195-196](file:///Users/davidojo/Desktop/go-projects/google-images/internal/api/handler.go#L195-L196) in [SearchImage](file:///Users/davidojo/Desktop/go-projects/google-images/internal/api/handler.go#172-224).

**Fix:**
```diff
-if _, err := file.Read(buf); err != nil {
+if _, err := io.ReadFull(file, buf); err != nil {
```

---

### 7. S3Store.Save Uses `context.Background()` Instead of Request Context

**File:** [s3.go](file:///Users/davidojo/Desktop/go-projects/google-images/internal/assets/s3.go#L100)

```go
_, err := s.client.PutObject(context.Background(), &s3.PutObjectInput{...})
```

**Problem:** Using `context.Background()` means:
- S3 uploads **cannot be cancelled** when the HTTP request times out
- If S3 is slow/unreachable, the goroutine handling the request will hang indefinitely
- No trace propagation — uploads are invisible to OpenTelemetry

**Fix:** Add `ctx context.Context` to the `Store.Save` method signature and pass it through:
```diff
 type Store interface {
-    Save(id, filename string, data []byte) (string, error)
+    Save(ctx context.Context, id, filename string, data []byte) (string, error)
 }
```

---

### 8. reflect.ValueOf(e.store).IsNil() Is Fragile and Panics on Non-Pointer Types

**File:** [engine.go](file:///Users/davidojo/Desktop/go-projects/google-images/internal/search/engine.go#L65)

```go
if e.store == nil || reflect.ValueOf(e.store).IsNil() {
```

**Problem:** This appears in **every method** of [engineImpl](file:///Users/davidojo/Desktop/go-projects/google-images/internal/search/engine.go#41-45). Using `reflect.ValueOf().IsNil()` on an interface:
- **Panics** if the underlying type is not a pointer, map, or function
- Is an anti-pattern — the nil check on the interface `e.store == nil` should be sufficient if the constructor is correct
- Adds unnecessary runtime overhead on every call

**Fix:** Validate at construction time (in [NewEngine](file:///Users/davidojo/Desktop/go-projects/google-images/internal/search/engine.go#46-52)) that `qdrantStore` is not nil, and remove all the reflect-based checks:
```go
func NewEngine(encoders *encoder.Registry, qdrantStore VectorStore) (Engine, error) {
    if qdrantStore == nil {
        return nil, fmt.Errorf("vector store is required")
    }
    return &engineImpl{encoders: encoders, store: qdrantStore}, nil
}
```

---

### 9. Error Messages Leak Internal Details to Clients

**File:** [handler.go](file:///Users/davidojo/Desktop/go-projects/google-images/internal/api/handler.go#L64)

```go
writeError(w, http.StatusInternalServerError, err.Error())
```

**Problem:** Internal error messages (including stack traces, database connection strings, gRPC errors, etc.) are sent directly to the client in [IndexFromURL](file:///Users/davidojo/Desktop/go-projects/google-images/internal/api/handler.go#35-70), [SearchText](file:///Users/davidojo/Desktop/go-projects/google-images/internal/api/handler.go#131-171), [SearchImage](file:///Users/davidojo/Desktop/go-projects/google-images/internal/api/handler.go#172-224), [SearchImageURL](file:///Users/davidojo/Desktop/go-projects/google-images/internal/api/handler.go#225-270), and [HandleReindex](file:///Users/davidojo/Desktop/go-projects/google-images/internal/api/handler.go#391-465). This is an **information disclosure vulnerability**.

**Fix:** Return generic error messages to clients and log the actual error server-side:
```go
slog.Error("index from url failed", "error", err)
writeError(w, http.StatusInternalServerError, "internal server error")
```

---

## 🟡 Medium Severity Issues

### 10. Worker Loop Exits on Transient LeaseNext or MarkSucceeded Errors

**File:** [cmd/worker/main.go](file:///Users/davidojo/Desktop/go-projects/google-images/cmd/worker/main.go#L243-L246)

```go
job, ok, err := jobStore.LeaseNext(ctx, ...)
if err != nil {
    return err  // 🔴 This kills the entire worker
}
```

Similarly at [line 270-273](file:///Users/davidojo/Desktop/go-projects/google-images/cmd/worker/main.go#L270-L273) and [line 281-283](file:///Users/davidojo/Desktop/go-projects/google-images/cmd/worker/main.go#L281-L283).

**Problem:** A transient database blip (brief Postgres outage, network hiccup) will cause the entire worker process to exit. In production, this means:
- Lost in-progress work
- Requires external restart (if not supervised)
- Cascading failures when all workers die simultaneously

**Fix:** Add retry logic with backoff for infrastructure errors, only exit on permanent/context errors:
```go
if err != nil {
    if ctx.Err() != nil {
        return nil
    }
    slog.Error("lease next failed, will retry", "error", err)
    time.Sleep(5 * time.Second)
    continue
}
```

---

### 11. Qdrant Store Uses Deprecated `grpc.DialContext` with Blocking

**File:** [qdrant.go](file:///Users/davidojo/Desktop/go-projects/google-images/internal/store/qdrant.go#L40-L43)

```go
conn, err := grpc.DialContext(ctx, addr,
    grpc.WithTransportCredentials(insecure.NewCredentials()),
    grpc.WithBlock(),
)
```

**Problem:**
- `grpc.DialContext` is **deprecated** in modern gRPC-Go; use `grpc.NewClient`
- `grpc.WithBlock()` is **deprecated** — modern gRPC lazily connects
- `insecure.NewCredentials()` — no TLS for Qdrant communication. Fine for internal networks but a risk if traffic crosses network boundaries

---

### 12. Database Connection Pool Not Configured

**Files:**
- [postgres.go (jobs)](file:///Users/davidojo/Desktop/go-projects/google-images/internal/jobs/postgres.go#L23)
- [postgres.go (crawl)](file:///Users/davidojo/Desktop/go-projects/google-images/internal/crawl/postgres.go#L22)
- [cache_postgres.go](file:///Users/davidojo/Desktop/go-projects/google-images/internal/crawl/cache_postgres.go#L20)

```go
db, err := sql.Open("pgx", dsn)
```

**Problem:** `sql.Open` uses Go's default pool settings:
- `MaxOpenConns = 0` (unlimited)
- `MaxIdleConns = 2`
- `ConnMaxLifetime = 0` (no limit)

In production with moderate load, this leads to:
- Connection exhaustion at the database (hundreds of open connections)
- `FATAL: too many connections for role` errors
- Each Postgres store opens its own `sql.DB`, multiplying the problem (3 separate pools for jobs, crawl, cache)

**Fix:** Configure pool settings and consider sharing a single `*sql.DB` instance:
```go
db.SetMaxOpenConns(25)
db.SetMaxIdleConns(5)
db.SetConnMaxLifetime(5 * time.Minute)
```

---

### 13. No Request Body Size Limit on JSON Endpoints

**File:** [handler.go](file:///Users/davidojo/Desktop/go-projects/google-images/internal/api/handler.go#L41)

```go
if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
```

**Problem:** The [SearchText](file:///Users/davidojo/Desktop/go-projects/google-images/internal/api/handler.go#131-171), [IndexFromURL](file:///Users/davidojo/Desktop/go-projects/google-images/internal/api/handler.go#35-70), [CreateSource](file:///Users/davidojo/Desktop/go-projects/google-images/internal/crawl/store.go#9-10), [SearchImageURL](file:///Users/davidojo/Desktop/go-projects/google-images/internal/api/handler.go#225-270), and [HandleReindex](file:///Users/davidojo/Desktop/go-projects/google-images/internal/api/handler.go#391-465) endpoints decode JSON from `r.Body` without any size limit. An attacker can send a multi-GB JSON payload, causing:
- OOM on the server
- Denial of service

The [MaxRequestSize](file:///Users/davidojo/Desktop/go-projects/google-images/internal/api/middleware.go#74-82) middleware is only applied to upload/search routes in the router, not to JSON-body endpoints.

**Fix:** Apply [MaxRequestSize](file:///Users/davidojo/Desktop/go-projects/google-images/internal/api/middleware.go#74-82) middleware to all POST routes, or use `io.LimitReader`:
```go
json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req) // 1MB limit
```

---

### 14. Missing `ThumbnailURL` in Qdrant Payload Persistence

**File:** [qdrant.go](file:///Users/davidojo/Desktop/go-projects/google-images/internal/store/qdrant.go#L311-L328)

The [recordToPayload](file:///Users/davidojo/Desktop/go-projects/google-images/internal/store/qdrant.go#311-329) function stores [id](file:///Users/davidojo/Desktop/go-projects/google-images/internal/ssrf/validator.go#92-98), `url`, `filename`, `tags`, and `meta` — but **`ThumbnailURL` is never stored**. It is only set transiently in [pipeline.go:241](file:///Users/davidojo/Desktop/go-projects/google-images/internal/indexing/pipeline.go#L241). After the thumbnail is generated and uploaded to S3, its URL is lost when the record is retrieved from Qdrant.

**Fix:** Add `thumbnail_url` to the payload:
```go
if record.ThumbnailURL != "" {
    payload["thumbnail_url"] = &pb.Value{Kind: &pb.Value_StringValue{StringValue: record.ThumbnailURL}}
}
```

---

### 15. `crawl.MemoryStore` Redefines [max()](file:///Users/davidojo/Desktop/go-projects/google-images/internal/crawl/memory.go#299-305) — Shadows Go 1.21 Builtin

**File:** [memory.go](file:///Users/davidojo/Desktop/go-projects/google-images/internal/crawl/memory.go#L299-L304)

```go
func max(a, b int) int {
```

**Problem:** Go 1.21+ has a builtin [max()](file:///Users/davidojo/Desktop/go-projects/google-images/internal/crawl/memory.go#299-305). This function shadows it. While not a runtime bug, it:
- Confuses readers
- May cause lint warnings
- Should be removed if the project's [go.mod](file:///Users/davidojo/Desktop/go-projects/google-images/go.mod) targets Go 1.21+

---

### 16. `math/rand` Used Without Seeding (Pre-Go 1.20)

**File:** [cmd/worker/main.go](file:///Users/davidojo/Desktop/go-projects/google-images/cmd/worker/main.go#L11)

```go
"math/rand"
```

At [line 100](file:///Users/davidojo/Desktop/go-projects/google-images/cmd/worker/main.go#L100):
```go
jitter := time.Duration(rand.Int63n(int64(baseDelay / 2)))
```

**Problem:** If the project runs on Go < 1.20, `math/rand` defaults to a seed of 1, making all backoff jitter deterministic. On Go 1.20+, automatic seeding was added, but using `math/rand/v2` or `crypto/rand` is recommended for production.

---

### 17. No Graceful Shutdown for Worker Goroutines

**File:** [cmd/worker/main.go](file:///Users/davidojo/Desktop/go-projects/google-images/cmd/worker/main.go#L307-L308)

```go
go runtime.runCachePruneLoop(ctx, cfg)
go runSchedulerLoop(ctx, cfg, crawl.NewService(crawlStore, jobStore))
```

**Problem:** These background goroutines take a `ctx` derived from the signal context, so they will *eventually* stop. But the main worker loop can return (killing the process) before these goroutines finish their current operation. There's no `sync.WaitGroup` or other coordination to ensure clean shutdown.

---

## 🔵 Low Severity Issues

### 18. Duplicate Constants / Message Strings

**File:** [constants.go](file:///Users/davidojo/Desktop/go-projects/google-images/internal/constants/constants.go#L60-L87)

There are many duplicate or near-duplicate constants:
- `StatusMsgFileTooLarge` and `MessageFileTooLarge` (both = `"file too large"`)
- `StatusMsgURLRequired` and `MessageURLRequired`
- `StatusMsgNotFound` and `MessageNotFound`
- `MetaKeyType` and `MetaKeyContentType` (both = `"type"`)

This makes it easy to use the wrong constant or introduce inconsistencies.

---

### 19. CSP Header Allows `'unsafe-inline'` for Scripts

**File:** [router.go](file:///Users/davidojo/Desktop/go-projects/google-images/internal/api/router.go#L54-L63)

```go
"script-src 'self' 'unsafe-inline' https://cdn.tailwindcss.com https://unpkg.com; "
```

The comment acknowledges this is for development, but in production:
- `'unsafe-inline'` undermines XSS protection
- External CDN scripts should be self-hosted with SRI hashes

---

### 20. CORS Allows All Origins

**File:** [router.go](file:///Users/davidojo/Desktop/go-projects/google-images/internal/api/router.go#L88)

```go
AllowedOrigins: []string{"*"},
```

If this is an internal API, wildcard CORS is unnecessary. If it's public-facing, it exposes the API to cross-origin attacks from any website.

---

### 21. [ListRuns](file:///Users/davidojo/Desktop/go-projects/google-images/internal/crawl/memory.go#78-86) Has No Pagination

**File:** [crawl/postgres.go](file:///Users/davidojo/Desktop/go-projects/google-images/internal/crawl/postgres.go#L100-L116)

```sql
SELECT ... FROM crawl_runs ORDER BY created_at DESC
```

No `LIMIT` or pagination — as runs accumulate, this query returns all historical runs, consuming progressively more memory and bandwidth.

---

### 22. [wrapRPCError](file:///Users/davidojo/Desktop/go-projects/google-images/internal/clip/client.go#169-179) Swallows Error Chain

**File:** [clip/client.go](file:///Users/davidojo/Desktop/go-projects/google-images/internal/clip/client.go#L169-L178)

```go
func wrapRPCError(operation string, err error) error {
    st, ok := status.FromError(err)
    if !ok {
        return fmt.Errorf("%s: %w", operation, err)
    }
    if st.Code() == codes.OK {
        return nil
    }
    return fmt.Errorf("%s: %s", operation, st.Message())  // %s, not %w
}
```

When the error *is* a gRPC status error, the original error is replaced with a new `fmt.Errorf` using `%s` (not `%w`), breaking `errors.Is()` and `errors.As()` chains. The [classifyError](file:///Users/davidojo/Desktop/go-projects/google-images/cmd/worker/main.go#43-87) function in the worker relies on error type inspection, which will fail for gRPC errors.

---

## Summary of Recommended Actions (Priority Order)

| # | Issue | Action | Effort |
|---|-------|--------|--------|
| 1 | Global rate limiter state | Refactor to instance-based with shutdown | 🟡 Medium |
| 2 | CachedFetcher thundering herd | Add `singleflight` | 🟢 Small |
| 3 | Unbounded in-memory cache | Add LRU eviction + prune loop | 🟡 Medium |
| 4 | MarkFailed TOCTOU race | Single atomic SQL update | 🟢 Small |
| 5 | Permanent errors still retry | Force dead_letter for permanent | 🟢 Small |
| 6 | Incomplete file.Read | Use `io.ReadFull` | 🟢 Trivial |
| 7 | S3 uses `context.Background()` | Add ctx to Store interface | 🟢 Small |
| 8 | reflect.IsNil anti-pattern | Validate at construction | 🟢 Small |
| 9 | Error message leaks | Sanitize client-facing errors | 🟢 Small |
| 10 | Worker exits on transient errors | Add retry with backoff | 🟡 Medium |
| 11 | Deprecated gRPC API | Migrate to `grpc.NewClient` | 🟢 Small |
| 12 | DB pool unconfigured | Set pool limits | 🟢 Trivial |
| 13 | No body size limit on JSON | Add [MaxRequestSize](file:///Users/davidojo/Desktop/go-projects/google-images/internal/api/middleware.go#74-82) or `LimitReader` | 🟢 Trivial |
| 14 | ThumbnailURL not persisted | Add to Qdrant payload | 🟢 Small |
| 15-22 | Low severity items | Address in follow-up PRs | 🟢 Various |

> [!IMPORTANT]
> Issues #1–#6 are **blockers for production**. Issues #7–#14 are **strongly recommended** before launch. Issues #15–#22 can be tracked as tech debt.
