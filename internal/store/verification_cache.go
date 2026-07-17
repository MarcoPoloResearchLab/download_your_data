package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func (store *Store) LoadVerificationCache(contextValue context.Context, cacheKey string) (string, bool, error) {
	var resultJSON string
	queryError := store.database.QueryRowContext(
		contextValue,
		`SELECT result_json FROM verification_cache WHERE cache_key = ?`,
		cacheKey,
	).Scan(&resultJSON)
	if queryError != nil {
		if queryError == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, fmt.Errorf("load verification cache: %w", queryError)
	}
	return resultJSON, true, nil
}

func (store *Store) SaveVerificationCache(
	contextValue context.Context,
	cacheKey string,
	verifierIdentity string,
	messageID string,
	inputHash string,
	resultJSON string,
) error {
	_, executeError := store.database.ExecContext(
		contextValue,
		`INSERT INTO verification_cache(
		    cache_key, verifier_identity, message_id, input_hash, result_json, created_at_ms
		 ) VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(cache_key) DO UPDATE SET
		    result_json = excluded.result_json,
		    created_at_ms = excluded.created_at_ms`,
		cacheKey,
		verifierIdentity,
		messageID,
		inputHash,
		resultJSON,
		time.Now().UTC().UnixMilli(),
	)
	if executeError != nil {
		return fmt.Errorf("save verification cache: %w", executeError)
	}
	return nil
}
