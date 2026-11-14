# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: 2026 COR Holdings, LLC

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/RunOutcome/laforge/cli.Version=$(VERSION)
PREFIX  ?= $(HOME)/.local/bin

.PHONY: build web test install dev clean image notices release-check release-snapshot

build:                      ## bin/la, with the committed web client embedded
	go build -ldflags "$(LDFLAGS)" -o bin/la ./cmd/la

web: notices                ## rebuild the web client into forge/dist
	cd web && npm ci && npm run build

notices:                    ## regenerate THIRD_PARTY_NOTICES.md (and the web copy)
	node scripts/notices.mjs

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

IMAGE ?= laforge:local

image:                      ## the container image (agent CLIs bundled); run with `la up --container`
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE) .

GORELEASER ?= go run github.com/goreleaser/goreleaser/v2@v2.15.2

release-check:              ## validate .goreleaser.yaml
	$(GORELEASER) check

release-snapshot:           ## every release archive, unsigned and unpublished, into dist/
	$(GORELEASER) release --snapshot --clean --skip=publish,sign,sbom

clean:
	rm -rf bin dist
