# Netflix Provider Incorporation Plan

Status: accepted planning contract

Date: 2026-07-28

Source: `/Users/tyemirov/Development/netflix` at `e4079718730533aa15141b567fa378def66b1265`

Target: `/Users/tyemirov/Development/download_your_data`

## Outcome

`download_your_data` becomes the sole maintained owner of the Netflix viewing-history workflow. One local Go process, one product executable, and one browser application own Netflix import, local analysis, optional TMDB enrichment, dashboarding, enriched CSV export, replacement, restart, cancellation, and deletion.

The target must not depend on the standalone checkout through a Go module, local replacement, subprocess, HTTP sidecar, copied database, compatibility package, or runtime filesystem path. The standalone repository remains unchanged until target-owned parity is released and the operator explicitly approves its removal.

## First Canonical Scope

The first Netflix provider accepts the per-profile viewing-activity CSV obtained from Netflix's **Viewing activity → Download all** workflow. Netflix documents that workflow in its [Viewing history help](https://help.netflix.com/en/node/101917).

The full-account personal-information request at `https://www.netflix.com/account/getmyinfo` is a separate export contract and is not accepted by this first importer. The UI must name the supported input precisely instead of implying that every file in the full Netflix account archive is supported.

The importer validates file content rather than depending on a filename. The current accepted schema is one unambiguous `Title` column and one unambiguous `Date` column with every row validated before activation. A changed Netflix export shape must be deliberately adopted as a new current contract; it must not be guessed through aliases or best-effort column selection.

## Canonical Product Decisions

1. Raw viewing-history import and raw analytics require no third-party service and never leave the local machine.
2. TMDB enrichment is an explicit, separately initiated operation. The app discloses that unique derived title queries will be sent to TMDB; viewing dates, profile information, the source CSV, and complete viewing rows are never sent.
3. A missing TMDB credential does not make the local application or raw Netflix provider invalid. It is a typed `not_configured` capability state, and an enrichment request is rejected until configured.
4. The TMDB read token is server-only configuration named `DOWNLOAD_YOUR_DATA_TMDB_READ_TOKEN`. The browser receives only a configured/not-configured capability value.
5. TMDB authentication uses the current Bearer-token contract, not an API key in a query string. Production calls use the fixed official HTTPS API origin; tests inject a local fake server.
6. The application owns concurrency, rate limiting, retry, and cancellation. Users do not choose worker counts. Retries honor `Retry-After`, remain bounded, and stop with the request context.
7. A raw generation is activated before optional enrichment. Enrichment builds a replacement generation from the active raw generation so the usable library remains available until the complete enriched replacement is ready.
8. Matching has closed `matched`, `review`, and `unmatched` outcomes. Ambiguous or low-confidence candidates never become accepted metadata merely because they are popular.
9. Ready analytics label Netflix rows as activity entries or plays, not completed views. Raw titles and local calendar dates are preserved exactly; derived title identity is versioned and auditable.
10. The browser loads no third-party scripts, styles, fonts, or charts. TMDB attribution appears in the product Credits surface using approved assets and the notice required by the current [TMDB FAQ](https://developer.themoviedb.org/docs/faq).

### Implemented TMDB boundary identities

The current target-owned enrichment contracts are:

| Boundary | Current identity |
| --- | --- |
| Derived Netflix title | `netflix-title-v1` |
| TMDB client | `tmdb-v3-bearer-client-v1` |
| Deterministic matcher | `netflix-tmdb-matcher-v1` |
| Cache freshness | `tmdb-cache-30d-v1` |
| Cache schema | `download_your_data/1`, `netflix-tmdb-enrichment-cache-v1` |

Production uses TMDB's documented [API Read Access Token as a Bearer credential](https://developer.themoviedb.org/docs/authentication-application) and the fixed [multi-search operation](https://developer.themoviedb.org/reference/search-multi). One response is limited to 2 MiB, one cache result to 256 KiB, one search to 20 candidates, concurrent title work to four workers, request pacing to four requests per second, attempts to three, and `Retry-After` to 30 seconds. These values are product constants, not user settings.

`make eval-netflix-matcher` is the named release gate. The initial 11-case synthetic corpus records five matched, three review, and three unmatched outcomes, with accepted-match precision `1.000` and expected-accept recall `1.000`. The gate requires precision `1.00` and recall `0.90`; popularity is retained only as source evidence and never participates in scoring.

## Source Capability Disposition

| Standalone capability | Target disposition |
| --- | --- |
| Viewing-history CSV parser | Rebuild under the target provider domain with strict edge validation, local-date types, cancellation, bounded input, and synthetic public-contract tests. |
| Base-title derivation | Replace the colon-truncating heuristic with a versioned title-identity contract evaluated against episodic, film, punctuation, localized, ambiguous, and negative fixtures. |
| TMDB search and details client | Keep the capability behind one injected, server-owned client using Bearer authentication, typed responses, bounded bodies, contextual errors, and fake-server coverage. |
| Concurrent enrichment | Keep bounded concurrency, but replace user-controlled workers, uncancelable token waits, unconditional sleeps, and partial-success ambiguity. |
| TMDB cache | Persist under the private configured data root with an explicit cache identity and deletion contract. Do not copy or ship `netflix_cache.sqlite`. |
| Dashboard aggregation | Preserve all maintained measures while correcting date validation, labels, deterministic ordering, match coverage, empty outcomes, and accessibility summaries. |
| Standalone web server | Delete at the target boundary. Routes, temp-file cookies, templates, CDN assets, and the second listen address are not incorporated. |
| Standalone Bootstrap/Chart.js dashboard | Recompose as checked ES modules inside the shared application shell with self-owned assets and the MPR operator visual language. |
| `tmdbenrich` CLI | Preserve useful inspection, enrichment, and export operations as provider-scoped commands in the single `download-your-data` executable. Do not ship a second binary. |
| Tracked SQLite cache | Do not import it. The source database is empty, is a generated runtime shape, and is not a product artifact. |

## Provider Domain

The target-owned domain should live beneath `internal/providers/netflix` and expose narrow validated types:

- `ViewingActivity`: immutable raw title plus validated local viewing date.
- `TitleIdentity`: raw title, derived searchable title, derivation version, and stable opaque identity.
- `TMDBMatch`: closed match status, media type, TMDB ID when accepted, normalized score evidence, and matcher version.
- `TitleMetadata`: genres, release date, runtime, original language, rating counts, countries, series counts, accepted matched title, and description.
- `NetflixGeneration`: opaque ID, source generation when derived, analysis level (`local` or `tmdb`), lifecycle state, counts, timestamps, and failure payload.
- `NetflixAnalytics`: deterministic filtered measures and chart-ready series.

Private persistence owns viewing rows, title identities, match outcomes, metadata snapshots, cache provenance, progress checkpoints, and one atomic active-generation pointer. Source CSV bytes are removed as soon as the validated reader closes. A ready generation is self-contained and reproducible without the source checkout or source CSV.

## Lifecycle And API

The generation lifecycle is closed:

```text
receiving -> validating -> importing --------------------> ready
                                  \-> enriching ---------> ready
                 any nonterminal state ------------------> failed
```

`analysis_level=local` skips `enriching`. `analysis_level=tmdb` is created from an existing ready local generation and activates only after every title has a terminal `matched`, `review`, or `unmatched` outcome and every accepted match has complete metadata.

Canonical HTTP surface:

| Method and path | Contract |
| --- | --- |
| `GET /api/providers/netflix` | Active/building generation snapshot, capabilities, counts, and typed last failure. |
| `POST /api/providers/netflix/generations` | Create either an upload generation or a TMDB replacement from a declared source generation. |
| `PUT /api/providers/netflix/generations/{generationID}/viewing-activity` | Stream one bounded CSV into a receiving generation. |
| `GET /api/providers/netflix/generations/{generationID}/events` | Ordered resumable progress events owned by the backend. |
| `GET /api/providers/netflix/generations/{generationID}/analytics` | Validated date-filtered analytics for one ready generation. |
| `GET /api/providers/netflix/generations/{generationID}/records` | Deterministic, cursor-paged activity and match rows. |
| `GET /api/providers/netflix/generations/{generationID}/export` | Stream the canonical enriched CSV without a temporary path cookie. |
| `DELETE /api/providers/netflix/generations/{generationID}` | Cancel and remove one non-active generation. |
| `DELETE /api/providers/netflix` | Delete the complete provider library after typed explicit confirmation. |

All mutations use the shared loopback Host, Origin, CSRF, lease, data-root, and owner-only permission contracts. API payloads use opaque identifiers; filenames and raw titles never appear in route segments or logs.

## User Experience

### Application shell

Replace the wrapping platform-link masthead and marketing hero with a compact provider catalog and workspace shell:

- centered `960px` catalog and `1180px` provider workspace;
- compact header with product identity, provider switcher, language, theme, and provider state;
- dark-first charcoal surfaces, thin borders, restrained semantic accents, and small controls;
- one main work surface plus a `210px` state/action rail on desktop;
- the rail becomes an inline status panel on small screens.

Guide-only providers remain available in the catalog. Netflix and OpenAI appear as workspace-capable providers with backend-owned state chips such as `NO DATA`, `READY LOCAL`, `ENRICHING`, `READY + TMDB`, and `ACTION NEEDED`.

### Empty state and import

The Netflix empty state is a compact three-step panel:

1. Open Netflix account settings, choose a profile, open Viewing activity, and select Download all.
2. Select or drop the CSV. Show accepted content, size limit, and local-only privacy statement.
3. Validate and import. Announce progress through an accessible live region and never simulate progress.

The import panel includes keyboard-operable file selection, an exact validation failure, and one clear retry. It does not request a TMDB credential or send title data during local import.

### Ready workspace

```text
┌ Download Your Data / Netflix  [READY + TMDB]       Replace  Export  Delete ┐
├ All time  Start date  End date  Match status      2,481 activities · 96%  ┤
├───────────────────────────────────────────────────────┬────────────────────┤
│ Activities  Unique titles  Date range  Match coverage │ DATA STATE         │
│                                                       │ Active generation  │
│ Activity over time                                    │ Imported date      │
│                                                       │ Source row count   │
│ Top genres                 Weekday rhythm             │                     │
│                                                       │ TMDB               │
│ Media / language / year    Top titles                 │ Configured         │
│                                                       │ Privacy disclosure │
│ Match quality: matched · review · unmatched           │ Enrich / Cancel    │
└───────────────────────────────────────────────────────┴────────────────────┘
```

The workspace has three compact views:

- **Overview:** activity count, unique titles, date range, monthly activity, weekday rhythm, top titles, and match coverage.
- **Catalog:** media types, genres, genres by viewing year, original languages, origin countries, release years, ratings, runtimes, seasons, and episodes.
- **Match quality:** deterministic matched/review/unmatched counts and a paged evidence table. Review and unmatched rows remain visible but do not silently acquire metadata.

Date filters affect every metric and chart from one source of truth. Every chart has a concise text summary and an accessible data-table alternative. Replace, TMDB enrichment, CSV export, cancellation, and full deletion remain separate controls.

### Enrichment

The state rail shows whether TMDB is configured and explains exactly what crosses the boundary. Starting enrichment requires a deliberate action after the raw generation is ready. The raw generation remains active while the replacement builds.

If TMDB is not configured, the UI gives the concrete server configuration name and restart instruction. It never accepts or stores the token in browser state. The Credits surface includes the approved TMDB logo, source link, and required non-endorsement notice.

## Privacy, Security, And Data Retention

- Apply bounded request-body, CSV-row, field-length, date-range, title-count, working-disk, response-body, concurrency, and retry limits.
- Store provider databases, caches, staging files, and exports only beneath the validated private data root.
- Do not write source data to shared `os.TempDir`, filenames to logs, title strings to logs, chart payloads to logs, or arbitrary filesystem paths to cookies.
- Stream CSV parsing and export; never expose a server filesystem path to the browser.
- Keep the TMDB token out of URLs, browser payloads, logs, reports, and persisted provider data.
- Treat TMDB request failures as a failed replacement generation. Do not activate partially enriched data.
- Treat `review` and `unmatched` as valid terminal matching outcomes with explicit coverage, not transport failures.
- On provider deletion, remove generation databases, WAL/SHM files, TMDB cache, progress journals, exports, and staging data.

## Delivery Sequence

1. **I007 — Netflix domain incorporation:** target-owned CSV, title identity, aggregation, synthetic fixtures, and behavioral inventory; no network or UI.
2. **I008 — TMDB boundary and matching quality:** server-only config, injected client, rate/retry/cancellation, cache, match evaluation, attribution contract, and deterministic fake-server tests.
3. **F006 — Local Netflix generation lifecycle:** private persistence, upload API, raw analytics, progress, atomic activation, replacement-safe storage, cancellation, and deletion.
4. **F007 — TMDB generation and export lifecycle:** explicit consent, enriched replacement, completeness, cache provenance, enriched CSV, restart, and failure behavior.
5. **F008 — Netflix provider workspace:** catalog entry in all locales, import flow, progress, dashboard, match-quality view, controls, Credits, accessibility, responsive behavior, and no-external-browser-network proof.
6. **I009 — Single-executable operator parity:** provider-scoped Netflix inspect, enrich, and export commands backed by the same packages and configuration.
7. **M409 — Standalone checkout retirement:** prove independent target parity and release, resolve any untracked/private data, then request explicit approval before removing `/Users/tyemirov/Development/netflix`.

## Completion Gate

Netflix incorporation is complete only when:

- every maintained source capability is present through the target's browser or single product executable;
- `go list -m all`, repository search, builds, tests, and runtime checks have no dependency on `github.com/tyemirov/netflix` or `/Users/tyemirov/Development/netflix`;
- a synthetic viewing-history CSV passes import, raw analytics, fake-TMDB enrichment, restart, replacement, export, cancellation, and deletion through public entry points;
- matching evaluation meets its recorded precision and review-coverage thresholds;
- browser tests cover empty, validating, importing, ready-local, not-configured, enriching, ready-enriched, review, failure, replace, export, and delete states;
- browser network assertions show no third-party frontend request;
- `make ci` passes with no real TMDB call or secret;
- a released target artifact passes without the standalone checkout;
- the operator separately approves any destructive removal of the standalone directory.
