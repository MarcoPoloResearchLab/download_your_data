package library

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/privatepath"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/enrichment"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
)

const recordCursorIdentity = "netflix-record-cursor-v3"

type workspaceOptions struct {
	now                       func() time.Time
	entropy                   io.Reader
	beforeArtifactWrite       func(context.Context) error
	afterEnrichmentCheckpoint func(context.Context, string, int) error
}

type runningJob struct {
	cancel      context.CancelFunc
	done        chan struct{}
	closeSource func() error
}

// Workspace owns one leased, restart-safe Netflix provider library.
type Workspace struct {
	mutex                     sync.Mutex
	root                      privatepath.Root
	repository                *repository
	lease                     *providerLease
	cacheFile                 privatepath.File
	metadataClient            enrichment.MetadataClient
	tmdbConfigured            bool
	now                       func() time.Time
	entropy                   io.Reader
	beforeArtifactWrite       func(context.Context) error
	afterEnrichmentCheckpoint func(context.Context, string, int) error
	context                   context.Context
	cancel                    context.CancelFunc
	jobs                      map[string]*runningJob
	uploads                   map[string]*runningJob
	closing                   bool
}

// Open acquires the provider lease and resumes the current generation checkpoint.
func Open(
	root privatepath.Root,
	stateFile privatepath.File,
	leaseFile privatepath.File,
	cacheFile privatepath.File,
	metadataClient enrichment.MetadataClient,
) (*Workspace, error) {
	return openWorkspace(
		root,
		stateFile,
		leaseFile,
		cacheFile,
		metadataClient,
		workspaceOptions{now: time.Now, entropy: rand.Reader},
	)
}

func openWorkspace(
	root privatepath.Root,
	stateFile privatepath.File,
	leaseFile privatepath.File,
	cacheFile privatepath.File,
	metadataClient enrichment.MetadataClient,
	options workspaceOptions,
) (*Workspace, error) {
	if root.Path() == "" || options.now == nil || options.entropy == nil {
		return nil, newLibraryError(
			ErrorInvalidRequest,
			"",
			0,
			errors.New("netflix workspace configuration is not initialized"),
		)
	}
	if cacheFile.RelativePath() != filepath.FromSlash(product.NetflixTMDBCacheRelativePath) {
		return nil, newLibraryError(
			ErrorInvalidPersistence,
			"",
			0,
			errors.New("netflix cache path does not match the current contract"),
		)
	}
	if metadataClient != nil && metadataClient.Identity() != tmdb.ClientIdentity {
		return nil, newLibraryError(
			ErrorInvalidRequest,
			"",
			0,
			errors.New("netflix metadata client identity is invalid"),
		)
	}
	lease, leaseError := acquireProviderLease(leaseFile)
	if leaseError != nil {
		return nil, leaseError
	}
	repositoryValue, repositoryError := openRepository(stateFile)
	if repositoryError != nil {
		_ = lease.close()
		return nil, repositoryError
	}
	workspaceContext, cancelWorkspace := context.WithCancel(context.Background())
	workspace := &Workspace{
		root:                      root,
		repository:                repositoryValue,
		lease:                     lease,
		cacheFile:                 cacheFile,
		metadataClient:            metadataClient,
		tmdbConfigured:            metadataClient != nil,
		now:                       options.now,
		entropy:                   options.entropy,
		beforeArtifactWrite:       options.beforeArtifactWrite,
		afterEnrichmentCheckpoint: options.afterEnrichmentCheckpoint,
		context:                   workspaceContext,
		cancel:                    cancelWorkspace,
		jobs:                      make(map[string]*runningJob),
		uploads:                   make(map[string]*runningJob),
	}
	if recoveryError := workspace.recoverDeletionCheckpoints(); recoveryError != nil {
		cancelWorkspace()
		_ = lease.close()
		return nil, recoveryError
	}
	if recoveryError := workspace.recoverBuildingGeneration(); recoveryError != nil {
		cancelWorkspace()
		_ = lease.close()
		return nil, recoveryError
	}
	return workspace, nil
}

// Close stops in-process work while preserving resumable nonterminal checkpoints.
func (workspace *Workspace) Close() error {
	if workspace == nil {
		return nil
	}
	workspace.mutex.Lock()
	if workspace.closing {
		workspace.mutex.Unlock()
		return nil
	}
	workspace.closing = true
	workspace.cancel()
	running := make(
		[]*runningJob,
		0,
		len(workspace.jobs)+len(workspace.uploads),
	)
	for _, job := range workspace.jobs {
		running = append(running, job)
	}
	for _, upload := range workspace.uploads {
		running = append(running, upload)
	}
	workspace.mutex.Unlock()
	for _, operation := range running {
		operation.cancel()
		if operation.closeSource != nil {
			_ = operation.closeSource()
		}
	}
	for _, operation := range running {
		<-operation.done
	}
	workspace.mutex.Lock()
	lease := workspace.lease
	workspace.lease = nil
	workspace.mutex.Unlock()
	if lease == nil {
		return nil
	}
	if closeError := lease.close(); closeError != nil {
		return newLibraryError(ErrorPersistenceFailed, "", 0, closeError)
	}
	return nil
}

// Snapshot returns the current provider-owned workspace state.
func (workspace *Workspace) Snapshot() Snapshot {
	workspace.mutex.Lock()
	defer workspace.mutex.Unlock()
	state := workspace.repository.state
	snapshot := Snapshot{
		Provider:     "netflix",
		State:        ProviderStateEmpty,
		Capabilities: capabilities(workspace.tmdbConfigured),
	}
	if state.Deleting {
		snapshot.State = ProviderStateDeleting
	}
	if active, exists := findGeneration(state, state.ActiveID); exists {
		activeSnapshot := active.snapshot()
		snapshot.Active = &activeSnapshot
		if active.AnalysisLevel == AnalysisLevelTMDB {
			snapshot.State = ProviderStateReadyTMDB
		} else {
			snapshot.State = ProviderStateReadyLocal
		}
	}
	if building, exists := findGeneration(state, state.BuildingID); exists {
		buildingSnapshot := building.snapshot()
		snapshot.Building = &buildingSnapshot
		snapshot.State = ProviderStateBuilding
	}
	if failed, exists := latestFailedGeneration(state); exists {
		failedSnapshot := failed.snapshot()
		snapshot.LatestFailed = &failedSnapshot
		if snapshot.Active == nil && snapshot.Building == nil && !state.Deleting {
			snapshot.State = ProviderStateActionNeeded
		}
	}
	if state.Deleting {
		snapshot.State = ProviderStateDeleting
	}
	return snapshot
}

// CreateLocalGeneration allocates one receiving generation.
func (workspace *Workspace) CreateLocalGeneration() (Generation, error) {
	workspace.mutex.Lock()
	defer workspace.mutex.Unlock()
	if workspace.closing {
		return Generation{}, newLibraryError(
			ErrorConflict,
			"",
			0,
			errors.New("netflix workspace is closing"),
		)
	}
	if workspace.repository.state.Deleting {
		return Generation{}, newLibraryError(
			ErrorConflict,
			"",
			0,
			errors.New("netflix provider deletion is in progress"),
		)
	}
	if workspace.repository.state.BuildingID != "" ||
		len(workspace.uploads) != 0 {
		return Generation{}, newLibraryError(
			ErrorConflict,
			workspace.repository.state.BuildingID,
			0,
			errors.New("one Netflix generation is already building"),
		)
	}
	generationID, identifierError := workspace.newGenerationIDLocked()
	if identifierError != nil {
		return Generation{}, identifierError
	}
	nowMilliseconds := workspace.now().UTC().UnixMilli()
	generation := generationState{
		ID:            generationID,
		AnalysisLevel: AnalysisLevelLocal,
		State:         GenerationStateReceiving,
		CreatedAtMS:   nowMilliseconds,
		UpdatedAtMS:   nowMilliseconds,
		Events: []eventState{{
			Sequence:     1,
			State:        GenerationStateReceiving,
			OccurredAtMS: nowMilliseconds,
		}},
	}
	if mutationError := workspace.repository.mutate(func(state *repositoryState) error {
		state.BuildingID = generationID
		state.Generations = append(state.Generations, generation)
		sortGenerations(state.Generations)
		return nil
	}); mutationError != nil {
		return Generation{}, mutationError
	}
	return generation.snapshot(), nil
}

// CreateTMDBGeneration starts one explicitly authorized enriched replacement
// from the active ready-local generation.
func (workspace *Workspace) CreateTMDBGeneration(
	ctx context.Context,
	sourceGenerationID string,
	locale tmdb.Locale,
	authorization enrichment.Authorization,
) (Generation, error) {
	if ctx == nil || !validGenerationID(sourceGenerationID) {
		return Generation{}, newLibraryError(
			ErrorInvalidRequest,
			sourceGenerationID,
			0,
			errors.New("current context and source generation are required"),
		)
	}
	if !authorization.Explicit() {
		return Generation{}, newLibraryError(
			ErrorConsentRequired,
			sourceGenerationID,
			0,
			errors.New("explicit TMDB title-query consent is required"),
		)
	}
	if _, localeError := tmdb.NewLocale(locale.String()); localeError != nil {
		return Generation{}, newLibraryError(
			ErrorInvalidRequest,
			sourceGenerationID,
			0,
			localeError,
		)
	}

	workspace.mutex.Lock()
	if workspace.closing ||
		workspace.repository.state.Deleting ||
		workspace.repository.state.BuildingID != "" ||
		len(workspace.uploads) != 0 {
		workspace.mutex.Unlock()
		return Generation{}, newLibraryError(
			ErrorConflict,
			sourceGenerationID,
			0,
			errors.New("netflix provider is not accepting an enriched generation"),
		)
	}
	if workspace.metadataClient == nil {
		workspace.mutex.Unlock()
		return Generation{}, newLibraryError(
			ErrorNotConfigured,
			sourceGenerationID,
			0,
			errors.New("TMDB enrichment is not configured"),
		)
	}
	sourceGeneration, sourceExists := findGeneration(
		workspace.repository.state,
		sourceGenerationID,
	)
	if !sourceExists ||
		workspace.repository.state.ActiveID != sourceGenerationID ||
		sourceGeneration.State != GenerationStateReady ||
		sourceGeneration.AnalysisLevel != AnalysisLevelLocal {
		workspace.mutex.Unlock()
		return Generation{}, newLibraryError(
			ErrorStaleSource,
			sourceGenerationID,
			0,
			errors.New("source generation is not the active ready-local generation"),
		)
	}
	workspace.mutex.Unlock()

	if _, _, validationError := validateGenerationArtifacts(
		ctx,
		workspace.root,
		sourceGeneration,
	); validationError != nil {
		return Generation{}, newLibraryError(
			ErrorStaleSource,
			sourceGenerationID,
			0,
			validationError,
		)
	}

	workspace.mutex.Lock()
	defer workspace.mutex.Unlock()
	currentSource, sourceExists := findGeneration(
		workspace.repository.state,
		sourceGenerationID,
	)
	if workspace.closing ||
		workspace.repository.state.Deleting ||
		workspace.repository.state.BuildingID != "" ||
		len(workspace.uploads) != 0 ||
		!sourceExists ||
		workspace.repository.state.ActiveID != sourceGenerationID ||
		currentSource.State != sourceGeneration.State ||
		currentSource.AnalysisLevel != sourceGeneration.AnalysisLevel ||
		currentSource.RecordsSHA256 != sourceGeneration.RecordsSHA256 ||
		currentSource.AnalyticsSHA256 != sourceGeneration.AnalyticsSHA256 ||
		currentSource.ActivityCount != sourceGeneration.ActivityCount ||
		currentSource.UniqueTitleCount != sourceGeneration.UniqueTitleCount {
		return Generation{}, newLibraryError(
			ErrorStaleSource,
			sourceGenerationID,
			0,
			errors.New("source generation changed during validation"),
		)
	}
	generationID, identifierError := workspace.newGenerationIDLocked()
	if identifierError != nil {
		return Generation{}, identifierError
	}
	nowMilliseconds := workspace.now().UTC().UnixMilli()
	generation := generationState{
		ID:                        generationID,
		SourceGenerationID:        sourceGenerationID,
		AnalysisLevel:             AnalysisLevelTMDB,
		State:                     GenerationStateEnriching,
		ActivityCount:             sourceGeneration.ActivityCount,
		UniqueTitleCount:          sourceGeneration.UniqueTitleCount,
		StartDate:                 sourceGeneration.StartDate,
		EndDate:                   sourceGeneration.EndDate,
		CreatedAtMS:               nowMilliseconds,
		UpdatedAtMS:               nowMilliseconds,
		Locale:                    locale.String(),
		TMDBAuthorizationIdentity: tmdbAuthorizationContract,
		TMDBClientIdentity:        workspace.metadataClient.Identity(),
		TMDBMatcherIdentity:       netflix.TMDBMatcherIdentity,
		TMDBCacheIdentity:         enrichment.CacheFreshnessIdentity,
		SourceRecordsSHA256:       sourceGeneration.RecordsSHA256,
		SourceAnalyticsSHA256:     sourceGeneration.AnalyticsSHA256,
	}
	generation.Events = []eventState{{
		Sequence:         1,
		State:            GenerationStateEnriching,
		ActivityCount:    generation.ActivityCount,
		UniqueTitleCount: generation.UniqueTitleCount,
		OccurredAtMS:     nowMilliseconds,
		TotalTitleCount:  generation.UniqueTitleCount,
	}}
	if mutationError := workspace.repository.mutate(func(state *repositoryState) error {
		state.BuildingID = generationID
		state.Generations = append(state.Generations, generation)
		sortGenerations(state.Generations)
		return nil
	}); mutationError != nil {
		return Generation{}, mutationError
	}
	workspace.dispatchTMDBEnrichmentLocked(generationID)
	return generation.snapshot(), nil
}

// UploadViewingActivity stages a bounded CSV and starts its local import.
func (workspace *Workspace) UploadViewingActivity(
	ctx context.Context,
	generationID string,
	source io.Reader,
) (Generation, error) {
	if ctx == nil || source == nil || !validGenerationID(generationID) {
		return Generation{}, newLibraryError(
			ErrorInvalidRequest,
			generationID,
			0,
			errors.New("current generation, context, and upload body are required"),
		)
	}
	workspace.mutex.Lock()
	generation, exists := findGeneration(workspace.repository.state, generationID)
	if !exists {
		workspace.mutex.Unlock()
		return Generation{}, newLibraryError(ErrorNotFound, generationID, 0, nil)
	}
	if workspace.closing ||
		workspace.repository.state.Deleting ||
		workspace.repository.state.BuildingID != generationID ||
		generation.State != GenerationStateReceiving {
		workspace.mutex.Unlock()
		return Generation{}, newLibraryError(
			ErrorInvalidState,
			generationID,
			0,
			errors.New("generation is not accepting a viewing-activity upload"),
		)
	}
	if _, uploading := workspace.uploads[generationID]; uploading {
		workspace.mutex.Unlock()
		return Generation{}, newLibraryError(
			ErrorConflict,
			generationID,
			0,
			errors.New("generation upload is already in progress"),
		)
	}
	uploadContext, cancelUpload := context.WithCancel(workspace.context)
	stopRequestCancellation := context.AfterFunc(ctx, cancelUpload)
	upload := &runningJob{
		cancel: cancelUpload,
		done:   make(chan struct{}),
	}
	if sourceCloser, closable := source.(io.Closer); closable {
		upload.closeSource = sourceCloser.Close
	}
	workspace.uploads[generationID] = upload
	workspace.mutex.Unlock()
	defer func() {
		stopRequestCancellation()
		cancelUpload()
		workspace.mutex.Lock()
		delete(workspace.uploads, generationID)
		close(upload.done)
		workspace.mutex.Unlock()
	}()

	stageError := stageViewingActivity(
		uploadContext,
		workspace.root,
		generationID,
		source,
	)

	workspace.mutex.Lock()
	if stageError != nil {
		workspace.mutex.Unlock()
		var typedError *Error
		if errors.As(stageError, &typedError) &&
			typedError.Code() != ErrorConflict &&
			typedError.Code() != ErrorInvalidState {
			_ = workspace.failGeneration(generationID, typedError)
		}
		return Generation{}, stageError
	}
	if workspace.closing || workspace.context.Err() != nil {
		workspace.mutex.Unlock()
		return Generation{}, newLibraryError(
			ErrorCanceled,
			generationID,
			0,
			context.Canceled,
		)
	}
	nowMilliseconds := workspace.now().UTC().UnixMilli()
	mutationError := workspace.repository.mutate(func(state *repositoryState) error {
		generationIndex, found := findGenerationIndex(*state, generationID)
		if !found ||
			state.BuildingID != generationID ||
			state.Generations[generationIndex].State != GenerationStateReceiving {
			return newLibraryError(
				ErrorInvalidState,
				generationID,
				0,
				errors.New("generation stopped accepting its upload"),
			)
		}
		transitionGeneration(
			&state.Generations[generationIndex],
			GenerationStateValidating,
			nowMilliseconds,
			nil,
		)
		return nil
	})
	if mutationError != nil {
		workspace.mutex.Unlock()
		_ = workspace.failGeneration(
			generationID,
			newLibraryError(ErrorPersistenceFailed, generationID, 0, mutationError),
		)
		return Generation{}, mutationError
	}
	generation, _ = findGeneration(workspace.repository.state, generationID)
	workspace.dispatchLocalImportLocked(generationID)
	workspace.mutex.Unlock()
	return generation.snapshot(), nil
}

// Events returns ordered transitions after the supplied sequence.
func (workspace *Workspace) Events(
	generationID string,
	afterSequence int64,
) (Events, error) {
	if !validGenerationID(generationID) || afterSequence < 0 {
		return Events{}, newLibraryError(
			ErrorInvalidRequest,
			generationID,
			0,
			errors.New("generation event boundary is invalid"),
		)
	}
	workspace.mutex.Lock()
	defer workspace.mutex.Unlock()
	generation, exists := findGeneration(workspace.repository.state, generationID)
	if !exists {
		return Events{}, newLibraryError(ErrorNotFound, generationID, 0, nil)
	}
	result := Events{
		GenerationID: generationID,
		Events:       []Event{},
		LastSequence: int64(len(generation.Events)),
	}
	for _, event := range generation.Events {
		if event.Sequence > afterSequence {
			result.Events = append(result.Events, event.snapshot())
		}
	}
	return result, nil
}

// Analytics returns complete or inclusively date-filtered raw activity measures.
func (workspace *Workspace) Analytics(
	ctx context.Context,
	generationID string,
	filter ActivityFilter,
) (Analytics, error) {
	if ctx == nil || !validGenerationID(generationID) {
		return Analytics{}, newLibraryError(
			ErrorInvalidRequest,
			generationID,
			0,
			errors.New("analytics context and generation are required"),
		)
	}
	normalizedFilter, dateRange, filterError := normalizeActivityFilter(
		generationID,
		filter,
	)
	if filterError != nil {
		return Analytics{}, filterError
	}
	generation, generationError := workspace.readyGeneration(generationID)
	if generationError != nil {
		return Analytics{}, generationError
	}
	files, filesError := resolveGenerationFiles(workspace.root, generationID)
	if filesError != nil {
		return Analytics{}, filesError
	}
	records, recordsError := readAllRecords(ctx, files.records, generationID)
	if recordsError != nil {
		return Analytics{}, recordsError
	}
	filteredRecords := filterActivityRecords(records, normalizedFilter.MatchStatus)
	aggregated, aggregateError := netflix.Aggregate(ctx, filteredRecords, dateRange)
	if aggregateError != nil {
		code := ErrorIncomplete
		if ctx.Err() != nil {
			code = ErrorCanceled
		}
		return Analytics{}, newLibraryError(code, generation.ID, 0, aggregateError)
	}
	return Analytics{
		GenerationID: generationID,
		Filter:       normalizedFilter,
		Data:         aggregated,
	}, nil
}

// Records returns one deterministic cursor-paged activity slice.
func (workspace *Workspace) Records(
	ctx context.Context,
	generationID string,
	cursor string,
	limit int,
	filter ActivityFilter,
) (ActivityPage, error) {
	if ctx == nil || !validGenerationID(generationID) {
		return ActivityPage{}, newLibraryError(
			ErrorInvalidRequest,
			generationID,
			0,
			errors.New("records context and generation are required"),
		)
	}
	if limit == 0 {
		limit = product.DefaultNetflixRecordPageSize
	}
	if limit < 1 || limit > product.MaxNetflixRecordPageSize {
		return ActivityPage{}, newLibraryError(
			ErrorInvalidRequest,
			generationID,
			0,
			errors.New("records limit is outside the current bound"),
		)
	}
	normalizedFilter, _, filterError := normalizeActivityFilter(generationID, filter)
	if filterError != nil {
		return ActivityPage{}, filterError
	}
	afterIndex := int64(0)
	if cursor != "" {
		decodedGenerationID, decodedIndex, decodedFilter, cursorError :=
			decodeRecordCursor(cursor)
		if cursorError != nil ||
			decodedGenerationID != generationID ||
			decodedFilter != normalizedFilter {
			return ActivityPage{}, newLibraryError(
				ErrorInvalidRequest,
				generationID,
				0,
				errors.New("records cursor is invalid for this generation"),
			)
		}
		afterIndex = decodedIndex
	}
	if _, generationError := workspace.readyGeneration(generationID); generationError != nil {
		return ActivityPage{}, generationError
	}
	records, nextAfter, recordsError := readActivityPage(
		ctx,
		workspace.root,
		generationID,
		afterIndex,
		limit,
		normalizedFilter,
	)
	if recordsError != nil {
		return ActivityPage{}, recordsError
	}
	result := ActivityPage{
		GenerationID: generationID,
		Filter:       normalizedFilter,
		Records:      records,
	}
	if nextAfter > 0 {
		result.NextCursor = encodeRecordCursor(
			generationID,
			nextAfter,
			normalizedFilter,
		)
	}
	return result, nil
}

// ExportRecords returns one fully revalidated enriched generation for direct
// CSV streaming without a temporary export file.
func (workspace *Workspace) ExportRecords(
	ctx context.Context,
	generationID string,
) ([]netflix.ActivityRecord, error) {
	if ctx == nil || !validGenerationID(generationID) {
		return nil, newLibraryError(
			ErrorInvalidRequest,
			generationID,
			0,
			errors.New("export context and generation are required"),
		)
	}
	generation, generationError := workspace.readyGeneration(generationID)
	if generationError != nil {
		return nil, generationError
	}
	if generation.AnalysisLevel != AnalysisLevelTMDB {
		return nil, newLibraryError(
			ErrorInvalidState,
			generationID,
			0,
			errors.New("only a ready TMDB generation has the enriched export contract"),
		)
	}
	records, _, validationError := validateGenerationArtifacts(
		ctx,
		workspace.root,
		generation,
	)
	if validationError != nil {
		return nil, validationError
	}
	return records, nil
}

// DeleteGeneration cancels and removes one non-active generation.
func (workspace *Workspace) DeleteGeneration(
	ctx context.Context,
	generationID string,
) error {
	if ctx == nil || !validGenerationID(generationID) {
		return newLibraryError(
			ErrorInvalidRequest,
			generationID,
			0,
			errors.New("deletion context and generation are required"),
		)
	}
	workspace.mutex.Lock()
	if workspace.closing || workspace.repository.state.Deleting {
		workspace.mutex.Unlock()
		return newLibraryError(
			ErrorConflict,
			generationID,
			0,
			errors.New("netflix workspace is not accepting generation deletion"),
		)
	}
	if workspace.repository.state.ActiveID == generationID {
		workspace.mutex.Unlock()
		return newLibraryError(
			ErrorConflict,
			generationID,
			0,
			errors.New("active generation can be removed only by complete provider deletion"),
		)
	}
	for _, candidate := range workspace.repository.state.Generations {
		if candidate.SourceGenerationID == generationID {
			workspace.mutex.Unlock()
			return newLibraryError(
				ErrorConflict,
				generationID,
				0,
				errors.New("generation is retained by a derived TMDB generation"),
			)
		}
	}
	generation, exists := findGeneration(workspace.repository.state, generationID)
	if !exists {
		workspace.mutex.Unlock()
		return newLibraryError(ErrorNotFound, generationID, 0, nil)
	}
	wasBuilding := isBuildingState(generation.State)
	if _, uploading := workspace.uploads[generationID]; uploading {
		workspace.mutex.Unlock()
		return newLibraryError(
			ErrorConflict,
			generationID,
			0,
			errors.New("generation upload is still in progress"),
		)
	}
	job := workspace.jobs[generationID]
	if job != nil {
		job.cancel()
	}
	workspace.mutex.Unlock()
	if job != nil {
		select {
		case <-ctx.Done():
			return newLibraryError(ErrorCanceled, generationID, 0, ctx.Err())
		case <-job.done:
		}
	}
	if wasBuilding {
		workspace.mutex.Lock()
		currentGeneration, stillExists := findGeneration(
			workspace.repository.state,
			generationID,
		)
		if !stillExists {
			workspace.mutex.Unlock()
			return nil
		}
		if currentGeneration.State == GenerationStateReady ||
			workspace.repository.state.ActiveID == generationID {
			workspace.mutex.Unlock()
			return newLibraryError(
				ErrorConflict,
				generationID,
				0,
				errors.New("generation became active before cancellation"),
			)
		}
		if currentGeneration.State == GenerationStateFailed {
			workspace.mutex.Unlock()
			return nil
		}
		workspace.mutex.Unlock()
		return workspace.failGeneration(
			generationID,
			newLibraryError(
				ErrorCanceled,
				generationID,
				0,
				context.Canceled,
			),
		)
	}

	workspace.mutex.Lock()
	if workspace.repository.state.ActiveID == generationID {
		workspace.mutex.Unlock()
		return newLibraryError(ErrorConflict, generationID, 0, nil)
	}
	markError := workspace.repository.mutate(func(state *repositoryState) error {
		if _, exists := findGeneration(*state, generationID); !exists {
			return newLibraryError(ErrorNotFound, generationID, 0, nil)
		}
		if !slices.Contains(state.PendingDeletions, generationID) {
			state.PendingDeletions = append(state.PendingDeletions, generationID)
			sort.Strings(state.PendingDeletions)
		}
		if state.BuildingID == generationID {
			state.BuildingID = ""
		}
		return nil
	})
	workspace.mutex.Unlock()
	if markError != nil {
		return markError
	}
	if removeError := removeGenerationDirectory(workspace.root, generationID); removeError != nil {
		return removeError
	}
	workspace.mutex.Lock()
	defer workspace.mutex.Unlock()
	return workspace.repository.mutate(func(state *repositoryState) error {
		removeGenerationState(state, generationID)
		state.PendingDeletions = removeString(state.PendingDeletions, generationID)
		return nil
	})
}

// DeleteProvider removes every generation, event, staged source, and TMDB cache.
func (workspace *Workspace) DeleteProvider(ctx context.Context) error {
	if ctx == nil {
		return newLibraryError(
			ErrorInvalidRequest,
			"",
			0,
			errors.New("provider deletion context is required"),
		)
	}
	if contextError := ctx.Err(); contextError != nil {
		return newLibraryError(ErrorCanceled, "", 0, contextError)
	}
	workspace.mutex.Lock()
	if workspace.closing || workspace.repository.state.Deleting {
		workspace.mutex.Unlock()
		return newLibraryError(
			ErrorConflict,
			"",
			0,
			errors.New("netflix workspace is not accepting provider deletion"),
		)
	}
	if len(workspace.uploads) != 0 {
		workspace.mutex.Unlock()
		return newLibraryError(
			ErrorConflict,
			"",
			0,
			errors.New("a viewing-activity upload is still in progress"),
		)
	}
	if mutationError := workspace.repository.mutate(func(state *repositoryState) error {
		state.Deleting = true
		return nil
	}); mutationError != nil {
		workspace.mutex.Unlock()
		return mutationError
	}
	jobs := make([]*runningJob, 0, len(workspace.jobs))
	for _, job := range workspace.jobs {
		job.cancel()
		jobs = append(jobs, job)
	}
	workspace.mutex.Unlock()
	for _, job := range jobs {
		<-job.done
	}
	if removalError := errors.Join(
		removeAllGenerationDirectories(workspace.root),
		removeTMDBCacheFiles(workspace.cacheFile),
	); removalError != nil {
		return removalError
	}
	workspace.mutex.Lock()
	defer workspace.mutex.Unlock()
	return workspace.repository.mutate(func(state *repositoryState) error {
		state.Deleting = false
		state.ActiveID = ""
		state.BuildingID = ""
		state.PendingDeletions = []string{}
		state.Generations = []generationState{}
		return nil
	})
}

func (workspace *Workspace) runLocalImport(
	ctx context.Context,
	generationID string,
) error {
	generation, generationError := workspace.buildingGeneration(generationID)
	if generationError != nil {
		return generationError
	}
	if generation.State == GenerationStateImporting &&
		validSHA256(generation.RecordsSHA256) &&
		validSHA256(generation.AnalyticsSHA256) {
		if _, _, validationError := validateGenerationArtifacts(
			ctx,
			workspace.root,
			generation,
		); validationError == nil {
			if removeError := removeStagedSource(workspace.root, generationID); removeError != nil {
				return removeError
			}
			return workspace.activateGeneration(generationID)
		}
		sourceExists, sourceError := stagedSourceExists(workspace.root, generationID)
		if sourceError != nil {
			return sourceError
		}
		if !sourceExists {
			return newLibraryError(
				ErrorIncomplete,
				generationID,
				0,
				errors.New("complete checkpoint cannot be reconstructed"),
			)
		}
	}

	sourceFile, sourceBytes, sourceError := openStagedSource(
		workspace.root,
		generationID,
	)
	if sourceError != nil {
		return sourceError
	}
	sourceClosed := false
	defer func() {
		if !sourceClosed {
			_ = sourceFile.Close()
		}
	}()
	csvLimits, limitsError := netflix.NewCSVLimits(
		product.MaxNetflixViewingRows,
		product.MaxNetflixTitleBytes,
		product.MaxNetflixFieldBytes,
	)
	if limitsError != nil {
		return newLibraryError(ErrorIncomplete, generationID, 0, limitsError)
	}
	activities, parseError := netflix.ParseViewingActivity(ctx, sourceFile, csvLimits)
	if parseError != nil {
		return classifyCSVError(generationID, parseError)
	}
	records, validationError := workspace.validateLocalActivities(
		generationID,
		activities,
	)
	if validationError != nil {
		return validationError
	}
	analytics, aggregateError := netflix.Aggregate(
		ctx,
		records,
		netflix.AllDates(),
	)
	if aggregateError != nil {
		code := ErrorIncomplete
		if ctx.Err() != nil {
			code = ErrorCanceled
		}
		return newLibraryError(code, generationID, 0, aggregateError)
	}
	if transitionError := workspace.recordImporting(generationID, analytics); transitionError != nil {
		return transitionError
	}
	if workspace.beforeArtifactWrite != nil {
		if hookError := workspace.beforeArtifactWrite(ctx); hookError != nil {
			if ctx.Err() != nil {
				return newLibraryError(ErrorCanceled, generationID, 0, ctx.Err())
			}
			return newLibraryError(ErrorIncomplete, generationID, 0, hookError)
		}
	}
	checkpoint, writeError := writeGenerationArtifacts(
		ctx,
		workspace.root,
		generationID,
		records,
		analytics,
		sourceBytes,
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
	if closeError := sourceFile.Close(); closeError != nil {
		return newLibraryError(ErrorPersistenceFailed, generationID, 0, closeError)
	}
	sourceClosed = true
	if removeError := removeStagedSource(workspace.root, generationID); removeError != nil {
		return removeError
	}
	return workspace.activateGeneration(generationID)
}

func (workspace *Workspace) validateLocalActivities(
	generationID string,
	activities []netflix.ViewingActivity,
) ([]netflix.ActivityRecord, error) {
	minimumDate := fmt.Sprintf("%04d-01-01", product.MinNetflixViewingYear)
	maximumDate := workspace.now().UTC().Format("2006-01-02")
	titleIdentities := make(map[string]struct{})
	records := make([]netflix.ActivityRecord, len(activities))
	for activityIndex, activity := range activities {
		dateISO := activity.Date().ISO()
		if dateISO < minimumDate || dateISO > maximumDate {
			return nil, newLibraryError(
				ErrorInvalidDate,
				generationID,
				activityIndex+2,
				errors.New("viewing date is outside the current product range"),
			)
		}
		titleIdentities[activity.TitleIdentity().Key()] = struct{}{}
		if len(titleIdentities) > product.MaxNetflixUniqueTitles {
			return nil, newLibraryError(
				ErrorLimitExceeded,
				generationID,
				activityIndex+2,
				errors.New("unique-title limit exceeded"),
			)
		}
		record, recordError := netflix.NewLocalActivityRecord(activity)
		if recordError != nil {
			return nil, newLibraryError(
				ErrorInvalidRow,
				generationID,
				activityIndex+2,
				recordError,
			)
		}
		records[activityIndex] = record
	}
	return records, nil
}

func (workspace *Workspace) recordImporting(
	generationID string,
	analytics netflix.Analytics,
) error {
	workspace.mutex.Lock()
	defer workspace.mutex.Unlock()
	nowMilliseconds := workspace.now().UTC().UnixMilli()
	return workspace.repository.mutate(func(state *repositoryState) error {
		generationIndex, found := findGenerationIndex(*state, generationID)
		if !found ||
			state.BuildingID != generationID ||
			(state.Generations[generationIndex].State != GenerationStateValidating &&
				state.Generations[generationIndex].State != GenerationStateImporting) {
			return newLibraryError(ErrorInvalidState, generationID, 0, nil)
		}
		generation := &state.Generations[generationIndex]
		generation.ActivityCount = analytics.ActivityCount
		generation.UniqueTitleCount = analytics.UniqueTitleCount
		generation.StartDate = analytics.StartDate
		generation.EndDate = analytics.EndDate
		generation.RecordsSHA256 = ""
		generation.AnalyticsSHA256 = ""
		if generation.State == GenerationStateValidating {
			transitionGeneration(
				generation,
				GenerationStateImporting,
				nowMilliseconds,
				nil,
			)
		} else {
			generation.UpdatedAtMS = nowMilliseconds
			latestEvent := &generation.Events[len(generation.Events)-1]
			latestEvent.ActivityCount = generation.ActivityCount
			latestEvent.UniqueTitleCount = generation.UniqueTitleCount
			latestEvent.OccurredAtMS = nowMilliseconds
		}
		return nil
	})
}

func (workspace *Workspace) recordCheckpoint(
	generationID string,
	checkpoint artifactCheckpoint,
) error {
	workspace.mutex.Lock()
	defer workspace.mutex.Unlock()
	return workspace.repository.mutate(func(state *repositoryState) error {
		generationIndex, found := findGenerationIndex(*state, generationID)
		if !found ||
			state.BuildingID != generationID ||
			(state.Generations[generationIndex].State != GenerationStateImporting &&
				state.Generations[generationIndex].State != GenerationStateEnriching) {
			return newLibraryError(ErrorInvalidState, generationID, 0, nil)
		}
		generation := &state.Generations[generationIndex]
		if generation.ActivityCount != checkpoint.activityCount ||
			generation.UniqueTitleCount != checkpoint.uniqueTitleCount ||
			generation.StartDate != checkpoint.startDate ||
			generation.EndDate != checkpoint.endDate {
			return newLibraryError(
				ErrorIncomplete,
				generationID,
				0,
				errors.New("artifact checkpoint summary changed during import"),
			)
		}
		generation.RecordsSHA256 = checkpoint.recordsSHA256
		generation.AnalyticsSHA256 = checkpoint.analyticsSHA256
		return nil
	})
}

func (workspace *Workspace) activateGeneration(generationID string) error {
	workspace.mutex.Lock()
	defer workspace.mutex.Unlock()
	nowMilliseconds := workspace.now().UTC().UnixMilli()
	return workspace.repository.mutate(func(state *repositoryState) error {
		generationIndex, found := findGenerationIndex(*state, generationID)
		if !found ||
			state.BuildingID != generationID ||
			(state.Generations[generationIndex].State != GenerationStateImporting &&
				state.Generations[generationIndex].State != GenerationStateEnriching) {
			return newLibraryError(ErrorInvalidState, generationID, 0, nil)
		}
		generation := &state.Generations[generationIndex]
		if !validSHA256(generation.RecordsSHA256) ||
			!validSHA256(generation.AnalyticsSHA256) {
			return newLibraryError(ErrorIncomplete, generationID, 0, nil)
		}
		transitionGeneration(
			generation,
			GenerationStateReady,
			nowMilliseconds,
			nil,
		)
		generation.CompletedAtMS = nowMilliseconds
		state.ActiveID = generationID
		state.BuildingID = ""
		return nil
	})
}

func (workspace *Workspace) failGeneration(
	generationID string,
	failure *Error,
) error {
	if failure == nil {
		failure = newLibraryError(ErrorIncomplete, generationID, 0, nil)
	}
	removalError := removeGenerationDirectory(workspace.root, generationID)
	workspace.mutex.Lock()
	defer workspace.mutex.Unlock()
	if workspace.closing || workspace.context.Err() != nil {
		return nil
	}
	nowMilliseconds := workspace.now().UTC().UnixMilli()
	mutationError := workspace.repository.mutate(func(state *repositoryState) error {
		generationIndex, found := findGenerationIndex(*state, generationID)
		if !found {
			return nil
		}
		generation := &state.Generations[generationIndex]
		if generation.State == GenerationStateReady ||
			generation.State == GenerationStateFailed {
			return nil
		}
		persistedFailure := &Failure{
			Code: failure.Code(),
			Row:  failure.Row(),
		}
		generation.RecordsSHA256 = ""
		generation.AnalyticsSHA256 = ""
		generation.EnrichmentCheckpointBytes = 0
		transitionGeneration(
			generation,
			GenerationStateFailed,
			nowMilliseconds,
			persistedFailure,
		)
		generation.CompletedAtMS = nowMilliseconds
		if state.BuildingID == generationID {
			state.BuildingID = ""
		}
		return nil
	})
	return errors.Join(removalError, mutationError)
}

func (workspace *Workspace) dispatchLocalImportLocked(generationID string) {
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
		runError := workspace.runLocalImport(jobContext, generationID)
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

func (workspace *Workspace) recoverDeletionCheckpoints() error {
	if workspace.repository.state.Deleting {
		if removalError := errors.Join(
			removeAllGenerationDirectories(workspace.root),
			removeTMDBCacheFiles(workspace.cacheFile),
		); removalError != nil {
			return removalError
		}
		if mutationError := workspace.repository.mutate(func(state *repositoryState) error {
			state.Deleting = false
			state.ActiveID = ""
			state.BuildingID = ""
			state.PendingDeletions = []string{}
			state.Generations = []generationState{}
			return nil
		}); mutationError != nil {
			return mutationError
		}
	}
	for _, generationID := range slices.Clone(workspace.repository.state.PendingDeletions) {
		if removalError := removeGenerationDirectory(workspace.root, generationID); removalError != nil {
			return removalError
		}
		if mutationError := workspace.repository.mutate(func(state *repositoryState) error {
			removeGenerationState(state, generationID)
			state.PendingDeletions = removeString(state.PendingDeletions, generationID)
			return nil
		}); mutationError != nil {
			return mutationError
		}
	}
	return nil
}

func (workspace *Workspace) recoverBuildingGeneration() error {
	buildingID := workspace.repository.state.BuildingID
	if buildingID == "" {
		return nil
	}
	generation, exists := findGeneration(workspace.repository.state, buildingID)
	if !exists {
		return newLibraryError(ErrorInvalidPersistence, buildingID, 0, nil)
	}
	if generation.AnalysisLevel == AnalysisLevelTMDB {
		if generation.State != GenerationStateEnriching {
			return newLibraryError(ErrorInvalidPersistence, buildingID, 0, nil)
		}
		workspace.mutex.Lock()
		workspace.dispatchTMDBEnrichmentLocked(buildingID)
		workspace.mutex.Unlock()
		return nil
	}
	if generation.State == GenerationStateReceiving {
		if cleanupError := removeStaleSourcePart(workspace.root, buildingID); cleanupError != nil {
			return cleanupError
		}
		sourceExists, sourceError := stagedSourceExists(workspace.root, buildingID)
		if sourceError != nil {
			return sourceError
		}
		if !sourceExists {
			return nil
		}
		nowMilliseconds := workspace.now().UTC().UnixMilli()
		if mutationError := workspace.repository.mutate(func(state *repositoryState) error {
			generationIndex, found := findGenerationIndex(*state, buildingID)
			if !found {
				return newLibraryError(ErrorInvalidPersistence, buildingID, 0, nil)
			}
			transitionGeneration(
				&state.Generations[generationIndex],
				GenerationStateValidating,
				nowMilliseconds,
				nil,
			)
			return nil
		}); mutationError != nil {
			return mutationError
		}
	}
	workspace.mutex.Lock()
	workspace.dispatchLocalImportLocked(buildingID)
	workspace.mutex.Unlock()
	return nil
}

func (workspace *Workspace) newGenerationIDLocked() (string, error) {
	for attempt := 0; attempt < 3; attempt++ {
		randomBytes := make([]byte, 16)
		if _, entropyError := io.ReadFull(workspace.entropy, randomBytes); entropyError != nil {
			return "", newLibraryError(
				ErrorPersistenceFailed,
				"",
				0,
				errors.New("generate opaque Netflix generation ID"),
			)
		}
		generationID := generationIDPrefix + hex.EncodeToString(randomBytes)
		if _, exists := findGeneration(workspace.repository.state, generationID); !exists {
			return generationID, nil
		}
	}
	return "", newLibraryError(
		ErrorConflict,
		"",
		0,
		errors.New("could not allocate a unique Netflix generation ID"),
	)
}

func (workspace *Workspace) buildingGeneration(
	generationID string,
) (generationState, error) {
	workspace.mutex.Lock()
	defer workspace.mutex.Unlock()
	generation, exists := findGeneration(workspace.repository.state, generationID)
	if !exists {
		return generationState{}, newLibraryError(ErrorNotFound, generationID, 0, nil)
	}
	if workspace.repository.state.BuildingID != generationID ||
		!isBuildingState(generation.State) {
		return generationState{}, newLibraryError(ErrorInvalidState, generationID, 0, nil)
	}
	return generation, nil
}

func (workspace *Workspace) readyGeneration(
	generationID string,
) (generationState, error) {
	workspace.mutex.Lock()
	defer workspace.mutex.Unlock()
	if workspace.repository.state.Deleting {
		return generationState{}, newLibraryError(
			ErrorConflict,
			generationID,
			0,
			errors.New("netflix provider deletion is in progress"),
		)
	}
	generation, exists := findGeneration(workspace.repository.state, generationID)
	if !exists {
		return generationState{}, newLibraryError(ErrorNotFound, generationID, 0, nil)
	}
	if generation.State != GenerationStateReady {
		return generationState{}, newLibraryError(
			ErrorInvalidState,
			generationID,
			0,
			errors.New("generation is not ready"),
		)
	}
	return generation, nil
}

func transitionGeneration(
	generation *generationState,
	nextState GenerationState,
	nowMilliseconds int64,
	failure *Failure,
) {
	generation.State = nextState
	generation.UpdatedAtMS = nowMilliseconds
	generation.Failure = cloneFailure(failure)
	totalTitleCount := 0
	if generation.AnalysisLevel == AnalysisLevelTMDB {
		totalTitleCount = generation.UniqueTitleCount
	}
	generation.Events = append(generation.Events, eventState{
		Sequence:            int64(len(generation.Events) + 1),
		State:               nextState,
		ActivityCount:       generation.ActivityCount,
		UniqueTitleCount:    generation.UniqueTitleCount,
		OccurredAtMS:        nowMilliseconds,
		Failure:             cloneFailure(failure),
		CompletedTitleCount: generation.CompletedTitleCount,
		TotalTitleCount:     totalTitleCount,
		MatchedTitleCount:   generation.MatchedTitleCount,
		ReviewTitleCount:    generation.ReviewTitleCount,
		UnmatchedTitleCount: generation.UnmatchedTitleCount,
		CacheHitTitleCount:  generation.CacheHitTitleCount,
		ProgressPercent: progressPercent(
			generation.CompletedTitleCount,
			totalTitleCount,
		),
	})
}

func findGeneration(
	state repositoryState,
	generationID string,
) (generationState, bool) {
	generationIndex, found := findGenerationIndex(state, generationID)
	if !found {
		return generationState{}, false
	}
	return state.Generations[generationIndex], true
}

func findGenerationIndex(
	state repositoryState,
	generationID string,
) (int, bool) {
	for generationIndex, generation := range state.Generations {
		if generation.ID == generationID {
			return generationIndex, true
		}
	}
	return 0, false
}

func latestFailedGeneration(
	state repositoryState,
) (generationState, bool) {
	var latest generationState
	found := false
	for _, generation := range state.Generations {
		if generation.State != GenerationStateFailed {
			continue
		}
		if !found ||
			generation.UpdatedAtMS > latest.UpdatedAtMS ||
			(generation.UpdatedAtMS == latest.UpdatedAtMS &&
				generation.ID > latest.ID) {
			latest = generation
			found = true
		}
	}
	return latest, found
}

func removeGenerationState(state *repositoryState, generationID string) {
	for generationIndex, generation := range state.Generations {
		if generation.ID == generationID {
			state.Generations = append(
				state.Generations[:generationIndex],
				state.Generations[generationIndex+1:]...,
			)
			break
		}
	}
	if state.ActiveID == generationID {
		state.ActiveID = ""
	}
	if state.BuildingID == generationID {
		state.BuildingID = ""
	}
}

func removeString(values []string, target string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != target {
			result = append(result, value)
		}
	}
	return result
}

func encodeRecordCursor(
	generationID string,
	afterIndex int64,
	filter ActivityFilter,
) string {
	payload := strings.Join(
		[]string{
			recordCursorIdentity,
			generationID,
			strconv.FormatInt(afterIndex, 10),
			filter.StartDate,
			filter.EndDate,
			string(filter.MatchStatus),
		},
		"\x00",
	)
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeRecordCursor(
	cursor string,
) (string, int64, ActivityFilter, error) {
	if len(cursor) == 0 || len(cursor) > 512 {
		return "", 0, ActivityFilter{}, errors.New("record cursor length is invalid")
	}
	decoded, decodeError := base64.RawURLEncoding.DecodeString(cursor)
	if decodeError != nil {
		return "", 0, ActivityFilter{}, decodeError
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 6 ||
		parts[0] != recordCursorIdentity ||
		!validGenerationID(parts[1]) {
		return "", 0, ActivityFilter{}, errors.New("record cursor identity is invalid")
	}
	afterIndex, parseError := strconv.ParseInt(parts[2], 10, 64)
	if parseError != nil || afterIndex <= 0 || afterIndex > product.MaxNetflixViewingRows {
		return "", 0, ActivityFilter{}, errors.New("record cursor position is invalid")
	}
	filter := ActivityFilter{
		StartDate:   parts[3],
		EndDate:     parts[4],
		MatchStatus: netflix.MatchStatus(parts[5]),
	}
	normalized, _, filterError := normalizeActivityFilter(parts[1], filter)
	if filterError != nil || normalized != filter {
		return "", 0, ActivityFilter{}, errors.New("record cursor filter is invalid")
	}
	return parts[1], afterIndex, filter, nil
}

func normalizeActivityFilter(
	generationID string,
	filter ActivityFilter,
) (ActivityFilter, netflix.DateRange, error) {
	if filter.MatchStatus != "" &&
		filter.MatchStatus != netflix.MatchStatusMatched &&
		filter.MatchStatus != netflix.MatchStatusReview &&
		filter.MatchStatus != netflix.MatchStatusUnmatched {
		return ActivityFilter{}, netflix.DateRange{}, newLibraryError(
			ErrorInvalidRequest,
			generationID,
			0,
			errors.New("activity match-status filter is invalid"),
		)
	}
	dateRange := netflix.AllDates()
	if filter.StartDate == "" && filter.EndDate == "" {
		return filter, dateRange, nil
	}
	if filter.StartDate == "" || filter.EndDate == "" {
		return ActivityFilter{}, netflix.DateRange{}, newLibraryError(
			ErrorInvalidRequest,
			generationID,
			0,
			errors.New("activity start and end dates are required together"),
		)
	}
	start, startError := netflix.ParseISODate(filter.StartDate)
	end, endError := netflix.ParseISODate(filter.EndDate)
	if startError != nil || endError != nil {
		return ActivityFilter{}, netflix.DateRange{}, newLibraryError(
			ErrorInvalidDate,
			generationID,
			0,
			errors.Join(startError, endError),
		)
	}
	var rangeError error
	dateRange, rangeError = netflix.NewDateRange(start, end)
	if rangeError != nil {
		return ActivityFilter{}, netflix.DateRange{}, newLibraryError(
			ErrorInvalidDate,
			generationID,
			0,
			rangeError,
		)
	}
	return filter, dateRange, nil
}

func filterActivityRecords(
	records []netflix.ActivityRecord,
	matchStatus netflix.MatchStatus,
) []netflix.ActivityRecord {
	if matchStatus == "" {
		return records
	}
	filtered := make([]netflix.ActivityRecord, 0, len(records))
	for _, record := range records {
		match, hasMatch := record.Match()
		if hasMatch && match.Status() == matchStatus {
			filtered = append(filtered, record)
		}
	}
	return filtered
}
