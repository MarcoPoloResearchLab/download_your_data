package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenRejectsSchemaVersionOneWithReimportInstruction(testContext *testing.T) {
	databasePath := filepath.Join(testContext.TempDir(), "legacy.db")
	legacyDatabase, openError := sql.Open("chatindex-sqlite3", databasePath)
	if openError != nil {
		testContext.Fatalf("open legacy database: %v", openError)
	}
	_, createError := legacyDatabase.Exec(`
CREATE TABLE schema_metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
INSERT INTO schema_metadata(key, value) VALUES ('schema_version', '1');
`)
	if createError != nil {
		legacyDatabase.Close()
		testContext.Fatalf("create legacy schema metadata: %v", createError)
	}
	if closeError := legacyDatabase.Close(); closeError != nil {
		testContext.Fatalf("close legacy database: %v", closeError)
	}

	openedStore, storeError := Open(databasePath)
	if openedStore != nil {
		openedStore.Close()
		testContext.Fatalf("expected schema version 1 to be rejected")
	}
	if storeError == nil {
		testContext.Fatalf("expected actionable legacy-schema error")
	}
	if !strings.Contains(storeError.Error(), "collapsed repeated OpenAI message IDs") ||
		!strings.Contains(storeError.Error(), "reimport with ChatIndex 0.1.1") {
		testContext.Fatalf("unexpected legacy-schema error: %v", storeError)
	}
}

func TestOpenMigratesSchemaVersionTwoWithoutLosingEmbeddingConfiguration(testContext *testing.T) {
	databasePath := filepath.Join(testContext.TempDir(), "version-two.db")
	legacyDatabase, openError := sql.Open("chatindex-sqlite3", databasePath)
	if openError != nil {
		testContext.Fatalf("open version-two database: %v", openError)
	}
	_, createError := legacyDatabase.Exec(`
CREATE TABLE schema_metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
INSERT INTO schema_metadata(key, value) VALUES ('schema_version', '2');

CREATE TABLE messages (
    message_id TEXT PRIMARY KEY,
	conversation_id TEXT NOT NULL DEFAULT '',
	created_at_ms INTEGER,
	source_node_id TEXT NOT NULL DEFAULT '',
	source_message_id TEXT,
    role TEXT NOT NULL,
    normalized_text TEXT NOT NULL
);
INSERT INTO messages(message_id, role, normalized_text) VALUES ('message-1', 'user', 'define berth');

CREATE TABLE embedding_configs (
    config_id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    dimensions INTEGER NOT NULL,
    base_url TEXT NOT NULL,
    vector_path TEXT NOT NULL,
    preprocessing_version TEXT NOT NULL,
    context_version TEXT NOT NULL,
    created_at_ms INTEGER NOT NULL,
    UNIQUE(provider, model, dimensions, base_url, preprocessing_version, context_version)
);
INSERT INTO embedding_configs(
    provider, model, dimensions, base_url, vector_path,
    preprocessing_version, context_version, created_at_ms
) VALUES (
    'lmstudio', 'chatindex-nomic', 768, 'http://127.0.0.1:1234/v1',
    'vectors/legacy.f32', '1', '2', 1000
);

CREATE TABLE embeddings (
    message_id TEXT NOT NULL,
    config_id INTEGER NOT NULL,
    vector_row INTEGER NOT NULL,
    dimensions INTEGER NOT NULL,
    search_text_hash TEXT NOT NULL,
    created_at_ms INTEGER NOT NULL,
    PRIMARY KEY (message_id, config_id),
    UNIQUE (config_id, vector_row)
);
INSERT INTO embeddings(message_id, config_id, vector_row, dimensions, search_text_hash, created_at_ms)
VALUES ('message-1', 1, 0, 768, 'hash', 1000);
`)
	if createError != nil {
		legacyDatabase.Close()
		testContext.Fatalf("create version-two fixture: %v", createError)
	}
	if closeError := legacyDatabase.Close(); closeError != nil {
		testContext.Fatalf("close version-two fixture: %v", closeError)
	}

	openedStore, storeError := Open(databasePath)
	if storeError != nil {
		testContext.Fatalf("migrate version-two database: %v", storeError)
	}
	defer openedStore.Close()

	config, configError := openedStore.EmbeddingConfigByID(context.Background(), 1)
	if configError != nil {
		testContext.Fatalf("load migrated embedding configuration: %v", configError)
	}
	if config.Status != "ready" || config.InputPrefix != "" || config.CompletedAtMillis == nil {
		testContext.Fatalf("unexpected migrated configuration: %+v", config)
	}

	var schemaVersion string
	if queryError := openedStore.Database().QueryRow(
		`SELECT value FROM schema_metadata WHERE key = 'schema_version'`,
	).Scan(&schemaVersion); queryError != nil {
		testContext.Fatalf("read migrated schema version: %v", queryError)
	}
	if schemaVersion != "4" {
		testContext.Fatalf("expected schema version 4, received %q", schemaVersion)
	}

	if cacheError := openedStore.SaveVerificationCache(
		context.Background(), "cache-key", "identity", "message-1", "input-hash", `{}`,
	); cacheError != nil {
		testContext.Fatalf("write migrated verification cache: %v", cacheError)
	}
}

func TestOpenMigratesSchemaVersionThreeToConversationSearchSchema(testContext *testing.T) {
	databasePath := filepath.Join(testContext.TempDir(), "version-three.db")
	legacyDatabase, openError := sql.Open("chatindex-sqlite3", databasePath)
	if openError != nil {
		testContext.Fatalf("open version-three database: %v", openError)
	}
	_, createError := legacyDatabase.Exec(`
CREATE TABLE schema_metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
INSERT INTO schema_metadata(key, value) VALUES ('schema_version', '3');

CREATE VIRTUAL TABLE messages_fts USING fts5(message_id UNINDEXED, conversation_id UNINDEXED, original_text);
INSERT INTO messages_fts(message_id, conversation_id, original_text) VALUES ('legacy-message', 'legacy-conversation', 'legacy raw search row');
`)
	if createError != nil {
		legacyDatabase.Close()
		testContext.Fatalf("create version-three fixture: %v", createError)
	}
	if closeError := legacyDatabase.Close(); closeError != nil {
		testContext.Fatalf("close version-three fixture: %v", closeError)
	}

	openedStore, storeError := Open(databasePath)
	if storeError != nil {
		testContext.Fatalf("migrate version-three database: %v", storeError)
	}
	defer openedStore.Close()

	var schemaVersion string
	if queryError := openedStore.Database().QueryRow(
		`SELECT value FROM schema_metadata WHERE key='schema_version'`,
	).Scan(&schemaVersion); queryError != nil {
		testContext.Fatalf("read migrated schema version: %v", queryError)
	}
	if schemaVersion != "4" {
		testContext.Fatalf("expected schema version 4, received %q", schemaVersion)
	}
	for _, tableName := range []string{"search_indexes", "search_documents", "search_documents_fts", "query_embedding_cache"} {
		var found string
		if queryError := openedStore.Database().QueryRow(
			`SELECT name FROM sqlite_master WHERE name=?`,
			tableName,
		).Scan(&found); queryError != nil {
			testContext.Fatalf("missing migrated table %s: %v", tableName, queryError)
		}
	}
	var legacyTableCount int
	if queryError := openedStore.Database().QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE name='messages_fts'`,
	).Scan(&legacyTableCount); queryError != nil {
		testContext.Fatalf("check removed legacy FTS table: %v", queryError)
	}
	if legacyTableCount != 0 {
		testContext.Fatalf("legacy messages_fts table survived schema-v4 migration")
	}
}
