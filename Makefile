VERSION := $(shell cat VERSION)
LDFLAGS := -s -w -X main.version=$(VERSION)
PLATFORMS := darwin/arm64 linux/arm64 linux/amd64
DIST := dist

.PHONY: build test vet release clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/helm-tui ./cmd/helm-tui

test:
	go test ./...

vet:
	go vet ./...

# Cross-compiles all release archives into dist/ with a shared version
# (from the VERSION file) and a checksums.txt. For a signed and notarized
# darwin binary set SIGN_IDENTITY and NOTARY_PROFILE (see scripts/release.sh).
release:
	./scripts/release.sh

clean:
	rm -rf $(DIST)
