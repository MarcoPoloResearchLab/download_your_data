package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/domain"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/embedding"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/exportformat"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/inference"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/ingest"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/intent"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/report"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/retrieval"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/store"
)

const version = "0.2.0"

func main() {
	if runError := run(os.Args[1:]); runError != nil {
		fmt.Fprintln(os.Stderr, "error:", runError)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) == 0 {
		printUsage()
		return nil
	}
	command := arguments[0]
	commandArguments := arguments[1:]
	switch command {
	case "inspect":
		return runInspect(commandArguments)
	case "import":
		return runImport(commandArguments)
	case "status":
		return runStatus(commandArguments)
	case "embed":
		return runEmbed(commandArguments)
	case "index":
		return runIndex(commandArguments)
	case "search":
		return runSearch(commandArguments)
	case "definitions":
		return runDefinitions(commandArguments)
	case "version":
		fmt.Println(version)
		return nil
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

func runInspect(arguments []string) error {
	flagSet := flag.NewFlagSet("inspect", flag.ContinueOnError)
	if parseError := flagSet.Parse(arguments); parseError != nil {
		return parseError
	}
	if flagSet.NArg() != 1 {
		return fmt.Errorf(
			"usage: %s inspect <openai-export.zip|conversations.json|directory>",
			product.ArchiveCommandName,
		)
	}
	collection, discoverError := exportformat.DiscoverSources(flagSet.Arg(0))
	if discoverError != nil {
		return discoverError
	}
	defer collection.Close()

	inspection, inspectError := exportformat.InspectSources(collection)
	if inspectError != nil {
		return inspectError
	}
	fmt.Println("Conversation files:")
	for _, sourceFile := range inspection.SourceFiles {
		fmt.Println("  ", sourceFile)
	}
	fmt.Printf("Conversations: %d\n", inspection.Conversations)
	fmt.Printf("Messages: %d\n", inspection.Messages)
	fmt.Printf("Messages with OpenAI source message ID: %d\n", inspection.MessagesWithSourceID)
	fmt.Printf("Messages without OpenAI source message ID: %d\n", inspection.MessagesWithoutSourceID)
	fmt.Printf("Unique OpenAI source message IDs: %d\n", inspection.UniqueSourceMessageIDs)
	fmt.Printf("Repeated OpenAI source message IDs: %d\n", inspection.RepeatedSourceMessageIDs)
	fmt.Printf("Repeated source-message occurrences: %d\n", inspection.RepeatedSourceMessageOccurrences)
	fmt.Printf("Archived conversations: %d\n", inspection.ArchivedConversations)
	fmt.Printf("Conversations with known archive status: %d\n", inspection.ArchiveStatusKnown)
	fmt.Println("Roles:")
	for _, role := range exportformat.SortedCountKeys(inspection.Roles) {
		fmt.Printf("  %s: %d\n", role, inspection.Roles[role])
	}
	fmt.Println("Content types:")
	for _, contentType := range exportformat.SortedCountKeys(inspection.ContentTypes) {
		fmt.Printf("  %s: %d\n", contentType, inspection.ContentTypes[contentType])
	}
	return nil
}

func runImport(arguments []string) error {
	flagSet := flag.NewFlagSet("import", flag.ContinueOnError)
	databasePath := flagSet.String("db", product.DefaultArchiveDatabasePath, "SQLite database path")
	forceImport := flagSet.Bool("force", false, "import even when the same export hash was already completed")
	if parseError := flagSet.Parse(arguments); parseError != nil {
		return parseError
	}
	if flagSet.NArg() != 1 {
		return fmt.Errorf(
			"usage: %s import [--db %s] <openai-export.zip>",
			product.ArchiveCommandName,
			product.DefaultArchiveDatabasePath,
		)
	}

	openedStore, openError := store.Open(*databasePath)
	if openError != nil {
		return openError
	}
	defer openedStore.Close()

	importer := ingest.Importer{
		Store: openedStore,
		Progress: func(progress ingest.Progress) {
			if progress.ConversationsSeen%100 == 0 {
				fmt.Printf(
					"Imported %d conversations and %d messages from %s; warnings: %d\n",
					progress.ConversationsSeen,
					progress.MessagesSeen,
					progress.SourceFile,
					progress.WarningsCount,
				)
			}
		},
	}
	result, importError := importer.Import(context.Background(), flagSet.Arg(0), *forceImport)
	if importError != nil {
		return importError
	}
	if result.Skipped {
		fmt.Println("This export was already imported. Use --force to import it again.")
		return nil
	}
	fmt.Printf("Import complete. Conversations: %d; messages: %d; warnings: %d\n", result.ConversationsSeen, result.MessagesSeen, result.WarningsCount)
	fmt.Printf("Database: %s\n", openedStore.DatabasePath())
	return nil
}

func runStatus(arguments []string) error {
	flagSet := flag.NewFlagSet("status", flag.ContinueOnError)
	databasePath := flagSet.String("db", product.DefaultArchiveDatabasePath, "SQLite database path")
	if parseError := flagSet.Parse(arguments); parseError != nil {
		return parseError
	}
	openedStore, openError := store.Open(*databasePath)
	if openError != nil {
		return openError
	}
	defer openedStore.Close()
	statistics, statsError := openedStore.Stats(context.Background())
	if statsError != nil {
		return statsError
	}
	fmt.Printf("Database: %s\n", openedStore.DatabasePath())
	fmt.Printf("Completed imports: %d\n", statistics.Imports)
	fmt.Printf("Latest import message occurrences: %d\n", statistics.LatestImportMessages)
	fmt.Printf("Conversations: %d\n", statistics.Conversations)
	fmt.Printf("Archived conversations: %d\n", statistics.ArchivedConversations)
	fmt.Printf("Stored message occurrences: %d\n", statistics.Messages)
	fmt.Printf("User messages: %d\n", statistics.UserMessages)
	fmt.Printf("Assistant messages: %d\n", statistics.AssistantMessages)
	fmt.Printf("Messages with OpenAI source message ID: %d\n", statistics.SourceMessageIDs)
	fmt.Printf("Unique OpenAI source message IDs: %d\n", statistics.UniqueSourceMessageIDs)
	fmt.Printf("Repeated source-message occurrences preserved: %d\n", statistics.RepeatedSourceMessages)
	fmt.Printf("Messages without OpenAI source message ID: %d\n", statistics.MessagesWithoutSourceID)
	fmt.Printf("Embedding configurations: %d\n", statistics.EmbeddingConfigurations)
	fmt.Printf("Embeddings: %d\n", statistics.Embeddings)
	configurationSummaries, summaryError := openedStore.ListEmbeddingConfigSummaries(context.Background())
	if summaryError != nil {
		return summaryError
	}
	for _, summary := range configurationSummaries {
		fmt.Printf(
			"Definition classification configuration %d: status=%s provider=%s model=%s dimensions=%d embeddings=%d/%d prefix=%q base_url=%s\n",
			summary.Config.ID,
			summary.Config.Status,
			summary.Config.Provider,
			summary.Config.Model,
			summary.Config.Dimensions,
			summary.EmbeddingCount,
			summary.EligibleCount,
			summary.Config.InputPrefix,
			summary.Config.BaseURL,
		)
	}
	searchIndexSummaries, searchSummaryError := openedStore.ListSearchIndexSummaries(context.Background())
	if searchSummaryError != nil {
		return searchSummaryError
	}
	for _, summary := range searchIndexSummaries {
		fmt.Printf(
			"Conversation search index %d (%s): status=%s provider=%s model=%s dimensions=%d documents=%d/%d conversations=%d/%d document_prefix=%q query_prefix=%q base_url=%s\n",
			summary.Config.ID,
			summary.Config.Name,
			summary.Config.Status,
			summary.Config.Provider,
			summary.Config.Model,
			summary.Config.Dimensions,
			summary.DocumentCount,
			summary.EligibleCount,
			summary.CoveredConversations,
			summary.EligibleConversations,
			summary.Config.DocumentPrefix,
			summary.Config.QueryPrefix,
			summary.Config.BaseURL,
		)
	}
	return nil
}

func runEmbed(arguments []string) error {
	flagSet := flag.NewFlagSet("embed", flag.ContinueOnError)
	configuredBaseURL := inference.ConfiguredBaseURL(os.Getenv(inference.BaseURLEnvironment))
	databasePath := flagSet.String("db", product.DefaultArchiveDatabasePath, "SQLite database path")
	provider := flagSet.String("provider", inference.DefaultEmbeddingProvider, "embedding provider label")
	model := flagSet.String("model", inference.DefaultEmbeddingModel, "embedding model or local server alias")
	dimensions := flagSet.Int("dimensions", inference.DefaultEmbeddingDimensions, "embedding dimensions")
	baseURL := flagSet.String("base-url", configuredBaseURL, "OpenAI-compatible embedding API base URL")
	inputPrefix := flagSet.String("input-prefix", inference.DefaultEmbeddingInputPrefix, "task prefix prepended to every embedding input")
	apiKeyEnvironment := flagSet.String("api-key-env", "", "optional environment variable containing an API key")
	allowRemote := flagSet.Bool("allow-remote", false, "allow text to be sent to a non-loopback inference endpoint")
	batchSize := flagSet.Int("batch-size", 64, "messages per embedding request")
	maximumMessages := flagSet.Int("max-messages", 0, "maximum messages to embed in this run; zero means all")
	refreshStale := flagSet.Bool("refresh-stale", false, "re-embed messages whose prepared text changed")
	if parseError := flagSet.Parse(arguments); parseError != nil {
		return parseError
	}

	apiKey, apiKeyError := readOptionalAPIKey(*apiKeyEnvironment)
	if apiKeyError != nil {
		return apiKeyError
	}
	if boundaryError := validateInferenceBoundary("embedding", *baseURL, *allowRemote); boundaryError != nil {
		return boundaryError
	}
	openedStore, openError := store.Open(*databasePath)
	if openError != nil {
		return openError
	}
	defer openedStore.Close()

	fmt.Printf("Embedding provider: %s\n", *provider)
	fmt.Printf("API base URL: %s\n", *baseURL)
	printInferenceBoundary("Embedding", *baseURL)
	fmt.Println("Text sent to inference: user-message text and limited neighboring context")
	fmt.Println("Attachments sent to inference: no")

	embedder := &embedding.HTTPEmbedder{
		BaseURL:     *baseURL,
		APIKey:      apiKey,
		Model:       *model,
		Dimensions:  *dimensions,
		InputPrefix: *inputPrefix,
	}
	service := embedding.Service{
		Store:    openedStore,
		Embedder: embedder,
		Progress: func(progress embedding.ServiceProgress) {
			fmt.Printf("Embedded this run: %d; total for configuration: %d\n", progress.EmbeddedThisRun, progress.TotalEmbedded)
		},
	}
	config, embeddedCount, embeddingError := service.Run(context.Background(), embedding.ServiceOptions{
		Provider:        *provider,
		Model:           *model,
		Dimensions:      *dimensions,
		BaseURL:         *baseURL,
		InputPrefix:     *inputPrefix,
		BatchSize:       *batchSize,
		MaximumMessages: *maximumMessages,
		RefreshStale:    *refreshStale,
	})
	if embeddingError != nil {
		return embeddingError
	}
	fmt.Printf("Embedding complete. New embeddings: %d\n", embeddedCount)
	fmt.Printf("Configuration ID: %d; vector file: %s\n", config.ID, openedStore.ResolveVectorPath(config))
	return nil
}

func runIndex(arguments []string) error {
	if len(arguments) == 0 || arguments[0] != "build" {
		return fmt.Errorf("usage: %s index build [options]", product.ArchiveCommandName)
	}
	flagSet := flag.NewFlagSet("index build", flag.ContinueOnError)
	configuredBaseURL := inference.ConfiguredBaseURL(os.Getenv(inference.BaseURLEnvironment))
	databasePath := flagSet.String("db", product.DefaultArchiveDatabasePath, "SQLite database path")
	name := flagSet.String("name", retrieval.DefaultIndexName, "conversation search index name")
	provider := flagSet.String("provider", inference.DefaultEmbeddingProvider, "embedding provider label")
	model := flagSet.String("model", inference.DefaultEmbeddingModel, "embedding model or local server alias")
	dimensions := flagSet.Int("dimensions", inference.DefaultEmbeddingDimensions, "embedding dimensions")
	baseURL := flagSet.String("base-url", configuredBaseURL, "OpenAI-compatible embedding API base URL")
	documentPrefix := flagSet.String("document-prefix", retrieval.DefaultDocumentPrefix, "task prefix for indexed conversation documents")
	queryPrefix := flagSet.String("query-prefix", retrieval.DefaultQueryPrefix, "task prefix for search queries")
	apiKeyEnvironment := flagSet.String("api-key-env", "", "optional environment variable containing an API key")
	allowRemote := flagSet.Bool("allow-remote", false, "allow conversation text to be sent to a non-loopback inference endpoint")
	batchSize := flagSet.Int("batch-size", 64, "conversation documents per embedding request")
	maximumDocuments := flagSet.Int("max-documents", 0, "maximum documents to embed in this run; zero means all")
	rebuild := flagSet.Bool("rebuild", false, "replace the named search index and its vector file after model preflight succeeds")
	if parseError := flagSet.Parse(arguments[1:]); parseError != nil {
		return parseError
	}
	if *batchSize <= 0 {
		return fmt.Errorf("--batch-size must be positive")
	}
	if *maximumDocuments < 0 {
		return fmt.Errorf("--max-documents must not be negative")
	}
	if boundaryError := validateInferenceBoundary("conversation indexing", *baseURL, *allowRemote); boundaryError != nil {
		return boundaryError
	}
	apiKey, apiKeyError := readOptionalAPIKey(*apiKeyEnvironment)
	if apiKeyError != nil {
		return apiKeyError
	}
	openedStore, openError := store.Open(*databasePath)
	if openError != nil {
		return openError
	}
	defer openedStore.Close()

	fmt.Printf("Conversation search index: %s\n", *name)
	fmt.Printf("Embedding provider: %s\n", *provider)
	fmt.Printf("API base URL: %s\n", *baseURL)
	printInferenceBoundary("Conversation indexing", *baseURL)
	fmt.Println("Indexed roles: visible user and assistant text across every branch")
	fmt.Println("Excluded content: thoughts, reasoning recaps, empty text, attachments, images, and audio")

	documentEmbedder := &embedding.HTTPEmbedder{
		BaseURL:     *baseURL,
		APIKey:      apiKey,
		Model:       *model,
		Dimensions:  *dimensions,
		InputPrefix: *documentPrefix,
	}
	service := retrieval.IndexService{
		Store:    openedStore,
		Embedder: documentEmbedder,
		Progress: func(progress retrieval.IndexProgress) {
			fmt.Printf(
				"Indexed this run: %d; stored documents: %d/%d; rate: %.1f documents/s; ETA: %s\n",
				progress.EmbeddedThisRun,
				progress.TotalDocuments,
				progress.Eligible,
				progress.DocumentsPerSecond,
				progress.EstimatedRemaining.Round(time.Second),
			)
		},
	}
	config, embeddedCount, indexError := service.Run(context.Background(), retrieval.IndexOptions{
		Name:             *name,
		Provider:         *provider,
		Model:            *model,
		Dimensions:       *dimensions,
		BaseURL:          *baseURL,
		DocumentPrefix:   *documentPrefix,
		QueryPrefix:      *queryPrefix,
		BatchSize:        *batchSize,
		MaximumDocuments: *maximumDocuments,
		Rebuild:          *rebuild,
	})
	if indexError != nil {
		if strings.Contains(strings.ToLower(indexError.Error()), "no models loaded") {
			return fmt.Errorf(
				"%w; load the default local embedding model with: lms load %s --identifier %s --ttl 3600 --yes",
				indexError,
				inference.DefaultEmbeddingModelSource,
				*model,
			)
		}
		return indexError
	}
	fmt.Printf("Index run complete. Embedded documents: %d\n", embeddedCount)
	fmt.Printf("Eligible documents: %d; excluded messages: %d\n", service.EligibleDocuments, service.ExcludedMessages)
	fmt.Printf("Search index ID: %d; status: %s; vector file: %s\n", config.ID, config.Status, openedStore.ResolveSearchVectorPath(config))
	return nil
}

func runSearch(arguments []string) error {
	flagSet := flag.NewFlagSet("search", flag.ContinueOnError)
	configuredBaseURLOverride := strings.TrimSpace(os.Getenv(inference.BaseURLEnvironment))
	databasePath := flagSet.String("db", product.DefaultArchiveDatabasePath, "SQLite database path")
	query := flagSet.String("query", "", "natural-language conversation search query")
	mode := flagSet.String("mode", retrieval.SearchModeHybrid, "hybrid, semantic, or lexical")
	limit := flagSet.Int("limit", 50, "maximum conversations; zero returns every qualifying conversation")
	minimumSemanticScore := flagSet.Float64("min-semantic-score", retrieval.DefaultMinSemanticScore, "minimum cosine similarity for semantic candidates")
	excerpts := flagSet.Int("excerpts", 3, "supporting excerpts per conversation")
	explain := flagSet.Bool("explain", false, "show component scores and retrieval methods for each excerpt")
	indexID := flagSet.Int64("index-id", 0, "conversation search index ID; zero selects the latest ready index")
	baseURLOverride := flagSet.String("base-url", configuredBaseURLOverride, "override the stored query embedding API base URL")
	apiKeyEnvironment := flagSet.String("api-key-env", "", "optional environment variable containing the query embedding API key")
	allowRemote := flagSet.Bool("allow-remote", false, "allow query text to be sent to a non-loopback inference endpoint")
	sinceValue := flagSet.String("since", "", "optional start date or RFC3339 timestamp")
	untilValue := flagSet.String("until", "", "optional inclusive end date or exclusive RFC3339 timestamp")
	timezoneName := flagSet.String("timezone", "America/Los_Angeles", "IANA timezone for date-only values")
	excludeArchived := flagSet.Bool("exclude-archived", false, "exclude archived conversations")
	outputPath := flagSet.String("output", "", "optional CSV or JSON report path")
	outputFormat := flagSet.String("format", "", "table, csv, or json; inferred from output extension when omitted")
	if parseError := flagSet.Parse(arguments); parseError != nil {
		return parseError
	}
	if strings.TrimSpace(*query) == "" {
		return fmt.Errorf("--query is required")
	}
	if *limit < 0 {
		return fmt.Errorf("--limit must not be negative")
	}
	if *excerpts <= 0 {
		return fmt.Errorf("--excerpts must be positive")
	}
	if *minimumSemanticScore < -1 || *minimumSemanticScore > 1 {
		return fmt.Errorf("--min-semantic-score must be between -1 and 1")
	}
	location, locationError := time.LoadLocation(*timezoneName)
	if locationError != nil {
		return fmt.Errorf("load timezone %s: %w", *timezoneName, locationError)
	}
	sinceTime, untilTime, rangeError := resolveOptionalSearchRange(*sinceValue, *untilValue, location)
	if rangeError != nil {
		return rangeError
	}
	openedStore, openError := store.Open(*databasePath)
	if openError != nil {
		return openError
	}
	defer openedStore.Close()

	var searchIndex domain.SearchIndexConfig
	var indexError error
	if *indexID > 0 {
		searchIndex, indexError = openedStore.SearchIndexByID(context.Background(), *indexID)
	} else {
		searchIndex, indexError = openedStore.LatestReadySearchIndex(context.Background())
	}
	if indexError != nil {
		return indexError
	}
	var queryEmbedder embedding.Embedder
	effectiveBaseURL := searchIndex.BaseURL
	if *mode != retrieval.SearchModeLexical {
		if strings.TrimSpace(*baseURLOverride) != "" {
			effectiveBaseURL = inference.NormalizeBaseURL(*baseURLOverride)
		}
		if boundaryError := validateInferenceBoundary("conversation search query", effectiveBaseURL, *allowRemote); boundaryError != nil {
			return boundaryError
		}
		apiKey, apiKeyError := readOptionalAPIKey(*apiKeyEnvironment)
		if apiKeyError != nil {
			return apiKeyError
		}
		printInferenceBoundary("Conversation search query", effectiveBaseURL)
		queryEmbedder = &embedding.HTTPEmbedder{
			BaseURL:     effectiveBaseURL,
			APIKey:      apiKey,
			Model:       searchIndex.Model,
			Dimensions:  searchIndex.Dimensions,
			InputPrefix: searchIndex.QueryPrefix,
		}
	}
	sinceMillis := optionalTimeMillis(sinceTime)
	untilMillis := optionalTimeMillis(untilTime)
	engine := retrieval.Engine{Store: openedStore, QueryEmbedder: queryEmbedder, QueryBaseURL: effectiveBaseURL}
	startedAt := time.Now()
	results, searchError := engine.Search(context.Background(), retrieval.SearchOptions{
		IndexID:          searchIndex.ID,
		Query:            *query,
		Mode:             *mode,
		Limit:            *limit,
		MinSemanticScore: *minimumSemanticScore,
		IncludeArchived:  !*excludeArchived,
		SinceMillis:      sinceMillis,
		UntilMillis:      untilMillis,
		Excerpts:         *excerpts,
	})
	if searchError != nil {
		return searchError
	}
	elapsed := time.Since(startedAt)
	for resultIndex, result := range results {
		fmt.Printf(
			"\n%d. %s [score=%.6f semantic=%.4f]\n",
			resultIndex+1,
			result.ConversationTitle,
			result.Score,
			result.SemanticScore,
		)
		for _, excerpt := range result.Excerpts {
			if *explain {
				fmt.Printf(
					"   %s [semantic=%.4f lexical=%.6f via=%s]: %s\n",
					excerpt.Role,
					excerpt.SemanticScore,
					excerpt.LexicalScore,
					strings.Join(excerpt.DetectionMethods, "+"),
					oneLineExcerpt(excerpt.Text, 300),
				)
			} else {
				fmt.Printf("   %s: %s\n", excerpt.Role, oneLineExcerpt(excerpt.Text, 300))
			}
		}
	}
	fmt.Printf("\nConversations: %d; elapsed: %s\n", len(results), elapsed.Round(time.Millisecond))
	if strings.TrimSpace(*outputPath) != "" {
		if reportError := report.WriteConversationSearchResults(*outputPath, *outputFormat, results); reportError != nil {
			return reportError
		}
		auditPath := strings.TrimSuffix(*outputPath, filepath.Ext(*outputPath)) + "-audit.json"
		if auditError := report.WriteConversationSearchAudit(auditPath, report.SearchAuditMetadata{
			ApplicationVersion:   version,
			Query:                *query,
			Mode:                 *mode,
			IndexConfig:          searchIndex,
			EffectiveBaseURL:     effectiveBaseURL,
			Since:                sinceTime,
			Until:                untilTime,
			Timezone:             *timezoneName,
			IncludeArchived:      !*excludeArchived,
			MinSemanticScore:     *minimumSemanticScore,
			Limit:                *limit,
			Excerpts:             *excerpts,
			ResultCount:          len(results),
			Elapsed:              elapsed,
			QueryEmbeddingCached: engine.LastQueryCacheHit,
		}); auditError != nil {
			return auditError
		}
		fmt.Printf("Report: %s\nAudit report: %s\n", *outputPath, auditPath)
	}
	return nil
}

func resolveOptionalSearchRange(sinceValue string, untilValue string, location *time.Location) (*time.Time, *time.Time, error) {
	var sinceTime *time.Time
	var untilTime *time.Time
	if strings.TrimSpace(sinceValue) != "" {
		parsedSince, _, parseError := parseDateOrTime(sinceValue, location)
		if parseError != nil {
			return nil, nil, parseError
		}
		sinceTime = &parsedSince
	}
	if strings.TrimSpace(untilValue) != "" {
		parsedUntil, dateOnly, parseError := parseDateOrTime(untilValue, location)
		if parseError != nil {
			return nil, nil, parseError
		}
		if dateOnly {
			parsedUntil = parsedUntil.AddDate(0, 0, 1)
		}
		untilTime = &parsedUntil
	}
	if sinceTime != nil && untilTime != nil && !untilTime.After(*sinceTime) {
		return nil, nil, fmt.Errorf("resolved end time must be after start time")
	}
	return sinceTime, untilTime, nil
}

func optionalTimeMillis(value *time.Time) int64 {
	if value == nil {
		return 0
	}
	return value.UTC().UnixMilli()
}

func oneLineExcerpt(value string, maximumRunes int) string {
	oneLine := strings.Join(strings.Fields(value), " ")
	runes := []rune(oneLine)
	if len(runes) <= maximumRunes {
		return oneLine
	}
	return string(runes[:maximumRunes]) + "…"
}

func runDefinitions(arguments []string) error {
	flagSet := flag.NewFlagSet("definitions", flag.ContinueOnError)
	configuredBaseURL := inference.ConfiguredBaseURL(os.Getenv(inference.BaseURLEnvironment))
	configuredBaseURLOverride := strings.TrimSpace(os.Getenv(inference.BaseURLEnvironment))
	databasePath := flagSet.String("db", product.DefaultArchiveDatabasePath, "SQLite database path")
	months := flagSet.Int("months", 3, "lookback in calendar months when --since is omitted")
	sinceValue := flagSet.String("since", "", "start date or RFC3339 timestamp")
	untilValue := flagSet.String("until", "", "inclusive end date or exclusive RFC3339 timestamp")
	asOfValue := flagSet.String("as-of", "", "reference date or RFC3339 timestamp for --months")
	timezoneName := flagSet.String("timezone", "America/Los_Angeles", "IANA timezone for date-only values")
	excludeArchived := flagSet.Bool("exclude-archived", false, "exclude archived conversations")
	semanticEnabled := flagSet.Bool("semantic", true, "use embeddings and semantic prototypes")
	intentConfigPath := flagSet.String("intent-config", "", "optional JSON definition-intent configuration")
	outputPath := flagSet.String("output", "definitions.csv", "main report path")
	outputFormat := flagSet.String("format", "", "csv or json; inferred from output extension when omitted")
	baseURLOverride := flagSet.String("base-url", configuredBaseURLOverride, "override the stored embedding API base URL")
	apiKeyEnvironment := flagSet.String("api-key-env", "", "optional environment variable containing the embedding API key")
	embeddingConfigID := flagSet.Int64("embedding-config-id", 0, "embedding configuration ID; zero selects the latest ready configuration")
	allowBuildingConfig := flagSet.Bool("allow-building-config", false, "allow semantic analysis with an incomplete embedding configuration")
	allowRemote := flagSet.Bool("allow-remote", false, "allow text to be sent to non-loopback inference endpoints")
	verifyEnabled := flagSet.Bool("verify", false, "verify retrieved candidates with a structured-output model")
	verifyModel := flagSet.String("verify-model", inference.DefaultVerifierModel, "verification model or local server alias")
	verifyBaseURL := flagSet.String("verify-base-url", configuredBaseURL, "OpenAI-compatible verification API base URL")
	verifyAPIKeyEnvironment := flagSet.String("verify-api-key-env", "", "optional environment variable containing the verification API key")
	verifyBatchSize := flagSet.Int("verify-batch-size", 8, "candidates per local verification request")
	verifyTimeout := flagSet.Duration("verify-timeout", 10*time.Minute, "timeout for each local verification request")
	verifyMaxRetries := flagSet.Int("verify-max-retries", 2, "retries before splitting a failed verification batch")
	if parseError := flagSet.Parse(arguments); parseError != nil {
		return parseError
	}

	location, locationError := time.LoadLocation(*timezoneName)
	if locationError != nil {
		return fmt.Errorf("load timezone %s: %w", *timezoneName, locationError)
	}
	sinceTime, untilTime, rangeError := resolveDateRange(*sinceValue, *untilValue, *asOfValue, *months, location)
	if rangeError != nil {
		return rangeError
	}
	definitionConfig, configError := intent.LoadDefinitionConfig(*intentConfigPath)
	if configError != nil {
		return configError
	}
	openedStore, openError := store.Open(*databasePath)
	if openError != nil {
		return openError
	}
	defer openedStore.Close()

	var embeddingConfig domain.EmbeddingConfig
	var semanticEmbedder embedding.Embedder
	effectiveEmbeddingBaseURL := ""
	if *semanticEnabled {
		var embeddingConfigError error
		if *embeddingConfigID > 0 {
			embeddingConfig, embeddingConfigError = openedStore.EmbeddingConfigByID(context.Background(), *embeddingConfigID)
		} else {
			embeddingConfig, embeddingConfigError = openedStore.LatestReadyEmbeddingConfig(context.Background())
		}
		if embeddingConfigError != nil {
			return embeddingConfigError
		}
		if embeddingConfig.Status != "ready" && !*allowBuildingConfig {
			return fmt.Errorf(
				"embedding configuration %d is %s; finish it or pass --allow-building-config",
				embeddingConfig.ID,
				embeddingConfig.Status,
			)
		}
		semanticBaseURL := embeddingConfig.BaseURL
		if strings.TrimSpace(*baseURLOverride) != "" {
			semanticBaseURL = inference.NormalizeBaseURL(*baseURLOverride)
		}
		if boundaryError := validateInferenceBoundary("semantic embedding", semanticBaseURL, *allowRemote); boundaryError != nil {
			return boundaryError
		}
		effectiveEmbeddingBaseURL = semanticBaseURL
		apiKey, apiKeyError := readOptionalAPIKey(*apiKeyEnvironment)
		if apiKeyError != nil {
			return apiKeyError
		}
		semanticEmbedder = &embedding.HTTPEmbedder{
			BaseURL:     semanticBaseURL,
			APIKey:      apiKey,
			Model:       embeddingConfig.Model,
			Dimensions:  embeddingConfig.Dimensions,
			InputPrefix: embeddingConfig.InputPrefix,
		}
	}

	var verifier intent.Verifier
	var cachedVerifier *intent.CachedVerifier
	if *verifyEnabled {
		if *verifyBatchSize <= 0 {
			return fmt.Errorf("--verify-batch-size must be positive")
		}
		if *verifyTimeout <= 0 {
			return fmt.Errorf("--verify-timeout must be positive")
		}
		if *verifyMaxRetries < 0 {
			return fmt.Errorf("--verify-max-retries must not be negative")
		}
		if boundaryError := validateInferenceBoundary("verification", *verifyBaseURL, *allowRemote); boundaryError != nil {
			return boundaryError
		}
		verificationAPIKey, apiKeyError := readOptionalAPIKey(*verifyAPIKeyEnvironment)
		if apiKeyError != nil {
			return apiKeyError
		}
		printInferenceBoundary("Verification", *verifyBaseURL)
		httpVerifier := &intent.HTTPVerifier{
			BaseURL:    *verifyBaseURL,
			APIKey:     verificationAPIKey,
			Model:      *verifyModel,
			BatchSize:  *verifyBatchSize,
			Timeout:    *verifyTimeout,
			MaxRetries: *verifyMaxRetries,
		}
		cachedVerifier = &intent.CachedVerifier{
			Store:     openedStore,
			Inner:     httpVerifier,
			BatchSize: *verifyBatchSize,
		}
		verifier = cachedVerifier
	}

	analyzer := intent.Analyzer{Store: openedStore}
	analysisOutput, analysisError := analyzer.Analyze(context.Background(), intent.AnalyzeOptions{
		Config:          definitionConfig,
		Since:           sinceTime,
		Until:           untilTime,
		IncludeArchived: !*excludeArchived,
		Semantic:        *semanticEnabled,
		EmbeddingConfig: embeddingConfig,
		Embedder:        semanticEmbedder,
		Verifier:        verifier,
	})
	if analysisError != nil {
		return analysisError
	}

	if reportError := report.WriteDefinitionResults(*outputPath, *outputFormat, analysisOutput.Results); reportError != nil {
		return reportError
	}
	reviewPath := report.RelatedPath(*outputPath, "-review")
	if reportError := report.WriteDefinitionResults(reviewPath, *outputFormat, analysisOutput.Review); reportError != nil {
		return reportError
	}
	auditPath := strings.TrimSuffix(*outputPath, filepath.Ext(*outputPath)) + "-audit.json"
	verificationCacheHits := 0
	verificationCacheMisses := 0
	if cachedVerifier != nil {
		verificationCacheHits = cachedVerifier.CacheHits
		verificationCacheMisses = cachedVerifier.CacheMisses
	}
	if auditError := report.WriteAudit(auditPath, analysisOutput, report.AuditMetadata{
		ApplicationVersion:      version,
		Since:                   sinceTime,
		Until:                   untilTime,
		Timezone:                *timezoneName,
		IncludeArchived:         !*excludeArchived,
		IntentConfigPath:        *intentConfigPath,
		IntentConfig:            definitionConfig,
		SemanticEnabled:         *semanticEnabled,
		EmbeddingConfig:         embeddingConfig,
		EffectiveEmbeddingURL:   effectiveEmbeddingBaseURL,
		VerificationEnabled:     *verifyEnabled,
		VerifierModel:           *verifyModel,
		VerifierBaseURL:         inference.NormalizeBaseURL(*verifyBaseURL),
		VerifierBatchSize:       *verifyBatchSize,
		VerifierTimeout:         *verifyTimeout,
		VerifierMaxRetries:      *verifyMaxRetries,
		VerificationCacheHits:   verificationCacheHits,
		VerificationCacheMisses: verificationCacheMisses,
	}); auditError != nil {
		return auditError
	}

	fmt.Printf("Date range: %s through %s\n", sinceTime.In(location).Format(time.RFC3339), untilTime.In(location).Format(time.RFC3339))
	fmt.Printf("Messages examined: %d\n", analysisOutput.MessagesExamined)
	fmt.Printf("Messages with embeddings: %d\n", analysisOutput.MessagesEmbedded)
	fmt.Printf("Retrieved candidates: %d\n", analysisOutput.Candidates)
	fmt.Printf("Accepted definition requests: %d\n", len(analysisOutput.Results))
	fmt.Printf("Review cases: %d\n", len(analysisOutput.Review))
	fmt.Printf("Report: %s\n", *outputPath)
	fmt.Printf("Review report: %s\n", reviewPath)
	fmt.Printf("Audit report: %s\n", auditPath)
	return nil
}

func resolveDateRange(sinceValue string, untilValue string, asOfValue string, months int, location *time.Location) (time.Time, time.Time, error) {
	referenceTime := time.Now().In(location)
	if strings.TrimSpace(asOfValue) != "" {
		parsedReference, dateOnly, parseError := parseDateOrTime(asOfValue, location)
		if parseError != nil {
			return time.Time{}, time.Time{}, parseError
		}
		if dateOnly {
			referenceTime = parsedReference.AddDate(0, 0, 1)
		} else {
			referenceTime = parsedReference
		}
	}

	untilTime := referenceTime
	if strings.TrimSpace(untilValue) != "" {
		parsedUntil, dateOnly, parseError := parseDateOrTime(untilValue, location)
		if parseError != nil {
			return time.Time{}, time.Time{}, parseError
		}
		if dateOnly {
			untilTime = parsedUntil.AddDate(0, 0, 1)
		} else {
			untilTime = parsedUntil
		}
	}

	var sinceTime time.Time
	if strings.TrimSpace(sinceValue) != "" {
		parsedSince, _, parseError := parseDateOrTime(sinceValue, location)
		if parseError != nil {
			return time.Time{}, time.Time{}, parseError
		}
		sinceTime = parsedSince
	} else {
		if months <= 0 {
			return time.Time{}, time.Time{}, fmt.Errorf("--months must be positive when --since is omitted")
		}
		sinceTime = untilTime.AddDate(0, -months, 0)
	}
	if !untilTime.After(sinceTime) {
		return time.Time{}, time.Time{}, fmt.Errorf("resolved end time must be after start time")
	}
	return sinceTime, untilTime, nil
}

func parseDateOrTime(value string, location *time.Location) (time.Time, bool, error) {
	trimmedValue := strings.TrimSpace(value)
	if parsedTime, parseError := time.Parse(time.RFC3339Nano, trimmedValue); parseError == nil {
		return parsedTime, false, nil
	}
	parsedDate, parseError := time.ParseInLocation("2006-01-02", trimmedValue, location)
	if parseError != nil {
		return time.Time{}, false, fmt.Errorf("parse date or timestamp %q: use YYYY-MM-DD or RFC3339", value)
	}
	return parsedDate, true, nil
}

func readOptionalAPIKey(environmentName string) (string, error) {
	trimmedName := strings.TrimSpace(environmentName)
	if trimmedName == "" {
		return "", nil
	}
	apiKey := strings.TrimSpace(os.Getenv(trimmedName))
	if apiKey == "" {
		return "", fmt.Errorf("%s is not set", trimmedName)
	}
	return apiKey, nil
}

func printInferenceBoundary(operation string, baseURL string) {
	if inference.IsLoopbackBaseURL(baseURL) {
		fmt.Printf("%s inference boundary: local loopback\n", operation)
		return
	}
	fmt.Printf("%s inference boundary: remote network endpoint explicitly configured\n", operation)
}

func validateInferenceBoundary(operation string, baseURL string, allowRemote bool) error {
	if inference.IsLoopbackBaseURL(baseURL) || allowRemote {
		return nil
	}
	return fmt.Errorf(
		"%s endpoint %s is not loopback; pass --allow-remote to authorize sending text",
		operation,
		inference.NormalizeBaseURL(baseURL),
	)
}

func printUsage() {
	fmt.Printf(`%[1]s %[2]s

Local semantic indexing for ChatGPT data exports.

Usage:
  %[1]s inspect <openai-export.zip>
  %[1]s import [--db %[3]s] <openai-export.zip>
  %[1]s status [--db %[3]s]
  %[1]s embed [options]
  %[1]s index build [options]
  %[1]s search --query <topic> [options]
  %[1]s definitions [options]
  %[1]s version

Typical conversation-search workflow:
  %[1]s inspect openai-export.zip
  %[1]s import --db %[3]s openai-export.zip
  lms load <embedding-model> --identifier %[4]s
  lms server start
  %[1]s index build --db %[3]s
  %[1]s search --db %[3]s --query anime

Optional definition-request workflow:
  %[1]s embed --db %[3]s
  lms load <instruction-model> --identifier %[5]s
  %[1]s definitions --db %[3]s --months 3 --verify --output definitions.csv

Run a command with -h for its flags.
`,
		product.ArchiveCommandName,
		version,
		product.DefaultArchiveDatabasePath,
		inference.DefaultEmbeddingModel,
		inference.DefaultVerifierModel,
	)
}
