package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"testing"
)

func TestPrimeHTTPRejectsUnknownQueriesBeforeDomainWork(testContext *testing.T) {
	config := testRuntimeConfig(testContext)
	handler, err := newApplicationHandler(config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		testContext.Fatal(err)
	}
	defer handler.Close()
	server := newAuthenticatedTestServer(testContext, config, handler, defaultTestUserID)
	defer server.Close()
	created := mutateNetflix(testContext, config, server.URL+testPrimeProviderPath+"/generations", http.MethodPost, "application/json", `{"analysis_level":"local"}`)
	var operation primeGenerationResponse
	decodeResponse(testContext, created, &operation)
	path := testPrimeProviderPath + "/generations/" + operation.Generation.ID
	for _, scenario := range []struct{ method, path string }{
		{http.MethodGet, path}, {http.MethodPost, testPrimeProviderPath + "/generations"},
		{http.MethodPut, path + "/archive"}, {http.MethodPut, path + "/selection"},
		{http.MethodDelete, path}, {http.MethodDelete, testPrimeProviderPath},
	} {
		testContext.Run(scenario.method+scenario.path, func(testContext *testing.T) {
			response := mutateNetflix(testContext, config, server.URL+scenario.path+"?unknown=value", scenario.method, "application/json", "")
			assertRequestError(testContext, response, http.StatusBadRequest, "invalid_query")
		})
	}
}
