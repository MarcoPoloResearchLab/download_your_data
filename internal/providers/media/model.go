// Package media owns the shared, source-aware screen-media analysis contract.
package media

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// Provider identifies the source service.
type Provider string

const (
	Netflix    Provider = "netflix"
	PrimeVideo Provider = "prime-video"
)

// Kind identifies what a source row can prove.
type Kind string

const (
	ActivityEntry Kind = "activity"
	Playback      Kind = "playback"
	WatchSummary  Kind = "watch_summary"
	Search        Kind = "search"
	Purchase      Kind = "purchase"
	Trailer       Kind = "trailer"
)

// Precision preserves the source's temporal resolution.
type Precision string

const (
	CalendarDate Precision = "calendar_date"
	Timestamp    Precision = "timestamp"
)

// Source identifies the original input row without storing source bytes.
type Source struct {
	File         string `json:"file"`
	Row          int    `json:"row"`
	GenerationID string `json:"generation_id"`
}

// Metadata is accepted title evidence shared by both provider adapters.
type Metadata struct {
	MediaType        string   `json:"media_type"`
	TMDBID           int64    `json:"tmdb_id"`
	Title            string   `json:"title"`
	Genres           []string `json:"genres"`
	OriginalLanguage string   `json:"original_language,omitempty"`
	RuntimeMinutes   *int     `json:"runtime_minutes,omitempty"`
	ReleaseDate      string   `json:"release_date,omitempty"`
	IMDbID           string   `json:"imdb_id,omitempty"`
}

// Activity is the transport and persistence representation of one validated record.
type Activity struct {
	ID               string    `json:"id"`
	Provider         Provider  `json:"provider"`
	Kind             Kind      `json:"kind"`
	Title            string    `json:"title"`
	RawTitle         string    `json:"raw_title"`
	TitleStatus      string    `json:"title_status"`
	SourceDate       string    `json:"source_date"`
	DatePrecision    Precision `json:"date_precision"`
	Timestamp        string    `json:"timestamp,omitempty"`
	EndTimestamp     string    `json:"end_timestamp,omitempty"`
	IntervalStatus   string    `json:"interval_status,omitempty"`
	Date             string    `json:"date"`
	RecordedSeconds  *float64  `json:"recorded_seconds"`
	Completion       string    `json:"completion"`
	ContentType      string    `json:"content_type"`
	ProfileType      string    `json:"profile_type"`
	ProfileLabel     string    `json:"profile_label,omitempty"`
	Autoplay         *bool     `json:"autoplay"`
	Deleted          *bool     `json:"deleted"`
	Device           string    `json:"device,omitempty"`
	AudioLanguage    string    `json:"audio_language,omitempty"`
	SubtitleLanguage string    `json:"subtitle_language,omitempty"`
	OfferType        string    `json:"offer_type,omitempty"`
	Description      string    `json:"description,omitempty"`
	Source           Source    `json:"source"`
	SearchTitle      string    `json:"search_title"`
	SeriesTitle      string    `json:"series_title,omitempty"`
	SeasonNumber     int       `json:"season_number,omitempty"`
	EpisodeTitle     string    `json:"episode_title,omitempty"`
	EpisodeIdentity  string    `json:"episode_identity,omitempty"`
	TitleIdentity    string    `json:"title_identity"`
	MatchStatus      string    `json:"match_status"`
	Metadata         *Metadata `json:"metadata,omitempty"`
}

// Record has validated source and measurement invariants.
type Record struct{ value Activity }

// NewRecord validates a source or persistence boundary exactly once.
func NewRecord(value Activity) (Record, error) {
	if value.Provider != Netflix && value.Provider != PrimeVideo {
		return Record{}, errors.New("media.invalid_provider")
	}
	if (value.Provider == Netflix && (value.Kind != ActivityEntry || value.DatePrecision != CalendarDate || value.RecordedSeconds != nil)) || (value.Provider == PrimeVideo && (value.Kind == ActivityEntry || value.DatePrecision != Timestamp)) {
		return Record{}, errors.New("media.invalid_source_contract")
	}
	switch value.Kind {
	case ActivityEntry, Playback, WatchSummary, Search, Purchase, Trailer:
	default:
		return Record{}, errors.New("media.invalid_kind")
	}
	if len(value.Title) > 8192 || len(value.RawTitle) > 8192 || value.Source.File == "" || value.Source.Row < 2 {
		return Record{}, errors.New("media.invalid_source")
	}
	if value.Title == "" {
		value.TitleStatus = "unavailable"
	} else {
		value.TitleStatus = "present"
	}
	switch value.DatePrecision {
	case CalendarDate:
		parsed, err := time.Parse(time.DateOnly, value.Date)
		if err != nil || parsed.Format(time.DateOnly) != value.Date || value.Timestamp != "" || value.SourceDate == "" {
			return Record{}, errors.New("media.invalid_date")
		}
	case Timestamp:
		parsed, err := time.Parse(time.RFC3339Nano, value.Timestamp)
		if err != nil || parsed.Location() != time.UTC {
			return Record{}, errors.New("media.invalid_timestamp")
		}
		value.Date = parsed.Format(time.DateOnly)
	default:
		return Record{}, errors.New("media.invalid_precision")
	}
	if value.RecordedSeconds != nil && (math.IsNaN(*value.RecordedSeconds) || math.IsInf(*value.RecordedSeconds, 0) || *value.RecordedSeconds < 0) {
		return Record{}, errors.New("media.invalid_seconds")
	}
	if value.Completion != "unknown" {
		return Record{}, errors.New("media.invalid_completion")
	}
	switch value.ContentType {
	case "content", "promotion", "trailer", "live", "unknown":
	default:
		return Record{}, errors.New("media.invalid_content_type")
	}
	switch value.ProfileType {
	case "adult", "child", "unknown":
	default:
		return Record{}, errors.New("media.invalid_profile_type")
	}
	if len(value.ProfileLabel) > 200 || strings.ContainsAny(value.ProfileLabel, "\r\n\x00") {
		return Record{}, errors.New("media.invalid_profile_label")
	}
	switch value.MatchStatus {
	case "not_enriched", "matched", "review", "unmatched":
	default:
		return Record{}, errors.New("media.invalid_match_status")
	}
	if (value.MatchStatus == "matched") != (value.Metadata != nil) {
		return Record{}, errors.New("media.invalid_metadata_state")
	}
	if value.Metadata != nil && (value.Metadata.TMDBID <= 0 || (value.Metadata.MediaType != "movie" && value.Metadata.MediaType != "series") || value.Metadata.Title == "") {
		return Record{}, errors.New("media.invalid_metadata")
	}
	if value.SeasonNumber < 0 || value.SeasonNumber > 999 || (value.SeasonNumber > 0) != (value.SeriesTitle != "") || (value.EpisodeTitle != "" && value.SeriesTitle == "") {
		return Record{}, errors.New("media.invalid_episode")
	}
	if value.EpisodeTitle != "" {
		digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%s", value.Provider, value.SeriesTitle, value.SeasonNumber, value.EpisodeTitle)))
		value.EpisodeIdentity = hex.EncodeToString(digest[:])
	}
	if value.SearchTitle == "" {
		value.SearchTitle = value.Title
	}
	if value.TitleIdentity == "" {
		identityInput := string(value.Provider) + "\x00" + value.SearchTitle
		if value.TitleStatus == "unavailable" {
			identityInput += fmt.Sprintf("\x00%s\x00%d\x00%s", value.Source.File, value.Source.Row, value.SourceDate)
		}
		digest := sha256.Sum256([]byte(identityInput))
		value.TitleIdentity = hex.EncodeToString(digest[:])
	}
	if value.ID == "" {
		digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%s\x00%s", value.Provider, value.Source.File, value.Source.Row, value.RawTitle, value.SourceDate)))
		value.ID = hex.EncodeToString(digest[:])
	}
	return Record{value: cloneActivity(value)}, nil
}

// Snapshot returns detached reader data.
func (record Record) Snapshot() Activity { return cloneActivity(record.value) }

// WithSource attaches a validated generation and user-supplied profile label.
func (record Record) WithSource(generationID, profileLabel string) Record {
	value := record.Snapshot()
	value.Source.GenerationID = generationID
	value.ProfileLabel = profileLabel
	return Record{value: value}
}

// WithoutPlaybackDetails drops optional device and language fields.
func (record Record) WithoutPlaybackDetails() Record {
	value := record.Snapshot()
	value.Device = ""
	value.AudioLanguage = ""
	value.SubtitleLanguage = ""
	return Record{value: value}
}

// IsConsumption reports membership in the declared default activity measure.
func (record Record) IsConsumption() bool {
	value := record.value
	return value.Kind == ActivityEntry || (value.Kind == Playback && value.ContentType == "content" && !record.IsDeleted() && value.RecordedSeconds != nil && *value.RecordedSeconds > 0)
}

// IsDeleted reports a positive source assertion of history deletion.
func (record Record) IsDeleted() bool { return record.value.Deleted != nil && *record.value.Deleted }

func cloneActivity(value Activity) Activity {
	if value.Autoplay != nil {
		flag := *value.Autoplay
		value.Autoplay = &flag
	}
	if value.Deleted != nil {
		flag := *value.Deleted
		value.Deleted = &flag
	}
	if value.RecordedSeconds != nil {
		seconds := *value.RecordedSeconds
		value.RecordedSeconds = &seconds
	}
	if value.Metadata != nil {
		metadata := *value.Metadata
		metadata.Genres = append([]string{}, metadata.Genres...)
		if metadata.RuntimeMinutes != nil {
			minutes := *metadata.RuntimeMinutes
			metadata.RuntimeMinutes = &minutes
		}
		value.Metadata = &metadata
	}
	return value
}
