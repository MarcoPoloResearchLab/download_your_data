GO ?= go
PLAYWRIGHT_CLI_VERSION ?= 0.1.17
TYPESCRIPT_VERSION ?= 5.9.2
CGO_ENABLED ?= 1

export CGO_ENABLED

.PHONY: build check-frontend ci clean deploy down eval-netflix-matcher fmt fmt-check lint publish release test test-browser test-local-lifecycle test-production-artifacts up validate-instruction-screenshots validate-provider-icons

build:
	@mkdir -p build
	@$(GO) build -o build/download-your-data ./cmd/download-your-data

clean:
	@./scripts/clean-generated.sh "$(CURDIR)"

up: build
	@./scripts/local-server.sh up "$(abspath build/download-your-data)"

down:
	@./scripts/local-server.sh down "$(abspath build/download-your-data)"

fmt:
	gofmt -w $$(find . -name '*.go' \
		-not -path './build/*' \
		-not -path './.tidy-folder-snapshots/*')

fmt-check:
	@test -z "$$(gofmt -l $$(find . -name '*.go' \
		-not -path './build/*' \
		-not -path './.tidy-folder-snapshots/*'))" || \
		{ echo "Go files require formatting; run make fmt"; exit 1; }

lint:
	$(GO) vet ./...
	$(GO) tool staticcheck ./...
	$(GO) tool ineffassign ./...

check-frontend:
	npx --yes --package "typescript@$(TYPESCRIPT_VERSION)" tsc \
		--allowJs --checkJs --noEmit --target ES2023 --module ES2022 \
		--moduleResolution bundler --lib ES2023,DOM \
		frontend/application/auth-lifecycle.js \
		frontend/application/app.js \
		frontend/application/api.js \
		frontend/application/charts.js \
		frontend/application/dom.js \
		frontend/application/provider-links.js \
		frontend/application/routing.js
	node --check scripts/browser-smoke.playwright.js
	node --check scripts/netflix-browser-workspace.playwright.js

test:
	$(GO) test ./...

test-local-lifecycle: build
	./scripts/test-local-lifecycle.sh ./scripts/local-server.sh "$(abspath build/download-your-data)"

eval-netflix-matcher:
	$(GO) test ./internal/providers/netflix -run '^TestMatcherEvaluationGate$$' -count=1 -v

test-browser: build
	PLAYWRIGHT_CLI_VERSION=$(PLAYWRIGHT_CLI_VERSION) ./scripts/browser-smoke.sh ./build/download-your-data
	DOWNLOAD_YOUR_DATA_RUN_BROWSER_CONTRACT=1 \
		PLAYWRIGHT_CLI_VERSION=$(PLAYWRIGHT_CLI_VERSION) \
		$(GO) test ./internal/httpapi -run '^TestNetflixBrowserWorkspaceContract$$' -count=1

test-production-artifacts:
	./scripts/test-production-artifacts.sh

validate-instruction-screenshots:
	$(GO) test ./frontend -run '^TestInstructionScreenshotContract$$' -count=1

validate-provider-icons:
	$(GO) test ./frontend -run '^TestProviderIconContract$$' -count=1

release publish deploy:
	@application_root="$$(git rev-parse --show-toplevel)"; \
	gateway_root="$$(dirname "$${application_root}")/mprlab-gateway"; \
	if [ ! -d "$${gateway_root}" ]; then \
		printf "required sibling gateway is missing: %s; clone mprlab-gateway at exactly %s\n" \
			"$${gateway_root}" "$${gateway_root}" >&2; \
		exit 2; \
	fi; \
	$(MAKE) --no-print-directory -C "$${gateway_root}" "app-$@" \
		MPRLAB_APP_ROOT="$${application_root}"

ci: fmt-check lint check-frontend eval-netflix-matcher test test-local-lifecycle validate-instruction-screenshots validate-provider-icons test-production-artifacts test-browser
