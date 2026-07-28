package library

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/privatepath"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
)

const (
	generationsRelativeDirectory = "providers/netflix/generations"
	sourceFileName               = "viewing-activity.csv"
	sourcePartFileName           = "viewing-activity.csv.part"
	recordsFileName              = "records.jsonl"
	analyticsFileName            = "analytics.json"
	maxAnalyticsArtifactBytes    = 8 * 1024 * 1024
	maxRecordLineBytes           = product.MaxNetflixTitleBytes*3 + 32*1024
)

type generationFiles struct {
	directoryPath string
	source        privatepath.File
	sourcePart    privatepath.File
	records       privatepath.File
	analytics     privatepath.File
}

type artifactCheckpoint struct {
	activityCount    int
	uniqueTitleCount int
	startDate        string
	endDate          string
	recordsSHA256    string
	analyticsSHA256  string
}

func resolveGenerationFiles(
	root privatepath.Root,
	generationID string,
) (generationFiles, error) {
	if !validGenerationID(generationID) {
		return generationFiles{}, newLibraryError(
			ErrorInvalidRequest,
			generationID,
			0,
			errors.New("current generation ID is required"),
		)
	}
	generationDirectory := filepath.Join(
		filepath.FromSlash(generationsRelativeDirectory),
		generationID,
	)
	source, sourceError := root.File(filepath.Join(generationDirectory, sourceFileName))
	if sourceError != nil {
		return generationFiles{}, newLibraryError(
			ErrorInvalidPersistence,
			generationID,
			0,
			sourceError,
		)
	}
	sourcePart, sourcePartError := root.File(
		filepath.Join(generationDirectory, sourcePartFileName),
	)
	if sourcePartError != nil {
		return generationFiles{}, newLibraryError(
			ErrorInvalidPersistence,
			generationID,
			0,
			sourcePartError,
		)
	}
	records, recordsError := root.File(filepath.Join(generationDirectory, recordsFileName))
	if recordsError != nil {
		return generationFiles{}, newLibraryError(
			ErrorInvalidPersistence,
			generationID,
			0,
			recordsError,
		)
	}
	analytics, analyticsError := root.File(
		filepath.Join(generationDirectory, analyticsFileName),
	)
	if analyticsError != nil {
		return generationFiles{}, newLibraryError(
			ErrorInvalidPersistence,
			generationID,
			0,
			analyticsError,
		)
	}
	return generationFiles{
		directoryPath: filepath.Join(root.Path(), generationDirectory),
		source:        source,
		sourcePart:    sourcePart,
		records:       records,
		analytics:     analytics,
	}, nil
}

func stageViewingActivity(
	ctx context.Context,
	root privatepath.Root,
	generationID string,
	source io.Reader,
) error {
	if ctx == nil || source == nil {
		return newLibraryError(
			ErrorInvalidRequest,
			generationID,
			0,
			errors.New("upload context and body are required"),
		)
	}
	files, filesError := resolveGenerationFiles(root, generationID)
	if filesError != nil {
		return filesError
	}
	if _, directoryError := root.EnsureDirectory(filepath.Join(
		filepath.FromSlash(generationsRelativeDirectory),
		generationID,
	)); directoryError != nil {
		return newLibraryError(ErrorPersistenceFailed, generationID, 0, directoryError)
	}
	if exists, existsError := privateFileExists(files.source); existsError != nil {
		return existsError
	} else if exists {
		return newLibraryError(
			ErrorConflict,
			generationID,
			0,
			errors.New("generation already has a staged upload"),
		)
	}
	if removeError := removePrivateFile(files.sourcePart); removeError != nil {
		return removeError
	}
	if prepareError := files.sourcePart.Prepare(); prepareError != nil {
		return newLibraryError(ErrorPersistenceFailed, generationID, 0, prepareError)
	}
	fileHandle, openError := os.OpenFile(
		files.sourcePart.Path(),
		os.O_WRONLY|os.O_TRUNC,
		0,
	)
	if openError != nil {
		return newLibraryError(ErrorPersistenceFailed, generationID, 0, openError)
	}
	removePart := true
	defer func() {
		if removePart {
			_ = os.Remove(files.sourcePart.Path())
		}
	}()

	boundedReader := io.LimitReader(
		&contextReader{ctx: ctx, source: source},
		product.MaxNetflixViewingCSVBytes+1,
	)
	writtenBytes, copyError := io.CopyBuffer(
		fileHandle,
		boundedReader,
		make([]byte, 64*1024),
	)
	if copyError != nil {
		_ = fileHandle.Close()
		if ctx.Err() != nil {
			return newLibraryError(ErrorCanceled, generationID, 0, ctx.Err())
		}
		return newLibraryError(ErrorInvalidRequest, generationID, 0, copyError)
	}
	if writtenBytes > product.MaxNetflixViewingCSVBytes {
		_ = fileHandle.Close()
		return newLibraryError(
			ErrorUploadTooLarge,
			generationID,
			0,
			errors.New("viewing-activity upload exceeds the byte limit"),
		)
	}
	if writtenBytes == 0 {
		_ = fileHandle.Close()
		return newLibraryError(
			ErrorInvalidCSV,
			generationID,
			0,
			errors.New("viewing-activity upload is empty"),
		)
	}
	if syncError := fileHandle.Sync(); syncError != nil {
		_ = fileHandle.Close()
		return newLibraryError(ErrorPersistenceFailed, generationID, 0, syncError)
	}
	if closeError := fileHandle.Close(); closeError != nil {
		return newLibraryError(ErrorPersistenceFailed, generationID, 0, closeError)
	}
	if renameError := os.Rename(files.sourcePart.Path(), files.source.Path()); renameError != nil {
		return newLibraryError(ErrorPersistenceFailed, generationID, 0, renameError)
	}
	removePart = false
	if directorySyncError := syncParentDirectory(files.source.Path()); directorySyncError != nil {
		return newLibraryError(
			ErrorPersistenceFailed,
			generationID,
			0,
			directorySyncError,
		)
	}
	return nil
}

func openStagedSource(
	root privatepath.Root,
	generationID string,
) (*os.File, int64, error) {
	files, filesError := resolveGenerationFiles(root, generationID)
	if filesError != nil {
		return nil, 0, filesError
	}
	pathInfo, statError := os.Stat(files.source.Path())
	if statError != nil {
		if errors.Is(statError, os.ErrNotExist) {
			return nil, 0, newLibraryError(
				ErrorIncomplete,
				generationID,
				0,
				errors.New("staged viewing-activity upload is missing"),
			)
		}
		return nil, 0, newLibraryError(ErrorPersistenceFailed, generationID, 0, statError)
	}
	if prepareError := validateExistingPrivateFile(files.source); prepareError != nil {
		return nil, 0, prepareError
	}
	if pathInfo.Size() <= 0 || pathInfo.Size() > product.MaxNetflixViewingCSVBytes {
		return nil, 0, newLibraryError(
			ErrorIncomplete,
			generationID,
			0,
			errors.New("staged viewing-activity upload size is invalid"),
		)
	}
	fileHandle, openError := os.Open(files.source.Path())
	if openError != nil {
		return nil, 0, newLibraryError(ErrorPersistenceFailed, generationID, 0, openError)
	}
	return fileHandle, pathInfo.Size(), nil
}

func removeStagedSource(root privatepath.Root, generationID string) error {
	files, filesError := resolveGenerationFiles(root, generationID)
	if filesError != nil {
		return filesError
	}
	return errors.Join(
		removePrivateFile(files.source),
		removePrivateFile(files.sourcePart),
	)
}

func stagedSourceExists(root privatepath.Root, generationID string) (bool, error) {
	files, filesError := resolveGenerationFiles(root, generationID)
	if filesError != nil {
		return false, filesError
	}
	return privateFileExists(files.source)
}

func removeStaleSourcePart(root privatepath.Root, generationID string) error {
	files, filesError := resolveGenerationFiles(root, generationID)
	if filesError != nil {
		return filesError
	}
	return removePrivateFile(files.sourcePart)
}

func writeGenerationArtifacts(
	ctx context.Context,
	root privatepath.Root,
	generationID string,
	records []netflix.ActivityRecord,
	analytics netflix.Analytics,
	sourceBytes int64,
) (artifactCheckpoint, error) {
	if ctx == nil || len(records) == 0 {
		return artifactCheckpoint{}, newLibraryError(
			ErrorIncomplete,
			generationID,
			0,
			errors.New("complete local records are required"),
		)
	}
	if sourceBytes <= 0 || sourceBytes > product.MaxNetflixViewingCSVBytes {
		return artifactCheckpoint{}, newLibraryError(
			ErrorIncomplete,
			generationID,
			0,
			errors.New("source checkpoint size is invalid"),
		)
	}
	files, filesError := resolveGenerationFiles(root, generationID)
	if filesError != nil {
		return artifactCheckpoint{}, filesError
	}
	recordsHash, recordsBytes, recordsError := writeRecordsAtomic(
		ctx,
		files.records,
		generationID,
		records,
		product.MaxNetflixWorkingBytes-sourceBytes-maxAnalyticsArtifactBytes,
	)
	if recordsError != nil {
		return artifactCheckpoint{}, recordsError
	}
	analyticsPayload := persistedAnalytics{
		Contract:     analyticsContract,
		GenerationID: generationID,
		Data:         analytics,
	}
	encodedAnalytics, encodeError := json.Marshal(analyticsPayload)
	if encodeError != nil {
		return artifactCheckpoint{}, newLibraryError(
			ErrorPersistenceFailed,
			generationID,
			0,
			encodeError,
		)
	}
	encodedAnalytics = append(encodedAnalytics, '\n')
	if len(encodedAnalytics) > maxAnalyticsArtifactBytes ||
		sourceBytes+recordsBytes+int64(len(encodedAnalytics)) >
			product.MaxNetflixWorkingBytes {
		return artifactCheckpoint{}, newLibraryError(
			ErrorLimitExceeded,
			generationID,
			0,
			errors.New("generation working data exceeds the byte limit"),
		)
	}
	if writeError := writePrivateFileAtomic(files.analytics, encodedAnalytics); writeError != nil {
		return artifactCheckpoint{}, writeError
	}

	analyticsHash := sha256.Sum256(encodedAnalytics)
	return artifactCheckpoint{
		activityCount:    analytics.ActivityCount,
		uniqueTitleCount: analytics.UniqueTitleCount,
		startDate:        analytics.StartDate,
		endDate:          analytics.EndDate,
		recordsSHA256:    recordsHash,
		analyticsSHA256:  hex.EncodeToString(analyticsHash[:]),
	}, nil
}

func writeRecordsAtomic(
	ctx context.Context,
	file privatepath.File,
	generationID string,
	records []netflix.ActivityRecord,
	maximumBytes int64,
) (string, int64, error) {
	if maximumBytes <= 0 {
		return "", 0, newLibraryError(
			ErrorLimitExceeded,
			generationID,
			0,
			errors.New("records artifact has no working-byte budget"),
		)
	}
	if prepareError := file.Prepare(); prepareError != nil {
		return "", 0, newLibraryError(ErrorPersistenceFailed, generationID, 0, prepareError)
	}
	temporaryFile, siblingError := atomicSibling(file)
	if siblingError != nil {
		return "", 0, siblingError
	}
	if prepareError := temporaryFile.Prepare(); prepareError != nil {
		return "", 0, newLibraryError(ErrorPersistenceFailed, generationID, 0, prepareError)
	}
	fileHandle, openError := os.OpenFile(
		temporaryFile.Path(),
		os.O_WRONLY|os.O_TRUNC,
		0,
	)
	if openError != nil {
		return "", 0, newLibraryError(ErrorPersistenceFailed, generationID, 0, openError)
	}
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryFile.Path())
		}
	}()
	hashValue := sha256.New()
	countingWriter := &boundedHashWriter{
		destination: io.MultiWriter(fileHandle, hashValue),
		maximum:     maximumBytes,
	}
	bufferedWriter := bufio.NewWriterSize(countingWriter, 64*1024)
	encoder := json.NewEncoder(bufferedWriter)
	for recordIndex, record := range records {
		if contextError := ctx.Err(); contextError != nil {
			_ = fileHandle.Close()
			return "", 0, newLibraryError(
				ErrorCanceled,
				generationID,
				0,
				contextError,
			)
		}
		activity := record.Activity()
		if _, hasMetadata := record.Metadata(); hasMetadata {
			_ = fileHandle.Close()
			return "", 0, newLibraryError(
				ErrorIncomplete,
				generationID,
				0,
				errors.New("local generation record contains metadata"),
			)
		}
		identity := activity.TitleIdentity()
		payload := persistedRecord{
			Contract:             recordsContract,
			Index:                int64(recordIndex + 1),
			RawTitle:             activity.RawTitle(),
			RawDate:              activity.RawDate(),
			DateISO:              activity.Date().ISO(),
			DerivedTitle:         identity.SearchTitle(),
			TitleIdentity:        identity.Key(),
			TitleIdentityVersion: identity.Version(),
		}
		if encodeError := encoder.Encode(payload); encodeError != nil {
			_ = fileHandle.Close()
			code := ErrorPersistenceFailed
			if errors.Is(encodeError, errArtifactLimitExceeded) {
				code = ErrorLimitExceeded
			}
			return "", 0, newLibraryError(code, generationID, 0, encodeError)
		}
	}
	if flushError := bufferedWriter.Flush(); flushError != nil {
		_ = fileHandle.Close()
		code := ErrorPersistenceFailed
		if errors.Is(flushError, errArtifactLimitExceeded) {
			code = ErrorLimitExceeded
		}
		return "", 0, newLibraryError(code, generationID, 0, flushError)
	}
	if syncError := fileHandle.Sync(); syncError != nil {
		_ = fileHandle.Close()
		return "", 0, newLibraryError(ErrorPersistenceFailed, generationID, 0, syncError)
	}
	if closeError := fileHandle.Close(); closeError != nil {
		return "", 0, newLibraryError(ErrorPersistenceFailed, generationID, 0, closeError)
	}
	if renameError := os.Rename(temporaryFile.Path(), file.Path()); renameError != nil {
		return "", 0, newLibraryError(ErrorPersistenceFailed, generationID, 0, renameError)
	}
	removeTemporary = false
	if directorySyncError := syncParentDirectory(file.Path()); directorySyncError != nil {
		return "", 0, newLibraryError(
			ErrorPersistenceFailed,
			generationID,
			0,
			directorySyncError,
		)
	}
	return hex.EncodeToString(hashValue.Sum(nil)), countingWriter.written, nil
}

func validateGenerationArtifacts(
	ctx context.Context,
	root privatepath.Root,
	generation generationState,
) ([]netflix.ActivityRecord, netflix.Analytics, error) {
	files, filesError := resolveGenerationFiles(root, generation.ID)
	if filesError != nil {
		return nil, netflix.Analytics{}, filesError
	}
	recordsHash, hashError := hashPrivateFile(
		ctx,
		files.records,
		product.MaxNetflixWorkingBytes,
	)
	if hashError != nil {
		return nil, netflix.Analytics{}, hashError
	}
	if recordsHash != generation.RecordsSHA256 {
		return nil, netflix.Analytics{}, newLibraryError(
			ErrorIncomplete,
			generation.ID,
			0,
			errors.New("records artifact hash does not match its checkpoint"),
		)
	}
	analyticsBytes, analyticsReadError := readPrivateFileBounded(
		files.analytics,
		maxAnalyticsArtifactBytes,
	)
	if analyticsReadError != nil {
		return nil, netflix.Analytics{}, analyticsReadError
	}
	analyticsHash := sha256.Sum256(analyticsBytes)
	if hex.EncodeToString(analyticsHash[:]) != generation.AnalyticsSHA256 {
		return nil, netflix.Analytics{}, newLibraryError(
			ErrorIncomplete,
			generation.ID,
			0,
			errors.New("analytics artifact hash does not match its checkpoint"),
		)
	}
	var storedAnalytics persistedAnalytics
	if decodeError := decodeStrictJSON(analyticsBytes, &storedAnalytics); decodeError != nil {
		return nil, netflix.Analytics{}, newLibraryError(
			ErrorInvalidPersistence,
			generation.ID,
			0,
			decodeError,
		)
	}
	if storedAnalytics.Contract != analyticsContract ||
		storedAnalytics.GenerationID != generation.ID {
		return nil, netflix.Analytics{}, newLibraryError(
			ErrorInvalidPersistence,
			generation.ID,
			0,
			errors.New("analytics artifact identity is invalid"),
		)
	}
	records, recordsError := readAllRecords(ctx, files.records, generation.ID)
	if recordsError != nil {
		return nil, netflix.Analytics{}, recordsError
	}
	recalculated, aggregateError := netflix.Aggregate(
		ctx,
		records,
		netflix.AllDates(),
	)
	if aggregateError != nil {
		return nil, netflix.Analytics{}, newLibraryError(
			ErrorIncomplete,
			generation.ID,
			0,
			aggregateError,
		)
	}
	if len(records) != generation.ActivityCount ||
		recalculated.ActivityCount != generation.ActivityCount ||
		recalculated.UniqueTitleCount != generation.UniqueTitleCount ||
		recalculated.StartDate != generation.StartDate ||
		recalculated.EndDate != generation.EndDate ||
		!reflect.DeepEqual(recalculated, storedAnalytics.Data) {
		return nil, netflix.Analytics{}, newLibraryError(
			ErrorIncomplete,
			generation.ID,
			0,
			errors.New("generation records and analytics checkpoint are inconsistent"),
		)
	}
	return records, storedAnalytics.Data, nil
}

func readAllRecords(
	ctx context.Context,
	file privatepath.File,
	generationID string,
) ([]netflix.ActivityRecord, error) {
	fileHandle, openError := openValidatedPrivateFile(file)
	if openError != nil {
		return nil, openError
	}
	defer fileHandle.Close()
	scanner := bufio.NewScanner(fileHandle)
	scanner.Buffer(make([]byte, 64*1024), maxRecordLineBytes)
	records := make([]netflix.ActivityRecord, 0)
	for scanner.Scan() {
		if ctx == nil || ctx.Err() != nil {
			contextError := context.Canceled
			if ctx != nil {
				contextError = ctx.Err()
			}
			return nil, newLibraryError(ErrorCanceled, generationID, 0, contextError)
		}
		if len(records) == product.MaxNetflixViewingRows {
			return nil, newLibraryError(
				ErrorLimitExceeded,
				generationID,
				0,
				errors.New("persisted records exceed the row limit"),
			)
		}
		record, recordError := decodePersistedRecord(
			scanner.Bytes(),
			generationID,
			int64(len(records)+1),
		)
		if recordError != nil {
			return nil, recordError
		}
		records = append(records, record)
	}
	if scanError := scanner.Err(); scanError != nil {
		return nil, newLibraryError(
			ErrorInvalidPersistence,
			generationID,
			0,
			scanError,
		)
	}
	if len(records) == 0 {
		return nil, newLibraryError(
			ErrorIncomplete,
			generationID,
			0,
			errors.New("generation records artifact is empty"),
		)
	}
	return records, nil
}

func readActivityPage(
	ctx context.Context,
	root privatepath.Root,
	generationID string,
	afterIndex int64,
	limit int,
) ([]Activity, int64, error) {
	if ctx == nil ||
		afterIndex < 0 ||
		limit <= 0 ||
		limit > product.MaxNetflixRecordPageSize {
		return nil, 0, newLibraryError(
			ErrorInvalidRequest,
			generationID,
			0,
			errors.New("record page boundary is invalid"),
		)
	}
	files, filesError := resolveGenerationFiles(root, generationID)
	if filesError != nil {
		return nil, 0, filesError
	}
	fileHandle, openError := openValidatedPrivateFile(files.records)
	if openError != nil {
		return nil, 0, openError
	}
	defer fileHandle.Close()
	scanner := bufio.NewScanner(fileHandle)
	scanner.Buffer(make([]byte, 64*1024), maxRecordLineBytes)
	activities := make([]Activity, 0, limit+1)
	var scannedIndex int64
	for scanner.Scan() {
		if contextError := ctx.Err(); contextError != nil {
			return nil, 0, newLibraryError(
				ErrorCanceled,
				generationID,
				0,
				contextError,
			)
		}
		scannedIndex++
		if scannedIndex <= afterIndex {
			continue
		}
		var payload persistedRecord
		if decodeError := decodeStrictJSON(scanner.Bytes(), &payload); decodeError != nil {
			return nil, 0, newLibraryError(
				ErrorInvalidPersistence,
				generationID,
				0,
				decodeError,
			)
		}
		record, recordError := decodePersistedRecord(
			scanner.Bytes(),
			generationID,
			scannedIndex,
		)
		if recordError != nil {
			return nil, 0, recordError
		}
		activity := record.Activity()
		identity := activity.TitleIdentity()
		activities = append(activities, Activity{
			Index:                payload.Index,
			RawTitle:             activity.RawTitle(),
			RawDate:              activity.RawDate(),
			DateISO:              activity.Date().ISO(),
			DerivedTitle:         identity.SearchTitle(),
			TitleIdentity:        identity.Key(),
			TitleIdentityVersion: identity.Version(),
		})
		if len(activities) == limit+1 {
			break
		}
	}
	if scanError := scanner.Err(); scanError != nil {
		return nil, 0, newLibraryError(
			ErrorInvalidPersistence,
			generationID,
			0,
			scanError,
		)
	}
	nextAfter := int64(0)
	if len(activities) > limit {
		activities = activities[:limit]
		nextAfter = activities[len(activities)-1].Index
	}
	return activities, nextAfter, nil
}

func decodePersistedRecord(
	encoded []byte,
	generationID string,
	expectedIndex int64,
) (netflix.ActivityRecord, error) {
	var payload persistedRecord
	if decodeError := decodeStrictJSON(encoded, &payload); decodeError != nil {
		return netflix.ActivityRecord{}, newLibraryError(
			ErrorInvalidPersistence,
			generationID,
			0,
			decodeError,
		)
	}
	activity, activityError := netflix.NewViewingActivity(payload.RawTitle, payload.RawDate)
	if activityError != nil {
		return netflix.ActivityRecord{}, newLibraryError(
			ErrorInvalidPersistence,
			generationID,
			0,
			activityError,
		)
	}
	identity := activity.TitleIdentity()
	if payload.Contract != recordsContract ||
		payload.Index != expectedIndex ||
		payload.DateISO != activity.Date().ISO() ||
		payload.DerivedTitle != identity.SearchTitle() ||
		payload.TitleIdentity != identity.Key() ||
		payload.TitleIdentityVersion != identity.Version() {
		return netflix.ActivityRecord{}, newLibraryError(
			ErrorInvalidPersistence,
			generationID,
			0,
			errors.New("persisted local record does not match the current contract"),
		)
	}
	record, recordError := netflix.NewLocalActivityRecord(activity)
	if recordError != nil {
		return netflix.ActivityRecord{}, newLibraryError(
			ErrorInvalidPersistence,
			generationID,
			0,
			recordError,
		)
	}
	return record, nil
}

func readPrivateFileBounded(
	file privatepath.File,
	maximumBytes int,
) ([]byte, error) {
	fileHandle, openError := openValidatedPrivateFile(file)
	if openError != nil {
		return nil, openError
	}
	defer fileHandle.Close()
	encoded, readError := io.ReadAll(io.LimitReader(fileHandle, int64(maximumBytes)+1))
	if readError != nil {
		return nil, newLibraryError(ErrorPersistenceFailed, "", 0, readError)
	}
	if len(encoded) == 0 || len(encoded) > maximumBytes {
		return nil, newLibraryError(
			ErrorInvalidPersistence,
			"",
			0,
			errors.New("private artifact size is outside the current bound"),
		)
	}
	return encoded, nil
}

func hashPrivateFile(
	ctx context.Context,
	file privatepath.File,
	maximumBytes int64,
) (string, error) {
	fileHandle, openError := openValidatedPrivateFile(file)
	if openError != nil {
		return "", openError
	}
	defer fileHandle.Close()
	hashValue := sha256.New()
	reader := &contextReader{ctx: ctx, source: fileHandle}
	written, copyError := io.Copy(
		hashValue,
		io.LimitReader(reader, maximumBytes+1),
	)
	if copyError != nil {
		code := ErrorPersistenceFailed
		if ctx == nil || ctx.Err() != nil {
			code = ErrorCanceled
		}
		return "", newLibraryError(code, "", 0, copyError)
	}
	if written <= 0 || written > maximumBytes {
		return "", newLibraryError(
			ErrorInvalidPersistence,
			"",
			0,
			errors.New("private artifact size is outside the current bound"),
		)
	}
	return hex.EncodeToString(hashValue.Sum(nil)), nil
}

func openValidatedPrivateFile(file privatepath.File) (*os.File, error) {
	if validationError := validateExistingPrivateFile(file); validationError != nil {
		return nil, validationError
	}
	fileHandle, openError := os.Open(file.Path())
	if openError != nil {
		return nil, newLibraryError(ErrorPersistenceFailed, "", 0, openError)
	}
	return fileHandle, nil
}

func validateExistingPrivateFile(file privatepath.File) error {
	pathInfo, statError := os.Lstat(file.Path())
	if statError != nil {
		return newLibraryError(ErrorPersistenceFailed, "", 0, statError)
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 ||
		!pathInfo.Mode().IsRegular() ||
		pathInfo.Mode().Perm() != 0o600 {
		return newLibraryError(
			ErrorInvalidPersistence,
			"",
			0,
			errors.New("private artifact must be a regular owner-only file"),
		)
	}
	return nil
}

func privateFileExists(file privatepath.File) (bool, error) {
	pathInfo, statError := os.Lstat(file.Path())
	if errors.Is(statError, os.ErrNotExist) {
		return false, nil
	}
	if statError != nil {
		return false, newLibraryError(ErrorPersistenceFailed, "", 0, statError)
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 ||
		!pathInfo.Mode().IsRegular() ||
		pathInfo.Mode().Perm() != 0o600 {
		return false, newLibraryError(
			ErrorInvalidPersistence,
			"",
			0,
			errors.New("private artifact must be a regular owner-only file"),
		)
	}
	return true, nil
}

func removePrivateFile(file privatepath.File) error {
	removeError := os.Remove(file.Path())
	if removeError != nil && !errors.Is(removeError, os.ErrNotExist) {
		return newLibraryError(ErrorPersistenceFailed, "", 0, removeError)
	}
	return nil
}

func removeGenerationDirectory(root privatepath.Root, generationID string) error {
	files, filesError := resolveGenerationFiles(root, generationID)
	if filesError != nil {
		return filesError
	}
	expectedParent := filepath.Join(
		root.Path(),
		filepath.FromSlash(generationsRelativeDirectory),
	)
	if filepath.Dir(files.directoryPath) != expectedParent ||
		filepath.Base(files.directoryPath) != generationID {
		return newLibraryError(
			ErrorInvalidPersistence,
			generationID,
			0,
			errors.New("generation deletion path is outside the current contract"),
		)
	}
	if removeError := os.RemoveAll(files.directoryPath); removeError != nil {
		return newLibraryError(ErrorPersistenceFailed, generationID, 0, removeError)
	}
	return syncExistingParent(expectedParent)
}

func removeAllGenerationDirectories(root privatepath.Root) error {
	generationsPath := filepath.Join(
		root.Path(),
		filepath.FromSlash(generationsRelativeDirectory),
	)
	expectedParent := filepath.Join(root.Path(), "providers", "netflix")
	if filepath.Dir(generationsPath) != expectedParent {
		return newLibraryError(
			ErrorInvalidPersistence,
			"",
			0,
			errors.New("generations deletion path is outside the current contract"),
		)
	}
	if removeError := os.RemoveAll(generationsPath); removeError != nil {
		return newLibraryError(ErrorPersistenceFailed, "", 0, removeError)
	}
	return syncExistingParent(expectedParent)
}

func removeTMDBCacheFiles(cacheFile privatepath.File) error {
	if cacheFile.RelativePath() != filepath.FromSlash(product.NetflixTMDBCacheRelativePath) {
		return newLibraryError(
			ErrorInvalidPersistence,
			"",
			0,
			errors.New("TMDB cache deletion path does not match the current contract"),
		)
	}
	var removalErrors []error
	for _, path := range []string{
		cacheFile.Path(),
		cacheFile.Path() + "-wal",
		cacheFile.Path() + "-shm",
		cacheFile.Path() + ".next",
	} {
		if removeError := os.Remove(path); removeError != nil &&
			!errors.Is(removeError, os.ErrNotExist) {
			removalErrors = append(
				removalErrors,
				newLibraryError(ErrorPersistenceFailed, "", 0, removeError),
			)
		}
	}
	return errors.Join(removalErrors...)
}

func syncExistingParent(path string) error {
	directoryHandle, openError := os.Open(path)
	if errors.Is(openError, os.ErrNotExist) {
		return nil
	}
	if openError != nil {
		return newLibraryError(ErrorPersistenceFailed, "", 0, openError)
	}
	defer directoryHandle.Close()
	if syncError := directoryHandle.Sync(); syncError != nil {
		return newLibraryError(ErrorPersistenceFailed, "", 0, syncError)
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	source io.Reader
}

func (reader *contextReader) Read(destination []byte) (int, error) {
	if reader.ctx == nil {
		return 0, context.Canceled
	}
	if contextError := reader.ctx.Err(); contextError != nil {
		return 0, contextError
	}
	return reader.source.Read(destination)
}

var errArtifactLimitExceeded = errors.New("artifact byte limit exceeded")

type boundedHashWriter struct {
	destination io.Writer
	maximum     int64
	written     int64
}

func (writer *boundedHashWriter) Write(contents []byte) (int, error) {
	if int64(len(contents)) > writer.maximum-writer.written {
		return 0, errArtifactLimitExceeded
	}
	written, writeError := writer.destination.Write(contents)
	writer.written += int64(written)
	return written, writeError
}
