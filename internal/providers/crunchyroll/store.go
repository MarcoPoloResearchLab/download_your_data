package crunchyroll

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"unicode/utf8"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/privatepath"
)

const (
	storeContract     = "crunchyroll-profile-history-v1"
	providerDirectory = "providers/crunchyroll"
	providerStatePath = providerDirectory + "/library.json"
	providerLeasePath = providerDirectory + "/library.lock"
	maxStateBytes     = 4 * MaxUploadBytes
)

type libraryState struct {
	Contract string      `json:"contract"`
	Revision string      `json:"revision"`
	Rows     []sourceRow `json:"rows"`
}

// ErrInvalidPersistence identifies data that violates the current stored contract.
var ErrInvalidPersistence = errors.New("invalid Crunchyroll persisted state")

// Read loads one fully committed provider snapshot.
func Read(ctx context.Context, root privatepath.Root) (Archive, error) {
	if err := ctx.Err(); err != nil {
		return Archive{}, err
	}
	file, err := root.File(providerStatePath)
	if err != nil {
		return Archive{}, fmt.Errorf("resolve Crunchyroll state: %w", err)
	}
	handle, err := os.Open(file.Path())
	if errors.Is(err, os.ErrNotExist) {
		return Archive{}, nil
	}
	if err != nil {
		return Archive{}, fmt.Errorf("open Crunchyroll state: %w", err)
	}
	encoded, readError := io.ReadAll(io.LimitReader(handle, maxStateBytes+1))
	if err = errors.Join(readError, handle.Close()); err != nil {
		return Archive{}, fmt.Errorf("read Crunchyroll state: %w", err)
	}
	if len(encoded) > maxStateBytes {
		return Archive{}, fmt.Errorf("%w: state exceeds its size bound", ErrInvalidPersistence)
	}
	if !utf8.Valid(encoded) {
		return Archive{}, fmt.Errorf("%w: invalid UTF-8", ErrInvalidPersistence)
	}
	var state libraryState
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&state); err != nil {
		return Archive{}, fmt.Errorf("%w: decode JSON: %w", ErrInvalidPersistence, err)
	}
	var extra any
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Archive{}, fmt.Errorf("%w: trailing data", ErrInvalidPersistence)
	}
	if state.Contract != storeContract || state.Rows == nil || len(state.Rows) > MaxRows {
		return Archive{}, fmt.Errorf("%w: invalid current contract", ErrInvalidPersistence)
	}
	if len(state.Rows) == 0 {
		if state.Revision != EmptyRevision {
			return Archive{}, fmt.Errorf("%w: invalid empty revision", ErrInvalidPersistence)
		}
		return Archive{}, nil
	}
	for index := range state.Rows {
		if err = ctx.Err(); err != nil {
			return Archive{}, err
		}
		if err = state.Rows[index].validate(index + 2); err != nil {
			return Archive{}, fmt.Errorf("%w: %w", ErrInvalidPersistence, err)
		}
	}
	archive, err := buildArchive(ctx, state.Rows)
	if err != nil {
		return Archive{}, err
	}
	if archive.Snapshot().Revision != state.Revision {
		return Archive{}, fmt.Errorf("%w: source revision mismatch", ErrInvalidPersistence)
	}
	return archive, nil
}

// Replace atomically activates a validated source while holding its writer lease.
func Replace(ctx context.Context, root privatepath.Root, expected string, archive Archive) (Snapshot, error) {
	if len(archive.rows) == 0 {
		return Snapshot{}, invalidInput(2, "at least one history entry is required")
	}
	if err := writeState(ctx, root, expected, archive); err != nil {
		return Snapshot{}, err
	}
	return archive.Snapshot(), nil
}

// Delete removes every source record under the current revision precondition.
func Delete(ctx context.Context, root privatepath.Root, expected string) error {
	return writeState(ctx, root, expected, Archive{})
}

func writeState(ctx context.Context, root privatepath.Root, expected string, archive Archive) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	lease, err := claimWriter(root)
	if err != nil {
		return err
	}
	defer func() {
		result = errors.Join(result, syscall.Flock(int(lease.Fd()), syscall.LOCK_UN), lease.Close())
	}()
	active, err := Read(ctx, root)
	if err != nil {
		return err
	}
	if active.Snapshot().Revision != expected {
		return ErrStaleRevision
	}
	if active.Snapshot().Revision == archive.Snapshot().Revision {
		return nil
	}
	state := libraryState{Contract: storeContract, Revision: archive.Snapshot().Revision, Rows: archive.rows}
	if state.Rows == nil {
		state.Rows = []sourceRow{}
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode Crunchyroll state: %w", err)
	}
	if len(encoded) > maxStateBytes {
		return ErrTooLarge
	}
	file, err := root.File(providerStatePath)
	if err != nil {
		return fmt.Errorf("resolve Crunchyroll replacement: %w", err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = file.Replace(func(writer io.Writer) error {
		_, writeError := writer.Write(encoded)
		return errors.Join(writeError, ctx.Err())
	}); err != nil {
		return fmt.Errorf("persist Crunchyroll replacement: %w", err)
	}
	return nil
}

func claimWriter(root privatepath.Root) (*os.File, error) {
	file, err := root.File(providerLeasePath)
	if err != nil {
		return nil, fmt.Errorf("resolve Crunchyroll writer lease: %w", err)
	}
	if err = file.Prepare(); err != nil {
		return nil, fmt.Errorf("prepare Crunchyroll writer lease: %w", err)
	}
	handle, err := os.OpenFile(file.Path(), os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open Crunchyroll writer lease: %w", err)
	}
	if err = syscall.Flock(int(handle.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		closeError := handle.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, errors.Join(ErrBusy, closeError)
		}
		return nil, fmt.Errorf("claim Crunchyroll writer lease: %w", errors.Join(err, closeError))
	}
	return handle, nil
}
