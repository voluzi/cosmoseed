VERSION ?= $(shell git describe --tags --abbrev=0 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD)
BUILDDIR ?= $(CURDIR)/build
LOCALBIN ?= $(CURDIR)/bin
GOLANGCI_LINT_VERSION ?= v2.13.2
GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint-$(GOLANGCI_LINT_VERSION)
HELM_VERSION ?= v4.2.4
HELM ?= $(LOCALBIN)/helm-$(HELM_VERSION)
OS_NAME := $(shell uname -s | tr A-Z a-z)
ifeq ($(shell uname -m),x86_64)
ARCH_NAME := amd64
else
ARCH_NAME := arm64
endif

SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec
.DELETE_ON_ERROR:

.PHONY: all help dev fmt vet lint test test.race build run clean golangci-lint helm chart.lint chart.template
all: build

##@ General
help: ## Show available targets.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make <target>\n"} /^[a-zA-Z0-9_.-]+:.*?##/ {printf "  %-20s %s\n", $$1, $$2} /^##@/ {printf "\n%s\n", substr($$0, 5)}' $(MAKEFILE_LIST)

##@ Development
dev: build ## Build and run locally.
	@$(BUILDDIR)/cosmoseed
fmt: ## Format Go source.
	@gofmt -w $$(rg --files -g '*.go')
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
	@$(HELM) lint charts/cosmoseed --set config.chainID=test-chain
chart.template: helm ## Render the Helm chart with a valid test chain.
	@$(HELM) template test charts/cosmoseed --set config.chainID=test-chain >/dev/null

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
helm: $(HELM) ## Install the pinned Helm binary locally.
$(HELM): | $(LOCALBIN)
	@curl -fsSL https://get.helm.sh/helm-$(HELM_VERSION)-$(OS_NAME)-$(ARCH_NAME).tar.gz | tar -xzOf - $(OS_NAME)-$(ARCH_NAME)/helm > $(HELM)
	@chmod +x $(HELM)
