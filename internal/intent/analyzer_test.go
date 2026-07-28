package intent

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/ingest"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/store"
)

func TestAnalyzerLexicalMode(testContext *testing.T) {
	databasePath := filepath.Join(testContext.TempDir(), "archive.db")
	openedStore, openError := store.Open(databasePath)
	if openError != nil {
		testContext.Fatalf("open store: %v", openError)
	}
	defer openedStore.Close()

	sourcePath := filepath.Join("..", "..", "testdata", "synthetic-openai-export.zip")
	importer := ingest.Importer{Store: openedStore}
	if _, importError := importer.Import(context.Background(), sourcePath, false); importError != nil {
		testContext.Fatalf("import fixture: %v", importError)
	}

	analyzer := Analyzer{Store: openedStore}
	analysisOutput, analysisError := analyzer.Analyze(context.Background(), AnalyzeOptions{
		Config:          DefaultDefinitionConfig(),
		Since:           time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		Until:           time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC),
		IncludeArchived: true,
		Semantic:        false,
	})
	if analysisError != nil {
		testContext.Fatalf("analyze definitions: %v", analysisError)
	}
	if len(analysisOutput.Results) != 3 {
		testContext.Fatalf("expected 3 direct definition requests, received %d", len(analysisOutput.Results))
	}
	if len(analysisOutput.Review) != 2 {
		testContext.Fatalf("expected 2 broad questions in review, received %d", len(analysisOutput.Review))
	}
}
