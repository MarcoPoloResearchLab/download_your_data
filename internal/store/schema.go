package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
)

const (
	currentSchemaOwner    = "download_your_data"
	currentSchemaVersion  = "1"
	currentSchemaContract = "openai-conversation-archive-1"
	schemaContractKey     = "schema_contract"
	schemaOwnerKey        = "schema_owner"
	schemaVersionKey      = "schema_version"
)

const connectionPragmasSQL = `
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA synchronous = NORMAL;
PRAGMA temp_store = MEMORY;
`

const currentSchemaSQL = `
CREATE TABLE schema_metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT INTO schema_metadata(key, value) VALUES
    ('schema_owner', 'download_your_data'),
    ('schema_version', '1'),
    ('schema_contract', 'openai-conversation-archive-1');

CREATE TABLE imports (
    import_id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_sha256 TEXT NOT NULL,
    parser_version TEXT NOT NULL,
    started_at_ms INTEGER NOT NULL,
    completed_at_ms INTEGER,
    status TEXT NOT NULL,
    conversations_seen INTEGER NOT NULL DEFAULT 0,
    messages_seen INTEGER NOT NULL DEFAULT 0,
    warnings_count INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX imports_source_hash_idx
ON imports(source_sha256, parser_version, status);

CREATE TABLE conversations (
    conversation_id TEXT PRIMARY KEY,
    title TEXT NOT NULL DEFAULT '',
    created_at_ms INTEGER,
    updated_at_ms INTEGER,
    is_archived INTEGER,
    current_node_id TEXT,
    source_import_id INTEGER NOT NULL,
    FOREIGN KEY (source_import_id) REFERENCES imports(import_id)
);

CREATE INDEX conversations_created_idx
ON conversations(created_at_ms);

CREATE INDEX conversations_archived_idx
ON conversations(is_archived);

CREATE TABLE messages (
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
    source_node_id TEXT NOT NULL,
    extraction_warning TEXT,
    FOREIGN KEY (conversation_id) REFERENCES conversations(conversation_id) ON DELETE CASCADE,
    UNIQUE (conversation_id, source_node_id)
);

CREATE INDEX messages_conversation_idx
ON messages(conversation_id);

CREATE INDEX messages_role_created_idx
ON messages(role, created_at_ms);

CREATE INDEX messages_source_node_idx
ON messages(conversation_id, source_node_id);

CREATE INDEX messages_source_message_idx
ON messages(source_message_id);

CREATE TABLE message_edges (
    conversation_id TEXT NOT NULL,
    parent_node_id TEXT NOT NULL,
    child_node_id TEXT NOT NULL,
    PRIMARY KEY (conversation_id, parent_node_id, child_node_id),
    FOREIGN KEY (conversation_id) REFERENCES conversations(conversation_id) ON DELETE CASCADE
);

CREATE INDEX message_edges_parent_idx
ON message_edges(conversation_id, parent_node_id);

CREATE TABLE embedding_configs (
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

CREATE INDEX embedding_configs_status_idx
ON embedding_configs(status, config_id);

CREATE TABLE embeddings (
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

CREATE INDEX embeddings_config_idx
ON embeddings(config_id, vector_row);

CREATE TABLE intent_prototypes (
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

CREATE TABLE verification_cache (
    cache_key TEXT PRIMARY KEY,
    verifier_identity TEXT NOT NULL,
    message_id TEXT NOT NULL,
    input_hash TEXT NOT NULL,
    result_json TEXT NOT NULL,
    created_at_ms INTEGER NOT NULL
);

CREATE INDEX verification_cache_identity_idx
ON verification_cache(verifier_identity, message_id);

CREATE TABLE search_indexes (
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

CREATE INDEX search_indexes_status_idx
ON search_indexes(status, search_index_id);

CREATE TABLE search_documents (
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

CREATE INDEX search_documents_conversation_idx
ON search_documents(search_index_id, conversation_id);

CREATE INDEX search_documents_created_idx
ON search_documents(search_index_id, created_at_ms);

CREATE VIRTUAL TABLE search_documents_fts USING fts5(
    search_index_id UNINDEXED,
    document_id UNINDEXED,
    conversation_title,
    document_text,
    tokenize = 'unicode61'
);

CREATE TABLE query_embedding_cache (
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

type schemaObject struct {
	objectType string
	name       string
}

var currentSchemaObjects = []schemaObject{
	{objectType: "table", name: "conversations"},
	{objectType: "table", name: "embedding_configs"},
	{objectType: "table", name: "embeddings"},
	{objectType: "table", name: "imports"},
	{objectType: "table", name: "intent_prototypes"},
	{objectType: "table", name: "message_edges"},
	{objectType: "table", name: "messages"},
	{objectType: "table", name: "query_embedding_cache"},
	{objectType: "table", name: "schema_metadata"},
	{objectType: "table", name: "search_documents"},
	{objectType: "table", name: "search_documents_fts"},
	{objectType: "table", name: "search_documents_fts_config"},
	{objectType: "table", name: "search_documents_fts_content"},
	{objectType: "table", name: "search_documents_fts_data"},
	{objectType: "table", name: "search_documents_fts_docsize"},
	{objectType: "table", name: "search_documents_fts_idx"},
	{objectType: "table", name: "search_indexes"},
	{objectType: "table", name: "verification_cache"},
	{objectType: "index", name: "conversations_archived_idx"},
	{objectType: "index", name: "conversations_created_idx"},
	{objectType: "index", name: "embedding_configs_status_idx"},
	{objectType: "index", name: "embeddings_config_idx"},
	{objectType: "index", name: "imports_source_hash_idx"},
	{objectType: "index", name: "message_edges_parent_idx"},
	{objectType: "index", name: "messages_conversation_idx"},
	{objectType: "index", name: "messages_role_created_idx"},
	{objectType: "index", name: "messages_source_message_idx"},
	{objectType: "index", name: "messages_source_node_idx"},
	{objectType: "index", name: "search_documents_conversation_idx"},
	{objectType: "index", name: "search_documents_created_idx"},
	{objectType: "index", name: "search_indexes_status_idx"},
	{objectType: "index", name: "verification_cache_identity_idx"},
}

func (store *Store) initializeOrValidateSchema(contextValue context.Context) error {
	if _, pragmaError := store.database.ExecContext(contextValue, connectionPragmasSQL); pragmaError != nil {
		return fmt.Errorf("configure SQLite connection: %w", pragmaError)
	}

	isEmpty, emptyError := store.databaseIsEmpty(contextValue)
	if emptyError != nil {
		return emptyError
	}
	if isEmpty {
		if createError := store.createCurrentSchema(contextValue); createError != nil {
			return createError
		}
	}
	if identityError := store.validateSchemaIdentity(contextValue); identityError != nil {
		return identityError
	}
	if shapeError := store.validateSchemaObjects(contextValue); shapeError != nil {
		return shapeError
	}
	return nil
}

func (store *Store) databaseIsEmpty(contextValue context.Context) (bool, error) {
	var objectCount int
	countError := store.database.QueryRowContext(
		contextValue,
		`SELECT COUNT(*) FROM sqlite_master WHERE name NOT LIKE 'sqlite_%'`,
	).Scan(&objectCount)
	if countError != nil {
		return false, fmt.Errorf("inspect database objects: %w", countError)
	}
	return objectCount == 0, nil
}

func (store *Store) createCurrentSchema(contextValue context.Context) error {
	transaction, beginError := store.database.BeginTx(contextValue, nil)
	if beginError != nil {
		return fmt.Errorf("begin current database schema creation: %w", beginError)
	}
	rollbackRequired := true
	defer func() {
		if rollbackRequired {
			_ = transaction.Rollback()
		}
	}()

	if _, executeError := transaction.ExecContext(contextValue, currentSchemaSQL); executeError != nil {
		return fmt.Errorf("create database schema %s/%s: %w", currentSchemaOwner, currentSchemaVersion, executeError)
	}
	if commitError := transaction.Commit(); commitError != nil {
		return fmt.Errorf("commit database schema %s/%s: %w", currentSchemaOwner, currentSchemaVersion, commitError)
	}
	rollbackRequired = false
	return nil
}

func (store *Store) validateSchemaIdentity(contextValue context.Context) error {
	var metadataTableCount int
	tableError := store.database.QueryRowContext(
		contextValue,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_metadata'`,
	).Scan(&metadataTableCount)
	if tableError != nil {
		return fmt.Errorf("inspect database schema identity: %w", tableError)
	}
	if metadataTableCount != 1 {
		return store.incompatibleSchemaError("does not declare a current schema identity")
	}

	schemaOwner, ownerError := store.readSchemaMetadata(contextValue, schemaOwnerKey)
	if ownerError != nil {
		return ownerError
	}
	if schemaOwner != currentSchemaOwner {
		return store.incompatibleSchemaError(
			fmt.Sprintf("declares schema owner %q; this application requires %q", schemaOwner, currentSchemaOwner),
		)
	}

	schemaVersion, versionError := store.readSchemaMetadata(contextValue, schemaVersionKey)
	if versionError != nil {
		return versionError
	}
	if schemaVersion != currentSchemaVersion {
		return store.incompatibleSchemaError(
			fmt.Sprintf("uses schema version %q; this application requires %q", schemaVersion, currentSchemaVersion),
		)
	}
	schemaContract, contractError := store.readSchemaMetadata(contextValue, schemaContractKey)
	if contractError != nil {
		return contractError
	}
	if schemaContract != currentSchemaContract {
		return store.incompatibleSchemaError(
			fmt.Sprintf(
				"declares schema contract %q; this application requires %q",
				schemaContract,
				currentSchemaContract,
			),
		)
	}
	return nil
}

func (store *Store) readSchemaMetadata(contextValue context.Context, key string) (string, error) {
	var value string
	queryError := store.database.QueryRowContext(
		contextValue,
		`SELECT value FROM schema_metadata WHERE key = ?`,
		key,
	).Scan(&value)
	if queryError != nil {
		if queryError == sql.ErrNoRows {
			return "", store.incompatibleSchemaError(fmt.Sprintf("does not declare %s", key))
		}
		return "", fmt.Errorf("read database schema metadata %q: %w", key, queryError)
	}
	return strings.TrimSpace(value), nil
}

func (store *Store) validateSchemaObjects(contextValue context.Context) error {
	for _, requiredObject := range currentSchemaObjects {
		var objectCount int
		queryError := store.database.QueryRowContext(
			contextValue,
			`SELECT COUNT(*) FROM sqlite_master WHERE type = ? AND name = ?`,
			requiredObject.objectType,
			requiredObject.name,
		).Scan(&objectCount)
		if queryError != nil {
			return fmt.Errorf("inspect required database object %q: %w", requiredObject.name, queryError)
		}
		if objectCount != 1 {
			return store.incompatibleSchemaError(
				fmt.Sprintf(
					"declares schema %s/%s but is missing required %s %q",
					currentSchemaOwner,
					currentSchemaVersion,
					requiredObject.objectType,
					requiredObject.name,
				),
			)
		}
	}
	return nil
}

func (store *Store) incompatibleSchemaError(reason string) error {
	return fmt.Errorf(
		"database %q %s; archive it outside the configured data root, then re-import its source export with `%s import <openai-export.zip>`",
		store.DatabasePath(),
		reason,
		product.CommandName,
	)
}
