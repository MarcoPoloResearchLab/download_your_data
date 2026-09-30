package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/media"
	netflixlibrary "github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/library"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/primevideo"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/runtimeconfig"
)

const testPrimeProviderPath = "/api/providers/prime-video"

type controlledPrimeClient struct {
	*httpMetadataClient
	started    chan struct{}
	canceled   chan struct{}
	release    chan struct{}
	once       sync.Once
	fail       bool
	blockQuery string
}

func (client *controlledPrimeClient) Search(ctx context.Context, query string, locale tmdb.Locale) ([]tmdb.Candidate, error) {
	if client.release != nil && (client.blockQuery == "" || client.blockQuery == query) {
		client.once.Do(func() { close(client.started) })
		select {
		case <-client.release:
		case <-ctx.Done():
			close(client.canceled)
			return nil, ctx.Err()
		}
	}
	if client.fail {
		return nil, errors.New("injected remote failure")
	}
	return client.httpMetadataClient.Search(ctx, query, locale)
}

func waitForPrimeSnapshot(testContext *testing.T, serverURL string, predicate func(primevideo.Snapshot) bool) primevideo.Snapshot {
	testContext.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var snapshot primevideo.Snapshot
		decodeResponse(testContext, getResponse(testContext, serverURL+testPrimeProviderPath), &snapshot)
		if predicate(snapshot) {
			return snapshot
		}
		if time.Now().After(deadline) {
			testContext.Fatalf("Prime operation timed out: %+v", snapshot)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestPrimeHTTPEnrichmentRestartCancellationAndFailurePreserveActive(testContext *testing.T) {
	for _, scenario := range []string{"restart", "cancel", "failure", "delete-user"} {
		testContext.Run(scenario, func(testContext *testing.T) {
			config := testRuntimeConfig(testContext)
			client := &controlledPrimeClient{httpMetadataClient: newHTTPMetadataClient(), started: make(chan struct{}), canceled: make(chan struct{}), release: make(chan struct{}), fail: scenario == "failure"}
			if client.fail {
				close(client.release)
			}
			if scenario == "restart" {
				client.blockQuery = "Another Film"
			}
			handler, err := newApplicationHandlerWithNetflixMetadata(config, slog.New(slog.NewTextHandler(io.Discard, nil)), client)
			if err != nil {
				testContext.Fatal(err)
			}
			defer handler.Close()
			server := newAuthenticatedTestServer(testContext, config, handler, defaultTestUserID)
			defer server.Close()
			archive := syntheticPrimeArchive(testContext)
			activities := 1
			if scenario == "restart" {
				rows := []map[string]string{}
				for _, title := range []string{"Synthetic Film", "Another Film"} {
					rows = append(rows, map[string]string{"Title": title, "Playback Start Datetime (UTC)": "2026-02-02T01:00:00Z", "Playback End Datetime (UTC)": "2026-02-02T01:01:00Z", "Seconds Viewed": "60", "Material Type Description": "Feature", "Profile Type": "ADULT", "Is Autoplay": "No", "Is Deleted": "No"})
				}
				archive = syntheticPrimeArchiveWithRows(testContext, rows)
				activities = 2
			}
			active := importPrimeForTest(testContext, config, server.URL, archive, []string{"viewing"})
			payload := `{"analysis_level":"tmdb","source_generation_id":"` + active.ID + `","locale":"en-US"}`
			response := mutateNetflix(testContext, config, server.URL+testPrimeProviderPath+"/generations", http.MethodPost, "application/json", payload)
			if response.StatusCode != http.StatusAccepted {
				testContext.Fatal(readBody(testContext, response))
			}
			var pending primeGenerationResponse
			decodeResponse(testContext, response, &pending)
			select {
			case <-client.started:
			case <-time.After(5 * time.Second):
				testContext.Fatal("Prime query did not start")
			}
			var report media.Report
			decodeResponse(testContext, getResponse(testContext, server.URL+"/api/viewing-history"), &report)
			if report.Overview.ActivityCount != activities || report.Sources[0].GenerationID != active.ID {
				testContext.Fatal("replacement interrupted the active report")
			}
			switch scenario {
			case "restart":
				snapshot := waitForPrimeSnapshot(testContext, server.URL, func(snapshot primevideo.Snapshot) bool {
					return snapshot.Building != nil && snapshot.Building.CompletedTitles == 1
				})
				if snapshot.Building.TotalTitles != 2 {
					testContext.Fatal("checkpoint coverage missing")
				}
				server.Close()
				if err := handler.Close(); err != nil {
					testContext.Fatal(err)
				}
				resumedClient := newHTTPMetadataClient()
				restarted, err := newApplicationHandlerWithNetflixMetadata(config, slog.New(slog.NewTextHandler(io.Discard, nil)), resumedClient)
				if err != nil {
					testContext.Fatal(err)
				}
				defer restarted.Close()
				server = newAuthenticatedTestServer(testContext, config, restarted, defaultTestUserID)
				defer server.Close()
				resumed := mutateNetflix(testContext, config, server.URL+testPrimeProviderPath+"/generations", http.MethodPost, "application/json", payload)
				if resumed.StatusCode != http.StatusAccepted {
					testContext.Fatal(readBody(testContext, resumed))
				}
				var same primeGenerationResponse
				decodeResponse(testContext, resumed, &same)
				if same.Generation.ID != pending.Generation.ID {
					testContext.Fatal("restart discarded generation checkpoints")
				}
				waitForPrimeSnapshot(testContext, server.URL, func(snapshot primevideo.Snapshot) bool {
					return snapshot.Active != nil && snapshot.Active.ID == pending.Generation.ID
				})
				calls := resumedClient.searchCallSnapshot()
				if calls["Synthetic Film"] != 0 || calls["Another Film"] != 1 {
					testContext.Fatalf("restart repeated a completed query: %+v", calls)
				}
			case "cancel":
				for range 2 {
					deleted := mutateNetflix(testContext, config, server.URL+testPrimeProviderPath+"/generations/"+pending.Generation.ID, http.MethodDelete, "application/json", "")
					if deleted.StatusCode != http.StatusNoContent {
						testContext.Fatal(readBody(testContext, deleted))
					}
					deleted.Body.Close()
				}
				select {
				case <-client.canceled:
				case <-time.After(5 * time.Second):
					testContext.Fatal("deletion did not cancel the remote request")
				}
				waitForPrimeSnapshot(testContext, server.URL, func(snapshot primevideo.Snapshot) bool {
					return snapshot.Building == nil && snapshot.Active.ID == active.ID
				})
			case "failure":
				waitForPrimeSnapshot(testContext, server.URL, func(snapshot primevideo.Snapshot) bool {
					return snapshot.Building != nil && snapshot.Building.State == primevideo.Failed && snapshot.Active.ID == active.ID
				})
			case "delete-user":
				deleted := mutateNetflix(testContext, config, server.URL+userWorkspacePath, http.MethodDelete, "application/json", `{"confirmation":"`+userWorkspaceDeleteConfirmation+`"}`)
				if deleted.StatusCode != http.StatusNoContent {
					testContext.Fatal(readBody(testContext, deleted))
				}
				deleted.Body.Close()
				select {
				case <-client.canceled:
				case <-time.After(5 * time.Second):
					testContext.Fatal("user deletion did not cancel the query")
				}
				waitForPrimeSnapshot(testContext, server.URL, func(snapshot primevideo.Snapshot) bool { return snapshot.Active == nil && snapshot.Building == nil })
			}
		})
	}
}

func TestPrimeHTTPAnalysisJoinsAcceptedNetflixIdentity(testContext *testing.T) {
	config := testRuntimeConfig(testContext)
	client := newHTTPMetadataClient()
	handler, err := newApplicationHandlerWithNetflixMetadata(config, slog.New(slog.NewTextHandler(io.Discard, nil)), client)
	if err != nil {
		testContext.Fatal(err)
	}
	defer handler.Close()
	server := newAuthenticatedTestServer(testContext, config, handler, defaultTestUserID)
	defer server.Close()
	prime := importPrimeForTest(testContext, config, server.URL, syntheticPrimeArchive(testContext), []string{"viewing", "searches", "purchases"})
	url := server.URL + testPrimeProviderPath + "/generations"
	created := mutateNetflix(testContext, config, server.URL+netflixGenerationsPath, http.MethodPost, "application/json", `{"analysis_level":"local"}`)
	var local generationResponse
	decodeResponse(testContext, created, &local)
	uploaded := mutateNetflix(testContext, config, server.URL+netflixGenerationsPath+"/"+local.Generation.ID+"/viewing-activity", http.MethodPut, "text/csv", "Title,Date\nSynthetic Film,2/1/26\n")
	uploaded.Body.Close()
	waitForHTTPSnapshot(testContext, server.URL, func(snapshot netflixlibrary.Snapshot) bool { return snapshot.Active != nil })
	var rawReport struct {
		Overview struct {
			TopTitles []struct {
				ID        string           `json:"id"`
				Providers []media.Provider `json:"providers"`
			} `json:"top_titles"`
		} `json:"overview"`
	}
	decodeResponse(testContext, getResponse(testContext, server.URL+"/api/viewing-history"), &rawReport)
	if len(rawReport.Overview.TopTitles) != 2 || rawReport.Overview.TopTitles[0].ID == rawReport.Overview.TopTitles[1].ID || len(rawReport.Overview.TopTitles[0].Providers) != 1 {
		testContext.Fatal("top titles joined source names before an accepted identity")
	}
	netflixEnrichment := mutateNetflix(testContext, config, server.URL+netflixGenerationsPath, http.MethodPost, "application/json", `{"analysis_level":"tmdb","source_generation_id":"`+local.Generation.ID+`","locale":"en-US"}`)
	if netflixEnrichment.StatusCode != http.StatusCreated {
		testContext.Fatal(readBody(testContext, netflixEnrichment))
	}
	netflixEnrichment.Body.Close()
	waitForHTTPSnapshot(testContext, server.URL, func(snapshot netflixlibrary.Snapshot) bool {
		return snapshot.Active != nil && snapshot.Active.AnalysisLevel == netflixlibrary.AnalysisLevelTMDB
	})
	enriched := mutateNetflix(testContext, config, url, http.MethodPost, "application/json", `{"analysis_level":"tmdb","source_generation_id":"`+prime.ID+`","locale":"en-US"}`)
	if enriched.StatusCode != http.StatusAccepted {
		testContext.Fatalf("Prime enrichment = %d: %s", enriched.StatusCode, readBody(testContext, enriched))
	}
	var operation primeGenerationResponse
	decodeResponse(testContext, enriched, &operation)
	deadline := time.Now().Add(5 * time.Second)
	for {
		var snapshot primevideo.Snapshot
		decodeResponse(testContext, getResponse(testContext, server.URL+testPrimeProviderPath), &snapshot)
		if snapshot.Active != nil && snapshot.Active.ID == operation.Generation.ID {
			break
		}
		if time.Now().After(deadline) {
			testContext.Fatalf("Prime enrichment incomplete: %+v", snapshot)
		}
		time.Sleep(5 * time.Millisecond)
	}
	var report media.Report
	decodeResponse(testContext, getResponse(testContext, server.URL+"/api/viewing-history"), &report)
	if len(report.Titles) != 1 || len(report.Titles[0].Providers) != 2 || report.Titles[0].ID != "tmdb:movie:2001" || report.Overview.RecordedSeconds != 120 {
		testContext.Fatalf("accepted shared identity or recorded time failed: %+v", report)
	}
	for query := range client.searchCallSnapshot() {
		if query != "Synthetic Film" {
			testContext.Fatalf("unauthorized noncontent query: %s", query)
		}
	}
	var titleHistory media.Report
	filtered := getResponse(testContext, server.URL+"/api/viewing-history?title_id=tmdb%3Amovie%3A2001")
	if filtered.StatusCode != http.StatusOK {
		testContext.Fatalf("accepted title history: %d %s", filtered.StatusCode, readBody(testContext, filtered))
	}
	decodeResponse(testContext, filtered, &titleHistory)
	if titleHistory.Overview.ActivityCount != 2 {
		testContext.Fatal("shared title history missing records")
	}
	for _, record := range titleHistory.Records {
		if record.Metadata == nil || record.Metadata.TMDBID != 2001 {
			testContext.Fatal("title history included another identity")
		}
	}
}

func TestMediaHTTPCombinesNetflixAndPrimeAndExportsEveryFilteredRecord(testContext *testing.T) {
	config := testRuntimeConfig(testContext)
	handler, err := newApplicationHandler(config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		testContext.Fatal(err)
	}
	defer handler.Close()
	server := newAuthenticatedTestServer(testContext, config, handler, defaultTestUserID)
	defer server.Close()
	importPrimeForTest(testContext, config, server.URL, syntheticPrimeArchive(testContext), []string{"viewing", "playback_details"})
	created := mutateNetflix(testContext, config, server.URL+netflixGenerationsPath, http.MethodPost, "application/json", `{"analysis_level":"local"}`)
	var generation generationResponse
	decodeResponse(testContext, created, &generation)
	labeled := mutateNetflix(testContext, config, server.URL+netflixGenerationsPath+"/"+generation.Generation.ID+"/profile-label", http.MethodPut, "application/json", `{"label":"Synthetic Netflix profile"}`)
	if labeled.StatusCode != http.StatusOK {
		testContext.Fatalf("profile label: %d %s", labeled.StatusCode, readBody(testContext, labeled))
	}
	labeled.Body.Close()
	uploaded := mutateNetflix(testContext, config, server.URL+netflixGenerationsPath+"/"+generation.Generation.ID+"/viewing-activity", http.MethodPut, "text/csv", "Title,Date\nSynthetic Film,2/1/26\nSynthetic Series: Season 1: Pilot,2/2/26\n")
	if uploaded.StatusCode != http.StatusAccepted {
		testContext.Fatal(readBody(testContext, uploaded))
	}
	uploaded.Body.Close()
	waitForHTTPSnapshot(testContext, server.URL, func(snapshot netflixlibrary.Snapshot) bool { return snapshot.Active != nil })
	response := getResponse(testContext, server.URL+"/api/viewing-history?timezone=America%2FLos_Angeles&limit=2")
	var report media.Report
	decodeResponse(testContext, response, &report)
	if report.Overview.ActivityCount != 3 || len(report.Overview.Services) != 2 || report.Overview.RecordedSeconds != 120 || report.NextCursor == "" {
		testContext.Fatalf("invalid combined measures: %+v", report.Overview)
	}
	pageResponse := getResponse(testContext, server.URL+"/api/viewing-history?timezone=America%2FLos_Angeles&limit=2&cursor="+report.NextCursor)
	var next media.Report
	decodeResponse(testContext, pageResponse, &next)
	if len(next.Records) != 2 || next.NextCursor == "" {
		testContext.Fatalf("invalid combined pagination: %+v", next)
	}
	stale := getResponse(testContext, server.URL+"/api/viewing-history?timezone=UTC&limit=2&cursor="+report.NextCursor)
	assertRequestError(testContext, stale, http.StatusBadRequest, "invalid_query")
	filtered := getResponse(testContext, server.URL+"/api/viewing-history?timezone=America%2FLos_Angeles&start_date=2026-02-01&end_date=2026-02-01&title=Synthetic%20Film")
	var day media.Report
	decodeResponse(testContext, filtered, &day)
	if day.Overview.ActivityCount != 2 {
		testContext.Fatalf("shared date/title filter: %+v", day.Overview)
	}
	for _, record := range day.Records {
		if record.Provider == media.Netflix && record.ProfileLabel != "Synthetic Netflix profile" {
			testContext.Fatal("Netflix import label missing")
		}
	}
	export := getResponse(testContext, server.URL+"/api/viewing-history/export?timezone=America%2FLos_Angeles&start_date=2026-02-01&end_date=2026-02-01&title=Synthetic%20Film")
	rows, err := csv.NewReader(strings.NewReader(readBody(testContext, export))).ReadAll()
	if err != nil {
		testContext.Fatal(err)
	}
	if len(rows) != 4 {
		testContext.Fatalf("combined export rows=%d", len(rows))
	}
}

func TestPrimeHTTPIsolationReplacementRestartAndDeletion(testContext *testing.T) {
	config := testRuntimeConfig(testContext)
	handler, err := newApplicationHandler(config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		testContext.Fatal(err)
	}
	server := newAuthenticatedTestServer(testContext, config, handler, defaultTestUserID)
	archive := syntheticPrimeArchive(testContext)
	active := importPrimeForTest(testContext, config, server.URL, archive, []string{"viewing"})
	repeated := importPrimeForTest(testContext, config, server.URL, archive, []string{"viewing"})
	if active.ID != repeated.ID {
		testContext.Fatalf("repeat archive changed identity")
	}
	create := mutateNetflix(testContext, config, server.URL+testPrimeProviderPath+"/generations", http.MethodPost, "application/json", `{}`)
	var pending primeGenerationResponse
	decodeResponse(testContext, create, &pending)
	invalid := mutateNetflix(testContext, config, server.URL+testPrimeProviderPath+"/generations/"+pending.Generation.ID+"/archive", http.MethodPut, "application/zip", "invalid")
	assertRequestError(testContext, invalid, http.StatusUnprocessableEntity, "invalid_archive")
	var snapshot primevideo.Snapshot
	decodeResponse(testContext, getResponse(testContext, server.URL+testPrimeProviderPath), &snapshot)
	if snapshot.Active.ID != active.ID || snapshot.Building.State != "failed" {
		testContext.Fatalf("failed replacement changed active data")
	}
	server.Close()
	if err = handler.Close(); err != nil {
		testContext.Fatal(err)
	}
	restarted, err := newApplicationHandler(config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		testContext.Fatal(err)
	}
	defer restarted.Close()
	first := newAuthenticatedTestServer(testContext, config, restarted, defaultTestUserID)
	defer first.Close()
	decodeResponse(testContext, getResponse(testContext, first.URL+testPrimeProviderPath), &snapshot)
	if snapshot.Active == nil || snapshot.Active.ID != active.ID {
		testContext.Fatal("restart lost Prime library")
	}
	other := newAuthenticatedTestServer(testContext, config, restarted, authorizationTestUserB)
	defer other.Close()
	crossUser := getResponse(testContext, other.URL+testPrimeProviderPath+"/generations/"+active.ID)
	assertRequestError(testContext, crossUser, http.StatusNotFound, "not_found")
	var otherSnapshot primevideo.Snapshot
	decodeResponse(testContext, getResponse(testContext, other.URL+testPrimeProviderPath), &otherSnapshot)
	if otherSnapshot.Active != nil {
		testContext.Fatal("cross-user Prime library leak")
	}
	deleted := mutateNetflix(testContext, config, first.URL+testPrimeProviderPath, http.MethodDelete, "application/json", `{"confirmation":"delete-prime-video-provider"}`)
	if deleted.StatusCode != http.StatusNoContent {
		testContext.Fatal(readBody(testContext, deleted))
	}
	deleted.Body.Close()
	var report media.Report
	decodeResponse(testContext, getResponse(testContext, first.URL+"/api/viewing-history"), &report)
	if len(report.Records) != 0 {
		testContext.Fatal("deleted Prime data remained")
	}
	unauthenticated := httptest.NewServer(restarted)
	defer unauthenticated.Close()
	for _, route := range []string{testPrimeProviderPath, "/api/viewing-history", "/api/viewing-history/export"} {
		response, err := http.Get(unauthenticated.URL + route)
		if err != nil {
			testContext.Fatal(err)
		}
		assertRequestError(testContext, response, http.StatusUnauthorized, "session_required")
	}
}

func TestPrimePrivateExportAcceptance(testContext *testing.T) {
	source := os.Getenv("F023_ACCEPTANCE_PRIME_ARCHIVE")
	if source == "" {
		testContext.Skip("private export acceptance is explicitly enabled outside routine CI")
	}
	encoded, err := os.ReadFile(source)
	if err != nil {
		testContext.Fatal("private acceptance input unavailable")
	}
	if _, err := primevideo.ParseArchive(context.Background(), encoded); err != nil {
		testContext.Fatalf("private archive boundary: %v; reason: %v", err, errors.Unwrap(err))
	}
	config := testRuntimeConfig(testContext)
	handler, err := newApplicationHandler(config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		testContext.Fatal(err)
	}
	defer handler.Close()
	server := newAuthenticatedTestServer(testContext, config, handler, defaultTestUserID)
	defer server.Close()
	generation := importPrimeForTest(testContext, config, server.URL, encoded, []string{"viewing", "playback_details", "watch_events", "searches", "purchases", "trailers"})
	var report media.Report
	decodeResponse(testContext, getResponse(testContext, server.URL+"/api/viewing-history"), &report)
	testContext.Logf("Private Prime acceptance: generation records=%d, default activities=%d, recorded seconds=%.3f, rentals=%d, purchases=%d", generation.RecordCount, report.Overview.ActivityCount, report.Overview.RecordedSeconds, report.Overview.Rentals, report.Overview.Purchases)
	if generation.RecordCount != 8861 || report.Overview.Rentals != 87 || report.Overview.Purchases != 47 {
		testContext.Fatal("private source aggregate mismatch")
	}
}

func importPrimeForTest(testContext *testing.T, config runtimeconfig.Config, serverURL string, archive []byte, datasets []string) primevideo.Generation {
	testContext.Helper()
	response := mutateNetflix(testContext, config, serverURL+testPrimeProviderPath+"/generations", http.MethodPost, "application/json", `{}`)
	if response.StatusCode != http.StatusCreated {
		testContext.Fatal(readBody(testContext, response))
	}
	var created primeGenerationResponse
	decodeResponse(testContext, response, &created)
	url := serverURL + testPrimeProviderPath + "/generations/" + created.Generation.ID
	uploaded := mutateNetflix(testContext, config, url+"/archive", http.MethodPut, "application/zip", string(archive))
	if uploaded.StatusCode != http.StatusOK {
		testContext.Fatalf("import failed: %s", readBody(testContext, uploaded))
	}
	uploaded.Body.Close()
	encoded, err := json.Marshal(primeSelectionRequest{Datasets: datasets, ProfileLabel: "Synthetic household"})
	if err != nil {
		testContext.Fatal(err)
	}
	confirmed := mutateNetflix(testContext, config, url+"/selection", http.MethodPut, "application/json", string(encoded))
	if confirmed.StatusCode != http.StatusOK {
		testContext.Fatal(readBody(testContext, confirmed))
	}
	var ready primeGenerationResponse
	decodeResponse(testContext, confirmed, &ready)
	return ready.Generation
}

func TestPrimeHTTPPreviewSelectionAndUnifiedReport(testContext *testing.T) {
	config := testRuntimeConfig(testContext)
	handler, err := newApplicationHandler(config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		testContext.Fatal(err)
	}
	defer handler.Close()
	server := newAuthenticatedTestServer(testContext, config, handler, defaultTestUserID)
	defer server.Close()
	created := mutateNetflix(testContext, config, server.URL+testPrimeProviderPath+"/generations", http.MethodPost, "application/json", `{}`)
	if created.StatusCode != http.StatusCreated {
		testContext.Fatalf("Prime creation = %d: %s", created.StatusCode, readBody(testContext, created))
	}
	var generation struct {
		Generation struct {
			ID string `json:"id"`
		} `json:"generation"`
	}
	decodeResponse(testContext, created, &generation)
	generationURL := server.URL + testPrimeProviderPath + "/generations/" + generation.Generation.ID
	archive := syntheticPrimeArchive(testContext)
	uploaded := mutateNetflix(testContext, config, generationURL+"/archive", http.MethodPut, "application/zip", string(archive))
	if uploaded.StatusCode != http.StatusOK {
		testContext.Fatalf("Prime archive = %d: %s", uploaded.StatusCode, readBody(testContext, uploaded))
	}
	var preview struct {
		Generation struct {
			State   string `json:"state"`
			Preview struct {
				Datasets []struct {
					ID   string `json:"id"`
					Rows int    `json:"rows"`
				} `json:"datasets"`
			} `json:"preview"`
		} `json:"generation"`
	}
	decodeResponse(testContext, uploaded, &preview)
	retriedUpload := mutateNetflix(testContext, config, generationURL+"/archive", http.MethodPut, "application/zip", string(archive))
	if retriedUpload.StatusCode != http.StatusOK {
		testContext.Fatal("identical PUT archive retry failed")
	}
	retriedUpload.Body.Close()
	if preview.Generation.State != "awaiting_confirmation" || len(preview.Generation.Preview.Datasets) != 6 {
		testContext.Fatalf("invalid preview: %+v", preview)
	}
	confirmed := mutateNetflix(testContext, config, generationURL+"/selection", http.MethodPut, "application/json", `{"datasets":["viewing","playback_details","purchases"],"profile_label":"Household"}`)
	if confirmed.StatusCode != http.StatusOK {
		testContext.Fatalf("Prime selection = %d: %s", confirmed.StatusCode, readBody(testContext, confirmed))
	}
	confirmed.Body.Close()
	reportResponse := getResponse(testContext, server.URL+"/api/viewing-history?timezone=America%2FLos_Angeles")
	if reportResponse.StatusCode != http.StatusOK {
		testContext.Fatalf("unified report = %d: %s", reportResponse.StatusCode, readBody(testContext, reportResponse))
	}
	var report struct {
		Overview struct {
			ActivityCount   int     `json:"activity_count"`
			RecordedSeconds float64 `json:"recorded_seconds"`
		} `json:"overview"`
		Records []struct {
			Title    string `json:"title"`
			Date     string `json:"date"`
			Provider string `json:"provider"`
			Kind     string `json:"kind"`
		} `json:"records"`
	}
	decodeResponse(testContext, reportResponse, &report)
	if report.Overview.ActivityCount != 1 || report.Overview.RecordedSeconds != 120 {
		testContext.Fatalf("inflated content measures: %+v", report.Overview)
	}
	if len(report.Records) != 4 {
		testContext.Fatalf("selected records=%d", len(report.Records))
	}
	for _, record := range report.Records {
		if record.Provider != "prime-video" || record.Kind == "search" || record.Kind == "watch_summary" {
			testContext.Fatalf("unselected source retained: %+v", record)
		}
		if record.Title == "Synthetic Film" && record.Kind == "playback" && record.Date != "2026-02-01" {
			testContext.Fatalf("timezone lost: %+v", record)
		}
	}
	exported := getResponse(testContext, server.URL+"/api/viewing-history/export?timezone=America%2FLos_Angeles")
	if exported.StatusCode != http.StatusOK {
		testContext.Fatalf("export=%d", exported.StatusCode)
	}
	contents := readBody(testContext, exported)
	if !strings.Contains(contents, "prime-video") || !strings.Contains(contents, "date_precision") || !strings.Contains(contents, "recorded_seconds") {
		testContext.Fatalf("missing combined export evidence")
	}
}

func syntheticPrimeArchive(testContext *testing.T) []byte {
	return syntheticPrimeArchiveWithRows(testContext, nil)
}

func syntheticPrimeArchiveWithRows(testContext *testing.T, viewingRows []map[string]string) []byte {
	testContext.Helper()
	encoded, err := os.ReadFile("testdata/prime_video_headers.json")
	if err != nil {
		testContext.Fatal(err)
	}
	var headers map[string][]string
	if err = json.Unmarshal(encoded, &headers); err != nil {
		testContext.Fatal(err)
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for name, columns := range headers {
		entry, err := writer.Create(name)
		if err != nil {
			testContext.Fatal(err)
		}
		csvWriter := csv.NewWriter(entry)
		if err = csvWriter.Write(columns); err != nil {
			testContext.Fatal(err)
		}
		var rows []map[string]string
		switch {
		case strings.HasSuffix(name, "/Viewing History.csv"):
			rows = []map[string]string{
				{"Title": "\"Synthetic Film\"", "Playback Start Datetime (UTC)": "2026-02-02T01:00:00Z", "Playback End Datetime (UTC)": "2026-02-02T01:02:00Z", "Seconds Viewed": "120.00000", "Material Type Description": "\"Feature\"", "Profile Type": "\"ADULT\"", "Is Autoplay": "\"No\"", "Is Deleted": "\"No\"", "Device Model": "\"Synthetic device\"", "Audio Language Code": "\"en\""},
				{"Title": "\"Synthetic Promo\"", "Playback Start Datetime (UTC)": "2026-02-02T01:02:00Z", "Playback End Datetime (UTC)": "2026-02-02T01:02:30Z", "Seconds Viewed": "30", "Material Type Description": "\"Promo\"", "Profile Type": "\"CHILD\"", "Is Autoplay": "\"Yes\"", "Is Deleted": "\"No\""},
				{"Title": "\"Synthetic Film\"", "Playback Start Datetime (UTC)": "2026-02-02T01:03:00Z", "Playback End Datetime (UTC)": "2026-02-02T01:03:00Z", "Seconds Viewed": "0", "Material Type Description": "\"Feature\"", "Profile Type": "\"ADULT\"", "Is Autoplay": "\"No\"", "Is Deleted": "\"No\""},
			}
			if viewingRows != nil {
				rows = viewingRows
			}
		case strings.HasSuffix(name, "/Watch Events.csv"):
			rows = []map[string]string{{"Title Name": "Synthetic Film", "Most Recent Watch Date": "2026-02-02T01:02:00Z", "Seconds Watched": "120", "Deleted from Watch History": "no"}}
		case strings.HasSuffix(name, "/Search History.csv"):
			rows = []map[string]string{{"Search Query from Customer": "Private search words", "Search Request Date": "2026-02-02T00:55:00Z"}}
		case strings.HasSuffix(name, "/Purchases and Rentals.csv"):
			rows = []map[string]string{{"Title": "\"Synthetic Film\"", "Origin Time": "2026-02-02T00:50:00Z", "Grant Time": "2026-02-02T00:50:00Z", "Offer Type": "RENTAL"}}
		default:
			rows = []map[string]string{{"Promoting Titles or Services": "Synthetic Film", "Trailer Watch Date": "2026-02-02T00:45:00Z"}}
		}
		for _, row := range rows {
			values := make([]string, len(columns))
			for index, column := range columns {
				values[index] = "Not Available"
				if value, ok := row[column]; ok {
					values[index] = value
				}
			}
			if err = csvWriter.Write(values); err != nil {
				testContext.Fatal(err)
			}
		}
		csvWriter.Flush()
		if err = csvWriter.Error(); err != nil {
			testContext.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		testContext.Fatal(err)
	}
	return output.Bytes()
}
