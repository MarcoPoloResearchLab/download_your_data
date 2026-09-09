package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestSharedUIProviderMapHTTP(testContext *testing.T) {
	config := loadTestRuntimeConfig(testContext, nil)
	handler, err := NewHandler(config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		testContext.Fatal(err)
	}
	defer handler.Close()
	server := httptest.NewServer(handler)
	defer server.Close()
	response, err := http.Get(server.URL + uiConfigPath)
	if err != nil {
		testContext.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "no-store" {
		testContext.Fatalf("config response: %d %s", response.StatusCode, response.Header.Get("Cache-Control"))
	}
	var document struct {
		Environments []struct {
			Auth map[string]any `yaml:"auth"`
		} `yaml:"environments"`
	}
	if err := yaml.NewDecoder(response.Body).Decode(&document); err != nil {
		testContext.Fatal(err)
	}
	auth := config.Authentication()
	expected := map[string]any{
		"tauthUrl": auth.TAuthURL(), "tenantId": auth.TenantID(), "sessionPath": "/auth/session", "logoutPath": "/auth/logout",
		"providers": map[string]any{
			"google": map[string]any{"enabled": true, "clientId": "test.apps.googleusercontent.com", "loginPath": "/auth/google", "noncePath": "/auth/nonce"},
			"apple":  map[string]any{"enabled": false}, "password": map[string]any{"enabled": false},
		},
	}
	if len(document.Environments) != 1 || !reflect.DeepEqual(document.Environments[0].Auth, expected) {
		testContext.Fatalf("generated auth contract: %#v", document.Environments)
	}
}

func TestSharedUIAuthenticationBrowser(testContext *testing.T) {
	for _, separateOrigins := range []bool{false, true} {
		name := "same-origin"
		if separateOrigins {
			name = "separate-origins"
		}
		testContext.Run(name, func(testContext *testing.T) { runSharedUIAuthenticationBrowser(testContext, separateOrigins) })
	}
}

func runSharedUIAuthenticationBrowser(testContext *testing.T, separateOrigins bool) {
	if os.Getenv("DOWNLOAD_YOUR_DATA_RUN_BROWSER_CONTRACT") != "1" {
		testContext.Skip("run make test-shared-ui")
	}
	server := httptest.NewUnstartedServer(nil)
	origin := "http://" + server.Listener.Addr().String()
	publicOrigin := origin
	var frontendServer *httptest.Server
	if separateOrigins {
		frontendServer = httptest.NewUnstartedServer(nil)
		publicOrigin = "http://" + frontendServer.Listener.Addr().String()
	}
	config := loadTestRuntimeConfig(testContext, map[string]string{
		"DOWNLOAD_YOUR_DATA_PUBLIC_ORIGIN": publicOrigin, "DOWNLOAD_YOUR_DATA_API_ORIGIN": origin, "DOWNLOAD_YOUR_DATA_TAUTH_URL": origin,
	})
	application, err := newApplicationHandlerWithNetflixMetadata(config, slog.New(slog.NewTextHandler(io.Discard, nil)), newBrowserLifecycleMetadataClient())
	if err != nil {
		testContext.Fatal(err)
	}
	defer application.Close()
	server.Config.Handler = application
	server.Start()
	defer server.Close()
	if frontendServer != nil {
		frontendServer.Config.Handler = application
		frontendServer.Start()
		defer frontendServer.Close()
	}
	commandContext, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	command := exec.CommandContext(commandContext, "bash", filepath.Join("..", "..", "scripts", "shared-ui-auth.sh"))
	command.Env = append(os.Environ(),
		"DOWNLOAD_YOUR_DATA_BROWSER_BASE_URL="+publicOrigin,
		"DOWNLOAD_YOUR_DATA_BROWSER_API_URL="+server.URL,
		"DOWNLOAD_YOUR_DATA_BROWSER_SESSION_COOKIE="+config.Authentication().SessionCookieName(),
		"DOWNLOAD_YOUR_DATA_BROWSER_SESSION_TOKEN="+testSessionCookie(testContext, config, "browser-netflix-user").Value,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		testContext.Fatalf("shared browser contract: %v\n%s", err, output)
	}
	testContext.Log(string(output))
}
