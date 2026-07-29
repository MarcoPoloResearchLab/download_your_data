package netflix_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
)

func TestViewingActivityCSVContract(testContext *testing.T) {
	limits := mustCSVLimits(testContext, 100, 512, 4096)
	fixture, openError := os.Open("testdata/viewing_activity.csv")
	if openError != nil {
		testContext.Fatalf("open synthetic viewing fixture: %v", openError)
	}
	defer fixture.Close()

	activities, parseError := netflix.ParseViewingActivity(
		context.Background(),
		fixture,
		limits,
	)
	if parseError != nil {
		testContext.Fatalf("parse synthetic viewing fixture: %v", parseError)
	}
	if len(activities) != 9 {
		testContext.Fatalf("activity count = %d; want 9", len(activities))
	}

	expectedSearchTitles := []string{
		"The Clockmaker",
		"Northern Lights",
		"Northern Lights",
		"Starship: Discovery",
		"La Casa",
		"Les Ombres",
		"Тайна",
		"The Journey: Part One",
		"The Journey",
	}
	for activityIndex, activity := range activities {
		identity := activity.TitleIdentity()
		if identity.SearchTitle() != expectedSearchTitles[activityIndex] {
			testContext.Errorf(
				"activity %d search title = %q; want %q",
				activityIndex,
				identity.SearchTitle(),
				expectedSearchTitles[activityIndex],
			)
		}
		if len(identity.Key()) != 64 {
			testContext.Errorf(
				"activity %d identity length = %d; want 64",
				activityIndex,
				len(identity.Key()),
			)
		}
		if identity.Version() == "" {
			testContext.Errorf("activity %d has an empty identity version", activityIndex)
		}
	}
	if activities[1].TitleIdentity().Key() != activities[2].TitleIdentity().Key() {
		testContext.Fatalf("episodes of the same series must share one title identity")
	}
	if activities[3].RawTitle() != "Starship: Discovery" {
		testContext.Fatalf("punctuated film title was not preserved")
	}
	if activities[3].TitleIdentity().SearchTitle() != activities[3].RawTitle() {
		testContext.Fatalf("unrecognized punctuation must not truncate a title")
	}
}

func TestViewingActivityCSVAcceptsCanonicalColumnOrderIndependence(testContext *testing.T) {
	limits := mustCSVLimits(testContext, 10, 128, 256)
	activities, parseError := netflix.ParseViewingActivity(
		context.Background(),
		strings.NewReader("Date,Title\n02/03/26,Column Order\n"),
		limits,
	)
	if parseError != nil {
		testContext.Fatalf("parse reversed canonical columns: %v", parseError)
	}
	if len(activities) != 1 {
		testContext.Fatalf("activity count = %d; want 1", len(activities))
	}
	if activities[0].RawTitle() != "Column Order" ||
		activities[0].RawDate() != "02/03/26" {
		testContext.Fatalf("unexpected activity: title=%q date=%q", activities[0].RawTitle(), activities[0].RawDate())
	}
}

func TestViewingActivityCSVRejectsInvalidBoundaries(testContext *testing.T) {
	testCases := []struct {
		name     string
		source   string
		maxRows  int
		wantCode netflix.CSVErrorCode
		wantRow  int
	}{
		{
			name:     "empty file",
			source:   "",
			maxRows:  10,
			wantCode: netflix.CSVErrorEmpty,
		},
		{
			name:     "header only",
			source:   "Title,Date\n",
			maxRows:  10,
			wantCode: netflix.CSVErrorEmpty,
		},
		{
			name:     "extra column",
			source:   "Title,Date,Profile\nExample,1/2/26,Main\n",
			maxRows:  10,
			wantCode: netflix.CSVErrorInvalidHeader,
			wantRow:  1,
		},
		{
			name:     "duplicate column",
			source:   "Title,Title\nExample,Example\n",
			maxRows:  10,
			wantCode: netflix.CSVErrorInvalidHeader,
			wantRow:  1,
		},
		{
			name:     "wrong column case",
			source:   "title,Date\nExample,1/2/26\n",
			maxRows:  10,
			wantCode: netflix.CSVErrorInvalidHeader,
			wantRow:  1,
		},
		{
			name:     "short row",
			source:   "Title,Date\nExample\n",
			maxRows:  10,
			wantCode: netflix.CSVErrorInvalidRow,
			wantRow:  2,
		},
		{
			name:     "blank title",
			source:   "Title,Date\n,1/2/26\n",
			maxRows:  10,
			wantCode: netflix.CSVErrorInvalidTitle,
			wantRow:  2,
		},
		{
			name:     "surrounding title whitespace",
			source:   "Title,Date\n Example,1/2/26\n",
			maxRows:  10,
			wantCode: netflix.CSVErrorInvalidTitle,
			wantRow:  2,
		},
		{
			name:     "invalid month",
			source:   "Title,Date\nExample,13/2/26\n",
			maxRows:  10,
			wantCode: netflix.CSVErrorInvalidDate,
			wantRow:  2,
		},
		{
			name:     "impossible date",
			source:   "Title,Date\nExample,2/30/26\n",
			maxRows:  10,
			wantCode: netflix.CSVErrorInvalidDate,
			wantRow:  2,
		},
		{
			name:     "row limit",
			source:   "Title,Date\nFirst,1/1/26\nSecond,1/2/26\n",
			maxRows:  1,
			wantCode: netflix.CSVErrorLimitExceeded,
			wantRow:  3,
		},
	}

	for _, testCase := range testCases {
		testContext.Run(testCase.name, func(testContext *testing.T) {
			limits := mustCSVLimits(testContext, testCase.maxRows, 128, 512)
			_, parseError := netflix.ParseViewingActivity(
				context.Background(),
				strings.NewReader(testCase.source),
				limits,
			)
			requireCSVError(
				testContext,
				parseError,
				testCase.wantCode,
				testCase.wantRow,
			)
		})
	}
}

func TestViewingActivityCSVHonorsCancellation(testContext *testing.T) {
	limits := mustCSVLimits(testContext, 10, 128, 512)
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()

	_, parseError := netflix.ParseViewingActivity(
		canceledContext,
		strings.NewReader("Title,Date\nExample,1/2/26\n"),
		limits,
	)
	requireCSVError(testContext, parseError, netflix.CSVErrorCanceled, 0)
}

func TestEnrichedActivityCSVRoundTrip(testContext *testing.T) {
	limits := mustCSVLimits(testContext, 10, 512, 4096)
	enrichedActivity := mustViewingActivity(testContext, "The Clockmaker", "1/2/26")
	localActivity := mustViewingActivity(testContext, "Unmatched Title", "1/3/26")
	runtimeMinutes := 104
	voteAverage := 7.4
	voteCount := 812
	metadata, metadataError := netflix.NewTitleMetadata(netflix.TitleMetadataInput{
		MediaType:        netflix.MediaTypeMovie,
		Genres:           []string{"Drama", "Mystery"},
		ReleaseDate:      "2024-10-11",
		RuntimeMinutes:   &runtimeMinutes,
		OriginalLanguage: "en",
		VoteAverage:      &voteAverage,
		VoteCount:        &voteCount,
		OriginCountries:  []string{"US"},
		TMDBID:           4242,
		MatchedTitle:     "The Clockmaker",
		Description:      "A synthetic clockmaker solves a synthetic mystery.",
	})
	if metadataError != nil {
		testContext.Fatalf("construct synthetic metadata: %v", metadataError)
	}
	matchedOutcome := mustTMDBMatch(
		testContext,
		enrichedActivity,
		[]netflix.MatchCandidate{{
			TMDBID:    4242,
			MediaType: netflix.MediaTypeMovie,
			Title:     "The Clockmaker",
		}},
	)
	enrichedRecord, enrichedRecordError := netflix.NewEnrichedActivityRecord(
		enrichedActivity,
		matchedOutcome,
		&metadata,
	)
	if enrichedRecordError != nil {
		testContext.Fatalf("construct enriched activity: %v", enrichedRecordError)
	}
	unmatchedOutcome := mustTMDBMatch(testContext, localActivity, nil)
	unmatchedRecord, unmatchedRecordError := netflix.NewEnrichedActivityRecord(
		localActivity,
		unmatchedOutcome,
		nil,
	)
	if unmatchedRecordError != nil {
		testContext.Fatalf("construct unmatched activity: %v", unmatchedRecordError)
	}

	var destination bytes.Buffer
	writeError := netflix.WriteEnrichedActivity(
		context.Background(),
		&destination,
		[]netflix.ActivityRecord{enrichedRecord, unmatchedRecord},
	)
	if writeError != nil {
		testContext.Fatalf("write enriched CSV: %v", writeError)
	}
	if strings.Contains(destination.String(), "BaseTitle") {
		testContext.Fatalf("enriched output retained the obsolete BaseTitle contract")
	}

	roundTripped, readError := netflix.ReadEnrichedActivity(
		context.Background(),
		bytes.NewReader(destination.Bytes()),
		limits,
	)
	if readError != nil {
		testContext.Fatalf("read enriched CSV: %v", readError)
	}
	if len(roundTripped) != 2 {
		testContext.Fatalf("round-trip record count = %d; want 2", len(roundTripped))
	}
	roundTrippedMetadata, hasMetadata := roundTripped[0].Metadata()
	if !hasMetadata {
		testContext.Fatalf("first round-trip record lost metadata")
	}
	if roundTrippedMetadata.TMDBID() != 4242 ||
		roundTrippedMetadata.MatchedTitle() != "The Clockmaker" {
		testContext.Fatalf(
			"unexpected round-trip metadata: id=%d title=%q",
			roundTrippedMetadata.TMDBID(),
			roundTrippedMetadata.MatchedTitle(),
		)
	}
	roundTrippedMatch, hasMatch := roundTripped[0].Match()
	if !hasMatch ||
		roundTrippedMatch.Status() != netflix.MatchStatusMatched ||
		roundTrippedMatch.MatcherIdentity() != netflix.TMDBMatcherIdentity {
		testContext.Fatalf("first round-trip record lost its match: %+v", roundTrippedMatch)
	}
	if _, hasMetadata := roundTripped[1].Metadata(); hasMetadata {
		testContext.Fatalf("unmatched round-trip record unexpectedly acquired metadata")
	}
	unmatchedMatch, hasMatch := roundTripped[1].Match()
	if !hasMatch || unmatchedMatch.Status() != netflix.MatchStatusUnmatched {
		testContext.Fatalf("unmatched round-trip status = %+v", unmatchedMatch)
	}
	if roundTripped[1].Activity().RawTitle() != "Unmatched Title" {
		testContext.Fatalf("unmatched round-trip record lost its raw title")
	}
}

func TestEnrichedActivityCSVRejectsStaleIdentity(testContext *testing.T) {
	limits := mustCSVLimits(testContext, 10, 512, 4096)
	header := "Title,Date,DerivedTitle,TitleIdentity,TitleIdentityVersion,MatchStatus,MatcherIdentity,MatchMediaType,MatchTMDBID,MatchNormalizedQuery,MatchBestCandidateTitle,MatchBestScore,MatchRunnerUpScore,MatchMargin,MatchExactCandidateCount,MatchCandidatesConsidered,MatchReason,MediaType,Genres,ReleaseDate,RuntimeMinutes,OriginalLanguage,VoteAverage,VoteCount,OriginCountries,Seasons,Episodes,TMDBID,MatchedTitle,Description\n"
	row := "Example,1/2/26,Example,stale,netflix-title-v0,unmatched,netflix-tmdb-matcher-v1,,,example,,0,0,0,0,0,no_candidates,,,,,,,,,,,,,\n"
	_, readError := netflix.ReadEnrichedActivity(
		context.Background(),
		strings.NewReader(header+row),
		limits,
	)
	requireCSVError(testContext, readError, netflix.CSVErrorInvalidMetadata, 2)
}

func TestEnrichedActivityCSVRejectsLocalRecordsWithoutMatchOutcomes(
	testContext *testing.T,
) {
	activity := mustViewingActivity(testContext, "Local Only", "1/2/26")
	record, recordError := netflix.NewLocalActivityRecord(activity)
	if recordError != nil {
		testContext.Fatalf("construct local record: %v", recordError)
	}
	var destination bytes.Buffer
	writeError := netflix.WriteEnrichedActivity(
		context.Background(),
		&destination,
		[]netflix.ActivityRecord{record},
	)
	requireCSVError(
		testContext,
		writeError,
		netflix.CSVErrorInvalidRow,
		2,
	)
}

func TestCSVLimitsRequireExplicitValidBounds(testContext *testing.T) {
	testCases := []struct {
		name          string
		maxRows       int
		maxTitleBytes int
		maxFieldBytes int
	}{
		{name: "zero rows", maxRows: 0, maxTitleBytes: 128, maxFieldBytes: 512},
		{name: "zero title", maxRows: 10, maxTitleBytes: 0, maxFieldBytes: 512},
		{name: "field smaller than title", maxRows: 10, maxTitleBytes: 512, maxFieldBytes: 128},
	}
	for _, testCase := range testCases {
		testContext.Run(testCase.name, func(testContext *testing.T) {
			_, limitError := netflix.NewCSVLimits(
				testCase.maxRows,
				testCase.maxTitleBytes,
				testCase.maxFieldBytes,
			)
			if limitError == nil {
				testContext.Fatalf("expected invalid limits to fail")
			}
		})
	}
}

func mustCSVLimits(
	testContext *testing.T,
	maxRows int,
	maxTitleBytes int,
	maxFieldBytes int,
) netflix.CSVLimits {
	testContext.Helper()
	limits, limitError := netflix.NewCSVLimits(
		maxRows,
		maxTitleBytes,
		maxFieldBytes,
	)
	if limitError != nil {
		testContext.Fatalf("construct CSV limits: %v", limitError)
	}
	return limits
}

func mustViewingActivity(
	testContext *testing.T,
	rawTitle string,
	rawDate string,
) netflix.ViewingActivity {
	testContext.Helper()
	activity, activityError := netflix.NewViewingActivity(rawTitle, rawDate)
	if activityError != nil {
		testContext.Fatalf("construct viewing activity: %v", activityError)
	}
	return activity
}

func mustTMDBMatch(
	testContext *testing.T,
	activity netflix.ViewingActivity,
	candidates []netflix.MatchCandidate,
) netflix.TMDBMatch {
	testContext.Helper()
	match, matchError := netflix.MatchTMDBTitle(
		activity.TitleIdentity(),
		candidates,
	)
	if matchError != nil {
		testContext.Fatalf("construct TMDB match: %v", matchError)
	}
	return match
}

func requireCSVError(
	testContext *testing.T,
	actualError error,
	wantCode netflix.CSVErrorCode,
	wantRow int,
) {
	testContext.Helper()
	if actualError == nil {
		testContext.Fatalf("expected CSV error %q", wantCode)
	}
	var boundaryError *netflix.CSVError
	if !errors.As(actualError, &boundaryError) {
		testContext.Fatalf("error %T is not *netflix.CSVError: %v", actualError, actualError)
	}
	if boundaryError.Code() != wantCode {
		testContext.Fatalf("CSV error code = %q; want %q", boundaryError.Code(), wantCode)
	}
	if boundaryError.Row() != wantRow {
		testContext.Fatalf("CSV error row = %d; want %d", boundaryError.Row(), wantRow)
	}
}
