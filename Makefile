GO ?= go
BINARY ?= bin/chaintrail

.PHONY: fmt fmt-check vet test race cover bench build check clean

fmt:
	$(GO)fmt -w .

fmt-check:
	@diff="$$(gofmt -d .)"; test -z "$$diff" || { printf '%s\n' "$$diff"; exit 1; }

vet:
	$(GO) vet ./...

test:
	$(GO) test -shuffle=on ./...

race:
	$(GO) test -race -timeout=60s ./...

cover:
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out

bench:
	$(GO) test -run='^$$' -bench='Benchmark(CanonicalJSON|Verify1000Records)$$' -benchmem -benchtime=100ms ./...

build:
	mkdir -p $$(dirname $(BINARY))
	$(GO) build -trimpath -o $(BINARY) ./cmd/chaintrail

check: fmt-check vet test race build

clean:
	rm -rf bin coverage.out
