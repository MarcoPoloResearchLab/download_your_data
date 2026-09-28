package primevideo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/privatepath"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/media"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/enrichment"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
)

const enrichmentLeasePath = providerDirectory + "/enrichment.lock"

func eligible(value media.Activity) bool {
	return value.TitleStatus == "present" && value.Kind != media.Search && value.Kind != media.Trailer && value.ContentType != "promotion" && value.ContentType != "trailer"
}

// BeginEnrichment creates a replacement from the current active generation.
// A request for the same pending source and locale resumes its durable checkpoints.
func (store *Store) BeginEnrichment(ctx context.Context, sourceID string, locale tmdb.Locale, authorization enrichment.Authorization) (Generation, error) {
	if !authorization.Explicit() {
		return Generation{}, newError("tmdb_consent_required", 0, nil)
	}
	if err := ctx.Err(); err != nil {
		return Generation{}, newError("canceled", 0, err)
	}
	if store.state.Active == nil || store.state.Active.Generation.ID != sourceID {
		return Generation{}, newError("not_found", 0, nil)
	}
	if pending := store.state.Pending; pending != nil && pending.Generation.State != Failed {
		if pending.Generation.State == Enriching && pending.Generation.SourceGenerationID == sourceID && pending.Generation.Locale == locale.String() {
			return cloneGeneration(pending.Generation), nil
		}
		return Generation{}, newError("conflict", 0, nil)
	}
	source := store.state.Active
	generation, err := newGeneration()
	if err != nil {
		return Generation{}, err
	}
	next := store.state
	pending := &storedGeneration{}
	next.Pending = pending
	pending.Generation = generation
	pending.Generation.AnalysisLevel = "tmdb"
	pending.Generation.TitleQueriesAuthorized = true
	pending.Generation.SourceGenerationID = sourceID
	pending.Generation.Locale = locale.String()
	pending.Generation.MatcherIdentity = MatcherIdentity
	pending.Generation.ClientIdentity = tmdb.ClientIdentity
	pending.Generation.Preview = cloneGeneration(source.Generation).Preview
	pending.Generation.ProfileLabel = source.Generation.ProfileLabel
	pending.Generation.Datasets = append([]string{}, source.Generation.Datasets...)
	pending.Generation.RecordCount = source.Generation.RecordCount
	pending.Records = make([]media.Activity, 0, len(source.Records))
	titles := map[string]bool{}
	completed := map[string]bool{}
	for _, row := range source.Records {
		value := row
		value.Source.GenerationID = generation.ID
		if source.Generation.Locale != locale.String() || source.Generation.MatcherIdentity != MatcherIdentity || source.Generation.ClientIdentity != tmdb.ClientIdentity {
			value.MatchStatus = "not_enriched"
			value.Metadata = nil
		}
		pending.Records = append(pending.Records, value)
		if eligible(value) {
			titles[value.TitleIdentity] = true
			if value.MatchStatus != "not_enriched" {
				completed[value.TitleIdentity] = true
			}
		}
	}
	pending.Generation.TotalTitles = len(titles)
	pending.Generation.CompletedTitles = len(completed)
	transition(&pending.Generation, Enriching)
	if err := store.save(next); err != nil {
		return Generation{}, err
	}
	return cloneGeneration(pending.Generation), nil
}

// EnrichmentRecords returns unique unfinished authorized title queries.
func (store *Store) EnrichmentRecords(id string) ([]media.Record, error) {
	pending, err := store.pending(id, Enriching)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	result := []media.Record{}
	for _, value := range pending.Records {
		if !eligible(value) || value.MatchStatus != "not_enriched" || seen[value.TitleIdentity] {
			continue
		}
		seen[value.TitleIdentity] = true
		record, err := media.NewRecord(value)
		if err != nil {
			return nil, newError("invalid_persistence", 0, err)
		}
		result = append(result, record)
	}
	return result, nil
}

// Checkpoint saves one completed query without changing the active library.
func (store *Store) Checkpoint(ctx context.Context, id string, result media.Record) error {
	if err := ctx.Err(); err != nil {
		return newError("canceled", 0, err)
	}
	pending, err := store.pending(id, Enriching)
	if err != nil {
		return err
	}
	outcome := result.Snapshot()
	for index, value := range pending.Records {
		if !eligible(value) || value.TitleIdentity != outcome.TitleIdentity {
			continue
		}
		value.MatchStatus = outcome.MatchStatus
		value.Metadata = outcome.Metadata
		value.SearchTitle = outcome.SearchTitle
		value.SeriesTitle = outcome.SeriesTitle
		value.EpisodeTitle = outcome.EpisodeTitle
		value.EpisodeIdentity = ""
		record, err := media.NewRecord(value)
		if err != nil {
			return newError("invalid_persistence", 0, err)
		}
		pending.Records[index] = record.Snapshot()
	}
	pending.Generation.CompletedTitles++
	next := store.state
	next.Pending = pending
	return store.save(next)
}

// CompleteEnrichment atomically activates a complete replacement.
func (store *Store) CompleteEnrichment(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return newError("canceled", 0, err)
	}
	pending, err := store.pending(id, Enriching)
	if err != nil {
		return err
	}
	if pending.Generation.CompletedTitles != pending.Generation.TotalTitles {
		return newError("incomplete", 0, nil)
	}
	transition(&pending.Generation, Ready)
	next := store.state
	next.Active = pending
	next.Pending = nil
	return store.save(next)
}

// FailEnrichment preserves the active library and removes private pending rows.
func (store *Store) FailEnrichment(id string, failure error) error {
	pending, err := store.pending(id, Enriching)
	if err != nil {
		return err
	}
	transition(&pending.Generation, Failed)
	pending.Generation.Failure = errorCode(failure)
	pending.Records = []media.Activity{}
	next := store.state
	next.Pending = pending
	return store.save(next)
}

// ClaimEnrichment acquires a worker lease independent of short state transactions.
func ClaimEnrichment(root privatepath.Root) (*os.File, error) {
	file, err := root.File(enrichmentLeasePath)
	if err != nil {
		return nil, fmt.Errorf("resolve Prime worker lease: %w", err)
	}
	if err = file.Prepare(); err != nil {
		return nil, fmt.Errorf("prepare Prime worker lease: %w", err)
	}
	lease, err := os.OpenFile(file.Path(), os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open Prime worker lease: %w", err)
	}
	if err = syscall.Flock(int(lease.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, newError("conflict", 0, errors.Join(err, lease.Close()))
	}
	return lease, nil
}
