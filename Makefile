# make dist: the release binaries, cross-compiled into build/bin (not dist/,
# which holds the bundled fonts, and not bin/, which the install step reads).
# The release workflow passes VERSION=<tag without v>; a local build describes
# the checkout, or keeps internal/bootstrap's Version when there is no tag.
DIST_DIR := build/bin
DIST_PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64
VERSION ?= $(shell git describe --tags --dirty 2>/dev/null | sed 's/^v//')

.PHONY: build dist test race lint golden tidy

build:
	go build -trimpath -o bin/herdr-scope ./cmd/herdr-scope

dist:
	rm -rf $(DIST_DIR)
	mkdir -p $(DIST_DIR)
	for p in $(DIST_PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=; \
		if [ $$os = windows ]; then ext=.exe; fi; \
		out=$(DIST_DIR)/herdr-scope-$$os-$$arch$$ext; \
		echo "$$out"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath \
			-ldflags "-s -w -X main.version=$(VERSION)" -o $$out ./cmd/herdr-scope || exit 1; \
	done

test:
	go test -count=1 ./...

race:
	go test -race -count=1 ./...

lint:
	golangci-lint run --config .golangci.yaml ./...
	GOOS=windows golangci-lint run --config .golangci.yaml ./...

# Rewrite tests/golden after an intended change to the look.
golden:
	go test ./tests/golden -update -count=1

tidy:
	go mod tidy
