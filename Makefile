GO ?= go
PLAYWRIGHT_CLI_VERSION ?= 0.1.17
CGO_ENABLED ?= 1

export CGO_ENABLED

.PHONY: build build-chatindex ci fmt fmt-check lint run smoke-chatindex test test-browser validate-instruction-screenshots

build:
	$(GO) build -o build/download-your-data .

build-chatindex:
	mkdir -p build
	$(GO) build -trimpath -o build/chatindex ./cmd/chatindex

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

test-browser:
	PLAYWRIGHT_CLI_VERSION=$(PLAYWRIGHT_CLI_VERSION) ./scripts/browser-smoke.sh

validate-instruction-screenshots:
	$(GO) test . -run '^TestInstructionScreenshotContract$$' -count=1

smoke-chatindex: build-chatindex
	./scripts/chatindex-smoke.sh ./build/chatindex

ci: fmt-check lint test validate-instruction-screenshots test-browser smoke-chatindex
