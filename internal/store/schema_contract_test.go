package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/sqlite3"
)

func TestOpenCreatesAndReopensOnlyTheCurrentProductSchema(testContext *testing.T) {
	databasePath := filepath.Join(testContext.TempDir(), "archive.db")
	openedStore, openError := Open(databasePath)
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
	if closeError := openedStore.Close(); closeError != nil {
		testContext.Fatalf("close current database: %v", closeError)
	}

	reopenedStore, reopenError := Open(databasePath)
	if reopenError != nil {
		testContext.Fatalf("reopen current database: %v", reopenError)
	}
	if closeError := reopenedStore.Close(); closeError != nil {
		testContext.Fatalf("close reopened database: %v", closeError)
	}
}

func TestOpenRejectsAProductDatabaseWithANonCurrentVersion(testContext *testing.T) {
	databasePath := filepath.Join(testContext.TempDir(), "non-current.db")
	rawDatabase := openRawDatabase(testContext, databasePath)
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
		databasePath,
		`uses schema version "2"; this application requires "1"`,
	)
}

func TestOpenRejectsADatabaseWithoutTheProductSchemaIdentity(testContext *testing.T) {
	databasePath := filepath.Join(testContext.TempDir(), "foreign.db")
	rawDatabase := openRawDatabase(testContext, databasePath)
	executeTestSQL(testContext, rawDatabase, `CREATE TABLE foreign_data (value TEXT NOT NULL);`)
	closeRawDatabase(testContext, rawDatabase)

	assertRejectedDatabase(testContext, databasePath, "does not declare a current schema identity")
}

func TestOpenRejectsACopiedVersionMarkerWithoutTheProductOwner(testContext *testing.T) {
	databasePath := filepath.Join(testContext.TempDir(), "unowned.db")
	rawDatabase := openRawDatabase(testContext, databasePath)
	executeTestSQL(testContext, rawDatabase, `
CREATE TABLE schema_metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
INSERT INTO schema_metadata(key, value) VALUES ('schema_version', '1');
`)
	closeRawDatabase(testContext, rawDatabase)

	assertRejectedDatabase(testContext, databasePath, "does not declare schema_owner")
}

func TestOpenRejectsAnIncompleteCurrentSchemaInsteadOfRepairingIt(testContext *testing.T) {
	databasePath := filepath.Join(testContext.TempDir(), "incomplete.db")
	openedStore, openError := Open(databasePath)
	if openError != nil {
		testContext.Fatalf("create current database: %v", openError)
	}
	if closeError := openedStore.Close(); closeError != nil {
		testContext.Fatalf("close current database: %v", closeError)
	}

	rawDatabase := openRawDatabase(testContext, databasePath)
	executeTestSQL(testContext, rawDatabase, `DROP TABLE verification_cache;`)
	closeRawDatabase(testContext, rawDatabase)

	assertRejectedDatabase(testContext, databasePath, `missing required table "verification_cache"`)
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

func assertRejectedDatabase(testContext *testing.T, databasePath string, expectedReason string) {
	testContext.Helper()
	openedStore, storeError := Open(databasePath)
	if openedStore != nil {
		openedStore.Close()
		testContext.Fatalf("expected database to be rejected")
	}
	if storeError == nil {
		testContext.Fatalf("expected an actionable schema error")
	}
	if !strings.Contains(storeError.Error(), expectedReason) ||
		!strings.Contains(storeError.Error(), "archive this database") ||
		!strings.Contains(storeError.Error(), "download-your-data-archive import") {
		testContext.Fatalf("unexpected schema error: %v", storeError)
	}
}
