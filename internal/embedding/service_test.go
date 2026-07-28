package embedding

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/ingest"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/store"
)

type recordingEmbedder struct {
	Inputs []string
}

func (embedder *recordingEmbedder) Embed(_ context.Context, inputs []string) ([][]float32, error) {
	embedder.Inputs = append(embedder.Inputs, inputs...)
	vectors := make([][]float32, len(inputs))
	for inputIndex := range inputs {
		vectors[inputIndex] = []float32{1, 0, 0}
	}
	return vectors, nil
}

func TestServiceMarksCompleteConfigurationReadyAndRefreshesOnlyStaleText(testContext *testing.T) {
	databasePath := filepath.Join(testContext.TempDir(), "archive.db")
	openedStore, openError := store.Open(databasePath)
	if openError != nil {
		testContext.Fatalf("open store: %v", openError)
	}
	defer openedStore.Close()

	importer := ingest.Importer{Store: openedStore}
	sourcePath := filepath.Join("..", "..", "testdata", "synthetic-openai-export.zip")
	if _, importError := importer.Import(context.Background(), sourcePath, false); importError != nil {
		testContext.Fatalf("import fixture: %v", importError)
	}

	recorder := &recordingEmbedder{}
	service := Service{Store: openedStore, Embedder: recorder}
	options := ServiceOptions{
		Provider:    "lmstudio",
		Model:       "download-your-data-embedding",
		Dimensions:  3,
		BaseURL:     "http://127.0.0.1:1234/v1",
		InputPrefix: "classification: ",
		BatchSize:   2,
	}
	config, embeddedCount, embedError := service.Run(context.Background(), options)
	if embedError != nil {
		testContext.Fatalf("embed fixture: %v", embedError)
	}
	if embeddedCount != 5 || len(recorder.Inputs) != 5 || config.Status != "ready" {
		testContext.Fatalf("unexpected initial embedding result: count=%d inputs=%d config=%+v", embeddedCount, len(recorder.Inputs), config)
	}
	storedConfig, configError := openedStore.EmbeddingConfigByID(context.Background(), config.ID)
	if configError != nil {
		testContext.Fatalf("load stored configuration: %v", configError)
	}
	if storedConfig.Status != "ready" || storedConfig.InputPrefix != "classification: " || storedConfig.CompletedAtMillis == nil {
		testContext.Fatalf("unexpected stored configuration: %+v", storedConfig)
	}

	var messageID string
	if queryError := openedStore.Database().QueryRow(
		`SELECT message_id FROM messages WHERE role = 'user' ORDER BY message_id LIMIT 1`,
	).Scan(&messageID); queryError != nil {
		testContext.Fatalf("select message to change: %v", queryError)
	}
	if _, updateError := openedStore.Database().Exec(
		`UPDATE messages SET original_text = original_text || ' changed' WHERE message_id = ?`,
		messageID,
	); updateError != nil {
		testContext.Fatalf("change message text: %v", updateError)
	}

	recorder.Inputs = nil
	options.RefreshStale = true
	refreshedConfig, refreshedCount, refreshError := service.Run(context.Background(), options)
	if refreshError != nil {
		testContext.Fatalf("refresh stale embeddings: %v", refreshError)
	}
	if refreshedCount != 1 || len(recorder.Inputs) != 1 || refreshedConfig.Status != "ready" {
		testContext.Fatalf(
			"expected one stale embedding refresh, received count=%d inputs=%d config=%+v",
			refreshedCount,
			len(recorder.Inputs),
			refreshedConfig,
		)
	}
}

func TestServiceKeepsLimitedRunBuildingUntilResumeCompletes(testContext *testing.T) {
	databasePath := filepath.Join(testContext.TempDir(), "archive.db")
	openedStore, openError := store.Open(databasePath)
	if openError != nil {
		testContext.Fatalf("open store: %v", openError)
	}
	defer openedStore.Close()

	importer := ingest.Importer{Store: openedStore}
	sourcePath := filepath.Join("..", "..", "testdata", "synthetic-openai-export.zip")
	if _, importError := importer.Import(context.Background(), sourcePath, false); importError != nil {
		testContext.Fatalf("import fixture: %v", importError)
	}

	recorder := &recordingEmbedder{}
	service := Service{Store: openedStore, Embedder: recorder}
	options := ServiceOptions{
		Provider:        "lmstudio",
		Model:           "download-your-data-embedding",
		Dimensions:      3,
		BaseURL:         "http://127.0.0.1:1234/v1",
		InputPrefix:     "classification: ",
		BatchSize:       2,
		MaximumMessages: 2,
	}
	config, embeddedCount, embedError := service.Run(context.Background(), options)
	if embedError != nil {
		testContext.Fatalf("run limited embedding pass: %v", embedError)
	}
	if embeddedCount != 2 || config.Status != "building" {
		testContext.Fatalf("limited run should remain building: count=%d config=%+v", embeddedCount, config)
	}

	options.MaximumMessages = 0
	config, embeddedCount, embedError = service.Run(context.Background(), options)
	if embedError != nil {
		testContext.Fatalf("resume embedding pass: %v", embedError)
	}
	if embeddedCount != 3 || config.Status != "ready" {
		testContext.Fatalf("resumed full pass should become ready: count=%d config=%+v", embeddedCount, config)
	}
}
