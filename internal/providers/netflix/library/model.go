// Package library owns the private Netflix generation lifecycle.
package library

import (
	"errors"
	"fmt"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/enrichment"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
)

const (
	stateSchemaOwner    = "download_your_data"
	stateSchemaVersion  = "1"
	stateSchemaContract = "netflix-generation-library-v1"
	recordsContract     = "netflix-generation-records-v1"
	analyticsContract   = "netflix-generation-analytics-v1"
	generationIDPrefix  = "ng_"

	tmdbAuthorizationContract = "tmdb-derived-title-queries-v1"
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
	ErrorNotConfigured      ErrorCode = "not_configured"
	ErrorConsentRequired    ErrorCode = "consent_required"
	ErrorRateLimited        ErrorCode = "rate_limited"
	ErrorUnavailable        ErrorCode = "unavailable"
	ErrorInvalidResponse    ErrorCode = "invalid_response"
	ErrorStaleSource        ErrorCode = "stale_source"
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
	ID                  string          `json:"id"`
	SourceGenerationID  string          `json:"source_generation_id,omitempty"`
	AnalysisLevel       AnalysisLevel   `json:"analysis_level"`
	State               GenerationState `json:"state"`
	ActivityCount       int             `json:"activity_count"`
	UniqueTitleCount    int             `json:"unique_title_count"`
	StartDate           string          `json:"start_date,omitempty"`
	EndDate             string          `json:"end_date,omitempty"`
	CreatedAt           string          `json:"created_at"`
	UpdatedAt           string          `json:"updated_at"`
	CompletedAt         string          `json:"completed_at,omitempty"`
	Failure             *Failure        `json:"failure,omitempty"`
	Locale              string          `json:"locale,omitempty"`
	TMDBClientIdentity  string          `json:"tmdb_client_identity,omitempty"`
	TMDBMatcherIdentity string          `json:"tmdb_matcher_identity,omitempty"`
	TMDBCacheIdentity   string          `json:"tmdb_cache_identity,omitempty"`
	CompletedTitleCount int             `json:"completed_title_count"`
	MatchedTitleCount   int             `json:"matched_title_count"`
	ReviewTitleCount    int             `json:"review_title_count"`
	UnmatchedTitleCount int             `json:"unmatched_title_count"`
	CacheHitTitleCount  int             `json:"cache_hit_title_count"`
	ProgressPercent     int             `json:"progress_percent"`
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
	LocalImport         bool             `json:"local_import"`
	TMDBConfigured      bool             `json:"tmdb_configured"`
	MaxUploadBytes      int64            `json:"max_upload_bytes"`
	MaxRows             int              `json:"max_rows"`
	MaxUniqueTitles     int              `json:"max_unique_titles"`
	MaxTitleBytes       int              `json:"max_title_bytes"`
	MaxFieldBytes       int              `json:"max_field_bytes"`
	MaxWorkingBytes     int64            `json:"max_working_bytes"`
	MaxProgressEvents   int              `json:"max_progress_events"`
	MaxConcurrentBuilds int              `json:"max_concurrent_builds"`
	MaxRecordPageSize   int              `json:"max_record_page_size"`
	MinimumViewingYear  int              `json:"minimum_viewing_year"`
	TMDBAttribution     tmdb.Attribution `json:"tmdb_attribution"`
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
	Sequence            int64           `json:"sequence"`
	State               GenerationState `json:"state"`
	ActivityCount       int             `json:"activity_count"`
	UniqueTitleCount    int             `json:"unique_title_count"`
	OccurredAt          string          `json:"occurred_at"`
	Failure             *Failure        `json:"failure,omitempty"`
	CompletedTitleCount int             `json:"completed_title_count"`
	TotalTitleCount     int             `json:"total_title_count"`
	MatchedTitleCount   int             `json:"matched_title_count"`
	ReviewTitleCount    int             `json:"review_title_count"`
	UnmatchedTitleCount int             `json:"unmatched_title_count"`
	CacheHitTitleCount  int             `json:"cache_hit_title_count"`
	ProgressPercent     int             `json:"progress_percent"`
}

// Events is an ordered resumable event page.
type Events struct {
	GenerationID string  `json:"generation_id"`
	Events       []Event `json:"events"`
	LastSequence int64   `json:"last_sequence"`
}

// Activity is one current generation record exposed by the paged API.
type Activity struct {
	Index                int64     `json:"index"`
	RawTitle             string    `json:"title"`
	RawDate              string    `json:"date"`
	DateISO              string    `json:"date_iso"`
	DerivedTitle         string    `json:"derived_title"`
	TitleIdentity        string    `json:"title_identity"`
	TitleIdentityVersion string    `json:"title_identity_version"`
	Match                *Match    `json:"match,omitempty"`
	Metadata             *Metadata `json:"metadata,omitempty"`
}

// Match is the reader-facing terminal title-match outcome.
type Match struct {
	Status          netflix.MatchStatus   `json:"status"`
	MatcherIdentity string                `json:"matcher_identity"`
	MediaType       netflix.MediaType     `json:"media_type,omitempty"`
	TMDBID          int64                 `json:"tmdb_id,omitempty"`
	Evidence        netflix.MatchEvidence `json:"evidence"`
}

// Metadata is one accepted reader-facing TMDB metadata snapshot.
type Metadata struct {
	MediaType        netflix.MediaType `json:"media_type"`
	Genres           []string          `json:"genres"`
	ReleaseDate      string            `json:"release_date,omitempty"`
	RuntimeMinutes   *int              `json:"runtime_minutes,omitempty"`
	OriginalLanguage string            `json:"original_language,omitempty"`
	VoteAverage      *float64          `json:"vote_average,omitempty"`
	VoteCount        *int              `json:"vote_count,omitempty"`
	OriginCountries  []string          `json:"origin_countries"`
	Seasons          *int              `json:"seasons,omitempty"`
	Episodes         *int              `json:"episodes,omitempty"`
	TMDBID           int64             `json:"tmdb_id"`
	MatchedTitle     string            `json:"matched_title"`
	Description      string            `json:"description,omitempty"`
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
	ID                        string          `json:"id"`
	SourceGenerationID        string          `json:"source_generation_id,omitempty"`
	AnalysisLevel             AnalysisLevel   `json:"analysis_level"`
	State                     GenerationState `json:"state"`
	ActivityCount             int             `json:"activity_count"`
	UniqueTitleCount          int             `json:"unique_title_count"`
	StartDate                 string          `json:"start_date,omitempty"`
	EndDate                   string          `json:"end_date,omitempty"`
	RecordsSHA256             string          `json:"records_sha256,omitempty"`
	AnalyticsSHA256           string          `json:"analytics_sha256,omitempty"`
	CreatedAtMS               int64           `json:"created_at_ms"`
	UpdatedAtMS               int64           `json:"updated_at_ms"`
	CompletedAtMS             int64           `json:"completed_at_ms,omitempty"`
	Failure                   *Failure        `json:"failure,omitempty"`
	Events                    []eventState    `json:"events"`
	Locale                    string          `json:"locale,omitempty"`
	TMDBAuthorizationIdentity string          `json:"tmdb_authorization_identity,omitempty"`
	TMDBClientIdentity        string          `json:"tmdb_client_identity,omitempty"`
	TMDBMatcherIdentity       string          `json:"tmdb_matcher_identity,omitempty"`
	TMDBCacheIdentity         string          `json:"tmdb_cache_identity,omitempty"`
	SourceRecordsSHA256       string          `json:"source_records_sha256,omitempty"`
	SourceAnalyticsSHA256     string          `json:"source_analytics_sha256,omitempty"`
	CompletedTitleCount       int             `json:"completed_title_count"`
	MatchedTitleCount         int             `json:"matched_title_count"`
	ReviewTitleCount          int             `json:"review_title_count"`
	UnmatchedTitleCount       int             `json:"unmatched_title_count"`
	CacheHitTitleCount        int             `json:"cache_hit_title_count"`
	EnrichmentCheckpointBytes int64           `json:"enrichment_checkpoint_bytes"`
}

type eventState struct {
	Sequence            int64           `json:"sequence"`
	State               GenerationState `json:"state"`
	ActivityCount       int             `json:"activity_count"`
	UniqueTitleCount    int             `json:"unique_title_count"`
	OccurredAtMS        int64           `json:"occurred_at_ms"`
	Failure             *Failure        `json:"failure,omitempty"`
	CompletedTitleCount int             `json:"completed_title_count"`
	TotalTitleCount     int             `json:"total_title_count"`
	MatchedTitleCount   int             `json:"matched_title_count"`
	ReviewTitleCount    int             `json:"review_title_count"`
	UnmatchedTitleCount int             `json:"unmatched_title_count"`
	CacheHitTitleCount  int             `json:"cache_hit_title_count"`
	ProgressPercent     int             `json:"progress_percent"`
}

type persistedRecord struct {
	Contract             string    `json:"contract"`
	Index                int64     `json:"index"`
	RawTitle             string    `json:"title"`
	RawDate              string    `json:"date"`
	DateISO              string    `json:"date_iso"`
	DerivedTitle         string    `json:"derived_title"`
	TitleIdentity        string    `json:"title_identity"`
	TitleIdentityVersion string    `json:"title_identity_version"`
	Match                *Match    `json:"match,omitempty"`
	Metadata             *Metadata `json:"metadata,omitempty"`
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
		TMDBAttribution:     tmdb.CreditsAttribution(),
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
		ID:                  generation.ID,
		SourceGenerationID:  generation.SourceGenerationID,
		AnalysisLevel:       generation.AnalysisLevel,
		State:               generation.State,
		ActivityCount:       generation.ActivityCount,
		UniqueTitleCount:    generation.UniqueTitleCount,
		StartDate:           generation.StartDate,
		EndDate:             generation.EndDate,
		CreatedAt:           timestamp(generation.CreatedAtMS),
		UpdatedAt:           timestamp(generation.UpdatedAtMS),
		CompletedAt:         timestamp(generation.CompletedAtMS),
		Failure:             cloneFailure(generation.Failure),
		Locale:              generation.Locale,
		TMDBClientIdentity:  generation.TMDBClientIdentity,
		TMDBMatcherIdentity: generation.TMDBMatcherIdentity,
		TMDBCacheIdentity:   generation.TMDBCacheIdentity,
		CompletedTitleCount: generation.CompletedTitleCount,
		MatchedTitleCount:   generation.MatchedTitleCount,
		ReviewTitleCount:    generation.ReviewTitleCount,
		UnmatchedTitleCount: generation.UnmatchedTitleCount,
		CacheHitTitleCount:  generation.CacheHitTitleCount,
		ProgressPercent: progressPercent(
			generation.CompletedTitleCount,
			generation.UniqueTitleCount,
		),
	}
}

func (event eventState) snapshot() Event {
	return Event{
		Sequence:            event.Sequence,
		State:               event.State,
		ActivityCount:       event.ActivityCount,
		UniqueTitleCount:    event.UniqueTitleCount,
		OccurredAt:          timestamp(event.OccurredAtMS),
		Failure:             cloneFailure(event.Failure),
		CompletedTitleCount: event.CompletedTitleCount,
		TotalTitleCount:     event.TotalTitleCount,
		MatchedTitleCount:   event.MatchedTitleCount,
		ReviewTitleCount:    event.ReviewTitleCount,
		UnmatchedTitleCount: event.UnmatchedTitleCount,
		CacheHitTitleCount:  event.CacheHitTitleCount,
		ProgressPercent:     event.ProgressPercent,
	}
}

func progressPercent(completed int, total int) int {
	if total <= 0 || completed <= 0 {
		return 0
	}
	if completed >= total {
		return 100
	}
	return completed * 100 / total
}

func matchSnapshot(match netflix.TMDBMatch) Match {
	return Match{
		Status:          match.Status(),
		MatcherIdentity: match.MatcherIdentity(),
		MediaType:       match.MediaType(),
		TMDBID:          match.TMDBID(),
		Evidence:        match.Evidence(),
	}
}

func (match Match) domain() (netflix.TMDBMatch, error) {
	return netflix.NewTMDBMatch(netflix.TMDBMatchInput{
		Status:          match.Status,
		MatcherIdentity: match.MatcherIdentity,
		MediaType:       match.MediaType,
		TMDBID:          match.TMDBID,
		Evidence:        match.Evidence,
	})
}

func metadataSnapshot(metadata netflix.TitleMetadata) Metadata {
	runtimeMinutes, hasRuntime := metadata.RuntimeMinutes()
	voteAverage, hasVoteAverage := metadata.VoteAverage()
	voteCount, hasVoteCount := metadata.VoteCount()
	seasons, hasSeasons := metadata.Seasons()
	episodes, hasEpisodes := metadata.Episodes()
	return Metadata{
		MediaType:        metadata.MediaType(),
		Genres:           metadata.Genres(),
		ReleaseDate:      metadata.ReleaseDate(),
		RuntimeMinutes:   optionalInt(runtimeMinutes, hasRuntime),
		OriginalLanguage: metadata.OriginalLanguage(),
		VoteAverage:      optionalFloat(voteAverage, hasVoteAverage),
		VoteCount:        optionalInt(voteCount, hasVoteCount),
		OriginCountries:  metadata.OriginCountries(),
		Seasons:          optionalInt(seasons, hasSeasons),
		Episodes:         optionalInt(episodes, hasEpisodes),
		TMDBID:           metadata.TMDBID(),
		MatchedTitle:     metadata.MatchedTitle(),
		Description:      metadata.Description(),
	}
}

func (metadata Metadata) domain() (netflix.TitleMetadata, error) {
	return netflix.NewTitleMetadata(netflix.TitleMetadataInput{
		MediaType:        metadata.MediaType,
		Genres:           metadata.Genres,
		ReleaseDate:      metadata.ReleaseDate,
		RuntimeMinutes:   metadata.RuntimeMinutes,
		OriginalLanguage: metadata.OriginalLanguage,
		VoteAverage:      metadata.VoteAverage,
		VoteCount:        metadata.VoteCount,
		OriginCountries:  metadata.OriginCountries,
		Seasons:          metadata.Seasons,
		Episodes:         metadata.Episodes,
		TMDBID:           metadata.TMDBID,
		MatchedTitle:     metadata.MatchedTitle,
		Description:      metadata.Description,
	})
}

func optionalInt(value int, present bool) *int {
	if !present {
		return nil
	}
	return &value
}

func optionalFloat(value float64, present bool) *float64 {
	if !present {
		return nil
	}
	return &value
}

func classifyEnrichmentError(
	generationID string,
	receivedError error,
) *Error {
	var serviceError *enrichment.Error
	if !errors.As(receivedError, &serviceError) {
		return newLibraryError(ErrorIncomplete, generationID, 0, receivedError)
	}
	if serviceError.Code() == enrichment.ErrorCanceled {
		return newLibraryError(ErrorCanceled, generationID, 0, receivedError)
	}
	if serviceError.Code() == enrichment.ErrorAuthorizationRequired {
		return newLibraryError(ErrorConsentRequired, generationID, 0, receivedError)
	}
	var clientFailure interface {
		Code() tmdb.ErrorCode
	}
	if errors.As(receivedError, &clientFailure) {
		switch clientFailure.Code() {
		case tmdb.ErrorCanceled:
			return newLibraryError(ErrorCanceled, generationID, 0, receivedError)
		case tmdb.ErrorRateLimited:
			return newLibraryError(ErrorRateLimited, generationID, 0, receivedError)
		case tmdb.ErrorUnavailable, tmdb.ErrorUnauthorized:
			return newLibraryError(ErrorUnavailable, generationID, 0, receivedError)
		case tmdb.ErrorInvalidResponse, tmdb.ErrorInvalidRequest:
			return newLibraryError(ErrorInvalidResponse, generationID, 0, receivedError)
		}
	}
	switch serviceError.Code() {
	case enrichment.ErrorCacheFailed:
		return newLibraryError(ErrorPersistenceFailed, generationID, 0, receivedError)
	case enrichment.ErrorInvalidInput:
		return newLibraryError(ErrorInvalidResponse, generationID, 0, receivedError)
	default:
		return newLibraryError(ErrorIncomplete, generationID, 0, receivedError)
	}
}
