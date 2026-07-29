package privatepath_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/privatepath"
)

func TestRootCreatesConfinedOwnerOnlyDirectoriesAndFiles(testContext *testing.T) {
	rootPath := filepath.Join(testContext.TempDir(), "application-data")
	root, rootError := privatepath.NewRoot(rootPath)
	if rootError != nil {
		testContext.Fatalf("create private root: %v", rootError)
	}
	assertPermissions(testContext, root.Path(), 0o700)

	directory, directoryError := root.EnsureDirectory(filepath.Join("providers", "netflix", "generations", "generation-1"))
	if directoryError != nil {
		testContext.Fatalf("create generation directory: %v", directoryError)
	}
	assertPermissions(testContext, directory.Path(), 0o700)

	privateFile, fileError := root.File(filepath.Join("providers", "netflix", "generations", "generation-1", "state.db"))
	if fileError != nil {
		testContext.Fatalf("resolve private file: %v", fileError)
	}
	if prepareError := privateFile.Prepare(); prepareError != nil {
		testContext.Fatalf("prepare private file: %v", prepareError)
	}
	assertPermissions(testContext, privateFile.Path(), 0o600)
}

func TestRootRejectsUnsafeAndEscapedPaths(testContext *testing.T) {
	if _, rootError := privatepath.NewRoot("relative/data"); rootError == nil ||
		!strings.Contains(rootError.Error(), "must be absolute") {
		testContext.Fatalf("relative root should be rejected: %v", rootError)
	}
	if _, rootError := privatepath.NewRoot(string(filepath.Separator)); rootError == nil ||
		!strings.Contains(rootError.Error(), "filesystem roots are not allowed") {
		testContext.Fatalf("filesystem root should be rejected: %v", rootError)
	}

	permissiveRoot := filepath.Join(testContext.TempDir(), "permissive")
	if createError := os.Mkdir(permissiveRoot, 0o755); createError != nil {
		testContext.Fatalf("create permissive root fixture: %v", createError)
	}
	if chmodError := os.Chmod(permissiveRoot, 0o755); chmodError != nil {
		testContext.Fatalf("set permissive root fixture mode: %v", chmodError)
	}
	if _, rootError := privatepath.NewRoot(permissiveRoot); rootError == nil ||
		!strings.Contains(rootError.Error(), "permissions must be 0700") {
		testContext.Fatalf("permissive root should be rejected: %v", rootError)
	}

	root, rootError := privatepath.NewRoot(filepath.Join(testContext.TempDir(), "private"))
	if rootError != nil {
		testContext.Fatalf("create private root: %v", rootError)
	}
	for _, escapedPath := range []string{"", ".", "..", filepath.Join("..", "outside.db"), "/outside.db"} {
		if _, fileError := root.File(escapedPath); fileError == nil {
			testContext.Errorf("escaped path %q should be rejected", escapedPath)
		}
	}
}

func TestRootRejectsSymbolicLinkTraversal(testContext *testing.T) {
	root, rootError := privatepath.NewRoot(filepath.Join(testContext.TempDir(), "private"))
	if rootError != nil {
		testContext.Fatalf("create private root: %v", rootError)
	}
	outsideDirectory := testContext.TempDir()
	linkPath := filepath.Join(root.Path(), "escaped")
	if symlinkError := os.Symlink(outsideDirectory, linkPath); symlinkError != nil {
		testContext.Fatalf("create symbolic link fixture: %v", symlinkError)
	}
	if _, fileError := root.File(filepath.Join("escaped", "payload.db")); fileError == nil ||
		!strings.Contains(fileError.Error(), "symbolic links are not allowed") {
		testContext.Fatalf("symbolic-link traversal should be rejected: %v", fileError)
	}
}

func TestFileReplacePublishesOnlyCompleteOwnerOnlyContents(testContext *testing.T) {
	root, rootError := privatepath.NewRoot(filepath.Join(testContext.TempDir(), "private"))
	if rootError != nil {
		testContext.Fatalf("create private root: %v", rootError)
	}
	privateFile, fileError := root.File(filepath.Join("reports", "netflix.csv"))
	if fileError != nil {
		testContext.Fatalf("resolve private report: %v", fileError)
	}
	if replaceError := privateFile.Replace(func(destination io.Writer) error {
		_, writeError := io.WriteString(destination, "complete")
		return writeError
	}); replaceError != nil {
		testContext.Fatalf("publish private report: %v", replaceError)
	}
	assertPermissions(testContext, privateFile.Path(), 0o600)
	if contents, readError := os.ReadFile(privateFile.Path()); readError != nil ||
		string(contents) != "complete" {
		testContext.Fatalf("published contents = %q error=%v", contents, readError)
	}

	syntheticError := errors.New("synthetic write failure")
	replaceError := privateFile.Replace(func(destination io.Writer) error {
		if _, writeError := io.WriteString(destination, "partial"); writeError != nil {
			return writeError
		}
		return syntheticError
	})
	if !errors.Is(replaceError, syntheticError) {
		testContext.Fatalf("replacement failure = %v; want %v", replaceError, syntheticError)
	}
	if contents, readError := os.ReadFile(privateFile.Path()); readError != nil ||
		string(contents) != "complete" {
		testContext.Fatalf("failed replacement changed contents = %q error=%v", contents, readError)
	}
	temporaryMatches, matchError := filepath.Glob(
		filepath.Join(filepath.Dir(privateFile.Path()), ".netflix.csv.*.next"),
	)
	if matchError != nil || len(temporaryMatches) != 0 {
		testContext.Fatalf(
			"failed replacement left temporary files = %v error=%v",
			temporaryMatches,
			matchError,
		)
	}
}

func assertPermissions(testContext *testing.T, path string, expected os.FileMode) {
	testContext.Helper()
	pathInfo, statError := os.Stat(path)
	if statError != nil {
		testContext.Fatalf("inspect %s: %v", path, statError)
	}
	if pathInfo.Mode().Perm() != expected {
		testContext.Fatalf("%s permissions = %04o; want %04o", path, pathInfo.Mode().Perm(), expected)
	}
}
