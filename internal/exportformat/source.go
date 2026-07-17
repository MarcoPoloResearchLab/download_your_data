package exportformat

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var conversationFilenamePattern = regexp.MustCompile(`(?i)(^|/)conversations(?:[-_]?\d+)?\.json$`)

type ConversationSource struct {
	Name string
	Open func() (io.ReadCloser, error)
}

type SourceCollection struct {
	Sources []ConversationSource
	Close   func() error
}

func DiscoverSources(sourcePath string) (*SourceCollection, error) {
	fileInfo, statError := os.Stat(sourcePath)
	if statError != nil {
		return nil, fmt.Errorf("inspect source path: %w", statError)
	}

	if fileInfo.IsDir() {
		return discoverDirectorySources(sourcePath)
	}

	lowerName := strings.ToLower(fileInfo.Name())
	if strings.HasSuffix(lowerName, ".zip") {
		return discoverZipSources(sourcePath)
	}
	if strings.HasSuffix(lowerName, ".json") {
		absolutePath, pathError := filepath.Abs(sourcePath)
		if pathError != nil {
			return nil, fmt.Errorf("resolve JSON path: %w", pathError)
		}
		return &SourceCollection{
			Sources: []ConversationSource{{
				Name: filepath.Base(absolutePath),
				Open: func() (io.ReadCloser, error) {
					return os.Open(absolutePath)
				},
			}},
			Close: func() error { return nil },
		}, nil
	}

	return nil, fmt.Errorf("source must be a ChatGPT export ZIP, a conversation JSON file, or a directory")
}

func discoverDirectorySources(directoryPath string) (*SourceCollection, error) {
	entries, readError := os.ReadDir(directoryPath)
	if readError != nil {
		return nil, fmt.Errorf("read source directory: %w", readError)
	}

	matchingPaths := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if conversationFilenamePattern.MatchString(entry.Name()) {
			matchingPaths = append(matchingPaths, filepath.Join(directoryPath, entry.Name()))
		}
	}
	sort.Strings(matchingPaths)
	if len(matchingPaths) == 0 {
		return nil, fmt.Errorf("no conversation JSON files found in %s", directoryPath)
	}

	sources := make([]ConversationSource, 0, len(matchingPaths))
	for _, matchingPath := range matchingPaths {
		resolvedPath := matchingPath
		sources = append(sources, ConversationSource{
			Name: filepath.Base(resolvedPath),
			Open: func() (io.ReadCloser, error) {
				return os.Open(resolvedPath)
			},
		})
	}

	return &SourceCollection{Sources: sources, Close: func() error { return nil }}, nil
}

func discoverZipSources(zipPath string) (*SourceCollection, error) {
	zipReader, openError := zip.OpenReader(zipPath)
	if openError != nil {
		return nil, fmt.Errorf("open ZIP export: %w", openError)
	}

	matchingFiles := make([]*zip.File, 0)
	for _, zipFile := range zipReader.File {
		if zipFile.FileInfo().IsDir() {
			continue
		}
		if conversationFilenamePattern.MatchString(filepath.ToSlash(zipFile.Name)) {
			matchingFiles = append(matchingFiles, zipFile)
		}
	}
	sort.Slice(matchingFiles, func(leftIndex int, rightIndex int) bool {
		return matchingFiles[leftIndex].Name < matchingFiles[rightIndex].Name
	})
	if len(matchingFiles) == 0 {
		zipReader.Close()
		return nil, fmt.Errorf("no conversations.json or numbered conversation JSON files found in ZIP")
	}

	sources := make([]ConversationSource, 0, len(matchingFiles))
	for _, matchingFile := range matchingFiles {
		zipEntry := matchingFile
		sources = append(sources, ConversationSource{
			Name: zipEntry.Name,
			Open: func() (io.ReadCloser, error) {
				return zipEntry.Open()
			},
		})
	}

	return &SourceCollection{Sources: sources, Close: zipReader.Close}, nil
}

func StreamConversations(reader io.Reader, handler func(RawConversation) error) error {
	bufferedReader := bufio.NewReaderSize(reader, 1024*1024)
	decoder := json.NewDecoder(bufferedReader)

	firstToken, tokenError := decoder.Token()
	if tokenError != nil {
		return fmt.Errorf("read top-level JSON token: %w", tokenError)
	}
	delimiter, isDelimiter := firstToken.(json.Delim)
	if !isDelimiter || delimiter != '[' {
		return fmt.Errorf("conversation JSON must contain a top-level array")
	}

	for decoder.More() {
		var rawConversation RawConversation
		if decodeError := decoder.Decode(&rawConversation); decodeError != nil {
			return fmt.Errorf("decode conversation: %w", decodeError)
		}
		if handlerError := handler(rawConversation); handlerError != nil {
			return handlerError
		}
	}

	closingToken, closingError := decoder.Token()
	if closingError != nil {
		return fmt.Errorf("read closing JSON token: %w", closingError)
	}
	closingDelimiter, isClosingDelimiter := closingToken.(json.Delim)
	if !isClosingDelimiter || closingDelimiter != ']' {
		return fmt.Errorf("conversation JSON array was not closed")
	}
	return nil
}
