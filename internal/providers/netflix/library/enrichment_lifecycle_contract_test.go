package library

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/enrichment"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
)

func TestTMDBReplacementKeepsRawActiveAndPublishesCompleteCoverageAndExport(
	testContext *testing.T,
) {
	fixture := newWorkspaceFixture(testContext)
	client := newLifecycleMetadataClient(true)
	workspace := fixture.openWithClient(testContext, client, workspaceOptions{
		now:     fixture.clock,
		entropy: testEntropy(0x91),
	})
	defer workspace.Close()

	localGeneration := importLocalFixture(testContext, workspace, syntheticViewingCSV)
	locale, localeError := tmdb.NewLocale("en-US")
	if localeError != nil {
		testContext.Fatalf("construct enrichment locale: %v", localeError)
	}
	tmdbGeneration, createError := workspace.CreateTMDBGeneration(
		context.Background(),
		localGeneration.ID,
		locale,
		enrichment.AuthorizeTMDBTitleQueries(),
	)
	if createError != nil {
		testContext.Fatalf("create TMDB replacement: %v", createError)
	}
	select {
	case <-client.started:
	case <-time.After(5 * time.Second):
		testContext.Fatalf("timed out waiting for a derived title query")
	}
	building := workspace.Snapshot()
	if building.Active == nil ||
		building.Active.ID != localGeneration.ID ||
		building.Building == nil ||
		building.Building.ID != tmdbGeneration.ID ||
		building.Building.AnalysisLevel != AnalysisLevelTMDB {
		testContext.Fatalf("raw generation was not available during enrichment: %+v", building)
	}
	if rawAnalytics, analyticsError := workspace.Analytics(
		context.Background(),
		localGeneration.ID,
		ActivityFilter{},
	); analyticsError != nil || rawAnalytics.Data.ActivityCount != 4 {
		testContext.Fatalf(
			"raw analytics during enrichment = %+v error=%v",
			rawAnalytics,
			analyticsError,
		)
	}
	client.releaseQueries()

	ready := waitForGenerationState(
		testContext,
		workspace,
		tmdbGeneration.ID,
		GenerationStateReady,
	)
	if ready.AnalysisLevel != AnalysisLevelTMDB ||
		ready.SourceGenerationID != localGeneration.ID ||
		ready.CompletedTitleCount != 3 ||
		ready.MatchedTitleCount != 1 ||
		ready.ReviewTitleCount != 1 ||
		ready.UnmatchedTitleCount != 1 ||
		ready.CacheHitTitleCount != 0 ||
		ready.ProgressPercent != 100 ||
		ready.Locale != "en-US" ||
		ready.TMDBClientIdentity != tmdb.ClientIdentity ||
		ready.TMDBMatcherIdentity != netflix.TMDBMatcherIdentity ||
		ready.TMDBCacheIdentity != enrichment.CacheFreshnessIdentity {
		testContext.Fatalf("unexpected ready TMDB generation: %+v", ready)
	}
	if snapshot := workspace.Snapshot(); snapshot.Active == nil ||
		snapshot.Active.ID != tmdbGeneration.ID ||
		snapshot.State != ProviderStateReadyTMDB {
		testContext.Fatalf("TMDB replacement was not activated: %+v", snapshot)
	}

	analytics, analyticsError := workspace.Analytics(
		context.Background(),
		tmdbGeneration.ID,
		ActivityFilter{},
	)
	if analyticsError != nil {
		testContext.Fatalf("read enriched analytics: %v", analyticsError)
	}
	assertMatchCounts(
		testContext,
		analytics.Data.MatchStatusTitles,
		map[string]int{"matched": 1, "review": 1, "unmatched": 1},
	)
	assertMatchCounts(
		testContext,
		analytics.Data.MatchStatusActivities,
		map[string]int{"matched": 1, "review": 2, "unmatched": 1},
	)

	reviewPage, pageError := workspace.Records(
		context.Background(),
		tmdbGeneration.ID,
		"",
		10,
		ActivityFilter{MatchStatus: netflix.MatchStatusReview},
	)
	if pageError != nil {
		testContext.Fatalf("read review records: %v", pageError)
	}
	if len(reviewPage.Records) != 2 {
		testContext.Fatalf("review record count = %d; want 2", len(reviewPage.Records))
	}
	for _, record := range reviewPage.Records {
		if record.Match == nil ||
			record.Match.Status != netflix.MatchStatusReview ||
			record.Metadata != nil {
			testContext.Fatalf("invalid review record: %+v", record)
		}
	}
	filteredReviewAnalytics, analyticsError := workspace.Analytics(
		context.Background(),
		tmdbGeneration.ID,
		ActivityFilter{
			StartDate:   "2026-02-01",
			EndDate:     "2026-02-28",
			MatchStatus: netflix.MatchStatusReview,
		},
	)
	if analyticsError != nil ||
		filteredReviewAnalytics.Data.ActivityCount != 1 ||
		filteredReviewAnalytics.Data.UniqueTitleCount != 1 {
		testContext.Fatalf(
			"shared review analytics filter = %+v error=%v",
			filteredReviewAnalytics,
			analyticsError,
		)
	}
	filteredReviewPage, pageError := workspace.Records(
		context.Background(),
		tmdbGeneration.ID,
		"",
		10,
		ActivityFilter{
			StartDate:   "2026-02-01",
			EndDate:     "2026-02-28",
			MatchStatus: netflix.MatchStatusReview,
		},
	)
	if pageError != nil ||
		len(filteredReviewPage.Records) != 1 ||
		filteredReviewPage.Records[0].DateISO != "2026-02-02" {
		testContext.Fatalf(
			"shared review records filter = %+v error=%v",
			filteredReviewPage,
			pageError,
		)
	}

	exportRecords, exportError := workspace.ExportRecords(
		context.Background(),
		tmdbGeneration.ID,
	)
	if exportError != nil {
		testContext.Fatalf("prepare enriched export: %v", exportError)
	}
	var export bytes.Buffer
	if writeError := netflix.WriteEnrichedActivity(
		context.Background(),
		&export,
		exportRecords,
	); writeError != nil {
		testContext.Fatalf("write enriched export: %v", writeError)
	}
	csvLimits, limitsError := netflix.NewCSVLimits(
		product.MaxNetflixViewingRows,
		product.MaxNetflixTitleBytes,
		int(product.MaxNetflixEnrichmentOutcomeBytes),
	)
	if limitsError != nil {
		testContext.Fatalf("construct enriched CSV limits: %v", limitsError)
	}
	roundTripped, readError := netflix.ReadEnrichedActivity(
		context.Background(),
		bytes.NewReader(export.Bytes()),
		csvLimits,
	)
	if readError != nil || len(roundTripped) != 4 {
		testContext.Fatalf(
			"enriched export round trip rows=%d error=%v",
			len(roundTripped),
			readError,
		)
	}
	if deleteError := workspace.DeleteGeneration(
		context.Background(),
		localGeneration.ID,
	); errorCode(deleteError) != ErrorConflict {
		testContext.Fatalf("derived source deletion error = %v", deleteError)
	}
	checkpointDirectory := filepath.Join(
		fixture.root.Path(),
		filepath.FromSlash(generationsRelativeDirectory),
		tmdbGeneration.ID,
		enrichmentOutcomesDirectory,
	)
	if _, statError := os.Stat(checkpointDirectory); !errors.Is(statError, os.ErrNotExist) {
		testContext.Fatalf("ready generation retained checkpoints: %v", statError)
	}

	events, eventsError := workspace.Events(tmdbGeneration.ID, 0)
	if eventsError != nil {
		testContext.Fatalf("read enrichment progress: %v", eventsError)
	}
	previousPercent := -1
	for eventIndex, event := range events.Events {
		if event.Sequence != int64(eventIndex+1) ||
			event.ProgressPercent < previousPercent ||
			event.TotalTitleCount != 3 {
			testContext.Fatalf("invalid enrichment event %d: %+v", eventIndex+1, event)
		}
		previousPercent = event.ProgressPercent
	}
	if len(events.Events) < 2 ||
		events.Events[len(events.Events)-1].State != GenerationStateReady ||
		events.Events[len(events.Events)-1].ProgressPercent != 100 {
		testContext.Fatalf("enrichment event journal is incomplete: %+v", events)
	}
	if calls := client.searchCallSnapshot(); len(calls) != 3 ||
		calls["Synthetic Film"] != 1 ||
		calls["Synthetic Series"] != 1 ||
		calls["Another Film"] != 1 {
		testContext.Fatalf("unexpected title queries: %#v", calls)
	}
}

func TestTMDBCheckpointResumesAfterRestartWithoutRepeatingCompletedQuery(
	testContext *testing.T,
) {
	fixture := newWorkspaceFixture(testContext)
	checkpointReached := make(chan struct{})
	var signalCheckpoint sync.Once
	client := newRestartCheckpointMetadataClient()
	workspace := fixture.openWithClient(testContext, client, workspaceOptions{
		now:     fixture.clock,
		entropy: testEntropy(0x92),
		afterEnrichmentCheckpoint: func(ctx context.Context, _ string, completed int) error {
			if completed != 1 {
				return errors.New("restart fixture expected exactly one checkpoint")
			}
			signalCheckpoint.Do(func() {
				close(checkpointReached)
			})
			<-ctx.Done()
			return ctx.Err()
		},
	})

	localGeneration := importLocalFixture(testContext, workspace, syntheticViewingCSV)
	tmdbGeneration, createError := workspace.CreateTMDBGeneration(
		context.Background(),
		localGeneration.ID,
		mustEnrichmentLocale(testContext),
		enrichment.AuthorizeTMDBTitleQueries(),
	)
	if createError != nil {
		testContext.Fatalf("create resumable TMDB generation: %v", createError)
	}
	select {
	case <-checkpointReached:
	case <-time.After(5 * time.Second):
		testContext.Fatalf("timed out waiting for the first enrichment checkpoint")
	}
	if building := workspace.Snapshot().Building; building == nil ||
		building.ID != tmdbGeneration.ID ||
		building.CompletedTitleCount != 1 {
		testContext.Fatalf("unexpected resumable enrichment state: %+v", building)
	}
	checkpointPattern := filepath.Join(
		fixture.root.Path(),
		filepath.FromSlash(generationsRelativeDirectory),
		tmdbGeneration.ID,
		enrichmentOutcomesDirectory,
		"*.json",
	)
	checkpointFiles, globError := filepath.Glob(checkpointPattern)
	if globError != nil || len(checkpointFiles) != 1 {
		testContext.Fatalf(
			"checkpoint files = %#v error=%v; want one",
			checkpointFiles,
			globError,
		)
	}
	checkpointDirectoryInfo, directoryStatError := os.Stat(
		filepath.Dir(checkpointFiles[0]),
	)
	checkpointInfo, checkpointStatError := os.Stat(checkpointFiles[0])
	if directoryStatError != nil || checkpointStatError != nil {
		testContext.Fatalf(
			"inspect checkpoint permissions: directory=%v file=%v",
			directoryStatError,
			checkpointStatError,
		)
	}
	if checkpointDirectoryInfo.Mode().Perm() != 0o700 ||
		checkpointInfo.Mode().Perm() != 0o600 {
		testContext.Fatalf(
			"checkpoint permissions directory=%v file=%v",
			checkpointDirectoryInfo.Mode().Perm(),
			checkpointInfo.Mode().Perm(),
		)
	}
	completedQuery := client.firstCompletedQuery()
	if completedQuery == "" {
		testContext.Fatalf("restart fixture did not record its completed query")
	}
	if closeError := workspace.Close(); closeError != nil {
		testContext.Fatalf("close checkpointed workspace: %v", closeError)
	}
	if writeError := os.WriteFile(
		checkpointFiles[0]+".next",
		[]byte("incomplete atomic sibling"),
		0o600,
	); writeError != nil {
		testContext.Fatalf("create stale atomic checkpoint sibling: %v", writeError)
	}

	resumeClient := newLifecycleMetadataClient(false)
	reopened := fixture.openWithClient(testContext, resumeClient, workspaceOptions{
		now:     fixture.clock,
		entropy: testEntropy(0x93),
	})
	defer reopened.Close()
	ready := waitForGenerationState(
		testContext,
		reopened,
		tmdbGeneration.ID,
		GenerationStateReady,
	)
	if ready.CompletedTitleCount != 3 ||
		ready.ProgressPercent != 100 ||
		ready.CacheHitTitleCount != 0 {
		testContext.Fatalf("unexpected resumed enrichment: %+v", ready)
	}
	resumeCalls := resumeClient.searchCallSnapshot()
	if resumeCalls[completedQuery] != 0 || len(resumeCalls) != 2 {
		testContext.Fatalf(
			"resumed queries = %#v; completed query %q was repeated",
			resumeCalls,
			completedQuery,
		)
	}
	if _, statError := os.Stat(filepath.Dir(checkpointFiles[0])); !errors.Is(
		statError,
		os.ErrNotExist,
	) {
		testContext.Fatalf("resumed generation retained checkpoints: %v", statError)
	}
}

func TestTMDBRateLimitedFailureKeepsRawActiveAndRetryUsesPrivateCache(
	testContext *testing.T,
) {
	fixture := newWorkspaceFixture(testContext)
	failureGate := make(chan struct{})
	var releaseFailure sync.Once
	seedClient := newCacheSeedMetadataClient(failureGate)
	workspace := fixture.openWithClient(testContext, seedClient, workspaceOptions{
		now:     fixture.clock,
		entropy: testEntropy(0x94),
		afterEnrichmentCheckpoint: func(
			_ context.Context,
			_ string,
			completed int,
		) error {
			if completed == 1 {
				releaseFailure.Do(func() {
					close(failureGate)
				})
			}
			return nil
		},
	})

	localGeneration := importLocalFixture(testContext, workspace, syntheticViewingCSV)
	failedGeneration, createError := workspace.CreateTMDBGeneration(
		context.Background(),
		localGeneration.ID,
		mustEnrichmentLocale(testContext),
		enrichment.AuthorizeTMDBTitleQueries(),
	)
	if createError != nil {
		testContext.Fatalf("create rate-limited generation: %v", createError)
	}
	failed := waitForGenerationState(
		testContext,
		workspace,
		failedGeneration.ID,
		GenerationStateFailed,
	)
	if failed.Failure == nil || failed.Failure.Code != ErrorRateLimited {
		testContext.Fatalf("rate-limited failure = %+v", failed.Failure)
	}
	if snapshot := workspace.Snapshot(); snapshot.Active == nil ||
		snapshot.Active.ID != localGeneration.ID ||
		snapshot.Building != nil {
		testContext.Fatalf("rate-limited replacement changed active raw data: %+v", snapshot)
	}
	if rawAnalytics, analyticsError := workspace.Analytics(
		context.Background(),
		localGeneration.ID,
		ActivityFilter{},
	); analyticsError != nil || rawAnalytics.Data.ActivityCount != 4 {
		testContext.Fatalf(
			"raw analytics after failure = %+v error=%v",
			rawAnalytics,
			analyticsError,
		)
	}
	if closeError := workspace.Close(); closeError != nil {
		testContext.Fatalf("close rate-limited workspace: %v", closeError)
	}

	retryClient := newLifecycleMetadataClient(false)
	reopened := fixture.openWithClient(testContext, retryClient, workspaceOptions{
		now:     fixture.clock,
		entropy: testEntropy(0x95),
	})
	defer reopened.Close()
	retryGeneration, retryError := reopened.CreateTMDBGeneration(
		context.Background(),
		localGeneration.ID,
		mustEnrichmentLocale(testContext),
		enrichment.AuthorizeTMDBTitleQueries(),
	)
	if retryError != nil {
		testContext.Fatalf("retry TMDB generation: %v", retryError)
	}
	ready := waitForGenerationState(
		testContext,
		reopened,
		retryGeneration.ID,
		GenerationStateReady,
	)
	if ready.CacheHitTitleCount != 1 ||
		ready.MatchedTitleCount != 1 ||
		ready.ReviewTitleCount != 1 ||
		ready.UnmatchedTitleCount != 1 {
		testContext.Fatalf("unexpected cached retry coverage: %+v", ready)
	}
	retryCalls := retryClient.searchCallSnapshot()
	if retryCalls["Synthetic Film"] != 0 ||
		retryCalls["Synthetic Series"] != 1 ||
		retryCalls["Another Film"] != 1 {
		testContext.Fatalf("cached retry queries = %#v", retryCalls)
	}
}

func TestCancelTMDBReplacementKeepsRawActiveAndRemovesPartialGeneration(
	testContext *testing.T,
) {
	fixture := newWorkspaceFixture(testContext)
	client := newLifecycleMetadataClient(true)
	workspace := fixture.openWithClient(testContext, client, workspaceOptions{
		now:     fixture.clock,
		entropy: testEntropy(0x96),
	})
	defer workspace.Close()

	localGeneration := importLocalFixture(testContext, workspace, syntheticViewingCSV)
	tmdbGeneration, createError := workspace.CreateTMDBGeneration(
		context.Background(),
		localGeneration.ID,
		mustEnrichmentLocale(testContext),
		enrichment.AuthorizeTMDBTitleQueries(),
	)
	if createError != nil {
		testContext.Fatalf("create cancelable TMDB generation: %v", createError)
	}
	select {
	case <-client.started:
	case <-time.After(5 * time.Second):
		testContext.Fatalf("timed out waiting for cancelable title query")
	}
	if deleteError := workspace.DeleteGeneration(
		context.Background(),
		tmdbGeneration.ID,
	); deleteError != nil {
		testContext.Fatalf("cancel TMDB generation: %v", deleteError)
	}
	snapshot := workspace.Snapshot()
	if snapshot.Active == nil ||
		snapshot.Active.ID != localGeneration.ID ||
		snapshot.Building != nil ||
		snapshot.LatestFailed == nil ||
		snapshot.LatestFailed.ID != tmdbGeneration.ID ||
		snapshot.LatestFailed.Failure == nil ||
		snapshot.LatestFailed.Failure.Code != ErrorCanceled {
		testContext.Fatalf("unexpected canceled enrichment state: %+v", snapshot)
	}
	generationDirectory := filepath.Join(
		fixture.root.Path(),
		filepath.FromSlash(generationsRelativeDirectory),
		tmdbGeneration.ID,
	)
	if _, statError := os.Stat(generationDirectory); !errors.Is(
		statError,
		os.ErrNotExist,
	) {
		testContext.Fatalf("canceled enrichment retained generation data: %v", statError)
	}
	if rawPage, pageError := workspace.Records(
		context.Background(),
		localGeneration.ID,
		"",
		10,
		ActivityFilter{},
	); pageError != nil || len(rawPage.Records) != 4 {
		testContext.Fatalf("raw records after cancellation = %+v error=%v", rawPage, pageError)
	}
	if deleteError := workspace.DeleteGeneration(
		context.Background(),
		tmdbGeneration.ID,
	); deleteError != nil {
		testContext.Fatalf("delete canceled generation history: %v", deleteError)
	}
	if snapshot := workspace.Snapshot(); snapshot.LatestFailed != nil {
		testContext.Fatalf("deleted canceled history remains: %+v", snapshot)
	}
}

func TestTMDBCreationRequiresExplicitConsentAndConfiguration(
	testContext *testing.T,
) {
	fixture := newWorkspaceFixture(testContext)
	workspace := fixture.open(testContext, workspaceOptions{
		now:     fixture.clock,
		entropy: testEntropy(0x97),
	})
	defer workspace.Close()
	localGeneration := importLocalFixture(testContext, workspace, syntheticViewingCSV)
	locale := mustEnrichmentLocale(testContext)

	if _, createError := workspace.CreateTMDBGeneration(
		context.Background(),
		localGeneration.ID,
		locale,
		enrichment.Authorization{},
	); errorCode(createError) != ErrorConsentRequired {
		testContext.Fatalf("missing consent error = %v", createError)
	}
	if _, createError := workspace.CreateTMDBGeneration(
		context.Background(),
		localGeneration.ID,
		locale,
		enrichment.AuthorizeTMDBTitleQueries(),
	); errorCode(createError) != ErrorNotConfigured {
		testContext.Fatalf("missing TMDB configuration error = %v", createError)
	}
	snapshot := workspace.Snapshot()
	if snapshot.Capabilities.TMDBConfigured ||
		snapshot.Capabilities.TMDBAttribution != tmdb.CreditsAttribution() ||
		snapshot.Building != nil ||
		snapshot.Active == nil ||
		snapshot.Active.ID != localGeneration.ID {
		testContext.Fatalf("unexpected unconfigured TMDB capability: %+v", snapshot)
	}
}

func TestTMDBCreationRejectsAReadyButInactiveSourceBeforeAnyQuery(
	testContext *testing.T,
) {
	fixture := newWorkspaceFixture(testContext)
	client := newLifecycleMetadataClient(false)
	workspace := fixture.openWithClient(testContext, client, workspaceOptions{
		now:     fixture.clock,
		entropy: testEntropy(0x9a),
	})
	defer workspace.Close()
	inactiveSource := importLocalFixture(
		testContext,
		workspace,
		syntheticViewingCSV,
	)
	activeSource := importLocalFixture(
		testContext,
		workspace,
		replacementViewingCSV,
	)

	if _, createError := workspace.CreateTMDBGeneration(
		context.Background(),
		inactiveSource.ID,
		mustEnrichmentLocale(testContext),
		enrichment.AuthorizeTMDBTitleQueries(),
	); errorCode(createError) != ErrorStaleSource {
		testContext.Fatalf("inactive source error = %v", createError)
	}
	if calls := client.searchCallSnapshot(); len(calls) != 0 {
		testContext.Fatalf("inactive source reached TMDB: %#v", calls)
	}
	if snapshot := workspace.Snapshot(); snapshot.Active == nil ||
		snapshot.Active.ID != activeSource.ID ||
		snapshot.Building != nil {
		testContext.Fatalf("inactive source request changed provider state: %+v", snapshot)
	}
}

func TestTMDBRemoteFailureCodesArePersistedWithoutDisplacingRaw(
	testContext *testing.T,
) {
	testCases := []struct {
		name         string
		remoteCode   tmdb.ErrorCode
		expectedCode ErrorCode
	}{
		{
			name:         "unavailable",
			remoteCode:   tmdb.ErrorUnavailable,
			expectedCode: ErrorUnavailable,
		},
		{
			name:         "invalid response",
			remoteCode:   tmdb.ErrorInvalidResponse,
			expectedCode: ErrorInvalidResponse,
		},
	}
	for testIndex, testCase := range testCases {
		testContext.Run(testCase.name, func(testContext *testing.T) {
			fixture := newWorkspaceFixture(testContext)
			workspace := fixture.openWithClient(
				testContext,
				&failingMetadataClient{code: testCase.remoteCode},
				workspaceOptions{
					now:     fixture.clock,
					entropy: testEntropy(byte(0x98 + testIndex)),
				},
			)
			defer workspace.Close()
			localGeneration := importLocalFixture(
				testContext,
				workspace,
				syntheticViewingCSV,
			)
			tmdbGeneration, createError := workspace.CreateTMDBGeneration(
				context.Background(),
				localGeneration.ID,
				mustEnrichmentLocale(testContext),
				enrichment.AuthorizeTMDBTitleQueries(),
			)
			if createError != nil {
				testContext.Fatalf("create failing TMDB generation: %v", createError)
			}
			failed := waitForGenerationState(
				testContext,
				workspace,
				tmdbGeneration.ID,
				GenerationStateFailed,
			)
			if failed.Failure == nil ||
				failed.Failure.Code != testCase.expectedCode {
				testContext.Fatalf("remote failure = %+v", failed.Failure)
			}
			if snapshot := workspace.Snapshot(); snapshot.Active == nil ||
				snapshot.Active.ID != localGeneration.ID ||
				snapshot.Building != nil {
				testContext.Fatalf("remote failure displaced raw data: %+v", snapshot)
			}
		})
	}
}

func importLocalFixture(
	testContext *testing.T,
	workspace *Workspace,
	source string,
) Generation {
	testContext.Helper()
	generation, createError := workspace.CreateLocalGeneration()
	if createError != nil {
		testContext.Fatalf("create local fixture: %v", createError)
	}
	if _, uploadError := workspace.UploadViewingActivity(
		context.Background(),
		generation.ID,
		bytes.NewBufferString(source),
	); uploadError != nil {
		testContext.Fatalf("upload local fixture: %v", uploadError)
	}
	return waitForGenerationState(
		testContext,
		workspace,
		generation.ID,
		GenerationStateReady,
	)
}

func assertMatchCounts(
	testContext *testing.T,
	actual []netflix.Count,
	expected map[string]int,
) {
	testContext.Helper()
	if len(actual) != len(expected) {
		testContext.Fatalf("match counts = %+v; want %#v", actual, expected)
	}
	for _, count := range actual {
		if expected[count.Label] != count.Value {
			testContext.Fatalf("match counts = %+v; want %#v", actual, expected)
		}
	}
}

func mustEnrichmentLocale(testContext *testing.T) tmdb.Locale {
	testContext.Helper()
	locale, localeError := tmdb.NewLocale("en-US")
	if localeError != nil {
		testContext.Fatalf("construct enrichment locale: %v", localeError)
	}
	return locale
}

type lifecycleMetadataClient struct {
	mutex       sync.Mutex
	searchCalls map[string]int
	started     chan struct{}
	release     chan struct{}
	startOnce   sync.Once
	releaseOnce sync.Once
	block       bool
}

func newLifecycleMetadataClient(block bool) *lifecycleMetadataClient {
	return &lifecycleMetadataClient{
		searchCalls: make(map[string]int),
		started:     make(chan struct{}),
		release:     make(chan struct{}),
		block:       block,
	}
}

func (client *lifecycleMetadataClient) Identity() string {
	return tmdb.ClientIdentity
}

func (client *lifecycleMetadataClient) Search(
	ctx context.Context,
	query string,
	locale tmdb.Locale,
) ([]tmdb.Candidate, error) {
	if locale.String() != "en-US" {
		return nil, errors.New("unexpected locale")
	}
	client.mutex.Lock()
	client.searchCalls[query]++
	client.mutex.Unlock()
	client.startOnce.Do(func() {
		close(client.started)
	})
	if client.block {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-client.release:
		}
	}
	switch query {
	case "Synthetic Film":
		return []tmdb.Candidate{{
			TMDBID:        1001,
			MediaType:     netflix.MediaTypeMovie,
			Title:         "Synthetic Film",
			OriginalTitle: "Synthetic Film",
			Popularity:    10,
		}}, nil
	case "Synthetic Series":
		return []tmdb.Candidate{
			{
				TMDBID:        1002,
				MediaType:     netflix.MediaTypeSeries,
				Title:         "Synthetic Series",
				OriginalTitle: "Synthetic Series",
				Popularity:    10,
			},
			{
				TMDBID:        1003,
				MediaType:     netflix.MediaTypeSeries,
				Title:         "Synthetic Series",
				OriginalTitle: "Synthetic Series",
				Popularity:    5,
			},
		}, nil
	case "Another Film":
		return []tmdb.Candidate{}, nil
	default:
		return nil, errors.New("unexpected derived title query")
	}
}

func (client *lifecycleMetadataClient) Details(
	_ context.Context,
	candidate tmdb.Candidate,
	locale tmdb.Locale,
) (tmdb.Details, error) {
	if candidate.TMDBID != 1001 || locale.String() != "en-US" {
		return tmdb.Details{}, errors.New("unexpected details request")
	}
	runtimeMinutes := 101
	voteAverage := 7.3
	voteCount := 800
	return tmdb.Details{
		TMDBID:           candidate.TMDBID,
		MediaType:        candidate.MediaType,
		Genres:           []string{"Drama"},
		ReleaseDate:      "2024-01-01",
		RuntimeMinutes:   &runtimeMinutes,
		OriginalLanguage: "en",
		VoteAverage:      &voteAverage,
		VoteCount:        &voteCount,
		OriginCountries:  []string{"US"},
		MatchedTitle:     candidate.Title,
		Description:      "Synthetic metadata for a deterministic contract test.",
	}, nil
}

func (client *lifecycleMetadataClient) releaseQueries() {
	client.releaseOnce.Do(func() {
		close(client.release)
	})
}

func (client *lifecycleMetadataClient) searchCallSnapshot() map[string]int {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	result := make(map[string]int, len(client.searchCalls))
	for query, count := range client.searchCalls {
		result[query] = count
	}
	return result
}

type restartCheckpointMetadataClient struct {
	mutex      sync.Mutex
	firstQuery string
}

func newRestartCheckpointMetadataClient() *restartCheckpointMetadataClient {
	return &restartCheckpointMetadataClient{}
}

func (client *restartCheckpointMetadataClient) Identity() string {
	return tmdb.ClientIdentity
}

func (client *restartCheckpointMetadataClient) Search(
	ctx context.Context,
	query string,
	_ tmdb.Locale,
) ([]tmdb.Candidate, error) {
	client.mutex.Lock()
	if client.firstQuery == "" {
		client.firstQuery = query
		client.mutex.Unlock()
		return []tmdb.Candidate{}, nil
	}
	client.mutex.Unlock()
	<-ctx.Done()
	return nil, ctx.Err()
}

func (client *restartCheckpointMetadataClient) Details(
	_ context.Context,
	_ tmdb.Candidate,
	_ tmdb.Locale,
) (tmdb.Details, error) {
	return tmdb.Details{}, errors.New("restart checkpoint fixture must not request details")
}

func (client *restartCheckpointMetadataClient) firstCompletedQuery() string {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return client.firstQuery
}

type cacheSeedMetadataClient struct {
	failureGate <-chan struct{}
}

func newCacheSeedMetadataClient(
	failureGate <-chan struct{},
) *cacheSeedMetadataClient {
	return &cacheSeedMetadataClient{failureGate: failureGate}
}

func (client *cacheSeedMetadataClient) Identity() string {
	return tmdb.ClientIdentity
}

func (client *cacheSeedMetadataClient) Search(
	ctx context.Context,
	query string,
	locale tmdb.Locale,
) ([]tmdb.Candidate, error) {
	if locale.String() != "en-US" {
		return nil, errors.New("unexpected locale")
	}
	switch query {
	case "Synthetic Film":
		return []tmdb.Candidate{{
			TMDBID:        1001,
			MediaType:     netflix.MediaTypeMovie,
			Title:         "Synthetic Film",
			OriginalTitle: "Synthetic Film",
			Popularity:    10,
		}}, nil
	case "Synthetic Series":
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-client.failureGate:
			return nil, typedTMDBFailure{code: tmdb.ErrorRateLimited}
		}
	case "Another Film":
		<-ctx.Done()
		return nil, ctx.Err()
	default:
		return nil, errors.New("unexpected derived title query")
	}
}

func (client *cacheSeedMetadataClient) Details(
	_ context.Context,
	candidate tmdb.Candidate,
	locale tmdb.Locale,
) (tmdb.Details, error) {
	return syntheticMatchedDetails(candidate, locale)
}

type failingMetadataClient struct {
	code tmdb.ErrorCode
}

func (client *failingMetadataClient) Identity() string {
	return tmdb.ClientIdentity
}

func (client *failingMetadataClient) Search(
	_ context.Context,
	_ string,
	_ tmdb.Locale,
) ([]tmdb.Candidate, error) {
	return nil, typedTMDBFailure{code: client.code}
}

func (client *failingMetadataClient) Details(
	_ context.Context,
	_ tmdb.Candidate,
	_ tmdb.Locale,
) (tmdb.Details, error) {
	return tmdb.Details{}, errors.New("failing metadata fixture must not request details")
}

type typedTMDBFailure struct {
	code tmdb.ErrorCode
}

func (failure typedTMDBFailure) Error() string {
	return "synthetic typed TMDB failure"
}

func (failure typedTMDBFailure) Code() tmdb.ErrorCode {
	return failure.code
}

func syntheticMatchedDetails(
	candidate tmdb.Candidate,
	locale tmdb.Locale,
) (tmdb.Details, error) {
	if candidate.TMDBID != 1001 || locale.String() != "en-US" {
		return tmdb.Details{}, errors.New("unexpected details request")
	}
	runtimeMinutes := 101
	voteAverage := 7.3
	voteCount := 800
	return tmdb.Details{
		TMDBID:           candidate.TMDBID,
		MediaType:        candidate.MediaType,
		Genres:           []string{"Drama"},
		ReleaseDate:      "2024-01-01",
		RuntimeMinutes:   &runtimeMinutes,
		OriginalLanguage: "en",
		VoteAverage:      &voteAverage,
		VoteCount:        &voteCount,
		OriginCountries:  []string{"US"},
		MatchedTitle:     candidate.Title,
		Description:      "Synthetic metadata for a deterministic contract test.",
	}, nil
}
