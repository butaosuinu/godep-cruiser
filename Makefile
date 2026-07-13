GO      ?= go
GO_BIN  ?= godep-cruiser

# GOCACHE / GOLANGCI_LINT_CACHE live under the repo-local .cache/ (gitignored)
# so builds and lints never touch a global cache and are removed with the tree.
GOCACHE             ?= $(CURDIR)/.cache/go-build
GOLANGCI_LINT_CACHE ?= $(CURDIR)/.cache/golangci-lint

# .golangci-lint-version is the single source for the pinned golangci-lint
# version; CI (golangci-lint-action in .github/workflows/test.yml) reads the
# same file via `version-file`.
GOLANGCI_LINT_VERSION ?= $(shell cat .golangci-lint-version)
GOLANGCI_LINT_BIN     := $(CURDIR)/.cache/tools/golangci-lint-$(GOLANGCI_LINT_VERSION)

.PHONY: build test lint check fmt vuln clean

# `make build` — compile the CLI binary (gitignored).
# `make test`  — run Go unit tests.
# `make lint`  — pinned golangci-lint v2 (.golangci.yml).
# `make check` — run the required CI gates in order: test, then lint.
# `make fmt`   — gofumpt/goimports formatting via `golangci-lint fmt`.
# `make vuln`  — govulncheck via the go.mod tool directive. Kept out of `lint`
#                on purpose: it fetches the vulnerability DB over the network
#                and can fail on new CVEs without any code change, so it is not
#                a deterministic lint gate.
# `make clean` — remove the built binary.

build:
	GOCACHE="$(GOCACHE)" $(GO) build -o "$(GO_BIN)" .

test:
	GOCACHE="$(GOCACHE)" $(GO) test ./...

# Binary install because upstream does not guarantee `go install`; the URL is
# pinned to the release tag.
$(GOLANGCI_LINT_BIN):
	@mkdir -p "$(dir $@)"
	curl -sSfL "https://raw.githubusercontent.com/golangci/golangci-lint/$(GOLANGCI_LINT_VERSION)/install.sh" \
		| sh -s -- -b "$(dir $@)" "$(GOLANGCI_LINT_VERSION)"
	@mv "$(dir $@)golangci-lint" "$@"

lint: $(GOLANGCI_LINT_BIN)
	GOCACHE="$(GOCACHE)" GOLANGCI_LINT_CACHE="$(GOLANGCI_LINT_CACHE)" "$(GOLANGCI_LINT_BIN)" run

check:
	$(MAKE) test
	$(MAKE) lint

fmt: $(GOLANGCI_LINT_BIN)
	GOCACHE="$(GOCACHE)" GOLANGCI_LINT_CACHE="$(GOLANGCI_LINT_CACHE)" "$(GOLANGCI_LINT_BIN)" fmt

vuln:
	GOCACHE="$(GOCACHE)" $(GO) tool govulncheck ./...

clean:
	rm -f "$(GO_BIN)"
