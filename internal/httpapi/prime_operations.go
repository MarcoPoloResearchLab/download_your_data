package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/authentication"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/media"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/enrichment"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/primevideo"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/runtimeconfig"
)

type primeJob struct {
	userID string
	cancel context.CancelFunc
}

type primeOperations struct {
	config       runtimeconfig.Config
	client       enrichment.MetadataClient
	logger       *slog.Logger
	coordinator  *userRequestCoordinator
	transactions [userRequestLockShardCount]sync.Mutex
	mutex        sync.Mutex
	jobs         map[string]primeJob
	workers      sync.WaitGroup
	closed       bool
}

func newPrimeOperations(config runtimeconfig.Config, client enrichment.MetadataClient, coordinator *userRequestCoordinator, logger *slog.Logger) *primeOperations {
	return &primeOperations{config: config, client: client, coordinator: coordinator, logger: logger, jobs: map[string]primeJob{}}
}

func (operations *primeOperations) transaction(ctx context.Context, user authentication.AuthenticatedUser, operation func(*primevideo.Store) error) error {
	index := 0
	for _, character := range []byte(user.StorageID()[:2]) {
		index = (index*16 + hexadecimalValue(character)) % userRequestLockShardCount
	}
	lock := &operations.transactions[index]
	lock.Lock()
	defer lock.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	paths, err := operations.config.UserWorkspace(user)
	if err != nil {
		return err
	}
	store, err := primevideo.Open(paths.Root())
	if err != nil {
		return err
	}
	return errors.Join(operation(store), store.Close())
}

func (operations *primeOperations) start(user authentication.AuthenticatedUser, generation primevideo.Generation) error {
	operations.mutex.Lock()
	defer operations.mutex.Unlock()
	if operations.closed {
		return errors.New("prime operations are closed")
	}
	key := user.StorageID() + ":" + generation.ID
	if _, exists := operations.jobs[key]; exists {
		return nil
	}
	if len(operations.jobs) >= 64 {
		return errors.New("prime operation capacity reached")
	}
	paths, err := operations.config.UserWorkspace(user)
	if err != nil {
		return err
	}
	lease, err := primevideo.ClaimEnrichment(paths.Root())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	operations.jobs[key] = primeJob{userID: user.StorageID(), cancel: cancel}
	operations.workers.Add(1)
	go func() {
		defer operations.workers.Done()
		defer func() {
			cancel()
			if err := lease.Close(); err != nil {
				operations.logger.Error("Prime worker lease failed", "error_type", "close_failed")
			}
			operations.mutex.Lock()
			delete(operations.jobs, key)
			operations.mutex.Unlock()
		}()
		operations.run(ctx, user, generation)
	}()
	return nil
}

func (operations *primeOperations) workerTransaction(ctx context.Context, user authentication.AuthenticatedUser, operation func(*primevideo.Store) error) error {
	var result error
	operations.coordinator.shared(user, func() { result = operations.transaction(ctx, user, operation) })
	return result
}

func (operations *primeOperations) run(ctx context.Context, user authentication.AuthenticatedUser, generation primevideo.Generation) {
	locale, err := tmdb.NewLocale(generation.Locale)
	if err != nil {
		operations.logger.Error("Prime worker failed", "error_type", "invalid_locale")
		return
	}
	var records []media.Record
	err = operations.workerTransaction(ctx, user, func(store *primevideo.Store) error {
		var readError error
		records, readError = store.EnrichmentRecords(generation.ID)
		return readError
	})
	if err == nil {
		for _, record := range records {
			result, queryError := primevideo.ResolveTitle(ctx, enrichment.AuthorizeTMDBTitleQueries(), record, operations.client, locale)
			if queryError != nil {
				err = queryError
				break
			}
			err = operations.workerTransaction(ctx, user, func(store *primevideo.Store) error { return store.Checkpoint(ctx, generation.ID, result) })
			if err != nil {
				break
			}
		}
	}
	if err == nil {
		err = operations.workerTransaction(ctx, user, func(store *primevideo.Store) error { return store.CompleteEnrichment(ctx, generation.ID) })
	}
	if err == nil || ctx.Err() != nil {
		return
	}
	var failure *primevideo.Error
	if errors.As(err, &failure) && failure.Code == "not_found" {
		return
	}
	if failureErr := operations.workerTransaction(ctx, user, func(store *primevideo.Store) error { return store.FailEnrichment(generation.ID, err) }); failureErr != nil {
		operations.logger.Error("Prime checkpoint failed", "error_type", "persistence_failed")
	}
}

func (operations *primeOperations) cancelUser(user authentication.AuthenticatedUser) {
	operations.mutex.Lock()
	defer operations.mutex.Unlock()
	for _, job := range operations.jobs {
		if job.userID == user.StorageID() {
			job.cancel()
		}
	}
}

func (operations *primeOperations) cancelGeneration(user authentication.AuthenticatedUser, id string) {
	operations.mutex.Lock()
	defer operations.mutex.Unlock()
	if job, exists := operations.jobs[user.StorageID()+":"+id]; exists {
		job.cancel()
	}
}

func (operations *primeOperations) close() {
	operations.mutex.Lock()
	operations.closed = true
	for _, job := range operations.jobs {
		job.cancel()
	}
	operations.mutex.Unlock()
	operations.workers.Wait()
}
