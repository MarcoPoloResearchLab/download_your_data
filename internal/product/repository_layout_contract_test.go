package product

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestRepositoryLayoutKeepsApplicationSourceOutOfTheRoot(testContext *testing.T) {
	_, currentFile, _, currentFileOK := runtime.Caller(0)
	if !currentFileOK {
		testContext.Fatal("resolve repository layout test path")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	rootEntries, readError := os.ReadDir(repositoryRoot)
	if readError != nil {
		testContext.Fatalf("read repository root: %v", readError)
	}

	allowedRootFiles := []string{
		".gitignore",
		"AGENTS.md",
		"CHANGELOG.md",
		"LICENSE",
		"Makefile",
		"README.md",
		"go.mod",
		"go.sum",
	}
	for _, entry := range rootEntries {
		if entry.IsDir() {
			continue
		}
		if !slices.Contains(allowedRootFiles, entry.Name()) {
			testContext.Errorf("application or unowned file remains at repository root: %s", entry.Name())
		}
	}

	requiredDirectories := []string{
		"cmd/download-your-data",
		"frontend/application",
		"frontend/content",
		"frontend/images",
		"frontend/manifests",
		"frontend/styles",
		"internal/httpapi",
	}
	for _, relativePath := range requiredDirectories {
		info, statError := os.Stat(filepath.Join(repositoryRoot, relativePath))
		if statError != nil {
			testContext.Errorf("required source directory %s is unavailable: %v", relativePath, statError)
			continue
		}
		if !info.IsDir() {
			testContext.Errorf("required source path %s is not a directory", relativePath)
		}
	}
}
