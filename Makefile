VERSION ?= $(shell git describe --tags --abbrev=0)
COMMIT ?= $(shell git rev-parse HEAD)
BUILD_TARGETS := build install

all: install

build:
	CGO_ENABLED=0 go $@ \
		-o build/ \
		-mod=readonly \
		-ldflags="-s -w -X github.com/voluzi/cosmoseed/pkg/cosmoseed.Version=$(VERSION) -X github.com/voluzi/cosmoseed/pkg/cosmoseed.CommitHash=$(COMMIT)" \
		./cmd/cosmoseed

install:
	CGO_ENABLED=0 go $@ \
		-mod=readonly \
		-ldflags="-s -w -X github.com/voluzi/cosmoseed/pkg/cosmoseed.Version=$(VERSION) -X github.com/voluzi/cosmoseed/pkg/cosmoseed.CommitHash=$(COMMIT)" \
		./cmd/cosmoseed

mod:
	go mod tidy

test: mod
	go test ./...

clean:
	rm -rf $(BUILDDIR)/

.PHONY: all build install mod test clean