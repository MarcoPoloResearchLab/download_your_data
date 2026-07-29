package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/enrichment"
	netflixlibrary "github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/library"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/runtimeconfig"
)

const netflixCommandPollInterval = 100 * time.Millisecond

type commandEnvironment struct {
	output                       io.Writer
	netflixMetadataClientFactory func(runtimeconfig.Config) (enrichment.MetadataClient, error)
	netflixPollInterval          time.Duration
}

func productionCommandEnvironment() commandEnvironment {
	return commandEnvironment{
		output:                       os.Stdout,
		netflixMetadataClientFactory: newNetflixMetadataClient,
		netflixPollInterval:          netflixCommandPollInterval,
	}
}

func newNetflixMetadataClient(
	config runtimeconfig.Config,
) (enrichment.MetadataClient, error) {
	readToken, configured := config.TMDBReadToken()
	if !configured {
		return nil, nil
	}
	client, clientError := tmdb.NewClient(readToken)
	if clientError != nil {
		return nil, fmt.Errorf("create Netflix TMDB client: %w", clientError)
	}
	return client, nil
}

func runNetflixCommand(
	applicationContext context.Context,
	config runtimeconfig.Config,
	arguments []string,
	environment commandEnvironment,
) error {
	if applicationContext == nil {
		return errors.New("run Netflix command: context is required")
	}
	if environment.output == nil ||
		environment.netflixMetadataClientFactory == nil ||
		environment.netflixPollInterval <= 0 {
		return errors.New("run Netflix command: command environment is not initialized")
	}
	if len(arguments) == 0 {
		return printNetflixUsage(environment.output)
	}
	subcommand := arguments[0]
	subcommandArguments := arguments[1:]
	switch subcommand {
	case "inspect":
		return runNetflixInspect(
			config,
			subcommandArguments,
			environment,
		)
	case "enrich":
		return runNetflixEnrich(
			applicationContext,
			config,
			subcommandArguments,
			environment,
		)
	case "export":
		return runNetflixExport(
			applicationContext,
			config,
			subcommandArguments,
			environment,
		)
	case "help", "-h", "--help":
		return printNetflixUsage(environment.output)
	default:
		return fmt.Errorf("unknown Netflix command %q", subcommand)
	}
}

func runNetflixInspect(
	config runtimeconfig.Config,
	arguments []string,
	environment commandEnvironment,
) error {
	flagSet := flag.NewFlagSet("netflix inspect", flag.ContinueOnError)
	flagSet.SetOutput(environment.output)
	flagSet.Usage = func() {
		_, _ = fmt.Fprintf(
			environment.output,
			"Usage: %s netflix inspect\n",
			product.CommandName,
		)
	}
	if parseError := flagSet.Parse(arguments); parseError != nil {
		if errors.Is(parseError, flag.ErrHelp) {
			return nil
		}
		return parseError
	}
	if flagSet.NArg() != 0 {
		return fmt.Errorf("usage: %s netflix inspect", product.CommandName)
	}

	metadataClient, clientError := environment.netflixMetadataClientFactory(config)
	if clientError != nil {
		return clientError
	}
	return withNetflixCommandWorkspace(
		config,
		metadataClient,
		func(workspace *netflixlibrary.Workspace) error {
			snapshot := workspace.Snapshot()
			if _, writeError := fmt.Fprintf(
				environment.output,
				"Netflix provider: state=%s tmdb_configured=%t\n",
				snapshot.State,
				snapshot.Capabilities.TMDBConfigured,
			); writeError != nil {
				return fmt.Errorf("write Netflix inspection: %w", writeError)
			}
			for _, entry := range []struct {
				label      string
				generation *netflixlibrary.Generation
			}{
				{label: "active", generation: snapshot.Active},
				{label: "building", generation: snapshot.Building},
				{label: "latest_failed", generation: snapshot.LatestFailed},
			} {
				if printError := printNetflixGeneration(
					environment.output,
					entry.label,
					entry.generation,
				); printError != nil {
					return fmt.Errorf("write Netflix inspection: %w", printError)
				}
			}
			return nil
		},
	)
}

func runNetflixEnrich(
	applicationContext context.Context,
	config runtimeconfig.Config,
	arguments []string,
	environment commandEnvironment,
) error {
	flagSet := flag.NewFlagSet("netflix enrich", flag.ContinueOnError)
	flagSet.SetOutput(environment.output)
	localeValue := flagSet.String(
		"locale",
		tmdb.DefaultLocale,
		"TMDB response locale in ll-RR form",
	)
	flagSet.Usage = func() {
		_, _ = fmt.Fprintf(
			environment.output,
			"Usage: %s netflix enrich [--locale %s]\n",
			product.CommandName,
			tmdb.DefaultLocale,
		)
	}
	if parseError := flagSet.Parse(arguments); parseError != nil {
		if errors.Is(parseError, flag.ErrHelp) {
			return nil
		}
		return parseError
	}
	if flagSet.NArg() != 0 {
		return fmt.Errorf(
			"usage: %s netflix enrich [--locale %s]",
			product.CommandName,
			tmdb.DefaultLocale,
		)
	}
	locale, localeError := tmdb.NewLocale(*localeValue)
	if localeError != nil {
		return fmt.Errorf("validate Netflix enrichment locale: %w", localeError)
	}
	metadataClient, clientError := environment.netflixMetadataClientFactory(config)
	if clientError != nil {
		return clientError
	}

	return withNetflixCommandWorkspace(
		config,
		metadataClient,
		func(workspace *netflixlibrary.Workspace) error {
			snapshot := workspace.Snapshot()
			if snapshot.Active == nil ||
				snapshot.Active.State != netflixlibrary.GenerationStateReady ||
				snapshot.Active.AnalysisLevel != netflixlibrary.AnalysisLevelLocal {
				return errors.New(
					"start Netflix enrichment: an active ready-local generation is required",
				)
			}
			if !snapshot.Capabilities.TMDBConfigured {
				return fmt.Errorf(
					"start Netflix enrichment: %s is not configured",
					tmdb.ReadTokenEnvironment,
				)
			}
			if _, writeError := fmt.Fprintf(
				environment.output,
				"Netflix TMDB boundary authorized: up to %d unique derived title queries; viewing dates and activity rows stay local.\n",
				snapshot.Active.UniqueTitleCount,
			); writeError != nil {
				return fmt.Errorf("write Netflix enrichment boundary: %w", writeError)
			}
			generation, createError := workspace.CreateTMDBGeneration(
				applicationContext,
				snapshot.Active.ID,
				locale,
				enrichment.AuthorizeTMDBTitleQueries(),
			)
			if createError != nil {
				return fmt.Errorf("start Netflix TMDB generation: %w", createError)
			}
			readyGeneration, waitError := waitForNetflixGeneration(
				applicationContext,
				workspace,
				generation.ID,
				environment.output,
				environment.netflixPollInterval,
			)
			if waitError != nil {
				return waitError
			}
			if _, writeError := fmt.Fprintf(
				environment.output,
				"Netflix enrichment complete: generation=%s matched=%d review=%d unmatched=%d cache_hits=%d\n",
				readyGeneration.ID,
				readyGeneration.MatchedTitleCount,
				readyGeneration.ReviewTitleCount,
				readyGeneration.UnmatchedTitleCount,
				readyGeneration.CacheHitTitleCount,
			); writeError != nil {
				return fmt.Errorf("write Netflix enrichment completion: %w", writeError)
			}
			return nil
		},
	)
}

func runNetflixExport(
	applicationContext context.Context,
	config runtimeconfig.Config,
	arguments []string,
	environment commandEnvironment,
) error {
	flagSet := flag.NewFlagSet("netflix export", flag.ContinueOnError)
	flagSet.SetOutput(environment.output)
	outputPath := flagSet.String(
		"output",
		"",
		"CSV path relative to DOWNLOAD_YOUR_DATA_DATA_DIR",
	)
	generationID := flagSet.String(
		"generation",
		"",
		"ready TMDB generation ID; defaults to the active generation",
	)
	flagSet.Usage = func() {
		_, _ = fmt.Fprintf(
			environment.output,
			"Usage: %s netflix export --output <relative.csv> [--generation <generation-id>]\n",
			product.CommandName,
		)
	}
	if parseError := flagSet.Parse(arguments); parseError != nil {
		if errors.Is(parseError, flag.ErrHelp) {
			return nil
		}
		return parseError
	}
	if flagSet.NArg() != 0 || *outputPath == "" {
		return fmt.Errorf(
			"usage: %s netflix export --output <relative.csv> [--generation <generation-id>]",
			product.CommandName,
		)
	}
	if filepath.Ext(*outputPath) != ".csv" {
		return errors.New("validate Netflix export --output: path must end in .csv")
	}
	outputFile, outputFileError := config.DataRoot().File(*outputPath)
	if outputFileError != nil {
		return fmt.Errorf("validate Netflix export --output: %w", outputFileError)
	}
	metadataClient, clientError := environment.netflixMetadataClientFactory(config)
	if clientError != nil {
		return clientError
	}

	return withNetflixCommandWorkspace(
		config,
		metadataClient,
		func(workspace *netflixlibrary.Workspace) error {
			selectedGenerationID := *generationID
			if selectedGenerationID == "" {
				snapshot := workspace.Snapshot()
				if snapshot.Active == nil {
					return errors.New(
						"export Netflix activity: an active ready TMDB generation is required",
					)
				}
				selectedGenerationID = snapshot.Active.ID
			}
			records, recordsError := workspace.ExportRecords(
				applicationContext,
				selectedGenerationID,
			)
			if recordsError != nil {
				return fmt.Errorf("read Netflix enriched generation: %w", recordsError)
			}
			if replaceError := outputFile.Replace(func(destination io.Writer) error {
				return netflix.WriteEnrichedActivity(
					applicationContext,
					destination,
					records,
				)
			}); replaceError != nil {
				return fmt.Errorf("write Netflix enriched CSV: %w", replaceError)
			}
			if _, writeError := fmt.Fprintf(
				environment.output,
				"Netflix export complete: generation=%s activities=%d output=%s\n",
				selectedGenerationID,
				len(records),
				outputFile.RelativePath(),
			); writeError != nil {
				return fmt.Errorf("write Netflix export completion: %w", writeError)
			}
			return nil
		},
	)
}

func withNetflixCommandWorkspace(
	config runtimeconfig.Config,
	metadataClient enrichment.MetadataClient,
	operation func(*netflixlibrary.Workspace) error,
) (operationError error) {
	workspace, workspaceError := netflixlibrary.Open(
		config.DataRoot(),
		config.NetflixLibrary(),
		config.NetflixLease(),
		config.NetflixTMDBCache(),
		metadataClient,
	)
	if workspaceError != nil {
		return fmt.Errorf("open Netflix provider workspace: %w", workspaceError)
	}
	defer func() {
		if closeError := workspace.Close(); closeError != nil {
			operationError = errors.Join(
				operationError,
				fmt.Errorf("close Netflix provider workspace: %w", closeError),
			)
		}
	}()
	return operation(workspace)
}

func waitForNetflixGeneration(
	applicationContext context.Context,
	workspace *netflixlibrary.Workspace,
	generationID string,
	output io.Writer,
	pollInterval time.Duration,
) (netflixlibrary.Generation, error) {
	lastSequence := int64(0)
	pollTimer := time.NewTimer(0)
	defer pollTimer.Stop()
	for {
		select {
		case <-pollTimer.C:
		case <-applicationContext.Done():
			return cancelNetflixCommandGeneration(
				applicationContext.Err(),
				workspace,
				generationID,
			)
		}

		events, eventsError := workspace.Events(generationID, lastSequence)
		if eventsError != nil {
			return netflixlibrary.Generation{}, fmt.Errorf(
				"read Netflix generation progress: %w",
				eventsError,
			)
		}
		for _, event := range events.Events {
			if printError := printNetflixEvent(output, generationID, event); printError != nil {
				return netflixlibrary.Generation{}, fmt.Errorf(
					"write Netflix generation progress: %w",
					printError,
				)
			}
		}
		lastSequence = events.LastSequence

		generation, found := netflixSnapshotGeneration(
			workspace.Snapshot(),
			generationID,
		)
		if !found {
			return netflixlibrary.Generation{}, fmt.Errorf(
				"inspect Netflix generation %s: generation is not present in the current workspace state",
				generationID,
			)
		}
		switch generation.State {
		case netflixlibrary.GenerationStateReady:
			return generation, nil
		case netflixlibrary.GenerationStateFailed:
			return netflixlibrary.Generation{}, netflixGenerationFailure(generation)
		}
		pollTimer.Reset(pollInterval)
	}
}

func cancelNetflixCommandGeneration(
	cancellationError error,
	workspace *netflixlibrary.Workspace,
	generationID string,
) (netflixlibrary.Generation, error) {
	cleanupContext, cancelCleanup := context.WithTimeout(
		context.Background(),
		shutdownTimeout,
	)
	defer cancelCleanup()
	deleteError := workspace.DeleteGeneration(cleanupContext, generationID)
	if generation, found := netflixSnapshotGeneration(
		workspace.Snapshot(),
		generationID,
	); found && generation.State == netflixlibrary.GenerationStateReady {
		return generation, nil
	}
	commandError := fmt.Errorf(
		"cancel Netflix generation %s: %w",
		generationID,
		cancellationError,
	)
	if deleteError != nil {
		commandError = errors.Join(
			commandError,
			fmt.Errorf("persist Netflix generation cancellation: %w", deleteError),
		)
	}
	return netflixlibrary.Generation{}, commandError
}

func netflixSnapshotGeneration(
	snapshot netflixlibrary.Snapshot,
	generationID string,
) (netflixlibrary.Generation, bool) {
	for _, generation := range []*netflixlibrary.Generation{
		snapshot.Active,
		snapshot.Building,
		snapshot.LatestFailed,
	} {
		if generation != nil && generation.ID == generationID {
			return *generation, true
		}
	}
	return netflixlibrary.Generation{}, false
}

func netflixGenerationFailure(generation netflixlibrary.Generation) error {
	if generation.Failure == nil {
		return fmt.Errorf(
			"netflix generation %s entered failed state without a failure identity",
			generation.ID,
		)
	}
	if generation.Failure.Row > 0 {
		return fmt.Errorf(
			"netflix generation %s failed with %s at CSV row %d",
			generation.ID,
			generation.Failure.Code,
			generation.Failure.Row,
		)
	}
	return fmt.Errorf(
		"netflix generation %s failed with %s",
		generation.ID,
		generation.Failure.Code,
	)
}

func printNetflixGeneration(
	output io.Writer,
	label string,
	generation *netflixlibrary.Generation,
) error {
	if generation == nil {
		_, writeError := fmt.Fprintf(output, "%s: none\n", label)
		return writeError
	}
	failureCode := "none"
	if generation.Failure != nil {
		failureCode = string(generation.Failure.Code)
	}
	_, writeError := fmt.Fprintf(
		output,
		"%s: id=%s analysis=%s state=%s activities=%d unique_titles=%d completed_titles=%d matched=%d review=%d unmatched=%d cache_hits=%d progress=%d%% failure=%s\n",
		label,
		generation.ID,
		generation.AnalysisLevel,
		generation.State,
		generation.ActivityCount,
		generation.UniqueTitleCount,
		generation.CompletedTitleCount,
		generation.MatchedTitleCount,
		generation.ReviewTitleCount,
		generation.UnmatchedTitleCount,
		generation.CacheHitTitleCount,
		generation.ProgressPercent,
		failureCode,
	)
	return writeError
}

func printNetflixEvent(
	output io.Writer,
	generationID string,
	event netflixlibrary.Event,
) error {
	failureCode := "none"
	if event.Failure != nil {
		failureCode = string(event.Failure.Code)
	}
	_, writeError := fmt.Fprintf(
		output,
		"Netflix generation %s: state=%s completed_titles=%d/%d matched=%d review=%d unmatched=%d cache_hits=%d progress=%d%% failure=%s\n",
		generationID,
		event.State,
		event.CompletedTitleCount,
		event.TotalTitleCount,
		event.MatchedTitleCount,
		event.ReviewTitleCount,
		event.UnmatchedTitleCount,
		event.CacheHitTitleCount,
		event.ProgressPercent,
		failureCode,
	)
	return writeError
}

func printNetflixUsage(output io.Writer) error {
	_, writeError := fmt.Fprintf(
		output,
		`Netflix provider operations

Usage:
  %[1]s netflix inspect
  %[1]s netflix enrich [--locale %[2]s]
  %[1]s netflix export --output <relative.csv> [--generation <generation-id>]

The commands use the same private provider library, TMDB token, cache, match
contract, and enriched CSV shape as the browser workspace.
`,
		product.CommandName,
		tmdb.DefaultLocale,
	)
	return writeError
}
