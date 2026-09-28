package primevideo

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/media"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/enrichment"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
)

// MatcherIdentity identifies the current Prime derivation and acceptance rules.
const MatcherIdentity = "prime-exact-title-matcher-v2"

var seasonPattern = regexp.MustCompile(`^(.+?) Season ([1-9][0-9]{0,2})$`)

func deriveTitle(value *media.Activity) {
	value.SearchTitle = value.Title
	parts := seasonPattern.FindStringSubmatch(value.Title)
	if len(parts) == 0 {
		return
	}
	series := strings.TrimSpace(parts[1])
	value.SeriesTitle = series
	value.SearchTitle = series
	value.SeasonNumber, _ = strconv.Atoi(parts[2])
}

type titleInterpretation struct {
	query   string
	series  string
	episode string
}

func titleInterpretations(value media.Activity) []titleInterpretation {
	result := []titleInterpretation{{query: value.SearchTitle, series: value.SeriesTitle, episode: value.EpisodeTitle}}
	if value.SeriesTitle != "" && value.EpisodeTitle == "" {
		if episode, series, found := strings.Cut(value.SeriesTitle, "-"); found && strings.TrimSpace(episode) != "" && strings.TrimSpace(series) != "" {
			result = append(result, titleInterpretation{query: strings.TrimSpace(series), series: strings.TrimSpace(series), episode: strings.TrimSpace(episode)})
		}
	}
	return result
}

func matchKey(value string) string {
	var result strings.Builder
	for _, character := range strings.ToLower(value) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			result.WriteRune(character)
		} else {
			result.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(result.String()), " ")
}

// ResolveTitle returns one complete source record with a conservative title outcome.
// Only exact normalized names with one identity are accepted. Popularity is not evidence.
func ResolveTitle(ctx context.Context, authorization enrichment.Authorization, record media.Record, client enrichment.MetadataClient, locale tmdb.Locale) (media.Record, error) {
	if !authorization.Explicit() {
		return media.Record{}, newError("tmdb_consent_required", 0, nil)
	}
	if client == nil || client.Identity() != tmdb.ClientIdentity {
		return media.Record{}, newError("invalid_client", 0, nil)
	}
	value := record.Snapshot()
	if value.SearchTitle == "" || value.Kind == media.Search || value.Kind == media.Trailer || value.ContentType == "promotion" || value.ContentType == "trailer" {
		return record, nil
	}
	type interpretedCandidate struct {
		candidate      tmdb.Candidate
		interpretation titleInterpretation
	}
	unique := map[string]interpretedCandidate{}
	hasCandidates := false
	for index, interpretation := range titleInterpretations(value) {
		candidates, err := client.Search(ctx, interpretation.query, locale)
		if err != nil {
			return media.Record{}, newError("remote_failed", 0, err)
		}
		hasCandidates = hasCandidates || len(candidates) > 0
		for _, candidate := range candidates {
			if interpretation.series != "" && candidate.MediaType != netflix.MediaTypeSeries {
				continue
			}
			if matchKey(candidate.Title) == matchKey(interpretation.query) || matchKey(candidate.OriginalTitle) == matchKey(interpretation.query) {
				key := fmt.Sprintf("%d:%s:%d", index, candidate.MediaType, candidate.TMDBID)
				unique[key] = interpretedCandidate{candidate: candidate, interpretation: interpretation}
			}
		}
	}
	value.MatchStatus = "unmatched"
	if hasCandidates {
		value.MatchStatus = "review"
	}
	if len(unique) != 1 {
		return media.NewRecord(value)
	}
	var accepted interpretedCandidate
	for _, candidate := range unique {
		accepted = candidate
	}
	details, err := client.Details(ctx, accepted.candidate, locale)
	if err != nil {
		return media.Record{}, newError("remote_failed", 0, err)
	}
	if details.TMDBID != accepted.candidate.TMDBID || details.MediaType != accepted.candidate.MediaType {
		return media.Record{}, newError("remote_failed", 0, errors.New("metadata identity changed"))
	}
	metadata, err := netflix.NewTitleMetadata(netflix.TitleMetadataInput{TMDBID: details.TMDBID, MediaType: details.MediaType, MatchedTitle: details.MatchedTitle, IMDbID: details.IMDbID, Genres: details.Genres, ReleaseDate: details.ReleaseDate, RuntimeMinutes: details.RuntimeMinutes, OriginalLanguage: details.OriginalLanguage, VoteAverage: details.VoteAverage, VoteCount: details.VoteCount, OriginCountries: details.OriginCountries, Seasons: details.Seasons, Episodes: details.Episodes, Description: details.Description})
	if err != nil {
		return media.Record{}, newError("remote_failed", 0, err)
	}
	value.MatchStatus = "matched"
	value.SearchTitle = accepted.interpretation.query
	value.SeriesTitle = accepted.interpretation.series
	value.EpisodeTitle = accepted.interpretation.episode
	value.Metadata = &media.Metadata{TMDBID: metadata.TMDBID(), MediaType: string(metadata.MediaType()), Title: metadata.MatchedTitle(), IMDbID: metadata.IMDbID(), Genres: metadata.Genres(), ReleaseDate: metadata.ReleaseDate(), OriginalLanguage: metadata.OriginalLanguage()}
	if minutes, present := metadata.RuntimeMinutes(); present {
		value.Metadata.RuntimeMinutes = &minutes
	}
	return media.NewRecord(value)
}
