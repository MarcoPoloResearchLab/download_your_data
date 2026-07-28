package intent

import (
	"path/filepath"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/inference"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/privatepath"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/store"
)

func openIntentTestStore(testContext *testing.T) *store.Store {
	testContext.Helper()
	root, rootError := privatepath.NewRoot(filepath.Join(testContext.TempDir(), "data"))
	if rootError != nil {
		testContext.Fatalf("create private test root: %v", rootError)
	}
	databaseFile, fileError := root.File("archive.db")
	if fileError != nil {
		testContext.Fatalf("resolve private test database: %v", fileError)
	}
	openedStore, openError := store.Open(databaseFile)
	if openError != nil {
		testContext.Fatalf("open store: %v", openError)
	}
	return openedStore
}

func mustIntentBaseURL(testContext *testing.T, value string) inference.BaseURL {
	testContext.Helper()
	baseURL, baseURLError := inference.NewBaseURL(value)
	if baseURLError != nil {
		testContext.Fatalf("create intent inference URL: %v", baseURLError)
	}
	return baseURL
}
