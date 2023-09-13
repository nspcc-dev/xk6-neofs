#!/usr/bin/make -f

export GOBIN ?= $(shell pwd)/bin
BINARY=xk6-neofs
XK6_VERSION=0.9.2

# Build xk6-neofs binary
build: install_xk6
	@echo "=> Building binary"
	@$(GOBIN)/xk6 build --with github.com/nspcc-dev/xk6-neofs=. --output $(BINARY)

# Install required utils
install_xk6:
	@echo "=> Installing utils"
	@go install go.k6.io/xk6/cmd/xk6@v$(XK6_VERSION)

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
