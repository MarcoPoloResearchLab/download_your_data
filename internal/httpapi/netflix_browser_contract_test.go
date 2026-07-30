package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
)

func TestNetflixBrowserWorkspaceContract(testContext *testing.T) {
	if os.Getenv("DOWNLOAD_YOUR_DATA_RUN_BROWSER_CONTRACT") != "1" {
		testContext.Skip("set DOWNLOAD_YOUR_DATA_RUN_BROWSER_CONTRACT=1 through make test-browser")
	}
	testContext.Parallel()

	server := httptest.NewUnstartedServer(nil)
	serverOrigin := "http://" + server.Listener.Addr().String()
	config := loadTestRuntimeConfig(testContext, map[string]string{
		"DOWNLOAD_YOUR_DATA_PUBLIC_ORIGIN": serverOrigin,
		"DOWNLOAD_YOUR_DATA_API_ORIGIN":    serverOrigin,
		"DOWNLOAD_YOUR_DATA_TAUTH_URL":     serverOrigin,
	})
	var logOutput bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logOutput, nil))
	metadataClient := newBrowserLifecycleMetadataClient()
	application, handlerError := newApplicationHandlerWithNetflixMetadata(
		config,
		logger,
		metadataClient,
	)
	if handlerError != nil {
		testContext.Fatalf("create browser contract handler: %v", handlerError)
	}
	defer application.Close()

	tracker := &browserConsentTracker{
		next:   application,
		client: metadataClient,
	}
	server.Config.Handler = tracker
	server.Start()
	defer server.Close()

	fixturePath := filepath.Join(testContext.TempDir(), "viewing-activity.csv")
	if writeError := os.WriteFile(
		fixturePath,
		[]byte(httpSyntheticViewingCSV),
		0o600,
	); writeError != nil {
		testContext.Fatalf("write browser viewing-activity fixture: %v", writeError)
	}

	playwrightVersion := os.Getenv("PLAYWRIGHT_CLI_VERSION")
	if playwrightVersion == "" {
		playwrightVersion = "0.1.17"
	}
	commandContext, cancelCommand := context.WithTimeout(
		context.Background(),
		90*time.Second,
	)
	defer cancelCommand()
	command := exec.CommandContext(
		commandContext,
		"bash",
		filepath.Join("..", "..", "scripts", "netflix-browser-workspace.sh"),
	)
	command.Env = append(
		os.Environ(),
		"DOWNLOAD_YOUR_DATA_BROWSER_BASE_URL="+server.URL,
		"DOWNLOAD_YOUR_DATA_BROWSER_CSV="+fixturePath,
		"DOWNLOAD_YOUR_DATA_BROWSER_SESSION_COOKIE="+
			config.Authentication().SessionCookieName(),
		"DOWNLOAD_YOUR_DATA_BROWSER_SESSION_TOKEN="+
			testSessionCookie(
				testContext,
				config,
				"browser-netflix-user",
			).Value,
		"PLAYWRIGHT_CLI_VERSION="+playwrightVersion,
	)
	output, commandError := command.CombinedOutput()
	if commandError != nil {
		if errors.Is(commandContext.Err(), context.DeadlineExceeded) {
			testContext.Fatalf(
				"Netflix browser contract timed out:\n%s",
				string(output),
			)
		}
		testContext.Fatalf(
			"Netflix browser contract failed: %v\n%s",
			commandError,
			string(output),
		)
	}

	creationCount, authorizedCount := tracker.snapshot()
	if creationCount != 4 || authorizedCount != 4 {
		testContext.Fatalf(
			"TMDB browser creation attempts = %d authorized=%d; want 4 and 4",
			creationCount,
			authorizedCount,
		)
	}
	preConsentCalls, calls := metadataClient.snapshot()
	if preConsentCalls != 0 {
		testContext.Fatalf(
			"metadata client calls before explicit browser consent = %d",
			preConsentCalls,
		)
	}
	for _, expected := range []string{
		"3|en-US|Synthetic Film",
		"3|en-US|Synthetic Series",
		"3|en-US|Another Film",
		"4|es-ES|Synthetic Film",
		"4|es-ES|Synthetic Series",
		"4|es-ES|Another Film",
	} {
		if calls[expected] != 1 {
			testContext.Fatalf(
				"successful metadata call %q count = %d; calls=%#v",
				expected,
				calls[expected],
				calls,
			)
		}
	}
	for _, attempt := range []int{1, 2} {
		callCount := 0
		for key, count := range calls {
			if strings.HasPrefix(key, fmt.Sprintf("%d|", attempt)) {
				callCount += count
			}
		}
		if callCount == 0 {
			testContext.Fatalf(
				"browser enrichment attempt %d never reached the metadata client",
				attempt,
			)
		}
	}
	for privateValue, label := range map[string]string{
		"Synthetic Film":   "title",
		"Synthetic Series": "title",
		"Another Film":     "title",
		"1/1/26":           "viewing date",
	} {
		if strings.Contains(logOutput.String(), privateValue) {
			testContext.Fatalf("browser server log exposed private %s", label)
		}
	}
}

type browserConsentTracker struct {
	mutex               sync.Mutex
	next                http.Handler
	client              *browserLifecycleMetadataClient
	tmdbCreations       int
	authorizedCreations int
}

func (tracker *browserConsentTracker) ServeHTTP(
	responseWriter http.ResponseWriter,
	request *http.Request,
) {
	if request.Method == http.MethodPost &&
		request.URL.Path == netflixGenerationsPath {
		body, readError := io.ReadAll(io.LimitReader(request.Body, 4097))
		request.Body = io.NopCloser(bytes.NewReader(body))
		if readError == nil && len(body) <= 4096 {
			var payload struct {
				AnalysisLevel string `json:"analysis_level"`
				Consent       string `json:"tmdb_title_query_consent"`
			}
			if json.Unmarshal(body, &payload) == nil &&
				payload.AnalysisLevel == "tmdb" {
				tracker.mutex.Lock()
				tracker.tmdbCreations++
				if payload.Consent == netflixTMDBQueryConsent {
					tracker.authorizedCreations++
					tracker.client.setAttempt(tracker.authorizedCreations)
				}
				tracker.mutex.Unlock()
			}
		}
	}
	tracker.next.ServeHTTP(responseWriter, request)
}

func (tracker *browserConsentTracker) snapshot() (int, int) {
	tracker.mutex.Lock()
	defer tracker.mutex.Unlock()
	return tracker.tmdbCreations, tracker.authorizedCreations
}

type browserLifecycleMetadataClient struct {
	mutex           sync.Mutex
	attempt         int
	preConsentCalls int
	calls           map[string]int
}

func newBrowserLifecycleMetadataClient() *browserLifecycleMetadataClient {
	return &browserLifecycleMetadataClient{calls: make(map[string]int)}
}

func (client *browserLifecycleMetadataClient) Identity() string {
	return tmdb.ClientIdentity
}

func (client *browserLifecycleMetadataClient) setAttempt(attempt int) {
	client.mutex.Lock()
	client.attempt = attempt
	client.mutex.Unlock()
}

func (client *browserLifecycleMetadataClient) Search(
	ctx context.Context,
	query string,
	locale tmdb.Locale,
) ([]tmdb.Candidate, error) {
	client.mutex.Lock()
	attempt := client.attempt
	if attempt == 0 {
		client.preConsentCalls++
	}
	client.calls[fmt.Sprintf("%d|%s|%s", attempt, locale.String(), query)]++
	client.mutex.Unlock()

	switch attempt {
	case 0:
		return nil, errors.New("metadata query reached the client without explicit consent")
	case 1:
		<-ctx.Done()
		return nil, ctx.Err()
	case 2:
		return nil, browserTMDBFailure{code: tmdb.ErrorRateLimited}
	case 3:
		if locale.String() != "en-US" {
			return nil, errors.New("ready-enriched fixture requires en-US")
		}
	case 4:
		if locale.String() != "es-ES" {
			return nil, errors.New("review fixture requires es-ES")
		}
	default:
		return nil, errors.New("unexpected browser enrichment attempt")
	}

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
		candidates := []tmdb.Candidate{{
			TMDBID:        2002,
			MediaType:     netflix.MediaTypeSeries,
			Title:         "Synthetic Series",
			OriginalTitle: "Synthetic Series",
			Popularity:    10,
		}}
		if attempt == 4 {
			candidates = append(candidates, tmdb.Candidate{
				TMDBID:        2003,
				MediaType:     netflix.MediaTypeSeries,
				Title:         "Synthetic Series",
				OriginalTitle: "Synthetic Series",
				Popularity:    5,
			})
		}
		return candidates, nil
	case "Another Film":
		return []tmdb.Candidate{}, nil
	default:
		return nil, errors.New("unexpected browser title query")
	}
}

func (client *browserLifecycleMetadataClient) Details(
	_ context.Context,
	candidate tmdb.Candidate,
	locale tmdb.Locale,
) (tmdb.Details, error) {
	if locale.String() != "en-US" && locale.String() != "es-ES" {
		return tmdb.Details{}, errors.New("unexpected browser details locale")
	}
	voteAverage := 7.8
	voteCount := 900
	switch candidate.TMDBID {
	case 2001:
		runtimeMinutes := 98
		return tmdb.Details{
			TMDBID:           candidate.TMDBID,
			MediaType:        candidate.MediaType,
			Genres:           []string{"Documentary"},
			ReleaseDate:      "2025-01-02",
			RuntimeMinutes:   &runtimeMinutes,
			OriginalLanguage: "en",
			VoteAverage:      &voteAverage,
			VoteCount:        &voteCount,
			OriginCountries:  []string{"US"},
			MatchedTitle:     candidate.Title,
			Description:      "Synthetic browser metadata.",
		}, nil
	case 2002:
		runtimeMinutes := 45
		seasons := 1
		episodes := 8
		return tmdb.Details{
			TMDBID:           candidate.TMDBID,
			MediaType:        candidate.MediaType,
			Genres:           []string{"Drama"},
			ReleaseDate:      "2024-10-01",
			RuntimeMinutes:   &runtimeMinutes,
			OriginalLanguage: "en",
			VoteAverage:      &voteAverage,
			VoteCount:        &voteCount,
			OriginCountries:  []string{"US"},
			Seasons:          &seasons,
			Episodes:         &episodes,
			MatchedTitle:     candidate.Title,
			Description:      "Synthetic browser series metadata.",
		}, nil
	default:
		return tmdb.Details{}, errors.New("unexpected browser details candidate")
	}
}

func (client *browserLifecycleMetadataClient) snapshot() (int, map[string]int) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	calls := make(map[string]int, len(client.calls))
	for key, count := range client.calls {
		calls[key] = count
	}
	return client.preConsentCalls, calls
}

type browserTMDBFailure struct {
	code tmdb.ErrorCode
}

func (failure browserTMDBFailure) Error() string {
	return "synthetic browser TMDB failure"
}

func (failure browserTMDBFailure) Code() tmdb.ErrorCode {
	return failure.code
}
