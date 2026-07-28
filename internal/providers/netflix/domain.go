// Package netflix owns the canonical Netflix viewing-activity domain.
package netflix

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// TitleIdentityVersion is the current derived-title grouping contract.
	TitleIdentityVersion = "netflix-title-v1"
	viewingDateLayout    = "1/2/06"
	releaseDateLayout    = "2006-01-02"
)

var (
	// ErrInvalidViewingTitle identifies a title outside the current Netflix
	// viewing-activity contract.
	ErrInvalidViewingTitle = errors.New("invalid Netflix viewing title")

	// ErrInvalidViewingDate identifies a date outside the current Netflix
	// viewing-activity contract.
	ErrInvalidViewingDate = errors.New("invalid Netflix viewing date")

	// ErrInvalidTitleMetadata identifies incomplete or inconsistent accepted
	// title metadata.
	ErrInvalidTitleMetadata = errors.New("invalid Netflix title metadata")

	// ErrInvalidActivityRecord identifies a zero or inconsistent activity
	// record supplied to a core operation.
	ErrInvalidActivityRecord = errors.New("invalid Netflix activity record")

	viewingDatePattern = regexp.MustCompile(
		`^(?:0?[1-9]|1[0-2])/(?:0?[1-9]|[12][0-9]|3[01])/[0-9]{2}$`,
	)
	episodicTitlePattern = regexp.MustCompile(
		`(?i)^(.+?)(?::\s*(?:season|temporada|saison|сезон)\s+[0-9]+\s*:|:\s*(?:limited series|miniserie|mini-série|мини-сериал)\s*:|:\s*(?:part|parte|partie|часть)\s+(?:[0-9]+|[ivxlcdm]+)\s*:|(?::|-)\s*(?:episode|episodio|épisode|серия)\s+[0-9]+(?:\s*:|\s+-|\s+|$)).*$`,
	)
)

// LocalDate is a validated calendar date without a time zone.
type LocalDate struct {
	year  int
	month time.Month
	day   int
}

// ParseLocalDate validates the current Netflix M/D/YY viewing-date contract.
func ParseLocalDate(rawDate string) (LocalDate, error) {
	if !viewingDatePattern.MatchString(rawDate) {
		return LocalDate{}, fmt.Errorf(
			"%w: expected M/D/YY",
			ErrInvalidViewingDate,
		)
	}
	parsedDate, parseError := time.Parse(viewingDateLayout, rawDate)
	if parseError != nil {
		return LocalDate{}, fmt.Errorf(
			"%w: parse calendar value: %v",
			ErrInvalidViewingDate,
			parseError,
		)
	}
	return LocalDate{
		year:  parsedDate.Year(),
		month: parsedDate.Month(),
		day:   parsedDate.Day(),
	}, nil
}

// ParseISODate validates an API-facing YYYY-MM-DD local calendar date.
func ParseISODate(rawDate string) (LocalDate, error) {
	parsedDate, parseError := time.Parse(releaseDateLayout, rawDate)
	if parseError != nil || parsedDate.Format(releaseDateLayout) != rawDate {
		return LocalDate{}, fmt.Errorf(
			"%w: expected YYYY-MM-DD",
			ErrInvalidViewingDate,
		)
	}
	return LocalDate{
		year:  parsedDate.Year(),
		month: parsedDate.Month(),
		day:   parsedDate.Day(),
	}, nil
}

// ISO returns the date in YYYY-MM-DD form.
func (date LocalDate) ISO() string {
	return date.asTime().Format(releaseDateLayout)
}

// Year returns the calendar year.
func (date LocalDate) Year() int {
	return date.year
}

// Month returns the calendar month.
func (date LocalDate) Month() time.Month {
	return date.month
}

// Day returns the calendar day.
func (date LocalDate) Day() int {
	return date.day
}

// Weekday returns the calendar weekday.
func (date LocalDate) Weekday() time.Weekday {
	return date.asTime().Weekday()
}

func (date LocalDate) valid() bool {
	if date.year == 0 || date.month < time.January || date.month > time.December || date.day < 1 {
		return false
	}
	normalized := date.asTime()
	return normalized.Year() == date.year &&
		normalized.Month() == date.month &&
		normalized.Day() == date.day
}

func (date LocalDate) asTime() time.Time {
	return time.Date(date.year, date.month, date.day, 0, 0, 0, 0, time.UTC)
}

func (date LocalDate) before(other LocalDate) bool {
	return date.asTime().Before(other.asTime())
}

func (date LocalDate) after(other LocalDate) bool {
	return date.asTime().After(other.asTime())
}

// TitleIdentity is the versioned identity used to group episode rows for
// analytics and later metadata enrichment.
type TitleIdentity struct {
	searchTitle string
	key         string
	version     string
}

// SearchTitle returns the derived title used for grouping and metadata lookup.
func (identity TitleIdentity) SearchTitle() string {
	return identity.searchTitle
}

// Key returns the stable opaque identity for this derivation version.
func (identity TitleIdentity) Key() string {
	return identity.key
}

// Version returns the derivation contract version.
func (identity TitleIdentity) Version() string {
	return identity.version
}

func (identity TitleIdentity) valid() bool {
	return identity.searchTitle != "" &&
		identity.key != "" &&
		identity.version == TitleIdentityVersion
}

func newTitleIdentity(rawTitle string) TitleIdentity {
	searchTitle := rawTitle
	patternParts := episodicTitlePattern.FindStringSubmatch(rawTitle)
	if len(patternParts) > 1 {
		seriesTitle := strings.TrimSpace(patternParts[1])
		if seriesTitle != "" {
			searchTitle = seriesTitle
		}
	}
	hashInput := TitleIdentityVersion + "\x00" + strings.ToLower(searchTitle)
	hashValue := sha256.Sum256([]byte(hashInput))
	return TitleIdentity{
		searchTitle: searchTitle,
		key:         hex.EncodeToString(hashValue[:]),
		version:     TitleIdentityVersion,
	}
}

// ViewingActivity is one validated row from Netflix's per-profile Viewing
// activity CSV.
type ViewingActivity struct {
	rawTitle string
	rawDate  string
	date     LocalDate
	identity TitleIdentity
}

// NewViewingActivity validates and constructs one viewing-activity row.
func NewViewingActivity(rawTitle string, rawDate string) (ViewingActivity, error) {
	if textError := validateSingleLine(rawTitle, "title", false); textError != nil {
		return ViewingActivity{}, fmt.Errorf("%w: %v", ErrInvalidViewingTitle, textError)
	}
	viewingDate, dateError := ParseLocalDate(rawDate)
	if dateError != nil {
		return ViewingActivity{}, dateError
	}
	return ViewingActivity{
		rawTitle: rawTitle,
		rawDate:  rawDate,
		date:     viewingDate,
		identity: newTitleIdentity(rawTitle),
	}, nil
}

// RawTitle returns the exact accepted CSV title.
func (activity ViewingActivity) RawTitle() string {
	return activity.rawTitle
}

// RawDate returns the exact accepted CSV date.
func (activity ViewingActivity) RawDate() string {
	return activity.rawDate
}

// Date returns the validated local calendar date.
func (activity ViewingActivity) Date() LocalDate {
	return activity.date
}

// TitleIdentity returns the derived, versioned title identity.
func (activity ViewingActivity) TitleIdentity() TitleIdentity {
	return activity.identity
}

func (activity ViewingActivity) valid() bool {
	return activity.rawTitle != "" &&
		activity.rawDate != "" &&
		activity.date.valid() &&
		activity.identity.valid()
}

// MediaType identifies the current accepted screen-media categories.
type MediaType string

const (
	// MediaTypeMovie identifies a feature or short film.
	MediaTypeMovie MediaType = "movie"

	// MediaTypeSeries identifies a television or streaming series.
	MediaTypeSeries MediaType = "series"
)

// TitleMetadataInput is the boundary input for one accepted metadata record.
type TitleMetadataInput struct {
	MediaType        MediaType
	Genres           []string
	ReleaseDate      string
	RuntimeMinutes   *int
	OriginalLanguage string
	VoteAverage      *float64
	VoteCount        *int
	OriginCountries  []string
	Seasons          *int
	Episodes         *int
	TMDBID           int64
	MatchedTitle     string
	Description      string
}

// TitleMetadata is one validated accepted metadata snapshot.
type TitleMetadata struct {
	mediaType        MediaType
	genres           []string
	releaseDate      string
	runtimeMinutes   *int
	originalLanguage string
	voteAverage      *float64
	voteCount        *int
	originCountries  []string
	seasons          *int
	episodes         *int
	tmdbID           int64
	matchedTitle     string
	description      string
}

// NewTitleMetadata validates one accepted metadata snapshot.
func NewTitleMetadata(input TitleMetadataInput) (TitleMetadata, error) {
	if input.MediaType != MediaTypeMovie && input.MediaType != MediaTypeSeries {
		return TitleMetadata{}, fmt.Errorf(
			"%w: media type must be movie or series",
			ErrInvalidTitleMetadata,
		)
	}
	genres, genreError := validateLabels(input.Genres, "genre")
	if genreError != nil {
		return TitleMetadata{}, genreError
	}
	originCountries, countryError := validateLabels(input.OriginCountries, "origin country")
	if countryError != nil {
		return TitleMetadata{}, countryError
	}
	if input.ReleaseDate != "" {
		if _, dateError := time.Parse(releaseDateLayout, input.ReleaseDate); dateError != nil {
			return TitleMetadata{}, fmt.Errorf(
				"%w: release date must use YYYY-MM-DD",
				ErrInvalidTitleMetadata,
			)
		}
	}
	if input.RuntimeMinutes != nil && *input.RuntimeMinutes <= 0 {
		return TitleMetadata{}, fmt.Errorf(
			"%w: runtime minutes must be positive",
			ErrInvalidTitleMetadata,
		)
	}
	if input.OriginalLanguage != "" {
		if languageError := validateSingleLine(input.OriginalLanguage, "original language", false); languageError != nil {
			return TitleMetadata{}, fmt.Errorf("%w: %v", ErrInvalidTitleMetadata, languageError)
		}
	}
	if (input.VoteAverage == nil) != (input.VoteCount == nil) {
		return TitleMetadata{}, fmt.Errorf(
			"%w: vote average and vote count must be present together",
			ErrInvalidTitleMetadata,
		)
	}
	if input.VoteAverage != nil && (*input.VoteAverage < 0 || *input.VoteAverage > 10) {
		return TitleMetadata{}, fmt.Errorf(
			"%w: vote average must be between 0 and 10",
			ErrInvalidTitleMetadata,
		)
	}
	if input.VoteCount != nil && *input.VoteCount < 0 {
		return TitleMetadata{}, fmt.Errorf(
			"%w: vote count must not be negative",
			ErrInvalidTitleMetadata,
		)
	}
	if input.Seasons != nil && *input.Seasons <= 0 {
		return TitleMetadata{}, fmt.Errorf(
			"%w: seasons must be positive",
			ErrInvalidTitleMetadata,
		)
	}
	if input.Episodes != nil && *input.Episodes <= 0 {
		return TitleMetadata{}, fmt.Errorf(
			"%w: episodes must be positive",
			ErrInvalidTitleMetadata,
		)
	}
	if input.MediaType == MediaTypeMovie && (input.Seasons != nil || input.Episodes != nil) {
		return TitleMetadata{}, fmt.Errorf(
			"%w: movie metadata cannot contain seasons or episodes",
			ErrInvalidTitleMetadata,
		)
	}
	if input.TMDBID <= 0 {
		return TitleMetadata{}, fmt.Errorf(
			"%w: TMDB ID must be positive",
			ErrInvalidTitleMetadata,
		)
	}
	if titleError := validateSingleLine(input.MatchedTitle, "matched title", false); titleError != nil {
		return TitleMetadata{}, fmt.Errorf("%w: %v", ErrInvalidTitleMetadata, titleError)
	}
	if input.Description != "" {
		if descriptionError := validateSingleLine(input.Description, "description", true); descriptionError != nil {
			return TitleMetadata{}, fmt.Errorf("%w: %v", ErrInvalidTitleMetadata, descriptionError)
		}
	}

	return TitleMetadata{
		mediaType:        input.MediaType,
		genres:           genres,
		releaseDate:      input.ReleaseDate,
		runtimeMinutes:   cloneInt(input.RuntimeMinutes),
		originalLanguage: input.OriginalLanguage,
		voteAverage:      cloneFloat(input.VoteAverage),
		voteCount:        cloneInt(input.VoteCount),
		originCountries:  originCountries,
		seasons:          cloneInt(input.Seasons),
		episodes:         cloneInt(input.Episodes),
		tmdbID:           input.TMDBID,
		matchedTitle:     input.MatchedTitle,
		description:      input.Description,
	}, nil
}

// MediaType returns the accepted media type.
func (metadata TitleMetadata) MediaType() MediaType {
	return metadata.mediaType
}

// Genres returns a defensive copy of the accepted genres.
func (metadata TitleMetadata) Genres() []string {
	return slices.Clone(metadata.genres)
}

// ReleaseDate returns the optional YYYY-MM-DD release date.
func (metadata TitleMetadata) ReleaseDate() string {
	return metadata.releaseDate
}

// RuntimeMinutes returns the optional runtime.
func (metadata TitleMetadata) RuntimeMinutes() (int, bool) {
	return optionalInt(metadata.runtimeMinutes)
}

// OriginalLanguage returns the optional original-language code.
func (metadata TitleMetadata) OriginalLanguage() string {
	return metadata.originalLanguage
}

// VoteAverage returns the optional TMDB vote average.
func (metadata TitleMetadata) VoteAverage() (float64, bool) {
	if metadata.voteAverage == nil {
		return 0, false
	}
	return *metadata.voteAverage, true
}

// VoteCount returns the optional TMDB vote count.
func (metadata TitleMetadata) VoteCount() (int, bool) {
	return optionalInt(metadata.voteCount)
}

// OriginCountries returns a defensive copy of the accepted origin countries.
func (metadata TitleMetadata) OriginCountries() []string {
	return slices.Clone(metadata.originCountries)
}

// Seasons returns the optional number of seasons.
func (metadata TitleMetadata) Seasons() (int, bool) {
	return optionalInt(metadata.seasons)
}

// Episodes returns the optional number of episodes.
func (metadata TitleMetadata) Episodes() (int, bool) {
	return optionalInt(metadata.episodes)
}

// TMDBID returns the accepted TMDB identifier.
func (metadata TitleMetadata) TMDBID() int64 {
	return metadata.tmdbID
}

// MatchedTitle returns the accepted TMDB title.
func (metadata TitleMetadata) MatchedTitle() string {
	return metadata.matchedTitle
}

// Description returns the accepted description.
func (metadata TitleMetadata) Description() string {
	return metadata.description
}

func (metadata TitleMetadata) valid() bool {
	return (metadata.mediaType == MediaTypeMovie || metadata.mediaType == MediaTypeSeries) &&
		metadata.tmdbID > 0 &&
		metadata.matchedTitle != ""
}

// ActivityRecord combines one viewing row with an optional accepted metadata
// snapshot.
type ActivityRecord struct {
	activity ViewingActivity
	metadata *TitleMetadata
}

// NewLocalActivityRecord constructs a record without third-party metadata.
func NewLocalActivityRecord(activity ViewingActivity) (ActivityRecord, error) {
	if !activity.valid() {
		return ActivityRecord{}, ErrInvalidActivityRecord
	}
	return ActivityRecord{activity: activity}, nil
}

// NewEnrichedActivityRecord constructs a record with accepted metadata.
func NewEnrichedActivityRecord(
	activity ViewingActivity,
	metadata TitleMetadata,
) (ActivityRecord, error) {
	if !activity.valid() || !metadata.valid() {
		return ActivityRecord{}, ErrInvalidActivityRecord
	}
	metadataCopy := metadata
	return ActivityRecord{
		activity: activity,
		metadata: &metadataCopy,
	}, nil
}

// Activity returns the validated viewing row.
func (record ActivityRecord) Activity() ViewingActivity {
	return record.activity
}

// Metadata returns the accepted metadata when present.
func (record ActivityRecord) Metadata() (TitleMetadata, bool) {
	if record.metadata == nil {
		return TitleMetadata{}, false
	}
	return *record.metadata, true
}

func (record ActivityRecord) valid() bool {
	return record.activity.valid() && (record.metadata == nil || record.metadata.valid())
}

func validateSingleLine(value string, fieldName string, allowLineBreaks bool) error {
	if value == "" {
		return fmt.Errorf("%s is required", fieldName)
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must be valid UTF-8", fieldName)
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not have surrounding whitespace", fieldName)
	}
	for _, character := range value {
		if !unicode.IsControl(character) {
			continue
		}
		if allowLineBreaks && (character == '\n' || character == '\r' || character == '\t') {
			continue
		}
		return fmt.Errorf("%s contains a control character", fieldName)
	}
	return nil
}

func validateLabels(values []string, fieldName string) ([]string, error) {
	validated := make([]string, len(values))
	seen := make(map[string]struct{}, len(values))
	for valueIndex, value := range values {
		if valueError := validateSingleLine(value, fieldName, false); valueError != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidTitleMetadata, valueError)
		}
		if strings.Contains(value, ";") {
			return nil, fmt.Errorf(
				"%w: %s cannot contain a semicolon",
				ErrInvalidTitleMetadata,
				fieldName,
			)
		}
		normalized := strings.ToLower(value)
		if _, duplicate := seen[normalized]; duplicate {
			return nil, fmt.Errorf(
				"%w: duplicate %s",
				ErrInvalidTitleMetadata,
				fieldName,
			)
		}
		seen[normalized] = struct{}{}
		validated[valueIndex] = value
	}
	return validated, nil
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func cloneFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func optionalInt(value *int) (int, bool) {
	if value == nil {
		return 0, false
	}
	return *value, true
}
