package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/inference"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/enrichment"
	netflixlibrary "github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/library"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/runtimeconfig"
)

func TestNetflixOperatorCommandSmokeCoversInspectEnrichmentCacheAndExport(
	testContext *testing.T,
) {
	config := testArchiveRuntimeConfig(testContext, inference.DefaultBaseURL)
	metadataClient := &netflixCommandMetadataClient{}
	var output bytes.Buffer
	environment := commandEnvironment{
		output: &output,
		netflixMetadataClientFactory: func(
			runtimeconfig.Config,
		) (enrichment.MetadataClient, error) {
			return metadataClient, nil
		},
		netflixPollInterval: time.Millisecond,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	run := func(arguments ...string) string {
		testContext.Helper()
		output.Reset()
		if commandError := runCommandWithEnvironment(
			context.Background(),
			arguments,
			config,
			logger,
			environment,
		); commandError != nil {
			testContext.Fatalf("run %q: %v", arguments, commandError)
		}
		return output.String()
	}

	seedNetflixCommandGeneration(testContext, config, netflixCommandViewingActivity)
	inspection := run("netflix", "inspect")
	for _, expected := range []string{
		"state=ready_local",
		"analysis=local",
		"activities=2",
		"unique_titles=1",
	} {
		if !strings.Contains(inspection, expected) {
			testContext.Fatalf(
				"Netflix inspection lacks %q:\n%s",
				expected,
				inspection,
			)
		}
	}
	assertNetflixCommandOutputIsPrivate(testContext, inspection, config.DataRoot().Path())

	firstEnrichment := run("netflix", "enrich", "--locale", tmdb.DefaultLocale)
	for _, expected := range []string{
		"TMDB boundary authorized",
		"state=ready",
		"matched=1",
		"progress=100%",
		"cache_hits=0",
		"Netflix enrichment complete",
	} {
		if !strings.Contains(firstEnrichment, expected) {
			testContext.Fatalf(
				"first Netflix enrichment lacks %q:\n%s",
				expected,
				firstEnrichment,
			)
		}
	}
	assertNetflixCommandOutputIsPrivate(
		testContext,
		firstEnrichment,
		config.DataRoot().Path(),
	)
	if searchCalls, detailCalls := metadataClient.callCounts(); searchCalls != 1 ||
		detailCalls != 1 {
		testContext.Fatalf(
			"first enrichment calls = search:%d details:%d; want 1 and 1",
			searchCalls,
			detailCalls,
		)
	}

	seedNetflixCommandGeneration(testContext, config, netflixCommandViewingActivity)
	secondEnrichment := run("netflix", "enrich")
	if !strings.Contains(secondEnrichment, "cache_hits=1") {
		testContext.Fatalf(
			"second Netflix enrichment did not report cache reuse:\n%s",
			secondEnrichment,
		)
	}
	if searchCalls, detailCalls := metadataClient.callCounts(); searchCalls != 1 ||
		detailCalls != 1 {
		testContext.Fatalf(
			"cache reuse made remote calls = search:%d details:%d; want 1 and 1 total",
			searchCalls,
			detailCalls,
		)
	}

	outputRelativePath := filepath.Join("reports", "netflix-enriched.csv")
	exportResult := run(
		"netflix",
		"export",
		"--output",
		outputRelativePath,
	)
	if !strings.Contains(exportResult, "activities=2") ||
		!strings.Contains(exportResult, "output="+outputRelativePath) {
		testContext.Fatalf("unexpected Netflix export result:\n%s", exportResult)
	}
	if strings.Contains(exportResult, config.DataRoot().Path()) {
		testContext.Fatalf("Netflix export disclosed its private root:\n%s", exportResult)
	}
	exportedContents, readError := os.ReadFile(
		filepath.Join(config.DataRoot().Path(), outputRelativePath),
	)
	if readError != nil {
		testContext.Fatalf("read Netflix command export: %v", readError)
	}
	limits, limitsError := netflix.NewCSVLimits(
		product.MaxNetflixViewingRows,
		product.MaxNetflixTitleBytes,
		product.MaxNetflixFieldBytes,
	)
	if limitsError != nil {
		testContext.Fatalf("construct Netflix command CSV limits: %v", limitsError)
	}
	exportedRecords, parseError := netflix.ReadEnrichedActivity(
		context.Background(),
		bytes.NewReader(exportedContents),
		limits,
	)
	if parseError != nil {
		testContext.Fatalf("parse Netflix command export: %v", parseError)
	}
	if len(exportedRecords) != 2 {
		testContext.Fatalf(
			"Netflix command export records = %d; want 2",
			len(exportedRecords),
		)
	}
	for recordIndex, record := range exportedRecords {
		match, hasMatch := record.Match()
		metadata, hasMetadata := record.Metadata()
		if !hasMatch ||
			match.Status() != netflix.MatchStatusMatched ||
			!hasMetadata ||
			metadata.TMDBID() != netflixCommandTMDBID {
			testContext.Fatalf(
				"Netflix command export record %d is incomplete",
				recordIndex+1,
			)
		}
	}
}

func TestNetflixExportRejectsPathsOutsideThePrivateDataRoot(
	testContext *testing.T,
) {
	config := testArchiveRuntimeConfig(testContext, inference.DefaultBaseURL)
	environment := commandEnvironment{
		output: io.Discard,
		netflixMetadataClientFactory: func(
			runtimeconfig.Config,
		) (enrichment.MetadataClient, error) {
			return nil, nil
		},
		netflixPollInterval: time.Millisecond,
	}
	exportError := runCommandWithEnvironment(
		context.Background(),
		[]string{
			"netflix",
			"export",
			"--output",
			filepath.Join(testContext.TempDir(), "escaped.csv"),
		},
		config,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		environment,
	)
	if exportError == nil ||
		!strings.Contains(exportError.Error(), "absolute paths are not allowed") {
		testContext.Fatalf("unexpected escaped Netflix export error: %v", exportError)
	}
}

func TestNetflixEnrichmentCommandFailsBeforeAuthorizationWhenTMDBIsNotConfigured(
	testContext *testing.T,
) {
	config := testArchiveRuntimeConfig(testContext, inference.DefaultBaseURL)
	seedNetflixCommandGeneration(testContext, config, netflixCommandViewingActivity)
	var output bytes.Buffer
	enrichmentError := runCommandWithEnvironment(
		context.Background(),
		[]string{"netflix", "enrich"},
		config,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		commandEnvironment{
			output:                       &output,
			netflixMetadataClientFactory: newNetflixMetadataClient,
			netflixPollInterval:          time.Millisecond,
		},
	)
	if enrichmentError == nil ||
		!strings.Contains(enrichmentError.Error(), tmdb.ReadTokenEnvironment) {
		testContext.Fatalf(
			"unconfigured Netflix enrichment error = %v",
			enrichmentError,
		)
	}
	if output.Len() != 0 {
		testContext.Fatalf(
			"unconfigured Netflix enrichment printed authorization:\n%s",
			output.String(),
		)
	}
}

func TestNetflixEnrichmentCommandCancelsWithoutDisplacingRawActivity(
	testContext *testing.T,
) {
	config := testArchiveRuntimeConfig(testContext, inference.DefaultBaseURL)
	seedNetflixCommandGeneration(testContext, config, netflixCommandViewingActivity)
	metadataClient := newBlockingNetflixCommandMetadataClient()
	var output bytes.Buffer
	environment := commandEnvironment{
		output: &output,
		netflixMetadataClientFactory: func(
			runtimeconfig.Config,
		) (enrichment.MetadataClient, error) {
			return metadataClient, nil
		},
		netflixPollInterval: time.Millisecond,
	}
	applicationContext, cancelApplication := context.WithCancel(context.Background())
	commandResult := make(chan error, 1)
	go func() {
		commandResult <- runCommandWithEnvironment(
			applicationContext,
			[]string{"netflix", "enrich"},
			config,
			slog.New(slog.NewTextHandler(io.Discard, nil)),
			environment,
		)
	}()
	select {
	case <-metadataClient.started:
	case <-time.After(5 * time.Second):
		cancelApplication()
		testContext.Fatal("timed out waiting for Netflix command TMDB request")
	}
	cancelApplication()
	select {
	case commandError := <-commandResult:
		if !errors.Is(commandError, context.Canceled) {
			testContext.Fatalf(
				"canceled Netflix command error = %v; want context cancellation",
				commandError,
			)
		}
	case <-time.After(5 * time.Second):
		testContext.Fatal("timed out waiting for canceled Netflix command")
	}
	assertNetflixCommandOutputIsPrivate(
		testContext,
		output.String(),
		config.DataRoot().Path(),
	)

	workspace, openError := netflixlibrary.Open(
		config.DataRoot(),
		config.NetflixLibrary(),
		config.NetflixLease(),
		config.NetflixTMDBCache(),
		nil,
	)
	if openError != nil {
		testContext.Fatalf("open canceled Netflix command workspace: %v", openError)
	}
	defer workspace.Close()
	snapshot := workspace.Snapshot()
	if snapshot.Active == nil ||
		snapshot.Active.AnalysisLevel != netflixlibrary.AnalysisLevelLocal ||
		snapshot.Active.State != netflixlibrary.GenerationStateReady ||
		snapshot.Building != nil ||
		snapshot.LatestFailed == nil ||
		snapshot.LatestFailed.Failure == nil ||
		snapshot.LatestFailed.Failure.Code != netflixlibrary.ErrorCanceled {
		testContext.Fatalf(
			"canceled Netflix command displaced raw activity: %+v",
			snapshot,
		)
	}
}

func seedNetflixCommandGeneration(
	testContext *testing.T,
	config runtimeconfig.Config,
	source string,
) {
	testContext.Helper()
	workspace, openError := netflixlibrary.Open(
		config.DataRoot(),
		config.NetflixLibrary(),
		config.NetflixLease(),
		config.NetflixTMDBCache(),
		nil,
	)
	if openError != nil {
		testContext.Fatalf("open Netflix command seed workspace: %v", openError)
	}
	closed := false
	testContext.Cleanup(func() {
		if !closed {
			_ = workspace.Close()
		}
	})
	generation, createError := workspace.CreateLocalGeneration()
	if createError != nil {
		testContext.Fatalf("create Netflix command seed generation: %v", createError)
	}
	if _, uploadError := workspace.UploadViewingActivity(
		context.Background(),
		generation.ID,
		strings.NewReader(source),
	); uploadError != nil {
		testContext.Fatalf("upload Netflix command seed generation: %v", uploadError)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		current, found := netflixSnapshotGeneration(
			workspace.Snapshot(),
			generation.ID,
		)
		if found && current.State == netflixlibrary.GenerationStateReady {
			if closeError := workspace.Close(); closeError != nil {
				testContext.Fatalf("close Netflix command seed workspace: %v", closeError)
			}
			closed = true
			return
		}
		if found && current.State == netflixlibrary.GenerationStateFailed {
			testContext.Fatalf(
				"Netflix command seed generation failed: %v",
				netflixGenerationFailure(current),
			)
		}
		time.Sleep(time.Millisecond)
	}
	testContext.Fatalf(
		"Netflix command seed generation did not become ready: %+v",
		workspace.Snapshot(),
	)
}

func assertNetflixCommandOutputIsPrivate(
	testContext *testing.T,
	output string,
	privateRoot string,
) {
	testContext.Helper()
	for _, privateValue := range []string{
		"Command Film",
		"7/1/26",
		"2026-07-01",
		privateRoot,
	} {
		if strings.Contains(output, privateValue) {
			testContext.Fatalf(
				"Netflix command output disclosed %q:\n%s",
				privateValue,
				output,
			)
		}
	}
}

const (
	netflixCommandTMDBID          = int64(701)
	netflixCommandViewingActivity = `Title,Date
Command Film,7/1/26
Command Film,7/2/26
`
)

type netflixCommandMetadataClient struct {
	mutex       sync.Mutex
	searchCalls int
	detailCalls int
}

func (client *netflixCommandMetadataClient) Identity() string {
	return tmdb.ClientIdentity
}

func (client *netflixCommandMetadataClient) Search(
	_ context.Context,
	query string,
	locale tmdb.Locale,
) ([]tmdb.Candidate, error) {
	if query != "Command Film" || locale.String() != tmdb.DefaultLocale {
		return nil, errors.New("unexpected Netflix command search")
	}
	client.mutex.Lock()
	client.searchCalls++
	client.mutex.Unlock()
	return []tmdb.Candidate{{
		TMDBID:        netflixCommandTMDBID,
		MediaType:     netflix.MediaTypeMovie,
		Title:         "Command Film",
		OriginalTitle: "Command Film",
		Popularity:    1,
	}}, nil
}

func (client *netflixCommandMetadataClient) Details(
	_ context.Context,
	candidate tmdb.Candidate,
	locale tmdb.Locale,
) (tmdb.Details, error) {
	if candidate.TMDBID != netflixCommandTMDBID ||
		candidate.MediaType != netflix.MediaTypeMovie ||
		locale.String() != tmdb.DefaultLocale {
		return tmdb.Details{}, errors.New("unexpected Netflix command details")
	}
	client.mutex.Lock()
	client.detailCalls++
	client.mutex.Unlock()
	runtimeMinutes := 90
	voteAverage := 8.0
	voteCount := 10
	return tmdb.Details{
		TMDBID:           netflixCommandTMDBID,
		MediaType:        netflix.MediaTypeMovie,
		Genres:           []string{"Drama"},
		ReleaseDate:      "2025-01-01",
		RuntimeMinutes:   &runtimeMinutes,
		OriginalLanguage: "en",
		VoteAverage:      &voteAverage,
		VoteCount:        &voteCount,
		OriginCountries:  []string{"US"},
		MatchedTitle:     "Command Film",
		Description:      "Synthetic command smoke metadata.",
	}, nil
}

func (client *netflixCommandMetadataClient) callCounts() (int, int) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return client.searchCalls, client.detailCalls
}

type blockingNetflixCommandMetadataClient struct {
	started chan struct{}
	once    sync.Once
}

func newBlockingNetflixCommandMetadataClient() *blockingNetflixCommandMetadataClient {
	return &blockingNetflixCommandMetadataClient{started: make(chan struct{})}
}

func (client *blockingNetflixCommandMetadataClient) Identity() string {
	return tmdb.ClientIdentity
}

func (client *blockingNetflixCommandMetadataClient) Search(
	ctx context.Context,
	query string,
	locale tmdb.Locale,
) ([]tmdb.Candidate, error) {
	if query != "Command Film" || locale.String() != tmdb.DefaultLocale {
		return nil, errors.New("unexpected blocking Netflix command search")
	}
	client.once.Do(func() {
		close(client.started)
	})
	<-ctx.Done()
	return nil, ctx.Err()
}

func (client *blockingNetflixCommandMetadataClient) Details(
	context.Context,
	tmdb.Candidate,
	tmdb.Locale,
) (tmdb.Details, error) {
	return tmdb.Details{}, errors.New(
		"blocking Netflix command fixture must not request details",
	)
}
