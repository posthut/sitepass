.PHONY: build test verify tidy web

VERSION := $(shell cat VERSION)

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/sitepass ./cmd/sitepass

web:
	cd web && npm run build

test:
	CGO_ENABLED=0 go test ./...

tidy:
	go mod tidy

verify:
	@echo "smoke test placeholder — implement against a running instance"
	@exit 1
