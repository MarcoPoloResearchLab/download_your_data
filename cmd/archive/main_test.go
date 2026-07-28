package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/inference"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/runtimeconfig"
)

func TestReadOptionalAPIKeyRequiresNamedEnvironmentValue(testContext *testing.T) {
	if apiKey, keyError := readOptionalAPIKey(""); keyError != nil || apiKey != "" {
		testContext.Fatalf("empty environment name should disable authentication: key=%q error=%v", apiKey, keyError)
	}
	testContext.Setenv("DOWNLOAD_YOUR_DATA_TEST_API_KEY", "secret")
	apiKey, keyError := readOptionalAPIKey("DOWNLOAD_YOUR_DATA_TEST_API_KEY")
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

	config := testArchiveRuntimeConfig(testContext, server.URL+"/v1")
	sourcePath := filepath.Join("..", "..", "testdata", "synthetic-openai-export.zip")
	if importError := run([]string{"import", sourcePath}, config); importError != nil {
		testContext.Fatalf("import CLI fixture: %v", importError)
	}
	if indexError := run([]string{
		"index", "build",
		"--provider", "fixture",
		"--model", "fixture-model",
		"--dimensions", "3",
		"--batch-size", "2",
	}, config); indexError != nil {
		testContext.Fatalf("build CLI retrieval index: %v", indexError)
	}

	outputRelativePath := filepath.Join("reports", "berth-search.json")
	outputPath := filepath.Join(config.DataRoot().Path(), outputRelativePath)
	if searchError := run([]string{
		"search",
		"--query", "berth",
		"--mode", "lexical",
		"--limit", strconv.Itoa(20),
		"--output", outputRelativePath,
	}, config); searchError != nil {
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

func TestSearchRejectsAnOutputPathOutsideThePrivateDataRoot(testContext *testing.T) {
	config := testArchiveRuntimeConfig(testContext, inference.DefaultBaseURL)
	searchError := run([]string{
		"search",
		"--query", "berth",
		"--mode", "lexical",
		"--output", filepath.Join(testContext.TempDir(), "escaped.json"),
	}, config)
	if searchError == nil || !strings.Contains(searchError.Error(), "absolute paths are not allowed") {
		testContext.Fatalf("unexpected escaped report error: %v", searchError)
	}
}

func TestScenarioCLIReportsExactModelLoadCommandWhenLMStudioHasNoModel(testContext *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, _ *http.Request) {
		responseWriter.Header().Set("Content-Type", "application/json")
		responseWriter.WriteHeader(http.StatusBadRequest)
		_, _ = responseWriter.Write([]byte(`{"error":"No models loaded."}`))
	}))
	defer server.Close()

	config := testArchiveRuntimeConfig(testContext, server.URL+"/v1")
	sourcePath := filepath.Join("..", "..", "testdata", "synthetic-openai-export.zip")
	if importError := run([]string{"import", sourcePath}, config); importError != nil {
		testContext.Fatalf("import CLI fixture: %v", importError)
	}
	indexError := run([]string{
		"index", "build",
		"--dimensions", "3",
	}, config)
	if indexError == nil || !strings.Contains(indexError.Error(), "lms load text-embedding-nomic-embed-text-v1.5") {
		testContext.Fatalf("missing actionable model load instruction: %v", indexError)
	}
}

func testArchiveRuntimeConfig(testContext *testing.T, baseURL string) runtimeconfig.Config {
	testContext.Helper()
	environment := map[string]string{
		runtimeconfig.DataDirectoryEnvironment: filepath.Join(testContext.TempDir(), "data"),
		inference.BaseURLEnvironment:           baseURL,
	}
	config, configError := runtimeconfig.Load(
		func(key string) string { return environment[key] },
		testContext.TempDir(),
		bytes.NewReader(bytes.Repeat([]byte{0x3c}, 32)),
	)
	if configError != nil {
		testContext.Fatalf("load archive command config: %v", configError)
	}
	return config
}
