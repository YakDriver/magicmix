.PHONY: build install test clean fmt vet tidy deps lint modern modern-check ci snapshot help

default: build

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## Build tool
	@go build ./...

install: ## Install the magicmix binary to $GOBIN
	@go install ./cmd/magicmix

test: ## Run tests
	@go test ./...

vet: ## Run go vet
	@go vet ./...

fmt: ## Format code
	@go fmt ./...

tidy: ## Tidy go.mod
	@go mod tidy

deps: ## Download dependencies
	@go mod download

lint: ## Run golangci-lint (includes the unused linter for dead code)
	@golangci-lint run

modern-check: ## Check for modern Go code
	@echo "make: Checking for modern Go code..."
	@go run golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@latest -test ./...

modern: ## Fix modern Go code issues
	@echo "make: Fixing checks for modern Go code..."
	@go run golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@latest -fix -test ./...

ci: tidy build test vet modern-check ## Run all CI checks locally

snapshot: ## Build a local release snapshot into dist/ (no publish)
	@go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean --skip=publish

clean: ## Clean build artifacts
	@rm -rf bin/ dist/ coverage.out coverage.html
