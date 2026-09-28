package httpapi

import (
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/media"
)

func TestMediaHTTPPreservesUnknownFlagsAndSeparatesEpisodeCounts(testContext *testing.T) {
	rows := []map[string]string{}
	for _, title := range []string{"Pilot-Synthetic Series Season 1", "Pilot-Synthetic Series Season 1", "Finale-Synthetic Series Season 1", "Not Available"} {
		rows = append(rows, map[string]string{"Title": title, "Playback Start Datetime (UTC)": "2026-02-02T01:00:00Z", "Seconds Viewed": "60", "Material Type Description": "Feature"})
	}
	config := testRuntimeConfig(testContext)
	handler, err := newApplicationHandler(config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		testContext.Fatal(err)
	}
	defer handler.Close()
	server := newAuthenticatedTestServer(testContext, config, handler, defaultTestUserID)
	defer server.Close()
	importPrimeForTest(testContext, config, server.URL, syntheticPrimeArchiveWithRows(testContext, rows), []string{"viewing"})
	response := getResponse(testContext, server.URL+"/api/viewing-history")
	var payload struct {
		Records []struct {
			Autoplay *bool `json:"autoplay"`
			Deleted  *bool `json:"deleted"`
		} `json:"records"`
		Overview struct {
			EpisodeCount      int           `json:"episode_count"`
			SeriesTitles      int           `json:"series_titles"`
			UnavailableTitles int           `json:"unavailable_title_records"`
			UniqueTitles      int           `json:"unique_title_count"`
			TopTitles         []media.Title `json:"top_titles"`
		} `json:"overview"`
	}
	decodeResponse(testContext, response, &payload)
	for _, record := range payload.Records {
		if record.Autoplay != nil || record.Deleted != nil {
			testContext.Fatal("unavailable flags became false")
		}
	}
	if payload.Overview.EpisodeCount != 2 || payload.Overview.SeriesTitles != 1 || payload.Overview.UnavailableTitles != 1 || payload.Overview.UniqueTitles != 1 {
		testContext.Fatalf("episode and title counts: %+v", payload.Overview)
	}
	for _, title := range payload.Overview.TopTitles {
		if title.Title == "" {
			testContext.Fatal("unavailable title became a blank top title")
		}
	}
}

func TestMediaHTTPExportRetainsPlaybackAndEpisodeEvidence(testContext *testing.T) {
	config := testRuntimeConfig(testContext)
	handler, err := newApplicationHandler(config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		testContext.Fatal(err)
	}
	defer handler.Close()
	server := newAuthenticatedTestServer(testContext, config, handler, defaultTestUserID)
	defer server.Close()
	rows := []map[string]string{{"Title": "Pilot-Synthetic Series Season 1", "Playback Start Datetime (UTC)": "2026-02-02T01:00:00Z", "Playback End Datetime (UTC)": "2026-02-02T01:02:00Z", "Seconds Viewed": "120", "Material Type Description": "Feature", "Is Autoplay": "Yes", "Is Deleted": "No", "Device Model": "Synthetic device", "Audio Language Code": "en"}}
	importPrimeForTest(testContext, config, server.URL, syntheticPrimeArchiveWithRows(testContext, rows), []string{"viewing", "playback_details"})
	var report media.Report
	decodeResponse(testContext, getResponse(testContext, server.URL+"/api/viewing-history?timezone=America%2FLos_Angeles"), &report)
	if len(report.Sources) != 1 || report.Sources[0].StartDate != "2026-02-01" || report.Sources[0].EndDate != "2026-02-01" {
		testContext.Fatal("source date coverage does not use the declared display timezone")
	}
	export := getResponse(testContext, server.URL+"/api/viewing-history/export?timezone=America%2FLos_Angeles")
	values, err := csv.NewReader(strings.NewReader(readBody(testContext, export))).ReadAll()
	if err != nil || len(values) != 2 {
		testContext.Fatalf("export rows: %d error=%v", len(values), err)
	}
	columns := map[string]string{}
	for index, name := range values[0] {
		columns[name] = values[1][index]
	}
	for key, expected := range map[string]string{"provider": "prime-video", "date": "2026-02-01", "display_timezone": "America/Los_Angeles", "end_timestamp_utc": "2026-02-02T01:02:00Z", "series_title": "Synthetic Series", "season_number": "1", "episode_title": "Pilot", "autoplay": "true", "deleted": "false", "device": "Synthetic device", "audio_language": "en"} {
		if columns[key] != expected {
			testContext.Errorf("export %s=%q want %q", key, columns[key], expected)
		}
	}
}

func TestMediaHTTPBoundsTitlesAndRejectsMixedCollectionCursors(testContext *testing.T) {
	rows := []map[string]string{}
	for index := range 5 {
		rows = append(rows, map[string]string{"Title": fmt.Sprintf("Synthetic Film %d", index), "Playback Start Datetime (UTC)": "2026-02-02T01:00:00Z", "Seconds Viewed": "60", "Material Type Description": "Feature", "Is Autoplay": "No", "Is Deleted": "No"})
	}
	config := testRuntimeConfig(testContext)
	handler, err := newApplicationHandler(config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		testContext.Fatal(err)
	}
	defer handler.Close()
	server := newAuthenticatedTestServer(testContext, config, handler, defaultTestUserID)
	defer server.Close()
	importPrimeForTest(testContext, config, server.URL, syntheticPrimeArchiveWithRows(testContext, rows), []string{"viewing"})
	var page struct {
		Titles      []media.Title `json:"titles"`
		NextTitles  string        `json:"next_titles_cursor"`
		NextRecords string        `json:"next_cursor"`
	}
	decodeResponse(testContext, getResponse(testContext, server.URL+"/api/viewing-history?limit=2"), &page)
	if len(page.Titles) != 2 || page.NextTitles == "" {
		testContext.Fatalf("unbounded titles: %+v", page)
	}
	bad := getResponse(testContext, server.URL+"/api/viewing-history?limit=2&titles_cursor="+page.NextRecords)
	assertRequestError(testContext, bad, http.StatusBadRequest, "invalid_query")
	first := page.Titles[0].ID
	decodeResponse(testContext, getResponse(testContext, server.URL+"/api/viewing-history?limit=2&titles_cursor="+page.NextTitles), &page)
	if len(page.Titles) != 2 || page.Titles[0].ID == first {
		testContext.Fatal("title cursor repeated the first page")
	}
}
