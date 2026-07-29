package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/domain"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/intent"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/normalize"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/privatepath"
)

type AuditMetadata struct {
	ApplicationVersion      string
	Since                   time.Time
	Until                   time.Time
	Timezone                string
	IncludeArchived         bool
	IntentConfigPath        string
	IntentConfig            intent.DefinitionConfig
	SemanticEnabled         bool
	EmbeddingConfig         domain.EmbeddingConfig
	EffectiveEmbeddingURL   string
	VerificationEnabled     bool
	VerifierModel           string
	VerifierBaseURL         string
	VerifierBatchSize       int
	VerifierTimeout         time.Duration
	VerifierMaxRetries      int
	VerificationCacheHits   int
	VerificationCacheMisses int
}

type SearchAuditMetadata struct {
	ApplicationVersion   string
	Query                string
	Mode                 string
	IndexConfig          domain.SearchIndexConfig
	EffectiveBaseURL     string
	Since                *time.Time
	Until                *time.Time
	Timezone             string
	IncludeArchived      bool
	MinSemanticScore     float64
	Limit                int
	Excerpts             int
	ResultCount          int
	Elapsed              time.Duration
	QueryEmbeddingCached bool
}

func WriteConversationSearchResults(file privatepath.File, format string, results []domain.ConversationSearchResult) error {
	normalizedFormat := strings.ToLower(strings.TrimSpace(format))
	if normalizedFormat == "" {
		normalizedFormat = strings.TrimPrefix(strings.ToLower(filepath.Ext(file.RelativePath())), ".")
	}
	switch normalizedFormat {
	case "json":
		encodedResults, marshalError := json.MarshalIndent(results, "", "  ")
		if marshalError != nil {
			return fmt.Errorf("encode conversation search JSON: %w", marshalError)
		}
		if writeError := writePrivateFile(file, append(encodedResults, '\n')); writeError != nil {
			return fmt.Errorf("write conversation search JSON: %w", writeError)
		}
		return nil
	case "csv":
		return writeConversationSearchCSV(file, results)
	case "table", "text", "txt":
		return writeConversationSearchTable(file, results)
	default:
		return fmt.Errorf("unsupported conversation search format %q; use table, csv, or json", normalizedFormat)
	}
}

func WriteConversationSearchAudit(file privatepath.File, metadata SearchAuditMetadata) error {
	dateRange := map[string]any{
		"timezone": metadata.Timezone,
	}
	if metadata.Since != nil {
		dateRange["since"] = metadata.Since.UTC().Format(time.RFC3339Nano)
	}
	if metadata.Until != nil {
		dateRange["until"] = metadata.Until.UTC().Format(time.RFC3339Nano)
	}
	audit := map[string]any{
		"application_version":       metadata.ApplicationVersion,
		"generated_at":              time.Now().UTC().Format(time.RFC3339Nano),
		"query":                     metadata.Query,
		"mode":                      metadata.Mode,
		"date_range":                dateRange,
		"include_archived":          metadata.IncludeArchived,
		"minimum_semantic_score":    metadata.MinSemanticScore,
		"limit":                     metadata.Limit,
		"excerpts_per_conversation": metadata.Excerpts,
		"result_count":              metadata.ResultCount,
		"elapsed":                   metadata.Elapsed.String(),
		"query_embedding_cached":    metadata.QueryEmbeddingCached,
		"search_index": map[string]any{
			"id":                 metadata.IndexConfig.ID,
			"name":               metadata.IndexConfig.Name,
			"status":             metadata.IndexConfig.Status,
			"provider":           metadata.IndexConfig.Provider,
			"model":              metadata.IndexConfig.Model,
			"dimensions":         metadata.IndexConfig.Dimensions,
			"stored_base_url":    metadata.IndexConfig.BaseURL,
			"effective_base_url": metadata.EffectiveBaseURL,
			"document_prefix":    metadata.IndexConfig.DocumentPrefix,
			"query_prefix":       metadata.IndexConfig.QueryPrefix,
			"builder_version":    metadata.IndexConfig.BuilderVersion,
			"corpus_policy":      metadata.IndexConfig.CorpusPolicy,
		},
	}
	encodedAudit, marshalError := json.MarshalIndent(audit, "", "  ")
	if marshalError != nil {
		return fmt.Errorf("encode conversation search audit: %w", marshalError)
	}
	if writeError := writePrivateFile(file, append(encodedAudit, '\n')); writeError != nil {
		return fmt.Errorf("write conversation search audit: %w", writeError)
	}
	return nil
}

func WriteDefinitionResults(file privatepath.File, format string, results []domain.DefinitionResult) error {
	normalizedFormat := strings.ToLower(strings.TrimSpace(format))
	if normalizedFormat == "" {
		normalizedFormat = strings.TrimPrefix(strings.ToLower(filepath.Ext(file.RelativePath())), ".")
	}
	switch normalizedFormat {
	case "csv":
		return writeCSV(file, results)
	case "json":
		return writeJSON(file, results)
	default:
		return fmt.Errorf("unsupported report format %q; use csv or json", normalizedFormat)
	}
}

func WriteAudit(file privatepath.File, output intent.AnalyzeOutput, metadata AuditMetadata) error {
	encodedIntentConfig, marshalConfigError := json.Marshal(metadata.IntentConfig)
	if marshalConfigError != nil {
		return fmt.Errorf("encode audit intent configuration: %w", marshalConfigError)
	}
	embeddingConfiguration := any(nil)
	if metadata.EmbeddingConfig.ID > 0 {
		embeddingConfiguration = map[string]any{
			"id":                    metadata.EmbeddingConfig.ID,
			"status":                metadata.EmbeddingConfig.Status,
			"provider":              metadata.EmbeddingConfig.Provider,
			"model":                 metadata.EmbeddingConfig.Model,
			"dimensions":            metadata.EmbeddingConfig.Dimensions,
			"stored_base_url":       metadata.EmbeddingConfig.BaseURL,
			"effective_base_url":    metadata.EffectiveEmbeddingURL,
			"input_prefix":          metadata.EmbeddingConfig.InputPrefix,
			"preprocessing_version": metadata.EmbeddingConfig.PreprocessingVersion,
			"context_version":       metadata.EmbeddingConfig.ContextVersion,
		}
	}
	audit := map[string]any{
		"application_version": metadata.ApplicationVersion,
		"generated_at":        time.Now().UTC().Format(time.RFC3339Nano),
		"date_range": map[string]any{
			"since":    metadata.Since.UTC().Format(time.RFC3339Nano),
			"until":    metadata.Until.UTC().Format(time.RFC3339Nano),
			"timezone": metadata.Timezone,
		},
		"include_archived": metadata.IncludeArchived,
		"intent": map[string]any{
			"name":                 metadata.IntentConfig.Name,
			"config_path":          metadata.IntentConfigPath,
			"config_sha256":        normalize.Hash(string(encodedIntentConfig)),
			"semantic_threshold":   metadata.IntentConfig.SemanticThreshold,
			"semantic_margin":      metadata.IntentConfig.SemanticMargin,
			"review_threshold":     metadata.IntentConfig.ReviewThreshold,
			"lexical_review_score": metadata.IntentConfig.LexicalReviewScore,
		},
		"semantic_enabled":        metadata.SemanticEnabled,
		"embedding_configuration": embeddingConfiguration,
		"verification": map[string]any{
			"enabled":        metadata.VerificationEnabled,
			"model":          metadata.VerifierModel,
			"base_url":       metadata.VerifierBaseURL,
			"batch_size":     metadata.VerifierBatchSize,
			"timeout":        metadata.VerifierTimeout.String(),
			"max_retries":    metadata.VerifierMaxRetries,
			"prompt_version": intent.VerificationPromptVersion,
			"cache_hits":     metadata.VerificationCacheHits,
			"cache_misses":   metadata.VerificationCacheMisses,
		},
		"messages_examined":        output.MessagesExamined,
		"messages_with_embeddings": output.MessagesEmbedded,
		"retrieved_candidates":     output.Candidates,
		"verified_candidates":      output.Verified,
		"accepted_results":         len(output.Results),
		"review_results":           len(output.Review),
	}
	encodedAudit, marshalError := json.MarshalIndent(audit, "", "  ")
	if marshalError != nil {
		return fmt.Errorf("encode audit report: %w", marshalError)
	}
	if writeError := writePrivateFile(file, append(encodedAudit, '\n')); writeError != nil {
		return fmt.Errorf("write audit report: %w", writeError)
	}
	return nil
}

// RelatedFile derives a confined sibling report path from an existing report file.
func RelatedFile(
	file privatepath.File,
	suffix string,
	replacementExtension string,
) (privatepath.File, error) {
	relativePath := file.RelativePath()
	if relativePath == "" {
		return privatepath.File{}, fmt.Errorf("derive related report file: source file is not initialized")
	}
	extension := filepath.Ext(relativePath)
	if replacementExtension == "" {
		replacementExtension = extension
	}
	if replacementExtension != "" && !strings.HasPrefix(replacementExtension, ".") {
		return privatepath.File{}, fmt.Errorf(
			"derive related report file: replacement extension %q must begin with a dot",
			replacementExtension,
		)
	}
	baseName := strings.TrimSuffix(filepath.Base(relativePath), extension)
	return file.Sibling(baseName + suffix + replacementExtension)
}

func writeCSV(file privatepath.File, results []domain.DefinitionResult) error {
	outputFile, createError := openPrivateFile(file)
	if createError != nil {
		return fmt.Errorf("create CSV report: %w", createError)
	}
	defer outputFile.Close()

	csvWriter := csv.NewWriter(outputFile)
	header := []string{
		"date",
		"term",
		"category",
		"exact_user_message",
		"conversation_title",
		"archived",
		"classification_confidence",
		"detection_methods",
		"semantic_positive",
		"semantic_negative",
		"semantic_margin",
		"needs_review",
		"verifier_explanation",
		"conversation_id",
		"message_id",
		"source_message_id",
	}
	if writeError := csvWriter.Write(header); writeError != nil {
		return fmt.Errorf("write CSV header: %w", writeError)
	}
	for _, result := range results {
		archivedValue := "unknown"
		if result.Archived != nil {
			archivedValue = strconv.FormatBool(*result.Archived)
		}
		record := []string{
			result.DateISO,
			result.Term,
			result.Category,
			result.ExactUserMessage,
			result.ConversationTitle,
			archivedValue,
			strconv.FormatFloat(result.Confidence, 'f', 6, 64),
			strings.Join(result.DetectionMethods, ";"),
			strconv.FormatFloat(result.SemanticPositive, 'f', 6, 64),
			strconv.FormatFloat(result.SemanticNegative, 'f', 6, 64),
			strconv.FormatFloat(result.SemanticMargin, 'f', 6, 64),
			strconv.FormatBool(result.NeedsReview),
			result.VerifierExplanation,
			result.ConversationID,
			result.MessageID,
			result.SourceMessageID,
		}
		if writeError := csvWriter.Write(record); writeError != nil {
			return fmt.Errorf("write CSV result: %w", writeError)
		}
	}
	csvWriter.Flush()
	if writerError := csvWriter.Error(); writerError != nil {
		return fmt.Errorf("flush CSV report: %w", writerError)
	}
	if syncError := outputFile.Sync(); syncError != nil {
		return fmt.Errorf("flush CSV file: %w", syncError)
	}
	return nil
}

func writeJSON(file privatepath.File, results []domain.DefinitionResult) error {
	encodedResults, marshalError := json.MarshalIndent(results, "", "  ")
	if marshalError != nil {
		return fmt.Errorf("encode JSON report: %w", marshalError)
	}
	if writeError := writePrivateFile(file, append(encodedResults, '\n')); writeError != nil {
		return fmt.Errorf("write JSON report: %w", writeError)
	}
	return nil
}

func writeConversationSearchCSV(file privatepath.File, results []domain.ConversationSearchResult) error {
	outputFile, createError := openPrivateFile(file)
	if createError != nil {
		return fmt.Errorf("create conversation search CSV: %w", createError)
	}
	defer outputFile.Close()
	writer := csv.NewWriter(outputFile)
	if writeError := writer.Write([]string{
		"conversation_id",
		"conversation_title",
		"archived",
		"score",
		"semantic_score",
		"lexical_score",
		"excerpts_json",
	}); writeError != nil {
		return fmt.Errorf("write conversation search CSV header: %w", writeError)
	}
	for _, result := range results {
		archived := "unknown"
		if result.Archived != nil {
			archived = strconv.FormatBool(*result.Archived)
		}
		encodedExcerpts, marshalError := json.Marshal(result.Excerpts)
		if marshalError != nil {
			return fmt.Errorf("encode conversation search excerpts: %w", marshalError)
		}
		if writeError := writer.Write([]string{
			result.ConversationID,
			result.ConversationTitle,
			archived,
			strconv.FormatFloat(result.Score, 'f', 8, 64),
			strconv.FormatFloat(result.SemanticScore, 'f', 8, 64),
			strconv.FormatFloat(result.LexicalScore, 'f', 8, 64),
			string(encodedExcerpts),
		}); writeError != nil {
			return fmt.Errorf("write conversation search CSV result: %w", writeError)
		}
	}
	writer.Flush()
	if writerError := writer.Error(); writerError != nil {
		return fmt.Errorf("flush conversation search CSV: %w", writerError)
	}
	if syncError := outputFile.Sync(); syncError != nil {
		return fmt.Errorf("flush conversation search CSV file: %w", syncError)
	}
	return nil
}

func writeConversationSearchTable(file privatepath.File, results []domain.ConversationSearchResult) error {
	var output strings.Builder
	for resultIndex, result := range results {
		fmt.Fprintf(
			&output,
			"%d. %s [score=%.6f semantic=%.4f]\n",
			resultIndex+1,
			result.ConversationTitle,
			result.Score,
			result.SemanticScore,
		)
		for _, excerpt := range result.Excerpts {
			fmt.Fprintf(&output, "   %s: %s\n", excerpt.Role, strings.Join(strings.Fields(excerpt.Text), " "))
		}
		output.WriteByte('\n')
	}
	if writeError := writePrivateFile(file, []byte(output.String())); writeError != nil {
		return fmt.Errorf("write conversation search table: %w", writeError)
	}
	return nil
}

func writePrivateFile(file privatepath.File, contents []byte) error {
	if prepareError := file.Prepare(); prepareError != nil {
		return prepareError
	}
	return os.WriteFile(file.Path(), contents, 0o600)
}

func openPrivateFile(file privatepath.File) (*os.File, error) {
	if prepareError := file.Prepare(); prepareError != nil {
		return nil, prepareError
	}
	return os.OpenFile(file.Path(), os.O_WRONLY|os.O_TRUNC, 0o600)
}
