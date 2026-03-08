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
