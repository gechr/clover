GO         ?= go
GO_BIN     ?= $(shell $(GO) env GOPATH)/bin
GO_TOOLS   ?= $(shell $(GO) tool | grep /)

VERSION    ?= $(shell git describe --tags 2>/dev/null || echo 0.0.0-dev)
GO_LDFLAGS ?= -s -w -X main.version=$(VERSION)

DIST_DIR   ?= dist
DOCS_DIR   ?= docs

.PHONY: all
all: fmt lint test

.PHONY: build
build:
	@$(GO) build -ldflags "$(GO_LDFLAGS)" -o $(DIST_DIR)/cusp ./cmd/cusp

.PHONY: fmt
fmt:
	@$(MAKE) --no-print-directory -C $(DOCS_DIR) fmt
	@rumdl fmt --quiet
	@$(GO) fix ./...
	@$(GO) tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint fmt --enable=gci,golines,gofumpt
	@$(GO) tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint run --fix --enable-only tagalign

.PHONY: install
install:
	@$(GO) install -ldflags "$(GO_LDFLAGS)" ./cmd/cusp
	@$(GO_BIN)/cusp --install-completion

.PHONY: lint
lint:
	@$(GO) tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint run

.PHONY: test
test:
	@$(GO) test -timeout 30s -race ./...

.PHONY: update
update:
	@$(GO) get $(GO_TOOLS) $(shell $(GO) list -f '{{if not (or .Main .Indirect)}}{{.Path}}{{end}}' -m all)
	@$(GO) mod tidy
