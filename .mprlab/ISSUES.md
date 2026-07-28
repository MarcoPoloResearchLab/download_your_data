# ISSUES

Entries record newly discovered requests or changes.

Read `AGENTS.md`, `.mprlab/POLICY.md`, `.mprlab/issues-md-format.md`, and relevant stack guides before implementing changes.

Format: `- [ ] [B042] (P1) {I007} Title`

## BugFixes

- [ ] [B001] (P1) {I002,I003} Calibrate definition-request classification before accepting semantic results
  Goal:
  Make definition analysis precise enough that accepted results can be used without treating broad semantic similarity as a definition request.

  Requirements:
  - Treat the current 5,903 accepted results and 20,262 review cases from 26,167 embedded messages as an uncalibrated baseline, not a completed classifier.
  - Build a deterministic labeled corpus covering explicit definitions, paraphrased definition requests, follow-up references, translations, pronunciation and usage requests, broad explanation requests, and unrelated negatives.
  - Keep retrieved candidates, accepted results, and review cases as distinct typed outcomes.
  - Do not promote unverified semantic-only matches into accepted results.
  - Record model identity, prefix identity, thresholds, and evaluation metrics in every audit artifact.
  - Commit only synthetic or deliberately anonymized evaluation data; never commit personal conversation content.

  Deliverables:
  - Reproducible classifier evaluation command and fixture.
  - Calibrated lexical and semantic thresholds with documented precision and recall.
  - Report output that clearly distinguishes accepted, rejected, and review results.

  Validation:
  - Achieve at least 95% precision and 90% recall on the labeled evaluation corpus.
  - Run the classifier against a private historical sample and record aggregate before-and-after counts without committing source text.
  - `make test`

- [ ] [B002] (P1) {I002,I003} Fail fast when definition-analysis inference is unavailable
  Goal:
  Report missing or incompatible local inference before scanning an archive or starting report generation.

  Requirements:
  - Preflight semantic prototype embeddings before querying historical messages when semantic analysis is enabled.
  - Preflight the verifier model before querying historical messages when verification is enabled.
  - Return typed, actionable errors for an unavailable server, no loaded model, model mismatch, and dimension mismatch.
  - Include the configured endpoint boundary and the required `lms load` action without exposing conversation content.
  - Propagate cancellation through readiness checks, archive queries, classification, and report writing.
  - Do not create partial report files when readiness fails.

  Deliverables:
  - Shared inference-readiness contract used by definition analysis and archive indexing.
  - Public CLI coverage for each readiness failure.
  - Bounded, paged archive scanning after readiness succeeds.

  Validation:
  - Black-box CLI test proving a missing model fails before history scanning or report creation.
  - Black-box CLI test proving a ready deterministic inference server completes the same workflow.
  - `make test`

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

- [ ] [I003] (P1) {I002} Normalize the incorporated engine to the forward-only product contract
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

- [ ] [I004] (P1) {I003} Establish a retrieval-quality and completeness gate
  Goal:
  Prove that local hybrid search finds relevant conversations and suppresses unrelated semantic matches before the browser depends on it.

  Requirements:
  - Build a deterministic search evaluation corpus covering exact terms, paraphrases, negatives, date filters, archive filters, multiple excerpts, and conversation aggregation.
  - Bind every index and query to the same provider, endpoint boundary, model identity, dimensions, document prefix, query prefix, builder version, and corpus policy.
  - Reject mixed or mismatched vector identities instead of selecting a nearby configuration.
  - Require every eligible searchable message to be indexed or carry a typed exclusion reason.
  - Keep thresholds model-specific and versioned; do not reuse a threshold after identity or corpus-policy changes.
  - Keep private conversation text out of committed evaluation fixtures and logs.

  Deliverables:
  - Repository-native `make eval-search` gate with deterministic inference.
  - Coverage audit reporting eligible, indexed, excluded, stale, and failed document counts.
  - Versioned ranking and cutoff configuration with an explainable evaluation report.

  Validation:
  - Every positive fixture returns its expected conversation in the top five.
  - Explicit negative fixtures remain below the accepted semantic cutoff.
  - Eligible-document coverage is 100% before a generation can become ready.
  - `make eval-search`

- [ ] [I005] (P1) {I003} Establish the secure local data and inference boundary
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

- [ ] [I006] (P1) {I003,P003} Consolidate operator workflows into the product command
  Goal:
  Ship one product-owned executable without retaining a transitional ChatIndex binary or alias.

  Requirements:
  - Make `download-your-data` own `serve`, `inspect`, `import`, `index`, `search`, and `definitions` entry points.
  - Delete `cmd/chatindex`, `build/chatindex`, ChatIndex-named Make targets, and old command documentation after command parity passes.
  - Keep definition analysis and reproducible reports as operator subcommands for the first release.
  - Share packages and validated runtime configuration with the browser backend; do not invoke one command from another as a subprocess.
  - Preserve one canonical command spelling only.

  Deliverables:
  - One application binary with server and operator subcommands.
  - Updated smoke workflow and operator documentation.
  - Deleted transitional command surfaces without compatibility aliases.

  Validation:
  - Black-box command smoke covers inspect, import, index, hybrid search, definitions, and serve.
  - Repository search finds no shipped `chatindex` executable, command, or build target.
  - `make ci`

- [x] [I007] (P1) {P004} Publish the authenticated web instruction screenshot set
  Goal:
  Capture and publish the current web export workflow so a user can follow each supported provider without the capture operator starting an export.

  Requirements:
  - Cover Facebook, Instagram, LinkedIn, X, YouTube, and Google with two canonical English desktop screenshots per provider.
  - Use the operator's authenticated Chrome session and the current official provider routes.
  - Navigate only through instructional setup screens and stop before every archive request, export creation, download, destination connection, password entry, verification-code request, or account mutation.
  - Treat X's empty password-verification form as the second instructional boundary; do not enter credentials solely to reach a later screen.
  - Capture the smallest useful panel at a consistent desktop viewport without browser chrome, credentials, names, handles, email addresses, avatars, organizations, account identifiers, notifications, or private counts.
  - Keep one 12-entry shared manifest and reuse the assets across `en`, `es`, `fr`, and `ru`; do not duplicate screenshots by locale.
  - Remove web placeholders when the complete 12-shot set is accepted. TikTok remains text-only until `I008` supplies its separate mobile set.

  Deliverables:
  - Twelve reviewed, metadata-free local assets beneath `images/instructions/`.
  - Current provider instructions, official references, capture runbook, and shared screenshot manifest.
  - Shared asset wiring with localized alternative text for all four locales.
  - Repository-native screenshot validation and browser coverage.

  Validation:
  - The manifest contains exactly 12 unique web screenshot IDs and two local assets for each supported web provider.
  - Every image is privacy-reviewed at full resolution and matches its recorded current live labels.
  - All four locales render the shared images without placeholders at desktop and mobile application viewports.
  - `make test-browser`
  - `make ci`
  - `git diff --check`
  - `git status --short`

- [ ] [I008] (P2) {P004} Publish the authenticated TikTok mobile instruction screenshots
  Goal:
  Add the app-owned TikTok export workflow as an independently scheduled mobile capture.

  Requirements:
  - Use an operator-connected authenticated TikTok mobile surface; do not substitute an unofficial web flow, mock, or stale screenshot.
  - Capture Settings and privacy navigation and Download your data before Request data.
  - Keep credentials, identity-verification material, personal identifiers, notifications, and private account content out of published assets.
  - Do not request, cancel, or download an archive, change settings, switch accounts, or cross an identity-verification boundary.
  - Record current official workflow and publication guidance at capture time.

  Deliverables:
  - Two reviewed, metadata-free portrait assets and their manifest entries.
  - Shared localized asset wiring without locale-specific image duplication.
  - Updated screenshot validation and browser coverage.

  Validation:
  - Both TikTok assets match the current authenticated app labels and contain no private content.
  - `make test-browser`
  - `make ci`

## Maintenance

- [ ] [M400R] (P2) Backlog hygiene and archive
  Goal:
  Keep the issue tracker reliable, readable, and focused on active work while preserving resolved history in the appropriate archive.

  Requirements:
  - Cadence: run weekly during active development and before each release cut.
  - Validate section names, identifier prefixes, recurrence suffixes, priority markers, dependencies, and duplicate IDs against the current `issues-md-format.md`.
  - Reconcile stale statuses, duplicate issues, broken references, obsolete instructions, and entries filed under the wrong section.
  - Move completed non-recurring history to the repository issue archive or durable documentation when the active tracker becomes noisy.
  - Keep active, blocked, planning, and recurring entries visible in `ISSUES.md`.

  Deliverables:
  - Normalized `ISSUES.md` structure and statuses.
  - Updated issue archive or docs when completed entries are removed from the active tracker.
  - A short `Last run:` note summarizing the cleanup and any follow-up issues filed.

  Validation:
  - Re-read `ISSUES.md` after edits and confirm every issue is under the right section with a unique section-aware ID.
  - Confirm recurring entries remain open and keep the `R` suffix.
  - Confirm no active, blocked, recurring, or planning work was archived.

  Last run:
  - 2026-07-15: normalized the integration backlog and added the missing quality, security, packaging, command-consolidation, and retirement work as dependency-linked issues.

- [ ] [M401R] (P2) Polish open issues
  Goal:
  Keep unresolved work executable by making each open issue concrete, ordered, and testable.

  Requirements:
  - Cadence: run weekly during active development and before handing a repo to automated execution.
  - Review every unresolved non-recurring issue for missing context, dependencies, repro steps, acceptance criteria, and validation expectations.
  - Make priorities concrete and ensure each open issue has actionable deliverables.
  - Merge duplicate open issues or add explicit dependency links when separate entries must remain.
  - Do not close or implement issues as part of this polish pass unless that work is separately requested.

  Deliverables:
  - Open issues with enough detail for a person or agent to execute without rediscovery.
  - New or updated dependency markers where ordering matters.
  - A short `Last run:` note listing the number of issues polished and any blockers found.

  Validation:
  - Sample the open entries after the pass and confirm each has clear next actions and validation expectations.
  - Confirm no recurring runbook was marked complete.
  - Confirm duplicates were merged or explicitly cross-referenced.

  Last run:
  - 2026-07-15: polished four existing feature issues, added eight focused open follow-ups, and found no external blocker beyond each issue's recorded dependencies.

- [ ] [M402R] (P2) Architecture and policy review
  Goal:
  Catch architecture, policy, and workflow drift before it becomes hidden maintenance debt.

  Requirements:
  - Cadence: run monthly, before large refactors, and after major framework or runtime changes.
  - Review the codebase, docs, and workflow against `AGENTS.md`, `POLICY.md`, stack guides, and the current architecture notes.
  - Look for drift from forward-only contracts, edge-validation boundaries, smart-constructor usage, testing policy, and module ownership.
  - Record findings as new Maintenance issues with concrete scope, priority, and validation.
  - Close the pass with a no-action note only when the review finds no actionable drift.

  Deliverables:
  - New Maintenance issues for each actionable architecture or policy drift finding.
  - Updated notes on areas reviewed and areas intentionally left unchanged.
  - A short `Last run:` note with the review scope and outcome.

  Validation:
  - Confirm every finding is represented as an issue with owner-readable context and validation criteria.
  - Confirm no implementation changes were mixed into the review runbook unless separately requested.
  - Confirm all recurring runbooks remain open.

- [ ] [M403R] (P1) Dependency and security audit
  Goal:
  Keep third-party dependencies, runtime versions, and security-sensitive configuration within the current supported contract.

  Requirements:
  - Cadence: run weekly for active apps and before each release cut.
  - Inspect package managers, lockfiles, language toolchains, container bases, and generated clients for known vulnerabilities or stale direct dependencies.
  - Review auth, secret, CORS, CSP, SQL, network, and permission-sensitive configuration for drift from the current contract.
  - Prefer current supported dependencies; do not add compatibility shims for obsolete dependency behavior.
  - File separate Maintenance or BugFix issues for each actionable vulnerability, unsupported runtime, or security-contract gap.

  Deliverables:
  - Documented audit commands or data sources used for the pass.
  - Updated issues for each actionable dependency or security finding.
  - A short `Last run:` note with clean result or follow-up issue IDs.

  Validation:
  - Rerun the repository-native audit, lint, or dependency checks used for the pass.
  - Confirm every finding is either filed, fixed under a separate issue, or explicitly marked not applicable with evidence.
  - Confirm no secrets or private payloads were written into the tracker.

- [ ] [M404R] (P1) CI, release, and artifact health
  Goal:
  Keep the repository's validation, release, publication, and generated artifact surfaces trustworthy.

  Requirements:
  - Cadence: run before every release, publish, or deploy, and weekly for critical services.
  - Verify repository-native CI, lint, format, coverage, release, publish, Docker image, Pages, and artifact workflows still match the documented contract.
  - Check generated artifacts, release tags, published images, and Pages outputs for source-to-public drift.
  - File concrete follow-up issues for failing gates, stale artifacts, missing release prerequisites, or undocumented workflow changes.
  - Do not perform production deployment from this runbook unless the operator explicitly requests that deployment.

  Deliverables:
  - Recorded gate status and artifact surfaces inspected.
  - Follow-up issues for each reproducible CI, release, publish, or artifact drift problem.
  - A short `Last run:` note with commands run and any skipped surfaces.

  Validation:
  - Use repository-native `make` targets or documented release helpers for checks.
  - Confirm release and deployment ownership boundaries remain separate.
  - Confirm public or published artifacts match the intended source revision when that surface is inspected.

- [ ] [M405R] (P1) Code contract and static hygiene
  Goal:
  Keep source contracts explicit, current, and statically guarded against policy drift.

  Requirements:
  - Cadence: run monthly and before large refactors.
  - Scan for dead code, unused exports, duplicated literals, silent fallbacks, legacy aliases, compatibility reads, and zero-but-invalid domain states.
  - Check static analysis, coverage, schema, and contract guards that are supposed to prevent drift.
  - File focused Maintenance issues for each concrete violation instead of broad cleanup placeholders.
  - Keep the current canonical contract only; do not preserve obsolete behavior unless a product requirement explicitly says so.

  Deliverables:
  - Issue entries for each actionable static hygiene or contract violation.
  - Notes on static tools, searches, and contract guards used during the pass.
  - A short `Last run:` note with clean result or follow-up issue IDs.

  Validation:
  - Rerun the relevant static checks, contract tests, or repository searches used to identify drift.
  - Confirm every finding has a narrow follow-up issue and does not duplicate existing backlog work.
  - Confirm no implementation changes were mixed into the audit unless separately requested.

- [ ] [M406R] (P1) Production drift and health
  Goal:
  Detect when production, public, or scheduled runtime state has drifted from the intended repository contract.

  Requirements:
  - Cadence: run weekly for deployed services and after each publish or deploy.
  - Compare current source, runtime configuration, published images, public routes, scheduled jobs, and health checks for drift.
  - Inspect real operator-facing surfaces rather than assuming merged source is deployed.
  - File follow-up issues for stale images, stale Pages output, missing routes, failed monitors, invalid production config, or undocumented runtime differences.
  - Stop before production deploy or destructive operator actions unless the operator explicitly requests them.

  Deliverables:
  - Recorded source revision, public artifact, route, image, or health surfaces inspected.
  - Follow-up issues for each source-to-runtime drift finding.
  - A short `Last run:` note with evidence links or commands used.

  Validation:
  - Verify inspected production or public surfaces directly where access is available.
  - Confirm any deploy-required finding is filed with the exact publish/deploy boundary and owner.
  - Confirm no production state was changed by the audit unless explicitly requested.

- [ ] [M407R] (P2) Documentation and runbook hygiene
  Goal:
  Keep durable documentation and runbooks aligned with the current behavior users and operators actually rely on.

  Requirements:
  - Cadence: run before release cuts and after merge bursts that change user-facing or operator-facing behavior.
  - Review README, ARCHITECTURE, PRD, CHANGELOG, docs, runbooks, setup guides, and local workflow notes for stale behavior or missing new contracts.
  - Update docs when closed issues changed durable behavior, public APIs, operator workflows, release semantics, or deployment expectations.
  - Remove or rewrite stale instructions instead of preserving obsolete alternatives.
  - File separate issues for documentation gaps that require product or implementation decisions.

  Deliverables:
  - Updated documentation or filed follow-up issues for each gap.
  - A short `Last run:` note listing docs inspected and changes made.
  - Cross-references from archived issue history to durable docs when useful.

  Validation:
  - Check links, command names, paths, and public contract descriptions touched by the pass.
  - Confirm docs describe the current canonical path only.
  - Confirm issue archive and active tracker references remain consistent.

- [ ] [M408] (P1) {F005} Retire the abandoned standalone ChatIndex checkout
  Goal:
  Remove the obsolete local project boundary after the first target-owned release proves complete parity.

  Requirements:
  - Confirm the target release owns every maintained engine, command, fixture, report, and validation capability without a filesystem or module dependency on the standalone checkout.
  - Search active local documentation and automation for references to the old directory or a nonexistent remote repository and remove them at their owning source.
  - Identify databases, vectors, exports, and reports under the standalone directory for explicit operator disposition; do not silently delete or copy personal data.
  - Remove `/Users/tyemirov/Development/chatIndex` only after explicit operator approval for that destructive step.
  - Do not create `MarcoPoloResearchLab/chatindex` or any replacement compatibility repository.

  Deliverables:
  - Evidence that the target repository passes independently of the standalone checkout.
  - Operator-approved disposition of non-source data.
  - Removed standalone checkout and stale external references.

  Validation:
  - Run target `make ci` with the standalone path unavailable.
  - Search confirms the target has no runtime, build, test, or documentation dependency on the old path or repository.
  - `git status --short`

## Features

- [ ] [F001] (P1) {I001,I002,I005} Add the OpenAI archive generation lifecycle
  Goal:
  Accept an OpenAI data-export ZIP and atomically build one active local conversation-search generation.

  Requirements:
  - Integrate the repository-owned conversation archive engine directly instead of invoking its CLI.
  - Define `POST /api/providers/openai/generations`, `PUT /api/providers/openai/generations/{generationID}/archive`, `GET /api/providers/openai`, and `GET /api/providers/openai/generations/{generationID}/events` as the canonical create, upload, snapshot, and progress contract.
  - Validate request, upload, ZIP, and OpenAI-export boundaries once and return typed payloads and errors.
  - Use a closed persisted state machine: `receiving`, `validating`, `importing`, `indexing`, `ready`, or `failed`.
  - Allow one building generation at a time and reject conflicting creation or upload requests.
  - Enforce centralized limits for compressed bytes, the recognized conversation entry, entry count, compression ratio, inference batch size, and working-disk use.
  - Reject malformed, encrypted, ambiguous, or duplicate conversation payloads and never extract arbitrary ZIP paths.
  - Preflight local inference before expensive import or indexing work.
  - Store SQLite and vector artifacts under a private generation-owned staging directory.
  - Activate a generation only after import, index-identity, and eligible-document completeness checks pass.
  - Remove the source ZIP and transient extraction data after the reader closes on success or failure.

  Deliverables:
  - Typed provider, generation, progress-event, capabilities, and error payloads.
  - Persisted job repository and bounded background worker.
  - Atomic active-generation pointer and private staging layout.

  Validation:
  - Black-box HTTP test with a synthetic OpenAI export and deterministic embedding server.
  - Failure test proving an incomplete generation never becomes active.
  - Security tests for oversized, malformed, encrypted, ambiguous, and traversal-shaped archives.
  - Cancellation and client-disconnect tests proving work and temporary files are bounded.

- [ ] [F002] (P1) {F001} Add the OpenAI upload and indexing experience
  Goal:
  Add OpenAI to the provider registry and provide ZIP upload, progress, failure, and replacement states.

  Requirements:
  - Add OpenAI to every supported locale in the provider registry with current export instructions.
  - Migrate the browser application to checked ES modules and validated API payloads.
  - Vendor required styles and scripts and tighten the CSP to self-owned assets; the local application must not require a CDN, font host, or other browser-side network dependency.
  - Render one authoritative workflow state and emit intent-specific events.
  - Display backend-owned upload bytes, generation progress, readiness, and actionable LM Studio errors without simulated timers.
  - Run an inference readiness check before asking the user to upload a large archive.
  - Explain exactly which message text reaches the configured inference endpoint and that attachments do not.
  - Support keyboard operation, accessible status announcements, retry, and explicit replacement confirmation.
  - Clean up object URLs, event streams, and pending requests.

  Validation:
  - Playwright coverage of upload, progress, failure, and ready states through the real server.
  - Playwright coverage of keyboard flow, accessible announcements, replacement confirmation, reconnect, and retry.
  - Browser-network assertion proving the shipped page requests no external asset.

- [ ] [F003] (P1) {F001,F002,I004} Add hybrid semantic conversation search
  Goal:
  Search the active OpenAI archive by meaning and exact terms with conversation-level results.

  Requirements:
  - Define `POST /api/providers/openai/search` as a validated, cancellable query contract against exactly one active ready generation.
  - Default to hybrid retrieval over the active ready generation.
  - Support bounded query text, date, archive, result-limit, and excerpt-count filters through typed requests.
  - Return stable conversation IDs, titles, timestamps, archive state, scores, match reason, and supporting excerpts.
  - Use deterministic ordering and a stable continuation cursor when the result cap is reached.
  - Return typed `not_ready`, `inference_unavailable`, `model_mismatch`, `invalid_query`, and `canceled` failures.
  - Keep advanced ranking details hidden unless requested.
  - Never write query text or returned excerpts to logs or browser persistence.

  Validation:
  - Black-box search test with known lexical and semantic results.
  - Playwright coverage of query, filters, results, empty state, and failure state.
  - Contract tests for query limits, cancellation, deterministic ordering, pagination, and inference-identity mismatch.

- [ ] [F004] (P1) {F001,F002,F003} Complete replacement, restart, and deletion contracts
  Goal:
  Make the local archive lifecycle safe across replacement, interruption, restart, and deletion.

  Requirements:
  - Keep the active generation searchable while a replacement generation builds.
  - Persist progress checkpoints and resume an interrupted build without dual reads or duplicate vector rows.
  - Reconcile receiving, building, failed, and orphaned staging directories at startup.
  - Commit generation readiness and the active pointer in one transaction, then delete the obsolete generation after successful activation.
  - Keep one process-owned generation lease so concurrent servers or jobs cannot mutate the same library.
  - Replay ordered server-sent events after reconnect without duplicating state transitions.
  - Expose explicit cancellation for a building generation and explicit confirmation for full provider deletion.
  - Treat a changed model, endpoint boundary, dimensions, prefix, builder version, or corpus policy as a new generation identity that requires reindexing.
  - Delete every database, WAL/SHM file, vector, cache, source archive, and temporary upload on library deletion.
  - Never log conversation content or search queries.

  Validation:
  - Black-box restart, failed replacement, successful replacement, and complete deletion scenarios.
  - Crash-point tests around checkpoint, readiness, active-pointer commit, and obsolete-generation cleanup.
  - Concurrency test proving a second server or builder cannot mutate the active library.
  - Filesystem audit proving canceled, failed, replaced, and deleted generations leave no private payload behind.

- [ ] [F005] (P1) {B001,B002,F004,I006} Package the first canonical local release
  Goal:
  Deliver a self-contained Apple Silicon application artifact that owns the server, browser assets, archive engine, and operator workflows.

  Requirements:
  - Build one `download-your-data` executable for macOS arm64 with embedded browser assets and no dependency on the source checkout.
  - Keep LM Studio as an explicit local runtime dependency and provide first-run readiness guidance for the required model and alias.
  - Start on loopback, use the canonical private data root, and open the local application without introducing hosted mode.
  - Include version, schema, model-identity, data-location, backup, replacement, and deletion guidance.
  - Keep `make release`, any future publication step, and any future deployment step as separate contracts.
  - Do not package personal archives, databases, vectors, reports, caches, or local environment files.

  Deliverables:
  - Reproducible `make release` artifact and checksum.
  - First-run and troubleshooting documentation for LM Studio, archive upload, search, backup, and deletion.
  - Release validation that runs from the extracted artifact with a temporary home and deterministic local inference server.

  Validation:
  - Black-box artifact smoke covers first start, health, capabilities, upload, ready state, hybrid search, definitions, restart, replacement, and deletion.
  - Browser test proves the packaged application has no external frontend requests.
  - `make ci`
  - `make release`

## Planning

- [x] [P001] (P1) Confirm the first canonical deployment and inference contract
  Goal:
  Select one deployment contract before backend implementation begins.

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
  - `I007` owns the authenticated Chrome capture, publication, registry, and validation for Facebook, Instagram, LinkedIn, X, YouTube, and Google.
  - `I008` owns the authenticated TikTok mobile capture and publication.
  - Both issues stop before export submission, archive request, download, credential entry, identity verification, destination connection, or account mutation.
  - The application uses only the current accepted asset contract; no partial-set fallback or legacy screenshot registry is introduced.

  Validation:
  - Confirm the web task can close without an authenticated mobile surface.
  - Confirm the mobile task can add TikTok assets without recapturing the web set.
