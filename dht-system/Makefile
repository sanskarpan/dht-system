.PHONY: build test run lint proto-gen clean frontend-build frontend-dev

BINARY_GATEWAY := bin/gateway
PROTO_DIR      := proto
PROTO_GEN_DIR  := proto/gen
GO_FILES       := $(shell find . -name '*.go' -not -path './vendor/*')

## build: compile the gateway binary
build:
	go build -o $(BINARY_GATEWAY) ./cmd/gateway/...

## test: run all Go tests
test:
	go test -race -timeout 60s ./internal/...

## test-verbose: run tests with verbose output
test-verbose:
	go test -v -race -timeout 60s ./internal/...

## run: build and run the gateway server
run: build
	./$(BINARY_GATEWAY)

## lint: run golangci-lint
lint:
	golangci-lint run ./...

## proto-gen: generate Go code from .proto files
proto-gen:
	mkdir -p $(PROTO_GEN_DIR)
	protoc \
		--go_out=$(PROTO_GEN_DIR) \
		--go_opt=paths=source_relative \
		--go-grpc_out=$(PROTO_GEN_DIR) \
		--go-grpc_opt=paths=source_relative \
		-I $(PROTO_DIR) \
		$(PROTO_DIR)/*.proto

## frontend-build: build the React frontend
frontend-build:
	cd frontend && npm run build

## frontend-dev: run frontend dev server
frontend-dev:
	cd frontend && npm run dev

## clean: remove build artifacts
clean:
	rm -rf bin/ $(PROTO_GEN_DIR)/ frontend/dist/

## deps: install/update Go dependencies
deps:
	go mod tidy
	go mod download

## vet: run go vet
vet:
	go vet ./...

## help: show this help
help:
	@grep -E '^## ' Makefile | sed 's/## //'
