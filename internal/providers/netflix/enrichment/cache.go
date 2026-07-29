package enrichment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/privatepath"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/sqlite3"
)

const (
	// CacheFreshnessIdentity changes whenever cache-freshness semantics change.
	CacheFreshnessIdentity = "tmdb-cache-30d-v1"

	cacheSchemaOwner    = "download_your_data"
	cacheSchemaVersion  = "1"
	cacheSchemaContract = "netflix-tmdb-enrichment-cache-v1"
)

const cacheConnectionPragmasSQL = `
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA synchronous = NORMAL;
PRAGMA temp_store = MEMORY;
`

const currentCacheSchemaSQL = `
CREATE TABLE schema_metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT INTO schema_metadata(key, value) VALUES
    ('schema_owner', 'download_your_data'),
    ('schema_version', '1'),
    ('schema_contract', 'netflix-tmdb-enrichment-cache-v1');

CREATE TABLE cache_entries (
    cache_key TEXT PRIMARY KEY,
    title_identity_key TEXT NOT NULL,
    query TEXT NOT NULL,
    locale TEXT NOT NULL,
    client_identity TEXT NOT NULL,
    matcher_identity TEXT NOT NULL,
    freshness_identity TEXT NOT NULL,
    fresh_until_ms INTEGER NOT NULL,
    result_json TEXT NOT NULL,
    created_at_ms INTEGER NOT NULL
);

CREATE INDEX cache_entries_fresh_until_idx
ON cache_entries(fresh_until_ms);
`

type cacheColumn struct {
	name       string
	columnType string
	notNull    int
	primaryKey int
}

var currentCacheColumns = map[string][]cacheColumn{
	"schema_metadata": {
		{name: "key", columnType: "TEXT", notNull: 0, primaryKey: 1},
		{name: "value", columnType: "TEXT", notNull: 1, primaryKey: 0},
	},
	"cache_entries": {
		{name: "cache_key", columnType: "TEXT", notNull: 0, primaryKey: 1},
		{name: "title_identity_key", columnType: "TEXT", notNull: 1, primaryKey: 0},
		{name: "query", columnType: "TEXT", notNull: 1, primaryKey: 0},
		{name: "locale", columnType: "TEXT", notNull: 1, primaryKey: 0},
		{name: "client_identity", columnType: "TEXT", notNull: 1, primaryKey: 0},
		{name: "matcher_identity", columnType: "TEXT", notNull: 1, primaryKey: 0},
		{name: "freshness_identity", columnType: "TEXT", notNull: 1, primaryKey: 0},
		{name: "fresh_until_ms", columnType: "INTEGER", notNull: 1, primaryKey: 0},
		{name: "result_json", columnType: "TEXT", notNull: 1, primaryKey: 0},
		{name: "created_at_ms", columnType: "INTEGER", notNull: 1, primaryKey: 0},
	},
}

var currentCacheObjects = map[string]string{
	"schema_metadata":               "table",
	"cache_entries":                 "table",
	"cache_entries_fresh_until_idx": "index",
}

// Cache owns current, private TMDB enrichment outcomes.
type Cache struct {
	mutex        sync.RWMutex
	database     *sql.DB
	databaseFile privatepath.File
	now          func() time.Time
}

type cacheLookup struct {
	titleIdentityKey string
	query            string
	locale           string
	clientIdentity   string
	matcherIdentity  string
}

type cachedOutcome struct {
	match    netflix.TMDBMatch
	metadata *netflix.TitleMetadata
}

type cacheWrite struct {
	lookup  cacheLookup
	outcome cachedOutcome
}

type cachePayload struct {
	Match    cacheMatchPayload     `json:"match"`
	Metadata *cacheMetadataPayload `json:"metadata,omitempty"`
}

type cacheMatchPayload struct {
	Status          netflix.MatchStatus   `json:"status"`
	MatcherIdentity string                `json:"matcher_identity"`
	MediaType       netflix.MediaType     `json:"media_type,omitempty"`
	TMDBID          int64                 `json:"tmdb_id,omitempty"`
	Evidence        netflix.MatchEvidence `json:"evidence"`
}

type cacheMetadataPayload struct {
	MediaType        netflix.MediaType `json:"media_type"`
	Genres           []string          `json:"genres"`
	ReleaseDate      string            `json:"release_date,omitempty"`
	RuntimeMinutes   *int              `json:"runtime_minutes,omitempty"`
	OriginalLanguage string            `json:"original_language,omitempty"`
	VoteAverage      *float64          `json:"vote_average,omitempty"`
	VoteCount        *int              `json:"vote_count,omitempty"`
	OriginCountries  []string          `json:"origin_countries"`
	Seasons          *int              `json:"seasons,omitempty"`
	Episodes         *int              `json:"episodes,omitempty"`
	TMDBID           int64             `json:"tmdb_id"`
	MatchedTitle     string            `json:"matched_title"`
	Description      string            `json:"description,omitempty"`
}

// OpenCache creates or validates the sole current private cache schema.
func OpenCache(databaseFile privatepath.File) (*Cache, error) {
	return openCache(databaseFile, time.Now)
}

func openCache(databaseFile privatepath.File, now func() time.Time) (*Cache, error) {
	if now == nil {
		return nil, errors.New("open Netflix TMDB cache: clock is required")
	}
	if prepareError := databaseFile.Prepare(); prepareError != nil {
		return nil, fmt.Errorf("prepare private Netflix TMDB cache: %w", prepareError)
	}
	if auxiliaryError := validateExistingSQLiteAuxiliaryFiles(databaseFile); auxiliaryError != nil {
		return nil, auxiliaryError
	}

	database, openError := sql.Open(sqlite3.DriverName, databaseFile.Path())
	if openError != nil {
		return nil, fmt.Errorf("open Netflix TMDB cache: %w", openError)
	}
	database.SetMaxOpenConns(1)

	cache := &Cache{
		database:     database,
		databaseFile: databaseFile,
		now:          now,
	}
	if schemaError := cache.initializeOrValidateSchema(context.Background()); schemaError != nil {
		_ = database.Close()
		return nil, schemaError
	}
	if auxiliaryError := validateExistingSQLiteAuxiliaryFiles(databaseFile); auxiliaryError != nil {
		_ = database.Close()
		return nil, auxiliaryError
	}
	return cache, nil
}

func (cache *Cache) ready() bool {
	if cache == nil {
		return false
	}
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()
	return cache.database != nil && cache.now != nil
}

// Close releases the cache. It is safe to call more than once.
func (cache *Cache) Close() error {
	if cache == nil {
		return nil
	}
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if cache.database == nil {
		return nil
	}
	closeError := cache.database.Close()
	if closeError == nil {
		cache.database = nil
	}
	return closeError
}

// Delete closes and removes the exact database, WAL, and SHM files.
func (cache *Cache) Delete() error {
	if cache == nil {
		return nil
	}
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if cache.database != nil {
		if closeError := cache.database.Close(); closeError != nil {
			return fmt.Errorf("close Netflix TMDB cache before deletion: %w", closeError)
		}
		cache.database = nil
	}

	files, filesError := sqliteCacheFiles(cache.databaseFile)
	if filesError != nil {
		return filesError
	}
	var deleteErrors []error
	for _, file := range files {
		removeError := os.Remove(file.Path())
		if removeError != nil && !errors.Is(removeError, os.ErrNotExist) {
			deleteErrors = append(
				deleteErrors,
				fmt.Errorf("delete private Netflix TMDB cache file %q: %w", file.Path(), removeError),
			)
		}
	}
	return errors.Join(deleteErrors...)
}

func (cache *Cache) lookup(
	ctx context.Context,
	lookup cacheLookup,
) (cachedOutcome, bool, error) {
	if validationError := validateCacheLookup(lookup); validationError != nil {
		return cachedOutcome{}, false, validationError
	}
	if ctx == nil {
		return cachedOutcome{}, false, errors.New("lookup Netflix TMDB cache: context is required")
	}

	cache.mutex.RLock()
	defer cache.mutex.RUnlock()
	if cache.database == nil {
		return cachedOutcome{}, false, errors.New("lookup Netflix TMDB cache: cache is closed")
	}

	cacheKey := deriveCacheKey(lookup)
	var storedTitleIdentityKey string
	var storedQuery string
	var storedLocale string
	var storedClientIdentity string
	var storedMatcherIdentity string
	var storedFreshnessIdentity string
	var freshUntilMilliseconds int64
	var resultJSON string
	var resultLength int64
	queryError := cache.database.QueryRowContext(
		ctx,
		`SELECT
		    title_identity_key,
		    query,
		    locale,
		    client_identity,
		    matcher_identity,
		    freshness_identity,
		    fresh_until_ms,
		    result_json,
		    length(result_json)
		FROM cache_entries
		WHERE cache_key = ?`,
		cacheKey,
	).Scan(
		&storedTitleIdentityKey,
		&storedQuery,
		&storedLocale,
		&storedClientIdentity,
		&storedMatcherIdentity,
		&storedFreshnessIdentity,
		&freshUntilMilliseconds,
		&resultJSON,
		&resultLength,
	)
	if errors.Is(queryError, sql.ErrNoRows) {
		return cachedOutcome{}, false, nil
	}
	if queryError != nil {
		return cachedOutcome{}, false, fmt.Errorf("read Netflix TMDB cache entry: %w", queryError)
	}
	if storedTitleIdentityKey != lookup.titleIdentityKey ||
		storedQuery != lookup.query ||
		storedLocale != lookup.locale ||
		storedClientIdentity != lookup.clientIdentity ||
		storedMatcherIdentity != lookup.matcherIdentity ||
		storedFreshnessIdentity != CacheFreshnessIdentity {
		return cachedOutcome{}, false, errors.New(
			"read Netflix TMDB cache entry: cache identity is inconsistent",
		)
	}
	if resultLength <= 0 || resultLength > product.MaxTMDBCacheResultBytes {
		return cachedOutcome{}, false, fmt.Errorf(
			"read Netflix TMDB cache entry: result size must be between 1 and %d bytes",
			product.MaxTMDBCacheResultBytes,
		)
	}
	if freshUntilMilliseconds <= cache.now().UTC().UnixMilli() {
		if _, deleteError := cache.database.ExecContext(
			ctx,
			`DELETE FROM cache_entries WHERE cache_key = ?`,
			cacheKey,
		); deleteError != nil {
			return cachedOutcome{}, false, fmt.Errorf(
				"delete expired Netflix TMDB cache entry: %w",
				deleteError,
			)
		}
		return cachedOutcome{}, false, nil
	}
	outcome, decodeError := decodeCacheOutcome([]byte(resultJSON))
	if decodeError != nil {
		return cachedOutcome{}, false, decodeError
	}
	return outcome, true, nil
}

func (cache *Cache) putAll(ctx context.Context, writes []cacheWrite) error {
	if ctx == nil {
		return errors.New("write Netflix TMDB cache: context is required")
	}
	if len(writes) == 0 {
		return errors.New("write Netflix TMDB cache: at least one entry is required")
	}

	type preparedWrite struct {
		lookup     cacheLookup
		cacheKey   string
		resultJSON string
	}
	preparedWrites := make([]preparedWrite, len(writes))
	for writeIndex, write := range writes {
		if validationError := validateCacheLookup(write.lookup); validationError != nil {
			return fmt.Errorf("write Netflix TMDB cache entry %d: %w", writeIndex+1, validationError)
		}
		resultJSON, encodeError := encodeCacheOutcome(write.outcome)
		if encodeError != nil {
			return fmt.Errorf("write Netflix TMDB cache entry %d: %w", writeIndex+1, encodeError)
		}
		preparedWrites[writeIndex] = preparedWrite{
			lookup:     write.lookup,
			cacheKey:   deriveCacheKey(write.lookup),
			resultJSON: string(resultJSON),
		}
	}

	cache.mutex.RLock()
	defer cache.mutex.RUnlock()
	if cache.database == nil {
		return errors.New("write Netflix TMDB cache: cache is closed")
	}
	transaction, beginError := cache.database.BeginTx(ctx, nil)
	if beginError != nil {
		return fmt.Errorf("begin Netflix TMDB cache write: %w", beginError)
	}
	rollbackRequired := true
	defer func() {
		if rollbackRequired {
			_ = transaction.Rollback()
		}
	}()

	createdAtMilliseconds := cache.now().UTC().UnixMilli()
	freshUntilMilliseconds := cache.now().
		UTC().
		Add(product.TMDBCacheFreshDays * 24 * time.Hour).
		UnixMilli()
	for _, write := range preparedWrites {
		_, executeError := transaction.ExecContext(
			ctx,
			`INSERT INTO cache_entries(
			    cache_key,
			    title_identity_key,
			    query,
			    locale,
			    client_identity,
			    matcher_identity,
			    freshness_identity,
			    fresh_until_ms,
			    result_json,
			    created_at_ms
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(cache_key) DO UPDATE SET
			    title_identity_key = excluded.title_identity_key,
			    query = excluded.query,
			    locale = excluded.locale,
			    client_identity = excluded.client_identity,
			    matcher_identity = excluded.matcher_identity,
			    freshness_identity = excluded.freshness_identity,
			    fresh_until_ms = excluded.fresh_until_ms,
			    result_json = excluded.result_json,
			    created_at_ms = excluded.created_at_ms`,
			write.cacheKey,
			write.lookup.titleIdentityKey,
			write.lookup.query,
			write.lookup.locale,
			write.lookup.clientIdentity,
			write.lookup.matcherIdentity,
			CacheFreshnessIdentity,
			freshUntilMilliseconds,
			write.resultJSON,
			createdAtMilliseconds,
		)
		if executeError != nil {
			return fmt.Errorf("write Netflix TMDB cache entry: %w", executeError)
		}
	}
	if commitError := transaction.Commit(); commitError != nil {
		return fmt.Errorf("commit Netflix TMDB cache write: %w", commitError)
	}
	rollbackRequired = false
	return nil
}

func (cache *Cache) initializeOrValidateSchema(ctx context.Context) error {
	if _, pragmaError := cache.database.ExecContext(ctx, cacheConnectionPragmasSQL); pragmaError != nil {
		return fmt.Errorf("configure Netflix TMDB cache: %w", pragmaError)
	}
	var objectCount int
	if countError := cache.database.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE name NOT LIKE 'sqlite_%'`,
	).Scan(&objectCount); countError != nil {
		return fmt.Errorf("inspect Netflix TMDB cache objects: %w", countError)
	}
	if objectCount == 0 {
		transaction, beginError := cache.database.BeginTx(ctx, nil)
		if beginError != nil {
			return fmt.Errorf("begin Netflix TMDB cache schema creation: %w", beginError)
		}
		rollbackRequired := true
		defer func() {
			if rollbackRequired {
				_ = transaction.Rollback()
			}
		}()
		if _, createError := transaction.ExecContext(ctx, currentCacheSchemaSQL); createError != nil {
			return fmt.Errorf("create Netflix TMDB cache schema: %w", createError)
		}
		if commitError := transaction.Commit(); commitError != nil {
			return fmt.Errorf("commit Netflix TMDB cache schema: %w", commitError)
		}
		rollbackRequired = false
	}
	if identityError := cache.validateSchemaIdentity(ctx); identityError != nil {
		return identityError
	}
	if shapeError := cache.validateSchemaShape(ctx); shapeError != nil {
		return shapeError
	}
	return nil
}

func (cache *Cache) validateSchemaIdentity(ctx context.Context) error {
	var metadataTableCount int
	if tableError := cache.database.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_metadata'`,
	).Scan(&metadataTableCount); tableError != nil {
		return fmt.Errorf("inspect Netflix TMDB cache schema identity: %w", tableError)
	}
	if metadataTableCount != 1 {
		return incompatibleCacheSchemaError("does not declare a current schema identity")
	}
	expectedMetadata := map[string]string{
		"schema_owner":    cacheSchemaOwner,
		"schema_version":  cacheSchemaVersion,
		"schema_contract": cacheSchemaContract,
	}
	rows, queryError := cache.database.QueryContext(
		ctx,
		`SELECT key, value FROM schema_metadata ORDER BY key`,
	)
	if queryError != nil {
		return fmt.Errorf("read Netflix TMDB cache schema identity: %w", queryError)
	}
	defer rows.Close()
	actualMetadata := make(map[string]string)
	for rows.Next() {
		var key string
		var value string
		if scanError := rows.Scan(&key, &value); scanError != nil {
			return fmt.Errorf("scan Netflix TMDB cache schema identity: %w", scanError)
		}
		actualMetadata[key] = value
	}
	if rowsError := rows.Err(); rowsError != nil {
		return fmt.Errorf("read Netflix TMDB cache schema identity: %w", rowsError)
	}
	if !reflect.DeepEqual(actualMetadata, expectedMetadata) {
		return incompatibleCacheSchemaError("declares a foreign or non-current schema identity")
	}
	return nil
}

func (cache *Cache) validateSchemaShape(ctx context.Context) error {
	rows, queryError := cache.database.QueryContext(
		ctx,
		`SELECT name, type
		FROM sqlite_master
		WHERE name NOT LIKE 'sqlite_%'
		ORDER BY name`,
	)
	if queryError != nil {
		return fmt.Errorf("inspect Netflix TMDB cache schema objects: %w", queryError)
	}
	defer rows.Close()
	actualObjects := make(map[string]string)
	for rows.Next() {
		var name string
		var objectType string
		if scanError := rows.Scan(&name, &objectType); scanError != nil {
			return fmt.Errorf("scan Netflix TMDB cache schema objects: %w", scanError)
		}
		actualObjects[name] = objectType
	}
	if rowsError := rows.Err(); rowsError != nil {
		return fmt.Errorf("inspect Netflix TMDB cache schema objects: %w", rowsError)
	}
	if !reflect.DeepEqual(actualObjects, currentCacheObjects) {
		return incompatibleCacheSchemaError("does not match the exact current schema objects")
	}

	for tableName, expectedColumns := range currentCacheColumns {
		columnRows, columnQueryError := cache.database.QueryContext(
			ctx,
			`PRAGMA table_info(`+tableName+`)`,
		)
		if columnQueryError != nil {
			return fmt.Errorf("inspect Netflix TMDB cache table %q: %w", tableName, columnQueryError)
		}
		actualColumns := make([]cacheColumn, 0, len(expectedColumns))
		for columnRows.Next() {
			var columnID int
			var column cacheColumn
			var defaultValue any
			if scanError := columnRows.Scan(
				&columnID,
				&column.name,
				&column.columnType,
				&column.notNull,
				&defaultValue,
				&column.primaryKey,
			); scanError != nil {
				columnRows.Close()
				return fmt.Errorf(
					"scan Netflix TMDB cache table %q: %w",
					tableName,
					scanError,
				)
			}
			if defaultValue != nil {
				columnRows.Close()
				return incompatibleCacheSchemaError(
					fmt.Sprintf("table %q declares a non-current default", tableName),
				)
			}
			actualColumns = append(actualColumns, column)
		}
		columnRowsError := columnRows.Err()
		columnRows.Close()
		if columnRowsError != nil {
			return fmt.Errorf("inspect Netflix TMDB cache table %q: %w", tableName, columnRowsError)
		}
		if !reflect.DeepEqual(actualColumns, expectedColumns) {
			return incompatibleCacheSchemaError(
				fmt.Sprintf("table %q does not match the exact current columns", tableName),
			)
		}
	}
	return nil
}

func validateCacheLookup(lookup cacheLookup) error {
	decodedKey, decodeError := hex.DecodeString(lookup.titleIdentityKey)
	if decodeError != nil || len(decodedKey) != sha256.Size {
		return errors.New("validate Netflix TMDB cache lookup: current title identity key is required")
	}
	if lookup.query == "" ||
		strings.TrimSpace(lookup.query) != lookup.query ||
		len(lookup.query) > product.MaxTMDBQueryBytes ||
		!utf8.ValidString(lookup.query) {
		return errors.New("validate Netflix TMDB cache lookup: current query is required")
	}
	for _, character := range lookup.query {
		if character == '\n' || character == '\r' || unicode.IsControl(character) {
			return errors.New("validate Netflix TMDB cache lookup: query must be one line")
		}
	}
	if _, localeError := tmdb.NewLocale(lookup.locale); localeError != nil {
		return localeError
	}
	if lookup.clientIdentity != tmdb.ClientIdentity {
		return fmt.Errorf(
			"validate Netflix TMDB cache lookup: client identity must be %s",
			tmdb.ClientIdentity,
		)
	}
	if lookup.matcherIdentity != netflix.TMDBMatcherIdentity {
		return fmt.Errorf(
			"validate Netflix TMDB cache lookup: matcher identity must be %s",
			netflix.TMDBMatcherIdentity,
		)
	}
	return nil
}

func deriveCacheKey(lookup cacheLookup) string {
	hashValue := sha256.Sum256([]byte(strings.Join([]string{
		netflix.TitleIdentityVersion,
		lookup.titleIdentityKey,
		lookup.query,
		lookup.locale,
		lookup.clientIdentity,
		lookup.matcherIdentity,
		CacheFreshnessIdentity,
	}, "\x00")))
	return hex.EncodeToString(hashValue[:])
}

func encodeCacheOutcome(outcome cachedOutcome) ([]byte, error) {
	payload, payloadError := payloadFromOutcome(outcome)
	if payloadError != nil {
		return nil, payloadError
	}
	encoded, encodeError := json.Marshal(payload)
	if encodeError != nil {
		return nil, fmt.Errorf("encode Netflix TMDB cache outcome: %w", encodeError)
	}
	if len(encoded) == 0 || len(encoded) > product.MaxTMDBCacheResultBytes {
		return nil, fmt.Errorf(
			"encode Netflix TMDB cache outcome: result size must be between 1 and %d bytes",
			product.MaxTMDBCacheResultBytes,
		)
	}
	return encoded, nil
}

func decodeCacheOutcome(encoded []byte) (cachedOutcome, error) {
	if len(encoded) == 0 || len(encoded) > product.MaxTMDBCacheResultBytes {
		return cachedOutcome{}, errors.New("decode Netflix TMDB cache outcome: invalid result size")
	}
	var payload cachePayload
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decodeError := decoder.Decode(&payload); decodeError != nil {
		return cachedOutcome{}, fmt.Errorf("decode Netflix TMDB cache outcome: %w", decodeError)
	}
	var trailingValue any
	if trailingError := decoder.Decode(&trailingValue); trailingError == nil {
		return cachedOutcome{}, errors.New("decode Netflix TMDB cache outcome: trailing JSON is not allowed")
	} else if !errors.Is(trailingError, io.EOF) {
		return cachedOutcome{}, fmt.Errorf("decode Netflix TMDB cache outcome: %w", trailingError)
	}
	return payload.outcome()
}

func payloadFromOutcome(outcome cachedOutcome) (cachePayload, error) {
	reconstructedMatch, matchError := netflix.NewTMDBMatch(netflix.TMDBMatchInput{
		Status:          outcome.match.Status(),
		MatcherIdentity: outcome.match.MatcherIdentity(),
		MediaType:       outcome.match.MediaType(),
		TMDBID:          outcome.match.TMDBID(),
		Evidence:        outcome.match.Evidence(),
	})
	if matchError != nil {
		return cachePayload{}, matchError
	}
	payload := cachePayload{
		Match: cacheMatchPayload{
			Status:          reconstructedMatch.Status(),
			MatcherIdentity: reconstructedMatch.MatcherIdentity(),
			MediaType:       reconstructedMatch.MediaType(),
			TMDBID:          reconstructedMatch.TMDBID(),
			Evidence:        reconstructedMatch.Evidence(),
		},
	}
	if outcome.metadata == nil {
		if outcome.match.Status() == netflix.MatchStatusMatched {
			return cachePayload{}, errors.New(
				"validate Netflix TMDB cache outcome: matched result requires metadata",
			)
		}
		return payload, nil
	}
	if outcome.match.Status() != netflix.MatchStatusMatched {
		return cachePayload{}, errors.New(
			"validate Netflix TMDB cache outcome: only matched results may contain metadata",
		)
	}
	metadataPayload := metadataPayloadFromDomain(*outcome.metadata)
	if metadataPayload.TMDBID != outcome.match.TMDBID() ||
		metadataPayload.MediaType != outcome.match.MediaType() {
		return cachePayload{}, errors.New(
			"validate Netflix TMDB cache outcome: match and metadata identities differ",
		)
	}
	payload.Metadata = &metadataPayload
	return payload, nil
}

func (payload cachePayload) outcome() (cachedOutcome, error) {
	match, matchError := netflix.NewTMDBMatch(netflix.TMDBMatchInput{
		Status:          payload.Match.Status,
		MatcherIdentity: payload.Match.MatcherIdentity,
		MediaType:       payload.Match.MediaType,
		TMDBID:          payload.Match.TMDBID,
		Evidence:        payload.Match.Evidence,
	})
	if matchError != nil {
		return cachedOutcome{}, matchError
	}
	outcome := cachedOutcome{match: match}
	if payload.Metadata == nil {
		if match.Status() == netflix.MatchStatusMatched {
			return cachedOutcome{}, errors.New(
				"decode Netflix TMDB cache outcome: matched result requires metadata",
			)
		}
		return outcome, nil
	}
	if match.Status() != netflix.MatchStatusMatched {
		return cachedOutcome{}, errors.New(
			"decode Netflix TMDB cache outcome: only matched results may contain metadata",
		)
	}
	metadata, metadataError := payload.Metadata.domain()
	if metadataError != nil {
		return cachedOutcome{}, metadataError
	}
	if metadata.TMDBID() != match.TMDBID() || metadata.MediaType() != match.MediaType() {
		return cachedOutcome{}, errors.New(
			"decode Netflix TMDB cache outcome: match and metadata identities differ",
		)
	}
	outcome.metadata = &metadata
	return outcome, nil
}

func metadataPayloadFromDomain(metadata netflix.TitleMetadata) cacheMetadataPayload {
	runtimeMinutes, hasRuntime := metadata.RuntimeMinutes()
	voteAverage, hasVoteAverage := metadata.VoteAverage()
	voteCount, hasVoteCount := metadata.VoteCount()
	seasons, hasSeasons := metadata.Seasons()
	episodes, hasEpisodes := metadata.Episodes()
	return cacheMetadataPayload{
		MediaType:        metadata.MediaType(),
		Genres:           metadata.Genres(),
		ReleaseDate:      metadata.ReleaseDate(),
		RuntimeMinutes:   optionalIntPointer(runtimeMinutes, hasRuntime),
		OriginalLanguage: metadata.OriginalLanguage(),
		VoteAverage:      optionalFloatPointer(voteAverage, hasVoteAverage),
		VoteCount:        optionalIntPointer(voteCount, hasVoteCount),
		OriginCountries:  metadata.OriginCountries(),
		Seasons:          optionalIntPointer(seasons, hasSeasons),
		Episodes:         optionalIntPointer(episodes, hasEpisodes),
		TMDBID:           metadata.TMDBID(),
		MatchedTitle:     metadata.MatchedTitle(),
		Description:      metadata.Description(),
	}
}

func (payload cacheMetadataPayload) domain() (netflix.TitleMetadata, error) {
	return netflix.NewTitleMetadata(netflix.TitleMetadataInput{
		MediaType:        payload.MediaType,
		Genres:           payload.Genres,
		ReleaseDate:      payload.ReleaseDate,
		RuntimeMinutes:   payload.RuntimeMinutes,
		OriginalLanguage: payload.OriginalLanguage,
		VoteAverage:      payload.VoteAverage,
		VoteCount:        payload.VoteCount,
		OriginCountries:  payload.OriginCountries,
		Seasons:          payload.Seasons,
		Episodes:         payload.Episodes,
		TMDBID:           payload.TMDBID,
		MatchedTitle:     payload.MatchedTitle,
		Description:      payload.Description,
	})
}

func optionalIntPointer(value int, present bool) *int {
	if !present {
		return nil
	}
	return &value
}

func optionalFloatPointer(value float64, present bool) *float64 {
	if !present {
		return nil
	}
	return &value
}

func validateExistingSQLiteAuxiliaryFiles(databaseFile privatepath.File) error {
	files, filesError := sqliteCacheFiles(databaseFile)
	if filesError != nil {
		return filesError
	}
	for _, file := range files {
		pathInfo, statError := os.Lstat(file.Path())
		if errors.Is(statError, os.ErrNotExist) {
			continue
		}
		if statError != nil {
			return fmt.Errorf("inspect private Netflix TMDB cache file %q: %w", file.Path(), statError)
		}
		if pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular() {
			return fmt.Errorf(
				"validate private Netflix TMDB cache file %q: regular files are required",
				file.Path(),
			)
		}
		if pathInfo.Mode().Perm() != 0o600 {
			return fmt.Errorf(
				"validate private Netflix TMDB cache file %q: permissions must be 0600, received %04o",
				file.Path(),
				pathInfo.Mode().Perm(),
			)
		}
	}
	return nil
}

func sqliteCacheFiles(databaseFile privatepath.File) ([]privatepath.File, error) {
	databaseBaseName := filepath.Base(databaseFile.Path())
	walFile, walError := databaseFile.Sibling(databaseBaseName + "-wal")
	if walError != nil {
		return nil, fmt.Errorf("resolve Netflix TMDB cache WAL: %w", walError)
	}
	shmFile, shmError := databaseFile.Sibling(databaseBaseName + "-shm")
	if shmError != nil {
		return nil, fmt.Errorf("resolve Netflix TMDB cache SHM: %w", shmError)
	}
	return []privatepath.File{databaseFile, walFile, shmFile}, nil
}

func incompatibleCacheSchemaError(reason string) error {
	return fmt.Errorf(
		"netflix TMDB cache %s; delete it through the provider deletion contract before enrichment",
		reason,
	)
}
