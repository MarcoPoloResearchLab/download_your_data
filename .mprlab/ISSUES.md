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
  Report missing or incompatible configured inference before scanning an archive or starting report generation.

  Requirements:
  - Preflight semantic prototype embeddings before querying historical messages when semantic analysis is enabled.
  - Preflight the verifier model before querying historical messages when verification is enabled.
  - Return typed, actionable errors for an unavailable server, no loaded model, model mismatch, and dimension mismatch.
  - Include the configured endpoint boundary and an operator-owned remediation code without exposing conversation content or server credentials.
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

- [ ] [I011] (P2) {P004} Publish the authenticated TikTok mobile instruction screenshots
  Goal:
  Replace the first-party TikTok Support visuals with authenticated app-native export screenshots.

  Requirements:
  - Use an operator-connected authenticated TikTok mobile surface; do not substitute an unofficial web flow, mock, or stale screenshot.
  - Capture Settings and privacy navigation and Download your data before Request data.
  - Keep credentials, identity-verification material, personal identifiers, notifications, and private account content out of published assets.
  - Do not request, cancel, or download an archive, change settings, switch accounts, or cross an identity-verification boundary.
  - Record current official workflow and publication guidance at capture time.

  Deliverables:
  - Reviewed, metadata-free portrait assets covering the current request and download panels.
  - Updated per-step visual mappings without locale-specific image duplication.
  - Updated screenshot validation and browser coverage.

  Validation:
  - Every TikTok step still renders one visual after the first-party help captures are removed.
  - All replacement assets match the current authenticated app labels and contain no private content.
  - `make test-browser`
  - `make ci`

- [ ] [I012] (P1) {F010} Make Pages the canonical anonymous guide frontend
  Goal:
  Publish the provider catalog and every provider guide as the canonical static browser surface while keeping all data-analysis routes behind the shared authenticated application boundary.

  Requirements:
  - Replace the Pages download landing page and embedded-frontend ownership split with one repository-owned static frontend artifact.
  - Keep `#catalog`, `#guide/{provider}`, Credits, and privacy content fully usable without a Download Your Data session.
  - Replace `#provider/{provider}` with `#app/{provider}` as the sole current data-analysis route; do not retain an alias or redirect.
  - Render the public catalog and guides from one strict provider registry with the current localized copy, exact first-party action links, local icons, and one reviewed screenshot per instruction step.
  - Keep provider application code in the public artifact but make zero protected API requests until the shared `mpr-ui` lifecycle reports authenticated.
  - Add the tracked `/config-ui.yaml`, `mpr-ui-config.js`, literal `mpr-ui@latest` bundle marker, shared user control, startup reconciliation, and auth transition contract without app-owned authentication code.
  - Render and seal the Pages artifact locally; do not use GitHub Actions as the publishing mechanism.

  Deliverables:
  - Canonical anonymous Pages catalog and guide routes plus authenticated application routes.
  - Validated static artifact, public runtime profile, CSP, responsive MPR styling, and real-browser coverage.
  - Removed embedded-product and download-landing frontend paths in the same forward change.

  Validation:
  - Fresh signed-out browser coverage proves every guide is readable and makes zero protected application API requests.
  - Static scans reject secrets, deployment-only values, MPRLab version pins, direct `tauth.js`, manual `tauth-*` wiring, and obsolete provider application routes.
  - Wide and narrow browser coverage proves public and authenticated layouts remain compact, keyboard-operable, and free of overflow.
  - `make test-browser`
  - `make ci`

- [x] [I014] (P1) {I012,F010} Adopt the schema-v3 production lifecycle
  Goal:
  Turn the existing never-deployed DNS and application foundation into one exact, forward-only production resource contract owned by this repository and orchestrated by the sibling gateway.

  Requirements:
  - Record the exact Pages, API, TAuth, cookie, CORS, DNS, Caddy, container-port, health, storage, and private-value literals for the current split-origin topology.
  - Replace the schema-v1 workflow stub with schema v3 typed resources and direct capability references.
  - Build one deterministic static Pages artifact and one Linux API container from committed source.
  - Keep `.mprlab/deploy/resources.yml` as the only tracked deployment file and `.mprlab/deploy/.env` as the ignored mode-`0600` private input.
  - Replace the fail-closed lifecycle placeholder with exact zero-argument delegators to the sibling `mprlab-gateway`.
  - Do not publish, release, or deploy as part of implementation validation.

  Deliverables:
  - Current production profile documentation and validated artifact inputs.
  - Schema-v3 resource manifest covering Pages, API runtime, TAuth, Caddy, health, storage, and private values.
  - Repository CI and non-mutating gateway plan evidence.

  Validation:
  - `make test-production-artifacts` builds the Linux/AMD64 Pages and API targets, rejects unresolved or private Pages inputs, and runs the API read-only as `65532:65532` through `/api/health`.
  - `make ci` passed on 2026-08-02.
  - Clean sealed release, publish, and deploy plans passed through the sibling gateway against the real operator inventory without production mutation.

## Maintenance

- [ ] [M400R] (P2) Backlog hygiene and archive
  Goal:
  Keep the issue tracker reliable, readable, and focused on active work while preserving resolved history in the appropriate archive.

  Requirements:
  - Cadence: run weekly during active development and before each release cut.
  - Validate section names, identifier prefixes, recurrence suffixes, priority markers, dependencies, and duplicate IDs against the current `issues-md-format.md`.
  - Reconcile stale statuses, duplicate issues, broken references, obsolete instructions, and entries filed under the wrong section.
  - Move completed non-recurring history to the repository issue archive or durable documentation when the active tracker becomes noisy.
  - Update README.md, ARCHITECTURE.md, PRD.md, and any related documentation with the results of completed issues, if their results materially change the details or procedures present.
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
  - 2026-07-30: audited all 74 backlog entries against issues-md-format.md. Fixed M409 title line formatting, corrected invalid dependency cross-references on B008, B014, and M410, and moved 42 completed non-recurring issues to .mprlab/ISSUES_ARCHIVE.md, leaving 32 active, blocked, and recurring entries in ISSUES.md.

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

  Last run:
  - 2026-07-30: reviewed codebase, docs, and workflows against AGENTS.md, POLICY.md, and stack guides (Go, Frontend, Git). Confirmed forward-only contract compliance, edge-validation boundaries, smart constructor usage, JS @ts-check enforcement, and single executable contract across all components. Found no actionable drift.

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
  - Confirm the target release owns every maintained engine, browser workflow, fixture, report, and validation capability required by the current authenticated web contract without a filesystem or module dependency on the standalone checkout.
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

- [!] [M409] (P1) {F005,F008,I009,F011} Retire the abandoned standalone Netflix checkout
  Goal:
  Remove the obsolete Netflix project boundary after the authenticated target release proves complete browser parity.

  Requirements:
  - Confirm the target owns viewing-history validation, import, analytics, TMDB enrichment, matching outcomes, cache, dashboard, CSV export, fixtures, and validation without the standalone checkout.
  - Prove the released target artifact runs without a module, subprocess, HTTP, build, test, documentation, or filesystem dependency on the source repository.
  - Inspect `/Users/tyemirov/Development/netflix` for untracked or private runtime data and record explicit operator disposition without printing or copying personal content.
  - Do not import or preserve `netflix_cache.sqlite` as a target artifact.
  - Remove `/Users/tyemirov/Development/netflix` only after explicit operator approval for that destructive action.
  - Do not retain or create a compatibility repository, package, binary, service, or redirect for the old boundary.

  Deliverables:
  - Target release and parity evidence tied to the incorporated source revision.
  - Operator-approved disposition of any non-source data.
  - Removed standalone checkout and stale owning-source references.

  Validation:
  - Run target `make ci` and artifact smoke with the standalone path unavailable.
  - `go list -m all` and repository search find no dependency on `github.com/tyemirov/netflix` or the old checkout.
  - `git status --short`

  Blocked: F005 has not produced the required target-owned release; the tracked nonempty source cache has no operator disposition, and destructive checkout removal has not received explicit approval.

## Features

- [ ] [F001] (P1) {I001,I002,F010} Add the user-owned OpenAI archive generation lifecycle
  Goal:
  Accept an OpenAI data-export ZIP and atomically build one active conversation-search generation for the authenticated user.

  Requirements:
  - Integrate the repository-owned conversation archive engine directly instead of invoking its CLI.
  - Define `POST /api/providers/openai/generations`, `PUT /api/providers/openai/generations/{generationID}/archive`, `GET /api/providers/openai`, and `GET /api/providers/openai/generations/{generationID}/events` as the canonical create, upload, snapshot, and progress contract.
  - Protect every route with the shared TAuth session and resolve all provider state beneath the authenticated user's OpenAI workspace.
  - Validate request, upload, ZIP, and OpenAI-export boundaries once and return typed payloads and errors.
  - Use a closed persisted state machine: `receiving`, `validating`, `importing`, `indexing`, `ready`, or `failed`.
  - Allow one building generation per user and reject conflicting creation or upload requests within that user workspace.
  - Enforce centralized limits for compressed bytes, the recognized conversation entry, entry count, compression ratio, inference batch size, and working-disk use.
  - Reject malformed, encrypted, ambiguous, or duplicate conversation payloads and never extract arbitrary ZIP paths.
  - Preflight the configured server-owned inference boundary before expensive import or indexing work.
  - Store SQLite and vector artifacts under a private user- and generation-owned staging directory.
  - Activate a generation only after import, index-identity, and eligible-document completeness checks pass.
  - Remove the source ZIP and transient extraction data after the reader closes on success or failure.

  Deliverables:
  - Typed provider, generation, progress-event, capabilities, and error payloads.
  - User-scoped persisted job repository and bounded background worker.
  - User-scoped atomic active-generation pointer and private staging layout.

  Validation:
  - Black-box HTTP test with a synthetic OpenAI export and deterministic embedding server.
  - Failure test proving an incomplete generation never becomes active.
  - Security tests for oversized, malformed, encrypted, ambiguous, and traversal-shaped archives.
  - Cancellation and client-disconnect tests proving work and temporary files are bounded.
  - Two-user tests prove generations, events, files, active pointers, and failures cannot cross user boundaries.

- [ ] [F002] (P1) {F001,I012} Add the authenticated OpenAI upload and indexing experience
  Goal:
  Open the existing public OpenAI guide and a distinct authenticated OpenAI application with ZIP upload, progress, failure, and replacement states.

  Requirements:
  - Keep OpenAI in every locale's anonymous guide registry and expose `#app/openai` as its sole data-analysis route.
  - Wait for the shared `mpr-ui:auth:authenticated` lifecycle before making the first OpenAI API request.
  - Use checked ES modules, validated API payloads, `credentials: include`, and the exact production API origin from the selected profile.
  - Keep application styles, scripts, fonts, icons, screenshots, and charts in the sealed Pages artifact; load every MPRLab library through the literal `@latest` contract.
  - Render one authoritative workflow state and emit intent-specific events.
  - Display backend-owned upload bytes, generation progress, readiness, and actionable inference errors without simulated timers.
  - Run an inference readiness check before asking the user to upload a large archive.
  - Explain exactly which message text reaches the configured inference endpoint and that attachments do not.
  - Support keyboard operation, accessible status announcements, retry, and explicit replacement confirmation.
  - Clean up object URLs, event streams, pending requests, and all app-owned OpenAI state on shared-shell sign-out.
  - Never add an OpenAI-specific Download Your Data login, tenant, session, cookie, or auth-state check.

  Validation:
  - Playwright coverage of shared authentication, upload, progress, failure, and ready states through the real stack.
  - Playwright coverage of keyboard flow, accessible announcements, replacement confirmation, reconnect, and retry.
  - Browser-network coverage proves the public OpenAI guide makes no protected request and the application waits for authenticated lifecycle evidence.
  - One authenticated browser opens Netflix and OpenAI without a second login action.

  Progress 2026-07-29: OpenAI now appears directly in the provider catalog as a guide-only surface in every supported locale. The guide follows OpenAI's current signed-in export flow, links to the official help article, and has real-browser contract coverage. F002 remains open for the shared-authenticated upload, indexing, progress, failure, and replacement experience.

- [ ] [F003] (P1) {F001,F002,I004} Add hybrid semantic conversation search
  Goal:
  Search the active OpenAI archive by meaning and exact terms with conversation-level results.

  Requirements:
  - Define `POST /api/providers/openai/search` as a protected, validated, cancellable query contract against exactly one active ready generation owned by the authenticated user.
  - Default to hybrid retrieval over that user's active ready generation.
  - Support bounded query text, date, archive, result-limit, and excerpt-count filters through typed requests.
  - Return stable conversation IDs, titles, timestamps, archive state, scores, match reason, and supporting excerpts.
  - Use deterministic ordering and a stable continuation cursor when the result cap is reached.
  - Return typed `not_ready`, `inference_unavailable`, `model_mismatch`, `invalid_query`, and `canceled` failures.
  - Keep advanced ranking details hidden unless requested.
  - Never write query text or returned excerpts to logs, browser persistence, or another user's cache.

  Validation:
  - Black-box search test with known lexical and semantic results.
  - Playwright coverage of query, filters, results, empty state, and failure state.
  - Contract tests for query limits, cancellation, deterministic ordering, pagination, and inference-identity mismatch.

- [ ] [F004] (P1) {F001,F002,F003} Complete user-owned replacement, restart, and deletion contracts
  Goal:
  Make each user's OpenAI archive lifecycle safe across replacement, interruption, restart, and deletion.

  Requirements:
  - Keep the authenticated user's active generation searchable while that user's replacement generation builds.
  - Persist progress checkpoints and resume an interrupted build without dual reads or duplicate vector rows.
  - Reconcile receiving, building, failed, and orphaned staging directories within each user workspace at startup.
  - Commit generation readiness and the active pointer in one transaction, then delete the obsolete generation after successful activation.
  - Keep one user/provider generation lease so concurrent servers or jobs cannot mutate the same library.
  - Replay ordered server-sent events after reconnect without duplicating state transitions.
  - Expose explicit cancellation for a building generation and explicit confirmation for full provider deletion.
  - Treat a changed model, endpoint boundary, dimensions, prefix, builder version, or corpus policy as a new generation identity that requires reindexing.
  - Delete every database, WAL/SHM file, vector, cache, source archive, and temporary upload on library deletion.
  - Never log conversation content or search queries.

  Validation:
  - Black-box restart, failed replacement, successful replacement, and complete deletion scenarios.
  - Crash-point tests around checkpoint, readiness, active-pointer commit, and obsolete-generation cleanup.
  - Concurrency test proving a second server or builder cannot mutate the same user's active library while independent users can progress safely.
  - Filesystem audit proving canceled, failed, replaced, and deleted generations leave no private payload behind.
  - Cross-user tests prove deletion and restart reconciliation never traverse another user's workspace.

- [ ] [F005] (P1) {B001,B002,F004,F011,I012} Publish the first canonical authenticated web release
  Goal:
  Deliver the anonymous static guide frontend and shared-authenticated provider applications as one repository-owned web release.

  Requirements:
  - Seal one GitHub Pages artifact containing the anonymous catalog, provider guides, authenticated application bundle, and public browser configuration.
  - Build and publish one Go API container containing the provider services and current server-owned inference boundary.
  - Declare one Download Your Data TAuth tenant, exact session and refresh cookies, gateway route, persistent storage mount, health check, and runtime assets beneath `.mprlab/deploy/`.
  - Use the literal `mpr-ui@latest` contract and the production profile's exact frontend, API, TAuth, OAuth, cookie, CORS, proxy, and storage values.
  - Keep `make release`, `make publish`, and user-owned `make deploy` as separate zero-argument contracts; use the sibling gateway plan targets for non-mutating proofs.
  - Remove the macOS application archive, end-user operator commands, embedded browser application, loopback-only production contract, and Pages download landing page in the same forward change.
  - Do not publish personal archives, databases, vectors, reports, caches, runtime secrets, or private deployment inventory.

  Deliverables:
  - Reproducible Pages and container artifacts tied to one source revision and manifest.
  - Complete app-owned deployment bundle and exact non-secret production profile.
  - User documentation for public guides, shared sign-in, provider upload, privacy, export, replacement, and workspace deletion.
  - Real local TAuth/provider stack and non-mutating release and deployment validation.

  Validation:
  - Black-box release smoke covers public guides, shared authentication, health, capabilities, Netflix, OpenAI, restart, replacement, cross-user isolation, and deletion.
  - Browser tests prove anonymous guide access, one login across provider applications, session restoration, and shared-shell sign-out.
  - Clean sealed gateway release, publish, and deploy plans.
  - `make ci`
  - `make release`

- [ ] [F010] (P1) {P006} Introduce the shared TAuth user and workspace boundary
  Goal:
  Give every data-analysis provider one authenticated Download Your Data user without creating provider-specific login or session systems.

  Requirements:
  - Use one app-owned `/config-ui.yaml`, one Download Your Data TAuth tenant, and the documented `mpr-ui:auth:*` lifecycle for every provider application.
  - Construct one current published TAuth session validator at backend startup from the exact deployment profile.
  - Convert validated tenant and user IDs into one immutable `AuthenticatedUser` domain value at the HTTP boundary.
  - Protect provider capabilities, snapshots, uploads, events, analytics, records, searches, exports, replacement, cancellation, and deletion with the TAuth session.
  - Require every protected service and repository operation to receive the authenticated user explicitly.
  - Scope storage, active pointers, generation IDs, caches, leases, events, exports, and deletion to `(user, provider)`.
  - Return `401` for an absent or invalid session and the canonical not-found response for an authenticated cross-user resource lookup.
  - Keep app code out of login, restoration, refresh, logout, credential exchange, cookie, storage, token, claim, and auth-status ownership.
  - Add one authenticated full-workspace deletion operation without attempting to delete the TAuth account.

  Deliverables:
  - Typed authenticated-user boundary, authorization middleware, user-scoped repositories, and complete workspace deletion.
  - Real local TAuth stack and two-user black-box authorization fixture.
  - Production profile schema containing every required origin, cookie, OAuth, CORS, proxy, port, storage, user-limit, and secret-reference literal, with unavailable inference capabilities rejected rather than inferred.

  Validation:
  - Unauthenticated protected routes return `401`.
  - A real TAuth session unlocks every provider through the same user identity.
  - Two test users cannot read, mutate, stream, export, or delete one another's resources.
  - Browser coverage proves no protected request occurs before `mpr-ui:auth:authenticated`, reload restores the workspace, and shared-shell sign-out clears app-owned state.
  - `make test`
  - `make test-browser`
  - `make ci`

- [ ] [F011] (P1) {F010,I012,F006,F007,F008} Move Netflix analysis into the authenticated user workspace
  Goal:
  Preserve the complete Netflix application while making every artifact and operation belong to the authenticated Download Your Data user.

  Requirements:
  - Replace the process-global Netflix workspace with a bounded user-scoped workspace registry and explicit user/provider repositories.
  - Keep current CSV validation, generation states, analytics, TMDB consent, matching, progress, export, replacement, cancellation, and deletion semantics.
  - Resolve every generation beneath the authenticated user's Netflix root; an opaque generation ID must never cross a user boundary.
  - Scope TMDB cache entries, checkpoints, leases, active pointers, and streamed exports to the user.
  - Keep raw exports and viewing data out of browser persistence, logs, routes, cross-user caches, and static artifacts.
  - Open Netflix from `#app/netflix` after the shared lifecycle authenticates, and return to its public guide without signing out.
  - Keep Netflix on the shared provider-application shell so later provider routes reuse the same TAuth session without another authentication implementation.

  Deliverables:
  - Authenticated Netflix application with user-scoped persistence and complete per-user deletion.
  - Updated API, browser, restart, concurrency, filesystem, and privacy coverage.

  Validation:
  - Two-user black-box scenarios prove independent imports, active generations, analytics, TMDB enrichment, exports, replacement, and deletion.
  - Browser coverage proves shared authentication, Netflix hydration, reload restoration, anonymous guide navigation, and shared-shell sign-out.
  - `make test-browser`
  - `make ci`

- [x] [F012] (P1) Add Amazon data export guide and order history workflow
  Goal:
  Provide a canonical export guide and provider workflow for downloading Amazon personal data, focusing on order history reports, digital purchases, Kindle content, and Prime Video history.

  Requirements:
  - Add `amazon` to the provider registry across supported locales with icon assets and visual step-by-step guidance.
  - Explain the exact first-party Amazon export path (Your Account → Request Your Information / Download Order Reports).
  - Document supported exports (order CSV reports, Kindle notebooks/highlights, Prime Video viewing history) and limitations.
  - Link to official Amazon Help & Customer Service privacy request documentation.
  - Provide complete accessibility, keyboard navigation, and locale resolution.

  Deliverables:
  - Localized Amazon provider guide entry, step assets, routing, and reference links.
  - Integration test fixtures and real-browser guide validation.

  Validation:
  - Browser tests verify `#guide/amazon` resolves instructions, visual captures, and official help references in all supported locales.
  - `make ci`

- [ ] [F013] (P1) Add Apple Data and Privacy export guide
  Goal:
  Deliver a step-by-step export guide for obtaining personal account archives from Apple's Data and Privacy portal.

  Requirements:
  - Add `apple` to the provider registry across supported locales with official iconography and visual steps.
  - Document navigation through `privacy.apple.com` (Request a copy of your data → Select categories: Apple Music, App Store, iCloud, Media services).
  - Clarify file delivery timeline, zip format options, and category selection rules.
  - Link to official Apple Support Data & Privacy documentation.
  - Maintain consistent MPR styling and accessible guide controls.

  Deliverables:
  - Localized Apple provider guide entry, instruction steps, visual screenshot contract, and reference links.
  - Browser test suite coverage across all supported locales.

  Validation:
  - Browser test asserts `#guide/apple` loads step images, localization strings, and verified external support links.
  - `make ci`

- [ ] [F014] (P1) Add Telegram data export guide and chat history workflow
  Goal:
  Provide a dedicated guide for exporting complete personal chat archives, media, and contacts using Telegram's desktop export tool.

  Requirements:
  - Add `telegram` to the provider registry across supported locales with verified first-party mark and step visuals.
  - Explain the Telegram Desktop export path (Settings → Advanced → Export Telegram data).
  - Distinguish JSON vs HTML export formats, media size thresholds, and private chat vs channel export options.
  - Link to official Telegram privacy FAQ and desktop guide resources.
  - Support keyboard navigation, high contrast, and responsive layout.

  Deliverables:
  - Localized Telegram provider guide entry, step assets, format explanations, and test coverage.
  - Real-browser test suite validation.

  Validation:
  - Browser tests verify `#guide/telegram` loads localized instructions, visual steps, and official documentation links.
  - `make ci`

- [ ] [F015] (P1) Add Reddit data export guide and post/comment archive workflow
  Goal:
  Deliver a canonical guide for requesting and downloading personal account history (posts, comments, saved items, upvotes, messages) from Reddit.

  Requirements:
  - Add `reddit` to the provider registry across supported locales with official iconography and visual instruction steps.
  - Detail the request workflow via `reddit.com/settings/data-request` (GDPR/CCPA Data Request).
  - Document the structure of returned CSV/JSON files (posts.csv, comments.csv, saved_posts.csv, chat_messages.csv).
  - Link to Reddit Help Center data export documentation.
  - Ensure full accessibility and locale consistency.

  Deliverables:
  - Localized Reddit provider guide entry, visual screenshot contract, routing, and reference links.
  - Playwright test coverage for all supported locales.

  Validation:
  - Browser tests verify `#guide/reddit` opens clean instructions, visual step captures, and official help links.
  - `make ci`

- [ ] [F016] (P1) Add Snapchat My Data export guide
  Goal:
  Provide a step-by-step export guide for requesting and downloading personal account archives from Snapchat's accounts portal.

  Requirements:
  - Add `snapchat` to the provider registry across supported locales with first-party mark and visual step captures.
  - Guide users through `accounts.snapchat.com` → My Data (Memories, chat history, friends list, location history, user profile).
  - Explain email verification, export link expiration, and Memories package downloading rules.
  - Link to Snapchat Support My Data documentation.
  - Provide full keyboard and screen reader accessibility.

  Deliverables:
  - Localized Snapchat provider guide entry, instruction steps, visual contract, and test suite.
  - Real-browser contract validation.

  Validation:
  - Browser tests assert `#guide/snapchat` resolves step imagery, localized content, and official support links.
  - `make ci`

- [ ] [F017] (P1) Add Spotify streaming history export guide and analysis workflow
  Goal:
  Deliver a step-by-step export guide for requesting Spotify's extended streaming history JSON archive and prepare for future listening analysis.

  Requirements:
  - Add `spotify` to the provider registry across supported locales with official brand mark and visual steps.
  - Detail the Privacy Settings data request flow (Account → Privacy settings → Download your data → Extended streaming history).
  - Document returned JSON data fields (`EndsSong`, `ms_played`, `master_metadata_track_name`, `master_metadata_album_artist_name`).
  - Explain the difference between basic account data (instant) and extended streaming history (up to 30 days).
  - Link to official Spotify Privacy Policy and support documentation.

  Deliverables:
  - Localized Spotify provider guide entry, instruction screenshot contract, and reference links.
  - Browser test coverage across supported locales.

  Validation:
  - Browser tests verify `#guide/spotify` loads instructions, visual steps, and official support references.
  - `make ci`

- [ ] [F018] (P1) Add Discord data package export guide
  Goal:
  Provide a clear export guide for requesting a personal data package from Discord's Privacy & Safety settings.

  Requirements:
  - Add `discord` to the provider registry across supported locales with official icon assets and visual step captures.
  - Explain the request workflow (User Settings → Privacy & Safety → Request all of my data).
  - Document contents of the JSON data package (messages index, server memberships, channels, friends, activity logs).
  - Link to official Discord Support data package request guide.
  - Ensure responsive presentation and full keyboard accessibility.

  Deliverables:
  - Localized Discord provider guide entry, step assets, routing, and reference links.
  - Playwright browser test coverage.

  Validation:
  - Browser test asserts `#guide/discord` loads step captures, localized instructions, and official support links.
  - `make ci`

- [ ] [F019] (P2) Add Pinterest data export guide
  Goal:
  Deliver a step-by-step export guide for requesting personal account data from Pinterest's Privacy settings.

  Requirements:
  - Add `pinterest` to the provider registry across supported locales with icon assets and visual instruction steps.
  - Document navigation through Pinterest Account Settings → Privacy and data → Request your data.
  - Detail exported categories (boards, pins, saved items, profile information, search history).
  - Link to official Pinterest Help Center data privacy documentation.
  - Maintain consistent MPR UI shell styling and accessibility.

  Deliverables:
  - Localized Pinterest provider guide entry, screenshot contract, routing, and reference links.
  - Browser test suite coverage.

  Validation:
  - Browser tests verify `#guide/pinterest` resolves visual steps, localized strings, and official help links.
  - `make ci`

- [ ] [F020] (P2) Add Steam account data export guide
  Goal:
  Provide a step-by-step export guide for downloading personal account and gameplay data from Valve's Steam Help portal.

  Requirements:
  - Add `steam` to the provider registry across supported locales with official brand mark and visual step captures.
  - Guide users through `help.steampowered.com/en/accountdata` (Steam Account Data / GDPR export).
  - Document exported categories (playtime history, purchase history, inventory items, friends list, community posts).
  - Link to official Steam Support account data documentation.
  - Ensure complete keyboard operation and accessible semantics.

  Deliverables:
  - Localized Steam provider guide entry, instruction steps, visual contract, and test suite.
  - Real-browser contract validation.

  Validation:
  - Browser tests assert `#guide/steam` resolves step imagery, localized content, and official support links.
  - `make ci`

- [ ] [F021] (P2) Add Strava bulk data export guide and activity workflow
  Goal:
  Deliver a dedicated guide for requesting and downloading full bulk archives of personal fitness and workout activities from Strava.

  Requirements:
  - Add `strava` to the provider registry across supported locales with official icon assets and visual step captures.
  - Explain the bulk export path (Settings → My Account → Download or Delete Your Account → Get Started → Request Your Archive).
  - Document archive contents (raw FIT/GPX activity files, activities.csv summary, profile data, gear list).
  - Link to official Strava Support bulk export documentation.
  - Provide full accessibility, high contrast, and responsive layout.

  Deliverables:
  - Localized Strava provider guide entry, step assets, format explanations, and test coverage.
  - Real-browser test suite validation.

  Validation:
  - Browser tests verify `#guide/strava` opens clean instructions, visual step captures, and official help links.
  - `make ci`
