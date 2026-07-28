package netflix

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

const (
	topGenreLimit         = 20
	genreYearSeriesLimit  = 8
	weekdayGenreLimit     = 10
	topLanguageLimit      = 15
	topTitleLimit         = 20
	recentMonthLimit      = 12
	unknownDimensionLabel = "unknown"
	movieDimensionLabel   = string(MediaTypeMovie)
	seriesDimensionLabel  = string(MediaTypeSeries)
)

var (
	// ErrInvalidDateRange identifies an unusable analytics date range.
	ErrInvalidDateRange = errors.New("invalid Netflix analytics date range")
)

// DateRange is a validated inclusive viewing-date filter.
type DateRange struct {
	all   bool
	start LocalDate
	end   LocalDate
}

// AllDates returns the explicit unbounded date filter.
func AllDates() DateRange {
	return DateRange{all: true}
}

// NewDateRange constructs an inclusive bounded date filter.
func NewDateRange(start LocalDate, end LocalDate) (DateRange, error) {
	if !start.valid() || !end.valid() {
		return DateRange{}, fmt.Errorf(
			"%w: validated start and end dates are required",
			ErrInvalidDateRange,
		)
	}
	if end.before(start) {
		return DateRange{}, fmt.Errorf(
			"%w: end date precedes start date",
			ErrInvalidDateRange,
		)
	}
	return DateRange{start: start, end: end}, nil
}

func (dateRange DateRange) valid() bool {
	if dateRange.all {
		return !dateRange.start.valid() && !dateRange.end.valid()
	}
	return dateRange.start.valid() &&
		dateRange.end.valid() &&
		!dateRange.end.before(dateRange.start)
}

func (dateRange DateRange) includes(date LocalDate) bool {
	if dateRange.all {
		return true
	}
	return !date.before(dateRange.start) && !date.after(dateRange.end)
}

// Count is one deterministic categorical count.
type Count struct {
	Label string `json:"label"`
	Value int    `json:"value"`
}

// Series is one named integer series aligned with an adjacent label axis.
type Series struct {
	Label  string `json:"label"`
	Values []int  `json:"values"`
}

// Analytics is the deterministic Netflix activity summary consumed by future
// HTTP and browser adapters.
type Analytics struct {
	ActivityCount         int      `json:"activity_count"`
	UniqueTitleCount      int      `json:"unique_title_count"`
	MetadataActivityCount int      `json:"metadata_activity_count"`
	MetadataTitleCount    int      `json:"metadata_title_count"`
	MatchStatusActivities []Count  `json:"match_status_activities"`
	MatchStatusTitles     []Count  `json:"match_status_titles"`
	StartDate             string   `json:"start_date"`
	EndDate               string   `json:"end_date"`
	MediaTypes            []Count  `json:"media_types"`
	Genres                []Count  `json:"genres"`
	ViewingYears          []int    `json:"viewing_years"`
	GenresByViewingYear   []Series `json:"genres_by_viewing_year"`
	MonthLabels           []string `json:"month_labels"`
	MonthlyMedia          []Series `json:"monthly_media"`
	Languages             []Count  `json:"languages"`
	WeekdayLabels         []string `json:"weekday_labels"`
	GenresByWeekday       []Series `json:"genres_by_weekday"`
	TopTitles             []Count  `json:"top_titles"`
}

// Aggregate produces all maintained Netflix activity measures from validated
// records through one shared date filter.
func Aggregate(
	ctx context.Context,
	records []ActivityRecord,
	dateRange DateRange,
) (Analytics, error) {
	if ctx == nil {
		return Analytics{}, errors.New("aggregate Netflix activities: context is required")
	}
	if cancellationError := ctx.Err(); cancellationError != nil {
		return Analytics{}, fmt.Errorf(
			"aggregate Netflix activities: %w",
			cancellationError,
		)
	}
	if !dateRange.valid() {
		return Analytics{}, ErrInvalidDateRange
	}
	for recordIndex, record := range records {
		if !record.valid() {
			return Analytics{}, fmt.Errorf(
				"aggregate Netflix activities: record %d: %w",
				recordIndex+1,
				ErrInvalidActivityRecord,
			)
		}
	}

	analytics := emptyAnalytics()
	uniqueTitles := make(map[string]struct{})
	metadataTitles := make(map[string]struct{})
	matchStatusByTitle := make(map[string]MatchStatus)
	matchActivityCounts := make(map[string]int)
	mediaTypeCounts := make(map[string]int)
	genreCounts := make(map[string]int)
	genreYearCounts := make(map[int]map[string]int)
	monthMediaCounts := make(map[string]map[string]int)
	languageCounts := make(map[string]int)
	weekdayGenreCounts := make(map[int]map[string]int)
	titleCounts := make(map[string]int)
	var earliestDate LocalDate
	var latestDate LocalDate

	for _, record := range records {
		if cancellationError := ctx.Err(); cancellationError != nil {
			return Analytics{}, fmt.Errorf(
				"aggregate Netflix activities: %w",
				cancellationError,
			)
		}
		activity := record.Activity()
		viewingDate := activity.Date()
		if !dateRange.includes(viewingDate) {
			continue
		}

		analytics.ActivityCount++
		if !earliestDate.valid() || viewingDate.before(earliestDate) {
			earliestDate = viewingDate
		}
		if !latestDate.valid() || viewingDate.after(latestDate) {
			latestDate = viewingDate
		}
		identity := activity.TitleIdentity()
		uniqueTitles[identity.Key()] = struct{}{}
		titleCounts[identity.SearchTitle()]++
		if match, hasMatch := record.Match(); hasMatch {
			if priorStatus, exists := matchStatusByTitle[identity.Key()]; exists &&
				priorStatus != match.Status() {
				return Analytics{}, fmt.Errorf(
					"aggregate Netflix activities: title identity has inconsistent match outcomes",
				)
			}
			matchStatusByTitle[identity.Key()] = match.Status()
			matchActivityCounts[string(match.Status())]++
		}

		mediaType := unknownDimensionLabel
		language := unknownDimensionLabel
		genres := []string(nil)
		metadata, hasMetadata := record.Metadata()
		if hasMetadata {
			analytics.MetadataActivityCount++
			metadataTitles[identity.Key()] = struct{}{}
			mediaType = string(metadata.MediaType())
			if metadata.OriginalLanguage() != "" {
				language = metadata.OriginalLanguage()
			}
			genres = metadata.Genres()
		}
		mediaTypeCounts[mediaType]++
		languageCounts[language]++
		for _, genre := range genres {
			genreCounts[genre]++
		}

		viewingYear := viewingDate.Year()
		if _, exists := genreYearCounts[viewingYear]; !exists {
			genreYearCounts[viewingYear] = make(map[string]int)
		}
		for _, genre := range genres {
			genreYearCounts[viewingYear][genre]++
		}

		monthLabel := fmt.Sprintf("%04d-%02d", viewingYear, viewingDate.Month())
		if _, exists := monthMediaCounts[monthLabel]; !exists {
			monthMediaCounts[monthLabel] = make(map[string]int)
		}
		monthMediaCounts[monthLabel][mediaType]++

		weekdayIndex := (int(viewingDate.Weekday()) + 6) % 7
		if _, exists := weekdayGenreCounts[weekdayIndex]; !exists {
			weekdayGenreCounts[weekdayIndex] = make(map[string]int)
		}
		for _, genre := range genres {
			weekdayGenreCounts[weekdayIndex][genre]++
		}
	}

	analytics.UniqueTitleCount = len(uniqueTitles)
	analytics.MetadataTitleCount = len(metadataTitles)
	matchTitleCounts := make(map[string]int)
	for _, status := range matchStatusByTitle {
		matchTitleCounts[string(status)]++
	}
	analytics.MatchStatusActivities = orderedMatchCounts(matchActivityCounts)
	analytics.MatchStatusTitles = orderedMatchCounts(matchTitleCounts)
	if analytics.ActivityCount == 0 {
		return analytics, nil
	}
	analytics.StartDate = earliestDate.ISO()
	analytics.EndDate = latestDate.ISO()
	analytics.MediaTypes = sortedCounts(mediaTypeCounts, 0)
	analytics.Genres = sortedCounts(genreCounts, topGenreLimit)
	analytics.Languages = sortedCounts(languageCounts, topLanguageLimit)
	analytics.TopTitles = sortedCounts(titleCounts, topTitleLimit)

	analytics.ViewingYears = sortedYears(genreYearCounts)
	yearGenres := topLabels(genreCounts, genreYearSeriesLimit)
	analytics.GenresByViewingYear = make([]Series, 0, len(yearGenres))
	for _, genre := range yearGenres {
		values := make([]int, len(analytics.ViewingYears))
		for yearIndex, year := range analytics.ViewingYears {
			values[yearIndex] = genreYearCounts[year][genre]
		}
		analytics.GenresByViewingYear = append(
			analytics.GenresByViewingYear,
			Series{Label: genre, Values: values},
		)
	}

	analytics.MonthLabels = sortedMonths(monthMediaCounts)
	analytics.MonthlyMedia = []Series{
		{
			Label:  movieDimensionLabel,
			Values: monthValues(analytics.MonthLabels, monthMediaCounts, movieDimensionLabel),
		},
		{
			Label:  seriesDimensionLabel,
			Values: monthValues(analytics.MonthLabels, monthMediaCounts, seriesDimensionLabel),
		},
		{
			Label:  unknownDimensionLabel,
			Values: monthValues(analytics.MonthLabels, monthMediaCounts, unknownDimensionLabel),
		},
	}

	weekdayGenres := topLabels(genreCounts, weekdayGenreLimit)
	analytics.GenresByWeekday = make([]Series, 0, len(weekdayGenres))
	for _, genre := range weekdayGenres {
		values := make([]int, len(analytics.WeekdayLabels))
		for weekdayIndex := range analytics.WeekdayLabels {
			values[weekdayIndex] = weekdayGenreCounts[weekdayIndex][genre]
		}
		analytics.GenresByWeekday = append(
			analytics.GenresByWeekday,
			Series{Label: genre, Values: values},
		)
	}
	return analytics, nil
}

func orderedMatchCounts(counts map[string]int) []Count {
	ordered := make([]Count, 0, 3)
	for _, status := range []MatchStatus{
		MatchStatusMatched,
		MatchStatusReview,
		MatchStatusUnmatched,
	} {
		if value := counts[string(status)]; value > 0 {
			ordered = append(ordered, Count{Label: string(status), Value: value})
		}
	}
	return ordered
}

func emptyAnalytics() Analytics {
	return Analytics{
		MatchStatusActivities: []Count{},
		MatchStatusTitles:     []Count{},
		MediaTypes:            []Count{},
		Genres:                []Count{},
		ViewingYears:          []int{},
		GenresByViewingYear:   []Series{},
		MonthLabels:           []string{},
		MonthlyMedia: []Series{
			{Label: movieDimensionLabel, Values: []int{}},
			{Label: seriesDimensionLabel, Values: []int{}},
			{Label: unknownDimensionLabel, Values: []int{}},
		},
		Languages:       []Count{},
		WeekdayLabels:   []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"},
		GenresByWeekday: []Series{},
		TopTitles:       []Count{},
	}
}

func sortedCounts(counts map[string]int, limit int) []Count {
	values := make([]Count, 0, len(counts))
	for label, value := range counts {
		values = append(values, Count{Label: label, Value: value})
	}
	sort.Slice(values, func(leftIndex int, rightIndex int) bool {
		if values[leftIndex].Value == values[rightIndex].Value {
			return values[leftIndex].Label < values[rightIndex].Label
		}
		return values[leftIndex].Value > values[rightIndex].Value
	})
	if limit > 0 && len(values) > limit {
		values = values[:limit]
	}
	return values
}

func topLabels(counts map[string]int, limit int) []string {
	countValues := sortedCounts(counts, limit)
	labels := make([]string, len(countValues))
	for countIndex, countValue := range countValues {
		labels[countIndex] = countValue.Label
	}
	return labels
}

func sortedYears(counts map[int]map[string]int) []int {
	years := make([]int, 0, len(counts))
	for year := range counts {
		years = append(years, year)
	}
	sort.Ints(years)
	return years
}

func sortedMonths(counts map[string]map[string]int) []string {
	months := make([]string, 0, len(counts))
	for month := range counts {
		months = append(months, month)
	}
	sort.Strings(months)
	if len(months) > recentMonthLimit {
		months = months[len(months)-recentMonthLimit:]
	}
	return months
}

func monthValues(
	months []string,
	counts map[string]map[string]int,
	mediaType string,
) []int {
	values := make([]int, len(months))
	for monthIndex, month := range months {
		values[monthIndex] = counts[month][mediaType]
	}
	return values
}
