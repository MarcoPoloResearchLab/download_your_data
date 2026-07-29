# download_your_data

`download_your_data` is a local-first application for obtaining and working with personal data exports. The canonical application is served by a Go process bound to the local machine; personal data is not uploaded to a hosted service.

The repository owns its complete conversation archive engine: OpenAI export inspection, branch-preserving import, SQLite storage, local embeddings, hybrid semantic and lexical retrieval, definition-request analysis, and reproducible reports. There is no external archive-engine service, module, subprocess, or filesystem dependency.

## Requirements

- Go 1.26.1 or later
- Node.js with `npx` for browser validation

## Run locally

```bash
make run
```

Open `http://127.0.0.1:8787`.

The listen address can be changed to another loopback address:

```bash
DOWNLOAD_YOUR_DATA_ADDRESS=127.0.0.1:9000 make run
```

Non-loopback bind addresses are rejected because the first canonical release is local-only.

Application state defaults to `~/.download-your-data`. To use another location, provide one absolute owner-only directory:

```bash
DOWNLOAD_YOUR_DATA_DATA_DIR=/absolute/private/path make run
```

The process creates the data root and every application-owned subdirectory with mode `0700`; databases, vectors, reports, caches, and other files use mode `0600`. Relative paths, filesystem roots, the user home itself, permissive existing directories, symbolic-link escapes, and application output paths outside this root are rejected.

## Validate

```bash
make ci
```

The full gate checks formatting, Go static analysis, public HTTP behavior, and the application through a real browser.

## Product operator commands

The same product executable owns the browser server and conversation archive operations:

```bash
make build
./build/download-your-data serve
./build/download-your-data inspect ~/Downloads/openai-export.zip
./build/download-your-data import ~/Downloads/openai-export.zip
./build/download-your-data index build
./build/download-your-data search --query "anime" --output reports/anime.json
```

All operator commands use the same validated runtime configuration as the local server. The sole conversation database is `<data-root>/openai/archive.db`; `--output` values are paths relative to the private data root.

Run its complete deterministic workflow with:

```bash
make smoke-command
```

The local inference endpoint defaults to LM Studio at `http://127.0.0.1:1234/v1`. `DOWNLOAD_YOUR_DATA_INFERENCE_BASE_URL` is the only endpoint override. It accepts a normalized HTTP or HTTPS server URL without credentials, query strings, or fragments. A remote endpoint also requires the explicit process-level authorization:

```bash
DOWNLOAD_YOUR_DATA_INFERENCE_BASE_URL=https://inference.example.com/v1 \
DOWNLOAD_YOUR_DATA_INFERENCE_BOUNDARY=authorized-remote \
./build/download-your-data index build
```

The browser cannot override the configured inference URL. The local HTTP server accepts only loopback hosts and same-origin browser requests, requires its per-process CSRF token for mutation requests, and does not enable cross-origin access. `GET /api/capabilities` reports the current non-secret runtime boundary, model identity, readiness state, archive limits, and data-root readiness.

Conversation databases use the sole first-release identity `download_your_data/1` and schema contract `openai-conversation-archive-1`. A brand-new empty database is initialized with that exact minimized schema. Any nonempty database with a different identity, version, contract, or incomplete object set is rejected with an archive-and-reimport instruction; the application does not read, migrate, or repair another persisted shape.

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

The browser opens Netflix as the first workspace-capable provider in the compact catalog. Its Overview, Catalog, and Match quality views share the same server-owned filters and expose import, enrichment, retry, cancellation, replacement, enriched export, and complete deletion as separate actions. Run `make test-browser` for both the unconfigured real-server path and the deterministic configured fake-TMDB lifecycle.

## Optional Netflix metadata

Raw Netflix viewing-activity import and analytics remain local and do not require TMDB. Optional metadata enrichment is a separate, explicitly initiated operation that sends only unique derived title queries to TMDB. It never sends viewing dates, source CSV bytes, profile data, or complete activity rows.

Configure the server-owned TMDB API Read Access Token before starting the application:

```bash
DOWNLOAD_YOUR_DATA_TMDB_READ_TOKEN=your-read-access-token make run
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
