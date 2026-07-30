# ISSUES ARCHIVE

Completed non-recurring issue history archived during backlog hygiene passes.

## BugFixes

- [x] [B003] (P1) Pair local startup with an ownership-safe shutdown target
  Goal:
  Make the repository-owned local orchestration started by `make up` stoppable through the paired `make down` target.

  Requirements:
  - Keep `make up` attached to its terminal while recording the exact server process identity beneath the checkout's Git state.
  - Keep `up` and `down` as phony operator targets without echoing internal build or launcher commands.
  - Make `make down` idempotent when no managed server is running.
  - Refuse to signal a reused, malformed, or unrelated process identity.
  - Keep custom address, data-root, inference, and TMDB environment configuration attached to the server process.

  Deliverables:
  - Managed local server lifecycle script and paired Make targets.
  - Public run documentation for same-terminal and second-terminal shutdown.
  - Black-box coverage for start, health, duplicate start, stop, repeated stop, and unrelated-process rejection.

  Validation:
  - `make test-local-lifecycle`
  - `make ci`

- [x] [B004] (P1) Replace text-only provider instructions with per-step visual guides
  Goal:
  Make every provider action directly understandable by pairing each instruction step with an approved first-party screenshot.

  Requirements:
  - Replace the localized string-step and provider-gallery shape with one canonical typed step containing text, screenshot identity, and localized alternative text.
  - Cover every step for every provider, including the Netflix workspace and guide-only providers; no text-only exception, empty screenshot set, gallery, placeholder, mock, or third-party tutorial may survive.
  - Render each screenshot inside its numbered step at wide and narrow web viewport widths.
  - Reuse approved assets across locales and across providers only when they genuinely share the same first-party surface, such as Threads and Instagram Accounts Center.
  - Preserve privacy-safe stop boundaries for credentials, identity verification, account/profile selection, export submission, and download.
  - Record authenticated provider captures and first-party help captures accurately in the manifest without presenting a help surface as an authenticated app screen.

  Deliverables:
  - Strict per-step frontend data and runtime validation.
  - Approved OpenAI, Netflix, WhatsApp, and TikTok visual assets plus the existing provider captures.
  - Updated manifest, capture runbook, localized accessibility text, contract tests, and real-browser coverage.

  Validation:
  - Contract tests reject an absent, unknown, empty, orphaned, or provider-gallery screenshot contract.
  - Browser coverage proves every rendered step has exactly one local screenshot and localized alternative text.
  - `make validate-instruction-screenshots`
  - `make test-browser`
  - `make ci`

- [x] [B005] (P1) {B004,F008} Keep the Netflix visual guide permanently reachable
  Goal:
  Make the Netflix download walkthrough available independently of the workspace's current data state.

  Requirements:
  - Expose `#guide/netflix` as the canonical visual walkthrough using the same localized step and screenshot contract as every other provider.
  - Make the complete Netflix catalog card open its guide and keep Data analysis as the separate workspace action.
  - Keep a View guide action in the Netflix workspace header across empty, building, ready, failure, and replacement states.
  - Preserve Netflix as the sole workspace-capable provider without duplicating backend state in guide content.

  Deliverables:
  - Permanent Netflix guide route and bidirectional guide/workspace navigation.
  - Wide and narrow web browser coverage across every locale and a ready workspace.

  Validation:
  - `make check-frontend`
  - `make test-browser`
  - `make ci`

- [x] [B006] (P1) {B004,B005,F009} Use web platform terminology consistently
  Goal:
  Describe provider workflows by their actual platform instead of a device class.

  Requirements:
  - Use web wording in every localized provider instruction that refers to a browser-based workflow.
  - Rename capture-manifest fields and surface identities to the sole current web contract.
  - Describe responsive validation by wide and narrow viewport behavior.
  - Remove the obsolete label case-insensitively from every tracked file and path.

  Deliverables:
  - Updated English, Spanish, French, and Russian provider instructions.
  - Updated screenshot manifest, validation, planning, and capture documentation.

  Validation:
  - Case-insensitive tracked-content and tracked-path audits return no obsolete label.
  - `make validate-instruction-screenshots`
  - `make test-browser`
  - `make ci`

- [x] [B007] (P1) {B004,F009} Remove duplicate guide controls from provider cards
  Goal:
  Keep each guide-only catalog action clear, singular, and actionable.

  Superseded metadata contract:
  B017 removes the remaining Guide word from every provider card while preserving the full-card destination.

  Requirements:
  - Render exactly one full-card guide link in every provider card without a separate guide button.
  - Keep the compact Guide label as metadata rather than a duplicate action.
  - Remove generic Guide badges from guide headings while preserving real Netflix workspace-state chips.
  - Prove the singular action contract at wide and narrow web viewport widths.

  Deliverables:
  - Simplified catalog and guide-heading rendering.
  - Browser regression coverage for all guide-only providers and locales.

  Validation:
  - `make check-frontend`
  - `make test-browser`
  - `make ci`

- [x] [B008] (P1) {B003} Make concurrent local-server cleanup idempotent
  Goal:
  Keep the required CI lifecycle gate deterministic when shutdown and wrapper cleanup converge.

  Requirements:
  - Read the two-line ownership identity through one file descriptor.
  - Treat a state file removed by the other owner-cleanup path as already cleaned.
  - Preserve exact process-ID and process-start matching before removing any remaining state.

  Deliverables:
  - Race-safe ownership-state cleanup in the canonical local lifecycle script.

  Validation:
  - `make test-local-lifecycle`
  - `make ci`

- [x] [B009] (P1) {B004} Make every provider instruction directly actionable
  Goal:
  Give every numbered instruction its exact first-party destination instead of asking the user to locate a provider surface themselves.

  Requirements:
  - Bind each approved screenshot asset to the manifest's exact first-party direct route.
  - Render one visible external link inside every numbered instruction step in every locale.
  - Reject missing, non-HTTPS, credential-bearing, non-first-party, or manifest-mismatched routes.
  - Preserve one screenshot and one instruction action per step at wide and narrow web viewport widths.

  Deliverables:
  - Canonical screenshot, action-link, and direct-route registry contract.
  - Runtime validation, contract tests, capture documentation, and real-browser coverage.

  Validation:
  - `make validate-instruction-screenshots`
  - `make test-browser`
  - `make ci`

- [x] [B010] (P1) {B008} Isolate browser validation from unrelated local servers
  Goal:
  Keep the browser gate deterministic without terminating or colliding with another repository's loopback listener.

  Requirements:
  - Allocate an available loopback port for the default browser-smoke server.
  - Preserve the explicit browser-test address as the sole caller-controlled override.
  - Never stop or reuse an unrelated process that happens to own the former fixed test port.

  Deliverables:
  - Collision-safe browser-smoke server startup.

  Validation:
  - `make test-browser`
  - `make ci`

- [x] [B011] (P1) {F008,F009} Replace imaginary provider glyphs with first-party icons
  Goal:
  Make every catalog identity immediately recognizable through its current product mark instead of a letter, text abbreviation, or invented symbol.

  Requirements:
  - Ship one reviewed first-party favicon or launcher icon for every canonical provider.
  - Keep provider icon files local to the application; rendering the catalog must not request a provider or third-party asset host.
  - Record the exact official site, source URL, review date, dimensions, digest, and local output path for every icon.
  - Remove the hard-coded glyph map and require the canonical local icon path in the provider registry.
  - Preserve legibility and containment at wide and narrow web viewport widths.

  Deliverables:
  - Eleven normalized provider icon assets and a strict provenance manifest.
  - Runtime validation, contract tests, and real-browser coverage.

  Validation:
  - `make validate-provider-icons`
  - `make test-browser`
  - `make ci`

- [x] [B012] (P1) {F008,F009} Replace app-owned chrome with the shared MPR UI shell
  Goal:
  Use the current MPR Lab header and footer contract without changing this application's local-only, unauthenticated product boundary.

  Requirements:
  - Render the shell through declarative `mpr-header` and `mpr-footer` custom elements.
  - Load both shared assets from the literal `mpr-ui@latest` jsDelivr contract.
  - Preserve the brand, route context, Credits action, language selector, theme action, network links, and local-data disclosure through supported attributes, slots, and custom properties.
  - Do not fabricate TAuth tenant data, `/config-ui.yaml`, a config loader, or a sign-in control for a product that has no authentication surface.
  - Keep provider icons, guide screenshots, charts, API traffic, and personal-data requests local; permit only the two exact shared-shell asset requests.
  - Narrow the CSP to jsDelivr for scripts and styles. Permit inline styles only because the current shared custom elements inject their component style sheets.

  Deliverables:
  - Shared header and footer integration with the retired local chrome removed.
  - Localized Credits disclosure, documentation, CSP contract, and real-browser network coverage.

  Validation:
  - `make check-frontend`
  - `make test-browser`
  - `make ci`

- [x] [B013] (P1) {B011,B012,F008,F009} Replace provider rows with large-logo tile cards
  Goal:
  Make the provider catalog visually scannable as a tiled product chooser with each reviewed brand mark as the card's dominant identity.

  Superseded contract:
  B014 replaces the tall card, framed 80-pixel mark, and separate View guide control with the current compact linked-card interaction.

  Requirements:
  - Replace the row-specific catalog markup and styles with one canonical provider-card grid.
  - Render three columns at the wide application width, two columns at intermediate widths, and one column on a narrow web viewport.
  - Display every reviewed local provider logo as a prominent, legible product identity.
  - Keep the localized provider name, surface type, full summary, and actionable controls visible in every card.
  - Preserve one canonical guide destination per provider and the separate workspace control for Netflix.
  - Keep cards compact, flat, bordered, responsive, keyboard-operable, and free of horizontal overflow.

  Deliverables:
  - Semantic provider-card rendering and responsive MPR tile-grid styles.
  - Updated real-browser assertions for grid shape, large local logos, summaries, actions, and narrow containment.

  Validation:
  - `make check-frontend`
  - `make test-browser`
  - `make ci`

- [x] [B014] (P1) {B004,B011,B013,F008} Make compact provider cards open their guides directly
  Goal:
  Make each provider card a compact, immediately actionable guide entry while keeping real data-analysis applications as distinct secondary actions.

  Superseded OpenAI boundary:
  B016 replaces the guide-only OpenAI constraint by projecting the already-incorporated private archive and retrieval engine into a browser workspace. OpenAI ZIP upload and replacement remain separate lifecycle work.

  Requirements:
  - Keep the three-, two-, and one-column catalog grid, but place the provider copy to the right of a 56-pixel reviewed local product logo.
  - Remove the catalog logo frame, padding, and background without altering the reviewed image asset.
  - Make the complete card surface one native, keyboard-operable link to the provider's canonical guide route.
  - Remove every View guide button from the catalog.
  - Render one localized Data analysis button only for providers that declare a current browser workspace route.
  - Keep the Data analysis control above the card link so it opens the provider application while every other card location opens the guide.
  - Treat Netflix as the current browser-workspace provider. Do not fabricate an OpenAI browser route while F001 and F002 remain incomplete; the existing OpenAI operator analysis commands are not a browser application.
  - Preserve provider summaries, focus visibility, wide and narrow containment, and the shared MPR shell.

  Deliverables:
  - Compact guide-linked provider cards with unframed logos and one distinct Netflix Data analysis action.
  - Localized action copy and real-browser assertions for card geometry, pointer and keyboard routing, application-action isolation, and responsive containment.

  Validation:
  - `make check-frontend`
  - `make test-browser`
  - `make ci`

- [x] [B015] (P1) {B014,F008} Replace the catalog state pill with the analysis action
  Goal:
  Put the useful Netflix application action in the card's highest-priority secondary position instead of repeating transient workspace state.

  Requirements:
  - Place the localized Data analysis button at the top right of the Netflix card beside the Guide metadata.
  - Remove the Netflix state pill and every other provider-state chip from the catalog.
  - Keep backend-owned Netflix state inside the workspace where it has operational context.
  - Preserve the full-card guide link beneath the higher stacking analysis control.
  - Prove top-right alignment, application routing, guide routing, localization, focus operation, and narrow containment in a real browser.

  Deliverables:
  - One top-right Netflix Data analysis action with no catalog state pill.
  - Updated responsive and browser contracts.

  Validation:
  - `make check-frontend`
  - `make test-browser`
  - `make ci`

- [x] [B016] (P1) {B014,B015,I003,I005,I006} Expose incorporated OpenAI search from the catalog
  Goal:
  Give OpenAI the same top-right Data analysis action as Netflix and open a real browser workspace backed by the incorporated private conversation engine.

  Requirements:
  - Declare OpenAI as workspace-capable while preserving the full-card OpenAI guide link beneath the distinct analysis action.
  - Open `#provider/openai` and report the actual archive and complete ready-index state from the canonical private data root.
  - When no archive or ready index exists, show the exact current import and indexing commands without fabricating browser upload.
  - Search the ready archive through one validated POST contract supporting hybrid, semantic, and lexical modes, bounded results and excerpts, and the archive filter.
  - Reuse the current retrieval engine, index identity, inference boundary, and query cache; do not introduce a second search implementation.
  - Never write query text, conversation content, or returned excerpts to logs or browser persistence.
  - Keep the action and workspace localized, keyboard-operable, compact, and contained at wide and narrow browser widths.

  Deliverables:
  - Top-right OpenAI Data analysis catalog action and private search workspace.
  - Validated OpenAI provider snapshot and search HTTP contracts.
  - Real-browser coverage for action isolation, guide routing, workspace preparation, localization, and responsive containment.

  Validation:
  - `make check-frontend`
  - Focused OpenAI HTTP contract tests with a synthetic archive and deterministic inference.
  - `make test-browser`
  - `make ci`

- [x] [B017] (P1) {B007,B014,B015,B016} Remove Guide metadata from provider cards
  Goal:
  Remove the redundant Guide word from every catalog card while preserving the card's destination and useful application action.

  Requirements:
  - Render no visible Guide metadata in any provider card or locale.
  - Keep the complete card surface as the provider's native keyboard-operable guide link with the provider name as its accessible label.
  - Render a metadata row only when it contains the top-right Data analysis action.
  - Remove obsolete Guide-label styling instead of retaining an empty or hidden element.
  - Preserve compact wide and narrow card geometry, provider summaries, focus visibility, and action isolation.

  Deliverables:
  - Provider cards with no Guide label and no empty metadata row.
  - Updated real-browser assertions for markup, routing, actions, and responsive containment.

  Validation:
  - `make check-frontend`
  - `make test-browser`
  - `make ci`

- [x] [B018] (P1) {B004,F009} Match every Meta export action to a first-party visual
  Goal:
  Replace the reused export-entry images that did not show later Facebook, Instagram, or Threads actions.

  Requirements:
  - Use current public first-party help captures for profile selection, device export, options, submission, availability, and protected retrieval without crossing an authenticated export boundary.
  - Keep each locale on the same canonical per-step screenshot mapping.
  - Fail the screenshot contract if the Meta mappings regress to the landing-panel images.

  Deliverables:
  - Privacy-reviewed Facebook, Instagram, and Threads help captures with exact manifest provenance.
  - Updated localized mappings and real-browser coverage.

  Validation:
  - `make validate-instruction-screenshots`
  - `make test-browser`
  - `make ci`

- [x] [B019] (P1) {B016} Require the current inference identity for OpenAI readiness
  Goal:
  Prevent the browser from advertising search readiness for an index built against another inference base URL.

  Requirements:
  - Select only a complete ready index whose persisted base URL matches the current validated inference configuration.
  - Report `index_required` consistently from both the provider snapshot and search endpoint when no compatible index exists.

  Deliverables:
  - One canonical compatible-index selection path and an HTTP regression scenario.

  Validation:
  - Focused OpenAI HTTP contract tests.
  - `make ci`

- [x] [B020] (P1) {B005,B011} Restore the Netflix workspace provider icon
  Goal:
  Render the canonical Netflix brand asset in the workspace header instead of an empty mark.

  Requirements:
  - Resolve the icon from the provider registry.
  - Decode and verify the shipped image through the real browser entry point.

  Deliverables:
  - Netflix workspace icon rendering and browser coverage in every locale.

  Validation:
  - `make check-frontend`
  - `make test-browser`
  - `make ci`

- [x] [B021] (P1) {B016} Contain unbroken OpenAI search result text
  Goal:
  Keep imported titles and excerpts inside the narrow OpenAI workspace.

  Requirements:
  - Permit arbitrary unbroken title and excerpt text to wrap.
  - Prove containment with a ready search response at the narrow browser viewport.

  Deliverables:
  - Result-text containment styles and a real-browser regression scenario.

  Validation:
  - `make check-frontend`
  - `make test-browser`
  - `make ci`

- [x] [B022] (P1) Restore self-contained zero-argument local startup
  Goal:
  Make the repository-owned `make up` command load its complete local runtime
  profile and report success only after the application is actually ready.

  Requirements:
  - Keep `make up` and `make down` argument-free as the sole public local
    lifecycle.
  - Load one ignored `configs/.env` as the sole local application and
    dependency profile without executing it as shell code or accepting
    inherited configuration overrides.
  - Require every authentication and ownership value, allow only current
    runtime keys, reject malformed, duplicate, unsupported, or empty entries,
    and enforce mode `0600`.
  - Initialize the one explicit signing-key bootstrap marker atomically, start
    official `TAuth:latest` and `ghttp:latest` services, and expose the app,
    `/auth`, and `/me` through one `http://localhost:8080` front door.
  - Keep `make down` independent of application configuration.
  - Announce readiness only after the app health, anonymous TAuth session and
    profile statuses, browser config, and gateway are all verified; clean up
    process and container ownership when configuration or readiness fails.
  - Do not source a tracked example, inject a fake authentication profile, or
    weaken the backend's fail-closed runtime validation. Do not place the
    unused Google OAuth client secret in the app profile or repository.

  Deliverables:
  - Strict local environment loading and secure signing-key initialization in
    the ownership-safe lifecycle.
  - Checkout-scoped local Compose and TAuth contracts with same-origin routing.
  - Black-box coverage for missing configuration, file authority, permissions,
    key initialization, dependency ordering, same-origin readiness, duplicate
    start, stop, repeated stop, and unrelated process rejection.
  - Updated local operator documentation and ignored-secret boundary.

  Validation:
  - `make test-local-lifecycle`
  - `make ci`

  Resolved 2026-07-30: `make up` now loads the ignored `configs/.env`, treats
  every value as inert data, enforces its current key set and mode `0600`,
  ignores inherited application configuration, and atomically replaces the
  explicit first-run marker with a private signing key. It starts the official
  latest TAuth and ghttp images plus the local binary, routes all browser auth
  through the authorized `http://localhost:8080` origin, and reports readiness
  only after app health, anonymous `/auth/session=204`, `/me=401`, and
  `/config-ui.yaml=200` checks pass. `make down` remains environment-file
  independent and preserves the checkout-scoped TAuth data volume. The supplied
  Google web client ID is retained only in the ignored profile; its unused
  client secret remains outside the repository under mode `0600`. Black-box
  lifecycle coverage, focused CSP coverage, literal real-container
  `make up`/`make down` acceptance, real-browser shared-shell and nonce
  acceptance, and `make ci` pass. The browser-discovered GIS stylesheet CSP
  violation was removed; the isolated browser had no Google account, so no
  authenticated session was fabricated or claimed.

## Improvements

- [x] [I001] (P1) Establish the canonical local server and validation foundation
  Goal:
  Replace the backendless Pages runtime with the first forward-only local application boundary.

  Requirements:
  - Serve the current frontend and a structured health endpoint from one Go process.
  - Bind only to a validated loopback address.
  - Establish repository-native formatting, lint, test, browser, and CI targets.
  - Exercise the shipped application through real HTTP and browser entry points.

  Deliverables:
  - Canonical Go application entry point and embedded frontend assets.
  - `make fmt`, `make lint`, `make test`, `make test-browser`, and `make ci`.
  - Current local-only setup documentation.

  Validation:
  - `make ci`
  - `git diff --check`
  - `git status --short`

- [x] [I002] (P1) Incorporate the target-owned conversation archive engine
  Goal:
  Move every maintained ChatIndex capability into this repository and end the separate-project boundary.

  Requirements:
  - Own export inspection, graph import, SQLite storage, embeddings, hybrid retrieval, definition analysis, reports, and their tests in this module.
  - Preserve the operator CLI as a target-owned entry point while the browser workflows are built.
  - Rewrite package ownership to the current module without a remote dependency, local path replacement, subprocess adapter, or compatibility bridge.
  - Carry the deterministic fixtures, fake inference server, smoke workflow, and architecture documentation into the current repository.
  - Leave the standalone `chatIndex` directory unchanged and abandoned after parity validation passes here.

  Deliverables:
  - Target-owned conversation archive packages and `cmd/chatindex`.
  - Repository-native ChatIndex build and smoke targets.
  - Passing package, CLI, smoke, and full CI validation.

  Validation:
  - `make test`
  - `make build-chatindex`
  - `make smoke-chatindex`
  - `make ci`

- [x] [I003] (P1) {I002} Normalize the incorporated engine to the forward-only product contract
  Goal:
  Remove copied project identity and persistence compatibility paths before new application APIs depend on them.

  Requirements:
  - Replace ChatIndex-specific product names, model aliases, errors, configuration keys, build targets, and durable documentation with `download_your_data`-owned terms.
  - Establish one first-release database schema for this product and delete copied schema-v1-through-v3 migration code and tests.
  - Reject databases outside the current schema with an actionable archive re-import instruction; do not add compatibility reads, automatic migrations, or aliases.
  - Use `DOWNLOAD_YOUR_DATA_INFERENCE_BASE_URL` as the only inference URL environment key.
  - Remove the old key instead of reading both names.

  Deliverables:
  - One current product-owned schema and inference identity.
  - Updated source, tests, commands, and architecture documentation with no active ChatIndex contract.
  - Explicit rejection coverage for non-current databases and configuration.

  Validation:
  - Repository search confirms active source and docs contain no `CHATINDEX_`, `ChatIndex`, or `chatindex` product contract.
  - `make ci`

- [x] [I005] (P1) {I003} Establish the secure local data and inference boundary
  Goal:
  Give every HTTP, storage, and inference operation one validated local runtime configuration.

  Requirements:
  - Validate `DOWNLOAD_YOUR_DATA_DATA_DIR`, `DOWNLOAD_YOUR_DATA_INFERENCE_BASE_URL`, and a closed inference-boundary value at process startup.
  - Default to loopback inference; require an explicit `authorized-remote` boundary for any non-loopback LM Studio URL and never accept an inference URL from an HTTP request.
  - Accept only normalized HTTP or HTTPS base URLs without credentials, query strings, or fragments.
  - Confine all generation, upload, database, vector, cache, and temporary paths beneath the configured data root.
  - Create private directories and files with owner-only permissions and reject unsafe roots or escaped paths.
  - Validate loopback `Host` and browser `Origin`, require a per-process CSRF token for mutations, and expose no wildcard CORS policy.
  - Remove unused raw export metadata from the canonical schema and keep logs limited to opaque IDs, counts, states, durations, and typed errors.
  - Expose a non-secret capabilities payload containing inference boundary, readiness, model identity, archive limits, and data-root readiness.

  Deliverables:
  - Smart-constructed runtime configuration shared by server, jobs, search, and operator commands.
  - Private generation-path abstraction and minimized current schema.
  - HTTP security middleware and capabilities endpoint.

  Validation:
  - Black-box startup tests reject invalid URLs, unsafe data roots, non-loopback inference without authorization, invalid hosts, invalid origins, and missing CSRF tokens.
  - Filesystem tests prove path confinement and owner-only permissions.
  - `make test`

- [x] [I006] (P1) {I003,P003} Consolidate operator workflows into the product command
  Goal:
  Ship one product-owned executable without retaining a transitional archive binary or alias.

  Requirements:
  - Make `download-your-data` own `serve`, `inspect`, `import`, `index`, `search`, and `definitions` entry points.
  - Delete `cmd/archive`, `build/download-your-data-archive`, the archive build and smoke targets, and transitional command documentation after command parity passes.
  - Keep definition analysis and reproducible reports as operator subcommands for the first release.
  - Share packages and validated runtime configuration with the browser backend; do not invoke one command from another as a subprocess.
  - Preserve one canonical command spelling only.

  Deliverables:
  - One application binary with server and operator subcommands.
  - Updated smoke workflow and operator documentation.
  - Deleted transitional command surfaces without compatibility aliases.

  Validation:
  - Black-box command smoke covers inspect, import, index, hybrid search, definitions, and serve.
  - Repository search finds no shipped second archive executable, command, or build target.
  - `make ci`

- [x] [I007] (P1) Incorporate the Netflix viewing-history domain
  Goal:
  Move the maintained local Netflix CSV and analytics capabilities into this repository without importing the standalone runtime boundary.

  Requirements:
  - Follow `docs/netflix-provider-plan.md` as the canonical incorporation contract.
  - Own validated viewing-activity rows, local dates, versioned title identities, deterministic aggregation, and enriched CSV shapes beneath the current module.
  - Replace the source parser's broad colon truncation, invalid-record fallbacks, and unchecked row indexing with typed edge validation.
  - Preserve raw titles and dates exactly while keeping derived title identity explicit and versioned.
  - Carry every maintained dashboard measure into deterministic target-owned aggregation with accurate activity terminology and match-coverage inputs.
  - Add synthetic fixtures for films, episodic titles, punctuation, localized text, ambiguous titles, invalid headers, short rows, invalid dates, duplicate columns, cancellation, and empty input.
  - Do not copy the source HTTP server, templates, CDN assets, duplicate CLI implementation, tracked SQLite cache, module namespace, or standalone filesystem path.
  - Leave `/Users/tyemirov/Development/netflix` unchanged and abandoned after target parity; do not add a Go module, path replacement, subprocess, or sidecar dependency.

  Deliverables:
  - Target-owned Netflix provider domain, CSV adapter, analytics, export shape, fixtures, and public-contract tests.
  - Capability disposition documentation tied to source revision `e4079718730533aa15141b567fa378def66b1265`.
  - No network dependency in this slice.

  Validation:
  - Black-box package or command tests prove valid import, exact rejection, cancellation, deterministic aggregation, and enriched CSV round-trip.
  - Repository search finds no active import or runtime reference to `github.com/tyemirov/netflix` or `/Users/tyemirov/Development/netflix`.
  - `make test`
  - `make ci`

- [x] [I008] (P1) {I005,I007} Establish the TMDB enrichment and matching boundary
  Goal:
  Enrich Netflix title identities through one privacy-explicit, server-owned TMDB client with measurable match quality.

  Requirements:
  - Use `DOWNLOAD_YOUR_DATA_TMDB_READ_TOKEN` as the sole server-side credential and report only a configured/not-configured capability to the browser.
  - Use Bearer authentication against the fixed official HTTPS API origin; allow endpoint injection only in tests.
  - Send only unique derived title queries after explicit user initiation; never send dates, source CSV bytes, profile data, or complete viewing rows.
  - Return closed `matched`, `review`, and `unmatched` outcomes with matcher identity and evidence.
  - Build a deterministic labeled corpus covering exact films and series, episodic titles, punctuation, localized titles, remakes, ambiguous popularity, unrelated negatives, and no-result cases.
  - Never accept an ambiguous or low-confidence result merely because it is popular.
  - Centralize bounded concurrency, response sizes, rate control, `Retry-After`, retry budget, cancellation, and contextual errors.
  - Persist cache entries only beneath the private data root with query, locale, client, matcher, and freshness identity; delete the cache with the provider.
  - Include the current TMDB attribution and approved-logo requirements in the product Credits contract.
  - Make no real TMDB request in tests or CI.

  Deliverables:
  - Injected official-boundary TMDB client, matcher, private cache, capability state, and evaluation command.
  - Deterministic fake-TMDB integration harness covering search, details, rate limits, malformed responses, cancellation, and outages.
  - Precision, review, and unmatched metrics recorded with every matcher evaluation.

  Validation:
  - Evaluation thresholds are recorded before accepted matches can become product metadata.
  - Fake-server tests prove the token is in the authorization header and absent from URLs, payloads, logs, and persisted provider data.
  - Incomplete remote work returns a typed failure rather than partial accepted metadata.
  - `make test`
  - `make ci`

- [x] [I009] (P1) {I006,I007,I008} Preserve Netflix operator parity in the single product executable
  Goal:
  Keep useful Netflix inspection, enrichment, and CSV export operations without shipping `tmdbenrich` or a second server.

  Requirements:
  - Add provider-scoped `netflix inspect`, `netflix enrich`, and `netflix export` operations to `download-your-data`.
  - Reuse the same validated provider packages, runtime configuration, match contract, cache, and export adapter as the browser backend.
  - Keep one canonical command spelling and delete copied Cobra, Viper, Zap, flag, worker, and cache configuration surfaces.
  - Preserve cancellation, bounded progress, contextual errors, and private-data logging rules.
  - Do not invoke the browser server, another command, the standalone executable, or the standalone checkout as a subprocess.

  Deliverables:
  - Product-owned Netflix operator commands and deterministic smoke workflow.
  - Updated operator documentation within the single-executable contract.
  - No target-owned `tmdbenrich` binary, alias, package, or build target.

  Validation:
  - Black-box smoke covers inspect, fake-TMDB enrichment, cache reuse, and enriched CSV export.
  - Repository search finds no shipped `tmdbenrich` command or standalone Netflix runtime reference.
  - `make ci`

- [x] [I010] (P1) {P004} Publish the authenticated web instruction screenshot set
  Goal:
  Capture and publish the current web export workflow so a user can follow each supported provider without the capture operator starting an export.

  Superseded contract:
  B004 and B018 replaced this initial 12-asset web baseline and its TikTok text-only exception with the current 26-asset, every-provider, one-visual-per-step contract.

  Requirements:
  - Cover Facebook, Instagram, LinkedIn, X, YouTube, and Google with two canonical English web screenshots per provider.
  - Use the operator's authenticated Chrome session and the current official provider routes.
  - Navigate only through instructional setup screens and stop before every archive request, export creation, download, destination connection, password entry, verification-code request, or account mutation.
  - Treat X's empty password-verification form as the second instructional boundary; do not enter credentials solely to reach a later screen.
  - Capture the smallest useful panel at a consistent wide browser viewport without browser chrome, credentials, names, handles, email addresses, avatars, organizations, account identifiers, notifications, or private counts.
  - Keep one 12-entry shared manifest and reuse the assets across `en`, `es`, `fr`, and `ru`; do not duplicate screenshots by locale.
  - Remove web placeholders when the complete 12-shot set is accepted. TikTok remains text-only until `I011` supplies its separate mobile set.

  Deliverables:
  - Twelve reviewed, metadata-free local assets beneath `frontend/images/instructions/`.
  - Current provider instructions, official references, capture runbook, and shared screenshot manifest.
  - Shared asset wiring with localized alternative text for all four locales.
  - Repository-native screenshot validation and browser coverage.

  Validation:
  - The manifest contains exactly 12 unique web screenshot IDs and two local assets for each supported web provider.
  - Every image is privacy-reviewed at full resolution and matches its recorded current live labels.
  - All four locales render the shared images without placeholders at wide and narrow application viewports.
  - `make test-browser`
  - `make ci`
  - `git diff --check`
  - `git status --short`

- [x] [I013] (P1) Publish the indexable data-export resource library
  Goal:
  Give searchers a useful, crawlable path from provider-specific data-export questions to the current anonymous guides and supported Netflix analysis workflow.

  Requirements:
  - Publish one path-based `/resources/` hub and one meaningfully distinct English resource for every current provider export workflow.
  - Publish a separate Netflix viewing-history analyzer resource grounded in the current per-profile CSV, private analytics, optional TMDB, export, replacement, and deletion contracts.
  - Keep ChatGPT browser import, full Netflix account-archive analysis, mandatory TMDB enrichment, customer proof, ranking claims, and invented production values out of public copy.
  - Derive canonical, Open Graph, JSON-LD, sitemap, robots, and internal-link URLs from the validated public origin instead of hard-coding the unresolved production host.
  - Use trailing-slash resource canonicals, crawlable root and related-resource links, visible authorship and review dates, repository evidence, bounded FAQs, and approved lazy-loaded screenshots.
  - Generate sitemap `<lastmod>` only from the explicit significant-content date recorded with each new resource.

  Deliverables:
  - Validated resource registry, reusable HTML rendering, resource styles, hub, provider resources, and Netflix analyzer resource.
  - Root metadata and crawlable Resources link, aligned `sitemap.xml` and `robots.txt`, and Article, CollectionPage, BreadcrumbList, and visible FAQ structured data.
  - Black-box HTTP and real-browser SEO coverage for canonical URLs, trailing-slash behavior, sitemap entries, structured data, public access, and responsive rendering.

  Validation:
  - `make check-frontend`
  - `make test`
  - `make test-browser`
  - `make ci`

  Resolved 2026-07-30: the public frontend now exposes a path-based resource
  hub, one grounded export resource for every current provider, and a distinct
  Netflix viewing-history analyzer resource. Canonical, Open Graph, JSON-LD,
  sitemap, robots, and internal URLs derive from the validated public origin;
  resource canonicals use trailing slashes with permanent slash redirects.
  The application footer, hub, and related-resource links provide a complete
  crawlable path. Registry validation, black-box HTTP coverage, structured-data
  parsing, sitemap-to-`200` checks, wide and narrow browser coverage, and
  `make ci` passed. No production profile was invented and no release,
  publication, deployment, Search Console request, or live indexing validation
  was performed.

## Maintenance

- [x] [M410] (P1) {M404R} Add the canonical release, publication, and deployment lifecycle
  Goal:
  Give the local-only product the same fixed repository-owned lifecycle as other MPR applications without introducing a hosted personal-data service.

  Superseded contract:
  P006 and the current F005 replace this completed first-release lifecycle with the forward authenticated web release. This entry remains historical evidence of the prior release boundary.

  Requirements:
  - Make `make up` the sole local development entrypoint and remove the obsolete `make run` target and documentation.
  - Make `make release` run the complete CI gate, build a deterministic macOS arm64 executable with embedded version and browser assets, package first-run guidance and the license, produce a checksum, and seal every payload without a remote write.
  - Make `make publish` publish the exact sealed commit, tag, manifest, application archive, checksum, and Pages archive without rebuilding.
  - Make user-owned `make deploy` activate only the published static download and documentation page through branch-based GitHub Pages.
  - Keep the product server loopback-only. Do not deploy an API, personal-data service, container, TAuth tenant, application authentication flow, runtime secret, or browser-side deployment configuration.
  - Own the complete gateway discovery and deployment contract beneath `.mprlab/deploy/`.

  Deliverables:
  - Fixed `make up`, `make release`, `make publish`, `make deploy`, and non-publishing `make deploy-dry-run` targets.
  - Reproducible application and Pages artifacts sealed beneath `.git/mprlab-release`.
  - App-owned `.mprlab/deploy/resources.yml`, exact production profile, and static Pages source.
  - Black-box release, repeat-release, publish-plan, local Pages deployment, extracted-application command, and extracted-application browser coverage.

  Validation:
  - `make deploy-dry-run`
  - `make ci`
  - Gateway resource discovery and plan validation against an isolated canonical checkout.
  - `git diff --check`
  - `git status --short`

  Resolved 2026-07-29: the repository now owns fixed `make up`, `make release`, `make publish`, and user-owned `make deploy` entrypoints. Release builds and packages the macOS arm64 application twice with an isolated Go build cache, seals its checksum and first-run guidance, and creates only the local release commit and tag; publish uploads that exact sealed release without rebuilding; deploy verifies the published manifest and tag, replaces `gh-pages`, configures branch publishing with the sealed `dyd.mprlab.com` CNAME, waits for the GitHub Pages certificate, enforces HTTPS, and verifies the source marker. Read-only production inspection confirmed `dyd.mprlab.com` points directly to `marcopoloresearchlab.github.io` and the repository is in the expected pre-first-deploy Pages API 404 state. `make deploy-dry-run`, `make ci`, Bash 3.2 syntax checks, deterministic application and Pages artifact checks, extracted command and browser smoke, local publication/deployment/idempotency fixtures, `git diff --check`, and isolated gateway discovery and workflow validation passed; the gateway admitted `download_your_data` as `READY`. No production release, publication, Pages configuration, or deployment command was run.

- [x] [M411] (P1) Align the repository layout with executable, API, and frontend ownership
  Goal:
  Make the repository root a concise project entrypoint while giving the executable bootstrap, protected HTTP API, and browser application separate canonical homes.

  Requirements:
  - Keep only project-wide governance, module, build, license, changelog, and operator documentation files at the repository root.
  - Place the sole executable bootstrap under `cmd/download-your-data`.
  - Place HTTP, authentication, user-workspace, Netflix, and OpenAI adapters with their contract tests under `internal/httpapi`.
  - Place the browser entrypoint, checked modules, styles, localized content, provenance manifests, and visual files under one `frontend` tree.
  - Remove obsolete duplicate fixtures and configuration instead of retaining aliases or parallel sources.
  - Keep generated builds and Playwright state out of the source tree through one ownership-safe cleanup command and temporary browser-test directories.

  Deliverables:
  - Canonical source tree with all build, test, documentation, and embedded-file paths updated.
  - Reversible pre-change file-placement snapshot and move ledger.
  - Repository-native cleanup and full validation.

  Validation:
  - Root inventory contains no application source or frontend payload files.
  - `make clean`
  - `make ci`
  - `git diff --check`

  Resolved 2026-07-30: the root now contains only project-wide entrypoint files; the executable lives under `cmd/download-your-data`, the protected transport layer and its contracts live under `internal/httpapi`, and the complete static application lives under `frontend`. The redundant JSON configuration and raw conversation fixture were removed from the canonical source tree, browser harnesses use temporary Playwright directories, and `make clean` owns generated-output cleanup. Tidy Folder snapshot `20260730_191146_171118` preserves the pre-change inventory, removed inputs, and scoped move ledger. Byte-for-byte visual-asset verification, duplicate-fixture verification, `make clean`, `make ci`, and `git diff --check` passed.

## Features

- [x] [F006] (P1) {I001,I005,I007} Add the local Netflix generation lifecycle
  Goal:
  Accept one Netflix viewing-activity CSV and atomically activate a private local generation with raw analytics.

  Requirements:
  - Define `GET /api/providers/netflix`, `POST /api/providers/netflix/generations`, `PUT /api/providers/netflix/generations/{generationID}/viewing-activity`, and `GET /api/providers/netflix/generations/{generationID}/events` as the canonical snapshot, create, upload, and progress contract.
  - Define generation analytics, cursor-paged records, cancellation, and complete provider deletion through the paths recorded in `docs/netflix-provider-plan.md`.
  - Accept only the current per-profile Viewing activity CSV contract; do not treat a full Netflix personal-information archive as the same input.
  - Validate request, upload, CSV, header, row, field, date, and generation boundaries once and return typed payloads and errors.
  - Use the closed `receiving`, `validating`, `importing`, `enriching`, `ready`, and `failed` lifecycle with an explicit `local` or `tmdb` analysis level.
  - Allow one building generation while keeping one ready generation active; reject conflicting creation and upload requests.
  - Enforce centralized compressed-independent byte, row, title, field, date, working-disk, progress, and concurrency limits.
  - Store parsed rows, title identities, analytics inputs, and checkpoints under a private generation staging directory.
  - Remove source CSV bytes after the validated reader closes on success or failure.
  - Activate only after row, date, title-identity, count, and analytics completeness checks pass.
  - Keep logs limited to opaque IDs, counts, states, durations, and typed failures.

  Deliverables:
  - Typed Netflix provider, generation, capability, progress, analytics, record, and error payloads.
  - Private persisted job repository, bounded worker, provider lease, and atomic active-generation pointer.
  - Raw activity analytics covering activity count, unique titles, date range, monthly activity, weekday rhythm, and top raw titles without TMDB.

  Validation:
  - Black-box HTTP test imports a synthetic Netflix CSV and queries its active raw analytics.
  - Failure tests prove invalid, oversized, canceled, incomplete, and conflicting generations never become active and leave no source upload behind.
  - Restart and lease tests prove checkpoints and single-process mutation ownership.
  - Filesystem tests prove path confinement, owner-only permissions, replacement isolation, and complete deletion.
  - `make ci`

- [x] [F007] (P1) {F006,I008} Add the TMDB replacement, matching, and export lifecycle
  Goal:
  Build a complete enriched replacement from the active raw Netflix generation without sacrificing local availability or privacy.

  Requirements:
  - Create TMDB analysis as a new generation derived from an explicit ready source generation.
  - Require a configured server-side token and explicit user initiation before any unique derived title query leaves the machine.
  - Keep the active raw generation readable while enrichment builds and activate the replacement atomically only after request and metadata completeness passes.
  - Persist every title as `matched`, `review`, or `unmatched`; omit metadata for non-accepted outcomes and report exact coverage.
  - Fail the replacement on exhausted transport, protocol, cache, or metadata errors instead of activating partial remote work.
  - Resume safe checkpoints after restart without duplicate requests, rows, or cache entries.
  - Replay ordered progress after browser reconnect and support explicit cancellation without changing the active generation.
  - Stream the canonical enriched CSV from a declared ready generation with raw values, match status, matcher identity, and accepted metadata.
  - Keep TMDB cache identity and accepted metadata reproducible; a matcher or cache-contract change requires a new generation.
  - Remove generation data, cache, WAL/SHM files, exports, and staging state through the declared cancellation, replacement, and provider-deletion contracts.

  Deliverables:
  - Enriched-generation orchestration, progress, cache provenance, match coverage, analytics, and streaming CSV export.
  - Typed not-configured, consent-required, rate-limited, unavailable, invalid-response, incomplete, canceled, and stale-source errors.
  - Required TMDB attribution payload for the browser Credits surface.

  Validation:
  - Fake-TMDB black-box test covers accepted, review, unmatched, cached, rate-limited, canceled, failed, resumed, and successfully activated generations.
  - Test proves the active raw generation remains readable through failed and canceled enrichment.
  - Export round-trip preserves raw rows and exposes deterministic match and metadata fields.
  - Filesystem and log audits prove no token, title, date, source row, temporary export, or orphaned provider data escapes its contract.
  - `make ci`

- [x] [F008] (P1) {F006,F007} Add the Netflix provider workspace
  Goal:
  Make Netflix a first-class localized provider with import, progress, analysis, enrichment, export, replacement, and deletion in the shared application shell.

  Requirements:
  - Add `netflix` to every supported locale in the canonical provider registry with current Viewing activity download instructions and official help.
  - Distinguish workspace-capable providers from guide-only providers without duplicating backend workflow state in localized content.
  - Replace the wrapping platform masthead and marketing hero with the compact provider catalog and MPR workspace described in `docs/netflix-provider-plan.md`.
  - Render one backend-owned Netflix state across empty, validating, importing, ready-local, not-configured, enriching, ready-enriched, review, failure, canceled, and replacement states.
  - Provide separate Overview, Catalog, and Match quality views with shared date and match-status filters.
  - Preserve all maintained source analytics: activity and unique-title KPIs, media type, genres, genres by viewing year, monthly activity, original language, origin country, release data, ratings, runtimes, seasons, episodes, weekday-by-genre activity, and top titles.
  - Label rows as activities or plays rather than completed views and show exact match coverage.
  - Keep import, TMDB enrichment, retry, cancel, replace, CSV export, and full delete as independent intent-specific controls.
  - Explain the local-only raw path and the exact TMDB title-query boundary before enrichment.
  - Display approved TMDB attribution and the current non-endorsement notice in Credits.
  - Use checked ES modules and self-owned application styles, scripts, fonts, icons, screenshots, and chart assets; load only the shared `mpr-ui@latest` header and footer assets from jsDelivr.
  - Provide keyboard operation, focus visibility, accessible progress announcements, chart summaries and tables, responsive rail collapse, and reduced-motion behavior.
  - Clean up event streams, pending requests, object URLs, chart instances, and subscriptions.

  Deliverables:
  - Localized Netflix catalog entry, empty/import state, provider workspace, dashboard views, match-quality view, state rail, and Credits attribution.
  - Compact dark-first MPR tokens and components adapted to the shared provider shell.
  - Browser fixtures and tests for the complete raw and enriched workflows.

  Validation:
  - Playwright covers every declared state and user action through the real local server and deterministic fake TMDB.
  - Accessibility coverage asserts keyboard flow, names, focus, live announcements, status semantics, chart alternatives, contrast, and reduced motion.
  - Wide and narrow viewport coverage proves the main/rail composition, dense filters, tables, charts, and destructive confirmations remain usable.
  - Browser network assertion permits only the two exact shared-shell asset requests and proves no TMDB call occurs before explicit enrichment.
  - All four locales resolve the same `netflix` provider identity and backend state without missing copy or placeholder assets.
  - `make test-browser`
  - `make ci`

- [x] [F009] (P1) {I001,P004} Add separate guides for the major Meta products
  Goal:
  Keep Facebook, Instagram, WhatsApp, and Threads as distinct provider identities with current, product-specific export instructions.

  Requirements:
  - Preserve the existing Facebook and Instagram entries and place WhatsApp and Threads beside them in the canonical provider order.
  - Treat WhatsApp account information and per-chat message history as separate exports; never imply that the account report contains messages.
  - Explain that Threads has its own export scope even though Meta currently starts the request from the Instagram app's Accounts Center.
  - Localize both new guides across English, Spanish, French, and Russian without adding backend workflow state.
  - Map every WhatsApp and Threads step to an approved first-party visual; Threads may reuse the current Instagram Accounts Center captures.
  - Link only to the current first-party WhatsApp and Meta help contracts.

  Deliverables:
  - Guide-only WhatsApp and Threads provider entries with distinct catalog marks, routes, instructions, references, and explanatory notes.
  - Checked-JavaScript routing, localized per-step visual contracts, and real-browser coverage for all four locales.

  Validation:
  - Every locale exposes exactly one Facebook, Instagram, WhatsApp, and Threads identity in the same canonical order.
  - Browser tests open the WhatsApp and Threads routes, verify complete instructions, and verify the official first-party references.
  - `make test-browser`
  - `make ci`

## Planning

- [x] [P001] (P1) Confirm the first canonical deployment and inference contract
  Goal:
  Select one deployment contract before backend implementation begins.

  Superseded contract:
  P006 replaces the first-release local-only deployment decision with anonymous static guides and shared-authenticated hosted provider applications.

  Deliverables:
  - The first release is local-only.
  - One loopback Go server owns the frontend and API.
  - Personal archives, databases, and vectors remain on the local machine.
  - LM Studio inference remains configurable without introducing a hosted application mode.

  Validation:
  - Confirm downstream feature issues depend on the local-only server foundation.

- [x] [P002] (P1) Confirm ownership of the conversation archive engine
  Goal:
  Resolve whether ChatIndex remains a separate project or becomes part of this application.

  Deliverables:
  - All maintained ChatIndex functionality is incorporated into `download_your_data`.
  - No `MarcoPoloResearchLab/chatindex` repository is created.
  - The standalone local `chatIndex` directory is abandoned after target-repository parity passes.

  Validation:
  - Confirm `F001` depends on the target-owned engine rather than an external module or process.

- [x] [P003] (P1) {I002} Confirm first-release coverage for incorporated analysis tools
  Goal:
  Define how every incorporated engine capability remains reachable after the standalone project is retired.

  Superseded contract:
  P006 and F001 through F005 move these capabilities into the authenticated web application and retire the current end-user product executable rather than preserving a second local workflow.

  Deliverables:
  - The browser first release owns OpenAI upload, status, replacement, hybrid search, and deletion workflows.
  - Definition-request analysis and reproducible report generation remain supported operator commands in the product executable for the first release.
  - Export inspection, import, indexing, and direct search remain operator diagnostics backed by the same packages and runtime configuration as the server.
  - Browser duplication of operator-only analysis is not required for the first canonical release.
  - There is one product executable and no transitional ChatIndex binary after command consolidation.

  Validation:
  - Confirm `I006` owns command consolidation and `F005` validates both browser and operator surfaces from the packaged artifact.

- [x] [P004] (P1) Confirm the privacy-safe instruction screenshot execution split
  Goal:
  Separate current web capture from the operator-dependent mobile capture so each can be executed and validated independently.

  Deliverables:
  - `I010` owns the authenticated Chrome capture, publication, registry, and validation for Facebook, Instagram, LinkedIn, X, YouTube, and Google.
  - `I011` owns the authenticated TikTok mobile capture and publication.
  - Both issues stop before export submission, archive request, download, credential entry, identity verification, destination connection, or account mutation.
  - The application uses only the current accepted asset contract; no partial-set fallback or legacy screenshot registry is introduced.

  Validation:
  - Confirm the web task can close without an authenticated mobile surface.
  - Confirm the mobile task can add TikTok assets without recapturing the web set.

- [x] [P005] (P1) Confirm the Netflix provider incorporation contract
  Goal:
  Select one ownership, privacy, lifecycle, UI, and retirement contract before Netflix implementation begins.

  Superseded user and deployment boundary:
  P006 and F011 replace the process-global local Netflix workspace with an authenticated user-scoped workspace. The accepted CSV, analytics, TMDB, lifecycle, and retirement semantics remain current.

  Deliverables:
  - `download_your_data` is the sole maintained owner; no module, path, subprocess, HTTP sidecar, copied database, CLI alias, or compatibility boundary survives.
  - The first accepted input is the per-profile Netflix Viewing activity CSV, not the separate full-account personal-information archive.
  - Raw import and raw analytics remain local and require no TMDB account.
  - TMDB enrichment is a separate explicit operation using a server-only read token and sends only unique derived title queries.
  - A raw generation remains active while an enriched replacement builds and activates atomically.
  - The shared compact provider catalog and Netflix MPR workspace own browser import, status, analytics, enrichment, export, replacement, and deletion.
  - Useful CLI capability moves under the single product executable rather than preserving `tmdbenrich`.
  - The standalone checkout remains unchanged until released target parity passes and destructive removal receives explicit operator approval.
  - `docs/netflix-provider-plan.md` records the complete accepted contract and delivery sequence.

  Validation:
  - Confirm `I007`, `I008`, `I009`, `F006`, `F007`, `F008`, and `M409` cover incorporation through retirement without a legacy bridge.
  - Confirm downstream API, UI, privacy, validation, and release requirements cite the same canonical plan.

- [x] [P006] (P1) Confirm anonymous guides and one shared authenticated user contract
  Goal:
  Separate public provider guidance from user-owned data analysis and select one authentication, storage, frontend, and deployment contract for every provider application.

  Deliverables:
  - `docs/user-authentication-plan.md` is the canonical cross-provider plan.
  - The provider catalog and `#guide/{provider}` routes are static and require no Download Your Data session.
  - `#app/{provider}` is the sole authenticated application route shape.
  - One Download Your Data TAuth tenant, one TAuth session, one `mpr-ui` lifecycle, and one validated TAuth user identity cover Netflix, OpenAI, and future provider applications.
  - Every provider artifact and operation is scoped to `(user, provider)`.
  - GitHub Pages owns the static frontend and an app-owned gateway container owns the protected API and persistent user workspaces.
  - The local-only packaged application, end-user operator commands, embedded frontend, unscoped data root, and Pages download landing page are superseded without a compatibility mode.
  - `I012`, `F010`, and `F011` own the public frontend, shared user boundary, and Netflix migration; `F001` through `F005` must implement OpenAI and release work against the same contract.

  Validation:
  - Confirm the plan names the public and protected routes, authentication owner, user identity, storage owner, profile requirements, rollout order, and local and production acceptance ladders.
  - Confirm no provider-specific authentication, anonymous analysis, hosted-profile guess, local/hosted dual mode, or production deployment action remains in scope.
