# First run and local data operations

`download-your-data` is a macOS Apple Silicon application. Its browser
workspace and API are embedded in one executable and run only on loopback.
Personal exports, databases, vectors, reports, and provider caches stay beneath
the private local data root.

## Verify and unpack the release

Download both the macOS arm64 archive and its `.sha256` file from the same
GitHub Release. In the download directory, verify and unpack them:

```bash
shasum -a 256 -c download-your-data_v1.0.0_darwin_arm64.tar.gz.sha256
tar -xzf download-your-data_v1.0.0_darwin_arm64.tar.gz
cd download-your-data_v1.0.0_darwin_arm64
./download-your-data version
```

Use the filenames from the selected release when its version differs from the
example.

## Start the local workspace

Start the process and open its loopback URL:

```bash
./download-your-data serve
open http://127.0.0.1:8787
```

The default data root is `~/.download-your-data`. To select another location,
set one absolute owner-only directory before the first start:

```bash
DOWNLOAD_YOUR_DATA_DATA_DIR=/absolute/private/path \
  ./download-your-data serve
```

The application rejects non-loopback listen addresses, unsafe data roots,
cross-origin browser requests, and unsupported persisted shapes.

## Prepare LM Studio

Install and start LM Studio separately. The application expects its
OpenAI-compatible API at `http://127.0.0.1:1234/v1`.

Load the default embedding model and alias:

```bash
lms load text-embedding-nomic-embed-text-v1.5 \
  --identifier download-your-data-embedding \
  --ttl 3600 \
  --yes
lms server start
```

Definition verification is optional. Load an instruction model of your choice
with the alias `download-your-data-verifier` before using `definitions
--verify`.

The local inference server is a runtime dependency and is not included in the
release archive.

## Import and search an OpenAI export

Stop the browser server before running an operator command against the same
data root:

```bash
./download-your-data inspect ~/Downloads/openai-export.zip
./download-your-data import ~/Downloads/openai-export.zip
./download-your-data index build
./download-your-data search \
  --query "anime" \
  --output reports/anime.json
```

Reports are written beneath the private data root. The browser and every
operator command use the same archive database and runtime configuration.

## Netflix import and optional enrichment

The browser accepts Netflix's per-profile Viewing activity CSV. Raw import and
analytics remain local. TMDB enrichment is a separate explicit action that
sends only unique derived title queries.

To enable it, stop the server and restart it with the TMDB Read Access Token:

```bash
DOWNLOAD_YOUR_DATA_TMDB_READ_TOKEN=your-read-access-token \
  ./download-your-data serve
```

The token stays in the process and is not logged, persisted, returned to the
browser, or included in release artifacts.

## Backup, replacement, and deletion

Stop the application before backup. Copy the complete private data root as one
unit so its databases, vectors, active-generation pointers, reports, and caches
remain consistent.

Do not merge files from different backups. To restore, place one complete
backup at the configured data-root path with owner-only permissions before
starting the same released application.

Conversation data lives under `<data-root>/openai`. Netflix provider state lives
under `<data-root>/providers/netflix`. Use the browser's explicit replacement,
cancellation, and complete provider-deletion actions for Netflix. Archive and
reimport is the only supported response to a rejected foreign or obsolete
conversation schema.

To remove every application-owned item, stop the process, verify the configured
data-root path, and move that one directory to Trash. Never delete a broad home
or filesystem path.

## Troubleshooting

- `address_not_loopback`: remove the address override or use a `127.0.0.1`
  address.
- `invalid_data_root`: choose an absolute, owner-only directory that is not the
  home directory, a filesystem root, or a symlink escape.
- An embedding readiness failure: load
  `text-embedding-nomic-embed-text-v1.5` with the
  `download-your-data-embedding` alias and confirm the LM Studio server is
  running.
- Port `8787` already in use: stop the other process or set another loopback
  address with `DOWNLOAD_YOUR_DATA_ADDRESS`.
- A rejected archive schema: archive the complete old data root and reimport
  into a new current data root. The application does not repair or dual-read
  obsolete persisted shapes.
