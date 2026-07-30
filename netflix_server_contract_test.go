package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
	netflixlibrary "github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/library"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/runtimeconfig"
)

func TestNetflixHTTPImportsQueriesRestartsAndDeletesLocalGeneration(
	testContext *testing.T,
) {
	config := testRuntimeConfig(testContext)
	var logOutput bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logOutput, nil))
	handler, handlerError := newApplicationHandler(config, logger)
	if handlerError != nil {
		testContext.Fatalf("create Netflix HTTP handler: %v", handlerError)
	}
	server := newAuthenticatedTestServer(testContext, config, handler, defaultTestUserID)

	snapshot := requestNetflixSnapshot(testContext, server.URL)
	if snapshot.State != netflixlibrary.ProviderStateEmpty ||
		snapshot.Capabilities.MaxUploadBytes != product.MaxNetflixViewingCSVBytes ||
		snapshot.Capabilities.MaxRows != product.MaxNetflixViewingRows ||
		snapshot.Capabilities.MaxFieldBytes != product.MaxNetflixFieldBytes ||
		snapshot.Capabilities.MaxWorkingBytes != product.MaxNetflixWorkingBytes ||
		snapshot.Capabilities.MaxConcurrentBuilds != product.MaxNetflixConcurrentBuilds {
		testContext.Fatalf("unexpected initial Netflix snapshot: %+v", snapshot)
	}
	invalidCreate := mutateNetflix(
		testContext,
		config,
		server.URL+netflixGenerationsPath,
		http.MethodPost,
		"application/json",
		`{"analysis_level":"local","legacy":true}`,
	)
	assertRequestError(testContext, invalidCreate, http.StatusBadRequest, "invalid_json")

	createResponse := mutateNetflix(
		testContext,
		config,
		server.URL+netflixGenerationsPath,
		http.MethodPost,
		"application/json; charset=UTF-8",
		`{"analysis_level":"local"}`,
	)
	if createResponse.StatusCode != http.StatusCreated {
		testContext.Fatalf("create status = %d; body=%s", createResponse.StatusCode, readBody(testContext, createResponse))
	}
	var created generationResponse
	decodeResponse(testContext, createResponse, &created)
	generationID := created.Generation.ID
	if created.Generation.State != netflixlibrary.GenerationStateReceiving ||
		generationID == "" {
		testContext.Fatalf("unexpected created generation: %+v", created)
	}

	conflictResponse := mutateNetflix(
		testContext,
		config,
		server.URL+netflixGenerationsPath,
		http.MethodPost,
		"application/json",
		`{"analysis_level":"local"}`,
	)
	assertRequestError(
		testContext,
		conflictResponse,
		http.StatusConflict,
		string(netflixlibrary.ErrorConflict),
	)
	archiveResponse := mutateNetflix(
		testContext,
		config,
		server.URL+netflixGenerationsPath+"/"+generationID+"/viewing-activity",
		http.MethodPut,
		"application/zip",
		"not-a-viewing-activity-csv",
	)
	assertRequestError(
		testContext,
		archiveResponse,
		http.StatusUnsupportedMediaType,
		"invalid_content_type",
	)

	uploadResponse := mutateNetflix(
		testContext,
		config,
		server.URL+netflixGenerationsPath+"/"+generationID+"/viewing-activity",
		http.MethodPut,
		"text/csv; charset=utf-8",
		httpSyntheticViewingCSV,
	)
	if uploadResponse.StatusCode != http.StatusAccepted {
		testContext.Fatalf("upload status = %d; body=%s", uploadResponse.StatusCode, readBody(testContext, uploadResponse))
	}
	uploadResponse.Body.Close()
	ready := waitForHTTPSnapshot(
		testContext,
		server.URL,
		func(snapshot netflixlibrary.Snapshot) bool {
			return snapshot.Active != nil &&
				snapshot.Active.ID == generationID &&
				snapshot.Active.State == netflixlibrary.GenerationStateReady
		},
	)
	if ready.Active.ActivityCount != 4 ||
		ready.Active.UniqueTitleCount != 3 ||
		ready.Active.AnalysisLevel != netflixlibrary.AnalysisLevelLocal {
		testContext.Fatalf("unexpected ready HTTP generation: %+v", ready.Active)
	}

	eventsResponse := getResponse(
		testContext,
		server.URL+netflixGenerationsPath+"/"+generationID+"/events?after=0",
	)
	if eventsResponse.StatusCode != http.StatusOK {
		testContext.Fatalf("events status = %d", eventsResponse.StatusCode)
	}
	var events netflixlibrary.Events
	decodeResponse(testContext, eventsResponse, &events)
	expectedStates := []netflixlibrary.GenerationState{
		netflixlibrary.GenerationStateReceiving,
		netflixlibrary.GenerationStateValidating,
		netflixlibrary.GenerationStateImporting,
		netflixlibrary.GenerationStateReady,
	}
	if len(events.Events) != len(expectedStates) {
		testContext.Fatalf("unexpected event journal: %+v", events)
	}
	for eventIndex, expectedState := range expectedStates {
		if events.Events[eventIndex].Sequence != int64(eventIndex+1) ||
			events.Events[eventIndex].State != expectedState {
			testContext.Fatalf("unexpected event %d: %+v", eventIndex+1, events.Events[eventIndex])
		}
	}
	resumedEventsResponse := getResponse(
		testContext,
		server.URL+netflixGenerationsPath+"/"+generationID+"/events?after=2",
	)
	var resumedEvents netflixlibrary.Events
	decodeResponse(testContext, resumedEventsResponse, &resumedEvents)
	if len(resumedEvents.Events) != 2 ||
		resumedEvents.Events[0].Sequence != 3 {
		testContext.Fatalf("unexpected resumed events: %+v", resumedEvents)
	}

	analyticsResponse := getResponse(
		testContext,
		server.URL+netflixGenerationsPath+"/"+generationID+
			"/analytics?start_date=2026-02-01&end_date=2026-02-28",
	)
	if analyticsResponse.StatusCode != http.StatusOK {
		testContext.Fatalf("analytics status = %d; body=%s", analyticsResponse.StatusCode, readBody(testContext, analyticsResponse))
	}
	var analytics netflixlibrary.Analytics
	decodeResponse(testContext, analyticsResponse, &analytics)
	if analytics.Data.ActivityCount != 2 ||
		analytics.Filter.StartDate != "2026-02-01" {
		testContext.Fatalf("unexpected HTTP analytics: %+v", analytics)
	}

	recordsResponse := getResponse(
		testContext,
		server.URL+netflixGenerationsPath+"/"+generationID+"/records?limit=2",
	)
	if recordsResponse.StatusCode != http.StatusOK {
		testContext.Fatalf("records status = %d", recordsResponse.StatusCode)
	}
	var firstPage netflixlibrary.ActivityPage
	decodeResponse(testContext, recordsResponse, &firstPage)
	if len(firstPage.Records) != 2 ||
		firstPage.NextCursor == "" ||
		firstPage.Records[0].RawTitle != "Synthetic Film" {
		testContext.Fatalf("unexpected HTTP records page: %+v", firstPage)
	}
	secondPageResponse := getResponse(
		testContext,
		server.URL+netflixGenerationsPath+"/"+generationID+
			"/records?limit=2&cursor="+firstPage.NextCursor,
	)
	var secondPage netflixlibrary.ActivityPage
	decodeResponse(testContext, secondPageResponse, &secondPage)
	if len(secondPage.Records) != 2 || secondPage.NextCursor != "" {
		testContext.Fatalf("unexpected HTTP second page: %+v", secondPage)
	}

	sourcePath := filepath.Join(
		testUserWorkspace(testContext, config, defaultTestUserID).Root().Path(),
		"providers",
		"netflix",
		"generations",
		generationID,
		"viewing-activity.csv",
	)
	if _, statError := os.Stat(sourcePath); !errors.Is(statError, os.ErrNotExist) {
		testContext.Fatalf("ready HTTP generation retained source upload: %v", statError)
	}

	server.Close()
	if closeError := handler.Close(); closeError != nil {
		testContext.Fatalf("close first HTTP handler: %v", closeError)
	}
	restartedHandler, restartError := newApplicationHandler(config, logger)
	if restartError != nil {
		testContext.Fatalf("restart Netflix HTTP handler: %v", restartError)
	}
	defer restartedHandler.Close()
	restartedServer := newAuthenticatedTestServer(
		testContext,
		config,
		restartedHandler,
		defaultTestUserID,
	)
	defer restartedServer.Close()
	restartedSnapshot := requestNetflixSnapshot(testContext, restartedServer.URL)
	if restartedSnapshot.Active == nil ||
		restartedSnapshot.Active.ID != generationID ||
		restartedSnapshot.Active.State != netflixlibrary.GenerationStateReady {
		testContext.Fatalf("HTTP restart lost active generation: %+v", restartedSnapshot)
	}

	activeDelete := mutateNetflix(
		testContext,
		config,
		restartedServer.URL+netflixGenerationsPath+"/"+generationID,
		http.MethodDelete,
		"",
		"",
	)
	assertRequestError(
		testContext,
		activeDelete,
		http.StatusConflict,
		string(netflixlibrary.ErrorConflict),
	)
	wrongDelete := mutateNetflix(
		testContext,
		config,
		restartedServer.URL+netflixProviderPath,
		http.MethodDelete,
		"application/json",
		`{"confirmation":"delete-something-else"}`,
	)
	assertRequestError(
		testContext,
		wrongDelete,
		http.StatusUnprocessableEntity,
		"invalid_deletion_confirmation",
	)
	deleteResponse := mutateNetflix(
		testContext,
		config,
		restartedServer.URL+netflixProviderPath,
		http.MethodDelete,
		"application/json",
		`{"confirmation":"delete-netflix-provider"}`,
	)
	if deleteResponse.StatusCode != http.StatusNoContent {
		testContext.Fatalf("provider delete status = %d; body=%s", deleteResponse.StatusCode, readBody(testContext, deleteResponse))
	}
	deleteResponse.Body.Close()
	if deleted := requestNetflixSnapshot(testContext, restartedServer.URL); deleted.State != netflixlibrary.ProviderStateEmpty ||
		deleted.Active != nil ||
		deleted.LatestFailed != nil {
		testContext.Fatalf("HTTP provider deletion left data: %+v", deleted)
	}
	if strings.Contains(logOutput.String(), "Synthetic Film") ||
		strings.Contains(logOutput.String(), httpSyntheticViewingCSV) {
		testContext.Fatalf("HTTP logs exposed private Netflix data: %s", logOutput.String())
	}
}

func TestNetflixHTTPInvalidCSVFailsTypedWithoutSourceOrActiveData(
	testContext *testing.T,
) {
	config := testRuntimeConfig(testContext)
	handler, handlerError := newApplicationHandler(
		config,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if handlerError != nil {
		testContext.Fatalf("create Netflix failure handler: %v", handlerError)
	}
	defer handler.Close()
	server := newAuthenticatedTestServer(testContext, config, handler, defaultTestUserID)
	defer server.Close()

	createResponse := mutateNetflix(
		testContext,
		config,
		server.URL+netflixGenerationsPath,
		http.MethodPost,
		"application/json",
		`{"analysis_level":"local"}`,
	)
	var created generationResponse
	decodeResponse(testContext, createResponse, &created)
	generationID := created.Generation.ID
	uploadResponse := mutateNetflix(
		testContext,
		config,
		server.URL+netflixGenerationsPath+"/"+generationID+"/viewing-activity",
		http.MethodPut,
		"text/csv",
		"Title,Wrong\nPrivate Failure Title,1/1/26\n",
	)
	if uploadResponse.StatusCode != http.StatusAccepted {
		testContext.Fatalf("invalid CSV stage status = %d", uploadResponse.StatusCode)
	}
	uploadResponse.Body.Close()
	failed := waitForHTTPSnapshot(
		testContext,
		server.URL,
		func(snapshot netflixlibrary.Snapshot) bool {
			return snapshot.LatestFailed != nil &&
				snapshot.LatestFailed.ID == generationID
		},
	)
	if failed.Active != nil ||
		failed.Building != nil ||
		failed.LatestFailed.Failure == nil ||
		failed.LatestFailed.Failure.Code != netflixlibrary.ErrorInvalidHeader {
		testContext.Fatalf("invalid CSV state = %+v", failed)
	}
	sourcePath := filepath.Join(
		testUserWorkspace(testContext, config, defaultTestUserID).Root().Path(),
		"providers",
		"netflix",
		"generations",
		generationID,
		"viewing-activity.csv",
	)
	if _, statError := os.Stat(sourcePath); !errors.Is(statError, os.ErrNotExist) {
		testContext.Fatalf("failed HTTP generation retained source upload: %v", statError)
	}
}

func TestNetflixHTTPEnrichesFiltersAndStreamsCanonicalCSV(
	testContext *testing.T,
) {
	config := testRuntimeConfig(testContext)
	var logOutput bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logOutput, nil))
	metadataClient := newHTTPMetadataClient()
	handler, handlerError := newApplicationHandlerWithNetflixMetadata(
		config,
		logger,
		metadataClient,
	)
	if handlerError != nil {
		testContext.Fatalf("create Netflix enrichment handler: %v", handlerError)
	}
	defer handler.Close()
	server := newAuthenticatedTestServer(testContext, config, handler, defaultTestUserID)
	defer server.Close()

	initial := requestNetflixSnapshot(testContext, server.URL)
	if !initial.Capabilities.TMDBConfigured ||
		initial.Capabilities.TMDBAttribution != tmdb.CreditsAttribution() {
		testContext.Fatalf("unexpected TMDB HTTP capabilities: %+v", initial.Capabilities)
	}
	createLocal := mutateNetflix(
		testContext,
		config,
		server.URL+netflixGenerationsPath,
		http.MethodPost,
		"application/json",
		`{"analysis_level":"local"}`,
	)
	if createLocal.StatusCode != http.StatusCreated {
		testContext.Fatalf(
			"create local generation status = %d; body=%s",
			createLocal.StatusCode,
			readBody(testContext, createLocal),
		)
	}
	var local generationResponse
	decodeResponse(testContext, createLocal, &local)
	uploadResponse := mutateNetflix(
		testContext,
		config,
		server.URL+netflixGenerationsPath+"/"+local.Generation.ID+
			"/viewing-activity",
		http.MethodPut,
		"text/csv",
		httpSyntheticViewingCSV,
	)
	if uploadResponse.StatusCode != http.StatusAccepted {
		testContext.Fatalf(
			"upload local generation status = %d; body=%s",
			uploadResponse.StatusCode,
			readBody(testContext, uploadResponse),
		)
	}
	uploadResponse.Body.Close()
	waitForHTTPSnapshot(
		testContext,
		server.URL,
		func(snapshot netflixlibrary.Snapshot) bool {
			return snapshot.Active != nil &&
				snapshot.Active.ID == local.Generation.ID
		},
	)

	localExport := getResponse(
		testContext,
		server.URL+netflixGenerationsPath+"/"+local.Generation.ID+"/export",
	)
	assertRequestError(
		testContext,
		localExport,
		http.StatusConflict,
		string(netflixlibrary.ErrorInvalidState),
	)
	missingConsent := mutateNetflix(
		testContext,
		config,
		server.URL+netflixGenerationsPath,
		http.MethodPost,
		"application/json",
		`{"analysis_level":"tmdb","source_generation_id":"`+
			local.Generation.ID+`","locale":"en-US"}`,
	)
	assertRequestError(
		testContext,
		missingConsent,
		http.StatusUnprocessableEntity,
		string(netflixlibrary.ErrorConsentRequired),
	)
	if calls := metadataClient.searchCallSnapshot(); len(calls) != 0 {
		testContext.Fatalf("missing consent reached TMDB: %#v", calls)
	}

	createTMDB := mutateNetflix(
		testContext,
		config,
		server.URL+netflixGenerationsPath,
		http.MethodPost,
		"application/json",
		`{"analysis_level":"tmdb","source_generation_id":"`+
			local.Generation.ID+
			`","locale":"en-US","tmdb_title_query_consent":"`+
			netflixTMDBQueryConsent+`"}`,
	)
	if createTMDB.StatusCode != http.StatusCreated {
		testContext.Fatalf(
			"create TMDB generation status = %d; body=%s",
			createTMDB.StatusCode,
			readBody(testContext, createTMDB),
		)
	}
	var enriched generationResponse
	decodeResponse(testContext, createTMDB, &enriched)
	ready := waitForHTTPSnapshot(
		testContext,
		server.URL,
		func(snapshot netflixlibrary.Snapshot) bool {
			return snapshot.Active != nil &&
				snapshot.Active.ID == enriched.Generation.ID &&
				snapshot.Active.AnalysisLevel == netflixlibrary.AnalysisLevelTMDB
		},
	)
	if ready.Active.SourceGenerationID != local.Generation.ID ||
		ready.Active.CompletedTitleCount != 3 ||
		ready.Active.MatchedTitleCount != 1 ||
		ready.Active.ReviewTitleCount != 1 ||
		ready.Active.UnmatchedTitleCount != 1 ||
		ready.Active.ProgressPercent != 100 {
		testContext.Fatalf("unexpected enriched HTTP generation: %+v", ready.Active)
	}

	reviewResponse := getResponse(
		testContext,
		server.URL+netflixGenerationsPath+"/"+enriched.Generation.ID+
			"/records?match_status=review&limit=10",
	)
	if reviewResponse.StatusCode != http.StatusOK {
		testContext.Fatalf(
			"review records status = %d; body=%s",
			reviewResponse.StatusCode,
			readBody(testContext, reviewResponse),
		)
	}
	var reviewPage netflixlibrary.ActivityPage
	decodeResponse(testContext, reviewResponse, &reviewPage)
	if len(reviewPage.Records) != 2 {
		testContext.Fatalf("review records = %+v", reviewPage)
	}
	for _, record := range reviewPage.Records {
		if record.Match == nil ||
			record.Match.Status != netflix.MatchStatusReview ||
			record.Metadata != nil {
			testContext.Fatalf("invalid HTTP review record: %+v", record)
		}
	}
	filteredReviewResponse := getResponse(
		testContext,
		server.URL+netflixGenerationsPath+"/"+enriched.Generation.ID+
			"/records?start_date=2026-02-01&end_date=2026-02-28&match_status=review&limit=10",
	)
	if filteredReviewResponse.StatusCode != http.StatusOK {
		testContext.Fatalf(
			"filtered review records status = %d; body=%s",
			filteredReviewResponse.StatusCode,
			readBody(testContext, filteredReviewResponse),
		)
	}
	var filteredReviewPage netflixlibrary.ActivityPage
	decodeResponse(testContext, filteredReviewResponse, &filteredReviewPage)
	if len(filteredReviewPage.Records) != 1 ||
		filteredReviewPage.Records[0].DateISO != "2026-02-02" ||
		filteredReviewPage.Filter.MatchStatus != netflix.MatchStatusReview {
		testContext.Fatalf("unexpected shared HTTP record filter: %+v", filteredReviewPage)
	}

	analyticsResponse := getResponse(
		testContext,
		server.URL+netflixGenerationsPath+"/"+enriched.Generation.ID+"/analytics",
	)
	if analyticsResponse.StatusCode != http.StatusOK {
		testContext.Fatalf(
			"enriched analytics status = %d; body=%s",
			analyticsResponse.StatusCode,
			readBody(testContext, analyticsResponse),
		)
	}
	var analytics netflixlibrary.Analytics
	decodeResponse(testContext, analyticsResponse, &analytics)
	if len(analytics.Data.MatchStatusTitles) != 3 ||
		len(analytics.Data.MatchStatusActivities) != 3 {
		testContext.Fatalf("incomplete HTTP match analytics: %+v", analytics.Data)
	}
	filteredAnalyticsResponse := getResponse(
		testContext,
		server.URL+netflixGenerationsPath+"/"+enriched.Generation.ID+
			"/analytics?start_date=2026-02-01&end_date=2026-02-28&match_status=review",
	)
	if filteredAnalyticsResponse.StatusCode != http.StatusOK {
		testContext.Fatalf(
			"filtered review analytics status = %d; body=%s",
			filteredAnalyticsResponse.StatusCode,
			readBody(testContext, filteredAnalyticsResponse),
		)
	}
	var filteredAnalytics netflixlibrary.Analytics
	decodeResponse(testContext, filteredAnalyticsResponse, &filteredAnalytics)
	if filteredAnalytics.Data.ActivityCount != 1 ||
		filteredAnalytics.Data.UniqueTitleCount != 1 ||
		filteredAnalytics.Filter.StartDate != "2026-02-01" ||
		filteredAnalytics.Filter.MatchStatus != netflix.MatchStatusReview {
		testContext.Fatalf("unexpected shared HTTP analytics filter: %+v", filteredAnalytics)
	}

	exportResponse := getResponse(
		testContext,
		server.URL+netflixGenerationsPath+"/"+enriched.Generation.ID+"/export",
	)
	if exportResponse.StatusCode != http.StatusOK {
		testContext.Fatalf(
			"enriched export status = %d; body=%s",
			exportResponse.StatusCode,
			readBody(testContext, exportResponse),
		)
	}
	if exportResponse.Header.Get("Cache-Control") != "no-store" ||
		exportResponse.Header.Get("Content-Type") != "text/csv; charset=utf-8" ||
		!strings.Contains(
			exportResponse.Header.Get("Content-Disposition"),
			enriched.Generation.ID,
		) {
		testContext.Fatalf("unexpected enriched export headers: %v", exportResponse.Header)
	}
	exportBytes, readError := io.ReadAll(exportResponse.Body)
	exportResponse.Body.Close()
	if readError != nil {
		testContext.Fatalf("read enriched export: %v", readError)
	}
	csvLimits, limitsError := netflix.NewCSVLimits(
		product.MaxNetflixViewingRows,
		product.MaxNetflixTitleBytes,
		int(product.MaxNetflixEnrichmentOutcomeBytes),
	)
	if limitsError != nil {
		testContext.Fatalf("construct enriched export limits: %v", limitsError)
	}
	records, parseError := netflix.ReadEnrichedActivity(
		context.Background(),
		bytes.NewReader(exportBytes),
		csvLimits,
	)
	if parseError != nil || len(records) != 4 {
		testContext.Fatalf(
			"parse enriched HTTP export rows=%d error=%v",
			len(records),
			parseError,
		)
	}
	if calls := metadataClient.searchCallSnapshot(); len(calls) != 3 ||
		calls["Synthetic Film"] != 1 ||
		calls["Synthetic Series"] != 1 ||
		calls["Another Film"] != 1 {
		testContext.Fatalf("unexpected HTTP title queries: %#v", calls)
	}
	if strings.Contains(logOutput.String(), "Synthetic Film") ||
		strings.Contains(logOutput.String(), httpSyntheticViewingCSV) {
		testContext.Fatalf("HTTP enrichment logs exposed private data: %s", logOutput.String())
	}
	var temporaryExports []string
	if walkError := filepath.WalkDir(
		testUserWorkspace(testContext, config, defaultTestUserID).Root().Path(),
		func(path string, entry os.DirEntry, walkError error) error {
			if walkError != nil {
				return walkError
			}
			if !entry.IsDir() &&
				(strings.Contains(entry.Name(), "netflix-enriched") ||
					strings.HasSuffix(entry.Name(), ".csv")) {
				temporaryExports = append(temporaryExports, path)
			}
			return nil
		},
	); walkError != nil {
		testContext.Fatalf("audit private export files: %v", walkError)
	}
	if len(temporaryExports) != 0 {
		testContext.Fatalf("streamed export left files: %#v", temporaryExports)
	}
}

func mutateNetflix(
	testContext *testing.T,
	config runtimeconfig.Config,
	requestURL string,
	method string,
	contentType string,
	body string,
) *http.Response {
	testContext.Helper()
	request, requestError := http.NewRequest(method, requestURL, strings.NewReader(body))
	if requestError != nil {
		testContext.Fatalf("create Netflix mutation request: %v", requestError)
	}
	request.Header.Set("Origin", config.Authentication().PublicOrigin())
	request.Header.Set(csrfHeaderName, config.CSRFToken())
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, responseError := http.DefaultClient.Do(request)
	if responseError != nil {
		testContext.Fatalf("perform Netflix mutation request: %v", responseError)
	}
	return response
}

func requestNetflixSnapshot(
	testContext *testing.T,
	serverURL string,
) netflixlibrary.Snapshot {
	testContext.Helper()
	response := getResponse(testContext, serverURL+netflixProviderPath)
	if response.StatusCode != http.StatusOK {
		testContext.Fatalf("Netflix snapshot status = %d; body=%s", response.StatusCode, readBody(testContext, response))
	}
	var snapshot netflixlibrary.Snapshot
	decodeResponse(testContext, response, &snapshot)
	return snapshot
}

func waitForHTTPSnapshot(
	testContext *testing.T,
	serverURL string,
	ready func(netflixlibrary.Snapshot) bool,
) netflixlibrary.Snapshot {
	testContext.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := requestNetflixSnapshot(testContext, serverURL)
		if ready(snapshot) {
			return snapshot
		}
		time.Sleep(5 * time.Millisecond)
	}
	testContext.Fatalf("timed out waiting for Netflix HTTP state")
	return netflixlibrary.Snapshot{}
}

func getResponse(testContext *testing.T, requestURL string) *http.Response {
	testContext.Helper()
	response, requestError := http.Get(requestURL)
	if requestError != nil {
		testContext.Fatalf("perform GET %s: %v", requestURL, requestError)
	}
	return response
}

func decodeResponse(
	testContext *testing.T,
	response *http.Response,
	destination any,
) {
	testContext.Helper()
	defer response.Body.Close()
	if decodeError := json.NewDecoder(response.Body).Decode(destination); decodeError != nil {
		testContext.Fatalf("decode HTTP response: %v", decodeError)
	}
}

func assertRequestError(
	testContext *testing.T,
	response *http.Response,
	expectedStatus int,
	expectedCode string,
) {
	testContext.Helper()
	if response.StatusCode != expectedStatus {
		testContext.Fatalf(
			"request status = %d; want %d; body=%s",
			response.StatusCode,
			expectedStatus,
			readBody(testContext, response),
		)
	}
	var payload requestErrorResponse
	decodeResponse(testContext, response, &payload)
	if payload.Error.Code != expectedCode {
		testContext.Fatalf("request error code = %q; want %q", payload.Error.Code, expectedCode)
	}
}

func readBody(testContext *testing.T, response *http.Response) string {
	testContext.Helper()
	defer response.Body.Close()
	body, readError := io.ReadAll(response.Body)
	if readError != nil {
		testContext.Fatalf("read HTTP response: %v", readError)
	}
	return string(body)
}

const httpSyntheticViewingCSV = `Title,Date
Synthetic Film,1/1/26
Synthetic Series: Season 1: First,1/2/26
Synthetic Series: Season 1: Second,2/2/26
Another Film,2/3/26
`

type httpMetadataClient struct {
	mutex       sync.Mutex
	searchCalls map[string]int
}

func newHTTPMetadataClient() *httpMetadataClient {
	return &httpMetadataClient{searchCalls: make(map[string]int)}
}

func (client *httpMetadataClient) Identity() string {
	return tmdb.ClientIdentity
}

func (client *httpMetadataClient) Search(
	_ context.Context,
	query string,
	locale tmdb.Locale,
) ([]tmdb.Candidate, error) {
	if locale.String() != "en-US" {
		return nil, errors.New("unexpected HTTP enrichment locale")
	}
	client.mutex.Lock()
	client.searchCalls[query]++
	client.mutex.Unlock()
	switch query {
	case "Synthetic Film":
		return []tmdb.Candidate{{
			TMDBID:        2001,
			MediaType:     netflix.MediaTypeMovie,
			Title:         "Synthetic Film",
			OriginalTitle: "Synthetic Film",
			Popularity:    10,
		}}, nil
	case "Synthetic Series":
		return []tmdb.Candidate{
			{
				TMDBID:        2002,
				MediaType:     netflix.MediaTypeSeries,
				Title:         "Synthetic Series",
				OriginalTitle: "Synthetic Series",
				Popularity:    10,
			},
			{
				TMDBID:        2003,
				MediaType:     netflix.MediaTypeSeries,
				Title:         "Synthetic Series",
				OriginalTitle: "Synthetic Series",
				Popularity:    5,
			},
		}, nil
	case "Another Film":
		return []tmdb.Candidate{}, nil
	default:
		return nil, errors.New("unexpected HTTP derived title query")
	}
}

func (client *httpMetadataClient) Details(
	_ context.Context,
	candidate tmdb.Candidate,
	locale tmdb.Locale,
) (tmdb.Details, error) {
	if candidate.TMDBID != 2001 || locale.String() != "en-US" {
		return tmdb.Details{}, errors.New("unexpected HTTP details request")
	}
	runtimeMinutes := 98
	return tmdb.Details{
		TMDBID:           candidate.TMDBID,
		MediaType:        candidate.MediaType,
		Genres:           []string{"Documentary"},
		ReleaseDate:      "2025-01-02",
		RuntimeMinutes:   &runtimeMinutes,
		OriginalLanguage: "en",
		OriginCountries:  []string{"US"},
		MatchedTitle:     candidate.Title,
		Description:      "Synthetic HTTP metadata.",
	}, nil
}

func (client *httpMetadataClient) searchCallSnapshot() map[string]int {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	result := make(map[string]int, len(client.searchCalls))
	for query, count := range client.searchCalls {
		result[query] = count
	}
	return result
}
