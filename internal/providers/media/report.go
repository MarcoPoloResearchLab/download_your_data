package media

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const DefaultPageSize = 100
const MaxPageSize = 200

// Filter is validated at the HTTP boundary and shared by report and export.
type Filter struct {
	Provider    string `json:"provider"`
	Timezone    string `json:"timezone"`
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	Title       string `json:"title"`
	TitleID     string `json:"title_id"`
	Kind        string `json:"kind"`
	MatchStatus string `json:"match_status"`
	MediaType   string `json:"media_type"`
	location    *time.Location
}

// NewFilter validates the shared filter contract.
func NewFilter(provider, timezone, start, end, title, kind, matchStatus, titleID, mediaType string) (Filter, error) {
	if provider == "" {
		provider = "all"
	}
	if timezone == "" {
		timezone = "UTC"
	}
	if kind == "" {
		kind = "all"
	}
	if matchStatus == "" {
		matchStatus = "all"
	}
	if provider != "all" && provider != string(Netflix) && provider != string(PrimeVideo) {
		return Filter{}, errors.New("media.invalid_filter")
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return Filter{}, errors.New("media.invalid_timezone")
	}
	for _, value := range []string{start, end} {
		if value != "" {
			parsed, err := time.Parse(time.DateOnly, value)
			if err != nil || parsed.Format(time.DateOnly) != value {
				return Filter{}, errors.New("media.invalid_date_filter")
			}
		}
	}
	if (start != "" && end != "" && start > end) || len(title) > 8192 {
		return Filter{}, errors.New("media.invalid_filter")
	}
	if titleID != "" && !titleIDPattern.MatchString(titleID) {
		return Filter{}, errors.New("media.invalid_title_filter")
	}
	switch kind {
	case "all", string(ActivityEntry), string(Playback), string(WatchSummary), string(Search), string(Purchase), string(Trailer):
	default:
		return Filter{}, errors.New("media.invalid_filter")
	}
	switch matchStatus {
	case "all", "not_enriched", "matched", "review", "unmatched":
	default:
		return Filter{}, errors.New("media.invalid_filter")
	}
	if mediaType == "" {
		mediaType = "all"
	}
	if mediaType != "all" && mediaType != "movie" && mediaType != "series" && mediaType != "unknown" {
		return Filter{}, errors.New("media.invalid_filter")
	}
	return Filter{MediaType: mediaType, Provider: provider, Timezone: timezone, StartDate: start, EndDate: end, Title: title, Kind: kind, MatchStatus: matchStatus, TitleID: titleID, location: location}, nil
}

var titleIDPattern = regexp.MustCompile(`^(tmdb:(movie|series):[1-9][0-9]{0,18}|(netflix|prime-video):[a-f0-9]{64})$`)

// Count is one named categorical count.
type Count struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

// Service summarizes a source using its actual counting unit.
type Service struct {
	Provider        Provider `json:"provider"`
	Unit            string   `json:"unit"`
	Activities      int      `json:"activities"`
	RecordedSeconds float64  `json:"recorded_seconds"`
	TimedRecords    int      `json:"timed_records"`
}

// Month is one service-specific calendar count.
type Month struct {
	Month    string   `json:"month"`
	Provider Provider `json:"provider"`
	Count    int      `json:"count"`
}

// PeriodCount is a categorical activity count within one calendar period.
type PeriodCount struct {
	Period string `json:"period"`
	Label  string `json:"label"`
	Count  int    `json:"count"`
}

// Title groups only accepted identities across services.
type Title struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	MediaType   string     `json:"media_type"`
	MatchStatus string     `json:"match_status"`
	Activities  int        `json:"activities"`
	Providers   []Provider `json:"providers"`
	Metadata    *Metadata  `json:"metadata,omitempty"`
}

// Overview labels default measurement coverage and exclusion counts.
type Overview struct {
	MediaTypes                  []Count       `json:"media_types"`
	MonthlyMedia                []PeriodCount `json:"monthly_media"`
	GenresByWeekday             []PeriodCount `json:"genres_by_weekday"`
	GenresByYear                []PeriodCount `json:"genres_by_year"`
	OriginalLanguages           []Count       `json:"original_languages"`
	ActivityCount               int           `json:"activity_count"`
	SourceRecordCount           int           `json:"source_record_count"`
	UniqueTitleCount            int           `json:"unique_title_count"`
	AcceptedTitleCount          int           `json:"accepted_title_count"`
	UnresolvedTitleCount        int           `json:"unresolved_title_count"`
	MovieTitles                 int           `json:"movie_titles"`
	SeriesTitles                int           `json:"series_titles"`
	EpisodeCount                int           `json:"episode_count"`
	UnavailableTitleRecords     int           `json:"unavailable_title_records"`
	RecordedSeconds             float64       `json:"recorded_seconds"`
	TimedRecords                int           `json:"timed_records"`
	UnknownDurationRecords      int           `json:"unknown_duration_records"`
	ZeroDurationRecords         int           `json:"zero_duration_records"`
	Rentals                     int           `json:"rentals"`
	Purchases                   int           `json:"purchases"`
	PurchaseRecordsWithPlayback int           `json:"purchase_records_with_playback"`
	MatchCoverage               []Count       `json:"match_coverage"`
	Exclusions                  []Count       `json:"exclusions"`
	Services                    []Service     `json:"services"`
	Months                      []Month       `json:"months"`
	Weekdays                    []Count       `json:"weekdays"`
	TopTitles                   []Title       `json:"top_titles"`
	Genres                      []Count       `json:"genres"`
	Devices                     []Count       `json:"devices"`
	AudioLanguages              []Count       `json:"audio_languages"`
	SubtitleLanguages           []Count       `json:"subtitle_languages"`
}

// SourceSummary describes one imported provider and its available date coverage.
type SourceSummary struct {
	Provider     Provider `json:"provider"`
	GenerationID string   `json:"generation_id"`
	Records      int      `json:"records"`
	StartDate    string   `json:"start_date"`
	EndDate      string   `json:"end_date"`
}

// Report is the shared filtered, paged media consumption report.
type Report struct {
	Contract         string          `json:"contract"`
	Filter           Filter          `json:"filter"`
	Overview         Overview        `json:"overview"`
	Titles           []Title         `json:"titles"`
	Sources          []SourceSummary `json:"sources"`
	Records          []Activity      `json:"records"`
	NextCursor       string          `json:"next_cursor"`
	NextTitlesCursor string          `json:"next_titles_cursor"`
	Revision         string          `json:"revision"`
}

// FilterRecords applies one calendar and title filter to every report measure.
func FilterRecords(records []Record, filter Filter) []Record {
	result := []Record{}
	for _, record := range records {
		value := record.Snapshot()
		if filter.Provider != "all" && string(value.Provider) != filter.Provider {
			continue
		}
		if value.DatePrecision == Timestamp {
			parsed, _ := time.Parse(time.RFC3339Nano, value.Timestamp)
			value.Date = parsed.In(filter.location).Format(time.DateOnly)
		}
		if (filter.StartDate != "" && value.Date < filter.StartDate) || (filter.EndDate != "" && value.Date > filter.EndDate) {
			continue
		}
		if filter.Kind != "all" && string(value.Kind) != filter.Kind {
			continue
		}
		if filter.MatchStatus != "all" && value.MatchStatus != filter.MatchStatus {
			continue
		}
		if filter.TitleID != "" && identity(value) != filter.TitleID {
			continue
		}
		if filter.MediaType != "all" && mediaType(value) != filter.MediaType {
			continue
		}
		titleText := value.Title + " " + value.SearchTitle
		if value.Metadata != nil {
			titleText += " " + value.Metadata.Title
		}
		if filter.Title != "" && !strings.Contains(strings.ToLower(titleText), strings.ToLower(filter.Title)) {
			continue
		}
		result = append(result, Record{value: value})
	}
	slices.SortFunc(result, func(left, right Record) int {
		a, b := left.value, right.value
		if order := strings.Compare(b.Date, a.Date); order != 0 {
			return order
		}
		if order := strings.Compare(b.Timestamp, a.Timestamp); order != 0 {
			return order
		}
		return strings.Compare(a.ID, b.ID)
	})
	return result
}

// Build produces complete filtered measures with a revision-bound activity page.
func Build(records []Record, filter Filter, cursor string, titleCursor string, limit int) (Report, error) {
	if limit == 0 {
		limit = DefaultPageSize
	}
	if limit < 1 || limit > MaxPageSize {
		return Report{}, errors.New("media.invalid_page_size")
	}
	filtered := FilterRecords(records, filter)
	revisionHash := sha256.New()
	for _, record := range records {
		value := record.value
		_, _ = io.WriteString(revisionHash, value.ID+value.Source.GenerationID+value.MatchStatus)
	}
	revision := hex.EncodeToString(revisionHash.Sum(nil))
	filterBytes, _ := json.Marshal(filter)
	filterHash := sha256.Sum256(filterBytes)
	filterIdentity := hex.EncodeToString(filterHash[:])
	offset, err := pageOffset(cursor, revision, filterIdentity, "activities", len(filtered))
	if err != nil {
		return Report{}, err
	}
	groups := titles(filtered)
	titleOffset, err := pageOffset(titleCursor, revision, filterIdentity, "titles", len(groups))
	if err != nil {
		return Report{}, err
	}
	titleEnd := min(titleOffset+limit, len(groups))
	report := Report{Contract: "viewing-history-report-v2", Filter: filter, Overview: aggregate(filtered), Sources: sourceSummaries(records, filter), Titles: groups[titleOffset:titleEnd], Records: []Activity{}, Revision: revision}
	if titleEnd < len(groups) {
		report.NextTitlesCursor = nextPageCursor(revision, filterIdentity, "titles", titleEnd)
	}
	end := min(offset+limit, len(filtered))
	for _, record := range filtered[offset:end] {
		report.Records = append(report.Records, record.Snapshot())
	}
	if end < len(filtered) {
		report.NextCursor = nextPageCursor(revision, filterIdentity, "activities", end)
	}
	return report, nil
}

// ErrStaleCursor identifies a collection revision conflict.
var ErrStaleCursor = errors.New("media.stale_cursor")

type recordCursor struct {
	Revision   string `json:"revision"`
	Filter     string `json:"filter"`
	Offset     int    `json:"offset"`
	Collection string `json:"collection"`
}

func pageOffset(cursor, revision, filter, collection string, total int) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	encoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(encoded) > 1024 {
		return 0, errors.New("media.invalid_cursor")
	}
	var value recordCursor
	if err = json.Unmarshal(encoded, &value); err != nil || value.Filter != filter || value.Collection != collection || value.Offset < 0 || value.Revision == "" {
		return 0, errors.New("media.invalid_cursor")
	}
	if value.Revision != revision {
		return 0, ErrStaleCursor
	}
	if value.Offset > total {
		return 0, errors.New("media.invalid_cursor")
	}
	return value.Offset, nil
}
func nextPageCursor(revision, filter, collection string, offset int) string {
	encoded, _ := json.Marshal(recordCursor{Revision: revision, Filter: filter, Collection: collection, Offset: offset})
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func identity(value Activity) string {
	if value.Metadata != nil {
		return fmt.Sprintf("tmdb:%s:%d", value.Metadata.MediaType, value.Metadata.TMDBID)
	}
	return string(value.Provider) + ":" + value.TitleIdentity
}
func titles(records []Record) []Title {
	groups := make(map[string]*Title)
	for _, record := range records {
		if !record.IsConsumption() {
			continue
		}
		value := record.value
		if value.TitleStatus == "unavailable" {
			continue
		}
		key := identity(value)
		group, ok := groups[key]
		if !ok {
			group = &Title{ID: key, Title: value.SearchTitle, MediaType: "unknown", MatchStatus: value.MatchStatus, Providers: []Provider{}, Metadata: record.Snapshot().Metadata}
			if value.Metadata != nil {
				group.Title = value.Metadata.Title
				group.MediaType = value.Metadata.MediaType
			} else if value.SeriesTitle != "" {
				group.MediaType = "series"
			}
			groups[key] = group
		}
		group.Activities++
		if !slices.Contains(group.Providers, value.Provider) {
			group.Providers = append(group.Providers, value.Provider)
		}
	}
	result := []Title{}
	for _, group := range groups {
		slices.Sort(group.Providers)
		result = append(result, *group)
	}
	slices.SortFunc(result, func(left, right Title) int {
		if left.Activities != right.Activities {
			return right.Activities - left.Activities
		}
		return strings.Compare(left.ID, right.ID)
	})
	return result
}
func aggregate(records []Record) Overview {
	result := Overview{MediaTypes: []Count{}, MonthlyMedia: []PeriodCount{}, GenresByWeekday: []PeriodCount{}, GenresByYear: []PeriodCount{}, OriginalLanguages: []Count{}, SourceRecordCount: len(records), Services: []Service{}, Months: []Month{}, MatchCoverage: []Count{}, Exclusions: []Count{}, Weekdays: []Count{}, TopTitles: []Title{}, Genres: []Count{}, Devices: []Count{}, AudioLanguages: []Count{}, SubtitleLanguages: []Count{}}
	mediaTypes := map[string]int{}
	monthlyMedia := map[string]int{}
	weekdayGenres := map[string]int{}
	yearlyGenres := map[string]int{}
	languages := map[string]int{}
	services := map[Provider]*Service{}
	months := map[string]int{}
	weekdays := map[string]int{}
	genres := map[string]int{}
	devices := map[string]int{}
	audio := map[string]int{}
	subtitles := map[string]int{}
	matches := map[string]int{}
	exclusions := map[string]int{}
	playbackTitles := map[string]bool{}
	episodes := map[string]bool{}
	for _, record := range records {
		if record.IsConsumption() && record.value.Kind == Playback && record.value.TitleStatus == "present" {
			playbackTitles[string(record.value.Provider)+":"+identity(record.value)] = true
		}
	}
	for _, record := range records {
		value := record.value
		if value.Kind == Purchase {
			if value.OfferType == "RENTAL" {
				result.Rentals++
			} else {
				result.Purchases++
			}
			if playbackTitles[string(value.Provider)+":"+identity(value)] {
				result.PurchaseRecordsWithPlayback++
			}
		}
		if value.Kind == Playback || value.Kind == ActivityEntry {
			if value.RecordedSeconds == nil {
				result.UnknownDurationRecords++
			} else if *value.RecordedSeconds == 0 {
				result.ZeroDurationRecords++
			}
		}
		if !record.IsConsumption() {
			reason := string(value.Kind)
			if value.Kind == Playback {
				reason = value.ContentType
				if record.IsDeleted() {
					reason = "deleted"
				} else if value.RecordedSeconds == nil {
					reason = "unknown_duration"
				} else if *value.RecordedSeconds == 0 {
					reason = "zero_duration"
				}
			}
			exclusions[reason]++
			continue
		}
		result.ActivityCount++
		if value.TitleStatus == "unavailable" {
			result.UnavailableTitleRecords++
		}
		if value.EpisodeIdentity != "" {
			episodes[value.EpisodeIdentity] = true
		}
		service, ok := services[value.Provider]
		if !ok {
			unit := "playback records"
			if value.Provider == Netflix {
				unit = "activity entries"
			}
			service = &Service{Provider: value.Provider, Unit: unit}
			services[value.Provider] = service
		}
		service.Activities++
		if value.RecordedSeconds != nil {
			result.RecordedSeconds += *value.RecordedSeconds
			result.TimedRecords++
			service.RecordedSeconds += *value.RecordedSeconds
			service.TimedRecords++
		}
		months[value.Date[:7]+"|"+string(value.Provider)]++
		date, _ := time.Parse(time.DateOnly, value.Date)
		weekdays[date.Weekday().String()]++
		matches[value.MatchStatus]++
		mediaTypes[mediaType(value)]++
		monthlyMedia[value.Date[:7]+"|"+mediaType(value)]++
		if value.Metadata != nil {
			for _, genre := range value.Metadata.Genres {
				genres[genre]++
				weekdayGenres[date.Weekday().String()+"|"+genre]++
				yearlyGenres[value.Date[:4]+"|"+genre]++
			}
		}
		if value.Metadata != nil && value.Metadata.OriginalLanguage != "" {
			languages[value.Metadata.OriginalLanguage]++
		}
		if value.Device != "" {
			devices[value.Device]++
		}
		if value.AudioLanguage != "" {
			audio[value.AudioLanguage]++
		}
		if value.SubtitleLanguage != "" {
			subtitles[value.SubtitleLanguage]++
		}
	}
	groups := titles(records)
	result.EpisodeCount = len(episodes)
	result.UniqueTitleCount = len(groups)
	for _, title := range groups {
		if title.MediaType == "movie" {
			result.MovieTitles++
		}
		if title.MediaType == "series" {
			result.SeriesTitles++
		}
		if title.MatchStatus == "matched" {
			result.AcceptedTitleCount++
		} else {
			result.UnresolvedTitleCount++
		}
	}
	for _, service := range services {
		result.Services = append(result.Services, *service)
	}
	slices.SortFunc(result.Services, func(left, right Service) int { return strings.Compare(string(left.Provider), string(right.Provider)) })
	for key, count := range months {
		parts := strings.Split(key, "|")
		result.Months = append(result.Months, Month{Month: parts[0], Provider: Provider(parts[1]), Count: count})
	}
	slices.SortFunc(result.Months, func(left, right Month) int {
		return strings.Compare(left.Month+string(left.Provider), right.Month+string(right.Provider))
	})
	result.MediaTypes = counts(mediaTypes)
	result.MonthlyMedia = periodCounts(monthlyMedia)
	result.GenresByWeekday = periodCounts(weekdayGenres)
	result.GenresByYear = periodCounts(yearlyGenres)
	result.OriginalLanguages = counts(languages)
	result.MatchCoverage = counts(matches)
	result.Exclusions = counts(exclusions)
	result.Weekdays = counts(weekdays)
	result.TopTitles = groups[:min(20, len(groups))]
	result.Genres = counts(genres)
	result.Devices = counts(devices)
	result.AudioLanguages = counts(audio)
	result.SubtitleLanguages = counts(subtitles)
	return result
}
func mediaType(value Activity) string {
	if value.Metadata != nil {
		return value.Metadata.MediaType
	}
	if value.SeriesTitle != "" {
		return "series"
	}
	return "unknown"
}
func periodCounts(values map[string]int) []PeriodCount {
	result := []PeriodCount{}
	for key, count := range values {
		period, label, _ := strings.Cut(key, "|")
		result = append(result, PeriodCount{Period: period, Label: label, Count: count})
	}
	slices.SortFunc(result, func(left, right PeriodCount) int {
		return strings.Compare(left.Period+"|"+left.Label, right.Period+"|"+right.Label)
	})
	return result
}
func counts(values map[string]int) []Count {
	result := []Count{}
	for label, count := range values {
		result = append(result, Count{Label: label, Count: count})
	}
	slices.SortFunc(result, func(left, right Count) int {
		if left.Count != right.Count {
			return right.Count - left.Count
		}
		return strings.Compare(left.Label, right.Label)
	})
	return result
}
func sourceSummaries(records []Record, filter Filter) []SourceSummary {
	sources := map[Provider]*SourceSummary{}
	for _, record := range records {
		value := record.value
		if value.DatePrecision == Timestamp {
			instant, _ := time.Parse(time.RFC3339Nano, value.Timestamp)
			value.Date = instant.In(filter.location).Format(time.DateOnly)
		}
		source, ok := sources[value.Provider]
		if !ok {
			source = &SourceSummary{Provider: value.Provider, GenerationID: value.Source.GenerationID, StartDate: value.Date}
			sources[value.Provider] = source
		}
		source.Records++
		if value.Date < source.StartDate {
			source.StartDate = value.Date
		}
		if value.Date > source.EndDate {
			source.EndDate = value.Date
		}
	}
	result := []SourceSummary{}
	for _, source := range sources {
		result = append(result, *source)
	}
	slices.SortFunc(result, func(left, right SourceSummary) int {
		return strings.Compare(string(left.Provider), string(right.Provider))
	})
	return result
}

// ExportCSV streams every filtered source record with its counting unit and provenance.
func ExportCSV(writer io.Writer, records []Record, filter Filter) error {
	csvWriter := csv.NewWriter(writer)
	if err := csvWriter.Write([]string{"provider", "kind", "title", "raw_title", "source_date", "date", "date_precision", "timestamp_utc", "recorded_seconds", "counting_unit", "included_in_consumption", "completion", "content_type", "profile_type", "profile_label", "source_file", "source_row", "generation_id", "match_status", "title_identity", "tmdb_id", "media_type", "metadata_runtime_minutes", "display_timezone", "title_status", "end_timestamp_utc", "interval_status", "series_title", "season_number", "episode_title", "episode_identity", "autoplay", "deleted", "device", "audio_language", "subtitle_language", "offer_type", "description", "imdb_id"}); err != nil {
		return err
	}
	for _, record := range FilterRecords(records, filter) {
		value := record.value
		seconds, tmdbID, mediaType, runtime, imdbID, season := "", "", "", "", "", ""
		unit := string(value.Kind)
		if value.Kind == ActivityEntry {
			unit = "activity entries"
		} else if value.Kind == Playback {
			unit = "playback records"
		}
		if value.RecordedSeconds != nil {
			seconds = strconv.FormatFloat(*value.RecordedSeconds, 'f', -1, 64)
		}
		if value.Metadata != nil {
			tmdbID = strconv.FormatInt(value.Metadata.TMDBID, 10)
			mediaType = value.Metadata.MediaType
			imdbID = value.Metadata.IMDbID
			if value.Metadata.RuntimeMinutes != nil {
				runtime = strconv.Itoa(*value.Metadata.RuntimeMinutes)
			}
		}
		if value.SeasonNumber > 0 {
			season = strconv.Itoa(value.SeasonNumber)
		}
		if err := csvWriter.Write([]string{string(value.Provider), string(value.Kind), value.Title, value.RawTitle, value.SourceDate, value.Date, string(value.DatePrecision), value.Timestamp, seconds, unit, strconv.FormatBool(record.IsConsumption()), value.Completion, value.ContentType, value.ProfileType, value.ProfileLabel, value.Source.File, strconv.Itoa(value.Source.Row), value.Source.GenerationID, value.MatchStatus, identity(value), tmdbID, mediaType, runtime, filter.Timezone, value.TitleStatus, value.EndTimestamp, value.IntervalStatus, value.SeriesTitle, season, value.EpisodeTitle, value.EpisodeIdentity, csvFlag(value.Autoplay), csvFlag(value.Deleted), value.Device, value.AudioLanguage, value.SubtitleLanguage, value.OfferType, value.Description, imdbID}); err != nil {
			return err
		}
	}
	csvWriter.Flush()
	return csvWriter.Error()
}

func csvFlag(value *bool) string {
	if value == nil {
		return ""
	}
	return strconv.FormatBool(*value)
}
