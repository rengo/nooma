# Local development targets.
#
# Two entry points, and the difference matters:
#
#   make check      the fast loop — lint, L1/L2, build. Seconds. Run it constantly.
#   make check-all  every gate CI blocks on. Slower. Run it before opening a PR.
#
# `check` is deliberately NOT full CI parity: it skips L3 (a real SQLite vault),
# the pending-red gate and the coverage floor, all of which cost real time. What
# it must never do is *claim* parity — an earlier version of this comment did,
# and a review caught it. If you add a blocking CI job, add it to `check-all`.

GOLANGCI_LINT_VERSION := v2.12.2
GOBIN := $(shell go env GOPATH)/bin

.DEFAULT_GOAL := check

.PHONY: check
check: lint test build ## The fast loop: lint + L1/L2 tests + build — NOT full CI parity, see check-all

.PHONY: check-all
check-all: check test-integration pending-red cover ## Every gate CI blocks on — run before opening a PR

.PHONY: lint
lint: $(GOBIN)/golangci-lint ## Dependency rule + clock port + standard linters
	$(GOBIN)/golangci-lint run
	go vet ./...

.PHONY: test
test: ## L1 (pure) + L2 (conformance)
	go test -race -shuffle=on ./...

.PHONY: test-integration
test-integration: ## L3 — a real temporary SQLite vault
	go test -race -shuffle=on -tags integration ./internal/store/sqlite/... ./test/integration/...

.PHONY: test-e2e
test-e2e: ## L4 — the compiled binary
	go test -tags e2e ./test/e2e/...

.PHONY: schema-golden
schema-golden: ## Regenerate testdata/schema/{structure,ddl}.golden from the embedded migrations
	go test -tags integration ./test/integration/ -run TestSchemaGolden -update

.PHONY: build
build: ## Compile every package
	go build ./...

.PHONY: cover
cover: ## Enforce docs/06-harness.md §3's >=90% floor on internal/core — armed, currently vacuous
	sh scripts/core-coverage.sh

.PHONY: store-api-golden
store-api-golden: ## Regenerate testdata/schema/store_api.golden — the exported-API golden (design §7.3, §9.2)
	go test ./test/conformance/ -run TestHarness_StoreAPIUnchanged -update

.PHONY: pending-red
pending-red: ## docs/06-harness.md §8 point 5 — test/conformance's pendingimpl tests must fail to compile, for the expected reason
	sh scripts/pending-red.sh

.PHONY: tools
tools: $(GOBIN)/golangci-lint ## Install pinned development tools

$(GOBIN)/golangci-lint:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

.PHONY: help
help: ## List available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'
