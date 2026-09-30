package httpapi

import (
	"bytes"
	"context"
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestMediaBrowserWorkspaceContract(testContext *testing.T) {
	if os.Getenv("DOWNLOAD_YOUR_DATA_RUN_BROWSER_CONTRACT") != "1" {
		testContext.Skip("enable browser contracts through make test-browser")
	}
	testContext.Run("dashboard", testMediaBrowserWorkspaceContract)
	for _, scenario := range []string{"drafts", "pagination", "upload-recovery", "charts", "analysis-start-failure", "selection-failure", "chart-inspection"} {
		testContext.Run(scenario, func(testContext *testing.T) { testMediaBrowserReviewRegression(testContext, scenario) })
	}
}

func testMediaBrowserWorkspaceContract(testContext *testing.T) {
	if os.Getenv("DOWNLOAD_YOUR_DATA_RUN_BROWSER_CONTRACT") != "1" {
		testContext.Skip("enable browser contracts through make test-browser")
	}
	server := httptest.NewUnstartedServer(nil)
	origin := "http://" + server.Listener.Addr().String()
	config := loadTestRuntimeConfig(testContext, map[string]string{"DOWNLOAD_YOUR_DATA_PUBLIC_ORIGIN": origin, "DOWNLOAD_YOUR_DATA_API_ORIGIN": origin, "DOWNLOAD_YOUR_DATA_TAUTH_URL": origin})
	var applicationLogs bytes.Buffer
	client := newHTTPMetadataClient()
	handler, err := newApplicationHandlerWithNetflixMetadata(config, slog.New(slog.NewTextHandler(&applicationLogs, nil)), client)
	if err != nil {
		testContext.Fatal(err)
	}
	defer handler.Close()
	server.Config.Handler = handler
	server.Start()
	defer server.Close()
	fixtureRoot := testContext.TempDir()
	primePath := filepath.Join(fixtureRoot, "prime-video.zip")
	netflixPath := filepath.Join(fixtureRoot, "netflix.csv")
	if err := os.WriteFile(primePath, syntheticPrimeArchive(testContext), 0o600); err != nil {
		testContext.Fatal(err)
	}
	if err := os.WriteFile(netflixPath, []byte("Title,Date\nSynthetic Film,2/1/26\n"), 0o600); err != nil {
		testContext.Fatal(err)
	}
	version := os.Getenv("PLAYWRIGHT_CLI_VERSION")
	if version == "" {
		version = "0.1.17"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "bash", "../../scripts/media-browser-workspace.sh")
	command.Env = append(os.Environ(), "DOWNLOAD_YOUR_DATA_BROWSER_BASE_URL="+origin, "DOWNLOAD_YOUR_DATA_BROWSER_CSV="+netflixPath, "DOWNLOAD_YOUR_DATA_BROWSER_PRIME="+primePath, "DOWNLOAD_YOUR_DATA_BROWSER_SESSION_COOKIE="+config.Authentication().SessionCookieName(), "DOWNLOAD_YOUR_DATA_BROWSER_SESSION_TOKEN="+testSessionCookie(testContext, config, "browser-media-user").Value, "PLAYWRIGHT_CLI_VERSION="+version)
	if output, err := command.CombinedOutput(); err != nil {
		testContext.Fatalf("shared media browser contract: %v\n%s\n%s", err, output, applicationLogs.String())
	}
	queries := client.searchCallSnapshot()
	if len(queries) != 1 || queries["Synthetic Film"] != 2 {
		testContext.Fatal("browser enrichment sent a query outside the derived title contract")
	}
}
