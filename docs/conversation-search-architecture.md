# Conversation Archive Engine Architecture

This engine is owned by `download_your_data`. It has no external module,
repository, subprocess, or filesystem dependency. The authenticated web API
and its server-owned jobs consume the same packages and persisted contracts.

## Data flow

```text
ChatGPT export ZIP
        |
        v
conversation-file discovery
        |
        v
streamed JSON array decoder
        |
        v
raw export adapter
        |
        v
normalized conversation graph
        |
        +--> SQLite graph metadata
        |
        +--> visible branch-aware user/assistant documents
                    |
                    v
       server-owned search_document embeddings
                    |
                    v
       exact vector scan + filtered FTS5
                    |
                    v
       rank fusion + conversation aggregation
                    |
                    v
       ranked conversations and excerpts
        |
        +--> contextual user-message classification documents
                    |
                    v
       definition intent + optional verifier
                    |
                    v
          accepted, review, audit reports
```

## Import boundaries

The raw ChatGPT export structs exist only in `internal/exportformat`. All downstream packages use the normalized types in `internal/domain`. This limits the impact of future export-format changes.

The top-level conversation array is streamed. One conversation and its node mapping are held in memory at a time.

## Conversation graphs

The importer does not follow only `current_node`. It imports every message node and every parent-child edge in the mapping. This preserves regenerated answers, edited branches, and historical paths.

## Message identity

The export's `message.id` is not treated as globally unique. The internal message identifier is a deterministic hash of:

- conversation ID
- mapping-node ID
- the message-occurrence identity version

The original OpenAI message ID is stored separately as `source_message_id`. Repeated source IDs therefore remain queryable as distinct conversation-node occurrences instead of overwriting one another.

## Context construction

Most user messages are embedded alone. Short messages with references such as `this`, `that`, or `it` include:

- conversation title
- immediate previous visible assistant message
- current user message
- immediate following visible assistant message

Assistant records with content type `thoughts` or `reasoning_recap` are not used as neighboring context. The context document is length-limited before being sent to an embedding endpoint.

Conversation-search documents use a separate deterministic builder. Every visible nonempty user or assistant `text` or `multimodal_text` message becomes one anchored document. The builder walks parent edges within the same conversation branch, includes at most two preceding visible messages, and always includes the conversation title and anchored role. Alternate branches therefore remain searchable without combining unrelated siblings.

## Inference boundary

Both inference operations use OpenAI-compatible HTTP shapes. Loopback is the
development default; the hosted endpoint and its authorization boundary must
come from the exact deployment profile:

- embeddings use `/v1/embeddings`, provider label `lmstudio`, model alias `download-your-data-embedding`, and 768 dimensions
- optional verification uses `/v1/chat/completions`, model alias `download-your-data-verifier`, and strict JSON Schema output

The aliases are resolved by the configured inference server. API keys are optional and are read only from an explicitly named environment variable. A non-loopback endpoint must be supplied in process configuration, paired with the closed `authorized-remote` boundary, and reported as remote before text is sent.

`DOWNLOAD_YOUR_DATA_INFERENCE_BASE_URL` is the sole server endpoint
configuration. The value is smart-constructed at startup as a normalized HTTP
or HTTPS base URL without credentials, query strings, fragments, encoded
paths, or backslash paths. A non-loopback URL is rejected unless
`DOWNLOAD_YOUR_DATA_INFERENCE_BOUNDARY=authorized-remote`; an HTTP request
cannot supply or override either value.

Conversation indexing uses `search_document: ` and query embedding uses `search_query: `. A synthetic, non-user readiness request validates the loaded model and dimensions before a search index row is created. Query vectors are cached by model, effective endpoint, query prefix, and normalized query.

## Browser projection

The authenticated browser workspace is a user-scoped projection of the archive
and retrieval engine. `GET /api/providers/openai` reports only that user's
archive counts, complete ready-index identity, supported search modes, limits,
and configured inference boundary. It never returns conversation text.

`POST /api/providers/openai/search` accepts one validated query, retrieval mode,
result limit, excerpt limit, and archive filter. Hybrid, semantic, and lexical
requests call the same `internal/retrieval` engine and current ready index.
Search text and returned excerpts exist only in the credentialed response and
current document; they are not written to logs or browser persistence.

The browser does not yet accept an OpenAI export ZIP. No unscoped command path
exists. Authenticated archive upload and index construction require the current
user-owned job lifecycle before this surface can launch.

## Persistence boundary

The conversation database has one current schema identity: owner
`download_your_data`, version `1`, contract
`openai-conversation-archive-1`. Each database lives only at
`<data-root>/users/<opaque-user-digest>/openai/archive.db`. Opening an empty
database creates the complete minimized schema in one transaction. Opening a
nonempty database validates the owner, version, contract, and every required
table and index before any archive operation begins.

A database with another identity, version, or incomplete current shape is rejected with an instruction to archive it and re-import the original OpenAI export into a new database. There are no automatic migrations, compatibility reads, or schema-repair writes.

The configured data root is an absolute, non-root, owner-only directory. All application-owned directories use mode `0700`, and databases, vectors, reports, caches, and generated files use mode `0600`. Stored paths are relative identities resolved through the private-root abstraction; absolute paths, traversal, symbolic links, and permissive existing paths are rejected.

## Vector consistency

Each embedding configuration is identified by:

- provider label
- model
- dimensions
- base URL
- task input prefix, represented in the preprocessing identity
- preprocessing version
- context-construction version

The endpoint remains part of the identity so local and explicitly configured remote vectors cannot be mixed accidentally. The same embedding model and configuration are used for message vectors and semantic prototypes.

The default Nomic prefix is `classification: `. It is applied inside the embedding client to every message and prototype input, stored on the configuration, and hashed into its preprocessing version. A prefix change therefore produces a new vector configuration.

Configurations have a two-state lifecycle. `building` means a pass was interrupted, limited, or is actively refreshing stale inputs. `ready` is written only after a complete scan finds no missing or stale messages. Semantic analysis uses the latest ready configuration unless a caller selects an ID explicitly; using a building configuration requires a separate opt-in.

Vectors are appended and flushed before their rows are committed to SQLite. At startup, bytes after the highest committed row are truncated. A vector file shorter than committed metadata is treated as corruption.

Normal embedding jobs fill missing rows. A deliberate stale refresh also rebuilds the contextual search text, compares its hash with the stored row, and appends a replacement vector only when the prepared input changed.

Conversation retrieval uses the current schema's separate `search_indexes`, `search_documents`, `search_documents_fts`, and `query_embedding_cache` tables. Its index identity contains both asymmetric prefixes, provider, model, dimensions, endpoint, builder version, and corpus policy. This prevents classification vectors from being selected for retrieval.

Search document hashes cover prepared text and filter-relevant metadata. Index rebuilds append replacements only for changed documents, delete documents that are no longer eligible, and become ready only after stored and desired document counts reconcile.

An explicit authenticated rebuild job must preflight the requested model before atomically deleting the named index's database state and removing its old vector file. This is the canonical path for an intentional retrieval-identity change; incompatible vectors are never mixed.

## Retrieval policy

General conversation search has two candidate paths:

1. Exact cosine similarity against every eligible retrieval document.
2. FTS5 BM25 ranking over the identical prepared document corpus.

Hybrid mode merges document ranks using reciprocal-rank fusion, groups candidates by conversation, applies a capped multi-hit contribution, and returns the strongest supporting excerpts. Date and archive filters are applied to both candidate paths before aggregation. Sequential buffered vector scans avoid one system call per vector while keeping exact search as the auditable source of truth.

Definition analysis remains a separate retrieval policy:

The definition intent has two candidate paths:

1. High-precision lexical rules.
2. Similarity to positive examples, constrained by similarity to negative examples.

A broad lower threshold sends uncertain cases to a review report. Optional model verification receives only retrieved candidates.

Verifier inputs are bounded per field before serialization. Failed batches use a bounded retry budget and are then recursively split, allowing a single problematic input to be identified without discarding successful work. Results are cached in SQLite by prompt version, endpoint/model identity, and the complete bounded input.

The audit artifact captures all reproducibility inputs: date boundaries, archive policy, intent configuration hash and thresholds, embedding configuration and effective endpoint, verifier controls, prompt version, and verification cache usage.

## Completeness

Exact cosine scanning is intentional. It avoids the recall loss possible with an approximate nearest-neighbor index. An approximate index can later be added as an acceleration layer while retaining exact scan as an audit mode.
