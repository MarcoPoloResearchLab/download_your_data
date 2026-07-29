package inference_test

import (
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/inference"
)

func TestBaseURLUsesTheCanonicalProductDefaults(testContext *testing.T) {
	if inference.BaseURLEnvironment != "DOWNLOAD_YOUR_DATA_INFERENCE_BASE_URL" {
		testContext.Fatalf("unexpected inference environment key %q", inference.BaseURLEnvironment)
	}
	if inference.DefaultEmbeddingModel != "download-your-data-embedding" {
		testContext.Fatalf("unexpected embedding model alias %q", inference.DefaultEmbeddingModel)
	}
	if inference.DefaultVerifierModel != "download-your-data-verifier" {
		testContext.Fatalf("unexpected verifier model alias %q", inference.DefaultVerifierModel)
	}

	baseURL, baseURLError := inference.NewBaseURL("")
	if baseURLError != nil {
		testContext.Fatalf("create default inference URL: %v", baseURLError)
	}
	if baseURL.String() != inference.DefaultBaseURL || !baseURL.IsLoopback() {
		testContext.Fatalf("unexpected default inference URL: %q loopback=%t", baseURL.String(), baseURL.IsLoopback())
	}
}

func TestBaseURLNormalizesSupportedHTTPBoundaries(testContext *testing.T) {
	testCases := []struct {
		configuredValue string
		expectedValue   string
		expectedLocal   bool
	}{
		{
			configuredValue: " HTTP://LOCALHOST:1234/v1/ ",
			expectedValue:   "http://localhost:1234/v1",
			expectedLocal:   true,
		},
		{
			configuredValue: "http://[0:0:0:0:0:0:0:1]:8000/v1",
			expectedValue:   "http://[::1]:8000/v1",
			expectedLocal:   true,
		},
		{
			configuredValue: "https://inference.example.com:8443/api/v1/",
			expectedValue:   "https://inference.example.com:8443/api/v1",
			expectedLocal:   false,
		},
		{
			configuredValue: "http://localhost:080/v1",
			expectedValue:   "http://localhost/v1",
			expectedLocal:   true,
		},
	}
	for _, testCase := range testCases {
		testContext.Run(testCase.configuredValue, func(testContext *testing.T) {
			baseURL, baseURLError := inference.NewBaseURL(testCase.configuredValue)
			if baseURLError != nil {
				testContext.Fatalf("create inference URL: %v", baseURLError)
			}
			if baseURL.String() != testCase.expectedValue || baseURL.IsLoopback() != testCase.expectedLocal {
				testContext.Fatalf(
					"inference URL = %q loopback=%t; want %q loopback=%t",
					baseURL.String(),
					baseURL.IsLoopback(),
					testCase.expectedValue,
					testCase.expectedLocal,
				)
			}
		})
	}
}

func TestBaseURLRejectsInvalidOrAmbiguousBoundaries(testContext *testing.T) {
	testCases := []struct {
		value        string
		expectedText string
	}{
		{value: "ftp://localhost/v1", expectedText: "scheme must be http or https"},
		{value: "http://user:secret@localhost/v1", expectedText: "credentials are not allowed"},
		{value: "http://localhost/v1?model=x", expectedText: "query strings are not allowed"},
		{value: "http://localhost/v1#fragment", expectedText: "fragments are not allowed"},
		{value: "http://localhost/v1/../v2", expectedText: "path must be normalized"},
		{value: "http://localhost/a b", expectedText: "whitespace is not allowed"},
		{value: "localhost:1234/v1", expectedText: "an absolute server URL is required"},
		{value: "http://localhost:70000/v1", expectedText: "port must be between 1 and 65535"},
	}
	for _, testCase := range testCases {
		testContext.Run(testCase.value, func(testContext *testing.T) {
			if _, baseURLError := inference.NewBaseURL(testCase.value); baseURLError == nil ||
				!strings.Contains(baseURLError.Error(), testCase.expectedText) {
				testContext.Fatalf("inference URL error = %v; want text %q", baseURLError, testCase.expectedText)
			} else if strings.Contains(baseURLError.Error(), "user:secret") {
				testContext.Fatalf("inference URL error exposed credentials: %v", baseURLError)
			}
		})
	}
}

func TestBaseURLBuildsOnlySingleSegmentEndpoints(testContext *testing.T) {
	baseURL, baseURLError := inference.NewBaseURL("http://127.0.0.1:1234/v1")
	if baseURLError != nil {
		testContext.Fatalf("create inference URL: %v", baseURLError)
	}
	endpoint, endpointError := baseURL.Endpoint("embeddings")
	if endpointError != nil {
		testContext.Fatalf("build embeddings endpoint: %v", endpointError)
	}
	if endpoint != "http://127.0.0.1:1234/v1/embeddings" {
		testContext.Fatalf("unexpected embeddings endpoint %q", endpoint)
	}
	chatEndpoint, chatEndpointError := baseURL.Endpoint("chat/completions")
	if chatEndpointError != nil {
		testContext.Fatalf("build chat completions endpoint: %v", chatEndpointError)
	}
	if chatEndpoint != "http://127.0.0.1:1234/v1/chat/completions" {
		testContext.Fatalf("unexpected chat completions endpoint %q", chatEndpoint)
	}
	for _, invalidEndpoint := range []string{
		"",
		".",
		"..",
		"../embeddings",
		"chat//completions",
		"chat/completions/",
		"chat%2fcompletions",
		"chat?operation=completions",
	} {
		if _, endpointError := baseURL.Endpoint(invalidEndpoint); endpointError == nil {
			testContext.Errorf("endpoint %q should be rejected", invalidEndpoint)
		}
	}
}
