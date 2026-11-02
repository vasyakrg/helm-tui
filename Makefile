.PHONY: build test vet

build:
	go build -o bin/helm-tui ./cmd/helm-tui

test:
	go test ./...

vet:
	go vet ./...
