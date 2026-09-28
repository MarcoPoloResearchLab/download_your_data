package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/media"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/primevideo"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/runtimeconfig"
)

func TestPrimeHTTPTitleHintsKeepAmbiguousMatchesSeparate(testContext *testing.T) {
	for _, sourceOrder := range [][]string{
		{"Fargo Season 1", "Fargo"},
		{"Fargo", "Fargo Season 1"},
		{"Pilot - Fargo Season 1", "Fargo"},
		{"Fargo", "Pilot - Fargo Season 1"},
		{"Pilot-Fargo Season 1", "Fargo"},
		{"Fargo", "Pilot-Fargo Season 1"},
	} {
		testContext.Run(sourceOrder[0], func(testContext *testing.T) {
			client := &primeQualificationClient{cases: map[string]primeMatcherCase{
				"Pilot-Fargo":   {},
				"Pilot - Fargo": {},
				"Fargo": {candidates: []tmdb.Candidate{
					{TMDBID: 1, MediaType: netflix.MediaTypeMovie, Title: "Fargo"},
					{TMDBID: 2, MediaType: netflix.MediaTypeSeries, Title: "Fargo"},
				}},
			}}
			before, after := enrichPrimeReviewTitles(testContext, sourceOrder, client)
			if before.Records[0].TitleIdentity == before.Records[1].TitleIdentity {
				testContext.Fatal("series and unclassified source titles share an identity")
			}
			for _, record := range after.Records {
				expectedEpisode := "Pilot"
				if record.Title == "Fargo Season 1" {
					expectedEpisode = ""
				}
				if record.Title == "Fargo" {
					if record.MatchStatus != "review" || record.Metadata != nil {
						testContext.Fatal("series outcome accepted the ambiguous bare title")
					}
				} else if record.MatchStatus != "matched" || record.Metadata == nil || record.Metadata.MediaType != "series" || record.SeriesTitle != "Fargo" || record.EpisodeTitle != expectedEpisode {
					testContext.Fatalf("series evidence missing: %+v", record)
				}
			}
		})
	}
}

func TestPrimeHTTPHyphenatedNamesRequireUniqueInterpretation(testContext *testing.T) {
	client := &primeQualificationClient{cases: map[string]primeMatcherCase{
		"The X-Files":       {candidates: []tmdb.Candidate{{TMDBID: 10, MediaType: netflix.MediaTypeSeries, Title: "The X-Files"}}},
		"Files":             {},
		"Pilot-The X-Files": {},
		"Double-Meaning":    {candidates: []tmdb.Candidate{{TMDBID: 11, MediaType: netflix.MediaTypeSeries, Title: "Double-Meaning"}}},
		"Meaning":           {candidates: []tmdb.Candidate{{TMDBID: 12, MediaType: netflix.MediaTypeSeries, Title: "Meaning"}}},
		"Alpha - Beta":      {candidates: []tmdb.Candidate{{TMDBID: 13, MediaType: netflix.MediaTypeSeries, Title: "Alpha - Beta"}}},
		"Beta":              {},
	}}
	before, after := enrichPrimeReviewTitles(testContext, []string{"The X-Files Season 1", "Pilot-The X-Files Season 1", "Double-Meaning Season 1", "Alpha - Beta Season 1"}, client)
	if before.Overview.EpisodeCount != 0 {
		testContext.Fatal("ambiguous hyphens fabricated source episodes")
	}
	for _, record := range before.Records {
		if record.Title == "The X-Files Season 1" && (record.SeriesTitle != "The X-Files" || record.SearchTitle != "The X-Files" || record.EpisodeTitle != "") {
			testContext.Fatalf("hyphenated series name changed: %+v", record)
		}
	}
	for _, record := range after.Records {
		switch record.Title {
		case "The X-Files Season 1":
			if record.MatchStatus != "matched" || record.SeriesTitle != "The X-Files" || record.EpisodeTitle != "" {
				testContext.Fatalf("series-only interpretation changed: %+v", record)
			}
		case "Pilot-The X-Files Season 1":
			if record.MatchStatus != "matched" || record.SeriesTitle != "The X-Files" || record.EpisodeTitle != "Pilot" {
				testContext.Fatalf("unique episode interpretation missing: %+v", record)
			}
		case "Double-Meaning Season 1":
			if record.MatchStatus != "review" || record.Metadata != nil || record.EpisodeTitle != "" {
				testContext.Fatal("conflicting series interpretations were accepted")
			}
		case "Alpha - Beta Season 1":
			if record.MatchStatus != "matched" || record.SeriesTitle != "Alpha - Beta" || record.EpisodeTitle != "" {
				testContext.Fatalf("spaced hyphen changed the series name: %+v", record)
			}
		}
	}
	if after.Overview.EpisodeCount != 1 {
		testContext.Fatal("confirmed episode count missing")
	}
}

func enrichPrimeReviewTitles(testContext *testing.T, titles []string, client *primeQualificationClient) (media.Report, media.Report) {
	testContext.Helper()
	config := testRuntimeConfig(testContext)
	handler, err := newApplicationHandlerWithNetflixMetadata(config, slog.New(slog.NewTextHandler(io.Discard, nil)), client)
	if err != nil {
		testContext.Fatal(err)
	}
	defer handler.Close()
	server := newAuthenticatedTestServer(testContext, config, handler, defaultTestUserID)
	defer server.Close()
	rows := []map[string]string{}
	for _, title := range titles {
		rows = append(rows, map[string]string{"Title": title, "Playback Start Datetime (UTC)": "2026-02-02T01:00:00Z", "Seconds Viewed": "60", "Material Type Description": "Feature"})
	}
	active := importPrimeForTest(testContext, config, server.URL, syntheticPrimeArchiveWithRows(testContext, rows), []string{"viewing"})
	var before media.Report
	decodeResponse(testContext, getResponse(testContext, server.URL+viewingHistoryPath), &before)
	enrichPrimeForTest(testContext, config, server.URL, active.ID)
	var after media.Report
	decodeResponse(testContext, getResponse(testContext, server.URL+viewingHistoryPath), &after)
	return before, after
}

func enrichPrimeForTest(testContext *testing.T, config runtimeconfig.Config, baseURL, sourceID string) {
	testContext.Helper()
	response := mutateNetflix(testContext, config, baseURL+primeGenerationsPath, http.MethodPost, "application/json", `{"analysis_level":"tmdb","source_generation_id":"`+sourceID+`","locale":"en-US","tmdb_title_query_consent":"authorize-tmdb-title-queries"}`)
	if response.StatusCode != http.StatusAccepted {
		testContext.Fatal(readBody(testContext, response))
	}
	response.Body.Close()
	waitForPrimeSnapshot(testContext, baseURL, func(snapshot primevideo.Snapshot) bool {
		return snapshot.Active != nil && snapshot.Active.AnalysisLevel == "tmdb"
	})
}

func TestMediaHTTPReportsRevisionConflictsForBothCursors(testContext *testing.T) {
	config := testRuntimeConfig(testContext)
	handler, err := newApplicationHandler(config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		testContext.Fatal(err)
	}
	defer handler.Close()
	server := newAuthenticatedTestServer(testContext, config, handler, defaultTestUserID)
	defer server.Close()
	rows := []map[string]string{}
	for _, title := range []string{"Before One", "Before Two"} {
		rows = append(rows, map[string]string{"Title": title, "Playback Start Datetime (UTC)": "2026-02-02T01:00:00Z", "Seconds Viewed": "60", "Material Type Description": "Feature"})
	}
	importPrimeForTest(testContext, config, server.URL, syntheticPrimeArchiveWithRows(testContext, rows), []string{"viewing"})
	var before media.Report
	decodeResponse(testContext, getResponse(testContext, server.URL+viewingHistoryPath+"?limit=1"), &before)
	rows[0]["Title"] = "After One"
	importPrimeForTest(testContext, config, server.URL, syntheticPrimeArchiveWithRows(testContext, rows), []string{"viewing"})
	for parameter, cursor := range map[string]string{"cursor": before.NextCursor, "titles_cursor": before.NextTitlesCursor} {
		response := getResponse(testContext, server.URL+viewingHistoryPath+"?limit=1&"+parameter+"="+cursor)
		assertRequestError(testContext, response, http.StatusConflict, "stale_cursor")
	}
}
