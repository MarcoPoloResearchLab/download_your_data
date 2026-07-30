package httpapi

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
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/frontend"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/authentication"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/runtimeconfig"
	"github.com/golang-jwt/jwt/v5"
	"github.com/tyemirov/tauth/pkg/sessionvalidator"
)

const defaultTestUserID = "user-default"

func TestApplicationHTTPContract(testContext *testing.T) {
	config := testRuntimeConfig(testContext)
	handler, handlerError := newApplicationHandler(
		config,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if handlerError != nil {
		testContext.Fatalf("create application handler: %v", handlerError)
	}
	defer handler.Close()
	server := newAuthenticatedTestServer(testContext, config, handler, defaultTestUserID)
	defer server.Close()

	testContext.Run("reports readiness", func(testContext *testing.T) {
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
		if payload.Status != healthStatusReady {
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
		if !payload.DataRoot.Ready ||
			payload.CSRFToken != config.CSRFToken() ||
			payload.Inference.Boundary != runtimeconfig.InferenceBoundaryLoopback ||
			payload.Inference.Readiness != inferenceNotChecked ||
			payload.Inference.EmbeddingModel == "" ||
			payload.Archive.MaxUploadBytes <= 0 ||
			payload.Archive.InferenceBatchSize <= 0 ||
			payload.Archive.BrowserUploadEnabled ||
			!payload.Providers.OpenAI.SemanticSearch ||
			payload.Providers.OpenAI.BrowserUpload ||
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
		if !strings.Contains(
			string(body),
			`data-api-origin="`+config.Authentication().APIOrigin()+`"`,
		) ||
			!strings.Contains(
				string(body),
				`<link rel="canonical" href="`+
					config.Authentication().PublicOrigin()+`/">`,
			) ||
			strings.Contains(string(body), frontend.APIOriginMarker) ||
			strings.Contains(string(body), frontend.PublicOriginMarker) {
			testContext.Fatalf(
				"application shell does not contain the configured API origin",
			)
		}
		if response.Header.Get("Content-Security-Policy") != buildContentSecurityPolicy(config) {
			testContext.Fatalf("application response is missing the canonical content security policy")
		}
	})

	testContext.Run("serves only browser-safe shared auth configuration", func(testContext *testing.T) {
		response, requestError := http.Get(server.URL + uiConfigPath)
		if requestError != nil {
			testContext.Fatalf("request browser configuration: %v", requestError)
		}
		defer response.Body.Close()
		encodedConfig, readError := io.ReadAll(response.Body)
		if readError != nil {
			testContext.Fatalf("read browser configuration: %v", readError)
		}
		configText := string(encodedConfig)
		if response.StatusCode != http.StatusOK ||
			!strings.Contains(configText, config.Authentication().PublicOrigin()) ||
			!strings.Contains(configText, config.Authentication().TAuthURL()) ||
			!strings.Contains(configText, config.Authentication().TenantID()) ||
			!strings.Contains(configText, runtimeconfig.TAuthSessionPath) {
			testContext.Fatalf("unexpected browser configuration: %s", configText)
		}
		if strings.Contains(
			configText,
			string(config.Authentication().SessionValidatorConfig().SigningKey),
		) ||
			strings.Contains(configText, config.Authentication().SessionCookieName()) ||
			strings.Contains(configText, config.Authentication().RefreshCookieName()) {
			testContext.Fatalf("browser configuration exposed backend session material")
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
	defer handler.Close()
	request := httptest.NewRequest(
		http.MethodGet,
		"http://127.0.0.1:8787"+capabilitiesPath,
		nil,
	)
	request.AddCookie(testSessionCookie(testContext, config, defaultTestUserID))
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

func TestRequestBoundaryEnforcesExactCORSOriginAndCSRF(testContext *testing.T) {
	config := testRuntimeConfig(testContext)
	handler, handlerError := newApplicationHandler(
		config,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if handlerError != nil {
		testContext.Fatalf("create application handler: %v", handlerError)
	}
	defer handler.Close()

	testCases := []struct {
		name         string
		method       string
		origin       string
		csrfToken    string
		expectedCode string
	}{
		{
			name:         "cross-origin read",
			method:       http.MethodGet,
			origin:       "https://attacker.example",
			expectedCode: "invalid_origin",
		},
		{
			name:         "mutation without origin",
			method:       http.MethodPost,
			expectedCode: "invalid_origin",
		},
		{
			name:         "mutation without CSRF token",
			method:       http.MethodPost,
			origin:       config.Authentication().PublicOrigin(),
			expectedCode: "invalid_csrf_token",
		},
		{
			name:         "mutation with wrong CSRF token",
			method:       http.MethodDelete,
			origin:       config.Authentication().PublicOrigin(),
			csrfToken:    "wrong-token",
			expectedCode: "invalid_csrf_token",
		},
	}
	for _, testCase := range testCases {
		testContext.Run(testCase.name, func(testContext *testing.T) {
			response := performBoundaryRequest(
				handler,
				testCase.method,
				testCase.origin,
				testCase.csrfToken,
				"",
				"",
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
			if testCase.origin != config.Authentication().PublicOrigin() &&
				response.Header().Get("Access-Control-Allow-Origin") != "" {
				testContext.Fatalf("rejected request must not enable CORS")
			}
		})
	}

	acceptedResponse := performBoundaryRequest(
		handler,
		http.MethodPost,
		config.Authentication().PublicOrigin(),
		config.CSRFToken(),
		"",
		"",
	)
	if acceptedResponse.Code != http.StatusUnauthorized {
		testContext.Fatalf(
			"valid edge request without a session status = %d; want %d: %s",
			acceptedResponse.Code,
			http.StatusUnauthorized,
			acceptedResponse.Body.String(),
		)
	}
	preflightResponse := performBoundaryRequest(
		handler,
		http.MethodOptions,
		config.Authentication().PublicOrigin(),
		"",
		http.MethodPost,
		"content-type, x-csrf-token",
	)
	if preflightResponse.Code != http.StatusNoContent ||
		preflightResponse.Header().Get("Access-Control-Allow-Origin") !=
			config.Authentication().PublicOrigin() ||
		preflightResponse.Header().Get("Access-Control-Allow-Credentials") != "true" {
		testContext.Fatalf(
			"valid credentialed preflight failed: status=%d headers=%v body=%s",
			preflightResponse.Code,
			preflightResponse.Header(),
			preflightResponse.Body.String(),
		)
	}
}

func TestContentSecurityPolicyIsolatesTheSharedShell(testContext *testing.T) {
	config := testRuntimeConfig(testContext)
	securityPolicy := buildContentSecurityPolicy(config)
	if !strings.Contains(securityPolicy, "default-src 'self'") ||
		!strings.Contains(securityPolicy, config.Authentication().APIOrigin()) ||
		!strings.Contains(securityPolicy, config.Authentication().TAuthURL()) ||
		!strings.Contains(
			securityPolicy,
			"style-src 'self' https://cdn.jsdelivr.net https://accounts.google.com 'unsafe-inline'",
		) ||
		!strings.Contains(securityPolicy, "frame-ancestors 'none'") ||
		strings.Contains(securityPolicy, "unsafe-eval") {
		testContext.Fatalf(
			"content security policy does not isolate the shared shell: %s",
			securityPolicy,
		)
	}
}

func performBoundaryRequest(
	handler http.Handler,
	method string,
	origin string,
	csrfToken string,
	preflightMethod string,
	preflightHeaders string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://127.0.0.1:8787/api/providers/netflix", nil)
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if csrfToken != "" {
		request.Header.Set(csrfHeaderName, csrfToken)
	}
	if preflightMethod != "" {
		request.Header.Set("Access-Control-Request-Method", preflightMethod)
	}
	if preflightHeaders != "" {
		request.Header.Set("Access-Control-Request-Headers", preflightHeaders)
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
	environment := testRuntimeEnvironment(dataDirectory)
	for key, value := range additionalEnvironment {
		environment[key] = value
	}
	config, configError := runtimeconfig.Load(
		func(key string) string { return environment[key] },
		bytes.NewReader(bytes.Repeat([]byte{0x5a}, 32)),
	)
	if configError != nil {
		testContext.Fatalf("load test runtime config: %v", configError)
	}
	return config
}

func testRuntimeEnvironment(dataDirectory string) map[string]string {
	return map[string]string{
		runtimeconfig.DataDirectoryEnvironment:      dataDirectory,
		runtimeconfig.PublicOriginEnvironment:       "http://127.0.0.1:4173",
		runtimeconfig.APIOriginEnvironment:          "http://127.0.0.1:8787",
		runtimeconfig.TAuthURLEnvironment:           "http://127.0.0.1:8787",
		runtimeconfig.TAuthTenantIDEnvironment:      "download-your-data-test",
		runtimeconfig.TAuthJWTSigningKeyEnvironment: strings.Repeat("test-signing-key-", 2),
		runtimeconfig.TAuthSessionCookieEnvironment: "app_session_dyd_test",
		runtimeconfig.TAuthRefreshCookieEnvironment: "app_refresh_dyd_test",
		runtimeconfig.GoogleClientIDEnvironment:     "test.apps.googleusercontent.com",
	}
}

func newAuthenticatedTestServer(
	testContext *testing.T,
	config runtimeconfig.Config,
	handler http.Handler,
	userID string,
) *httptest.Server {
	testContext.Helper()
	sessionCookie := testSessionCookie(testContext, config, userID)
	server := httptest.NewServer(http.HandlerFunc(
		func(responseWriter http.ResponseWriter, request *http.Request) {
			request.AddCookie(sessionCookie)
			handler.ServeHTTP(responseWriter, request)
		},
	))
	testContext.Cleanup(server.Close)
	return server
}

func testSessionCookie(
	testContext *testing.T,
	config runtimeconfig.Config,
	userID string,
) *http.Cookie {
	testContext.Helper()
	now := time.Now().UTC()
	validatorConfig := config.Authentication().SessionValidatorConfig()
	claims := sessionvalidator.Claims{
		TenantID: config.Authentication().TenantID(),
		UserID:   userID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    validatorConfig.Issuer,
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now.Add(-time.Minute)),
			NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedToken, signingError := token.SignedString(validatorConfig.SigningKey)
	if signingError != nil {
		testContext.Fatalf("mint test TAuth session: %v", signingError)
	}
	return &http.Cookie{
		Name:  config.Authentication().SessionCookieName(),
		Value: signedToken,
	}
}

func testAuthenticatedUser(
	testContext *testing.T,
	config runtimeconfig.Config,
	userID string,
) authentication.AuthenticatedUser {
	testContext.Helper()
	user, userError := authentication.NewAuthenticatedUser(
		config.Authentication().TenantID(),
		userID,
	)
	if userError != nil {
		testContext.Fatalf("create test authenticated user: %v", userError)
	}
	return user
}

func testUserWorkspace(
	testContext *testing.T,
	config runtimeconfig.Config,
	userID string,
) runtimeconfig.UserWorkspace {
	testContext.Helper()
	workspace, workspaceError := config.UserWorkspace(
		testAuthenticatedUser(testContext, config, userID),
	)
	if workspaceError != nil {
		testContext.Fatalf("resolve test user workspace: %v", workspaceError)
	}
	return workspace
}
