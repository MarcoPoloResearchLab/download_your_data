// Package library owns the private Netflix generation lifecycle.
package library

import (
	"errors"
	"fmt"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
)

const (
	stateSchemaOwner    = "download_your_data"
	stateSchemaVersion  = "1"
	stateSchemaContract = "netflix-generation-library-v1"
	recordsContract     = "netflix-local-records-v1"
	analyticsContract   = "netflix-local-analytics-v1"
	generationIDPrefix  = "ng_"
)

// AnalysisLevel is the closed generation analysis contract.
type AnalysisLevel string

const (
	AnalysisLevelLocal AnalysisLevel = "local"
	AnalysisLevelTMDB  AnalysisLevel = "tmdb"
)

// GenerationState is the closed persisted lifecycle.
type GenerationState string

const (
	GenerationStateReceiving  GenerationState = "receiving"
	GenerationStateValidating GenerationState = "validating"
	GenerationStateImporting  GenerationState = "importing"
	GenerationStateEnriching  GenerationState = "enriching"
	GenerationStateReady      GenerationState = "ready"
	GenerationStateFailed     GenerationState = "failed"
)

// ErrorCode is the closed generation and repository failure identity.
type ErrorCode string

const (
	ErrorInvalidRequest     ErrorCode = "invalid_request"
	ErrorNotFound           ErrorCode = "not_found"
	ErrorConflict           ErrorCode = "conflict"
	ErrorInvalidState       ErrorCode = "invalid_state"
	ErrorUploadTooLarge     ErrorCode = "upload_too_large"
	ErrorInvalidCSV         ErrorCode = "invalid_csv"
	ErrorInvalidHeader      ErrorCode = "invalid_header"
	ErrorInvalidRow         ErrorCode = "invalid_row"
	ErrorInvalidTitle       ErrorCode = "invalid_title"
	ErrorInvalidDate        ErrorCode = "invalid_date"
	ErrorLimitExceeded      ErrorCode = "limit_exceeded"
	ErrorCanceled           ErrorCode = "canceled"
	ErrorIncomplete         ErrorCode = "incomplete"
	ErrorLeaseUnavailable   ErrorCode = "lease_unavailable"
	ErrorPersistenceFailed  ErrorCode = "persistence_failed"
	ErrorInvalidPersistence ErrorCode = "invalid_persistence"
)

// Error is a typed lifecycle failure without private row content.
type Error struct {
	code         ErrorCode
	generationID string
	row          int
	cause        error
}

func (libraryError *Error) Error() string {
	if libraryError.generationID == "" {
		return fmt.Sprintf("Netflix library operation failed with %s", libraryError.code)
	}
	if libraryError.row > 0 {
		return fmt.Sprintf(
			"Netflix generation %s failed with %s at CSV row %d",
			libraryError.generationID,
			libraryError.code,
			libraryError.row,
		)
	}
	return fmt.Sprintf(
		"Netflix generation %s failed with %s",
		libraryError.generationID,
		libraryError.code,
	)
}

func (libraryError *Error) Unwrap() error {
	return libraryError.cause
}

// Code returns the machine-readable failure identity.
func (libraryError *Error) Code() ErrorCode {
	return libraryError.code
}

// GenerationID returns the opaque affected generation.
func (libraryError *Error) GenerationID() string {
	return libraryError.generationID
}

// Row returns the one-based CSV row when validation reached one.
func (libraryError *Error) Row() int {
	return libraryError.row
}

// Failure is the reader-safe persisted terminal failure.
type Failure struct {
	Code ErrorCode `json:"code"`
	Row  int       `json:"row,omitempty"`
}

// Generation is the reader-facing immutable lifecycle snapshot.
type Generation struct {
	ID                 string          `json:"id"`
	SourceGenerationID string          `json:"source_generation_id,omitempty"`
	AnalysisLevel      AnalysisLevel   `json:"analysis_level"`
	State              GenerationState `json:"state"`
	ActivityCount      int             `json:"activity_count"`
	UniqueTitleCount   int             `json:"unique_title_count"`
	StartDate          string          `json:"start_date,omitempty"`
	EndDate            string          `json:"end_date,omitempty"`
	CreatedAt          string          `json:"created_at"`
	UpdatedAt          string          `json:"updated_at"`
	CompletedAt        string          `json:"completed_at,omitempty"`
	Failure            *Failure        `json:"failure,omitempty"`
}

// ProviderState is the compact backend-owned workspace state.
type ProviderState string

const (
	ProviderStateEmpty        ProviderState = "empty"
	ProviderStateBuilding     ProviderState = "building"
	ProviderStateReadyLocal   ProviderState = "ready_local"
	ProviderStateReadyTMDB    ProviderState = "ready_tmdb"
	ProviderStateActionNeeded ProviderState = "action_needed"
	ProviderStateDeleting     ProviderState = "deleting"
)

// Capabilities contains public product limits and non-secret readiness.
type Capabilities struct {
	LocalImport         bool  `json:"local_import"`
	TMDBConfigured      bool  `json:"tmdb_configured"`
	MaxUploadBytes      int64 `json:"max_upload_bytes"`
	MaxRows             int   `json:"max_rows"`
	MaxUniqueTitles     int   `json:"max_unique_titles"`
	MaxTitleBytes       int   `json:"max_title_bytes"`
	MaxFieldBytes       int   `json:"max_field_bytes"`
	MaxWorkingBytes     int64 `json:"max_working_bytes"`
	MaxProgressEvents   int   `json:"max_progress_events"`
	MaxConcurrentBuilds int   `json:"max_concurrent_builds"`
	MaxRecordPageSize   int   `json:"max_record_page_size"`
	MinimumViewingYear  int   `json:"minimum_viewing_year"`
}

// Snapshot is the canonical GET /api/providers/netflix payload.
type Snapshot struct {
	Provider     string        `json:"provider"`
	State        ProviderState `json:"state"`
	Active       *Generation   `json:"active_generation,omitempty"`
	Building     *Generation   `json:"building_generation,omitempty"`
	LatestFailed *Generation   `json:"latest_failed_generation,omitempty"`
	Capabilities Capabilities  `json:"capabilities"`
}

// Event is one ordered, persisted lifecycle transition.
type Event struct {
	Sequence         int64           `json:"sequence"`
	State            GenerationState `json:"state"`
	ActivityCount    int             `json:"activity_count"`
	UniqueTitleCount int             `json:"unique_title_count"`
	OccurredAt       string          `json:"occurred_at"`
	Failure          *Failure        `json:"failure,omitempty"`
}

// Events is an ordered resumable event page.
type Events struct {
	GenerationID string  `json:"generation_id"`
	Events       []Event `json:"events"`
	LastSequence int64   `json:"last_sequence"`
}

// Activity is one current local record exposed by the paged API.
type Activity struct {
	Index                int64  `json:"index"`
	RawTitle             string `json:"title"`
	RawDate              string `json:"date"`
	DateISO              string `json:"date_iso"`
	DerivedTitle         string `json:"derived_title"`
	TitleIdentity        string `json:"title_identity"`
	TitleIdentityVersion string `json:"title_identity_version"`
}

// ActivityPage is one deterministic generation-bound records page.
type ActivityPage struct {
	GenerationID string     `json:"generation_id"`
	Records      []Activity `json:"records"`
	NextCursor   string     `json:"next_cursor,omitempty"`
}

// Analytics is the generation-bound analytics response.
type Analytics struct {
	GenerationID string             `json:"generation_id"`
	DateFilter   AnalyticsDateRange `json:"date_filter"`
	Data         netflix.Analytics  `json:"data"`
}

// AnalyticsDateRange is the normalized inclusive API filter.
type AnalyticsDateRange struct {
	StartDate string `json:"start_date,omitempty"`
	EndDate   string `json:"end_date,omitempty"`
}

type repositoryState struct {
	SchemaOwner      string            `json:"schema_owner"`
	SchemaVersion    string            `json:"schema_version"`
	SchemaContract   string            `json:"schema_contract"`
	Revision         int64             `json:"revision"`
	Deleting         bool              `json:"deleting"`
	ActiveID         string            `json:"active_generation_id,omitempty"`
	BuildingID       string            `json:"building_generation_id,omitempty"`
	PendingDeletions []string          `json:"pending_deletions"`
	Generations      []generationState `json:"generations"`
}

type generationState struct {
	ID                 string          `json:"id"`
	SourceGenerationID string          `json:"source_generation_id,omitempty"`
	AnalysisLevel      AnalysisLevel   `json:"analysis_level"`
	State              GenerationState `json:"state"`
	ActivityCount      int             `json:"activity_count"`
	UniqueTitleCount   int             `json:"unique_title_count"`
	StartDate          string          `json:"start_date,omitempty"`
	EndDate            string          `json:"end_date,omitempty"`
	RecordsSHA256      string          `json:"records_sha256,omitempty"`
	AnalyticsSHA256    string          `json:"analytics_sha256,omitempty"`
	CreatedAtMS        int64           `json:"created_at_ms"`
	UpdatedAtMS        int64           `json:"updated_at_ms"`
	CompletedAtMS      int64           `json:"completed_at_ms,omitempty"`
	Failure            *Failure        `json:"failure,omitempty"`
	Events             []eventState    `json:"events"`
}

type eventState struct {
	Sequence         int64           `json:"sequence"`
	State            GenerationState `json:"state"`
	ActivityCount    int             `json:"activity_count"`
	UniqueTitleCount int             `json:"unique_title_count"`
	OccurredAtMS     int64           `json:"occurred_at_ms"`
	Failure          *Failure        `json:"failure,omitempty"`
}

type persistedRecord struct {
	Contract             string `json:"contract"`
	Index                int64  `json:"index"`
	RawTitle             string `json:"title"`
	RawDate              string `json:"date"`
	DateISO              string `json:"date_iso"`
	DerivedTitle         string `json:"derived_title"`
	TitleIdentity        string `json:"title_identity"`
	TitleIdentityVersion string `json:"title_identity_version"`
}

type persistedAnalytics struct {
	Contract     string            `json:"contract"`
	GenerationID string            `json:"generation_id"`
	Data         netflix.Analytics `json:"data"`
}

func newLibraryError(
	code ErrorCode,
	generationID string,
	row int,
	cause error,
) *Error {
	return &Error{
		code:         code,
		generationID: generationID,
		row:          row,
		cause:        cause,
	}
}

func classifyCSVError(generationID string, csvError error) *Error {
	var typedCSVError *netflix.CSVError
	if !errors.As(csvError, &typedCSVError) {
		return newLibraryError(ErrorInvalidCSV, generationID, 0, csvError)
	}
	var code ErrorCode
	switch typedCSVError.Code() {
	case netflix.CSVErrorCanceled:
		code = ErrorCanceled
	case netflix.CSVErrorInvalidHeader:
		code = ErrorInvalidHeader
	case netflix.CSVErrorInvalidRow:
		code = ErrorInvalidRow
	case netflix.CSVErrorInvalidTitle:
		code = ErrorInvalidTitle
	case netflix.CSVErrorInvalidDate:
		code = ErrorInvalidDate
	case netflix.CSVErrorLimitExceeded:
		code = ErrorLimitExceeded
	case netflix.CSVErrorEmpty, netflix.CSVErrorInvalidCSV:
		code = ErrorInvalidCSV
	default:
		code = ErrorInvalidCSV
	}
	return newLibraryError(code, generationID, typedCSVError.Row(), csvError)
}

func capabilities(tmdbConfigured bool) Capabilities {
	return Capabilities{
		LocalImport:         true,
		TMDBConfigured:      tmdbConfigured,
		MaxUploadBytes:      product.MaxNetflixViewingCSVBytes,
		MaxRows:             product.MaxNetflixViewingRows,
		MaxUniqueTitles:     product.MaxNetflixUniqueTitles,
		MaxTitleBytes:       product.MaxNetflixTitleBytes,
		MaxFieldBytes:       product.MaxNetflixFieldBytes,
		MaxWorkingBytes:     product.MaxNetflixWorkingBytes,
		MaxProgressEvents:   product.MaxNetflixProgressEvents,
		MaxConcurrentBuilds: product.MaxNetflixConcurrentBuilds,
		MaxRecordPageSize:   product.MaxNetflixRecordPageSize,
		MinimumViewingYear:  product.MinNetflixViewingYear,
	}
}

func timestamp(milliseconds int64) string {
	if milliseconds <= 0 {
		return ""
	}
	return time.UnixMilli(milliseconds).UTC().Format(time.RFC3339Nano)
}

func cloneFailure(failure *Failure) *Failure {
	if failure == nil {
		return nil
	}
	copyValue := *failure
	return &copyValue
}

func (generation generationState) snapshot() Generation {
	return Generation{
		ID:                 generation.ID,
		SourceGenerationID: generation.SourceGenerationID,
		AnalysisLevel:      generation.AnalysisLevel,
		State:              generation.State,
		ActivityCount:      generation.ActivityCount,
		UniqueTitleCount:   generation.UniqueTitleCount,
		StartDate:          generation.StartDate,
		EndDate:            generation.EndDate,
		CreatedAt:          timestamp(generation.CreatedAtMS),
		UpdatedAt:          timestamp(generation.UpdatedAtMS),
		CompletedAt:        timestamp(generation.CompletedAtMS),
		Failure:            cloneFailure(generation.Failure),
	}
}

func (event eventState) snapshot() Event {
	return Event{
		Sequence:         event.Sequence,
		State:            event.State,
		ActivityCount:    event.ActivityCount,
		UniqueTitleCount: event.UniqueTitleCount,
		OccurredAt:       timestamp(event.OccurredAtMS),
		Failure:          cloneFailure(event.Failure),
	}
}
