# download_your_data

`download_your_data` is a local-first application for obtaining and working with personal data exports. The canonical application is served by a Go process bound to the local machine; personal data is not uploaded to a hosted service.

The repository owns its complete conversation archive engine: OpenAI export inspection, branch-preserving import, SQLite storage, local embeddings, hybrid semantic and lexical retrieval, definition-request analysis, and reproducible reports. There is no external ChatIndex service or module dependency.

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

## Validate

```bash
make ci
```

The full gate checks formatting, Go static analysis, public HTTP behavior, and the application through a real browser.

## Conversation archive operator CLI

The target-owned CLI remains available while the browser archive workflows are built:

```bash
make build-chatindex
./build/chatindex inspect ~/Downloads/openai-export.zip
./build/chatindex import --db ~/.download-your-data/archive.db ~/Downloads/openai-export.zip
./build/chatindex index build --db ~/.download-your-data/archive.db
./build/chatindex search --db ~/.download-your-data/archive.db --query "anime"
```

Run its complete deterministic workflow with:

```bash
make smoke-chatindex
```

The local inference endpoint defaults to LM Studio at `http://127.0.0.1:1234/v1`. Override it with `CHATINDEX_INFERENCE_BASE_URL`; non-loopback inference still requires explicit authorization at the operation boundary.
