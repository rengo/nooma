# Local development targets.
#
# These run exactly what CI runs. Local/CI drift is how a green machine and a red
# laptop start disagreeing about whether the code is fine.

GOLANGCI_LINT_VERSION := v2.12.2
GOBIN := $(shell go env GOPATH)/bin

.DEFAULT_GOAL := check

.PHONY: check
check: lint test build ## Run everything CI runs

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
cover: ## Coverage of the cognitive core only — see docs/06-harness.md §3
	go test -coverprofile=coverage.out -coverpkg=./internal/core/... ./internal/core/...
	go tool cover -func=coverage.out | tail -1

.PHONY: store-api-golden
store-api-golden: ## Regenerate testdata/schema/store_api.golden — the exported-API golden (design §7.3, §9.2)
	go test ./test/conformance/ -run TestHarness_StoreAPIUnchanged -update

.PHONY: tools
tools: $(GOBIN)/golangci-lint ## Install pinned development tools

$(GOBIN)/golangci-lint:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

.PHONY: help
help: ## List available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'
