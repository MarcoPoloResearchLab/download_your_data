GO ?= go
PLAYWRIGHT_CLI_VERSION ?= 0.1.17
TYPESCRIPT_VERSION ?= 5.9.2
CGO_ENABLED ?= 1
RELEASE_TOOL_DIR := $(abspath $(CURDIR)/scripts/release)
override PAGES_URL := https://dyd.mprlab.com/

export CGO_ENABLED

.PHONY: build check-frontend ci deploy deploy-dry-run down eval-netflix-matcher fmt fmt-check lint pages-deploy publish publish-release release release-artifacts smoke-command smoke-netflix-command test test-browser test-local-lifecycle test-release-artifact test-release-workflow up validate-instruction-screenshots validate-provider-icons

build:
	@mkdir -p build
	@$(GO) build -o build/download-your-data .

up: build
	@./scripts/local-server.sh up "$(abspath build/download-your-data)"

down:
	@./scripts/local-server.sh down "$(abspath build/download-your-data)"

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

test-local-lifecycle: build
	./scripts/test-local-lifecycle.sh ./scripts/local-server.sh "$(abspath build/download-your-data)"

eval-netflix-matcher:
	$(GO) test ./internal/providers/netflix -run '^TestMatcherEvaluationGate$$' -count=1 -v

test-browser: build
	PLAYWRIGHT_CLI_VERSION=$(PLAYWRIGHT_CLI_VERSION) ./scripts/browser-smoke.sh ./build/download-your-data
	DOWNLOAD_YOUR_DATA_RUN_BROWSER_CONTRACT=1 \
		PLAYWRIGHT_CLI_VERSION=$(PLAYWRIGHT_CLI_VERSION) \
		$(GO) test . -run '^TestNetflixBrowserWorkspaceContract$$' -count=1

validate-instruction-screenshots:
	$(GO) test . -run '^TestInstructionScreenshotContract$$' -count=1

validate-provider-icons:
	$(GO) test . -run '^TestProviderIconContract$$' -count=1

smoke-command: build
	./scripts/command-smoke.sh ./build/download-your-data

smoke-netflix-command:
	$(GO) test . -run '^TestNetflixOperatorCommandSmokeCoversInspectEnrichmentCacheAndExport$$' -count=1 -v

test-release-workflow:
	./scripts/release/test_release_workflow.sh

test-release-artifact:
	PLAYWRIGHT_CLI_VERSION=$(PLAYWRIGHT_CLI_VERSION) ./scripts/release/test_application_artifact.sh

release:
	@"$(RELEASE_TOOL_DIR)/prepare_release.sh"

release-artifacts:
	@python3 "$(RELEASE_TOOL_DIR)/prepare_artifacts.py"

publish-release:
	@"$(RELEASE_TOOL_DIR)/publish_release.sh"

publish: publish-release

pages-deploy:
	@"$(RELEASE_TOOL_DIR)/deploy_pages_artifact.sh" --branch gh-pages --url "$(PAGES_URL)"

deploy: pages-deploy

deploy-dry-run: test-release-workflow test-release-artifact

ci: fmt-check lint check-frontend eval-netflix-matcher test test-local-lifecycle smoke-netflix-command validate-instruction-screenshots validate-provider-icons test-browser smoke-command test-release-workflow test-release-artifact
