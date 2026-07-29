package tmdb

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
)

func TestReadTokenAndCreditsContract(testContext *testing.T) {
	if ReadTokenEnvironment != "DOWNLOAD_YOUR_DATA_TMDB_READ_TOKEN" {
		testContext.Fatalf("unexpected token environment %q", ReadTokenEnvironment)
	}
	if _, configured, tokenError := OptionalReadToken(""); tokenError != nil || configured {
		testContext.Fatalf("absent token = configured %t, error %v", configured, tokenError)
	}
	secret := "private-read-token"
	token, configured, tokenError := OptionalReadToken(secret)
	if tokenError != nil || !configured || !token.valid() {
		testContext.Fatalf("configure token: configured=%t error=%v", configured, tokenError)
	}
	if _, tokenError := NewReadToken(" private-read-token "); tokenError == nil ||
		strings.Contains(tokenError.Error(), secret) {
		testContext.Fatalf("invalid token error exposed or accepted secret: %v", tokenError)
	}

	attribution := CreditsAttribution()
	if attribution.Name != "TMDB" ||
		attribution.Website != "https://www.themoviedb.org" ||
		attribution.Notice != "This product uses the TMDB API but is not endorsed or certified by TMDB." ||
		!attribution.CreditsPlacement ||
		!attribution.ApprovedLogoRequired ||
		attribution.LogoModification {
		testContext.Fatalf("unexpected Credits attribution: %+v", attribution)
	}
}

func TestFakeTMDBSearchAndDetailsUseOnlyBearerAuthenticatedTitleQueries(testContext *testing.T) {
	const tokenValue = "test-read-token"
	var receivedPaths []string
	var pathMutex sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		pathMutex.Lock()
		receivedPaths = append(receivedPaths, request.URL.Path)
		pathMutex.Unlock()

		if request.Header.Get("Authorization") != "Bearer "+tokenValue {
			testContext.Errorf("unexpected Authorization header")
		}
		if request.Header.Get("Accept") != "application/json" {
			testContext.Errorf("missing JSON accept header")
		}
		if request.URL.Query().Has("api_key") ||
			strings.Contains(request.URL.RawQuery, tokenValue) {
			testContext.Errorf("credential leaked into URL %q", request.URL.RawQuery)
		}
		requestBody, readError := io.ReadAll(request.Body)
		if readError != nil {
			testContext.Errorf("read request body: %v", readError)
		}
		if len(requestBody) != 0 {
			testContext.Errorf("GET request sent a payload: %q", requestBody)
		}

		responseWriter.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/3/search/multi":
			assertSearchQuery(testContext, request.URL.Query())
			_, _ = responseWriter.Write([]byte(`{
			  "results": [
			    {
			      "id": 603,
			      "media_type": "movie",
			      "title": "The Matrix",
			      "original_title": "The Matrix",
			      "popularity": 80.1
			    },
			    {
			      "id": 1,
			      "media_type": "person",
			      "name": "Private Person",
			      "popularity": 1000
			    }
			  ]
			}`))
		case "/3/movie/603":
			if request.URL.Query().Get("language") != "en-US" || len(request.URL.Query()) != 1 {
				testContext.Errorf("unexpected details query %q", request.URL.RawQuery)
			}
			_, _ = responseWriter.Write([]byte(`{
			  "id": 603,
			  "title": "The Matrix",
			  "overview": "A synthetic details response.",
			  "genres": [{"name": "Science Fiction"}, {"name": "Action"}],
			  "release_date": "1999-03-30",
			  "runtime": 136,
			  "original_language": "en",
			  "vote_average": 8.2,
			  "vote_count": 26000,
			  "production_countries": [{"iso_3166_1": "US"}]
			}`))
		default:
			testContext.Errorf("unexpected TMDB path %q", request.URL.Path)
			responseWriter.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := newFakeServerClient(testContext, tokenValue, server.URL+"/3", nil)
	locale := mustLocale(testContext, DefaultLocale)
	candidates, searchError := client.Search(context.Background(), "The Matrix", locale)
	if searchError != nil {
		testContext.Fatalf("search fake TMDB: %v", searchError)
	}
	if len(candidates) != 1 ||
		candidates[0].TMDBID != 603 ||
		candidates[0].MediaType != netflix.MediaTypeMovie ||
		candidates[0].Title != "The Matrix" {
		testContext.Fatalf("unexpected candidates: %+v", candidates)
	}
	details, detailsError := client.Details(context.Background(), candidates[0], locale)
	if detailsError != nil {
		testContext.Fatalf("load fake TMDB details: %v", detailsError)
	}
	if details.TMDBID != 603 ||
		details.MatchedTitle != "The Matrix" ||
		details.ReleaseDate != "1999-03-30" ||
		len(details.Genres) != 2 ||
		details.RuntimeMinutes == nil ||
		*details.RuntimeMinutes != 136 {
		testContext.Fatalf("unexpected details: %+v", details)
	}
	pathMutex.Lock()
	defer pathMutex.Unlock()
	if strings.Join(receivedPaths, ",") != "/3/search/multi,/3/movie/603" {
		testContext.Fatalf("unexpected request sequence %v", receivedPaths)
	}
}

func TestFakeTMDBHonorsRetryAfterWithinTheBoundedRetryBudget(testContext *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, _ *http.Request) {
		if requestCount.Add(1) == 1 {
			responseWriter.Header().Set("Retry-After", "2")
			responseWriter.WriteHeader(http.StatusTooManyRequests)
			_, _ = responseWriter.Write([]byte(`{"status_message":"rate limited"}`))
			return
		}
		responseWriter.Header().Set("Content-Type", "application/json")
		_, _ = responseWriter.Write([]byte(`{"results":[]}`))
	}))
	defer server.Close()

	var delays []time.Duration
	var delayMutex sync.Mutex
	recordingSleep := func(_ context.Context, delay time.Duration) error {
		delayMutex.Lock()
		delays = append(delays, delay)
		delayMutex.Unlock()
		return nil
	}
	client := newFakeServerClient(testContext, "test-token", server.URL+"/3", recordingSleep)
	if _, searchError := client.Search(
		context.Background(),
		"Synthetic Title",
		mustLocale(testContext, DefaultLocale),
	); searchError != nil {
		testContext.Fatalf("retry fake TMDB search: %v", searchError)
	}
	if requestCount.Load() != 2 {
		testContext.Fatalf("request count = %d; want 2", requestCount.Load())
	}
	delayMutex.Lock()
	defer delayMutex.Unlock()
	if len(delays) != 1 || delays[0] != 2*time.Second {
		testContext.Fatalf("retry delays = %v; want [2s]", delays)
	}
}

func TestFakeTMDBReturnsTypedFailuresForMalformedOversizedOutageAndCancellation(testContext *testing.T) {
	testCases := []struct {
		name         string
		handler      http.Handler
		expectedCode ErrorCode
	}{
		{
			name: "malformed response",
			handler: http.HandlerFunc(func(responseWriter http.ResponseWriter, _ *http.Request) {
				_, _ = responseWriter.Write([]byte(`{"results":`))
			}),
			expectedCode: ErrorInvalidResponse,
		},
		{
			name: "oversized response",
			handler: http.HandlerFunc(func(responseWriter http.ResponseWriter, _ *http.Request) {
				_, _ = responseWriter.Write([]byte(strings.Repeat("x", int(product.MaxTMDBResponseBytes)+1)))
			}),
			expectedCode: ErrorInvalidResponse,
		},
		{
			name: "remote outage",
			handler: http.HandlerFunc(func(responseWriter http.ResponseWriter, _ *http.Request) {
				responseWriter.WriteHeader(http.StatusServiceUnavailable)
				_, _ = responseWriter.Write([]byte(`{"status_message":"unavailable"}`))
			}),
			expectedCode: ErrorUnavailable,
		},
	}
	for _, testCase := range testCases {
		testContext.Run(testCase.name, func(testContext *testing.T) {
			server := httptest.NewServer(testCase.handler)
			defer server.Close()
			client := newFakeServerClient(testContext, "test-token", server.URL+"/3", nil)
			_, searchError := client.Search(
				context.Background(),
				"Synthetic Title",
				mustLocale(testContext, DefaultLocale),
			)
			assertTMDBErrorCode(testContext, searchError, testCase.expectedCode)
		})
	}

	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requestCount.Add(1)
	}))
	defer server.Close()
	client := newFakeServerClient(testContext, "test-token", server.URL+"/3", nil)
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	_, searchError := client.Search(
		canceledContext,
		"Synthetic Title",
		mustLocale(testContext, DefaultLocale),
	)
	assertTMDBErrorCode(testContext, searchError, ErrorCanceled)
	if requestCount.Load() != 0 {
		testContext.Fatalf("canceled request reached fake TMDB")
	}
}

func TestClientRejectsRedirectsAndMalformedAcceptedDetails(
	testContext *testing.T,
) {
	testContext.Run("redirect", func(testContext *testing.T) {
		var redirectedRequests atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(
			responseWriter http.ResponseWriter,
			request *http.Request,
		) {
			if request.URL.Path == "/redirected" {
				redirectedRequests.Add(1)
				_, _ = responseWriter.Write([]byte(`{"results":[]}`))
				return
			}
			http.Redirect(responseWriter, request, "/redirected", http.StatusFound)
		}))
		defer server.Close()
		client := newFakeServerClient(testContext, "test-token", server.URL+"/3", nil)
		_, searchError := client.Search(
			context.Background(),
			"Synthetic Title",
			mustLocale(testContext, DefaultLocale),
		)
		assertTMDBErrorCode(testContext, searchError, ErrorInvalidResponse)
		if redirectedRequests.Load() != 0 {
			testContext.Fatalf("TMDB client followed a redirect")
		}
	})

	testContext.Run("invalid movie runtime", func(testContext *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(
			responseWriter http.ResponseWriter,
			_ *http.Request,
		) {
			responseWriter.Header().Set("Content-Type", "application/json")
			_, _ = responseWriter.Write([]byte(`{
			  "id": 603,
			  "title": "The Matrix",
			  "runtime": 0,
			  "vote_average": 8.2,
			  "vote_count": 100
			}`))
		}))
		defer server.Close()
		client := newFakeServerClient(testContext, "test-token", server.URL+"/3", nil)
		_, detailsError := client.Details(
			context.Background(),
			Candidate{
				TMDBID:        603,
				MediaType:     netflix.MediaTypeMovie,
				Title:         "The Matrix",
				OriginalTitle: "The Matrix",
				Popularity:    1,
			},
			mustLocale(testContext, DefaultLocale),
		)
		assertTMDBErrorCode(testContext, detailsError, ErrorInvalidResponse)
	})
}

func TestClientOwnsTheOfficialOriginPacingAndRetryBounds(testContext *testing.T) {
	token, tokenError := NewReadToken("test-token")
	if tokenError != nil {
		testContext.Fatalf("create test token: %v", tokenError)
	}
	productionClient, clientError := NewClient(token)
	if clientError != nil {
		testContext.Fatalf("create production client: %v", clientError)
	}
	if productionClient.baseURL != OfficialBaseURL {
		testContext.Fatalf("production base URL = %q; want %q", productionClient.baseURL, OfficialBaseURL)
	}
	if _, injectedError := newClient(token, clientOptions{
		httpClient:      &http.Client{},
		baseURL:         "https://example.com/3",
		requestInterval: 0,
		sleep:           sleepWithContext,
		now:             time.Now,
		maxAttempts:     1,
	}); injectedError == nil {
		testContext.Fatalf("non-loopback injected TMDB origin should be rejected")
	}

	currentTime := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	var pacingDelays []time.Duration
	pacer := newRequestPacer(
		time.Second/product.TMDBRequestsPerSecond,
		func() time.Time { return currentTime },
		func(_ context.Context, delay time.Duration) error {
			pacingDelays = append(pacingDelays, delay)
			return nil
		},
	)
	for requestIndex := 0; requestIndex < 3; requestIndex++ {
		if waitError := pacer.Wait(context.Background()); waitError != nil {
			testContext.Fatalf("pace request %d: %v", requestIndex+1, waitError)
		}
	}
	expectedPacingDelays := []time.Duration{250 * time.Millisecond, 500 * time.Millisecond}
	if len(pacingDelays) != len(expectedPacingDelays) {
		testContext.Fatalf("pacing delays = %v; want %v", pacingDelays, expectedPacingDelays)
	}
	for delayIndex := range pacingDelays {
		if pacingDelays[delayIndex] != expectedPacingDelays[delayIndex] {
			testContext.Fatalf("pacing delays = %v; want %v", pacingDelays, expectedPacingDelays)
		}
	}

	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(
		responseWriter http.ResponseWriter,
		_ *http.Request,
	) {
		requestCount.Add(1)
		responseWriter.Header().Set("Retry-After", "9999")
		responseWriter.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	var retryDelays []time.Duration
	client := newFakeServerClient(
		testContext,
		"test-token",
		server.URL+"/3",
		func(_ context.Context, delay time.Duration) error {
			retryDelays = append(retryDelays, delay)
			return nil
		},
	)
	_, searchError := client.Search(
		context.Background(),
		"Synthetic Title",
		mustLocale(testContext, DefaultLocale),
	)
	assertTMDBErrorCode(testContext, searchError, ErrorRateLimited)
	if requestCount.Load() != product.MaxTMDBAttempts {
		testContext.Fatalf(
			"TMDB attempts = %d; want %d",
			requestCount.Load(),
			product.MaxTMDBAttempts,
		)
	}
	if len(retryDelays) != product.MaxTMDBAttempts-1 {
		testContext.Fatalf("retry delays = %v", retryDelays)
	}
	for _, delay := range retryDelays {
		if delay != time.Duration(product.MaxTMDBRetryAfterSeconds)*time.Second {
			testContext.Fatalf("uncapped Retry-After delay %v", delay)
		}
	}
}

func newFakeServerClient(
	testContext *testing.T,
	tokenValue string,
	baseURL string,
	sleep sleepFunction,
) *Client {
	testContext.Helper()
	token, tokenError := NewReadToken(tokenValue)
	if tokenError != nil {
		testContext.Fatalf("create test read token: %v", tokenError)
	}
	if sleep == nil {
		sleep = func(context.Context, time.Duration) error { return nil }
	}
	client, clientError := newClient(token, clientOptions{
		httpClient:      &http.Client{Timeout: 5 * time.Second},
		baseURL:         baseURL,
		requestInterval: 0,
		sleep:           sleep,
		now: func() time.Time {
			return time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
		},
		maxAttempts: product.MaxTMDBAttempts,
	})
	if clientError != nil {
		testContext.Fatalf("create fake-server client: %v", clientError)
	}
	return client
}

func mustLocale(testContext *testing.T, value string) Locale {
	testContext.Helper()
	locale, localeError := NewLocale(value)
	if localeError != nil {
		testContext.Fatalf("create locale: %v", localeError)
	}
	return locale
}

func assertSearchQuery(testContext *testing.T, values url.Values) {
	testContext.Helper()
	if values.Get("query") != "The Matrix" ||
		values.Get("language") != "en-US" ||
		values.Get("include_adult") != "false" ||
		values.Get("page") != "1" ||
		len(values) != 4 {
		testContext.Fatalf("unexpected search query %q", values.Encode())
	}
	for forbiddenKey := range map[string]struct{}{
		"api_key":   {},
		"date":      {},
		"profile":   {},
		"title_raw": {},
	} {
		if values.Has(forbiddenKey) {
			testContext.Fatalf("search query contains forbidden key %q", forbiddenKey)
		}
	}
}

func assertTMDBErrorCode(
	testContext *testing.T,
	receivedError error,
	expectedCode ErrorCode,
) {
	testContext.Helper()
	var clientError *Error
	if !errors.As(receivedError, &clientError) {
		testContext.Fatalf("error %v is not a typed TMDB error", receivedError)
	}
	if clientError.Code() != expectedCode {
		testContext.Fatalf("error code = %q; want %q", clientError.Code(), expectedCode)
	}
	if strings.Contains(clientError.Error(), "Synthetic Title") ||
		strings.Contains(clientError.Error(), "test-token") {
		testContext.Fatalf("typed error exposed private input: %v", clientError)
	}
}
