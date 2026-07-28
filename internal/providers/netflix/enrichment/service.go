// Package enrichment coordinates explicit, failure-atomic Netflix TMDB enrichment.
package enrichment

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
)

// ErrorCode is the closed enrichment failure identity.
type ErrorCode string

const (
	ErrorAuthorizationRequired ErrorCode = "authorization_required"
	ErrorInvalidInput          ErrorCode = "invalid_input"
	ErrorCanceled              ErrorCode = "canceled"
	ErrorRemoteFailed          ErrorCode = "remote_failed"
	ErrorCacheFailed           ErrorCode = "cache_failed"
	ErrorIncomplete            ErrorCode = "incomplete"
)

// Error describes an enrichment failure without exposing a title query.
type Error struct {
	code             ErrorCode
	titleIdentityKey string
	cause            error
}

func (enrichmentError *Error) Error() string {
	if enrichmentError.titleIdentityKey == "" {
		return fmt.Sprintf("Netflix TMDB enrichment failed with %s", enrichmentError.code)
	}
	return fmt.Sprintf(
		"Netflix TMDB enrichment failed for title identity %s with %s",
		enrichmentError.titleIdentityKey,
		enrichmentError.code,
	)
}

func (enrichmentError *Error) Unwrap() error {
	return enrichmentError.cause
}

// Code returns the machine-readable failure identity.
func (enrichmentError *Error) Code() ErrorCode {
	return enrichmentError.code
}

// TitleIdentityKey returns the opaque affected title identity, when available.
func (enrichmentError *Error) TitleIdentityKey() string {
	return enrichmentError.titleIdentityKey
}

// Authorization is proof that the caller explicitly initiated TMDB title queries.
type Authorization struct {
	explicit bool
}

// AuthorizeTMDBTitleQueries creates the explicit remote-boundary authorization.
func AuthorizeTMDBTitleQueries() Authorization {
	return Authorization{explicit: true}
}

// MetadataClient is the narrow server-owned TMDB client surface.
type MetadataClient interface {
	Identity() string
	Search(context.Context, string, tmdb.Locale) ([]tmdb.Candidate, error)
	Details(context.Context, tmdb.Candidate, tmdb.Locale) (tmdb.Details, error)
}

// Result is one complete cached or freshly evaluated title outcome.
type Result struct {
	titleIdentity netflix.TitleIdentity
	match         netflix.TMDBMatch
	metadata      *netflix.TitleMetadata
	cacheHit      bool
}

// TitleIdentity returns the derived title that was evaluated.
func (result Result) TitleIdentity() netflix.TitleIdentity {
	return result.titleIdentity
}

// Match returns the closed matching outcome.
func (result Result) Match() netflix.TMDBMatch {
	return result.match
}

// Metadata returns accepted metadata only for matched outcomes.
func (result Result) Metadata() (netflix.TitleMetadata, bool) {
	if result.metadata == nil {
		return netflix.TitleMetadata{}, false
	}
	return *result.metadata, true
}

// CacheHit reports whether the complete current result came from private cache.
func (result Result) CacheHit() bool {
	return result.cacheHit
}

// Service enriches unique derived title identities under fixed product limits.
type Service struct {
	client MetadataClient
	cache  *Cache
}

// NewService requires the shared TMDB client and private current cache.
func NewService(client MetadataClient, cache *Cache) (*Service, error) {
	if client == nil {
		return nil, errors.New("create Netflix enrichment service: TMDB client is required")
	}
	if client.Identity() != tmdb.ClientIdentity {
		return nil, fmt.Errorf(
			"create Netflix enrichment service: client identity must be %s",
			tmdb.ClientIdentity,
		)
	}
	if cache == nil || !cache.ready() {
		return nil, errors.New("create Netflix enrichment service: private cache is required")
	}
	return &Service{client: client, cache: cache}, nil
}

// Enrich evaluates each unique derived title and returns no partial result on failure.
func (service *Service) Enrich(
	ctx context.Context,
	authorization Authorization,
	titleIdentities []netflix.TitleIdentity,
	locale tmdb.Locale,
) ([]Result, error) {
	if !authorization.explicit {
		return nil, newError(ErrorAuthorizationRequired, "", nil)
	}
	if ctx == nil {
		return nil, newError(ErrorInvalidInput, "", errors.New("context is required"))
	}
	if contextError := ctx.Err(); contextError != nil {
		return nil, newError(ErrorCanceled, "", contextError)
	}
	if len(titleIdentities) == 0 {
		return nil, newError(
			ErrorInvalidInput,
			"",
			errors.New("at least one title identity is required"),
		)
	}
	if _, localeError := tmdb.NewLocale(locale.String()); localeError != nil {
		return nil, newError(ErrorInvalidInput, "", tmdb.ErrInvalidLocale)
	}

	uniqueIdentities, identityError := uniqueCurrentTitleIdentities(titleIdentities)
	if identityError != nil {
		return nil, identityError
	}
	results := make([]Result, len(uniqueIdentities))
	misses := make([]enrichmentJob, 0, len(uniqueIdentities))
	for identityIndex, identity := range uniqueIdentities {
		lookup := cacheLookup{
			titleIdentityKey: identity.Key(),
			query:            identity.SearchTitle(),
			locale:           locale.String(),
			clientIdentity:   service.client.Identity(),
			matcherIdentity:  netflix.TMDBMatcherIdentity,
		}
		cachedOutcome, found, cacheError := service.cache.lookup(ctx, lookup)
		if cacheError != nil {
			if contextError := ctx.Err(); contextError != nil {
				return nil, newError(ErrorCanceled, identity.Key(), contextError)
			}
			return nil, newError(ErrorCacheFailed, identity.Key(), cacheError)
		}
		if found {
			results[identityIndex] = resultFromOutcome(identity, cachedOutcome, true)
			continue
		}
		misses = append(misses, enrichmentJob{
			resultIndex:   identityIndex,
			titleIdentity: identity,
			locale:        locale,
			cacheLookup:   lookup,
		})
	}
	if len(misses) == 0 {
		return results, nil
	}

	remoteResults, remoteError := service.runRemoteJobs(ctx, misses)
	if remoteError != nil {
		return nil, remoteError
	}
	cacheWrites := make([]cacheWrite, len(remoteResults))
	for resultIndex, remoteResult := range remoteResults {
		results[remoteResult.job.resultIndex] = resultFromOutcome(
			remoteResult.job.titleIdentity,
			remoteResult.outcome,
			false,
		)
		cacheWrites[resultIndex] = cacheWrite{
			lookup:  remoteResult.job.cacheLookup,
			outcome: remoteResult.outcome,
		}
	}
	if cacheError := service.cache.putAll(ctx, cacheWrites); cacheError != nil {
		if contextError := ctx.Err(); contextError != nil {
			return nil, newError(ErrorCanceled, "", contextError)
		}
		return nil, newError(ErrorCacheFailed, "", cacheError)
	}
	return results, nil
}

type enrichmentJob struct {
	resultIndex   int
	titleIdentity netflix.TitleIdentity
	locale        tmdb.Locale
	cacheLookup   cacheLookup
}

type enrichmentJobResult struct {
	job     enrichmentJob
	outcome cachedOutcome
	err     error
}

func (service *Service) runRemoteJobs(
	ctx context.Context,
	jobs []enrichmentJob,
) ([]enrichmentJobResult, error) {
	workerContext, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()

	jobChannel := make(chan enrichmentJob)
	resultChannel := make(chan enrichmentJobResult, len(jobs))
	workerCount := min(product.MaxTMDBConcurrency, len(jobs))
	var workers sync.WaitGroup
	for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range jobChannel {
				outcome, enrichError := service.enrichOne(workerContext, job)
				resultChannel <- enrichmentJobResult{
					job:     job,
					outcome: outcome,
					err:     enrichError,
				}
			}
		}()
	}
	go func() {
		defer close(jobChannel)
		for _, job := range jobs {
			select {
			case <-workerContext.Done():
				return
			case jobChannel <- job:
			}
		}
	}()
	go func() {
		workers.Wait()
		close(resultChannel)
	}()

	completed := make([]enrichmentJobResult, 0, len(jobs))
	var firstError error
	for result := range resultChannel {
		if result.err != nil && firstError == nil {
			firstError = result.err
			cancelWorkers()
		}
		completed = append(completed, result)
	}
	if firstError != nil {
		return nil, firstError
	}
	if len(completed) != len(jobs) {
		return nil, newError(
			ErrorIncomplete,
			"",
			fmt.Errorf("completed %d of %d title queries", len(completed), len(jobs)),
		)
	}
	sort.Slice(completed, func(leftIndex int, rightIndex int) bool {
		return completed[leftIndex].job.resultIndex < completed[rightIndex].job.resultIndex
	})
	return completed, nil
}

func (service *Service) enrichOne(
	ctx context.Context,
	job enrichmentJob,
) (cachedOutcome, error) {
	candidates, searchError := service.client.Search(
		ctx,
		job.titleIdentity.SearchTitle(),
		job.locale,
	)
	if searchError != nil {
		if contextError := ctx.Err(); contextError != nil {
			return cachedOutcome{}, newError(
				ErrorCanceled,
				job.titleIdentity.Key(),
				contextError,
			)
		}
		return cachedOutcome{}, newError(
			ErrorRemoteFailed,
			job.titleIdentity.Key(),
			searchError,
		)
	}
	matchCandidates := make([]netflix.MatchCandidate, len(candidates))
	candidateByIdentity := make(map[tmdbCandidateIdentity]tmdb.Candidate, len(candidates))
	for candidateIndex, candidate := range candidates {
		matchCandidates[candidateIndex] = netflix.MatchCandidate{
			TMDBID:        candidate.TMDBID,
			MediaType:     candidate.MediaType,
			Title:         candidate.Title,
			OriginalTitle: candidate.OriginalTitle,
			Popularity:    candidate.Popularity,
		}
		candidateByIdentity[tmdbCandidateIdentity{
			mediaType: candidate.MediaType,
			tmdbID:    candidate.TMDBID,
		}] = candidate
	}
	match, matchError := netflix.MatchTMDBTitle(job.titleIdentity, matchCandidates)
	if matchError != nil {
		return cachedOutcome{}, newError(
			ErrorRemoteFailed,
			job.titleIdentity.Key(),
			matchError,
		)
	}
	outcome := cachedOutcome{match: match}
	if match.Status() != netflix.MatchStatusMatched {
		return outcome, nil
	}
	acceptedCandidate, exists := candidateByIdentity[tmdbCandidateIdentity{
		mediaType: match.MediaType(),
		tmdbID:    match.TMDBID(),
	}]
	if !exists {
		return cachedOutcome{}, newError(
			ErrorIncomplete,
			job.titleIdentity.Key(),
			errors.New("accepted candidate is missing"),
		)
	}
	details, detailsError := service.client.Details(ctx, acceptedCandidate, job.locale)
	if detailsError != nil {
		if contextError := ctx.Err(); contextError != nil {
			return cachedOutcome{}, newError(
				ErrorCanceled,
				job.titleIdentity.Key(),
				contextError,
			)
		}
		return cachedOutcome{}, newError(
			ErrorRemoteFailed,
			job.titleIdentity.Key(),
			detailsError,
		)
	}
	metadata, metadataError := netflix.NewTitleMetadata(netflix.TitleMetadataInput{
		MediaType:        details.MediaType,
		Genres:           details.Genres,
		ReleaseDate:      details.ReleaseDate,
		RuntimeMinutes:   details.RuntimeMinutes,
		OriginalLanguage: details.OriginalLanguage,
		VoteAverage:      details.VoteAverage,
		VoteCount:        details.VoteCount,
		OriginCountries:  details.OriginCountries,
		Seasons:          details.Seasons,
		Episodes:         details.Episodes,
		TMDBID:           details.TMDBID,
		MatchedTitle:     details.MatchedTitle,
		Description:      details.Description,
	})
	if metadataError != nil {
		return cachedOutcome{}, newError(
			ErrorRemoteFailed,
			job.titleIdentity.Key(),
			metadataError,
		)
	}
	outcome.metadata = &metadata
	return outcome, nil
}

type tmdbCandidateIdentity struct {
	mediaType netflix.MediaType
	tmdbID    int64
}

func uniqueCurrentTitleIdentities(
	identities []netflix.TitleIdentity,
) ([]netflix.TitleIdentity, error) {
	identityByKey := make(map[string]netflix.TitleIdentity, len(identities))
	for _, identity := range identities {
		if identity.Key() == "" ||
			identity.SearchTitle() == "" ||
			identity.Version() != netflix.TitleIdentityVersion {
			return nil, newError(
				ErrorInvalidInput,
				identity.Key(),
				errors.New("current title identity is required"),
			)
		}
		if existingIdentity, exists := identityByKey[identity.Key()]; exists &&
			existingIdentity.SearchTitle() != identity.SearchTitle() {
			return nil, newError(
				ErrorInvalidInput,
				identity.Key(),
				errors.New("title identity key is inconsistent"),
			)
		}
		identityByKey[identity.Key()] = identity
	}
	uniqueIdentities := make([]netflix.TitleIdentity, 0, len(identityByKey))
	for _, identity := range identityByKey {
		uniqueIdentities = append(uniqueIdentities, identity)
	}
	sort.Slice(uniqueIdentities, func(leftIndex int, rightIndex int) bool {
		return uniqueIdentities[leftIndex].Key() < uniqueIdentities[rightIndex].Key()
	})
	return uniqueIdentities, nil
}

func resultFromOutcome(
	identity netflix.TitleIdentity,
	outcome cachedOutcome,
	cacheHit bool,
) Result {
	result := Result{
		titleIdentity: identity,
		match:         outcome.match,
		cacheHit:      cacheHit,
	}
	if outcome.metadata != nil {
		metadata := *outcome.metadata
		result.metadata = &metadata
	}
	return result
}

func newError(code ErrorCode, titleIdentityKey string, cause error) *Error {
	return &Error{
		code:             code,
		titleIdentityKey: titleIdentityKey,
		cause:            cause,
	}
}
