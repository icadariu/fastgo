.DEFAULT_GOAL := help

help: ## show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*?## ' '{printf "  %-20s %s\n", $$1, $$2}'

BRANCH    := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo dev)
ifeq ($(BRANCH),main)
VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
else
VERSION   := $(shell git describe --tags --always 2>/dev/null || echo dev)-dev
endif
COMMIT    := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILDTIME := $(shell date -u +%d-%m-%y_%H:%M)
LDFLAGS   := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILDTIME)"

MAIN := ./cmd/fastgo

install: ## build and install with version metadata
	go install $(LDFLAGS) $(MAIN)
