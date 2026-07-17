package retrieval

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/domain"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/embedding"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/inference"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/normalize"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/store"
)

type IndexOptions struct {
	Name             string
	Provider         string
	Model            string
	Dimensions       int
	BaseURL          string
	DocumentPrefix   string
	QueryPrefix      string
	BatchSize        int
	MaximumDocuments int
	Rebuild          bool
}

type IndexProgress struct {
	EmbeddedThisRun    int
	TotalDocuments     int64
	Eligible           int
	Excluded           int
	DocumentsPerSecond float64
	EstimatedRemaining time.Duration
}

type IndexService struct {
	Store             *store.Store
	Embedder          embedding.Embedder
	Progress          func(IndexProgress)
	EligibleDocuments int
	ExcludedMessages  int
}

func (service *IndexService) Run(contextValue context.Context, options IndexOptions) (domain.SearchIndexConfig, int, error) {
	if service.Store == nil {
		return domain.SearchIndexConfig{}, 0, fmt.Errorf("retrieval index service requires a store")
	}
	if service.Embedder == nil {
		return domain.SearchIndexConfig{}, 0, fmt.Errorf("retrieval index service requires an embedder")
	}
	if options.Dimensions <= 0 {
		return domain.SearchIndexConfig{}, 0, fmt.Errorf("search embedding dimensions must be positive")
	}
	if strings.TrimSpace(options.Model) == "" {
		return domain.SearchIndexConfig{}, 0, fmt.Errorf("search embedding model must not be empty")
	}
	if options.Name == "" {
		options.Name = DefaultIndexName
	}
	if options.Provider == "" {
		options.Provider = inference.DefaultEmbeddingProvider
	}
	if options.DocumentPrefix == "" {
		options.DocumentPrefix = DefaultDocumentPrefix
	}
	if options.QueryPrefix == "" {
		options.QueryPrefix = DefaultQueryPrefix
	}
	if options.BatchSize <= 0 {
		options.BatchSize = 64
	}
	options.BaseURL = inference.NormalizeBaseURL(options.BaseURL)

	documents, documentError := BuildDocuments(contextValue, service.Store)
	if documentError != nil {
		return domain.SearchIndexConfig{}, 0, documentError
	}
	service.EligibleDocuments = len(documents)
	statistics, statisticsError := service.Store.Stats(contextValue)
	if statisticsError != nil {
		return domain.SearchIndexConfig{}, 0, statisticsError
	}
	service.ExcludedMessages = int(statistics.Messages) - len(documents)
	if service.ExcludedMessages < 0 {
		service.ExcludedMessages = 0
	}
	runStartedAt := time.Now()
	preflightVectors, preflightError := service.Embedder.Embed(contextValue, []string{"chatindex retrieval readiness check"})
	if preflightError != nil {
		return domain.SearchIndexConfig{}, 0, fmt.Errorf("search embedding model preflight failed: %w", preflightError)
	}
	if len(preflightVectors) != 1 || len(preflightVectors[0]) != options.Dimensions {
		return domain.SearchIndexConfig{}, 0, fmt.Errorf(
			"search embedding preflight returned invalid dimensions: expected %d",
			options.Dimensions,
		)
	}
	if options.Rebuild {
		oldVectorPath, deleted, deleteError := service.Store.DeleteSearchIndexByName(contextValue, options.Name)
		if deleteError != nil {
			return domain.SearchIndexConfig{}, 0, deleteError
		}
		if deleted {
			resolvedOldVectorPath := oldVectorPath
			if !filepath.IsAbs(resolvedOldVectorPath) {
				resolvedOldVectorPath = filepath.Join(service.Store.DatabaseDirectory(), resolvedOldVectorPath)
			}
			if removeError := os.Remove(resolvedOldVectorPath); removeError != nil && !os.IsNotExist(removeError) {
				return domain.SearchIndexConfig{}, 0, fmt.Errorf("remove rebuilt search vector file: %w", removeError)
			}
		}
	}

	identity := normalize.Hash(
		options.Name,
		options.Provider,
		options.Model,
		fmt.Sprintf("%d", options.Dimensions),
		options.BaseURL,
		options.DocumentPrefix,
		options.QueryPrefix,
		DefaultBuilderVersion,
		DefaultCorpusPolicy,
	)
	config, configError := service.Store.GetOrCreateSearchIndex(contextValue, domain.SearchIndexConfig{
		Name:           options.Name,
		Provider:       options.Provider,
		Model:          options.Model,
		Dimensions:     options.Dimensions,
		BaseURL:        options.BaseURL,
		DocumentPrefix: options.DocumentPrefix,
		QueryPrefix:    options.QueryPrefix,
		VectorPath:     filepath.Join("search-vectors", identity[:20]+".f32"),
		BuilderVersion: DefaultBuilderVersion,
		CorpusPolicy:   DefaultCorpusPolicy,
	})
	if configError != nil {
		return config, 0, configError
	}

	states, stateError := service.Store.ListSearchDocumentStates(contextValue, config.ID)
	if stateError != nil {
		return config, 0, stateError
	}
	desiredIDs := make(map[string]struct{}, len(documents))
	pending := make([]domain.SearchDocument, 0)
	for documentIndex := range documents {
		documents[documentIndex].IndexID = config.ID
		desiredIDs[documents[documentIndex].ID] = struct{}{}
		state, exists := states[documents[documentIndex].ID]
		if !exists || state.VectorRow == nil || state.TextHash != documents[documentIndex].TextHash {
			pending = append(pending, documents[documentIndex])
		}
	}
	deletedIDs := make([]string, 0)
	for documentID := range states {
		if _, exists := desiredIDs[documentID]; !exists {
			deletedIDs = append(deletedIDs, documentID)
		}
	}

	if len(pending) > 0 || len(deletedIDs) > 0 {
		if statusError := service.Store.MarkSearchIndexBuilding(contextValue, config.ID); statusError != nil {
			return config, 0, statusError
		}
		config.Status = "building"
	}
	if deleteError := service.Store.DeleteSearchDocuments(contextValue, config.ID, deletedIDs); deleteError != nil {
		return config, 0, deleteError
	}

	maximumVectorRow, rowError := service.Store.MaximumSearchVectorRow(contextValue, config.ID)
	if rowError != nil {
		return config, 0, rowError
	}
	vectorFile, vectorError := embedding.OpenVectorFile(
		service.Store.ResolveSearchVectorPath(config),
		config.Dimensions,
		maximumVectorRow,
	)
	if vectorError != nil {
		return config, 0, vectorError
	}
	defer vectorFile.Close()

	embeddedThisRun := 0
	processedAll := true
	for startingIndex := 0; startingIndex < len(pending); startingIndex += options.BatchSize {
		if options.MaximumDocuments > 0 && embeddedThisRun >= options.MaximumDocuments {
			processedAll = false
			break
		}
		endingIndex := startingIndex + options.BatchSize
		if endingIndex > len(pending) {
			endingIndex = len(pending)
		}
		if options.MaximumDocuments > 0 {
			remaining := options.MaximumDocuments - embeddedThisRun
			if endingIndex-startingIndex > remaining {
				endingIndex = startingIndex + remaining
				processedAll = false
			}
		}
		batch := pending[startingIndex:endingIndex]
		texts := make([]string, len(batch))
		for batchIndex := range batch {
			texts[batchIndex] = batch[batchIndex].Text
		}
		vectors, embedError := service.Embedder.Embed(contextValue, texts)
		if embedError != nil {
			return config, embeddedThisRun, embedError
		}
		vectorRows, appendError := vectorFile.Append(vectors)
		if appendError != nil {
			return config, embeddedThisRun, appendError
		}
		embeddedAt := time.Now().UTC().UnixMilli()
		for batchIndex := range batch {
			vectorRow := vectorRows[batchIndex]
			batch[batchIndex].VectorRow = &vectorRow
			batch[batchIndex].Dimensions = config.Dimensions
			batch[batchIndex].EmbeddedAtMillis = &embeddedAt
		}
		if saveError := service.Store.SaveSearchDocuments(contextValue, config.ID, batch); saveError != nil {
			return config, embeddedThisRun, saveError
		}
		embeddedThisRun += len(batch)
		if service.Progress != nil {
			totalDocuments, countError := service.Store.CountSearchDocuments(contextValue, config.ID)
			if countError != nil {
				return config, embeddedThisRun, countError
			}
			elapsed := time.Since(runStartedAt)
			documentsPerSecond := 0.0
			estimatedRemaining := time.Duration(0)
			if elapsed > 0 && embeddedThisRun > 0 {
				documentsPerSecond = float64(embeddedThisRun) / elapsed.Seconds()
				remainingDocuments := len(documents) - int(totalDocuments)
				if remainingDocuments > 0 && documentsPerSecond > 0 {
					estimatedRemaining = time.Duration(float64(time.Second) * float64(remainingDocuments) / documentsPerSecond)
				}
			}
			service.Progress(IndexProgress{
				EmbeddedThisRun:    embeddedThisRun,
				TotalDocuments:     totalDocuments,
				Eligible:           len(documents),
				Excluded:           service.ExcludedMessages,
				DocumentsPerSecond: documentsPerSecond,
				EstimatedRemaining: estimatedRemaining,
			})
		}
		if endingIndex < startingIndex+options.BatchSize || endingIndex == len(pending) {
			if endingIndex < len(pending) {
				processedAll = false
			}
			break
		}
	}
	if processedAll {
		remainingStates, remainingError := service.Store.CountSearchDocuments(contextValue, config.ID)
		if remainingError != nil {
			return config, embeddedThisRun, remainingError
		}
		if remainingStates == int64(len(documents)) {
			if statusError := service.Store.MarkSearchIndexReady(contextValue, config.ID); statusError != nil {
				return config, embeddedThisRun, statusError
			}
			config.Status = "ready"
		}
	}
	return config, embeddedThisRun, nil
}
