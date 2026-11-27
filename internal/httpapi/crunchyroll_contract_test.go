package httpapi

import (
	"encoding/csv"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/authentication"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/media"
	netflixlibrary "github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/library"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/runtimeconfig"
)

const (
	testCrunchyrollPath      = "/api/providers/crunchyroll"
	crunchyrollTestCSVHeader = "history_date,series_or_collection,title,crunchyroll_status,format,crunchyroll_url\r\n"
	crunchyrollTestCSV       = "\ufeff" + crunchyrollTestCSVHeader +
		"2026-05-10,Synthetic Series,Pilot,Watched,Episode,https://www.crunchyroll.com/watch/G12345678/pilot\r\n" +
		"2026-05-11,Synthetic Series,Pilot,8m left,Episode,https://www.crunchyroll.com/watch/G12345678/pilot\r\n" +
		"2026-05-12,Synthetic Movies,Synthetic Movie,Watched,Movie,https://www.crunchyroll.com/watch/G12345679/movie\r\n" +
		"2026-05-13,Synthetic Movies,Synthetic Special,24m,Theatrical special,https://www.crunchyroll.com/watch/G12345670/special\r\n"
	crunchyrollTestDeleteBody = `{"confirmation":"delete-crunchyroll-provider"}`
)

type crunchyrollSnapshotReply struct {
	Revision string `json:"revision"`
	Active   *struct {
		ID          string `json:"id"`
		RecordCount int    `json:"record_count"`
		StartDate   string `json:"start_date"`
		EndDate     string `json:"end_date"`
	} `json:"active_generation"`
}

func TestCrunchyrollHTTPImportReportAndCSV(testContext *testing.T) {
	config := testRuntimeConfig(testContext)
	handler := newCrunchyrollTestHandler(testContext, config)
	server := newAuthenticatedTestServer(testContext, config, handler, defaultTestUserID)
	url := server.URL + testCrunchyrollPath
	initial := readCrunchyrollSnapshot(testContext, getResponse(testContext, url))
	if initial.Revision != "empty" || initial.Active != nil {
		testContext.Fatal("new Crunchyroll provider has an active snapshot")
	}

	preflight, err := http.NewRequest(http.MethodOptions, url, nil)
	if err != nil {
		testContext.Fatal(err)
	}
	preflight.Header.Set("Origin", config.Authentication().PublicOrigin())
	preflight.Header.Set("Access-Control-Request-Method", http.MethodPut)
	preflight.Header.Set("Access-Control-Request-Headers", "content-type, if-match, x-csrf-token")
	preflightReply, err := http.DefaultClient.Do(preflight)
	if err != nil {
		testContext.Fatal(err)
	}
	preflightReply.Body.Close()
	if preflightReply.StatusCode != http.StatusNoContent || !strings.Contains(strings.ToLower(preflightReply.Header.Get("Access-Control-Allow-Headers")), "if-match") {
		testContext.Fatal("cross-origin uploads cannot send their revision precondition")
	}

	response := mutateCrunchyroll(testContext, config, url, http.MethodPut, "text/csv", initial.Revision, crunchyrollTestCSV)
	if !strings.Contains(strings.ToLower(response.Header.Get("Access-Control-Expose-Headers")), "etag") {
		testContext.Fatal("cross-origin upload response does not expose its ETag")
	}
	imported := readCrunchyrollSnapshot(testContext, response)
	if imported.Active == nil || imported.Active.RecordCount != 4 || imported.Revision != imported.Active.ID ||
		imported.Active.StartDate != "2026-05-10" || imported.Active.EndDate != "2026-05-13" {
		testContext.Fatal("import lost rows, dates, or source revision")
	}

	importPrimeForTest(testContext, config, server.URL, syntheticPrimeArchive(testContext), []string{"viewing"})
	created := mutateNetflix(testContext, config, server.URL+netflixGenerationsPath, http.MethodPost, "application/json", `{"analysis_level":"local"}`)
	var netflix generationResponse
	decodeResponse(testContext, created, &netflix)
	uploaded := mutateNetflix(testContext, config, server.URL+netflixGenerationsPath+"/"+netflix.Generation.ID+"/viewing-activity", http.MethodPut, "text/csv", "Title,Date\nSynthetic Netflix Film,5/12/26\n")
	if uploaded.StatusCode != http.StatusAccepted {
		testContext.Fatal(readBody(testContext, uploaded))
	}
	uploaded.Body.Close()
	waitForHTTPSnapshot(testContext, server.URL, func(snapshot netflixlibrary.Snapshot) bool { return snapshot.Active != nil })

	historyURL := server.URL + "/api/viewing-history"
	var combined media.Report
	decodeResponse(testContext, getResponse(testContext, historyURL), &combined)
	if len(combined.Sources) != 3 || combined.Overview.ActivityCount != 6 || combined.Overview.RecordedSeconds != 120 {
		testContext.Fatal("combined report did not preserve all three sources and their measures")
	}

	filter := "?provider=crunchyroll&timezone=America%2FLos_Angeles"
	var report media.Report
	decodeResponse(testContext, getResponse(testContext, historyURL+filter), &report)
	if len(report.Records) != 4 || len(report.Titles) != 3 || report.Overview.EpisodeCount != 1 ||
		report.Overview.MovieTitles != 1 || report.Overview.SeriesTitles != 1 ||
		report.Overview.TimedRecords != 0 || report.Overview.UnknownDurationRecords != 4 ||
		len(report.Overview.Services) != 1 || report.Overview.Services[0].Unit != "history entries" {
		testContext.Fatal("Crunchyroll report miscounted history entries, title types, episodes, or duration")
	}
	if report.Records[0].Date != "2026-05-13" || report.Records[3].Date != "2026-05-10" ||
		report.Records[3].DatePrecision != media.CalendarDate || report.Records[3].Completion != "unknown" ||
		report.Records[3].SeasonNumber != 0 || report.Records[2].ID == report.Records[3].ID ||
		report.Records[3].EpisodeIdentity == "" || report.Records[3].EpisodeIdentity != report.Records[2].EpisodeIdentity {
		testContext.Fatal("calendar dates, repeat rows, and episode identity were not preserved")
	}
	var filtered media.Report
	decodeResponse(testContext, getResponse(testContext, historyURL+filter+"&media_type=series&title_id="+report.Titles[0].ID), &filtered)
	if len(filtered.Records) != 2 || report.Titles[0].Title != "Synthetic Series" {
		testContext.Fatal("Crunchyroll series grouping or title filtering failed")
	}

	export := readCrunchyrollTestExport(testContext, historyURL+"/export"+filter)
	if len(export) != 4 || export[0]["source_format"] != "Theatrical special" || export[0]["media_type"] != "unknown" ||
		export[1]["media_type"] != "movie" || export[2]["source_status"] != "8m left" || export[3]["source_status"] != "Watched" ||
		export[3]["source_collection"] != "Synthetic Series" || export[3]["source_url"] != "https://www.crunchyroll.com/watch/G12345678/pilot" ||
		export[3]["counting_unit"] != "history entries" || export[3]["recorded_seconds"] != "" {
		testContext.Fatal("Crunchyroll export lost source provenance or inferred watch time")
	}
}

func TestCrunchyrollHTTPReplacementIsolationAndDeletion(testContext *testing.T) {
	config := testRuntimeConfig(testContext)
	handler := newCrunchyrollTestHandler(testContext, config)
	first := newAuthenticatedTestServer(testContext, config, handler, defaultTestUserID)
	url := first.URL + testCrunchyrollPath
	upload := func(revision, body string) *http.Response {
		return mutateCrunchyroll(testContext, config, url, http.MethodPut, "text/csv", revision, body)
	}
	assertRequestError(testContext, upload("", crunchyrollTestCSV), http.StatusPreconditionRequired, "revision_required")
	assertRequestError(testContext, upload("empty", crunchyrollTestCSVHeader), http.StatusUnprocessableEntity, "invalid_crunchyroll_csv")
	initial := readCrunchyrollSnapshot(testContext, upload("empty", crunchyrollTestCSV))
	repeated := readCrunchyrollSnapshot(testContext, upload(initial.Revision, crunchyrollTestCSV))
	if initial.Revision != repeated.Revision || repeated.Active == nil || repeated.Active.RecordCount != 4 {
		testContext.Fatal("repeat upload changed the generation or duplicated its rows")
	}
	assertRequestError(testContext, upload("empty", crunchyrollTestCSV), http.StatusPreconditionFailed, "stale_revision")
	assertRequestError(testContext, upload(initial.Revision, crunchyrollTestCSV+"bad row\r\n"), http.StatusUnprocessableEntity, "invalid_crunchyroll_csv")
	if after := readCrunchyrollSnapshot(testContext, getResponse(testContext, url)); after.Revision != initial.Revision {
		testContext.Fatal("invalid replacement changed the active generation")
	}

	historyURL := first.URL + "/api/viewing-history?provider=crunchyroll&limit=1"
	var page media.Report
	decodeResponse(testContext, getResponse(testContext, historyURL), &page)
	if page.NextCursor == "" {
		testContext.Fatal("expected a history cursor before replacement")
	}
	replacement := readCrunchyrollSnapshot(testContext, upload(initial.Revision, strings.Replace(crunchyrollTestCSV, "8m left", "Watched", 1)))
	if replacement.Revision == initial.Revision {
		testContext.Fatal("source status change did not change the revision")
	}
	assertRequestError(testContext, getResponse(testContext, historyURL+"&cursor="+page.NextCursor), http.StatusConflict, "stale_cursor")
	assertRequestError(testContext, mutateCrunchyroll(testContext, config, url, http.MethodDelete, "application/json", initial.Revision, crunchyrollTestDeleteBody), http.StatusPreconditionFailed, "stale_revision")

	first.Close()
	if err := handler.Close(); err != nil {
		testContext.Fatal(err)
	}
	restarted := newCrunchyrollTestHandler(testContext, config)
	first = newAuthenticatedTestServer(testContext, config, restarted, defaultTestUserID)
	other := newAuthenticatedTestServer(testContext, config, restarted, authorizationTestUserB)
	if after := readCrunchyrollSnapshot(testContext, getResponse(testContext, first.URL+testCrunchyrollPath)); after.Revision != replacement.Revision {
		testContext.Fatal("restart lost the imported history")
	}
	if isolated := readCrunchyrollSnapshot(testContext, getResponse(testContext, other.URL+testCrunchyrollPath)); isolated.Active != nil {
		testContext.Fatal("another account could see imported history")
	}
	unauthenticated := httptest.NewServer(restarted)
	defer unauthenticated.Close()
	assertRequestError(testContext, getResponse(testContext, unauthenticated.URL+testCrunchyrollPath), http.StatusUnauthorized, "session_required")

	importPrimeForTest(testContext, config, first.URL, syntheticPrimeArchive(testContext), []string{"viewing"})
	deleted := mutateCrunchyroll(testContext, config, first.URL+testCrunchyrollPath, http.MethodDelete, "application/json", replacement.Revision, crunchyrollTestDeleteBody)
	if deleted.StatusCode != http.StatusNoContent {
		testContext.Fatal(readBody(testContext, deleted))
	}
	deleted.Body.Close()
	var report media.Report
	decodeResponse(testContext, getResponse(testContext, first.URL+"/api/viewing-history"), &report)
	if len(report.Sources) != 1 || report.Sources[0].Provider != media.PrimeVideo {
		testContext.Fatal("Crunchyroll provider deletion did not leave the Prime library intact")
	}
	readCrunchyrollSnapshot(testContext, mutateCrunchyroll(testContext, config, first.URL+testCrunchyrollPath, http.MethodPut, "text/csv", "empty", crunchyrollTestCSV))
	workspaceDeleted := mutateNetflix(testContext, config, first.URL+userWorkspacePath, http.MethodDelete, "application/json", `{"confirmation":"delete-download-your-data-workspace"}`)
	if workspaceDeleted.StatusCode != http.StatusNoContent {
		testContext.Fatal(readBody(testContext, workspaceDeleted))
	}
	workspaceDeleted.Body.Close()
	if after := readCrunchyrollSnapshot(testContext, getResponse(testContext, first.URL+testCrunchyrollPath)); after.Active != nil {
		testContext.Fatal("workspace deletion left Crunchyroll history behind")
	}
}

func TestCrunchyrollHTTPPrivateExportAcceptance(testContext *testing.T) {
	filePath := os.Getenv("DOWNLOAD_YOUR_DATA_CRUNCHYROLL_ACCEPTANCE_CSV")
	if filePath == "" {
		testContext.Skip("private export acceptance is enabled separately")
	}
	encoded, err := os.ReadFile(filePath)
	if err != nil {
		testContext.Fatal("private acceptance CSV is unavailable")
	}
	config := testRuntimeConfig(testContext)
	handler := newCrunchyrollTestHandler(testContext, config)
	server := newAuthenticatedTestServer(testContext, config, handler, defaultTestUserID)
	response := mutateCrunchyroll(testContext, config, server.URL+testCrunchyrollPath, http.MethodPut, "text/csv", "empty", string(encoded))
	imported := readCrunchyrollSnapshot(testContext, response)
	export := readCrunchyrollTestExport(testContext, server.URL+"/api/viewing-history/export?provider=crunchyroll")
	watched := 0
	for _, row := range export {
		if row["source_status"] == "Watched" {
			watched++
		}
	}
	if imported.Active == nil || imported.Active.RecordCount != 934 || imported.Active.StartDate != "2026-05-10" ||
		imported.Active.EndDate != "2026-11-24" || len(export) != 934 || watched != 817 {
		testContext.Fatal("private acceptance aggregates differ from the recovered CSV")
	}
	testContext.Logf("Private Crunchyroll acceptance: %d rows, %d marked Watched, %s through %s", len(export), watched, imported.Active.StartDate, imported.Active.EndDate)
}

func TestCrunchyrollHTTPRejectsCorruptPersistenceAsServerError(testContext *testing.T) {
	config := testRuntimeConfig(testContext)
	handler := newCrunchyrollTestHandler(testContext, config)
	server := newAuthenticatedTestServer(testContext, config, handler, defaultTestUserID)
	url := server.URL + testCrunchyrollPath
	snapshot := readCrunchyrollSnapshot(testContext, mutateCrunchyroll(testContext, config, url, http.MethodPut, "text/csv", "empty", crunchyrollTestCSV))
	user, err := authentication.NewAuthenticatedUser(config.Authentication().TenantID(), defaultTestUserID)
	if err != nil {
		testContext.Fatal(err)
	}
	workspace, err := config.UserWorkspace(user)
	if err != nil {
		testContext.Fatal(err)
	}
	file, err := workspace.Root().File("providers/crunchyroll/library.json")
	if err != nil {
		testContext.Fatal(err)
	}
	encoded, err := os.ReadFile(file.Path())
	if err != nil {
		testContext.Fatal(err)
	}
	corrupt := strings.Replace(string(encoded), "2026-05-10", "invalid", 1)
	if err = file.Replace(func(writer io.Writer) error {
		_, writeErr := io.WriteString(writer, corrupt)
		return writeErr
	}); err != nil {
		testContext.Fatal(err)
	}
	assertRequestError(testContext, getResponse(testContext, url), http.StatusInternalServerError, "invalid_persistence")
	assertRequestError(testContext, mutateCrunchyroll(testContext, config, url, http.MethodPut, "text/csv", snapshot.Revision, crunchyrollTestCSV), http.StatusInternalServerError, "invalid_persistence")
}

func newCrunchyrollTestHandler(testContext *testing.T, config runtimeconfig.Config) *applicationHandler {
	testContext.Helper()
	handler, err := newApplicationHandler(config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		testContext.Fatal(err)
	}
	testContext.Cleanup(func() {
		if err := handler.Close(); err != nil {
			testContext.Error(err)
		}
	})
	return handler
}

func mutateCrunchyroll(testContext *testing.T, config runtimeconfig.Config, url, method, contentType, revision, body string) *http.Response {
	testContext.Helper()
	request, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		testContext.Fatal(err)
	}
	request.Header.Set("Origin", config.Authentication().PublicOrigin())
	request.Header.Set(csrfHeaderName, config.CSRFToken())
	request.Header.Set("Content-Type", contentType)
	if revision != "" {
		request.Header.Set("If-Match", strconv.Quote(revision))
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		testContext.Fatal(err)
	}
	return response
}

func readCrunchyrollSnapshot(testContext *testing.T, response *http.Response) crunchyrollSnapshotReply {
	testContext.Helper()
	if response.StatusCode != http.StatusOK {
		testContext.Fatalf("Crunchyroll provider: HTTP %d: %s", response.StatusCode, readBody(testContext, response))
	}
	var snapshot crunchyrollSnapshotReply
	decodeResponse(testContext, response, &snapshot)
	if response.Header.Get("ETag") != strconv.Quote(snapshot.Revision) {
		testContext.Fatal("ETag does not identify the returned revision")
	}
	return snapshot
}

func readCrunchyrollTestExport(testContext *testing.T, url string) []map[string]string {
	testContext.Helper()
	response := getResponse(testContext, url)
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		testContext.Fatalf("Crunchyroll export: HTTP %d", response.StatusCode)
	}
	defer response.Body.Close()
	rows, err := csv.NewReader(response.Body).ReadAll()
	if err != nil || len(rows) == 0 {
		testContext.Fatal("invalid Crunchyroll CSV export")
	}
	result := make([]map[string]string, 0, len(rows)-1)
	for _, values := range rows[1:] {
		row := make(map[string]string, len(rows[0]))
		for index, header := range rows[0] {
			row[header] = values[index]
		}
		result = append(result, row)
	}
	return result
}
