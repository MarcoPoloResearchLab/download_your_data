package retrieval

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/domain"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/embedding"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/normalize"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/store"
)

const (
	SearchModeHybrid        = "hybrid"
	SearchModeSemantic      = "semantic"
	SearchModeLexical       = "lexical"
	DefaultMinSemanticScore = 0.65
)

var ftsTokenPattern = regexp.MustCompile(`[\p{L}\p{N}]+`)

type SearchOptions struct {
	IndexID          int64
	Query            string
	Mode             string
	Limit            int
	MinSemanticScore float64
	IncludeArchived  bool
	SinceMillis      int64
	UntilMillis      int64
	Excerpts         int
}

type Engine struct {
	Store             *store.Store
	QueryEmbedder     embedding.Embedder
	QueryBaseURL      string
	LastQueryCacheHit bool
}

type rankedDocument struct {
	document       domain.SearchDocument
	semanticScore  float64
	lexicalScore   float64
	semanticRank   int
	lexicalRank    int
	fusionScore    float64
	detectionModes []string
}

func (engine *Engine) Search(contextValue context.Context, options SearchOptions) ([]domain.ConversationSearchResult, error) {
	engine.LastQueryCacheHit = false
	if engine.Store == nil {
		return nil, fmt.Errorf("conversation search requires a store")
	}
	query := strings.TrimSpace(options.Query)
	if query == "" {
		return nil, fmt.Errorf("conversation search query must not be empty")
	}
	if options.Mode == "" {
		options.Mode = SearchModeHybrid
	}
	if options.Mode != SearchModeHybrid && options.Mode != SearchModeSemantic && options.Mode != SearchModeLexical {
		return nil, fmt.Errorf("unsupported search mode %q; use hybrid, semantic, or lexical", options.Mode)
	}
	if options.Excerpts <= 0 {
		options.Excerpts = 3
	}

	var config domain.SearchIndexConfig
	var configError error
	if options.IndexID > 0 {
		config, configError = engine.Store.SearchIndexByID(contextValue, options.IndexID)
	} else {
		config, configError = engine.Store.LatestReadySearchIndex(contextValue)
	}
	if configError != nil {
		return nil, configError
	}
	if config.Status != "ready" {
		return nil, fmt.Errorf("search index %d is %s; finish indexing before searching", config.ID, config.Status)
	}

	documentRanks := make(map[string]*rankedDocument)
	candidateLimit := searchCandidateLimit(options.Limit)
	if options.Mode == SearchModeHybrid || options.Mode == SearchModeSemantic {
		if engine.QueryEmbedder == nil {
			return nil, fmt.Errorf("semantic conversation search requires a query embedder")
		}
		queryVector, vectorError := engine.queryVector(contextValue, config, query)
		if vectorError != nil {
			return nil, vectorError
		}
		documents, documentError := engine.Store.ListSearchDocumentsForScan(
			contextValue,
			config.ID,
			options.SinceMillis,
			options.UntilMillis,
			options.IncludeArchived,
		)
		if documentError != nil {
			return nil, documentError
		}
		documentByRow := make(map[int64]domain.SearchDocument, len(documents))
		for _, document := range documents {
			if document.VectorRow != nil {
				documentByRow[*document.VectorRow] = document
			}
		}
		semanticCandidates := make([]rankedDocument, 0, len(documents))
		privateVectorFile, pathError := engine.Store.ResolveSearchVectorFile(config)
		if pathError != nil {
			return nil, pathError
		}
		scanError := embedding.ScanVectorFile(
			privateVectorFile,
			config.Dimensions,
			func(row int64, vector []float32) error {
				document, exists := documentByRow[row]
				if !exists {
					return nil
				}
				score, dotError := embedding.DotProduct(queryVector, vector)
				if dotError != nil {
					return dotError
				}
				if score >= options.MinSemanticScore {
					semanticCandidates = append(semanticCandidates, rankedDocument{
						document:      document,
						semanticScore: score,
					})
				}
				return nil
			},
		)
		if scanError != nil {
			return nil, scanError
		}
		sort.Slice(semanticCandidates, func(leftIndex int, rightIndex int) bool {
			if semanticCandidates[leftIndex].semanticScore == semanticCandidates[rightIndex].semanticScore {
				return semanticCandidates[leftIndex].document.ID < semanticCandidates[rightIndex].document.ID
			}
			return semanticCandidates[leftIndex].semanticScore > semanticCandidates[rightIndex].semanticScore
		})
		if candidateLimit > 0 && len(semanticCandidates) > candidateLimit {
			semanticCandidates = semanticCandidates[:candidateLimit]
		}
		for candidateIndex, candidate := range semanticCandidates {
			rank := candidateIndex + 1
			candidate.semanticRank = rank
			candidate.fusionScore = reciprocalRank(rank)
			candidate.detectionModes = []string{"semantic"}
			copied := candidate
			documentRanks[candidate.document.ID] = &copied
		}
	}

	if options.Mode == SearchModeHybrid || options.Mode == SearchModeLexical {
		ftsQuery := buildFTSQuery(query)
		if ftsQuery != "" {
			lexicalHits, lexicalError := engine.Store.SearchLexicalDocuments(
				contextValue,
				config.ID,
				ftsQuery,
				options.SinceMillis,
				options.UntilMillis,
				options.IncludeArchived,
				0,
			)
			if lexicalError != nil {
				return nil, lexicalError
			}
			for hitIndex, hit := range lexicalHits {
				rank := hitIndex + 1
				candidate, exists := documentRanks[hit.Document.ID]
				if !exists {
					candidate = &rankedDocument{document: hit.Document}
					documentRanks[hit.Document.ID] = candidate
				}
				candidate.lexicalScore = hit.Score
				candidate.lexicalRank = rank
				candidate.fusionScore += reciprocalRank(rank)
				candidate.detectionModes = append(candidate.detectionModes, "lexical")
			}
		}
	}

	return aggregateConversations(documentRanks, options.Limit, options.Excerpts), nil
}

func (engine *Engine) queryVector(contextValue context.Context, config domain.SearchIndexConfig, query string) ([]float32, error) {
	normalizedQuery := strings.Join(strings.Fields(strings.ToLower(query)), " ")
	queryBaseURL := config.BaseURL
	if strings.TrimSpace(engine.QueryBaseURL) != "" {
		queryBaseURL = strings.TrimSpace(engine.QueryBaseURL)
	}
	queryHash := normalize.Hash(config.Model, queryBaseURL, config.QueryPrefix, normalizedQuery)
	vector, exists, cacheError := engine.Store.LoadQueryEmbedding(contextValue, config.ID, queryHash)
	if cacheError != nil {
		return nil, cacheError
	}
	if exists {
		if len(vector) != config.Dimensions {
			return nil, fmt.Errorf("cached query vector has %d dimensions; expected %d", len(vector), config.Dimensions)
		}
		engine.LastQueryCacheHit = true
		return vector, nil
	}
	vectors, embedError := engine.QueryEmbedder.Embed(contextValue, []string{query})
	if embedError != nil {
		return nil, fmt.Errorf("embed conversation search query: %w", embedError)
	}
	if len(vectors) != 1 || len(vectors[0]) != config.Dimensions {
		return nil, fmt.Errorf("query embedding returned invalid dimensions; expected %d", config.Dimensions)
	}
	normalizeInPlace(vectors[0])
	if saveError := engine.Store.SaveQueryEmbedding(contextValue, config.ID, queryHash, query, vectors[0]); saveError != nil {
		return nil, saveError
	}
	return vectors[0], nil
}

func buildFTSQuery(query string) string {
	tokens := ftsTokenPattern.FindAllString(strings.ToLower(query), -1)
	quoted := make([]string, 0, len(tokens))
	seen := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		if _, exists := seen[token]; exists {
			continue
		}
		seen[token] = struct{}{}
		quoted = append(quoted, `"`+strings.ReplaceAll(token, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " AND ")
}

func searchCandidateLimit(resultLimit int) int {
	if resultLimit <= 0 {
		return 0
	}
	limit := resultLimit * 20
	if limit < 200 {
		limit = 200
	}
	return limit
}

func reciprocalRank(rank int) float64 {
	return 1 / float64(60+rank)
}

func aggregateConversations(documentRanks map[string]*rankedDocument, limit int, excerptLimit int) []domain.ConversationSearchResult {
	byConversation := make(map[string][]rankedDocument)
	for _, documentRank := range documentRanks {
		byConversation[documentRank.document.ConversationID] = append(
			byConversation[documentRank.document.ConversationID],
			*documentRank,
		)
	}
	results := make([]domain.ConversationSearchResult, 0, len(byConversation))
	for _, candidates := range byConversation {
		sort.Slice(candidates, func(leftIndex int, rightIndex int) bool {
			if candidates[leftIndex].fusionScore == candidates[rightIndex].fusionScore {
				return candidates[leftIndex].semanticScore > candidates[rightIndex].semanticScore
			}
			return candidates[leftIndex].fusionScore > candidates[rightIndex].fusionScore
		})
		first := candidates[0]
		result := domain.ConversationSearchResult{
			ConversationID:    first.document.ConversationID,
			ConversationTitle: first.document.ConversationTitle,
			Archived:          first.document.IsArchived,
		}
		for candidateIndex, candidate := range candidates {
			if candidateIndex < 3 {
				result.Score += candidate.fusionScore / float64(candidateIndex+1)
			}
			if candidate.semanticScore > result.SemanticScore {
				result.SemanticScore = candidate.semanticScore
			}
			if candidate.lexicalRank > 0 && (result.LexicalScore == 0 || candidate.lexicalScore < result.LexicalScore) {
				result.LexicalScore = candidate.lexicalScore
			}
			if len(result.Excerpts) < excerptLimit {
				result.Excerpts = append(result.Excerpts, domain.ConversationSearchExcerpt{
					MessageID:        candidate.document.AnchorMessageID,
					Role:             candidate.document.Role,
					Text:             candidate.document.SourceText,
					SemanticScore:    candidate.semanticScore,
					LexicalScore:     candidate.lexicalScore,
					DetectionMethods: append([]string(nil), candidate.detectionModes...),
				})
			}
		}
		results = append(results, result)
	}
	sort.Slice(results, func(leftIndex int, rightIndex int) bool {
		if results[leftIndex].Score == results[rightIndex].Score {
			return results[leftIndex].ConversationID < results[rightIndex].ConversationID
		}
		return results[leftIndex].Score > results[rightIndex].Score
	})
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results
}

func normalizeInPlace(vector []float32) {
	var squaredLength float64
	for _, value := range vector {
		squaredLength += float64(value) * float64(value)
	}
	if squaredLength == 0 {
		return
	}
	inverseLength := float32(1 / math.Sqrt(squaredLength))
	for vectorIndex := range vector {
		vector[vectorIndex] *= inverseLength
	}
}
