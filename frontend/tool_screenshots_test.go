package frontend

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	toolScreenshotManifestPath = "manifests/tool-screenshots.json"
	toolScreenshotDirectory    = "images/tools/google-authenticator"
	toolScreenshotPathPrefix   = toolScreenshotDirectory + "/"
)

type toolScreenshotManifest struct {
	SchemaVersion int                           `json:"schema_version"`
	Screenshots   []toolScreenshotManifestEntry `json:"screenshots"`
}

type toolScreenshotManifestEntry struct {
	ID                    string                   `json:"id"`
	ExpectedVisibleLabels []string                 `json:"expected_visible_labels"`
	DirectRoute           string                   `json:"direct_route"`
	Surface               string                   `json:"surface"`
	OutputPath            string                   `json:"output_path"`
	SHA256                string                   `json:"sha256"`
	PixelDimensions       toolScreenshotDimensions `json:"pixel_dimensions"`
	CaptureDate           string                   `json:"capture_date"`
	ReviewStatus          string                   `json:"review_status"`
}

type toolScreenshotDimensions struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

func TestToolScreenshotContract(testContext *testing.T) {
	manifest := readToolScreenshotJSON[toolScreenshotManifest](testContext, toolScreenshotManifestPath)
	if manifest.SchemaVersion != 1 {
		testContext.Fatalf("tool screenshot manifest schema version = %d; want 1", manifest.SchemaVersion)
	}
	expectations := map[string]struct {
		host    string
		surface string
		width   int
		height  int
	}{
		"google-authenticator-simulator-onboarding": {host: "play.google.com", surface: "first_party_simulator_capture", width: 1080, height: 2400},
		"google-authenticator-transfer-help":        {host: "support.google.com", surface: "first_party_help_web", width: 1440, height: 1000},
		"apple-passwords-setup-key-help":            {host: "support.apple.com", surface: "first_party_help_web", width: 1440, height: 1000},
	}
	if len(manifest.Screenshots) != len(expectations) {
		testContext.Fatalf("tool screenshot count = %d; want %d", len(manifest.Screenshots), len(expectations))
	}
	seenIDs := make(map[string]struct{}, len(manifest.Screenshots))
	seenPaths := make(map[string]struct{}, len(manifest.Screenshots))
	for _, screenshot := range manifest.Screenshots {
		expectation, exists := expectations[screenshot.ID]
		if !exists {
			testContext.Fatalf("manifest contains unexpected tool screenshot %q", screenshot.ID)
		}
		if _, exists := seenIDs[screenshot.ID]; exists {
			testContext.Fatalf("manifest repeats tool screenshot %q", screenshot.ID)
		}
		seenIDs[screenshot.ID] = struct{}{}
		if len(screenshot.ExpectedVisibleLabels) == 0 {
			testContext.Fatalf("tool screenshot %q has no visible labels", screenshot.ID)
		}
		if screenshot.Surface != expectation.surface || screenshot.ReviewStatus != "approved" {
			testContext.Fatalf("tool screenshot %q is %q/%q; want %s/approved", screenshot.ID, screenshot.Surface, screenshot.ReviewStatus, expectation.surface)
		}
		if _, parseError := time.Parse(time.DateOnly, screenshot.CaptureDate); parseError != nil {
			testContext.Fatalf("tool screenshot %q capture date is invalid: %v", screenshot.ID, parseError)
		}
		directRoute, parseError := url.Parse(screenshot.DirectRoute)
		if parseError != nil || directRoute.Scheme != "https" || directRoute.Hostname() != expectation.host || directRoute.User != nil || directRoute.Fragment != "" {
			testContext.Fatalf("tool screenshot %q route is not the approved first-party HTTPS route: %q", screenshot.ID, screenshot.DirectRoute)
		}
		if !strings.HasPrefix(screenshot.OutputPath, toolScreenshotPathPrefix) || filepath.Ext(screenshot.OutputPath) != ".png" {
			testContext.Fatalf("tool screenshot %q output path is outside the tool asset directory: %q", screenshot.ID, screenshot.OutputPath)
		}
		if _, exists := seenPaths[screenshot.OutputPath]; exists {
			testContext.Fatalf("manifest repeats tool screenshot path %q", screenshot.OutputPath)
		}
		seenPaths[screenshot.OutputPath] = struct{}{}
		content, readError := os.ReadFile(screenshot.OutputPath)
		if readError != nil {
			testContext.Fatalf("read tool screenshot %q: %v", screenshot.ID, readError)
		}
		digest := sha256.Sum256(content)
		if actualDigest := hex.EncodeToString(digest[:]); actualDigest != screenshot.SHA256 {
			testContext.Fatalf("tool screenshot %q digest = %s; want %s", screenshot.ID, actualDigest, screenshot.SHA256)
		}
		config, decodeError := png.DecodeConfig(bytes.NewReader(content))
		if decodeError != nil {
			testContext.Fatalf("decode tool screenshot %q: %v", screenshot.ID, decodeError)
		}
		if config.Width != screenshot.PixelDimensions.Width || config.Height != screenshot.PixelDimensions.Height || config.Width != expectation.width || config.Height != expectation.height {
			testContext.Fatalf("tool screenshot %q dimensions = %dx%d; want %dx%d", screenshot.ID, config.Width, config.Height, expectation.width, expectation.height)
		}
	}

	directoryEntries, readDirectoryError := os.ReadDir(toolScreenshotDirectory)
	if readDirectoryError != nil {
		testContext.Fatalf("read tool screenshot directory: %v", readDirectoryError)
	}
	if len(directoryEntries) != len(expectations) {
		testContext.Fatalf("tool screenshot directory count = %d; want %d", len(directoryEntries), len(expectations))
	}
	for _, entry := range directoryEntries {
		path := filepath.ToSlash(filepath.Join(toolScreenshotDirectory, entry.Name()))
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".png" {
			testContext.Fatalf("tool screenshot directory contains unexpected entry %q", entry.Name())
		}
		if _, exists := seenPaths[path]; !exists {
			testContext.Fatalf("tool screenshot %q is not declared in the manifest", path)
		}
	}
}

func readToolScreenshotJSON[valueType any](testContext *testing.T, path string) valueType {
	testContext.Helper()
	content, readError := os.ReadFile(path)
	if readError != nil {
		testContext.Fatalf("read %s: %v", path, readError)
	}
	var value valueType
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if decodeError := decoder.Decode(&value); decodeError != nil {
		testContext.Fatalf("decode %s: %v", path, decodeError)
	}
	return value
}
