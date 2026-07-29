package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/privatepath"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/enrichment"
)

const (
	enrichmentOutcomesDirectory = "enrichment-outcomes"
	enrichmentOutcomeContract   = "netflix-enrichment-outcome-v1"
)

type enrichmentCheckpoint struct {
	titleIdentity netflix.TitleIdentity
	match         netflix.TMDBMatch
	metadata      *netflix.TitleMetadata
	cacheHit      bool
	bytes         int64
}

type persistedEnrichmentOutcome struct {
	Contract             string    `json:"contract"`
	GenerationID         string    `json:"generation_id"`
	SourceGenerationID   string    `json:"source_generation_id"`
	Locale               string    `json:"locale"`
	ClientIdentity       string    `json:"client_identity"`
	MatcherIdentity      string    `json:"matcher_identity"`
	CacheIdentity        string    `json:"cache_identity"`
	TitleIdentity        string    `json:"title_identity"`
	DerivedTitle         string    `json:"derived_title"`
	TitleIdentityVersion string    `json:"title_identity_version"`
	Match                Match     `json:"match"`
	Metadata             *Metadata `json:"metadata,omitempty"`
	CacheHit             bool      `json:"cache_hit"`
}

func checkpointFromResult(
	result enrichment.Result,
	bytes int64,
) enrichmentCheckpoint {
	checkpoint := enrichmentCheckpoint{
		titleIdentity: result.TitleIdentity(),
		match:         result.Match(),
		cacheHit:      result.CacheHit(),
		bytes:         bytes,
	}
	if metadata, hasMetadata := result.Metadata(); hasMetadata {
		checkpoint.metadata = &metadata
	}
	return checkpoint
}

func writeEnrichmentCheckpoint(
	root privatepath.Root,
	generation generationState,
	result enrichment.Result,
	existingCheckpointBytes int64,
) (enrichmentCheckpoint, error) {
	identity := result.TitleIdentity()
	if identity.Key() == "" ||
		identity.SearchTitle() == "" ||
		identity.Version() != netflix.TitleIdentityVersion ||
		existingCheckpointBytes < 0 ||
		existingCheckpointBytes > product.MaxNetflixWorkingBytes {
		return enrichmentCheckpoint{}, newLibraryError(
			ErrorInvalidResponse,
			generation.ID,
			0,
			errors.New("enrichment result has an invalid title identity"),
		)
	}
	file, fileError := enrichmentOutcomeFile(root, generation.ID, identity.Key())
	if fileError != nil {
		return enrichmentCheckpoint{}, fileError
	}
	if exists, existsError := privateFileExists(file); existsError != nil {
		return enrichmentCheckpoint{}, existsError
	} else if exists {
		return enrichmentCheckpoint{}, newLibraryError(
			ErrorInvalidPersistence,
			generation.ID,
			0,
			errors.New("enrichment outcome checkpoint already exists"),
		)
	}
	matchValue := matchSnapshot(result.Match())
	payload := persistedEnrichmentOutcome{
		Contract:             enrichmentOutcomeContract,
		GenerationID:         generation.ID,
		SourceGenerationID:   generation.SourceGenerationID,
		Locale:               generation.Locale,
		ClientIdentity:       generation.TMDBClientIdentity,
		MatcherIdentity:      generation.TMDBMatcherIdentity,
		CacheIdentity:        generation.TMDBCacheIdentity,
		TitleIdentity:        identity.Key(),
		DerivedTitle:         identity.SearchTitle(),
		TitleIdentityVersion: identity.Version(),
		Match:                matchValue,
		CacheHit:             result.CacheHit(),
	}
	if metadata, hasMetadata := result.Metadata(); hasMetadata {
		metadataValue := metadataSnapshot(metadata)
		payload.Metadata = &metadataValue
	}
	if validationError := validateOutcomePayload(
		payload,
		generation,
		identity,
	); validationError != nil {
		return enrichmentCheckpoint{}, validationError
	}
	encoded, encodeError := json.Marshal(payload)
	if encodeError != nil {
		return enrichmentCheckpoint{}, newLibraryError(
			ErrorPersistenceFailed,
			generation.ID,
			0,
			encodeError,
		)
	}
	encoded = append(encoded, '\n')
	if len(encoded) == 0 ||
		int64(len(encoded)) > product.MaxNetflixEnrichmentOutcomeBytes ||
		existingCheckpointBytes+int64(len(encoded)) >
			product.MaxNetflixWorkingBytes {
		return enrichmentCheckpoint{}, newLibraryError(
			ErrorLimitExceeded,
			generation.ID,
			0,
			errors.New("enrichment outcome exceeds its byte limit"),
		)
	}
	if writeError := writePrivateFileAtomic(file, encoded); writeError != nil {
		return enrichmentCheckpoint{}, writeError
	}
	return checkpointFromResult(result, int64(len(encoded))), nil
}

func readEnrichmentCheckpoint(
	root privatepath.Root,
	generation generationState,
	identity netflix.TitleIdentity,
) (enrichmentCheckpoint, bool, error) {
	file, fileError := enrichmentOutcomeFile(root, generation.ID, identity.Key())
	if fileError != nil {
		return enrichmentCheckpoint{}, false, fileError
	}
	if cleanupError := removeAtomicSibling(file); cleanupError != nil {
		return enrichmentCheckpoint{}, false, cleanupError
	}
	exists, existsError := privateFileExists(file)
	if existsError != nil || !exists {
		return enrichmentCheckpoint{}, false, existsError
	}
	encoded, readError := readPrivateFileBounded(
		file,
		int(product.MaxNetflixEnrichmentOutcomeBytes),
	)
	if readError != nil {
		return enrichmentCheckpoint{}, false, readError
	}
	var payload persistedEnrichmentOutcome
	if decodeError := decodeStrictJSON(encoded, &payload); decodeError != nil {
		return enrichmentCheckpoint{}, false, newLibraryError(
			ErrorInvalidPersistence,
			generation.ID,
			0,
			decodeError,
		)
	}
	if validationError := validateOutcomePayload(
		payload,
		generation,
		identity,
	); validationError != nil {
		return enrichmentCheckpoint{}, false, validationError
	}
	match, matchError := payload.Match.domain()
	if matchError != nil {
		return enrichmentCheckpoint{}, false, newLibraryError(
			ErrorInvalidPersistence,
			generation.ID,
			0,
			matchError,
		)
	}
	checkpoint := enrichmentCheckpoint{
		titleIdentity: identity,
		match:         match,
		cacheHit:      payload.CacheHit,
		bytes:         int64(len(encoded)),
	}
	if payload.Metadata != nil {
		metadata, metadataError := payload.Metadata.domain()
		if metadataError != nil {
			return enrichmentCheckpoint{}, false, newLibraryError(
				ErrorInvalidPersistence,
				generation.ID,
				0,
				metadataError,
			)
		}
		checkpoint.metadata = &metadata
	}
	return checkpoint, true, nil
}

func validateOutcomePayload(
	payload persistedEnrichmentOutcome,
	generation generationState,
	identity netflix.TitleIdentity,
) error {
	if payload.Contract != enrichmentOutcomeContract ||
		payload.GenerationID != generation.ID ||
		payload.SourceGenerationID != generation.SourceGenerationID ||
		payload.Locale != generation.Locale ||
		payload.ClientIdentity != generation.TMDBClientIdentity ||
		payload.MatcherIdentity != generation.TMDBMatcherIdentity ||
		payload.CacheIdentity != generation.TMDBCacheIdentity ||
		payload.TitleIdentity != identity.Key() ||
		payload.DerivedTitle != identity.SearchTitle() ||
		payload.TitleIdentityVersion != identity.Version() ||
		payload.Match.MatcherIdentity != generation.TMDBMatcherIdentity {
		return newLibraryError(
			ErrorInvalidPersistence,
			generation.ID,
			0,
			errors.New("enrichment outcome identity is inconsistent"),
		)
	}
	match, matchError := payload.Match.domain()
	if matchError != nil {
		return newLibraryError(
			ErrorInvalidPersistence,
			generation.ID,
			0,
			matchError,
		)
	}
	if match.Status() == netflix.MatchStatusMatched {
		if payload.Metadata == nil {
			return newLibraryError(
				ErrorInvalidPersistence,
				generation.ID,
				0,
				errors.New("matched enrichment outcome is missing metadata"),
			)
		}
		metadata, metadataError := payload.Metadata.domain()
		if metadataError != nil ||
			metadata.TMDBID() != match.TMDBID() ||
			metadata.MediaType() != match.MediaType() {
			return newLibraryError(
				ErrorInvalidPersistence,
				generation.ID,
				0,
				errors.New("matched enrichment outcome metadata is inconsistent"),
			)
		}
	} else if payload.Metadata != nil {
		return newLibraryError(
			ErrorInvalidPersistence,
			generation.ID,
			0,
			errors.New("non-matched enrichment outcome contains metadata"),
		)
	}
	return nil
}

func validateEnrichmentCheckpointSet(
	root privatepath.Root,
	generation generationState,
	checkpoints map[string]enrichmentCheckpoint,
) error {
	relativeDirectory := filepath.Join(
		filepath.FromSlash(generationsRelativeDirectory),
		generation.ID,
		enrichmentOutcomesDirectory,
	)
	directory, directoryError := root.EnsureDirectory(relativeDirectory)
	if directoryError != nil {
		return newLibraryError(
			ErrorInvalidPersistence,
			generation.ID,
			0,
			directoryError,
		)
	}
	entries, readError := os.ReadDir(directory.Path())
	if readError != nil {
		return newLibraryError(
			ErrorPersistenceFailed,
			generation.ID,
			0,
			readError,
		)
	}
	expectedFiles := make(map[string]struct{}, len(checkpoints))
	for titleIdentity := range checkpoints {
		expectedFiles[titleIdentity+".json"] = struct{}{}
	}
	if len(entries) != len(expectedFiles) {
		return newLibraryError(
			ErrorInvalidPersistence,
			generation.ID,
			0,
			errors.New("enrichment outcome directory contains orphaned state"),
		)
	}
	for _, entry := range entries {
		if _, expected := expectedFiles[entry.Name()]; !expected {
			return newLibraryError(
				ErrorInvalidPersistence,
				generation.ID,
				0,
				errors.New("enrichment outcome directory contains an unknown file"),
			)
		}
		pathInfo, infoError := entry.Info()
		if infoError != nil {
			return newLibraryError(
				ErrorPersistenceFailed,
				generation.ID,
				0,
				infoError,
			)
		}
		if !pathInfo.Mode().IsRegular() || pathInfo.Mode().Perm() != 0o600 {
			return newLibraryError(
				ErrorInvalidPersistence,
				generation.ID,
				0,
				errors.New("enrichment outcome must be an owner-only regular file"),
			)
		}
	}
	return nil
}

func enrichmentOutcomeFile(
	root privatepath.Root,
	generationID string,
	titleIdentity string,
) (privatepath.File, error) {
	if !validGenerationID(generationID) || !validSHA256(titleIdentity) {
		return privatepath.File{}, newLibraryError(
			ErrorInvalidRequest,
			generationID,
			0,
			errors.New("current generation and title identities are required"),
		)
	}
	relativeDirectory := filepath.Join(
		filepath.FromSlash(generationsRelativeDirectory),
		generationID,
		enrichmentOutcomesDirectory,
	)
	if _, directoryError := root.EnsureDirectory(relativeDirectory); directoryError != nil {
		return privatepath.File{}, newLibraryError(
			ErrorPersistenceFailed,
			generationID,
			0,
			directoryError,
		)
	}
	file, fileError := root.File(filepath.Join(
		relativeDirectory,
		titleIdentity+".json",
	))
	if fileError != nil {
		return privatepath.File{}, newLibraryError(
			ErrorInvalidPersistence,
			generationID,
			0,
			fileError,
		)
	}
	return file, nil
}

func removeEnrichmentCheckpoints(
	root privatepath.Root,
	generationID string,
) error {
	if !validGenerationID(generationID) {
		return newLibraryError(ErrorInvalidRequest, generationID, 0, nil)
	}
	expectedGenerationDirectory := filepath.Join(
		root.Path(),
		filepath.FromSlash(generationsRelativeDirectory),
		generationID,
	)
	directoryPath := filepath.Join(
		expectedGenerationDirectory,
		enrichmentOutcomesDirectory,
	)
	if filepath.Dir(directoryPath) != expectedGenerationDirectory {
		return newLibraryError(
			ErrorInvalidPersistence,
			generationID,
			0,
			errors.New("enrichment checkpoint deletion path is outside the current contract"),
		)
	}
	if removeError := os.RemoveAll(directoryPath); removeError != nil {
		return newLibraryError(
			ErrorPersistenceFailed,
			generationID,
			0,
			fmt.Errorf("remove enrichment checkpoints: %w", removeError),
		)
	}
	return syncExistingParent(expectedGenerationDirectory)
}
