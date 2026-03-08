set dotenv-load := true

default:
  @just --list

build:
  @mkdir -p bin
  go build -o bin/server ./cmd/server
  go build -o bin/indexer ./cmd/indexer
  go build -o bin/worker ./cmd/worker

test:
  go test ./...

dev:
  docker compose -f infra/docker-compose.yml up --build

clean:
  rm -rf bin/
  go clean

run-server:
  go run ./cmd/server

run-indexer mode input:
  go run ./cmd/indexer -mode {{mode}} -input {{input}}

index-demo:
  go run ./cmd/indexer -mode urls -input ./examples/demo-urls.txt

run-worker:
  go run ./cmd/worker

run-worker-indexer:
  go run ./cmd/worker -seed-url-file ./examples/demo-urls.txt

run-worker-postgres:
  JOB_BACKEND=postgres go run ./cmd/worker -seed-url-file ./examples/demo-urls.txt
