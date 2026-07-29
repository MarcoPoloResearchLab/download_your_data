package enrichment

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
)

func TestServiceRequiresExplicitAuthorizationThenReturnsFailureAtomicCachedResults(
	testContext *testing.T,
) {
	cache, openError := OpenCache(testCacheFile(testContext, "service.db"))
	if openError != nil {
		testContext.Fatalf("open service cache: %v", openError)
	}
	defer cache.Close()
	client := newFakeMetadataClient()
	service, serviceError := NewService(client, cache)
	if serviceError != nil {
		testContext.Fatalf("create enrichment service: %v", serviceError)
	}
	locale, localeError := tmdb.NewLocale(tmdb.DefaultLocale)
	if localeError != nil {
		testContext.Fatalf("construct test locale: %v", localeError)
	}
	identities := testTitleIdentities(
		testContext,
		"The Matrix: Season 1: Episode 1",
		"The Matrix: Season 1: Episode 2",
		"Dune",
		"Missing Title",
	)

	results, enrichmentError := service.Enrich(
		context.Background(),
		Authorization{},
		identities,
		locale,
	)
	if results != nil {
		testContext.Fatalf("unauthorized enrichment returned results")
	}
	assertEnrichmentErrorCode(
		testContext,
		enrichmentError,
		ErrorAuthorizationRequired,
	)
	if client.totalCalls() != 0 {
		testContext.Fatalf("unauthorized enrichment reached TMDB")
	}

	results, enrichmentError = service.Enrich(
		context.Background(),
		AuthorizeTMDBTitleQueries(),
		identities,
		locale,
	)
	if enrichmentError != nil {
		testContext.Fatalf("enrich synthetic titles: %v", enrichmentError)
	}
	assertServiceResults(testContext, results, false)
	firstCallCount := client.totalCalls()
	if firstCallCount != 4 {
		testContext.Fatalf("TMDB calls = %d; want 4 (3 searches and 1 details)", firstCallCount)
	}

	results, enrichmentError = service.Enrich(
		context.Background(),
		AuthorizeTMDBTitleQueries(),
		identities,
		locale,
	)
	if enrichmentError != nil {
		testContext.Fatalf("reuse enrichment cache: %v", enrichmentError)
	}
	assertServiceResults(testContext, results, true)
	if client.totalCalls() != firstCallCount {
		testContext.Fatalf("cache reuse made an additional TMDB request")
	}
}

func TestServiceCachesNothingWhenAnyRemoteTitleFails(testContext *testing.T) {
	cache, openError := OpenCache(testCacheFile(testContext, "atomic.db"))
	if openError != nil {
		testContext.Fatalf("open atomic cache: %v", openError)
	}
	defer cache.Close()
	client := newFakeMetadataClient()
	client.failQuery = "Failure Title"
	service, serviceError := NewService(client, cache)
	if serviceError != nil {
		testContext.Fatalf("create enrichment service: %v", serviceError)
	}
	locale, localeError := tmdb.NewLocale(tmdb.DefaultLocale)
	if localeError != nil {
		testContext.Fatalf("construct test locale: %v", localeError)
	}
	identities := testTitleIdentities(testContext, "Success Title", "Failure Title")
	results, enrichmentError := service.Enrich(
		context.Background(),
		AuthorizeTMDBTitleQueries(),
		identities,
		locale,
	)
	if results != nil {
		testContext.Fatalf("failed enrichment returned partial results")
	}
	assertEnrichmentErrorCode(testContext, enrichmentError, ErrorRemoteFailed)
	if containsPrivateQuery(enrichmentError.Error(), identities) {
		testContext.Fatalf("enrichment error exposed a title query: %v", enrichmentError)
	}
	for _, identity := range identities {
		_, found, lookupError := cache.lookup(context.Background(), cacheLookup{
			titleIdentityKey: identity.Key(),
			query:            identity.SearchTitle(),
			locale:           locale.String(),
			clientIdentity:   tmdb.ClientIdentity,
			matcherIdentity:  netflix.TMDBMatcherIdentity,
		})
		if lookupError != nil {
			testContext.Fatalf("inspect atomic cache: %v", lookupError)
		}
		if found {
			testContext.Fatalf("failed enrichment persisted a partial cache entry")
		}
	}

	client.mutex.Lock()
	client.failQuery = ""
	client.mutex.Unlock()
	results, enrichmentError = service.Enrich(
		context.Background(),
		AuthorizeTMDBTitleQueries(),
		identities,
		locale,
	)
	if enrichmentError != nil || len(results) != 2 {
		testContext.Fatalf("retry complete enrichment results=%d error=%v", len(results), enrichmentError)
	}
	for _, result := range results {
		if result.CacheHit() {
			testContext.Fatalf("retry unexpectedly reused a partial cache result")
		}
	}
}

func TestServiceHonorsCancellationAndFixedConcurrency(testContext *testing.T) {
	cache, openError := OpenCache(testCacheFile(testContext, "concurrency.db"))
	if openError != nil {
		testContext.Fatalf("open concurrency cache: %v", openError)
	}
	defer cache.Close()
	client := newFakeMetadataClient()
	service, serviceError := NewService(client, cache)
	if serviceError != nil {
		testContext.Fatalf("create enrichment service: %v", serviceError)
	}
	locale, localeError := tmdb.NewLocale(tmdb.DefaultLocale)
	if localeError != nil {
		testContext.Fatalf("construct test locale: %v", localeError)
	}

	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	results, enrichmentError := service.Enrich(
		canceledContext,
		AuthorizeTMDBTitleQueries(),
		testTitleIdentities(testContext, "Canceled Title"),
		locale,
	)
	if results != nil {
		testContext.Fatalf("canceled enrichment returned results")
	}
	assertEnrichmentErrorCode(testContext, enrichmentError, ErrorCanceled)
	if client.totalCalls() != 0 {
		testContext.Fatalf("pre-canceled enrichment reached TMDB")
	}

	client.blockSearch = make(chan struct{})
	client.searchStarted = make(chan struct{}, 16)
	titles := make([]string, product.MaxTMDBConcurrency*2)
	for titleIndex := range titles {
		titles[titleIndex] = fmt.Sprintf("Concurrency Title %d", titleIndex+1)
	}
	identities := testTitleIdentities(testContext, titles...)
	resultChannel := make(chan error, 1)
	go func() {
		_, runError := service.Enrich(
			context.Background(),
			AuthorizeTMDBTitleQueries(),
			identities,
			locale,
		)
		resultChannel <- runError
	}()
	for startedCount := 0; startedCount < product.MaxTMDBConcurrency; startedCount++ {
		select {
		case <-client.searchStarted:
		case <-time.After(5 * time.Second):
			testContext.Fatalf("timed out waiting for fixed enrichment workers")
		}
	}
	if maximum := client.maximumActive(); maximum != product.MaxTMDBConcurrency {
		testContext.Fatalf(
			"maximum active enrichment jobs = %d; want %d",
			maximum,
			product.MaxTMDBConcurrency,
		)
	}
	select {
	case <-client.searchStarted:
		testContext.Fatalf("enrichment exceeded the fixed worker count")
	default:
	}
	close(client.blockSearch)
	select {
	case runError := <-resultChannel:
		if runError != nil {
			testContext.Fatalf("complete fixed-concurrency enrichment: %v", runError)
		}
	case <-time.After(5 * time.Second):
		testContext.Fatalf("timed out completing fixed-concurrency enrichment")
	}
	if maximum := client.maximumActive(); maximum > product.MaxTMDBConcurrency {
		testContext.Fatalf(
			"maximum active enrichment jobs = %d; limit %d",
			maximum,
			product.MaxTMDBConcurrency,
		)
	}
}

type fakeMetadataClient struct {
	mutex         sync.Mutex
	searchCalls   map[string]int
	detailsCalls  int
	active        int
	maxActive     int
	failQuery     string
	blockSearch   chan struct{}
	searchStarted chan struct{}
}

func newFakeMetadataClient() *fakeMetadataClient {
	return &fakeMetadataClient{searchCalls: make(map[string]int)}
}

func (client *fakeMetadataClient) Identity() string {
	return tmdb.ClientIdentity
}

func (client *fakeMetadataClient) Search(
	ctx context.Context,
	query string,
	_ tmdb.Locale,
) ([]tmdb.Candidate, error) {
	client.beginCall(query, true)
	defer client.endCall()

	client.mutex.Lock()
	failQuery := client.failQuery
	blockSearch := client.blockSearch
	searchStarted := client.searchStarted
	client.mutex.Unlock()
	if searchStarted != nil {
		searchStarted <- struct{}{}
	}
	if blockSearch != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-blockSearch:
		}
	}
	if query == failQuery {
		return nil, errors.New("synthetic remote failure")
	}
	switch query {
	case "Dune":
		return []tmdb.Candidate{
			{
				TMDBID:        11,
				MediaType:     netflix.MediaTypeMovie,
				Title:         "Dune",
				OriginalTitle: "Dune",
				Popularity:    1,
			},
			{
				TMDBID:        12,
				MediaType:     netflix.MediaTypeSeries,
				Title:         "Dune",
				OriginalTitle: "Dune",
				Popularity:    1000,
			},
		}, nil
	case "Missing Title":
		return nil, nil
	default:
		return []tmdb.Candidate{{
			TMDBID:        syntheticTMDBID(query),
			MediaType:     netflix.MediaTypeMovie,
			Title:         query,
			OriginalTitle: query,
			Popularity:    1,
		}}, nil
	}
}

func (client *fakeMetadataClient) Details(
	ctx context.Context,
	candidate tmdb.Candidate,
	_ tmdb.Locale,
) (tmdb.Details, error) {
	client.beginCall("", false)
	defer client.endCall()
	if contextError := ctx.Err(); contextError != nil {
		return tmdb.Details{}, contextError
	}
	runtimeMinutes := 120
	voteAverage := 8.2
	voteCount := 100
	return tmdb.Details{
		TMDBID:           candidate.TMDBID,
		MediaType:        candidate.MediaType,
		Genres:           []string{"Drama"},
		ReleaseDate:      "2026-07-28",
		RuntimeMinutes:   &runtimeMinutes,
		OriginalLanguage: "en",
		VoteAverage:      &voteAverage,
		VoteCount:        &voteCount,
		OriginCountries:  []string{"US"},
		MatchedTitle:     candidate.Title,
		Description:      "Synthetic details.",
	}, nil
}

func (client *fakeMetadataClient) beginCall(query string, search bool) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if search {
		client.searchCalls[query]++
	} else {
		client.detailsCalls++
	}
	client.active++
	client.maxActive = max(client.maxActive, client.active)
}

func (client *fakeMetadataClient) endCall() {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	client.active--
}

func (client *fakeMetadataClient) totalCalls() int {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	total := client.detailsCalls
	for _, callCount := range client.searchCalls {
		total += callCount
	}
	return total
}

func (client *fakeMetadataClient) maximumActive() int {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return client.maxActive
}

func syntheticTMDBID(query string) int64 {
	var identifier int64 = 1
	for _, character := range query {
		identifier = (identifier*31 + int64(character)) % 1_000_000_000
	}
	return identifier + 1
}

func testTitleIdentities(
	testContext *testing.T,
	titles ...string,
) []netflix.TitleIdentity {
	testContext.Helper()
	identities := make([]netflix.TitleIdentity, len(titles))
	for titleIndex, title := range titles {
		activity, activityError := netflix.NewViewingActivity(title, "7/28/26")
		if activityError != nil {
			testContext.Fatalf("construct test title identity: %v", activityError)
		}
		identities[titleIndex] = activity.TitleIdentity()
	}
	return identities
}

func assertServiceResults(
	testContext *testing.T,
	results []Result,
	expectedCacheHit bool,
) {
	testContext.Helper()
	if len(results) != 3 {
		testContext.Fatalf("unique result count = %d; want 3", len(results))
	}
	statusByTitle := make(map[string]netflix.MatchStatus, len(results))
	for resultIndex, result := range results {
		if result.CacheHit() != expectedCacheHit {
			testContext.Fatalf(
				"result %d cache hit = %t; want %t",
				resultIndex,
				result.CacheHit(),
				expectedCacheHit,
			)
		}
		statusByTitle[result.TitleIdentity().SearchTitle()] = result.Match().Status()
		_, hasMetadata := result.Metadata()
		if hasMetadata != (result.Match().Status() == netflix.MatchStatusMatched) {
			testContext.Fatalf(
				"result %s metadata presence disagrees with match status %s",
				result.TitleIdentity().SearchTitle(),
				result.Match().Status(),
			)
		}
	}
	if statusByTitle["The Matrix"] != netflix.MatchStatusMatched ||
		statusByTitle["Dune"] != netflix.MatchStatusReview ||
		statusByTitle["Missing Title"] != netflix.MatchStatusUnmatched {
		testContext.Fatalf("unexpected service statuses: %v", statusByTitle)
	}
}

func assertEnrichmentErrorCode(
	testContext *testing.T,
	enrichmentError error,
	expectedCode ErrorCode,
) {
	testContext.Helper()
	var typedError *Error
	if !errors.As(enrichmentError, &typedError) {
		testContext.Fatalf("expected typed enrichment error, received %v", enrichmentError)
	}
	if typedError.Code() != expectedCode {
		testContext.Fatalf(
			"enrichment error code = %s; want %s",
			typedError.Code(),
			expectedCode,
		)
	}
}

func containsPrivateQuery(
	errorMessage string,
	identities []netflix.TitleIdentity,
) bool {
	for _, identity := range identities {
		if strings.Contains(errorMessage, identity.SearchTitle()) {
			return true
		}
	}
	return false
}
