# download_your_data

`download_your_data` is a local-first application for obtaining and working with personal data exports. The canonical application is served by a Go process bound to the local machine; personal data is not uploaded to a hosted service.

The repository owns its complete conversation archive engine: OpenAI export inspection, branch-preserving import, SQLite storage, local embeddings, hybrid semantic and lexical retrieval, definition-request analysis, and reproducible reports. There is no external archive-engine service, module, subprocess, or filesystem dependency.

## Requirements

- Go 1.26.1 or later
- Node.js with `npx` for browser validation
- Git, Python 3, and the GitHub CLI for release publication and Pages deployment
- Network access to `cdn.jsdelivr.net` for the shared MPR header and footer

## Run locally

```bash
make up
```

Open `http://127.0.0.1:8787`.

`make up` remains attached to its terminal and records the exact server process
owned by this checkout. Stop that process with `Ctrl-C` in the same terminal or,
from another terminal, run:

```bash
make down
```

`make down` is idempotent and refuses to stop a process whose identity does not
match the server started by `make up`.

The listen address can be changed to another loopback address:

```bash
DOWNLOAD_YOUR_DATA_ADDRESS=127.0.0.1:9000 make up
```

Non-loopback bind addresses are rejected because the first canonical release is local-only.

The page loads `mpr-ui@latest` from jsDelivr to render the shared MPR Lab header
and footer. Those asset requests carry ordinary browser request metadata but no
imported files, provider records, search queries, screenshots, charts, or other
personal data. The application has no authentication surface, so it uses
`mpr-ui` only as declarative shell chrome and does not fabricate TAuth
configuration or a sign-in control.

Application state defaults to `~/.download-your-data`. To use another location, provide one absolute owner-only directory:

```bash
DOWNLOAD_YOUR_DATA_DATA_DIR=/absolute/private/path make up
```

The process creates the data root and every application-owned subdirectory with mode `0700`; databases, vectors, reports, caches, and other files use mode `0600`. Relative paths, filesystem roots, the user home itself, permissive existing directories, symbolic-link escapes, and application output paths outside this root are rejected.

## Validate

```bash
make ci
```

The full gate checks formatting, Go static analysis, public HTTP behavior, and the application through a real browser.

## Release, publish, and deploy

The repository owns one fixed three-stage operator lifecycle:

```bash
make release
make publish
make deploy
```

`make release` runs `make ci`, builds the macOS arm64 executable twice to prove deterministic output, packages the executable with the first-run documentation and license, writes its SHA-256 checksum, prepares the static download site, and seals every payload beneath `.git/mprlab-release`. It creates one CHANGELOG-only release commit and annotated tag but performs no remote write.

`make publish` publishes that exact commit, tag, manifest, application archive, checksum, and static-site archive as a non-draft GitHub Release. It does not rebuild.

`make deploy` activates the published static download and documentation page at
`https://dyd.mprlab.com/` through branch-based GitHub Pages, configures the
sealed custom domain, and enforces HTTPS. The deployed page has no application
API, authentication, runtime secret, or personal-data upload path; the released
application itself remains loopback-only.

See [the first-run guide](docs/first-run.md) for artifact verification, LM Studio setup, local data operations, backup, replacement, deletion, and troubleshooting. The app-owned deployment topology and gateway manifest live under `.mprlab/deploy/`.

## Product operator commands

The same product executable owns the browser server, conversation archive operations, and Netflix provider operations:

```bash
make build
./build/download-your-data serve
./build/download-your-data inspect ~/Downloads/openai-export.zip
./build/download-your-data import ~/Downloads/openai-export.zip
./build/download-your-data index build
./build/download-your-data search --query "anime" --output reports/anime.json
./build/download-your-data netflix inspect
```

All operator commands use the same validated runtime configuration as the local server. The sole conversation database is `<data-root>/openai/archive.db`; `--output` values are paths relative to the private data root.

Run its complete deterministic workflow with:

```bash
make smoke-command
make smoke-netflix-command
```

The local inference endpoint defaults to LM Studio at `http://127.0.0.1:1234/v1`. `DOWNLOAD_YOUR_DATA_INFERENCE_BASE_URL` is the only endpoint override. It accepts a normalized HTTP or HTTPS server URL without credentials, query strings, or fragments. A remote endpoint also requires the explicit process-level authorization:

```bash
DOWNLOAD_YOUR_DATA_INFERENCE_BASE_URL=https://inference.example.com/v1 \
DOWNLOAD_YOUR_DATA_INFERENCE_BOUNDARY=authorized-remote \
./build/download-your-data index build
```

The browser cannot override the configured inference URL. The local HTTP server accepts only loopback hosts and same-origin browser requests, requires its per-process CSRF token for mutation requests, and does not enable cross-origin access. `GET /api/capabilities` reports the current non-secret runtime boundary, model identity, readiness state, archive limits, and data-root readiness.

Conversation databases use the sole first-release identity `download_your_data/1` and schema contract `openai-conversation-archive-1`. A brand-new empty database is initialized with that exact minimized schema. Any nonempty database with a different identity, version, contract, or incomplete object set is rejected with an archive-and-reimport instruction; the application does not read, migrate, or repair another persisted shape.

## OpenAI conversation analysis

The OpenAI catalog card has a top-right **Data analysis** action in addition to
its permanent export guide. The action opens a local workspace backed by the
same private archive database, complete ready index, retrieval engine, query
cache, and inference configuration as the operator commands.

With a ready index, the workspace supports hybrid, semantic, and exact-term
conversation search, an archived-conversation filter, bounded result counts,
and supporting excerpts. Without a ready archive and index, it displays the
exact import and indexing commands. Browser ZIP upload is not part of this
surface; import and index construction remain explicit operator operations.

## Netflix viewing activity

The Netflix provider accepts only the current per-profile Viewing activity CSV with the exact `Title,Date` column set in either order. It does not accept the full Netflix personal-information archive. A local import uses these canonical routes:

- `GET /api/providers/netflix` for the provider snapshot and current limits.
- `POST /api/providers/netflix/generations` with `{"analysis_level":"local"}` to create one receiving generation.
- `PUT /api/providers/netflix/generations/{generationID}/viewing-activity` with `Content-Type: text/csv` to stage the CSV.
- `GET /api/providers/netflix/generations/{generationID}/events` for ordered resumable progress.
- `GET /api/providers/netflix/generations/{generationID}/analytics` and `/records` for declared ready results. Both use one optional `start_date`, `end_date`, and `match_status` filter; match status accepts only `matched`, `review`, or `unmatched`, and record cursors are bound to the exact filter.
- `GET /api/providers/netflix/generations/{generationID}/export` to stream the canonical enriched CSV from a declared ready TMDB generation.
- `DELETE /api/providers/netflix/generations/{generationID}` to cancel and remove a non-active generation.
- `DELETE /api/providers/netflix` with `{"confirmation":"delete-netflix-provider"}` to remove the complete provider library and TMDB cache.

The server accepts at most one building generation, keeps the existing ready generation active while a replacement builds, and swaps the active pointer only after it revalidates every record, title identity, date, count, analytics result, and artifact hash. Upload bytes are removed after import success or failure; only a complete staged upload needed to resume a nonterminal generation survives an orderly process restart.

Provider state uses the sole current `netflix-generation-library-v1` contract at `<data-root>/providers/netflix/library.json`. Immutable ready records and analytics use `netflix-generation-records-v1` and `netflix-generation-analytics-v1` below `<data-root>/providers/netflix/generations/{generationID}`; record cursors use `netflix-record-cursor-v3`. The provider holds an operating-system lease for its entire lifetime, and every directory and file remains owner-only.

The browser exposes both a permanent visual Netflix download guide and the first workspace-capable provider in the compact catalog. The guide remains reachable from the catalog and workspace regardless of provider state. Its Overview, Catalog, and Match quality views share the same server-owned filters and expose import, enrichment, retry, cancellation, replacement, enriched export, and complete deletion as separate actions. Run `make test-browser` for both the unconfigured real-server path and the deterministic configured fake-TMDB lifecycle.

The single product executable also exposes the same provider library to an operator. Stop the local server first so the command can acquire the provider lease, and import a Viewing activity CSV through the browser before enriching:

```bash
./build/download-your-data netflix inspect
DOWNLOAD_YOUR_DATA_TMDB_READ_TOKEN=your-read-access-token \
  ./build/download-your-data netflix enrich --locale en-US
./build/download-your-data netflix export \
  --output exports/netflix-viewing-activity.csv
```

`netflix inspect` reports only bounded state and counts. `netflix enrich` is the deliberate authorization to send the active generation's unique derived title queries to TMDB, emits persisted progress without titles or viewing dates, and activates only a complete replacement. `netflix export` writes the canonical enriched CSV atomically beneath the private data root; `--generation` can select a retained ready TMDB generation instead of the active one. These commands have no separate worker, rate, timeout, credential, or cache settings.

## Optional Netflix metadata

Raw Netflix viewing-activity import and analytics remain local and do not require TMDB. Optional metadata enrichment is a separate, explicitly initiated operation that sends only unique derived title queries to TMDB. It never sends viewing dates, source CSV bytes, profile data, or complete activity rows.

Configure the server-owned TMDB API Read Access Token before starting the application:

```bash
DOWNLOAD_YOUR_DATA_TMDB_READ_TOKEN=your-read-access-token make up
```

The token is accepted only from `DOWNLOAD_YOUR_DATA_TMDB_READ_TOKEN`. It stays in the Go process and is never returned to the browser, placed in a URL, logged, reported, or persisted. Production requests use Bearer authentication against TMDB's fixed official HTTPS API origin. The browser capability payload reports only whether enrichment is configured.

Enrichment is created only from the active ready-local generation with this exact request shape:

```json
{
  "analysis_level": "tmdb",
  "source_generation_id": "ng_<opaque-id>",
  "locale": "en-US",
  "tmdb_title_query_consent": "authorize-tmdb-title-queries"
}
```

This creates a separate `enriching` generation. The raw source remains active and readable until every unique derived title has a persisted `matched`, `review`, or `unmatched` outcome and every accepted match has complete metadata. Transport, protocol, cache, metadata, stale-source, and cancellation failures leave the active generation unchanged. Ordered progress and per-title owner-only checkpoints survive an orderly restart; activation removes those checkpoints atomically after final artifacts revalidate.

Enrichment uses fixed product-owned concurrency, pacing, retry, cancellation, matching, and 30-day private-cache contracts. Accepted matches must pass the deterministic quality gate:

```bash
make eval-netflix-matcher
```

The current matcher identity is `netflix-tmdb-matcher-v1`; the current client identity is `tmdb-v3-bearer-client-v1`; and the current freshness identity is `tmdb-cache-30d-v1`. Cache entries live only at `<data-root>/providers/netflix/tmdb-cache.db`. Foreign or stale cache schemas are rejected rather than read or repaired.

The product Credits surface must use an approved TMDB logo, link to [TMDB](https://www.themoviedb.org), and display: “This product uses the TMDB API but is not endorsed or certified by TMDB.”
