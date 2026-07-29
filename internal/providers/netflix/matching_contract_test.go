package netflix_test

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
)

type matcherEvaluationCase struct {
	Name           string                       `json:"name"`
	Category       string                       `json:"category"`
	RawTitle       string                       `json:"raw_title"`
	Candidates     []matcherEvaluationCandidate `json:"candidates"`
	ExpectedStatus netflix.MatchStatus          `json:"expected_status"`
	ExpectedTMDBID int64                        `json:"expected_tmdb_id"`
}

type matcherEvaluationCandidate struct {
	TMDBID        int64   `json:"tmdb_id"`
	MediaType     string  `json:"media_type"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"original_title"`
	Popularity    float64 `json:"popularity"`
}

func TestMatcherEvaluationGate(testContext *testing.T) {
	evaluationCases := loadMatcherEvaluationCases(testContext)
	acceptedPredictions := 0
	correctAcceptedPredictions := 0
	expectedAccepted := 0
	correctExpectedAccepted := 0
	statusCounts := map[netflix.MatchStatus]int{}

	for _, evaluationCase := range evaluationCases {
		testContext.Run(evaluationCase.Name, func(testContext *testing.T) {
			activity, activityError := netflix.NewViewingActivity(evaluationCase.RawTitle, "7/28/26")
			if activityError != nil {
				testContext.Fatalf("construct evaluation activity: %v", activityError)
			}
			candidates := make([]netflix.MatchCandidate, len(evaluationCase.Candidates))
			for candidateIndex, evaluationCandidate := range evaluationCase.Candidates {
				candidates[candidateIndex] = netflix.MatchCandidate{
					TMDBID:        evaluationCandidate.TMDBID,
					MediaType:     matcherMediaType(testContext, evaluationCandidate.MediaType),
					Title:         evaluationCandidate.Title,
					OriginalTitle: evaluationCandidate.OriginalTitle,
					Popularity:    evaluationCandidate.Popularity,
				}
			}

			match, matchError := netflix.MatchTMDBTitle(activity.TitleIdentity(), candidates)
			if matchError != nil {
				testContext.Fatalf("match evaluation title: %v", matchError)
			}
			if match.Status() != evaluationCase.ExpectedStatus {
				testContext.Fatalf(
					"status = %q; want %q; evidence=%+v",
					match.Status(),
					evaluationCase.ExpectedStatus,
					match.Evidence(),
				)
			}
			if match.TMDBID() != evaluationCase.ExpectedTMDBID {
				testContext.Fatalf(
					"TMDB ID = %d; want %d",
					match.TMDBID(),
					evaluationCase.ExpectedTMDBID,
				)
			}
			if match.MatcherIdentity() != netflix.TMDBMatcherIdentity {
				testContext.Fatalf("unexpected matcher identity %q", match.MatcherIdentity())
			}
			if evaluationCase.Category == "" {
				testContext.Fatalf("evaluation category is required")
			}

			statusCounts[match.Status()]++
			if match.Status() == netflix.MatchStatusMatched {
				acceptedPredictions++
				if evaluationCase.ExpectedStatus == netflix.MatchStatusMatched &&
					match.TMDBID() == evaluationCase.ExpectedTMDBID {
					correctAcceptedPredictions++
				}
			}
			if evaluationCase.ExpectedStatus == netflix.MatchStatusMatched {
				expectedAccepted++
				if match.Status() == netflix.MatchStatusMatched &&
					match.TMDBID() == evaluationCase.ExpectedTMDBID {
					correctExpectedAccepted++
				}
			}
		})
	}

	precision := ratio(correctAcceptedPredictions, acceptedPredictions)
	recall := ratio(correctExpectedAccepted, expectedAccepted)
	testContext.Logf(
		"matcher=%s cases=%d matched=%d review=%d unmatched=%d precision=%.3f recall=%.3f thresholds=(precision=%.2f recall=%.2f)",
		netflix.TMDBMatcherIdentity,
		len(evaluationCases),
		statusCounts[netflix.MatchStatusMatched],
		statusCounts[netflix.MatchStatusReview],
		statusCounts[netflix.MatchStatusUnmatched],
		precision,
		recall,
		netflix.TMDBMatcherMinimumEvaluationPrecision,
		netflix.TMDBMatcherMinimumEvaluationRecall,
	)
	if precision < netflix.TMDBMatcherMinimumEvaluationPrecision {
		testContext.Fatalf("accepted-match precision %.3f is below %.3f", precision, netflix.TMDBMatcherMinimumEvaluationPrecision)
	}
	if recall < netflix.TMDBMatcherMinimumEvaluationRecall {
		testContext.Fatalf("accepted-match recall %.3f is below %.3f", recall, netflix.TMDBMatcherMinimumEvaluationRecall)
	}
}

func TestMatcherRejectsInvalidCandidatesAndCachedShapes(testContext *testing.T) {
	activity, activityError := netflix.NewViewingActivity("Synthetic Film", "7/28/26")
	if activityError != nil {
		testContext.Fatalf("construct activity: %v", activityError)
	}
	if _, matchError := netflix.MatchTMDBTitle(
		activity.TitleIdentity(),
		[]netflix.MatchCandidate{{
			TMDBID:     7,
			MediaType:  netflix.MediaTypeMovie,
			Title:      "Synthetic Film",
			Popularity: -1,
		}},
	); !errors.Is(matchError, netflix.ErrInvalidMatchCandidate) {
		testContext.Fatalf("invalid candidate error = %v", matchError)
	}

	if _, matchError := netflix.NewTMDBMatch(netflix.TMDBMatchInput{
		Status:          netflix.MatchStatusReview,
		MatcherIdentity: netflix.TMDBMatcherIdentity,
		TMDBID:          7,
		MediaType:       netflix.MediaTypeMovie,
		Evidence: netflix.MatchEvidence{
			NormalizedQuery:      "synthetic film",
			BestCandidateTitle:   "Synthetic Film",
			BestScore:            1,
			Margin:               1,
			ExactCandidateCount:  1,
			CandidatesConsidered: 1,
			Reason:               "ambiguous_exact_title",
		},
	}); !errors.Is(matchError, netflix.ErrInvalidTMDBMatch) {
		testContext.Fatalf("invalid cached match error = %v", matchError)
	}
}

func loadMatcherEvaluationCases(testContext *testing.T) []matcherEvaluationCase {
	testContext.Helper()
	encodedFixture, readError := os.ReadFile("testdata/tmdb_matcher_cases.json")
	if readError != nil {
		testContext.Fatalf("read matcher fixture: %v", readError)
	}
	var evaluationCases []matcherEvaluationCase
	if decodeError := json.Unmarshal(encodedFixture, &evaluationCases); decodeError != nil {
		testContext.Fatalf("decode matcher fixture: %v", decodeError)
	}
	if len(evaluationCases) == 0 {
		testContext.Fatalf("matcher fixture must not be empty")
	}
	return evaluationCases
}

func matcherMediaType(testContext *testing.T, value string) netflix.MediaType {
	testContext.Helper()
	switch value {
	case string(netflix.MediaTypeMovie):
		return netflix.MediaTypeMovie
	case string(netflix.MediaTypeSeries):
		return netflix.MediaTypeSeries
	default:
		testContext.Fatalf("unknown evaluation media type %q", value)
		return ""
	}
}

func ratio(numerator int, denominator int) float64 {
	if denominator == 0 {
		return 1
	}
	return float64(numerator) / float64(denominator)
}
