package library

import (
	"bytes"
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/enrichment"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
)

func TestIMDbIDSurvivesEnrichmentCheckpointRestart(testContext *testing.T) {
	fixture := newWorkspaceFixture(testContext)
	checkpointReached := make(chan struct{})
	client := &imdbCheckpointClient{maxQueries: 1}
	workspace := fixture.openWithClient(testContext, client, workspaceOptions{
		now: fixture.clock, entropy: testEntropy(0xb2),
		afterEnrichmentCheckpoint: func(ctx context.Context, _ string, completed int) error {
			if completed == 1 {
				close(checkpointReached)
				<-ctx.Done()
				return ctx.Err()
			}
			return nil
		},
	})
	local := importLocalFixture(testContext, workspace, syntheticViewingCSV)
	enriched, createError := workspace.CreateTMDBGeneration(context.Background(), local.ID,
		mustEnrichmentLocale(testContext), enrichment.AuthorizeTMDBTitleQueries())
	if createError != nil {
		testContext.Fatal(createError)
	}
	select {
	case <-checkpointReached:
	case <-time.After(5 * time.Second):
		testContext.Fatal("IMDb checkpoint was not reached")
	}
	if closeError := workspace.Close(); closeError != nil {
		testContext.Fatal(closeError)
	}
	resumeClient := &imdbCheckpointClient{}
	reopened := fixture.openWithClient(testContext, resumeClient, workspaceOptions{
		now: fixture.clock, entropy: testEntropy(0xb3),
	})
	defer reopened.Close()
	waitForGenerationState(testContext, reopened, enriched.ID, GenerationStateReady)
	page, recordsError := reopened.Records(context.Background(), enriched.ID, "", 10, ActivityFilter{})
	if recordsError != nil || len(page.Records) != 4 {
		testContext.Fatalf("read resumed IMDb records: %v", recordsError)
	}
	for _, record := range page.Records {
		if record.Metadata == nil || record.Metadata.IMDbID != "tt0133093" ||
			record.Metadata.IMDbIDSource != netflix.IMDbIDSource {
			testContext.Fatal("resumed checkpoint lost the IMDb identity")
		}
	}
	if resumeClient.searches.Load() != 2 {
		testContext.Fatal("restart repeated the completed checkpoint query")
	}
}

type imdbCheckpointClient struct {
	searches   atomic.Int64
	maxQueries int64
}

func (client *imdbCheckpointClient) Identity() string { return tmdb.ClientIdentity }
func (client *imdbCheckpointClient) Search(ctx context.Context, query string, _ tmdb.Locale) ([]tmdb.Candidate, error) {
	count := client.searches.Add(1)
	if client.maxQueries > 0 && count > client.maxQueries {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return []tmdb.Candidate{{TMDBID: 1001, MediaType: netflix.MediaTypeMovie, Title: query, OriginalTitle: query}}, nil
}
func (client *imdbCheckpointClient) Details(_ context.Context, candidate tmdb.Candidate, locale tmdb.Locale) (tmdb.Details, error) {
	details, detailsError := syntheticMatchedDetails(candidate, locale)
	if detailsError != nil {
		return tmdb.Details{}, detailsError
	}
	identifier, identifierError := netflix.NewIMDbTitleID("tt0133093")
	details.IMDbID = identifier
	return details, identifierError
}

func TestIMDbContractRejectsEarlierTMDBGenerationOnRestart(testContext *testing.T) {
	fixture := newWorkspaceFixture(testContext)
	client := newLifecycleMetadataClient(false)
	workspace := fixture.openWithClient(testContext, client, workspaceOptions{now: fixture.clock, entropy: testEntropy(0xb1)})
	local := importLocalFixture(testContext, workspace, syntheticViewingCSV)
	enriched, createError := workspace.CreateTMDBGeneration(context.Background(), local.ID,
		mustEnrichmentLocale(testContext), enrichment.AuthorizeTMDBTitleQueries())
	if createError != nil {
		testContext.Fatal(createError)
	}
	waitForGenerationState(testContext, workspace, enriched.ID, GenerationStateReady)
	if closeError := workspace.Close(); closeError != nil {
		testContext.Fatal(closeError)
	}
	encoded, readError := os.ReadFile(fixture.stateFile.Path())
	if readError != nil {
		testContext.Fatal(readError)
	}
	stale := bytes.ReplaceAll(encoded, []byte(tmdb.ClientIdentity), []byte("tmdb-v3-bearer-client-v1"))
	if bytes.Equal(stale, encoded) {
		testContext.Fatal("fixture did not change the client identity")
	}
	if writeError := os.WriteFile(fixture.stateFile.Path(), stale, 0o600); writeError != nil {
		testContext.Fatal(writeError)
	}
	reopened, openError := Open(fixture.root, fixture.stateFile, fixture.leaseFile, fixture.cacheFile, client)
	if reopened != nil {
		reopened.Close()
	}
	if errorCode(openError) != ErrorInvalidPersistence {
		testContext.Fatalf("earlier TMDB generation accepted: %v", openError)
	}
	unchanged, readError := os.ReadFile(fixture.stateFile.Path())
	if readError != nil || !bytes.Equal(unchanged, stale) {
		testContext.Fatal("stale generation was silently rewritten")
	}
}
