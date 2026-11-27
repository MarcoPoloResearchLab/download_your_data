// Package crunchyroll imports captured profile history with source evidence intact.
package crunchyroll

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/media"
)

const (
	// MaxUploadBytes bounds one complete CSV before parsing.
	MaxUploadBytes = 32 * 1024 * 1024
	// MaxFieldBytes bounds every captured source field.
	MaxFieldBytes = 16 * 1024
	// MaxRows bounds a complete profile history snapshot.
	MaxRows = 250000
	// EmptyRevision identifies a provider without an active import.
	EmptyRevision  = "empty"
	sourceFilename = "crunchyroll_watch_history.csv"
)

var (
	// ErrTooLarge identifies an upload or record limit.
	ErrTooLarge = errors.New("crunchyroll import exceeds its size limit")
	// ErrStaleRevision identifies an obsolete replacement or deletion.
	ErrStaleRevision = errors.New("crunchyroll import revision changed")
	// ErrBusy identifies a concurrent provider writer.
	ErrBusy        = errors.New("crunchyroll provider is busy")
	expectedHeader = []string{"history_date", "series_or_collection", "title", "crunchyroll_status", "format", "crunchyroll_url"}
	watchIDPattern = regexp.MustCompile(`^[A-Z0-9]{3,64}$`)
)

// InputError identifies an invalid source row without exposing its contents.
type InputError struct {
	Row   int
	cause error
}

func (failure *InputError) Error() string {
	return fmt.Sprintf("validate Crunchyroll CSV row %d: %v", failure.Row, failure.cause)
}

// Unwrap exposes the operation failure for typed boundary handling.
func (failure *InputError) Unwrap() error { return failure.cause }

// Generation summarizes one validated, active profile snapshot.
type Generation struct {
	ID          string `json:"id"`
	RecordCount int    `json:"record_count"`
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
}

// Snapshot is the public provider state and its replacement precondition.
type Snapshot struct {
	Revision         string      `json:"revision"`
	ActiveGeneration *Generation `json:"active_generation"`
}

type sourceRow struct {
	HistoryDate string `json:"history_date"`
	Collection  string `json:"series_or_collection"`
	Title       string `json:"title"`
	Status      string `json:"crunchyroll_status"`
	Format      string `json:"format"`
	URL         string `json:"crunchyroll_url"`
	watchID     string
}

// Archive owns a validated ordered snapshot without retaining its input CSV.
type Archive struct {
	rows     []sourceRow
	records  []media.Record
	snapshot Snapshot
}

// Snapshot returns detached provider metadata.
func (archive Archive) Snapshot() Snapshot {
	result := archive.snapshot
	if result.Revision == "" {
		result.Revision = EmptyRevision
	}
	if result.ActiveGeneration != nil {
		generation := *result.ActiveGeneration
		result.ActiveGeneration = &generation
	}
	return result
}

// Records returns detached record storage for shared report construction.
func (archive Archive) Records() []media.Record {
	return slices.Clone(archive.records)
}

// Parse validates the complete bounded CSV before it can become active.
func Parse(ctx context.Context, input io.Reader) (Archive, error) {
	if err := ctx.Err(); err != nil {
		return Archive{}, err
	}
	encoded, err := io.ReadAll(io.LimitReader(input, MaxUploadBytes+1))
	if err != nil {
		return Archive{}, fmt.Errorf("read Crunchyroll CSV: %w", err)
	}
	if len(encoded) > MaxUploadBytes {
		return Archive{}, ErrTooLarge
	}
	if !utf8.Valid(encoded) {
		return Archive{}, invalidInput(1, "invalid UTF-8")
	}
	encoded = bytes.TrimPrefix(encoded, []byte("\xef\xbb\xbf"))
	reader := csv.NewReader(bytes.NewReader(encoded))
	reader.FieldsPerRecord = len(expectedHeader)
	header, err := reader.Read()
	if err != nil || !slices.Equal(header, expectedHeader) {
		return Archive{}, invalidInput(1, "expected the captured profile-history columns")
	}
	rows := []sourceRow{}
	for rowNumber := 2; ; rowNumber++ {
		if err = ctx.Err(); err != nil {
			return Archive{}, err
		}
		fields, readError := reader.Read()
		if errors.Is(readError, io.EOF) {
			break
		}
		if readError != nil {
			return Archive{}, &InputError{Row: rowNumber, cause: readError}
		}
		if len(rows) >= MaxRows {
			return Archive{}, ErrTooLarge
		}
		row := sourceRow{HistoryDate: fields[0], Collection: fields[1], Title: fields[2], Status: fields[3], Format: fields[4], URL: fields[5]}
		if err = row.validate(rowNumber); err != nil {
			return Archive{}, err
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return Archive{}, invalidInput(2, "at least one history entry is required")
	}
	return buildArchive(ctx, rows)
}

func invalidInput(row int, message string) error {
	return &InputError{Row: row, cause: errors.New(message)}
}

func (row *sourceRow) validate(ordinal int) error {
	fields := []string{row.HistoryDate, row.Collection, row.Title, row.Status, row.Format, row.URL}
	for _, field := range fields {
		if strings.TrimSpace(field) == "" || len(field) > MaxFieldBytes || !utf8.ValidString(field) || strings.ContainsRune(field, '\x00') {
			return invalidInput(ordinal, "fields must contain bounded UTF-8 text")
		}
	}
	if len(row.Title) > 8192 || len(row.Collection) > 8192 {
		return invalidInput(ordinal, "title or collection exceeds its size limit")
	}
	date, err := time.Parse(time.DateOnly, row.HistoryDate)
	if err != nil || date.Year() < 1 || date.Format(time.DateOnly) != row.HistoryDate {
		return invalidInput(ordinal, "invalid calendar date")
	}
	switch row.Format {
	case media.CrunchyrollEpisode, media.CrunchyrollMovie, media.CrunchyrollTheatricalSpecial:
	default:
		return invalidInput(ordinal, "invalid captured item format")
	}
	row.watchID, err = watchID(row.URL)
	if err != nil {
		return invalidInput(ordinal, "invalid Crunchyroll watch URL")
	}
	return nil
}

func watchID(rawURL string) (string, error) {
	target, err := url.Parse(rawURL)
	if err != nil || target.Scheme != "https" || target.Host != "www.crunchyroll.com" || target.User != nil || target.RawQuery != "" || target.ForceQuery || strings.Contains(rawURL, "#") || target.Opaque != "" {
		return "", errors.New("invalid watch URL")
	}
	parts := strings.Split(target.Path, "/")
	if len(parts) < 3 || len(parts) > 4 || parts[0] != "" || parts[1] != "watch" || !watchIDPattern.MatchString(parts[2]) {
		return "", errors.New("invalid watch URL path")
	}
	// The item identity comes from the literal URL segment.
	if !strings.HasPrefix(target.EscapedPath(), "/watch/"+parts[2]) {
		return "", errors.New("encoded watch identity")
	}
	if len(parts) == 4 {
		slug := parts[3]
		if slug == "" || slug == "." || slug == ".." || strings.ContainsAny(slug, "\\\r\n\t\x00") {
			return "", errors.New("invalid watch URL slug")
		}
	}
	return parts[2], nil
}

func buildArchive(ctx context.Context, rows []sourceRow) (Archive, error) {
	canonical, err := json.Marshal(rows)
	if err != nil {
		return Archive{}, fmt.Errorf("encode Crunchyroll source identity: %w", err)
	}
	digest := sha256.Sum256(canonical)
	revision := hex.EncodeToString(digest[:])
	generation := Generation{ID: revision, RecordCount: len(rows), StartDate: rows[0].HistoryDate, EndDate: rows[0].HistoryDate}
	records := make([]media.Record, 0, len(rows))
	for ordinal, row := range rows {
		if err = ctx.Err(); err != nil {
			return Archive{}, err
		}
		value := media.Activity{
			Provider: media.Crunchyroll, Kind: media.ActivityEntry, Title: row.Title, RawTitle: row.Title,
			SourceDate: row.HistoryDate, Date: row.HistoryDate, DatePrecision: media.CalendarDate,
			Completion: "unknown", ContentType: "content", ProfileType: "unknown", MatchStatus: "not_enriched",
			SearchTitle: row.Title,
			Source:      media.Source{File: sourceFilename, Row: ordinal + 2, GenerationID: revision, Collection: row.Collection, Format: row.Format, Status: row.Status, URL: row.URL},
		}
		if row.Format == media.CrunchyrollEpisode {
			itemDigest := sha256.Sum256([]byte(row.watchID))
			value.SeriesTitle = row.Collection
			value.SearchTitle = row.Collection
			value.EpisodeTitle = row.Title
			value.EpisodeIdentity = hex.EncodeToString(itemDigest[:])
		}
		record, recordError := media.NewRecord(value)
		if recordError != nil {
			return Archive{}, fmt.Errorf("construct Crunchyroll row %d: %w", ordinal+2, recordError)
		}
		records = append(records, record)
		if row.HistoryDate < generation.StartDate {
			generation.StartDate = row.HistoryDate
		}
		if row.HistoryDate > generation.EndDate {
			generation.EndDate = row.HistoryDate
		}
	}
	return Archive{rows: rows, records: records, snapshot: Snapshot{Revision: revision, ActiveGeneration: &generation}}, nil
}
