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
# (from the VERSION file) and a checksums.txt.
release: clean
	@mkdir -p $(DIST)
	@set -e; for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		name=helm-tui_$(VERSION)_$${os}_$${arch}; \
		mkdir -p $(DIST)/$$name; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" \
			-o $(DIST)/$$name/helm-tui ./cmd/helm-tui; \
		cp README.md LICENSE $(DIST)/$$name/ 2>/dev/null || cp README.md $(DIST)/$$name/; \
		tar -C $(DIST) -czf $(DIST)/$$name.tar.gz $$name; \
		rm -rf $(DIST)/$$name; \
	done
	@cd $(DIST) && shasum -a 256 *.tar.gz > checksums.txt && cat checksums.txt

clean:
	rm -rf $(DIST)
