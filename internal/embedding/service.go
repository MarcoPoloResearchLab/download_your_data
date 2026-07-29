package embedding

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/domain"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/inference"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/normalize"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/store"
)

type ServiceOptions struct {
	Provider        string
	Model           string
	Dimensions      int
	BaseURL         inference.BaseURL
	InputPrefix     string
	BatchSize       int
	MaximumMessages int
	RefreshStale    bool
}

type ServiceProgress struct {
	EmbeddedThisRun int
	TotalEmbedded   int64
	LastMessageID   string
}

type Service struct {
	Store    *store.Store
	Embedder Embedder
	Progress func(ServiceProgress)
}

func (service *Service) Run(contextValue context.Context, options ServiceOptions) (domain.EmbeddingConfig, int, error) {
	if service.Store == nil {
		return domain.EmbeddingConfig{}, 0, fmt.Errorf("embedding service requires a store")
	}
	if service.Embedder == nil {
		return domain.EmbeddingConfig{}, 0, fmt.Errorf("embedding service requires an embedder")
	}
	if options.Dimensions <= 0 {
		return domain.EmbeddingConfig{}, 0, fmt.Errorf("dimensions must be positive")
	}
	if strings.TrimSpace(options.Model) == "" {
		return domain.EmbeddingConfig{}, 0, fmt.Errorf("embedding model must not be empty")
	}
	if options.BatchSize <= 0 {
		options.BatchSize = product.DefaultInferenceBatchSize
	}
	provider := strings.TrimSpace(options.Provider)
	if provider == "" {
		provider = inference.DefaultEmbeddingProvider
	}
	baseURL := options.BaseURL.String()
	if baseURL == "" {
		return domain.EmbeddingConfig{}, 0, fmt.Errorf("inference base URL is required")
	}
	preprocessingVersion := preprocessingIdentity(options.InputPrefix)
	vectorIdentity := normalize.Hash(provider, options.Model, fmt.Sprintf("%d", options.Dimensions), baseURL, preprocessingVersion, domain.ContextVersion)
	vectorPath := filepath.Join("vectors", vectorIdentity[:20]+".f32")
	config, configError := service.Store.GetOrCreateEmbeddingConfig(contextValue, domain.EmbeddingConfig{
		Provider:             provider,
		Model:                options.Model,
		Dimensions:           options.Dimensions,
		BaseURL:              baseURL,
		InputPrefix:          options.InputPrefix,
		VectorPath:           vectorPath,
		PreprocessingVersion: preprocessingVersion,
		ContextVersion:       domain.ContextVersion,
	})
	if configError != nil {
		return config, 0, configError
	}

	maximumVectorRow, rowError := service.Store.MaximumVectorRow(contextValue, config.ID)
	if rowError != nil {
		return config, 0, rowError
	}
	privateVectorFile, pathError := service.Store.ResolveVectorFile(config)
	if pathError != nil {
		return config, 0, pathError
	}
	vectorFile, vectorError := OpenVectorFile(privateVectorFile, config.Dimensions, maximumVectorRow)
	if vectorError != nil {
		return config, 0, vectorError
	}
	defer vectorFile.Close()

	embeddedThisRun := 0
	afterMessageID := ""
	configurationBuilding := false
	configurationComplete := false
	for {
		remainingLimit := options.BatchSize
		if options.MaximumMessages > 0 {
			remainingMessages := options.MaximumMessages - embeddedThisRun
			if remainingMessages <= 0 {
				break
			}
			if remainingMessages < remainingLimit {
				remainingLimit = remainingMessages
			}
		}

		candidates, candidateError := service.Store.ListEmbeddingCandidates(
			contextValue,
			config.ID,
			remainingLimit,
			options.RefreshStale,
			afterMessageID,
		)
		if candidateError != nil {
			return config, embeddedThisRun, candidateError
		}
		if len(candidates) == 0 {
			configurationComplete = true
			break
		}
		if options.RefreshStale {
			afterMessageID = candidates[len(candidates)-1].MessageID
		}

		selectedCandidates := make([]domain.EmbeddingCandidate, 0, len(candidates))
		searchTexts := make([]string, 0, len(candidates))
		searchHashes := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			searchText := normalize.BuildSearchText(
				candidate.ConversationTitle,
				candidate.OriginalText,
				candidate.ParentText,
				candidate.FollowingText,
			)
			searchHash := normalize.Hash(config.PreprocessingVersion, config.ContextVersion, searchText)
			if candidate.ExistingSearchHash == searchHash {
				continue
			}
			selectedCandidates = append(selectedCandidates, candidate)
			searchTexts = append(searchTexts, searchText)
			searchHashes = append(searchHashes, searchHash)
		}
		if len(selectedCandidates) == 0 {
			continue
		}
		if !configurationBuilding {
			if statusError := service.Store.MarkEmbeddingConfigBuilding(contextValue, config.ID); statusError != nil {
				return config, embeddedThisRun, statusError
			}
			configurationBuilding = true
		}

		vectors, embeddingError := service.Embedder.Embed(contextValue, searchTexts)
		if embeddingError != nil {
			return config, embeddedThisRun, embeddingError
		}
		vectorRows, appendError := vectorFile.Append(vectors)
		if appendError != nil {
			return config, embeddedThisRun, appendError
		}

		records := make([]domain.EmbeddingRecord, len(selectedCandidates))
		createdAtMillis := time.Now().UTC().UnixMilli()
		for candidateIndex, candidate := range selectedCandidates {
			records[candidateIndex] = domain.EmbeddingRecord{
				MessageID:       candidate.MessageID,
				VectorRow:       vectorRows[candidateIndex],
				Dimensions:      config.Dimensions,
				SearchTextHash:  searchHashes[candidateIndex],
				CreatedAtMillis: createdAtMillis,
			}
		}
		if saveError := service.Store.SaveEmbeddingRecords(contextValue, config.ID, records); saveError != nil {
			return config, embeddedThisRun, saveError
		}
		embeddedThisRun += len(selectedCandidates)

		if service.Progress != nil {
			totalEmbedded, countError := service.Store.CountEmbeddings(contextValue, config.ID)
			if countError != nil {
				return config, embeddedThisRun, countError
			}
			service.Progress(ServiceProgress{
				EmbeddedThisRun: embeddedThisRun,
				TotalEmbedded:   totalEmbedded,
				LastMessageID:   selectedCandidates[len(selectedCandidates)-1].MessageID,
			})
		}
	}
	if configurationComplete {
		if statusError := service.Store.MarkEmbeddingConfigReady(contextValue, config.ID); statusError != nil {
			return config, embeddedThisRun, statusError
		}
		config.Status = "ready"
	}
	return config, embeddedThisRun, nil
}

func preprocessingIdentity(inputPrefix string) string {
	return domain.PreprocessingVersion + ":" + normalize.Hash(inputPrefix)[:16]
}
