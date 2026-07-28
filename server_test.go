package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/runtimeconfig"
)

func TestApplicationHTTPContract(testContext *testing.T) {
	config := testRuntimeConfig(testContext)
	handler, handlerError := newApplicationHandler(
		config,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if handlerError != nil {
		testContext.Fatalf("create application handler: %v", handlerError)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	testContext.Run("reports local readiness", func(testContext *testing.T) {
		response, requestError := http.Get(server.URL + healthPath)
		if requestError != nil {
			testContext.Fatalf("request health endpoint: %v", requestError)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			testContext.Fatalf("health status = %d; want %d", response.StatusCode, http.StatusOK)
		}
		var payload healthResponse
		if decodeError := json.NewDecoder(response.Body).Decode(&payload); decodeError != nil {
			testContext.Fatalf("decode health response: %v", decodeError)
		}
		if payload.Status != healthStatusReady || !payload.LocalOnly {
			testContext.Fatalf("unexpected health response: %+v", payload)
		}
		if response.Header.Get("Cache-Control") != "no-store" {
			testContext.Fatalf("health response must not be cached")
		}
	})

	testContext.Run("reports non-secret runtime capabilities", func(testContext *testing.T) {
		request, requestError := http.NewRequest(
			http.MethodGet,
			server.URL+capabilitiesPath+"?inference_base_url=https://attacker.example/v1",
			nil,
		)
		if requestError != nil {
			testContext.Fatalf("create capabilities request: %v", requestError)
		}
		request.Header.Set("X-Inference-Base-URL", "https://attacker.example/v1")
		response, requestError := http.DefaultClient.Do(request)
		if requestError != nil {
			testContext.Fatalf("request capabilities endpoint: %v", requestError)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			testContext.Fatalf("capabilities status = %d; want %d", response.StatusCode, http.StatusOK)
		}
		encodedPayload, readError := io.ReadAll(response.Body)
		if readError != nil {
			testContext.Fatalf("read capabilities response: %v", readError)
		}
		var payload capabilitiesResponse
		if decodeError := json.Unmarshal(encodedPayload, &payload); decodeError != nil {
			testContext.Fatalf("decode capabilities response: %v", decodeError)
		}
		if !payload.LocalOnly ||
			!payload.DataRoot.Ready ||
			payload.CSRFToken != config.CSRFToken() ||
			payload.Inference.Boundary != runtimeconfig.InferenceBoundaryLoopback ||
			payload.Inference.Readiness != inferenceNotChecked ||
			payload.Inference.EmbeddingModel == "" ||
			payload.Archive.MaxUploadBytes <= 0 ||
			payload.Archive.InferenceBatchSize <= 0 ||
			payload.Archive.BrowserUploadEnabled ||
			payload.Providers.Netflix.TMDB.Configured {
			testContext.Fatalf("unexpected capabilities response: %+v", payload)
		}
		if response.Header.Get("Access-Control-Allow-Origin") != "" {
			testContext.Fatalf("capabilities response must not enable CORS")
		}
		if strings.Contains(string(encodedPayload), "attacker.example") {
			testContext.Fatalf("HTTP input changed the configured inference boundary: %s", encodedPayload)
		}
	})

	testContext.Run("serves the application shell", func(testContext *testing.T) {
		response, requestError := http.Get(server.URL + "/")
		if requestError != nil {
			testContext.Fatalf("request application shell: %v", requestError)
		}
		defer response.Body.Close()
		body, readError := io.ReadAll(response.Body)
		if readError != nil {
			testContext.Fatalf("read application shell: %v", readError)
		}
		if response.StatusCode != http.StatusOK {
			testContext.Fatalf("application status = %d; want %d", response.StatusCode, http.StatusOK)
		}
		if !strings.Contains(string(body), "Download Your Data") {
			testContext.Fatalf("application shell is missing the product title")
		}
		if response.Header.Get("Content-Security-Policy") != contentSecurityPolicy {
			testContext.Fatalf("application response is missing the canonical content security policy")
		}
	})

	testContext.Run("rejects unknown routes", func(testContext *testing.T) {
		response, requestError := http.Get(server.URL + "/not-a-current-route")
		if requestError != nil {
			testContext.Fatalf("request unknown route: %v", requestError)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			testContext.Fatalf("unknown route status = %d; want %d", response.StatusCode, http.StatusNotFound)
		}
	})
}

func TestCapabilitiesExposeOnlyTMDBConfiguredState(testContext *testing.T) {
	const readToken = "private-test-read-token"
	config := loadTestRuntimeConfig(testContext, map[string]string{
		tmdb.ReadTokenEnvironment: readToken,
	})
	handler, handlerError := newApplicationHandler(
		config,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if handlerError != nil {
		testContext.Fatalf("create application handler: %v", handlerError)
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"http://127.0.0.1:8787"+capabilitiesPath,
		nil,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		testContext.Fatalf("capabilities status = %d; want %d", response.Code, http.StatusOK)
	}
	var payload capabilitiesResponse
	if decodeError := json.Unmarshal(response.Body.Bytes(), &payload); decodeError != nil {
		testContext.Fatalf("decode capabilities: %v", decodeError)
	}
	if !payload.Providers.Netflix.TMDB.Configured {
		testContext.Fatalf("configured TMDB capability was not reported")
	}
	if strings.Contains(response.Body.String(), readToken) {
		testContext.Fatalf("capabilities exposed the server-only TMDB token")
	}
}

func TestLocalRequestBoundaryRejectsInvalidHostOriginAndCSRF(testContext *testing.T) {
	config := testRuntimeConfig(testContext)
	handler, handlerError := newApplicationHandler(
		config,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if handlerError != nil {
		testContext.Fatalf("create application handler: %v", handlerError)
	}

	testCases := []struct {
		name         string
		method       string
		host         string
		origin       string
		csrfToken    string
		expectedCode string
	}{
		{
			name:         "non-loopback host",
			method:       http.MethodGet,
			host:         "attacker.example",
			expectedCode: "invalid_host",
		},
		{
			name:         "ambiguous empty host port",
			method:       http.MethodGet,
			host:         "localhost:",
			expectedCode: "invalid_host",
		},
		{
			name:         "cross-origin read",
			method:       http.MethodGet,
			host:         "127.0.0.1:8787",
			origin:       "https://attacker.example",
			expectedCode: "invalid_origin",
		},
		{
			name:         "mutation without origin",
			method:       http.MethodPost,
			host:         "127.0.0.1:8787",
			expectedCode: "invalid_origin",
		},
		{
			name:         "mutation without CSRF token",
			method:       http.MethodPost,
			host:         "127.0.0.1:8787",
			origin:       "http://127.0.0.1:8787",
			expectedCode: "invalid_csrf_token",
		},
		{
			name:         "mutation with wrong CSRF token",
			method:       http.MethodDelete,
			host:         "localhost:8787",
			origin:       "http://localhost:8787",
			csrfToken:    "wrong-token",
			expectedCode: "invalid_csrf_token",
		},
	}
	for _, testCase := range testCases {
		testContext.Run(testCase.name, func(testContext *testing.T) {
			response := performBoundaryRequest(
				handler,
				testCase.method,
				testCase.host,
				testCase.origin,
				testCase.csrfToken,
			)
			if response.Code != http.StatusForbidden {
				testContext.Fatalf("status = %d; want %d", response.Code, http.StatusForbidden)
			}
			var payload requestErrorResponse
			if decodeError := json.Unmarshal(response.Body.Bytes(), &payload); decodeError != nil {
				testContext.Fatalf("decode request error: %v", decodeError)
			}
			if payload.Error.Code != testCase.expectedCode {
				testContext.Fatalf("error code = %q; want %q", payload.Error.Code, testCase.expectedCode)
			}
			if response.Header().Get("Access-Control-Allow-Origin") != "" {
				testContext.Fatalf("rejected request must not enable CORS")
			}
		})
	}

	acceptedResponse := performBoundaryRequest(
		handler,
		http.MethodPost,
		"127.0.0.1:8787",
		"http://127.0.0.1:8787",
		config.CSRFToken(),
	)
	if acceptedResponse.Code == http.StatusForbidden {
		testContext.Fatalf("valid same-origin mutation was rejected: %s", acceptedResponse.Body.String())
	}
	defaultPortResponse := performBoundaryRequest(
		handler,
		http.MethodPost,
		"localhost:080",
		"http://localhost",
		config.CSRFToken(),
	)
	if defaultPortResponse.Code == http.StatusForbidden {
		testContext.Fatalf(
			"equivalent default-port origin was rejected: %s",
			defaultPortResponse.Body.String(),
		)
	}
}

func performBoundaryRequest(
	handler http.Handler,
	method string,
	host string,
	origin string,
	csrfToken string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://127.0.0.1:8787/api/providers/netflix", nil)
	request.Host = host
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if csrfToken != "" {
		request.Header.Set(csrfHeaderName, csrfToken)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func testRuntimeConfig(testContext *testing.T) runtimeconfig.Config {
	testContext.Helper()
	return loadTestRuntimeConfig(testContext, nil)
}

func loadTestRuntimeConfig(
	testContext *testing.T,
	additionalEnvironment map[string]string,
) runtimeconfig.Config {
	testContext.Helper()
	dataDirectory := filepath.Join(testContext.TempDir(), "data")
	environment := map[string]string{
		runtimeconfig.DataDirectoryEnvironment: dataDirectory,
	}
	for key, value := range additionalEnvironment {
		environment[key] = value
	}
	config, configError := runtimeconfig.Load(
		func(key string) string { return environment[key] },
		testContext.TempDir(),
		bytes.NewReader(bytes.Repeat([]byte{0x5a}, 32)),
	)
	if configError != nil {
		testContext.Fatalf("load test runtime config: %v", configError)
	}
	return config
}
