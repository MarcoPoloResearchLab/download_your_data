package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/sqlite3"
)

type Store struct {
	database     *sql.DB
	databasePath string
}

func Open(databasePath string) (*Store, error) {
	absolutePath, pathError := filepath.Abs(databasePath)
	if pathError != nil {
		return nil, fmt.Errorf("resolve database path: %w", pathError)
	}
	if directoryError := os.MkdirAll(filepath.Dir(absolutePath), 0o755); directoryError != nil {
		return nil, fmt.Errorf("create database directory: %w", directoryError)
	}

	database, openError := sql.Open(sqlite3.DriverName, absolutePath)
	if openError != nil {
		return nil, fmt.Errorf("open SQLite database: %w", openError)
	}
	database.SetMaxOpenConns(1)

	store := &Store{database: database, databasePath: absolutePath}
	if schemaError := store.initializeOrValidateSchema(context.Background()); schemaError != nil {
		database.Close()
		return nil, schemaError
	}
	return store, nil
}

func (store *Store) Close() error {
	return store.database.Close()
}

func (store *Store) DatabasePath() string {
	return store.databasePath
}

func (store *Store) DatabaseDirectory() string {
	return filepath.Dir(store.databasePath)
}

func (store *Store) Database() *sql.DB {
	return store.database
}
