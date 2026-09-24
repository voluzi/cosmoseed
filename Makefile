VERSION ?= $(shell git describe --tags --abbrev=0 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD)
override BUILDDIR := $(CURDIR)/build
LOCALBIN ?= $(CURDIR)/bin
GOLANGCI_LINT_VERSION ?= v2.13.2
GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint-$(GOLANGCI_LINT_VERSION)
HELM_VERSION ?= v4.2.4
HELM ?= $(LOCALBIN)/helm-$(HELM_VERSION)
OS_NAME := $(shell uname -s | tr A-Z a-z)
# SHA-256 digests from the pinned archives' official get.helm.sh checksum files.
HELM_SHA256_v4.2.4_darwin_amd64 := 6c163d687ca03c3b5c01928e53bbbcf9518278f47ce7a2f249a5a08e8bdaa2bc
HELM_SHA256_v4.2.4_darwin_arm64 := d747eb4e28bd2727173d15b759fa0a17822291ec09db7ced3d55af290a3661a2
HELM_SHA256_v4.2.4_linux_amd64 := c306b46f719b0a4da32d0f78ee21bf90ce8d602f15b22ab753f0674d1670a7f3
HELM_SHA256_v4.2.4_linux_arm64 := 564de2191b881e9f71b5606b25345821ea1682f06ab90499d3ab22b530176da1
HOST_ARCH ?= $(shell uname -m)
ifneq ($(filter x86_64 amd64,$(HOST_ARCH)),)
ARCH_NAME := amd64
else ifneq ($(filter arm64 aarch64,$(HOST_ARCH)),)
ARCH_NAME := arm64
else
ARCH_NAME := unsupported
endif
HELM_SHA256 := $(HELM_SHA256_$(HELM_VERSION)_$(OS_NAME)_$(ARCH_NAME))

SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec
.DELETE_ON_ERROR:

.PHONY: all help dev fmt vet lint test test.race build run clean golangci-lint helm helm-platform-check chart.lint chart.template
all: build

##@ General
help: ## Show available targets.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make <target>\n"} /^[a-zA-Z0-9_.-]+:.*?##/ {printf "  %-20s %s\n", $$1, $$2} /^##@/ {printf "\n%s\n", substr($$0, 5)}' $(MAKEFILE_LIST)

##@ Development
dev: build ## Build and run locally.
	@$(BUILDDIR)/cosmoseed
fmt: ## Format Go source.
	@find . -path './vendor' -prune -o -type f -name '*.go' -exec gofmt -w {} +
vet: ## Run go vet.
	@go vet ./...
lint: golangci-lint ## Run the pinned linter.
	@$(GOLANGCI_LINT) run ./...

##@ Tests
test: ## Run all tests without changing dependencies.
	@go test ./... -count=1
test.race: ## Run tests with the race detector.
	@go test -race ./... -count=1
chart.lint: helm ## Lint the Helm chart with a valid test chain.
	@$(HELM) lint helm/cosmoseed --set config.chainID=test-chain
chart.template: helm ## Render the Helm chart with a valid test chain.
	@$(HELM) template test helm/cosmoseed --set config.chainID=test-chain >/dev/null
	@bash helm/cosmoseed/tests/render.sh "$(HELM)"

##@ Build
$(BUILDDIR):
	@mkdir -p $(BUILDDIR)
build: | $(BUILDDIR) ## Build the binary.
	@CGO_ENABLED=0 go build -mod=readonly -ldflags="-s -w -X github.com/voluzi/cosmoseed/pkg/cosmoseed.Version=$(VERSION) -X github.com/voluzi/cosmoseed/pkg/cosmoseed.CommitHash=$(COMMIT)" -o $(BUILDDIR)/cosmoseed ./cmd/cosmoseed
run: build ## Run the built binary.
	@$(BUILDDIR)/cosmoseed
clean: ## Remove only the local build output.
	@rm -f "$(CURDIR)/build/cosmoseed"
	@rmdir "$(CURDIR)/build" 2>/dev/null || true

##@ Build Dependencies
$(LOCALBIN):
	@mkdir -p $(LOCALBIN)
golangci-lint: $(GOLANGCI_LINT) ## Install the pinned linter locally.
$(GOLANGCI_LINT): | $(LOCALBIN)
	@GOBIN=$(LOCALBIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@mv $(LOCALBIN)/golangci-lint $(GOLANGCI_LINT)
helm-platform-check:
	@if [[ "$(ARCH_NAME)" == unsupported ]]; then echo 'unsupported Helm architecture $(HOST_ARCH); expected x86_64/amd64 or arm64/aarch64' >&2; exit 1; fi
	@if [[ -z "$(HELM_SHA256)" ]]; then echo 'unsupported Helm artifact $(HELM_VERSION)-$(OS_NAME)-$(ARCH_NAME)' >&2; exit 1; fi
helm: helm-platform-check ## Install the pinned Helm binary locally.
	@$(MAKE) --no-print-directory $(HELM)
$(HELM): | $(LOCALBIN)
	@if [[ "$(ARCH_NAME)" == unsupported ]]; then echo 'unsupported Helm architecture $(HOST_ARCH); expected x86_64/amd64 or arm64/aarch64' >&2; exit 1; fi
	@if [[ -z "$(HELM_SHA256)" ]]; then echo 'unsupported Helm artifact $(HELM_VERSION)-$(OS_NAME)-$(ARCH_NAME)' >&2; exit 1; fi
	@url="https://get.helm.sh/helm-$(HELM_VERSION)-$(OS_NAME)-$(ARCH_NAME).tar.gz"; \
	archive=$$(mktemp); binary=$$(mktemp "$(LOCALBIN)/.helm.XXXXXX"); \
	trap 'rm -f "$$archive" "$$binary"' EXIT; \
	curl -fsSL "$$url" -o "$$archive"; \
	expected="$(HELM_SHA256)"; \
	if command -v sha256sum >/dev/null 2>&1; then actual=$$(sha256sum "$$archive" | awk '{print $$1}'); \
	else actual=$$(shasum -a 256 "$$archive" | awk '{print $$1}'); fi; \
	[[ "$$actual" == "$$expected" ]] || { echo 'Helm checksum mismatch' >&2; exit 1; }; \
	tar -xzOf "$$archive" $(OS_NAME)-$(ARCH_NAME)/helm > "$$binary"; \
	chmod +x "$$binary"; mv "$$binary" "$(HELM)"
