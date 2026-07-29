package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/domain"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/embedding"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/retrieval"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/runtimeconfig"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/store"
)

const (
	openAIProviderPath     = "/api/providers/openai"
	openAISearchPath       = "/api/providers/openai/search"
	openAIProviderID       = "openai"
	openAIStateEmpty       = "empty"
	openAIStateIndexReady  = "ready"
	openAIStateIndexNeeded = "index_required"

	maxOpenAIQueryBytes = 2 * 1024
	maxOpenAIResults    = 50
	maxOpenAIExcerpts   = 5
)

var openAISearchModes = []string{
	retrieval.SearchModeHybrid,
	retrieval.SearchModeSemantic,
	retrieval.SearchModeLexical,
}

type openAIProviderCapability struct {
	SemanticSearch bool `json:"semantic_search"`
	BrowserUpload  bool `json:"browser_upload"`
}

type openAIProviderSnapshot struct {
	Provider     string                      `json:"provider"`
	State        string                      `json:"state"`
	Statistics   openAIArchiveStatistics     `json:"statistics"`
	SearchIndex  *openAISearchIndexSnapshot  `json:"search_index,omitempty"`
	Capabilities openAIWorkspaceCapabilities `json:"capabilities"`
}

type openAIArchiveStatistics struct {
	Imports       int64 `json:"imports"`
	Conversations int64 `json:"conversations"`
	Messages      int64 `json:"messages"`
}

type openAISearchIndexSnapshot struct {
	ID                        int64  `json:"id"`
	Name                      string `json:"name"`
	Model                     string `json:"model"`
	Dimensions                int    `json:"dimensions"`
	DocumentCount             int64  `json:"document_count"`
	EligibleDocumentCount     int64  `json:"eligible_document_count"`
	ConversationCount         int64  `json:"conversation_count"`
	EligibleConversationCount int64  `json:"eligible_conversation_count"`
}

type openAIWorkspaceCapabilities struct {
	BrowserUpload     bool                            `json:"browser_upload"`
	SearchModes       []string                        `json:"search_modes"`
	MaxQueryBytes     int                             `json:"max_query_bytes"`
	MaxResults        int                             `json:"max_results"`
	MaxExcerpts       int                             `json:"max_excerpts"`
	InferenceBoundary runtimeconfig.InferenceBoundary `json:"inference_boundary"`
}

type openAISearchRequest struct {
	Query           string `json:"query"`
	Mode            string `json:"mode"`
	Limit           int    `json:"limit"`
	Excerpts        int    `json:"excerpts"`
	IncludeArchived bool   `json:"include_archived"`
}

type openAISearchResponse struct {
	Results              []domain.ConversationSearchResult `json:"results"`
	QueryEmbeddingCached bool                              `json:"query_embedding_cached"`
}

func registerOpenAIRoutes(
	routes *http.ServeMux,
	config runtimeconfig.Config,
	logger *slog.Logger,
) {
	routes.HandleFunc(
		"GET "+openAIProviderPath,
		getOpenAIProvider(config, logger),
	)
	routes.HandleFunc(
		"POST "+openAISearchPath,
		searchOpenAIProvider(config, logger),
	)
}

func getOpenAIProvider(
	config runtimeconfig.Config,
	logger *slog.Logger,
) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, request *http.Request) {
		if queryError := requireQueryKeys(request, nil); queryError != nil {
			writeRequestError(responseWriter, http.StatusBadRequest, "invalid_query")
			return
		}
		snapshot, snapshotError := loadOpenAIProviderSnapshot(request.Context(), config)
		if snapshotError != nil {
			logger.Error("OpenAI provider snapshot failed", "error_type", "openai_snapshot_failed")
			writeRequestError(responseWriter, http.StatusInternalServerError, "openai_unavailable")
			return
		}
		writeJSON(responseWriter, logger, http.StatusOK, snapshot)
	}
}

func searchOpenAIProvider(
	config runtimeconfig.Config,
	logger *slog.Logger,
) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, request *http.Request) {
		if queryError := requireQueryKeys(request, nil); queryError != nil {
			writeRequestError(responseWriter, http.StatusBadRequest, "invalid_query")
			return
		}
		var payload openAISearchRequest
		if decodeError := decodeJSONRequest(responseWriter, request, &payload); decodeError != nil {
			writeJSONRequestError(responseWriter, decodeError)
			return
		}
		payload.Query = strings.TrimSpace(payload.Query)
		if validationCode := validateOpenAISearchRequest(payload); validationCode != "" {
			writeRequestError(responseWriter, http.StatusUnprocessableEntity, validationCode)
			return
		}

		openedStore, openError := store.Open(config.ArchiveDatabase())
		if openError != nil {
			logger.Error("OpenAI search failed", "error_type", "openai_store_unavailable")
			writeRequestError(responseWriter, http.StatusInternalServerError, "openai_unavailable")
			return
		}
		defer openedStore.Close()

		searchSummary, searchSummaryExists, summaryError :=
			latestCompleteReadyOpenAISearchIndex(request.Context(), openedStore)
		if summaryError != nil {
			logger.Error("OpenAI search failed", "error_type", "openai_index_unavailable")
			writeRequestError(responseWriter, http.StatusInternalServerError, "openai_unavailable")
			return
		}
		if !searchSummaryExists {
			writeRequestError(responseWriter, http.StatusConflict, "openai_index_required")
			return
		}
		searchIndex := searchSummary.Config

		var queryEmbedder embedding.Embedder
		effectiveBaseURL := searchIndex.BaseURL
		if payload.Mode != retrieval.SearchModeLexical {
			effectiveBaseURL = config.InferenceBaseURL().String()
			if searchIndex.BaseURL != effectiveBaseURL {
				writeRequestError(
					responseWriter,
					http.StatusConflict,
					"openai_index_identity_mismatch",
				)
				return
			}
			queryEmbedder = &embedding.HTTPEmbedder{
				BaseURL:     config.InferenceBaseURL(),
				Model:       searchIndex.Model,
				Dimensions:  searchIndex.Dimensions,
				InputPrefix: searchIndex.QueryPrefix,
			}
		}

		engine := retrieval.Engine{
			Store:         openedStore,
			QueryEmbedder: queryEmbedder,
			QueryBaseURL:  effectiveBaseURL,
		}
		results, searchError := engine.Search(
			request.Context(),
			retrieval.SearchOptions{
				IndexID:          searchIndex.ID,
				Query:            payload.Query,
				Mode:             payload.Mode,
				Limit:            payload.Limit,
				MinSemanticScore: retrieval.DefaultMinSemanticScore,
				IncludeArchived:  payload.IncludeArchived,
				Excerpts:         payload.Excerpts,
			},
		)
		if searchError != nil {
			if errors.Is(request.Context().Err(), context.Canceled) ||
				errors.Is(request.Context().Err(), context.DeadlineExceeded) {
				writeRequestError(responseWriter, http.StatusRequestTimeout, "search_canceled")
				return
			}
			logger.Error("OpenAI search failed", "error_type", "openai_search_unavailable")
			writeRequestError(
				responseWriter,
				http.StatusServiceUnavailable,
				"openai_search_unavailable",
			)
			return
		}
		writeJSON(responseWriter, logger, http.StatusOK, openAISearchResponse{
			Results:              results,
			QueryEmbeddingCached: engine.LastQueryCacheHit,
		})
	}
}

func loadOpenAIProviderSnapshot(
	contextValue context.Context,
	config runtimeconfig.Config,
) (openAIProviderSnapshot, error) {
	openedStore, openError := store.Open(config.ArchiveDatabase())
	if openError != nil {
		return openAIProviderSnapshot{}, openError
	}
	defer openedStore.Close()

	statistics, statisticsError := openedStore.Stats(contextValue)
	if statisticsError != nil {
		return openAIProviderSnapshot{}, statisticsError
	}

	snapshot := openAIProviderSnapshot{
		Provider: openAIProviderID,
		State:    openAIStateEmpty,
		Statistics: openAIArchiveStatistics{
			Imports:       statistics.Imports,
			Conversations: statistics.Conversations,
			Messages:      statistics.Messages,
		},
		Capabilities: openAIWorkspaceCapabilities{
			BrowserUpload:     false,
			SearchModes:       append([]string(nil), openAISearchModes...),
			MaxQueryBytes:     maxOpenAIQueryBytes,
			MaxResults:        maxOpenAIResults,
			MaxExcerpts:       maxOpenAIExcerpts,
			InferenceBoundary: config.InferenceBoundary(),
		},
	}
	if statistics.Imports == 0 {
		return snapshot, nil
	}
	snapshot.State = openAIStateIndexNeeded
	summary, summaryExists, summaryError :=
		latestCompleteReadyOpenAISearchIndex(contextValue, openedStore)
	if summaryError != nil {
		return openAIProviderSnapshot{}, summaryError
	}
	if summaryExists {
		snapshot.State = openAIStateIndexReady
		snapshot.SearchIndex = &openAISearchIndexSnapshot{
			ID:                        summary.Config.ID,
			Name:                      summary.Config.Name,
			Model:                     summary.Config.Model,
			Dimensions:                summary.Config.Dimensions,
			DocumentCount:             summary.DocumentCount,
			EligibleDocumentCount:     summary.EligibleCount,
			ConversationCount:         summary.CoveredConversations,
			EligibleConversationCount: summary.EligibleConversations,
		}
	}
	return snapshot, nil
}

func latestCompleteReadyOpenAISearchIndex(
	contextValue context.Context,
	openedStore *store.Store,
) (domain.SearchIndexSummary, bool, error) {
	indexSummaries, summariesError := openedStore.ListSearchIndexSummaries(contextValue)
	if summariesError != nil {
		return domain.SearchIndexSummary{}, false, summariesError
	}
	for summaryIndex := len(indexSummaries) - 1; summaryIndex >= 0; summaryIndex-- {
		summary := indexSummaries[summaryIndex]
		if isCompleteReadySearchIndex(summary) {
			return summary, true, nil
		}
	}
	return domain.SearchIndexSummary{}, false, nil
}

func isCompleteReadySearchIndex(summary domain.SearchIndexSummary) bool {
	return summary.Config.Status == openAIStateIndexReady &&
		summary.EligibleCount > 0 &&
		summary.DocumentCount == summary.EligibleCount &&
		summary.CoveredConversations == summary.EligibleConversations
}

func validateOpenAISearchRequest(payload openAISearchRequest) string {
	if payload.Query == "" || len([]byte(payload.Query)) > maxOpenAIQueryBytes {
		return "invalid_openai_query"
	}
	if !containsString(openAISearchModes, payload.Mode) {
		return "invalid_openai_search_mode"
	}
	if payload.Limit < 1 || payload.Limit > maxOpenAIResults {
		return "invalid_openai_result_limit"
	}
	if payload.Excerpts < 1 || payload.Excerpts > maxOpenAIExcerpts {
		return "invalid_openai_excerpt_limit"
	}
	return ""
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
