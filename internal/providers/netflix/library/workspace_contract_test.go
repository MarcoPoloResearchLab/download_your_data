package library

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/privatepath"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/enrichment"
)

func TestLocalGenerationLifecycleIsPrivateAtomicPagedAndRestartSafe(
	testContext *testing.T,
) {
	fixture := newWorkspaceFixture(testContext)
	workspace := fixture.open(testContext, workspaceOptions{
		now:     fixture.clock,
		entropy: testEntropy(0x11),
	})

	if snapshot := workspace.Snapshot(); snapshot.State != ProviderStateEmpty ||
		snapshot.Active != nil ||
		!snapshot.Capabilities.LocalImport {
		testContext.Fatalf("unexpected empty snapshot: %+v", snapshot)
	}
	firstGeneration, createError := workspace.CreateLocalGeneration()
	if createError != nil {
		testContext.Fatalf("create first local generation: %v", createError)
	}
	if firstGeneration.State != GenerationStateReceiving {
		testContext.Fatalf("first generation state = %s", firstGeneration.State)
	}
	if _, conflictError := workspace.CreateLocalGeneration(); errorCode(conflictError) != ErrorConflict {
		testContext.Fatalf("parallel generation error = %v", conflictError)
	}
	if _, uploadError := workspace.UploadViewingActivity(
		context.Background(),
		firstGeneration.ID,
		strings.NewReader(syntheticViewingCSV),
	); uploadError != nil {
		testContext.Fatalf("upload first viewing activity: %v", uploadError)
	}
	firstReady := waitForGenerationState(
		testContext,
		workspace,
		firstGeneration.ID,
		GenerationStateReady,
	)
	if firstReady.ActivityCount != 4 ||
		firstReady.UniqueTitleCount != 3 ||
		firstReady.StartDate != "2026-01-01" ||
		firstReady.EndDate != "2026-02-03" {
		testContext.Fatalf("unexpected first ready generation: %+v", firstReady)
	}
	assertNoStagedSource(testContext, fixture.root, firstGeneration.ID)
	assertGenerationFileModes(testContext, fixture.root, firstGeneration.ID)

	analytics, analyticsError := workspace.Analytics(
		context.Background(),
		firstGeneration.ID,
		ActivityFilter{},
	)
	if analyticsError != nil {
		testContext.Fatalf("read first analytics: %v", analyticsError)
	}
	if analytics.Data.ActivityCount != 4 ||
		analytics.Data.UniqueTitleCount != 3 ||
		len(analytics.Data.MonthLabels) != 2 {
		testContext.Fatalf("unexpected first analytics: %+v", analytics)
	}
	filtered, filteredError := workspace.Analytics(
		context.Background(),
		firstGeneration.ID,
		ActivityFilter{
			StartDate: "2026-02-01",
			EndDate:   "2026-02-28",
		},
	)
	if filteredError != nil || filtered.Data.ActivityCount != 2 {
		testContext.Fatalf("filtered analytics = %+v error=%v", filtered, filteredError)
	}

	firstPage, pageError := workspace.Records(
		context.Background(),
		firstGeneration.ID,
		"",
		2,
		ActivityFilter{},
	)
	if pageError != nil {
		testContext.Fatalf("read first records page: %v", pageError)
	}
	if len(firstPage.Records) != 2 || firstPage.NextCursor == "" {
		testContext.Fatalf("unexpected first records page: %+v", firstPage)
	}
	if _, changedFilterError := workspace.Records(
		context.Background(),
		firstGeneration.ID,
		firstPage.NextCursor,
		2,
		ActivityFilter{
			StartDate: "2026-02-01",
			EndDate:   "2026-02-28",
		},
	); errorCode(changedFilterError) != ErrorInvalidRequest {
		testContext.Fatalf(
			"cursor accepted a changed shared filter: %v",
			changedFilterError,
		)
	}
	secondPage, pageError := workspace.Records(
		context.Background(),
		firstGeneration.ID,
		firstPage.NextCursor,
		2,
		ActivityFilter{},
	)
	if pageError != nil {
		testContext.Fatalf("read second records page: %v", pageError)
	}
	if len(secondPage.Records) != 2 || secondPage.NextCursor != "" {
		testContext.Fatalf("unexpected second records page: %+v", secondPage)
	}

	secondGeneration, createError := workspace.CreateLocalGeneration()
	if createError != nil {
		testContext.Fatalf("create replacement local generation: %v", createError)
	}
	buildingSnapshot := workspace.Snapshot()
	if buildingSnapshot.Active == nil ||
		buildingSnapshot.Active.ID != firstGeneration.ID ||
		buildingSnapshot.Building == nil ||
		buildingSnapshot.Building.ID != secondGeneration.ID {
		testContext.Fatalf("ready generation was not isolated from replacement: %+v", buildingSnapshot)
	}
	if _, uploadError := workspace.UploadViewingActivity(
		context.Background(),
		secondGeneration.ID,
		strings.NewReader(replacementViewingCSV),
	); uploadError != nil {
		testContext.Fatalf("upload replacement viewing activity: %v", uploadError)
	}
	waitForGenerationState(
		testContext,
		workspace,
		secondGeneration.ID,
		GenerationStateReady,
	)
	if snapshot := workspace.Snapshot(); snapshot.Active == nil ||
		snapshot.Active.ID != secondGeneration.ID {
		testContext.Fatalf("replacement was not activated: %+v", snapshot)
	}
	if oldPage, oldPageError := workspace.Records(
		context.Background(),
		firstGeneration.ID,
		"",
		1,
		ActivityFilter{},
	); oldPageError != nil || len(oldPage.Records) != 1 {
		testContext.Fatalf("old ready generation was not preserved: %+v %v", oldPage, oldPageError)
	}

	if closeError := workspace.Close(); closeError != nil {
		testContext.Fatalf("close first workspace: %v", closeError)
	}
	reopened := fixture.open(testContext, workspaceOptions{
		now:     fixture.clock,
		entropy: testEntropy(0x22),
	})
	defer reopened.Close()
	if snapshot := reopened.Snapshot(); snapshot.Active == nil ||
		snapshot.Active.ID != secondGeneration.ID ||
		snapshot.Active.State != GenerationStateReady {
		testContext.Fatalf("restart lost active generation: %+v", snapshot)
	}
	if deleteError := reopened.DeleteGeneration(
		context.Background(),
		firstGeneration.ID,
	); deleteError != nil {
		testContext.Fatalf("delete non-active generation: %v", deleteError)
	}
	firstFiles, filesError := resolveGenerationFiles(fixture.root, firstGeneration.ID)
	if filesError != nil {
		testContext.Fatalf("resolve deleted generation: %v", filesError)
	}
	if _, statError := os.Stat(firstFiles.directoryPath); !errors.Is(statError, os.ErrNotExist) {
		testContext.Fatalf("deleted generation directory remains: %v", statError)
	}
}

func TestFailedReplacementNeverDisplacesActiveOrRetainsSource(
	testContext *testing.T,
) {
	fixture := newWorkspaceFixture(testContext)
	workspace := fixture.open(testContext, workspaceOptions{
		now:     fixture.clock,
		entropy: testEntropy(0x31),
	})
	defer workspace.Close()

	active, createError := workspace.CreateLocalGeneration()
	if createError != nil {
		testContext.Fatalf("create active fixture: %v", createError)
	}
	if _, uploadError := workspace.UploadViewingActivity(
		context.Background(),
		active.ID,
		strings.NewReader(syntheticViewingCSV),
	); uploadError != nil {
		testContext.Fatalf("upload active fixture: %v", uploadError)
	}
	waitForGenerationState(testContext, workspace, active.ID, GenerationStateReady)

	failed, createError := workspace.CreateLocalGeneration()
	if createError != nil {
		testContext.Fatalf("create failed replacement: %v", createError)
	}
	if _, uploadError := workspace.UploadViewingActivity(
		context.Background(),
		failed.ID,
		strings.NewReader("Title,Wrong\nPrivate Title,1/1/26\n"),
	); uploadError != nil {
		testContext.Fatalf("stage invalid replacement: %v", uploadError)
	}
	failedSnapshot := waitForGenerationState(
		testContext,
		workspace,
		failed.ID,
		GenerationStateFailed,
	)
	if failedSnapshot.Failure == nil ||
		failedSnapshot.Failure.Code != ErrorInvalidHeader {
		testContext.Fatalf("unexpected failed replacement: %+v", failedSnapshot)
	}
	if snapshot := workspace.Snapshot(); snapshot.Active == nil ||
		snapshot.Active.ID != active.ID ||
		snapshot.Building != nil {
		testContext.Fatalf("failed replacement displaced active data: %+v", snapshot)
	}
	assertNoStagedSource(testContext, fixture.root, failed.ID)

	canceled, createError := workspace.CreateLocalGeneration()
	if createError != nil {
		testContext.Fatalf("create canceled generation: %v", createError)
	}
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	if _, uploadError := workspace.UploadViewingActivity(
		canceledContext,
		canceled.ID,
		strings.NewReader(syntheticViewingCSV),
	); errorCode(uploadError) != ErrorCanceled {
		testContext.Fatalf("canceled upload error = %v", uploadError)
	}
	waitForGenerationState(testContext, workspace, canceled.ID, GenerationStateFailed)
	assertNoStagedSource(testContext, fixture.root, canceled.ID)

	oversized, createError := workspace.CreateLocalGeneration()
	if createError != nil {
		testContext.Fatalf("create oversized generation: %v", createError)
	}
	if _, uploadError := workspace.UploadViewingActivity(
		context.Background(),
		oversized.ID,
		io.LimitReader(zeroReader{}, product.MaxNetflixViewingCSVBytes+1),
	); errorCode(uploadError) != ErrorUploadTooLarge {
		testContext.Fatalf("oversized upload error = %v", uploadError)
	}
	waitForGenerationState(testContext, workspace, oversized.ID, GenerationStateFailed)
	assertNoStagedSource(testContext, fixture.root, oversized.ID)

	futureDated, createError := workspace.CreateLocalGeneration()
	if createError != nil {
		testContext.Fatalf("create future-dated generation: %v", createError)
	}
	if _, uploadError := workspace.UploadViewingActivity(
		context.Background(),
		futureDated.ID,
		strings.NewReader("Title,Date\nFuture Activity,1/1/27\n"),
	); uploadError != nil {
		testContext.Fatalf("stage future-dated generation: %v", uploadError)
	}
	futureFailure := waitForGenerationState(
		testContext,
		workspace,
		futureDated.ID,
		GenerationStateFailed,
	)
	if futureFailure.Failure == nil ||
		futureFailure.Failure.Code != ErrorInvalidDate ||
		futureFailure.Failure.Row != 2 {
		testContext.Fatalf("future-date failure = %+v", futureFailure.Failure)
	}
	assertNoStagedSource(testContext, fixture.root, futureDated.ID)
}

func TestUploadCancellationOnTheFinalReadFailsAndRemovesTheStagedSource(
	testContext *testing.T,
) {
	fixture := newWorkspaceFixture(testContext)
	workspace := fixture.open(testContext, workspaceOptions{
		now:     fixture.clock,
		entropy: testEntropy(0x39),
	})
	defer workspace.Close()

	generation, createError := workspace.CreateLocalGeneration()
	if createError != nil {
		testContext.Fatalf("create cancelable upload generation: %v", createError)
	}
	uploadContext, cancelUpload := context.WithCancel(context.Background())
	_, uploadError := workspace.UploadViewingActivity(
		uploadContext,
		generation.ID,
		&cancelingFinalReader{
			contents: []byte(syntheticViewingCSV),
			cancel:   cancelUpload,
		},
	)
	if errorCode(uploadError) != ErrorCanceled {
		testContext.Fatalf("final-read cancellation error = %v", uploadError)
	}
	failed := waitForGenerationState(
		testContext,
		workspace,
		generation.ID,
		GenerationStateFailed,
	)
	if failed.Failure == nil || failed.Failure.Code != ErrorCanceled {
		testContext.Fatalf("final-read cancellation failure = %+v", failed.Failure)
	}
	assertNoStagedSource(testContext, fixture.root, generation.ID)
}

func TestWorkspaceLeaseAndNonterminalCheckpointResume(
	testContext *testing.T,
) {
	fixture := newWorkspaceFixture(testContext)
	enteredHook := make(chan struct{})
	workspace := fixture.open(testContext, workspaceOptions{
		now:     fixture.clock,
		entropy: testEntropy(0x41),
		beforeArtifactWrite: func(ctx context.Context) error {
			close(enteredHook)
			<-ctx.Done()
			return ctx.Err()
		},
	})
	if second, secondError := openWorkspace(
		fixture.root,
		fixture.stateFile,
		fixture.leaseFile,
		fixture.cacheFile,
		nil,
		workspaceOptions{
			now:     fixture.clock,
			entropy: testEntropy(0x42),
		},
	); secondError == nil || errorCode(secondError) != ErrorLeaseUnavailable {
		if second != nil {
			second.Close()
		}
		testContext.Fatalf("second workspace lease error = %v", secondError)
	}

	generation, createError := workspace.CreateLocalGeneration()
	if createError != nil {
		testContext.Fatalf("create resumable generation: %v", createError)
	}
	if _, uploadError := workspace.UploadViewingActivity(
		context.Background(),
		generation.ID,
		strings.NewReader(syntheticViewingCSV),
	); uploadError != nil {
		testContext.Fatalf("upload resumable generation: %v", uploadError)
	}
	select {
	case <-enteredHook:
	case <-time.After(5 * time.Second):
		testContext.Fatalf("timed out waiting for importing checkpoint")
	}
	if closeError := workspace.Close(); closeError != nil {
		testContext.Fatalf("close at importing checkpoint: %v", closeError)
	}
	sourceExists, sourceError := stagedSourceExists(fixture.root, generation.ID)
	if sourceError != nil || !sourceExists {
		testContext.Fatalf("resumable source exists=%t error=%v", sourceExists, sourceError)
	}

	reopened := fixture.open(testContext, workspaceOptions{
		now:     fixture.clock,
		entropy: testEntropy(0x43),
	})
	defer reopened.Close()
	waitForGenerationState(testContext, reopened, generation.ID, GenerationStateReady)
	assertNoStagedSource(testContext, fixture.root, generation.ID)
}

func TestRestartFailsIncompleteCheckpointWithoutSource(
	testContext *testing.T,
) {
	fixture := newWorkspaceFixture(testContext)
	enteredHook := make(chan struct{})
	workspace := fixture.open(testContext, workspaceOptions{
		now:     fixture.clock,
		entropy: testEntropy(0x49),
		beforeArtifactWrite: func(ctx context.Context) error {
			close(enteredHook)
			<-ctx.Done()
			return ctx.Err()
		},
	})
	generation, createError := workspace.CreateLocalGeneration()
	if createError != nil {
		testContext.Fatalf("create incomplete generation: %v", createError)
	}
	if _, uploadError := workspace.UploadViewingActivity(
		context.Background(),
		generation.ID,
		strings.NewReader(syntheticViewingCSV),
	); uploadError != nil {
		testContext.Fatalf("upload incomplete generation: %v", uploadError)
	}
	select {
	case <-enteredHook:
	case <-time.After(5 * time.Second):
		testContext.Fatalf("timed out waiting for incomplete checkpoint")
	}
	if closeError := workspace.Close(); closeError != nil {
		testContext.Fatalf("close incomplete workspace: %v", closeError)
	}
	files, filesError := resolveGenerationFiles(fixture.root, generation.ID)
	if filesError != nil {
		testContext.Fatalf("resolve incomplete source: %v", filesError)
	}
	if removeError := os.Remove(files.source.Path()); removeError != nil {
		testContext.Fatalf("remove incomplete source fixture: %v", removeError)
	}

	reopened := fixture.open(testContext, workspaceOptions{
		now:     fixture.clock,
		entropy: testEntropy(0x4a),
	})
	defer reopened.Close()
	failed := waitForGenerationState(
		testContext,
		reopened,
		generation.ID,
		GenerationStateFailed,
	)
	if failed.Failure == nil || failed.Failure.Code != ErrorIncomplete {
		testContext.Fatalf("incomplete restart failure = %+v", failed.Failure)
	}
	if snapshot := reopened.Snapshot(); snapshot.Active != nil ||
		snapshot.Building != nil {
		testContext.Fatalf("incomplete generation became usable: %+v", snapshot)
	}
	assertNoStagedSource(testContext, fixture.root, generation.ID)
}

func TestCloseRetainsLeaseUntilBlockingUploadStops(
	testContext *testing.T,
) {
	fixture := newWorkspaceFixture(testContext)
	workspace := fixture.open(testContext, workspaceOptions{
		now:     fixture.clock,
		entropy: testEntropy(0x4b),
	})
	generation, createError := workspace.CreateLocalGeneration()
	if createError != nil {
		testContext.Fatalf("create blocking upload generation: %v", createError)
	}
	source := newBlockingReadCloser()
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
		testContext.Fatalf("timed out waiting for blocking upload")
	}
	closeResult := make(chan error, 1)
	go func() {
		closeResult <- workspace.Close()
	}()
	select {
	case closeError := <-closeResult:
		if closeError != nil {
			testContext.Fatalf("close during upload: %v", closeError)
		}
	case <-time.After(5 * time.Second):
		testContext.Fatalf("workspace close did not stop its upload")
	}
	select {
	case uploadError := <-uploadResult:
		if errorCode(uploadError) != ErrorCanceled {
			testContext.Fatalf("shutdown upload error = %v", uploadError)
		}
	case <-time.After(5 * time.Second):
		testContext.Fatalf("blocking upload outlived workspace close")
	}
	assertNoStagedSource(testContext, fixture.root, generation.ID)

	reopened := fixture.open(testContext, workspaceOptions{
		now:     fixture.clock,
		entropy: testEntropy(0x4c),
	})
	defer reopened.Close()
	if snapshot := reopened.Snapshot(); snapshot.Building == nil ||
		snapshot.Building.ID != generation.ID ||
		snapshot.Building.State != GenerationStateReceiving {
		testContext.Fatalf("shutdown changed the resumable receiving state: %+v", snapshot)
	}
}

func TestCompleteProviderDeletionClearsGenerationsEventsAndCache(
	testContext *testing.T,
) {
	fixture := newWorkspaceFixture(testContext)
	workspace := fixture.open(testContext, workspaceOptions{
		now:     fixture.clock,
		entropy: testEntropy(0x51),
	})
	defer workspace.Close()
	generation, createError := workspace.CreateLocalGeneration()
	if createError != nil {
		testContext.Fatalf("create deletion fixture: %v", createError)
	}
	if _, uploadError := workspace.UploadViewingActivity(
		context.Background(),
		generation.ID,
		strings.NewReader(syntheticViewingCSV),
	); uploadError != nil {
		testContext.Fatalf("upload deletion fixture: %v", uploadError)
	}
	waitForGenerationState(testContext, workspace, generation.ID, GenerationStateReady)
	for _, path := range []string{
		fixture.cacheFile.Path(),
		fixture.cacheFile.Path() + "-wal",
		fixture.cacheFile.Path() + "-shm",
	} {
		if writeError := os.WriteFile(path, []byte("private-cache"), 0o600); writeError != nil {
			testContext.Fatalf("create cache deletion fixture: %v", writeError)
		}
		if chmodError := os.Chmod(path, 0o600); chmodError != nil {
			testContext.Fatalf("set cache fixture mode: %v", chmodError)
		}
	}
	if deleteError := workspace.DeleteProvider(context.Background()); deleteError != nil {
		testContext.Fatalf("delete complete provider: %v", deleteError)
	}
	if snapshot := workspace.Snapshot(); snapshot.State != ProviderStateEmpty ||
		snapshot.Active != nil ||
		snapshot.Building != nil ||
		snapshot.LatestFailed != nil {
		testContext.Fatalf("provider deletion left state: %+v", snapshot)
	}
	for _, path := range []string{
		filepath.Join(fixture.root.Path(), filepath.FromSlash(generationsRelativeDirectory)),
		fixture.cacheFile.Path(),
		fixture.cacheFile.Path() + "-wal",
		fixture.cacheFile.Path() + "-shm",
	} {
		if _, statError := os.Stat(path); !errors.Is(statError, os.ErrNotExist) {
			testContext.Fatalf("provider deletion left %s: %v", path, statError)
		}
	}
	stateBytes, readError := os.ReadFile(fixture.stateFile.Path())
	if readError != nil {
		testContext.Fatalf("read reset provider state: %v", readError)
	}
	for _, privateValue := range []string{"Synthetic Film", generation.ID} {
		if strings.Contains(string(stateBytes), privateValue) {
			testContext.Fatalf("reset provider state retained %q", privateValue)
		}
	}
}

type workspaceFixture struct {
	root      privatepath.Root
	stateFile privatepath.File
	leaseFile privatepath.File
	cacheFile privatepath.File
	now       time.Time
}

func newWorkspaceFixture(testContext *testing.T) workspaceFixture {
	testContext.Helper()
	root, rootError := privatepath.NewRoot(filepath.Join(testContext.TempDir(), "private-data"))
	if rootError != nil {
		testContext.Fatalf("create private workspace root: %v", rootError)
	}
	resolve := func(relativePath string) privatepath.File {
		file, fileError := root.File(filepath.FromSlash(relativePath))
		if fileError != nil {
			testContext.Fatalf("resolve private workspace file: %v", fileError)
		}
		return file
	}
	return workspaceFixture{
		root:      root,
		stateFile: resolve(product.NetflixLibraryStateRelativePath),
		leaseFile: resolve(product.NetflixLibraryLeaseRelativePath),
		cacheFile: resolve(product.NetflixTMDBCacheRelativePath),
		now:       time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
	}
}

func (fixture workspaceFixture) clock() time.Time {
	return fixture.now
}

func (fixture workspaceFixture) open(
	testContext *testing.T,
	options workspaceOptions,
) *Workspace {
	return fixture.openWithClient(testContext, nil, options)
}

func (fixture workspaceFixture) openWithClient(
	testContext *testing.T,
	metadataClient enrichment.MetadataClient,
	options workspaceOptions,
) *Workspace {
	testContext.Helper()
	workspace, openError := openWorkspace(
		fixture.root,
		fixture.stateFile,
		fixture.leaseFile,
		fixture.cacheFile,
		metadataClient,
		options,
	)
	if openError != nil {
		testContext.Fatalf("open Netflix workspace: %v", openError)
	}
	return workspace
}

func waitForGenerationState(
	testContext *testing.T,
	workspace *Workspace,
	generationID string,
	expectedState GenerationState,
) Generation {
	testContext.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := workspace.Snapshot()
		for _, generation := range []*Generation{
			snapshot.Active,
			snapshot.Building,
			snapshot.LatestFailed,
		} {
			if generation != nil &&
				generation.ID == generationID &&
				generation.State == expectedState {
				return *generation
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	testContext.Fatalf(
		"generation %s did not reach %s; snapshot=%+v",
		generationID,
		expectedState,
		workspace.Snapshot(),
	)
	return Generation{}
}

func assertNoStagedSource(
	testContext *testing.T,
	root privatepath.Root,
	generationID string,
) {
	testContext.Helper()
	files, filesError := resolveGenerationFiles(root, generationID)
	if filesError != nil {
		testContext.Fatalf("resolve source files: %v", filesError)
	}
	for _, file := range []privatepath.File{files.source, files.sourcePart} {
		if _, statError := os.Stat(file.Path()); !errors.Is(statError, os.ErrNotExist) {
			testContext.Fatalf("staged source remains at %s: %v", file.Path(), statError)
		}
	}
}

func assertGenerationFileModes(
	testContext *testing.T,
	root privatepath.Root,
	generationID string,
) {
	testContext.Helper()
	files, filesError := resolveGenerationFiles(root, generationID)
	if filesError != nil {
		testContext.Fatalf("resolve generation files: %v", filesError)
	}
	for _, path := range []string{files.directoryPath, files.records.Path(), files.analytics.Path()} {
		pathInfo, statError := os.Stat(path)
		if statError != nil {
			testContext.Fatalf("inspect private generation path: %v", statError)
		}
		expectedMode := os.FileMode(0o600)
		if pathInfo.IsDir() {
			expectedMode = 0o700
		}
		if pathInfo.Mode().Perm() != expectedMode {
			testContext.Fatalf(
				"%s mode = %04o; want %04o",
				path,
				pathInfo.Mode().Perm(),
				expectedMode,
			)
		}
	}
}

func errorCode(receivedError error) ErrorCode {
	var typedError *Error
	if errors.As(receivedError, &typedError) {
		return typedError.Code()
	}
	return ""
}

type zeroReader struct{}

func (zeroReader) Read(destination []byte) (int, error) {
	for byteIndex := range destination {
		destination[byteIndex] = 0
	}
	return len(destination), nil
}

type cancelingFinalReader struct {
	contents []byte
	cancel   context.CancelFunc
	read     bool
}

func (reader *cancelingFinalReader) Read(destination []byte) (int, error) {
	if reader.read {
		return 0, io.EOF
	}
	reader.read = true
	readBytes := copy(destination, reader.contents)
	reader.cancel()
	return readBytes, io.EOF
}

type blockingReadCloser struct {
	started   chan struct{}
	closed    chan struct{}
	startOnce sync.Once
	closeOnce sync.Once
}

func newBlockingReadCloser() *blockingReadCloser {
	return &blockingReadCloser{
		started: make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

func (reader *blockingReadCloser) Read([]byte) (int, error) {
	reader.startOnce.Do(func() {
		close(reader.started)
	})
	<-reader.closed
	return 0, os.ErrClosed
}

func (reader *blockingReadCloser) Close() error {
	reader.closeOnce.Do(func() {
		close(reader.closed)
	})
	return nil
}

func testEntropy(seed byte) io.Reader {
	entropy := make([]byte, 16*8)
	for blockIndex := 0; blockIndex < 8; blockIndex++ {
		for byteIndex := 0; byteIndex < 16; byteIndex++ {
			entropy[blockIndex*16+byteIndex] = seed + byte(blockIndex)
		}
	}
	return bytes.NewReader(entropy)
}

const syntheticViewingCSV = `Title,Date
Synthetic Film,1/1/26
Synthetic Series: Season 1: First,1/2/26
Synthetic Series: Season 1: Second,2/2/26
Another Film,2/3/26
`

const replacementViewingCSV = `Date,Title
3/1/26,Replacement Film
3/2/26,Replacement Series: Season 1: First
`
