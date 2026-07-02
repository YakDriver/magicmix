.PHONY: build test clean fmt vet tidy deps modern modern-check ci help

default: build

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## Build tool
	@go build ./...

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

modern-check: ## Check for modern Go code
	@echo "make: Checking for modern Go code..."
	@go run golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@latest -test ./...

modern: ## Fix modern Go code issues
	@echo "make: Fixing checks for modern Go code..."
	@go run golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@latest -fix -test ./...

ci: tidy build test vet modern-check ## Run all CI checks locally

clean: ## Clean build artifacts
	@rm -rf bin/ coverage.out coverage.html
