package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/inference"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/ingest"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/retrieval"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/runtimeconfig"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/store"
)

const openAITestDimensions = 3

type openAITestEmbedder struct{}

func (openAITestEmbedder) Embed(
	_ context.Context,
	inputs []string,
) ([][]float32, error) {
	vectors := make([][]float32, len(inputs))
	for inputIndex := range inputs {
		vectors[inputIndex] = []float32{1, 0, 0}
	}
	return vectors, nil
}

func TestOpenAIProviderHTTPContract(testContext *testing.T) {
	inferenceServer := newOpenAITestInferenceServer(testContext)
	config := loadTestRuntimeConfig(testContext, map[string]string{
		inference.BaseURLEnvironment: inferenceServer.URL + "/v1",
	})
	seedOpenAISearchArchive(testContext, config)

	var logOutput bytes.Buffer
	handler, handlerError := newApplicationHandler(
		config,
		slog.New(slog.NewTextHandler(&logOutput, nil)),
	)
	if handlerError != nil {
		testContext.Fatalf("create application handler: %v", handlerError)
	}
	defer handler.Close()
	server := httptest.NewServer(handler)
	defer server.Close()

	testContext.Run("reports a complete private search workspace", func(testContext *testing.T) {
		response, requestError := http.Get(server.URL + openAIProviderPath)
		if requestError != nil {
			testContext.Fatalf("request OpenAI provider: %v", requestError)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			testContext.Fatalf(
				"OpenAI provider status = %d; want %d",
				response.StatusCode,
				http.StatusOK,
			)
		}
		var snapshot openAIProviderSnapshot
		if decodeError := json.NewDecoder(response.Body).Decode(&snapshot); decodeError != nil {
			testContext.Fatalf("decode OpenAI provider: %v", decodeError)
		}
		if snapshot.Provider != openAIProviderID ||
			snapshot.State != openAIStateIndexReady ||
			snapshot.Statistics.Imports != 1 ||
			snapshot.Statistics.Conversations == 0 ||
			snapshot.Statistics.Messages == 0 ||
			snapshot.SearchIndex == nil ||
			snapshot.SearchIndex.DocumentCount == 0 ||
			snapshot.SearchIndex.DocumentCount != snapshot.SearchIndex.EligibleDocumentCount ||
			snapshot.Capabilities.BrowserUpload ||
			snapshot.Capabilities.InferenceBoundary != runtimeconfig.InferenceBoundaryLoopback {
			testContext.Fatalf("unexpected OpenAI provider snapshot: %+v", snapshot)
		}
	})

	testContext.Run("runs hybrid semantic search without logging query text", func(testContext *testing.T) {
		payload := openAISearchRequest{
			Query:           "synthetic conversation",
			Mode:            retrieval.SearchModeHybrid,
			Limit:           10,
			Excerpts:        2,
			IncludeArchived: true,
		}
		response := performOpenAISearchRequest(testContext, server.URL, config, payload)
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(response.Body)
			testContext.Fatalf(
				"OpenAI search status = %d; want %d: %s",
				response.StatusCode,
				http.StatusOK,
				body,
			)
		}
		var searchResponse openAISearchResponse
		if decodeError := json.NewDecoder(response.Body).Decode(&searchResponse); decodeError != nil {
			testContext.Fatalf("decode OpenAI search: %v", decodeError)
		}
		if len(searchResponse.Results) == 0 {
			testContext.Fatalf("OpenAI hybrid search returned no conversations")
		}
		for _, result := range searchResponse.Results {
			if strings.TrimSpace(result.ConversationID) == "" ||
				strings.TrimSpace(result.ConversationTitle) == "" ||
				len(result.Excerpts) == 0 {
				testContext.Fatalf("incomplete OpenAI search result: %+v", result)
			}
		}
		if strings.Contains(logOutput.String(), payload.Query) {
			testContext.Fatalf("OpenAI search query appeared in application logs")
		}
	})

	testContext.Run("runs lexical search through the same ready index", func(testContext *testing.T) {
		response := performOpenAISearchRequest(
			testContext,
			server.URL,
			config,
			openAISearchRequest{
				Query:           "berth",
				Mode:            retrieval.SearchModeLexical,
				Limit:           10,
				Excerpts:        2,
				IncludeArchived: true,
			},
		)
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(response.Body)
			testContext.Fatalf(
				"OpenAI lexical search status = %d; want %d: %s",
				response.StatusCode,
				http.StatusOK,
				body,
			)
		}
		var searchResponse openAISearchResponse
		if decodeError := json.NewDecoder(response.Body).Decode(&searchResponse); decodeError != nil {
			testContext.Fatalf("decode OpenAI lexical search: %v", decodeError)
		}
		if len(searchResponse.Results) == 0 {
			testContext.Fatalf("OpenAI lexical search returned no conversations")
		}
	})

	testContext.Run("rejects an invalid search contract", func(testContext *testing.T) {
		response := performOpenAISearchRequest(
			testContext,
			server.URL,
			config,
			openAISearchRequest{
				Query:           "synthetic conversation",
				Mode:            "nearby",
				Limit:           10,
				Excerpts:        2,
				IncludeArchived: true,
			},
		)
		defer response.Body.Close()
		if response.StatusCode != http.StatusUnprocessableEntity {
			testContext.Fatalf(
				"invalid OpenAI search status = %d; want %d",
				response.StatusCode,
				http.StatusUnprocessableEntity,
			)
		}
		var payload requestErrorResponse
		if decodeError := json.NewDecoder(response.Body).Decode(&payload); decodeError != nil {
			testContext.Fatalf("decode invalid OpenAI search response: %v", decodeError)
		}
		if payload.Error.Code != "invalid_openai_search_mode" {
			testContext.Fatalf("invalid OpenAI search error = %q", payload.Error.Code)
		}
	})
}

func TestOpenAIProviderReportsEmptyArchive(testContext *testing.T) {
	config := testRuntimeConfig(testContext)
	snapshot, snapshotError := loadOpenAIProviderSnapshot(context.Background(), config)
	if snapshotError != nil {
		testContext.Fatalf("load empty OpenAI provider snapshot: %v", snapshotError)
	}
	if snapshot.Provider != openAIProviderID ||
		snapshot.State != openAIStateEmpty ||
		snapshot.SearchIndex != nil ||
		snapshot.Statistics != (openAIArchiveStatistics{}) {
		testContext.Fatalf("unexpected empty OpenAI provider snapshot: %+v", snapshot)
	}
}

func TestOpenAIProviderReportsIndexRequired(testContext *testing.T) {
	config := testRuntimeConfig(testContext)
	importOpenAITestArchive(testContext, config)
	snapshot, snapshotError := loadOpenAIProviderSnapshot(context.Background(), config)
	if snapshotError != nil {
		testContext.Fatalf("load unindexed OpenAI provider snapshot: %v", snapshotError)
	}
	if snapshot.Provider != openAIProviderID ||
		snapshot.State != openAIStateIndexNeeded ||
		snapshot.SearchIndex != nil ||
		snapshot.Statistics.Imports != 1 ||
		snapshot.Statistics.Conversations == 0 ||
		snapshot.Statistics.Messages == 0 {
		testContext.Fatalf("unexpected unindexed OpenAI provider snapshot: %+v", snapshot)
	}
}

func TestOpenAIProviderRejectsAnIndexFromAnotherInferenceIdentity(
	testContext *testing.T,
) {
	dataDirectory := filepath.Join(testContext.TempDir(), "data")
	originalInferenceServer := newOpenAITestInferenceServer(testContext)
	originalConfig := loadTestRuntimeConfig(testContext, map[string]string{
		runtimeconfig.DataDirectoryEnvironment: dataDirectory,
		inference.BaseURLEnvironment:           originalInferenceServer.URL + "/v1",
	})
	seedOpenAISearchArchive(testContext, originalConfig)

	currentInferenceServer := newOpenAITestInferenceServer(testContext)
	currentConfig := loadTestRuntimeConfig(testContext, map[string]string{
		runtimeconfig.DataDirectoryEnvironment: dataDirectory,
		inference.BaseURLEnvironment:           currentInferenceServer.URL + "/v1",
	})
	handler, handlerError := newApplicationHandler(
		currentConfig,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if handlerError != nil {
		testContext.Fatalf("create application handler: %v", handlerError)
	}
	defer handler.Close()
	server := httptest.NewServer(handler)
	defer server.Close()

	response, requestError := http.Get(server.URL + openAIProviderPath)
	if requestError != nil {
		testContext.Fatalf("request OpenAI provider: %v", requestError)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		testContext.Fatalf(
			"OpenAI provider status = %d; want %d",
			response.StatusCode,
			http.StatusOK,
		)
	}
	var snapshot openAIProviderSnapshot
	if decodeError := json.NewDecoder(response.Body).Decode(&snapshot); decodeError != nil {
		testContext.Fatalf("decode OpenAI provider: %v", decodeError)
	}
	if snapshot.State != openAIStateIndexNeeded || snapshot.SearchIndex != nil {
		testContext.Fatalf(
			"OpenAI provider exposed an incompatible ready index: %+v",
			snapshot,
		)
	}

	searchResponse := performOpenAISearchRequest(
		testContext,
		server.URL,
		currentConfig,
		openAISearchRequest{
			Query:           "synthetic conversation",
			Mode:            retrieval.SearchModeLexical,
			Limit:           10,
			Excerpts:        2,
			IncludeArchived: true,
		},
	)
	defer searchResponse.Body.Close()
	if searchResponse.StatusCode != http.StatusConflict {
		body, _ := io.ReadAll(searchResponse.Body)
		testContext.Fatalf(
			"OpenAI search status = %d; want %d: %s",
			searchResponse.StatusCode,
			http.StatusConflict,
			body,
		)
	}
	var errorPayload requestErrorResponse
	if decodeError := json.NewDecoder(searchResponse.Body).Decode(&errorPayload); decodeError != nil {
		testContext.Fatalf("decode incompatible-index search response: %v", decodeError)
	}
	if errorPayload.Error.Code != "openai_index_required" {
		testContext.Fatalf(
			"incompatible-index search error = %q; want openai_index_required",
			errorPayload.Error.Code,
		)
	}
}

func seedOpenAISearchArchive(
	testContext *testing.T,
	config runtimeconfig.Config,
) {
	testContext.Helper()
	importOpenAITestArchive(testContext, config)
	openedStore, openError := store.Open(config.ArchiveDatabase())
	if openError != nil {
		testContext.Fatalf("open OpenAI archive store: %v", openError)
	}
	defer openedStore.Close()

	service := retrieval.IndexService{
		Store:    openedStore,
		Embedder: openAITestEmbedder{},
	}
	searchIndex, _, indexError := service.Run(
		context.Background(),
		retrieval.IndexOptions{
			Name:           retrieval.DefaultIndexName,
			Provider:       inference.DefaultEmbeddingProvider,
			Model:          "openai-browser-contract-embedding",
			Dimensions:     openAITestDimensions,
			BaseURL:        config.InferenceBaseURL(),
			DocumentPrefix: retrieval.DefaultDocumentPrefix,
			QueryPrefix:    retrieval.DefaultQueryPrefix,
			BatchSize:      8,
		},
	)
	if indexError != nil {
		testContext.Fatalf("index synthetic OpenAI archive: %v", indexError)
	}
	if searchIndex.Status != openAIStateIndexReady {
		testContext.Fatalf("search index status = %q; want ready", searchIndex.Status)
	}
}

func importOpenAITestArchive(
	testContext *testing.T,
	config runtimeconfig.Config,
) {
	testContext.Helper()
	openedStore, openError := store.Open(config.ArchiveDatabase())
	if openError != nil {
		testContext.Fatalf("open OpenAI archive store: %v", openError)
	}
	defer openedStore.Close()

	importer := ingest.Importer{Store: openedStore}
	sourcePath := filepath.Join("testdata", "synthetic-openai-export.zip")
	if _, importError := importer.Import(
		context.Background(),
		sourcePath,
		false,
	); importError != nil {
		testContext.Fatalf("import synthetic OpenAI archive: %v", importError)
	}
}

func performOpenAISearchRequest(
	testContext *testing.T,
	serverURL string,
	config runtimeconfig.Config,
	payload openAISearchRequest,
) *http.Response {
	testContext.Helper()
	encodedPayload, encodeError := json.Marshal(payload)
	if encodeError != nil {
		testContext.Fatalf("encode OpenAI search request: %v", encodeError)
	}
	request, requestError := http.NewRequest(
		http.MethodPost,
		serverURL+openAISearchPath,
		bytes.NewReader(encodedPayload),
	)
	if requestError != nil {
		testContext.Fatalf("create OpenAI search request: %v", requestError)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", serverURL)
	request.Header.Set(csrfHeaderName, config.CSRFToken())
	response, requestError := http.DefaultClient.Do(request)
	if requestError != nil {
		testContext.Fatalf("perform OpenAI search request: %v", requestError)
	}
	return response
}

func newOpenAITestInferenceServer(testContext *testing.T) *httptest.Server {
	testContext.Helper()
	server := httptest.NewServer(http.HandlerFunc(
		func(responseWriter http.ResponseWriter, request *http.Request) {
			if request.URL.Path != "/v1/embeddings" || request.Method != http.MethodPost {
				http.NotFound(responseWriter, request)
				return
			}
			var payload struct {
				Input []string `json:"input"`
			}
			if decodeError := json.NewDecoder(request.Body).Decode(&payload); decodeError != nil {
				http.Error(responseWriter, "invalid embedding request", http.StatusBadRequest)
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
		},
	))
	testContext.Cleanup(server.Close)
	return server
}
