package netflix_test

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
)

func TestNetflixAnalyticsContract(testContext *testing.T) {
	records := syntheticAnalyticsRecords(testContext)
	analytics, aggregateError := netflix.Aggregate(
		context.Background(),
		records,
		netflix.AllDates(),
	)
	if aggregateError != nil {
		testContext.Fatalf("aggregate synthetic activities: %v", aggregateError)
	}

	if analytics.ActivityCount != 5 {
		testContext.Errorf("activity count = %d; want 5", analytics.ActivityCount)
	}
	if analytics.UniqueTitleCount != 4 {
		testContext.Errorf("unique title count = %d; want 4", analytics.UniqueTitleCount)
	}
	if analytics.MetadataActivityCount != 4 {
		testContext.Errorf(
			"metadata activity count = %d; want 4",
			analytics.MetadataActivityCount,
		)
	}
	if analytics.MetadataTitleCount != 3 {
		testContext.Errorf(
			"metadata title count = %d; want 3",
			analytics.MetadataTitleCount,
		)
	}
	requireCounts(
		testContext,
		"match status activities",
		analytics.MatchStatusActivities,
		[]netflix.Count{
			{Label: "matched", Value: 4},
			{Label: "unmatched", Value: 1},
		},
	)
	requireCounts(
		testContext,
		"match status titles",
		analytics.MatchStatusTitles,
		[]netflix.Count{
			{Label: "matched", Value: 3},
			{Label: "unmatched", Value: 1},
		},
	)
	if analytics.StartDate != "2026-01-01" || analytics.EndDate != "2026-02-02" {
		testContext.Errorf(
			"date range = %s through %s; want 2026-01-01 through 2026-02-02",
			analytics.StartDate,
			analytics.EndDate,
		)
	}

	requireCounts(testContext, "media types", analytics.MediaTypes, []netflix.Count{
		{Label: "movie", Value: 2},
		{Label: "series", Value: 2},
		{Label: "unknown", Value: 1},
	})
	requireCounts(testContext, "genres", analytics.Genres, []netflix.Count{
		{Label: "Drama", Value: 3},
		{Label: "Science Fiction", Value: 2},
		{Label: "Comedy", Value: 1},
		{Label: "Mystery", Value: 1},
	})
	requireCounts(testContext, "languages", analytics.Languages, []netflix.Count{
		{Label: "en", Value: 3},
		{Label: "fr", Value: 1},
		{Label: "unknown", Value: 1},
	})
	requireCounts(testContext, "top titles", analytics.TopTitles, []netflix.Count{
		{Label: "Synthetic Series", Value: 2},
		{Label: "Another Film", Value: 1},
		{Label: "Synthetic Film", Value: 1},
		{Label: "Unmatched Local Title", Value: 1},
	})

	if !reflect.DeepEqual(analytics.ViewingYears, []int{2026}) {
		testContext.Errorf("viewing years = %#v; want [2026]", analytics.ViewingYears)
	}
	if !reflect.DeepEqual(analytics.MonthLabels, []string{"2026-01", "2026-02"}) {
		testContext.Errorf(
			"month labels = %#v; want [2026-01 2026-02]",
			analytics.MonthLabels,
		)
	}
	requireSeries(testContext, "monthly media", analytics.MonthlyMedia, []netflix.Series{
		{Label: "movie", Values: []int{1, 1}},
		{Label: "series", Values: []int{2, 0}},
		{Label: "unknown", Values: []int{0, 1}},
	})
	if !reflect.DeepEqual(
		analytics.WeekdayLabels,
		[]string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"},
	) {
		testContext.Errorf("weekday labels = %#v", analytics.WeekdayLabels)
	}
	dramaSeries, found := findSeries(analytics.GenresByWeekday, "Drama")
	if !found {
		testContext.Fatalf("weekday genre series is missing Drama")
	}
	if !reflect.DeepEqual(dramaSeries.Values, []int{0, 0, 0, 1, 1, 1, 0}) {
		testContext.Errorf(
			"Drama weekday values = %#v; want [0 0 0 1 1 1 0]",
			dramaSeries.Values,
		)
	}
}

func TestNetflixAnalyticsUsesOneInclusiveDateFilter(testContext *testing.T) {
	records := syntheticAnalyticsRecords(testContext)
	startDate := mustLocalDate(testContext, "2/1/26")
	endDate := mustLocalDate(testContext, "2/2/26")
	dateRange, rangeError := netflix.NewDateRange(startDate, endDate)
	if rangeError != nil {
		testContext.Fatalf("construct February filter: %v", rangeError)
	}

	analytics, aggregateError := netflix.Aggregate(
		context.Background(),
		records,
		dateRange,
	)
	if aggregateError != nil {
		testContext.Fatalf("aggregate filtered activities: %v", aggregateError)
	}
	if analytics.ActivityCount != 2 ||
		analytics.MetadataActivityCount != 1 ||
		analytics.UniqueTitleCount != 2 {
		testContext.Fatalf(
			"filtered counts = activities:%d metadata:%d titles:%d; want 2, 1, 2",
			analytics.ActivityCount,
			analytics.MetadataActivityCount,
			analytics.UniqueTitleCount,
		)
	}
	if analytics.StartDate != "2026-02-01" || analytics.EndDate != "2026-02-02" {
		testContext.Fatalf(
			"filtered dates = %s through %s",
			analytics.StartDate,
			analytics.EndDate,
		)
	}
	requireCounts(testContext, "filtered media", analytics.MediaTypes, []netflix.Count{
		{Label: "movie", Value: 1},
		{Label: "unknown", Value: 1},
	})
}

func TestNetflixAnalyticsReturnsExplicitEmptyState(testContext *testing.T) {
	records := syntheticAnalyticsRecords(testContext)
	startDate := mustLocalDate(testContext, "3/1/26")
	endDate := mustLocalDate(testContext, "3/31/26")
	dateRange, rangeError := netflix.NewDateRange(startDate, endDate)
	if rangeError != nil {
		testContext.Fatalf("construct empty date filter: %v", rangeError)
	}

	analytics, aggregateError := netflix.Aggregate(
		context.Background(),
		records,
		dateRange,
	)
	if aggregateError != nil {
		testContext.Fatalf("aggregate empty date filter: %v", aggregateError)
	}
	if analytics.ActivityCount != 0 ||
		analytics.StartDate != "" ||
		analytics.EndDate != "" {
		testContext.Fatalf("unexpected nonempty analytics: %+v", analytics)
	}
	if analytics.MediaTypes == nil ||
		analytics.Genres == nil ||
		analytics.MonthLabels == nil ||
		analytics.TopTitles == nil {
		testContext.Fatalf("empty analytics must use explicit empty collections")
	}
}

func TestNetflixAnalyticsHonorsCancellation(testContext *testing.T) {
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	_, aggregateError := netflix.Aggregate(
		canceledContext,
		syntheticAnalyticsRecords(testContext),
		netflix.AllDates(),
	)
	if !errors.Is(aggregateError, context.Canceled) {
		testContext.Fatalf("aggregate error = %v; want context.Canceled", aggregateError)
	}
}

func TestNetflixMetadataRejectsInconsistentStates(testContext *testing.T) {
	one := 1
	voteAverage := 7.0
	notANumber := math.NaN()
	testCases := []struct {
		name  string
		input netflix.TitleMetadataInput
	}{
		{
			name: "movie with seasons",
			input: netflix.TitleMetadataInput{
				MediaType:    netflix.MediaTypeMovie,
				Seasons:      &one,
				TMDBID:       1,
				MatchedTitle: "Invalid Movie",
			},
		},
		{
			name: "partial vote pair",
			input: netflix.TitleMetadataInput{
				MediaType:    netflix.MediaTypeSeries,
				VoteAverage:  &voteAverage,
				TMDBID:       2,
				MatchedTitle: "Invalid Series",
			},
		},
		{
			name: "duplicate genres",
			input: netflix.TitleMetadataInput{
				MediaType:    netflix.MediaTypeMovie,
				Genres:       []string{"Drama", "drama"},
				TMDBID:       3,
				MatchedTitle: "Duplicate Genre",
			},
		},
		{
			name: "non-finite vote average",
			input: netflix.TitleMetadataInput{
				MediaType:    netflix.MediaTypeMovie,
				VoteAverage:  &notANumber,
				VoteCount:    &one,
				TMDBID:       4,
				MatchedTitle: "Invalid Rating",
			},
		},
		{
			name: "zero TMDB ID",
			input: netflix.TitleMetadataInput{
				MediaType:    netflix.MediaTypeMovie,
				TMDBID:       0,
				MatchedTitle: "Missing ID",
			},
		},
	}

	for _, testCase := range testCases {
		testContext.Run(testCase.name, func(testContext *testing.T) {
			_, metadataError := netflix.NewTitleMetadata(testCase.input)
			if !errors.Is(metadataError, netflix.ErrInvalidTitleMetadata) {
				testContext.Fatalf(
					"metadata error = %v; want ErrInvalidTitleMetadata",
					metadataError,
				)
			}
		})
	}
}

func syntheticAnalyticsRecords(testContext *testing.T) []netflix.ActivityRecord {
	testContext.Helper()
	movieMetadata := mustTitleMetadata(testContext, netflix.TitleMetadataInput{
		MediaType:        netflix.MediaTypeMovie,
		Genres:           []string{"Drama", "Mystery"},
		ReleaseDate:      "2024-01-10",
		OriginalLanguage: "en",
		TMDBID:           1001,
		MatchedTitle:     "Synthetic Film",
	})
	seriesMetadata := mustTitleMetadata(testContext, netflix.TitleMetadataInput{
		MediaType:        netflix.MediaTypeSeries,
		Genres:           []string{"Drama", "Science Fiction"},
		ReleaseDate:      "2025-03-04",
		OriginalLanguage: "en",
		TMDBID:           1002,
		MatchedTitle:     "Synthetic Series",
	})
	otherMovieMetadata := mustTitleMetadata(testContext, netflix.TitleMetadataInput{
		MediaType:        netflix.MediaTypeMovie,
		Genres:           []string{"Comedy"},
		ReleaseDate:      "2023-05-06",
		OriginalLanguage: "fr",
		TMDBID:           1003,
		MatchedTitle:     "Another Film",
	})

	recordInputs := []struct {
		rawTitle string
		rawDate  string
		metadata *netflix.TitleMetadata
	}{
		{rawTitle: "Synthetic Film", rawDate: "1/1/26", metadata: &movieMetadata},
		{rawTitle: "Synthetic Series: Season 1: First", rawDate: "1/2/26", metadata: &seriesMetadata},
		{rawTitle: "Synthetic Series: Season 1: Second", rawDate: "1/3/26", metadata: &seriesMetadata},
		{rawTitle: "Another Film", rawDate: "2/1/26", metadata: &otherMovieMetadata},
		{rawTitle: "Unmatched Local Title", rawDate: "2/2/26"},
	}
	records := make([]netflix.ActivityRecord, 0, len(recordInputs))
	for _, input := range recordInputs {
		activity := mustViewingActivity(testContext, input.rawTitle, input.rawDate)
		var match netflix.TMDBMatch
		var matchError error
		if input.metadata == nil {
			match, matchError = netflix.MatchTMDBTitle(
				activity.TitleIdentity(),
				nil,
			)
		} else {
			match, matchError = netflix.MatchTMDBTitle(
				activity.TitleIdentity(),
				[]netflix.MatchCandidate{{
					TMDBID:    input.metadata.TMDBID(),
					MediaType: input.metadata.MediaType(),
					Title:     input.metadata.MatchedTitle(),
				}},
			)
		}
		if matchError != nil {
			testContext.Fatalf("construct analytics match: %v", matchError)
		}
		record, recordError := netflix.NewEnrichedActivityRecord(
			activity,
			match,
			input.metadata,
		)
		if recordError != nil {
			testContext.Fatalf("construct enriched record: %v", recordError)
		}
		records = append(records, record)
	}
	return records
}

func mustTitleMetadata(
	testContext *testing.T,
	input netflix.TitleMetadataInput,
) netflix.TitleMetadata {
	testContext.Helper()
	metadata, metadataError := netflix.NewTitleMetadata(input)
	if metadataError != nil {
		testContext.Fatalf("construct title metadata: %v", metadataError)
	}
	return metadata
}

func mustLocalDate(testContext *testing.T, rawDate string) netflix.LocalDate {
	testContext.Helper()
	localDate, dateError := netflix.ParseLocalDate(rawDate)
	if dateError != nil {
		testContext.Fatalf("parse local date %q: %v", rawDate, dateError)
	}
	return localDate
}

func requireCounts(
	testContext *testing.T,
	name string,
	actual []netflix.Count,
	expected []netflix.Count,
) {
	testContext.Helper()
	if !reflect.DeepEqual(actual, expected) {
		testContext.Errorf("%s = %#v; want %#v", name, actual, expected)
	}
}

func requireSeries(
	testContext *testing.T,
	name string,
	actual []netflix.Series,
	expected []netflix.Series,
) {
	testContext.Helper()
	if !reflect.DeepEqual(actual, expected) {
		testContext.Errorf("%s = %#v; want %#v", name, actual, expected)
	}
}

func findSeries(seriesValues []netflix.Series, label string) (netflix.Series, bool) {
	for _, seriesValue := range seriesValues {
		if seriesValue.Label == label {
			return seriesValue, true
		}
	}
	return netflix.Series{}, false
}
