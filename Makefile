.DEFAULT_GOAL := help

.PHONY: help
help:
	@grep -hE '^[a-z][a-zA-Z0-9_.-]*:' $(MAKEFILE_LIST) | cut -d: -f1 | sort -u | sed 's/^/  make /'

.PHONY: build
build:
	go build ./...

.PHONY: test
test:
	go test ./...

.PHONY: test-race
test-race:
	go test -race ./...

.PHONY: cover
cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o cover.html
	@echo "открой cover.html"

.PHONY: vet
vet:
	go vet ./...

.PHONY: fmt
fmt:
	gofmt -w .

.PHONY: lint
lint:
	golangci-lint run

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: check
check: fmt vet test
