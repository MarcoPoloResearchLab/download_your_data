package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestToolsIndexCanonicalHTTPContract(testContext *testing.T) {
	config := testRuntimeConfig(testContext)
	handler, handlerError := newApplicationHandler(config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if handlerError != nil {
		testContext.Fatalf("create application handler: %v", handlerError)
	}
	defer handler.Close()
	server := httptest.NewServer(handler)
	defer server.Close()
	for _, path := range []string{"/tools/", "/tools/index.html"} {
		request, requestError := http.NewRequest(http.MethodGet, server.URL+path, nil)
		if requestError != nil {
			testContext.Fatal(requestError)
		}
		request.Host = "api-copy.example"
		response, requestError := http.DefaultClient.Do(request)
		if requestError != nil {
			testContext.Fatalf("request %s: %v", path, requestError)
		}
		body, readError := io.ReadAll(response.Body)
		response.Body.Close()
		if readError != nil {
			testContext.Fatalf("read %s: %v", path, readError)
		}
		canonical := `<link rel="canonical" href="` + config.Authentication().PublicOrigin() + `/tools/">`
		if response.StatusCode != http.StatusOK || strings.Count(string(body), canonical) != 1 ||
			strings.Contains(string(body), "__DOWNLOAD_YOUR_DATA_PUBLIC_ORIGIN__") {
			testContext.Fatalf("%s status=%d lacks one bound public Tools canonical %s", path, response.StatusCode, canonical)
		}
	}
}
