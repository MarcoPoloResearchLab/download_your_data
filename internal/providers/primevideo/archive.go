// Package primevideo owns the current Amazon Prime Video export boundary.
package primevideo

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/media"
)

const (
	MaxUploadBytes      int64  = 64 * 1024 * 1024
	MaxExpandedBytes    uint64 = 256 * 1024 * 1024
	MaxFiles                   = 128
	MaxRows                    = 250000
	MaxFieldBytes              = 16 * 1024
	MaxCompressionRatio        = 100
	ViewingDataset             = "viewing"
	DetailsDataset             = "playback_details"
	WatchDataset               = "watch_events"
	SearchDataset              = "searches"
	PurchaseDataset            = "purchases"
	TrailerDataset             = "trailers"
	viewingFile                = "Your Prime Video Viewing Activity/Viewing History.csv"
	watchFile                  = "Your Prime Video Viewing Activity/Watch Events.csv"
	searchFile                 = "Your Prime Video Viewing Activity/Search History.csv"
	purchaseFile               = "Your Prime Video Library & Purchases/Purchases and Rentals.csv"
	trailerFile                = "Your Prime Video Viewing Activity/Promotional Trailers Viewed.csv"
)

//go:embed headers.json
var schemaBytes []byte

// Error is a safe boundary failure with a source row but no source contents.
type Error struct {
	Code  string
	Row   int
	cause error
}

func (failure *Error) Error() string {
	return fmt.Sprintf("Prime Video operation: %s (row %d)", failure.Code, failure.Row)
}
func (failure *Error) Unwrap() error { return failure.cause }
func newError(code string, row int, cause error) error {
	return &Error{Code: code, Row: row, cause: cause}
}

// Dataset describes a supported selectable import dataset.
type Dataset struct {
	ID        string `json:"id"`
	File      string `json:"file"`
	Rows      int    `json:"rows"`
	StartDate string `json:"start_date,omitempty"`
	EndDate   string `json:"end_date,omitempty"`
}

// Preview contains only counts, coverage, and archive names.
type Preview struct {
	Datasets         []Dataset `json:"datasets"`
	UnsupportedFiles []string  `json:"unsupported_files"`
	SourceHash       string    `json:"source_hash"`
}

// Bundle is a fully validated import awaiting dataset selection.
type Bundle struct {
	preview Preview
	records []media.Record
}

func (bundle Bundle) Preview() Preview {
	result := bundle.preview
	result.Datasets = slices.Clone(result.Datasets)
	result.UnsupportedFiles = slices.Clone(result.UnsupportedFiles)
	return result
}
func (bundle Bundle) Records() []media.Record { return slices.Clone(bundle.records) }

// ParseArchive validates the complete current recognized export without extracting files.
func ParseArchive(ctx context.Context, encoded []byte) (Bundle, error) {
	if int64(len(encoded)) > MaxUploadBytes {
		return Bundle{}, newError("upload_too_large", 0, nil)
	}
	archive, err := zip.NewReader(bytes.NewReader(encoded), int64(len(encoded)))
	if err != nil {
		return Bundle{}, newError("invalid_archive", 0, err)
	}
	if len(archive.File) > MaxFiles {
		return Bundle{}, newError("limit_exceeded", 0, nil)
	}
	var schemas map[string][]string
	if err = json.Unmarshal(schemaBytes, &schemas); err != nil {
		return Bundle{}, fmt.Errorf("read Prime schemas: %w", err)
	}
	digest := sha256.Sum256(encoded)
	bundle := Bundle{preview: Preview{Datasets: []Dataset{}, UnsupportedFiles: []string{}, SourceHash: hex.EncodeToString(digest[:])}, records: []media.Record{}}
	seen := make(map[string]bool)
	var expanded uint64
	for _, file := range archive.File {
		if err = ctx.Err(); err != nil {
			return Bundle{}, newError("canceled", 0, err)
		}
		name := file.Name
		cleanName := strings.TrimSuffix(name, "/")
		parts := strings.Split(cleanName, "/")
		if cleanName == "" || path.IsAbs(name) || path.Clean(cleanName) != cleanName || slices.Contains(parts, "..") || slices.Contains(parts, ".") || strings.ContainsAny(name, "\\\x00") || strings.Contains(parts[0], ":") {
			return Bundle{}, newError("unsafe_archive_path", 0, nil)
		}
		if seen[name] {
			return Bundle{}, newError("duplicate_archive_entry", 0, nil)
		}
		seen[name] = true
		if file.UncompressedSize64 > MaxExpandedBytes-expanded {
			return Bundle{}, newError("limit_exceeded", 0, nil)
		}
		expanded += file.UncompressedSize64
		if file.UncompressedSize64 > 1024*1024 && (file.CompressedSize64 == 0 || file.UncompressedSize64/file.CompressedSize64 > MaxCompressionRatio) {
			return Bundle{}, newError("limit_exceeded", 0, nil)
		}
		if strings.HasSuffix(name, "/") {
			continue
		}
		header, recognized := schemas[name]
		if !recognized {
			bundle.preview.UnsupportedFiles = append(bundle.preview.UnsupportedFiles, name)
			continue
		}
		rows, err := readCSV(ctx, file, header)
		if err != nil {
			return Bundle{}, err
		}
		if len(bundle.records)+len(rows) > MaxRows {
			return Bundle{}, newError("limit_exceeded", 0, nil)
		}
		kind, datasetID := fileKind(name)
		dataset := Dataset{ID: datasetID, File: name, Rows: len(rows)}
		for index, row := range rows {
			record, err := parseRecord(name, index+2, kind, row)
			if err != nil {
				return Bundle{}, newError("invalid_row", index+2, err)
			}
			value := record.Snapshot()
			if dataset.StartDate == "" || value.Date < dataset.StartDate {
				dataset.StartDate = value.Date
			}
			if value.Date > dataset.EndDate {
				dataset.EndDate = value.Date
			}
			bundle.records = append(bundle.records, record)
		}
		bundle.preview.Datasets = append(bundle.preview.Datasets, dataset)
		if name == viewingFile {
			details := dataset
			details.ID = DetailsDataset
			bundle.preview.Datasets = append(bundle.preview.Datasets, details)
		}
	}
	if !seen[viewingFile] {
		return Bundle{}, newError("missing_viewing_history", 0, nil)
	}
	slices.SortFunc(bundle.preview.Datasets, func(left, right Dataset) int { return strings.Compare(left.ID, right.ID) })
	slices.Sort(bundle.preview.UnsupportedFiles)
	return bundle, nil
}

func readCSV(ctx context.Context, file *zip.File, header []string) ([]map[string]string, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, newError("invalid_archive", 0, err)
	}
	contents, readErr := io.ReadAll(io.LimitReader(reader, int64(MaxExpandedBytes)+1))
	closeErr := reader.Close()
	if err = errors.Join(readErr, closeErr); err != nil {
		return nil, newError("invalid_archive", 0, err)
	}
	if uint64(len(contents)) > MaxExpandedBytes || uint64(len(contents)) != file.UncompressedSize64 {
		return nil, newError("limit_exceeded", 0, nil)
	}
	parser := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(contents), "\ufeff")))
	columns, err := parser.Read()
	if err != nil || !slices.Equal(columns, header) {
		return nil, newError("invalid_header", 1, err)
	}
	result := []map[string]string{}
	for {
		if err = ctx.Err(); err != nil {
			return nil, newError("canceled", 0, err)
		}
		fields, err := parser.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, newError("invalid_csv", len(result)+2, err)
		}
		if len(result) >= MaxRows {
			return nil, newError("limit_exceeded", len(result)+2, nil)
		}
		row := make(map[string]string, len(columns))
		for index, field := range fields {
			if len(field) > MaxFieldBytes || !utf8.ValidString(field) || strings.ContainsRune(field, 0) {
				return nil, newError("invalid_field", len(result)+2, nil)
			}
			row[columns[index]] = field
		}
		result = append(result, row)
	}
	return result, nil
}

func fileKind(name string) (media.Kind, string) {
	switch name {
	case viewingFile:
		return media.Playback, ViewingDataset
	case watchFile:
		return media.WatchSummary, WatchDataset
	case searchFile:
		return media.Search, SearchDataset
	case purchaseFile:
		return media.Purchase, PurchaseDataset
	case trailerFile:
		return media.Trailer, TrailerDataset
	}
	return "", ""
}

func normalized(raw string) string {
	value := strings.TrimSpace(raw)
	if len(value) >= 2 && strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
		value = value[1 : len(value)-1]
	}
	switch value {
	case "Not Available", "Not available", "Not Applicable", "Not applicable", "":
		return ""
	}
	return value
}

func parseRecord(file string, rowNumber int, kind media.Kind, row map[string]string) (media.Record, error) {
	value := media.Activity{Provider: media.PrimeVideo, Kind: kind, DatePrecision: media.Timestamp, Completion: "unknown", ContentType: "unknown", ProfileType: "unknown", MatchStatus: "not_enriched", Source: media.Source{File: file, Row: rowNumber}}
	var date, seconds string
	switch kind {
	case media.Playback:
		value.RawTitle = row["Title"]
		date = row["Playback Start Datetime (UTC)"]
		seconds = row["Seconds Viewed"]
		end, err := parseTimestamp(row["Playback End Datetime (UTC)"])
		if err != nil {
			return media.Record{}, err
		}
		start, err := parseTimestamp(date)
		if err != nil {
			return media.Record{}, errors.New("invalid playback interval")
		}
		value.IntervalStatus = "end_unavailable"
		if !end.IsZero() {
			value.EndTimestamp = end.Format(time.RFC3339Nano)
			value.IntervalStatus = "ordered"
			if end.Before(start) {
				value.IntervalStatus = "end_before_start"
			}
		}
		switch normalized(row["Material Type Description"]) {
		case "Full", "Feature", "Short":
			value.ContentType = "content"
		case "Promo":
			value.ContentType = "promotion"
		case "Trailer":
			value.ContentType = "trailer"
		case "Live", "LiveStreaming":
			value.ContentType = "live"
		case "":
		default:
			return media.Record{}, errors.New("unknown material type")
		}
		switch normalized(row["Profile Type"]) {
		case "ADULT":
			value.ProfileType = "adult"
		case "CHILD":
			value.ProfileType = "child"
		case "":
		default:
			return media.Record{}, errors.New("unknown profile type")
		}
		autoplay, err := parseBoolean(row["Is Autoplay"])
		if err != nil {
			return media.Record{}, err
		}
		value.Autoplay = autoplay
		deleted, err := parseBoolean(row["Is Deleted"])
		if err != nil {
			return media.Record{}, err
		}
		value.Deleted = deleted
		value.Device = normalized(row["Device Model"])
		value.AudioLanguage = normalized(row["Audio Language Code"])
		value.SubtitleLanguage = normalized(row["Subtitle Language Code"])
		for _, column := range []string{"Start Point Seconds", "End Point Seconds", "Seconds Paused", "Pause Count", "Rewind Count", "Fast Forward Count", "Seconds Buffered", "Buffer Count", "Error Count", "Fatal Error Count", "Dropped Frame Count", "Average Bandwidth", "Video Duration in 1080p", "Video Duration in 2K", "Video Duration in 4K", "Video Duration in 720p", "Video Duration in Full-SD", "Video Duration in SD", "Video Duration in Sub-SD"} {
			if _, err := parseSeconds(row[column]); err != nil {
				return media.Record{}, err
			}
		}
	case media.WatchSummary:
		value.RawTitle = row["Title Name"]
		date = row["Most Recent Watch Date"]
		seconds = row["Seconds Watched"]
		value.Description = normalized(row["Title Description"])
		deleted, err := parseBoolean(row["Deleted from Watch History"])
		if err != nil {
			return media.Record{}, err
		}
		value.Deleted = deleted
	case media.Search:
		value.RawTitle = row["Search Query from Customer"]
		date = row["Search Request Date"]
	case media.Purchase:
		value.RawTitle = row["Title"]
		date = row["Origin Time"]
		value.OfferType = normalized(row["Offer Type"])
		if value.OfferType != "RENTAL" && value.OfferType != "PURCHASE" {
			return media.Record{}, errors.New("invalid offer type")
		}
		for _, column := range []string{"Grant Time", "Rental Expiry Time", "Rental Playback Start Time"} {
			if _, err := parseTimestamp(row[column]); err != nil {
				return media.Record{}, err
			}
		}
	case media.Trailer:
		value.RawTitle = row["Promoting Titles or Services"]
		date = row["Trailer Watch Date"]
		value.ContentType = "trailer"
	}
	value.Title = normalized(value.RawTitle)
	if kind != media.Search && kind != media.Trailer {
		deriveTitle(&value)
	}
	value.SourceDate = date
	timestamp, err := parseTimestamp(date)
	if err != nil || timestamp.IsZero() {
		return media.Record{}, errors.New("missing or invalid timestamp")
	}
	value.Timestamp = timestamp.Format(time.RFC3339Nano)
	value.RecordedSeconds, err = parseSeconds(seconds)
	if err != nil {
		return media.Record{}, err
	}
	return media.NewRecord(value)
}

func parseTimestamp(raw string) (time.Time, error) {
	value := normalized(raw)
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || !strings.HasSuffix(value, "Z") || parsed.Year() < 2006 || parsed.Year() > time.Now().UTC().Year()+1 {
		return time.Time{}, errors.New("invalid source timestamp")
	}
	return parsed, nil
}

func parseSeconds(raw string) (*float64, error) {
	value := normalized(raw)
	if value == "" {
		return nil, nil
	}
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil || seconds < 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return nil, errors.New("invalid source numeric value")
	}
	return &seconds, nil
}

func parseBoolean(raw string) (*bool, error) {
	switch normalized(raw) {
	case "Yes", "yes":
		value := true
		return &value, nil
	case "No", "no":
		value := false
		return &value, nil
	case "":
		return nil, nil
	}
	return nil, errors.New("invalid source boolean")
}
