package enrichment

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/privatepath"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/sqlite3"
)

func TestPrivateCachePersistsOnlyCurrentCompleteOutcomesUntilFreshnessExpires(
	testContext *testing.T,
) {
	currentTime := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	databaseFile := testCacheFile(testContext, "tmdb-cache.db")
	cache, openError := openCache(databaseFile, func() time.Time {
		return currentTime
	})
	if openError != nil {
		testContext.Fatalf("open private cache: %v", openError)
	}

	lookup := testCacheLookup(testContext, "The Matrix")
	outcome := testMatchedOutcome(testContext, "The Matrix", 603)
	if writeError := cache.putAll(
		context.Background(),
		[]cacheWrite{{lookup: lookup, outcome: outcome}},
	); writeError != nil {
		testContext.Fatalf("write complete cache outcome: %v", writeError)
	}
	cached, found, lookupError := cache.lookup(context.Background(), lookup)
	if lookupError != nil {
		testContext.Fatalf("lookup complete cache outcome: %v", lookupError)
	}
	assertMatchedOutcome(testContext, cached, found, 603)

	if closeError := cache.Close(); closeError != nil {
		testContext.Fatalf("close private cache: %v", closeError)
	}
	assertFileMode(testContext, databaseFile.Path(), 0o600)
	reopened, reopenError := openCache(databaseFile, func() time.Time {
		return currentTime
	})
	if reopenError != nil {
		testContext.Fatalf("reopen private cache: %v", reopenError)
	}
	cached, found, lookupError = reopened.lookup(context.Background(), lookup)
	if lookupError != nil {
		testContext.Fatalf("lookup reopened cache outcome: %v", lookupError)
	}
	assertMatchedOutcome(testContext, cached, found, 603)

	currentTime = currentTime.Add((time.Duration(24*30) * time.Hour) + time.Millisecond)
	if _, found, lookupError = reopened.lookup(
		context.Background(),
		lookup,
	); lookupError != nil || found {
		testContext.Fatalf("expired cache lookup found=%t error=%v", found, lookupError)
	}
	if closeError := reopened.Close(); closeError != nil {
		testContext.Fatalf("close reopened cache: %v", closeError)
	}

	databaseBytes, readError := os.ReadFile(databaseFile.Path())
	if readError != nil {
		testContext.Fatalf("read private cache bytes: %v", readError)
	}
	if strings.Contains(string(databaseBytes), "private-read-token") {
		testContext.Fatalf("private cache persisted a credential")
	}
}

func TestCacheRejectsForeignStaleAndMalformedShapesWithoutRepair(
	testContext *testing.T,
) {
	testContext.Run("foreign schema", func(testContext *testing.T) {
		databaseFile := testCacheFile(testContext, "foreign.db")
		rawDatabase := openRawCacheDatabase(testContext, databaseFile)
		if _, executeError := rawDatabase.Exec(`CREATE TABLE foreign_data(value TEXT NOT NULL)`); executeError != nil {
			testContext.Fatalf("create foreign cache fixture: %v", executeError)
		}
		if closeError := rawDatabase.Close(); closeError != nil {
			testContext.Fatalf("close foreign cache fixture: %v", closeError)
		}
		if _, openError := OpenCache(databaseFile); openError == nil ||
			!strings.Contains(openError.Error(), "current schema identity") {
			testContext.Fatalf("foreign cache should be rejected: %v", openError)
		}
	})

	testContext.Run("stale identity", func(testContext *testing.T) {
		databaseFile := testCacheFile(testContext, "stale.db")
		cache, openError := OpenCache(databaseFile)
		if openError != nil {
			testContext.Fatalf("open current cache: %v", openError)
		}
		if _, updateError := cache.database.Exec(
			`UPDATE schema_metadata SET value = '0' WHERE key = 'schema_version'`,
		); updateError != nil {
			testContext.Fatalf("make stale cache fixture: %v", updateError)
		}
		if closeError := cache.Close(); closeError != nil {
			testContext.Fatalf("close stale cache fixture: %v", closeError)
		}
		if _, reopenError := OpenCache(databaseFile); reopenError == nil ||
			!strings.Contains(reopenError.Error(), "foreign or non-current") {
			testContext.Fatalf("stale cache should be rejected: %v", reopenError)
		}
	})

	testContext.Run("malformed outcome", func(testContext *testing.T) {
		databaseFile := testCacheFile(testContext, "malformed.db")
		cache, openError := OpenCache(databaseFile)
		if openError != nil {
			testContext.Fatalf("open current cache: %v", openError)
		}
		defer cache.Close()
		lookup := testCacheLookup(testContext, "The Matrix")
		if writeError := cache.putAll(
			context.Background(),
			[]cacheWrite{{
				lookup:  lookup,
				outcome: testMatchedOutcome(testContext, "The Matrix", 603),
			}},
		); writeError != nil {
			testContext.Fatalf("write cache fixture: %v", writeError)
		}
		if _, updateError := cache.database.Exec(
			`UPDATE cache_entries SET result_json = '{"foreign":true}'`,
		); updateError != nil {
			testContext.Fatalf("corrupt cache fixture: %v", updateError)
		}
		if _, _, lookupError := cache.lookup(
			context.Background(),
			lookup,
		); lookupError == nil || !strings.Contains(lookupError.Error(), "unknown field") {
			testContext.Fatalf("malformed cache result should be rejected: %v", lookupError)
		}
	})
}

func TestCacheRejectsIncompleteWritesAndDeletesExactSQLiteFiles(
	testContext *testing.T,
) {
	databaseFile := testCacheFile(testContext, "delete.db")
	cache, openError := OpenCache(databaseFile)
	if openError != nil {
		testContext.Fatalf("open private cache: %v", openError)
	}
	match := testMatch(testContext, "The Matrix", 603)
	if writeError := cache.putAll(
		context.Background(),
		[]cacheWrite{{
			lookup:  testCacheLookup(testContext, "The Matrix"),
			outcome: cachedOutcome{match: match},
		}},
	); writeError == nil || !strings.Contains(writeError.Error(), "requires metadata") {
		testContext.Fatalf("incomplete matched cache write should be rejected: %v", writeError)
	}
	if closeError := cache.Close(); closeError != nil {
		testContext.Fatalf("close cache before auxiliary fixture: %v", closeError)
	}

	files, filesError := sqliteCacheFiles(databaseFile)
	if filesError != nil {
		testContext.Fatalf("resolve exact cache files: %v", filesError)
	}
	for _, file := range files[1:] {
		if writeError := os.WriteFile(file.Path(), []byte("synthetic"), 0o600); writeError != nil {
			testContext.Fatalf("create cache auxiliary fixture: %v", writeError)
		}
		if chmodError := os.Chmod(file.Path(), 0o600); chmodError != nil {
			testContext.Fatalf("set cache auxiliary fixture mode: %v", chmodError)
		}
	}
	if deleteError := cache.Delete(); deleteError != nil {
		testContext.Fatalf("delete exact cache files: %v", deleteError)
	}
	for _, file := range files {
		if _, statError := os.Stat(file.Path()); !errors.Is(statError, os.ErrNotExist) {
			testContext.Fatalf("cache file still exists after deletion: %s", file.Path())
		}
	}
}

func testCacheFile(testContext *testing.T, name string) privatepath.File {
	testContext.Helper()
	root, rootError := privatepath.NewRoot(filepath.Join(testContext.TempDir(), "private-data"))
	if rootError != nil {
		testContext.Fatalf("create private cache root: %v", rootError)
	}
	file, fileError := root.File(filepath.Join("providers", "netflix", name))
	if fileError != nil {
		testContext.Fatalf("resolve private cache file: %v", fileError)
	}
	return file
}

func openRawCacheDatabase(
	testContext *testing.T,
	databaseFile privatepath.File,
) *sql.DB {
	testContext.Helper()
	if prepareError := databaseFile.Prepare(); prepareError != nil {
		testContext.Fatalf("prepare raw cache database: %v", prepareError)
	}
	database, openError := sql.Open(sqlite3.DriverName, databaseFile.Path())
	if openError != nil {
		testContext.Fatalf("open raw cache database: %v", openError)
	}
	return database
}

func testCacheLookup(testContext *testing.T, title string) cacheLookup {
	testContext.Helper()
	activity, activityError := netflix.NewViewingActivity(title, "7/28/26")
	if activityError != nil {
		testContext.Fatalf("construct cache title identity: %v", activityError)
	}
	identity := activity.TitleIdentity()
	return cacheLookup{
		titleIdentityKey: identity.Key(),
		query:            identity.SearchTitle(),
		locale:           tmdb.DefaultLocale,
		clientIdentity:   tmdb.ClientIdentity,
		matcherIdentity:  netflix.TMDBMatcherIdentity,
	}
}

func testMatchedOutcome(
	testContext *testing.T,
	title string,
	tmdbID int64,
) cachedOutcome {
	testContext.Helper()
	match := testMatch(testContext, title, tmdbID)
	runtimeMinutes := 120
	voteAverage := 8.1
	voteCount := 400
	metadata, metadataError := netflix.NewTitleMetadata(netflix.TitleMetadataInput{
		MediaType:        netflix.MediaTypeMovie,
		Genres:           []string{"Drama"},
		ReleaseDate:      "2026-07-28",
		RuntimeMinutes:   &runtimeMinutes,
		OriginalLanguage: "en",
		VoteAverage:      &voteAverage,
		VoteCount:        &voteCount,
		OriginCountries:  []string{"US"},
		TMDBID:           tmdbID,
		MatchedTitle:     title,
		Description:      "Synthetic metadata.",
	})
	if metadataError != nil {
		testContext.Fatalf("construct cache metadata: %v", metadataError)
	}
	return cachedOutcome{match: match, metadata: &metadata}
}

func testMatch(testContext *testing.T, title string, tmdbID int64) netflix.TMDBMatch {
	testContext.Helper()
	activity, activityError := netflix.NewViewingActivity(title, "7/28/26")
	if activityError != nil {
		testContext.Fatalf("construct match title identity: %v", activityError)
	}
	match, matchError := netflix.MatchTMDBTitle(
		activity.TitleIdentity(),
		[]netflix.MatchCandidate{{
			TMDBID:        tmdbID,
			MediaType:     netflix.MediaTypeMovie,
			Title:         title,
			OriginalTitle: title,
			Popularity:    1,
		}},
	)
	if matchError != nil {
		testContext.Fatalf("construct cache match: %v", matchError)
	}
	return match
}

func assertMatchedOutcome(
	testContext *testing.T,
	outcome cachedOutcome,
	found bool,
	expectedTMDBID int64,
) {
	testContext.Helper()
	if !found ||
		outcome.match.Status() != netflix.MatchStatusMatched ||
		outcome.match.TMDBID() != expectedTMDBID ||
		outcome.metadata == nil ||
		outcome.metadata.TMDBID() != expectedTMDBID {
		testContext.Fatalf("unexpected cached outcome found=%t outcome=%+v", found, outcome)
	}
}

func assertFileMode(
	testContext *testing.T,
	path string,
	expectedMode os.FileMode,
) {
	testContext.Helper()
	pathInfo, statError := os.Stat(path)
	if statError != nil {
		testContext.Fatalf("inspect private cache file: %v", statError)
	}
	if pathInfo.Mode().Perm() != expectedMode {
		testContext.Fatalf(
			"private cache mode = %04o; want %04o",
			pathInfo.Mode().Perm(),
			expectedMode,
		)
	}
}
