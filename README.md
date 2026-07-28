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

## Conversation archive operator CLI

The product-owned archive command remains available while its operator subcommands are consolidated into the main executable:

```bash
make build-archive
./build/download-your-data-archive inspect ~/Downloads/openai-export.zip
./build/download-your-data-archive import ~/Downloads/openai-export.zip
./build/download-your-data-archive index build
./build/download-your-data-archive search --query "anime" --output reports/anime.json
```

All operator commands use the same validated runtime configuration as the local server. The sole conversation database is `<data-root>/openai/archive.db`; `--output` values are paths relative to the private data root.

Run its complete deterministic workflow with:

```bash
make smoke-archive
```

The local inference endpoint defaults to LM Studio at `http://127.0.0.1:1234/v1`. `DOWNLOAD_YOUR_DATA_INFERENCE_BASE_URL` is the only endpoint override. It accepts a normalized HTTP or HTTPS server URL without credentials, query strings, or fragments. A remote endpoint also requires the explicit process-level authorization:

```bash
DOWNLOAD_YOUR_DATA_INFERENCE_BASE_URL=https://inference.example.com/v1 \
DOWNLOAD_YOUR_DATA_INFERENCE_BOUNDARY=authorized-remote \
./build/download-your-data-archive index build
```

The browser cannot override the configured inference URL. The local HTTP server accepts only loopback hosts and same-origin browser requests, requires its per-process CSRF token for mutation requests, and does not enable cross-origin access. `GET /api/capabilities` reports the current non-secret runtime boundary, model identity, readiness state, archive limits, and data-root readiness.

Conversation databases use the sole first-release identity `download_your_data/1` and schema contract `openai-conversation-archive-1`. A brand-new empty database is initialized with that exact minimized schema. Any nonempty database with a different identity, version, contract, or incomplete object set is rejected with an archive-and-reimport instruction; the application does not read, migrate, or repair another persisted shape.

## Optional Netflix metadata

Raw Netflix viewing-activity import and analytics remain local and do not require TMDB. Optional metadata enrichment is a separate, explicitly initiated operation that sends only unique derived title queries to TMDB. It never sends viewing dates, source CSV bytes, profile data, or complete activity rows.

Configure the server-owned TMDB API Read Access Token before starting the application:

```bash
DOWNLOAD_YOUR_DATA_TMDB_READ_TOKEN=your-read-access-token make run
```

The token is accepted only from `DOWNLOAD_YOUR_DATA_TMDB_READ_TOKEN`. It stays in the Go process and is never returned to the browser, placed in a URL, logged, reported, or persisted. Production requests use Bearer authentication against TMDB's fixed official HTTPS API origin. The browser capability payload reports only whether enrichment is configured.

Enrichment uses fixed product-owned concurrency, pacing, retry, cancellation, matching, and 30-day private-cache contracts. Accepted matches must pass the deterministic quality gate:

```bash
make eval-netflix-matcher
```

The current matcher identity is `netflix-tmdb-matcher-v1`; the current client identity is `tmdb-v3-bearer-client-v1`; and the current freshness identity is `tmdb-cache-30d-v1`. Cache entries live only at `<data-root>/providers/netflix/tmdb-cache.db`. Foreign or stale cache schemas are rejected rather than read or repaired.

The product Credits surface must use an approved TMDB logo, link to [TMDB](https://www.themoviedb.org), and display: “This product uses the TMDB API but is not endorsed or certified by TMDB.”
