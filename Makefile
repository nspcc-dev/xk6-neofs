#!/usr/bin/make -f

# Run tests
test:
	@go test ./... -cover

# Run linters
lint:
	@golangci-lint --timeout=5m run

# Reformat code
format:
	@echo "⇒ Processing gofmt check"
	@gofmt -s -w ./
	@echo "⇒ Processing goimports check"
	@goimports -w ./
