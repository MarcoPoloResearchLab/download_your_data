package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func (store *Store) CompletedImportExists(contextValue context.Context, sourceHash string, parserVersion string) (bool, error) {
	var existingCount int64
	queryError := store.database.QueryRowContext(
		contextValue,
		`SELECT COUNT(*) FROM imports WHERE source_sha256 = ? AND parser_version = ? AND status = 'completed'`,
		sourceHash,
		parserVersion,
	).Scan(&existingCount)
	if queryError != nil {
		return false, fmt.Errorf("check prior import: %w", queryError)
	}
	return existingCount > 0, nil
}

func (store *Store) BeginImport(contextValue context.Context, sourcePath string, sourceHash string, parserVersion string) (int64, error) {
	startedAtMillis := time.Now().UTC().UnixMilli()
	result, executeError := store.database.ExecContext(
		contextValue,
		`INSERT INTO imports(source_path, source_sha256, parser_version, started_at_ms, status)
         VALUES (?, ?, ?, ?, 'running')`,
		sourcePath,
		sourceHash,
		parserVersion,
		startedAtMillis,
	)
	if executeError != nil {
		return 0, fmt.Errorf("create import record: %w", executeError)
	}
	importID, identifierError := result.LastInsertId()
	if identifierError != nil {
		return 0, fmt.Errorf("read import identifier: %w", identifierError)
	}
	return importID, nil
}

func (store *Store) UpdateImportProgress(contextValue context.Context, importID int64, conversationsSeen int64, messagesSeen int64, warningsCount int64) error {
	_, executeError := store.database.ExecContext(
		contextValue,
		`UPDATE imports
         SET conversations_seen = ?, messages_seen = ?, warnings_count = ?
         WHERE import_id = ?`,
		conversationsSeen,
		messagesSeen,
		warningsCount,
		importID,
	)
	if executeError != nil {
		return fmt.Errorf("update import progress: %w", executeError)
	}
	return nil
}

func (store *Store) CompleteImport(contextValue context.Context, importID int64, conversationsSeen int64, messagesSeen int64, warningsCount int64) error {
	completedAtMillis := time.Now().UTC().UnixMilli()
	transaction, beginError := store.database.BeginTx(contextValue, nil)
	if beginError != nil {
		return fmt.Errorf("begin completed import transaction: %w", beginError)
	}
	rollbackRequired := true
	defer func() {
		if rollbackRequired {
			_ = transaction.Rollback()
		}
	}()
	_, executeError := transaction.ExecContext(
		contextValue,
		`UPDATE imports
         SET completed_at_ms = ?, status = 'completed', conversations_seen = ?, messages_seen = ?, warnings_count = ?
         WHERE import_id = ?`,
		completedAtMillis,
		conversationsSeen,
		messagesSeen,
		warningsCount,
		importID,
	)
	if executeError != nil {
		return fmt.Errorf("complete import: %w", executeError)
	}
	if _, invalidateError := transaction.ExecContext(
		contextValue,
		`UPDATE search_indexes SET status='building', completed_at_ms=NULL WHERE status='ready'`,
	); invalidateError != nil {
		return fmt.Errorf("invalidate conversation search indexes after import: %w", invalidateError)
	}
	if commitError := transaction.Commit(); commitError != nil {
		return fmt.Errorf("commit completed import: %w", commitError)
	}
	rollbackRequired = false
	return nil
}

func (store *Store) FailImport(contextValue context.Context, importID int64) error {
	_, executeError := store.database.ExecContext(
		contextValue,
		`UPDATE imports SET completed_at_ms = ?, status = 'failed' WHERE import_id = ?`,
		time.Now().UTC().UnixMilli(),
		importID,
	)
	if executeError != nil && executeError != sql.ErrNoRows {
		return fmt.Errorf("mark import failed: %w", executeError)
	}
	return nil
}
