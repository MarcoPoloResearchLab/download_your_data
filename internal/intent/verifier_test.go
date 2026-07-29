package intent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/inference"
)

type countingVerifier struct {
	Calls int
}

func (verifier *countingVerifier) Identity() string {
	return "counting-verifier-v1"
}

func (verifier *countingVerifier) Verify(_ context.Context, inputs []VerificationInput) (map[string]VerificationResult, error) {
	verifier.Calls++
	results := make(map[string]VerificationResult, len(inputs))
	for _, input := range inputs {
		results[input.MessageID] = VerificationResult{
			MessageID:           input.MessageID,
			IsDefinitionRequest: true,
			Terms:               []string{"berth"},
			Category:            "word",
			Confidence:          0.99,
			Explanation:         "Direct definition request.",
		}
	}
	return results, nil
}

func TestVerifierDefaultsToLocalInference(testContext *testing.T) {
	expectedEndpoint := inference.DefaultBaseURL + "/chat/completions"
	received, endpointError := mustIntentBaseURL(testContext, "").Endpoint("chat/completions")
	if endpointError != nil {
		testContext.Fatalf("build verifier endpoint: %v", endpointError)
	}
	if received != expectedEndpoint {
		testContext.Fatalf("expected %q, received %q", expectedEndpoint, received)
	}
	if received := verifierModel(""); received != inference.DefaultVerifierModel {
		testContext.Fatalf("expected %q, received %q", inference.DefaultVerifierModel, received)
	}
}

func TestHTTPVerifierUsesStructuredLocalRequestWithoutAPIKey(testContext *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			testContext.Errorf("unexpected request path %q", request.URL.Path)
		}
		if authorization := request.Header.Get("Authorization"); authorization != "" {
			testContext.Errorf("unexpected Authorization header %q", authorization)
		}
		var payload map[string]any
		if decodeError := json.NewDecoder(request.Body).Decode(&payload); decodeError != nil {
			testContext.Errorf("decode request: %v", decodeError)
		}
		if payload["model"] != "download-your-data-verifier" {
			testContext.Errorf("unexpected verifier model %v", payload["model"])
		}
		responseFormat, exists := payload["response_format"].(map[string]any)
		if !exists || responseFormat["type"] != "json_schema" {
			testContext.Errorf("missing structured response format: %v", payload["response_format"])
		}

		content := `{"results":[{"id":"message-1","is_definition_request":true,"terms":["berth"],"category":"word","confidence":0.99,"explanation":"Direct definition request."}]}`
		encodedContent, _ := json.Marshal(content)
		responseWriter.Header().Set("Content-Type", "application/json")
		_, _ = responseWriter.Write([]byte(`{"choices":[{"message":{"content":` + string(encodedContent) + `}}]}`))
	}))
	defer server.Close()

	verifier := HTTPVerifier{
		BaseURL: mustIntentBaseURL(testContext, server.URL+"/v1"),
		Model:   "download-your-data-verifier",
	}
	results, verifyError := verifier.Verify(context.Background(), []VerificationInput{{
		MessageID:   "message-1",
		UserMessage: "define berth",
	}})
	if verifyError != nil {
		testContext.Fatalf("verify through local-compatible server: %v", verifyError)
	}
	if !results["message-1"].IsDefinitionRequest {
		testContext.Fatalf("unexpected verification results: %+v", results)
	}
}

func TestCachedVerifierResumesFromStoredResults(testContext *testing.T) {
	openedStore := openIntentTestStore(testContext)
	defer openedStore.Close()

	inner := &countingVerifier{}
	inputs := []VerificationInput{
		{MessageID: "message-1", UserMessage: "define berth"},
		{MessageID: "message-2", UserMessage: "what does wily mean"},
	}
	first := &CachedVerifier{Store: openedStore, Inner: inner, BatchSize: 1}
	if _, verifyError := first.Verify(context.Background(), inputs); verifyError != nil {
		testContext.Fatalf("populate verification cache: %v", verifyError)
	}
	if inner.Calls != 2 || first.CacheMisses != 2 {
		testContext.Fatalf("unexpected initial cache behavior: calls=%d misses=%d", inner.Calls, first.CacheMisses)
	}

	second := &CachedVerifier{Store: openedStore, Inner: inner, BatchSize: 1}
	results, verifyError := second.Verify(context.Background(), inputs)
	if verifyError != nil {
		testContext.Fatalf("reuse verification cache: %v", verifyError)
	}
	if inner.Calls != 2 || second.CacheHits != 2 || len(results) != 2 {
		testContext.Fatalf(
			"expected cache-only resume, received calls=%d hits=%d results=%d",
			inner.Calls,
			second.CacheHits,
			len(results),
		)
	}
}

func TestHTTPVerifierSplitsFailedBatch(testContext *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		var payload struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if decodeError := json.NewDecoder(request.Body).Decode(&payload); decodeError != nil {
			testContext.Errorf("decode request: %v", decodeError)
			responseWriter.WriteHeader(http.StatusBadRequest)
			return
		}
		var inputs []VerificationInput
		if decodeError := json.Unmarshal([]byte(payload.Messages[1].Content), &inputs); decodeError != nil {
			testContext.Errorf("decode inputs: %v", decodeError)
			responseWriter.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(inputs) > 1 {
			responseWriter.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		content, _ := json.Marshal(verificationEnvelope{Results: []VerificationResult{{
			MessageID:           inputs[0].MessageID,
			IsDefinitionRequest: true,
			Terms:               []string{"term"},
			Category:            "word",
			Confidence:          0.9,
			Explanation:         "Definition request.",
		}}})
		encodedContent, _ := json.Marshal(string(content))
		responseWriter.Header().Set("Content-Type", "application/json")
		_, _ = responseWriter.Write([]byte(`{"choices":[{"message":{"content":` + string(encodedContent) + `}}]}`))
	}))
	defer server.Close()

	verifier := HTTPVerifier{
		BaseURL:    mustIntentBaseURL(testContext, server.URL+"/v1"),
		Model:      "download-your-data-verifier",
		BatchSize:  2,
		MaxRetries: 0,
	}
	results, verifyError := verifier.Verify(context.Background(), []VerificationInput{
		{MessageID: "message-1", UserMessage: "define berth"},
		{MessageID: "message-2", UserMessage: "define wily"},
	})
	if verifyError != nil {
		testContext.Fatalf("verify split batch: %v", verifyError)
	}
	if len(results) != 2 || requests.Load() != 3 {
		testContext.Fatalf("expected one failed batch and two successful splits: results=%d requests=%d", len(results), requests.Load())
	}
}
