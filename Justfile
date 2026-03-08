set dotenv-load := true

default:
  @just --list

build:
  go build ./...

test:
  go test ./...

dev:
  docker compose up --build

run-server:
  go run ./cmd/server

run-indexer mode input:
  go run ./cmd/indexer -mode {{mode}} -input {{input}}

index-demo:
  go run ./cmd/indexer -mode urls -input ./examples/demo-urls.txt
