GO         ?= go
GO_BIN     ?= $(shell $(GO) env GOPATH)/bin
GO_TOOLS   ?= $(shell $(GO) list tool)

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
	@$(GO) tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint run --fix -c .golangci.ruleguard.yml

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

# Resolve one real resource per provider against its actual upstream, checking
# that each still parses a response it has only ever been shown a fixture of.
# Deliberately outside `all`: it needs the network, spends real rate limit, and
# fails when an upstream is down.
.PHONY: smoke
smoke:
	@$(GO) test -tags=live -count=1 -timeout 300s -run 'TestLive' -v ./internal/provider/all/

.PHONY: update
update:
	@clover run
	@$(GO) get $(GO_TOOLS) $(shell $(GO) list -f '{{if not (or .Main .Indirect)}}{{.Path}}{{end}}' -m all)
	@$(GO) mod tidy
