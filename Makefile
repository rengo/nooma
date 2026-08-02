# Local development targets.
#
# Two entry points, and the difference matters:
#
#   make check      the fast loop — lint, L1/L2, build. Seconds. Run it constantly.
#   make check-all  every gate CI blocks on that a Makefile CAN run locally.
#                   Slower. Run it before opening a PR.
#
# `check` is deliberately NOT full CI parity: it skips L3 (a real SQLite vault),
# the schema-golden regeneration-diff check, the coverage floor, the
# seven-target cross-compile matrix and L4 (which compiles the real binary),
# all of which cost real time. What it must never do is *claim* parity — an
# earlier version of this comment did, and a review caught it. If you add a
# blocking CI job, add it to `check-all` too — unless it needs PR metadata a
# Makefile cannot produce (see the note below).
#
# `cross-compile` and `test-e2e` joined `check-all` when ADR-0013 moved both
# onto the `pull_request` trigger: they became blocking gates, and the rule above
# applies to them literally — neither needs PR metadata.
#
# One CI gate `check-all` deliberately cannot cover: docs-sync.yml's
# docs<->code sync check. It decides on pull request metadata (the base
# branch, the PR's label list) that only exists once a PR is open on GitHub —
# a local `make` run has neither. Its classification logic is still testable
# locally: scripts/docs-sync.sh takes a changed-file list and a labels JSON
# string as plain inputs, covered by test/harness/docs_sync_test.go.
# "Every gate CI blocks on" therefore means every gate below — `check-all`
# does not claim docs-sync, and CLAUDE.md's Workflow section says so too.

GOLANGCI_LINT_VERSION := v2.12.2
GOBIN := $(shell go env GOPATH)/bin

# The version stamped into the binary, derived from the tag rather than written
# in a file (ADR-0015): an untagged tree reports `dev`, and a tagged one cannot
# disagree with its tag because there is no constant to forget to bump.
#
# Between tags `git describe` reports v0.1.0-7-gabc1234 — correct, and
# deliberately not pretty: that is a development build, not a release. The
# fallback matters, because `git describe --tags` fails outright when no tag
# exists yet, which is every tree until M1.
VERSION := $(shell git describe --tags --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.DEFAULT_GOAL := check

.PHONY: check
check: lint test build ## The fast loop: lint + L1/L2 tests + build — NOT full CI parity, see check-all

.PHONY: check-all
check-all: check test-integration schema-golden-clean cover cross-compile test-e2e ## Every gate CI blocks on that a Makefile can run locally (docs-sync excluded — see header) — run before opening a PR

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

.PHONY: schema-golden-clean
schema-golden-clean: schema-golden ## Fail if regenerating the schema golden leaves a dirty tree — mirrors the second half of ci.yml's integration job
	git diff --exit-code -- testdata/schema

.PHONY: build
build: ## Compile every package
	go build -ldflags "$(LDFLAGS)" ./...

.PHONY: binary
binary: ## Build ./nooma with the version stamped in — the only target that writes an artifact
	go build -ldflags "$(LDFLAGS)" -o nooma ./cmd/nooma

# The seven targets of ADR-0013, which supersedes ADR-0001's acceptance
# criterion 5. Build-only: this proves the code compiles for a platform, never
# that it behaves there — platform behavior needs a test that names the
# platform (see main.yml's cross-compile comment for a bug a matrix like this
# compiled without complaint).
CROSS_TARGETS := linux/amd64 linux/arm64 linux/arm darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

.PHONY: cross-compile
cross-compile: ## ADR-0013's seven GOOS/GOARCH pairs — mirrors main.yml's matrix
	@set -e; for t in $(CROSS_TARGETS); do \
		goos=$${t%/*}; goarch=$${t#*/}; \
		printf '%-16s' "$$t"; \
		CGO_ENABLED=0 GOOS=$$goos GOARCH=$$goarch go build -ldflags "$(LDFLAGS)" -o /dev/null ./... && echo OK; \
	done

.PHONY: cover
cover: ## Enforce docs/06-harness.md §3's >=90% floor on internal/core — armed, currently vacuous
	sh scripts/core-coverage.sh

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
