package httpapi

import (
	"context"
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

	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
)

type mediaBrowserRegressionClient struct {
	*httpMetadataClient
	release chan struct{}
	once    sync.Once
}

func (client *mediaBrowserRegressionClient) Search(ctx context.Context, query string, locale tmdb.Locale) ([]tmdb.Candidate, error) {
	if query == "Synthetic Film" {
		select {
		case <-client.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return client.httpMetadataClient.Search(ctx, query, locale)
	}
	if strings.HasPrefix(query, "Synthetic Title ") {
		return []tmdb.Candidate{}, nil
	}
	return nil, fmt.Errorf("unexpected regression title query")
}

func testMediaBrowserReviewRegression(testContext *testing.T, scenario string) {
	server := httptest.NewUnstartedServer(nil)
	origin := "http://" + server.Listener.Addr().String()
	config := loadTestRuntimeConfig(testContext, map[string]string{"DOWNLOAD_YOUR_DATA_PUBLIC_ORIGIN": origin, "DOWNLOAD_YOUR_DATA_API_ORIGIN": origin, "DOWNLOAD_YOUR_DATA_TAUTH_URL": origin})
	client := &mediaBrowserRegressionClient{httpMetadataClient: newHTTPMetadataClient(), release: make(chan struct{})}
	handler, err := newApplicationHandlerWithNetflixMetadata(config, slog.New(slog.NewTextHandler(io.Discard, nil)), client)
	if err != nil {
		testContext.Fatal(err)
	}
	defer handler.Close()
	server.Config.Handler = http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/fixture/release-enrichment" {
			client.once.Do(func() { close(client.release) })
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		handler.ServeHTTP(writer, request)
	})
	server.Start()
	defer server.Close()
	rows := []map[string]string{}
	for index := range 120 {
		title := fmt.Sprintf("Synthetic Title %03d", index)
		if index == 0 {
			title = "Synthetic Film"
		}
		rows = append(rows, map[string]string{"Title": title, "Playback Start Datetime (UTC)": "2026-02-02T01:00:00Z", "Seconds Viewed": "60", "Material Type Description": "Feature"})
	}
	fixtureRoot := testContext.TempDir()
	primePath := filepath.Join(fixtureRoot, "prime.zip")
	netflixPath := filepath.Join(fixtureRoot, "netflix.csv")
	for path, contents := range map[string][]byte{primePath: syntheticPrimeArchiveWithRows(testContext, rows), netflixPath: []byte("Title,Date\nSynthetic Film,2/1/26\n")} {
		file, err := os.Create(path)
		if err != nil {
			testContext.Fatal(err)
		}
		_, writeErr := file.Write(contents)
		closeErr := file.Close()
		if writeErr != nil {
			testContext.Fatal(writeErr)
		}
		if closeErr != nil {
			testContext.Fatal(closeErr)
		}
	}
	version := os.Getenv("PLAYWRIGHT_CLI_VERSION")
	if version == "" {
		version = "0.1.17"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	sessionToken := testSessionCookie(testContext, config, "browser-media-user").Value
	command := exec.CommandContext(ctx, "bash", "../../scripts/media-browser-workspace.sh", "../../scripts/media-browser-regressions.playwright.js")
	command.Env = append(os.Environ(), "DOWNLOAD_YOUR_DATA_BROWSER_BASE_URL="+origin, "DOWNLOAD_YOUR_DATA_BROWSER_CSV="+netflixPath, "DOWNLOAD_YOUR_DATA_BROWSER_PRIME="+primePath, "DOWNLOAD_YOUR_DATA_BROWSER_SESSION_COOKIE="+config.Authentication().SessionCookieName(), "DOWNLOAD_YOUR_DATA_BROWSER_SESSION_TOKEN="+sessionToken, "DOWNLOAD_YOUR_DATA_BROWSER_REGRESSION="+scenario, "PLAYWRIGHT_CLI_VERSION="+version)
	if output, err := command.CombinedOutput(); err != nil {
		testContext.Fatalf("%s regression: %v\n%s", scenario, err, strings.ReplaceAll(string(output), sessionToken, "fixture-session"))
	}
}
