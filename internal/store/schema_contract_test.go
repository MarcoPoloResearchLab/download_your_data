package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/domain"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/privatepath"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/sqlite3"
)

func TestOpenCreatesAndReopensOnlyTheCurrentProductSchema(testContext *testing.T) {
	databaseFile := testDatabaseFile(testContext, "archive.db")
	openedStore, openError := Open(databaseFile)
	if openError != nil {
		testContext.Fatalf("create current database: %v", openError)
	}

	var schemaOwner string
	if queryError := openedStore.Database().QueryRow(
		`SELECT value FROM schema_metadata WHERE key = ?`,
		schemaOwnerKey,
	).Scan(&schemaOwner); queryError != nil {
		openedStore.Close()
		testContext.Fatalf("read schema owner: %v", queryError)
	}
	var schemaVersion string
	if queryError := openedStore.Database().QueryRow(
		`SELECT value FROM schema_metadata WHERE key = ?`,
		schemaVersionKey,
	).Scan(&schemaVersion); queryError != nil {
		openedStore.Close()
		testContext.Fatalf("read schema version: %v", queryError)
	}
	if schemaOwner != currentSchemaOwner || schemaVersion != currentSchemaVersion {
		openedStore.Close()
		testContext.Fatalf("unexpected schema identity %q/%q", schemaOwner, schemaVersion)
	}
	var schemaContract string
	if queryError := openedStore.Database().QueryRow(
		`SELECT value FROM schema_metadata WHERE key = ?`,
		schemaContractKey,
	).Scan(&schemaContract); queryError != nil {
		openedStore.Close()
		testContext.Fatalf("read schema contract: %v", queryError)
	}
	if schemaContract != currentSchemaContract {
		openedStore.Close()
		testContext.Fatalf("unexpected schema contract %q", schemaContract)
	}
	if closeError := openedStore.Close(); closeError != nil {
		testContext.Fatalf("close current database: %v", closeError)
	}

	reopenedStore, reopenError := Open(databaseFile)
	if reopenError != nil {
		testContext.Fatalf("reopen current database: %v", reopenError)
	}
	if closeError := reopenedStore.Close(); closeError != nil {
		testContext.Fatalf("close reopened database: %v", closeError)
	}
}

func TestOpenRejectsAProductDatabaseWithANonCurrentVersion(testContext *testing.T) {
	databaseFile := testDatabaseFile(testContext, "non-current.db")
	rawDatabase := openRawDatabase(testContext, databaseFile.Path())
	executeTestSQL(testContext, rawDatabase, `
CREATE TABLE schema_metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
INSERT INTO schema_metadata(key, value) VALUES
    ('schema_owner', 'download_your_data'),
    ('schema_version', '2');
`)
	closeRawDatabase(testContext, rawDatabase)

	assertRejectedDatabase(
		testContext,
		databaseFile,
		`uses schema version "2"; this application requires "1"`,
	)
}

func TestOpenRejectsADatabaseWithoutTheProductSchemaIdentity(testContext *testing.T) {
	databaseFile := testDatabaseFile(testContext, "foreign.db")
	rawDatabase := openRawDatabase(testContext, databaseFile.Path())
	executeTestSQL(testContext, rawDatabase, `CREATE TABLE foreign_data (value TEXT NOT NULL);`)
	closeRawDatabase(testContext, rawDatabase)

	assertRejectedDatabase(testContext, databaseFile, "does not declare a current schema identity")
}

func TestOpenRejectsACopiedVersionMarkerWithoutTheProductOwner(testContext *testing.T) {
	databaseFile := testDatabaseFile(testContext, "unowned.db")
	rawDatabase := openRawDatabase(testContext, databaseFile.Path())
	executeTestSQL(testContext, rawDatabase, `
CREATE TABLE schema_metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
INSERT INTO schema_metadata(key, value) VALUES ('schema_version', '1');
`)
	closeRawDatabase(testContext, rawDatabase)

	assertRejectedDatabase(testContext, databaseFile, "does not declare schema_owner")
}

func TestOpenRejectsAnOwnerAndVersionWithoutTheCurrentSchemaContract(testContext *testing.T) {
	databaseFile := testDatabaseFile(testContext, "missing-contract.db")
	rawDatabase := openRawDatabase(testContext, databaseFile.Path())
	executeTestSQL(testContext, rawDatabase, `
CREATE TABLE schema_metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
INSERT INTO schema_metadata(key, value) VALUES
    ('schema_owner', 'download_your_data'),
    ('schema_version', '1');
`)
	closeRawDatabase(testContext, rawDatabase)

	assertRejectedDatabase(testContext, databaseFile, "does not declare schema_contract")
}

func TestOpenRejectsAnIncompleteCurrentSchemaInsteadOfRepairingIt(testContext *testing.T) {
	databaseFile := testDatabaseFile(testContext, "incomplete.db")
	openedStore, openError := Open(databaseFile)
	if openError != nil {
		testContext.Fatalf("create current database: %v", openError)
	}
	if closeError := openedStore.Close(); closeError != nil {
		testContext.Fatalf("close current database: %v", closeError)
	}

	rawDatabase := openRawDatabase(testContext, databaseFile.Path())
	executeTestSQL(testContext, rawDatabase, `DROP TABLE verification_cache;`)
	closeRawDatabase(testContext, rawDatabase)

	assertRejectedDatabase(testContext, databaseFile, `missing required table "verification_cache"`)
}

func TestCurrentSchemaOmitsUnusedRawExportMetadata(testContext *testing.T) {
	databaseFile := testDatabaseFile(testContext, "minimal.db")
	openedStore, openError := Open(databaseFile)
	if openError != nil {
		testContext.Fatalf("create current database: %v", openError)
	}
	defer openedStore.Close()

	for tableName, forbiddenColumns := range map[string][]string{
		"imports":       {"source_path"},
		"conversations": {"source_filename", "raw_metadata_json"},
		"messages":      {"source_filename", "raw_metadata_json"},
	} {
		rows, queryError := openedStore.Database().Query(`PRAGMA table_info(` + tableName + `)`)
		if queryError != nil {
			testContext.Fatalf("inspect %s columns: %v", tableName, queryError)
		}
		columnNames := make(map[string]struct{})
		for rows.Next() {
			var columnID int
			var columnName string
			var columnType string
			var notNull int
			var defaultValue any
			var primaryKey int
			if scanError := rows.Scan(
				&columnID,
				&columnName,
				&columnType,
				&notNull,
				&defaultValue,
				&primaryKey,
			); scanError != nil {
				rows.Close()
				testContext.Fatalf("scan %s column: %v", tableName, scanError)
			}
			columnNames[columnName] = struct{}{}
		}
		if rowsError := rows.Err(); rowsError != nil {
			rows.Close()
			testContext.Fatalf("iterate %s columns: %v", tableName, rowsError)
		}
		rows.Close()
		for _, forbiddenColumn := range forbiddenColumns {
			if _, exists := columnNames[forbiddenColumn]; exists {
				testContext.Errorf("%s retains forbidden column %s", tableName, forbiddenColumn)
			}
		}
	}
}

func TestStoreConfinesDatabaseAndVectorFilesToOwnerOnlyPaths(testContext *testing.T) {
	databaseFile := testDatabaseFile(testContext, "openai/archive.db")
	openedStore, openError := Open(databaseFile)
	if openError != nil {
		testContext.Fatalf("create private store: %v", openError)
	}
	defer openedStore.Close()

	assertFileMode(testContext, databaseFile.Path(), 0o600)
	assertFileMode(testContext, filepath.Dir(databaseFile.Path()), 0o700)
	for _, auxiliarySuffix := range []string{"-wal", "-shm"} {
		auxiliaryPath := databaseFile.Path() + auxiliarySuffix
		if _, statError := os.Stat(auxiliaryPath); statError == nil {
			assertFileMode(testContext, auxiliaryPath, 0o600)
		} else if !os.IsNotExist(statError) {
			testContext.Fatalf("inspect SQLite auxiliary file %s: %v", auxiliaryPath, statError)
		}
	}

	escapedPath := filepath.Join(string(filepath.Separator), "tmp", "escaped.f32")
	if _, pathError := openedStore.ResolveVectorFile(
		domain.EmbeddingConfig{VectorPath: escapedPath},
	); pathError == nil {
		testContext.Fatalf("absolute vector path should be rejected")
	}
	if _, configError := openedStore.GetOrCreateEmbeddingConfig(
		context.Background(),
		domain.EmbeddingConfig{VectorPath: escapedPath},
	); configError == nil {
		testContext.Fatalf("absolute embedding vector path should be rejected before persistence")
	}
	if _, configError := openedStore.GetOrCreateSearchIndex(
		context.Background(),
		domain.SearchIndexConfig{VectorPath: escapedPath},
	); configError == nil {
		testContext.Fatalf("absolute search vector path should be rejected before persistence")
	}
	for tableName := range map[string]struct{}{
		"embedding_configs": {},
		"search_indexes":    {},
	} {
		var rowCount int
		if countError := openedStore.Database().QueryRow(
			`SELECT COUNT(*) FROM ` + tableName,
		).Scan(&rowCount); countError != nil {
			testContext.Fatalf("count %s: %v", tableName, countError)
		}
		if rowCount != 0 {
			testContext.Fatalf("%s persisted an invalid private path", tableName)
		}
	}
}

func TestSearchIndexDeletionValidatesStoredVectorPathBeforeMutation(testContext *testing.T) {
	databaseFile := testDatabaseFile(testContext, "openai/archive.db")
	openedStore, openError := Open(databaseFile)
	if openError != nil {
		testContext.Fatalf("create private store: %v", openError)
	}
	defer openedStore.Close()

	executeTestSQL(testContext, openedStore.Database(), `
INSERT INTO search_indexes(
    name, provider, model, dimensions, base_url, document_prefix, query_prefix,
    vector_path, builder_version, corpus_policy, status, created_at_ms
) VALUES (
    'corrupt-index', 'fixture', 'fixture-model', 3, 'http://127.0.0.1:1234/v1',
    'search_document: ', 'search_query: ', '/tmp/escaped.f32',
    '1', 'visible-branches', 'ready', 1
);
`)
	if _, deleted, deleteError := openedStore.DeleteSearchIndexByName(
		context.Background(),
		"corrupt-index",
	); deleteError == nil || deleted {
		testContext.Fatalf(
			"invalid stored vector path deletion = deleted %t, error %v",
			deleted,
			deleteError,
		)
	}
	var rowCount int
	if countError := openedStore.Database().QueryRow(
		`SELECT COUNT(*) FROM search_indexes WHERE name='corrupt-index'`,
	).Scan(&rowCount); countError != nil {
		testContext.Fatalf("count preserved corrupt index: %v", countError)
	}
	if rowCount != 1 {
		testContext.Fatalf("invalid stored vector path was deleted before validation")
	}
}

func openRawDatabase(testContext *testing.T, databasePath string) *sql.DB {
	testContext.Helper()
	rawDatabase, openError := sql.Open(sqlite3.DriverName, databasePath)
	if openError != nil {
		testContext.Fatalf("open raw database: %v", openError)
	}
	return rawDatabase
}

func executeTestSQL(testContext *testing.T, database *sql.DB, statement string) {
	testContext.Helper()
	if _, executeError := database.Exec(statement); executeError != nil {
		database.Close()
		testContext.Fatalf("create database fixture: %v", executeError)
	}
}

func closeRawDatabase(testContext *testing.T, database *sql.DB) {
	testContext.Helper()
	if closeError := database.Close(); closeError != nil {
		testContext.Fatalf("close raw database: %v", closeError)
	}
}

func assertRejectedDatabase(testContext *testing.T, databaseFile privatepath.File, expectedReason string) {
	testContext.Helper()
	openedStore, storeError := Open(databaseFile)
	if openedStore != nil {
		openedStore.Close()
		testContext.Fatalf("expected database to be rejected")
	}
	if storeError == nil {
		testContext.Fatalf("expected an actionable schema error")
	}
	if !strings.Contains(storeError.Error(), expectedReason) ||
		!strings.Contains(storeError.Error(), "archive it outside the configured data root") ||
		!strings.Contains(storeError.Error(), "download-your-data import") {
		testContext.Fatalf("unexpected schema error: %v", storeError)
	}
}

func testDatabaseFile(testContext *testing.T, name string) privatepath.File {
	testContext.Helper()
	root, rootError := privatepath.NewRoot(filepath.Join(testContext.TempDir(), "data"))
	if rootError != nil {
		testContext.Fatalf("create private test root: %v", rootError)
	}
	databaseFile, fileError := root.File(name)
	if fileError != nil {
		testContext.Fatalf("resolve private test database: %v", fileError)
	}
	if prepareError := databaseFile.Prepare(); prepareError != nil {
		testContext.Fatalf("prepare private test database: %v", prepareError)
	}
	return databaseFile
}

func assertFileMode(testContext *testing.T, path string, expected os.FileMode) {
	testContext.Helper()
	pathInfo, statError := os.Stat(path)
	if statError != nil {
		testContext.Fatalf("inspect %s: %v", path, statError)
	}
	if pathInfo.Mode().Perm() != expected {
		testContext.Fatalf("%s mode = %04o; want %04o", path, pathInfo.Mode().Perm(), expected)
	}
}
