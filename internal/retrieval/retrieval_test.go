package retrieval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/domain"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/store"
)

type semanticFixtureEmbedder struct {
	Inputs []string
}

type failingFixtureEmbedder struct{}

func TestDefaultSemanticCutoffUsesFullArchiveCalibration(testContext *testing.T) {
	// The Nomic v1.5 full-archive sweep showed unrelated background similarities
	// extending past 0.60. Keep the executable default at the first high-precision
	// cutoff that still admits useful semantic-only candidates for topic search.
	if DefaultMinSemanticScore != 0.65 {
		testContext.Fatalf("default semantic cutoff lost its full-archive calibration: %.2f", DefaultMinSemanticScore)
	}
}

func TestLexicalFallbackRequiresEveryNaturalQueryTerm(testContext *testing.T) {
	query := buildFTSQuery("Japanese animated series Japanese")
	if query != `"japanese" AND "animated" AND "series"` {
		testContext.Fatalf("natural-query FTS fallback became overly broad: %s", query)
	}
}

func (*failingFixtureEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	return nil, errors.New("No models loaded")
}

func (embedder *semanticFixtureEmbedder) Embed(_ context.Context, inputs []string) ([][]float32, error) {
	embedder.Inputs = append(embedder.Inputs, inputs...)
	vectors := make([][]float32, len(inputs))
	for inputIndex, input := range inputs {
		normalized := strings.ToLower(input)
		switch {
		case strings.Contains(normalized, "anime"),
			strings.Contains(normalized, "cowboy bebop"),
			strings.Contains(normalized, "trigun"),
			strings.Contains(normalized, "japanese animated"):
			vectors[inputIndex] = []float32{1, 0, 0}
		case strings.Contains(normalized, "garden"), strings.Contains(normalized, "tomato"):
			vectors[inputIndex] = []float32{0, 1, 0}
		default:
			vectors[inputIndex] = []float32{0, 0, 1}
		}
	}
	return vectors, nil
}

func TestScenarioBuildAllVisibleBranchesAndFindAnimeConversations(testContext *testing.T) {
	// Given a corpus with a semantic-only anime conversation, an explicit anime
	// conversation, an alternate answer branch, and an unrelated conversation.
	openedStore := openRetrievalFixture(testContext)
	defer openedStore.Close()
	seedRetrievalFixture(testContext, openedStore)

	documents, documentError := BuildDocuments(context.Background(), openedStore)
	if documentError != nil {
		testContext.Fatalf("build retrieval documents: %v", documentError)
	}
	if len(documents) != 7 {
		testContext.Fatalf("expected seven visible branch documents, received %d", len(documents))
	}
	anchors := make(map[string]domain.SearchDocument, len(documents))
	for _, document := range documents {
		anchors[document.AnchorMessageID] = document
		if document.ContentType == "reasoning_recap" || strings.Contains(document.Text, "private chain") {
			testContext.Fatalf("reasoning content entered search document: %+v", document)
		}
	}
	for _, branchAnchor := range []string{"anime-answer-a", "anime-answer-b"} {
		document, exists := anchors[branchAnchor]
		if !exists {
			testContext.Fatalf("alternate branch %s was not indexed", branchAnchor)
		}
		if !strings.Contains(document.Text, "Space western recommendation") {
			testContext.Fatalf("branch document lacks preceding visible context: %s", document.Text)
		}
	}

	// When the resumable local retrieval index is built.
	documentEmbedder := &semanticFixtureEmbedder{}
	indexService := IndexService{Store: openedStore, Embedder: documentEmbedder}
	indexConfig, embeddedCount, indexError := indexService.Run(context.Background(), IndexOptions{
		Name:           DefaultIndexName,
		Provider:       "fixture",
		Model:          "fixture-model",
		Dimensions:     3,
		BaseURL:        "http://127.0.0.1:1234/v1",
		DocumentPrefix: DefaultDocumentPrefix,
		QueryPrefix:    DefaultQueryPrefix,
		BatchSize:      2,
	})
	if indexError != nil {
		testContext.Fatalf("build retrieval index: %v", indexError)
	}
	if indexConfig.Status != "ready" || embeddedCount != 7 {
		testContext.Fatalf("unexpected completed index: config=%+v embedded=%d", indexConfig, embeddedCount)
	}
	summaries, summaryError := openedStore.ListSearchIndexSummaries(context.Background())
	if summaryError != nil {
		testContext.Fatalf("summarize completed retrieval index: %v", summaryError)
	}
	if len(summaries) != 1 || summaries[0].DocumentCount != 7 || summaries[0].EligibleCount != 7 ||
		summaries[0].CoveredConversations != 3 || summaries[0].EligibleConversations != 3 {
		testContext.Fatalf("status does not prove full document and conversation coverage: %+v", summaries)
	}

	// And the user performs a hybrid topic search.
	queryEmbedder := &semanticFixtureEmbedder{}
	engine := Engine{Store: openedStore, QueryEmbedder: queryEmbedder}
	results, searchError := engine.Search(context.Background(), SearchOptions{
		IndexID:          indexConfig.ID,
		Query:            "anime",
		Mode:             SearchModeHybrid,
		Limit:            10,
		MinSemanticScore: 0.5,
		IncludeArchived:  true,
		Excerpts:         3,
	})
	if searchError != nil {
		testContext.Fatalf("search anime conversations: %v", searchError)
	}

	// Then both semantic and literal conversations are returned once, with
	// supporting excerpts, while the unrelated gardening conversation is absent.
	resultByConversation := make(map[string]domain.ConversationSearchResult, len(results))
	for _, result := range results {
		resultByConversation[result.ConversationID] = result
	}
	for _, expectedConversation := range []string{"anime-semantic", "anime-literal"} {
		result, exists := resultByConversation[expectedConversation]
		if !exists {
			testContext.Fatalf("missing expected conversation %s from %+v", expectedConversation, results)
		}
		if len(result.Excerpts) == 0 {
			testContext.Fatalf("conversation %s lacks supporting excerpts", expectedConversation)
		}
	}
	if _, exists := resultByConversation["gardening"]; exists {
		testContext.Fatalf("unrelated gardening conversation was returned: %+v", results)
	}

	// And archive and date filters apply consistently before aggregation.
	lexicalResults, searchError := engine.Search(context.Background(), SearchOptions{
		IndexID:         indexConfig.ID,
		Query:           "anime",
		Mode:            SearchModeLexical,
		Limit:           10,
		IncludeArchived: false,
	})
	if searchError != nil {
		testContext.Fatalf("search with archive filter: %v", searchError)
	}
	if len(lexicalResults) != 0 {
		testContext.Fatalf("archived literal conversation bypassed filter: %+v", lexicalResults)
	}
	dateFilteredResults, searchError := engine.Search(context.Background(), SearchOptions{
		IndexID:          indexConfig.ID,
		Query:            "anime",
		Mode:             SearchModeHybrid,
		Limit:            10,
		MinSemanticScore: 0.5,
		IncludeArchived:  true,
		SinceMillis:      time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC).UnixMilli(),
	})
	if searchError != nil {
		testContext.Fatalf("search with date filter: %v", searchError)
	}
	if len(dateFilteredResults) != 0 {
		testContext.Fatalf("out-of-range conversations bypassed date filter: %+v", dateFilteredResults)
	}

	// And repeating the same query uses the persisted query-vector cache.
	cachedQueryEmbedder := &semanticFixtureEmbedder{}
	cachedEngine := Engine{Store: openedStore, QueryEmbedder: cachedQueryEmbedder}
	if _, searchError := cachedEngine.Search(context.Background(), SearchOptions{
		IndexID:          indexConfig.ID,
		Query:            "anime",
		Mode:             SearchModeSemantic,
		Limit:            10,
		MinSemanticScore: 0.5,
		IncludeArchived:  true,
	}); searchError != nil {
		testContext.Fatalf("repeat cached semantic search: %v", searchError)
	}
	if len(cachedQueryEmbedder.Inputs) != 0 {
		testContext.Fatalf("cached query unexpectedly called embedder with %v", cachedQueryEmbedder.Inputs)
	}
}

func TestScenarioInterruptedIndexResumesAndRefreshesOnlyChangedDocuments(testContext *testing.T) {
	// Given a retrieval corpus and an index run limited to two documents.
	openedStore := openRetrievalFixture(testContext)
	defer openedStore.Close()
	seedRetrievalFixture(testContext, openedStore)
	embedder := &semanticFixtureEmbedder{}
	service := IndexService{Store: openedStore, Embedder: embedder}
	options := IndexOptions{
		Name:             DefaultIndexName,
		Provider:         "fixture",
		Model:            "fixture-model",
		Dimensions:       3,
		BaseURL:          "http://127.0.0.1:1234/v1",
		DocumentPrefix:   DefaultDocumentPrefix,
		QueryPrefix:      DefaultQueryPrefix,
		BatchSize:        2,
		MaximumDocuments: 2,
	}
	config, embeddedCount, indexError := service.Run(context.Background(), options)
	if indexError != nil {
		testContext.Fatalf("run limited index build: %v", indexError)
	}
	if config.Status != "building" || embeddedCount != 2 {
		testContext.Fatalf("limited index must remain building: config=%+v embedded=%d", config, embeddedCount)
	}

	// When the same index is resumed without a limit.
	options.MaximumDocuments = 0
	config, embeddedCount, indexError = service.Run(context.Background(), options)
	if indexError != nil {
		testContext.Fatalf("resume index build: %v", indexError)
	}
	if config.Status != "ready" || embeddedCount != 5 {
		testContext.Fatalf("resume should finish remaining documents: config=%+v embedded=%d", config, embeddedCount)
	}

	// And one source message changes before a refresh.
	if _, updateError := openedStore.Database().Exec(
		`UPDATE messages SET original_text='Cowboy Bebop and Samurai Champloo are Japanese animated classics', normalized_text='cowboy bebop and samurai champloo are japanese animated classics' WHERE message_id='anime-answer-a'`,
	); updateError != nil {
		testContext.Fatalf("update source message: %v", updateError)
	}
	embedder.Inputs = nil
	config, embeddedCount, indexError = service.Run(context.Background(), options)
	if indexError != nil {
		testContext.Fatalf("refresh completed index: %v", indexError)
	}
	if config.Status != "ready" || embeddedCount != 1 {
		testContext.Fatalf("only the changed document should refresh: config=%+v embedded=%d", config, embeddedCount)
	}

	// And a source that becomes ineligible is removed from both vector metadata
	// and full-text search without re-embedding unrelated documents.
	if _, updateError := openedStore.Database().Exec(
		`UPDATE messages SET content_type='reasoning_recap' WHERE message_id='anime-answer-b'`,
	); updateError != nil {
		testContext.Fatalf("make source message ineligible: %v", updateError)
	}
	config, embeddedCount, indexError = service.Run(context.Background(), options)
	if indexError != nil {
		testContext.Fatalf("reconcile removed document: %v", indexError)
	}
	documentCount, countError := openedStore.CountSearchDocuments(context.Background(), config.ID)
	if countError != nil {
		testContext.Fatalf("count reconciled documents: %v", countError)
	}
	if config.Status != "ready" || embeddedCount != 0 || documentCount != 6 {
		testContext.Fatalf("ineligible document was not cleanly removed: config=%+v embedded=%d documents=%d", config, embeddedCount, documentCount)
	}

	// And completing any later import invalidates the ready search lifecycle
	// until an explicit reconciliation proves the corpus current again.
	importID, importError := openedStore.BeginImport(context.Background(), "later-fixture", "later-hash", domain.ParserVersion)
	if importError != nil {
		testContext.Fatalf("begin later import: %v", importError)
	}
	if completeError := openedStore.CompleteImport(context.Background(), importID, 0, 0, 0); completeError != nil {
		testContext.Fatalf("complete later import: %v", completeError)
	}
	invalidatedConfig, configError := openedStore.SearchIndexByID(context.Background(), config.ID)
	if configError != nil {
		testContext.Fatalf("load invalidated index: %v", configError)
	}
	if invalidatedConfig.Status != "building" {
		testContext.Fatalf("completed import did not invalidate search index: %+v", invalidatedConfig)
	}
	config, embeddedCount, indexError = service.Run(context.Background(), options)
	if indexError != nil {
		testContext.Fatalf("reconcile invalidated index: %v", indexError)
	}
	if config.Status != "ready" || embeddedCount != 0 {
		testContext.Fatalf("no-change reconciliation should restore ready without embedding: config=%+v embedded=%d", config, embeddedCount)
	}
}

func TestScenarioModelPreflightFailsBeforeCreatingIndexState(testContext *testing.T) {
	// Given an imported corpus but an unavailable local embedding model.
	openedStore := openRetrievalFixture(testContext)
	defer openedStore.Close()
	seedRetrievalFixture(testContext, openedStore)
	service := IndexService{Store: openedStore, Embedder: &failingFixtureEmbedder{}}

	// When index construction performs its synthetic readiness embedding.
	_, _, indexError := service.Run(context.Background(), IndexOptions{
		Name:           DefaultIndexName,
		Provider:       "fixture",
		Model:          "fixture-model",
		Dimensions:     3,
		BaseURL:        "http://127.0.0.1:1234/v1",
		DocumentPrefix: DefaultDocumentPrefix,
		QueryPrefix:    DefaultQueryPrefix,
	})
	if indexError == nil || !strings.Contains(indexError.Error(), "preflight") {
		testContext.Fatalf("expected actionable preflight error, received %v", indexError)
	}

	// Then no misleading building configuration exists.
	var indexCount int
	if queryError := openedStore.Database().QueryRow(`SELECT COUNT(*) FROM search_indexes`).Scan(&indexCount); queryError != nil {
		testContext.Fatalf("count search indexes: %v", queryError)
	}
	if indexCount != 0 {
		testContext.Fatalf("preflight failure created %d search index rows", indexCount)
	}
}

func TestScenarioExplicitRebuildReplacesAnIncompatibleSearchIndex(testContext *testing.T) {
	// Given a ready search index built with one model identity.
	openedStore := openRetrievalFixture(testContext)
	defer openedStore.Close()
	seedRetrievalFixture(testContext, openedStore)
	service := IndexService{Store: openedStore, Embedder: &semanticFixtureEmbedder{}}
	options := IndexOptions{
		Name:           DefaultIndexName,
		Provider:       "fixture",
		Model:          "fixture-model-v1",
		Dimensions:     3,
		BaseURL:        "http://127.0.0.1:1234/v1",
		DocumentPrefix: DefaultDocumentPrefix,
		QueryPrefix:    DefaultQueryPrefix,
	}
	oldConfig, _, indexError := service.Run(context.Background(), options)
	if indexError != nil {
		testContext.Fatalf("build original retrieval index: %v", indexError)
	}
	oldVectorPath := openedStore.ResolveSearchVectorPath(oldConfig)
	if _, statError := os.Stat(oldVectorPath); statError != nil {
		testContext.Fatalf("stat original vector file: %v", statError)
	}
	oldEngine := Engine{Store: openedStore, QueryEmbedder: &semanticFixtureEmbedder{}}
	if _, searchError := oldEngine.Search(context.Background(), SearchOptions{
		IndexID:          oldConfig.ID,
		Query:            "anime",
		Mode:             SearchModeSemantic,
		Limit:            10,
		MinSemanticScore: 0.5,
		IncludeArchived:  true,
	}); searchError != nil {
		testContext.Fatalf("seed original query cache: %v", searchError)
	}

	// When the requested identity changes, a normal run refuses to mix vectors.
	options.Model = "fixture-model-v2"
	if _, _, indexError := service.Run(context.Background(), options); indexError == nil || !strings.Contains(indexError.Error(), "--rebuild") {
		testContext.Fatalf("expected actionable identity mismatch, received %v", indexError)
	}

	// And a failed replacement preflight preserves the working index and vectors.
	options.Rebuild = true
	service.Embedder = &failingFixtureEmbedder{}
	if _, _, indexError := service.Run(context.Background(), options); indexError == nil || !strings.Contains(indexError.Error(), "preflight") {
		testContext.Fatalf("expected replacement preflight failure, received %v", indexError)
	}
	if preservedConfig, configError := openedStore.SearchIndexByID(context.Background(), oldConfig.ID); configError != nil || preservedConfig.Status != "ready" {
		testContext.Fatalf("failed preflight destroyed the working index: config=%+v error=%v", preservedConfig, configError)
	}
	if _, statError := os.Stat(oldVectorPath); statError != nil {
		testContext.Fatalf("failed preflight removed the working vector file: %v", statError)
	}

	// When an explicit rebuild is requested after the new model passes preflight.
	service.Embedder = &semanticFixtureEmbedder{}
	newConfig, embeddedCount, indexError := service.Run(context.Background(), options)
	if indexError != nil {
		testContext.Fatalf("rebuild retrieval index: %v", indexError)
	}

	// Then the old configuration and vector file are gone and the replacement is complete.
	if newConfig.ID == oldConfig.ID || newConfig.Model != options.Model || newConfig.Status != "ready" || embeddedCount != 7 {
		testContext.Fatalf("unexpected rebuilt index: old=%+v new=%+v embedded=%d", oldConfig, newConfig, embeddedCount)
	}
	if _, statError := os.Stat(oldVectorPath); !os.IsNotExist(statError) {
		testContext.Fatalf("old vector file still exists after rebuild: %v", statError)
	}
	var indexCount int
	if queryError := openedStore.Database().QueryRow(`SELECT COUNT(*) FROM search_indexes`).Scan(&indexCount); queryError != nil {
		testContext.Fatalf("count rebuilt search indexes: %v", queryError)
	}
	if indexCount != 1 {
		testContext.Fatalf("expected one canonical search index after rebuild, received %d", indexCount)
	}
	var oldFTSRows int
	if queryError := openedStore.Database().QueryRow(
		`SELECT COUNT(*) FROM search_documents_fts WHERE search_index_id=?`, oldConfig.ID,
	).Scan(&oldFTSRows); queryError != nil {
		testContext.Fatalf("count old FTS rows after rebuild: %v", queryError)
	}
	var oldQueryCacheRows int
	if queryError := openedStore.Database().QueryRow(
		`SELECT COUNT(*) FROM query_embedding_cache WHERE search_index_id=?`, oldConfig.ID,
	).Scan(&oldQueryCacheRows); queryError != nil {
		testContext.Fatalf("count old query cache rows after rebuild: %v", queryError)
	}
	if oldFTSRows != 0 || oldQueryCacheRows != 0 {
		testContext.Fatalf("rebuild left old index state: fts=%d query_cache=%d", oldFTSRows, oldQueryCacheRows)
	}
}

func openRetrievalFixture(testContext *testing.T) *store.Store {
	testContext.Helper()
	openedStore, openError := store.Open(filepath.Join(testContext.TempDir(), "archive.db"))
	if openError != nil {
		testContext.Fatalf("open retrieval fixture: %v", openError)
	}
	return openedStore
}

func seedRetrievalFixture(testContext *testing.T, openedStore *store.Store) {
	testContext.Helper()
	contextValue := context.Background()
	importID, importError := openedStore.BeginImport(contextValue, "fixture", "fixture-hash", domain.ParserVersion)
	if importError != nil {
		testContext.Fatalf("begin fixture import: %v", importError)
	}
	conversations := []domain.Conversation{
		{
			ID:         "anime-semantic",
			Title:      "Space western recommendation",
			SourceFile: "fixture.json",
			Messages: []domain.Message{
				fixtureMessage("anime-question", "anime-semantic", "anime-question-node", "", "user", "text", "Recommend a space western series"),
				fixtureMessage("anime-answer-a", "anime-semantic", "anime-answer-a-node", "anime-question-node", "assistant", "text", "Cowboy Bebop is a landmark Japanese animated series"),
				fixtureMessage("anime-answer-b", "anime-semantic", "anime-answer-b-node", "anime-question-node", "assistant", "multimodal_text", "Trigun is another Japanese animated space western"),
				fixtureMessage("anime-reasoning", "anime-semantic", "anime-reasoning-node", "anime-answer-a-node", "assistant", "reasoning_recap", "private chain should not be searchable"),
			},
			Edges: []domain.MessageEdge{
				{ConversationID: "anime-semantic", ParentNodeID: "anime-question-node", ChildNodeID: "anime-answer-a-node"},
				{ConversationID: "anime-semantic", ParentNodeID: "anime-question-node", ChildNodeID: "anime-answer-b-node"},
				{ConversationID: "anime-semantic", ParentNodeID: "anime-answer-a-node", ChildNodeID: "anime-reasoning-node"},
			},
		},
		{
			ID:         "anime-literal",
			Title:      "Weekend viewing",
			IsArchived: boolPointer(true),
			SourceFile: "fixture.json",
			Messages: []domain.Message{
				fixtureMessage("literal-question", "anime-literal", "literal-question-node", "", "user", "text", "What anime should I watch this weekend?"),
				fixtureMessage("literal-answer", "anime-literal", "literal-answer-node", "literal-question-node", "assistant", "text", "Try a short science fiction series"),
			},
			Edges: []domain.MessageEdge{{ConversationID: "anime-literal", ParentNodeID: "literal-question-node", ChildNodeID: "literal-answer-node"}},
		},
		{
			ID:         "gardening",
			Title:      "Vegetable garden",
			SourceFile: "fixture.json",
			Messages: []domain.Message{
				fixtureMessage("garden-question", "gardening", "garden-question-node", "", "user", "text", "How do I grow tomatoes?"),
				fixtureMessage("garden-answer", "gardening", "garden-answer-node", "garden-question-node", "assistant", "text", "Give the garden full sun and steady water"),
			},
			Edges: []domain.MessageEdge{{ConversationID: "gardening", ParentNodeID: "garden-question-node", ChildNodeID: "garden-answer-node"}},
		},
	}
	var messageCount int64
	for _, conversation := range conversations {
		importedMessages, _, conversationError := openedStore.ImportConversation(contextValue, importID, conversation)
		if conversationError != nil {
			testContext.Fatalf("import fixture conversation %s: %v", conversation.ID, conversationError)
		}
		messageCount += importedMessages
	}
	if completeError := openedStore.CompleteImport(contextValue, importID, int64(len(conversations)), messageCount, 0); completeError != nil {
		testContext.Fatalf("complete fixture import: %v", completeError)
	}
}

func fixtureMessage(id string, conversationID string, nodeID string, parentNodeID string, role string, contentType string, text string) domain.Message {
	createdAt := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	return domain.Message{
		ID:              id,
		SourceMessageID: id,
		ConversationID:  conversationID,
		ParentNodeID:    parentNodeID,
		Role:            role,
		CreatedAt:       &createdAt,
		ContentType:     contentType,
		OriginalText:    text,
		NormalizedText:  strings.ToLower(text),
		ContentHash:     id + "-hash",
		SourceFile:      "fixture.json",
		SourceNodeID:    nodeID,
	}
}

func boolPointer(value bool) *bool {
	return &value
}
