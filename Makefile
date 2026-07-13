GO         ?= go
GO_BIN     ?= $(shell $(GO) env GOPATH)/bin
GO_TOOLS   ?= $(shell $(GO) tool | grep /)

VERSION    ?= $(shell git describe --tags 2>/dev/null || echo 0.0.0-dev)
BUILDTIME  ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GO_LDFLAGS ?= -s -w \
	-X github.com/gechr/clive.version=$(VERSION) \
	-X github.com/gechr/clive.buildTime=$(BUILDTIME)

DIST_DIR   ?= dist
DOCS_DIR   ?= docs

.PHONY: all
all: fmt lint test

.PHONY: build
build:
	@$(GO) build -ldflags "$(GO_LDFLAGS)" -o $(DIST_DIR)/clover .

.PHONY: fmt
fmt:
	@$(MAKE) --no-print-directory -C $(DOCS_DIR) fmt
	@clover format
	@rumdl fmt --quiet
	@$(GO) fix ./...
	@$(GO) tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint fmt --enable=gci,golines,gofumpt

.PHONY: gen
gen:
	@$(GO) generate ./...

.PHONY: install
install:
	@$(GO) install -ldflags "$(GO_LDFLAGS)" .
	@$(GO_BIN)/clover --install-completion

.PHONY: lint
lint:
ifndef CI
	@zizmor --persona=pedantic --min-severity=medium .github/
endif
	@$(GO) tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint run

.PHONY: test
test:
	@$(GO) test -timeout 30s -race ./...

.PHONY: update
update:
	@clover run
	@$(GO) get $(GO_TOOLS) $(shell $(GO) list -f '{{if not (or .Main .Indirect)}}{{.Path}}{{end}}' -m all)
	@$(GO) mod tidy
