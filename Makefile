# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: 2026 COR Holdings, LLC

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/RunOutcome/laforge/cli.Version=$(VERSION)
PREFIX  ?= $(HOME)/.local/bin

.PHONY: build web test install dev clean

build:                      ## bin/la, with the committed web client embedded
	go build -ldflags "$(LDFLAGS)" -o bin/la ./cmd/la

web:                        ## rebuild the web client into forge/dist
	cd web && npm ci && npm run build

test:
	go test ./...
	cd web && npx vitest run

# A copy, not a link: the running forge keeps the binary it started with, so
# rebuilding never pulls it out from under live panes. `la down && la up` to
# move to the new build.
install: build
	mkdir -p $(PREFIX)
	cp bin/la $(PREFIX)/la-$(VERSION)
	ln -sfn la-$(VERSION) $(PREFIX)/la.tmp && mv -f $(PREFIX)/la.tmp $(PREFIX)/la
	@echo "installed $(PREFIX)/la -> la-$(VERSION)"

dev:                        ## the web client with hot reload, against a running forge
	cd web && npm run dev

clean:
	rm -rf bin
