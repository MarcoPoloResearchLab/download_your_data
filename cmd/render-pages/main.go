package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"

	"github.com/MarcoPoloResearchLab/download_your_data/frontend"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/productionprofile"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/uiconfig"
)

func main() {
	flags := flag.NewFlagSet("render-pages", flag.ExitOnError)
	profilePath := flags.String("profile", "", "path to the production profile")
	outputRoot := flags.String("output", "", "empty output directory")
	flags.Parse(os.Args[1:])
	if flags.NArg() != 0 || *profilePath == "" || *outputRoot == "" {
		fmt.Fprintln(os.Stderr, "render Pages: --profile and --output are required")
		os.Exit(2)
	}
	if renderError := render(*profilePath, *outputRoot); renderError != nil {
		fmt.Fprintf(os.Stderr, "render Pages: %v\n", renderError)
		os.Exit(1)
	}
}

func render(profilePath string, outputRoot string) error {
	profile, profileError := productionprofile.Load(profilePath)
	if profileError != nil {
		return profileError
	}
	if outputError := requireEmptyOutput(outputRoot); outputError != nil {
		return outputError
	}
	if copyError := copyApplicationAssets(outputRoot); copyError != nil {
		return copyError
	}
	indexDocument, indexError := frontend.RenderApplicationIndex(
		profile.Browser.PublicOrigin,
		profile.Browser.APIOrigin,
		profile.Browser.TAuthOrigin,
	)
	if indexError != nil {
		return indexError
	}
	if writeError := writeOutputFile(outputRoot, "index.html", indexDocument); writeError != nil {
		return writeError
	}
	uiDocument, uiError := uiconfig.Render(profile.UIConfigInput())
	if uiError != nil {
		return uiError
	}
	if writeError := writeOutputFile(outputRoot, "config-ui.yaml", uiDocument); writeError != nil {
		return writeError
	}
	publicSite, siteError := frontend.NewPublicSite(profile.Browser.PublicOrigin)
	if siteError != nil {
		return fmt.Errorf("build public Pages documents: %w", siteError)
	}
	for _, publicPath := range publicSite.Paths() {
		body, _, found := publicSite.Read(publicPath)
		if !found {
			return fmt.Errorf("read rendered public document %s: document is missing", publicPath)
		}
		relativePath, pathError := publicOutputPath(publicPath)
		if pathError != nil {
			return pathError
		}
		if writeError := writeOutputFile(outputRoot, relativePath, body); writeError != nil {
			return writeError
		}
	}
	return nil
}

func requireEmptyOutput(outputRoot string) error {
	if strings.TrimSpace(outputRoot) == "" || !filepath.IsAbs(outputRoot) {
		return errors.New("output directory must be one absolute path")
	}
	entries, readError := os.ReadDir(outputRoot)
	if readError != nil {
		if !errors.Is(readError, os.ErrNotExist) {
			return fmt.Errorf("inspect Pages output %s: %w", outputRoot, readError)
		}
		if createError := os.MkdirAll(outputRoot, 0o755); createError != nil {
			return fmt.Errorf("create Pages output %s: %w", outputRoot, createError)
		}
		return nil
	}
	if len(entries) != 0 {
		return errors.New("pages output directory must be empty")
	}
	return nil
}

func copyApplicationAssets(outputRoot string) error {
	assetFileSystem := frontend.Assets()
	return fs.WalkDir(assetFileSystem, ".", func(sourcePath string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return fmt.Errorf("walk browser asset %s: %w", sourcePath, walkError)
		}
		if sourcePath == "." || sourcePath == "index.html" {
			return nil
		}
		destination := filepath.Join(outputRoot, filepath.FromSlash(sourcePath))
		if entry.IsDir() {
			if createError := os.MkdirAll(destination, 0o755); createError != nil {
				return fmt.Errorf("create browser asset directory %s: %w", sourcePath, createError)
			}
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return fmt.Errorf("copy browser asset %s: only regular files are allowed", sourcePath)
		}
		body, readError := fs.ReadFile(assetFileSystem, sourcePath)
		if readError != nil {
			return fmt.Errorf("read browser asset %s: %w", sourcePath, readError)
		}
		if writeError := os.WriteFile(destination, body, 0o644); writeError != nil {
			return fmt.Errorf("write browser asset %s: %w", sourcePath, writeError)
		}
		return nil
	})
}

func publicOutputPath(publicPath string) (string, error) {
	if !strings.HasPrefix(publicPath, "/") || strings.Contains(publicPath, "\\") {
		return "", fmt.Errorf("map public document %s: path is invalid", publicPath)
	}
	cleanPath := pathpkg.Clean(publicPath)
	if cleanPath != strings.TrimSuffix(publicPath, "/") {
		return "", fmt.Errorf("map public document %s: path is not normalized", publicPath)
	}
	relativePath := strings.TrimPrefix(cleanPath, "/")
	if strings.HasSuffix(publicPath, "/") {
		relativePath = pathpkg.Join(relativePath, "index.html")
	}
	if relativePath == "" || relativePath == "." || strings.HasPrefix(relativePath, "../") {
		return "", fmt.Errorf("map public document %s: path has no output file", publicPath)
	}
	return relativePath, nil
}

func writeOutputFile(outputRoot string, relativePath string, body []byte) error {
	destination := filepath.Join(outputRoot, filepath.FromSlash(relativePath))
	if createError := os.MkdirAll(filepath.Dir(destination), 0o755); createError != nil {
		return fmt.Errorf("create Pages directory for %s: %w", relativePath, createError)
	}
	if writeError := os.WriteFile(destination, body, 0o644); writeError != nil {
		return fmt.Errorf("write Pages file %s: %w", relativePath, writeError)
	}
	return nil
}
