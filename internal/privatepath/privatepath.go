// Package privatepath confines application-owned files and directories beneath one private root.
package privatepath

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	privateDirectoryMode = 0o700
	privateFileMode      = 0o600
)

// Root is a validated owner-only application data directory.
type Root struct {
	path string
}

// Directory is a validated owner-only directory beneath a Root.
type Directory struct {
	path string
}

// File is a validated application file location beneath a Root.
type File struct {
	root         Root
	path         string
	relativePath string
}

// NewRoot validates or creates an absolute owner-only data root.
func NewRoot(rootPath string) (Root, error) {
	trimmedPath := strings.TrimSpace(rootPath)
	if trimmedPath == "" {
		return Root{}, fmt.Errorf("validate private data root: path is required")
	}
	if !filepath.IsAbs(trimmedPath) {
		return Root{}, fmt.Errorf("validate private data root %q: path must be absolute", rootPath)
	}
	cleanedPath := filepath.Clean(trimmedPath)
	if isVolumeRoot(cleanedPath) {
		return Root{}, fmt.Errorf("validate private data root %q: filesystem roots are not allowed", cleanedPath)
	}

	pathInfo, statError := os.Lstat(cleanedPath)
	switch {
	case statError == nil:
		if validationError := validatePrivateDirectory(cleanedPath, pathInfo); validationError != nil {
			return Root{}, validationError
		}
	case errors.Is(statError, os.ErrNotExist):
		if createError := os.MkdirAll(cleanedPath, privateDirectoryMode); createError != nil {
			return Root{}, fmt.Errorf("create private data root %q: %w", cleanedPath, createError)
		}
		if chmodError := os.Chmod(cleanedPath, privateDirectoryMode); chmodError != nil {
			return Root{}, fmt.Errorf("set private data root permissions %q: %w", cleanedPath, chmodError)
		}
	default:
		return Root{}, fmt.Errorf("inspect private data root %q: %w", cleanedPath, statError)
	}

	resolvedPath, resolveError := filepath.EvalSymlinks(cleanedPath)
	if resolveError != nil {
		return Root{}, fmt.Errorf("resolve private data root %q: %w", cleanedPath, resolveError)
	}
	resolvedPath, absoluteError := filepath.Abs(resolvedPath)
	if absoluteError != nil {
		return Root{}, fmt.Errorf("normalize private data root %q: %w", resolvedPath, absoluteError)
	}
	resolvedPath = filepath.Clean(resolvedPath)
	if isVolumeRoot(resolvedPath) {
		return Root{}, fmt.Errorf("validate private data root %q: filesystem roots are not allowed", resolvedPath)
	}
	resolvedInfo, resolvedStatError := os.Lstat(resolvedPath)
	if resolvedStatError != nil {
		return Root{}, fmt.Errorf("inspect resolved private data root %q: %w", resolvedPath, resolvedStatError)
	}
	if validationError := validatePrivateDirectory(resolvedPath, resolvedInfo); validationError != nil {
		return Root{}, validationError
	}
	return Root{path: resolvedPath}, nil
}

// Path returns the canonical absolute data-root path.
func (root Root) Path() string {
	return root.path
}

// File returns a confined file location without creating the file.
func (root Root) File(relativePath string) (File, error) {
	resolvedPath, cleanedRelativePath, resolveError := root.resolve(relativePath)
	if resolveError != nil {
		return File{}, resolveError
	}
	if validationError := root.validateExistingComponents(cleanedRelativePath, false); validationError != nil {
		return File{}, validationError
	}
	return File{
		root:         root,
		path:         resolvedPath,
		relativePath: cleanedRelativePath,
	}, nil
}

// EnsureDirectory validates or creates an owner-only directory beneath the root.
func (root Root) EnsureDirectory(relativePath string) (Directory, error) {
	if root.path == "" {
		return Directory{}, fmt.Errorf("create private directory: data root is not initialized")
	}
	cleanedRelativePath := filepath.Clean(strings.TrimSpace(relativePath))
	if cleanedRelativePath == "." || cleanedRelativePath == "" {
		return Directory(root), nil
	}
	if _, _, resolveError := root.resolve(cleanedRelativePath); resolveError != nil {
		return Directory{}, resolveError
	}

	currentPath := root.path
	for _, pathPart := range splitRelativePath(cleanedRelativePath) {
		currentPath = filepath.Join(currentPath, pathPart)
		pathInfo, statError := os.Lstat(currentPath)
		switch {
		case statError == nil:
			if validationError := validatePrivateDirectory(currentPath, pathInfo); validationError != nil {
				return Directory{}, validationError
			}
		case errors.Is(statError, os.ErrNotExist):
			if createError := os.Mkdir(currentPath, privateDirectoryMode); createError != nil {
				return Directory{}, fmt.Errorf("create private directory %q: %w", currentPath, createError)
			}
		default:
			return Directory{}, fmt.Errorf("inspect private directory %q: %w", currentPath, statError)
		}
	}
	return Directory{path: currentPath}, nil
}

// RemoveDirectory validates and recursively removes one exact owner-only
// directory beneath the root. It never removes the root itself and rejects
// symbolic links and foreign filesystem object types before deletion.
func (root Root) RemoveDirectory(relativePath string) error {
	resolvedPath, cleanedRelativePath, resolveError := root.resolve(relativePath)
	if resolveError != nil {
		return resolveError
	}
	if validationError := root.validateExistingComponents(
		cleanedRelativePath,
		true,
	); validationError != nil {
		return validationError
	}
	pathInfo, statError := os.Lstat(resolvedPath)
	if errors.Is(statError, os.ErrNotExist) {
		return nil
	}
	if statError != nil {
		return fmt.Errorf("inspect private directory %q: %w", resolvedPath, statError)
	}
	if validationError := validatePrivateDirectory(resolvedPath, pathInfo); validationError != nil {
		return validationError
	}
	walkError := filepath.WalkDir(
		resolvedPath,
		func(path string, entry fs.DirEntry, receivedError error) error {
			if receivedError != nil {
				return receivedError
			}
			entryInfo, infoError := entry.Info()
			if infoError != nil {
				return infoError
			}
			switch {
			case entryInfo.Mode()&os.ModeSymlink != 0:
				return fmt.Errorf("validate private deletion path %q: symbolic links are not allowed", path)
			case entryInfo.IsDir():
				if entryInfo.Mode().Perm() != privateDirectoryMode {
					return fmt.Errorf(
						"validate private deletion directory %q: permissions must be 0700, received %04o",
						path,
						entryInfo.Mode().Perm(),
					)
				}
			case entryInfo.Mode().IsRegular():
				if entryInfo.Mode().Perm() != privateFileMode {
					return fmt.Errorf(
						"validate private deletion file %q: permissions must be 0600, received %04o",
						path,
						entryInfo.Mode().Perm(),
					)
				}
			default:
				return fmt.Errorf(
					"validate private deletion path %q: only regular files and directories are allowed",
					path,
				)
			}
			return nil
		},
	)
	if walkError != nil {
		return fmt.Errorf("validate private directory deletion %q: %w", resolvedPath, walkError)
	}
	if removeError := os.RemoveAll(resolvedPath); removeError != nil {
		return fmt.Errorf("remove private directory %q: %w", resolvedPath, removeError)
	}
	parentHandle, openError := os.Open(filepath.Dir(resolvedPath))
	if openError != nil {
		return fmt.Errorf("open private deletion parent %q: %w", filepath.Dir(resolvedPath), openError)
	}
	if syncError := parentHandle.Sync(); syncError != nil {
		_ = parentHandle.Close()
		return fmt.Errorf("sync private deletion parent %q: %w", filepath.Dir(resolvedPath), syncError)
	}
	if closeError := parentHandle.Close(); closeError != nil {
		return fmt.Errorf("close private deletion parent %q: %w", filepath.Dir(resolvedPath), closeError)
	}
	return nil
}

// Path returns the canonical absolute directory path.
func (directory Directory) Path() string {
	return directory.path
}

// Path returns the canonical absolute file path.
func (file File) Path() string {
	return file.path
}

// RelativePath returns the file path relative to its private root.
func (file File) RelativePath() string {
	return file.relativePath
}

// Sibling resolves another file relative to this file's directory.
func (file File) Sibling(relativePath string) (File, error) {
	if file.root.path == "" || file.relativePath == "" {
		return File{}, fmt.Errorf("resolve private sibling: file is not initialized")
	}
	if filepath.IsAbs(relativePath) {
		return File{}, fmt.Errorf("resolve private sibling %q: path must be relative", relativePath)
	}
	return file.root.File(filepath.Join(filepath.Dir(file.relativePath), relativePath))
}

// Prepare validates the file and creates it with owner-only permissions when absent.
func (file File) Prepare() error {
	if file.root.path == "" || file.path == "" || file.relativePath == "" {
		return fmt.Errorf("prepare private file: file is not initialized")
	}
	parentRelativePath := filepath.Dir(file.relativePath)
	if _, directoryError := file.root.EnsureDirectory(parentRelativePath); directoryError != nil {
		return directoryError
	}

	pathInfo, statError := os.Lstat(file.path)
	switch {
	case statError == nil:
		if pathInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("validate private file %q: symbolic links are not allowed", file.path)
		}
		if !pathInfo.Mode().IsRegular() {
			return fmt.Errorf("validate private file %q: path must be a regular file", file.path)
		}
		if pathInfo.Mode().Perm() != privateFileMode {
			return fmt.Errorf(
				"validate private file %q: permissions must be 0600, received %04o",
				file.path,
				pathInfo.Mode().Perm(),
			)
		}
		return nil
	case !errors.Is(statError, os.ErrNotExist):
		return fmt.Errorf("inspect private file %q: %w", file.path, statError)
	}

	fileHandle, createError := os.OpenFile(file.path, os.O_CREATE|os.O_EXCL|os.O_RDWR, privateFileMode)
	if createError != nil {
		return fmt.Errorf("create private file %q: %w", file.path, createError)
	}
	if closeError := fileHandle.Close(); closeError != nil {
		return fmt.Errorf("close new private file %q: %w", file.path, closeError)
	}
	return nil
}

// Replace atomically publishes owner-only file contents after the complete
// writer callback succeeds.
func (file File) Replace(writeContents func(io.Writer) error) error {
	if file.root.path == "" || file.path == "" || file.relativePath == "" {
		return fmt.Errorf("replace private file: file is not initialized")
	}
	if writeContents == nil {
		return fmt.Errorf("replace private file %q: writer is required", file.relativePath)
	}
	parentRelativePath := filepath.Dir(file.relativePath)
	if _, directoryError := file.root.EnsureDirectory(parentRelativePath); directoryError != nil {
		return directoryError
	}
	if validationError := file.validateExisting(); validationError != nil {
		return validationError
	}

	temporaryHandle, createError := os.CreateTemp(
		filepath.Dir(file.path),
		"."+filepath.Base(file.path)+".*.next",
	)
	if createError != nil {
		return fmt.Errorf("create private replacement for %q: %w", file.path, createError)
	}
	temporaryPath := temporaryHandle.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if chmodError := temporaryHandle.Chmod(privateFileMode); chmodError != nil {
		_ = temporaryHandle.Close()
		return fmt.Errorf("set private replacement permissions %q: %w", temporaryPath, chmodError)
	}
	if writeError := writeContents(temporaryHandle); writeError != nil {
		_ = temporaryHandle.Close()
		return fmt.Errorf("write private replacement %q: %w", file.relativePath, writeError)
	}
	if syncError := temporaryHandle.Sync(); syncError != nil {
		_ = temporaryHandle.Close()
		return fmt.Errorf("sync private replacement %q: %w", file.relativePath, syncError)
	}
	if closeError := temporaryHandle.Close(); closeError != nil {
		return fmt.Errorf("close private replacement %q: %w", file.relativePath, closeError)
	}
	if renameError := os.Rename(temporaryPath, file.path); renameError != nil {
		return fmt.Errorf("publish private replacement %q: %w", file.relativePath, renameError)
	}
	removeTemporary = false

	parentHandle, openError := os.Open(filepath.Dir(file.path))
	if openError != nil {
		return fmt.Errorf("open private replacement directory %q: %w", parentRelativePath, openError)
	}
	if syncError := parentHandle.Sync(); syncError != nil {
		_ = parentHandle.Close()
		return fmt.Errorf("sync private replacement directory %q: %w", parentRelativePath, syncError)
	}
	if closeError := parentHandle.Close(); closeError != nil {
		return fmt.Errorf("close private replacement directory %q: %w", parentRelativePath, closeError)
	}
	return nil
}

func (file File) validateExisting() error {
	pathInfo, statError := os.Lstat(file.path)
	switch {
	case errors.Is(statError, os.ErrNotExist):
		return nil
	case statError != nil:
		return fmt.Errorf("inspect private file %q: %w", file.path, statError)
	case pathInfo.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("validate private file %q: symbolic links are not allowed", file.path)
	case !pathInfo.Mode().IsRegular():
		return fmt.Errorf("validate private file %q: path must be a regular file", file.path)
	case pathInfo.Mode().Perm() != privateFileMode:
		return fmt.Errorf(
			"validate private file %q: permissions must be 0600, received %04o",
			file.path,
			pathInfo.Mode().Perm(),
		)
	default:
		return nil
	}
}

func (root Root) resolve(relativePath string) (string, string, error) {
	if root.path == "" {
		return "", "", fmt.Errorf("resolve private path: data root is not initialized")
	}
	trimmedPath := strings.TrimSpace(relativePath)
	if trimmedPath == "" {
		return "", "", fmt.Errorf("resolve private path: relative path is required")
	}
	if filepath.IsAbs(trimmedPath) {
		return "", "", fmt.Errorf("resolve private path %q: absolute paths are not allowed", relativePath)
	}
	cleanedRelativePath := filepath.Clean(trimmedPath)
	if cleanedRelativePath == "." ||
		cleanedRelativePath == ".." ||
		strings.HasPrefix(cleanedRelativePath, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("resolve private path %q: path escapes the data root", relativePath)
	}
	resolvedPath := filepath.Join(root.path, cleanedRelativePath)
	pathWithinRoot, relativeError := filepath.Rel(root.path, resolvedPath)
	if relativeError != nil {
		return "", "", fmt.Errorf("resolve private path %q: %w", relativePath, relativeError)
	}
	if pathWithinRoot == ".." || strings.HasPrefix(pathWithinRoot, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("resolve private path %q: path escapes the data root", relativePath)
	}
	return resolvedPath, cleanedRelativePath, nil
}

func (root Root) validateExistingComponents(relativePath string, finalMustBeDirectory bool) error {
	currentPath := root.path
	pathParts := splitRelativePath(relativePath)
	for pathIndex, pathPart := range pathParts {
		currentPath = filepath.Join(currentPath, pathPart)
		pathInfo, statError := os.Lstat(currentPath)
		if errors.Is(statError, os.ErrNotExist) {
			return nil
		}
		if statError != nil {
			return fmt.Errorf("inspect private path component %q: %w", currentPath, statError)
		}
		if pathInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("validate private path component %q: symbolic links are not allowed", currentPath)
		}
		isFinalPart := pathIndex == len(pathParts)-1
		if !isFinalPart || finalMustBeDirectory {
			if !pathInfo.IsDir() {
				return fmt.Errorf("validate private path component %q: expected a directory", currentPath)
			}
			if pathInfo.Mode().Perm() != privateDirectoryMode {
				return fmt.Errorf(
					"validate private directory %q: permissions must be 0700, received %04o",
					currentPath,
					pathInfo.Mode().Perm(),
				)
			}
		}
	}
	return nil
}

func validatePrivateDirectory(path string, pathInfo os.FileInfo) error {
	if pathInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("validate private directory %q: symbolic links are not allowed", path)
	}
	if !pathInfo.IsDir() {
		return fmt.Errorf("validate private directory %q: path must be a directory", path)
	}
	if pathInfo.Mode().Perm() != privateDirectoryMode {
		return fmt.Errorf(
			"validate private directory %q: permissions must be 0700, received %04o",
			path,
			pathInfo.Mode().Perm(),
		)
	}
	return nil
}

func splitRelativePath(relativePath string) []string {
	return strings.FieldsFunc(relativePath, func(character rune) bool {
		return character == filepath.Separator
	})
}

func isVolumeRoot(path string) bool {
	volumeName := filepath.VolumeName(path)
	return filepath.Clean(path) == filepath.Clean(volumeName+string(filepath.Separator))
}
