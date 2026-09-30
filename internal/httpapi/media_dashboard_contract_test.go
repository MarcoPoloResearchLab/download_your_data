package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"testing"

	netflixlibrary "github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/library"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/primevideo"
)

func TestMediaHTTPDashboardAnalysisWithoutPermissionPayload(testContext *testing.T) {
	config := testRuntimeConfig(testContext)
	handler, err := newApplicationHandlerWithNetflixMetadata(config, slog.New(slog.NewTextHandler(io.Discard, nil)), newHTTPMetadataClient())
	if err != nil {
		testContext.Fatal(err)
	}
	defer handler.Close()
	server := newAuthenticatedTestServer(testContext, config, handler, defaultTestUserID)
	defer server.Close()
	active := importPrimeForTest(testContext, config, server.URL, syntheticPrimeArchive(testContext), []string{"viewing", "playback_details"})
	response := mutateNetflix(testContext, config, server.URL+primeGenerationsPath, http.MethodPost, "application/json", `{"analysis_level":"tmdb","source_generation_id":"`+active.ID+`","locale":"en-US"}`)
	if response.StatusCode != http.StatusAccepted {
		testContext.Fatalf("automatic title analysis = %d: %s", response.StatusCode, readBody(testContext, response))
	}
	response.Body.Close()
	waitForPrimeSnapshot(testContext, server.URL, func(snapshot primevideo.Snapshot) bool {
		return snapshot.Active != nil && snapshot.Active.AnalysisLevel == "tmdb"
	})
	created := mutateNetflix(testContext, config, server.URL+netflixGenerationsPath, http.MethodPost, "application/json", `{"analysis_level":"local"}`)
	var local generationResponse
	decodeResponse(testContext, created, &local)
	upload := mutateNetflix(testContext, config, server.URL+netflixGenerationsPath+"/"+local.Generation.ID+"/viewing-activity", http.MethodPut, "text/csv", "Title,Date\nSynthetic Film,1/1/25\nSynthetic Film,2/1/26\n")
	upload.Body.Close()
	waitForHTTPSnapshot(testContext, server.URL, func(snapshot netflixlibrary.Snapshot) bool { return snapshot.Active != nil })
	response = mutateNetflix(testContext, config, server.URL+netflixGenerationsPath, http.MethodPost, "application/json", `{"analysis_level":"tmdb","source_generation_id":"`+local.Generation.ID+`","locale":"en-US"}`)
	if response.StatusCode != http.StatusCreated {
		testContext.Fatalf("Netflix automatic analysis = %d: %s", response.StatusCode, readBody(testContext, response))
	}
	response.Body.Close()
	waitForHTTPSnapshot(testContext, server.URL, func(snapshot netflixlibrary.Snapshot) bool {
		return snapshot.Active != nil && snapshot.Active.AnalysisLevel == netflixlibrary.AnalysisLevelTMDB
	})
	var report struct {
		Contract string `json:"contract"`
		Records  []any  `json:"records"`
		Overview struct {
			Activities int `json:"activity_count"`
			MediaTypes []struct {
				Label string
				Count int
			} `json:"media_types"`
			Monthly []struct {
				Period string
				Label  string
				Count  int
			} `json:"monthly_media"`
			Weekdays []struct {
				Period string
				Label  string
				Count  int
			} `json:"genres_by_weekday"`
			Years []struct {
				Period string
				Label  string
				Count  int
			} `json:"genres_by_year"`
			Languages []struct {
				Label string
				Count int
			} `json:"original_languages"`
		} `json:"overview"`
	}
	decodeResponse(testContext, getResponse(testContext, server.URL+viewingHistoryPath+"?limit=1"), &report)
	if report.Contract != "viewing-history-report-v2" || report.Overview.Activities != 3 || len(report.Records) != 1 {
		testContext.Fatalf("complete dashboard contract: %+v", report)
	}
	if len(report.Overview.MediaTypes) != 1 || report.Overview.MediaTypes[0].Label != "movie" || report.Overview.MediaTypes[0].Count != 3 {
		testContext.Fatalf("media split is paged or uses unique titles: %+v", report.Overview.MediaTypes)
	}
	if len(report.Overview.Monthly) != 2 || len(report.Overview.Weekdays) == 0 || len(report.Overview.Years) == 0 || len(report.Overview.Languages) != 1 || report.Overview.Languages[0].Count != 3 {
		testContext.Fatalf("dashboard datasets missing or paged: %+v", report.Overview)
	}
	decodeResponse(testContext, getResponse(testContext, server.URL+viewingHistoryPath+"?provider=netflix&start_date=2026-01-01&end_date=2026-12-31&media_type=movie"), &report)
	if report.Overview.Activities != 1 || len(report.Overview.Monthly) != 1 || report.Overview.Monthly[0].Period != "2026-02" || report.Overview.Languages[0].Count != 1 {
		testContext.Fatalf("chart filters diverge: %+v", report.Overview)
	}
	decodeResponse(testContext, getResponse(testContext, server.URL+viewingHistoryPath+"?media_type=series"), &report)
	if report.Overview.Activities != 0 || len(report.Overview.MediaTypes) != 0 || len(report.Overview.Years) != 0 {
		testContext.Fatal("empty chart filters retain stale data")
	}
	assertRequestError(testContext, getResponse(testContext, server.URL+viewingHistoryPath+"?media_type=invalid"), http.StatusBadRequest, "invalid_query")
}
