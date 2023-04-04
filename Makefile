MAKEFLAGS += --silent
GOLANGCI_LINT_VERSION = $(shell head -n 1 .golangci.yml | tr -d '\# ')
TMPDIR ?= /tmp

all: clean lint test build

## help: Prints a list of available build targets.
help:
	echo "Usage: make <OPTIONS> ... <TARGETS>"
	echo ""
	echo "Available targets are:"
	echo ''
	sed -n 's/^##//p' ${PWD}/Makefile | column -t -s ':' | sed -e 's/^/ /'
	echo
	echo "Targets run by default are: `sed -n 's/^all: //p' ./Makefile | sed -e 's/ /, /g' | sed -e 's/\(.*\), /\1, and /'`"


## build: Builds a custom 'k6' with the local extension. 
build:
	xk6 build --with $(shell go list -m)=.

## grpc-server-run: Runs the gRPC server example.
grpc-server-run:
	go run -mod=mod examples/grpc_server/*.go

## test: Executes any tests.
test:
	go test -race -timeout 60s ./...

## lint: Runs the linters.
lint:
	golangci-lint run --out-format=tab ./...

## check: Runs the linters and tests.
check: lint test

## clean: Removes any previously created build artifacts.
clean:
	rm -f ./k6

.PHONY: test clean help lint check grpc-server-run build