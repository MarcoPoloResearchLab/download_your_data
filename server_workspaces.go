package main

import (
	"errors"
	"fmt"
	"sync"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/authentication"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/enrichment"
	netflixlibrary "github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/library"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/runtimeconfig"
)

var (
	errWorkspaceRegistryClosed   = errors.New("workspace.registry_closed")
	errWorkspaceRegistryCapacity = errors.New("workspace.registry_capacity")
	errWorkspaceInUse            = errors.New("workspace.in_use")
)

const userRequestLockShardCount = 64

type userRequestCoordinator struct {
	shards [userRequestLockShardCount]sync.RWMutex
}

func (coordinator *userRequestCoordinator) shared(
	user authentication.AuthenticatedUser,
	operation func(),
) {
	lock := coordinator.lock(user)
	lock.RLock()
	defer lock.RUnlock()
	operation()
}

func (coordinator *userRequestCoordinator) exclusive(
	user authentication.AuthenticatedUser,
	operation func(),
) {
	lock := coordinator.lock(user)
	lock.Lock()
	defer lock.Unlock()
	operation()
}

func (coordinator *userRequestCoordinator) lock(
	user authentication.AuthenticatedUser,
) *sync.RWMutex {
	storageID := user.StorageID()
	shardIndex := 0
	if len(storageID) >= 2 {
		for _, character := range []byte(storageID[:2]) {
			shardIndex = (shardIndex*16 + hexadecimalValue(character)) %
				userRequestLockShardCount
		}
	}
	return &coordinator.shards[shardIndex]
}

func hexadecimalValue(character byte) int {
	if character >= '0' && character <= '9' {
		return int(character - '0')
	}
	if character >= 'a' && character <= 'f' {
		return int(character-'a') + 10
	}
	return 0
}

type netflixWorkspaceEntry struct {
	workspace *netflixlibrary.Workspace
	inUse     int
	lastUse   uint64
}

type netflixWorkspaceRegistry struct {
	mutex          sync.Mutex
	config         runtimeconfig.Config
	metadataClient enrichment.MetadataClient
	entries        map[string]*netflixWorkspaceEntry
	useSequence    uint64
	closed         bool
}

func newNetflixWorkspaceRegistry(
	config runtimeconfig.Config,
	metadataClient enrichment.MetadataClient,
) (*netflixWorkspaceRegistry, error) {
	if config.DataRoot().Path() == "" {
		return nil, errors.New("create Netflix workspace registry: data root is required")
	}
	return &netflixWorkspaceRegistry{
		config:         config,
		metadataClient: metadataClient,
		entries:        make(map[string]*netflixWorkspaceEntry),
	}, nil
}

func (registry *netflixWorkspaceRegistry) acquire(
	user authentication.AuthenticatedUser,
) (*netflixlibrary.Workspace, func(), error) {
	if validationError := user.Validate(); validationError != nil {
		return nil, nil, fmt.Errorf("acquire Netflix workspace: %w", validationError)
	}
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	if registry.closed {
		return nil, nil, errWorkspaceRegistryClosed
	}
	storageID := user.StorageID()
	registry.useSequence++
	if entry, exists := registry.entries[storageID]; exists {
		entry.inUse++
		entry.lastUse = registry.useSequence
		return entry.workspace, registry.releaseFunction(storageID, entry), nil
	}
	if len(registry.entries) >= product.MaxOpenUserNetflixWorkspaces {
		if evictionError := registry.evictOneIdleLocked(); evictionError != nil {
			return nil, nil, evictionError
		}
	}
	userWorkspace, pathsError := registry.config.UserWorkspace(user)
	if pathsError != nil {
		return nil, nil, fmt.Errorf("acquire Netflix workspace paths: %w", pathsError)
	}
	workspace, workspaceError := netflixlibrary.Open(
		userWorkspace.Root(),
		userWorkspace.NetflixLibrary(),
		userWorkspace.NetflixLease(),
		userWorkspace.NetflixTMDBCache(),
		registry.metadataClient,
	)
	if workspaceError != nil {
		return nil, nil, fmt.Errorf("open user Netflix workspace: %w", workspaceError)
	}
	entry := &netflixWorkspaceEntry{
		workspace: workspace,
		inUse:     1,
		lastUse:   registry.useSequence,
	}
	registry.entries[storageID] = entry
	return workspace, registry.releaseFunction(storageID, entry), nil
}

func (registry *netflixWorkspaceRegistry) releaseFunction(
	storageID string,
	expectedEntry *netflixWorkspaceEntry,
) func() {
	var releaseOnce sync.Once
	return func() {
		releaseOnce.Do(func() {
			registry.mutex.Lock()
			defer registry.mutex.Unlock()
			entry, exists := registry.entries[storageID]
			if !exists || entry != expectedEntry || entry.inUse == 0 {
				return
			}
			entry.inUse--
		})
	}
}

func (registry *netflixWorkspaceRegistry) evictOneIdleLocked() error {
	var selectedStorageID string
	var selectedEntry *netflixWorkspaceEntry
	for storageID, entry := range registry.entries {
		if entry.inUse != 0 {
			continue
		}
		if selectedEntry == nil || entry.lastUse < selectedEntry.lastUse {
			selectedStorageID = storageID
			selectedEntry = entry
		}
	}
	if selectedEntry == nil {
		return errWorkspaceRegistryCapacity
	}
	delete(registry.entries, selectedStorageID)
	if closeError := selectedEntry.workspace.Close(); closeError != nil {
		return fmt.Errorf("evict idle Netflix workspace: %w", closeError)
	}
	return nil
}

func (registry *netflixWorkspaceRegistry) deleteUser(
	user authentication.AuthenticatedUser,
) error {
	if validationError := user.Validate(); validationError != nil {
		return fmt.Errorf("delete user workspace: %w", validationError)
	}
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	if registry.closed {
		return errWorkspaceRegistryClosed
	}
	if entry, exists := registry.entries[user.StorageID()]; exists {
		if entry.inUse != 0 {
			return errWorkspaceInUse
		}
		delete(registry.entries, user.StorageID())
		if closeError := entry.workspace.Close(); closeError != nil {
			return fmt.Errorf("close user Netflix workspace: %w", closeError)
		}
	}
	if deleteError := registry.config.DeleteUserWorkspace(user); deleteError != nil {
		return deleteError
	}
	return nil
}

func (registry *netflixWorkspaceRegistry) close() error {
	if registry == nil {
		return nil
	}
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	if registry.closed {
		return nil
	}
	registry.closed = true
	var closeError error
	for storageID, entry := range registry.entries {
		if entry.inUse != 0 {
			closeError = errors.Join(
				closeError,
				fmt.Errorf("close Netflix workspace %s: %w", storageID, errWorkspaceInUse),
			)
			continue
		}
		if entryError := entry.workspace.Close(); entryError != nil {
			closeError = errors.Join(closeError, entryError)
		}
	}
	clear(registry.entries)
	return closeError
}
