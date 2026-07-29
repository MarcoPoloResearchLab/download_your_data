package intent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/domain"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/embedding"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/normalize"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/store"
)

type AnalyzeOptions struct {
	Config          DefinitionConfig
	Since           time.Time
	Until           time.Time
	IncludeArchived bool
	Semantic        bool
	EmbeddingConfig domain.EmbeddingConfig
	Embedder        embedding.Embedder
	Verifier        Verifier
}

type AnalyzeOutput struct {
	Results          []domain.DefinitionResult
	Review           []domain.DefinitionResult
	MessagesExamined int
	MessagesEmbedded int
	Candidates       int
	Verified         int
}

type Analyzer struct {
	Store *store.Store
}

type analysisCandidate struct {
	Message          domain.SearchMessage
	Lexical          LexicalMatch
	PositiveScore    float64
	NegativeScore    float64
	Margin           float64
	Methods          []string
	InitialCandidate bool
	InitialReview    bool
}

func (analyzer *Analyzer) Analyze(contextValue context.Context, options AnalyzeOptions) (AnalyzeOutput, error) {
	output := AnalyzeOutput{}
	if analyzer.Store == nil {
		return output, fmt.Errorf("definition analyzer requires a store")
	}
	if !options.Until.After(options.Since) {
		return output, fmt.Errorf("until time must be after since time")
	}
	compiledRules, compileError := CompileRules(options.Config.LexicalRules)
	if compileError != nil {
		return output, compileError
	}

	configID := int64(0)
	if options.Semantic {
		if options.EmbeddingConfig.ID == 0 {
			return output, fmt.Errorf("semantic analysis requires an embedding configuration")
		}
		configID = options.EmbeddingConfig.ID
	}
	messages, messageError := analyzer.Store.ListSearchMessages(
		contextValue,
		configID,
		options.Since.UTC().UnixMilli(),
		options.Until.UTC().UnixMilli(),
		options.IncludeArchived,
	)
	if messageError != nil {
		return output, messageError
	}
	output.MessagesExamined = len(messages)

	var vectorFile *embedding.VectorFile
	var positiveVectors [][]float32
	var negativeVectors [][]float32
	if options.Semantic {
		maximumVectorRow, rowError := analyzer.Store.MaximumVectorRow(contextValue, options.EmbeddingConfig.ID)
		if rowError != nil {
			return output, rowError
		}
		privateVectorFile, pathError := analyzer.Store.ResolveVectorFile(options.EmbeddingConfig)
		if pathError != nil {
			return output, pathError
		}
		openedVectorFile, vectorError := embedding.OpenVectorFile(
			privateVectorFile,
			options.EmbeddingConfig.Dimensions,
			maximumVectorRow,
		)
		if vectorError != nil {
			return output, vectorError
		}
		vectorFile = openedVectorFile
		defer vectorFile.Close()

		var prototypeError error
		positiveVectors, prototypeError = analyzer.loadPrototypeVectors(
			contextValue,
			options.EmbeddingConfig,
			options.Config.Name,
			"positive",
			options.Config.PositiveExamples,
			options.Embedder,
		)
		if prototypeError != nil {
			return output, prototypeError
		}
		negativeVectors, prototypeError = analyzer.loadPrototypeVectors(
			contextValue,
			options.EmbeddingConfig,
			options.Config.Name,
			"negative",
			options.Config.NegativeExamples,
			options.Embedder,
		)
		if prototypeError != nil {
			return output, prototypeError
		}
	}

	candidates := make([]analysisCandidate, 0)
	for _, message := range messages {
		lexicalMatch := EvaluateLexical(message.OriginalText, compiledRules)
		candidate := analysisCandidate{
			Message: message,
			Lexical: lexicalMatch,
			Methods: append([]string{}, lexicalMatch.Methods...),
		}

		if options.Semantic && message.VectorRow != nil {
			messageVector, readError := vectorFile.Read(*message.VectorRow)
			if readError != nil {
				return output, fmt.Errorf("read vector for message %s: %w", message.MessageID, readError)
			}
			candidate.PositiveScore, readError = maximumSimilarity(messageVector, positiveVectors)
			if readError != nil {
				return output, readError
			}
			candidate.NegativeScore, readError = maximumSimilarity(messageVector, negativeVectors)
			if readError != nil {
				return output, readError
			}
			candidate.Margin = candidate.PositiveScore - candidate.NegativeScore
			candidate.Methods = append(candidate.Methods, "semantic:prototype")
			output.MessagesEmbedded++
		}

		highLexicalMatch := lexicalMatch.Score >= 0.82
		semanticMatch := options.Semantic && message.VectorRow != nil &&
			candidate.PositiveScore >= options.Config.SemanticThreshold &&
			candidate.Margin >= options.Config.SemanticMargin
		candidate.InitialCandidate = highLexicalMatch || semanticMatch
		candidate.InitialReview = !candidate.InitialCandidate && (lexicalMatch.Score >= options.Config.LexicalReviewScore ||
			(options.Semantic && message.VectorRow != nil && candidate.PositiveScore >= options.Config.ReviewThreshold))

		if candidate.InitialCandidate || candidate.InitialReview {
			candidates = append(candidates, candidate)
		}
	}
	output.Candidates = len(candidates)

	verificationResults := make(map[string]VerificationResult)
	if options.Verifier != nil && len(candidates) > 0 {
		verificationInputs := make([]VerificationInput, len(candidates))
		for candidateIndex, candidate := range candidates {
			verificationInputs[candidateIndex] = VerificationInput{
				MessageID:         candidate.Message.MessageID,
				ConversationTitle: normalize.TruncateUTF8(candidate.Message.ConversationTitle, 300),
				PreviousMessage:   normalize.TruncateUTF8(candidate.Message.ParentText, 1800),
				UserMessage:       normalize.TruncateUTF8(candidate.Message.OriginalText, 3000),
				FollowingMessage:  normalize.TruncateUTF8(candidate.Message.FollowingText, 1800),
			}
		}
		var verificationError error
		verificationResults, verificationError = options.Verifier.Verify(contextValue, verificationInputs)
		if verificationError != nil {
			return output, verificationError
		}
		output.Verified = len(verificationResults)
	}

	for _, candidate := range candidates {
		result := buildDefinitionResult(candidate)
		verificationResult, wasVerified := verificationResults[candidate.Message.MessageID]
		if wasVerified {
			result.DetectionMethods = append(result.DetectionMethods, "llm:structured_verifier")
			result.Confidence = clampScore(verificationResult.Confidence)
			result.VerifierExplanation = strings.TrimSpace(verificationResult.Explanation)
			if len(verificationResult.Terms) > 0 {
				result.Term = cleanTerm(verificationResult.Terms[0])
			}
			if strings.TrimSpace(verificationResult.Category) != "" {
				result.Category = strings.TrimSpace(verificationResult.Category)
			}
			if verificationResult.IsDefinitionRequest {
				result.NeedsReview = false
				output.Results = append(output.Results, result)
			} else if candidate.InitialCandidate {
				result.NeedsReview = true
				output.Review = append(output.Review, result)
			}
			continue
		}

		if candidate.InitialCandidate {
			result.NeedsReview = false
			output.Results = append(output.Results, result)
		} else {
			result.NeedsReview = true
			output.Review = append(output.Review, result)
		}
	}

	sortDefinitionResults(output.Results)
	sortDefinitionResults(output.Review)
	return output, nil
}

func (analyzer *Analyzer) loadPrototypeVectors(
	contextValue context.Context,
	config domain.EmbeddingConfig,
	intentName string,
	label string,
	examples []string,
	embedder embedding.Embedder,
) ([][]float32, error) {
	vectors := make([][]float32, len(examples))
	missingIndices := make([]int, 0)
	missingExamples := make([]string, 0)
	for exampleIndex, exampleText := range examples {
		storedVector, exists, loadError := analyzer.Store.LoadPrototypeVector(contextValue, config.ID, intentName, label, exampleText)
		if loadError != nil {
			return nil, loadError
		}
		if exists {
			vectors[exampleIndex] = storedVector
			continue
		}
		missingIndices = append(missingIndices, exampleIndex)
		missingExamples = append(missingExamples, exampleText)
	}

	if len(missingExamples) > 0 {
		if embedder == nil {
			return nil, fmt.Errorf("prototype embeddings are not cached and no embedder was supplied")
		}
		newVectors, embeddingError := embedder.Embed(contextValue, missingExamples)
		if embeddingError != nil {
			return nil, fmt.Errorf("embed %s intent prototypes: %w", label, embeddingError)
		}
		for missingPosition, exampleIndex := range missingIndices {
			vector := newVectors[missingPosition]
			if len(vector) != config.Dimensions {
				return nil, fmt.Errorf("prototype dimension mismatch: expected %d, received %d", config.Dimensions, len(vector))
			}
			vectors[exampleIndex] = vector
			if saveError := analyzer.Store.SavePrototypeVector(contextValue, config.ID, intentName, label, examples[exampleIndex], vector); saveError != nil {
				return nil, saveError
			}
		}
	}
	return vectors, nil
}

func maximumSimilarity(messageVector []float32, prototypes [][]float32) (float64, error) {
	if len(prototypes) == 0 {
		return 0, nil
	}
	maximumScore := -1.0
	for _, prototypeVector := range prototypes {
		similarity, similarityError := embedding.DotProduct(messageVector, prototypeVector)
		if similarityError != nil {
			return 0, similarityError
		}
		if similarity > maximumScore {
			maximumScore = similarity
		}
	}
	return maximumScore, nil
}

func buildDefinitionResult(candidate analysisCandidate) domain.DefinitionResult {
	dateISO := ""
	if candidate.Message.CreatedAtMillis != nil {
		dateISO = time.UnixMilli(*candidate.Message.CreatedAtMillis).UTC().Format(time.RFC3339)
	}
	confidence := candidate.Lexical.Score
	if candidate.PositiveScore > confidence {
		confidence = candidate.PositiveScore
	}
	resolvedTerm := candidate.Lexical.Term
	methods := append([]string{}, candidate.Methods...)
	if contextualTerm, resolved := resolveContextualTerm(
		resolvedTerm,
		candidate.Message.ParentText,
		candidate.Message.FollowingText,
		candidate.Message.ConversationTitle,
	); resolved {
		resolvedTerm = contextualTerm
		methods = append(methods, "context:term_resolution")
	}
	category := candidate.Lexical.Category
	if category == "" || candidate.Lexical.Term != resolvedTerm {
		category = inferCategory(resolvedTerm, candidate.Message.OriginalText)
	}
	methods = uniqueStrings(methods)
	return domain.DefinitionResult{
		DateISO:           dateISO,
		Term:              resolvedTerm,
		Category:          category,
		ExactUserMessage:  candidate.Message.OriginalText,
		ConversationTitle: candidate.Message.ConversationTitle,
		Archived:          candidate.Message.IsArchived,
		Confidence:        clampScore(confidence),
		DetectionMethods:  methods,
		ConversationID:    candidate.Message.ConversationID,
		MessageID:         candidate.Message.MessageID,
		SourceMessageID:   candidate.Message.SourceMessageID,
		SemanticPositive:  candidate.PositiveScore,
		SemanticNegative:  candidate.NegativeScore,
		SemanticMargin:    candidate.Margin,
		NeedsReview:       candidate.InitialReview,
	}
}

func sortDefinitionResults(results []domain.DefinitionResult) {
	sort.Slice(results, func(leftIndex int, rightIndex int) bool {
		if results[leftIndex].DateISO == results[rightIndex].DateISO {
			return results[leftIndex].MessageID < results[rightIndex].MessageID
		}
		return results[leftIndex].DateISO < results[rightIndex].DateISO
	})
}

func clampScore(score float64) float64 {
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

func uniqueStrings(values []string) []string {
	seenValues := make(map[string]struct{}, len(values))
	uniqueValues := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if _, exists := seenValues[value]; exists {
			continue
		}
		seenValues[value] = struct{}{}
		uniqueValues = append(uniqueValues, value)
	}
	return uniqueValues
}
