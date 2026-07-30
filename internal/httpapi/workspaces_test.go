package httpapi

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/privatepath"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	netflixlibrary "github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/library"
)

func TestWorkspaceRegistryDoesNotEvictBackgroundWork(testContext *testing.T) {
	dataRootPath := testContext.TempDir()
	if modeError := os.Chmod(dataRootPath, 0o700); modeError != nil {
		testContext.Fatalf("secure test data root: %v", modeError)
	}
	dataRoot, rootError := privatepath.NewRoot(dataRootPath)
	if rootError != nil {
		testContext.Fatalf("create test data root: %v", rootError)
	}
	stateFile, stateError := dataRoot.File(product.NetflixLibraryStateRelativePath)
	leaseFile, leaseError := dataRoot.File(product.NetflixLibraryLeaseRelativePath)
	cacheFile, cacheError := dataRoot.File(product.NetflixTMDBCacheRelativePath)
	if joinedError := errors.Join(stateError, leaseError, cacheError); joinedError != nil {
		testContext.Fatalf("resolve test workspace files: %v", joinedError)
	}
	workspace, workspaceError := netflixlibrary.Open(
		dataRoot,
		stateFile,
		leaseFile,
		cacheFile,
		nil,
	)
	if workspaceError != nil {
		testContext.Fatalf("open test workspace: %v", workspaceError)
	}
	testContext.Cleanup(func() {
		if closeError := workspace.Close(); closeError != nil {
			testContext.Errorf("close test workspace: %v", closeError)
		}
	})
	generation, createError := workspace.CreateLocalGeneration()
	if createError != nil {
		testContext.Fatalf("create test generation: %v", createError)
	}
	source := newBlockingWorkspaceReader()
	uploadResult := make(chan error, 1)
	go func() {
		_, uploadError := workspace.UploadViewingActivity(
			context.Background(),
			generation.ID,
			source,
		)
		uploadResult <- uploadError
	}()
	select {
	case <-source.started:
	case <-time.After(5 * time.Second):
		testContext.Fatalf("timed out waiting for background upload")
	}
	if !workspace.HasRunningOperations() {
		testContext.Fatalf("workspace did not expose its running upload")
	}

	registry := &netflixWorkspaceRegistry{
		entries: map[string]*netflixWorkspaceEntry{
			"active": {
				workspace: workspace,
				lastUse:   1,
			},
		},
	}
	for entryIndex := 1; entryIndex < product.MaxOpenUserNetflixWorkspaces; entryIndex++ {
		registry.entries[string(rune(entryIndex))] = &netflixWorkspaceEntry{inUse: 1}
	}
	if evictionError := registry.evictOneIdleLocked(); !errors.Is(
		evictionError,
		errWorkspaceRegistryCapacity,
	) {
		testContext.Fatalf(
			"evict registry with background work error = %v; want %v",
			evictionError,
			errWorkspaceRegistryCapacity,
		)
	}
	if _, exists := registry.entries["active"]; !exists {
		testContext.Fatalf("registry evicted a workspace with background work")
	}

	if closeError := source.Close(); closeError != nil {
		testContext.Fatalf("release background upload: %v", closeError)
	}
	select {
	case uploadError := <-uploadResult:
		if uploadError == nil {
			testContext.Fatalf("blocking upload unexpectedly succeeded")
		}
	case <-time.After(5 * time.Second):
		testContext.Fatalf("background upload did not stop")
	}
}

type blockingWorkspaceReader struct {
	started   chan struct{}
	closed    chan struct{}
	startOnce sync.Once
	closeOnce sync.Once
}

func newBlockingWorkspaceReader() *blockingWorkspaceReader {
	return &blockingWorkspaceReader{
		started: make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

func (reader *blockingWorkspaceReader) Read([]byte) (int, error) {
	reader.startOnce.Do(func() {
		close(reader.started)
	})
	<-reader.closed
	return 0, os.ErrClosed
}

func (reader *blockingWorkspaceReader) Close() error {
	reader.closeOnce.Do(func() {
		close(reader.closed)
	})
	return nil
}
