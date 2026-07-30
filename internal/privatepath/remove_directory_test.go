package privatepath_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/privatepath"
)

func TestRootRemoveDirectoryDeletesOnlyTheValidatedChild(testContext *testing.T) {
	rootPath := filepath.Join(testContext.TempDir(), "private")
	root, rootError := privatepath.NewRoot(rootPath)
	if rootError != nil {
		testContext.Fatalf("create private root: %v", rootError)
	}
	targetFile, targetError := root.File("users/target/provider/state.json")
	if targetError != nil {
		testContext.Fatalf("resolve target file: %v", targetError)
	}
	if replaceError := targetFile.Replace(func(destination io.Writer) error {
		_, writeError := destination.Write([]byte("{}"))
		return writeError
	}); replaceError != nil {
		testContext.Fatalf("write target file: %v", replaceError)
	}
	otherFile, otherError := root.File("users/other/provider/state.json")
	if otherError != nil {
		testContext.Fatalf("resolve other file: %v", otherError)
	}
	if prepareError := otherFile.Prepare(); prepareError != nil {
		testContext.Fatalf("prepare other file: %v", prepareError)
	}

	if removeError := root.RemoveDirectory("users/target"); removeError != nil {
		testContext.Fatalf("remove target directory: %v", removeError)
	}
	if _, statError := os.Stat(filepath.Join(rootPath, "users", "target")); !errors.Is(
		statError,
		os.ErrNotExist,
	) {
		testContext.Fatalf("target directory remains: %v", statError)
	}
	if _, statError := os.Stat(otherFile.Path()); statError != nil {
		testContext.Fatalf("sibling user data was removed: %v", statError)
	}
	if removeError := root.RemoveDirectory("users/target"); removeError != nil {
		testContext.Fatalf("repeat missing-directory deletion: %v", removeError)
	}
}

func TestRootRemoveDirectoryRejectsBroadAndForeignPaths(testContext *testing.T) {
	rootPath := filepath.Join(testContext.TempDir(), "private")
	root, rootError := privatepath.NewRoot(rootPath)
	if rootError != nil {
		testContext.Fatalf("create private root: %v", rootError)
	}
	if removeError := root.RemoveDirectory("."); removeError == nil {
		testContext.Fatalf("private root deletion was accepted")
	}
	target, targetError := root.EnsureDirectory("users/target")
	if targetError != nil {
		testContext.Fatalf("create target directory: %v", targetError)
	}
	symlinkPath := filepath.Join(target.Path(), "foreign")
	if symlinkError := os.Symlink(testContext.TempDir(), symlinkPath); symlinkError != nil {
		testContext.Fatalf("create foreign symlink: %v", symlinkError)
	}
	if removeError := root.RemoveDirectory("users/target"); removeError == nil {
		testContext.Fatalf("directory containing a symbolic link was removed")
	}
	if _, statError := os.Lstat(symlinkPath); statError != nil {
		testContext.Fatalf("rejected symbolic link was unexpectedly removed: %v", statError)
	}
}
