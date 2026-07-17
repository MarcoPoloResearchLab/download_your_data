package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const currentSchemaVersion = "4"

const connectionPragmasSQL = `
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA synchronous = NORMAL;
PRAGMA temp_store = MEMORY;
`

const schemaV4SQL = `
CREATE TABLE IF NOT EXISTS schema_metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT INTO schema_metadata(key, value)
VALUES ('schema_version', '4')
ON CONFLICT(key) DO UPDATE SET value = excluded.value;

CREATE TABLE IF NOT EXISTS imports (
    import_id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_path TEXT NOT NULL,
    source_sha256 TEXT NOT NULL,
    parser_version TEXT NOT NULL,
    started_at_ms INTEGER NOT NULL,
    completed_at_ms INTEGER,
    status TEXT NOT NULL,
    conversations_seen INTEGER NOT NULL DEFAULT 0,
    messages_seen INTEGER NOT NULL DEFAULT 0,
    warnings_count INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS imports_source_hash_idx
ON imports(source_sha256, parser_version, status);

CREATE TABLE IF NOT EXISTS conversations (
    conversation_id TEXT PRIMARY KEY,
    title TEXT NOT NULL DEFAULT '',
    created_at_ms INTEGER,
    updated_at_ms INTEGER,
    is_archived INTEGER,
    current_node_id TEXT,
    source_import_id INTEGER NOT NULL,
    source_filename TEXT NOT NULL,
    raw_metadata_json TEXT,
    FOREIGN KEY (source_import_id) REFERENCES imports(import_id)
);

CREATE INDEX IF NOT EXISTS conversations_created_idx
ON conversations(created_at_ms);

CREATE INDEX IF NOT EXISTS conversations_archived_idx
ON conversations(is_archived);

CREATE TABLE IF NOT EXISTS messages (
    message_id TEXT PRIMARY KEY,
    source_message_id TEXT,
    conversation_id TEXT NOT NULL,
    parent_node_id TEXT,
    role TEXT NOT NULL,
    created_at_ms INTEGER,
    content_type TEXT,
    original_text TEXT NOT NULL,
    normalized_text TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    source_filename TEXT NOT NULL,
    source_node_id TEXT NOT NULL,
    extraction_warning TEXT,
    raw_metadata_json TEXT,
    FOREIGN KEY (conversation_id) REFERENCES conversations(conversation_id) ON DELETE CASCADE,
    UNIQUE (conversation_id, source_node_id)
);

CREATE INDEX IF NOT EXISTS messages_conversation_idx
ON messages(conversation_id);

CREATE INDEX IF NOT EXISTS messages_role_created_idx
ON messages(role, created_at_ms);

CREATE INDEX IF NOT EXISTS messages_source_node_idx
ON messages(conversation_id, source_node_id);

CREATE INDEX IF NOT EXISTS messages_source_message_idx
ON messages(source_message_id);

CREATE TABLE IF NOT EXISTS message_edges (
    conversation_id TEXT NOT NULL,
    parent_node_id TEXT NOT NULL,
    child_node_id TEXT NOT NULL,
    PRIMARY KEY (conversation_id, parent_node_id, child_node_id),
    FOREIGN KEY (conversation_id) REFERENCES conversations(conversation_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS message_edges_parent_idx
ON message_edges(conversation_id, parent_node_id);

DROP TABLE IF EXISTS messages_fts;

CREATE TABLE IF NOT EXISTS embedding_configs (
    config_id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    dimensions INTEGER NOT NULL,
	base_url TEXT NOT NULL,
	input_prefix TEXT NOT NULL DEFAULT '',
	vector_path TEXT NOT NULL,
	preprocessing_version TEXT NOT NULL,
	context_version TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'building' CHECK(status IN ('building', 'ready')),
	created_at_ms INTEGER NOT NULL,
	completed_at_ms INTEGER,
	UNIQUE(provider, model, dimensions, base_url, preprocessing_version, context_version)
);

CREATE INDEX IF NOT EXISTS embedding_configs_status_idx
ON embedding_configs(status, config_id);

CREATE TABLE IF NOT EXISTS embeddings (
    message_id TEXT NOT NULL,
    config_id INTEGER NOT NULL,
    vector_row INTEGER NOT NULL,
    dimensions INTEGER NOT NULL,
    search_text_hash TEXT NOT NULL,
    created_at_ms INTEGER NOT NULL,
    PRIMARY KEY (message_id, config_id),
    UNIQUE (config_id, vector_row),
    FOREIGN KEY (message_id) REFERENCES messages(message_id) ON DELETE CASCADE,
    FOREIGN KEY (config_id) REFERENCES embedding_configs(config_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS embeddings_config_idx
ON embeddings(config_id, vector_row);

CREATE TABLE IF NOT EXISTS intent_prototypes (
    config_id INTEGER NOT NULL,
    intent_name TEXT NOT NULL,
    example_hash TEXT NOT NULL,
    label TEXT NOT NULL,
    example_text TEXT NOT NULL,
    vector_blob BLOB NOT NULL,
    dimensions INTEGER NOT NULL,
    created_at_ms INTEGER NOT NULL,
    PRIMARY KEY (config_id, intent_name, example_hash),
    FOREIGN KEY (config_id) REFERENCES embedding_configs(config_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS verification_cache (
	cache_key TEXT PRIMARY KEY,
	verifier_identity TEXT NOT NULL,
	message_id TEXT NOT NULL,
	input_hash TEXT NOT NULL,
	result_json TEXT NOT NULL,
	created_at_ms INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS verification_cache_identity_idx
ON verification_cache(verifier_identity, message_id);

CREATE TABLE IF NOT EXISTS search_indexes (
	search_index_id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE,
	provider TEXT NOT NULL,
	model TEXT NOT NULL,
	dimensions INTEGER NOT NULL,
	base_url TEXT NOT NULL,
	document_prefix TEXT NOT NULL,
	query_prefix TEXT NOT NULL,
	vector_path TEXT NOT NULL,
	builder_version TEXT NOT NULL,
	corpus_policy TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'building' CHECK(status IN ('building', 'ready')),
	created_at_ms INTEGER NOT NULL,
	completed_at_ms INTEGER
);

CREATE INDEX IF NOT EXISTS search_indexes_status_idx
ON search_indexes(status, search_index_id);

CREATE TABLE IF NOT EXISTS search_documents (
	search_index_id INTEGER NOT NULL,
	document_id TEXT NOT NULL,
	conversation_id TEXT NOT NULL,
	anchor_message_id TEXT NOT NULL,
	conversation_title TEXT NOT NULL,
	role TEXT NOT NULL,
	content_type TEXT NOT NULL,
	source_text TEXT NOT NULL,
	document_text TEXT NOT NULL,
	text_hash TEXT NOT NULL,
	created_at_ms INTEGER,
	is_archived INTEGER,
	vector_row INTEGER NOT NULL,
	dimensions INTEGER NOT NULL,
	embedded_at_ms INTEGER NOT NULL,
	PRIMARY KEY(search_index_id, document_id),
	UNIQUE(search_index_id, vector_row),
	FOREIGN KEY(search_index_id) REFERENCES search_indexes(search_index_id) ON DELETE CASCADE,
	FOREIGN KEY(conversation_id) REFERENCES conversations(conversation_id) ON DELETE CASCADE,
	FOREIGN KEY(anchor_message_id) REFERENCES messages(message_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS search_documents_conversation_idx
ON search_documents(search_index_id, conversation_id);

CREATE INDEX IF NOT EXISTS search_documents_created_idx
ON search_documents(search_index_id, created_at_ms);

CREATE VIRTUAL TABLE IF NOT EXISTS search_documents_fts USING fts5(
	search_index_id UNINDEXED,
	document_id UNINDEXED,
	conversation_title,
	document_text,
	tokenize = 'unicode61'
);

CREATE TABLE IF NOT EXISTS query_embedding_cache (
	search_index_id INTEGER NOT NULL,
	query_hash TEXT NOT NULL,
	query_text TEXT NOT NULL,
	vector_blob BLOB NOT NULL,
	dimensions INTEGER NOT NULL,
	created_at_ms INTEGER NOT NULL,
	PRIMARY KEY(search_index_id, query_hash),
	FOREIGN KEY(search_index_id) REFERENCES search_indexes(search_index_id) ON DELETE CASCADE
);
`

func (store *Store) migrate(contextValue context.Context) error {
	if _, pragmaError := store.database.ExecContext(contextValue, connectionPragmasSQL); pragmaError != nil {
		return fmt.Errorf("configure SQLite connection: %w", pragmaError)
	}

	schemaVersion, schemaExists, versionError := store.readSchemaVersion(contextValue)
	if versionError != nil {
		return versionError
	}
	if schemaExists {
		switch schemaVersion {
		case "1":
			return fmt.Errorf(
				"database %s uses schema version 1, which collapsed repeated OpenAI message IDs; move or delete this database and reimport with ChatIndex 0.1.1",
				store.databasePath,
			)
		case "2":
			if migrationError := store.migrateV2ToV3(contextValue); migrationError != nil {
				return migrationError
			}
			if migrationError := store.migrateV3ToV4(contextValue); migrationError != nil {
				return migrationError
			}
		case "3":
			if migrationError := store.migrateV3ToV4(contextValue); migrationError != nil {
				return migrationError
			}
		case currentSchemaVersion:
			// The idempotent schema below repairs missing indexes and tables.
		default:
			return fmt.Errorf(
				"database %s uses unsupported schema version %q; this binary requires schema version %s",
				store.databasePath,
				schemaVersion,
				currentSchemaVersion,
			)
		}
	}

	if _, executeError := store.database.ExecContext(contextValue, schemaV4SQL); executeError != nil {
		return fmt.Errorf("apply database schema version %s: %w", currentSchemaVersion, executeError)
	}
	return nil
}

func (store *Store) migrateV3ToV4(contextValue context.Context) error {
	transaction, beginError := store.database.BeginTx(contextValue, nil)
	if beginError != nil {
		return fmt.Errorf("begin schema version 4 migration: %w", beginError)
	}
	rollbackRequired := true
	defer func() {
		if rollbackRequired {
			_ = transaction.Rollback()
		}
	}()

	statements := []string{
		`CREATE TABLE IF NOT EXISTS search_indexes (
			search_index_id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			provider TEXT NOT NULL,
			model TEXT NOT NULL,
			dimensions INTEGER NOT NULL,
			base_url TEXT NOT NULL,
			document_prefix TEXT NOT NULL,
			query_prefix TEXT NOT NULL,
			vector_path TEXT NOT NULL,
			builder_version TEXT NOT NULL,
			corpus_policy TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'building' CHECK(status IN ('building', 'ready')),
			created_at_ms INTEGER NOT NULL,
			completed_at_ms INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS search_indexes_status_idx ON search_indexes(status, search_index_id)`,
		`CREATE TABLE IF NOT EXISTS search_documents (
			search_index_id INTEGER NOT NULL,
			document_id TEXT NOT NULL,
			conversation_id TEXT NOT NULL,
			anchor_message_id TEXT NOT NULL,
			conversation_title TEXT NOT NULL,
			role TEXT NOT NULL,
			content_type TEXT NOT NULL,
			source_text TEXT NOT NULL,
			document_text TEXT NOT NULL,
			text_hash TEXT NOT NULL,
			created_at_ms INTEGER,
			is_archived INTEGER,
			vector_row INTEGER NOT NULL,
			dimensions INTEGER NOT NULL,
			embedded_at_ms INTEGER NOT NULL,
			PRIMARY KEY(search_index_id, document_id),
			UNIQUE(search_index_id, vector_row),
			FOREIGN KEY(search_index_id) REFERENCES search_indexes(search_index_id) ON DELETE CASCADE,
			FOREIGN KEY(conversation_id) REFERENCES conversations(conversation_id) ON DELETE CASCADE,
			FOREIGN KEY(anchor_message_id) REFERENCES messages(message_id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS search_documents_conversation_idx ON search_documents(search_index_id, conversation_id)`,
		`CREATE INDEX IF NOT EXISTS search_documents_created_idx ON search_documents(search_index_id, created_at_ms)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS search_documents_fts USING fts5(
			search_index_id UNINDEXED,
			document_id UNINDEXED,
			conversation_title,
			document_text,
			tokenize = 'unicode61'
		)`,
		`CREATE TABLE IF NOT EXISTS query_embedding_cache (
			search_index_id INTEGER NOT NULL,
			query_hash TEXT NOT NULL,
			query_text TEXT NOT NULL,
			vector_blob BLOB NOT NULL,
			dimensions INTEGER NOT NULL,
			created_at_ms INTEGER NOT NULL,
			PRIMARY KEY(search_index_id, query_hash),
			FOREIGN KEY(search_index_id) REFERENCES search_indexes(search_index_id) ON DELETE CASCADE
		)`,
	}
	for _, statement := range statements {
		if _, executeError := transaction.ExecContext(contextValue, statement); executeError != nil {
			return fmt.Errorf("migrate database to schema version 4: %w", executeError)
		}
	}
	if _, executeError := transaction.ExecContext(
		contextValue,
		`UPDATE schema_metadata SET value = ? WHERE key = 'schema_version'`,
		currentSchemaVersion,
	); executeError != nil {
		return fmt.Errorf("record schema version 4: %w", executeError)
	}
	if commitError := transaction.Commit(); commitError != nil {
		return fmt.Errorf("commit schema version 4 migration: %w", commitError)
	}
	rollbackRequired = false
	return nil
}

func (store *Store) migrateV2ToV3(contextValue context.Context) error {
	transaction, beginError := store.database.BeginTx(contextValue, nil)
	if beginError != nil {
		return fmt.Errorf("begin schema version 3 migration: %w", beginError)
	}
	rollbackRequired := true
	defer func() {
		if rollbackRequired {
			_ = transaction.Rollback()
		}
	}()

	migrationStatements := []string{
		`ALTER TABLE embedding_configs ADD COLUMN input_prefix TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE embedding_configs ADD COLUMN status TEXT NOT NULL DEFAULT 'building' CHECK(status IN ('building', 'ready'))`,
		`ALTER TABLE embedding_configs ADD COLUMN completed_at_ms INTEGER`,
		`CREATE INDEX IF NOT EXISTS embedding_configs_status_idx ON embedding_configs(status, config_id)`,
		`CREATE TABLE IF NOT EXISTS verification_cache (
			cache_key TEXT PRIMARY KEY,
			verifier_identity TEXT NOT NULL,
			message_id TEXT NOT NULL,
			input_hash TEXT NOT NULL,
			result_json TEXT NOT NULL,
			created_at_ms INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS verification_cache_identity_idx ON verification_cache(verifier_identity, message_id)`,
	}
	for _, statement := range migrationStatements {
		if _, executeError := transaction.ExecContext(contextValue, statement); executeError != nil {
			return fmt.Errorf("migrate database to schema version 3: %w", executeError)
		}
	}

	completedAtMillis := time.Now().UTC().UnixMilli()
	if _, executeError := transaction.ExecContext(
		contextValue,
		`UPDATE embedding_configs
		 SET status = CASE
		     WHEN (SELECT COUNT(*) FROM embeddings embedding WHERE embedding.config_id = embedding_configs.config_id) >=
		          (SELECT COUNT(*) FROM messages WHERE role = 'user' AND normalized_text <> '')
		     THEN 'ready'
		     ELSE 'building'
		 END,
		 completed_at_ms = CASE
		     WHEN (SELECT COUNT(*) FROM embeddings embedding WHERE embedding.config_id = embedding_configs.config_id) >=
		          (SELECT COUNT(*) FROM messages WHERE role = 'user' AND normalized_text <> '')
		     THEN ?
		     ELSE NULL
		 END`,
		completedAtMillis,
	); executeError != nil {
		return fmt.Errorf("classify migrated embedding configurations: %w", executeError)
	}
	if _, executeError := transaction.ExecContext(
		contextValue,
		`UPDATE schema_metadata SET value = ? WHERE key = 'schema_version'`,
		"3",
	); executeError != nil {
		return fmt.Errorf("record schema version 3: %w", executeError)
	}
	if commitError := transaction.Commit(); commitError != nil {
		return fmt.Errorf("commit schema version 3 migration: %w", commitError)
	}
	rollbackRequired = false
	return nil
}

func (store *Store) readSchemaVersion(contextValue context.Context) (string, bool, error) {
	var tableName string
	tableError := store.database.QueryRowContext(
		contextValue,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'schema_metadata'`,
	).Scan(&tableName)
	if tableError != nil {
		if tableError == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, fmt.Errorf("inspect database schema metadata: %w", tableError)
	}

	var schemaVersion string
	versionError := store.database.QueryRowContext(
		contextValue,
		`SELECT value FROM schema_metadata WHERE key = 'schema_version'`,
	).Scan(&schemaVersion)
	if versionError != nil {
		if versionError == sql.ErrNoRows {
			return "", false, fmt.Errorf("database contains schema_metadata but no schema_version value")
		}
		return "", false, fmt.Errorf("read database schema version: %w", versionError)
	}
	return strings.TrimSpace(schemaVersion), true, nil
}
