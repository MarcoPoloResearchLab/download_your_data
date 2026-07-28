package main

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/domain"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/embedding"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/exportformat"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/inference"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/ingest"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/intent"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/privatepath"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/report"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/retrieval"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/runtimeconfig"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/store"
)

const version = "0.2.0"

func main() {
	homeDirectory, homeError := os.UserHomeDir()
	if homeError != nil {
		fmt.Fprintln(os.Stderr, "error: resolve user home directory:", homeError)
		os.Exit(1)
	}
	config, configError := runtimeconfig.Load(os.Getenv, homeDirectory, rand.Reader)
	if configError != nil {
		fmt.Fprintln(os.Stderr, "error:", configError)
		os.Exit(1)
	}
	if runError := run(os.Args[1:], config); runError != nil {
		fmt.Fprintln(os.Stderr, "error:", runError)
		os.Exit(1)
	}
}

func run(arguments []string, config runtimeconfig.Config) error {
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
		return runImport(config, commandArguments)
	case "status":
		return runStatus(config, commandArguments)
	case "embed":
		return runEmbed(config, commandArguments)
	case "index":
		return runIndex(config, commandArguments)
	case "search":
		return runSearch(config, commandArguments)
	case "definitions":
		return runDefinitions(config, commandArguments)
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

func runImport(config runtimeconfig.Config, arguments []string) error {
	flagSet := flag.NewFlagSet("import", flag.ContinueOnError)
	forceImport := flagSet.Bool("force", false, "import even when the same export hash was already completed")
	if parseError := flagSet.Parse(arguments); parseError != nil {
		return parseError
	}
	if flagSet.NArg() != 1 {
		return fmt.Errorf("usage: %s import <openai-export.zip>", product.ArchiveCommandName)
	}

	openedStore, openError := store.Open(config.ArchiveDatabase())
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

func runStatus(config runtimeconfig.Config, arguments []string) error {
	flagSet := flag.NewFlagSet("status", flag.ContinueOnError)
	if parseError := flagSet.Parse(arguments); parseError != nil {
		return parseError
	}
	if flagSet.NArg() != 0 {
		return fmt.Errorf("usage: %s status", product.ArchiveCommandName)
	}
	openedStore, openError := store.Open(config.ArchiveDatabase())
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

func runEmbed(runtimeConfig runtimeconfig.Config, arguments []string) error {
	flagSet := flag.NewFlagSet("embed", flag.ContinueOnError)
	provider := flagSet.String("provider", inference.DefaultEmbeddingProvider, "embedding provider label")
	model := flagSet.String("model", inference.DefaultEmbeddingModel, "embedding model or local server alias")
	dimensions := flagSet.Int("dimensions", inference.DefaultEmbeddingDimensions, "embedding dimensions")
	inputPrefix := flagSet.String("input-prefix", inference.DefaultEmbeddingInputPrefix, "task prefix prepended to every embedding input")
	apiKeyEnvironment := flagSet.String("api-key-env", "", "optional environment variable containing an API key")
	batchSize := flagSet.Int("batch-size", product.DefaultInferenceBatchSize, "messages per embedding request")
	maximumMessages := flagSet.Int("max-messages", 0, "maximum messages to embed in this run; zero means all")
	refreshStale := flagSet.Bool("refresh-stale", false, "re-embed messages whose prepared text changed")
	if parseError := flagSet.Parse(arguments); parseError != nil {
		return parseError
	}
	if flagSet.NArg() != 0 {
		return fmt.Errorf("usage: %s embed [options]", product.ArchiveCommandName)
	}

	apiKey, apiKeyError := readOptionalAPIKey(*apiKeyEnvironment)
	if apiKeyError != nil {
		return apiKeyError
	}
	openedStore, openError := store.Open(runtimeConfig.ArchiveDatabase())
	if openError != nil {
		return openError
	}
	defer openedStore.Close()

	fmt.Printf("Embedding provider: %s\n", *provider)
	fmt.Printf("API base URL: %s\n", runtimeConfig.InferenceBaseURL().String())
	printInferenceBoundary("Embedding", runtimeConfig.InferenceBoundary())
	fmt.Println("Text sent to inference: user-message text and limited neighboring context")
	fmt.Println("Attachments sent to inference: no")

	embedder := &embedding.HTTPEmbedder{
		BaseURL:     runtimeConfig.InferenceBaseURL(),
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
	embeddingConfig, embeddedCount, embeddingError := service.Run(context.Background(), embedding.ServiceOptions{
		Provider:        *provider,
		Model:           *model,
		Dimensions:      *dimensions,
		BaseURL:         runtimeConfig.InferenceBaseURL(),
		InputPrefix:     *inputPrefix,
		BatchSize:       *batchSize,
		MaximumMessages: *maximumMessages,
		RefreshStale:    *refreshStale,
	})
	if embeddingError != nil {
		return embeddingError
	}
	vectorFile, pathError := openedStore.ResolveVectorFile(embeddingConfig)
	if pathError != nil {
		return pathError
	}
	fmt.Printf("Embedding complete. New embeddings: %d\n", embeddedCount)
	fmt.Printf("Configuration ID: %d; vector file: %s\n", embeddingConfig.ID, vectorFile.Path())
	return nil
}

func runIndex(runtimeConfig runtimeconfig.Config, arguments []string) error {
	if len(arguments) == 0 || arguments[0] != "build" {
		return fmt.Errorf("usage: %s index build [options]", product.ArchiveCommandName)
	}
	flagSet := flag.NewFlagSet("index build", flag.ContinueOnError)
	name := flagSet.String("name", retrieval.DefaultIndexName, "conversation search index name")
	provider := flagSet.String("provider", inference.DefaultEmbeddingProvider, "embedding provider label")
	model := flagSet.String("model", inference.DefaultEmbeddingModel, "embedding model or local server alias")
	dimensions := flagSet.Int("dimensions", inference.DefaultEmbeddingDimensions, "embedding dimensions")
	documentPrefix := flagSet.String("document-prefix", retrieval.DefaultDocumentPrefix, "task prefix for indexed conversation documents")
	queryPrefix := flagSet.String("query-prefix", retrieval.DefaultQueryPrefix, "task prefix for search queries")
	apiKeyEnvironment := flagSet.String("api-key-env", "", "optional environment variable containing an API key")
	batchSize := flagSet.Int("batch-size", product.DefaultInferenceBatchSize, "conversation documents per embedding request")
	maximumDocuments := flagSet.Int("max-documents", 0, "maximum documents to embed in this run; zero means all")
	rebuild := flagSet.Bool("rebuild", false, "replace the named search index and its vector file after model preflight succeeds")
	if parseError := flagSet.Parse(arguments[1:]); parseError != nil {
		return parseError
	}
	if flagSet.NArg() != 0 {
		return fmt.Errorf("usage: %s index build [options]", product.ArchiveCommandName)
	}
	if *batchSize <= 0 {
		return fmt.Errorf("--batch-size must be positive")
	}
	if *maximumDocuments < 0 {
		return fmt.Errorf("--max-documents must not be negative")
	}
	apiKey, apiKeyError := readOptionalAPIKey(*apiKeyEnvironment)
	if apiKeyError != nil {
		return apiKeyError
	}
	openedStore, openError := store.Open(runtimeConfig.ArchiveDatabase())
	if openError != nil {
		return openError
	}
	defer openedStore.Close()

	fmt.Printf("Conversation search index: %s\n", *name)
	fmt.Printf("Embedding provider: %s\n", *provider)
	fmt.Printf("API base URL: %s\n", runtimeConfig.InferenceBaseURL().String())
	printInferenceBoundary("Conversation indexing", runtimeConfig.InferenceBoundary())
	fmt.Println("Indexed roles: visible user and assistant text across every branch")
	fmt.Println("Excluded content: thoughts, reasoning recaps, empty text, attachments, images, and audio")

	documentEmbedder := &embedding.HTTPEmbedder{
		BaseURL:     runtimeConfig.InferenceBaseURL(),
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
	searchConfig, embeddedCount, indexError := service.Run(context.Background(), retrieval.IndexOptions{
		Name:             *name,
		Provider:         *provider,
		Model:            *model,
		Dimensions:       *dimensions,
		BaseURL:          runtimeConfig.InferenceBaseURL(),
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
	vectorFile, pathError := openedStore.ResolveSearchVectorFile(searchConfig)
	if pathError != nil {
		return pathError
	}
	fmt.Printf("Index run complete. Embedded documents: %d\n", embeddedCount)
	fmt.Printf("Eligible documents: %d; excluded messages: %d\n", service.EligibleDocuments, service.ExcludedMessages)
	fmt.Printf("Search index ID: %d; status: %s; vector file: %s\n", searchConfig.ID, searchConfig.Status, vectorFile.Path())
	return nil
}

func runSearch(runtimeConfig runtimeconfig.Config, arguments []string) error {
	flagSet := flag.NewFlagSet("search", flag.ContinueOnError)
	query := flagSet.String("query", "", "natural-language conversation search query")
	mode := flagSet.String("mode", retrieval.SearchModeHybrid, "hybrid, semantic, or lexical")
	limit := flagSet.Int("limit", 50, "maximum conversations; zero returns every qualifying conversation")
	minimumSemanticScore := flagSet.Float64("min-semantic-score", retrieval.DefaultMinSemanticScore, "minimum cosine similarity for semantic candidates")
	excerpts := flagSet.Int("excerpts", 3, "supporting excerpts per conversation")
	explain := flagSet.Bool("explain", false, "show component scores and retrieval methods for each excerpt")
	indexID := flagSet.Int64("index-id", 0, "conversation search index ID; zero selects the latest ready index")
	apiKeyEnvironment := flagSet.String("api-key-env", "", "optional environment variable containing the query embedding API key")
	sinceValue := flagSet.String("since", "", "optional start date or RFC3339 timestamp")
	untilValue := flagSet.String("until", "", "optional inclusive end date or exclusive RFC3339 timestamp")
	timezoneName := flagSet.String("timezone", "America/Los_Angeles", "IANA timezone for date-only values")
	excludeArchived := flagSet.Bool("exclude-archived", false, "exclude archived conversations")
	outputPath := flagSet.String("output", "", "optional report path relative to the private data root")
	outputFormat := flagSet.String("format", "", "table, csv, or json; inferred from output extension when omitted")
	if parseError := flagSet.Parse(arguments); parseError != nil {
		return parseError
	}
	if flagSet.NArg() != 0 {
		return fmt.Errorf("usage: %s search --query <topic> [options]", product.ArchiveCommandName)
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
	var outputFile privatepath.File
	var auditFile privatepath.File
	if strings.TrimSpace(*outputPath) != "" {
		var outputFileError error
		outputFile, outputFileError = runtimeConfig.DataRoot().File(*outputPath)
		if outputFileError != nil {
			return fmt.Errorf("validate --output: %w", outputFileError)
		}
		var auditFileError error
		auditFile, auditFileError = report.RelatedFile(outputFile, "-audit", ".json")
		if auditFileError != nil {
			return auditFileError
		}
	}
	location, locationError := time.LoadLocation(*timezoneName)
	if locationError != nil {
		return fmt.Errorf("load timezone %s: %w", *timezoneName, locationError)
	}
	sinceTime, untilTime, rangeError := resolveOptionalSearchRange(*sinceValue, *untilValue, location)
	if rangeError != nil {
		return rangeError
	}
	openedStore, openError := store.Open(runtimeConfig.ArchiveDatabase())
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
		configuredBaseURL := runtimeConfig.InferenceBaseURL()
		effectiveBaseURL = configuredBaseURL.String()
		if searchIndex.BaseURL != effectiveBaseURL {
			return fmt.Errorf(
				"search index %d uses inference URL %s; run %s index build --rebuild with the current runtime configuration",
				searchIndex.ID,
				searchIndex.BaseURL,
				product.ArchiveCommandName,
			)
		}
		apiKey, apiKeyError := readOptionalAPIKey(*apiKeyEnvironment)
		if apiKeyError != nil {
			return apiKeyError
		}
		printInferenceBoundary("Conversation search query", runtimeConfig.InferenceBoundary())
		queryEmbedder = &embedding.HTTPEmbedder{
			BaseURL:     configuredBaseURL,
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
		if reportError := report.WriteConversationSearchResults(outputFile, *outputFormat, results); reportError != nil {
			return reportError
		}
		if auditError := report.WriteConversationSearchAudit(auditFile, report.SearchAuditMetadata{
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
		fmt.Printf("Report: %s\nAudit report: %s\n", outputFile.Path(), auditFile.Path())
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

func runDefinitions(runtimeConfig runtimeconfig.Config, arguments []string) error {
	flagSet := flag.NewFlagSet("definitions", flag.ContinueOnError)
	months := flagSet.Int("months", 3, "lookback in calendar months when --since is omitted")
	sinceValue := flagSet.String("since", "", "start date or RFC3339 timestamp")
	untilValue := flagSet.String("until", "", "inclusive end date or exclusive RFC3339 timestamp")
	asOfValue := flagSet.String("as-of", "", "reference date or RFC3339 timestamp for --months")
	timezoneName := flagSet.String("timezone", "America/Los_Angeles", "IANA timezone for date-only values")
	excludeArchived := flagSet.Bool("exclude-archived", false, "exclude archived conversations")
	semanticEnabled := flagSet.Bool("semantic", true, "use embeddings and semantic prototypes")
	intentConfigPath := flagSet.String("intent-config", "", "optional JSON definition-intent configuration")
	outputPath := flagSet.String("output", "reports/definitions.csv", "report path relative to the private data root")
	outputFormat := flagSet.String("format", "", "csv or json; inferred from output extension when omitted")
	apiKeyEnvironment := flagSet.String("api-key-env", "", "optional environment variable containing the embedding API key")
	embeddingConfigID := flagSet.Int64("embedding-config-id", 0, "embedding configuration ID; zero selects the latest ready configuration")
	allowBuildingConfig := flagSet.Bool("allow-building-config", false, "allow semantic analysis with an incomplete embedding configuration")
	verifyEnabled := flagSet.Bool("verify", false, "verify retrieved candidates with a structured-output model")
	verifyModel := flagSet.String("verify-model", inference.DefaultVerifierModel, "verification model or local server alias")
	verifyAPIKeyEnvironment := flagSet.String("verify-api-key-env", "", "optional environment variable containing the verification API key")
	verifyBatchSize := flagSet.Int("verify-batch-size", 8, "candidates per local verification request")
	verifyTimeout := flagSet.Duration("verify-timeout", 10*time.Minute, "timeout for each local verification request")
	verifyMaxRetries := flagSet.Int("verify-max-retries", 2, "retries before splitting a failed verification batch")
	if parseError := flagSet.Parse(arguments); parseError != nil {
		return parseError
	}
	if flagSet.NArg() != 0 {
		return fmt.Errorf("usage: %s definitions [options]", product.ArchiveCommandName)
	}
	outputFile, outputFileError := runtimeConfig.DataRoot().File(*outputPath)
	if outputFileError != nil {
		return fmt.Errorf("validate --output: %w", outputFileError)
	}
	reviewFile, reviewFileError := report.RelatedFile(outputFile, "-review", "")
	if reviewFileError != nil {
		return reviewFileError
	}
	auditFile, auditFileError := report.RelatedFile(outputFile, "-audit", ".json")
	if auditFileError != nil {
		return auditFileError
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
	openedStore, openError := store.Open(runtimeConfig.ArchiveDatabase())
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
		semanticBaseURL := runtimeConfig.InferenceBaseURL()
		if embeddingConfig.BaseURL != semanticBaseURL.String() {
			return fmt.Errorf(
				"embedding configuration %d uses inference URL %s; run %s embed with the current runtime configuration",
				embeddingConfig.ID,
				embeddingConfig.BaseURL,
				product.ArchiveCommandName,
			)
		}
		effectiveEmbeddingBaseURL = semanticBaseURL.String()
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
		printInferenceBoundary("Semantic embedding", runtimeConfig.InferenceBoundary())
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
		verificationAPIKey, apiKeyError := readOptionalAPIKey(*verifyAPIKeyEnvironment)
		if apiKeyError != nil {
			return apiKeyError
		}
		printInferenceBoundary("Verification", runtimeConfig.InferenceBoundary())
		httpVerifier := &intent.HTTPVerifier{
			BaseURL:    runtimeConfig.InferenceBaseURL(),
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

	if reportError := report.WriteDefinitionResults(outputFile, *outputFormat, analysisOutput.Results); reportError != nil {
		return reportError
	}
	if reportError := report.WriteDefinitionResults(reviewFile, *outputFormat, analysisOutput.Review); reportError != nil {
		return reportError
	}
	verificationCacheHits := 0
	verificationCacheMisses := 0
	if cachedVerifier != nil {
		verificationCacheHits = cachedVerifier.CacheHits
		verificationCacheMisses = cachedVerifier.CacheMisses
	}
	if auditError := report.WriteAudit(auditFile, analysisOutput, report.AuditMetadata{
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
		VerifierBaseURL:         runtimeConfig.InferenceBaseURL().String(),
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
	fmt.Printf("Report: %s\n", outputFile.Path())
	fmt.Printf("Review report: %s\n", reviewFile.Path())
	fmt.Printf("Audit report: %s\n", auditFile.Path())
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

func printInferenceBoundary(operation string, boundary runtimeconfig.InferenceBoundary) {
	if boundary == runtimeconfig.InferenceBoundaryLoopback {
		fmt.Printf("%s inference boundary: local loopback\n", operation)
		return
	}
	fmt.Printf("%s inference boundary: remote network endpoint explicitly configured\n", operation)
}

func printUsage() {
	fmt.Printf(`%[1]s %[2]s

Local semantic indexing for ChatGPT data exports.

Usage:
  %[1]s inspect <openai-export.zip>
  %[1]s import <openai-export.zip>
  %[1]s status
  %[1]s embed [options]
  %[1]s index build [options]
  %[1]s search --query <topic> [options]
  %[1]s definitions [options]
  %[1]s version

Typical conversation-search workflow:
  %[1]s inspect openai-export.zip
  %[1]s import openai-export.zip
  lms load <embedding-model> --identifier %[3]s
  lms server start
  %[1]s index build
  %[1]s search --query anime

Optional definition-request workflow:
  %[1]s embed
  lms load <instruction-model> --identifier %[4]s
  %[1]s definitions --months 3 --verify --output reports/definitions.csv

Runtime configuration:
  DOWNLOAD_YOUR_DATA_ADDRESS
  DOWNLOAD_YOUR_DATA_DATA_DIR
  DOWNLOAD_YOUR_DATA_INFERENCE_BASE_URL
  DOWNLOAD_YOUR_DATA_INFERENCE_BOUNDARY=loopback|authorized-remote

Report --output values are relative to DOWNLOAD_YOUR_DATA_DATA_DIR.
Run a command with -h for its flags.
`,
		product.ArchiveCommandName,
		version,
		inference.DefaultEmbeddingModel,
		inference.DefaultVerifierModel,
	)
}
