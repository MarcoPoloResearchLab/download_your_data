GO ?= go
PLAYWRIGHT_CLI_VERSION ?= 0.1.17
TYPESCRIPT_VERSION ?= 5.9.2
CGO_ENABLED ?= 1

export CGO_ENABLED

.PHONY: build build-archive check-frontend ci eval-netflix-matcher fmt fmt-check lint run smoke-archive test test-browser validate-instruction-screenshots

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

check-frontend:
	npx --yes --package "typescript@$(TYPESCRIPT_VERSION)" tsc \
		--allowJs --checkJs --noEmit --target ES2023 --module ES2022 \
		--moduleResolution bundler --lib ES2023,DOM app.js api.js charts.js
	node --check scripts/browser-smoke.playwright.js
	node --check scripts/netflix-browser-workspace.playwright.js

test:
	$(GO) test ./...

eval-netflix-matcher:
	$(GO) test ./internal/providers/netflix -run '^TestMatcherEvaluationGate$$' -count=1 -v

test-browser:
	PLAYWRIGHT_CLI_VERSION=$(PLAYWRIGHT_CLI_VERSION) ./scripts/browser-smoke.sh
	DOWNLOAD_YOUR_DATA_RUN_BROWSER_CONTRACT=1 \
		PLAYWRIGHT_CLI_VERSION=$(PLAYWRIGHT_CLI_VERSION) \
		$(GO) test . -run '^TestNetflixBrowserWorkspaceContract$$' -count=1

validate-instruction-screenshots:
	$(GO) test . -run '^TestInstructionScreenshotContract$$' -count=1

smoke-archive: build-archive
	./scripts/archive-smoke.sh ./build/download-your-data-archive

ci: fmt-check lint check-frontend eval-netflix-matcher test validate-instruction-screenshots test-browser smoke-archive
