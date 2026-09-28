package primevideo

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/privatepath"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/media"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
)

const (
	storeContract              = "prime-video-library-v1"
	providerDirectory          = "providers/prime-video"
	statePath                  = providerDirectory + "/library.json"
	leasePath                  = providerDirectory + "/library.lock"
	Receiving                  = "receiving"
	Validating                 = "validating"
	AwaitingConfirmation       = "awaiting_confirmation"
	Importing                  = "importing"
	Ready                      = "ready"
	Failed                     = "failed"
	Enriching                  = "enriching"
	maxStateBytes        int64 = 512 * 1024 * 1024
)

// Event is a durable ordered lifecycle observation.
type Event struct {
	Sequence int    `json:"sequence"`
	State    string `json:"state"`
	At       string `json:"at"`
}

// Generation is the public generation state without private source records.
type Generation struct {
	ID                     string   `json:"id"`
	State                  string   `json:"state"`
	AnalysisLevel          string   `json:"analysis_level"`
	CreatedAt              string   `json:"created_at"`
	UpdatedAt              string   `json:"updated_at"`
	Preview                *Preview `json:"preview,omitempty"`
	Datasets               []string `json:"datasets"`
	ProfileLabel           string   `json:"profile_label"`
	RecordCount            int      `json:"record_count"`
	Failure                string   `json:"failure,omitempty"`
	Events                 []Event  `json:"events"`
	SourceGenerationID     string   `json:"source_generation_id,omitempty"`
	Locale                 string   `json:"locale,omitempty"`
	MatcherIdentity        string   `json:"matcher_identity,omitempty"`
	ClientIdentity         string   `json:"client_identity,omitempty"`
	CompletedTitles        int      `json:"completed_titles"`
	TotalTitles            int      `json:"total_titles"`
	TitleQueriesAuthorized bool     `json:"title_queries_authorized"`
}

// Snapshot reports independent active and pending provider generations.
type Snapshot struct {
	Active           *Generation `json:"active_generation"`
	Building         *Generation `json:"building_generation"`
	MaxUploadBytes   int64       `json:"max_upload_bytes"`
	MaxExpandedBytes uint64      `json:"max_expanded_bytes"`
	TMDBConfigured   bool        `json:"tmdb_configured"`
}

type storedGeneration struct {
	Generation Generation       `json:"generation"`
	Records    []media.Activity `json:"records"`
}
type libraryState struct {
	Contract string            `json:"contract"`
	Active   *storedGeneration `json:"active"`
	Pending  *storedGeneration `json:"pending"`
}

// Store owns one user/provider transaction and its cross-process lease.
type Store struct {
	root  privatepath.Root
	file  privatepath.File
	lease *os.File
	state libraryState
}

// Open resolves current private state and acquires the exclusive provider transaction lease.
func Open(root privatepath.Root) (*Store, error) {
	stateFile, err := root.File(statePath)
	if err != nil {
		return nil, fmt.Errorf("resolve Prime state: %w", err)
	}
	leaseFile, err := root.File(leasePath)
	if err != nil {
		return nil, fmt.Errorf("resolve Prime lease: %w", err)
	}
	if err = leaseFile.Prepare(); err != nil {
		return nil, fmt.Errorf("prepare Prime lease: %w", err)
	}
	lease, err := os.OpenFile(leaseFile.Path(), os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open Prime lease: %w", err)
	}
	if err = syscall.Flock(int(lease.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, newError("conflict", 0, errors.Join(err, lease.Close()))
	}
	store := &Store{root: root, file: stateFile, lease: lease, state: libraryState{Contract: storeContract}}
	if err = stateFile.Prepare(); err != nil {
		return nil, errors.Join(err, store.Close())
	}
	handle, err := os.Open(stateFile.Path())
	if err != nil {
		return nil, errors.Join(err, store.Close())
	}
	encoded, readErr := io.ReadAll(io.LimitReader(handle, maxStateBytes+1))
	err = errors.Join(readErr, handle.Close())
	if err != nil {
		return nil, errors.Join(err, store.Close())
	}
	if int64(len(encoded)) > maxStateBytes {
		return nil, errors.Join(newError("invalid_persistence", 0, nil), store.Close())
	}
	if len(encoded) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&store.state); err != nil {
			return nil, errors.Join(newError("invalid_persistence", 0, err), store.Close())
		}
		var extra any
		if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			return nil, errors.Join(newError("invalid_persistence", 0, err), store.Close())
		}
		if err = validateState(store.state); err != nil {
			return nil, errors.Join(err, store.Close())
		}
	}
	return store, nil
}

// Close releases the transaction lease.
func (store *Store) Close() error {
	if store.lease == nil {
		return nil
	}
	err := errors.Join(syscall.Flock(int(store.lease.Fd()), syscall.LOCK_UN), store.lease.Close())
	store.lease = nil
	if err != nil {
		return fmt.Errorf("close Prime transaction: %w", err)
	}
	return nil
}

// Snapshot returns the detached public provider state.
func (store *Store) Snapshot() Snapshot {
	result := Snapshot{MaxUploadBytes: MaxUploadBytes, MaxExpandedBytes: MaxExpandedBytes}
	if store.state.Active != nil {
		generation := cloneGeneration(store.state.Active.Generation)
		result.Active = &generation
	}
	if store.state.Pending != nil {
		generation := cloneGeneration(store.state.Pending.Generation)
		result.Building = &generation
	}
	return result
}

// Create starts one receiving generation without changing the active library.
func (store *Store) Create(ctx context.Context) (Generation, error) {
	if err := ctx.Err(); err != nil {
		return Generation{}, newError("canceled", 0, err)
	}
	if store.state.Pending != nil && store.state.Pending.Generation.State != Failed {
		return Generation{}, newError("conflict", 0, nil)
	}
	generation, err := newGeneration()
	if err != nil {
		return Generation{}, err
	}
	next := store.state
	next.Pending = &storedGeneration{Generation: generation, Records: []media.Activity{}}
	if err := store.save(next); err != nil {
		return Generation{}, err
	}
	return cloneGeneration(generation), nil
}

func newGeneration() (Generation, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return Generation{}, fmt.Errorf("create Prime generation ID: %w", err)
	}
	generation := Generation{ID: "pg_" + hex.EncodeToString(random), State: Receiving, AnalysisLevel: "local", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Datasets: []string{}, Events: []Event{}}
	transition(&generation, Receiving)
	return generation, nil
}

// Upload parses the archive and removes source bytes before exposing the import preview.
func (store *Store) Upload(ctx context.Context, id string, encoded []byte) (Generation, error) {
	if err := ctx.Err(); err != nil {
		return Generation{}, newError("canceled", 0, err)
	}
	for _, candidate := range []*storedGeneration{store.state.Active, store.state.Pending} {
		if candidate != nil && candidate.Generation.ID == id && candidate.Generation.AnalysisLevel == "local" && (candidate.Generation.State == AwaitingConfirmation || candidate.Generation.State == Ready) {
			digest := sha256.Sum256(encoded)
			if candidate.Generation.Preview.SourceHash == hex.EncodeToString(digest[:]) {
				return cloneGeneration(candidate.Generation), nil
			}
			return Generation{}, newError("conflict", 0, nil)
		}
	}
	pending, err := store.pending(id, Receiving)
	if err != nil {
		return Generation{}, err
	}
	next := store.state
	next.Pending = pending
	transition(&pending.Generation, Validating)
	bundle, err := ParseArchive(ctx, encoded)
	if err != nil {
		transition(&pending.Generation, Failed)
		pending.Generation.Failure = errorCode(err)
		pending.Records = []media.Activity{}
		return Generation{}, errors.Join(err, store.save(next))
	}
	preview := bundle.Preview()
	pending.Generation.Preview = &preview
	pending.Records = make([]media.Activity, 0, len(bundle.records))
	for _, record := range bundle.records {
		pending.Records = append(pending.Records, record.WithSource(id, "").Snapshot())
	}
	transition(&pending.Generation, AwaitingConfirmation)
	if err = store.save(next); err != nil {
		return Generation{}, err
	}
	return cloneGeneration(pending.Generation), nil
}

// Selection is a validated dataset selection and profile label.
type Selection struct {
	datasets []string
	label    string
}

// NewSelection validates the current closed import selection contract.
func NewSelection(datasets []string, label string) (Selection, error) {
	if len(datasets) == 0 || len(datasets) > 6 || len(label) > 200 || strings.ContainsAny(label, "\r\n\x00") {
		return Selection{}, newError("invalid_selection", 0, nil)
	}
	seen := make(map[string]bool)
	for _, dataset := range datasets {
		switch dataset {
		case ViewingDataset, DetailsDataset, WatchDataset, SearchDataset, PurchaseDataset, TrailerDataset:
		default:
			return Selection{}, newError("invalid_selection", 0, nil)
		}
		if seen[dataset] {
			return Selection{}, newError("invalid_selection", 0, nil)
		}
		seen[dataset] = true
	}
	if seen[DetailsDataset] && !seen[ViewingDataset] {
		return Selection{}, newError("invalid_selection", 0, nil)
	}
	selected := slices.Clone(datasets)
	slices.Sort(selected)
	return Selection{datasets: selected, label: label}, nil
}

// Select activates only the confirmed datasets in one atomic private transaction.
func (store *Store) Select(ctx context.Context, id string, selection Selection) (Generation, error) {
	if store.state.Active != nil && store.state.Active.Generation.ID == id {
		generation := store.state.Active.Generation
		if slices.Equal(generation.Datasets, selection.datasets) && generation.ProfileLabel == selection.label {
			return cloneGeneration(generation), nil
		}
		return Generation{}, newError("conflict", 0, nil)
	}
	pending, err := store.pending(id, AwaitingConfirmation)
	if err != nil {
		return Generation{}, err
	}
	if err = ctx.Err(); err != nil {
		return Generation{}, newError("canceled", 0, err)
	}
	available := make(map[string]bool)
	for _, dataset := range pending.Generation.Preview.Datasets {
		available[dataset.ID] = true
	}
	for _, dataset := range selection.datasets {
		if !available[dataset] {
			return Generation{}, newError("invalid_selection", 0, nil)
		}
	}
	selected := make(map[string]bool)
	for _, dataset := range selection.datasets {
		selected[dataset] = true
	}
	records := []media.Activity{}
	for _, value := range pending.Records {
		if err = ctx.Err(); err != nil {
			return Generation{}, newError("canceled", 0, err)
		}
		_, dataset := fileKind(value.Source.File)
		if !selected[dataset] {
			continue
		}
		record, err := media.NewRecord(value)
		if err != nil {
			return Generation{}, newError("invalid_persistence", 0, err)
		}
		record = record.WithSource(id, selection.label)
		if !selected[DetailsDataset] {
			record = record.WithoutPlaybackDetails()
		}
		records = append(records, record.Snapshot())
	}
	next := store.state
	if next.Active != nil && next.Active.Generation.Preview.SourceHash == pending.Generation.Preview.SourceHash && slices.Equal(next.Active.Generation.Datasets, selection.datasets) && next.Active.Generation.ProfileLabel == selection.label {
		next.Pending = nil
		if err = store.save(next); err != nil {
			return Generation{}, err
		}
		return cloneGeneration(next.Active.Generation), nil
	}
	pending.Records = records
	pending.Generation.Datasets = slices.Clone(selection.datasets)
	pending.Generation.ProfileLabel = selection.label
	pending.Generation.RecordCount = len(records)
	preview := pending.Generation.Preview
	selectedPreview := *preview
	selectedPreview.Datasets = []Dataset{}
	selectedPreview.UnsupportedFiles = []string{}
	for _, dataset := range preview.Datasets {
		if selected[dataset.ID] {
			selectedPreview.Datasets = append(selectedPreview.Datasets, dataset)
		}
	}
	pending.Generation.Preview = &selectedPreview
	transition(&pending.Generation, Importing)
	transition(&pending.Generation, Ready)
	next.Active = pending
	next.Pending = nil
	if err = ctx.Err(); err != nil {
		return Generation{}, newError("canceled", 0, err)
	}
	if err = store.save(next); err != nil {
		return Generation{}, err
	}
	return cloneGeneration(pending.Generation), nil
}

// Records returns the active library as validated domain records.
func (store *Store) Records(ctx context.Context) ([]media.Record, error) {
	result := []media.Record{}
	if store.state.Active == nil {
		return result, nil
	}
	for _, value := range store.state.Active.Records {
		if err := ctx.Err(); err != nil {
			return nil, newError("canceled", 0, err)
		}
		record, err := media.NewRecord(value)
		if err != nil {
			return nil, newError("invalid_persistence", 0, err)
		}
		result = append(result, record)
	}
	return result, nil
}

// Generation returns a current active or pending generation by opaque ID.
func (store *Store) Generation(id string) (Generation, error) {
	for _, candidate := range []*storedGeneration{store.state.Active, store.state.Pending} {
		if candidate != nil && candidate.Generation.ID == id {
			return cloneGeneration(candidate.Generation), nil
		}
	}
	return Generation{}, newError("not_found", 0, nil)
}

// DeleteGeneration cancels and removes the pending generation.
func (store *Store) DeleteGeneration(id string) error {
	if store.state.Active != nil && store.state.Active.Generation.ID == id {
		return newError("conflict", 0, nil)
	}
	if store.state.Pending == nil || store.state.Pending.Generation.ID != id {
		return nil
	}
	next := store.state
	next.Pending = nil
	return store.save(next)
}

// DeleteProvider removes all private Prime records and previews.
func (store *Store) DeleteProvider() error { return store.save(libraryState{Contract: storeContract}) }

func (store *Store) pending(id, expectedState string) (*storedGeneration, error) {
	if store.state.Pending == nil || store.state.Pending.Generation.ID != id {
		return nil, newError("not_found", 0, nil)
	}
	if store.state.Pending.Generation.State != expectedState {
		return nil, newError("conflict", 0, nil)
	}
	copy := *store.state.Pending
	copy.Generation = cloneGeneration(copy.Generation)
	copy.Records = slices.Clone(copy.Records)
	return &copy, nil
}

func (store *Store) save(next libraryState) error {
	if err := validateState(next); err != nil {
		return err
	}
	encoded, err := json.Marshal(next)
	if err != nil {
		return fmt.Errorf("encode Prime state: %w", err)
	}
	if int64(len(encoded)) > maxStateBytes {
		return newError("limit_exceeded", 0, nil)
	}
	if err = store.file.Replace(func(writer io.Writer) error { _, err := writer.Write(encoded); return err }); err != nil {
		return fmt.Errorf("persist Prime state: %w", err)
	}
	store.state = next
	return nil
}

func validateState(state libraryState) error {
	if state.Contract != storeContract {
		return newError("invalid_persistence", 0, nil)
	}
	for _, stored := range []*storedGeneration{state.Active, state.Pending} {
		if stored == nil {
			continue
		}
		generation := stored.Generation
		if len(generation.ID) != 35 || !strings.HasPrefix(generation.ID, "pg_") {
			return newError("invalid_persistence", 0, nil)
		}
		if _, err := hex.DecodeString(generation.ID[3:]); err != nil {
			return newError("invalid_persistence", 0, err)
		}
		switch generation.State {
		case Receiving, Validating, AwaitingConfirmation, Importing, Enriching, Ready, Failed:
		default:
			return newError("invalid_persistence", 0, nil)
		}
		if (generation.AnalysisLevel != "local" && generation.AnalysisLevel != "tmdb") || len(stored.Records) > MaxRows || len(generation.Events) > 256 {
			return newError("invalid_persistence", 0, nil)
		}
		if generation.AnalysisLevel == "tmdb" {
			if _, err := tmdb.NewLocale(generation.Locale); err != nil || !generation.TitleQueriesAuthorized || generation.SourceGenerationID == "" || generation.MatcherIdentity != MatcherIdentity || generation.ClientIdentity != tmdb.ClientIdentity || generation.CompletedTitles < 0 || generation.CompletedTitles > generation.TotalTitles {
				return newError("invalid_persistence", 0, err)
			}
		}
		if generation.State == Ready && (generation.RecordCount != len(stored.Records) || generation.Preview == nil) {
			return newError("invalid_persistence", 0, nil)
		}
		if generation.State == AwaitingConfirmation && generation.Preview == nil {
			return newError("invalid_persistence", 0, nil)
		}
		for _, value := range stored.Records {
			if value.Provider != media.PrimeVideo || value.Source.GenerationID != generation.ID {
				return newError("invalid_persistence", 0, nil)
			}
			if _, err := media.NewRecord(value); err != nil {
				return newError("invalid_persistence", 0, err)
			}
		}
	}
	if state.Active != nil && state.Active.Generation.State != Ready {
		return newError("invalid_persistence", 0, nil)
	}
	if state.Pending != nil && state.Pending.Generation.State == Ready {
		return newError("invalid_persistence", 0, nil)
	}
	return nil
}

func transition(generation *Generation, state string) {
	generation.State = state
	generation.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	generation.Events = append(generation.Events, Event{Sequence: len(generation.Events) + 1, State: state, At: generation.UpdatedAt})
}
func cloneGeneration(generation Generation) Generation {
	generation.Datasets = slices.Clone(generation.Datasets)
	generation.Events = slices.Clone(generation.Events)
	if generation.Preview != nil {
		preview := *generation.Preview
		preview.Datasets = slices.Clone(preview.Datasets)
		preview.UnsupportedFiles = slices.Clone(preview.UnsupportedFiles)
		generation.Preview = &preview
	}
	return generation
}
func errorCode(err error) string {
	var failure *Error
	if errors.As(err, &failure) {
		return failure.Code
	}
	return "internal_error"
}
