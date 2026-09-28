package library

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

const profileLabelFileName = "profile-label.json"
const profileLabelContract = "netflix-import-profile-label-v1"

type profileLabelPayload struct {
	Contract     string `json:"contract"`
	GenerationID string `json:"generation_id"`
	Label        string `json:"label"`
}

func validProfileLabel(label string) bool {
	return len(label) <= 200 && utf8.ValidString(label) && !strings.ContainsAny(label, "\r\n\x00")
}

// SetProfileLabel replaces the optional label attached to one local profile import.
func (workspace *Workspace) SetProfileLabel(id, label string) error {
	if !validProfileLabel(label) {
		return newLibraryError(ErrorInvalidRequest, id, 0, errors.New("profile label is invalid"))
	}
	workspace.mutex.Lock()
	defer workspace.mutex.Unlock()
	generation, found := findGeneration(workspace.repository.state, id)
	if !found {
		return newLibraryError(ErrorNotFound, id, 0, nil)
	}
	if generation.AnalysisLevel != AnalysisLevelLocal || workspace.closing || workspace.repository.state.Deleting {
		return newLibraryError(ErrorConflict, id, 0, nil)
	}
	files, err := resolveGenerationFiles(workspace.root, id)
	if err != nil {
		return err
	}
	file, err := files.records.Sibling(profileLabelFileName)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(profileLabelPayload{Contract: profileLabelContract, GenerationID: id, Label: label})
	if err != nil {
		return err
	}
	return file.Replace(func(writer io.Writer) error { _, err := writer.Write(encoded); return err })
}

// ProfileLabel reads the label of the source import for a local or enriched generation.
func (workspace *Workspace) ProfileLabel(id string) (string, error) {
	workspace.mutex.Lock()
	defer workspace.mutex.Unlock()
	generation, found := findGeneration(workspace.repository.state, id)
	if !found {
		return "", newLibraryError(ErrorNotFound, id, 0, nil)
	}
	for generation.AnalysisLevel == AnalysisLevelTMDB {
		generation, found = findGeneration(workspace.repository.state, generation.SourceGenerationID)
		if !found {
			return "", newLibraryError(ErrorInvalidPersistence, id, 0, nil)
		}
	}
	files, err := resolveGenerationFiles(workspace.root, generation.ID)
	if err != nil {
		return "", err
	}
	file, err := files.records.Sibling(profileLabelFileName)
	if err != nil {
		return "", err
	}
	exists, err := privateFileExists(file)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", nil
	}
	encoded, err := readPrivateFileBounded(file, 1024)
	if err != nil {
		return "", err
	}
	var payload profileLabelPayload
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return "", newLibraryError(ErrorInvalidPersistence, id, 0, err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return "", newLibraryError(ErrorInvalidPersistence, id, 0, err)
	}
	if payload.Contract != profileLabelContract || payload.GenerationID != generation.ID || !validProfileLabel(payload.Label) {
		return "", newLibraryError(ErrorInvalidPersistence, id, 0, nil)
	}
	return payload.Label, nil
}
