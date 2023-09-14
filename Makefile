#!/usr/bin/make -f

BINARY=xk6-neofs
XK6_VERSION=0.9.2
VERSION ?= $(shell git describe --tags --match "v*" --abbrev=8 2>/dev/null | sed -r 's,^v([0-9]+\.[0-9]+)\.([0-9]+)(-.*)?$$,\1 \2 \3,' | while read mm patch suffix; do if [ -z "$$suffix" ]; then echo $$mm.$$patch; else patch=`expr $$patch + 1`; echo $$mm.$${patch}-pre$$suffix; fi; done)
LDFLAGS:=-s -w -X 'go.k6.io/k6/lib/consts.VersionDetails=xk6-neofs-$(VERSION)'

# Install required utils
install_xk6:
	@echo "=> Installing utils"
	@go install go.k6.io/xk6/cmd/xk6@v$(XK6_VERSION)

# Build xk6-neofs binary
build:
	@echo "=> Building binary"
	@XK6_BUILD_FLAGS="-ldflags '$(LDFLAGS)'" xk6 build --with github.com/nspcc-dev/xk6-neofs=. --output $(BINARY)

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
