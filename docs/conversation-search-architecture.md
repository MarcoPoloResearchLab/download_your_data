# Conversation Archive Engine Architecture

This engine is owned by `download_your_data`. It has no external ChatIndex module, service, repository, or subprocess boundary. The target-owned operator CLI and the local web backend consume the same packages and persisted contracts.

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
       search_document local embeddings
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

Both inference operations use OpenAI-compatible HTTP shapes but default to the loopback server at `http://127.0.0.1:1234/v1`:

- embeddings use `/v1/embeddings`, provider label `lmstudio`, model alias `chatindex-nomic`, and 768 dimensions
- optional verification uses `/v1/chat/completions`, model alias `chatindex-verifier`, and strict JSON Schema output

The aliases are resolved by the local inference server. API keys are optional and are read only from an explicitly named environment variable. A non-loopback endpoint must be supplied explicitly and is reported as a remote inference boundary before text is sent.

`CHATINDEX_INFERENCE_BASE_URL` changes the default LM Studio server for local inference. Command flags take precedence: `index build --base-url`, `search --base-url`, `embed --base-url`, `definitions --base-url` for semantic prototype generation, and `definitions --verify-base-url` for verification. Non-loopback endpoints require the additional `--allow-remote` authorization gate.

Conversation indexing uses `search_document: ` and query embedding uses `search_query: `. A synthetic, non-user readiness request validates the loaded model and dimensions before a search index row is created. Query vectors are cached by model, effective endpoint, query prefix, and normalized query.

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

Normal embedding runs fill missing rows. `--refresh-stale` also rebuilds the contextual search text, compares its hash with the stored row, and appends a replacement vector only when the prepared input changed.

Conversation retrieval uses separate schema-v4 `search_indexes`, `search_documents`, `search_documents_fts`, and `query_embedding_cache` tables. Its index identity contains both asymmetric prefixes, provider, model, dimensions, endpoint, builder version, and corpus policy. This prevents classification vectors from being selected for retrieval.

Search document hashes cover prepared text and filter-relevant metadata. Index rebuilds append replacements only for changed documents, delete documents that are no longer eligible, and become ready only after stored and desired document counts reconcile.

An explicit `index build --rebuild` preflights the requested model before atomically deleting the named index's database state and removing its old vector file. This is the canonical path for an intentional retrieval-identity change; incompatible vectors are never mixed.

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
