package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/inference"
)

func TestEmbeddingsEndpointDefaultsToLocalInference(testContext *testing.T) {
	expected := inference.DefaultBaseURL + "/embeddings"
	if received := embeddingsEndpoint(""); received != expected {
		testContext.Fatalf("expected %q, received %q", expected, received)
	}
}

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
		if payload.Model != "chatindex-nomic" || payload.Dimensions != 3 || len(payload.Input) != 1 || payload.Input[0] != "classification: local test" {
			testContext.Errorf("unexpected embedding request: %+v", payload)
		}
		responseWriter.Header().Set("Content-Type", "application/json")
		_, _ = responseWriter.Write([]byte(`{"data":[{"index":0,"embedding":[3,4,0]}]}`))
	}))
	defer server.Close()

	embedder := HTTPEmbedder{
		BaseURL:     server.URL + "/v1",
		Model:       "chatindex-nomic",
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
