package library

import (
	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/enrichment"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
)

func validateGenerationEnrichmentFields(generation generationState) error {
	if generation.CompletedTitleCount < 0 ||
		generation.MatchedTitleCount < 0 ||
		generation.ReviewTitleCount < 0 ||
		generation.UnmatchedTitleCount < 0 ||
		generation.CacheHitTitleCount < 0 ||
		generation.EnrichmentCheckpointBytes < 0 ||
		generation.EnrichmentCheckpointBytes > product.MaxNetflixWorkingBytes ||
		generation.CompletedTitleCount > generation.UniqueTitleCount ||
		generation.CacheHitTitleCount > generation.CompletedTitleCount ||
		generation.MatchedTitleCount+
			generation.ReviewTitleCount+
			generation.UnmatchedTitleCount != generation.CompletedTitleCount {
		return invalidStatePersistence("enrichment counts or bytes are invalid")
	}
	if generation.AnalysisLevel == AnalysisLevelLocal {
		if generation.SourceGenerationID != "" ||
			generation.Locale != "" ||
			generation.TMDBAuthorizationIdentity != "" ||
			generation.TMDBClientIdentity != "" ||
			generation.TMDBMatcherIdentity != "" ||
			generation.TMDBCacheIdentity != "" ||
			generation.SourceRecordsSHA256 != "" ||
			generation.SourceAnalyticsSHA256 != "" ||
			generation.CompletedTitleCount != 0 ||
			generation.EnrichmentCheckpointBytes != 0 {
			return invalidStatePersistence("local generation contains TMDB state")
		}
		return nil
	}
	if generation.SourceGenerationID == "" ||
		generation.TMDBAuthorizationIdentity != tmdbAuthorizationContract ||
		generation.TMDBClientIdentity != tmdb.ClientIdentity ||
		generation.TMDBMatcherIdentity != netflix.TMDBMatcherIdentity ||
		generation.TMDBCacheIdentity != enrichment.CacheFreshnessIdentity ||
		!validSHA256(generation.SourceRecordsSHA256) ||
		!validSHA256(generation.SourceAnalyticsSHA256) ||
		generation.UniqueTitleCount <= 0 {
		return invalidStatePersistence("TMDB generation provenance is invalid")
	}
	if _, localeError := tmdb.NewLocale(generation.Locale); localeError != nil {
		return invalidStatePersistence("TMDB generation locale is invalid")
	}
	if (generation.State == GenerationStateReady ||
		(generation.State == GenerationStateEnriching &&
			validSHA256(generation.RecordsSHA256))) &&
		generation.CompletedTitleCount != generation.UniqueTitleCount {
		return invalidStatePersistence("complete TMDB generation has incomplete match coverage")
	}
	if generation.State == GenerationStateReady &&
		generation.EnrichmentCheckpointBytes != 0 {
		return invalidStatePersistence("ready TMDB generation retains enrichment checkpoints")
	}
	return nil
}

func validateEventEnrichmentFields(
	generation generationState,
	event eventState,
) error {
	if event.CompletedTitleCount < 0 ||
		event.TotalTitleCount < 0 ||
		event.MatchedTitleCount < 0 ||
		event.ReviewTitleCount < 0 ||
		event.UnmatchedTitleCount < 0 ||
		event.CacheHitTitleCount < 0 ||
		event.ProgressPercent < 0 ||
		event.ProgressPercent > 100 ||
		event.CompletedTitleCount > event.TotalTitleCount ||
		event.CacheHitTitleCount > event.CompletedTitleCount ||
		event.MatchedTitleCount+
			event.ReviewTitleCount+
			event.UnmatchedTitleCount != event.CompletedTitleCount ||
		event.ProgressPercent != progressPercent(
			event.CompletedTitleCount,
			event.TotalTitleCount,
		) {
		return invalidStatePersistence("generation event progress is invalid")
	}
	if generation.AnalysisLevel == AnalysisLevelLocal {
		if event.CompletedTitleCount != 0 ||
			event.TotalTitleCount != 0 {
			return invalidStatePersistence("local generation event contains TMDB progress")
		}
		return nil
	}
	if event.TotalTitleCount != generation.UniqueTitleCount {
		return invalidStatePersistence("TMDB event total does not match its generation")
	}
	return nil
}
