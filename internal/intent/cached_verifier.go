package intent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/normalize"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/store"
)

type CachedVerifier struct {
	Store       *store.Store
	Inner       Verifier
	BatchSize   int
	CacheHits   int
	CacheMisses int
}

func (verifier *CachedVerifier) Identity() string {
	identified, exists := verifier.Inner.(IdentifiedVerifier)
	if !exists {
		return fmt.Sprintf("%T", verifier.Inner)
	}
	return identified.Identity()
}

func (verifier *CachedVerifier) Verify(contextValue context.Context, inputs []VerificationInput) (map[string]VerificationResult, error) {
	if verifier.Store == nil {
		return nil, fmt.Errorf("cached verifier requires a store")
	}
	if verifier.Inner == nil {
		return nil, fmt.Errorf("cached verifier requires an inner verifier")
	}
	batchSize := verifier.BatchSize
	if batchSize <= 0 {
		batchSize = 8
	}
	identity := verifier.Identity()
	results := make(map[string]VerificationResult, len(inputs))

	for startingIndex := 0; startingIndex < len(inputs); startingIndex += batchSize {
		endingIndex := startingIndex + batchSize
		if endingIndex > len(inputs) {
			endingIndex = len(inputs)
		}
		batch := inputs[startingIndex:endingIndex]
		missingInputs := make([]VerificationInput, 0, len(batch))
		cacheKeys := make(map[string]string, len(batch))
		inputHashes := make(map[string]string, len(batch))

		for _, input := range batch {
			encodedInput, marshalError := json.Marshal(input)
			if marshalError != nil {
				return nil, fmt.Errorf("encode verification cache input: %w", marshalError)
			}
			inputHash := normalize.Hash(string(encodedInput))
			cacheKey := normalize.Hash(VerificationPromptVersion, identity, inputHash)
			cachedJSON, exists, cacheError := verifier.Store.LoadVerificationCache(contextValue, cacheKey)
			if cacheError != nil {
				return nil, cacheError
			}
			if exists {
				var cachedResult VerificationResult
				if decodeError := json.Unmarshal([]byte(cachedJSON), &cachedResult); decodeError == nil {
					results[input.MessageID] = cachedResult
					verifier.CacheHits++
					continue
				}
			}
			missingInputs = append(missingInputs, input)
			cacheKeys[input.MessageID] = cacheKey
			inputHashes[input.MessageID] = inputHash
			verifier.CacheMisses++
		}

		if len(missingInputs) == 0 {
			continue
		}
		batchResults, verifyError := verifier.Inner.Verify(contextValue, missingInputs)
		if verifyError != nil {
			return nil, verifyError
		}
		for _, input := range missingInputs {
			result, exists := batchResults[input.MessageID]
			if !exists {
				return nil, fmt.Errorf("verifier omitted message ID %s", input.MessageID)
			}
			encodedResult, marshalError := json.Marshal(result)
			if marshalError != nil {
				return nil, fmt.Errorf("encode verification cache result: %w", marshalError)
			}
			if cacheError := verifier.Store.SaveVerificationCache(
				contextValue,
				cacheKeys[input.MessageID],
				identity,
				input.MessageID,
				inputHashes[input.MessageID],
				string(encodedResult),
			); cacheError != nil {
				return nil, cacheError
			}
			results[input.MessageID] = result
		}
	}
	return results, nil
}
