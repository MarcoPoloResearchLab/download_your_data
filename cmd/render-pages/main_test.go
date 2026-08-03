package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/frontend"
)

func TestRenderBuildsTheCompleteProductionPagesArtifact(testContext *testing.T) {
	outputRoot := filepath.Join(testContext.TempDir(), "pages")
	profilePath := filepath.Join("..", "..", "configs", "production.yml")
	if renderError := render(profilePath, outputRoot); renderError != nil {
		testContext.Fatalf("render production Pages artifact: %v", renderError)
	}

	index := readRenderedFile(testContext, outputRoot, "index.html")
	if bytes.Contains(index, []byte(frontend.APIOriginMarker)) ||
		bytes.Contains(index, []byte(frontend.PublicOriginMarker)) ||
		bytes.Contains(index, []byte(frontend.ContentSecurityPolicyMarker)) ||
		!bytes.Contains(index, []byte("https://dyd.mprlab.com")) ||
		!bytes.Contains(index, []byte("https://dyd-api.mprlab.com")) ||
		!bytes.Contains(index, []byte(`http-equiv="Content-Security-Policy"`)) ||
		bytes.Contains(index, []byte(`frame-ancestors`)) {
		testContext.Fatalf("rendered application index is not bound to the production origins")
	}

	uiConfig := string(readRenderedFile(testContext, outputRoot, "config-ui.yaml"))
	for _, expected := range []string{
		"- https://dyd.mprlab.com",
		"tauthUrl: https://dyd-api.mprlab.com",
		"tenantId: download-your-data",
		"sessionPath: /auth/session",
	} {
		if !strings.Contains(uiConfig, expected) {
			testContext.Fatalf("rendered browser configuration is missing %q", expected)
		}
	}

	publicSite, siteError := frontend.NewPublicSite("https://dyd.mprlab.com")
	if siteError != nil {
		testContext.Fatalf("build expected public site: %v", siteError)
	}
	for _, publicPath := range publicSite.Paths() {
		relativePath, pathError := publicOutputPath(publicPath)
		if pathError != nil {
			testContext.Fatalf("map expected public path %q: %v", publicPath, pathError)
		}
		expectedBody, _, found := publicSite.Read(publicPath)
		if !found {
			testContext.Fatalf("expected public document %q is missing", publicPath)
		}
		if renderedBody := readRenderedFile(testContext, outputRoot, relativePath); !bytes.Equal(renderedBody, expectedBody) {
			testContext.Fatalf("rendered public document %q differs from source contract", publicPath)
		}
	}

	for _, expectedAsset := range []string{
		"application/app.js",
		"application/auth-lifecycle.js",
		"styles/application.css",
		"images/favicon.svg",
	} {
		readRenderedFile(testContext, outputRoot, expectedAsset)
	}

	for _, forbidden := range []string{".env", "resources.yml", "production.yml"} {
		if _, statError := os.Stat(filepath.Join(outputRoot, forbidden)); !os.IsNotExist(statError) {
			testContext.Fatalf("deployment-only file %q escaped into Pages output", forbidden)
		}
	}
}

func TestRenderRequiresAnEmptyAbsoluteOutputDirectory(testContext *testing.T) {
	profilePath := filepath.Join("..", "..", "configs", "production.yml")
	if renderError := render(profilePath, "relative-output"); renderError == nil {
		testContext.Fatalf("relative Pages output was accepted")
	}

	outputRoot := testContext.TempDir()
	if writeError := os.WriteFile(filepath.Join(outputRoot, "existing"), []byte("occupied"), 0o600); writeError != nil {
		testContext.Fatalf("write occupied output marker: %v", writeError)
	}
	if renderError := render(profilePath, outputRoot); renderError == nil {
		testContext.Fatalf("non-empty Pages output was accepted")
	}
}

func TestPublicOutputPathRejectsTraversalAndNonDocuments(testContext *testing.T) {
	validPaths := map[string]string{
		"/resources/":             "resources/index.html",
		"/resources/netflix.html": "resources/netflix.html",
		"/robots.txt":             "robots.txt",
	}
	for publicPath, expected := range validPaths {
		mapped, pathError := publicOutputPath(publicPath)
		if pathError != nil || mapped != expected {
			testContext.Fatalf("map %q = %q, %v; want %q", publicPath, mapped, pathError, expected)
		}
	}
	for _, publicPath := range []string{"", "/", "resources/", "/resources//", "/../secret", `/resources\\index`} {
		if mapped, pathError := publicOutputPath(publicPath); pathError == nil {
			testContext.Fatalf("invalid public path %q mapped to %q", publicPath, mapped)
		}
	}
}

func readRenderedFile(testContext *testing.T, outputRoot string, relativePath string) []byte {
	testContext.Helper()
	body, readError := os.ReadFile(filepath.Join(outputRoot, filepath.FromSlash(relativePath)))
	if readError != nil {
		entries := []string{}
		_ = filepath.WalkDir(outputRoot, func(path string, entry os.DirEntry, walkError error) error {
			if walkError == nil && !entry.IsDir() {
				if relative, relativeError := filepath.Rel(outputRoot, path); relativeError == nil {
					entries = append(entries, relative)
				}
			}
			return nil
		})
		slices.Sort(entries)
		testContext.Fatalf("read rendered file %q: %v; artifact contains %v", relativePath, readError, entries)
	}
	return body
}
