set dotenv-load := true

default:
  @just --list

build:
  @mkdir -p bin
  go build -o bin/server ./cmd/server
  go build -o bin/indexer ./cmd/indexer
  go build -o bin/worker ./cmd/worker

proto:
  protoc --proto_path=proto --go_out=. --go_opt=module=iris --go-grpc_out=. --go-grpc_opt=module=iris proto/clip/v1/clip.proto
  PYTHONPATH=./.tmp-grpc-tools python3 -m grpc_tools.protoc --proto_path=proto --python_out=clip_service --grpc_python_out=clip_service proto/clip/v1/clip.proto

test:
  go test ./...

templ:
  go run github.com/a-h/templ/cmd/templ@v0.3.1001 generate

templ-watch:
  go run github.com/a-h/templ/cmd/templ@v0.3.1001 generate -watch

dev:
  docker compose -f infra/docker-compose.yml up --build

shutdown:
  docker compose -f infra/docker-compose.yml down

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
