.DEFAULT_GOAL := help

# ESPHome release the vendored api.proto was taken from. Bump with `make proto`.
ESPHOME_VERSION ?= $(shell cat proto/ESPHOME_VERSION 2>/dev/null || echo "unknown")
PROTO_SRC := https://raw.githubusercontent.com/esphome/esphome

BUILD_DIR := bin

##@ Development

.PHONY: build
build: ## Build all packages
	go build ./...

.PHONY: simdevice
simdevice: ## Build the simulated device into ./bin
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/simdevice ./cmd/simdevice

.PHONY: sim
sim: ## Run the simulated device so Home Assistant can discover it
	go run ./cmd/simdevice $(ARGS)

.PHONY: test
test: ## Run tests
	go test ./...

.PHONY: test-race
test-race: ## Run tests with the race detector
	go test -race ./...

.PHONY: cover
cover: ## Run tests and open a coverage report
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

.PHONY: fmt
fmt: ## Format Go source
	go fmt ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint
	@if command -v golangci-lint >/dev/null; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed, skipping"; \
	fi

.PHONY: tidy
tidy: ## Tidy go.mod / go.sum
	go mod tidy

.PHONY: check
check: fmt vet lint test ## Format, vet, lint and test

##@ Protocol

.PHONY: proto-fetch
proto-fetch: ## Fetch api.proto from upstream ESPHome (make proto-fetch REF=2026.7.0)
	@test -n "$(REF)" || (echo "usage: make proto-fetch REF=<esphome tag or branch>"; exit 1)
	curl -fsSL $(PROTO_SRC)/$(REF)/esphome/components/api/api.proto -o proto/api.proto
	curl -fsSL $(PROTO_SRC)/$(REF)/esphome/components/api/api_options.proto -o proto/api_options.proto
	@printf '%s\n' "$(REF)" > proto/ESPHOME_VERSION
	@echo "fetched api.proto at $(REF)"

.PHONY: proto-gen
proto-gen: ## Generate Go bindings from proto/
	protoc --proto_path=proto --go_out=api --go_opt=paths=source_relative \
		proto/api.proto proto/api_options.proto

.PHONY: proto
proto: proto-fetch proto-gen ## Fetch and regenerate (make proto REF=2026.7.0)

.PHONY: proto-version
proto-version: ## Show which ESPHome release the bindings track
	@echo $(ESPHOME_VERSION)

##@ Help

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
