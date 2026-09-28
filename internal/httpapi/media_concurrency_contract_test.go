package httpapi

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMediaHTTPConcurrentFirstProviderReads(testContext *testing.T) {
	config := testRuntimeConfig(testContext)
	handler, err := newApplicationHandlerWithNetflixMetadata(config, slog.New(slog.NewTextHandler(io.Discard, nil)), newHTTPMetadataClient())
	if err != nil {
		testContext.Fatal(err)
	}
	defer handler.Close()
	server := httptest.NewServer(handler)
	defer server.Close()
	for userIndex := range 24 {
		cookie := testSessionCookie(testContext, config, fmt.Sprintf("first-media-user-%d", userIndex))
		start := make(chan struct{})
		type result struct {
			path   string
			status int
			err    error
		}
		results := make(chan result, 3)
		for _, path := range []string{testPrimeProviderPath, "/api/providers/netflix", "/api/viewing-history"} {
			request, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
			if err != nil {
				testContext.Fatal(err)
			}
			request.AddCookie(cookie)
			go func() {
				<-start
				response, err := http.DefaultClient.Do(request)
				if err != nil {
					results <- result{path: path, err: err}
					return
				}
				_, drainErr := io.Copy(io.Discard, response.Body)
				closeErr := response.Body.Close()
				if drainErr == nil {
					drainErr = closeErr
				}
				results <- result{path: path, status: response.StatusCode, err: drainErr}
			}()
		}
		close(start)
		for range 3 {
			result := <-results
			if result.err != nil || result.status != http.StatusOK {
				testContext.Errorf("first concurrent provider read %s: status=%d error=%v", result.path, result.status, result.err)
			}
		}
	}
}
