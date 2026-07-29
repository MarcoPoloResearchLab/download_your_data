package library

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/enrichment"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
)

type enrichmentJobResult struct {
	identity netflix.TitleIdentity
	result   enrichment.Result
	err      error
}

func (workspace *Workspace) runTMDBEnrichment(
	ctx context.Context,
	generationID string,
) error {
	generation, generationError := workspace.buildingGeneration(generationID)
	if generationError != nil {
		return generationError
	}
	if generation.AnalysisLevel != AnalysisLevelTMDB ||
		generation.State != GenerationStateEnriching {
		return newLibraryError(
			ErrorInvalidState,
			generationID,
			0,
			errors.New("generation is not a current TMDB build"),
		)
	}
	if validSHA256(generation.RecordsSHA256) &&
		validSHA256(generation.AnalyticsSHA256) {
		if _, _, validationError := validateGenerationArtifacts(
			ctx,
			workspace.root,
			generation,
		); validationError != nil {
			return validationError
		}
		if removalError := removeEnrichmentCheckpoints(
			workspace.root,
			generationID,
		); removalError != nil {
			return removalError
		}
		if clearError := workspace.clearEnrichmentCheckpointBytes(
			generationID,
		); clearError != nil {
			return clearError
		}
		return workspace.activateGeneration(generationID)
	}

	sourceGeneration, sourceError := workspace.enrichmentSource(generation)
	if sourceError != nil {
		return sourceError
	}
	sourceRecords, _, artifactError := validateGenerationArtifacts(
		ctx,
		workspace.root,
		sourceGeneration,
	)
	if artifactError != nil {
		return newLibraryError(
			ErrorStaleSource,
			generationID,
			0,
			artifactError,
		)
	}
	identities, identityError := uniqueTitleIdentities(sourceRecords)
	if identityError != nil {
		return newLibraryError(
			ErrorStaleSource,
			generationID,
			0,
			identityError,
		)
	}
	if len(identities) != generation.UniqueTitleCount {
		return newLibraryError(
			ErrorStaleSource,
			generationID,
			0,
			errors.New("source unique-title count changed"),
		)
	}

	checkpoints := make(
		map[string]enrichmentCheckpoint,
		len(identities),
	)
	for _, identity := range identities {
		checkpoint, exists, checkpointError := readEnrichmentCheckpoint(
			workspace.root,
			generation,
			identity,
		)
		if checkpointError != nil {
			return checkpointError
		}
		if exists {
			checkpoints[identity.Key()] = checkpoint
		}
	}
	if checkpointSetError := validateEnrichmentCheckpointSet(
		workspace.root,
		generation,
		checkpoints,
	); checkpointSetError != nil {
		return checkpointSetError
	}
	if reconcileError := workspace.reconcileEnrichmentProgress(
		generationID,
		checkpoints,
	); reconcileError != nil {
		return reconcileError
	}

	remaining := make([]netflix.TitleIdentity, 0, len(identities)-len(checkpoints))
	for _, identity := range identities {
		if _, complete := checkpoints[identity.Key()]; !complete {
			remaining = append(remaining, identity)
		}
	}
	if len(remaining) > 0 {
		if workspace.metadataClient == nil {
			return newLibraryError(
				ErrorNotConfigured,
				generationID,
				0,
				errors.New("TMDB enrichment is not configured"),
			)
		}
		cache, cacheError := enrichment.OpenCache(workspace.cacheFile)
		if cacheError != nil {
			return newLibraryError(
				ErrorPersistenceFailed,
				generationID,
				0,
				cacheError,
			)
		}
		service, serviceError := enrichment.NewService(
			workspace.metadataClient,
			cache,
		)
		if serviceError != nil {
			_ = cache.Close()
			return newLibraryError(
				ErrorInvalidResponse,
				generationID,
				0,
				serviceError,
			)
		}
		enrichError := workspace.enrichRemainingTitles(
			ctx,
			generation,
			service,
			remaining,
			checkpoints,
		)
		closeError := cache.Close()
		if enrichError != nil {
			return errors.Join(enrichError, closeError)
		}
		if closeError != nil {
			return newLibraryError(
				ErrorPersistenceFailed,
				generationID,
				0,
				closeError,
			)
		}
	}

	if len(checkpoints) != len(identities) {
		return newLibraryError(
			ErrorIncomplete,
			generationID,
			0,
			fmt.Errorf(
				"completed %d of %d title outcomes",
				len(checkpoints),
				len(identities),
			),
		)
	}
	enrichedRecords, buildError := buildEnrichedRecords(
		sourceRecords,
		checkpoints,
	)
	if buildError != nil {
		return newLibraryError(
			ErrorIncomplete,
			generationID,
			0,
			buildError,
		)
	}
	analytics, aggregateError := netflix.Aggregate(
		ctx,
		enrichedRecords,
		netflix.AllDates(),
	)
	if aggregateError != nil {
		return newLibraryError(
			ErrorIncomplete,
			generationID,
			0,
			aggregateError,
		)
	}
	generation, generationError = workspace.buildingGeneration(generationID)
	if generationError != nil {
		return generationError
	}
	if coverageError := validateCompleteCoverage(generation, analytics); coverageError != nil {
		return coverageError
	}
	checkpoint, writeError := writeEnrichedGenerationArtifacts(
		ctx,
		workspace.root,
		generationID,
		enrichedRecords,
		analytics,
		generation.EnrichmentCheckpointBytes,
	)
	if writeError != nil {
		return writeError
	}
	if checkpointError := workspace.recordCheckpoint(
		generationID,
		checkpoint,
	); checkpointError != nil {
		return checkpointError
	}
	generation, generationError = workspace.buildingGeneration(generationID)
	if generationError != nil {
		return generationError
	}
	if _, _, validationError := validateGenerationArtifacts(
		ctx,
		workspace.root,
		generation,
	); validationError != nil {
		return validationError
	}
	if removalError := removeEnrichmentCheckpoints(
		workspace.root,
		generationID,
	); removalError != nil {
		return removalError
	}
	if clearError := workspace.clearEnrichmentCheckpointBytes(
		generationID,
	); clearError != nil {
		return clearError
	}
	return workspace.activateGeneration(generationID)
}

func (workspace *Workspace) enrichRemainingTitles(
	ctx context.Context,
	generation generationState,
	service *enrichment.Service,
	identities []netflix.TitleIdentity,
	checkpoints map[string]enrichmentCheckpoint,
) error {
	workerContext, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()
	locale, localeError := tmdb.NewLocale(generation.Locale)
	if localeError != nil {
		return newLibraryError(
			ErrorInvalidPersistence,
			generation.ID,
			0,
			localeError,
		)
	}
	jobs := make(chan netflix.TitleIdentity)
	results := make(chan enrichmentJobResult, len(identities))
	workerCount := min(product.MaxTMDBConcurrency, len(identities))
	var workers sync.WaitGroup
	for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for identity := range jobs {
				enriched, enrichError := service.Enrich(
					workerContext,
					enrichment.AuthorizeTMDBTitleQueries(),
					[]netflix.TitleIdentity{identity},
					locale,
				)
				result := enrichmentJobResult{
					identity: identity,
					err:      enrichError,
				}
				if enrichError == nil {
					if len(enriched) != 1 ||
						enriched[0].TitleIdentity().Key() != identity.Key() {
						result.err = newLibraryError(
							ErrorInvalidResponse,
							generation.ID,
							0,
							errors.New("enrichment service returned an inconsistent result"),
						)
					} else {
						result.result = enriched[0]
					}
				}
				results <- result
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, identity := range identities {
			select {
			case <-workerContext.Done():
				return
			case jobs <- identity:
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()

	var firstError error
	for result := range results {
		if result.err != nil {
			if firstError == nil {
				firstError = classifyEnrichmentError(
					generation.ID,
					result.err,
				)
				cancelWorkers()
			}
			continue
		}
		if firstError != nil {
			continue
		}
		_, _, _, _, _, existingCheckpointBytes := checkpointCoverage(checkpoints)
		checkpoint, checkpointError := writeEnrichmentCheckpoint(
			workspace.root,
			generation,
			result.result,
			existingCheckpointBytes,
		)
		if checkpointError != nil {
			firstError = checkpointError
			cancelWorkers()
			continue
		}
		checkpoints[result.identity.Key()] = checkpoint
		if progressError := workspace.recordEnrichmentProgress(
			generation.ID,
			checkpoints,
		); progressError != nil {
			firstError = progressError
			cancelWorkers()
			continue
		}
		if workspace.afterEnrichmentCheckpoint != nil {
			if hookError := workspace.afterEnrichmentCheckpoint(
				workerContext,
				generation.ID,
				len(checkpoints),
			); hookError != nil {
				code := ErrorIncomplete
				if workerContext.Err() != nil {
					code = ErrorCanceled
				}
				firstError = newLibraryError(
					code,
					generation.ID,
					0,
					hookError,
				)
				cancelWorkers()
			}
		}
	}
	return firstError
}

func (workspace *Workspace) enrichmentSource(
	generation generationState,
) (generationState, error) {
	workspace.mutex.Lock()
	defer workspace.mutex.Unlock()
	source, exists := findGeneration(
		workspace.repository.state,
		generation.SourceGenerationID,
	)
	if !exists ||
		source.State != GenerationStateReady ||
		source.AnalysisLevel != AnalysisLevelLocal ||
		source.RecordsSHA256 != generation.SourceRecordsSHA256 ||
		source.AnalyticsSHA256 != generation.SourceAnalyticsSHA256 ||
		workspace.repository.state.ActiveID != source.ID {
		return generationState{}, newLibraryError(
			ErrorStaleSource,
			generation.ID,
			0,
			errors.New("enrichment source is missing, stale, or inactive"),
		)
	}
	return source, nil
}

func uniqueTitleIdentities(
	records []netflix.ActivityRecord,
) ([]netflix.TitleIdentity, error) {
	identityByKey := make(map[string]netflix.TitleIdentity)
	for _, record := range records {
		identity := record.Activity().TitleIdentity()
		if existing, exists := identityByKey[identity.Key()]; exists &&
			existing.SearchTitle() != identity.SearchTitle() {
			return nil, errors.New("source title identity is inconsistent")
		}
		identityByKey[identity.Key()] = identity
	}
	identities := make([]netflix.TitleIdentity, 0, len(identityByKey))
	for _, identity := range identityByKey {
		identities = append(identities, identity)
	}
	sort.Slice(identities, func(leftIndex int, rightIndex int) bool {
		return identities[leftIndex].Key() < identities[rightIndex].Key()
	})
	return identities, nil
}

func buildEnrichedRecords(
	source []netflix.ActivityRecord,
	checkpoints map[string]enrichmentCheckpoint,
) ([]netflix.ActivityRecord, error) {
	records := make([]netflix.ActivityRecord, len(source))
	for recordIndex, sourceRecord := range source {
		activity := sourceRecord.Activity()
		checkpoint, exists := checkpoints[activity.TitleIdentity().Key()]
		if !exists {
			return nil, errors.New("title outcome checkpoint is missing")
		}
		record, recordError := netflix.NewEnrichedActivityRecord(
			activity,
			checkpoint.match,
			checkpoint.metadata,
		)
		if recordError != nil {
			return nil, recordError
		}
		records[recordIndex] = record
	}
	return records, nil
}

func checkpointCoverage(
	checkpoints map[string]enrichmentCheckpoint,
) (
	completed int,
	matched int,
	review int,
	unmatched int,
	cacheHits int,
	bytes int64,
) {
	for _, checkpoint := range checkpoints {
		completed++
		bytes += checkpoint.bytes
		if checkpoint.cacheHit {
			cacheHits++
		}
		switch checkpoint.match.Status() {
		case netflix.MatchStatusMatched:
			matched++
		case netflix.MatchStatusReview:
			review++
		case netflix.MatchStatusUnmatched:
			unmatched++
		}
	}
	return
}

func (workspace *Workspace) reconcileEnrichmentProgress(
	generationID string,
	checkpoints map[string]enrichmentCheckpoint,
) error {
	workspace.mutex.Lock()
	defer workspace.mutex.Unlock()
	generation, exists := findGeneration(
		workspace.repository.state,
		generationID,
	)
	if !exists {
		return newLibraryError(ErrorNotFound, generationID, 0, nil)
	}
	completed, matched, review, unmatched, cacheHits, bytes :=
		checkpointCoverage(checkpoints)
	if generation.CompletedTitleCount > completed ||
		generation.MatchedTitleCount > matched ||
		generation.ReviewTitleCount > review ||
		generation.UnmatchedTitleCount > unmatched ||
		generation.CacheHitTitleCount > cacheHits ||
		generation.EnrichmentCheckpointBytes > bytes {
		return newLibraryError(
			ErrorIncomplete,
			generationID,
			0,
			errors.New("persisted enrichment progress is ahead of its checkpoints"),
		)
	}
	if generation.CompletedTitleCount == completed &&
		generation.MatchedTitleCount == matched &&
		generation.ReviewTitleCount == review &&
		generation.UnmatchedTitleCount == unmatched &&
		generation.CacheHitTitleCount == cacheHits &&
		generation.EnrichmentCheckpointBytes == bytes {
		return nil
	}
	return workspace.mutateEnrichmentProgressLocked(
		generationID,
		completed,
		matched,
		review,
		unmatched,
		cacheHits,
		bytes,
	)
}

func (workspace *Workspace) recordEnrichmentProgress(
	generationID string,
	checkpoints map[string]enrichmentCheckpoint,
) error {
	workspace.mutex.Lock()
	defer workspace.mutex.Unlock()
	completed, matched, review, unmatched, cacheHits, bytes :=
		checkpointCoverage(checkpoints)
	return workspace.mutateEnrichmentProgressLocked(
		generationID,
		completed,
		matched,
		review,
		unmatched,
		cacheHits,
		bytes,
	)
}

func (workspace *Workspace) mutateEnrichmentProgressLocked(
	generationID string,
	completed int,
	matched int,
	review int,
	unmatched int,
	cacheHits int,
	bytes int64,
) error {
	nowMilliseconds := workspace.now().UTC().UnixMilli()
	return workspace.repository.mutate(func(state *repositoryState) error {
		generationIndex, found := findGenerationIndex(*state, generationID)
		if !found ||
			state.BuildingID != generationID ||
			state.Generations[generationIndex].AnalysisLevel != AnalysisLevelTMDB ||
			state.Generations[generationIndex].State != GenerationStateEnriching {
			return newLibraryError(ErrorInvalidState, generationID, 0, nil)
		}
		generation := &state.Generations[generationIndex]
		if completed < generation.CompletedTitleCount ||
			completed > generation.UniqueTitleCount ||
			matched+review+unmatched != completed ||
			cacheHits > completed ||
			bytes < 0 ||
			bytes > product.MaxNetflixWorkingBytes {
			return newLibraryError(
				ErrorIncomplete,
				generationID,
				0,
				errors.New("enrichment progress is inconsistent"),
			)
		}
		generation.CompletedTitleCount = completed
		generation.MatchedTitleCount = matched
		generation.ReviewTitleCount = review
		generation.UnmatchedTitleCount = unmatched
		generation.CacheHitTitleCount = cacheHits
		generation.EnrichmentCheckpointBytes = bytes
		nextPercent := progressPercent(completed, generation.UniqueTitleCount)
		latestEvent := &generation.Events[len(generation.Events)-1]
		if nextPercent > latestEvent.ProgressPercent {
			transitionGeneration(
				generation,
				GenerationStateEnriching,
				nowMilliseconds,
				nil,
			)
		} else {
			generation.UpdatedAtMS = nowMilliseconds
			latestEvent.CompletedTitleCount = completed
			latestEvent.MatchedTitleCount = matched
			latestEvent.ReviewTitleCount = review
			latestEvent.UnmatchedTitleCount = unmatched
			latestEvent.CacheHitTitleCount = cacheHits
			latestEvent.ProgressPercent = nextPercent
			latestEvent.OccurredAtMS = nowMilliseconds
		}
		return nil
	})
}

func (workspace *Workspace) clearEnrichmentCheckpointBytes(
	generationID string,
) error {
	workspace.mutex.Lock()
	defer workspace.mutex.Unlock()
	return workspace.repository.mutate(func(state *repositoryState) error {
		generationIndex, found := findGenerationIndex(*state, generationID)
		if !found ||
			state.BuildingID != generationID ||
			state.Generations[generationIndex].AnalysisLevel != AnalysisLevelTMDB ||
			state.Generations[generationIndex].State != GenerationStateEnriching ||
			!validSHA256(state.Generations[generationIndex].RecordsSHA256) ||
			!validSHA256(state.Generations[generationIndex].AnalyticsSHA256) {
			return newLibraryError(ErrorInvalidState, generationID, 0, nil)
		}
		state.Generations[generationIndex].EnrichmentCheckpointBytes = 0
		return nil
	})
}

func validateCompleteCoverage(
	generation generationState,
	analytics netflix.Analytics,
) error {
	statusTitles := make(map[string]int, len(analytics.MatchStatusTitles))
	for _, count := range analytics.MatchStatusTitles {
		statusTitles[count.Label] = count.Value
	}
	if analytics.ActivityCount != generation.ActivityCount ||
		analytics.UniqueTitleCount != generation.UniqueTitleCount ||
		statusTitles[string(netflix.MatchStatusMatched)] != generation.MatchedTitleCount ||
		statusTitles[string(netflix.MatchStatusReview)] != generation.ReviewTitleCount ||
		statusTitles[string(netflix.MatchStatusUnmatched)] != generation.UnmatchedTitleCount ||
		generation.CompletedTitleCount != generation.UniqueTitleCount {
		return newLibraryError(
			ErrorIncomplete,
			generation.ID,
			0,
			errors.New("enriched analytics coverage is incomplete"),
		)
	}
	return nil
}

func (workspace *Workspace) dispatchTMDBEnrichmentLocked(generationID string) {
	if workspace.closing {
		return
	}
	if _, running := workspace.jobs[generationID]; running {
		return
	}
	jobContext, cancelJob := context.WithCancel(workspace.context)
	job := &runningJob{cancel: cancelJob, done: make(chan struct{})}
	workspace.jobs[generationID] = job
	go func() {
		runError := workspace.runTMDBEnrichment(jobContext, generationID)
		if runError != nil && workspace.context.Err() == nil {
			var typedError *Error
			if !errors.As(runError, &typedError) {
				typedError = newLibraryError(
					ErrorIncomplete,
					generationID,
					0,
					runError,
				)
			}
			_ = workspace.failGeneration(generationID, typedError)
		}
		workspace.mutex.Lock()
		delete(workspace.jobs, generationID)
		close(job.done)
		workspace.mutex.Unlock()
	}()
}
