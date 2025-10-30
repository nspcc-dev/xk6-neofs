#!/usr/bin/make -f

export GOBIN ?= $(shell pwd)/bin
BINARY=xk6-neofs
XK6_VERSION=1.2.3
VERSION ?= $(shell git describe --tags --match "v*" --abbrev=8 2>/dev/null | sed -r 's,^v([0-9]+\.[0-9]+)\.([0-9]+)(-.*)?$$,\1 \2 \3,' | while read mm patch suffix; do if [ -z "$$suffix" ]; then echo $$mm.$$patch; else patch=`expr $$patch + 1`; echo $$mm.$${patch}-pre$$suffix; fi; done)
LDFLAGS:=-s -w -X 'go.k6.io/k6/lib/consts.VersionDetails=xk6-neofs-$(VERSION)'

# Build xk6-neofs binary
build: install_xk6
	@echo "=> Building binary"
	$(GOBIN)/xk6 build --build-flags="-ldflags=$(LDFLAGS)" -v --with github.com/nspcc-dev/xk6-neofs=. --output $(BINARY)

# Install required utils
install_xk6:
	@echo "=> Installing utils"
	@go install go.k6.io/xk6/cmd/xk6@v$(XK6_VERSION)

# Run tests
test:
	@go test ./... -cover

.golangci.yml:
	wget -O $@ https://github.com/nspcc-dev/.github/raw/master/.golangci.yml

# Run linters
lint: .golangci.yml
	@golangci-lint --timeout=5m run

# Reformat code
format:
	@echo "⇒ Processing gofmt check"
	@gofmt -s -w ./
	@echo "⇒ Processing goimports check"
	@goimports -w ./
