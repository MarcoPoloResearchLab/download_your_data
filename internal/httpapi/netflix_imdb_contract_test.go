package httpapi

import (
	"bytes"
	"context"
	"encoding/csv"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/library"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/runtimeconfig"
)

const imdbViewingCSV = "Title,Date\nIMDb Movie,1/1/26\nIMDb Series: Season 1: First,1/2/26\nAbsent ID,1/3/26\nReview,1/4/26\nUnmatched,1/5/26\n"

func TestNetflixHTTPIMDbIDsPersistThroughRestartCacheAndExport(testContext *testing.T) {
	config := testRuntimeConfig(testContext)
	client := &imdbMetadataClient{}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	handler, handlerError := newApplicationHandlerWithNetflixMetadata(config, logger, client)
	if handlerError != nil {
		testContext.Fatal(handlerError)
	}
	server := newAuthenticatedTestServer(testContext, config, handler, defaultTestUserID)
	localID := importIMDbLocalHTTP(testContext, config, server.URL)
	enrichedID := createIMDbEnrichmentHTTP(testContext, config, server.URL, localID)
	waitForHTTPSnapshot(testContext, server.URL, func(snapshot library.Snapshot) bool {
		return snapshot.Active != nil && snapshot.Active.ID == enrichedID
	})
	assertIMDbHTTPRecordsAndExport(testContext, server.URL, enrichedID)
	server.Close()
	if closeError := handler.Close(); closeError != nil {
		testContext.Fatal(closeError)
	}

	reopened, reopenError := newApplicationHandlerWithNetflixMetadata(config, logger, client)
	if reopenError != nil {
		testContext.Fatal(reopenError)
	}
	defer reopened.Close()
	server = newAuthenticatedTestServer(testContext, config, reopened, defaultTestUserID)
	assertIMDbHTTPRecordsAndExport(testContext, server.URL, enrichedID)
	queriesBeforeCache := client.searches.Load()
	localID = importIMDbLocalHTTP(testContext, config, server.URL)
	enrichedID = createIMDbEnrichmentHTTP(testContext, config, server.URL, localID)
	ready := waitForHTTPSnapshot(testContext, server.URL, func(snapshot library.Snapshot) bool {
		return snapshot.Active != nil && snapshot.Active.ID == enrichedID
	})
	if ready.Active.CacheHitTitleCount != 5 || client.searches.Load() != queriesBeforeCache {
		testContext.Fatal("enrichment did not reuse all five persisted cache outcomes")
	}
	assertIMDbHTTPRecordsAndExport(testContext, server.URL, enrichedID)
}

func TestNetflixHTTPInvalidIMDbResponseKeepsLocalGenerationActive(testContext *testing.T) {
	config := testRuntimeConfig(testContext)
	handler, handlerError := newApplicationHandlerWithNetflixMetadata(config,
		slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), &imdbMetadataClient{invalid: true})
	if handlerError != nil {
		testContext.Fatal(handlerError)
	}
	defer handler.Close()
	server := newAuthenticatedTestServer(testContext, config, handler, defaultTestUserID)
	localID := importIMDbLocalHTTP(testContext, config, server.URL)
	enrichedID := createIMDbEnrichmentHTTP(testContext, config, server.URL, localID)
	failed := waitForHTTPSnapshot(testContext, server.URL, func(snapshot library.Snapshot) bool {
		return snapshot.Building == nil && snapshot.LatestFailed != nil && snapshot.LatestFailed.ID == enrichedID
	})
	if failed.Active == nil || failed.Active.ID != localID ||
		failed.LatestFailed.Failure.Code != library.ErrorInvalidResponse {
		testContext.Fatalf("invalid IMDb response changed active data or error: %+v", failed)
	}
	response := getResponse(testContext, server.URL+netflixGenerationsPath+"/"+enrichedID+"/records?limit=10")
	assertRequestError(testContext, response, http.StatusConflict, string(library.ErrorInvalidState))
}

func importIMDbLocalHTTP(testContext *testing.T, config runtimeconfig.Config, serverURL string) string {
	testContext.Helper()
	response := mutateNetflix(testContext, config, serverURL+netflixGenerationsPath,
		http.MethodPost, "application/json", `{"analysis_level":"local"}`)
	if response.StatusCode != http.StatusCreated {
		testContext.Fatalf("create local generation: %s", readBody(testContext, response))
	}
	var created generationResponse
	decodeResponse(testContext, response, &created)
	response = mutateNetflix(testContext, config, serverURL+netflixGenerationsPath+"/"+created.Generation.ID+"/viewing-activity",
		http.MethodPut, "text/csv", imdbViewingCSV)
	if response.StatusCode != http.StatusAccepted {
		testContext.Fatalf("upload IMDb fixture: %s", readBody(testContext, response))
	}
	response.Body.Close()
	waitForHTTPSnapshot(testContext, serverURL, func(snapshot library.Snapshot) bool {
		return snapshot.Active != nil && snapshot.Active.ID == created.Generation.ID
	})
	return created.Generation.ID
}

func createIMDbEnrichmentHTTP(testContext *testing.T, config runtimeconfig.Config, serverURL, sourceID string) string {
	testContext.Helper()
	response := mutateNetflix(testContext, config, serverURL+netflixGenerationsPath,
		http.MethodPost, "application/json", `{"analysis_level":"tmdb","source_generation_id":"`+sourceID+
			`","locale":"en-US","tmdb_title_query_consent":"`+netflixTMDBQueryConsent+`"}`)
	if response.StatusCode != http.StatusCreated {
		testContext.Fatalf("create IMDb enrichment: %s", readBody(testContext, response))
	}
	var created generationResponse
	decodeResponse(testContext, response, &created)
	return created.Generation.ID
}

func assertIMDbHTTPRecordsAndExport(testContext *testing.T, serverURL, generationID string) {
	testContext.Helper()
	response := getResponse(testContext, serverURL+netflixGenerationsPath+"/"+generationID+"/records?limit=10")
	if response.StatusCode != http.StatusOK {
		testContext.Fatalf("read IMDb records: %s", readBody(testContext, response))
	}
	var page library.ActivityPage
	decodeResponse(testContext, response, &page)
	if len(page.Records) != 5 {
		testContext.Fatalf("IMDb records = %d; want 5", len(page.Records))
	}
	ids := map[string]string{"IMDb Movie": "tt0133093", "IMDb Series: Season 1: First": "tt12345678"}
	for _, record := range page.Records {
		expected := ids[record.RawTitle]
		if record.RawTitle == "Review" || record.RawTitle == "Unmatched" {
			if record.Metadata != nil {
				testContext.Fatal("review or unmatched outcome has metadata")
			}
			continue
		}
		if record.Metadata == nil || record.Metadata.IMDbID != expected ||
			(expected != "" && record.Metadata.IMDbIDSource != netflix.IMDbIDSource) ||
			(expected == "" && record.Metadata.IMDbIDSource != "") {
			testContext.Fatalf("IMDb API identity for %s: %+v", record.RawTitle, record.Metadata)
		}
	}
	response = getResponse(testContext, serverURL+netflixGenerationsPath+"/"+generationID+"/export")
	if response.StatusCode != http.StatusOK {
		testContext.Fatalf("export IMDb records: %s", readBody(testContext, response))
	}
	encoded := readBody(testContext, response)
	rows, parseError := csv.NewReader(strings.NewReader(encoded)).ReadAll()
	if parseError != nil || len(rows) != 6 || rows[0][30] != "imdb_id" || rows[0][31] != "imdb_id_source" {
		testContext.Fatalf("IMDb export header or rows are invalid: %v", parseError)
	}
	limits, limitsError := netflix.NewCSVLimits(product.MaxNetflixViewingRows, product.MaxNetflixTitleBytes,
		int(product.MaxNetflixEnrichmentOutcomeBytes))
	if limitsError != nil {
		testContext.Fatal(limitsError)
	}
	records, recordsError := netflix.ReadEnrichedActivity(context.Background(), strings.NewReader(encoded), limits)
	if recordsError != nil || len(records) != 5 {
		testContext.Fatalf("read current IMDb CSV: %v", recordsError)
	}
	for _, record := range records {
		if metadata, present := record.Metadata(); present && metadata.IMDbID() != ids[record.Activity().RawTitle()] {
			testContext.Fatal("IMDb CSV identity differs from API identity")
		}
	}
	for _, corrupt := range []struct{ identifier, source string }{
		{identifier: "nm0133093", source: netflix.IMDbIDSource},
		{identifier: "tt0133093", source: "imdb"},
		{identifier: "tt0133093"},
		{source: netflix.IMDbIDSource},
	} {
		rows[1][30], rows[1][31] = corrupt.identifier, corrupt.source
		var corrupted bytes.Buffer
		writer := csv.NewWriter(&corrupted)
		if writeError := writer.WriteAll(rows); writeError != nil {
			testContext.Fatal(writeError)
		}
		if _, readError := netflix.ReadEnrichedActivity(context.Background(), &corrupted, limits); readError == nil {
			testContext.Fatal("CSV accepted a malformed IMDb identity or foreign source")
		}
	}
}

type imdbMetadataClient struct {
	searches atomic.Int64
	invalid  bool
}

func (client *imdbMetadataClient) Identity() string { return tmdb.ClientIdentity }

func (client *imdbMetadataClient) Search(_ context.Context, query string, _ tmdb.Locale) ([]tmdb.Candidate, error) {
	client.searches.Add(1)
	if query == "Unmatched" {
		return []tmdb.Candidate{}, nil
	}
	mediaType := netflix.MediaTypeMovie
	if query == "IMDb Series" {
		mediaType = netflix.MediaTypeSeries
	}
	candidates := []tmdb.Candidate{{TMDBID: 603, MediaType: mediaType, Title: query, OriginalTitle: query}}
	if query == "Review" {
		candidates = append(candidates, tmdb.Candidate{TMDBID: 604, MediaType: mediaType, Title: query, OriginalTitle: query})
	}
	return candidates, nil
}

func (client *imdbMetadataClient) Details(_ context.Context, candidate tmdb.Candidate, _ tmdb.Locale) (tmdb.Details, error) {
	if client.invalid {
		return tmdb.Details{}, imdbResponseFailure{}
	}
	var identifier netflix.IMDbTitleID
	if candidate.Title != "Absent ID" {
		value := "tt0133093"
		if candidate.MediaType == netflix.MediaTypeSeries {
			value = "tt12345678"
		}
		var identifierError error
		identifier, identifierError = netflix.NewIMDbTitleID(value)
		if identifierError != nil {
			return tmdb.Details{}, identifierError
		}
	}
	return tmdb.Details{IMDbID: identifier, TMDBID: candidate.TMDBID, MediaType: candidate.MediaType,
		MatchedTitle: candidate.Title, Genres: []string{}, OriginCountries: []string{}}, nil
}

type imdbResponseFailure struct{}

func (imdbResponseFailure) Error() string        { return "invalid external IMDb ID" }
func (imdbResponseFailure) Code() tmdb.ErrorCode { return tmdb.ErrorInvalidResponse }
