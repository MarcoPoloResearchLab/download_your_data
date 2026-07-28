package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/domain"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/intent"
)

func TestWriteAuditIncludesReproducibleInferenceMetadata(testContext *testing.T) {
	outputPath := filepath.Join(testContext.TempDir(), "definition-audit.json")
	metadata := AuditMetadata{
		ApplicationVersion: "test-version",
		Since:              time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		Until:              time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Timezone:           "America/Los_Angeles",
		IncludeArchived:    true,
		IntentConfigPath:   "configs/definition_request.json",
		IntentConfig: intent.DefinitionConfig{
			Name:              "definition_request",
			SemanticThreshold: 0.7,
		},
		SemanticEnabled: true,
		EmbeddingConfig: domain.EmbeddingConfig{
			ID:                   7,
			Status:               "ready",
			Provider:             "lmstudio",
			Model:                "download-your-data-embedding",
			Dimensions:           768,
			BaseURL:              "http://127.0.0.1:1234/v1",
			InputPrefix:          "classification: ",
			PreprocessingVersion: "2:prefix-hash",
			ContextVersion:       "2",
		},
		EffectiveEmbeddingURL:   "http://127.0.0.1:1235/v1",
		VerificationEnabled:     true,
		VerifierModel:           "download-your-data-verifier",
		VerifierBaseURL:         "http://127.0.0.1:1235/v1",
		VerifierBatchSize:       8,
		VerifierTimeout:         10 * time.Minute,
		VerifierMaxRetries:      2,
		VerificationCacheHits:   3,
		VerificationCacheMisses: 1,
	}
	if writeError := WriteAudit(outputPath, intent.AnalyzeOutput{}, metadata); writeError != nil {
		testContext.Fatalf("write audit: %v", writeError)
	}

	encodedAudit, readError := os.ReadFile(outputPath)
	if readError != nil {
		testContext.Fatalf("read audit: %v", readError)
	}
	var audit map[string]any
	if decodeError := json.Unmarshal(encodedAudit, &audit); decodeError != nil {
		testContext.Fatalf("decode audit: %v", decodeError)
	}
	embeddingConfiguration := audit["embedding_configuration"].(map[string]any)
	if embeddingConfiguration["input_prefix"] != "classification: " ||
		embeddingConfiguration["effective_base_url"] != "http://127.0.0.1:1235/v1" {
		testContext.Fatalf("missing embedding provenance: %+v", embeddingConfiguration)
	}
	verification := audit["verification"].(map[string]any)
	if verification["cache_hits"] != float64(3) || verification["max_retries"] != float64(2) {
		testContext.Fatalf("missing verification provenance: %+v", verification)
	}
}

func TestWriteConversationSearchResultsAndAudit(testContext *testing.T) {
	resultPath := filepath.Join(testContext.TempDir(), "anime-search.csv")
	results := []domain.ConversationSearchResult{{
		ConversationID:    "conversation-1",
		ConversationTitle: "Space westerns",
		Score:             0.03,
		SemanticScore:     0.91,
		Excerpts: []domain.ConversationSearchExcerpt{{
			MessageID:        "message-1",
			Role:             "assistant",
			Text:             "Cowboy Bebop is a Japanese animated series.",
			SemanticScore:    0.91,
			DetectionMethods: []string{"semantic"},
		}},
	}}
	if writeError := WriteConversationSearchResults(resultPath, "csv", results); writeError != nil {
		testContext.Fatalf("write conversation search CSV: %v", writeError)
	}
	encodedResults, readError := os.ReadFile(resultPath)
	if readError != nil {
		testContext.Fatalf("read conversation search CSV: %v", readError)
	}
	if !strings.Contains(string(encodedResults), "Space westerns") || !strings.Contains(string(encodedResults), "Cowboy Bebop") {
		testContext.Fatalf("search CSV lacks result evidence: %s", encodedResults)
	}

	auditPath := filepath.Join(testContext.TempDir(), "anime-search-audit.json")
	if writeError := WriteConversationSearchAudit(auditPath, SearchAuditMetadata{
		ApplicationVersion: "test-version",
		Query:              "anime",
		Mode:               "hybrid",
		IndexConfig: domain.SearchIndexConfig{
			ID:             4,
			Name:           "conversation-search",
			Status:         "ready",
			Model:          "download-your-data-embedding",
			Dimensions:     768,
			DocumentPrefix: "search_document: ",
			QueryPrefix:    "search_query: ",
		},
		MinSemanticScore: 0.35,
		ResultCount:      1,
	}); writeError != nil {
		testContext.Fatalf("write conversation search audit: %v", writeError)
	}
	encodedAudit, readError := os.ReadFile(auditPath)
	if readError != nil {
		testContext.Fatalf("read conversation search audit: %v", readError)
	}
	if !strings.Contains(string(encodedAudit), `"query_prefix": "search_query: "`) ||
		!strings.Contains(string(encodedAudit), `"query": "anime"`) {
		testContext.Fatalf("search audit lacks reproducibility metadata: %s", encodedAudit)
	}
}
