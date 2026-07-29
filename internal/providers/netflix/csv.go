package netflix

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

const (
	viewingTitleColumn = "Title"
	viewingDateColumn  = "Date"
)

var (
	viewingActivityHeader = []string{
		viewingTitleColumn,
		viewingDateColumn,
	}
	enrichedActivityHeader = []string{
		viewingTitleColumn,
		viewingDateColumn,
		"DerivedTitle",
		"TitleIdentity",
		"TitleIdentityVersion",
		"MatchStatus",
		"MatcherIdentity",
		"MatchMediaType",
		"MatchTMDBID",
		"MatchNormalizedQuery",
		"MatchBestCandidateTitle",
		"MatchBestScore",
		"MatchRunnerUpScore",
		"MatchMargin",
		"MatchExactCandidateCount",
		"MatchCandidatesConsidered",
		"MatchReason",
		"MediaType",
		"Genres",
		"ReleaseDate",
		"RuntimeMinutes",
		"OriginalLanguage",
		"VoteAverage",
		"VoteCount",
		"OriginCountries",
		"Seasons",
		"Episodes",
		"TMDBID",
		"MatchedTitle",
		"Description",
	}
)

// CSVErrorCode is a closed machine-readable CSV boundary failure.
type CSVErrorCode string

const (
	// CSVErrorCanceled means the caller canceled the operation.
	CSVErrorCanceled CSVErrorCode = "canceled"

	// CSVErrorEmpty means the file has no accepted activity rows.
	CSVErrorEmpty CSVErrorCode = "empty"

	// CSVErrorInvalidCSV means CSV tokenization failed.
	CSVErrorInvalidCSV CSVErrorCode = "invalid_csv"

	// CSVErrorInvalidHeader means the file does not use the current schema.
	CSVErrorInvalidHeader CSVErrorCode = "invalid_header"

	// CSVErrorInvalidRow means one row has the wrong shape or identity.
	CSVErrorInvalidRow CSVErrorCode = "invalid_row"

	// CSVErrorInvalidTitle means one title failed domain validation.
	CSVErrorInvalidTitle CSVErrorCode = "invalid_title"

	// CSVErrorInvalidDate means one viewing date failed domain validation.
	CSVErrorInvalidDate CSVErrorCode = "invalid_date"

	// CSVErrorInvalidMetadata means one enriched row has invalid metadata.
	CSVErrorInvalidMetadata CSVErrorCode = "invalid_metadata"

	// CSVErrorLimitExceeded means an explicit import bound was exceeded.
	CSVErrorLimitExceeded CSVErrorCode = "limit_exceeded"

	// CSVErrorWrite means the destination rejected output.
	CSVErrorWrite CSVErrorCode = "write_failed"
)

// CSVError describes a Netflix CSV boundary failure without including private
// row content.
type CSVError struct {
	code CSVErrorCode
	row  int
	err  error
}

// Error returns the contextual boundary failure.
func (boundaryError *CSVError) Error() string {
	if boundaryError.row > 0 {
		return fmt.Sprintf(
			"process Netflix CSV row %d: %v",
			boundaryError.row,
			boundaryError.err,
		)
	}
	return fmt.Sprintf("process Netflix CSV: %v", boundaryError.err)
}

// Unwrap exposes the underlying validation or I/O failure.
func (boundaryError *CSVError) Unwrap() error {
	return boundaryError.err
}

// Code returns the machine-readable failure code.
func (boundaryError *CSVError) Code() CSVErrorCode {
	return boundaryError.code
}

// Row returns the one-based CSV row, or zero for a file-level failure.
func (boundaryError *CSVError) Row() int {
	return boundaryError.row
}

// CSVLimits contains explicit row and field bounds for a CSV boundary.
type CSVLimits struct {
	maxRows       int
	maxTitleBytes int
	maxFieldBytes int
}

// NewCSVLimits validates explicit CSV import bounds.
func NewCSVLimits(
	maxRows int,
	maxTitleBytes int,
	maxFieldBytes int,
) (CSVLimits, error) {
	if maxRows <= 0 {
		return CSVLimits{}, errors.New("create Netflix CSV limits: max rows must be positive")
	}
	if maxTitleBytes <= 0 {
		return CSVLimits{}, errors.New("create Netflix CSV limits: max title bytes must be positive")
	}
	if maxFieldBytes < maxTitleBytes {
		return CSVLimits{}, errors.New(
			"create Netflix CSV limits: max field bytes must be at least max title bytes",
		)
	}
	return CSVLimits{
		maxRows:       maxRows,
		maxTitleBytes: maxTitleBytes,
		maxFieldBytes: maxFieldBytes,
	}, nil
}

func (limits CSVLimits) valid() bool {
	return limits.maxRows > 0 &&
		limits.maxTitleBytes > 0 &&
		limits.maxFieldBytes >= limits.maxTitleBytes
}

// ParseViewingActivity parses the current per-profile Netflix Viewing
// activity CSV contract.
func ParseViewingActivity(
	ctx context.Context,
	source io.Reader,
	limits CSVLimits,
) ([]ViewingActivity, error) {
	if source == nil {
		return nil, newCSVError(
			CSVErrorInvalidCSV,
			0,
			errors.New("source reader is required"),
		)
	}
	if !limits.valid() {
		return nil, newCSVError(
			CSVErrorLimitExceeded,
			0,
			errors.New("validated CSV limits are required"),
		)
	}
	if cancellationError := contextError(ctx); cancellationError != nil {
		return nil, cancellationError
	}

	csvReader := newCSVReader(source)
	header, headerError := csvReader.Read()
	if errors.Is(headerError, io.EOF) {
		return nil, newCSVError(CSVErrorEmpty, 0, errors.New("file is empty"))
	}
	if headerError != nil {
		return nil, newCSVError(CSVErrorInvalidCSV, 1, headerError)
	}
	columnIndexes, schemaError := validateViewingHeader(header)
	if schemaError != nil {
		return nil, schemaError
	}

	activities := make([]ViewingActivity, 0)
	rowNumber := 1
	for {
		if cancellationError := contextError(ctx); cancellationError != nil {
			return nil, cancellationError
		}
		row, readError := csvReader.Read()
		rowNumber++
		if errors.Is(readError, io.EOF) {
			break
		}
		if readError != nil {
			return nil, newCSVError(CSVErrorInvalidCSV, rowNumber, readError)
		}
		if len(row) != len(viewingActivityHeader) {
			return nil, newCSVError(
				CSVErrorInvalidRow,
				rowNumber,
				fmt.Errorf(
					"expected %d fields, received %d",
					len(viewingActivityHeader),
					len(row),
				),
			)
		}
		if len(activities) == limits.maxRows {
			return nil, newCSVError(
				CSVErrorLimitExceeded,
				rowNumber,
				fmt.Errorf("row limit %d exceeded", limits.maxRows),
			)
		}
		rawTitle := row[columnIndexes[viewingTitleColumn]]
		rawDate := row[columnIndexes[viewingDateColumn]]
		if len(rawTitle) > limits.maxTitleBytes {
			return nil, newCSVError(
				CSVErrorLimitExceeded,
				rowNumber,
				fmt.Errorf("title exceeds %d bytes", limits.maxTitleBytes),
			)
		}
		if len(rawDate) > limits.maxFieldBytes {
			return nil, newCSVError(
				CSVErrorLimitExceeded,
				rowNumber,
				fmt.Errorf("date exceeds %d bytes", limits.maxFieldBytes),
			)
		}

		activity, activityError := NewViewingActivity(rawTitle, rawDate)
		if activityError != nil {
			return nil, classifyActivityError(rowNumber, activityError)
		}
		activities = append(activities, activity)
	}
	if len(activities) == 0 {
		return nil, newCSVError(
			CSVErrorEmpty,
			0,
			errors.New("file contains no viewing activity rows"),
		)
	}
	return activities, nil
}

// WriteEnrichedActivity writes the current target-owned enriched CSV shape.
func WriteEnrichedActivity(
	ctx context.Context,
	destination io.Writer,
	records []ActivityRecord,
) error {
	if destination == nil {
		return newCSVError(
			CSVErrorWrite,
			0,
			errors.New("destination writer is required"),
		)
	}
	if len(records) == 0 {
		return newCSVError(
			CSVErrorEmpty,
			0,
			errors.New("at least one activity record is required"),
		)
	}
	for recordIndex, record := range records {
		if _, hasMatch := record.Match(); !record.valid() || !hasMatch {
			return newCSVError(
				CSVErrorInvalidRow,
				recordIndex+2,
				ErrInvalidActivityRecord,
			)
		}
	}
	if cancellationError := contextError(ctx); cancellationError != nil {
		return cancellationError
	}

	csvWriter := csv.NewWriter(destination)
	if writeError := csvWriter.Write(enrichedActivityHeader); writeError != nil {
		return newCSVError(CSVErrorWrite, 1, writeError)
	}
	for recordIndex, record := range records {
		if cancellationError := contextError(ctx); cancellationError != nil {
			return cancellationError
		}
		if writeError := csvWriter.Write(enrichedRow(record)); writeError != nil {
			return newCSVError(CSVErrorWrite, recordIndex+2, writeError)
		}
	}
	csvWriter.Flush()
	if flushError := csvWriter.Error(); flushError != nil {
		return newCSVError(CSVErrorWrite, 0, flushError)
	}
	return nil
}

// ReadEnrichedActivity reads and validates the current target-owned enriched
// CSV shape.
func ReadEnrichedActivity(
	ctx context.Context,
	source io.Reader,
	limits CSVLimits,
) ([]ActivityRecord, error) {
	if source == nil {
		return nil, newCSVError(
			CSVErrorInvalidCSV,
			0,
			errors.New("source reader is required"),
		)
	}
	if !limits.valid() {
		return nil, newCSVError(
			CSVErrorLimitExceeded,
			0,
			errors.New("validated CSV limits are required"),
		)
	}
	if cancellationError := contextError(ctx); cancellationError != nil {
		return nil, cancellationError
	}

	csvReader := newCSVReader(source)
	header, headerError := csvReader.Read()
	if errors.Is(headerError, io.EOF) {
		return nil, newCSVError(CSVErrorEmpty, 0, errors.New("file is empty"))
	}
	if headerError != nil {
		return nil, newCSVError(CSVErrorInvalidCSV, 1, headerError)
	}
	if !slices.Equal(header, enrichedActivityHeader) {
		return nil, newCSVError(
			CSVErrorInvalidHeader,
			1,
			errors.New("header does not match the current enriched schema"),
		)
	}

	records := make([]ActivityRecord, 0)
	rowNumber := 1
	for {
		if cancellationError := contextError(ctx); cancellationError != nil {
			return nil, cancellationError
		}
		row, readError := csvReader.Read()
		rowNumber++
		if errors.Is(readError, io.EOF) {
			break
		}
		if readError != nil {
			return nil, newCSVError(CSVErrorInvalidCSV, rowNumber, readError)
		}
		if len(row) != len(enrichedActivityHeader) {
			return nil, newCSVError(
				CSVErrorInvalidRow,
				rowNumber,
				fmt.Errorf(
					"expected %d fields, received %d",
					len(enrichedActivityHeader),
					len(row),
				),
			)
		}
		if len(records) == limits.maxRows {
			return nil, newCSVError(
				CSVErrorLimitExceeded,
				rowNumber,
				fmt.Errorf("row limit %d exceeded", limits.maxRows),
			)
		}
		for fieldIndex, fieldValue := range row {
			fieldLimit := limits.maxFieldBytes
			if fieldIndex == 0 {
				fieldLimit = limits.maxTitleBytes
			}
			if len(fieldValue) > fieldLimit {
				return nil, newCSVError(
					CSVErrorLimitExceeded,
					rowNumber,
					fmt.Errorf("field %d exceeds %d bytes", fieldIndex+1, fieldLimit),
				)
			}
		}

		record, recordError := parseEnrichedRow(row)
		if recordError != nil {
			return nil, newCSVError(CSVErrorInvalidMetadata, rowNumber, recordError)
		}
		records = append(records, record)
	}
	if len(records) == 0 {
		return nil, newCSVError(
			CSVErrorEmpty,
			0,
			errors.New("file contains no enriched activity rows"),
		)
	}
	return records, nil
}

func newCSVReader(source io.Reader) *csv.Reader {
	csvReader := csv.NewReader(source)
	csvReader.FieldsPerRecord = -1
	csvReader.ReuseRecord = true
	return csvReader
}

func validateViewingHeader(header []string) (map[string]int, error) {
	if len(header) != len(viewingActivityHeader) {
		return nil, newCSVError(
			CSVErrorInvalidHeader,
			1,
			fmt.Errorf(
				"expected exactly %d columns, received %d",
				len(viewingActivityHeader),
				len(header),
			),
		)
	}
	columnIndexes := make(map[string]int, len(header))
	for columnIndex, columnName := range header {
		if columnName != viewingTitleColumn && columnName != viewingDateColumn {
			return nil, newCSVError(
				CSVErrorInvalidHeader,
				1,
				fmt.Errorf("unexpected column at position %d", columnIndex+1),
			)
		}
		if _, duplicate := columnIndexes[columnName]; duplicate {
			return nil, newCSVError(
				CSVErrorInvalidHeader,
				1,
				fmt.Errorf("duplicate %s column", columnName),
			)
		}
		columnIndexes[columnName] = columnIndex
	}
	if len(columnIndexes) != len(viewingActivityHeader) {
		return nil, newCSVError(
			CSVErrorInvalidHeader,
			1,
			errors.New("title and date columns are required"),
		)
	}
	return columnIndexes, nil
}

func enrichedRow(record ActivityRecord) []string {
	activity := record.Activity()
	identity := activity.TitleIdentity()
	match, _ := record.Match()
	evidence := match.Evidence()
	row := []string{
		activity.RawTitle(),
		activity.RawDate(),
		identity.SearchTitle(),
		identity.Key(),
		identity.Version(),
		string(match.Status()),
		match.MatcherIdentity(),
		string(match.MediaType()),
		formatOptionalInt64(match.TMDBID()),
		evidence.NormalizedQuery,
		evidence.BestCandidateTitle,
		strconv.FormatFloat(evidence.BestScore, 'f', -1, 64),
		strconv.FormatFloat(evidence.RunnerUpScore, 'f', -1, 64),
		strconv.FormatFloat(evidence.Margin, 'f', -1, 64),
		strconv.Itoa(evidence.ExactCandidateCount),
		strconv.Itoa(evidence.CandidatesConsidered),
		evidence.Reason,
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
	}
	metadata, hasMetadata := record.Metadata()
	if !hasMetadata {
		return row
	}
	row[17] = string(metadata.MediaType())
	row[18] = strings.Join(metadata.Genres(), ";")
	row[19] = metadata.ReleaseDate()
	row[20] = formatOptionalInt(metadata.RuntimeMinutes())
	row[21] = metadata.OriginalLanguage()
	row[22] = formatOptionalFloat(metadata.VoteAverage())
	row[23] = formatOptionalInt(metadata.VoteCount())
	row[24] = strings.Join(metadata.OriginCountries(), ";")
	row[25] = formatOptionalInt(metadata.Seasons())
	row[26] = formatOptionalInt(metadata.Episodes())
	row[27] = strconv.FormatInt(metadata.TMDBID(), 10)
	row[28] = metadata.MatchedTitle()
	row[29] = metadata.Description()
	return row
}

func parseEnrichedRow(row []string) (ActivityRecord, error) {
	activity, activityError := NewViewingActivity(row[0], row[1])
	if activityError != nil {
		return ActivityRecord{}, activityError
	}
	identity := activity.TitleIdentity()
	if row[2] != identity.SearchTitle() ||
		row[3] != identity.Key() ||
		row[4] != identity.Version() {
		return ActivityRecord{}, errors.New(
			"derived title identity does not match the current contract",
		)
	}
	matchTMDBID, matchIdentifierError := parseOptionalInt64(
		row[8],
		"match TMDB ID",
	)
	if matchIdentifierError != nil {
		return ActivityRecord{}, matchIdentifierError
	}
	bestScore, bestScoreError := parseRequiredFloat(row[11], "match best score")
	if bestScoreError != nil {
		return ActivityRecord{}, bestScoreError
	}
	runnerUpScore, runnerUpError := parseRequiredFloat(
		row[12],
		"match runner-up score",
	)
	if runnerUpError != nil {
		return ActivityRecord{}, runnerUpError
	}
	margin, marginError := parseRequiredFloat(row[13], "match margin")
	if marginError != nil {
		return ActivityRecord{}, marginError
	}
	exactCandidateCount, exactCountError := parseRequiredInt(
		row[14],
		"match exact candidate count",
	)
	if exactCountError != nil {
		return ActivityRecord{}, exactCountError
	}
	candidatesConsidered, candidateCountError := parseRequiredInt(
		row[15],
		"match candidates considered",
	)
	if candidateCountError != nil {
		return ActivityRecord{}, candidateCountError
	}
	match, matchError := NewTMDBMatch(TMDBMatchInput{
		Status:          MatchStatus(row[5]),
		MatcherIdentity: row[6],
		MediaType:       MediaType(row[7]),
		TMDBID:          matchTMDBID,
		Evidence: MatchEvidence{
			NormalizedQuery:      row[9],
			BestCandidateTitle:   row[10],
			BestScore:            bestScore,
			RunnerUpScore:        runnerUpScore,
			Margin:               margin,
			ExactCandidateCount:  exactCandidateCount,
			CandidatesConsidered: candidatesConsidered,
			Reason:               row[16],
		},
	})
	if matchError != nil {
		return ActivityRecord{}, matchError
	}
	if allEmpty(row[17:]) {
		return NewEnrichedActivityRecord(activity, match, nil)
	}

	mediaType := MediaType(row[17])
	genres, genreError := parseLabels(row[18])
	if genreError != nil {
		return ActivityRecord{}, genreError
	}
	runtimeMinutes, runtimeError := parseOptionalInt(row[20], "runtime minutes")
	if runtimeError != nil {
		return ActivityRecord{}, runtimeError
	}
	voteAverage, averageError := parseOptionalFloat(row[22], "vote average")
	if averageError != nil {
		return ActivityRecord{}, averageError
	}
	voteCount, voteCountError := parseOptionalInt(row[23], "vote count")
	if voteCountError != nil {
		return ActivityRecord{}, voteCountError
	}
	originCountries, countryError := parseLabels(row[24])
	if countryError != nil {
		return ActivityRecord{}, countryError
	}
	seasons, seasonError := parseOptionalInt(row[25], "seasons")
	if seasonError != nil {
		return ActivityRecord{}, seasonError
	}
	episodes, episodeError := parseOptionalInt(row[26], "episodes")
	if episodeError != nil {
		return ActivityRecord{}, episodeError
	}
	tmdbID, identifierError := strconv.ParseInt(row[27], 10, 64)
	if identifierError != nil {
		return ActivityRecord{}, fmt.Errorf("parse TMDB ID: %w", identifierError)
	}

	metadata, metadataError := NewTitleMetadata(TitleMetadataInput{
		MediaType:        mediaType,
		Genres:           genres,
		ReleaseDate:      row[19],
		RuntimeMinutes:   runtimeMinutes,
		OriginalLanguage: row[21],
		VoteAverage:      voteAverage,
		VoteCount:        voteCount,
		OriginCountries:  originCountries,
		Seasons:          seasons,
		Episodes:         episodes,
		TMDBID:           tmdbID,
		MatchedTitle:     row[28],
		Description:      row[29],
	})
	if metadataError != nil {
		return ActivityRecord{}, metadataError
	}
	return NewEnrichedActivityRecord(activity, match, &metadata)
}

func parseLabels(rawValue string) ([]string, error) {
	if rawValue == "" {
		return nil, nil
	}
	labels := strings.Split(rawValue, ";")
	for _, label := range labels {
		if label == "" {
			return nil, errors.New("list contains an empty label")
		}
	}
	return labels, nil
}

func parseOptionalInt(rawValue string, fieldName string) (*int, error) {
	if rawValue == "" {
		return nil, nil
	}
	parsedValue, parseError := strconv.Atoi(rawValue)
	if parseError != nil {
		return nil, fmt.Errorf("parse %s: %w", fieldName, parseError)
	}
	return &parsedValue, nil
}

func parseOptionalFloat(rawValue string, fieldName string) (*float64, error) {
	if rawValue == "" {
		return nil, nil
	}
	parsedValue, parseError := strconv.ParseFloat(rawValue, 64)
	if parseError != nil {
		return nil, fmt.Errorf("parse %s: %w", fieldName, parseError)
	}
	return &parsedValue, nil
}

func parseOptionalInt64(rawValue string, fieldName string) (int64, error) {
	if rawValue == "" {
		return 0, nil
	}
	parsedValue, parseError := strconv.ParseInt(rawValue, 10, 64)
	if parseError != nil {
		return 0, fmt.Errorf("parse %s: %w", fieldName, parseError)
	}
	return parsedValue, nil
}

func parseRequiredInt(rawValue string, fieldName string) (int, error) {
	if rawValue == "" {
		return 0, fmt.Errorf("%s is required", fieldName)
	}
	parsedValue, parseError := strconv.Atoi(rawValue)
	if parseError != nil {
		return 0, fmt.Errorf("parse %s: %w", fieldName, parseError)
	}
	return parsedValue, nil
}

func parseRequiredFloat(rawValue string, fieldName string) (float64, error) {
	if rawValue == "" {
		return 0, fmt.Errorf("%s is required", fieldName)
	}
	parsedValue, parseError := strconv.ParseFloat(rawValue, 64)
	if parseError != nil {
		return 0, fmt.Errorf("parse %s: %w", fieldName, parseError)
	}
	return parsedValue, nil
}

func formatOptionalInt(value int, present bool) string {
	if !present {
		return ""
	}
	return strconv.Itoa(value)
}

func formatOptionalInt64(value int64) string {
	if value == 0 {
		return ""
	}
	return strconv.FormatInt(value, 10)
}

func formatOptionalFloat(value float64, present bool) string {
	if !present {
		return ""
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func allEmpty(values []string) bool {
	for _, value := range values {
		if value != "" {
			return false
		}
	}
	return true
}

func classifyActivityError(rowNumber int, activityError error) error {
	switch {
	case errors.Is(activityError, ErrInvalidViewingTitle):
		return newCSVError(CSVErrorInvalidTitle, rowNumber, activityError)
	case errors.Is(activityError, ErrInvalidViewingDate):
		return newCSVError(CSVErrorInvalidDate, rowNumber, activityError)
	default:
		return newCSVError(CSVErrorInvalidRow, rowNumber, activityError)
	}
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return newCSVError(
			CSVErrorCanceled,
			0,
			errors.New("context is required"),
		)
	}
	if cancellationError := ctx.Err(); cancellationError != nil {
		return newCSVError(CSVErrorCanceled, 0, cancellationError)
	}
	return nil
}

func newCSVError(code CSVErrorCode, row int, err error) *CSVError {
	return &CSVError{
		code: code,
		row:  row,
		err:  err,
	}
}
