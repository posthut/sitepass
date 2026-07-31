.PHONY: build test verify tidy web

VERSION := $(shell cat VERSION)

build:
	mkdir -p "$(CURDIR)/.cache/go-build" "$(CURDIR)/.cache/go-tmp"
	CGO_ENABLED=0 GOCACHE="$(CURDIR)/.cache/go-build" GOTMPDIR="$(CURDIR)/.cache/go-tmp" \
		go build -trimpath -ldflags "-s -w" -o bin/sitepass ./cmd/sitepass

web:
	cd web && npm ci && npm run build

test:
	mkdir -p "$(CURDIR)/.cache/go-build" "$(CURDIR)/.cache/go-tmp"
	CGO_ENABLED=0 GOCACHE="$(CURDIR)/.cache/go-build" GOTMPDIR="$(CURDIR)/.cache/go-tmp" \
		go test ./...

tidy:
	go mod tidy

verify:
	./deploy/verify.sh
