package library

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/privatepath"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
)

const maxRepositoryStateBytes int64 = 8 * 1024 * 1024

type repository struct {
	stateFile privatepath.File
	state     repositoryState
}

type providerLease struct {
	file *os.File
}

func acquireProviderLease(file privatepath.File) (*providerLease, error) {
	if file.RelativePath() != filepath.FromSlash(product.NetflixLibraryLeaseRelativePath) {
		return nil, newLibraryError(
			ErrorInvalidPersistence,
			"",
			0,
			errors.New("netflix provider lease path does not match the current contract"),
		)
	}
	if prepareError := file.Prepare(); prepareError != nil {
		return nil, newLibraryError(ErrorPersistenceFailed, "", 0, prepareError)
	}
	fileHandle, openError := os.OpenFile(file.Path(), os.O_RDWR, 0)
	if openError != nil {
		return nil, newLibraryError(ErrorPersistenceFailed, "", 0, openError)
	}
	if lockError := syscall.Flock(
		int(fileHandle.Fd()),
		syscall.LOCK_EX|syscall.LOCK_NB,
	); lockError != nil {
		_ = fileHandle.Close()
		if errors.Is(lockError, syscall.EWOULDBLOCK) ||
			errors.Is(lockError, syscall.EAGAIN) {
			return nil, newLibraryError(
				ErrorLeaseUnavailable,
				"",
				0,
				errors.New("another process owns the Netflix provider lease"),
			)
		}
		return nil, newLibraryError(ErrorPersistenceFailed, "", 0, lockError)
	}
	return &providerLease{file: fileHandle}, nil
}

func (lease *providerLease) close() error {
	if lease == nil || lease.file == nil {
		return nil
	}
	unlockError := syscall.Flock(int(lease.file.Fd()), syscall.LOCK_UN)
	closeError := lease.file.Close()
	lease.file = nil
	return errors.Join(unlockError, closeError)
}

func openRepository(stateFile privatepath.File) (*repository, error) {
	if stateFile.RelativePath() != filepath.FromSlash(product.NetflixLibraryStateRelativePath) {
		return nil, newLibraryError(
			ErrorInvalidPersistence,
			"",
			0,
			errors.New("netflix provider state path does not match the current contract"),
		)
	}
	if prepareError := stateFile.Prepare(); prepareError != nil {
		return nil, newLibraryError(ErrorPersistenceFailed, "", 0, prepareError)
	}
	if cleanupError := removeAtomicSibling(stateFile); cleanupError != nil {
		return nil, cleanupError
	}
	pathInfo, statError := os.Stat(stateFile.Path())
	if statError != nil {
		return nil, newLibraryError(ErrorPersistenceFailed, "", 0, statError)
	}
	repositoryValue := &repository{stateFile: stateFile}
	if pathInfo.Size() == 0 {
		repositoryValue.state = newRepositoryState()
		if writeError := writeRepositoryState(stateFile, repositoryValue.state); writeError != nil {
			return nil, writeError
		}
		return repositoryValue, nil
	}
	state, readError := readRepositoryState(stateFile)
	if readError != nil {
		return nil, readError
	}
	repositoryValue.state = state
	return repositoryValue, nil
}

func newRepositoryState() repositoryState {
	return repositoryState{
		SchemaOwner:      stateSchemaOwner,
		SchemaVersion:    stateSchemaVersion,
		SchemaContract:   stateSchemaContract,
		Revision:         1,
		PendingDeletions: []string{},
		Generations:      []generationState{},
	}
}

func (repositoryValue *repository) mutate(
	mutation func(*repositoryState) error,
) error {
	if repositoryValue == nil || mutation == nil {
		return newLibraryError(
			ErrorPersistenceFailed,
			"",
			0,
			errors.New("netflix repository mutation is not initialized"),
		)
	}
	nextState := cloneRepositoryState(repositoryValue.state)
	if mutationError := mutation(&nextState); mutationError != nil {
		return mutationError
	}
	nextState.Revision = repositoryValue.state.Revision + 1
	if validationError := validateRepositoryState(nextState); validationError != nil {
		return validationError
	}
	if writeError := writeRepositoryState(repositoryValue.stateFile, nextState); writeError != nil {
		return writeError
	}
	repositoryValue.state = nextState
	return nil
}

func cloneRepositoryState(state repositoryState) repositoryState {
	cloned := state
	cloned.PendingDeletions = slices.Clone(state.PendingDeletions)
	cloned.Generations = make([]generationState, len(state.Generations))
	for generationIndex, generation := range state.Generations {
		cloned.Generations[generationIndex] = generation
		cloned.Generations[generationIndex].Failure = cloneFailure(generation.Failure)
		cloned.Generations[generationIndex].Events = make(
			[]eventState,
			len(generation.Events),
		)
		for eventIndex, event := range generation.Events {
			cloned.Generations[generationIndex].Events[eventIndex] = event
			cloned.Generations[generationIndex].Events[eventIndex].Failure = cloneFailure(event.Failure)
		}
	}
	return cloned
}

func readRepositoryState(file privatepath.File) (repositoryState, error) {
	fileHandle, openError := os.Open(file.Path())
	if openError != nil {
		return repositoryState{}, newLibraryError(
			ErrorPersistenceFailed,
			"",
			0,
			openError,
		)
	}
	defer fileHandle.Close()
	limitedReader := io.LimitReader(fileHandle, maxRepositoryStateBytes+1)
	encoded, readError := io.ReadAll(limitedReader)
	if readError != nil {
		return repositoryState{}, newLibraryError(
			ErrorPersistenceFailed,
			"",
			0,
			readError,
		)
	}
	if len(encoded) == 0 || int64(len(encoded)) > maxRepositoryStateBytes {
		return repositoryState{}, newLibraryError(
			ErrorInvalidPersistence,
			"",
			0,
			errors.New("netflix repository state size is outside the current bound"),
		)
	}
	var state repositoryState
	if decodeError := decodeStrictJSON(encoded, &state); decodeError != nil {
		return repositoryState{}, newLibraryError(
			ErrorInvalidPersistence,
			"",
			0,
			decodeError,
		)
	}
	if validationError := validateRepositoryState(state); validationError != nil {
		return repositoryState{}, validationError
	}
	return state, nil
}

func writeRepositoryState(file privatepath.File, state repositoryState) error {
	if validationError := validateRepositoryState(state); validationError != nil {
		return validationError
	}
	encoded, encodeError := json.MarshalIndent(state, "", "  ")
	if encodeError != nil {
		return newLibraryError(ErrorPersistenceFailed, "", 0, encodeError)
	}
	encoded = append(encoded, '\n')
	if int64(len(encoded)) > maxRepositoryStateBytes {
		return newLibraryError(
			ErrorLimitExceeded,
			"",
			0,
			errors.New("netflix repository state exceeds its byte limit"),
		)
	}
	return writePrivateFileAtomic(file, encoded)
}

func writePrivateFileAtomic(file privatepath.File, contents []byte) error {
	if prepareError := file.Prepare(); prepareError != nil {
		return newLibraryError(ErrorPersistenceFailed, "", 0, prepareError)
	}
	temporaryFile, siblingError := atomicSibling(file)
	if siblingError != nil {
		return siblingError
	}
	if prepareError := temporaryFile.Prepare(); prepareError != nil {
		return newLibraryError(ErrorPersistenceFailed, "", 0, prepareError)
	}
	fileHandle, openError := os.OpenFile(
		temporaryFile.Path(),
		os.O_WRONLY|os.O_TRUNC,
		0,
	)
	if openError != nil {
		return newLibraryError(ErrorPersistenceFailed, "", 0, openError)
	}
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryFile.Path())
		}
	}()
	if _, writeError := fileHandle.Write(contents); writeError != nil {
		_ = fileHandle.Close()
		return newLibraryError(ErrorPersistenceFailed, "", 0, writeError)
	}
	if syncError := fileHandle.Sync(); syncError != nil {
		_ = fileHandle.Close()
		return newLibraryError(ErrorPersistenceFailed, "", 0, syncError)
	}
	if closeError := fileHandle.Close(); closeError != nil {
		return newLibraryError(ErrorPersistenceFailed, "", 0, closeError)
	}
	if renameError := os.Rename(temporaryFile.Path(), file.Path()); renameError != nil {
		return newLibraryError(ErrorPersistenceFailed, "", 0, renameError)
	}
	removeTemporary = false
	if directorySyncError := syncParentDirectory(file.Path()); directorySyncError != nil {
		return newLibraryError(ErrorPersistenceFailed, "", 0, directorySyncError)
	}
	return nil
}

func syncParentDirectory(path string) error {
	directoryHandle, openError := os.Open(filepath.Dir(path))
	if openError != nil {
		return openError
	}
	defer directoryHandle.Close()
	return directoryHandle.Sync()
}

func atomicSibling(file privatepath.File) (privatepath.File, error) {
	sibling, siblingError := file.Sibling(filepath.Base(file.Path()) + ".next")
	if siblingError != nil {
		return privatepath.File{}, newLibraryError(
			ErrorInvalidPersistence,
			"",
			0,
			siblingError,
		)
	}
	return sibling, nil
}

func removeAtomicSibling(file privatepath.File) error {
	sibling, siblingError := atomicSibling(file)
	if siblingError != nil {
		return siblingError
	}
	if removeError := os.Remove(sibling.Path()); removeError != nil &&
		!errors.Is(removeError, os.ErrNotExist) {
		return newLibraryError(ErrorPersistenceFailed, "", 0, removeError)
	}
	return nil
}

func validateRepositoryState(state repositoryState) error {
	if state.SchemaOwner != stateSchemaOwner ||
		state.SchemaVersion != stateSchemaVersion ||
		state.SchemaContract != stateSchemaContract {
		return newLibraryError(
			ErrorInvalidPersistence,
			"",
			0,
			errors.New("netflix repository does not declare the exact current schema"),
		)
	}
	if state.Revision <= 0 ||
		state.PendingDeletions == nil ||
		state.Generations == nil {
		return invalidStatePersistence("repository collections or revision are invalid")
	}
	if len(state.Generations) > product.MaxNetflixGenerationHistory {
		return invalidStatePersistence("generation history exceeds its current bound")
	}

	generationByID := make(map[string]generationState, len(state.Generations))
	buildingGenerationIDs := make([]string, 0, 1)
	for generationIndex, generation := range state.Generations {
		if validationError := validateGenerationState(generation); validationError != nil {
			return fmt.Errorf("validate persisted generation %d: %w", generationIndex+1, validationError)
		}
		if _, duplicate := generationByID[generation.ID]; duplicate {
			return invalidStatePersistence("generation IDs are not unique")
		}
		generationByID[generation.ID] = generation
		if isBuildingState(generation.State) {
			buildingGenerationIDs = append(buildingGenerationIDs, generation.ID)
		}
	}
	for _, generation := range state.Generations {
		if generation.AnalysisLevel == AnalysisLevelLocal {
			if generation.SourceGenerationID != "" {
				return invalidStatePersistence("local generation declares a source generation")
			}
			continue
		}
		sourceGeneration, sourceExists := generationByID[generation.SourceGenerationID]
		if !sourceExists || sourceGeneration.State != GenerationStateReady {
			return invalidStatePersistence("TMDB generation source is missing or not ready")
		}
	}
	if state.ActiveID != "" {
		active, exists := generationByID[state.ActiveID]
		if !exists || active.State != GenerationStateReady {
			return invalidStatePersistence("active generation is missing or not ready")
		}
	}
	if state.BuildingID != "" {
		building, exists := generationByID[state.BuildingID]
		if !exists || !isBuildingState(building.State) {
			return invalidStatePersistence("building generation is missing or terminal")
		}
	}
	if state.ActiveID != "" && state.ActiveID == state.BuildingID {
		return invalidStatePersistence("active and building generations must differ")
	}

	pendingSet := make(map[string]struct{}, len(state.PendingDeletions))
	for _, generationID := range state.PendingDeletions {
		if !validGenerationID(generationID) {
			return invalidStatePersistence("pending deletion has an invalid generation ID")
		}
		if _, duplicate := pendingSet[generationID]; duplicate {
			return invalidStatePersistence("pending deletion IDs are not unique")
		}
		if generationID == state.ActiveID || generationID == state.BuildingID {
			return invalidStatePersistence("active or building generation cannot be pending deletion")
		}
		if _, exists := generationByID[generationID]; !exists {
			return invalidStatePersistence("pending deletion generation is missing")
		}
		pendingSet[generationID] = struct{}{}
	}
	if !state.Deleting {
		expectedBuildingIDs := make([]string, 0, 1)
		for _, generationID := range buildingGenerationIDs {
			if _, pending := pendingSet[generationID]; !pending {
				expectedBuildingIDs = append(expectedBuildingIDs, generationID)
			}
		}
		if len(expectedBuildingIDs) > 1 ||
			(len(expectedBuildingIDs) == 0 && state.BuildingID != "") ||
			(len(expectedBuildingIDs) == 1 &&
				state.BuildingID != expectedBuildingIDs[0]) {
			return invalidStatePersistence("building pointer is inconsistent")
		}
	}
	return nil
}

func validateGenerationState(generation generationState) error {
	if !validGenerationID(generation.ID) {
		return invalidStatePersistence("generation ID is invalid")
	}
	if generation.SourceGenerationID != "" &&
		(!validGenerationID(generation.SourceGenerationID) ||
			generation.SourceGenerationID == generation.ID) {
		return invalidStatePersistence("source generation ID is invalid")
	}
	if generation.AnalysisLevel != AnalysisLevelLocal &&
		generation.AnalysisLevel != AnalysisLevelTMDB {
		return invalidStatePersistence("analysis level is invalid")
	}
	if !validGenerationState(generation.State) {
		return invalidStatePersistence("lifecycle state is invalid")
	}
	if generation.CreatedAtMS <= 0 ||
		generation.UpdatedAtMS < generation.CreatedAtMS ||
		generation.CompletedAtMS < 0 ||
		generation.ActivityCount < 0 ||
		generation.UniqueTitleCount < 0 ||
		generation.UniqueTitleCount > generation.ActivityCount ||
		generation.ActivityCount > product.MaxNetflixViewingRows ||
		generation.UniqueTitleCount > product.MaxNetflixUniqueTitles {
		return invalidStatePersistence("generation counts or timestamps are invalid")
	}
	if len(generation.Events) == 0 ||
		len(generation.Events) > product.MaxNetflixProgressEvents {
		return invalidStatePersistence("generation event journal is invalid")
	}
	for eventIndex, event := range generation.Events {
		if event.Sequence != int64(eventIndex+1) ||
			!validGenerationState(event.State) ||
			event.OccurredAtMS < generation.CreatedAtMS ||
			event.ActivityCount < 0 ||
			event.UniqueTitleCount < 0 ||
			event.UniqueTitleCount > event.ActivityCount {
			return invalidStatePersistence("generation event is invalid")
		}
		if event.Failure != nil {
			if event.State != GenerationStateFailed ||
				!validFailure(*event.Failure) {
				return invalidStatePersistence("generation event failure is invalid")
			}
		}
		if eventIndex > 0 &&
			!validLifecycleTransition(
				generation.Events[eventIndex-1].State,
				event.State,
			) {
			return invalidStatePersistence("generation event transition is invalid")
		}
	}
	if generation.AnalysisLevel == AnalysisLevelLocal {
		if generation.SourceGenerationID != "" ||
			generation.Events[0].State != GenerationStateReceiving ||
			generation.State == GenerationStateEnriching {
			return invalidStatePersistence("local generation lifecycle is invalid")
		}
	} else if generation.SourceGenerationID == "" ||
		generation.Events[0].State != GenerationStateEnriching ||
		generation.State == GenerationStateReceiving ||
		generation.State == GenerationStateValidating ||
		generation.State == GenerationStateImporting {
		return invalidStatePersistence("TMDB generation lifecycle is invalid")
	}
	lastEvent := generation.Events[len(generation.Events)-1]
	if lastEvent.State != generation.State ||
		lastEvent.ActivityCount != generation.ActivityCount ||
		lastEvent.UniqueTitleCount != generation.UniqueTitleCount {
		return invalidStatePersistence("latest event does not match generation state")
	}

	hasSummary := generation.ActivityCount > 0 &&
		generation.UniqueTitleCount > 0 &&
		generation.StartDate != "" &&
		generation.EndDate != ""
	hasHashes := validSHA256(generation.RecordsSHA256) &&
		validSHA256(generation.AnalyticsSHA256)
	switch generation.State {
	case GenerationStateReceiving, GenerationStateValidating:
		if generation.ActivityCount != 0 ||
			generation.UniqueTitleCount != 0 ||
			generation.StartDate != "" ||
			generation.EndDate != "" ||
			generation.RecordsSHA256 != "" ||
			generation.AnalyticsSHA256 != "" ||
			generation.CompletedAtMS != 0 ||
			generation.Failure != nil {
			return invalidStatePersistence("pre-import generation contains terminal data")
		}
	case GenerationStateImporting, GenerationStateEnriching:
		if generation.CompletedAtMS != 0 || generation.Failure != nil {
			return invalidStatePersistence("building generation contains terminal data")
		}
		if generation.ActivityCount == 0 {
			if generation.UniqueTitleCount != 0 ||
				generation.StartDate != "" ||
				generation.EndDate != "" ||
				generation.RecordsSHA256 != "" ||
				generation.AnalyticsSHA256 != "" {
				return invalidStatePersistence("empty building checkpoint is inconsistent")
			}
		} else if !hasSummary ||
			((generation.RecordsSHA256 == "") !=
				(generation.AnalyticsSHA256 == "")) ||
			(generation.RecordsSHA256 != "" && !hasHashes) {
			return invalidStatePersistence("building checkpoint is incomplete")
		}
	case GenerationStateReady:
		if !hasSummary ||
			!hasHashes ||
			generation.CompletedAtMS < generation.UpdatedAtMS ||
			generation.Failure != nil {
			return invalidStatePersistence("ready generation is incomplete")
		}
	case GenerationStateFailed:
		if generation.CompletedAtMS < generation.UpdatedAtMS ||
			generation.Failure == nil ||
			!validFailure(*generation.Failure) ||
			generation.RecordsSHA256 != "" ||
			generation.AnalyticsSHA256 != "" {
			return invalidStatePersistence("failed generation is inconsistent")
		}
	}
	return nil
}

func validLifecycleTransition(
	current GenerationState,
	next GenerationState,
) bool {
	switch current {
	case GenerationStateReceiving:
		return next == GenerationStateValidating ||
			next == GenerationStateFailed
	case GenerationStateValidating:
		return next == GenerationStateImporting ||
			next == GenerationStateFailed
	case GenerationStateImporting, GenerationStateEnriching:
		return next == GenerationStateReady ||
			next == GenerationStateFailed
	default:
		return false
	}
}

func validFailure(failure Failure) bool {
	if !validErrorCode(failure.Code) {
		return false
	}
	return failure.Row >= 0 && failure.Row <= product.MaxNetflixViewingRows+1
}

func validErrorCode(code ErrorCode) bool {
	switch code {
	case ErrorInvalidRequest,
		ErrorNotFound,
		ErrorConflict,
		ErrorInvalidState,
		ErrorUploadTooLarge,
		ErrorInvalidCSV,
		ErrorInvalidHeader,
		ErrorInvalidRow,
		ErrorInvalidTitle,
		ErrorInvalidDate,
		ErrorLimitExceeded,
		ErrorCanceled,
		ErrorIncomplete,
		ErrorLeaseUnavailable,
		ErrorPersistenceFailed,
		ErrorInvalidPersistence:
		return true
	default:
		return false
	}
}

func validGenerationState(state GenerationState) bool {
	switch state {
	case GenerationStateReceiving,
		GenerationStateValidating,
		GenerationStateImporting,
		GenerationStateEnriching,
		GenerationStateReady,
		GenerationStateFailed:
		return true
	default:
		return false
	}
}

func isBuildingState(state GenerationState) bool {
	return state == GenerationStateReceiving ||
		state == GenerationStateValidating ||
		state == GenerationStateImporting ||
		state == GenerationStateEnriching
}

func validGenerationID(generationID string) bool {
	if !strings.HasPrefix(generationID, generationIDPrefix) {
		return false
	}
	decoded, decodeError := hex.DecodeString(strings.TrimPrefix(generationID, generationIDPrefix))
	return decodeError == nil && len(decoded) == 16
}

func validSHA256(value string) bool {
	decoded, decodeError := hex.DecodeString(value)
	return decodeError == nil && len(decoded) == 32
}

func invalidStatePersistence(reason string) error {
	return newLibraryError(
		ErrorInvalidPersistence,
		"",
		0,
		errors.New("netflix repository "+reason),
	)
}

func decodeStrictJSON(encoded []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decodeError := decoder.Decode(destination); decodeError != nil {
		return decodeError
	}
	var trailing any
	if trailingError := decoder.Decode(&trailing); trailingError == nil {
		return errors.New("trailing JSON value is not allowed")
	} else if !errors.Is(trailingError, io.EOF) {
		return trailingError
	}
	return nil
}

func sortGenerations(generations []generationState) {
	sort.Slice(generations, func(leftIndex int, rightIndex int) bool {
		if generations[leftIndex].CreatedAtMS == generations[rightIndex].CreatedAtMS {
			return generations[leftIndex].ID < generations[rightIndex].ID
		}
		return generations[leftIndex].CreatedAtMS < generations[rightIndex].CreatedAtMS
	})
}
