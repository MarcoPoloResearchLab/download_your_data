package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestValidateInferenceBoundaryRequiresExplicitRemoteAuthorization(testContext *testing.T) {
	if boundaryError := validateInferenceBoundary("embedding", "http://127.0.0.1:1234/v1", false); boundaryError != nil {
		testContext.Fatalf("loopback endpoint should be allowed: %v", boundaryError)
	}
	boundaryError := validateInferenceBoundary("embedding", "https://example.com/v1", false)
	if boundaryError == nil || !strings.Contains(boundaryError.Error(), "--allow-remote") {
		testContext.Fatalf("remote endpoint should require explicit authorization: %v", boundaryError)
	}
	if boundaryError := validateInferenceBoundary("embedding", "https://example.com/v1", true); boundaryError != nil {
		testContext.Fatalf("explicitly authorized remote endpoint should be allowed: %v", boundaryError)
	}
}

func TestReadOptionalAPIKeyRequiresNamedEnvironmentValue(testContext *testing.T) {
	if apiKey, keyError := readOptionalAPIKey(""); keyError != nil || apiKey != "" {
		testContext.Fatalf("empty environment name should disable authentication: key=%q error=%v", apiKey, keyError)
	}
	testContext.Setenv("CHATINDEX_TEST_API_KEY", "secret")
	apiKey, keyError := readOptionalAPIKey("CHATINDEX_TEST_API_KEY")
	if keyError != nil || apiKey != "secret" {
		testContext.Fatalf("read named API key: key=%q error=%v", apiKey, keyError)
	}
}

func TestScenarioCLIIndexesVisibleConversationTextAndProducesLexicalSearchReport(testContext *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/embeddings" {
			testContext.Errorf("unexpected embedding endpoint %s", request.URL.Path)
		}
		var payload struct {
			Input []string `json:"input"`
		}
		if decodeError := json.NewDecoder(request.Body).Decode(&payload); decodeError != nil {
			testContext.Errorf("decode embedding payload: %v", decodeError)
			responseWriter.WriteHeader(http.StatusBadRequest)
			return
		}
		data := make([]map[string]any, len(payload.Input))
		for inputIndex := range payload.Input {
			data[inputIndex] = map[string]any{
				"index":     inputIndex,
				"embedding": []float64{1, 0, 0},
			}
		}
		responseWriter.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(responseWriter).Encode(map[string]any{"data": data})
	}))
	defer server.Close()

	workingDirectory := testContext.TempDir()
	databasePath := filepath.Join(workingDirectory, "chatindex.db")
	sourcePath := filepath.Join("..", "..", "testdata", "synthetic-openai-export.zip")
	if importError := run([]string{"import", "--db", databasePath, sourcePath}); importError != nil {
		testContext.Fatalf("import CLI fixture: %v", importError)
	}
	if indexError := run([]string{
		"index", "build",
		"--db", databasePath,
		"--provider", "fixture",
		"--model", "fixture-model",
		"--dimensions", "3",
		"--base-url", server.URL + "/v1",
		"--batch-size", "2",
	}); indexError != nil {
		testContext.Fatalf("build CLI retrieval index: %v", indexError)
	}

	outputPath := filepath.Join(workingDirectory, "berth-search.json")
	if searchError := run([]string{
		"search",
		"--db", databasePath,
		"--query", "berth",
		"--mode", "lexical",
		"--limit", strconv.Itoa(20),
		"--output", outputPath,
	}); searchError != nil {
		testContext.Fatalf("search CLI retrieval index: %v", searchError)
	}
	encodedResults, readError := os.ReadFile(outputPath)
	if readError != nil {
		testContext.Fatalf("read CLI search report: %v", readError)
	}
	if !strings.Contains(string(encodedResults), "berth") {
		testContext.Fatalf("CLI search report lacks literal evidence: %s", encodedResults)
	}
	if _, statError := os.Stat(strings.TrimSuffix(outputPath, filepath.Ext(outputPath)) + "-audit.json"); statError != nil {
		testContext.Fatalf("CLI search audit was not written: %v", statError)
	}
}

func TestScenarioCLIReportsExactModelLoadCommandWhenLMStudioHasNoModel(testContext *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, _ *http.Request) {
		responseWriter.Header().Set("Content-Type", "application/json")
		responseWriter.WriteHeader(http.StatusBadRequest)
		_, _ = responseWriter.Write([]byte(`{"error":"No models loaded."}`))
	}))
	defer server.Close()

	workingDirectory := testContext.TempDir()
	databasePath := filepath.Join(workingDirectory, "chatindex.db")
	sourcePath := filepath.Join("..", "..", "testdata", "synthetic-openai-export.zip")
	if importError := run([]string{"import", "--db", databasePath, sourcePath}); importError != nil {
		testContext.Fatalf("import CLI fixture: %v", importError)
	}
	indexError := run([]string{
		"index", "build",
		"--db", databasePath,
		"--base-url", server.URL + "/v1",
		"--dimensions", "3",
	})
	if indexError == nil || !strings.Contains(indexError.Error(), "lms load text-embedding-nomic-embed-text-v1.5") {
		testContext.Fatalf("missing actionable model load instruction: %v", indexError)
	}
}
