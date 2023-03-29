GOLANGCI_LINT_VERSION = $(shell head -n 1 .golangci.yml | tr -d '\# ')
TMPDIR ?= /tmp

build: ## Build the binary
	xk6 build --with github.com/grafana/xk6-grpc=.

grpc-server-run: ## Run the demo gRPC server
	go run -mod=mod examples/grpc_server/*.go

test:
	go test -race -timeout 30s ./...

lint:
	golangci-lint run --out-format=tab ./...

check: lint test

.PHONY: test lint check