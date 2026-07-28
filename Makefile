GO ?= go
PLAYWRIGHT_CLI_VERSION ?= 0.1.17
CGO_ENABLED ?= 1

export CGO_ENABLED

.PHONY: build build-archive ci eval-netflix-matcher fmt fmt-check lint run smoke-archive test test-browser

build:
	$(GO) build -o build/download-your-data .

build-archive:
	mkdir -p build
	$(GO) build -trimpath -o build/download-your-data-archive ./cmd/archive

run:
	$(GO) run .

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './build/*')

fmt-check:
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './build/*'))" || \
		{ echo "Go files require formatting; run make fmt"; exit 1; }

lint:
	$(GO) vet ./...
	$(GO) tool staticcheck ./...
	$(GO) tool ineffassign ./...

test:
	$(GO) test ./...

eval-netflix-matcher:
	$(GO) test ./internal/providers/netflix -run '^TestMatcherEvaluationGate$$' -count=1 -v

test-browser:
	PLAYWRIGHT_CLI_VERSION=$(PLAYWRIGHT_CLI_VERSION) ./scripts/browser-smoke.sh

smoke-archive: build-archive
	./scripts/archive-smoke.sh ./build/download-your-data-archive

ci: fmt-check lint eval-netflix-matcher test test-browser smoke-archive
