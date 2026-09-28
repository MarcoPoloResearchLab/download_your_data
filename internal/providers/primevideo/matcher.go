package primevideo

import (
	"context"
	"errors"
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
const MatcherIdentity = "prime-exact-title-matcher-v1"

var seasonPattern = regexp.MustCompile(`^(.+?) Season ([1-9][0-9]{0,2})$`)

func deriveTitle(value *media.Activity) {
	value.SearchTitle = value.Title
	parts := seasonPattern.FindStringSubmatch(value.Title)
	if len(parts) == 0 {
		return
	}
	series := strings.TrimSpace(parts[1])
	if episode, title, found := strings.Cut(series, "-"); found && strings.TrimSpace(episode) != "" && strings.TrimSpace(title) != "" {
		value.EpisodeTitle = strings.TrimSpace(episode)
		series = strings.TrimSpace(title)
	}
	value.SeriesTitle = series
	value.SearchTitle = series
	value.SeasonNumber, _ = strconv.Atoi(parts[2])
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
	candidates, err := client.Search(ctx, value.SearchTitle, locale)
	if err != nil {
		return media.Record{}, newError("remote_failed", 0, err)
	}
	unique := map[string]tmdb.Candidate{}
	for _, candidate := range candidates {
		if value.SeriesTitle != "" && candidate.MediaType != netflix.MediaTypeSeries {
			continue
		}
		if matchKey(candidate.Title) == matchKey(value.SearchTitle) || matchKey(candidate.OriginalTitle) == matchKey(value.SearchTitle) {
			key := string(candidate.MediaType) + ":" + strconv.FormatInt(candidate.TMDBID, 10)
			unique[key] = candidate
		}
	}
	value.MatchStatus = "unmatched"
	if len(candidates) > 0 {
		value.MatchStatus = "review"
	}
	if len(unique) != 1 {
		return media.NewRecord(value)
	}
	var accepted tmdb.Candidate
	for _, candidate := range unique {
		accepted = candidate
	}
	details, err := client.Details(ctx, accepted, locale)
	if err != nil {
		return media.Record{}, newError("remote_failed", 0, err)
	}
	if details.TMDBID != accepted.TMDBID || details.MediaType != accepted.MediaType {
		return media.Record{}, newError("remote_failed", 0, errors.New("metadata identity changed"))
	}
	metadata, err := netflix.NewTitleMetadata(netflix.TitleMetadataInput{TMDBID: details.TMDBID, MediaType: details.MediaType, MatchedTitle: details.MatchedTitle, IMDbID: details.IMDbID, Genres: details.Genres, ReleaseDate: details.ReleaseDate, RuntimeMinutes: details.RuntimeMinutes, OriginalLanguage: details.OriginalLanguage, VoteAverage: details.VoteAverage, VoteCount: details.VoteCount, OriginCountries: details.OriginCountries, Seasons: details.Seasons, Episodes: details.Episodes, Description: details.Description})
	if err != nil {
		return media.Record{}, newError("remote_failed", 0, err)
	}
	value.MatchStatus = "matched"
	value.Metadata = &media.Metadata{TMDBID: metadata.TMDBID(), MediaType: string(metadata.MediaType()), Title: metadata.MatchedTitle(), IMDbID: metadata.IMDbID(), Genres: metadata.Genres(), ReleaseDate: metadata.ReleaseDate(), OriginalLanguage: metadata.OriginalLanguage()}
	if minutes, present := metadata.RuntimeMinutes(); present {
		value.Metadata.RuntimeMinutes = &minutes
	}
	return media.NewRecord(value)
}
