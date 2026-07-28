package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/privatepath"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/sqlite3"
)

type Store struct {
	database     *sql.DB
	databaseFile privatepath.File
}

func Open(databaseFile privatepath.File) (*Store, error) {
	if prepareError := databaseFile.Prepare(); prepareError != nil {
		return nil, fmt.Errorf("prepare private database: %w", prepareError)
	}

	database, openError := sql.Open(sqlite3.DriverName, databaseFile.Path())
	if openError != nil {
		return nil, fmt.Errorf("open SQLite database: %w", openError)
	}
	database.SetMaxOpenConns(1)

	store := &Store{database: database, databaseFile: databaseFile}
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
	return store.databaseFile.Path()
}

func (store *Store) DatabaseDirectory() string {
	return filepath.Dir(store.databaseFile.Path())
}

func (store *Store) Database() *sql.DB {
	return store.database
}

func (store *Store) resolveDatabaseRelativeFile(relativePath string) (privatepath.File, error) {
	if strings.TrimSpace(relativePath) == "" {
		return privatepath.File{}, fmt.Errorf("resolve stored private path: relative path is required")
	}
	file, fileError := store.databaseFile.Sibling(relativePath)
	if fileError != nil {
		return privatepath.File{}, fmt.Errorf("resolve stored private path %q: %w", relativePath, fileError)
	}
	return file, nil
}
