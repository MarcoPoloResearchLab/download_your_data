package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/inference"
)

func TestHTTPEmbedderUsesCompatibleLocalRequestWithoutAPIKey(testContext *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/embeddings" {
			testContext.Errorf("unexpected request path %q", request.URL.Path)
		}
		if authorization := request.Header.Get("Authorization"); authorization != "" {
			testContext.Errorf("unexpected Authorization header %q", authorization)
		}
		var payload embeddingRequest
		if decodeError := json.NewDecoder(request.Body).Decode(&payload); decodeError != nil {
			testContext.Errorf("decode request: %v", decodeError)
		}
		if payload.Model != "download-your-data-embedding" || payload.Dimensions != 3 || len(payload.Input) != 1 || payload.Input[0] != "classification: local test" {
			testContext.Errorf("unexpected embedding request: %+v", payload)
		}
		responseWriter.Header().Set("Content-Type", "application/json")
		_, _ = responseWriter.Write([]byte(`{"data":[{"index":0,"embedding":[3,4,0]}]}`))
	}))
	defer server.Close()

	embedder := HTTPEmbedder{
		BaseURL:     mustInferenceBaseURL(testContext, server.URL+"/v1"),
		Model:       "download-your-data-embedding",
		Dimensions:  3,
		InputPrefix: "classification: ",
	}
	vectors, embedError := embedder.Embed(context.Background(), []string{"local test"})
	if embedError != nil {
		testContext.Fatalf("embed through local-compatible server: %v", embedError)
	}
	if len(vectors) != 1 || len(vectors[0]) != 3 || vectors[0][0] != 0.6 || vectors[0][1] != 0.8 {
		testContext.Fatalf("unexpected normalized vectors: %v", vectors)
	}
}

func mustInferenceBaseURL(testContext *testing.T, value string) inference.BaseURL {
	testContext.Helper()
	baseURL, baseURLError := inference.NewBaseURL(value)
	if baseURLError != nil {
		testContext.Fatalf("create inference base URL: %v", baseURLError)
	}
	return baseURL
}
