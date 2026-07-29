package netflix

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"
)

const (
	// TMDBMatcherIdentity changes whenever title matching semantics change.
	TMDBMatcherIdentity = "netflix-tmdb-matcher-v1"

	// TMDBMatcherAcceptScore is the minimum non-exact score eligible for acceptance.
	TMDBMatcherAcceptScore = 0.96

	// TMDBMatcherReviewScore is the minimum score retained for human review.
	TMDBMatcherReviewScore = 0.68

	// TMDBMatcherMinimumMargin prevents a close runner-up from being accepted.
	TMDBMatcherMinimumMargin = 0.15

	// TMDBMatcherMinimumEvaluationPrecision is the release gate for accepted matches.
	TMDBMatcherMinimumEvaluationPrecision = 1.0

	// TMDBMatcherMinimumEvaluationRecall is the release gate for expected accepted matches.
	TMDBMatcherMinimumEvaluationRecall = 0.90
)

var (
	// ErrInvalidMatchCandidate identifies malformed TMDB search evidence.
	ErrInvalidMatchCandidate = errors.New("invalid Netflix TMDB match candidate")

	// ErrInvalidTMDBMatch identifies a match outside the current closed contract.
	ErrInvalidTMDBMatch = errors.New("invalid Netflix TMDB match")
)

// MatchStatus is the closed title-matching outcome.
type MatchStatus string

const (
	// MatchStatusMatched contains one accepted TMDB identity.
	MatchStatusMatched MatchStatus = "matched"

	// MatchStatusReview retains ambiguous evidence without accepted metadata.
	MatchStatusReview MatchStatus = "review"

	// MatchStatusUnmatched records that no candidate met the review threshold.
	MatchStatusUnmatched MatchStatus = "unmatched"
)

// MatchCandidate is one validated screen-media search candidate.
// Popularity is retained as source evidence but is deliberately excluded from scoring.
type MatchCandidate struct {
	TMDBID        int64
	MediaType     MediaType
	Title         string
	OriginalTitle string
	Popularity    float64
}

// MatchEvidence explains one deterministic matching decision.
type MatchEvidence struct {
	NormalizedQuery      string  `json:"normalized_query"`
	BestCandidateTitle   string  `json:"best_candidate_title"`
	BestScore            float64 `json:"best_score"`
	RunnerUpScore        float64 `json:"runner_up_score"`
	Margin               float64 `json:"margin"`
	ExactCandidateCount  int     `json:"exact_candidate_count"`
	CandidatesConsidered int     `json:"candidates_considered"`
	Reason               string  `json:"reason"`
}

// TMDBMatchInput is the validated reconstruction boundary for cached outcomes.
type TMDBMatchInput struct {
	Status          MatchStatus
	MatcherIdentity string
	MediaType       MediaType
	TMDBID          int64
	Evidence        MatchEvidence
}

// TMDBMatch is one closed, versioned title-matching outcome.
type TMDBMatch struct {
	status          MatchStatus
	matcherIdentity string
	mediaType       MediaType
	tmdbID          int64
	evidence        MatchEvidence
}

// NewTMDBMatch validates a current match snapshot, including cache reconstruction.
func NewTMDBMatch(input TMDBMatchInput) (TMDBMatch, error) {
	if input.MatcherIdentity != TMDBMatcherIdentity {
		return TMDBMatch{}, fmt.Errorf(
			"%w: matcher identity must be %s",
			ErrInvalidTMDBMatch,
			TMDBMatcherIdentity,
		)
	}
	if input.Status != MatchStatusMatched &&
		input.Status != MatchStatusReview &&
		input.Status != MatchStatusUnmatched {
		return TMDBMatch{}, fmt.Errorf("%w: unknown status %q", ErrInvalidTMDBMatch, input.Status)
	}
	if evidenceError := validateMatchEvidence(input.Status, input.Evidence); evidenceError != nil {
		return TMDBMatch{}, evidenceError
	}
	if input.Status == MatchStatusMatched {
		if input.TMDBID <= 0 {
			return TMDBMatch{}, fmt.Errorf("%w: matched outcome requires a TMDB ID", ErrInvalidTMDBMatch)
		}
		if input.MediaType != MediaTypeMovie && input.MediaType != MediaTypeSeries {
			return TMDBMatch{}, fmt.Errorf("%w: matched outcome requires a media type", ErrInvalidTMDBMatch)
		}
	} else if input.TMDBID != 0 || input.MediaType != "" {
		return TMDBMatch{}, fmt.Errorf(
			"%w: review and unmatched outcomes cannot accept a TMDB identity",
			ErrInvalidTMDBMatch,
		)
	}
	return TMDBMatch{
		status:          input.Status,
		matcherIdentity: input.MatcherIdentity,
		mediaType:       input.MediaType,
		tmdbID:          input.TMDBID,
		evidence:        input.Evidence,
	}, nil
}

// Status returns the closed outcome.
func (match TMDBMatch) Status() MatchStatus {
	return match.status
}

// MatcherIdentity returns the exact matcher contract.
func (match TMDBMatch) MatcherIdentity() string {
	return match.matcherIdentity
}

// MediaType returns the accepted media type only for matched outcomes.
func (match TMDBMatch) MediaType() MediaType {
	return match.mediaType
}

// TMDBID returns the accepted TMDB ID only for matched outcomes.
func (match TMDBMatch) TMDBID() int64 {
	return match.tmdbID
}

// Evidence returns deterministic decision evidence.
func (match TMDBMatch) Evidence() MatchEvidence {
	return match.evidence
}

// MatchTMDBTitle applies the current deterministic matcher to one derived title.
func MatchTMDBTitle(
	identity TitleIdentity,
	candidates []MatchCandidate,
) (TMDBMatch, error) {
	if !identity.valid() {
		return TMDBMatch{}, fmt.Errorf("%w: title identity is required", ErrInvalidTMDBMatch)
	}
	rankedCandidates := make([]rankedMatchCandidate, len(candidates))
	for candidateIndex, candidate := range candidates {
		if candidateError := validateMatchCandidate(candidate); candidateError != nil {
			return TMDBMatch{}, fmt.Errorf("candidate %d: %w", candidateIndex+1, candidateError)
		}
		rankedCandidates[candidateIndex] = rankMatchCandidate(identity.SearchTitle(), candidate)
	}
	sort.Slice(rankedCandidates, func(leftIndex int, rightIndex int) bool {
		left := rankedCandidates[leftIndex]
		right := rankedCandidates[rightIndex]
		if left.score == right.score {
			return left.candidate.TMDBID < right.candidate.TMDBID
		}
		return left.score > right.score
	})

	evidence := MatchEvidence{
		NormalizedQuery:      normalizeMatchTitle(identity.SearchTitle()),
		CandidatesConsidered: len(rankedCandidates),
		Reason:               "no_candidates",
	}
	if len(rankedCandidates) == 0 {
		return NewTMDBMatch(TMDBMatchInput{
			Status:          MatchStatusUnmatched,
			MatcherIdentity: TMDBMatcherIdentity,
			Evidence:        evidence,
		})
	}

	bestCandidate := rankedCandidates[0]
	evidence.BestCandidateTitle = bestCandidate.candidate.Title
	evidence.BestScore = bestCandidate.score
	if len(rankedCandidates) > 1 {
		evidence.RunnerUpScore = rankedCandidates[1].score
	}
	evidence.Margin = evidence.BestScore - evidence.RunnerUpScore
	for _, rankedCandidate := range rankedCandidates {
		if rankedCandidate.score == 1 {
			evidence.ExactCandidateCount++
		}
	}

	status := MatchStatusUnmatched
	acceptedCandidate := MatchCandidate{}
	switch {
	case evidence.ExactCandidateCount == 1:
		status = MatchStatusMatched
		acceptedCandidate = bestCandidate.candidate
		evidence.Reason = "unique_exact_title"
	case evidence.ExactCandidateCount > 1:
		status = MatchStatusReview
		evidence.Reason = "ambiguous_exact_title"
	case evidence.BestScore >= TMDBMatcherAcceptScore &&
		evidence.Margin >= TMDBMatcherMinimumMargin:
		status = MatchStatusMatched
		acceptedCandidate = bestCandidate.candidate
		evidence.Reason = "high_confidence_title"
	case evidence.BestScore >= TMDBMatcherReviewScore:
		status = MatchStatusReview
		evidence.Reason = "similar_title_requires_review"
	default:
		evidence.Reason = "below_review_threshold"
	}

	matchInput := TMDBMatchInput{
		Status:          status,
		MatcherIdentity: TMDBMatcherIdentity,
		Evidence:        evidence,
	}
	if status == MatchStatusMatched {
		matchInput.MediaType = acceptedCandidate.MediaType
		matchInput.TMDBID = acceptedCandidate.TMDBID
	}
	return NewTMDBMatch(matchInput)
}

type rankedMatchCandidate struct {
	candidate MatchCandidate
	score     float64
}

func validateMatchCandidate(candidate MatchCandidate) error {
	if candidate.TMDBID <= 0 {
		return fmt.Errorf("%w: TMDB ID must be positive", ErrInvalidMatchCandidate)
	}
	if candidate.MediaType != MediaTypeMovie && candidate.MediaType != MediaTypeSeries {
		return fmt.Errorf("%w: media type must be movie or series", ErrInvalidMatchCandidate)
	}
	if titleError := validateSingleLine(candidate.Title, "candidate title", false); titleError != nil {
		return fmt.Errorf("%w: %v", ErrInvalidMatchCandidate, titleError)
	}
	if candidate.OriginalTitle != "" {
		if titleError := validateSingleLine(candidate.OriginalTitle, "candidate original title", false); titleError != nil {
			return fmt.Errorf("%w: %v", ErrInvalidMatchCandidate, titleError)
		}
	}
	if math.IsNaN(candidate.Popularity) ||
		math.IsInf(candidate.Popularity, 0) ||
		candidate.Popularity < 0 {
		return fmt.Errorf("%w: popularity must be a finite nonnegative value", ErrInvalidMatchCandidate)
	}
	return nil
}

func validateMatchEvidence(status MatchStatus, evidence MatchEvidence) error {
	if evidence.NormalizedQuery == "" {
		return fmt.Errorf("%w: normalized query is required", ErrInvalidTMDBMatch)
	}
	if evidence.CandidatesConsidered < 0 ||
		evidence.ExactCandidateCount < 0 ||
		evidence.ExactCandidateCount > evidence.CandidatesConsidered {
		return fmt.Errorf("%w: candidate counts are inconsistent", ErrInvalidTMDBMatch)
	}
	for _, score := range []float64{
		evidence.BestScore,
		evidence.RunnerUpScore,
		evidence.Margin,
	} {
		if math.IsNaN(score) || math.IsInf(score, 0) || score < 0 || score > 1 {
			return fmt.Errorf("%w: evidence scores must be between zero and one", ErrInvalidTMDBMatch)
		}
	}
	if math.Abs(evidence.Margin-(evidence.BestScore-evidence.RunnerUpScore)) > 1e-12 {
		return fmt.Errorf("%w: evidence margin is inconsistent", ErrInvalidTMDBMatch)
	}
	if evidence.CandidatesConsidered == 0 {
		if status != MatchStatusUnmatched ||
			evidence.ExactCandidateCount != 0 ||
			evidence.BestCandidateTitle != "" ||
			evidence.BestScore != 0 ||
			evidence.RunnerUpScore != 0 ||
			evidence.Margin != 0 ||
			evidence.Reason != "no_candidates" {
			return fmt.Errorf("%w: no-candidate evidence is inconsistent", ErrInvalidTMDBMatch)
		}
		return nil
	}
	if evidence.BestCandidateTitle == "" {
		return fmt.Errorf("%w: best candidate title is required", ErrInvalidTMDBMatch)
	}
	switch status {
	case MatchStatusMatched:
		validUniqueExact := evidence.Reason == "unique_exact_title" &&
			evidence.ExactCandidateCount == 1 &&
			evidence.BestScore == 1
		validHighConfidence := evidence.Reason == "high_confidence_title" &&
			evidence.ExactCandidateCount == 0 &&
			evidence.BestScore >= TMDBMatcherAcceptScore &&
			evidence.Margin >= TMDBMatcherMinimumMargin
		if !validUniqueExact && !validHighConfidence {
			return fmt.Errorf("%w: matched evidence is inconsistent", ErrInvalidTMDBMatch)
		}
	case MatchStatusReview:
		validAmbiguousExact := evidence.Reason == "ambiguous_exact_title" &&
			evidence.ExactCandidateCount > 1 &&
			evidence.BestScore == 1
		validSimilar := evidence.Reason == "similar_title_requires_review" &&
			evidence.ExactCandidateCount == 0 &&
			evidence.BestScore >= TMDBMatcherReviewScore &&
			(evidence.BestScore < TMDBMatcherAcceptScore ||
				evidence.Margin < TMDBMatcherMinimumMargin)
		if !validAmbiguousExact && !validSimilar {
			return fmt.Errorf("%w: review evidence is inconsistent", ErrInvalidTMDBMatch)
		}
	case MatchStatusUnmatched:
		if evidence.Reason != "below_review_threshold" ||
			evidence.ExactCandidateCount != 0 ||
			evidence.BestScore >= TMDBMatcherReviewScore {
			return fmt.Errorf("%w: unmatched evidence is inconsistent", ErrInvalidTMDBMatch)
		}
	}
	return nil
}

func rankMatchCandidate(query string, candidate MatchCandidate) rankedMatchCandidate {
	score := titleSimilarity(query, candidate.Title)
	if candidate.OriginalTitle != "" {
		originalTitleScore := titleSimilarity(query, candidate.OriginalTitle)
		if originalTitleScore > score {
			score = originalTitleScore
		}
	}
	return rankedMatchCandidate{candidate: candidate, score: score}
}

func titleSimilarity(left string, right string) float64 {
	normalizedLeft := normalizeMatchTitle(left)
	normalizedRight := normalizeMatchTitle(right)
	if normalizedLeft == normalizedRight {
		return 1
	}
	leftRunes := []rune(normalizedLeft)
	rightRunes := []rune(normalizedRight)
	maximumLength := max(len(leftRunes), len(rightRunes))
	if maximumLength == 0 {
		return 0
	}
	characterScore := 1 - float64(levenshteinDistance(leftRunes, rightRunes))/float64(maximumLength)
	tokenScore := tokenJaccard(normalizedLeft, normalizedRight)
	return clampScore(characterScore*0.7 + tokenScore*0.3)
}

func normalizeMatchTitle(value string) string {
	var normalized strings.Builder
	pendingSeparator := false
	for _, character := range strings.ToLower(value) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			if pendingSeparator && normalized.Len() > 0 {
				normalized.WriteByte(' ')
			}
			normalized.WriteRune(character)
			pendingSeparator = false
			continue
		}
		pendingSeparator = true
	}
	return normalized.String()
}

func levenshteinDistance(left []rune, right []rune) int {
	previous := make([]int, len(right)+1)
	for rightIndex := range previous {
		previous[rightIndex] = rightIndex
	}
	for leftIndex, leftRune := range left {
		current := make([]int, len(right)+1)
		current[0] = leftIndex + 1
		for rightIndex, rightRune := range right {
			substitutionCost := 1
			if leftRune == rightRune {
				substitutionCost = 0
			}
			current[rightIndex+1] = min(
				current[rightIndex]+1,
				previous[rightIndex+1]+1,
				previous[rightIndex]+substitutionCost,
			)
		}
		previous = current
	}
	return previous[len(right)]
}

func tokenJaccard(left string, right string) float64 {
	leftTokens := stringSet(strings.Fields(left))
	rightTokens := stringSet(strings.Fields(right))
	if len(leftTokens) == 0 || len(rightTokens) == 0 {
		return 0
	}
	intersection := 0
	for token := range leftTokens {
		if _, exists := rightTokens[token]; exists {
			intersection++
		}
	}
	union := len(leftTokens) + len(rightTokens) - intersection
	return float64(intersection) / float64(union)
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func clampScore(score float64) float64 {
	switch {
	case score < 0:
		return 0
	case score > 1:
		return 1
	default:
		return score
	}
}
