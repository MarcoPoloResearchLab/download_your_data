package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/media"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/primevideo"
)

type primeMatcherCase struct {
	title           string
	query           string
	unresolvedQuery string
	candidates      []tmdb.Candidate
	acceptedID      int64
	status          string
}
type primeQualificationClient struct{ cases map[string]primeMatcherCase }

func (*primeQualificationClient) Identity() string { return tmdb.ClientIdentity }
func (client *primeQualificationClient) Search(_ context.Context, query string, _ tmdb.Locale) ([]tmdb.Candidate, error) {
	fixture, ok := client.cases[query]
	if !ok {
		return nil, errors.New("unexpected derived query")
	}
	return fixture.candidates, nil
}
func (*primeQualificationClient) Details(_ context.Context, candidate tmdb.Candidate, _ tmdb.Locale) (tmdb.Details, error) {
	return tmdb.Details{TMDBID: candidate.TMDBID, MediaType: candidate.MediaType, MatchedTitle: candidate.Title, Genres: []string{}}, nil
}

func TestPrimeMatcherEvaluationGate(testContext *testing.T) {
	candidate := func(id int64, kind netflix.MediaType, title string) tmdb.Candidate {
		return tmdb.Candidate{TMDBID: id, MediaType: kind, Title: title}
	}
	cases := []primeMatcherCase{
		{title: "Synthetic Film", query: "Synthetic Film", candidates: []tmdb.Candidate{candidate(1, netflix.MediaTypeMovie, "Synthetic Film")}, acceptedID: 1, status: "matched"},
		{title: "Pilot-Synthetic Series Season 1", query: "Synthetic Series", candidates: []tmdb.Candidate{candidate(2, netflix.MediaTypeSeries, "Synthetic Series"), candidate(3, netflix.MediaTypeMovie, "Synthetic Series")}, acceptedID: 2, status: "matched"},
		{title: "Episode - Another-Series Season 12", query: "Another-Series", candidates: []tmdb.Candidate{candidate(4, netflix.MediaTypeSeries, "Another Series")}, acceptedID: 4, status: "matched"},
		{title: "A Movie-With Hyphens", query: "A Movie-With Hyphens", candidates: []tmdb.Candidate{candidate(5, netflix.MediaTypeMovie, "A Movie With Hyphens")}, acceptedID: 5, status: "matched"},
		{title: "La película", query: "La película", candidates: []tmdb.Candidate{{TMDBID: 6, MediaType: netflix.MediaTypeMovie, Title: "The Film", OriginalTitle: "La película"}}, acceptedID: 6, status: "matched"},
		{title: "Remake", query: "Remake", candidates: []tmdb.Candidate{candidate(7, netflix.MediaTypeMovie, "Remake"), candidate(8, netflix.MediaTypeMovie, "Remake")}, status: "review"},
		{title: "Collision", query: "Collision", candidates: []tmdb.Candidate{candidate(9, netflix.MediaTypeMovie, "Collision"), candidate(10, netflix.MediaTypeSeries, "Collision")}, status: "review"},
		{title: "Pilot-Wrong Kind Season 1", query: "Wrong Kind", unresolvedQuery: "Pilot-Wrong Kind", candidates: []tmdb.Candidate{candidate(11, netflix.MediaTypeMovie, "Wrong Kind")}, status: "review"},
		{title: "Popular Near Match", query: "Popular Near Match", candidates: []tmdb.Candidate{candidate(12, netflix.MediaTypeMovie, "Popular Near Match 2")}, status: "review"},
		{title: "A B", query: "A B", candidates: []tmdb.Candidate{candidate(13, netflix.MediaTypeMovie, "AB")}, status: "review"},
		{title: "Missing Title", query: "Missing Title", status: "unmatched"},
	}
	client := &primeQualificationClient{cases: map[string]primeMatcherCase{}}
	rows := []map[string]string{}
	for _, fixture := range cases {
		client.cases[fixture.query] = fixture
		rows = append(rows, map[string]string{"Title": fixture.title, "Playback Start Datetime (UTC)": "2026-02-02T01:00:00Z", "Playback End Datetime (UTC)": "2026-02-02T01:01:00Z", "Seconds Viewed": "60", "Material Type Description": "Feature", "Profile Type": "ADULT", "Is Autoplay": "No", "Is Deleted": "No"})
	}
	client.cases["Pilot-Synthetic Series"] = primeMatcherCase{}
	client.cases["Pilot-Wrong Kind"] = primeMatcherCase{}
	client.cases["Episode - Another-Series"] = primeMatcherCase{}
	config := testRuntimeConfig(testContext)
	handler, err := newApplicationHandlerWithNetflixMetadata(config, slog.New(slog.NewTextHandler(io.Discard, nil)), client)
	if err != nil {
		testContext.Fatal(err)
	}
	defer handler.Close()
	server := newAuthenticatedTestServer(testContext, config, handler, defaultTestUserID)
	defer server.Close()
	active := importPrimeForTest(testContext, config, server.URL, syntheticPrimeArchiveWithRows(testContext, rows), []string{"viewing"})
	response := mutateNetflix(testContext, config, server.URL+testPrimeProviderPath+"/generations", http.MethodPost, "application/json", `{"analysis_level":"tmdb","source_generation_id":"`+active.ID+`","locale":"en-US","tmdb_title_query_consent":"authorize-tmdb-title-queries"}`)
	if response.StatusCode != http.StatusAccepted {
		testContext.Fatal(readBody(testContext, response))
	}
	response.Body.Close()
	waitForPrimeSnapshot(testContext, server.URL, func(snapshot primevideo.Snapshot) bool {
		return snapshot.Active != nil && snapshot.Active.AnalysisLevel == "tmdb"
	})
	var report media.Report
	decodeResponse(testContext, getResponse(testContext, server.URL+"/api/viewing-history"), &report)
	expected := map[string]primeMatcherCase{}
	for _, fixture := range cases {
		expected[fixture.title] = fixture
	}
	accepted, correct, positives := 0, 0, 0
	for _, record := range report.Records {
		fixture := expected[record.Title]
		if fixture.acceptedID > 0 {
			positives++
		}
		expectedQuery := fixture.query
		if fixture.unresolvedQuery != "" {
			expectedQuery = fixture.unresolvedQuery
		}
		if record.MatchStatus != fixture.status || record.SearchTitle != expectedQuery {
			testContext.Fatalf("matcher outcome for %q: %+v", fixture.title, record)
		}
		if record.Metadata != nil {
			accepted++
			if record.Metadata.TMDBID == fixture.acceptedID {
				correct++
			}
		}
		if fixture.status == "matched" && fixture.query != fixture.title && (record.SeasonNumber == 0 || record.EpisodeIdentity == "") {
			testContext.Fatal("episode identity evidence missing")
		}
	}
	precision := float64(correct) / float64(accepted)
	recall := float64(correct) / float64(positives)
	testContext.Logf("Prime matcher qualification: fixtures=%d precision=%.3f recall=%.3f", len(cases), precision, recall)
	if len(report.Records) != len(cases) || precision != 1 || recall < 0.9 {
		testContext.Fatal("Prime matcher qualification gate failed")
	}
}
